package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestEffectsShadersCompile(t *testing.T) {
	if err := compileEffectPrograms(); err != nil {
		t.Fatal(err)
	}
}

func effectTestPixel(t *testing.T, white, black color.RGBA, foreground ui.Color, bright, dark float32) color.RGBA {
	t.Helper()
	if err := compileEffectPrograms(); err != nil {
		t.Fatal(err)
	}
	w, b := image.NewRGBA(image.Rect(0, 0, 1, 1)), image.NewRGBA(image.Rect(0, 0, 1, 1))
	w.SetRGBA(0, 0, white)
	b.SetRGBA(0, 0, black)
	v, u := effectPrograms.word.NewValues(), effectPrograms.wordUniforms
	v.Float(u["RasterScale"], 1).Float(u["Padding"], 0).Vec2(u["WordSize"], 1, 1).Vec4(u["Mask"], 0, 1, bright, dark).Color(u["Foreground"], foreground).Vec4(u["UnderRect"], 0, 0, 1, 1).Int(u["Count"], 0)
	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
			effectPrograms.word.Draw(p, ui.Rect{W: 1, H: 1}, shader.Options{Images: [4]*ui.Bitmap{ui.NewBitmap(w), ui.NewBitmap(b)}, Values: v})
		})
	}, 1, 1, 1)
	return img.RGBAAt(0, 0)
}

func TestEffectsGradientAlphaAndColorEmoji(t *testing.T) {
	// Pixel-center x=.5 must be genuinely half transparent, rather than a
	// different opaque gray. CPU tolerance is one final 8-bit rounding step.
	got := effectTestPixel(t, color.RGBA{255, 255, 255, 255}, color.RGBA{0, 0, 0, 255}, ui.RGB(255, 255, 255), 1, 0)
	if abs8(got.A, 128) > 1 || abs8(got.R, 128) > 1 {
		t.Fatalf("gradient pixel=%v", got)
	}
	// Colored glyphs have the same raster in white/black foreground renders.
	// They retain their own RGB while receiving the identical mask alpha.
	emoji := color.RGBA{240, 80, 20, 255}
	got = effectTestPixel(t, emoji, emoji, ui.RGB(0, 0, 255), .5, .5)
	if abs8(got.R, 120) > 1 || abs8(got.G, 40) > 1 || abs8(got.B, 10) > 1 || abs8(got.A, 128) > 1 {
		t.Fatalf("colored glyph mask/tint=%v", got)
	}
	got = effectTestPixel(t, color.RGBA{255, 255, 255, 255}, color.RGBA{0, 0, 0, 255}, ui.RGB(20, 100, 240), 1, 1)
	if abs8(got.R, 20) > 1 || abs8(got.G, 100) > 1 || abs8(got.B, 240) > 1 || got.A != 255 {
		t.Fatalf("foreground=%v", got)
	}
	// The reference keeps emoji RGB, while the inherited text color's alpha
	// still scales the entire color glyph (Chrome's Segoe UI Emoji probe).
	got = effectTestPixel(t, emoji, emoji, ui.Color{G: 255, A: 128}, 1, 1)
	if abs8(got.R, 120) > 1 || abs8(got.G, 40) > 1 || abs8(got.B, 10) > 1 || abs8(got.A, 128) > 1 {
		t.Fatalf("colored glyph foreground alpha=%v", got)
	}
	got = effectTestPixel(t, emoji, emoji, ui.Color{}, 1, 1)
	if got != (color.RGBA{}) {
		t.Fatalf("explicit transparent foreground retained glyph pixels: %v", got)
	}
}

func abs8(a, b uint8) int { return int(math.Abs(float64(a) - float64(b))) }

func TestEffectsGaussianUsesAlphaAndTransparentEdges(t *testing.T) {
	if err := compileEffectPrograms(); err != nil {
		t.Fatal(err)
	}
	impulse := image.NewRGBA(image.Rect(0, 0, 3, 1))
	impulse.SetRGBA(1, 0, color.RGBA{255, 0, 0, 255})
	bitmap := ui.NewBitmap(impulse)
	values := effectPrograms.gaussian.NewValues()
	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
			effectGaussian(p, ui.Rect{W: 5, H: 1}, bitmap, ui.Rect{W: 3, H: 1}, ui.Point{X: -1}, ui.Point{X: 1}, .5, 1, 1, true, values)
		})
	}, 5, 1, 1)
	denom := float64(0)
	for i := -2; i <= 2; i++ {
		denom += math.Exp(-float64(i*i) / .5)
	}
	for x := 0; x < 5; x++ {
		want := uint8(math.Round(255 * math.Exp(-float64((x-2)*(x-2))/.5) / denom))
		got := img.RGBAAt(x, 0)
		if abs8(got.A, want) > 1 || got.R != got.G || got.G != got.B {
			t.Fatalf("x=%d got=%v want alpha=%d", x, got, want)
		}
	}
}

func TestEffectsMaskReferenceVectors(t *testing.T) {
	g := &lineGeometry{start: 0, end: 4 * time.Second, blocks: []wordBlock{{start: 0, end: time.Second, width: 40, padding: 10, height: 20}, {start: 2 * time.Second, end: 3 * time.Second, width: 60, padding: 10, height: 20}}}
	path := newEffectMaskPath(g)
	for _, test := range []struct {
		at            time.Duration
		first, second float32
	}{{0, -70, -90}, {time.Second, -15, -75}, {2 * time.Second, -15, -75}, {2500 * time.Millisecond, 0, -42.5}, {3 * time.Second, 0, -10}, {3500 * time.Millisecond, 0, -5}, {4 * time.Second, 0, 0}} {
		for i, want := range []float32{test.first, test.second} {
			block := g.blocks[i]
			got := path.position(i, block.width, block.padding, 10, test.at)
			if math.Abs(float64(got-want)) > 1e-5 {
				t.Errorf("t=%v word=%d got=%g want=%g", test.at, i, got, want)
			}
		}
	}
	// Source overlaps accumulate full durations, then clamp frame offsets to
	// one; they do not rewind the cursor to the overlapping word's timestamp.
	g.end = 3 * time.Second
	g.blocks[0].end = 2 * time.Second
	g.blocks[1].start = time.Second
	path = newEffectMaskPath(g)
	if got := path.position(1, 60, 10, 10, 2500*time.Millisecond); math.Abs(float64(got+42.5)) > 1e-5 {
		t.Fatalf("overlap path=%g", got)
	}
}

