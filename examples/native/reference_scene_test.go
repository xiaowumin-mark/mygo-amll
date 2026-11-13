// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func referenceTestScene(t testing.TB, count int) *scene {
	t.Helper()
	lines := make([]lyric.Line, count)
	for i := range lines {
		start := time.Duration(i) * 3 * time.Second
		lines[i] = lyric.Line{ID: fmt.Sprintf("reference:%d", i), Text: "word", Start: start, End: start + 2*time.Second, Words: []lyric.Word{{Text: "word", Start: start, End: start + 2*time.Second}}}
	}
	s := newScene(geometryDocument(t, lines), time.Unix(0, 0))
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestReferenceFrameCenterAndActualContentWidth(t *testing.T) {
	s := referenceTestScene(t, 40)
	s.forceStatic = true
	s.font.Size = 0
	s.setReferenceHostSize(960, 740)
	box := ui.Rect{W: 800.625, H: 500}
	s.prepareReferenceFrame(s.origin, box, 1, false)
	r := s.reference
	if r.font.Size != 37 || r.width != box.W-74 {
		t.Fatalf("font/actual width = %v/%v", r.font.Size, r.width)
	}
	found := false
	for _, row := range r.rows[:r.rowCount] {
		if row.index == 0 {
			found = true
			if math.Abs(float64(row.main.width-(box.W-74-2*cssLayoutLength(.2*37)))) > .001 {
				t.Fatal("CSS line padding was not removed from the actual fractional content width")
			}
			if math.Abs(float64(row.y+row.height/2-box.H*.35)) > .051 {
				t.Fatalf("focus center=%v want %v", row.y+row.height/2, box.H*.35)
			}
		}
	}
	if !found {
		t.Fatal("focus not prepared")
	}
	if r.rowCount > geometryEntryLimit || len(s.cache.entries) > geometryEntryLimit || s.cache.bytes > geometryByteLimit {
		t.Fatal("unbounded geometry")
	}
	if referenceFont(ui.Font{}, 700, 740).Size != 56 {
		t.Fatal("narrow media query did not use host viewport")
	}
}

func TestReferenceFrameIdempotentAndReadOnlyFocus(t *testing.T) {
	s := referenceTestScene(t, 20)
	s.forceStatic = true
	now := s.origin
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, false)
	r := s.reference
	before, rows, state := r.lastStep, r.rows, r.state
	s.prepareReferenceFrame(now, r.box, 1, false)
	if r.lastStep != before || !reflect.DeepEqual(rows, r.rows) {
		t.Fatal("same timestamp/revision prepared twice")
	}
	if got := s.focusAt(30 * time.Second); got != 10 {
		t.Fatalf("focus=%d", got)
	}
	if !reflect.DeepEqual(state, r.state) {
		t.Fatal("accessibility/focus query changed frozen frame state")
	}
	s.setReferenceHover(true)
	s.prepareReferenceFrame(now, r.box, 1, false)
	if r.lastStep != before+1 {
		t.Fatal("same-time configuration revision was lost")
	}
}

func TestReferenceSeekUsesSlowSpringWithoutSnap(t *testing.T) {
	s := referenceTestScene(t, 40)
	now := s.origin
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, true)
	// Stay before the first lyrics enter the real viewport: no GPU window is
	// needed to check the same spring events used by the accelerated host.
	now = now.Add(10 * time.Millisecond)
	s.updateReference(now, false)
	r := s.reference
	before := r.groups[0].y.Sample(now)
	s.seek(now, 60*time.Second)
	after := r.groups[0].y.Sample(now)
	if before.Position != after.Position {
		t.Fatalf("seek snapped Y: %v -> %v", before, after)
	}
	if r.lastParams != motion.RefYParams(true, false, false, 0, false) {
		t.Fatalf("seek policy=%+v", r.lastParams)
	}
	s.updateReference(now.Add(time.Millisecond), false)
	if r.lastParams.Stiffness != 90 || r.lastParams.Damping != 15 {
		t.Fatal("seek spring prematurely restored normal policy")
	}
}

