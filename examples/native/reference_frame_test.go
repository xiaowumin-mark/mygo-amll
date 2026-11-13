// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Keep both measured and dormant groups, plus independently timed background
// clocks. The fixed host size prevents a component-height change from also
// changing the font, as in the resize case of the original-player host oracle.
func referenceFrameScene(t *testing.T, initial time.Duration) (*scene, time.Time) {
	t.Helper()
	var lines []lyric.Line
	for i := range 24 {
		start := time.Duration(i) * 10 * time.Second
		id := fmt.Sprintf("frame:%d", i)
		lines = append(lines, lyric.Line{ID: id, Text: "AA BB", Start: start, End: start + 2*time.Second,
			Words: []lyric.Word{{Text: "AA ", Start: start, End: start + time.Second}, {Text: "BB", Start: start + time.Second, End: start + 2*time.Second}}})
		if i%7 == 0 {
			lines = append(lines, lyric.Line{ID: id + ":bg", ParentID: id, Background: true, Text: "CC DD", Start: start + time.Second/4, End: start + 7*time.Second/4,
				Words: []lyric.Word{{Text: "CC ", Start: start + time.Second/4, End: start + time.Second}, {Text: "DD", Start: start + time.Second, End: start + 7*time.Second/4}}})
		}
	}
	s := newScene(geometryDocument(t, lines), time.Unix(0, 0))
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.setReferenceHostSize(800, 500)
	s.clock.setPlaying(s.origin, true)
	box := ui.Rect{W: 800, H: 500}
	for i := range 81 {
		now := s.origin.Add(time.Duration(i) * 16 * time.Millisecond)
		s.clock.seek(now, initial)
		s.prepareReferenceFrame(now, box, 1, true)
	}
	return s, s.origin.Add(81 * 16 * time.Millisecond)
}

type referenceFrameScalars struct {
	groups                       []referenceGroupMotion
	clocks                       []motion.LineAnimationClock
	masks                        []referenceLineMask
	heights                      []float64
	measured                     []bool
	previousPlaying, highlighted []int
	interlude                    motion.Interlude
	pose                         motion.InterludePose
	interludeCSS                 referenceTransformTween
	interludeMetrics             referenceInterludeMetrics
	interludeObservedContent     [2]float64
	interludeObserved            bool
	header                       referenceStep
	firstStep, lastStep          uint64
	lastFrame                    time.Time
	offset, minOffset, maxOffset float64
}

func referenceFrameSnapshot(r *referenceScene) referenceFrameScalars {
	// A row outside the prepared viewport must consume the replacement header,
	// too. Comparing only eagerly prepared rows would miss stale lazy history.
	for i := range r.groups {
		r.replay(i, r.lastStep)
	}
	state := referenceFrameScalars{
		groups: append([]referenceGroupMotion(nil), r.groups...), clocks: append([]motion.LineAnimationClock(nil), r.clocks...),
		masks:    append([]referenceLineMask(nil), r.maskSignatures...),
		measured: append([]bool(nil), r.measured...), previousPlaying: append([]int(nil), r.previousPlaying...), highlighted: append([]int(nil), r.previousHighlighted...),
		interlude: r.interlude, pose: r.interludePose, interludeCSS: r.interludeCSS,
		interludeMetrics: r.interludeMetrics, interludeObservedContent: r.interludeObservedContent, interludeObserved: r.interludeObserved,
		firstStep: r.firstStep, lastStep: r.lastStep, lastFrame: r.lastFrame,
		offset: r.offset, minOffset: r.minOffset, maxOffset: r.maxOffset,
	}
	for i := range r.groups {
		state.heights = append(state.heights, r.heights.Height(i))
	}
	if r.lastStep > 0 {
		state.header = r.history[(r.lastStep-1)%uint64(len(r.history))]
	}
	return state
}

func assertReferenceFrameScalars(t *testing.T, got, want referenceFrameScalars) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	for i := range got.groups {
		if i < len(want.groups) && got.groups[i] != want.groups[i] {
			t.Errorf("group %d differs after replacement frame:\n got %+v\nwant %+v", i, got.groups[i], want.groups[i])
			break
		}
	}
	if !reflect.DeepEqual(got.clocks, want.clocks) {
		t.Error("line clocks differ after replacement frame")
	}
	if got.interlude != want.interlude || got.pose != want.pose || got.interludeCSS != want.interludeCSS {
		t.Errorf("dots differ after replacement frame:\n got %+v\nwant %+v", got.pose, want.pose)
	}
	if got.header != want.header {
		t.Errorf("replacement header differs:\n got %+v\nwant %+v", got.header, want.header)
	}
	t.Fatalf("replacement frame scalar state differs (steps %d/%d versus %d/%d, offset %g versus %g)", got.firstStep, got.lastStep, want.firstStep, want.lastStep, got.offset, want.offset)
}