func TestEffectsRubyMaskUsesUTF16AndZeroDurationCursor(t *testing.T) {
	g := &lineGeometry{end: 4 * time.Second, blocks: []wordBlock{{width: 100, padding: 10, height: 20, end: 4 * time.Second, ruby: []lyric.RubyWord{{Text: "A😀", Start: 0, End: 3 * time.Second}, {Text: "B", Start: 3 * time.Second, End: 4 * time.Second}}}}}
	path := newEffectMaskPath(g)
	for i, want := range []float32{-130, -90, -65, -40, -10} {
		if got := path.position(0, 100, 10, 10, time.Duration(i)*time.Second); math.Abs(float64(got-want)) > 1e-5 {
			t.Errorf("ruby t=%d got=%g want=%g", i, got, want)
		}
	}
	g.end = time.Second
	g.blocks = []wordBlock{{width: 100, padding: 10, height: 20}, {width: 20, padding: 10, height: 20, end: time.Second}}
	path = newEffectMaskPath(g)
	if got := path.position(1, 20, 10, 10, 500*time.Millisecond); got != -50 {
		t.Fatalf("zero-duration motion was applied eagerly: %g", got)
	}
}

func TestEffectsCachePinsInvalidationAndBudget(t *testing.T) {
	var renderer effectRenderer
	geometry := &lineGeometry{font: ui.Font{Size: 10}, width: 1, height: 1, end: time.Second}
	if err := renderer.Prepare(geometry, 1); err != nil {
		t.Fatal(err)
	}
	first, bytes := renderer.entries[0], renderer.bytes
	if err := renderer.Prepare(geometry, 1); err != nil || renderer.entries[0] != first || renderer.bytes != bytes {
		t.Fatal("warm preparation replaced the entry")
	}
	for i := 1; i < effectEntryLimit; i++ {
		g := *geometry
		if err := renderer.Prepare(&g, 1); err != nil {
			t.Fatal(err)
		}
	}
	other := *geometry
	if err := renderer.Prepare(&other, 1); !errors.Is(err, errEffectLimit) {
		t.Fatalf("pinned entries exceeded limit: %v", err)
	}
	if len(renderer.entries) != effectEntryLimit || renderer.bytes > effectByteLimit {
		t.Fatal("cache exceeded budget")
	}
	renderer.BeginFrame()
	if err := renderer.Prepare(&other, 1); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Prepare(&other, 2); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []float32{0, -1, float32(math.Inf(1)), float32(math.NaN()), effectScaleLimit + 1} {
		if err := renderer.Prepare(&other, bad); !errors.Is(err, errEffectLimit) {
			t.Errorf("scale %g: %v", bad, err)
		}
	}
	wide := *geometry
	wide.width = 10000
	if err := renderer.Prepare(&wide, 1); !errors.Is(err, errEffectLimit) {
		t.Fatalf("oversize accepted: %v", err)
	}
	renderer.Clear()
	if renderer.bytes != 0 || len(renderer.entries) != 0 {
		t.Fatal("Clear retained resources")
	}
}

func TestEffectsRootPlusLighterUsesBackdropOnce(t *testing.T) {
	fx, err := RootEffect()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ background, source ui.Color }{{ui.RGB(20, 30, 40), ui.RGBA(255, 255, 255, .2)}, {ui.RGBA(20, 30, 40, .5), ui.RGBA(200, 120, 60, .25)}} {
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Background(test.background).Children(func() {
				ui.Box(c).Fill().Effect(fx).Draw(func(p *ui.Painter, box ui.Rect) { p.Fill(box, test.source, 0) })
			})
		}, 8, 8, 1)
		b, s := test.background, test.source
		want := color.RGBA{uint8(min(255, (int(b.R)*int(b.A)+127)/255+(int(s.R)*int(s.A)+127)/255)), uint8(min(255, (int(b.G)*int(b.A)+127)/255+(int(s.G)*int(s.A)+127)/255)), uint8(min(255, (int(b.B)*int(b.A)+127)/255+(int(s.B)*int(s.A)+127)/255)), uint8(min(255, int(b.A)+int(s.A)))}
		got := img.RGBAAt(4, 4)
		if abs8(got.R, want.R) > 2 || abs8(got.G, want.G) > 2 || abs8(got.B, want.B) > 2 || abs8(got.A, want.A) > 2 {
			t.Fatalf("plus-lighter=%v want=%v background=%v source=%v", got, want, b, s)
		}
	}
}

func TestEffectsActualGlyphRasterAndWholeLineAlpha(t *testing.T) {
	registerSnapshotFonts(t)
	doc, err := lyric.NewDocument([]lyric.Line{{Text: "AB", Start: 0, End: time.Second, Words: []lyric.Word{{Text: "AB", Start: 0, End: 200 * time.Millisecond}}}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := buildLineGeometryWithStyle(doc.Line(0), ui.Font{Family: snapshotRegularFamily, Size: 12, LineHeight: 1.2}, 80, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	var r effectRenderer
	if err = r.Prepare(g, 1); err != nil {
		t.Fatal(err)
	}
	entry := r.entries[0]
	if entry.whitePixels != nil || entry.blackPixels != nil {
		t.Fatal("preparation source images were retained in addition to converted bitmaps")
	}
	render := func(opacity float32) *image.RGBA {
		var drawErr error
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				drawErr = r.DrawLine(p, g, 4, 16, 0, effectStyle{Opacity: opacity, BrightOpacity: 1, BrightMaskAlpha: 1, DarkMaskAlpha: 1, AllowSoftware: true})
			})
		}, 80, 64, 1)
		if drawErr != nil {
			t.Fatal(drawErr)
		}
		return img
	}
	full, half := render(1), render(.5)
	visible, colored, transparent := 0, 0, 0
	for y := 0; y < 64; y++ {
		for x := 0; x < 80; x++ {
			a, b := full.RGBAAt(x, y), half.RGBAAt(x, y)
			if a.A == 0 {
				transparent++
			}
			if int(max(a.R, a.G, a.B))-int(min(a.R, a.G, a.B)) > 20 {
				colored++
			}
			if a.A > 8 {
				visible++
				if abs8(b.A, uint8(math.Round(float64(a.A)*.5))) > 2 {
					t.Fatalf("group alpha at %d,%d: full=%v half=%v", x, y, a, b)
				}
			}
		}
	}
	if visible == 0 || transparent == 0 {
		t.Fatal("native shader produced no glyph pixels or lost transparency")
	}
	t.Logf("CPU glyph pixels=%d colored pixels=%d; COLRv1 has separate explicit-provider tests; GPU not measured", visible, colored)
}

