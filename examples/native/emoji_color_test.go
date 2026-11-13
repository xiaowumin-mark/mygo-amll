// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/binary"
	"errors"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
)

type emojiSyntheticFont struct {
	paints   map[font.GID]tables.PaintTable
	outlines map[font.GID]font.GlyphOutline
	clips    map[font.GID]tables.ClipBox
	lists    map[uint32][]tables.PaintTable
	colors   font.CPAL
}

func (f *emojiSyntheticFont) color(id font.GID) (tables.PaintTable, bool) {
	p, ok := f.paints[id]
	return p, ok
}
func (f *emojiSyntheticFont) outline(id font.GID) (font.GlyphOutline, bool) {
	p, ok := f.outlines[id]
	return p, ok
}
func (f *emojiSyntheticFont) clip(id font.GID) (tables.ClipBox, bool) {
	p, ok := f.clips[id]
	return p, ok
}
func (f *emojiSyntheticFont) palette() font.CPAL { return f.colors }
func (f *emojiSyntheticFont) upem() uint16       { return 20 }
func (f *emojiSyntheticFont) layers(p tables.PaintColrLayers) ([]tables.PaintTable, error) {
	ls, ok := f.lists[p.FirstLayerIndex]
	if !ok || len(ls) != int(p.NumLayers) {
		return nil, errors.New("synthetic layer reference is invalid")
	}
	return ls, nil
}

func syntheticEmojiFont(paint tables.PaintTable) (*emojiColorFont, *emojiSyntheticFont) {
	f := &emojiSyntheticFont{
		paints: map[font.GID]tables.PaintTable{1: paint},
		outlines: map[font.GID]font.GlyphOutline{
			2: {Segments: emojiRectSegments(0, 0, 20, 20)},
			3: {}, // a valid, transparent space/default-ignorable outline
		},
		clips: make(map[font.GID]tables.ClipBox), lists: make(map[uint32][]tables.PaintTable),
		colors: font.CPAL{{{Red: 255, Alpha: 255}, {Blue: 255, Alpha: 255}, {Red: 255, Alpha: 128}, {Red: 255, Green: 255, Blue: 255, Alpha: 128}}},
	}
	return &emojiColorFont{source: f, glyphCount: 16}, f
}

func emojiSolidPaint(index uint16) tables.PaintSolid {
	return tables.PaintSolid{PaletteIndex: index, Alpha: tables.Fixed214(16384)}
}
func emojiOutlinePaint(paint tables.PaintTable) tables.PaintGlyph {
	return tables.PaintGlyph{GlyphID: 2, Paint: paint}
}
func emojiTestGlyph(owner *emojiColorFont) []referenceColorGlyph {
	return []referenceColorGlyph{{Font: owner, ID: 1, Size: 20}}
}

func TestEmojiColorSolidOutlineAndLifetime(t *testing.T) {
	owner, _ := syntheticEmojiFont(emojiOutlinePaint(emojiSolidPaint(0)))
	raster, err := rasterEmojiColorGlyphs(emojiTestGlyph(owner), ui.RGB(0, 255, 0))
	if err != nil {
		t.Fatal(err)
	}
	if raster.Origin != image.Pt(-1, -21) || raster.Pixels.RGBAAt(10, -10).R != 255 || raster.Pixels.RGBAAt(10, -10).A != 255 || raster.Pixels.RGBAAt(-1, -10).A != 0 {
		t.Fatal("palette, baseline-relative origin or transparent bounds changed")
	}
	// Empty outlines are not unsupported; their advance is owned by shaping.
	space, err := rasterEmojiColorGlyphs([]referenceColorGlyph{{Font: owner, ID: 3, Size: 20}}, ui.RGB(255, 255, 255))
	if err != nil || space.Pixels != nil {
		t.Fatalf("valid empty glyph: %+v %v", space, err)
	}
	mono, err := rasterEmojiColorGlyphs([]referenceColorGlyph{{Font: owner, ID: 2, Size: 20}}, ui.Color{G: 255, A: 128})
	if err != nil || abs8(mono.Pixels.RGBAAt(10, -10).G, 128) > 1 || abs8(mono.Pixels.RGBAAt(10, -10).A, 128) > 1 {
		t.Fatalf("public non-color outline did not receive foreground: %v", err)
	}
	owner.Close()
	owner.Close()
	if _, err := rasterEmojiColorGlyphs(emojiTestGlyph(owner), ui.RGB(255, 255, 255)); !errors.Is(err, errEmojiColorClosed) || owner.newShapingFace() != nil || owner.Stats().Entries != 0 {
		t.Fatal("closed owner retained usable font resources")
	}
}

