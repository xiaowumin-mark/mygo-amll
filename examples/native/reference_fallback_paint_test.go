// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"errors"
	"image"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func referenceFallbackFixture(t *testing.T) (*scene, *lineGeometry, referenceRow, *referenceFontProvider) {
	t.Helper()
	cssInlineRequireWindowsFonts(t)
	end := 600 * time.Millisecond
	doc := geometryDocument(t, []lyric.Line{{Text: "AB next", End: 2 * end, Words: []lyric.Word{
		{Text: "AB", End: end, Romanization: "ro0\nro1", Ruby: []lyric.RubyWord{{Text: "UP\nRX", End: end}}},
		{Text: " next", Start: end, End: 2 * end},
	}}})
	provider := newReferenceFontProvider(nil)
	s := newScene(doc, time.Unix(0, 0))
	s.fontProvider = provider
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); provider.Close() })
	font := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600, LineHeight: 1.2, Lang: "en"}
	g, err := buildLineGeometryWithStyle(s.reference.presentation.document.Line(0), font, 240, geometryStyle{Dynamic: true, FontProvider: provider, UseHostLanguage: true})
	if err != nil {
		t.Fatal(err)
	}
	// Frozen original wordWithRuby/romanWord CSS: body36 + Ruby2*15 +
	// Roman2*15. The independent original DOM fixture uses "kan\nji" and
	// measures these same typed 1em slots (baseline_reference_test.go).
	first := g.blocks[0]
	if first.height != 96 || first.bodyY != 30 || first.romanY != 66 || len(first.rubyParts) != 1 || len(first.rubyParts[0].rows) != 2 || len(first.romanRows) != 2 {
		t.Fatal("fixture lost the independently measured 96-DIP Ruby/body/Roman slots")
	}
	// This isolated paint fixture skips frame simulation; initialize only the
	// one group flag consumed by static paint, independently of motion clocks.
	if len(s.reference.groups) == 0 {
		s.reference.groups = make([]referenceGroupMotion, 1)
	}
	s.reference.groups[0].enabled = true
	row := referenceRow{main: g, index: 0, x: 8, y: 8, opacity: 1, visible: true, layerCount: 1}
	row.layers[0] = effectGroupLayer{Geometry: g, Scale: 1, Style: effectStyle{Opacity: 1, Color: ui.RGB(255, 255, 255), HasColor: true}}
	return s, g, row, provider
}

