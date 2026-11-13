// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

// CSS transitions use wall time; springs use capped UI-frame delta. Retargeting
// samples the old curve before installing its replacement.
type referenceTween struct {
	from, to                   float64
	reversingStart, shortening float64
	start                      time.Time
	duration                   time.Duration
	out                        bool
}

func (v referenceTween) sample(now time.Time) float64 {
	if v.duration <= 0 {
		return v.to
	}
	p := float64(now.Sub(v.start)) / float64(v.duration)
	if v.out {
		p = motion.EaseOut(p)
	} else {
		p = motion.Ease(p)
	}
	return v.from + (v.to-v.from)*p
}
func (v *referenceTween) set(now time.Time, target float64, duration time.Duration, out bool) {
	if v.to == target {
		return
	}
	current := v.sample(now)
	reversingStart, shortening := current, float64(1)
	// CSS Transitions §3.1: reversing an unfinished transition shortens its
	// replacement by the old eased progress (and its previous shortening factor).
	// A plain full-duration retarget visibly lags during rapid seek/hover changes.
	if v.running(now) && target == v.reversingStart {
		progress := float64(now.Sub(v.start)) / float64(v.duration)
		if v.out {
			progress = motion.EaseOut(progress)
		} else {
			progress = motion.Ease(progress)
		}
		shortening = min(1, math.Abs(progress*v.shortening+(1-v.shortening)))
		reversingStart = v.to
		duration = time.Duration(math.Round(float64(duration) * shortening))
	}
	v.from, v.to, v.start, v.duration, v.out = current, target, now, duration, out
	v.reversingStart, v.shortening = reversingStart, shortening
}
func (v *referenceTween) snap(value float64) {
	*v = referenceTween{from: value, to: value, reversingStart: value, shortening: 1}
}
func (v referenceTween) running(now time.Time) bool {
	return v.from != v.to && now.Before(v.start.Add(v.duration))
}

type referenceHeightChange struct {
	index                          int
	delta                          float64
	firstMeasurement               bool
	mainMaskReset, bgMaskReset     bool
	mainElementsNew, bgElementsNew bool
}

// Dormant groups replay these scalar instructions, including the exact UI frame
// that consumes a delayed target. History never retains text or UI resources.
// Before ring reuse, cold scalar checkpoints preserve every offscreen trajectory.
type referenceStep struct {
	id                                                   uint64
	now                                                  time.Time
	delta, media                                         time.Duration
	revision                                             uint64
	fadeRevision                                         uint64
	elementRevision                                      uint64
	layout, paramsChanged, snap, stagger                 bool
	params                                               motion.RefParams
	base, height, width                                  float64
	estimateDelta                                        float64
	focus, latest, firstVisible, windowFrom, windowTo    int
	latestStart                                          time.Duration
	highlighting, playing, dynamic, hover, touch, static bool
	interludeAnchor                                      int
	interludeHeight                                      float64
	changes                                              [maxPreparedLines]referenceHeightChange
	changeCount                                          int
	config                                               referenceFrameOptions
	scaleParamsChanged                                   bool
	hoverGroup, pressedGroup, passedBoundary             int
	hasHoverGroup, hasPressedGroup                       bool
}

type referenceGroupMotion struct {
	y, bg, mainScale, bgScale                  motion.RefSpring
	prefix, height                             float64
	measuredBefore                             int
	measured                                   bool
	step                                       uint64
	springNow                                  time.Time
	fadeRevision                               uint64
	elementRevision                            uint64
	maskDirty                                  bool
	mainMaskDirty, bgMaskDirty                 bool
	mainElementsNew, bgElementsNew             bool
	elementsDirty                              bool
	bgTop, bgTopConfigured                     bool
	bgStyleSlide                               float64
	bgStyleSet, bgMarginPostposition           bool
	active, enabled, connected, copy           bool
	opacityTarget, blurTarget, bgOpacityTarget float64
	hover                                      bool
	springDisabled                             bool
	cssY, cssMainScale, cssBGScale             referenceTween
	background                                 referenceColorTween
	exited                                     time.Time
	opacity, blur, bgOpacity, bright, dim      referenceTween
}

// Metadata is a cold scalar snapshot. Background checkpoint work may copy it;
// it intentionally contains no source document, text, UI resource or scene pointer.
type referenceGroupMetadata struct {
	index                              int
	group                              presentationGroup
	mainStart, mainEnd, bgStart, bgEnd time.Duration
}

