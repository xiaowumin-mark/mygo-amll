// SPDX-License-Identifier: AGPL-3.0-only
// Derived from AMLL commit f32285864056587c4afa59d310208b0aa829cb53:
// packages/core/src/lyric-player/base/interlude-dots.ts,
// packages/core/src/lyric-player/dom/interlude-dots.ts, and
// packages/core/src/styles/lyric-player.module.css (interludeDots).
// Adapted to explicit caller-owned clocks and numeric Native drawing poses.
package motion

import (
	"math"
	"time"
)

type InterludePhase string
type InterludeMode string

const (
	InterludeIdle       InterludePhase = "idle"
	InterludePerforming InterludePhase = "performing"
	InterludeFading     InterludePhase = "fading"
	InterludeBreathe    InterludeMode  = "breathe"
	InterludeHold       InterludeMode  = "fallback-hold"
)

var (
	interludeLighting = makeRefBezier(0.56, 0.01, 0.45, 1)
	interludeEnter    = makeRefBezier(0.59, 0.02, 0.07, 1)
	interludeExit1    = makeRefBezier(0.14, 0.06, 0.25, 1)
	interludeExit2    = makeRefBezier(0.29, 0.03, 1, 0.38)
	interludeExitFade = makeRefBezier(0.43, 0.08, 0.83, 0.31)
)

// InterludeSnapshot is the reference base class's unrounded render callback.
// It is a value copy; retaining a pose never borrows controller-owned storage.
type InterludeSnapshot struct {
	Active       bool
	DotOpacities [3]float64
	Scale        float64
	Opacity      float64
}

type InterludeDotPose struct {
	Opacity, Progress, Scale, TranslateYEm float64
}

// InterludePose describes the actual DOM-style rounding separately from Raw.
// A point's own opacity multiplies container Opacity. Individual points have
// no scale or translation in the reference; breathing scales the whole box
// around its center. Occupancy is one during performing (including its entry
// hold), zero during dismissal/idle; layout still owns the physical row height.
type InterludePose struct {
	Active, NeedsFrame bool
	Opacity, Scale     float64
	DotOpacities       [3]float64
	Dots               [3]InterludeDotPose
	X, Y               float64
	RawX, RawY         float64
	Raw                InterludeSnapshot
	Phase              InterludePhase
	Mode               InterludeMode
	Occupancy          float64
	NextWake           time.Duration // media-time delay; divide by host playback rate
	Duet               bool
}

type InterludeMetrics struct {
	Width, Height, ContentHeight, DotDiameter, Gap, PaddingX, PaddingY float64
}

// MeasureInterludeDots resolves the reference CSS box. windowHeight is the
// viewport used by vh; paddingX is the resolved --lyric-line-padding-x, which
// the host supplies (normally 1em, or 20px at <=500px window width).
func MeasureInterludeDots(fontSize, windowHeight, paddingX float64) InterludeMetrics {
	if !finite(fontSize) || fontSize <= 0 || !finite(windowHeight) || !finite(paddingX) {
		return InterludeMetrics{}
	}
	paddingX = max(0, paddingX)
	content := min(fontSize*3, max(fontSize*0.5, windowHeight*0.01))
	return InterludeMetrics{Width: fontSize*1.26 + paddingX*2, Height: content + fontSize*0.8,
		ContentHeight: content, DotDiameter: fontSize * 0.3, Gap: fontSize * 0.18,
		PaddingX: paddingX, PaddingY: fontSize * 0.4}
}

// InterludeInput is an optional Native frame adapter. Media is the desired
// absolute song position at this frame, not the preceding clock anchor.
// CappedDelta is physical UI elapsed time; Frame caps it at the reference
// player's 100ms. Present selects the active timeline interval. AnchorLineIndex
// -1 selects a real intro; seeking always reinstates the 500ms entry hold.
type InterludeInput struct {
	Media, CappedDelta time.Duration
	Playing, Present   bool
	Start, End         time.Duration
	AnchorLineIndex    int
	SeekRevision       uint64
	Duet               bool
}

// Interlude ports the reference lifecycle. Mutations belong to one UI owner;
// Pose is read-only and allocation-free. It owns no ticker, goroutine, document
// or UI resource. Low-level methods preserve the original method ordering;
// Frame additionally makes repeated preparation at the same UI time idempotent.
type Interlude struct {
	initialized                               bool
	phase                                     InterludePhase
	mode                                      InterludeMode
	playing                                   bool
	start, end, anchor, media                 float64 // reference floating-point milliseconds
	delayEnd, bodyEnd, totalEnd               float64
	period, segment, dot3Duration, dot3Target float64
	fadeElapsed, fadeInitial                  float64
	snapshot, rendered                        InterludeSnapshot
	progress                                  [3]float64
	renderedProgress                          [3]float64
	left, top, renderedLeft, renderedTop      float64
	shouldSnap                                bool
	positionY                                 RefSpring
	springNow                                 time.Time // advanced only by explicit source update(delta)
	advanceNow                                time.Time
	hasAdvance                                bool
	frameNow                                  time.Time
	frameInput                                InterludeInput
	hasFrame                                  bool
	duet                                      bool
}

