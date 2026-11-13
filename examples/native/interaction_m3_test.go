package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Keep interaction timing independent of the longer visual demonstration. The
// tester supplies pointer capture, cancellation, keyboard dispatch and its clock.
func newInteractionScene(t *testing.T) (*scene, *ui.Tester) {
	t.Helper()
	lines := make([]lyric.Line, 30)
	for i := range lines {
		start := time.Duration(i) * 2 * time.Second
		suffix := fmt.Sprintf("words %d", i)
		lines[i] = lyric.Line{
			ID: fmt.Sprintf("interaction:%d", i), Text: "Timed " + suffix,
			Start: start, End: start + 1800*time.Millisecond,
			Words: []lyric.Word{
				{Text: "Timed ", Start: start, End: start + 900*time.Millisecond},
				{Text: suffix, Start: start + 900*time.Millisecond, End: start + 1800*time.Millisecond},
			},
		}
	}
	document, err := lyric.NewDocument(lines)
	if err != nil {
		t.Fatal(err)
	}
	s := newScene(document, time.Time{})
	t.Cleanup(func() { _ = s.Close() })
	tt := ui.NewTester(s.view, 960, 740)
	s.origin = tt.Now()
	s.clock = newPlaybackClock(s.duration, tt.Now())
	s.seek(tt.Now(), 0)
	tt.Frame()
	return s, tt
}

func interactionRowPoint(t *testing.T, s *scene, index int) (float32, float32) {
	t.Helper()
	for _, row := range s.rows[:s.rowCount] {
		if row.index == index && row.geometry != nil {
			y := row.y + row.geometry.height/2
			if y >= s.viewport.Y && y < s.viewport.Y+s.viewport.H {
				return s.viewport.X + 40, y
			}
		}
	}
	t.Fatalf("line %d has no visible hit point", index)
	return 0, 0
}

func TestM3TesterLyricClickRequiresHostSeek(t *testing.T) {
	s, tt := newInteractionScene(t)
	x, y := interactionRowPoint(t, s, 1)
	tt.ClickAt(x, y)
	if s.selectedLine != 1 || s.clock.positionAt(tt.Now()) != 0 {
		t.Fatal("click without a host callback should select the line without moving playback")
	}
	var requests []seekRequest
	s.onSeek = func(_ time.Time, request seekRequest) { requests = append(requests, request) }
	x, y = interactionRowPoint(t, s, 1)
	tt.ClickAt(x, y)
	if len(requests) != 1 || requests[0].LineID != "interaction:1" || requests[0].Position != 2*time.Second {
		t.Fatalf("click did not report the stable line identity and absolute start: %+v", requests)
	}
	if s.clock.positionAt(tt.Now()) != 0 {
		t.Fatal("recording a request implicitly changed host playback")
	}
	s.onSeek = func(at time.Time, request seekRequest) { s.seek(at, request.Position) }
	x, y = interactionRowPoint(t, s, 1)
	tt.ClickAt(x, y)
	if s.clock.positionAt(tt.Now()) != 2*time.Second || s.manualLine != -1 {
		t.Fatal("host-accepted click did not seek and restore automatic following")
	}
}

func TestM3TesterDragCancelAndSeekDuringGesture(t *testing.T) {
	s, tt := newInteractionScene(t)
	requests := 0
	s.onSeek = func(time.Time, seekRequest) { requests++ }
	x, y := interactionRowPoint(t, s, 0)
	tt.Press(x, y)
	tt.Move(x, y-80)
	if !s.pointerDown || !s.dragged || s.manualLine < 0 {
		t.Fatal("captured pointer motion did not begin manual scrolling")
	}
	before := s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now()))
	s.seek(tt.Now(), 8*time.Second)
	tt.Frame()
	if after := s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now())); after != before || !s.pointerDown || s.manualLine < 0 {
		t.Fatalf("host seek took the viewport away from an active drag: %g -> %g", before, after)
	}
	tt.Release(x, y-80)
	if s.pointerDown || requests != 0 || s.manualLine < 0 {
		t.Fatal("drag release generated a seek or immediately restored automatic following")
	}
	// Cancellation must release capture without interpreting the release as click.
	x, y = interactionRowPoint(t, s, s.rows[0].index)
	tt.Press(x, y)
	tt.CancelPress()
	if s.pointerDown || requests != 0 {
		t.Fatal("canceled pointer capture retained a drag or generated a seek")
	}
	// Secondary-button presses are not lyric activation gestures.
	tt.PressButton(1, x, y)
	tt.ReleaseButton(1, x, y)
	if s.pointerDown || requests != 0 {
		t.Fatal("secondary pointer button activated a lyric")
	}
}

