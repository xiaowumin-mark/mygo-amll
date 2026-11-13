package motion

import (
	"math"
	"time"
)

// This file adapts AMLL utils/spring.ts and utils/derivative.ts at
// f32285864056587c4afa59d310208b0aa829cb53. See NOTICE.md, including the solver's
// original notice: /** MIT License github.com/pushkine/ */.

// RefParams is a complete reference spring configuration. Mass and stiffness
// must be positive, damping nonnegative, and all numbers finite. SetParams takes
// a complete value, unlike the reference's JavaScript Partial object.
type RefParams struct {
	Mass, Damping, Stiffness float64
	Soft                     bool
}

// RefSample contains the current scalar position and the reference's central
// finite-difference velocity/acceleration (h=0.001 seconds), not analytic
// derivatives. Units are caller-chosen logical units, seconds and seconds².
type RefSample struct {
	Position, Velocity, Acceleration float64
	Settled                          bool
}

type refSolver struct {
	from, velocity, to float64
	params             RefParams
	constant           bool
}

func (solver refSolver) value(t float64) float64 {
	if solver.constant || solver.from == solver.to && solver.velocity == 0 {
		return solver.to
	}
	if t < 0 {
		return solver.from
	}
	p := solver.params
	delta := solver.to - solver.from
	if p.Soft || p.Damping/(2*math.Sqrt(p.Stiffness*p.Mass)) >= 1 {
		// The reference intentionally uses this critical solution for the entire
		// >=1 damping-ratio branch. A textbook overdamped solver differs.
		frequency := -math.Sqrt(p.Stiffness / p.Mass)
		leftover := -frequency*delta - solver.velocity
		return solver.to - (delta+t*leftover)*math.Pow(math.E, t*frequency)
	}
	frequency := math.Sqrt(4*p.Mass*p.Stiffness - p.Damping*p.Damping)
	leftover := (p.Damping*delta - 2*p.Mass*solver.velocity) / frequency
	dfm := (0.5 * frequency) / p.Mass
	dm := -(0.5 * p.Damping) / p.Mass
	return solver.to - (math.Cos(t*dfm)*delta+math.Sin(t*dfm)*leftover)*math.Pow(math.E, t*dm)
}

func (solver refSolver) speed(t float64) float64 {
	return (solver.value(t+0.001) - solver.value(t-0.001)) / 0.002
}

func (solver refSolver) acceleration(t float64) float64 {
	return (solver.speed(t+0.001) - solver.speed(t-0.001)) / 0.002
}

type refTargetQueue struct {
	position, remaining float64
	pending             bool
}

type refParamsQueue struct {
	params    RefParams
	remaining float64
	pending   bool
}

// RefSpring preserves the fixed AMLL spring's update ordering, delayed target
// replacement, finite derivatives, and arrival checks. It is UI-thread-owned;
// it has no clock or goroutine. Its zero value is settled at zero.
//
// Advance belongs to frame preparation, once for each monotonically increasing
// timestamp. Repeating it at the same timestamp is a no-op. Same-time mutations
// are applied in call order; Sample then observes the revised trajectory without
// consuming queues. The caller must Advance the next prepared frame to commit
// delayed queues. A pure Sample is a preview of one reference update at now.
type RefSpring struct {
	now         time.Time
	initialized bool
	position    float64
	target      float64
	currentTime float64
	params      RefParams
	solver      refSolver
	queuedPos   refTargetQueue
	queuedParam refParamsQueue
}

// RefDefaultParams returns the reference utility's fallback parameters.
func RefDefaultParams() RefParams { return RefParams{Mass: 1, Damping: 10, Stiffness: 100} }

// NewRefSpring starts settled at position. Invalid parameters use the reference
// defaults, and a nonfinite initial position uses zero. Runtime setters ignore
// invalid values; the faithful numeric domain is finite, valid UI coordinates.
func NewRefSpring(now time.Time, position float64, params RefParams) RefSpring {
	if !validRefParams(params) {
		params = RefDefaultParams()
	}
	if !finite(position) {
		position = 0
	}
	return RefSpring{now: now, initialized: true, position: position, target: position,
		params: params, solver: refSolver{to: position, constant: true}}
}

func validRefParams(p RefParams) bool {
	return finite(p.Mass) && finite(p.Damping) && finite(p.Stiffness) && p.Mass > 0 && p.Damping >= 0 && p.Stiffness > 0
}

func (spring *RefSpring) initialize(now time.Time) {
	if !spring.initialized {
		*spring = NewRefSpring(now, 0, RefDefaultParams())
	}
}

func (spring *RefSpring) resetSolver() {
	velocity := spring.solver.speed(spring.currentTime)
	spring.currentTime = 0
	spring.solver = refSolver{from: spring.position, velocity: velocity, to: spring.target, params: spring.params}
}

func (spring *RefSpring) arrived() bool {
	return math.Abs(spring.target-spring.position) < 0.01 &&
		math.Abs(spring.solver.speed(spring.currentTime)) < 0.01 &&
		math.Abs(spring.solver.acceleration(spring.currentTime)) < 0.01 &&
		!spring.queuedPos.pending && !spring.queuedParam.pending
}

func (spring *RefSpring) setPosition(position float64) {
	spring.position, spring.target = position, position
	spring.solver = refSolver{to: position, constant: true}
}

// SetPosition reproduces reference setPosition. In particular it does not clear
// an already queued target or parameter update. To discard every queued effect
// on a document change, replace the value with NewRefSpring.
func (spring *RefSpring) SetPosition(now time.Time, position float64) {
	if !finite(position) {
		return
	}
	spring.initialize(now)
	spring.Advance(now)
	spring.setPosition(position)
}