func TestEffectsSixVisibleLinesHighDPIResources(t *testing.T) {
	registerSnapshotFonts(t)
	var lines []lyric.Line
	for i := 0; i < 6; i++ {
		start := time.Duration(i) * 5 * time.Second
		parts := []string{"A ", "bright ", "synthetic ", "line ", "for ", "native ", "motion"}
		line := lyric.Line{Text: "A bright synthetic line for native motion", Translation: "A separate synthetic translation", Start: start, End: start + 4*time.Second}
		for j, text := range parts {
			a := start + time.Duration(j)*300*time.Millisecond
			b := a + 300*time.Millisecond
			if j == len(parts)-1 {
				b = start + 3500*time.Millisecond
			}
			line.Words = append(line.Words, lyric.Word{Text: text, Start: a, End: b})
		}
		lines = append(lines, line)
	}
	doc, err := lyric.NewDocument(lines)
	if err != nil {
		t.Fatal(err)
	}
	var r effectRenderer
	for i := range doc.Len() {
		g, err := buildLineGeometryWithStyle(doc.Line(i), ui.Font{Family: snapshotRegularFamily, Size: 48, LineHeight: 1.2}, 912, geometryStyle{Dynamic: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = r.Prepare(g, 2); err != nil {
			t.Fatalf("visible line %d: %v; retained=%d bytes", i, err, r.bytes)
		}
		e := r.entries[len(r.entries)-1]
		t.Logf("line %d: logical=%gx%g canvas=%dx%d atlas=%dx%d estimated=%dB", i, g.width, g.height, e.width, e.height, func() int { w, _ := e.white.Size(); return w }(), func() int { _, h := e.white.Size(); return h }(), e.bytes)
	}
	if r.bytes > effectByteLimit {
		t.Fatal("six visible lines exceeded shared budget")
	}
	t.Logf("six 48px lines/912DIP width/2x: %d bytes incl CPU atlas+estimated GPU textures; excludes upstream font caches/driver/RSS", r.bytes)
}

func TestEffectsGroupOpacityAfterOverlappingChildren(t *testing.T) {
	registerSnapshotFonts(t)
	doc, err := lyric.NewDocument([]lyric.Line{{Text: "A", Start: 0, End: time.Second}, {Text: "A", Start: 0, End: time.Second, Background: true}})
	if err != nil {
		t.Fatal(err)
	}
	var layers []effectGroupLayer
	for i := 0; i < 2; i++ {
		g, err := buildLineGeometryWithStyle(doc.Line(i), ui.Font{Family: snapshotRegularFamily, Size: 12, LineHeight: 1.2}, 40, geometryStyle{})
		if err != nil {
			t.Fatal(err)
		}
		layers = append(layers, effectGroupLayer{Geometry: g, Scale: 1, Style: effectStyle{Opacity: 1, BrightOpacity: 1, BrightMaskAlpha: 1, DarkMaskAlpha: 1, AllowSoftware: true}})
	}
	var r effectRenderer
	if err := r.PrepareGroup(layers[0].Geometry, layers, 1, ui.Rect{W: 40, H: 20}); err != nil {
		t.Fatal(err)
	}
	for _, layer := range layers {
		if err := r.SyncFrame(layer.Geometry, time.Unix(0, 0), 0, layer.Style); err != nil {
			t.Fatal(err)
		}
	}
	render := func(opacity float32) *image.RGBA {
		var drawErr error
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				drawErr = r.DrawGroup(p, layers[0].Geometry, 4, 16, layers, opacity, 0)
			})
		}, 64, 64, 1)
		if drawErr != nil {
			t.Fatal(drawErr)
		}
		return img
	}
	full, half := render(1), render(.5)
	visible := 0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			a, b := full.RGBAAt(x, y), half.RGBAAt(x, y)
			if a.A > 128 {
				visible++
				if abs8(b.A, uint8(math.Round(float64(a.A)*.5))) > 2 {
					t.Fatalf("overlap group alpha applied per child at %d,%d: %v -> %v", x, y, a, b)
				}
			}
		}
	}
	if visible == 0 {
		t.Fatal("overlapping group produced no opaque glyph coverage")
	}
}

func TestEffectsAutoSizeSixGroupsIncludeRootReserve(t *testing.T) {
	registerSnapshotFonts(t)
	var r effectRenderer
	if err := r.ReserveRoot(960, 740, 2); err != nil {
		t.Fatal(err)
	}
	root := r.rootBytes
	for i := 0; i < 6; i++ {
		start := time.Duration(i) * 5 * time.Second
		doc, err := lyric.NewDocument([]lyric.Line{{Text: "A bright synthetic line for native motion", Translation: "A separate synthetic translation", Start: start, End: start + 4*time.Second, Words: []lyric.Word{{Text: "A bright synthetic line for native ", Start: start, End: start + 2*time.Second}, {Text: "motion", Start: start + 2*time.Second, End: start + 3500*time.Millisecond}}}})
		if err != nil {
			t.Fatal(err)
		}
		g, err := buildLineGeometryWithStyle(doc.Line(0), ui.Font{Family: snapshotRegularFamily, Size: 37, LineHeight: 1.2}, 912, geometryStyle{Dynamic: true})
		if err != nil {
			t.Fatal(err)
		}
		layer := effectGroupLayer{Geometry: g, Scale: .97, Style: effectStyle{Opacity: 1, BrightOpacity: 1, BrightMaskAlpha: 1, DarkMaskAlpha: .4}}
		if err := r.PrepareGroup(g, []effectGroupLayer{layer}, 2, ui.Rect{W: 912, H: g.height}); err != nil {
			t.Fatalf("group %d: %v retained=%d root=%d", i, err, r.bytes, root)
		}
	}
	if r.bytes > effectByteLimit {
		t.Fatal("root+visible groups exceeded budget")
	}
	t.Logf("960x740/2x, six font37 singleton groups incl root content+backdrop: total=%dB root=%dB lines=%dB", r.bytes, r.rootBytes, r.bytes-r.rootBytes)
}

func TestEffectsGaussianFinalPassMatchesIntermediate(t *testing.T) {
	if err := compileEffectPrograms(); err != nil {
		t.Fatal(err)
	}
	for _, scale := range []float32{1, 2} {
		w, h := int(32*scale), int(28*scale)
		pixels := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := h / 3; y < h*2/3; y++ {
			for x := w / 3; x < w*2/3; x++ {
				a := uint8(100 + (x+y)%156)
				pixels.SetRGBA(x, y, color.RGBA{a, a / 3, a / 2, a})
			}
		}
		source := ui.NewBitmap(pixels)
		render := func(intermediate bool) *image.RGBA {
			x, y := ui.NewCanvas(w, h), ui.NewCanvas(w, h)
			v := effectPrograms.gaussian.NewValues()
			return ui.Render(func(c *ui.Context) {
				c.Root().Background(ui.Transparent)
				ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
					rect := ui.Rect{W: float32(w), H: float32(h)}
					x.Draw(p, func(cp *ui.Painter) {
						effectGaussian(cp, rect, source, rect, ui.Point{}, ui.Point{X: 1}, 1.5*scale, 1, 1, false, v)
					})
					box := ui.Rect{X: .35, Y: .6, W: 32, H: 28}
					if intermediate {
						y.Draw(p, func(cp *ui.Painter) {
							effectGaussian(cp, rect, x.Bitmap(), rect, ui.Point{}, ui.Point{Y: 1}, 1.5*scale, 1, 1, false, v)
						})
						effectGaussian(p, box, y.Bitmap(), rect, ui.Point{}, ui.Point{}, 0, scale, .65, false, v)
					} else {
						effectGaussian(p, box, x.Bitmap(), rect, ui.Point{}, ui.Point{Y: 1}, 1.5*scale, scale, .65, false, v)
					}
				})
			}, 36, 32, scale)
		}
		before, after := render(true), render(false)
		visible := 0
		for i, b := range before.Pix {
			if b > 0 {
				visible++
			}
			// Removing the intermediate also removes one 8-bit rounding step.
			if abs8(b, after.Pix[i]) > 2 {
				t.Fatalf("scale=%g byte=%d staged=%d direct=%d", scale, i, b, after.Pix[i])
			}
		}
		if visible == 0 {
			t.Fatal("Gaussian comparison produced no content")
		}
	}
}

