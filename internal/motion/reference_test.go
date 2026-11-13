package motion

import (
	"math"
	"testing"
	"time"
)

func TestRefSpringFramePreparationAndQueueOrder(t *testing.T) {
	now := time.Unix(100, 0)
	spring := NewRefSpring(now, 0, RefYParams(true, false, false, 0, false))
	spring.SetTarget(now, 100, 50*time.Millisecond)
	original := spring
	preview := spring.Sample(now.Add(100 * time.Millisecond))
	if spring != original || preview.Position != 0 || preview.Settled {
		t.Fatal("preview consumed a queue or advanced the newly delivered target in the same frame")
	}
	spring.Advance(now.Add(100 * time.Millisecond))
	committed := spring
	spring.Advance(now.Add(100 * time.Millisecond))
	if spring != committed {
		t.Fatal("repeated frame preparation at one timestamp changed state")
	}
	spring.Advance(now.Add(116 * time.Millisecond))
	if sample := spring.Sample(now.Add(116 * time.Millisecond)); sample.Position <= 0 {
		t.Fatal("committed target did not animate on the following frame")
	}
	// Same-time input changes occur in order, without an extra frame delta.
	spring.SetTarget(now.Add(116*time.Millisecond), 200, 0)
	spring.SetTarget(now.Add(116*time.Millisecond), -100, 0)
	if spring.target != -100 || spring.now != now.Add(116*time.Millisecond) {
		t.Fatal("same-time retarget order was lost")
	}
	if allocations := testing.AllocsPerRun(1000, func() { spring.Sample(now.Add(140 * time.Millisecond)) }); allocations != 0 {
		t.Fatalf("reference sampling allocated %g times", allocations)
	}
}

func TestRefSpringReferenceQueueQuirks(t *testing.T) {
	now := time.Unix(100, 0)
	spring := NewRefSpring(now, 0, RefDefaultParams())
	spring.SetTarget(now, 100, time.Second)
	spring.SetPosition(now, 25)
	spring.Advance(now.Add(time.Second))
	if spring.position != 25 || spring.target != 100 {
		t.Fatal("SetPosition unexpectedly discarded the reference's pending target")
	}
	spring = NewRefSpring(now, 0, RefDefaultParams())
	spring.SetTarget(now, 100, time.Second)
	spring.SetParams(now, RefMainScaleParams(), 0)
	if spring.queuedPos.pending {
		t.Fatal("immediate reference parameter update did not cancel a delayed target")
	}
	spring.SetParams(now, RefBGScaleParams(), 50*time.Millisecond)
	spring.Advance(now.Add(100 * time.Millisecond))
	if !spring.queuedParam.pending || spring.Sample(now.Add(100*time.Millisecond)).Settled {
		t.Fatal("delayed parameter queue's fixed-reference behavior was silently changed")
	}
}

func TestRefSpringExplicitCappedFrameDelta(t *testing.T) {
	now := time.Unix(100, 0)
	params := RefYParams(true, false, false, 0, false)
	a := NewRefSpring(now, 0, params)
	b := NewRefSpring(now, 0, params)
	a.SetTarget(now, 100, 0)
	b.SetTarget(now, 100, 0)
	a.AdvanceFrame(now.Add(30*time.Second), 100*time.Millisecond)
	b.Advance(now.Add(100 * time.Millisecond))
	if a.Sample(now.Add(30*time.Second)) != b.Sample(now.Add(100*time.Millisecond)) {
		t.Fatal("explicit reference frame cap changed the physical trajectory")
	}
	before := a
	a.AdvanceFrame(now.Add(30*time.Second), 100*time.Millisecond)
	if a != before {
		t.Fatal("duplicate draw timestamp consumed another capped delta")
	}
}

