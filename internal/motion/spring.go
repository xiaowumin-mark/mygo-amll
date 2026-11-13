// Package motion contains deterministic animation primitives used by the native
// example. It has no clock, goroutine, document, or UI resource of its own.
package motion

import (
	"math"
	"time"
)

const (
	// These values describe UI coordinates in logical pixels and seconds.
	angularFrequency  = 18.0
	positionTolerance = 0.05
	velocityTolerance = 0.2
	maximumInterval   = 2 * time.Second
)

// Spring is a critically damped scalar spring. Its zero value is stationary at
// zero. Sample is a pure, allocation-free query of an analytic trajectory; calling
// it more often does not change the result. Mutations belong to the owning UI
// thread. Concurrent reads are safe only while no caller mutates the spring.
//
// Times should come from the same monotonic clock. Queries before the last event
// use that event's state. Events with an earlier time are clamped to the preceding
// event, so an out-of-order input cannot move the animation clock backwards.
type Spring struct {
	anchor      time.Time
	position    float64
	velocity    float64
	target      float64
	initialized bool
}

// Retarget starts a trajectory towards target without discontinuities in either
// position or velocity. Retargeting to the existing target leaves the trajectory
// unchanged. Non-finite targets are ignored.
func (s *Spring) Retarget(now time.Time, target float64) {
	if !finite(target) {
		return
	}
	if s.initialized && target == s.target {
		return
	}
	now = s.eventTime(now)
	position, velocity := s.Sample(now)
	s.anchor, s.position, s.velocity, s.target = now, position, velocity, target
	s.initialized = true
}

// Snap immediately places the spring at target with no velocity. The caller uses
// this for seeks, document/geometry changes, and reduced or disabled motion.
// Non-finite targets are ignored.
func (s *Spring) Snap(now time.Time, target float64) {
	if !finite(target) {
		return
	}
	now = s.eventTime(now)
	s.anchor, s.position, s.velocity, s.target = now, target, 0, target
	s.initialized = true
}

// Sample returns position and velocity (logical units and logical units/second)
// at now. After two seconds without another target it returns the exact target,
// avoiding historical frame replay after a hidden window or a long pause. Small
// residuals also settle to the target. Arithmetic overflow from extreme finite
// coordinates takes the same bounded snap path rather than propagating NaN/Inf.
func (s *Spring) Sample(now time.Time) (position, velocity float64) {
	if !s.initialized {
		return 0, 0
	}
	delta := now.Sub(s.anchor)
	if delta <= 0 {
		return s.position, s.velocity
	}
	if delta >= maximumInterval {
		return s.target, 0
	}
	seconds := delta.Seconds()
	displacement := s.position - s.target
	coefficient := s.velocity + angularFrequency*displacement
	decay := math.Exp(-angularFrequency * seconds)
	position = s.target + (displacement+coefficient*seconds)*decay
	velocity = (s.velocity - angularFrequency*coefficient*seconds) * decay
	if !finite(position) || !finite(velocity) ||
		(math.Abs(position-s.target) <= positionTolerance && math.Abs(velocity) <= velocityTolerance) {
		return s.target, 0
	}
	return position, velocity
}

// Settled reports whether no further animation frames are needed at now.
func (s *Spring) Settled(now time.Time) bool {
	if !s.initialized {
		return true
	}
	position, velocity := s.Sample(now)
	return math.Abs(position-s.target) <= positionTolerance && math.Abs(velocity) <= velocityTolerance
}

func (s *Spring) eventTime(now time.Time) time.Time {
	if s.initialized && now.Before(s.anchor) {
		return s.anchor
	}
	return now
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