func TestReferenceFrameViewportReplacementMatchesKnownSize(t *testing.T) {
	for _, test := range []struct {
		name           string
		initial, media time.Duration
	}{
		{"playing", 0, 1250 * time.Millisecond},
		{"focus-change", 0, 10016 * time.Millisecond},
		{"interlude", 2 * time.Second, 2750 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			late, now := referenceFrameScene(t, test.initial)
			early, _ := referenceFrameScene(t, test.initial)
			late.clock.seek(now, test.media)
			early.clock.seek(now, test.media)
			// The view observes the previous box; Draw observes the new box at
			// the same timestamp. The control knows that box before preparing.
			late.updateReference(now, false)
			before := late.reference.lastStep
			box := ui.Rect{W: 800, H: 560}
			late.prepareReferenceFrame(now, box, 1, true)
			early.prepareReferenceFrame(now, box, 1, true)
			if late.reference.lastStep != before {
				t.Fatal("viewport replacement appended an extra elapsed frame")
			}
			assertReferenceFrameScalars(t, referenceFrameSnapshot(late.reference), referenceFrameSnapshot(early.reference))
			if test.name == "interlude" && (!late.reference.interludePose.Active || late.reference.interludePose.Raw.Opacity == 0) {
				t.Fatal("fixture did not exercise visible dots")
			}
		})
	}
}

func TestReferenceFrameViewportRoundTripAndRepeatDoNotAdvanceHost(t *testing.T) {
	s, now := referenceFrameScene(t, 2*time.Second)
	control, _ := referenceFrameScene(t, 2*time.Second)
	media := 2750 * time.Millisecond
	s.clock.seek(now, media)
	control.clock.seek(now, media)
	requests := 0
	s.onSeek = func(time.Time, seekRequest) { requests++ }
	s.requestSeek(now, 0)
	clock := s.clock
	box := ui.Rect{W: 800, H: 500}
	s.updateReference(now, false)
	control.prepareReferenceFrame(now, box, 1, true)
	step := s.reference.lastStep
	box.H = 560
	s.prepareReferenceFrame(now, box, 1, true)
	box.H = 500
	s.prepareReferenceFrame(now, box, 1, true)
	got := referenceFrameSnapshot(s.reference)
	assertReferenceFrameScalars(t, got, referenceFrameSnapshot(control.reference))
	for range 3 {
		s.prepareReferenceFrame(now, box, 1, true)
		assertReferenceFrameScalars(t, referenceFrameSnapshot(s.reference), got)
	}
	if s.reference.lastStep != step || s.clock != clock || s.clock.positionAt(now) != media || requests != 1 {
		t.Fatalf("repeated Draw changed host state: step %d/%d, clock %+v/%+v, requests %d", s.reference.lastStep, step, s.clock, clock, requests)
	}
}

func TestReferenceFrameViewportRoundTripRestoresClampedOffset(t *testing.T) {
	s := referenceTestScene(t, 8)
	s.forceStatic = true
	s.setReferenceHostSize(800, 500)
	box := ui.Rect{W: 800, H: 500}
	s.prepareReferenceFrame(s.origin, box, 1, false)
	r := s.reference
	for _, measured := range r.measured {
		if !measured {
			t.Fatal("offset fixture must have stable measured heights")
		}
	}
	r.offset, r.suspended, r.dirty = r.maxOffset-1, true, true
	want := r.offset
	now := s.origin.Add(16 * time.Millisecond)
	s.prepareReferenceFrame(now, box, 1, false)
	if r.offset != want {
		t.Fatal("initial offset was already out of range")
	}
	box.H = 560
	s.prepareReferenceFrame(now, box, 1, false)
	if r.offset >= want || r.offset != r.maxOffset {
		t.Fatalf("fixture did not temporarily clamp offset: %g, max %g, original %g", r.offset, r.maxOffset, want)
	}
	box.H = 500
	s.prepareReferenceFrame(now, box, 1, false)
	if r.offset != want {
		t.Fatalf("temporary viewport clamp escaped rollback: got %g want %g", r.offset, want)
	}
}

func TestReferenceFrameViewportReplacementRejectsStaleCheckpoint(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprintf("completed=%t", completed), func(t *testing.T) {
			s, now := referenceFrameScene(t, 2*time.Second)
			control, _ := referenceFrameScene(t, 2*time.Second)
			s.clock.seek(now, 2750*time.Millisecond)
			control.clock.seek(now, 2750*time.Millisecond)
			s.updateReference(now, false)
			r := s.reference
			task := snapshotReferenceCheckpoint(r)
			if task == nil {
				t.Fatal("valid provisional checkpoint rejected")
			}
			if completed {
				task.run()
				if !task.complete {
					t.Fatal("checkpoint did not complete")
				}
				// Isolated synthetic poison makes any stale publication visible even
				// if a dormant group's provisional and replacement Y happen to match.
				for i := range task.groups {
					task.groups[i].prefix += 1e6
				}
			}
			r.checkpoint = task
			first := r.firstStep
			box := ui.Rect{W: 800, H: 560}
			s.prepareReferenceFrame(now, box, 1, true)
			control.prepareReferenceFrame(now, box, 1, true)
			if !task.cancelled.Load() || r.firstStep != first || r.frameBudgetBroken || r.historyBroken {
				t.Fatal("provisional checkpoint was published before replacement invalidated it")
			}
			if !completed {
				if r.checkpoint != task {
					t.Fatal("cancellation released the unfinished worker slot")
				}
				r.startCheckpoint()
				if r.checkpoint != task {
					t.Fatal("replacement allocated a second unfinished checkpoint")
				}
				task.run() // cancellation closes done without replay or publication.
				r.pollCheckpoint()
			}
			if r.checkpoint != nil {
				t.Fatal("finished stale worker retained its slot")
			}
			assertReferenceFrameScalars(t, referenceFrameSnapshot(r), referenceFrameSnapshot(control.reference))
		})
	}
}