func TestM3TesterWheelKeyboardAndBrowseActivation(t *testing.T) {
	s, tt := newInteractionScene(t)
	x, y := interactionRowPoint(t, s, 0)
	tt.Scroll(x, y, 0, 100)
	first := s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now()))
	if first <= 0 || s.manualLine < 0 {
		t.Fatal("touchpad scroll did not suspend follow and move the viewport")
	}
	tt.Wheel(x, y, 0, 80)
	if next := s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now())); next <= first {
		t.Fatal("mouse wheel was not routed to the lyric viewport")
	}
	tt.Key(0, ui.KeyHome)
	if s.manualLine != -1 || s.anchorAt(tt.Now(), 0) != 0 {
		t.Fatal("Home did not explicitly restore following")
	}
	// A previous mouse selection must not override a subsequent keyboard browse.
	x, y = interactionRowPoint(t, s, 1)
	tt.ClickAt(x, y)
	tt.Key(0, ui.KeyDown)
	if s.manualLine != 1 {
		t.Fatalf("Down browsed to %d, want 1", s.manualLine)
	}
	tt.Key(0, ui.KeyPageDown)
	if s.manualLine != 6 {
		t.Fatalf("PageDown browsed to %d, want 6", s.manualLine)
	}
	var requested seekRequest
	s.onSeek = func(_ time.Time, request seekRequest) { requested = request }
	tt.Key(0, ui.KeyEnter)
	if requested.LineID != "interaction:6" || requested.Position != 12*time.Second {
		t.Fatalf("Enter activated a stale selection instead of the browsed line: %+v", requested)
	}
	if s.clock.positionAt(tt.Now()) != 0 {
		t.Fatal("keyboard activation bypassed the host seek callback")
	}
	tt.Key(0, ui.KeyUp)
	if s.manualLine != 5 {
		t.Fatal("Up did not browse one line backwards")
	}
	tt.Key(0, ui.KeyPageUp)
	if s.manualLine != 0 {
		t.Fatal("PageUp did not browse five lines backwards")
	}
}

func TestM3TesterAutomaticFollowRequiresNewVisiblePlayingFocus(t *testing.T) {
	t.Run("quiet time alone", func(t *testing.T) {
		s, tt := newInteractionScene(t)
		s.seek(tt.Now(), 250*time.Millisecond)
		tt.Frame()
		if err := tt.Click("Play"); err != nil {
			t.Fatal(err)
		}
		x, y := interactionRowPoint(t, s, 0)
		tt.Scroll(x, y, 0, 12)
		tt.Advance(800 * time.Millisecond)
		if s.manualLine < 0 {
			t.Fatal("the 500 ms quiet period alone resumed following")
		}
		tt.Advance(1100 * time.Millisecond)
		if s.manualLine != -1 || s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now())) != 1 {
			t.Fatal("a new visible playing focus after the quiet period did not resume following")
		}
	})
	t.Run("early focus does not qualify later", func(t *testing.T) {
		s, tt := newInteractionScene(t)
		s.seek(tt.Now(), 1700*time.Millisecond)
		tt.Frame()
		if err := tt.Click("Play"); err != nil {
			t.Fatal(err)
		}
		x, y := interactionRowPoint(t, s, 0)
		tt.Scroll(x, y, 0, 12)
		tt.Advance(600 * time.Millisecond)
		if s.manualLine < 0 {
			t.Fatal("focus which arrived before the quiet period was incorrectly reused after expiry")
		}
		tt.Advance(1800 * time.Millisecond)
		if s.manualLine != -1 {
			t.Fatal("later visible focus failed to restore following")
		}
	})
	t.Run("offscreen focus", func(t *testing.T) {
		s, tt := newInteractionScene(t)
		if err := tt.Click("Play"); err != nil {
			t.Fatal(err)
		}
		x, y := interactionRowPoint(t, s, 0)
		tt.Scroll(x, y, 0, 1000)
		tt.Advance(2100 * time.Millisecond)
		if s.manualLine < 0 {
			t.Fatal("offscreen playing focus unexpectedly reclaimed the viewport")
		}
		tt.Key(0, ui.KeyHome)
		if s.manualLine != -1 || s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now())) != 1 {
			t.Fatal("explicit follow failed after browsing away from playback")
		}
	})
}

