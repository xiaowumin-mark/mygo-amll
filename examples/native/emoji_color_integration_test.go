// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
)

func explicitEmojiFont(t *testing.T) (*emojiColorFont, *referenceFontProvider) {
	t.Helper()
	filename := os.Getenv("MYGO_AMLL_TEST_EMOJI_FONT")
	if filename == "" {
		t.Skip("set MYGO_AMLL_TEST_EMOJI_FONT to an authorized installed COLR font; no font asset is embedded")
	}
	owner, err := openEmojiColorFont(filename)
	if err != nil {
		t.Fatal(err)
	}
	provider := newReferenceFontProvider(owner)
	t.Cleanup(func() { provider.Close(); owner.Close() })
	return owner, provider
}

func TestEmojiColorProviderSequenceCoverage(t *testing.T) {
	_, provider := explicitEmojiFont(t)
	fontStyle := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600, LineHeight: 1.5}
	for _, text := range []string{"1️⃣", "🇨🇳", "👍🏽", "👩🏽‍🚀", "😀\u200d", "😀\ufe0f\u2060"} {
		t.Run(text, func(t *testing.T) {
			shape := provider.Shape(text, fontStyle)
			if shape.Err != nil || !shape.Exact || len(shape.Colors) == 0 || shape.Width <= 0 {
				t.Fatalf("sequence %q exact=%v colors=%d width=%g: %v", text, shape.Exact, len(shape.Colors), shape.Width, shape.Err)
			}
			raster, err := rasterEmojiColorGlyphs(shape.Colors, ui.RGB(255, 255, 255))
			if err != nil || raster.Pixels == nil || effectInk(raster.Pixels, raster.Pixels.Rect).Empty() {
				t.Fatalf("sequence %q lost its color paint or valid empty default-ignorable glyph: %v", text, err)
			}
			t.Logf("sequence=%q colors=%d width=%.6f bounds=%v", text, len(shape.Colors), shape.Width, raster.Pixels.Rect)
		})
	}
}

