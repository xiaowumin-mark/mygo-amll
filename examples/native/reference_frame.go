// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"reflect"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/layout"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

const referenceFrameGroupLimit = 128
const referenceFrameHeightLimit = 256

// The public UI API reports a new box during Draw, after its view callback.
// Keep the numerical base of that candidate frame, so a changed box rebuilds
// the same elapsed interval instead of adding an extra tick or losing a tick.
// Only touched scalar groups/clocks and changed heights are saved; no document,
// glyph, bitmap, canvas, painter, or other UI resource is copied here.
type referenceFrameBase struct {
	offset, minOffset, maxOffset                                 float64
	valid                                                        bool
	now                                                          time.Time
	step                                                         uint64
	count                                                        int
	indices                                                      [referenceFrameGroupLimit]int
	groups                                                       [referenceFrameGroupLimit]referenceGroupMotion
	main, bg                                                     [referenceFrameGroupLimit]motion.LineAnimationClock
	heightCount                                                  int
	heightUndo                                                   [referenceFrameHeightLimit]referenceFrameHeightUndo
	maskCount                                                    int
	maskUndo                                                     [referenceFrameGroupLimit * 2]referenceFrameMaskUndo
	heights                                                      *layout.Heights
	previousPlaying, previousHighlighted                         []int
	lastFrame                                                    time.Time
	lastFocus                                                    int
	lastInterlude, lastEnded, lastPlaying, lastStatic, lastHover bool
	ready, dirty, seeking, continuous                            bool
	preparedRevision                                             uint64
	font                                                         ui.Font
	width, scale                                                 float32
	box                                                          ui.Rect
	accelerated, geometryInvalid                                 bool
	defaultHeight, pendingEstimateDelta                          float64
	yParams, lastParams                                          motion.RefParams
	yParamsDirty, scaleParamsDirty                               bool
	yOptions, scaleOptions                                       referenceSpringOptions
	alignFocus, alignAnchor                                      int
	alignInterlude, canDisplayInterlude                          bool
	interlude                                                    motion.Interlude
	interludePose                                                motion.InterludePose
	interludeMetrics                                             referenceInterludeMetrics
	interludeObservedContent                                     [2]float64
	interludeObserved                                            bool
	interludeCSS                                                 referenceTransformTween
	interludeSpringDisabled                                      bool
}

type referenceFrameHeightUndo struct {
	index     int
	value     float64
	measured  bool
	baseIndex bool
}

type referenceLineMask struct {
	signature [32]byte
	has       bool
}

type referenceFrameMaskUndo struct {
	index int
	value referenceLineMask
}

func (r *referenceScene) recordMaskSignature(index int, geometry *lineGeometry) (changed, first bool) {
	if geometry == nil {
		return false, false
	}
	previous := r.maskSignatures[index]
	if previous.has && previous.signature == geometry.maskSignature {
		return false, false
	}
	if b := r.frameBase; b != nil && b.valid {
		if b.maskCount == len(b.maskUndo) {
			r.failFrameBudget()
		} else {
			b.maskUndo[b.maskCount] = referenceFrameMaskUndo{index: index, value: previous}
			b.maskCount++
		}
	}
	r.maskSignatures[index] = referenceLineMask{signature: geometry.maskSignature, has: true}
	return true, !previous.has
}

func (r *referenceScene) saveFrameBase(now time.Time) {
	if r.frameBase == nil {
		r.frameBase = &referenceFrameBase{}
	}
	b := r.frameBase
	if b.valid && b.now == now {
		return
	}
	b.valid = r.ready
	b.now = now
	b.step = r.lastStep
	b.count = 0
	b.heightCount = 0
	b.maskCount = 0
	b.heights = r.heights
	b.previousPlaying = append(b.previousPlaying[:0], r.previousPlaying...)
	b.previousHighlighted = append(b.previousHighlighted[:0], r.previousHighlighted...)
	b.lastFrame, b.lastFocus = r.lastFrame, r.lastFocus
	b.lastInterlude, b.lastEnded, b.lastPlaying, b.lastStatic, b.lastHover = r.lastInterlude, r.lastEnded, r.lastPlaying, r.lastStatic, r.lastHover
	b.ready, b.dirty, b.seeking, b.continuous = r.ready, r.dirty, r.seeking, r.continuous
	b.preparedRevision = r.preparedRevision
	b.font, b.width, b.scale, b.box, b.accelerated, b.geometryInvalid = r.font, r.width, r.scale, r.box, r.accelerated, r.geometryInvalid
	b.defaultHeight, b.pendingEstimateDelta = r.defaultHeight, r.pendingEstimateDelta
	b.offset, b.minOffset, b.maxOffset = r.offset, r.minOffset, r.maxOffset
	b.yParams, b.lastParams, b.yParamsDirty, b.scaleParamsDirty = r.yParams, r.lastParams, r.yParamsDirty, r.scaleParamsDirty
	b.yOptions, b.scaleOptions = r.options.YSpring, r.options.ScaleSpring
	b.alignFocus, b.alignAnchor, b.alignInterlude, b.canDisplayInterlude = r.alignFocus, r.alignAnchor, r.alignInterlude, r.canDisplayInterlude
	b.interlude, b.interludePose, b.interludeMetrics, b.interludeCSS, b.interludeSpringDisabled = r.interlude, r.interludePose, r.interludeMetrics, r.interludeCSS, r.interludeSpringDisabled
	b.interludeObservedContent, b.interludeObserved = r.interludeObservedContent, r.interludeObserved
}

