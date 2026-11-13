// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestReferenceWakeStopsAfterEndedAndPausedSettle(t *testing.T) {
	for _, ended := range []bool{true, false} {
		name := "paused"
		if ended {
			name = "ended"
		}
		t.Run(name, func(t *testing.T) {
			s := referenceTestScene(t, 4)
			now := s.origin
			s.clock.setPlaying(now, true)
			s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, true)
			position := 500 * time.Millisecond
			if ended {
				position = s.duration
			}
			s.seek(now, position)
			if !ended {
				s.clock.setPlaying(now, false)
			}
			// Keep full ordinary frame steps until residual springs, CSS fades,
			// reversed floats and emphasis have all reached their own endpoints.
			for range 750 {
				now = now.Add(16 * time.Millisecond)
				s.updateReference(now, false)
			}
			if ended && (!s.reference.state.Ended || !s.clock.playing) {
				t.Fatal("test lost the host's playing intent at the clamped song end")
			}
			if got := s.referenceWake(now); got != 0 {
				t.Fatalf("settled frame still requests wake %v (running=%v, dots=%+v)", got, s.reference.running, s.reference.interludePose)
			}
		})
	}
}

func TestReferenceHiddenDoesNotRequestFrames(t *testing.T) {
	s := referenceTestScene(t, 4)
	now := s.origin
	s.clock.setPlaying(now, true)
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, true)
	s.hidden = true
	if got := s.referenceWake(now); got != 0 {
		t.Fatalf("hidden scene requested wake %v", got)
	}
}

func TestReferenceStaticWakeAtWordBoundaryWithoutContinuousFrames(t *testing.T) {
	// The first word ends while the group remains active for another ten
	// seconds. An idle static player must repaint this step before group end.
	doc := geometryDocument(t, []lyric.Line{{Text: "one two", End: 20 * time.Second, Words: []lyric.Word{
		{Text: "one", End: 10 * time.Second},
		{Text: " two", Start: 10 * time.Second, End: 20 * time.Second},
	}}})
	s := newScene(doc, time.Unix(0, 0))
	t.Cleanup(func() { _ = s.Close() })
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	s.forceStatic = true
	s.clock.seek(s.origin, 2*time.Second)
	s.clock.setPlaying(s.origin, true)
	now := s.origin
	for range 240 {
		now = now.Add(16 * time.Millisecond)
		s.prepareReferenceFrame(now, ui.Rect{W: 500, H: 300}, 1, true)
	}
	if s.reference.running {
		t.Fatal("fixture has not settled its residual group animations")
	}
	want := 10*time.Second - s.clock.positionAt(now)
	if got := s.referenceWake(now); got != want || got == referenceFrameInterval {
		t.Fatalf("static word boundary wake=%v; expected a single timer after %v", got, want)
	}
	if err := s.clock.setRate(now, 2); err != nil {
		t.Fatal(err)
	}
	if got := s.referenceWake(now); got != want/2 {
		t.Fatalf("double-speed word wake=%v; expected %v", got, want/2)
	}
	s.clock.seek(now, s.duration)
	if got := s.referenceWake(now); got != 0 {
		t.Fatalf("ended static player scheduled a word wake: %v", got)
	}
	s.clock.seek(now, 2*time.Second)
	s.clock.setPlaying(now, false)
	if got := s.referenceWake(now); got != 0 {
		t.Fatalf("paused static player scheduled a word wake: %v", got)
	}
}