func TestEffectsDimClonePreservesCharacterBaselineOffset(t *testing.T) {
	registerSnapshotFonts(t)
	doc, err := lyric.NewDocument([]lyric.Line{{Text: "AB", End: 4 * time.Second, Words: []lyric.Word{{Text: "AB", End: 4 * time.Second}}}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := buildLineGeometryWithStyle(doc.Line(0), ui.Font{Family: snapshotRegularFamily, Size: 16, LineHeight: 1.2}, 40, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.blocks) != 1 || len(g.blocks[0].characters) != 2 {
		t.Fatal("fixture did not retain the emphasized grapheme box")
	}
	// Keep the grapheme boxes while removing animation, and give the glyph a
	// nonzero CSS line-box/baseline correction shared by bright and dim clones.
	g.blocks[0].shouldEmphasize = false
	for i := range g.blocks[0].characters {
		g.blocks[0].characters[i].drawY += 5
	}
	var r effectRenderer
	if err := r.Prepare(g, 1); err != nil {
		t.Fatal(err)
	}
	render := func(bright, dim float32) *image.RGBA {
		style := effectStyle{Opacity: 1, BrightOpacity: bright, DimOpacity: dim, BrightMaskAlpha: 1, DarkMaskAlpha: 1, AllowSoftware: true, Enabled: true}
		if err := r.SyncFrame(g, time.Unix(0, 0), 0, style); err != nil {
			t.Fatal(err)
		}
		var drawErr error
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				drawErr = r.DrawLine(p, g, 10, 12, 0, style)
			})
		}, 64, 64, 1)
		if drawErr != nil {
			t.Fatal(drawErr)
		}
		return img
	}
	bright, dim := render(1, 0), render(0, 1)
	verticalInk := func(img *image.RGBA) (int, int, float64) {
		minY, maxY, mass, moment := img.Rect.Max.Y, -1, float64(0), float64(0)
		for y := 0; y < img.Rect.Max.Y; y++ {
			for x := 0; x < img.Rect.Max.X; x++ {
				a := img.RGBAAt(x, y).A
				if a > 1 {
					minY, maxY = min(minY, y), max(maxY, y)
					mass += float64(a)
					moment += float64(a) * (float64(y) + .5)
				}
			}
		}
		return minY, maxY, moment / max(1, mass)
	}
	by0, by1, bc := verticalInk(bright)
	dy0, dy1, dc := verticalInk(dim)
	if by1 < 0 || dy1 < 0 {
		t.Fatal("baseline fixture produced no glyph pixels")
	}
	// Separate atlas placements can change horizontal subpixel coverage; the
	// glyphs' vertical extents and alpha-weighted baseline must stay aligned.
	if by0 != dy0 || by1 != dy1 || math.Abs(bc-dc) > .1 {
		t.Fatalf("static clone baseline differs: bright=(%d,%d,%g) dim=(%d,%d,%g)", by0, by1, bc, dy0, dy1, dc)
	}
}

func TestEffectsTransparentBackgroundDoesNotReserveTextures(t *testing.T) {
	main := &lineGeometry{font: ui.Font{Size: 10}, width: 40, height: 20, end: time.Second}
	background := *main
	background.width = effectSideLimit * 2 // invisible children need no raster
	layers := []effectGroupLayer{
		{Geometry: main, Scale: .97, Style: effectStyle{Opacity: 1}},
		{Geometry: &background, Scale: .6, Style: effectStyle{Opacity: 0}},
	}
	var r effectRenderer
	if err := r.PrepareGroup(main, layers, 2, ui.Rect{W: 40, H: 80}); err != nil {
		t.Fatal(err)
	}
	if len(r.entries) != 1 || len(r.groups) != 1 || !r.groups[0].single {
		t.Fatal("zero-alpha background retained resources or expanded the parent filter")
	}
	if err := r.SyncFrame(&background, time.Unix(0, 0), 0, layers[1].Style); err != nil {
		t.Fatal("zero-alpha background unnecessarily required a prepared raster:", err)
	}
	// When a visible BG becomes transparent, the main source changes from tight
	// grouped ink to a singleton with full 3-sigma Gaussian support.
	background.width = 40
	layers[1].Style.Opacity = .4
	r.BeginFrame()
	if err := r.PrepareGroup(main, layers, 2, ui.Rect{W: 40, H: 80}); err != nil {
		t.Fatal(err)
	}
	tight := r.preparedEntry(main, 2).area
	layers[1].Style.Opacity = 0
	r.BeginFrame()
	if err := r.PrepareGroup(main, layers, 2, ui.Rect{W: 40, H: 80}); err != nil {
		t.Fatal(err)
	}
	entry := r.preparedEntry(main, 2)
	if entry.area.X >= tight.X || entry.area.Y >= tight.Y || entry.blurX == nil {
		t.Fatal("group-to-singleton transition cropped parent Gaussian support")
	}
}