// This opt-in diagnostic separates a union of all actual paint-contour edges
// from pixels at least two physical pixels away. It logs errors rather than
// declaring a new visual tolerance or silently treating every mismatch as AA.
func TestEmojiColorDOMInteriorDiagnostic(t *testing.T) {
	domFilename := os.Getenv("MYGO_AMLL_COLOR_DOM_PNG")
	if domFilename == "" {
		t.Skip("set MYGO_AMLL_COLOR_DOM_PNG to a separately captured 960x144 DOM sprite")
	}
	file, err := os.Open(domFilename)
	if err != nil {
		t.Fatal(err)
	}
	dom, err := png.Decode(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, provider := explicitEmojiFont(t)
	fontStyle := ui.Font{Family: "Segoe UI Emoji", Size: 80, Weight: 400, LineHeight: 1.8}
	const family = "👩‍👩‍👧‍👦"
	for i, text := range []string{"😀", family, "ע\u200b" + family, "ע\u200b" + family + "\u200bע"} {
		shape := provider.Shape(text, fontStyle)
		if shape.Err != nil {
			t.Fatal(shape.Err)
		}
		glyphs := append([]referenceColorGlyph(nil), shape.Colors...)
		if i >= 2 {
			prefix := provider.Shape("ע\u200b", fontStyle)
			for j := range glyphs {
				glyphs[j].X -= prefix.Width
			}
		}
		foreground := ui.RGB(255, 255, 255)
		if i >= 2 {
			foreground = ui.RGB(0, 0, 0)
		}
		raster, err := rasterEmojiColorGlyphs(glyphs, foreground)
		if err != nil {
			t.Fatal(err)
		}
		program, err := compileEmojiPaint(glyphs, emojiColor(foreground))
		if err != nil {
			t.Fatal(err)
		}
		boundary := image.NewAlpha(raster.Pixels.Rect)
		worker := emojiRasterizer{dst: image.NewRGBA(raster.Pixels.Rect), budget: &emojiRasterBudget{}}
		var mark func(*emojiPaintNode)
		mark = func(node *emojiPaintNode) {
			if node.kind == emojiClip {
				mask, bytes, err := worker.mask(node, nil)
				if err != nil {
					t.Fatal(err)
				}
				for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
					for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
						a := mask.AlphaAt(x, y).A
						edge := a > 0 && a < 255
						for _, d := range [4]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
							edge = edge || mask.AlphaAt(x+d.X, y+d.Y).A != a
						}
						if edge {
							for dy := -2; dy <= 2; dy++ {
								for dx := -2; dx <= 2; dx++ {
									boundary.SetAlpha(x+dx, y+dy, color.Alpha{A: 255})
								}
							}
						}
					}
				}
				worker.budget.release(bytes)
			}
			for _, child := range node.children {
				mark(child)
			}
		}
		for _, root := range program.roots {
			mark(root)
		}
		var edges, interiors []int
		reported := 0
		for y := raster.Pixels.Rect.Min.Y; y < raster.Pixels.Rect.Max.Y; y++ {
			for x := raster.Pixels.Rect.Min.X; x < raster.Pixels.Rect.Max.X; x++ {
				a := raster.Pixels.RGBAAt(x, y)
				br, bg, bb, _ := dom.At(16+i*240+x, 110+y).RGBA()
				if max(a.R, a.G, a.B, uint8(br>>8), uint8(bg>>8), uint8(bb>>8)) < 8 {
					continue
				}
				if boundary.AlphaAt(x, y).A == 0 && max(abs8(a.R, uint8(br>>8)), abs8(a.G, uint8(bg>>8)), abs8(a.B, uint8(bb>>8))) > 8 && reported < 8 {
					t.Logf("far mismatch sprite=%d relative=(%d,%d) src=%v DOM=(%d,%d,%d)", i, x, y, a, br>>8, bg>>8, bb>>8)
					reported++
				}
				for _, pair := range [3][2]uint8{{a.R, uint8(br >> 8)}, {a.G, uint8(bg >> 8)}, {a.B, uint8(bb >> 8)}} {
					d := abs8(pair[0], pair[1])
					if boundary.AlphaAt(x, y).A != 0 {
						edges = append(edges, d)
					} else {
						interiors = append(interiors, d)
					}
				}
			}
		}
		for _, part := range []struct {
			name   string
			values []int
		}{{"all-contour-edge-within-2px", edges}, {"far-from-every-contour", interiors}} {
			sort.Ints(part.values)
			if len(part.values) == 0 {
				t.Logf("sprite=%d %s no samples", i, part.name)
				continue
			}
			sum := 0
			for _, d := range part.values {
				sum += d
			}
			t.Logf("sprite=%d %s pixels=%d meanRGB=%.4f p95=%d p99=%d max=%d", i, part.name, len(part.values)/3, float64(sum)/float64(len(part.values)), part.values[int(math.Floor(float64(len(part.values)-1)*.95))], part.values[int(math.Floor(float64(len(part.values)-1)*.99))], part.values[len(part.values)-1])
		}
	}
}

