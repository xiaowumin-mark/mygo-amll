// SPDX-License-Identifier: AGPL-3.0-only
package motion

import (
	"math/rand"
	"testing"
	"time"
)

type sampleKeyObservation struct {
	clock LineAnimationClock
	now   time.Time
	kind  AnimationKind
}

func checkSampleKeyPositions(t *testing.T, seen map[AnimationSampleKey]sampleKeyObservation, clock LineAnimationClock, now time.Time, kind AnimationKind, maxEnd time.Duration, ends []time.Duration) {
	t.Helper()
	key := clock.SampleKey(now, kind, maxEnd)
	if previous, ok := seen[key]; ok {
		for _, end := range ends {
			if end > maxEnd {
				t.Fatal("test observation exceeded its key's scope")
			}
			if a, b := previous.clock.Position(previous.now, previous.kind, end), clock.Position(now, kind, end); a != b {
				t.Fatalf("equal key changed position at end=%v: %v != %v; key=%+v", end, a, b, key)
			}
		}
	} else {
		seen[key] = sampleKeyObservation{clock, now, kind}
	}
}

func TestAnimationSampleKeyEquivalentPositions(t *testing.T) {
	base := time.Unix(100, 0)
	for _, lineStart := range []time.Duration{0, 5 * time.Second, 1<<53 + 1, 1<<63 - 1 - 10*time.Second} {
		const span = 6 * time.Second
		maxEnd := lineStart + span
		ends := []time.Duration{lineStart - time.Second, lineStart, lineStart + 1, maxEnd}
		for i := range 48 {
			ends = append(ends, lineStart+time.Duration(i)*span/48)
		}
		seen := make(map[AnimationSampleKey]sampleKeyObservation)
		for _, position := range []float64{-1, 0, .5, 1, float64(350 * time.Millisecond), float64(2 * time.Second), float64(10 * time.Second)} {
			for _, rate := range []float64{-1, 0, 1} {
				for _, reverse := range []bool{false, true} {
					clock := LineAnimationClock{initialized: true, lineStart: lineStart, ordinary: lineAnimationPhase{anchor: base, position: position, rate: rate, reverse: reverse}}
					for _, elapsed := range []time.Duration{-time.Second, 0, 1, time.Millisecond, 100 * time.Millisecond, time.Second, 5 * time.Second, 20 * time.Second} {
						checkSampleKeyPositions(t, seen, clock, base.Add(elapsed), FloatWordAnimation, maxEnd, ends)
					}
				}
			}
		}
	}
}

func TestAnimationSampleKeyHostTransitions(t *testing.T) {
	const start, span = 5 * time.Second, 7 * time.Second
	base := time.Unix(100, 0)
	now := base
	random := rand.New(rand.NewSource(7301))
	ends := []time.Duration{start - 1, start, start + 1, start + span}
	for range 32 {
		ends = append(ends, start+time.Duration(random.Int63n(int64(span))))
	}
	seen := make(map[AnimationSampleKey]sampleKeyObservation)
	var clock LineAnimationClock
	var revision uint64
	for action := range 400 {
		now = now.Add(time.Duration(random.Int63n(int64(250 * time.Millisecond))))
		if action%11 == 0 {
			revision++ // explicit seeks, including disabled residual emphasis
		}
		media := start + time.Duration(random.Int63n(int64(2*span)))
		clock.Sync(now, media, start, start+4*time.Second, random.Intn(2) == 0, random.Intn(2) == 0, revision)
		before := clock
		for _, kind := range []AnimationKind{MaskAnimation, FloatWordAnimation, EmphasisGlowAnimation, EmphasisFloatAnimation, FloatOnlyAnimation} {
			for _, offset := range []time.Duration{0, time.Second / 120, time.Second / 60, span, 2 * span} {
				checkSampleKeyPositions(t, seen, clock, now.Add(offset), kind, start+span, ends)
			}
		}
		if clock != before {
			t.Fatal("sample key queries mutated the animation clock")
		}
	}
	if allocations := testing.AllocsPerRun(1000, func() {
		clock.SampleKey(now, FloatWordAnimation, start+span)
		clock.SampleKey(now, EmphasisGlowAnimation, start+span)
	}); allocations != 0 {
		t.Fatalf("sample keys allocated %g times", allocations)
	}
}

func TestAnimationSampleKeyStableAndReverseBoundaries(t *testing.T) {
	base := time.Unix(100, 0)
	const start, end = 5 * time.Second, 15 * time.Second
	var clock LineAnimationClock
	clock.Sync(base, start+time.Second, start, end, true, false, 0)
	paused := clock.SampleKey(base, FloatWordAnimation, end)
	clock.Sync(base.Add(time.Second), start+time.Second, start, end, true, false, 0)
	if paused != clock.SampleKey(base.Add(2*time.Second), FloatWordAnimation, end) {
		t.Fatal("paused key changed with now or an otherwise identical Sync")
	}
	clock.Sync(base.Add(3*time.Second), start, start, end, true, true, 1)
	if clock.SampleKey(base.Add(20*time.Second), FloatWordAnimation, end) != clock.SampleKey(base.Add(30*time.Second), FloatWordAnimation, end) {
		t.Fatal("finished forward key continued changing")
	}
	a := LineAnimationClock{initialized: true, lineStart: start, ordinary: lineAnimationPhase{anchor: base, position: float64(5 * time.Second), rate: -1, reverse: true}}
	b := a
	b.ordinary.position = float64(8 * time.Second)
	left, right := base.Add(time.Second), base.Add(4*time.Second)
	if a.Position(left, FloatWordAnimation, end) != b.Position(right, FloatWordAnimation, end) || a.Position(left, FloatWordAnimation, start+6*time.Second) == b.Position(right, FloatWordAnimation, start+6*time.Second) {
		t.Fatal("fixture must agree at maxEnd but differ at a shorter auto-rewind boundary")
	}
	if a.SampleKey(left, FloatWordAnimation, end) == b.SampleKey(right, FloatWordAnimation, end) {
		t.Fatal("reverse key lost a shorter animation's independent auto-rewind")
	}
	if a.SampleKey(base.Add(20*time.Second), FloatWordAnimation, end) != b.SampleKey(base.Add(30*time.Second), FloatWordAnimation, end) {
		t.Fatal("fully reversed clocks did not converge to a stable zero key")
	}
	// Position's uninitialized path returns the original integer duration;
	// its initialized path adds through float64. The key must preserve that
	// distinction even at durations above float64's exact-integer range.
	a = LineAnimationClock{lineStart: 1<<53 + 1}
	b = a
	b.initialized = true
	if a.Position(base, MaskAnimation, a.lineStart) == b.Position(base, MaskAnimation, a.lineStart) || a.SampleKey(base, MaskAnimation, a.lineStart) == b.SampleKey(base, MaskAnimation, a.lineStart) {
		t.Fatal("uninitialized integer position was merged with a rounded initialized sample")
	}
}