func TestEffectsDimOnlyResourcesMatchCompletePipeline(t *testing.T) {
	registerSnapshotFonts(t)
	doc, err := lyric.NewDocument([]lyric.Line{{Text: "Hi blue glow", Translation: "sub", End: 4 * time.Second, Words: []lyric.Word{{Text: "Hi ", End: time.Second}, {Text: "blue ", Start: time.Second, End: 2 * time.Second}, {Text: "glow", Start: 2 * time.Second, End: 4 * time.Second}}}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := buildLineGeometryWithStyle(doc.Line(0), ui.Font{Family: snapshotRegularFamily, Size: 10, LineHeight: 1.2}, 64, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range []float32{1, 2} {
		layers := []effectGroupLayer{{Geometry: g, X: 2, Y: 3, Scale: .97, OriginX: 64, OriginY: g.height / 2, Style: effectStyle{Opacity: 1, BrightOpacity: 0, DimOpacity: .2, BrightMaskAlpha: 1, DarkMaskAlpha: .4, Enabled: true, Playing: true, AllowSoftware: true}}}
		var full, compact effectRenderer
		fullLayers := append([]effectGroupLayer(nil), layers...)
		fullLayers[0].Style.BrightOpacity = 1
		bounds := ui.Rect{W: 64, H: g.height + 10}
		if err := full.PrepareGroup(g, fullLayers, scale, bounds); err != nil {
			t.Fatal(err)
		}
		if err := compact.PrepareGroup(g, layers, scale, bounds); err != nil {
			t.Fatal(err)
		}
		entry := compact.preparedEntry(g, scale)
		if !entry.dimOnly || entry.source != nil || entry.glowX != nil || entry.glowY != nil || compact.bytes >= full.bytes {
			t.Fatal("zero-brightness pipeline retained invisible animated resources")
		}
		render := func(r *effectRenderer) *image.RGBA {
			if err := r.SyncFrame(g, time.Unix(1, 0), 800*time.Millisecond, layers[0].Style); err != nil {
				t.Fatal(err)
			}
			var drawErr error
			img := ui.Render(func(c *ui.Context) {
				c.Root().Background(ui.Transparent)
				ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
					drawErr = r.DrawGroup(p, g, 4, 8, layers, .65, .5)
				})
			}, 80, 60, scale)
			if drawErr != nil {
				t.Fatal(drawErr)
			}
			return img
		}
		before, after := render(&full), render(&compact)
		visible := 0
		for i, value := range before.Pix {
			if value != 0 {
				visible++
			}
			if abs8(value, after.Pix[i]) > 2 {
				t.Fatalf("scale=%g byte=%d full=%d dim-only=%d", scale, i, value, after.Pix[i])
			}
		}
		if visible == 0 {
			t.Fatal("dim-only comparison produced no content")
		}
		// Restore the animated resources without resetting paused/residual clocks.
		clock := compact.preparedEntry(g, scale).animation
		compact.BeginFrame()
		layers[0].Style.BrightOpacity = .5
		if err := compact.PrepareGroup(g, layers, scale, bounds); err != nil {
			t.Fatal(err)
		}
		restored := compact.preparedEntry(g, scale)
		if restored.dimOnly || restored.source == nil || restored.animation != clock {
			t.Fatal("restoring animated resources reset the persistent animation clock")
		}
	}
}

func TestEffectsGroupBackgroundUsesParentFilterAndOpacity(t *testing.T) {
	g := &lineGeometry{font: ui.Font{Size: 10}, width: 20, height: 10, end: time.Second}
	layers := []effectGroupLayer{{Geometry: g, Scale: 1, Style: effectStyle{Opacity: 0, AllowSoftware: true}}}
	background := effectGroupBackground{Rect: ui.Rect{X: -6, Y: 4, W: 40, H: 20}, Radius: 4, Color: ui.Color{R: 255, G: 255, B: 255, A: 17}}
	var r effectRenderer
	if err := r.PrepareGroup(g, layers, 1, ui.Rect{W: 20, H: 10}, background); err != nil {
		t.Fatal(err)
	}
	if r.groups[0].single || r.groups[0].empty || len(r.entries) != 0 {
		t.Fatal("wrapper background failed to reserve a parent group independently of transparent children")
	}
	render := func(opacity, blur float32) *image.RGBA {
		var drawErr error
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				drawErr = r.DrawGroup(p, g, 16, 12, layers, opacity, blur, background)
			})
		}, 80, 64, 1)
		if drawErr != nil {
			t.Fatal(drawErr)
		}
		return img
	}
	full, half, blurred := render(1, 0), render(.5, 0), render(.5, 2)
	if full.RGBAAt(30, 25).A != 17 || abs8(half.RGBAAt(30, 25).A, 9) > 1 {
		t.Fatal("wrapper background did not share parent opacity")
	}
	if full.RGBAAt(10, 16).A != 0 {
		t.Fatal("wrapper .25em rounded corner was lost")
	}
	if full.RGBAAt(9, 25).A != 0 || blurred.RGBAAt(9, 25).A == 0 {
		t.Fatal("wrapper background was not inside the same group Gaussian during its exit transition")
	}
	for i, value := range full.Pix {
		if abs8(half.Pix[i], uint8(math.Round(float64(value)*.5))) > 1 {
			t.Fatalf("parent alpha applied incorrectly at byte=%d full=%d half=%d", i, value, half.Pix[i])
		}
	}
}

func TestEffectsConfigurableFadeKeepsRawTimeline(t *testing.T) {
	g := &lineGeometry{end: 4 * time.Second, blocks: []wordBlock{{end: time.Second, width: 40, height: 20, padding: 10}, {start: 2 * time.Second, end: 3 * time.Second, width: 60, height: 20, padding: 10}}}
	path := newEffectMaskPath(g)
	for _, width := range []float32{.0001, .25, .5, 1, 2} {
		fade := float32(20) * width
		// At the end of the first word, the cumulative raw cursor is its
		// measured width plus 1.5 fade units, independent of any raster cache.
		got := path.position(0, 40, 10, fade, time.Second)
		want := min(float32(0), -fade*.5-10)
		if math.Abs(float64(got-want)) > .0001 {
			t.Errorf("width=%g end mask-position=%g want=%g", width, got, want)
		}
	}
}

func TestEffectsAtlasRetainsDifferentGlyphTileIdentities(t *testing.T) {
	registerSnapshotFonts(t)
	var requests []*effectTileRequest
	var tiles [3]effectTile
	var expected []*image.RGBA
	for i, item := range []struct {
		text string
		size float32
	}{{".", 10}, {"MM", 36}, {"g", 20}} {
		layout := ui.ShapeText(item.text, ui.Font{Family: snapshotRegularFamily, Size: item.size, LineHeight: 1.2}, 0)
		requests = append(requests, &effectTileRequest{width: 100, height: 80, tile: &tiles[i], paint: []effectRaster{{layout: layout, x: 3, y: 2}}})
		expected = append(expected, ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				p.TextLayout(layout, 3, 2, ui.RGB(255, 255, 255))
			})
		}, 100, 80, 1))
	}
	original := append([]*effectTileRequest(nil), requests...)
	atlas, _, _, err := effectAtlas(requests, 1)
	if err != nil {
		t.Fatal(err)
	}
	if tiles[1].rect.H <= tiles[0].rect.H || tiles[1].rect.W <= tiles[2].rect.W {
		t.Fatal("fixture did not force a different height/width packing order")
	}
	for i, tile := range tiles {
		if requests[i] != original[i] {
			t.Fatal("packing reordered requests independently of their raster pixel pairs")
		}
		ink := effectInk(expected[i], expected[i].Rect)
		if int(tile.rect.W) != ink.Dx() || int(tile.rect.H) != ink.Dy() {
			t.Fatalf("glyph %d dimensions changed", i)
		}
		for y := 0; y < ink.Dy(); y++ {
			for x := 0; x < ink.Dx(); x++ {
				got := atlas.RGBAAt(int(tile.rect.X)+x, int(tile.rect.Y)+y)
				want := expected[i].RGBAAt(ink.Min.X+x, ink.Min.Y+y)
				if got != want {
					t.Fatalf("glyph %d atlas pixel=%d,%d got=%v want=%v", i, x, y, got, want)
				}
			}
		}
	}
}