func NewInterlude(now time.Time) Interlude {
	return Interlude{initialized: true, phase: InterludeIdle, mode: InterludeBreathe, playing: true,
		period: 4000, shouldSnap: true, snapshot: InterludeSnapshot{Active: true, Scale: 1},
		rendered: InterludeSnapshot{Scale: 1}, springNow: now, positionY: NewRefSpring(now, 0, RefDefaultParams())}
}

func (controller *Interlude) initialize(now time.Time) {
	if !controller.initialized {
		*controller = NewInterlude(now)
	}
}

// SetInterlude mirrors setInterlude, including the same-interval idle guard:
// dismissing without clearing does not restart that interval unless forced.
// This method does not render; call AdvanceFrame with zero delta, SetTransform,
// or the Frame adapter to commit the first visible snapshot.
func (controller *Interlude) SetInterlude(now time.Time, start, end, media time.Duration, forceReset bool, anchorLineIndex int) bool {
	controller.initialize(now)
	startMS, endMS := float64(start)/float64(time.Millisecond), float64(end)/float64(time.Millisecond)
	if !forceReset && controller.start == startMS && controller.end == endMS && controller.phase != InterludeFading {
		return controller.phase == InterludePerforming
	}
	if controller.phase == InterludeFading {
		controller.phase = InterludeIdle // cancelFadeOut intentionally does not render
	}
	controller.start, controller.end = startMS, endMS
	controller.media = float64(media) / float64(time.Millisecond)
	controller.anchor, controller.delayEnd = startMS, 500
	if forceReset {
		controller.anchor = controller.media
	} else if anchorLineIndex == -1 {
		controller.delayEnd = 0
	}
	remaining := max(0, endMS-controller.anchor)
	body := remaining - controller.delayEnd - 1000
	if body < 910 {
		controller.hide()
		return false
	}
	controller.bodyEnd = controller.delayEnd + body
	controller.totalEnd = controller.bodyEnd + 1000
	controller.mode, controller.dot3Target = InterludeHold, 1
	if body >= 3000 {
		controller.mode = InterludeBreathe
		controller.period = body / max(1, math.Floor(body/4000))
		controller.segment = math.Floor((body+750)/3 + 0.5) // positive JS Math.round
		controller.dot3Duration = body - controller.segment*2
		controller.dot3Target = controller.dot3Duration / controller.segment
	}
	controller.phase, controller.shouldSnap = InterludePerforming, true
	return true
}

func (controller *Interlude) SyncClock(media time.Duration) {
	controller.initialize(time.Time{})
	controller.media = float64(media) / float64(time.Millisecond)
}

func (controller *Interlude) Pause() {
	controller.initialize(time.Time{})
	controller.playing = false
	if controller.phase == InterludeFading {
		controller.hide()
	}
}

func (controller *Interlude) Resume() {
	controller.initialize(time.Time{})
	controller.playing = true
}

func (controller *Interlude) ClearInterlude(immediate bool) {
	controller.initialize(time.Time{})
	controller.start, controller.end = 0, 0
	controller.Dismiss(immediate)
}

func (controller *Interlude) Dismiss(immediate bool) {
	controller.initialize(time.Time{})
	if controller.phase == InterludeIdle {
		return
	}
	if immediate || !controller.playing {
		if controller.phase == InterludeFading {
			controller.phase = InterludeIdle // source cancelFadeOut before hidePerformance
		}
		controller.hide()
		return
	}
	if controller.phase == InterludeFading {
		return
	}
	if controller.snapshot.Opacity <= 0 {
		controller.hide()
		return
	}
	controller.fadeElapsed, controller.fadeInitial = 0, controller.snapshot.Opacity
	controller.phase = InterludeFading
}

func (controller *Interlude) SetYParams(now time.Time, params RefParams) {
	controller.initialize(now)
	controller.positionY.SetParams(controller.springNow, params, 0)
}

// SetTransform preserves the first-position snap after each performance reset.
// It commits a zero-delta source update, so changing layout never advances audio.
func (controller *Interlude) SetTransform(now time.Time, left, top float64, immediate bool) {
	controller.initialize(now)
	if !finite(left) || !finite(top) {
		return
	}
	controller.left, controller.top = left, top
	if !immediate && controller.phase != InterludeIdle && !controller.shouldSnap {
		controller.positionY.SetTarget(controller.springNow, top, 0)
	} else {
		controller.shouldSnap = false
		controller.positionY.SetPosition(controller.springNow, top)
	}
	controller.update(0)
}