func TestEmojiColorProviderSprites(t *testing.T) {
	owner, provider := explicitEmojiFont(t)
	if owner.Stats().Entries != 1 || owner.Stats().EstimatedRetainedBytes > emojiFontRetainedLimit {
		t.Fatal("font owner exceeded its independent cache admission")
	}
	const family = "👩‍👩‍👧‍👦"
	fontStyle := ui.Font{Family: "Segoe UI Emoji", Size: 80, Weight: 400, LineHeight: 1.8}
	canvas := image.NewRGBA(image.Rect(0, 0, 960, 144))
	for i, text := range []string{"😀", family, "ע\u200b" + family, "ע\u200b" + family + "\u200bע"} {
		shape := provider.Shape(text, fontStyle)
		if shape.Err != nil || !shape.Exact || len(shape.Colors) == 0 {
			t.Fatalf("provider sprite %d exact=%v error=%v", i, shape.Exact, shape.Err)
		}
		glyphs := append([]referenceColorGlyph(nil), shape.Colors...)
		if i >= 2 {
			prefix := provider.Shape("ע\u200b", fontStyle)
			if prefix.Err != nil {
				t.Fatal(prefix.Err)
			}
			for j := range glyphs {
				glyphs[j].X -= prefix.Width
			}
		}
		foreground := ui.RGB(255, 255, 255)
		if i >= 2 {
			foreground = ui.RGB(0, 0, 0)
		}
		raster, err := rasterEmojiColorGlyphs(glyphs, foreground)
		if err != nil || raster.Pixels == nil {
			t.Fatalf("provider sprite %d: %v", i, err)
		}
		x, y := 16+240*i+raster.Origin.X, 110+raster.Origin.Y
		box := image.Rect(x, y, x+raster.Pixels.Rect.Dx(), y+raster.Pixels.Rect.Dy())
		draw.Draw(canvas, box, raster.Pixels, raster.Pixels.Rect.Min, draw.Over)
	}
	if directory := os.Getenv("MYGO_AMLL_COLOR_CAPTURE"); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		filename := filepath.Join(directory, "native-public-colrv1-provider-sprites.png")
		file, err := os.Create(filename)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, canvas)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("PNG output: %v %v", err, closeErr)
		}
		t.Logf("CPU diagnostic sprites at %s; compare the separately captured DOM image, not a GPU claim", filename)
	}
}

func TestEmojiColorInstalledPaintCoverage(t *testing.T) {
	owner, _ := explicitEmojiFont(t)
	seen := make(map[string]int)
	var walk func(tables.PaintTable, int) error
	walk = func(raw tables.PaintTable, depth int) error {
		if depth > emojiPaintDepthLimit {
			return errEmojiColorLimit
		}
		switch paint := raw.(type) {
		case tables.PaintColrLayers:
			seen["layers"]++
			children, err := owner.faceLayers(paint)
			if err != nil {
				return err
			}
			for _, child := range children {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case tables.PaintGlyph:
			seen["glyph"]++
			return walk(paint.Paint, depth+1)
		case tables.PaintTransform:
			seen["transform"]++
			return walk(paint.Paint, depth+1)
		case tables.PaintTranslate:
			seen["translate"]++
			return walk(paint.Paint, depth+1)
		case tables.PaintScale:
			seen["scale"]++
			return walk(paint.Paint, depth+1)
		case tables.PaintScaleAroundCenter:
			seen["scale-center"]++
			return walk(paint.Paint, depth+1)
		case tables.PaintComposite:
			seen["composite"]++
			if paint.CompositeMode != tables.CompositeSrcIn && paint.CompositeMode != tables.CompositeSrcOver {
				return errEmojiColorUnsupported
			}
			if err := walk(paint.SourcePaint, depth+1); err != nil {
				return err
			}
			return walk(paint.BackdropPaint, depth+1)
		case tables.PaintSolid:
			seen["solid"]++
		case tables.PaintLinearGradient:
			seen["linear"]++
		case tables.PaintRadialGradient:
			seen["radial"]++
		default:
			return errEmojiColorUnsupported
		}
		return nil
	}
	roots := 0
	for id := uint32(0); id < owner.glyphCount; id++ {
		paint, found := owner.source.color(font.GID(id))
		if !found {
			continue
		}
		roots++
		if err := walk(paint, 0); err != nil {
			t.Fatalf("installed base glyph %d has unsupported/over-budget paint graph: %v", id, err)
		}
		if _, err := compileEmojiPaint([]referenceColorGlyph{{Font: owner, ID: font.GID(id), Size: 20}}, emojiColor(ui.RGB(255, 255, 255))); err != nil {
			t.Fatalf("installed base glyph %d failed bounded compilation: %v", id, err)
		}
	}
	t.Logf("checked all %d color base glyphs through public font data: %v", roots, seen)
}