func TestEffectsExplicitTransparentStyleColor(t *testing.T) {
	registerSnapshotFonts(t)
	doc, err := lyric.NewDocument([]lyric.Line{{Text: "AB", End: time.Second, Words: []lyric.Word{{Text: "A", End: 500 * time.Millisecond}, {Text: "B", Start: 500 * time.Millisecond, End: time.Second}}}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := buildLineGeometryWithStyle(doc.Line(0), ui.Font{Family: snapshotRegularFamily, Size: 12, LineHeight: 1.2}, 32, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	var r effectRenderer
	if err := r.Prepare(g, 1); err != nil {
		t.Fatal(err)
	}
	alphaSum := func(hasColor bool) int {
		style := effectStyle{Opacity: 1, BrightOpacity: 1, BrightMaskAlpha: 1, DarkMaskAlpha: 1, HasColor: hasColor, Enabled: true, AllowSoftware: true}
		if err := r.SyncFrame(g, time.Unix(0, 0), 0, style); err != nil {
			t.Fatal(err)
		}
		var drawErr error
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				drawErr = r.DrawLine(p, g, 8, 12, 0, style)
			})
		}, 48, 40, 1)
		if drawErr != nil {
			t.Fatal(drawErr)
		}
		total := 0
		for y := 0; y < img.Rect.Max.Y; y++ {
			for x := 0; x < img.Rect.Max.X; x++ {
				total += int(img.RGBAAt(x, y).A)
			}
		}
		return total
	}
	if alphaSum(false) == 0 || alphaSum(true) != 0 {
		t.Fatal("explicit transparent black was confused with the legacy default white foreground")
	}
}

func TestEffectsColdAtlasUsesLTRBidiGlyphsAtCSSBaseline(t *testing.T) {
	font := ui.Font{Family: "Segoe UI", Size: 20, Weight: 600, LineHeight: 1.2}
	for _, text := range []string{"אבג!", "(אבג) 123", "عربي 123"} {
		for _, scale := range []float32{1, 2} {
			physical := effectPhysicalFont(font, scale)
			glyphs, width, exact := referenceTextGlyphs(text, physical)
			if !exact {
				t.Skip("the platform reports the explicit bidi fallback; exact UAX9 native validation requires Windows ICU")
			}
			layout := ui.ShapeText(text, physical, 0)
			baseline := layout.Lines()[0].Baseline
			w, h := int(math.Ceil(float64(width+8*scale))), int(60*scale)
			paint := effectPhysicalRaster(text, font, scale, 2, 4)
			if len(paint.glyphs) == 0 || paint.baseline != baseline {
				t.Fatal("cold raster discarded visual runs or changed the native baseline")
			}
			paint.x *= scale
			paint.y *= scale
			var tile effectTile
			atlas, _, _, err := effectAtlas([]*effectTileRequest{{width: w, height: h, tile: &tile, paint: []effectRaster{paint}}}, scale)
			if err != nil {
				t.Fatal(err)
			}
			expected := ui.Render(func(c *ui.Context) {
				c.Root().Background(ui.Transparent)
				ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
					p.Glyphs(glyphs, 2*scale, 4*scale+baseline, ui.RGB(255, 255, 255))
				})
			}, w, h, 1)
			ink := effectInk(expected, expected.Rect)
			if ink.Empty() || int(tile.rect.W) != ink.Dx() || int(tile.rect.H) != ink.Dy() {
				t.Fatalf("scale=%g cold bidi ink dimensions changed", scale)
			}
			if tile.origin.X != float32(ink.Min.X)/scale || tile.origin.Y != float32(ink.Min.Y)/scale {
				t.Fatal("CSS drawY was lost when cropping the visual glyph raster")
			}
			for y := 0; y < ink.Dy(); y++ {
				for x := 0; x < ink.Dx(); x++ {
					got := atlas.RGBAAt(int(tile.rect.X)+x, int(tile.rect.Y)+y)
					want := expected.RGBAAt(ink.Min.X+x, ink.Min.Y+y)
					if got != want {
						t.Fatalf("scale=%g pixel=%d,%d cold atlas reordered or offset explicit visual glyphs", scale, x, y)
					}
				}
			}
		}
	}
}