// AdvanceFrame reproduces one base.update(delta). Unlike Frame, this method
// does not cap delta, matching the reference base class's direct API. Repeating
// the same timestamp consumes no second positive delta; explicit SyncClock and
// zero-delta refreshes at that time can still change the rendered snapshot.
func (controller *Interlude) AdvanceFrame(now time.Time, delta time.Duration) {
	controller.initialize(now)
	if delta < 0 || controller.hasAdvance && !now.After(controller.advanceNow) {
		delta = 0
	}
	if !controller.hasAdvance || now.After(controller.advanceNow) {
		controller.advanceNow = now
	}
	controller.hasAdvance = true
	controller.update(float64(delta) / float64(time.Millisecond))
}

func (controller *Interlude) Frame(now time.Time, input InterludeInput) InterludePose {
	controller.initialize(now)
	if controller.hasFrame && now.Before(controller.frameNow) {
		now = controller.frameNow
	}
	if controller.hasFrame && now.Equal(controller.frameNow) && input == controller.frameInput {
		return controller.Pose()
	}
	delta := min(100*time.Millisecond, max(0, input.CappedDelta))
	if controller.hasFrame && !now.After(controller.frameNow) {
		delta = 0
	}
	if controller.hasAdvance && !now.After(controller.advanceNow) {
		delta = 0
	}
	force := controller.hasFrame && input.SeekRevision != controller.frameInput.SeekRevision
	controller.SyncClock(input.Media)
	if input.Playing {
		controller.Resume()
	} else {
		controller.Pause()
	}
	if input.Present {
		controller.SetInterlude(now, input.Start, input.End, input.Media, force, input.AnchorLineIndex)
	} else {
		controller.ClearInterlude(false)
	}
	// The Native adapter supplies the current frame's desired media position.
	// Base.update adds delta, so prepare its preceding anchor instead of adding
	// the physical frame twice to an already-current absolute host clock.
	if controller.phase == InterludePerforming && controller.playing {
		controller.media -= float64(delta) / float64(time.Millisecond)
	}
	controller.AdvanceFrame(now, delta)
	controller.duet = input.Duet
	controller.frameNow, controller.frameInput, controller.hasFrame = now, input, true
	return controller.Pose()
}

func (controller *Interlude) update(deltaMS float64) {
	if controller.phase == InterludeIdle {
		return
	}
	if controller.phase == InterludeFading {
		controller.fadeElapsed += deltaMS
		progress := clampInterlude(controller.fadeElapsed / 150)
		if progress >= 1 {
			controller.hide()
			return
		}
		controller.snapshot.Opacity = controller.fadeInitial * (1 - interludeExitFade.sample(progress))
		controller.snapshot.Active = true
	} else {
		if controller.playing {
			controller.media += deltaMS
		}
		controller.resolve(max(0, controller.media-controller.anchor))
	}
	controller.springNow = controller.springNow.Add(refDuration(deltaMS * float64(time.Millisecond)))
	controller.positionY.AdvanceFrame(controller.springNow, refDuration(deltaMS*float64(time.Millisecond)))
	controller.rendered, controller.renderedProgress = controller.snapshot, controller.progress
	controller.renderedLeft, controller.renderedTop = controller.left, controller.positionY.Sample(controller.springNow).Position
	if !controller.snapshot.Active {
		controller.phase = InterludeIdle
	}
}

func (controller *Interlude) hide() {
	active := controller.phase != InterludeIdle
	controller.phase = InterludeIdle
	if active {
		controller.rendered = InterludeSnapshot{Scale: 1}
		controller.renderedProgress = [3]float64{}
		controller.renderedLeft, controller.renderedTop = controller.left, controller.positionY.Sample(controller.springNow).Position
	}
}

