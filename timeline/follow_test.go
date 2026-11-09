package timeline_test

import (
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func followState(focus int) timeline.VisualState {
	return timeline.VisualState{Focus: focus, Playing: []int{focus}}
}

func TestFollowInteractionDelayAndVisibleFocus(t *testing.T) {
	var controller timeline.FollowController
	if !controller.Update(followState(0), 0, true) {
		t.Fatal("zero value should follow")
	}
	controller.BeginInteraction()
	if controller.Update(followState(1), time.Second, true) {
		t.Fatal("active interaction must suspend following")
	}
	controller.EndInteraction(time.Second)
	if controller.Update(followState(2), time.Second+timeline.FollowResumeDelay-1, true) {
		t.Fatal("quiet period ended too soon")
	}
	if controller.Update(followState(2), time.Second+timeline.FollowResumeDelay, true) {
		t.Fatal("quiet period alone must not restore following")
	}
	if controller.Update(followState(3), 2*time.Second, false) {
		t.Fatal("offscreen focus must not restore following")
	}
	if !controller.Update(followState(4), 3*time.Second, true) {
		t.Fatal("visible playing focus change should restore following")
	}
}

func TestFollowResumeAtExactDelay(t *testing.T) {
	var controller timeline.FollowController
	controller.Update(followState(0), 0, true)
	controller.BeginInteraction()
	controller.EndInteraction(0)
	if !controller.Update(followState(1), timeline.FollowResumeDelay, true) {
		t.Fatal("exact 500ms boundary should allow a qualifying focus change")
	}
}

func TestFollowInterludeAndEndedFocusDoNotResume(t *testing.T) {
	var controller timeline.FollowController
	controller.Update(followState(0), 0, true)
	controller.BeginInteraction()
	controller.EndInteraction(0)
	if controller.Update(timeline.VisualState{Focus: 1, InInterlude: true}, time.Second, true) {
		t.Fatal("interlude focus should remain suspended")
	}
	if !controller.Update(followState(1), 2*time.Second, true) {
		t.Fatal("leaving interlude for a playing line should restore following")
	}
	controller.BeginInteraction()
	controller.EndInteraction(2 * time.Second)
	if controller.Update(timeline.VisualState{Focus: 2, Ended: true}, 3*time.Second, true) {
		t.Fatal("completed lyric focus should remain suspended")
	}
}

func TestFollowExplicitResumeSeekAndRepeatedInteraction(t *testing.T) {
	var controller timeline.FollowController
	controller.Update(followState(0), 0, true)
	controller.BeginInteraction()
	controller.Seek()
	if controller.Update(followState(1), time.Second, true) {
		t.Fatal("seek during a gesture should preserve suspension")
	}
	controller.EndInteraction(time.Second)
	controller.BeginInteraction()
	if controller.Update(followState(2), 10*time.Second, true) {
		t.Fatal("old quiet period must not end renewed interaction")
	}
	controller.EndInteraction(10 * time.Second)
	controller.Seek()
	if !controller.Update(followState(3), 10*time.Second, true) {
		t.Fatal("seek after gesture should restore following immediately")
	}
	controller.BeginInteraction()
	controller.Resume()
	if !controller.Update(followState(0), 0, true) {
		t.Fatal("explicit resume should restore following and reset song history")
	}
}

func TestFollowIgnoresStrayReleaseAndEarlierClock(t *testing.T) {
	var controller timeline.FollowController
	controller.EndInteraction(0)
	if !controller.Update(followState(0), 0, true) {
		t.Fatal("stray release suspended following")
	}
	controller.BeginInteraction()
	controller.EndInteraction(time.Second)
	controller.EndInteraction(0)
	if controller.Update(followState(1), time.Second-1, true) {
		t.Fatal("earlier clock expired quiet period")
	}
	if controller.Update(followState(2), time.Second, true) {
		t.Fatal("stray release changed quiet period")
	}
}