func TestEffectsColorAtlasKeepsSubpixelXAndSnappedBaseline(t *testing.T) {
	owner, _ := syntheticEmojiFont(emojiOutlinePaint(emojiSolidPaint(0)))
	defer owner.Close()
	for _, phase := range []float32{0, .25, .5, .75} {
		original := []referenceColorGlyph{{Font: owner, ID: 1, Size: 20, X: .3, Y: .6}}
		raster := effectRaster{colors: original, x: 2 + phase, y: phase, baseline: 21}
		var tile effectTile
		atlas, _, _, err := effectAtlas([]*effectTileRequest{{width: 30, height: 30, tile: &tile, paint: []effectRaster{raster}}}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if original[0].X != .3 || original[0].Y != .6 {
			t.Fatal("atlas changed the borrowed shaping offsets")
		}
		// This reference raster includes the complete physical text position;
		// comparing every covered pixel detects a second image interpolation.
		wantGlyph := original[0]
		wantGlyph.X += raster.x
		wantGlyph.Y += float32(math.Floor(float64(raster.y+raster.baseline) + .5))
		want, err := rasterEmojiColorGlyphs([]referenceColorGlyph{wantGlyph}, ui.RGB(255, 255, 255))
		if err != nil {
			t.Fatal(err)
		}
		expected := image.NewRGBA(image.Rect(0, 0, 30, 30))
		draw.Draw(expected, want.Pixels.Rect, want.Pixels, want.Pixels.Rect.Min, draw.Src)
		ink := effectInk(expected, expected.Rect)
		if tile.origin != (ui.Point{X: float32(ink.Min.X), Y: float32(ink.Min.Y)}) || int(tile.rect.W) != ink.Dx() || int(tile.rect.H) != ink.Dy() {
			t.Fatalf("phase=%g changed physical color ink bounds: %v vs %v", phase, tile, ink)
		}
		for y := 0; y < ink.Dy(); y++ {
			for x := 0; x < ink.Dx(); x++ {
				got := atlas.RGBAAt(int(tile.rect.X)+x, int(tile.rect.Y)+y)
				if got != expected.RGBAAt(ink.Min.X+x, ink.Min.Y+y) {
					t.Fatalf("phase=%g color pixel=%d,%d received a second subpixel translation", phase, x, y)
				}
			}
		}
	}
}

func TestEffectsContextualSoftHyphenOnlyRowKeepsVisibleBreakGlyph(t *testing.T) {
	provider := newReferenceFontProvider(nil)
	defer provider.Close()
	font := ui.Font{Family: "Segoe UI", Size: 20, Weight: 600, LineHeight: 1.5}
	rows, _, exact, err := referenceAnnotationRows("\u00adabc", font, 10, false, provider)
	if err != nil {
		t.Fatal(err)
	}
	if !exact {
		t.Skip("contextual annotation shaping is unavailable on this platform")
	}
	if len(rows) != 2 || rows[0].text != "\u00ad" || rows[0].startRune != 0 || rows[0].endRune != 1 {
		t.Fatalf("discretionary break lost its original source range: %+v", rows)
	}
	for _, scale := range []float32{1, 2} {
		physical := effectPhysicalFont(font, scale)
		shape := provider.ShapeRange(rows[0].context, rows[0].startRune, rows[0].endRune, physical)
		if shape.Err != nil || !shape.Exact || len(shape.Glyphs) == 0 || shape.Width <= 0 {
			t.Fatalf("discretionary break produced no contextual visible glyph: %+v", shape)
		}
		paints := effectRowRasters(rows[:1], "\u00adabc", font, scale, 8, 8, 0, provider)
		if len(paints) != 1 || paints[0].err != nil || len(paints[0].glyphs) == 0 {
			t.Fatal("the invisible standalone SHY discarded the contextual hyphen")
		}
		paint := paints[0]
		paint.x, paint.y = paint.x*scale, paint.y*scale
		hyphen := ui.ShapeText("\u2010", physical, 0)
		if len(hyphen.Lines()) == 0 || paint.baseline != hyphen.Lines()[0].Baseline {
			t.Fatal("standalone SHY layout lost the visible break glyph's native baseline")
		}
		var tile effectTile
		side := int(64 * scale)
		atlas, _, _, err := effectAtlas([]*effectTileRequest{{width: side, height: side, tile: &tile, paint: []effectRaster{paint}}}, scale)
		if err != nil {
			t.Fatal(err)
		}
		expected := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				p.Glyphs(shape.Glyphs, paint.x, paint.y+hyphen.Lines()[0].Baseline, ui.RGB(255, 255, 255))
			})
		}, side, side, 1)
		ink := effectInk(expected, expected.Rect)
		if ink.Empty() || int(tile.rect.W) != ink.Dx() || int(tile.rect.H) != ink.Dy() {
			t.Fatal("contextual hyphen ink was discarded or cropped by the cold atlas")
		}
		for y := 0; y < ink.Dy(); y++ {
			for x := 0; x < ink.Dx(); x++ {
				if atlas.RGBAAt(int(tile.rect.X)+x, int(tile.rect.Y)+y) != expected.RGBAAt(ink.Min.X+x, ink.Min.Y+y) {
					t.Fatalf("scale=%g contextual hyphen changed at pixel=%d,%d", scale, x, y)
				}
			}
		}
	}
}

func TestEffectsLinePaintContainmentBeforeParentFilter(t *testing.T) {
	if err := compileEffectPrograms(); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := 0; y < pixels.Rect.Dy(); y++ {
		for x := 0; x < pixels.Rect.Dx(); x++ {
			pixels.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	bitmap := ui.NewBitmap(pixels)
	entry := effectLine{
		geometry: &lineGeometry{paintBounds: ui.Rect{X: 2, Y: 1, W: 4, H: 4}},
		scale:    1, width: 24, height: 16, area: ui.Rect{X: -4, Y: -4},
		white: bitmap, black: bitmap, compositeValues: effectPrograms.composite.NewValues(),
		dim:         effectTile{rect: ui.Rect{W: 24, H: 16}, origin: ui.Point{X: -4, Y: -4}},
		annotations: effectTile{rect: ui.Rect{W: 24, H: 16}, origin: ui.Point{X: -4, Y: -4}},
	}
	render := func(style effectStyle, sigma float32) *image.RGBA {
		entry.setComposite(style, sigma, 1, 1, ui.Point{X: 1}, false)
		return ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				effectPrograms.composite.Draw(p, ui.Rect{W: 24, H: 16}, shader.Options{Images: [4]*ui.Bitmap{bitmap, bitmap, bitmap}, Values: entry.compositeValues})
			})
		}, 24, 16, 1)
	}
	for _, style := range []effectStyle{{BrightOpacity: 1}, {DimOpacity: 1}, {SubOpacity: 1}} {
		// Zero SubOpacity selects the normal .3 default; all three sources
		// must nevertheless respect the same unscaled CSS border box.
		img := render(style, 0)
		if img.RGBAAt(7, 6).A == 0 || img.RGBAAt(5, 6).A != 0 || img.RGBAAt(7, 4).A != 0 || img.RGBAAt(10, 6).A != 0 || img.RGBAAt(7, 9).A != 0 {
			t.Fatal("line paint containment did not clip bright, dim and annotation pixels together")
		}
	}
	entry.contentScale, entry.contentPivot = .5, ui.Point{X: 8, Y: 6}
	img := render(effectStyle{BrightOpacity: 1}, 0)
	if img.RGBAAt(8, 6).A == 0 || img.RGBAAt(6, 6).A != 0 || img.RGBAAt(9, 6).A != 0 {
		t.Fatal("line containment was applied after child scaling")
	}
	entry.contentScale = 1
	img = render(effectStyle{BrightOpacity: 1}, 1)
	if img.RGBAAt(5, 6).A == 0 || img.RGBAAt(7, 4).A != 0 {
		t.Fatal("line containment clipped the parent's Gaussian result instead of its input")
	}
	entry.geometry.paintBounds = ui.Rect{}
	img = render(effectStyle{BrightOpacity: 1}, 0)
	if img.RGBAAt(5, 6).A == 0 {
		t.Fatal("legacy geometry unexpectedly enabled CSS containment")
	}
}

