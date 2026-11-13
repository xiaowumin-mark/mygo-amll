package motion

import "time"

// AnimationKind distinguishes the exact IDs acted on by the reference's line
// enable/disable/pause/resume methods. EmphasisFloat is the actual generated ID
// emphasize-word-float, which does NOT match emphasize-word-float-only.
type AnimationKind uint8

const (
	MaskAnimation AnimationKind = iota
	FloatWordAnimation
	EmphasisGlowAnimation
	EmphasisFloatAnimation
	FloatOnlyAnimation
)

type lineAnimationPhase struct {
	anchor   time.Time
	position float64 // line-relative nanoseconds
	rate     float64
	reverse  bool
}

func (phase lineAnimationPhase) elapsed(now time.Time) float64 {
	return max(0, float64(now.Sub(phase.anchor)))
}

func (phase lineAnimationPhase) rawPosition(now time.Time) float64 {
	return phase.position + phase.rate*phase.elapsed(now)
}

func (phase *lineAnimationPhase) pause(now time.Time) {
	phase.position = phase.rawPosition(now)
	phase.anchor, phase.rate = now, 0
}

func (phase *lineAnimationPhase) play(now time.Time) {
	phase.position = phase.rawPosition(now)
	phase.anchor, phase.rate = now, 1
}

// LineAnimationClock retains the reference line animation state machine with
// constant storage. It owns no document or UI resources. Sync belongs to view/
// input preparation; Position and Running are pure frame queries. Repeating a
// Sync with identical state does not resynchronize to media or accumulate time.
//
// Each Position query supplies that individual animation's absolute end, so a
// finished short-word float reverses from its own end rather than the end of the
// longest animation in the line. The caller owns word/grapheme segmentation.
type LineAnimationClock struct {
	initialized          bool
	lineStart, lineEnd   time.Duration
	enabled, playing     bool
	revision             uint64
	lastSync             time.Time
	mask, ordinary, glow lineAnimationPhase
	extra                lineAnimationPhase
}

// Sync receives absolute song positions and a monotonic UI timestamp. A changed
// revision means explicit seek/re-enable, and resets clocks only for enabled
// lines, matching syncTime's group.enable calls. An inactive line is not enabled
// merely because its media position changed. Geometry recreation should use a
// new clock value to reproduce newly created paused Web Animations objects.
func (clock *LineAnimationClock) Sync(now time.Time, media, lineStart, lineEnd time.Duration, enabled, playing bool, revision uint64) {
	if clock.initialized && now.Before(clock.lastSync) {
		now = clock.lastSync
	}
	if !clock.initialized || clock.lineStart != lineStart || clock.lineEnd != lineEnd {
		*clock = LineAnimationClock{initialized: true, lineStart: lineStart, lineEnd: lineEnd,
			playing: playing, revision: revision, lastSync: now,
			mask: lineAnimationPhase{anchor: now}, ordinary: lineAnimationPhase{anchor: now},
			glow: lineAnimationPhase{anchor: now}, extra: lineAnimationPhase{anchor: now}}
	}
	if enabled && (!clock.enabled || clock.revision != revision) {
		position := max(0, float64(media)-float64(lineStart))
		rate := 0.0
		if playing {
			rate = 1
		}
		phase := lineAnimationPhase{anchor: now, position: position, rate: rate}
		clock.ordinary, clock.glow, clock.extra = phase, phase, phase
		phase.position = min(position, max(0, float64(lineEnd)-float64(lineStart)))
		clock.mask = phase
	} else if !enabled && clock.enabled {
		// disable() reverses only ordinary float-word / float-only IDs. Mask
		// pauses; actual emphasis glow and emphasize-word-float keep their prior
		// running/paused state, including through later disabled-line pause calls.
		clock.ordinary = lineAnimationPhase{anchor: now, position: clock.ordinary.rawPosition(now), rate: -1, reverse: true}
		clock.mask.pause(now)
	} else if enabled && playing != clock.playing {
		phases := [...]*lineAnimationPhase{&clock.mask, &clock.ordinary, &clock.glow, &clock.extra}
		for _, phase := range phases {
			if playing {
				phase.play(now)
			} else {
				phase.pause(now)
			}
		}
	}
	clock.enabled, clock.playing, clock.revision, clock.lastSync = enabled, playing, revision, now
}

func (clock LineAnimationClock) phase(kind AnimationKind) lineAnimationPhase {
	switch kind {
	case FloatWordAnimation, FloatOnlyAnimation:
		return clock.ordinary
	case EmphasisGlowAnimation:
		return clock.glow
	case EmphasisFloatAnimation:
		return clock.extra
	default:
		return clock.mask
	}
}

func (clock LineAnimationClock) relative(now time.Time, kind AnimationKind, end time.Duration) (float64, float64, float64) {
	phase := clock.phase(kind)
	bound := max(0, float64(end)-float64(clock.lineStart))
	position := phase.rawPosition(now)
	if phase.reverse {
		start := phase.position
		// Web Animations play() with a negative rate auto-rewinds when at zero
		// or past the end. This includes the animation's positive delay in end.
		if start <= 0 || start > bound {
			start = bound
		}
		position = start - phase.elapsed(now)
	}
	return min(bound, max(0, position)), phase.rate, bound
}

// Position returns the absolute song-coordinate position to pass to Float.Sample
// or Emphasis.Character, using the selected animation's wall-clock state. For
// masks, end is the absolute end of the retained mask animation.
func (clock LineAnimationClock) Position(now time.Time, kind AnimationKind, end time.Duration) time.Duration {
	if !clock.initialized {
		return clock.lineStart
	}
	position, _, _ := clock.relative(now, kind, end)
	return refDuration(float64(clock.lineStart) + position)
}

// Running reports whether this effect still changes at now, including a
// pre-word delay while it runs. Finished per-animation clocks stop scheduling.
func (clock LineAnimationClock) Running(now time.Time, kind AnimationKind, end time.Duration) bool {
	if !clock.initialized {
		return false
	}
	position, rate, bound := clock.relative(now, kind, end)
	return rate > 0 && position < bound || rate < 0 && position > 0
}