func (spring *RefSpring) setTarget(position float64, delay float64) {
	if delay <= 0 && math.Abs(spring.target-position) < 0.001 {
		spring.queuedPos.pending = false
		return
	}
	if delay > 0 {
		spring.queuedPos = refTargetQueue{position: position, remaining: delay, pending: true}
		return
	}
	spring.queuedPos.pending = false
	spring.target = position
	spring.resetSolver()
}

// SetTarget replaces the one pending target, or retargets immediately while
// preserving the reference's numerical velocity. delay uses wall/UI time.
func (spring *RefSpring) SetTarget(now time.Time, position float64, delay time.Duration) {
	if !finite(position) {
		return
	}
	spring.initialize(now)
	spring.Advance(now)
	spring.setTarget(position, delay.Seconds())
}

func (spring *RefSpring) setParams(params RefParams) {
	// This surprising queue cancellation is present in reference updateParams.
	spring.queuedPos.pending = false
	spring.params = params
	spring.resetSolver()
}

// SetParams applies complete parameters. The fixed reference leaves a delayed
// parameter queue armed after it fires; subsequent updates reapply it and it
// prevents arrived() from becoming true. That observable behavior is preserved.
// Native layout uses immediate parameter changes, as AMLL's own layout does.
func (spring *RefSpring) SetParams(now time.Time, params RefParams, delay time.Duration) {
	if !validRefParams(params) {
		return
	}
	spring.initialize(now)
	spring.Advance(now)
	if delay > 0 {
		spring.queuedParam = refParamsQueue{params: params, remaining: delay.Seconds(), pending: true}
		return
	}
	spring.setParams(params)
}

// Advance commits one reference update. Long hidden gaps are not silently
// clamped here: the host supplies the fixed reference's capped frame delta or a
// deliberate reset. Explicit input events can share a timestamp with this frame.
func (spring *RefSpring) Advance(now time.Time) {
	spring.initialize(now)
	if !now.After(spring.now) {
		return
	}
	spring.AdvanceFrame(now, now.Sub(spring.now))
}

// AdvanceFrame commits an explicitly supplied frame delta, retaining the real
// timestamp for idempotency and input ordering. Native preparation uses this to
// reproduce base/dom update's min(delta,100ms), while Advance above reproduces
// the uncapped utility. Call this before same-time target/parameter mutations.
func (spring *RefSpring) AdvanceFrame(now time.Time, delta time.Duration) {
	spring.initialize(now)
	if !now.After(spring.now) {
		return
	}
	seconds := max(0, delta.Seconds())
	spring.now = now
	spring.currentTime += seconds
	spring.position = spring.solver.value(spring.currentTime)
	if spring.queuedParam.pending {
		spring.queuedParam.remaining -= seconds
		if spring.queuedParam.remaining <= 0 {
			spring.setParams(spring.queuedParam.params)
		}
	}
	if spring.queuedPos.pending {
		spring.queuedPos.remaining -= seconds
		if spring.queuedPos.remaining <= 0 {
			spring.setTarget(spring.queuedPos.position, 0)
		}
	}
	if spring.arrived() {
		spring.setPosition(spring.target)
	}
}

// Sample is allocation-free and does not mutate the spring or consume queues.
func (spring RefSpring) Sample(now time.Time) RefSample {
	spring.Advance(now)
	return RefSample{Position: spring.position, Velocity: spring.solver.speed(spring.currentTime),
		Acceleration: spring.solver.acceleration(spring.currentTime), Settled: spring.arrived()}
}

// RefYParams implements base/spring.ts. interval is the difference between group
// start times, not the silence between the preceding end and current start.
func RefYParams(seeking, interlude, ended bool, interval time.Duration, hasInterval bool) RefParams {
	p := RefParams{Mass: 0.9, Stiffness: 90, Damping: 15}
	if seeking || interlude {
		return p
	}
	if ended {
		p.Stiffness, p.Damping = 140, 22
		return p
	}
	if !hasInterval {
		return p
	}
	ms := min(800, max(100, float64(interval)/float64(time.Millisecond)))
	ratio := math.Pow(1-(ms-100)/700, 0.2)
	p.Stiffness = 170 + ratio*50
	p.Damping = math.Sqrt(p.Stiffness) * 2.2
	return p
}

func RefMainScaleParams() RefParams { return RefParams{Mass: 2, Damping: 25, Stiffness: 100} }
func RefBGScaleParams() RefParams   { return RefParams{Mass: 1, Damping: 20, Stiffness: 50} }

// RefStagger returns the delay of index when firstVisible is the first group
// whose target bottom reaches viewport Y=0. It evaluates the 50ms /1.05 sequence
// in O(1), without walking every preceding group in a long song.
func RefStagger(firstVisible, index, focus int) time.Duration {
	if index <= firstVisible {
		return 0
	}
	n := index - firstVisible
	const ratio = 1 / 1.05
	var seconds float64
	if firstVisible > focus {
		seconds = 0.05 * (1 - math.Pow(ratio, float64(n))) / (1 - ratio)
	} else {
		fixed := focus - firstVisible + 1
		if n <= fixed {
			seconds = float64(n) * 0.05
		} else {
			seconds = float64(fixed)*0.05 + 0.05*ratio*(1-math.Pow(ratio, float64(n-fixed)))/(1-ratio)
		}
	}
	return refDuration(seconds * float64(time.Second))
}