func TestReferenceBackgroundFlowAndWrapperComposition(t *testing.T) {
	main, bg := &lineGeometry{height: 80}, &lineGeometry{height: 40}
	if got := referenceGroupHeight(main, bg, 30, false, false, -80); math.Abs(got-104) > 1e-6 {
		t.Fatalf("bottom collapsed height=%v", got)
	}
	if got := referenceGroupHeight(main, bg, 30, false, true, 0); math.Abs(got-153) > 1e-6 {
		t.Fatalf("bottom expanded height=%v", got)
	}
	if got := referenceGroupHeight(main, bg, 30, true, false, 80); math.Abs(got-113) > 1e-6 {
		t.Fatalf("top collapse must retain flex gap: %v", got)
	}
	if got := referenceGroupHeight(main, bg, 30, true, false, 40); math.Abs(got-133) > 1e-6 {
		t.Fatalf("top halfway height=%v", got)
	}
}

func TestReferenceInteractionThresholdWheelAndInertia(t *testing.T) {
	s := referenceTestScene(t, 30)
	s.forceStatic = true
	now := s.origin
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, false)
	s.input(ui.InputEvent{Kind: ui.InputPointerDown, Time: now, X: 100, Y: 100})
	s.input(ui.InputEvent{Kind: ui.InputPointerMove, Time: now.Add(time.Millisecond), X: 110, Y: 100})
	if s.dragged || s.reference.suspended {
		t.Fatal("exactly 10px is below intent threshold")
	}
	s.input(ui.InputEvent{Kind: ui.InputPointerMove, Time: now.Add(20 * time.Millisecond), X: 111, Y: 80})
	if !s.dragged || !s.reference.touch || !s.reference.continuous {
		t.Fatal("drag intent not confirmed")
	}
	s.input(ui.InputEvent{Kind: ui.InputPointerUp, Time: now.Add(21 * time.Millisecond)})
	if !s.reference.inertia {
		t.Fatal("fast release did not start inertia")
	}
	s.input(ui.InputEvent{Kind: ui.InputScroll, Time: now.Add(30 * time.Millisecond), DY: 20})
	if s.reference.inertia || !s.reference.touch {
		t.Fatal("wheel did not interrupt inertia while preserving the existing interaction type")
	}
	s.updateReference(now.Add(179*time.Millisecond), false)
	if !s.reference.endedInteraction.IsZero() {
		t.Fatal("wheel ended before 150ms debounce")
	}
	s.updateReference(now.Add(180*time.Millisecond), false)
	if s.reference.endedInteraction != now.Add(180*time.Millisecond) {
		t.Fatal("wheel debounce did not end interaction")
	}
}

