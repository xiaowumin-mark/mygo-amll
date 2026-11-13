// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

func TestReferenceStationaryPointerTracksCurrentFrame(t *testing.T) {
	s := referenceTestScene(t, 20)
	s.forceStatic = true
	box := ui.Rect{X: 30, Y: 40, W: 800, H: 500}
	now := s.origin
	s.prepareReferenceFrame(now, box, 1, false)
	r := s.reference
	var first referenceRow
	for _, row := range r.rows[:r.rowCount] {
		if row.index == 0 {
			first = row
			break
		}
	}
	if first.main == nil {
		t.Fatal("first row missing")
	}
	s.setReferenceHover(true)
	s.setReferencePointer(100, first.y+first.height/2, true)
	now = now.Add(time.Millisecond)
	s.prepareReferenceFrame(now, box, 1, false)
	if r.hoverGroup != 0 || r.groups[0].background.to != [4]uint8{255, 255, 255, 17} {
		t.Fatal("initial pointer target was not committed in its frame")
	}
	// Scroll exactly one main wrapper while the pointer remains stationary.
	r.offset = float64(first.height)
	r.continuous, r.dirty = true, true
	now = now.Add(16 * time.Millisecond)
	s.prepareReferenceFrame(now, box, 1, false)
	step := r.history[(r.lastStep-1)%uint64(len(r.history))]
	if r.hoverGroup != 1 || step.hoverGroup != 1 || !step.hasHoverGroup {
		t.Fatalf("stationary pointer target=%d, history=%d/%v", r.hoverGroup, step.hoverGroup, step.hasHoverGroup)
	}
	if r.groups[1].background.to != [4]uint8{255, 255, 255, 17} || r.groups[0].background.to != [4]uint8{} {
		t.Fatal("hover CSS targets lagged the current frame")
	}
	if s.referenceHitGroup(100, first.y+first.height/2) != 1 {
		t.Fatal("prepared input geometry disagrees with current-frame preview")
	}
	before := r.revision
	s.setReferencePointer(100, first.y+first.height/2, true)
	if r.revision != before || r.dirty {
		t.Fatal("unchanged pointer invalidated the frame or layout")
	}
}

func TestReferenceHitUsesDOMOrderAndAttachedRows(t *testing.T) {
	s := referenceTestScene(t, 2)
	r := s.reference
	r.box = ui.Rect{W: 800, H: 500}
	r.rowCount = 2
	for i := range 2 {
		r.rows[i] = referenceRow{index: i, visible: true, hitCount: 1}
		r.rows[i].hitBounds[0] = ui.Rect{Y: 100, W: 800, H: 100}
	}
	if got := s.referenceHitGroup(400, 150); got != 1 {
		t.Fatalf("overlap target=%d, want later DOM sibling", got)
	}
	r.rows[1].visible = false
	if got := s.referenceHitGroup(400, 150); got != 0 {
		t.Fatalf("detached/invisible row intercepted input: %d", got)
	}
	if got := s.referenceHitGroup(400, 200); got != -1 {
		t.Fatalf("half-open bottom intercepted input: %d", got)
	}
}

func TestReferencePointerWindowCoordinatesFollowWidget(t *testing.T) {
	s := referenceTestScene(t, 20)
	s.forceStatic = true
	box := ui.Rect{X: 20, Y: 30, W: 800, H: 500}
	now := s.origin
	s.prepareReferenceFrame(now, box, 1, false)
	s.setReferenceHover(true)
	s.setReferencePointer(100, 175, true)
	now = now.Add(time.Millisecond)
	s.prepareReferenceFrame(now, box, 1, false)
	r := s.reference
	if r.hoverGroup != 0 {
		t.Fatalf("initial hit=%d", r.hoverGroup)
	}
	box.Y += 400
	s.prepareReferenceFrame(now, box, 1, false)
	if r.hoverGroup != -1 {
		t.Fatal("moving the widget retained a stale relative-pointer hover")
	}
}

func TestReferencePointerUsesPhysicalWrapperAndBackgroundOverflow(t *testing.T) {
	s := referenceTestScene(t, 2)
	now := s.origin
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, true)
	r := s.reference
	r.presentation.groups[0].backgroundLayout = 1
	row := referenceRow{index: 0,
		main:       &lineGeometry{font: ui.Font{Size: 30}, height: 80, width: 728},
		background: &lineGeometry{font: ui.Font{Size: 21}, height: 40, width: 731.6},
	}
	g := referenceGroupMotion{connected: true, height: 104,
		y:         motion.NewRefSpring(now, 150, motion.RefDefaultParams()),
		bg:        motion.NewRefSpring(now, -40, motion.RefDefaultParams()),
		mainScale: motion.NewRefSpring(now, 97, motion.RefMainScaleParams()),
		bgScale:   motion.NewRefSpring(now, 75, motion.RefBGScaleParams()),
	}
	step := referenceStep{now: now, playing: true, config: r.frameOptions()}
	step.config.widthPadding = 30
	// Opacity zero leaves CSS pointer events enabled until visibility:hidden.
	g.bgOpacity.snap(0)
	s.positionReferenceRow(&row, &g, step, false)
	if !row.contains(300, 270) {
		t.Fatal("transparent background wrapper overflow did not receive pointer events")
	}
	g.bg.SetPosition(now, -80)
	s.positionReferenceRow(&row, &g, step, false)
	if row.contains(300, 270) {
		t.Fatal("visibility:hidden background retained its hit region")
	}
	r.presentation.groups[0].backgroundFirst = true
	g.bg.SetPosition(now, 40)
	s.positionReferenceRow(&row, &g, step, false)
	if row.height != 133 || g.height != 104 {
		t.Fatalf("physical/cached wrapper heights=%v/%v, want 133/104", row.height, g.height)
	}
	// The no-spring CSS transform can remain on screen after the numeric target
	// has detached the element. A detached subtree must not intercept input.
	step.config.disableSpring = true
	g.connected = false
	g.cssY.snap(100)
	s.positionReferenceRow(&row, &g, step, false)
	if row.visible {
		t.Fatal("detached CSS-transition position remained interactive")
	}
}
