// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestReferenceDrawDoesNotScheduleFrozenPaintFrames(t *testing.T) {
	s := referenceTestScene(t, 4)
	// This host deliberately does not request a new prepared frame. Draw must
	// consume the frozen timestamp rather than start a paint-only timer that
	// repeatedly presents the same animation sample without a view update.
	tt := ui.NewTester(func(c *ui.Context) {
		s.update(c.Now(), false)
		ui.Box(c).Fill().Draw(s.draw)
	}, 320, 200)
	if !s.reference.running {
		t.Fatal("fixture must retain a residual CSS transition after its first paint")
	}
	draws, step, now := s.drawCalls, s.reference.lastStep, s.reference.now
	tt.Advance(100 * time.Millisecond)
	if s.drawCalls != draws {
		t.Fatalf("frozen frame repainted without a view request: draws %d -> %d", draws, s.drawCalls)
	}
	if s.reference.lastStep != step || s.reference.now != now {
		t.Fatal("painting advanced the frozen reference state")
	}
}

func TestReferenceViewSchedulesMountAndSleepsAfterPause(t *testing.T) {
	s := referenceTestScene(t, 4)
	tt := ui.NewTester(s.view, 640, 440)
	first := s.reference.lastFrame
	if !s.reference.running || !tt.FramePending() {
		t.Fatal("initial view did not schedule its residual animation")
	}
	tt.Advance(100 * time.Millisecond)
	if !s.reference.lastFrame.After(first) || s.viewBuilds < 2 {
		t.Fatal("initial animation did not receive a new prepared view timestamp")
	}
	tt.Advance(time.Second)
	if s.reference.running || s.referenceWake(tt.Now()) != 0 || tt.FramePending() {
		t.Fatal("paused and settled scene still requests frames")
	}
	draws, views := s.drawCalls, s.viewBuilds
	tt.Advance(time.Second)
	if s.drawCalls != draws || s.viewBuilds != views {
		t.Fatal("paused scene woke without an input or scheduled boundary")
	}
}

func TestReferencePausedLayoutDiscoveryGetsPreparedFrames(t *testing.T) {
	for _, change := range []string{"resize", "idle-layout", "dpi"} {
		t.Run(change, func(t *testing.T) {
			s := referenceTestScene(t, 4)
			s.font.Size = 30.3 // DPI quantization changes these fractional line boxes.
			height, scale := float32(160), float32(1)
			views := 0
			// Exercise the same scheduler with accelerated frame preparation and
			// deterministic clocks. Painting is omitted: this checks deferred
			// viewport discovery and wake ownership, not GPU frame delivery.
			tt := ui.NewTester(func(c *ui.Context) {
				views++
				now := c.Now()
				s.updateReference(now, false)
				ui.Box(c).Fill().Draw(func(_ *ui.Painter, box ui.Rect) {
					box.H = height
					s.prepareReferenceFrame(now, box, scale, true)
				})
				w, h := c.Size()
				if s.referenceViewFrame(now, w, h) {
					c.AnimationFrame()
				}
			}, 320, 200)
			tt.Advance(4 * time.Second)
			if tt.FramePending() || s.reference.running {
				t.Fatal("initial paused scene did not settle")
			}
			previous := s.reference.lastFrame
			switch change {
			case "resize":
				height = 190
				tt.SetSize(340, 230)
			case "idle-layout":
				height = 190
				tt.Frame()
			case "dpi":
				scale = 1.25
				tt.SetScale(scale)
			}
			if !tt.FramePending() {
				t.Fatal("layout discovered after an idle view got no confirming view frame")
			}
			tt.Advance(100 * time.Millisecond)
			if !s.reference.lastFrame.After(previous) {
				t.Fatal("deferred layout never received a fresh prepared timestamp")
			}
			tt.Advance(4 * time.Second)
			if tt.FramePending() || s.reference.running {
				t.Fatal("paused layout confirmation became a continuous frame loop")
			}
			settledViews := views
			tt.Advance(time.Second)
			if views != settledViews {
				t.Fatal("settled layout kept rebuilding views")
			}
			tt.Frame() // A later idle input gets one finite confirmation chain.
			tt.Advance(2 * referenceFrameInterval)
			if views-settledViews > 3 || tt.FramePending() {
				t.Fatalf("idle input did not stop after its confirming view: %d views", views-settledViews)
			}
		})
	}
}

func TestReferenceLayoutConfirmationSkipsStaticHiddenAndSoftware(t *testing.T) {
	for _, policy := range []string{"static", "reduced-motion", "hidden", "software"} {
		t.Run(policy, func(t *testing.T) {
			s := referenceTestScene(t, 4)
			s.prepareReferenceFrame(s.origin, ui.Rect{W: 320, H: 200}, 1, true)
			now := s.origin
			for range 250 {
				now = now.Add(16 * time.Millisecond)
				s.updateReference(now, false)
			}
			switch policy {
			case "static":
				s.forceStatic = true
			case "reduced-motion":
				s.reduceMotion = true
			case "hidden":
				s.hidden = true
			case "software":
				s.reference.accelerated = false
			}
			if s.referenceViewFrame(now, 340, 230) {
				t.Fatal("nonanimated/hidden path scheduled a layout confirmation")
			}
		})
	}
}