func TestReferenceFrameSnapshotBudgetFallbackAndClose(t *testing.T) {
	for _, budget := range []string{"groups", "heights", "masks"} {
		t.Run(budget, func(t *testing.T) {
			count := 8
			if budget == "groups" {
				count = referenceFrameGroupLimit + 1
			}
			s := referenceTestScene(t, count)
			s.forceStatic = true
			box := ui.Rect{W: 800, H: 500}
			s.prepareReferenceFrame(s.origin, box, 1, false)
			now := s.origin.Add(16 * time.Millisecond)
			s.prepareReferenceFrame(now, box, 1, false)
			r := s.reference
			task := &referenceCheckpointTask{done: make(chan struct{}), generation: r.checkpointGeneration}
			r.checkpoint = task
			if budget == "groups" {
				for i := range r.groups {
					r.replay(i, r.lastStep)
				}
			} else if budget == "heights" {
				for range referenceFrameHeightLimit + 1 {
					r.captureFrameHeight(0, r.heights.Height(0), r.measured[0])
				}
			} else {
				for i := range referenceFrameGroupLimit*2 + 1 {
					geometry := &lineGeometry{maskSignature: [32]byte{byte(i), byte(i >> 8)}}
					r.recordMaskSignature(0, geometry)
				}
			}
			if !r.frameBudgetBroken || !r.historyBroken || r.frameBase.valid || !task.cancelled.Load() || r.checkpoint != task {
				t.Fatal("snapshot budget did not invalidate the candidate and cancel its worker")
			}
			if r.frameBase.count > referenceFrameGroupLimit || r.frameBase.heightCount > referenceFrameHeightLimit {
				t.Fatal("snapshot storage exceeded its fixed bounds")
			}
			s.prepareReferenceFrame(now.Add(16*time.Millisecond), box, 1, false)
			if !strings.Contains(r.fallback, "Layout frame budget fallback") {
				t.Fatalf("budget fallback was not explicit: %q", r.fallback)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if s.reference != nil || r.frameBase != nil || r.checkpoint != nil || r.heights != nil || r.history != nil || r.motionFrames != nil || r.groups != nil || r.clocks != nil || r.maskSignatures != nil {
				t.Fatal("Close retained frame snapshot or worker owner storage")
			}
		})
	}
}

func TestReferenceFrameMaskSignaturesDistinguishMetricsFromCache(t *testing.T) {
	s, now := referenceFrameScene(t, 500*time.Millisecond)
	r := s.reference
	line := r.presentation.groups[0].mainLayout
	box := ui.Rect{W: 800, H: 500}
	s.clock.seek(now, 500*time.Millisecond)
	s.prepareReferenceFrame(now, box, 1, true)
	before := r.clocks[line].Position(now, motion.EmphasisGlowAnimation, 5*time.Second)
	if before <= 500*time.Millisecond {
		t.Fatal("held-media fixture did not advance its independent wall clock")
	}
	signature := r.maskSignatures[line]
	box.W = 700
	s.prepareReferenceFrame(now, box, 1, true)
	if r.maskSignatures[line] == signature || r.clocks[line].Position(now, motion.EmphasisGlowAnimation, 5*time.Second) != 500*time.Millisecond {
		t.Fatal("true container metric change did not reanchor the enabled line")
	}
	box.W = 800
	s.prepareReferenceFrame(now, box, 1, true)
	if r.maskSignatures[line] != signature || r.clocks[line].Position(now, motion.EmphasisGlowAnimation, 5*time.Second) != before {
		t.Fatal("same-timestamp width round trip retained a provisional mask reset")
	}
	// A fresh geometry pointer after cache eviction or DPR recreation has the
	// same CSS signature. It must not act like a new Web Animations object.
	now = now.Add(16 * time.Millisecond)
	s.clock.seek(now, 500*time.Millisecond)
	s.cache.clear()
	s.prepareReferenceFrame(now, box, 2, true)
	if r.maskSignatures[line] != signature || r.clocks[line].Position(now, motion.EmphasisGlowAnimation, 5*time.Second) != before+16*time.Millisecond {
		t.Fatal("cache/DPR-only recreation reset an unchanged CSS mask")
	}
	now = now.Add(16 * time.Millisecond)
	s.clock.seek(now, 500*time.Millisecond)
	s.font.Size = 36
	s.prepareReferenceFrame(now, box, 2, true)
	if r.maskSignatures[line] == signature || r.clocks[line].Position(now, motion.EmphasisGlowAnimation, 5*time.Second) != 500*time.Millisecond {
		t.Fatal("changed measured font metrics did not reanchor the enabled line")
	}
}
