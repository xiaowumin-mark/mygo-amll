package motion

import (
	"math"
	"testing"
	"time"
)

func TestSpringAnalyticTrajectory(t *testing.T) {
	now := time.Unix(100, 0)
	var spring Spring
	spring.Snap(now, 10)
	spring.Retarget(now, 110)
	const seconds = 0.1
	position, velocity := spring.Sample(now.Add(100 * time.Millisecond))
	decay := math.Exp(-18 * seconds)
	wantPosition := 110 - 100*(1+18*seconds)*decay
	wantVelocity := 100 * 18 * 18 * seconds * decay
	if math.Abs(position-wantPosition) > 1e-12 || math.Abs(velocity-wantVelocity) > 1e-12 {
		t.Fatalf("Sample=(%g,%g), want (%g,%g)", position, velocity, wantPosition, wantVelocity)
	}
	// The reported velocity is the derivative of the returned position.
	before, _ := spring.Sample(now.Add(100*time.Millisecond - time.Microsecond))
	after, _ := spring.Sample(now.Add(100*time.Millisecond + time.Microsecond))
	if derivative := (after - before) / 0.000002; math.Abs(derivative-velocity) > 0.000001 {
		t.Fatalf("velocity %g disagrees with derivative %g", velocity, derivative)
	}
}

func TestSpringFrameRateIndependentAndReadOnly(t *testing.T) {
	now := time.Unix(100, 0)
	var spring Spring
	spring.Retarget(now, 400)
	original := spring
	end := now.Add(437 * time.Millisecond)
	wantPosition, wantVelocity := spring.Sample(end)
	for _, hz := range []int{30, 60, 120, 144} {
		for frame := 0; frame < hz; frame++ {
			spring.Sample(now.Add(time.Duration(frame) * time.Second / time.Duration(hz)))
		}
		position, velocity := spring.Sample(end)
		if position != wantPosition || velocity != wantVelocity || spring != original {
			t.Fatalf("%d Hz sampling altered trajectory or state", hz)
		}
	}
	if allocations := testing.AllocsPerRun(1000, func() {
		spring.Sample(end)
		spring.Settled(end)
	}); allocations != 0 {
		t.Fatalf("queries allocated %g times", allocations)
	}
}

func TestSpringRetargetPreservesVelocityAndPosition(t *testing.T) {
	now := time.Unix(100, 0)
	var spring Spring
	spring.Retarget(now, 100)
	event := now.Add(100 * time.Millisecond)
	beforePosition, beforeVelocity := spring.Sample(event)
	if beforeVelocity <= 0 {
		t.Fatal("fixture has no forward velocity")
	}
	spring.Retarget(event, -100)
	position, velocity := spring.Sample(event)
	if position != beforePosition || velocity != beforeVelocity {
		t.Fatalf("retarget jumped from (%g,%g) to (%g,%g)", beforePosition, beforeVelocity, position, velocity)
	}
	_, laterVelocity := spring.Sample(event.Add(100 * time.Millisecond))
	if laterVelocity >= 0 {
		t.Fatal("spring did not reverse towards new target")
	}
	before := spring
	spring.Retarget(event.Add(50*time.Millisecond), -100)
	if spring != before {
		t.Fatal("identical target restarted the trajectory")
	}
}

func TestSpringSettlementAndSnap(t *testing.T) {
	now := time.Unix(100, 0)
	var spring Spring
	if !spring.Settled(now) {
		t.Fatal("zero value should be settled")
	}
	spring.Retarget(now, 120)
	if spring.Settled(now) {
		t.Fatal("new nonzero target should need frames")
	}
	previous := 0.0
	for step := 1; step <= 1000; step++ {
		position, velocity := spring.Sample(now.Add(time.Duration(step) * time.Millisecond))
		if position < previous || position > 120 || velocity < 0 {
			t.Fatalf("stationary critical spring overshot or moved backwards at %d: %g,%g", step, position, velocity)
		}
		previous = position
	}
	for _, interval := range []time.Duration{2 * time.Second, time.Hour, 365 * 24 * time.Hour} {
		position, velocity := spring.Sample(now.Add(interval))
		if position != 120 || velocity != 0 || !spring.Settled(now.Add(interval)) {
			t.Fatalf("long interval %v did not settle: %g,%g", interval, position, velocity)
		}
	}
	// Reduced-motion and seek handlers use Snap rather than integrating a jump.
	spring.Snap(now.Add(10*time.Millisecond), -30)
	for _, interval := range []time.Duration{0, time.Millisecond, time.Second} {
		position, velocity := spring.Sample(now.Add(10*time.Millisecond + interval))
		if position != -30 || velocity != 0 || !spring.Settled(now.Add(10*time.Millisecond+interval)) {
			t.Fatal("snap retained motion")
		}
	}
}

func TestSpringClockBackwardsAndInvalidCoordinates(t *testing.T) {
	now := time.Unix(100, 0)
	var spring Spring
	spring.Snap(now, 10)
	spring.Retarget(now, 100)
	position, velocity := spring.Sample(now.Add(-time.Second))
	if position != 10 || velocity != 0 {
		t.Fatal("sample extrapolated before anchor")
	}
	spring.Retarget(now.Add(-time.Second), 200)
	if spring.anchor != now {
		t.Fatal("out-of-order event moved anchor backwards")
	}
	before := spring
	for _, invalid := range []float64{math.NaN(), math.Inf(-1), math.Inf(1)} {
		spring.Retarget(now.Add(time.Second), invalid)
		spring.Snap(now.Add(time.Second), invalid)
		if spring != before {
			t.Fatal("nonfinite input changed spring")
		}
	}
	spring.Snap(now, math.MaxFloat64)
	spring.Retarget(now, -math.MaxFloat64)
	position, velocity = spring.Sample(now.Add(time.Millisecond))
	if !finite(position) || !finite(velocity) || position != -math.MaxFloat64 || velocity != 0 {
		t.Fatalf("extreme finite input propagated overflow: %g,%g", position, velocity)
	}
}

var benchmarkPosition, benchmarkVelocity float64

func BenchmarkSpringSample(b *testing.B) {
	now := time.Unix(100, 0)
	var spring Spring
	spring.Retarget(now, 600)
	sample := now.Add(150 * time.Millisecond)
	b.ReportAllocs()
	for b.Loop() {
		benchmarkPosition, benchmarkVelocity = spring.Sample(sample)
	}
}
