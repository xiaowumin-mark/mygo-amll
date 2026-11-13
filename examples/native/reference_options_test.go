// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestReferenceOptionsDefaultsAndNumericCoercion(t *testing.T) {
	defaults := defaultReferenceOptions()
	if !defaults.EnableSpring || !defaults.EnableScale || !defaults.EnableBlur || defaults.AlignAnchor != referenceAlignCenter || defaults.AlignPosition != .35 || defaults.Overscan != 300 || defaults.WordFadeWidth != .5 {
		t.Fatalf("incorrect defaults: %+v", defaults)
	}
	for _, test := range []struct{ input, want float64 }{{300.99, 300}, {-2, 0}, {4294967297, 1}, {2147483648, 0}} {
		o := defaults
		o.Overscan = test.input
		o.WordFadeWidth = 0
		got, err := normalizeReferenceOptions(o)
		if err != nil {
			t.Fatal(err)
		}
		if got.Overscan != test.want || got.WordFadeWidth != .0001 {
			t.Fatalf("coercion %g: overscan=%g fade=%g", test.input, got.Overscan, got.WordFadeWidth)
		}
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1)} {
		o := defaults
		o.AlignPosition = invalid
		if _, err := normalizeReferenceOptions(o); err == nil {
			t.Fatal("accepted nonfinite alignment")
		}
	}
}

func TestReferenceOptionsAlignmentAndHidePassed(t *testing.T) {
	s := referenceTestScene(t, 20)
	s.forceStatic = true
	now := s.origin
	box := ui.Rect{W: 800, H: 500}
	s.prepareReferenceFrame(now, box, 1, false)
	for _, anchor := range []referenceAlignAnchor{referenceAlignTop, referenceAlignCenter, referenceAlignBottom} {
		o := defaultReferenceOptions()
		o.AlignAnchor = anchor
		o.AlignPosition = .2
		o.Overscan = 0
		if err := s.setReferenceOptions(now, o); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range s.reference.rows[:s.reference.rowCount] {
			if row.index == 0 {
				found = true
				want := 100 - referenceAnchorOffset(anchor, float64(row.height))
				if math.Abs(float64(row.y)-want) > .051 {
					t.Fatalf("%s Y=%v want %v", anchor, row.y, want)
				}
			}
		}
		if !found {
			t.Fatal("aligned focus missing")
		}
	}
	o := defaultReferenceOptions()
	o.HidePassedLines = true
	if err := s.setReferenceOptions(now, o); err != nil {
		t.Fatal(err)
	}
	s.clock.setPlaying(now, true)
	s.seek(now, 6*time.Second)
	for _, row := range s.reference.rows[:s.reference.rowCount] {
		if row.index < s.reference.state.Focus && s.reference.groups[row.index].opacityTarget != 1e-4 {
			t.Fatal("passed line was not hidden")
		}
	}
}

func TestReferenceOptionsPartialSpringsAndInputOwnership(t *testing.T) {
	s := referenceTestScene(t, 10)
	s.forceStatic = true
	now := s.origin
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, false)
	mass, damping, scaleMass := 2.5, 55.0, 3.0
	o := defaultReferenceOptions()
	o.YSpring = referenceSpringOptions{Mass: &mass, Damping: &damping}
	o.ScaleSpring = referenceSpringOptions{Mass: &scaleMass}
	if err := s.setReferenceOptions(now, o); err != nil {
		t.Fatal(err)
	}
	r := s.reference
	if r.yParams.Mass != 2.5 || r.yParams.Damping != 55 || r.mainScaleParams.Mass != 3 || r.bgScaleParams.Mass != 3 {
		t.Fatal("partial parameters did not merge into both source defaults")
	}
	mass, damping, scaleMass = 900, 900, 900
	if *r.options.YSpring.Mass != 2.5 || *r.options.ScaleSpring.Mass != 3 {
		t.Fatal("options retained caller-owned pointers")
	}
	s.seek(now, 9*time.Second)
	if r.yParams.Mass != 2.5 || r.yParams.Damping != 15 || r.yParams.Stiffness != 90 {
		t.Fatalf("seek did not preserve mass and overwrite policy damping/stiffness: %+v", r.yParams)
	}
	bad := -1.0
	o = defaultReferenceOptions()
	o.YSpring.Mass = &bad
	if err := s.setReferenceOptions(now, o); err == nil {
		t.Fatal("invalid spring accepted")
	}
}