func TestReferenceFallbackPreservesFrozenRubyAndRomanPixels(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		name := "admitted-static-atlas"
		if rejected {
			name = "budget-rejected-readable-layouts"
		}
		t.Run(name, func(t *testing.T) {
			s, g, row, provider := referenceFallbackFixture(t)
			r := &s.reference.effects
			r.BeginFrame()
			if rejected {
				// A root reservation consumes the complete shared estimate. This
				// tests admission without allocating a fake 64-MiB image.
				if err := r.ReserveRoot(4096, 2048, 1); err != nil {
					t.Fatal(err)
				}
				if err := r.prepareReferenceFallback(g, 1); !errors.Is(err, errEffectLimit) {
					t.Fatalf("expected explicit budget rejection, got %v", err)
				}
			} else if err := r.prepareReferenceFallback(g, 1); err != nil {
				t.Fatal(err)
			}
			got := ui.Render(func(c *ui.Context) {
				c.Root().Background(ui.Transparent)
				ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) { s.paintReferenceFallback(p, ui.Rect{W: 280, H: 120}, row) })
			}, 280, 120, 1)
			font := g.font
			sub := font
			sub.Size, sub.LineHeight = 15, 1
			body := provider.Shape("AB", font)
			up, rx := provider.Shape("UP", sub), provider.Shape("RX", sub)
			ro0, ro1 := provider.Shape("ro0", sub), provider.Shape("ro1", sub)
			// Independent public glyph drawing uses frozen source CSS baselines:
			// Ruby13/28, body60, Roman79/94. It does not consume the production
			// atlas or block drawing offsets. Ruby follows source center alignment.
			rubyX := (body.Width - max(up.Width, rx.Width)) / 2
			want := ui.Render(func(c *ui.Context) {
				c.Root().Background(ui.Transparent)
				ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
					color := ui.Color{R: 255, G: 255, B: 255, A: 102}
					p.Glyphs(up.Glyphs, 8+rubyX, 8+13, color)
					p.Glyphs(rx.Glyphs, 8+rubyX, 8+28, color)
					p.Glyphs(body.Glyphs, 8, 8+60, color)
					p.Glyphs(ro0.Glyphs, 8, 8+79, color)
					p.Glyphs(ro1.Glyphs, 8, 8+94, color)
				})
			}, 280, 120, 1)
			clip := image.Rect(8, 8, 8+int(math.Ceil(float64(body.Width))), 104)
			for y := clip.Min.Y; y < clip.Max.Y; y++ {
				for x := clip.Min.X; x < clip.Max.X; x++ {
					a, b := got.RGBAAt(x, y), want.RGBAAt(x, y)
					if abs8(a.A, b.A) > 1 || abs8(a.R, b.R) > 1 {
						t.Fatalf("static glyph pixel %d,%d=%v source-box glyph oracle=%v", x, y, a, b)
					}
				}
			}
			if !rejected {
				e := r.preparedEntry(g, 1)
				if e == nil || !e.dimOnly || e.source != nil || e.blurX != nil || e.glowX != nil || e.glowY != nil {
					t.Fatal("static text created animated or Gaussian surfaces")
				}
			}
		})
	}
}

func TestReferenceFallbackReportsUnverifiedGlyphCapability(t *testing.T) {
	s, g, _, _ := referenceFallbackFixture(t)
	g.bidiFallback = true // Isolated unsupported-capability fixture.
	if err := s.reference.effects.prepareReferenceFallback(g, 1); !errors.Is(err, errReferenceFallbackCapability) {
		t.Fatalf("unverified font/bidi path was not explicitly reported: %v", err)
	}
}

func TestReferenceFallbackStaticSwitchIgnoresRetainedAnimationWake(t *testing.T) {
	cssInlineRequireWindowsFonts(t)
	doc := geometryDocument(t, []lyric.Line{{Text: "hold next", End: 20 * time.Second, Words: []lyric.Word{{Text: "hold", End: 10 * time.Second}, {Text: " next", Start: 10 * time.Second, End: 20 * time.Second}}}})
	provider := newReferenceFontProvider(nil)
	s := newScene(doc, time.Unix(0, 0))
	s.fontProvider, s.font.Family, s.font.Size = provider, "Segoe UI", 30
	defer func() { _ = s.Close(); provider.Close() }()
	if err := s.enableAMLL(false); err != nil {
		t.Fatal(err)
	}
	now := s.origin
	s.clock.seek(now, 100*time.Millisecond)
	s.clock.setPlaying(now, true)
	s.prepareReferenceFrame(now, ui.Rect{W: 500, H: 300}, 1, true)
	for range 45 {
		now = now.Add(16 * time.Millisecond)
		s.updateReference(now, false)
	}
	if !s.reference.effects.Running(now) {
		t.Fatalf("fixture did not establish running retained word clocks: rows=%d entries=%d fallback=%q", s.reference.rowCount, len(s.reference.effects.entries), s.reference.fallback)
	}
	s.forceStatic = true
	s.clock.setPlaying(now, false)
	for range 180 {
		now = now.Add(16 * time.Millisecond)
		s.updateReference(now, false)
	}
	if !s.reference.lastStatic || s.referenceWake(now) != 0 {
		t.Fatalf("static cached text kept continuous wake: static=%v running=%v wake=%v", s.reference.lastStatic, s.reference.running, s.referenceWake(now))
	}
}