func (step referenceStep) highlighted(_ int, group presentationGroup) bool {
	return step.highlighting && group.start <= step.latestStart && group.end > step.latestStart
}

func (r *referenceScene) appendStep(step referenceStep) {
	r.pollCheckpoint()
	if r.lastStep-r.firstStep+1 >= uint64(len(r.history)) {
		r.cancelCheckpoint()
		r.historyBroken = true
	}
	if r.historyBroken {
		r.firstStep = r.lastStep + 1
	}
	r.lastStep++
	step.id = r.lastStep
	r.history[(r.lastStep-1)%uint64(len(r.history))] = step
	r.motionFrames[(r.lastStep-1)%uint64(len(r.history))] = motion.RefFrame{Now: step.now, Delta: step.delta}
	r.startCheckpoint()
}

func (r *referenceScene) replay(index int, to uint64) {
	g := &r.groups[index]
	if b := r.frameBase; b != nil && b.valid && to > b.step && g.step < to && r.history[(to-1)%uint64(len(r.history))].now == b.now {
		if g.step < b.step {
			r.replay(index, b.step)
		}
		r.captureFrameGroup(index)
	}
	metadata := r.metadata[index]
	var bgClock *motion.LineAnimationClock
	if metadata.group.backgroundLayout >= 0 {
		bgClock = &r.clocks[metadata.group.backgroundLayout]
	}
	if r.historyBroken {
		if to == 0 {
			return
		}
		step := r.history[(to-1)%uint64(len(r.history))]
		g.prefix, g.height = r.heights.Prefix(index), r.heights.Height(index)
		step.layout, step.snap, step.static, step.paramsChanged = true, true, true, true
		step.changeCount = 0
		step.estimateDelta = 0
		referenceApplyStep(g, &r.clocks[metadata.group.mainLayout], bgClock, metadata, step)
		return
	}
	for start := max(g.step+1, r.firstStep); start <= to; {
		offset := (start - 1) % uint64(len(r.history))
		count := min(to-start+1, uint64(len(r.history))-offset)
		referenceReplaySteps(g, &r.clocks[metadata.group.mainLayout], bgClock, metadata, r.history[offset:offset+count], r.motionFrames[offset:offset+count])
		start += count
	}
}

// bootstrapGroups preserves the numerical side effects of Base.setLyricLines
// followed by Dom.rebuildLyricView: the estimated-height layout is targeted,
// then the inherited spring parameters are applied again before update(0).
// The central finite difference has a one-sided value at t=0, so merging these
// setters loses an observable initial velocity. This is one cold O(n) pass.
func (r *referenceScene) bootstrapGroups(step referenceStep, base float64) {
	initial := step
	initial.delta, initial.base = 0, base
	initial.layout, initial.stagger = true, false
	initial.playing = r.initialPlaying
	initial.firstVisible = r.targetLineAt(-base, initial, true)
	for i := range r.groups {
		metadata := r.metadata[i]
		var bg *motion.LineAnimationClock
		if metadata.group.backgroundLayout >= 0 {
			bg = &r.clocks[metadata.group.backgroundLayout]
		}
		g := &r.groups[i]
		main := &r.clocks[metadata.group.mainLayout]
		referenceApplyStep(g, main, bg, metadata, initial)
		g.y.SetParams(step.now, step.params, 0)
		g.bg.SetParams(step.now, step.params, 0)
		g.mainScale.SetParams(step.now, r.mainScaleParams, 0)
		g.bgScale.SetParams(step.now, r.bgScaleParams, 0)
		if initial.playing != step.playing {
			resumed := initial
			resumed.playing = step.playing
			resumed.paramsChanged, resumed.scaleParamsChanged = false, false
			referenceApplyStep(g, main, bg, metadata, resumed)
		}
	}
}

