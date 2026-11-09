package main

import (
	"errors"
	"math"
	"time"
)

// playbackClock is owned by the UI thread. Track positions and duration use
// nanoseconds through time.Duration; anchorTime uses the host's monotonic clock.
// Queries do not mutate the anchor, so repeated Draw calls and hidden windows
// cannot change playback speed. The playing flag records the host's intent;
// reaching the end clamps the position without changing that flag during Draw.
type playbackClock struct {
	duration       time.Duration
	anchorPosition time.Duration
	anchorTime     time.Time
	playing        bool
	rate           float64
}

func newPlaybackClock(duration time.Duration, now time.Time) playbackClock {
	if duration < 0 {
		duration = 0
	}
	return playbackClock{duration: duration, anchorTime: now, rate: 1}
}

func (clock *playbackClock) positionAt(now time.Time) time.Duration {
	if !clock.playing {
		return clock.anchorPosition
	}
	elapsed := now.Sub(clock.anchorTime)
	remaining := clock.duration - clock.anchorPosition
	if clock.rate == 1 {
		// Preserve nanosecond precision at normal speed, comparing before adding
		// so very long suspensions and near-MaxInt64 durations cannot overflow.
		if elapsed >= remaining {
			return clock.duration
		}
		if elapsed <= -clock.anchorPosition {
			return 0
		}
		return clock.anchorPosition + elapsed
	}

	// Scaling can overflow float64 for a valid but enormous finite rate. The
	// comparisons saturate +/-Inf before conversion to an integer duration.
	delta := float64(elapsed) * clock.rate
	if delta >= float64(remaining) {
		return clock.duration
	}
	if delta <= -float64(clock.anchorPosition) {
		return 0
	}
	return clock.anchorPosition + time.Duration(delta)
}

func (clock *playbackClock) seek(now time.Time, position time.Duration) {
	if position < 0 {
		position = 0
	} else if position > clock.duration {
		position = clock.duration
	}
	clock.anchorPosition = position
	clock.anchorTime = now
}

func (clock *playbackClock) setPlaying(now time.Time, playing bool) {
	if clock.playing == playing {
		return
	}
	clock.seek(now, clock.positionAt(now))
	clock.playing = playing
}

func (clock *playbackClock) setRate(now time.Time, rate float64) error {
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return errors.New("native: playback rate must be finite and positive")
	}
	if rate != clock.rate {
		clock.seek(now, clock.positionAt(now))
		clock.rate = rate
	}
	return nil
}
