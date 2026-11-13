// SPDX-License-Identifier: AGPL-3.0-only
package motion

import "time"

// AnimationSampleKey is an opaque, comparable description of sampled animation
// positions. Equal keys from SampleKey guarantee equal Position results for
// every animation end at or before the supplied maxEnd. Unequal keys may still
// produce identical pixels; they must conservatively invalidate a raster cache.
// A cache needs a separate validity flag: the zero value is not an empty marker.
type AnimationSampleKey struct {
	lineStart, maxEnd    time.Duration
	position, elapsed    float64
	initialized, reverse bool
}

// SampleKey describes this clock's positions for kind at now without retaining
// the wall clock or a last-Sync timestamp. maxEnd is the greatest absolute end
// of the effects represented by the key, in the same units as Position. The
// query is pure and allocation-free. Finished and paused samples remain equal
// while time advances; a reverse sample keeps each shorter effect's independent
// Web Animations auto-rewind behavior.
func (clock LineAnimationClock) SampleKey(now time.Time, kind AnimationKind, maxEnd time.Duration) AnimationSampleKey {
	key := AnimationSampleKey{lineStart: clock.lineStart, maxEnd: maxEnd, initialized: clock.initialized}
	if !clock.initialized {
		return key
	}
	phase := clock.phase(kind)
	bound := max(0, float64(maxEnd)-float64(clock.lineStart))
	if !phase.reverse {
		key.position = min(bound, max(0, phase.rawPosition(now)))
		return key
	}
	// The greatest end has at least as much reverse travel left as every
	// shorter end. Once it reaches zero, all those samples are stable as well.
	elapsed := phase.elapsed(now)
	start := phase.position
	if start <= 0 || start > bound {
		start = bound
	}
	if start <= elapsed {
		return key
	}
	// Keeping only Position(maxEnd) is insufficient: two reverse clocks can
	// agree there while auto-rewinding differently at a shorter effect's end.
	key.position, key.elapsed, key.reverse = phase.position, elapsed, true
	return key
}