func referenceReplaySteps(g *referenceGroupMotion, mainClock, bgClock *motion.LineAnimationClock, metadata referenceGroupMetadata, steps []referenceStep, frames []motion.RefFrame) {
	for i := 0; i < len(steps); {
		step := steps[i]
		if step.id <= g.step {
			i++
			continue
		}
		if step.id != g.step+1 {
			panic("native: missing reference frame history")
		}
		// In an unchanged, wholly detached span, spring queue/arrival updates are
		// still replayed exactly. The visibility proof lets us omit document,
		// CSS-copy lifecycle and animation-controller work on every intermediate
		// frame. No threshold, guessed settling time or catch-up delta is used.
		if !step.layout && !step.paramsChanged && !step.scaleParamsChanged && step.changeCount == 0 && step.estimateDelta == 0 && !step.frameOptions().disableSpring && !g.connected && !g.copy {
			j := i + 1
			for j < len(steps) && !steps[j].layout && !steps[j].paramsChanged && !steps[j].scaleParamsChanged && steps[j].changeCount == 0 && steps[j].estimateDelta == 0 && !steps[j].frameOptions().disableSpring {
				j++
			}
			overscan := step.frameOptions().overscan
			if j-i >= 4 && g.y.ReplayOutside(frames[i:j], -g.height-overscan, step.height+overscan) {
				g.bg.ReplayFrames(frames[i:j])
				g.mainScale.ReplayFrames(frames[i:j])
				g.bgScale.ReplayFrames(frames[i:j])
				last := steps[j-1]
				mainClock.Sync(last.now, last.media, metadata.mainStart, metadata.mainEnd, g.enabled, last.playing, last.revision)
				if bgClock != nil {
					bgClock.Sync(last.now, last.media, metadata.bgStart, metadata.bgEnd, g.enabled, last.playing, last.revision)
				}
				g.bright.snap(1)
				g.dim.snap(0)
				g.step = last.id
				g.springNow = last.now
				i = j
				continue
			}
		}
		referenceApplyStep(g, mainClock, bgClock, metadata, step)
		i++
	}
}

