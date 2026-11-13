// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"image"
	"math"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

const (
	emojiGlyphLimit      = 64
	emojiPaintNodeLimit  = 4096
	emojiPaintDepthLimit = 32
	emojiSegmentLimit    = 65536
	emojiStopLimit       = 4096
	emojiProgramLimit    = 8 << 20
	emojiRasterSideLimit = 1024
	emojiRasterPixels    = 256 << 10
	emojiWorkingLimit    = 32 << 20
	emojiWorkPixelLimit  = 64 << 20
)

// This interface keeps tests synthetic and accesses fonts only through the
// public go-text API. None of the renderer reads private mygo glyph resources.
type emojiPaintSource interface {
	color(font.GID) (tables.PaintTable, bool)
	outline(font.GID) (font.GlyphOutline, bool)
	clip(font.GID) (tables.ClipBox, bool)
	palette() font.CPAL
	upem() uint16
	layers(tables.PaintColrLayers) ([]tables.PaintTable, error)
}

type emojiFaceSource struct{ face *font.Face }

func (s emojiFaceSource) color(id font.GID) (tables.PaintTable, bool) {
	c, ok := s.face.GlyphDataColor(id)
	return c.Paint, ok
}
func (s emojiFaceSource) outline(id font.GID) (font.GlyphOutline, bool) {
	return s.face.GlyphDataOutline(id)
}
func (s emojiFaceSource) clip(id font.GID) (tables.ClipBox, bool) {
	return s.face.COLR.ClipList.Search(tables.GlyphID(id))
}
func (s emojiFaceSource) palette() font.CPAL { return s.face.CPAL }
func (s emojiFaceSource) upem() uint16       { return s.face.Upem() }
func (s emojiFaceSource) layers(paint tables.PaintColrLayers) ([]tables.PaintTable, error) {
	return s.face.COLR.LayerList.Resolve(paint)
}

func (f *emojiColorFont) faceLayers(paint tables.PaintColrLayers) ([]tables.PaintTable, error) {
	if f == nil || f.source == nil {
		return nil, errEmojiColorClosed
	}
	layers, err := f.source.layers(paint)
	if err != nil {
		return nil, fmt.Errorf("%w: color layers: %v", errEmojiColorInvalid, err)
	}
	return layers, nil
}

type emojiMatrix struct{ a, b, c, d, x, y float64 }

func (m emojiMatrix) point(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.x, m.b*x + m.d*y + m.y
}
func (m emojiMatrix) compose(n emojiMatrix) emojiMatrix {
	return emojiMatrix{m.a*n.a + m.c*n.b, m.b*n.a + m.d*n.b, m.a*n.c + m.c*n.d, m.b*n.c + m.d*n.d, m.a*n.x + m.c*n.y + m.x, m.b*n.x + m.d*n.y + m.y}
}
func (m emojiMatrix) inverse() (emojiMatrix, bool) {
	det := m.a*m.d - m.b*m.c
	if !emojiFinite(m.a, m.b, m.c, m.d, m.x, m.y, det) || math.Abs(det) < 1e-12 {
		return emojiMatrix{}, false
	}
	return emojiMatrix{m.d / det, -m.b / det, -m.c / det, m.a / det, (m.c*m.y - m.d*m.x) / det, (m.b*m.x - m.a*m.y) / det}, true
}

func emojiFinite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

type emojiBox struct{ x0, y0, x1, y1 float64 }

func (b emojiBox) empty() bool { return b.x1 <= b.x0 || b.y1 <= b.y0 }
func (b emojiBox) intersect(c emojiBox) emojiBox {
	return emojiBox{max(b.x0, c.x0), max(b.y0, c.y0), min(b.x1, c.x1), min(b.y1, c.y1)}
}
func (b emojiBox) union(c emojiBox) emojiBox {
	if b.empty() {
		return c
	}
	if c.empty() {
		return b
	}
	return emojiBox{min(b.x0, c.x0), min(b.y0, c.y0), max(b.x1, c.x1), max(b.y1, c.y1)}
}
func (b emojiBox) pixels() image.Rectangle {
	if b.empty() {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Floor(b.x0))-1, int(math.Floor(b.y0))-1, int(math.Ceil(b.x1))+1, int(math.Ceil(b.y1))+1)
}