func TestEmojiColorGradientsAndTransforms(t *testing.T) {
	line := tables.ColorLine{ColorStops: []tables.ColorStop{{PaletteIndex: 0, Alpha: 16384}, {StopOffset: 16384, PaletteIndex: 1, Alpha: 16384}}}
	for _, test := range []struct {
		name  string
		paint tables.PaintTable
		red   uint8
		blue  uint8
	}{
		{"linear", tables.PaintLinearGradient{ColorLine: line, X1: 20, Y2: 20}, 121, 134},
		{"radial", tables.PaintRadialGradient{ColorLine: line, X0: 10, Y0: 10, X1: 10, Y1: 10, Radius1: 10}, 237, 18},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner, _ := syntheticEmojiFont(emojiOutlinePaint(test.paint))
			raster, err := rasterEmojiColorGlyphs(emojiTestGlyph(owner), ui.RGB(255, 255, 255))
			if err != nil {
				t.Fatal(err)
			}
			pixel := raster.Pixels.RGBAAt(10, -10)
			if abs8(pixel.R, test.red) > 1 || abs8(pixel.B, test.blue) > 1 || pixel.A != 255 {
				t.Fatalf("gradient=%v", pixel)
			}
		})
	}
	paint := tables.PaintTranslate{Dx: 10, Dy: 5, Paint: tables.PaintScaleAroundCenter{ScaleX: 8192, ScaleY: 8192, CenterX: 10, CenterY: 10, Paint: emojiOutlinePaint(emojiSolidPaint(0))}}
	owner, _ := syntheticEmojiFont(paint)
	glyphs := emojiTestGlyph(owner)
	glyphs[0].X, glyphs[0].Y = 3, 2
	raster, err := rasterEmojiColorGlyphs(glyphs, ui.RGB(255, 255, 255))
	if err != nil || raster.Origin != image.Pt(17, -19) || raster.Pixels.RGBAAt(20, -15).A != 255 {
		t.Fatalf("nested font transform changed: %+v %v", raster.Origin, err)
	}
}

func TestEmojiColorSrcInClipsParentOnce(t *testing.T) {
	composite := tables.PaintComposite{CompositeMode: tables.CompositeSrcIn, SourcePaint: emojiSolidPaint(0), BackdropPaint: emojiSolidPaint(3)}
	a, _ := syntheticEmojiFont(emojiOutlinePaint(composite))
	b, _ := syntheticEmojiFont(emojiOutlinePaint(emojiSolidPaint(2)))
	render := func(owner *emojiColorFont) emojiColorRaster {
		glyphs := emojiTestGlyph(owner)
		glyphs[0].X = .25 // partial antialiased parent edge
		raster, err := rasterEmojiColorGlyphs(glyphs, ui.RGB(255, 255, 255))
		if err != nil {
			t.Fatal(err)
		}
		return raster
	}
	got, want := render(a), render(b)
	if got.Pixels.Rect != want.Pixels.Rect {
		t.Fatal("composite changed bounds")
	}
	for i, value := range want.Pixels.Pix {
		if abs8(got.Pixels.Pix[i], value) > 1 {
			t.Fatalf("SrcIn squared parent clip alpha at byte=%d: got=%d want=%d", i, got.Pixels.Pix[i], value)
		}
	}
}

func TestEmojiColorRejectsUnsupportedCyclesAndLimits(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*emojiColorFont, *emojiSyntheticFont)
		want error
	}{
		{"unsupported", func(_ *emojiColorFont, f *emojiSyntheticFont) { f.paints[1] = tables.PaintSweepGradient{} }, errEmojiColorUnsupported},
		{"composite", func(_ *emojiColorFont, f *emojiSyntheticFont) {
			f.paints[1] = tables.PaintComposite{CompositeMode: tables.CompositeMultiply}
		}, errEmojiColorUnsupported},
		{"cycle", func(_ *emojiColorFont, f *emojiSyntheticFont) { f.paints[1] = tables.PaintColrGlyph{GlyphID: 1} }, errEmojiColorInvalid},
		{"palette", func(_ *emojiColorFont, f *emojiSyntheticFont) { f.paints[1] = emojiOutlinePaint(emojiSolidPaint(99)) }, errEmojiColorInvalid},
		{"unbounded", func(_ *emojiColorFont, f *emojiSyntheticFont) { f.paints[1] = emojiSolidPaint(0) }, errEmojiColorUnsupported},
		{"depth", func(_ *emojiColorFont, f *emojiSyntheticFont) {
			var p tables.PaintTable = emojiOutlinePaint(emojiSolidPaint(0))
			for range emojiPaintDepthLimit + 1 {
				p = tables.PaintTransform{Paint: p, Transform: tables.Affine2x3{Xx: 1, Yy: 1}}
			}
			f.paints[1] = p
		}, errEmojiColorLimit},
		{"segments", func(_ *emojiColorFont, f *emojiSyntheticFont) {
			f.outlines[2] = font.GlyphOutline{Segments: make([]font.Segment, emojiSegmentLimit+1)}
		}, errEmojiColorLimit},
		{"clip-variation", func(_ *emojiColorFont, f *emojiSyntheticFont) { f.clips[1] = tables.ClipBoxFormat2{} }, errEmojiColorUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner, f := syntheticEmojiFont(emojiOutlinePaint(emojiSolidPaint(0)))
			test.edit(owner, f)
			if _, err := rasterEmojiColorGlyphs(emojiTestGlyph(owner), ui.RGB(255, 255, 255)); !errors.Is(err, test.want) {
				t.Fatalf("got %v want %v", err, test.want)
			}
		})
	}
	owner, _ := syntheticEmojiFont(emojiOutlinePaint(emojiSolidPaint(0)))
	for _, glyph := range []referenceColorGlyph{{Font: owner, ID: 1, Size: 600}, {Font: owner, ID: 1, Size: float32(math.NaN())}, {Font: owner, ID: 99, Size: 20}, {Font: owner, ID: 1, Size: 20, X: float32(math.Inf(1))}} {
		if _, err := rasterEmojiColorGlyphs([]referenceColorGlyph{glyph}, ui.RGB(255, 255, 255)); !errors.Is(err, errEmojiColorLimit) {
			t.Errorf("bad glyph admitted: %v", err)
		}
	}
	tooMany := make([]referenceColorGlyph, emojiGlyphLimit+1)
	if _, err := rasterEmojiColorGlyphs(tooMany, ui.RGB(255, 255, 255)); !errors.Is(err, errEmojiColorLimit) {
		t.Fatal("glyph count budget was bypassed")
	}
	budget := emojiRasterBudget{}
	if budget.reserve(emojiWorkingLimit) != nil || !errors.Is(budget.reserve(1), errEmojiColorLimit) || budget.work(emojiWorkPixelLimit) != nil || !errors.Is(budget.work(1), errEmojiColorLimit) {
		t.Fatal("working byte or sampled-pixel budget was bypassed")
	}
}

