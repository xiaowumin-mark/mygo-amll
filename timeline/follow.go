package timeline

import (
	"slices"
	"time"
)

// FollowResumeDelay is the quiet period after an interaction ends before a new
// visible, playing focus may automatically resume scrolling.
const FollowResumeDelay = 500 * time.Millisecond

// FollowController controls permission to apply automatic focus, independently
// of playback and highlighting. Its zero value follows automatically. It owns
// no document references, timers, goroutines or UI resources. A host should keep
// one controller per view, call Update on prepared time changes (including while
// suspended), and reset it with Resume on song changes. It is not concurrency safe.
type FollowController struct {
	suspended   bool
	interacting bool
	released    bool
	releasedAt  time.Duration
	initialized bool
	focus       int
	interlude   bool
}

// BeginInteraction suspends automatic focus until explicit resume or a qualifying
// focus change after EndInteraction. Further input can call it again; inertia
// counts as interaction and should end before EndInteraction is called.
func (controller *FollowController) BeginInteraction() {
	controller.suspended = true
	controller.interacting = true
	controller.released = false
}

// EndInteraction starts the quiet period using an injected monotonic timestamp,
// such as time.Since(hostOrigin). All timestamps for a controller must be
// nonnegative durations from the same origin; song position is not this clock.
// Calling this method without an active interaction has no effect.
func (controller *FollowController) EndInteraction(now time.Duration) {
	if !controller.interacting {
		return
	}
	controller.interacting = false
	controller.released = true
	controller.releasedAt = now
}

// Resume explicitly restores automatic following immediately and ends any manual
// interaction. It also clears focus history, as needed when replacing a song.
func (controller *FollowController) Resume() {
	*controller = FollowController{}
}

// Seek restores following unless a gesture is still active. The host calls it
// for explicit seeks, including lyric clicks; VisualAt itself has no interaction
// side effects. A seek during a drag preserves the drag and its eventual delay.
func (controller *FollowController) Seek() {
	if !controller.interacting {
		controller.Resume()
	}
}

// Update reports whether the host may apply the state's automatic focus. Manual
// scrolling never alters Playing or Highlighted. When suspended, expiration of
// the quiet period alone does not resume scrolling: the focus must also change
// to a visible, currently playing line. Leaving an interlude for such a line is
// a focus change. The host supplies focusVisible from its prepared viewport;
// geometry, touch thresholds, wheel debouncing and inertia remain host concerns.
//
// now uses EndInteraction's monotonic clock. A timestamp earlier than the last
// release cannot expire the delay. Call this method during frame preparation or
// input handling, not from a Draw callback that may execute more than once.
func (controller *FollowController) Update(state VisualState, now time.Duration, focusVisible bool) bool {
	changed := controller.initialized && (controller.focus != state.Focus || controller.interlude != state.InInterlude)
	if controller.suspended && !controller.interacting && controller.released &&
		now >= controller.releasedAt && now-controller.releasedAt >= FollowResumeDelay &&
		changed && !state.InInterlude && focusVisible && slices.Contains(state.Playing, state.Focus) {
		controller.Resume()
	}
	controller.initialized = true
	controller.focus = state.Focus
	controller.interlude = state.InInterlude
	return !controller.suspended
}
