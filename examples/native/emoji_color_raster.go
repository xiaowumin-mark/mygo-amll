// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/egoist/mygo/ui"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// Compositing uses premultiplied sRGB. COLR color-line stops are kept straight
// until after interpolation; otherwise an alpha-zero stop loses its color.
type emojiRGBA struct{ r, g, b, a float64 }

func (c emojiRGBA) scale(a float64) emojiRGBA { return emojiRGBA{c.r * a, c.g * a, c.b * a, c.a * a} }
func (c emojiRGBA) premultiplied() emojiRGBA  { return emojiRGBA{c.r * c.a, c.g * c.a, c.b * c.a, c.a} }
func (c emojiRGBA) over(b emojiRGBA) emojiRGBA {
	left := 1 - c.a
	return emojiRGBA{c.r + b.r*left, c.g + b.g*left, c.b + b.b*left, c.a + b.a*left}
}
func emojiColor(c ui.Color) emojiRGBA {
	return emojiStraightColor(c).premultiplied()
}
func emojiStraightColor(c ui.Color) emojiRGBA {
	a := float64(c.A) / 255
	return emojiRGBA{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, a}
}
func emojiPixel(c color.RGBA) emojiRGBA {
	return emojiRGBA{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, float64(c.A) / 255}
}
func (c emojiRGBA) pixel() color.RGBA {
	channel := func(v float64) uint8 { return uint8(math.Round(max(0, min(1, v)) * 255)) }
	return color.RGBA{channel(c.r), channel(c.g), channel(c.b), channel(c.a)}
}

type emojiRasterBudget struct {
	live, peak, samples int64
}

func (b *emojiRasterBudget) reserve(bytes int64) error {
	if bytes < 0 || bytes > emojiWorkingLimit-b.live {
		return errEmojiColorLimit
	}
	b.live += bytes
	b.peak = max(b.peak, b.live)
	return nil
}
func (b *emojiRasterBudget) release(bytes int64) { b.live -= bytes }
func (b *emojiRasterBudget) work(samples int64) error {
	if samples < 0 || samples > emojiWorkPixelLimit-b.samples {
		return errEmojiColorLimit
	}
	b.samples += samples
	return nil
}

type emojiRasterizer struct {
	dst    *image.RGBA
	budget *emojiRasterBudget
}

// rasterEmojiColorGlyphs is cold preparation, not a Draw operation. It performs
// no I/O, font parsing, registration, or persistent glyph/image caching. The
// caller owns the returned pixels; the font owner must outlive this call.
// foreground is CPAL's special currentColor. The inherited CSS color alpha is
// subsequently applied to the complete glyph by the existing effects shader.
func rasterEmojiColorGlyphs(glyphs []referenceColorGlyph, foreground ui.Color) (result emojiColorRaster, err error) {
	return rasterEmojiColorGlyphsWithin(glyphs, foreground, emojiRasterPixels)
}

// The atlas supplies its remaining pixel allowance so aggregate temporary
// color images are admitted before allocation, rather than checked afterwards.
func rasterEmojiColorGlyphsWithin(glyphs []referenceColorGlyph, foreground ui.Color, remainingPixels int64) (result emojiColorRaster, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			result, err = emojiColorRaster{}, fmt.Errorf("%w: public font/raster operation failed: %v", errEmojiColorInvalid, failure)
		}
	}()
	program, err := compileEmojiPaint(glyphs, emojiStraightColor(foreground))
	if err != nil {
		return emojiColorRaster{}, err
	}
	if program.bounds.empty() {
		return emojiColorRaster{}, nil
	}
	rect := program.bounds.pixels()
	if int64(rect.Dx())*int64(rect.Dy()) > remainingPixels {
		return emojiColorRaster{}, errEmojiColorLimit
	}
	budget := emojiRasterBudget{}
	if err := budget.reserve(program.bytes + int64(rect.Dx())*int64(rect.Dy())*4); err != nil {
		return emojiColorRaster{}, err
	}
	raster := emojiRasterizer{image.NewRGBA(rect), &budget}
	for _, node := range program.roots {
		if err := raster.paint(node, nil); err != nil {
			return emojiColorRaster{}, err
		}
	}
	return emojiColorRaster{Pixels: raster.dst, Origin: rect.Min}, nil
}

func emojiMaskAlpha(mask *image.Alpha, x, y int) float64 {
	if mask == nil {
		return 1
	}
	return float64(mask.AlphaAt(x, y).A) / 255
}