func TestRefPoliciesAndStagger(t *testing.T) {
	slow := RefParams{Mass: 0.9, Damping: 15, Stiffness: 90}
	if RefYParams(true, false, true, time.Second, true) != slow || RefYParams(false, true, false, 0, true) != slow || RefYParams(false, false, false, 0, false) != slow {
		t.Fatal("seek/interlude/missing-interval precedence differs")
	}
	if end := RefYParams(false, false, true, 0, false); end.Stiffness != 140 || end.Damping != 22 || end.Mass != 0.9 {
		t.Fatal("song-end policy differs")
	}
	if RefYParams(false, false, false, 0, true).Stiffness != 220 || RefYParams(false, false, false, time.Second, true).Stiffness != 170 {
		t.Fatal("interval clamp endpoints differ")
	}
	for i, want := range []time.Duration{0, 50 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond, 197619048 * time.Nanosecond, 242970522 * time.Nanosecond} {
		if got := RefStagger(0, i, 2); got != want {
			t.Fatalf("stagger[%d]=%v, want %v", i, got, want)
		}
	}
	if RefStagger(20, 20, 2) != 0 || RefStagger(20, 21, 2) != 50*time.Millisecond {
		t.Fatal("first visible line did not restart the stagger sequence")
	}
}

func TestReferenceWordEffects(t *testing.T) {
	start := 2 * time.Second
	normal := NewFloat(start, start+400*time.Millisecond, false)
	background := NewFloat(start, start+400*time.Millisecond, true)
	if normal.Sample(start-time.Millisecond) != 0 || normal.Sample(start+time.Second) != -0.05 || background.Sample(start+time.Second) != -0.1 || normal.End() != start+time.Second {
		t.Fatal("ordinary float's fill, minimum duration or background amplitude differs")
	}
	effect := NewEmphasis(EmphasisOptions{Start: start, End: start + 2*time.Second, Characters: 4, Last: true})
	if effect.duration != float64(2400*time.Millisecond) || effect.charDelay != float64(240*time.Millisecond) || effect.End() != 6080*time.Millisecond {
		t.Fatalf("last-word duration, delay or extra-float duration differs: %+v", effect)
	}
	initial := effect.Character(start, 0)
	if initial != (CharacterPose{Scale: 1}) {
		t.Fatalf("implicit offset-0 frame is not neutral: %+v", initial)
	}
	first := effect.Character(start+75*time.Millisecond, 0)
	if first.Scale*10000 != math.Round(first.Scale*10000) || first.ShadowBlurEm != effect.ShadowBlurEm() {
		t.Fatal("first actual frame lost matrix-to-CSS rounding or shadow radius")
	}
	if effect.Character(start+75*time.Millisecond, 1) != (CharacterPose{Scale: 1}) {
		t.Fatal("character delay was omitted")
	}
	// Between frame offsets, interpolate the retained frames rather than using
	// the ideal continuous curve. The two samples below bound their midpoint.
	a := effect.Character(start+75*time.Millisecond, 0)
	b := effect.Character(start+150*time.Millisecond, 0)
	mid := effect.Character(start+112500*time.Microsecond, 0)
	if math.Abs(mid.Scale-(a.Scale+b.Scale)/2) > 1e-12 || math.Abs(mid.ShadowAlpha-(a.ShadowAlpha+b.ShadowAlpha)/2) > 1e-12 {
		t.Fatal("emphasis sampling skipped the actual 32-frame interpolation")
	}
	ruby := NewEmphasis(EmphasisOptions{Start: start, End: start + 2*time.Second, Characters: 4, RubyCharacters: 2})
	if ruby.charDelay != float64(400*time.Millisecond) {
		t.Fatal("ruby count did not control character delay")
	}
	if allocations := testing.AllocsPerRun(1000, func() { effect.Character(start+time.Second, 2); normal.Sample(start + time.Second/2) }); allocations != 0 {
		t.Fatalf("effect sampling allocated %g times", allocations)
	}
}

func BenchmarkRefSpringSample(b *testing.B) {
	now := time.Unix(100, 0)
	spring := NewRefSpring(now, 0, RefYParams(true, false, false, 0, false))
	spring.SetTarget(now, 100, 0)
	sample := now.Add(150 * time.Millisecond)
	spring.Advance(sample)
	b.ReportAllocs()
	for b.Loop() {
		benchmarkPosition = spring.Sample(sample).Position
	}
}

func BenchmarkRefCharacterSample(b *testing.B) {
	effect := NewEmphasis(EmphasisOptions{End: 2 * time.Second, Characters: 4, Last: true})
	b.ReportAllocs()
	for b.Loop() {
		benchmarkPosition = effect.Character(time.Second, 2).Scale
	}
}