func (r *referenceScene) captureFrameGroup(index int) {
	b := r.frameBase
	if b == nil || !b.valid || r.lastStep <= b.step || r.history[(r.lastStep-1)%uint64(len(r.history))].now != b.now {
		return
	}
	for _, saved := range b.indices[:b.count] {
		if saved == index {
			return
		}
	}
	if r.groups[index].step > b.step {
		r.failFrameBudget()
		return
	}
	if b.count == len(b.groups) {
		r.failFrameBudget()
		return
	}
	i := b.count
	b.count++
	b.indices[i] = index
	b.groups[i] = r.groups[index]
	metadata := r.metadata[index]
	b.main[i] = r.clocks[metadata.group.mainLayout]
	if metadata.group.backgroundLayout >= 0 {
		b.bg[i] = r.clocks[metadata.group.backgroundLayout]
	}
}

func (r *referenceScene) captureFrameHeight(index int, value float64, measured bool) {
	b := r.frameBase
	if b == nil || !b.valid {
		return
	}
	if b.heightCount == len(b.heightUndo) {
		r.failFrameBudget()
		return
	}
	b.heightUndo[b.heightCount] = referenceFrameHeightUndo{index: index, value: value, measured: measured, baseIndex: r.heights == b.heights}
	b.heightCount++
}

func (r *referenceScene) failFrameBudget() {
	r.frameBudgetBroken = true
	r.historyBroken = true
	if r.frameBase != nil {
		r.frameBase.valid = false
	}
	r.cancelCheckpoint()
}

func (r *referenceScene) restoreFrameForViewport(now time.Time, box ui.Rect, scale float32, accelerated bool, font ui.Font, width float32) bool {
	b := r.frameBase
	if b == nil || !b.valid || b.now != now || r.lastFrame != now || r.lastStep <= b.step {
		return false
	}
	if r.box == box && r.scale == scale && r.accelerated == accelerated && r.font == font && r.width == width && !r.geometryInvalid {
		return false
	}
	if r.firstStep > b.step+1 {
		r.failFrameBudget()
		return false
	}
	// A completed result for the provisional header must be invalidated before
	// polling; a canceled task retains the single inflight slot until it exits.
	r.cancelCheckpoint()
	for i := b.maskCount - 1; i >= 0; i-- {
		undo := b.maskUndo[i]
		r.maskSignatures[undo.index] = undo.value
	}
	b.maskCount = 0
	for i := b.heightCount - 1; i >= 0; i-- {
		undo := b.heightUndo[i]
		if undo.baseIndex && b.heights != nil {
			_ = b.heights.SetHeight(undo.index, undo.value)
		}
		r.measured[undo.index] = undo.measured
	}
	b.heightCount = 0
	for i, index := range b.indices[:b.count] {
		r.groups[index] = b.groups[i]
		metadata := r.metadata[index]
		r.clocks[metadata.group.mainLayout] = b.main[i]
		if metadata.group.backgroundLayout >= 0 {
			r.clocks[metadata.group.backgroundLayout] = b.bg[i]
		}
	}
	r.lastStep = b.step
	r.heights = b.heights
	r.lastFrame, r.lastFocus = b.lastFrame, b.lastFocus
	r.lastInterlude, r.lastEnded, r.lastPlaying, r.lastStatic, r.lastHover = b.lastInterlude, b.lastEnded, b.lastPlaying, b.lastStatic, b.lastHover
	r.ready, r.dirty, r.seeking, r.continuous = b.ready, b.dirty, b.seeking, b.continuous
	r.preparedRevision = b.preparedRevision
	r.font, r.width, r.scale, r.box, r.accelerated, r.geometryInvalid = b.font, b.width, b.scale, b.box, b.accelerated, b.geometryInvalid
	r.defaultHeight, r.pendingEstimateDelta = b.defaultHeight, b.pendingEstimateDelta
	r.offset, r.minOffset, r.maxOffset = b.offset, b.minOffset, b.maxOffset
	r.yParams, r.lastParams, r.yParamsDirty, r.scaleParamsDirty = b.yParams, b.lastParams, b.yParamsDirty, b.scaleParamsDirty
	if !reflect.DeepEqual(b.yOptions, r.options.YSpring) {
		r.yParams, _ = applyReferenceSpringOptions(r.yParams, r.options.YSpring)
		r.yParamsDirty = true
	}
	if !reflect.DeepEqual(b.scaleOptions, r.options.ScaleSpring) {
		r.scaleParamsDirty = true
	}
	r.alignFocus, r.alignAnchor, r.alignInterlude, r.canDisplayInterlude = b.alignFocus, b.alignAnchor, b.alignInterlude, b.canDisplayInterlude
	r.interlude, r.interludePose, r.interludeMetrics, r.interludeCSS, r.interludeSpringDisabled = b.interlude, b.interludePose, b.interludeMetrics, b.interludeCSS, b.interludeSpringDisabled
	r.interludeObservedContent, r.interludeObserved = b.interludeObservedContent, b.interludeObserved
	r.previousPlaying = append(r.previousPlaying[:0], b.previousPlaying...)
	r.previousHighlighted = append(r.previousHighlighted[:0], b.previousHighlighted...)
	return true
}