type emojiPaintKind uint8

const (
	emojiLayers emojiPaintKind = iota
	emojiClip
	emojiSolid
	emojiLinear
	emojiRadial
	emojiComposite
)

type emojiStop struct {
	at    float64
	color emojiRGBA // straight sRGB RGBA; transparent stops retain their RGB
}

type emojiPaintNode struct {
	kind     emojiPaintKind
	bounds   emojiBox
	matrix   emojiMatrix
	children []*emojiPaintNode
	segments []font.Segment
	color    emojiRGBA
	stops    []emojiStop
	extend   tables.Extend
	params   [6]float64
	mode     tables.CompositeMode
}

type emojiPaintProgram struct {
	roots    []*emojiPaintNode
	bounds   emojiBox
	bytes    int64
	nodes    int
	segments int
	stops    int
}

type emojiGlyphKey struct {
	font *emojiColorFont
	id   font.GID
}

type emojiPaintCompiler struct {
	program    emojiPaintProgram
	foreground emojiRGBA
	visiting   map[emojiGlyphKey]bool
}

func (c *emojiPaintCompiler) allocate(kind emojiPaintKind, depth int) (*emojiPaintNode, error) {
	c.program.nodes++
	c.program.bytes += 512 // node, child pointers and conservative scalar allowance
	if depth > emojiPaintDepthLimit || c.program.nodes > emojiPaintNodeLimit || c.program.bytes > emojiProgramLimit {
		return nil, errEmojiColorLimit
	}
	return &emojiPaintNode{kind: kind}, nil
}

func compileEmojiPaint(glyphs []referenceColorGlyph, foreground emojiRGBA) (emojiPaintProgram, error) {
	if len(glyphs) > emojiGlyphLimit {
		return emojiPaintProgram{}, errEmojiColorLimit
	}
	c := emojiPaintCompiler{foreground: foreground, visiting: make(map[emojiGlyphKey]bool)}
	for _, glyph := range glyphs {
		if glyph.Font == nil || glyph.Font.source == nil {
			return emojiPaintProgram{}, errEmojiColorClosed
		}
		if !emojiFinite(float64(glyph.X), float64(glyph.Y), float64(glyph.Size)) || glyph.Size <= 0 || glyph.Size > 512 || glyph.ID > 65535 || glyph.Font.glyphCount != 0 && uint32(glyph.ID) >= glyph.Font.glyphCount {
			return emojiPaintProgram{}, errEmojiColorLimit
		}
		upem := glyph.Font.source.upem()
		if upem == 0 {
			return emojiPaintProgram{}, errEmojiColorInvalid
		}
		scale := float64(glyph.Size) / float64(upem)
		node, err := c.glyph(glyph.Font, glyph.ID, emojiMatrix{a: scale, d: -scale, x: float64(glyph.X), y: float64(glyph.Y)}, emojiBox{}, false, 0)
		if err != nil {
			return emojiPaintProgram{}, err
		}
		c.program.roots = append(c.program.roots, node)
		c.program.bounds = c.program.bounds.union(node.bounds)
	}
	box := c.program.bounds
	if !emojiFinite(box.x0, box.y0, box.x1, box.y1) || max(math.Abs(box.x0), math.Abs(box.y0), math.Abs(box.x1), math.Abs(box.y1)) > 1<<20 {
		return emojiPaintProgram{}, errEmojiColorLimit
	}
	rect := box.pixels()
	if rect.Dx() > emojiRasterSideLimit || rect.Dy() > emojiRasterSideLimit || int64(rect.Dx())*int64(rect.Dy()) > emojiRasterPixels {
		return emojiPaintProgram{}, errEmojiColorLimit
	}
	return c.program, nil
}