func TestM3TesterLineBoundaryBuildsAndStaticScheduling(t *testing.T) {
	s, tt := newInteractionScene(t)
	s.forceStatic = true
	if err := tt.Click("Play"); err != nil {
		t.Fatal(err)
	}
	builds, shapes := s.viewBuilds, s.cache.shapes
	tt.Advance(1700 * time.Millisecond)
	if s.viewBuilds != builds || s.cache.shapes != shapes || tt.FramePending() {
		t.Fatalf("word-only static updates rebuilt views, reshaped or kept requesting frames: builds %d -> %d, shapes %d -> %d", builds, s.viewBuilds, shapes, s.cache.shapes)
	}
	tt.Advance(400 * time.Millisecond)
	if delta := s.viewBuilds - builds; delta < 1 || delta > 3 {
		t.Fatalf("line transitions should rebuild at boundaries, got %d builds", delta)
	}
	if s.rows[0].index != 1 {
		t.Fatal("line boundary timer failed to advance visible focus")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if !s.reduceMotion {
		t.Fatal("native reduced-motion preference was ignored")
	}
	if err := tt.Click("Pause"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Minute)
	if tt.FramePending() {
		t.Fatal("paused static lyrics continue requesting frames")
	}
}

func TestM3DrawSamplesMotionAndReusesFractionalViewportGeometry(t *testing.T) {
	s, tt := newInteractionScene(t)
	// The drawing-only host deliberately has no business update callback.
	drawOnly := ui.NewTester(func(c *ui.Context) { ui.Box(c).Fill().Draw(s.draw) }, 900, 600)
	s.follow.BeginInteraction()
	s.scroll.Snap(tt.Now(), 0)
	s.scroll.Retarget(tt.Now(), 3*scrollUnit)
	s.motionReady = true
	follow, spring := s.follow, s.scroll
	drawOnly.Frame()
	drawOnly.Frame()
	if s.follow != follow || s.scroll != spring {
		t.Fatal("repeated Draw advanced follow or spring business state")
	}
	box := ui.Rect{W: 900, H: 600}
	s.prepareWindow(1.1, box)
	s.prepareWindow(1.75, box)
	shapes := s.cache.shapes
	allocations := testing.AllocsPerRun(100, func() {
		for _, anchor := range [...]float64{1.1, 1.75} {
			s.prepareWindow(anchor, box)
			for _, row := range s.rows[:s.rowCount] {
				if row.geometry != nil {
					s.highlights = row.geometry.appendHighlights(s.highlights[:0], 2500*time.Millisecond)
				}
			}
		}
	})
	if allocations != 0 || s.cache.shapes != shapes {
		t.Fatalf("warm fractional scrolling and highlighting allocated or reshaped: allocations %g, shapes %d -> %d", allocations, shapes, s.cache.shapes)
	}
}

func TestM3TesterPressFreezesExistingScrollTrajectory(t *testing.T) {
	s, tt := newInteractionScene(t)
	// Inject a running trajectory independently of the software Tester's backend.
	// This tests business state and pointer routing, not GPU rendering.
	s.scroll.Snap(tt.Now(), 0)
	s.scroll.Retarget(tt.Now(), 3*scrollUnit)
	s.motionReady = true
	tt.Advance(50 * time.Millisecond)
	before := s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now()))
	if before <= 0 || before >= 3 {
		t.Fatal("fixture needs an unfinished scroll trajectory")
	}
	x, y := interactionRowPoint(t, s, 0)
	tt.Press(x, y)
	tt.Advance(600 * time.Millisecond)
	if after := s.anchorAt(tt.Now(), s.clock.positionAt(tt.Now())); after != before {
		t.Fatalf("holding the pointer let the viewport drift under it: %g -> %g", before, after)
	}
	if !s.pointerDown || s.dragged {
		t.Fatal("stationary held press became a drag or lost capture")
	}
	tt.CancelPress()
}

func TestM3TesterHiddenRestoreSnapsAcceleratedBusinessState(t *testing.T) {
	s, initial := newInteractionScene(t)
	// update receives the accelerated flag before Draw overwrites it with the
	// Tester's software backend. The positive control below proves that an ordinary
	// update would animate, so the restore assertion exercises the instant branch.
	now := initial.Now()
	s.accelerated = true
	s.clock.seek(now, 2*time.Second)
	s.update(now, false)
	if s.scroll.Settled(now) {
		t.Fatal("accelerated control unexpectedly snapped an ordinary focus change")
	}
	s.seek(now, 0)
	restored := false
	var restoredPosition, restoredVelocity float64
	tt := ui.NewTester(func(c *ui.Context) {
		wasHidden := s.hidden
		s.accelerated = true
		s.view(c)
		if wasHidden && !s.hidden {
			restored = true
			restoredPosition, restoredVelocity = s.scroll.Sample(c.Now())
		}
	}, 960, 740)
	s.origin = tt.Now()
	s.clock = newPlaybackClock(s.duration, tt.Now())
	s.seek(tt.Now(), 0)
	s.clock.setPlaying(tt.Now(), true)
	tt.Frame()
	tt.SetWindowHidden(true)
	if !s.hidden {
		t.Fatal("view did not observe the public WindowHidden state")
	}
	shapes := s.cache.shapes
	tt.Advance(30 * time.Second)
	if s.cache.shapes != shapes {
		t.Fatal("hidden window prepared new text geometry")
	}
	tt.SetWindowHidden(false)
	want := 15 * scrollUnit
	if !restored || s.hidden || restoredPosition != want || restoredVelocity != 0 || !s.scroll.Settled(tt.Now()) {
		t.Fatalf("restored accelerated business state did not snap to current focus: restored=%t, position=%g, velocity=%g, want=%g", restored, restoredPosition, restoredVelocity, want)
	}
}
