// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

func (s *scene) referenceInput(ev ui.InputEvent) bool {
	r := s.reference
	if s.closed {
		return false
	}
	switch ev.Kind {
	case ui.InputScroll:
		r.inertia = false
		r.speed = 0
		r.beginInteraction(false)
		r.wheelUntil = ev.Time.Add(150 * time.Millisecond)
		r.endedInteraction = time.Time{}
		r.offset = r.clamp(r.offset + float64(ev.DY))
		r.dirty = true
		r.revision++
		return true
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		s.pointerDown, s.dragged = true, false
		s.setReferencePointer(ev.X, ev.Y, true)
		r.pressedGroup = s.referenceHitGroup(ev.X, ev.Y)
		r.hoverGroup = r.pressedGroup
		r.revision++
		r.pressX, r.pressY, r.lastY = ev.X, ev.Y, ev.Y
		r.pressOffset = r.offset
		r.lastPointer = ev.Time
		r.speed = 0
		r.interrupted = r.inertia || !r.wheelUntil.IsZero() || r.interacting
		r.inertia = false
		r.wheelUntil = time.Time{}
		return true
	case ui.InputPointerMove:
		previousRevision := r.revision
		previousHover := r.hoverGroup
		s.setReferencePointer(ev.X, ev.Y, ev.X >= 0 && ev.X < r.box.W && ev.Y >= 0 && ev.Y < r.box.H)
		hovered := s.referenceHitGroup(ev.X, ev.Y)
		changed := hovered != previousHover
		if changed {
			r.hoverGroup = hovered
			r.revision++
		}
		if !s.pointerDown {
			return changed || previousRevision != r.revision
		}
		if !s.dragged && (math.Abs(float64(ev.X-r.pressX)) > 10 || math.Abs(float64(ev.Y-r.pressY)) > 10) {
			s.dragged = true
			r.beginInteraction(true)
		}
		if s.dragged {
			r.offset = r.clamp(r.pressOffset - float64(ev.Y-r.pressY))
			if dt := ev.Time.Sub(r.lastPointer); dt > 0 {
				r.speed = float64(ev.Y-r.lastY) / (float64(dt) / float64(time.Millisecond))
			}
			r.continuous = true
			r.dirty = true
			r.revision++
			r.lastY = ev.Y
			r.lastPointer = ev.Time
		}
		return true
	case ui.InputPointerUp:
		if ev.Button != 0 || !s.pointerDown {
			return false
		}
		s.pointerDown = false
		r.pressedGroup = -1
		r.revision++
		if !s.dragged {
			if r.interrupted {
				r.endInteraction(ev.Time)
			}
			if !ev.Canceled {
				index := s.referenceHit(ev.X, ev.Y)
				s.selectedLine = index
				s.requestSeek(ev.Time, index)
			}
			return true
		}
		if ev.Canceled || math.Abs(r.speed) < .1 {
			r.speed = 0
			r.endInteraction(ev.Time)
		} else {
			r.inertia = true
			r.inertiaFrame = ev.Time
		}
		r.revision++
		return true
	}
	return false
}

func (s *scene) referenceHit(x, y float32) int {
	index := s.referenceHitGroup(x, y)
	if index < 0 {
		return -1
	}
	return s.reference.presentation.groups[index].main
}

func (s *scene) referenceHitGroup(x, y float32) int {
	r := s.reference
	if x < 0 || x >= r.box.W || y < 0 || y >= r.box.H {
		return -1
	}
	// Later DOM siblings paint above earlier ones, including during overlap.
	for i := r.rowCount - 1; i >= 0; i-- {
		row := &r.rows[i]
		if row.visible && row.contains(x, y) {
			return row.index
		}
	}
	return -1
}

func (row *referenceRow) contains(x, y float32) bool {
	for _, rect := range row.hitBounds[:row.hitCount] {
		if x >= rect.X && x < rect.X+rect.W && y >= rect.Y && y < rect.Y+rect.H {
			return true
		}
	}
	return false
}

// Preview only the bounded prepared rows. Applying the pending header to value
// copies makes stationary-pointer CSS targets part of this frame's history,
// rather than discovering the new hover target one frame after the movement.
func (s *scene) refreshReferenceHover(step *referenceStep, static bool) {
	r := s.reference
	if !r.pointerKnown {
		return
	}
	hit := -1
	x, y := r.pointerX-r.box.X, r.pointerY-r.box.Y
	if r.hover && x >= 0 && x < r.box.W && y >= 0 && y < r.box.H {
		for i := r.rowCount - 1; i >= 0; i-- {
			row := r.rows[i]
			g, metadata := r.groups[row.index], r.metadata[row.index]
			main := r.clocks[metadata.group.mainLayout]
			var bg *motion.LineAnimationClock
			if metadata.group.backgroundLayout >= 0 {
				clock := r.clocks[metadata.group.backgroundLayout]
				bg = &clock
			}
			referenceApplyStep(&g, &main, bg, metadata, *step)
			s.positionReferenceRow(&row, &g, *step, static)
			if row.visible && row.contains(x, y) {
				hit = row.index
				break
			}
		}
	}
	r.hoverGroup = hit
	step.hoverGroup, step.hasHoverGroup = hit, hit >= 0
}

func (s *scene) referenceScrollLines(now time.Time, delta int) {
	r := s.reference
	if len(r.groups) == 0 || r.heights == nil {
		return
	}
	group := max(0, r.state.Focus)
	if s.manualLine >= 0 {
		group = r.presentation.groupForLine[s.manualLine]
	}
	group = min(max(group+delta, 0), len(r.groups)-1)
	s.manualLine = r.presentation.groups[group].main
	s.selectedLine = s.manualLine
	r.suspended = true
	r.offset = r.clamp(r.heights.Prefix(group) - r.heights.Prefix(max(0, r.state.Focus)))
	r.endInteraction(now)
	r.dirty = true
	r.revision++
}
