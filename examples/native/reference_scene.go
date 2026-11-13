// SPDX-License-Identifier: AGPL-3.0-only
// Presentation policy follows AMLL f32285864056587c4afa59d310208b0aa829cb53,
// packages/core/src/lyric-player/base/{index,layout,group,scroll}.ts and DOM CSS.
package main

import (
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/layout"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

const (
	referenceOverscan      = 300.0
	referenceHistoryLimit  = 4096
	referenceFrameInterval = time.Second / 60
)

type referenceRow struct {
	index            int
	main, background *lineGeometry
	x, y, height     float32
	layers           [2]effectGroupLayer
	layerCount       int
	highlights       [2][]ui.Rect
	opacity, blur    float32
	visible          bool
	err              error
	backgroundStyle  effectGroupBackground
	hitBounds        [4]ui.Rect
	hitCount         int
}

type referenceScene struct {
	presentation                             *presentation
	options                                  referenceOptions
	yParams, mainScaleParams, bgScaleParams  motion.RefParams
	yParamsDirty, scaleParamsDirty           bool
	hoverGroup, pressedGroup                 int
	pointerKnown                             bool
	pointerX, pointerY                       float32
	index                                    *timeline.Index
	state                                    timeline.VisualState
	visualPlaying, visualHighlighted         []int
	visualReady                              bool
	visualPosition                           time.Duration
	visualSeekRevision                       uint64
	visualFocus                              int
	queryPlaying, queryHighlighted           []int
	effects                                  effectRenderer
	root                                     ui.Effect
	allowSoftware                            bool
	hostW, hostH                             float32
	hover                                    bool
	initialPlaying                           bool
	revision, seekRevision, preparedRevision uint64
	fadeRevision                             uint64
	elementRevision                          uint64
	lastFrame, now                           time.Time
	groups                                   []referenceGroupMotion
	metadata                                 []referenceGroupMetadata
	clocks                                   []motion.LineAnimationClock
	maskSignatures                           []referenceLineMask
	history                                  []referenceStep
	motionFrames                             []motion.RefFrame
	checkpoint                               *referenceCheckpointTask
	checkpointGeneration                     uint64
	checkpointNotBefore                      time.Time
	historyBroken                            bool
	frameBase                                *referenceFrameBase
	frameBudgetBroken                        bool
	firstStep, lastStep                      uint64
	rows                                     [maxPreparedLines]referenceRow
	rowCount                                 int
	heights                                  *layout.Heights
	measured                                 []bool
	defaultHeight, pendingEstimateDelta      float64
	geometryInvalid                          bool
	font                                     ui.Font
	width, scale                             float32
	box                                      ui.Rect
	accelerated                              bool
	ready, dirty, seeking, continuous        bool
	lastFocus                                int
	alignFocus                               int
	lastInterlude, lastEnded                 bool
	alignInterlude                           bool
	alignAnchor                              int
	interlude                                motion.Interlude
	interludePose                            motion.InterludePose
	interludeMetrics                         referenceInterludeMetrics
	interludeObservedContent                 [2]float64
	interludeObserved                        bool
	interludeCSS                             referenceTransformTween
	interludeSpringDisabled                  bool
	lastPlaying, lastStatic, lastHover       bool
	previousPlaying, previousHighlighted     []int
	canDisplayInterlude                      bool
	lastParams                               motion.RefParams
	offset, minOffset, maxOffset             float64
	suspended, touch, inertia                bool
	interacting                              bool
	wheelUntil, endedInteraction             time.Time
	pressX, pressY, lastY                    float32
	pressOffset, speed                       float64
	lastPointer                              time.Time
	inertiaFrame                             time.Time
	interrupted                              bool
	fallback                                 string
	lastFallback                             string
	fallbackWidth                            float32
	fallbackLayout                           *ui.TextLayout
	running                                  bool
}

func (s *scene) enableAMLL(allowSoftware bool) error {
	return s.enableAMLLWithOptions(allowSoftware, defaultReferenceOptions(), s.origin)
}

func (s *scene) enableAMLLWithOptions(allowSoftware bool, options referenceOptions, now time.Time) error {
	options, err := normalizeReferenceOptions(options)
	if err != nil {
		return err
	}
	presentation, err := newPresentationWithOptions(s.document, options.Presentation)
	if err != nil {
		return err
	}
	index, err := timeline.New(presentation.timeline)
	if err != nil {
		return err
	}
	root, err := RootEffect()
	if err != nil {
		return err
	}
	var retiredCheckpoint *referenceCheckpointTask
	var hostW, hostH float32
	var hover bool
	if s.reference != nil {
		retiredCheckpoint = s.reference.checkpoint
		hostW, hostH, hover = s.reference.hostW, s.reference.hostH, s.reference.hover
		_ = s.reference.close()
	}
	s.reference = &referenceScene{presentation: presentation, index: index, root: root, allowSoftware: allowSoftware, dirty: true, lastFocus: -1, now: now, options: options, hostW: hostW, hostH: hostH, hover: hover, hoverGroup: -1, pressedGroup: -1, checkpoint: retiredCheckpoint, scaleParamsDirty: true, initialPlaying: s.clock.playing}
	r := s.reference
	r.yParams, _ = applyReferenceSpringOptions(motion.RefYParams(true, false, false, 0, false), options.YSpring)
	r.mainScaleParams, _ = applyReferenceSpringOptions(motion.RefMainScaleParams(), options.ScaleSpring)
	r.bgScaleParams, _ = applyReferenceSpringOptions(motion.RefBGScaleParams(), options.ScaleSpring)
	r.interlude = motion.NewInterlude(now)
	r.state.Playing = make([]int, 0, len(presentation.groups))
	r.state.Highlighted = make([]int, 0, len(presentation.groups))
	r.previousPlaying = make([]int, 0, len(presentation.groups))
	r.previousHighlighted = make([]int, 0, len(presentation.groups))
	r.queryPlaying = make([]int, 0, len(presentation.groups))
	r.queryHighlighted = make([]int, 0, len(presentation.groups))
	r.visualPlaying = make([]int, 0, len(presentation.groups))
	r.visualHighlighted = make([]int, 0, len(presentation.groups))
	r.clocks = make([]motion.LineAnimationClock, presentation.document.Len())
	r.maskSignatures = make([]referenceLineMask, presentation.document.Len())
	r.metadata = make([]referenceGroupMetadata, len(presentation.groups))
	for i, pg := range presentation.groups {
		main := presentation.document.Line(pg.mainLayout)
		metadata := referenceGroupMetadata{index: i, group: pg, mainStart: presentation.lineTimes[pg.mainLayout].start, mainEnd: main.End()}
		if pg.backgroundLayout >= 0 {
			bg := presentation.document.Line(pg.backgroundLayout)
			metadata.bgStart, metadata.bgEnd = presentation.lineTimes[pg.backgroundLayout].start, bg.End()
		}
		r.metadata[i] = metadata
	}
	s.focusEvents = s.focusEvents[:0]
	for _, group := range presentation.groups {
		s.focusEvents = append(s.focusEvents, group.start, group.end)
	}
	slices.Sort(s.focusEvents)
	s.focusEvents = slices.Compact(s.focusEvents)
	s.invalidateGeometry()
	s.manualLine, s.selectedLine = -1, -1
	return nil
}

func (s *scene) hasReference() bool { return s.reference != nil }
func (s *scene) referenceRootEffect() ui.Effect {
	if s.reference == nil {
		return nil
	}
	return s.reference.root
}
func (s *scene) setReferenceHostSize(width, height float32) {
	if r := s.reference; r != nil && (r.hostW != width || r.hostH != height) {
		r.hostW, r.hostH = width, height
		r.dirty = true
		r.revision++
	}
}
func (s *scene) setReferenceHover(hover bool) {
	if r := s.reference; r != nil && r.hover != hover {
		r.hover = hover
		r.revision++
		if !hover {
			r.hoverGroup = -1
		}
	}
}

// setReferencePointer retains the stationary pointer in window coordinates.
// The current frame re-tests it after spring/CSS movement, as browser :hover
// does when an element moves without a pointer event. It does not relayout.
func (s *scene) setReferencePointer(x, y float32, over bool) {
	r := s.reference
	if r == nil || s.closed {
		return
	}
	x, y = x+r.box.X, y+r.box.Y
	if r.pointerKnown != over || over && (r.pointerX != x || r.pointerY != y) {
		r.pointerKnown, r.pointerX, r.pointerY = over, x, y
		if !over {
			r.hoverGroup = -1
		}
		r.revision++
	}
}

func (r *referenceScene) visualAt(position time.Duration) timeline.VisualState {
	r.state = r.index.VisualAt(referenceMediaPosition(position), r.state.Playing, r.state.Highlighted)
	for i, v := range r.state.Playing {
		r.state.Playing[i] = r.presentation.indexToGroup[v]
	}
	for i, v := range r.state.Highlighted {
		r.state.Highlighted[i] = r.presentation.indexToGroup[v]
	}
	if r.state.Focus >= 0 {
		r.state.Focus = r.presentation.indexToGroup[r.state.Focus]
	}
	if r.state.LatestHighlighted >= 0 {
		r.state.LatestHighlighted = r.presentation.indexToGroup[r.state.LatestHighlighted]
	}
	if r.state.InInterlude && r.state.Interlude.Anchor >= 0 {
		r.state.Interlude.Anchor = r.presentation.indexToGroup[r.state.Interlude.Anchor]
	}
	// Original Timeline.performPlayback activates only lines actually sampled
	// inside their playing interval. A continuous tick which skips entire lines
	// must not reconstruct them as if the caller had sought to this position.
	// In particular scrollToIndex persists across gaps/end, while FocusController
	// may independently align an interlude or the last line.
	jump := !r.visualReady || r.state.Position < r.visualPosition || r.seekRevision != r.visualSeekRevision
	if !jump {
		added := false
		for _, index := range r.state.Playing {
			if _, present := slices.BinarySearch(r.visualPlaying, index); !present {
				added = true
				break
			}
		}
		if added {
			r.state.Highlighted = append(r.state.Highlighted[:0], r.state.Playing...)
			r.state.Focus = r.state.Playing[0]
		} else {
			r.state.Highlighted = append(r.state.Highlighted[:0], r.visualHighlighted...)
			r.state.Focus = r.visualFocus
		}
		if r.state.Ended || r.state.InInterlude && len(r.state.Playing) == 0 {
			r.state.Highlighted = r.state.Highlighted[:0]
		}
		r.state.LatestHighlighted = -1
		for _, index := range r.state.Highlighted {
			r.state.LatestHighlighted = max(r.state.LatestHighlighted, index)
		}
	}
	r.visualPlaying = append(r.visualPlaying[:0], r.state.Playing...)
	r.visualHighlighted = append(r.visualHighlighted[:0], r.state.Highlighted...)
	r.visualReady, r.visualPosition, r.visualSeekRevision, r.visualFocus = true, r.state.Position, r.seekRevision, r.state.Focus
	return r.state
}

func (s *scene) referenceFocusAt(position time.Duration) int {
	r := s.reference
	if len(r.presentation.groups) == 0 {
		return -1
	}
	if s.manualLine >= 0 {
		return min(s.manualLine, s.document.Len()-1)
	}
	if r.visualReady && referenceMediaPosition(position) == r.visualPosition && r.visualFocus >= 0 {
		return r.presentation.groups[r.visualFocus].main
	}
	state := r.index.VisualAt(referenceMediaPosition(position), r.queryPlaying, r.queryHighlighted)
	r.queryPlaying, r.queryHighlighted = state.Playing, state.Highlighted
	if state.Focus < 0 {
		return -1
	}
	return r.presentation.groups[r.presentation.indexToGroup[state.Focus]].main
}

// Base.setCurrentTime rounds the pushed media time to an integer millisecond.
// Keep that compatibility at the Native presentation boundary; the immutable
// core and the host playback clock retain their nanosecond precision.
func referenceMediaPosition(position time.Duration) time.Duration {
	whole, part := position/time.Millisecond, position%time.Millisecond
	if part >= time.Millisecond/2 {
		if whole == time.Duration(1<<63-1)/time.Millisecond {
			return time.Duration(1<<63 - 1)
		}
		whole++
	} else if part < -time.Millisecond/2 {
		if whole == time.Duration(-1<<63)/time.Millisecond {
			return time.Duration(-1 << 63)
		}
		whole--
	}
	return whole * time.Millisecond
}

func (s *scene) updateReference(now time.Time, instant bool) {
	r := s.reference
	if now.Before(r.now) {
		now = r.now
	}
	r.now = now
	state := r.visualAt(s.clock.positionAt(now))
	if !r.wheelUntil.IsZero() && !now.Before(r.wheelUntil) {
		deadline := r.wheelUntil
		r.wheelUntil = time.Time{}
		r.endInteraction(deadline)
	}
	dt := now.Sub(r.inertiaFrame)
	if r.inertia {
		r.inertiaFrame = now
	}
	if r.inertia && dt > 0 && dt <= 100*time.Millisecond {
		if math.Abs(r.speed) > .05 {
			r.offset = r.clamp(r.offset - r.speed*float64(dt)/float64(time.Millisecond))
			r.speed *= math.Pow(.95, float64(dt)/(float64(time.Second)/60))
			r.continuous = true
			r.dirty = true
		} else {
			r.inertia = false
			r.endInteraction(now)
		}
	}
	_, focusPlaying := slices.BinarySearch(state.Playing, state.Focus)
	if r.suspended && !r.seeking && !instant && !state.InInterlude && focusPlaying && (state.Focus != r.lastFocus || r.lastInterlude) && !r.endedInteraction.IsZero() && now.Sub(r.endedInteraction) >= 500*time.Millisecond {
		visible := false
		for _, row := range r.rows[:r.rowCount] {
			if row.index == state.Focus && row.visible {
				visible = true
				break
			}
		}
		if visible {
			r.resetInteraction()
			s.manualLine, s.selectedLine = -1, -1
			r.dirty = true
		}
	}
	if state.Focus != r.lastFocus || s.clock.playing != r.lastPlaying {
		r.dirty = true
	}
	if instant && r.ready {
		if !r.seeking {
			r.seekRevision++
			r.revision++
		}
		r.seeking = true
		r.continuous = false
		r.dirty = true
	}
	if r.box.W > 0 && r.box.H > 0 {
		s.prepareReferenceFrame(now, r.box, r.scale, r.accelerated)
	}
}

func (r *referenceScene) resetInteraction() {
	r.offset = 0
	r.speed = 0
	r.suspended = false
	r.touch = false
	r.inertia = false
	r.interacting = false
	r.wheelUntil = time.Time{}
	r.endedInteraction = time.Time{}
	r.continuous = false
}
func (r *referenceScene) endInteraction(now time.Time) {
	r.interacting = false
	r.wheelUntil = time.Time{}
	r.endedInteraction = now
}
func (r *referenceScene) beginInteraction(touch bool) {
	r.endedInteraction = time.Time{}
	if !r.interacting {
		r.interacting = true
		r.suspended = true
		r.touch = touch
	}
}
func (r *referenceScene) clamp(offset float64) float64 {
	return min(max(offset, r.minOffset), r.maxOffset)
}

func referenceFont(font ui.Font, hostW, hostH float32) ui.Font {
	if font.Size <= 0 {
		font.Size = max(12, max(.05*hostH, .025*hostW))
		if hostW <= 768 {
			font.Size = max(12, .08*hostW)
		}
	}
	font.LineHeight = 1.2
	return font
}

func (r *referenceScene) measureInterludeMetrics(fontSize, windowHeight, paddingX float64, scale float32) {
	metrics := measureReferenceInterludeDots(fontSize, windowHeight, paddingX, scale)
	textScale := cssGeometryScale([]float32{scale})
	physicalUnits := 64 * textScale
	// The source observes content-box. A padding-only media-query change
	// updates CSS drawing but leaves its cached borderBoxSize unchanged until
	// content size changes. Recover integer physical units before unscaling.
	content := [2]float64{
		math.Floor(math.Round((3*metrics.DotDiameter+2*metrics.Gap)*physicalUnits)/textScale) / 64,
		math.Floor(math.Round(metrics.ContentHeight*physicalUnits)/textScale) / 64,
	}
	if r.interludeObserved && content == r.interludeObservedContent {
		metrics.MeasuredWidth, metrics.MeasuredHeight = r.interludeMetrics.MeasuredWidth, r.interludeMetrics.MeasuredHeight
	} else {
		if metrics.MeasuredWidth != r.interludeMetrics.MeasuredWidth || metrics.MeasuredHeight != r.interludeMetrics.MeasuredHeight {
			r.dirty = true
		}
		r.interludeObservedContent, r.interludeObserved = content, true
	}
	r.interludeMetrics = metrics
}

func (s *scene) resetReferenceGeometry(now time.Time, font ui.Font, width, scale float32, box ui.Rect) {
	r := s.reference
	r.pollCheckpoint()
	r.cancelCheckpoint()
	wasReady := r.ready && r.heights != nil && len(r.groups) == len(r.presentation.groups)
	clear(r.rows[:])
	r.rowCount = 0
	s.cache.clear()
	r.effects.Clear()
	oldHeights := r.heights
	defaultHeight := float64(max(1, box.H/5))
	r.heights, _ = layout.NewHeights(len(r.presentation.groups), defaultHeight)
	if wasReady && oldHeights != nil {
		for i := range r.groups {
			if r.measured[i] {
				_ = r.heights.SetHeight(i, oldHeights.Height(i))
			}
		}
	}
	r.font, r.width, r.scale = font, width, scale
	r.geometryInvalid = false
	if wasReady {
		r.pendingEstimateDelta += defaultHeight - r.defaultHeight
		r.defaultHeight = defaultHeight
		r.checkpointNotBefore = now.Add(500 * time.Millisecond)
		r.dirty = true
		return
	}
	r.defaultHeight = defaultHeight
	r.history = make([]referenceStep, referenceHistoryLimit)
	r.motionFrames = make([]motion.RefFrame, referenceHistoryLimit)
	r.firstStep, r.lastStep = 1, 0
	r.groups = make([]referenceGroupMotion, len(r.presentation.groups))
	r.measured = make([]bool, len(r.groups))
	params := r.yParams
	initialHeight := r.hostH
	if initialHeight <= 0 {
		initialHeight = box.H
	}
	for i := range r.groups {
		group := &r.groups[i]
		group.height = r.heights.Height(i)
		group.prefix = r.heights.Prefix(i)
		group.step = 0
		group.springNow = now
		group.y = motion.NewRefSpring(now, float64(initialHeight*2), params)
		hidden := float64(-80)
		if r.presentation.groups[i].backgroundFirst && !r.options.AlwaysPostpositionBackground {
			hidden = 80
		}
		group.bg = motion.NewRefSpring(now, hidden, params)
		group.bgTop, group.bgTopConfigured = r.presentation.groups[i].backgroundFirst && !r.options.AlwaysPostpositionBackground, true
		group.mainScale = motion.NewRefSpring(now, 100, r.mainScaleParams)
		group.bgScale = motion.NewRefSpring(now, 100, r.bgScaleParams)
		group.cssY.snap(0)
		group.cssMainScale.snap(100)
		group.cssBGScale.snap(100)
		group.opacity.snap(1)
		group.blur.snap(0)
		group.bgOpacity.snap(0)
		group.bright.snap(0)
		group.dim.snap(.2)
	}
	r.ready = false
	r.dirty = true
	r.lastFrame = now
	r.lastParams = motion.RefParams{}
}

func (s *scene) referenceBase(focus int, box ui.Rect) float64 {
	r := s.reference
	if focus < 0 || r.heights.Len() == 0 {
		return 0
	}
	focus = min(focus, r.heights.Len()-1)
	return r.options.AlignPosition*float64(box.H) - referenceAnchorOffset(r.options.AlignAnchor, r.heights.Height(focus)) - r.heights.Prefix(focus)
}

func referenceRound(value float64, digits int) float64 {
	p := math.Pow10(digits)
	abs := math.Abs(value)
	product := abs * p
	if product >= 1<<53 || math.IsInf(product, 0) || math.IsNaN(product) {
		return value
	}
	whole := math.Floor(product)
	fraction := product - whole
	if fraction > .5 || fraction == .5 && math.FMA(abs, p, -product) >= 0 {
		whole++
	}
	return math.Copysign(whole/p, value)
}

func (s *scene) referenceLayoutBase(box ui.Rect, step referenceStep) (base, focalTop, total float64) {
	r := s.reference
	targetHeight := r.heights.Height(r.alignFocus)
	focalTop = r.heights.Prefix(r.alignFocus)
	if r.alignInterlude {
		focalTop = r.heights.Prefix(r.alignAnchor + 1)
		targetHeight = step.interludeHeight
	} else if step.interludeAnchor >= -1 && r.alignFocus > step.interludeAnchor {
		focalTop += step.interludeHeight
	}
	total = r.heights.Total()
	if step.interludeAnchor >= -1 {
		total += step.interludeHeight
	}
	base = r.options.AlignPosition*float64(box.H) - referenceAnchorOffset(r.options.AlignAnchor, targetHeight) - focalTop
	return
}

func (r *referenceScene) targetLineAt(offset float64, step referenceStep, inclusiveBottom bool) int {
	if step.interludeAnchor >= -1 {
		boundary := r.heights.Prefix(step.interludeAnchor + 1)
		if offset > boundary {
			if offset <= boundary+step.interludeHeight {
				return min(r.heights.Len()-1, step.interludeAnchor+1)
			}
			offset -= step.interludeHeight
		}
	}
	if inclusiveBottom {
		offset = math.Nextafter(offset, math.Inf(-1))
	}
	return min(r.heights.Len()-1, r.heights.LineAt(offset))
}

func referenceGroupHeight(main, bg *lineGeometry, baseFont float32, bgFirst, expanded bool, slide float64) float64 {
	return referenceGroupFlowHeight(main, bg, baseFont, bgFirst, bgFirst, expanded, slide)
}

func referenceGroupFlowHeight(main, bg *lineGeometry, baseFont float32, top, topMargin, expanded bool, slide float64, scales ...float32) float64 {
	h := float64(2 * cssLayoutLength(.4*baseFont, scales...))
	if main != nil {
		h += float64(main.height)
	}
	if bg != nil {
		if top || expanded {
			h += float64(cssLayoutLength(.3*baseFont, scales...) + bg.height)
			if topMargin {
				h += referenceRound(-float64(bg.height)*(1-min(1, max(0, 1-math.Abs(slide)/80))), 1)
			}
		}
	}
	return max(1, h)
}

func (s *scene) prepareReferenceFrame(now time.Time, box ui.Rect, scale float32, accelerated bool) {
	r := s.reference
	if s.closed || box.W <= 0 || box.H <= 0 {
		return
	}
	var previouslyVisible [maxPreparedLines]int
	previouslyVisibleCount := 0
	for _, row := range r.rows[:r.rowCount] {
		if row.visible {
			previouslyVisible[previouslyVisibleCount] = row.index
			previouslyVisibleCount++
		}
	}
	hostW, hostH := r.hostW, r.hostH
	if hostW <= 0 {
		hostW = box.W
	}
	if hostH <= 0 {
		hostH = box.H
	}
	font := referenceFont(s.font, hostW, hostH)
	paddingX := font.Size
	if hostW <= 500 {
		paddingX = 20
	}
	pad := cssLayoutLength(paddingX, scale)
	borderWidth := cssLayoutLength(max(1, box.W-2*pad), scale)
	width := borderWidth
	if r.presentation.hasDuet {
		width -= cssLayoutLength(.15*width, scale)
	}
	r.restoreFrameForViewport(now, box, scale, accelerated, font, width)
	r.pollCheckpoint()
	if r.ready && r.lastStep-r.firstStep+1 >= referenceHistoryLimit {
		r.cancelCheckpoint()
		r.historyBroken = true
	}
	r.saveFrameBase(now)
	geometryChanged := r.geometryInvalid || r.heights == nil || r.font != font || r.width != width || r.scale != scale || r.box.H != box.H
	if geometryChanged {
		s.resetReferenceGeometry(now, font, width, scale, box)
	}
	if r.box != box || r.accelerated != accelerated {
		r.dirty = true
	}
	r.box, r.accelerated = box, accelerated
	s.viewport, s.scale, s.accelerated = box, scale, accelerated
	if r.lastFrame == now && r.preparedRevision == r.revision && !r.dirty && r.ready {
		return
	}
	state := r.visualAt(s.clock.positionAt(now))
	n := len(r.groups)
	if n == 0 {
		r.rowCount = 0
		r.lastFrame = now
		r.ready = true
		return
	}
	focus := max(0, state.Focus)
	timelineChanged := focus != r.lastFocus || state.InInterlude != r.lastInterlude || state.Ended != r.lastEnded || !slices.Equal(state.Playing, r.previousPlaying) || !slices.Equal(state.Highlighted, r.previousHighlighted)
	static := s.forceStatic || s.reduceMotion || !accelerated && !r.allowSoftware || r.historyBroken
	var interval time.Duration
	hasInterval := focus > 0
	if hasInterval {
		interval = r.presentation.groups[focus].start - r.presentation.groups[focus-1].start
	}
	policyChanged := r.options.EnableSpring && (!r.ready || r.seeking || focus != r.lastFocus || state.InInterlude != r.lastInterlude || state.Ended != r.lastEnded)
	params := r.yParams
	if policyChanged {
		policy := motion.RefYParams(r.seeking, state.InInterlude, state.Ended, interval, hasInterval)
		params.Stiffness, params.Damping = policy.Stiffness, policy.Damping
	}
	step := referenceStep{now: now, delta: min(max(0, now.Sub(r.lastFrame)), 100*time.Millisecond), media: state.Position, revision: r.seekRevision, focus: focus, latest: max(focus, state.LatestHighlighted), playing: s.clock.playing, dynamic: r.presentation.dynamic, hover: r.hover, touch: r.touch, static: static, params: params, paramsChanged: policyChanged, height: float64(box.H), width: float64(box.W), interludeAnchor: -2, snap: r.continuous || static}
	step.estimateDelta = r.pendingEstimateDelta
	step.fadeRevision = r.fadeRevision
	step.elementRevision = r.elementRevision
	step.config = r.frameOptions()
	step.config.widthPadding = float64(pad)
	step.paramsChanged = policyChanged || r.yParamsDirty
	step.scaleParamsChanged = r.scaleParamsDirty
	step.hoverGroup, step.pressedGroup = r.hoverGroup, r.pressedGroup
	step.hasHoverGroup = r.hoverGroup >= 0 && r.hover
	step.hasPressedGroup = r.pressedGroup >= 0 && s.pointerDown
	step.passedBoundary = focus
	if state.InInterlude {
		step.passedBoundary = state.Interlude.Anchor + 1
	}
	if step.paramsChanged {
		r.interlude.SetYParams(now, params)
	}
	interludeInput := motion.InterludeInput{Media: state.Position, CappedDelta: step.delta, Playing: s.clock.playing, Present: state.InInterlude, Start: state.Interlude.Start, End: state.Interlude.End, AnchorLineIndex: state.Interlude.Anchor, SeekRevision: r.seekRevision}
	if state.InInterlude && state.Interlude.Anchor+1 < n {
		interludeInput.Duet = r.presentation.document.Line(r.presentation.groups[state.Interlude.Anchor+1].mainLayout).Duet()
	}
	previousInterludePose := r.interludePose
	if !r.ready || now.After(r.lastFrame) || r.seeking {
		r.interlude.SyncClock(state.Position)
	}
	if s.clock.playing {
		r.interlude.Resume()
	} else {
		r.interlude.Pause()
	}
	r.measureInterludeMetrics(float64(font.Size), float64(hostH), float64(paddingX), scale)
	interludeConfigured := false
	configureInterlude := func() {
		if state.InInterlude {
			r.canDisplayInterlude = r.interlude.SetInterlude(now, state.Interlude.Start, state.Interlude.End, state.Position, r.seeking || !r.ready, state.Interlude.Anchor)
		} else {
			r.interlude.ClearInterlude(false)
			r.canDisplayInterlude = false
		}
		interludeConfigured = true
	}
	if r.dirty || !r.ready || step.paramsChanged || timelineChanged || s.clock.playing != r.lastPlaying || static != r.lastStatic {
		configureInterlude()
	}
	canDisplayInterlude := state.InInterlude && r.canDisplayInterlude
	resolveAlignment := func() {
		canDisplayInterlude = state.InInterlude && r.canDisplayInterlude
		step.interludeAnchor, step.interludeHeight = -2, 0
		if canDisplayInterlude {
			step.interludeAnchor = state.Interlude.Anchor
			step.interludeHeight = r.interludeMetrics.MeasuredHeight + float64(.8*font.Size)
		}
		if !r.suspended {
			r.alignInterlude = canDisplayInterlude
			r.alignAnchor = state.Interlude.Anchor
			r.alignFocus = focus
			if state.Ended {
				r.alignFocus = n - 1
			} else if state.InInterlude && !canDisplayInterlude {
				r.alignFocus = min(n-1, state.Interlude.Anchor+1)
			}
		} else if r.alignInterlude && !canDisplayInterlude {
			r.alignInterlude = false
			r.alignFocus = min(n-1, r.alignAnchor+1)
		}
	}
	resolveAlignment()
	step.highlighting = len(state.Highlighted) > 0
	if step.highlighting {
		step.latestStart = r.presentation.groups[state.LatestHighlighted].start
	}
	step.windowFrom, step.windowTo = focus-2, focus+2
	for _, v := range state.Highlighted {
		step.windowFrom = min(step.windowFrom, v-2)
		step.windowTo = max(step.windowTo, v+2)
	}
	oldRows, oldCount := r.rows, r.rowCount
	clear(r.rows[:])
	r.rowCount = 0
	s.cache.beginFrame()
	base, focalTop, totalHeight := s.referenceLayoutBase(box, step)
	if !r.ready && r.lastStep == 0 {
		r.bootstrapGroups(step, base)
		// The initial policy and Dom.rebuild's explicit parameter application
		// precede the first measured-layout target, rather than replacing it.
		step.paramsChanged, step.scaleParamsChanged = false, false
	}
	r.minOffset = min(0, -focalTop)
	r.maxOffset = max(0, base+totalHeight-float64(box.H)/2)
	r.offset = r.clamp(r.offset)
	base -= r.offset
	center := max(0, r.targetLineAt(float64(box.H)*.35-base, step, false))
	geometryCount := 0
	paddingSides := 2
	if r.presentation.hasDuet {
		paddingSides = 1
	}
	style := geometryStyle{Dynamic: r.presentation.dynamic, BackgroundFontSize: max(10, float32(r.options.BackgroundFontScale)*font.Size), CSSPaddingSides: paddingSides, FontProvider: s.fontProvider, UseHostLanguage: true, BorderWidth: borderWidth, TextScale: scale}
	geometryLimited := false
	add := func(index int) bool {
		if index < 0 || index >= n || r.rowCount >= len(r.rows) {
			return false
		}
		for _, row := range r.rows[:r.rowCount] {
			if row.index == index {
				return true
			}
		}
		pg := r.presentation.groups[index]
		cost := 1
		if pg.backgroundLayout >= 0 {
			cost++
		}
		if geometryCount+cost > geometryEntryLimit {
			geometryLimited = true
			return false
		}
		row := referenceRow{index: index}
		for _, previous := range oldRows[:oldCount] {
			if previous.index == index {
				row.highlights = previous.highlights
				break
			}
		}
		row.main, row.err = s.cache.getWithStyle(r.presentation.document, pg.mainLayout, font, width, scale, style)
		if row.main != nil {
			row.main.start = r.metadata[index].mainStart
			s.cache.pin(row.main)
		}
		if pg.backgroundLayout >= 0 {
			var err error
			row.background, err = s.cache.getWithStyle(r.presentation.document, pg.backgroundLayout, font, width, scale, style)
			if row.err == nil {
				row.err = err
			}
			if row.background != nil {
				row.background.start = r.metadata[index].bgStart
				s.cache.pin(row.background)
			}
		}
		geometryCount += cost
		r.replay(index, r.lastStep)
		group := &r.groups[index]
		active := step.highlighted(index, pg) || index >= step.focus && index < step.latest
		change := referenceHeightChange{index: index}
		change.mainMaskReset, change.mainElementsNew = r.recordMaskSignature(pg.mainLayout, row.main)
		if pg.backgroundLayout >= 0 {
			change.bgMaskReset, change.bgElementsNew = r.recordMaskSignature(pg.backgroundLayout, row.background)
		}
		if row.main != nil {
			firstMeasurement := !r.measured[index]
			r.measured[index] = true
			height := referenceGroupFlowHeight(row.main, row.background, font.Size, referenceBackgroundTop(group, pg, step.config), referenceBackgroundHasMargin(group, pg, step.config), active || !s.clock.playing, group.bg.Sample(group.springNow).Position, scale)
			delta := height - r.heights.Height(index)
			if delta != 0 || firstMeasurement {
				r.captureFrameHeight(index, r.heights.Height(index), !firstMeasurement)
				change.delta, change.firstMeasurement = delta, firstMeasurement
				_ = r.heights.SetHeight(index, height)
				r.dirty = true
			}
		}
		if change.delta != 0 || change.firstMeasurement || change.mainMaskReset || change.bgMaskReset {
			step.changes[step.changeCount] = change
			step.changeCount++
		}
		r.rows[r.rowCount] = row
		r.rowCount++
		return true
	}
	_ = add(center)
	// Keep actual visible departing rows pinned during a long seek, then cover
	// the target viewport before spending the remaining geometry budget on overscan.
	for _, index := range previouslyVisible[:previouslyVisibleCount] {
		_ = add(index)
	}
	for _, overscan := range [...]float64{0, r.options.Overscan} {
		for distance := 1; r.rowCount < len(r.rows); distance++ {
			added := false
			before, after := center-distance, center+distance
			if before >= 0 && base+r.heights.Prefix(before)+r.heights.Height(before) > -overscan {
				added = add(before) || added
			}
			if after < n && base+r.heights.Prefix(after) < float64(box.H)+overscan {
				added = add(after) || added
			}
			if !added {
				break
			}
		}
	}
	slices.SortFunc(r.rows[:r.rowCount], func(a, b referenceRow) int { return a.index - b.index })
	step.layout = r.dirty || !r.ready || step.paramsChanged || timelineChanged || s.clock.playing != r.lastPlaying || static != r.lastStatic
	if step.layout && !interludeConfigured {
		configureInterlude()
		resolveAlignment()
	}
	base, focalTop, totalHeight = s.referenceLayoutBase(box, step)
	r.minOffset = min(0, -focalTop)
	r.maxOffset = max(0, base+totalHeight-float64(box.H)/2)
	r.offset = r.clamp(r.offset)
	step.base = base - r.offset
	step.firstVisible = r.targetLineAt(-step.base, step, true)
	// Only a normal playback timeline change has stagger. Initial mounting,
	// resize, configuration, seek, wheel and continuous input disable it.
	step.stagger = timelineChanged && r.ready && !r.seeking && !geometryChanged && !r.continuous && r.wheelUntil.IsZero() && s.clock.playing == r.lastPlaying
	if canDisplayInterlude && step.layout {
		x := float64(0)
		if interludeInput.Duet {
			x = float64(box.W) - r.interludeMetrics.MeasuredWidth
		}
		r.interlude.SetTransform(now, x, step.base+r.heights.Prefix(step.interludeAnchor+1)+float64(.4*font.Size), step.snap || !r.options.EnableSpring)
	}
	r.interlude.AdvanceFrame(now, step.delta)
	r.interludePose = r.interlude.Pose()
	r.interludePose.Duet = interludeInput.Duet
	if !r.options.EnableSpring && !static {
		if !r.interludeSpringDisabled {
			scale := previousInterludePose.Scale
			if scale <= 0 {
				scale = 1
			}
			r.interludeCSS.snap([3]float64{previousInterludePose.X, previousInterludePose.Y, scale})
		}
		r.interludeCSS.set(now, [3]float64{r.interludePose.X, r.interludePose.Y, r.interludePose.Scale}, 500*time.Millisecond)
		pose := r.interludeCSS.sample(now)
		r.interludePose.X, r.interludePose.Y, r.interludePose.Scale = pose[0], pose[1], pose[2]
	} else {
		r.interludeCSS.snap([3]float64{r.interludePose.X, r.interludePose.Y, r.interludePose.Scale})
	}
	r.interludeSpringDisabled = !r.options.EnableSpring && !static
	s.refreshReferenceHover(&step, static)
	r.appendStep(step)
	r.pendingEstimateDelta = 0
	if r.historyBroken {
		static = true
	}
	r.effects.BeginFrame()
	r.fallback = ""
	if geometryLimited {
		r.fallback = "Geometry budget fallback: some viewport/overscan rows exceed the 32-line cache"
	}
	if !accelerated && !r.allowSoftware {
		r.fallback = "Software fallback: static highlighting; shaders, blur, glow and spring motion disabled"
	} else if static {
		r.fallback = "Reduced motion / static lyrics: word effects, blur and spring motion disabled"
	}
	if r.historyBroken {
		r.fallback = "Motion history budget fallback: static lyrics; checkpoint exceeded its bounded time or memory window"
	}
	if r.frameBudgetBroken {
		r.fallback = "Layout frame budget fallback: static lyrics after too many same-frame revisions"
	}
	if err := r.effects.ReserveRoot(box.W, box.H, scale); err != nil {
		r.fallback = "Effect budget fallback: " + err.Error()
	}
	r.running = false
	for i := range r.rows[:r.rowCount] {
		row := &r.rows[i]
		r.replay(row.index, r.lastStep)
		group := &r.groups[row.index]
		s.positionReferenceRow(row, group, step, static)
		if row.visible && row.main != nil && row.err == nil && !static {
			bounds := ui.Rect{W: width, H: 2*cssLayoutLength(.4*font.Size, scale) + row.main.height}
			if row.background != nil {
				bounds.H += cssLayoutLength(.3*font.Size, scale) + row.background.height*2
			}
			row.err = r.effects.PrepareGroup(row.main, row.layers[:row.layerCount], scale, bounds, row.backgroundStyle)
			if row.err == nil {
				for _, layer := range row.layers[:row.layerCount] {
					if err := r.effects.SyncFrame(layer.Geometry, now, state.Position, layer.Style); err != nil {
						row.err = err
						break
					}
				}
			}
			if row.err != nil {
				r.fallback = "Effect budget fallback: static line; " + row.err.Error()
			}
		}
		if row.visible && row.main != nil && (static || row.err != nil) {
			for _, layer := range row.layers[:row.layerCount] {
				if err := r.effects.prepareReferenceFallback(layer.Geometry, scale); err != nil {
					// Preserve the failure which selected this path. A second atlas
					// refusal still leaves all retained word/Ruby/Roman text readable.
					if row.err == nil {
						row.err = err
					}
					r.fallback = "Static text fallback: " + row.err.Error()
					break
				}
			}
		}
		if row.visible {
			for layerIndex, layer := range row.layers[:row.layerCount] {
				row.highlights[layerIndex] = row.highlights[layerIndex][:0]
				if group.enabled && r.presentation.dynamic {
					row.highlights[layerIndex] = layer.Geometry.appendStaticHighlights(row.highlights[layerIndex], state.Position)
				}
			}
		}
		if group.running(now) {
			r.running = true
		}
	}
	r.running = r.running || !static && r.effects.Running(now) || r.interludePose.NeedsFrame || r.interludeCSS.phase.running(now)
	if r.fallback != "" {
		if r.fallback != r.lastFallback || r.fallbackWidth != box.W {
			r.fallbackLayout = ui.ShapeText(r.fallback, ui.Font{Size: 11}, max(1, box.W-16))
			r.lastFallback = r.fallback
			r.fallbackWidth = box.W
		}
	} else {
		r.fallbackLayout = nil
		r.lastFallback = ""
	}
	r.lastFrame = now
	r.preparedRevision = r.revision
	r.lastFocus = focus
	r.lastPlaying = s.clock.playing
	r.lastStatic = static
	r.lastHover = r.hover
	r.lastParams = params
	r.yParams = params
	r.yParamsDirty = false
	r.scaleParamsDirty = false
	r.lastInterlude, r.lastEnded = state.InInterlude, state.Ended
	r.previousPlaying = append(r.previousPlaying[:0], state.Playing...)
	r.previousHighlighted = append(r.previousHighlighted[:0], state.Highlighted...)
	r.dirty = false
	r.seeking = false
	r.continuous = false
	r.ready = true
}

func (r *referenceScene) close() error {
	r.cancelCheckpoint()
	r.checkpoint = nil
	err := r.effects.Close()
	r.presentation, r.index, r.heights = nil, nil, nil
	r.state = timeline.VisualState{}
	r.queryPlaying, r.queryHighlighted = nil, nil
	r.visualPlaying, r.visualHighlighted = nil, nil
	r.previousPlaying, r.previousHighlighted = nil, nil
	r.groups = nil
	r.metadata = nil
	r.measured = nil
	r.clocks = nil
	r.maskSignatures = nil
	r.history = nil
	r.motionFrames = nil
	r.frameBase = nil
	clear(r.rows[:])
	r.rowCount = 0
	r.root = nil
	r.fallbackLayout = nil
	return err
}