func TestReferenceOptionsSpringOffRetainsWordAnimations(t *testing.T) {
	doc := geometryDocument(t, []lyric.Line{{ID: "dynamic", Text: "go now", End: 2 * time.Second, Words: []lyric.Word{{Text: "go ", End: time.Second}, {Text: "now", Start: time.Second, End: 2 * time.Second}}}})
	s := newScene(doc, time.Unix(0, 0))
	defer s.Close()
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	now := s.origin
	o := defaultReferenceOptions()
	o.EnableSpring = false
	o.EnableBlur = false
	o.EnableScale = false
	o.WordFadeWidth = 1.25
	o.Foreground = ui.RGBA(12, 34, 56, .5)
	if err := s.setReferenceOptions(now, o); err != nil {
		t.Fatal(err)
	}
	s.clock.setPlaying(now, true)
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, true)
	r := s.reference
	if r.lastStatic {
		t.Fatal("spring-off incorrectly selected static renderer")
	}
	found := false
	for _, row := range r.rows[:r.rowCount] {
		if row.index == 0 {
			found = true
			if !r.presentation.dynamic || len(row.main.blocks) == 0 {
				t.Fatal("fixture did not exercise dynamic word effects")
			}
			g := &r.groups[0]
			if !g.cssY.running(now) || g.cssY.duration != 500*time.Millisecond || g.y.Sample(now).Position == g.cssY.sample(now) {
				t.Fatal("spring-off did not use independent 500ms CSS transform")
			}
			style := row.layers[0].Style
			if !style.HasColor || style.Color != o.Foreground.SRGB() || style.WordFadeWidth != 1.25 || !style.Enabled || !style.Playing {
				t.Fatalf("independent word style missing: %+v", style)
			}
			if !r.clocks[r.presentation.groups[0].mainLayout].Running(now, motion.MaskAnimation, row.main.end) {
				t.Fatal("spring-off paused mask animation")
			}
			if g.blurTarget != 0 {
				t.Fatal("blur disable did not apply")
			}
		}
	}
	if !found {
		t.Fatal("focus geometry missing")
	}
}

func TestReferenceOptionsPresentationRebuildAndTransparentForeground(t *testing.T) {
	doc := geometryDocument(t, []lyric.Line{{ID: "obscene", Text: "word", End: 2 * time.Second, Words: []lyric.Word{{Text: "word", End: 2 * time.Second, Obscene: true}}}})
	s := newScene(doc, time.Unix(0, 0))
	defer s.Close()
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	old := s.reference
	o := defaultReferenceOptions()
	o.Presentation.MaskMode = presentationMaskFull
	o.Foreground = ui.Color{}
	if err := s.setReferenceOptions(s.origin, o); err != nil {
		t.Fatal(err)
	}
	if s.reference == old || old.presentation != nil || s.document.Line(0).Text() != "word" || s.reference.presentation.document.Line(0).Text() != "****" {
		t.Fatal("processing options did not rebuild an owned presentation")
	}
	s.forceStatic = true
	s.prepareReferenceFrame(s.origin, ui.Rect{W: 800, H: 500}, 1, false)
	style := s.reference.rows[0].layers[0].Style
	if !style.HasColor || style.Color.A != 0 {
		t.Fatal("explicit transparent foreground became default white")
	}
}

func TestReferenceHoverPressedBackgroundTimingAndBounds(t *testing.T) {
	s := referenceTestScene(t, 3)
	s.forceStatic = true
	now := s.origin
	box := ui.Rect{W: 800, H: 500}
	s.prepareReferenceFrame(now, box, 1, false)
	r := s.reference
	row := r.rows[0]
	y := row.y + row.height/2
	s.setReferenceHover(true)
	s.input(ui.InputEvent{Kind: ui.InputPointerMove, Time: now, X: 100, Y: y})
	s.updateReference(now, false)
	if r.groups[0].background.phase.duration != 250*time.Millisecond {
		t.Fatal("hover duration differs from CSS")
	}
	now = now.Add(250 * time.Millisecond)
	s.updateReference(now, false)
	var hovered referenceRow
	for _, candidate := range r.rows[:r.rowCount] {
		if candidate.index == 0 {
			hovered = candidate
		}
	}
	if hovered.backgroundStyle.Color.A != 17 || hovered.backgroundStyle.Rect.X != -hovered.x || hovered.backgroundStyle.Rect.W != 800 || hovered.backgroundStyle.Radius != 7.5 {
		t.Fatalf("hover wrapper shape=%+v", hovered.backgroundStyle)
	}
	s.input(ui.InputEvent{Kind: ui.InputPointerDown, Time: now, X: 100, Y: y})
	s.updateReference(now, false)
	now = now.Add(250 * time.Millisecond)
	s.updateReference(now, false)
	if r.groups[0].background.sample(now)[3] != 5 {
		t.Fatal("pressed style did not override hover")
	}
}

func TestReferenceOptionsBackgroundFontScaleAndPostposition(t *testing.T) {
	doc := geometryDocument(t, []lyric.Line{
		{ID: "bg", ParentID: "main", Text: "bg", Start: time.Second, End: 4 * time.Second, Background: true, Words: []lyric.Word{{Text: "bg", Start: time.Second, End: 2 * time.Second}}},
		{ID: "main", Text: "main", Start: 2 * time.Second, End: 4 * time.Second, Words: []lyric.Word{{Text: "main", Start: 2 * time.Second, End: 4 * time.Second}}},
	})
	s := newScene(doc, time.Unix(0, 0))
	defer s.Close()
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	s.forceStatic = true
	o := defaultReferenceOptions()
	o.BackgroundFontScale = .9
	o.AlwaysPostpositionBackground = true
	if err := s.setReferenceOptions(s.origin, o); err != nil {
		t.Fatal(err)
	}
	s.prepareReferenceFrame(s.origin, ui.Rect{W: 800, H: 500}, 1, false)
	row := s.reference.rows[0]
	if row.background == nil || row.background.font.Size != 27 || row.layers[0].Y != 12 || row.layers[1].Y <= row.layers[0].Y+row.main.height {
		t.Fatalf("BG options did not affect geometry/flow: %+v", row)
	}
}