func TestReferenceLazyScalarReplayAndRingRollover(t *testing.T) {
	eager := referenceTestScene(t, 4)
	lazy := referenceTestScene(t, 4)
	now := eager.origin
	font := ui.Font{Size: 30, LineHeight: 1.2}
	box := ui.Rect{W: 800, H: 500}
	eager.resetReferenceGeometry(now, font, 740, 1, box)
	lazy.resetReferenceGeometry(now, font, 740, 1, box)
	// A small ring makes rollover deterministic independently of worker timing.
	// The checkpoint worker's publish/cancel paths have dedicated tests.
	const ringSize = 64
	for _, r := range []*referenceScene{eager.reference, lazy.reference} {
		r.history = make([]referenceStep, ringSize)
		r.motionFrames = make([]motion.RefFrame, ringSize)
	}
	for frame := 0; frame < ringSize*2+31; frame++ {
		now = now.Add(3 * time.Millisecond)
		focus := (frame / 100) % 4
		step := referenceStep{now: now, delta: 3 * time.Millisecond, media: time.Duration(frame) * time.Millisecond, focus: focus, latest: focus, firstVisible: 0, windowFrom: focus - 2, windowTo: focus + 2, highlighting: true, latestStart: time.Duration(focus) * 3 * time.Second, playing: true, dynamic: true, base: float64(-focus * 100), height: 500, width: 800, params: motion.RefYParams(false, false, false, time.Second, true), paramsChanged: frame%100 == 0, layout: frame%37 == 0, stagger: true, interludeAnchor: -2}
		if frame%57 == 0 {
			step.changes[0] = referenceHeightChange{index: 0, delta: .25}
			step.changeCount = 1
		}
		for _, r := range []*referenceScene{eager.reference, lazy.reference} {
			if r.lastStep-r.firstStep+1 >= ringSize {
				for i := range r.groups {
					r.replay(i, r.lastStep)
				}
				r.firstStep = r.lastStep + 1
			}
			r.appendStep(step)
		}
		for index := range eager.reference.groups {
			eager.reference.replay(index, eager.reference.lastStep)
		}
	}
	for index := range lazy.reference.groups {
		lazy.reference.replay(index, lazy.reference.lastStep)
		a, b := eager.reference.groups[index], lazy.reference.groups[index]
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("group %d lazy replay differs after ring rollover\neager=%+v\nlazy=%+v", index, a, b)
		}
	}
	if len(lazy.reference.history) != ringSize {
		t.Fatal("history grew beyond fixed bound")
	}
}