func referenceApplyStep(g *referenceGroupMotion, mainClock, bgClock *motion.LineAnimationClock, metadata referenceGroupMetadata, step referenceStep) {
	index := metadata.index
	config := step.frameOptions()
	if !g.bgTopConfigured {
		g.bgTop, g.bgTopConfigured = metadata.group.backgroundFirst && !config.postposition, true
	}
	delta := step.delta
	if config.disableSpring {
		delta = 0
	}
	springEvent := g.springNow
	if config.disableSpring && !g.springDisabled && g.connected {
		g.cssY.snap(referenceRound(g.y.Sample(springEvent).Position, 1))
		g.cssMainScale.snap(referenceRound(g.mainScale.Sample(springEvent).Position/100, 3) * 100)
		g.cssBGScale.snap(referenceRound(g.bgScale.Sample(springEvent).Position/100, 3) * 100)
	}
	if !config.disableSpring && g.springDisabled {
		g.cssY.snap(referenceRound(g.y.Sample(springEvent).Position, 1))
		g.cssMainScale.snap(referenceRound(g.mainScale.Sample(springEvent).Position/100, 3) * 100)
		g.cssBGScale.snap(referenceRound(g.bgScale.Sample(springEvent).Position/100, 3) * 100)
	}
	if step.estimateDelta != 0 {
		g.prefix += float64(index-g.measuredBefore) * step.estimateDelta
		if !g.measured {
			g.height += step.estimateDelta
		}
	}
	for _, change := range step.changes[:step.changeCount] {
		if change.index < index {
			g.prefix += change.delta
			if change.firstMeasurement {
				g.measuredBefore++
			}
		}
		if change.index == index {
			g.height += change.delta
			g.mainMaskDirty = g.mainMaskDirty || change.mainMaskReset
			g.bgMaskDirty = g.bgMaskDirty || change.bgMaskReset
			g.mainElementsNew = g.mainElementsNew || change.mainElementsNew
			g.bgElementsNew = g.bgElementsNew || change.bgElementsNew
			if change.firstMeasurement {
				g.measured = true
			}
		}
	}
	pg := metadata.group
	enabled := step.highlighted(index, pg)
	active := enabled || index >= step.focus && index < step.latest
	if step.paramsChanged {
		g.y.SetParams(springEvent, step.params, 0)
		g.bg.SetParams(springEvent, step.params, 0)
	}
	if step.scaleParamsChanged {
		g.mainScale.SetParams(springEvent, config.mainScaleParams, 0)
		g.bgScale.SetParams(springEvent, config.bgScaleParams, 0)
	}
	if step.layout {
		top := step.base + g.prefix
		if step.interludeAnchor >= -1 && index > step.interludeAnchor {
			top += step.interludeHeight
		}
		hidden := float64(-80)
		if pg.backgroundFirst && !config.postposition {
			hidden = 80
		}
		slide := hidden
		if active || !step.playing {
			slide = 0
		}
		delay := time.Duration(0)
		if step.stagger {
			delay = motion.RefStagger(step.firstVisible, index, step.focus)
		}
		if step.snap || config.disableSpring {
			g.y.SetPosition(springEvent, top)
			g.bg.SetPosition(springEvent, slide)
		} else {
			g.y.SetTarget(springEvent, top, delay)
			g.bg.SetTarget(springEvent, slide, delay)
		}
		scale, bgScale := float64(100), float64(100)
		if !active && step.playing {
			scale, bgScale = 97, 75
			if config.disableScale {
				scale = 100
			}
		}
		if step.static || config.disableSpring {
			if step.static {
				scale, bgScale = 100, 100
			}
			g.mainScale.SetPosition(springEvent, scale)
			g.bgScale.SetPosition(springEvent, bgScale)
		} else {
			g.mainScale.SetTarget(springEvent, scale, 0)
			g.bgScale.SetTarget(springEvent, bgScale, 0)
		}
		motionBuffer := step.height * .4
		inRange := top <= step.height+config.overscan+motionBuffer && top+g.height >= -config.overscan-motionBuffer
		opacity := float64(1)
		if !inRange {
			opacity = 0
		} else if config.hidePassed && step.playing && index < step.passedBoundary {
			opacity = 1e-4
		} else if enabled {
			opacity = .85
		} else if !step.dynamic {
			opacity = .2
		}
		blur := float64(0)
		if inRange && !active && !step.touch && !step.static && !config.disableBlur {
			distance := math.Abs(float64(index - step.latest))
			if index < step.focus {
				distance = math.Abs(float64(step.focus-index)) + 1
			}
			blur = 1 + distance
			if step.width <= 1024 {
				blur *= .8
			}
			blur = min(5, blur)
		}
		g.opacityTarget, g.blurTarget = opacity, referenceRound(blur, 2)
		bgOpacity := float64(0)
		if active || !step.playing {
			bgOpacity = 1
		}
		g.bgOpacityTarget = bgOpacity
	}
	g.y.AdvanceFrame(step.now, delta)
	g.bg.AdvanceFrame(step.now, delta)
	g.mainScale.AdvanceFrame(step.now, delta)
	g.bgScale.AdvanceFrame(step.now, delta)
	g.springNow = step.now
	if g.active && !active {
		g.exited = step.now
	}
	pos := g.y.Sample(step.now).Position
	connected := pos < step.height+config.overscan && pos+g.height > -config.overscan
	if connected {
		slide := g.bg.Sample(step.now).Position
		if !g.bgStyleSet || math.Abs(slide-g.bgStyleSlide) >= .001 {
			g.bgStyleSlide, g.bgMarginPostposition, g.bgStyleSet = slide, config.postposition, true
		}
		// renderStyles is committed only after show()/mask measurement attaches
		// the group. Detached targets must not start CSS transitions early.
		if math.Abs(g.opacity.to-g.opacityTarget) >= .05 {
			g.opacity.set(step.now, g.opacityTarget, 400*time.Millisecond, false)
		}
		if step.hover {
			duration := 400 * time.Millisecond
			if config.disableSpring {
				duration = 250 * time.Millisecond
			}
			g.blur.set(step.now, 0, duration, false)
		} else if math.Abs(g.blur.to-g.blurTarget) >= .05 || g.hover {
			duration := 400 * time.Millisecond
			if config.disableSpring {
				duration = 250 * time.Millisecond
			}
			g.blur.set(step.now, g.blurTarget, duration, false)
		}
		g.bgOpacity.set(step.now, g.bgOpacityTarget, 300*time.Millisecond, false)
		if step.static || config.disableSpring {
			g.opacity.snap(g.opacityTarget)
			if step.static {
				g.blur.snap(0)
			}
		}
		if config.disableSpring {
			g.cssY.set(step.now, referenceRound(pos, 1), 500*time.Millisecond, false)
			g.cssMainScale.set(step.now, referenceRound(g.mainScale.Sample(step.now).Position/100, 3)*100, 500*time.Millisecond, false)
			g.cssBGScale.set(step.now, referenceRound(g.bgScale.Sample(step.now).Position/100, 3)*100, 500*time.Millisecond, false)
		}
		background := [4]uint8{}
		if step.hasHoverGroup && step.hoverGroup == index {
			background = [4]uint8{255, 255, 255, 17}
			if config.hasHoverColor {
				background = config.hoverColor
			}
		}
		if step.hasPressedGroup && step.pressedGroup == index {
			background = [4]uint8{255, 255, 255, 5}
			if config.hasHoverColor {
				background = config.hoverColor
			}
		}
		g.background.set(step.now, background, 250*time.Millisecond)
	} else if g.connected {
		// Removing a DOM group cancels its CSS transitions. Its inline targets
		// remain available as the starting styles if that element is reattached.
		g.opacity.snap(g.opacity.to)
		g.blur.snap(g.blur.to)
		g.bgOpacity.snap(g.bgOpacity.to)
		g.background.snap(g.background.to)
	}
	inWindow := index >= step.windowFrom && index <= step.windowTo
	wasCopy := g.copy
	if !connected {
		g.copy = false
	} else if inWindow {
		g.copy = true
	} else if g.copy && (g.exited.IsZero() || step.now.Sub(g.exited) >= 500*time.Millisecond) {
		g.copy = false
	}
	if g.copy {
		b, d := float64(0), float64(.2)
		duration := 450 * time.Millisecond
		if active {
			b, d, duration = 1, 0, 300*time.Millisecond
		}
		// A connected cold activation has an explicit WAAPI crossfade. A new
		// mount already under the active ancestor takes its initial CSS style.
		if !wasCopy {
			if g.connected && active {
				g.bright.snap(0)
				g.dim.snap(.2)
			} else {
				g.bright.snap(b)
				g.dim.snap(d)
			}
		}
		g.bright.set(step.now, b, duration, true)
		g.dim.set(step.now, d, duration, true)
	} else {
		g.bright.snap(1)
		g.dim.snap(0)
	}
	if g.fadeRevision != step.fadeRevision {
		g.maskDirty = true
		g.fadeRevision = step.fadeRevision
	}
	if g.elementRevision != step.elementRevision {
		g.elementsDirty = true
		g.elementRevision = step.elementRevision
	}
	if connected && (g.maskDirty || g.elementsDirty || g.mainMaskDirty || g.bgMaskDirty) {
		// Dom.setWordFadeWidth rebuilds the connected mask and calls enable
		// again for an enabled line, which also reanchors float/glow clocks.
		// Detached masks defer this until show(); inactive word effects retain
		// their prior phase because updateMaskImageSync does not enable them.
		if enabled && (g.maskDirty || g.mainMaskDirty) || g.elementsDirty || g.mainElementsNew {
			*mainClock = motion.LineAnimationClock{}
		}
		if bgClock != nil && (enabled && (g.maskDirty || g.bgMaskDirty) || g.elementsDirty || g.bgElementsNew) {
			*bgClock = motion.LineAnimationClock{}
		}
		if g.elementsDirty {
			g.copy = inWindow
			if g.copy && active {
				g.bright.snap(1)
				g.dim.snap(0)
			} else if g.copy {
				g.bright.snap(0)
				g.dim.snap(.2)
			} else {
				g.bright.snap(1)
				g.dim.snap(0)
			}
		}
		g.maskDirty = false
		g.elementsDirty = false
		g.mainMaskDirty, g.bgMaskDirty = false, false
		g.mainElementsNew, g.bgElementsNew = false, false
	}
	mainClock.Sync(step.now, step.media, metadata.mainStart, metadata.mainEnd, enabled, step.playing, step.revision)
	if bgClock != nil {
		bgClock.Sync(step.now, step.media, metadata.bgStart, metadata.bgEnd, enabled, step.playing, step.revision)
	}
	g.active, g.enabled, g.connected = active, enabled, connected
	g.hover = step.hover
	g.springDisabled = config.disableSpring
	g.step = step.id
}

func referenceBackgroundTop(g *referenceGroupMotion, pg presentationGroup, config referenceFrameOptions) bool {
	if g.bgTopConfigured {
		return g.bgTop
	}
	return pg.backgroundFirst && !config.postposition
}

func referenceBackgroundHasMargin(g *referenceGroupMotion, pg presentationGroup, config referenceFrameOptions) bool {
	postposition := config.postposition
	if g.bgStyleSet {
		postposition = g.bgMarginPostposition
	}
	return pg.backgroundFirst && !postposition
}

func (g *referenceGroupMotion) running(now time.Time) bool {
	springs := !g.springDisabled && (!g.y.Sample(now).Settled || !g.bg.Sample(now).Settled || !g.mainScale.Sample(now).Settled || !g.bgScale.Sample(now).Settled)
	return springs || g.cssY.running(now) || g.cssMainScale.running(now) || g.cssBGScale.running(now) || g.background.running(now) || g.opacity.running(now) || g.blur.running(now) || g.bgOpacity.running(now) || g.bright.running(now) || g.dim.running(now)
}