func TestReferenceWorkerOptionsRemainEquivalentAndOwned(t *testing.T) {
	r := referenceCheckpointFixture(16, 513, 4000)
	for i := range r.groups {
		r.groups[i].cssMainScale.snap(100)
		r.groups[i].cssBGScale.snap(100)
	}
	for id := r.firstStep; id <= r.lastStep; id++ {
		offset := (id - 1) % uint64(len(r.history))
		step := &r.history[offset]
		k := int(id - r.firstStep)
		step.config = referenceFrameOptions{configured: true, overscan: float64(k/127%3) * 100, fadeWidth: .9, foreground: [4]uint8{12, 34, 56, 128}, mainScaleParams: motion.RefMainScaleParams(), bgScaleParams: motion.RefBGScaleParams(), disableSpring: k >= 127 && k < 381, disableScale: k >= 254, disableBlur: k >= 381, postposition: k >= 254, hidePassed: k >= 127}
		if k%127 == 0 {
			step.layout = true
		}
		step.hasHoverGroup, step.hoverGroup = true, k/127%16
		step.hasPressedGroup, step.pressedGroup = k%83 < 20, k/127%16
		step.passedBoundary = step.focus
	}
	worker, eager := snapshotReferenceCheckpoint(r), snapshotReferenceCheckpoint(r)
	if worker == nil || eager == nil {
		t.Fatal("scalar option snapshot rejected")
	}
	referenceCheckpointEager(eager)
	worker.run()
	if !worker.complete || !reflect.DeepEqual(worker.groups, eager.groups) || !reflect.DeepEqual(worker.clocks, eager.clocks) {
		t.Fatal("options/foreground/spring-off checkpoint differs from eager source order")
	}
	r.history[0].config.foreground = [4]uint8{255, 0, 0, 255}
	if worker.steps[0].config.foreground != [4]uint8{12, 34, 56, 128} {
		t.Fatal("worker options alias owner state")
	}
}

func TestReferenceVisualOptionsPreservePendingStagger(t *testing.T) {
	s := referenceTestScene(t, 10)
	now := s.origin
	s.clock.setPlaying(now, true)
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, true)
	r := s.reference
	g := &r.groups[1]
	g.y.SetTarget(now, 123, 200*time.Millisecond)
	for _, field := range []string{"foreground", "hover", "fade"} {
		options := cloneReferenceOptions(r.options)
		switch field {
		case "foreground":
			options.Foreground = ui.RGBA(30, 50, 70, .5)
		case "hover":
			color := ui.RGB(10, 20, 30)
			options.HoverColor = &color
		case "fade":
			options.WordFadeWidth = .8
		}
		before := g.y
		if err := s.setReferenceOptions(now, options); err != nil {
			t.Fatal(err)
		}
		step := r.history[(r.lastStep-1)%uint64(len(r.history))]
		if step.layout || g.y != before {
			t.Fatalf("%s option retargeted/cancelled an unchanged pending Y stagger", field)
		}
	}
}

func TestReferenceFadeWidthDefersDetachedClockReset(t *testing.T) {
	base := time.Unix(0, 0)
	metadata := referenceGroupMetadata{index: 0, group: presentationGroup{start: 0, end: 2 * time.Second, backgroundLayout: -1}, mainEnd: 2 * time.Second}
	g := referenceGroupMotion{height: 60, y: motion.NewRefSpring(base, 1000, motion.RefDefaultParams())}
	var clock motion.LineAnimationClock
	step := referenceStep{now: base, media: 500 * time.Millisecond, playing: true, highlighting: true, height: 500, width: 800}
	referenceApplyStep(&g, &clock, nil, metadata, step)
	step.now, step.fadeRevision = base.Add(300*time.Millisecond), 1
	referenceApplyStep(&g, &clock, nil, metadata, step)
	if !g.maskDirty || clock.Position(step.now, motion.EmphasisGlowAnimation, 5*time.Second) != 800*time.Millisecond {
		t.Fatal("detached mask rebuilt its animations before show")
	}
	g.y.SetPosition(step.now, 100)
	step.now = base.Add(500 * time.Millisecond)
	referenceApplyStep(&g, &clock, nil, metadata, step)
	if g.maskDirty || clock.Position(step.now, motion.EmphasisGlowAnimation, 5*time.Second) != 500*time.Millisecond {
		t.Fatal("show did not consume the pending mask rebuild at current media")
	}
}