func (r *emojiRasterizer) mask(node *emojiPaintNode, parent *image.Alpha) (*image.Alpha, int64, error) {
	rect := node.bounds.pixels().Intersect(r.dst.Rect)
	if parent != nil {
		rect = rect.Intersect(parent.Rect)
	}
	area := int64(rect.Dx()) * int64(rect.Dy())
	if rect.Empty() || len(node.segments) == 0 {
		return image.NewAlpha(image.Rectangle{}), 0, nil
	}
	// Account the output mask, a path copy and conservative transient RGBA
	// planes in ui.Render. Upstream runtime/font caches and RSS are separate.
	pathBytes := int64(len(node.segments)) * 32
	transient := area*12 + pathBytes
	if err := r.budget.reserve(area + transient); err != nil {
		return nil, 0, err
	}
	if err := r.budget.work(area * 4); err != nil {
		r.budget.release(area + transient)
		return nil, 0, err
	}
	path := new(ui.Path).FillRule(ui.NonZero)
	point := func(p ot.SegmentPoint) (float32, float32) {
		x, y := node.matrix.point(float64(p.X), float64(p.Y))
		return float32(x - float64(rect.Min.X)), float32(y - float64(rect.Min.Y))
	}
	open := false
	for _, segment := range node.segments {
		switch segment.Op {
		case ot.SegmentOpMoveTo:
			if open {
				path.Close()
			}
			x, y := point(segment.Args[0])
			path.MoveTo(x, y)
			open = true
		case ot.SegmentOpLineTo:
			x, y := point(segment.Args[0])
			path.LineTo(x, y)
		case ot.SegmentOpQuadTo:
			a, b := point(segment.Args[0])
			c, d := point(segment.Args[1])
			path.QuadTo(a, b, c, d)
		case ot.SegmentOpCubeTo:
			a, b := point(segment.Args[0])
			c, d := point(segment.Args[1])
			e, f := point(segment.Args[2])
			path.CubeTo(a, b, c, d, e, f)
		}
	}
	if open {
		path.Close()
	}
	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) { p.FillPath(path, ui.RGB(255, 255, 255)) })
	}, rect.Dx(), rect.Dy(), 1)
	mask := image.NewAlpha(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			value := img.RGBAAt(x-rect.Min.X, y-rect.Min.Y).A
			if parent != nil {
				value = uint8((uint16(value)*uint16(parent.AlphaAt(x, y).A) + 127) / 255)
			}
			mask.SetAlpha(x, y, color.Alpha{A: value})
		}
	}
	r.budget.release(transient)
	return mask, area, nil
}

func (r *emojiRasterizer) paint(node *emojiPaintNode, clip *image.Alpha) error {
	if node == nil || node.bounds.empty() {
		return nil
	}
	switch node.kind {
	case emojiLayers:
		for _, child := range node.children {
			if err := r.paint(child, clip); err != nil {
				return err
			}
		}
		return nil
	case emojiClip:
		mask, bytes, err := r.mask(node, clip)
		if err != nil {
			return err
		}
		defer r.budget.release(bytes)
		for _, child := range node.children {
			if err := r.paint(child, mask); err != nil {
				return err
			}
		}
		return nil
	case emojiSolid, emojiLinear, emojiRadial:
		return r.fill(node, clip)
	case emojiComposite:
		return r.composite(node, clip)
	default:
		return errEmojiColorUnsupported
	}
}

func (r *emojiRasterizer) fill(node *emojiPaintNode, clip *image.Alpha) error {
	rect := node.bounds.pixels().Intersect(r.dst.Rect)
	if clip != nil {
		rect = rect.Intersect(clip.Rect)
	}
	if err := r.budget.work(int64(rect.Dx()) * int64(rect.Dy())); err != nil {
		return err
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			alpha := emojiMaskAlpha(clip, x, y)
			if alpha == 0 {
				continue
			}
			c := node.color
			if node.kind != emojiSolid {
				px, py := node.matrix.point(float64(x)+.5, float64(y)+.5)
				position, valid := emojiGradientPosition(node, px, py)
				if !valid {
					continue
				}
				c = emojiGradientColor(node.stops, node.extend, position)
			}
			r.dst.SetRGBA(x, y, c.scale(alpha).over(emojiPixel(r.dst.RGBAAt(x, y))).pixel())
		}
	}
	return nil
}