func (controller *Interlude) resolve(elapsed float64) {
	if elapsed >= controller.totalEnd {
		controller.snapshot = InterludeSnapshot{Scale: 1}
		controller.progress = [3]float64{}
		return
	}
	controller.snapshot.Active = true
	if elapsed < controller.delayEnd {
		controller.snapshot.Opacity, controller.snapshot.Scale = 0, 1
		controller.snapshot.DotOpacities, controller.progress = [3]float64{}, [3]float64{}
		return
	}
	internal := elapsed - controller.delayEnd
	enter := interludeEnter.sample(clampInterlude(internal / 180))
	if elapsed >= controller.bodyEnd {
		exit := elapsed - controller.bodyEnd
		controller.snapshot.Opacity = enter * (1 - interludeExitFade.sample(clampInterlude((exit-750)/250)))
		if exit < 750 {
			controller.snapshot.Scale = 1 + interludeExit1.sample(exit/750)*0.25
		} else {
			controller.snapshot.Scale = 1.25 - interludeExit2.sample(clampInterlude((exit-750)/250))*0.85
		}
		controller.writeDots(internal, [3]float64{1, 1, controller.dot3Target + (1-controller.dot3Target)*clampInterlude(exit/750)})
		return
	}
	controller.snapshot.Opacity = enter
	if controller.mode == InterludeHold {
		controller.snapshot.Scale = 1
		controller.writeDots(internal, [3]float64{1, 1, 1})
		return
	}
	x := math.Mod(internal, controller.period) / controller.period
	angle := 4 * math.Pi * x
	sine, cosine := math.Sin(angle), math.Cos(angle)
	progress := x - 0.084*sine + 0.008*(1-cosine) + 0.0046*sine*(cosine-sine)
	if progress <= 0.5 {
		controller.snapshot.Scale = 1 + progress/0.5*0.25
	} else {
		controller.snapshot.Scale = 1.25 - (progress-0.5)/0.5*0.25
	}
	controller.writeDots(internal, [3]float64{
		interludeFraction(0, controller.segment, internal, 1),
		interludeFraction(controller.segment, controller.segment, internal, 1),
		interludeFraction(controller.segment*2, controller.dot3Duration, internal, controller.dot3Target),
	})
}

func (controller *Interlude) writeDots(internal float64, fractions [3]float64) {
	controller.progress = fractions
	for i, fraction := range fractions {
		enter := clampInterlude((internal - float64(i)*80) / 750)
		controller.snapshot.DotOpacities[i] = (0.2 + 0.7*clampInterlude(fraction)) * enter * enter
	}
}

func interludeFraction(delay, duration, internal, target float64) float64 {
	if internal <= delay {
		return 0
	}
	return interludeLighting.sample(clampInterlude((internal-delay)/duration)) * target
}

func clampInterlude(value float64) float64 { return max(0, min(1, value)) }

func (controller Interlude) Pose() InterludePose {
	if !controller.initialized {
		return InterludePose{Scale: 1, Raw: InterludeSnapshot{Scale: 1}, Phase: InterludeIdle, Mode: InterludeBreathe}
	}
	raw := controller.rendered
	pose := InterludePose{Active: raw.Active, Raw: raw, Opacity: interludeFixed(raw.Opacity, 3), Scale: interludeFixed(raw.Scale, 4),
		X: interludeFixed(controller.renderedLeft, 2), Y: interludeFixed(controller.renderedTop, 2),
		RawX: controller.renderedLeft, RawY: controller.renderedTop, Phase: controller.phase, Mode: controller.mode, Duet: controller.duet}
	for i := range pose.Dots {
		pose.DotOpacities[i] = interludeFixed(raw.DotOpacities[i], 3)
		pose.Dots[i] = InterludeDotPose{Opacity: pose.DotOpacities[i], Progress: controller.renderedProgress[i], Scale: 1}
	}
	if controller.phase == InterludeIdle {
		return pose
	}
	pose.NeedsFrame = !controller.positionY.Sample(controller.springNow).Settled
	if controller.phase == InterludeFading {
		pose.NeedsFrame = true
		return pose
	}
	pose.Occupancy = 1
	if !controller.playing {
		return pose
	}
	elapsed := max(0, controller.media-controller.anchor)
	if elapsed < controller.delayEnd {
		pose.NextWake = refDuration((controller.delayEnd - elapsed) * float64(time.Millisecond))
	} else if controller.mode == InterludeHold && elapsed >= controller.delayEnd+910 && elapsed < controller.bodyEnd {
		pose.NextWake = refDuration((controller.bodyEnd - elapsed) * float64(time.Millisecond))
	} else {
		pose.NeedsFrame = true
	}
	return pose
}

// This is numeric Number.toFixed for the finite UI range. FMA distinguishes an
// exact binary half from multiplication rounded onto a half: JS 1.125→1.13,
// but 100.005→100.00 at two places. No string formatting or allocation occurs.
func interludeFixed(value float64, digits int) float64 {
	scale := math.Pow10(digits)
	abs := math.Abs(value)
	product := abs * scale
	if product >= 1<<53 || !finite(product) {
		return value
	}
	whole := math.Floor(product)
	fraction := product - whole
	if fraction > 0.5 || fraction == 0.5 && math.FMA(abs, scale, -product) >= 0 {
		whole++
	}
	return math.Copysign(whole/scale, value)
}