func (c *emojiPaintCompiler) glyph(owner *emojiColorFont, id font.GID, m emojiMatrix, clip emojiBox, hasClip bool, depth int) (*emojiPaintNode, error) {
	key := emojiGlyphKey{owner, id}
	if c.visiting[key] {
		return nil, fmt.Errorf("%w: cyclic COLR glyph reference", errEmojiColorInvalid)
	}
	c.visiting[key] = true
	defer delete(c.visiting, key)
	if id > 65535 || owner.glyphCount != 0 && uint32(id) >= owner.glyphCount {
		return nil, errEmojiColorInvalid
	}
	paint, colored := owner.source.color(id)
	if !colored {
		node, err := c.outline(owner, id, m, clip, hasClip, depth)
		if err != nil {
			return nil, err
		}
		fill, err := c.allocate(emojiSolid, depth+1)
		if err != nil {
			return nil, err
		}
		fill.bounds, fill.color = node.bounds, c.foreground.premultiplied()
		node.children = []*emojiPaintNode{fill}
		return node, nil
	}
	if box, ok := owner.source.clip(id); ok {
		value, ok := box.(tables.ClipBoxFormat1)
		if !ok { // Variation instances are not silently rendered at the wrong coordinates.
			return nil, fmt.Errorf("%w: variable clip box", errEmojiColorUnsupported)
		}
		if value.XMax < value.XMin || value.YMax < value.YMin {
			return nil, errEmojiColorInvalid
		}
		segments := emojiRectSegments(float32(value.XMin), float32(value.YMin), float32(value.XMax), float32(value.YMax))
		node, err := c.outlineSegments(segments, m, clip, hasClip, depth)
		if err != nil {
			return nil, err
		}
		child, err := c.paint(owner, paint, m, node.bounds, true, depth+1)
		if err != nil {
			return nil, err
		}
		node.children, node.bounds = []*emojiPaintNode{child}, node.bounds.intersect(child.bounds)
		return node, nil
	}
	return c.paint(owner, paint, m, clip, hasClip, depth)
}

func emojiRectSegments(x0, y0, x1, y1 float32) []font.Segment {
	return []font.Segment{
		{Op: ot.SegmentOpMoveTo, Args: [3]font.SegmentPoint{{X: x0, Y: y0}}},
		{Op: ot.SegmentOpLineTo, Args: [3]font.SegmentPoint{{X: x1, Y: y0}}},
		{Op: ot.SegmentOpLineTo, Args: [3]font.SegmentPoint{{X: x1, Y: y1}}},
		{Op: ot.SegmentOpLineTo, Args: [3]font.SegmentPoint{{X: x0, Y: y1}}},
	}
}

func (c *emojiPaintCompiler) outline(owner *emojiColorFont, id font.GID, m emojiMatrix, clip emojiBox, hasClip bool, depth int) (*emojiPaintNode, error) {
	if id > 65535 || owner.glyphCount != 0 && uint32(id) >= owner.glyphCount {
		return nil, errEmojiColorInvalid
	}
	outline, ok := owner.source.outline(id)
	if !ok {
		return nil, fmt.Errorf("%w: no public outline for glyph %d", errEmojiColorUnsupported, id)
	}
	return c.outlineSegments(outline.Segments, m, clip, hasClip, depth)
}