func (r *emojiRasterizer) composite(node *emojiPaintNode, clip *image.Alpha) error {
	if len(node.children) != 2 {
		return errEmojiColorInvalid
	}
	rect := node.bounds.pixels().Intersect(r.dst.Rect)
	if rect.Empty() {
		return nil
	}
	area := int64(rect.Dx()) * int64(rect.Dy())
	if err := r.budget.reserve(area * 8); err != nil {
		return err
	}
	defer r.budget.release(area * 8)
	source, backdrop := image.NewRGBA(rect), image.NewRGBA(rect)
	// Isolate both operands. Applying the parent's antialiased clip to each
	// operand would square edge alpha for SrcIn; apply it to the result once.
	if err := (&emojiRasterizer{source, r.budget}).paint(node.children[0], nil); err != nil {
		return err
	}
	if err := (&emojiRasterizer{backdrop, r.budget}).paint(node.children[1], nil); err != nil {
		return err
	}
	if err := r.budget.work(area); err != nil {
		return err
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			s, b := emojiPixel(source.RGBAAt(x, y)), emojiPixel(backdrop.RGBAAt(x, y))
			var c emojiRGBA
			switch node.mode {
			case tables.CompositeSrcIn:
				c = s.scale(b.a)
			case tables.CompositeSrcOver:
				c = s.over(b)
			default:
				return errEmojiColorUnsupported
			}
			c = c.scale(emojiMaskAlpha(clip, x, y))
			r.dst.SetRGBA(x, y, c.over(emojiPixel(r.dst.RGBAAt(x, y))).pixel())
		}
	}
	return nil
}

func emojiGradientColor(stops []emojiStop, extend tables.Extend, position float64) emojiRGBA {
	if len(stops) == 0 || !emojiFinite(position) {
		return emojiRGBA{}
	}
	first, last := stops[0].at, stops[len(stops)-1].at
	// COLR repeats/reflection use the first-to-last stop interval, not a
	// hard-coded [0,1] interval; stops may lie outside that unit range.
	span := last - first
	if span > 0 && extend != tables.ExtendPad {
		u := (position - first) / span
		if extend == tables.ExtendRepeat {
			u -= math.Floor(u)
		} else {
			u = math.Mod(u, 2)
			if u < 0 {
				u += 2
			}
			if u > 1 {
				u = 2 - u
			}
		}
		position = first + u*span
	}
	next := sort.Search(len(stops), func(i int) bool { return stops[i].at > position })
	if next == 0 {
		return stops[0].color.premultiplied()
	}
	if next == len(stops) {
		return stops[len(stops)-1].color.premultiplied()
	}
	a, b := stops[next-1], stops[next]
	if b.at <= a.at {
		return b.color.premultiplied()
	}
	t := max(0, min(1, (position-a.at)/(b.at-a.at)))
	return (emojiRGBA{a.color.r + (b.color.r-a.color.r)*t, a.color.g + (b.color.g-a.color.g)*t, a.color.b + (b.color.b-a.color.b)*t, a.color.a + (b.color.a-a.color.a)*t}).premultiplied()
}

func emojiGradientPosition(node *emojiPaintNode, x, y float64) (float64, bool) {
	p := node.params
	if node.kind == emojiLinear {
		dx, dy := p[2]-p[0], p[3]-p[1]
		rx, ry := p[4]-p[0], p[5]-p[1]
		denom := dx*ry - dy*rx
		if math.Abs(denom) < 1e-12 {
			return 0, false
		}
		value := ((x-p[0])*ry - (y-p[1])*rx) / denom
		return value, emojiFinite(value)
	}
	px, py := x-p[0], y-p[1]
	dx, dy, dr := p[3]-p[0], p[4]-p[1], p[5]-p[2]
	a := dx*dx + dy*dy - dr*dr
	b := -2 * (px*dx + py*dy + p[2]*dr)
	c := px*px + py*py - p[2]*p[2]
	if math.Abs(a) < 1e-12 {
		if math.Abs(b) < 1e-12 {
			return 0, false
		}
		value := -c / b
		return value, emojiFinite(value) && p[2]+value*dr >= 0
	}
	discriminant := b*b - 4*a*c
	if discriminant < 0 {
		return 0, false
	}
	value := math.Inf(-1)
	for _, root := range [2]float64{(-b - math.Sqrt(discriminant)) / (2 * a), (-b + math.Sqrt(discriminant)) / (2 * a)} {
		if p[2]+root*dr >= 0 && root > value {
			value = root
		}
	}
	return value, emojiFinite(value)
}