func TestEffectsMaskFrozenOriginalWebAnimator(t *testing.T) {
	// These are frozen .animate() frames emitted by the unmodified AMLL
	// f322858 WebMaskAnimator.apply (Node transform-types), not a second port.
	// Chrome supplies underlying mask-position 0px when offset 1 is omitted.
	for _, tc := range []struct {
		name   string
		end    time.Duration
		blocks []wordBlock
		frames [2][][2]float64
	}{
		{"pause-tail", 4 * time.Second, []wordBlock{{end: time.Second}, {start: 2 * time.Second, end: 3 * time.Second}}, [2][][2]float64{
			{{0, -70}, {.25, -15}, {.5, -15}, {.5576923076923077, 0}, {.75, 0}, {1, 0}},
			{{0, -90}, {.18181818181818182, -90}, {.25, -75}, {.5, -75}, {.75, -10}, {1, 0}},
		}},
		{"overlap", 3 * time.Second, []wordBlock{{end: 2 * time.Second}, {end: 3 * time.Second}}, [2][][2]float64{
			{{0, -70}, {.6666666666666666, -15}, {.7435897435897436, 0}, {1, 0}},
			{{0, -90}, {.48484848484848486, -90}, {.6666666666666666, -75}, {1, -10}},
		}},
		{"ruby-utf16", 4 * time.Second, []wordBlock{{end: 3 * time.Second, ruby: []lyric.RubyWord{{Text: "😊", End: time.Second}, {Text: "an", Start: 1500 * time.Millisecond, End: 2500 * time.Millisecond}}}, {start: 3 * time.Second, end: 4 * time.Second}}, [2][][2]float64{
			{{0, -70}, {.125, -45}, {.25, -35}, {.375, -35}, {.5, -25}, {.625, -15}, {.75, -15}, {.8076923076923077, 0}, {1, 0}},
			{{0, -90}, {.125, -90}, {.25, -90}, {.375, -90}, {.4375, -90}, {.5, -85}, {.625, -75}, {.75, -75}, {1, -10}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.blocks {
				tc.blocks[i].width, tc.blocks[i].height, tc.blocks[i].padding = float32(40+20*i), 20, 10
			}
			path := newEffectMaskPath(&lineGeometry{end: tc.end, blocks: tc.blocks})
			for i, frames := range tc.frames {
				for j, point := range frames {
					points := [][2]float64{point}
					if j > 0 {
						prev := frames[j-1]
						points = append(points, [2]float64{(point[0] + prev[0]) / 2, (point[1] + prev[1]) / 2})
					}
					for _, point := range points {
						at := time.Duration(point[0] * float64(tc.end))
						got := path.position(i, tc.blocks[i].width, 10, 10, at)
						if math.Abs(float64(got)-point[1]) > .0001 {
							t.Fatalf("word=%d offset=%g got=%g original=%g", i, point[0], got, point[1])
						}
					}
				}
			}
		})
	}
}

func BenchmarkEffectsWarmPrepareGroup(b *testing.B) {
	doc, err := lyric.NewDocument([]lyric.Line{{Text: "A bright synthetic native line", Start: 0, End: 4 * time.Second, Words: []lyric.Word{{Text: "A bright synthetic native ", End: 2 * time.Second}, {Text: "line", Start: 2 * time.Second, End: 4 * time.Second}}}})
	if err != nil {
		b.Fatal(err)
	}
	g, err := buildLineGeometryWithStyle(doc.Line(0), ui.Font{Size: 30, Weight: 600, LineHeight: 1.2}, 765, geometryStyle{Dynamic: true})
	if err != nil {
		b.Fatal(err)
	}
	layers := []effectGroupLayer{{Geometry: g, Scale: .97, Style: effectStyle{Opacity: 1, BrightOpacity: 1, BrightMaskAlpha: 1, DarkMaskAlpha: .4}}}
	var r effectRenderer
	bounds := ui.Rect{W: 765, H: g.height}
	if err := r.PrepareGroup(g, layers, 2, bounds); err != nil {
		b.Fatal(err)
	}
	now := time.Unix(0, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		r.BeginFrame()
		if err := r.PrepareGroup(g, layers, 2, bounds); err != nil {
			b.Fatal(err)
		}
		if err := r.SyncFrame(g, now, time.Second, layers[0].Style); err != nil {
			b.Fatal(err)
		}
	}
}

func TestEffectsExternalCommonViewportBudget(t *testing.T) {
	input := os.Getenv("MYGO_AMLL_TEST_TTML")
	if input == "" {
		t.Skip("set MYGO_AMLL_TEST_TTML to an authorized local TTML; no lyrics are copied into the repository")
	}
	loaded, err := loadTTML(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	fonts, releaseFonts, err := openNativeReferenceFonts()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseFonts)
	for _, start := range []time.Duration{6 * time.Second, 62 * time.Second, 137 * time.Second} {
		t.Run(start.String(), func(t *testing.T) {
			now := time.Unix(0, 0)
			s := newScene(loaded.Document, now)
			s.fontProvider = fonts
			t.Cleanup(func() { _ = s.Close() })
			s.font = ui.Font{Family: "Segoe UI", Size: 37, Weight: 600, LineHeight: 1.2, Lang: "en"}
			if err := s.enableAMLL(true); err != nil {
				t.Fatal(err)
			}
			s.setReferenceHostSize(960, 740)
			s.clock.seek(now, start)
			s.clock.setPlaying(now, true)
			s.prepareReferenceFrame(now, ui.Rect{W: 960, H: 740}, 2, false)
			peak := 0
			for frame := 1; frame <= 600; frame++ {
				at := now.Add(time.Duration(frame) * time.Second / 60)
				s.update(at, false)
				if frame%150 == 0 {
					// Admit a full-width hovered wrapper while every other visible
					// row stays pinned. Replace this key's prior wrapper, exactly as
					// a new host frame does, rather than retaining two parent filters.
					for _, row := range s.reference.rows[:s.reference.rowCount] {
						if !row.visible || row.main == nil || row.index != s.reference.state.Focus {
							continue
						}
						for _, group := range s.reference.effects.groups {
							if group.key == row.main {
								group.pinned = false
							}
						}
						bounds := ui.Rect{W: row.main.width, H: .8*s.reference.font.Size + row.main.height}
						if row.background != nil {
							bounds.H += .3*s.reference.font.Size + row.background.height*2
						}
						background := effectGroupBackground{Rect: ui.Rect{X: -row.x, W: 960, H: row.height}, Radius: .25 * s.reference.font.Size, Color: ui.Color{R: 255, G: 255, B: 255, A: 17}}
						if err := s.reference.effects.PrepareGroup(row.main, row.layers[:row.layerCount], 2, bounds, background); err != nil {
							t.Fatalf("at=%s hovered wrapper exceeded resource budget: %v", s.clock.positionAt(at), err)
						}
					}
				}
				peak = max(peak, s.reference.effects.bytes)
				if s.reference.fallback != "" {
					t.Fatalf("at=%s effects=%dB fallback=%s", s.clock.positionAt(at), s.reference.effects.bytes, s.reference.fallback)
				}
			}
			t.Logf("960x740 font37 DPR2, ten seconds/600 updates, peak retained estimate=%dB; no GPU/RSS measurement", peak)
		})
	}
}