func TestReferenceCloseDropsPresentationAndFrameResources(t *testing.T) {
	s := referenceTestScene(t, 10)
	s.forceStatic = true
	s.prepareReferenceFrame(s.origin, ui.Rect{W: 800, H: 500}, 1, false)
	r := s.reference
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.reference != nil || r.presentation != nil || r.index != nil || r.heights != nil || r.history != nil || r.groups != nil || r.clocks != nil || r.root != nil || r.rowCount != 0 {
		t.Fatal("close retained reference state")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceResizeKeepsDormantHistoryAndMotion(t *testing.T) {
	s := referenceTestScene(t, 200)
	now := s.origin
	box := ui.Rect{W: 800, H: 500}
	s.prepareReferenceFrame(now, box, 1, true)
	r := s.reference
	before := r.groups[199]
	lastStep := r.lastStep
	box.H = 600
	s.prepareReferenceFrame(now, box, 1, true)
	if r.lastStep <= lastStep || r.firstStep != 1 {
		t.Fatal("resize discarded reference history")
	}
	if r.groups[199] != before {
		t.Fatal("resize traversed/advanced dormant motion")
	}
	if r.history[(r.lastStep-1)%uint64(len(r.history))].estimateDelta != 20 {
		t.Fatal("resize did not log new H/5 estimate")
	}
	r.replay(199, r.lastStep)
	if math.Abs(r.groups[199].prefix-r.heights.Prefix(199)) > 1e-7 || math.Abs(r.groups[199].height-r.heights.Height(199)) > 1e-7 {
		t.Fatalf("lazy resize geometry differs: %+v", r.groups[199])
	}
}

func TestReferenceMeasuredPrefixAcrossRepeatedResize(t *testing.T) {
	r := referenceCheckpointFixture(4, 0, 1)
	for i := range r.groups {
		r.groups[i].prefix = float64(i) * 100
		r.groups[i].height = 100
	}
	now := time.Unix(0, 0)
	steps := []referenceStep{
		{id: 1, layout: true, static: true, snap: true, base: 0, height: 500, width: 800, interludeAnchor: -2, changeCount: 2, changes: [maxPreparedLines]referenceHeightChange{{index: 1, delta: -20, firstMeasurement: true}, {index: 3, delta: 20, firstMeasurement: true}}},
		{id: 2, layout: true, static: true, snap: true, height: 600, width: 800, interludeAnchor: -2, estimateDelta: 20},
		{id: 3, layout: true, static: true, snap: true, height: 600, width: 800, interludeAnchor: -2, changeCount: 1, changes: [maxPreparedLines]referenceHeightChange{{index: 0, firstMeasurement: true}}},
		{id: 4, layout: true, static: true, snap: true, height: 550, width: 800, interludeAnchor: -2, estimateDelta: -10},
	}
	for k := range steps {
		steps[k].now = now.Add(time.Duration(k+1) * time.Millisecond)
		steps[k].delta = time.Millisecond
	}
	for i := range r.groups {
		metadata := r.metadata[i]
		for _, step := range steps {
			referenceApplyStep(&r.groups[i], &r.clocks[2*i], &r.clocks[2*i+1], metadata, step)
		}
	}
	wantPrefix := []float64{0, 120, 200, 310}
	wantHeight := []float64{120, 80, 110, 120}
	for i, g := range r.groups {
		if g.prefix != wantPrefix[i] || g.height != wantHeight[i] {
			t.Fatalf("group %d prefix/height=%v/%v want %v/%v", i, g.prefix, g.height, wantPrefix[i], wantHeight[i])
		}
	}
}

func TestReferenceInterludeUsesControllerAndCenterLayout(t *testing.T) {
	doc := geometryDocument(t, []lyric.Line{{Text: "first", End: time.Second}, {Text: "next", Start: 12 * time.Second, End: 14 * time.Second, Duet: true}})
	s := newScene(doc, time.Unix(0, 0))
	defer s.Close()
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	s.forceStatic = true
	s.clock.seek(s.origin, 2*time.Second)
	s.prepareReferenceFrame(s.origin, ui.Rect{W: 800, H: 500}, 1, false)
	r := s.reference
	if r.interludePose.Phase != motion.InterludePerforming || !r.alignInterlude {
		t.Fatalf("interlude not focused: %+v", r.interludePose)
	}
	metrics := r.interludeMetrics
	if math.Abs(r.interludePose.Y+metrics.Height/2-175) > .011 {
		t.Fatalf("interlude center=%v", r.interludePose.Y+metrics.Height/2)
	}
	if math.Abs(r.interludePose.X+metrics.Width-800) > .011 {
		t.Fatal("next duet did not right-align interlude")
	}
	// Pause freezes body animation; paint and waiting cannot advance it.
	before := r.interludePose
	s.updateReference(s.origin.Add(10*time.Second), false)
	if r.interludePose.Raw != before.Raw {
		t.Fatal("paused interlude advanced with wall time")
	}
}

func TestReferenceBackgroundClickRequestsMainIdentity(t *testing.T) {
	doc := geometryDocument(t, []lyric.Line{
		{ID: "bg", ParentID: "main", Text: "(bg)", Start: time.Second, End: 4 * time.Second, Background: true, Words: []lyric.Word{{Text: "(bg)", Start: time.Second, End: 2 * time.Second}}},
		{ID: "main", Text: "main", Start: 2 * time.Second, End: 4 * time.Second, Words: []lyric.Word{{Text: "main", Start: 2 * time.Second, End: 4 * time.Second}}},
	})
	s := newScene(doc, time.Unix(0, 0))
	defer s.Close()
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	s.forceStatic = true
	s.prepareReferenceFrame(s.origin, ui.Rect{W: 800, H: 500}, 1, false)
	r := s.reference
	if r.rowCount != 1 || r.rows[0].background == nil {
		t.Fatal("background not grouped")
	}
	row := r.rows[0]
	y := row.y + row.layers[1].Y + row.background.height/2
	index := s.referenceHit(row.x+10, y)
	if index < 0 || s.document.Line(index).ID() != "main" {
		t.Fatalf("background hit=%d", index)
	}
	var request seekRequest
	s.onSeek = func(_ time.Time, r seekRequest) { request = r }
	s.requestSeek(s.origin, index)
	if request.LineID != "main" {
		t.Fatal("background click exposed background identity")
	}
}

func TestReferenceCSSTransitionReversalShortensWithoutJump(t *testing.T) {
	now := time.Unix(0, 0)
	var tween referenceTween
	tween.snap(0)
	tween.set(now, 1, 400*time.Millisecond, false)
	when := now.Add(200 * time.Millisecond)
	before := tween.sample(when)
	tween.set(when, 0, 400*time.Millisecond, false)
	if tween.sample(when) != before {
		t.Fatal("reversal jumped at its event timestamp")
	}
	want := time.Duration(math.Round(float64(400*time.Millisecond) * motion.Ease(.5)))
	if tween.duration != want {
		t.Fatalf("reversed duration=%v want %v", tween.duration, want)
	}
	if tween.sample(when.Add(want)) != 0 {
		t.Fatal("shortened reverse did not finish")
	}
}

func TestReferenceDetachedOpacityWaitsForShowMeasurement(t *testing.T) {
	r := referenceCheckpointFixture(1, 0, 1)
	g := &r.groups[0]
	now := time.Unix(0, 0)
	step := referenceStep{id: 1, now: now, layout: true, base: 0, height: 500, width: 800, focus: 0, latest: 0, highlighting: true, latestStart: 0, dynamic: true, windowFrom: -2, windowTo: 2, interludeAnchor: -2}
	referenceApplyStep(g, &r.clocks[0], &r.clocks[1], r.metadata[0], step)
	if g.connected || g.opacity.running(now) || g.opacity.to != 1 || g.opacityTarget != .85 {
		t.Fatal("detached group started its opacity transition")
	}
	now = now.Add(200 * time.Millisecond)
	g.y.SetPosition(now, 100)
	step.id, step.now, step.layout = 2, now, false
	referenceApplyStep(g, &r.clocks[0], &r.clocks[1], r.metadata[0], step)
	if !g.connected || !g.opacity.running(now) || g.opacity.sample(now) != 1 || g.opacity.sample(now.Add(400*time.Millisecond)) != .85 {
		t.Fatal("show did not begin a fresh 400ms parent transition")
	}
}

func TestReferenceReplacementCancelsPendingCheckpoint(t *testing.T) {
	s := referenceTestScene(t, 5)
	old := s.reference
	task := &referenceCheckpointTask{done: make(chan struct{})}
	old.checkpoint = task
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	if !task.cancelled.Load() || old.checkpoint != nil || old.presentation != nil || old.root != nil {
		t.Fatal("replacing a song retained its checkpoint or resources")
	}
}

func TestReferenceHistoryBudgetHasExplicitStaticFallback(t *testing.T) {
	s := referenceTestScene(t, 10)
	s.reference.historyBroken = true
	s.prepareReferenceFrame(s.origin, ui.Rect{W: 800, H: 500}, 1, true)
	r := s.reference
	if !r.lastStatic || r.fallback != "Motion history budget fallback: static lyrics; checkpoint exceeded its bounded time or memory window" || r.checkpoint != nil {
		t.Fatalf("history overflow did not select labeled bounded fallback: %q", r.fallback)
	}
}

func TestReferenceMediaPositionRoundsWithoutChangingHostClock(t *testing.T) {
	for _, test := range []struct{ input, want time.Duration }{
		{999490 * time.Microsecond, 999 * time.Millisecond},
		{999500 * time.Microsecond, time.Second},
		{-500 * time.Microsecond, 0},
		{-500*time.Microsecond - time.Nanosecond, -time.Millisecond},
		{time.Duration(1<<63 - 1), time.Duration(1<<63 - 1)},
		{time.Duration(-1 << 63), time.Duration(-1 << 63)},
	} {
		if got := referenceMediaPosition(test.input); got != test.want {
			t.Errorf("referenceMediaPosition(%v)=%v want %v", test.input, got, test.want)
		}
	}
	s := referenceTestScene(t, 2)
	position := 999500 * time.Microsecond
	s.clock.seek(s.origin, position)
	_ = s.reference.visualAt(s.clock.positionAt(s.origin))
	if s.clock.positionAt(s.origin) != position {
		t.Fatal("visual millisecond rounding mutated the host's nanosecond clock")
	}
}