func TestEmojiColorGradientExtendAndFocalCircles(t *testing.T) {
	stops := []emojiStop{{.25, emojiRGBA{r: 1, a: 1}}, {.75, emojiRGBA{b: 1, a: 1}}}
	for _, test := range []struct {
		extend tables.Extend
		at     float64
		want   emojiRGBA
	}{{tables.ExtendPad, -1, stops[0].color}, {tables.ExtendPad, 2, stops[1].color}, {tables.ExtendRepeat, 1, emojiRGBA{r: .5, b: .5, a: 1}}, {tables.ExtendReflect, 1, emojiRGBA{r: .5, b: .5, a: 1}}} {
		got := emojiGradientColor(stops, test.extend, test.at)
		if math.Abs(got.r-test.want.r) > 1e-9 || math.Abs(got.b-test.want.b) > 1e-9 || got.a != 1 {
			t.Fatalf("extend=%d at=%g got=%+v want=%+v", test.extend, test.at, got, test.want)
		}
	}
	// A moving focal circle requires the full two-circle equation, rather than
	// a concentric CSS radial approximation. At t=1 the point (22,10) lies on
	// the ending circle (12,10,r10).
	node := emojiPaintNode{kind: emojiRadial, params: [6]float64{10, 10, 2, 12, 10, 10}}
	if got, valid := emojiGradientPosition(&node, 22, 10); !valid || math.Abs(got-1) > 1e-9 {
		t.Fatalf("moving focal circle position=%g valid=%v", got, valid)
	}
}

func TestEmojiColorGradientRetainsTransparentStopRGB(t *testing.T) {
	// A COLR alpha-zero stop still has a real palette color. Interpolate
	// straight RGBA before premultiplying; dropping that RGB caused a visible
	// blue/pink error in the actual Segoe UI Emoji grinning-face cheek.
	line := tables.ColorLine{ColorStops: []tables.ColorStop{{PaletteIndex: 0, Alpha: 0}, {StopOffset: 16384, PaletteIndex: 1, Alpha: 16384}}}
	owner, _ := syntheticEmojiFont(emojiOutlinePaint(tables.PaintLinearGradient{ColorLine: line, X1: 20, Y2: 20}))
	raster, err := rasterEmojiColorGlyphs(emojiTestGlyph(owner), ui.RGB(255, 255, 255))
	if err != nil {
		t.Fatal(err)
	}
	pixel := raster.Pixels.RGBAAt(10, -10) // t=.525: straight (.475,0,.525,.525)
	if abs8(pixel.R, 64) > 1 || abs8(pixel.B, 70) > 1 || abs8(pixel.A, 134) > 1 {
		t.Fatalf("transparent stop lost RGB before interpolation: %v", pixel)
	}
}

func TestEmojiColorSourceAndDirectoryAdmission(t *testing.T) {
	file := filepath.Join(t.TempDir(), "oversized.ttf")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(emojiFontSourceLimit + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := openEmojiColorFont(file); !errors.Is(err, errEmojiColorLimit) {
		t.Fatalf("oversized font was read: %v", err)
	}
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data[:4], 0x00010000)
	binary.BigEndian.PutUint16(data[4:6], emojiFontTableLimit+1)
	if !errors.Is(preflightEmojiFont(data), errEmojiColorLimit) {
		t.Fatal("unbounded SFNT table count admitted")
	}
}