func (c *emojiPaintCompiler) outlineSegments(segments []font.Segment, m emojiMatrix, clip emojiBox, hasClip bool, depth int) (*emojiPaintNode, error) {
	node, err := c.allocate(emojiClip, depth)
	if err != nil {
		return nil, err
	}
	c.program.segments += len(segments)
	c.program.bytes += int64(len(segments)) * 32
	if c.program.segments > emojiSegmentLimit || c.program.bytes > emojiProgramLimit {
		return nil, errEmojiColorLimit
	}
	box := emojiBox{x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
	for _, segment := range segments {
		if segment.Op > ot.SegmentOpCubeTo {
			return nil, errEmojiColorInvalid
		}
		for _, point := range segment.ArgsSlice() {
			x, y := m.point(float64(point.X), float64(point.Y))
			if !emojiFinite(x, y) || max(math.Abs(x), math.Abs(y)) > 1<<20 {
				return nil, errEmojiColorLimit
			}
			box.x0, box.y0 = min(box.x0, x), min(box.y0, y)
			box.x1, box.y1 = max(box.x1, x), max(box.y1, y)
		}
	}
	if len(segments) == 0 {
		box = emojiBox{}
	}
	if hasClip {
		box = box.intersect(clip)
	}
	node.matrix, node.bounds = m, box
	node.segments = append([]font.Segment(nil), segments...)
	return node, nil
}

func (c *emojiPaintCompiler) color(owner *emojiColorFont, index uint16, alpha tables.Fixed214) (emojiRGBA, error) {
	value, err := c.straightColor(owner, index, alpha)
	return value.premultiplied(), err
}

func (c *emojiPaintCompiler) straightColor(owner *emojiColorFont, index uint16, alpha tables.Fixed214) (emojiRGBA, error) {
	a := float64(alpha) / 16384
	if a < 0 || a > 1 {
		return emojiRGBA{}, errEmojiColorInvalid
	}
	if index == 65535 {
		value := c.foreground
		value.a *= a
		return value, nil
	}
	palettes := owner.source.palette()
	if len(palettes) == 0 || int(index) >= len(palettes[0]) {
		return emojiRGBA{}, errEmojiColorInvalid
	}
	value := palettes[0][index]
	a *= float64(value.Alpha) / 255
	return emojiRGBA{float64(value.Red) / 255, float64(value.Green) / 255, float64(value.Blue) / 255, a}, nil
}

func (c *emojiPaintCompiler) stops(owner *emojiColorFont, line tables.ColorLine) ([]emojiStop, error) {
	if len(line.ColorStops) == 0 || line.Extend > tables.ExtendReflect {
		return nil, errEmojiColorInvalid
	}
	c.program.stops += len(line.ColorStops)
	c.program.bytes += int64(len(line.ColorStops)) * 48
	if c.program.stops > emojiStopLimit || c.program.bytes > emojiProgramLimit {
		return nil, errEmojiColorLimit
	}
	values := make([]emojiStop, 0, len(line.ColorStops))
	for _, stop := range line.ColorStops {
		at := float64(stop.StopOffset) / 16384
		if len(values) != 0 && at < values[len(values)-1].at {
			return nil, errEmojiColorInvalid
		}
		color, err := c.straightColor(owner, stop.PaletteIndex, stop.Alpha)
		if err != nil {
			return nil, err
		}
		values = append(values, emojiStop{at, color})
	}
	return values, nil
}

func (c *emojiPaintCompiler) paint(owner *emojiColorFont, raw tables.PaintTable, m emojiMatrix, clip emojiBox, hasClip bool, depth int) (*emojiPaintNode, error) {
	if depth > emojiPaintDepthLimit || raw == nil {
		return nil, errEmojiColorLimit
	}
	transform := func(child tables.PaintTable, local emojiMatrix) (*emojiPaintNode, error) {
		// Count even transform-only chains; otherwise they could bypass node caps.
		if _, err := c.allocate(emojiLayers, depth); err != nil {
			return nil, err
		}
		return c.paint(owner, child, m.compose(local), clip, hasClip, depth+1)
	}
	switch paint := raw.(type) {
	case tables.PaintColrLayers:
		node, err := c.allocate(emojiLayers, depth)
		if err != nil {
			return nil, err
		}
		layers, err := owner.faceLayers(paint)
		if err != nil {
			return nil, err
		}
		if len(layers) > emojiPaintNodeLimit-c.program.nodes {
			return nil, errEmojiColorLimit
		}
		for _, layer := range layers {
			child, err := c.paint(owner, layer, m, clip, hasClip, depth+1)
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, child)
			node.bounds = node.bounds.union(child.bounds)
		}
		return node, nil
	case tables.PaintGlyph:
		node, err := c.outline(owner, font.GID(paint.GlyphID), m, clip, hasClip, depth)
		if err != nil {
			return nil, err
		}
		child, err := c.paint(owner, paint.Paint, m, node.bounds, true, depth+1)
		if err != nil {
			return nil, err
		}
		node.children, node.bounds = []*emojiPaintNode{child}, node.bounds.intersect(child.bounds)
		return node, nil
	case tables.PaintColrGlyph:
		return c.glyph(owner, font.GID(paint.GlyphID), m, clip, hasClip, depth+1)
	case tables.PaintTransform:
		value := paint.Transform
		return transform(paint.Paint, emojiMatrix{float64(value.Xx), float64(value.Yx), float64(value.Xy), float64(value.Yy), float64(value.Dx), float64(value.Dy)})
	case tables.PaintTranslate:
		return transform(paint.Paint, emojiMatrix{a: 1, d: 1, x: float64(paint.Dx), y: float64(paint.Dy)})
	case tables.PaintScale:
		return transform(paint.Paint, emojiMatrix{a: float64(paint.ScaleX) / 16384, d: float64(paint.ScaleY) / 16384})
	case tables.PaintScaleAroundCenter:
		x, y := float64(paint.CenterX), float64(paint.CenterY)
		scaleX, scaleY := float64(paint.ScaleX)/16384, float64(paint.ScaleY)/16384
		return transform(paint.Paint, emojiMatrix{a: scaleX, d: scaleY, x: x * (1 - scaleX), y: y * (1 - scaleY)})
	case tables.PaintComposite:
		if paint.CompositeMode != tables.CompositeSrcIn && paint.CompositeMode != tables.CompositeSrcOver {
			return nil, fmt.Errorf("%w: composite mode %d", errEmojiColorUnsupported, paint.CompositeMode)
		}
		node, err := c.allocate(emojiComposite, depth)
		if err != nil {
			return nil, err
		}
		source, err := c.paint(owner, paint.SourcePaint, m, clip, hasClip, depth+1)
		if err != nil {
			return nil, err
		}
		backdrop, err := c.paint(owner, paint.BackdropPaint, m, clip, hasClip, depth+1)
		if err != nil {
			return nil, err
		}
		node.mode, node.children = paint.CompositeMode, []*emojiPaintNode{source, backdrop}
		node.bounds = source.bounds.union(backdrop.bounds)
		if paint.CompositeMode == tables.CompositeSrcIn {
			node.bounds = source.bounds.intersect(backdrop.bounds)
		}
		return node, nil
	case tables.PaintSolid, tables.PaintLinearGradient, tables.PaintRadialGradient:
		if !hasClip {
			return nil, fmt.Errorf("%w: unbounded paint without glyph/clip box", errEmojiColorUnsupported)
		}
		node, err := c.allocate(emojiSolid, depth)
		if err != nil {
			return nil, err
		}
		node.bounds = clip
		switch paint := paint.(type) {
		case tables.PaintSolid:
			node.color, err = c.color(owner, paint.PaletteIndex, paint.Alpha)
		case tables.PaintLinearGradient:
			node.kind, node.extend = emojiLinear, paint.ColorLine.Extend
			node.stops, err = c.stops(owner, paint.ColorLine)
			node.params = [6]float64{float64(paint.X0), float64(paint.Y0), float64(paint.X1), float64(paint.Y1), float64(paint.X2), float64(paint.Y2)}
		case tables.PaintRadialGradient:
			node.kind, node.extend = emojiRadial, paint.ColorLine.Extend
			node.stops, err = c.stops(owner, paint.ColorLine)
			node.params = [6]float64{float64(paint.X0), float64(paint.Y0), float64(paint.Radius0), float64(paint.X1), float64(paint.Y1), float64(paint.Radius1)}
		}
		if err != nil {
			return nil, err
		}
		if node.kind != emojiSolid {
			var valid bool
			node.matrix, valid = m.inverse()
			if !valid && !clip.empty() {
				return nil, fmt.Errorf("%w: singular gradient transform", errEmojiColorInvalid)
			}
		}
		return node, nil
	default:
		return nil, fmt.Errorf("%w: %T", errEmojiColorUnsupported, raw)
	}
}
