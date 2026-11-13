package main

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

// Effect mathematics are adapted from AMLL (AGPL-3.0-only), fixed commit
// f32285864056587c4afa59d310208b0aa829cb53, packages/core/src/lyric-player/dom/
// lyric-line/animation/{mask,float,emphasize} and line-brightness.ts. Native
// raster preparation, packing, budgets, and Kage shaders are original code.
const (
	effectByteLimit      = 64 << 20
	effectEntryLimit     = 16
	effectSideLimit      = 4096
	effectCharacterLimit = 64
	effectBlurLimit      = 5
	effectScaleLimit     = 4
	effectSigmaLimit     = 64
	effectPreparePixels  = 2 << 20 // packed atlas pixels; cold workspace is separately bounded at 32 MiB
)

var (
	errEffectLimit    = errors.New("native effects: texture or character budget exceeded")
	errEffectSoftware = errors.New("native effects: shader rendering requires hardware acceleration; the host must label its fallback")
	errEffectMissing  = errors.New("native effects: line was not prepared for this frame")
)

// effectStyle is a sampled style, not an animation clock. Every alpha is
// explicit; Color's zero value selects the reference's white foreground.
type effectStyle struct {
	Clock                          *motion.LineAnimationClock
	Opacity, Blur                  float32
	BrightOpacity, DimOpacity      float32
	BrightMaskAlpha, DarkMaskAlpha float32
	SubOpacity                     float32
	WordFadeWidth                  float32 // zero selects .5; configured values use at least .0001
	Color                          ui.Color
	HasColor                       bool // explicit transparent black differs from the default foreground
	Background, AllowSoftware      bool
	Enabled, Playing               bool
	Revision                       uint64
}

var effectPrograms struct {
	sync.Once
	word, composite, gaussian, root                   *shader.Shader
	wordUniforms, compositeUniforms, gaussianUniforms map[string]shader.Uniform
	err                                               error
}

func compileEffectPrograms() error {
	effectPrograms.Do(func() {
		for _, item := range []struct {
			source string
			target **shader.Shader
		}{{effectWordShader, &effectPrograms.word}, {effectCompositeShader, &effectPrograms.composite}, {effectGaussianShader, &effectPrograms.gaussian}, {effectRootShader, &effectPrograms.root}} {
			*item.target, effectPrograms.err = shader.Compile(item.source)
			if effectPrograms.err != nil {
				return
			}
		}
		bind := func(program *shader.Shader, names string) map[string]shader.Uniform {
			result := make(map[string]shader.Uniform)
			for _, name := range strings.Fields(names) {
				result[name] = program.Uniform(name)
			}
			return result
		}
		effectPrograms.wordUniforms = bind(effectPrograms.word, "RasterScale Padding WordSize WordFloat Mask Foreground BlackOffset UnderRect UnderOrigin OverRect OverOrigin Count GlyphRects GlyphOrigins GlyphBoxes Poses GlowRects GlowOrigins")
		effectPrograms.compositeUniforms = bind(effectPrograms.composite, "SourceRect DimRect AnnotationRect DimOrigin AnnotationOrigin RasterScale Foreground BlackOffset BrightOpacity DimOpacity SubOpacity Opacity Sigma Axis BlurOnly ContentScale ContentPivot ContentClip Kernel KernelRadius KernelTotal")
		effectPrograms.gaussianUniforms = bind(effectPrograms.gaussian, "SourceRect SourceOrigin Axis Sigma AlphaOnly RasterScale Opacity Kernel KernelRadius KernelTotal")
	})
	return effectPrograms.err
}

type effectTile struct {
	// rect is the tight, physical atlas rectangle. origin is the logical
	// coordinate of its top-left relative to the owning word/character.
	rect   ui.Rect
	origin ui.Point
}

type effectRaster struct {
	layout   *ui.TextLayout
	glyphs   []ui.Glyph
	colors   []referenceColorGlyph
	err      error
	baseline float32 // physical native layout baseline, before the CSS drawY offset
	x, y     float32
}

type effectTileRequest struct {
	width, height int
	origin        ui.Point
	paint         []effectRaster
	tile          *effectTile
	packed        image.Rectangle
	ink           image.Rectangle
	empty         bool
}

type effectCharacter struct {
	tile, glow              effectTile
	x, y, width, height, em float32
	index                   int
	glowSigma, lastSigma    float32
}

type effectWord struct {
	under, over effectTile
	characters  []effectCharacter
	float       motion.Float
	emphasis    *motion.Emphasis
}

type effectLine struct {
	inkArea                                     ui.Rect
	contentScale                                float32
	contentPivot                                ui.Point
	standalone                                  bool
	dimOnly                                     bool
	blackOffset                                 ui.Point
	geometry                                    *lineGeometry
	scale                                       float32
	words                                       []effectWord
	path                                        effectMaskPath
	white, black                                *ui.Bitmap
	whitePixels, blackPixels                    *image.RGBA
	dim, annotations                            effectTile
	glowX, glowY                                *ui.Canvas
	source, blurX                               *ui.Canvas
	area                                        ui.Rect
	width, height                               int
	bytes                                       int
	used                                        uint64
	pinned                                      bool
	wordValues, compositeValues, gaussianValues *shader.Values
	now                                         time.Time
	media                                       time.Duration
	synced                                      bool
	style                                       effectStyle
	animation                                   motion.LineAnimationClock
	cache                                       effectLineCache
}

// effectRenderer owns UI-thread resources. BeginFrame drops old pins; Prepare
// pins only visible lines. Cached bitmaps, all canvases, and frame references
// share the same byte/entry budget. Clear releases references, not GPU memory
// synchronously: the pinned mygo version exposes no Canvas.Close or Resize.
type effectRenderer struct {
	groups    []*effectGroup
	rootBytes int
	entries   []*effectLine
	bytes     int
	clock     uint64
}

func (r *effectRenderer) BeginFrame() {
	for _, group := range r.groups {
		group.pinned = false
	}
	for _, entry := range r.entries {
		entry.pinned = false
	}
}

func (r *effectRenderer) Clear() { *r = effectRenderer{} }

func (r *effectRenderer) Close() error { r.Clear(); return nil }

func effectGroupUsesRaster(group *effectGroup, identity *effectRasterIdentity) bool {
	if identity == nil {
		return false
	}
	for _, layer := range group.cache.source.layers[:group.cache.source.count] {
		if layer.identity == identity {
			return true
		}
	}
	return false
}

func (r *effectRenderer) rasterPinned(identity *effectRasterIdentity) bool {
	for _, group := range r.groups {
		if group.pinned && effectGroupUsesRaster(group, identity) {
			return true
		}
	}
	return false
}

// discardRasterGroups releases stale unpinned command scenes as well as their
// signatures. Canvas scenes retain their image operands, so evicting just the
// line would leave its atlases alive through an otherwise reusable group.
func (r *effectRenderer) discardRasterGroups(identity *effectRasterIdentity) {
	for i := 0; i < len(r.groups); {
		group := r.groups[i]
		if group.pinned || !effectGroupUsesRaster(group, identity) {
			i++
			continue
		}
		r.bytes -= group.bytes
		r.groups[i] = r.groups[len(r.groups)-1]
		r.groups[len(r.groups)-1] = nil
		r.groups = r.groups[:len(r.groups)-1]
	}
}

func (r *effectRenderer) makeRoom(extra int, line, group bool) bool {
	for r.bytes+extra > effectByteLimit || line && len(r.entries) >= effectEntryLimit || group && len(r.groups) >= effectEntryLimit {
		li, gi := -1, -1
		for i, e := range r.entries {
			if !e.pinned && !r.rasterPinned(e.cache.identity) && (li < 0 || e.used < r.entries[li].used) {
				li = i
			}
		}
		for i, e := range r.groups {
			if !e.pinned && (gi < 0 || e.used < r.groups[gi].used) {
				gi = i
			}
		}
		if line && len(r.entries) >= effectEntryLimit {
			gi = -1
		}
		if group && len(r.groups) >= effectEntryLimit {
			li = -1
		}
		if li < 0 && gi < 0 {
			return false
		}
		if li >= 0 && (gi < 0 || r.entries[li].used <= r.groups[gi].used) {
			r.discardRasterGroups(r.entries[li].cache.identity)
			r.bytes -= r.entries[li].bytes
			r.entries[li] = r.entries[len(r.entries)-1]
			r.entries[len(r.entries)-1] = nil
			r.entries = r.entries[:len(r.entries)-1]
		} else {
			r.bytes -= r.groups[gi].bytes
			r.groups[gi] = r.groups[len(r.groups)-1]
			r.groups[len(r.groups)-1] = nil
			r.groups = r.groups[:len(r.groups)-1]
		}
	}
	return true
}

// ReserveRoot budgets the element effect's content and backdrop textures along
// with our own canvases. It is an RGBA8 estimate, not GPU-driver/RSS telemetry.
func (r *effectRenderer) ReserveRoot(width, height, scale float32) error {
	if width == 0 || height == 0 {
		r.bytes -= r.rootBytes
		r.rootBytes = 0
		return nil
	}
	if !finitePositive(width) || !finitePositive(height) || !finitePositive(scale) || scale > effectScaleLimit {
		return errEffectLimit
	}
	if width*scale > effectSideLimit || height*scale > effectSideLimit {
		return errEffectLimit
	}
	w, h := effectBucket(int(math.Ceil(float64(width*scale)))), effectBucket(int(math.Ceil(float64(height*scale))))
	if w > effectSideLimit || h > effectSideLimit {
		return errEffectLimit
	}
	bytes := w * h * 8
	if !r.makeRoom(bytes-r.rootBytes, false, false) {
		return errEffectLimit
	}
	r.bytes += bytes - r.rootBytes
	r.rootBytes = bytes
	return nil
}

func (r *effectRenderer) Prepare(g *lineGeometry, scale float32) error {
	return r.prepare(g, scale, true, false)
}
func (r *effectRenderer) prepare(g *lineGeometry, scale float32, standalone, dimOnly bool) error {
	if g == nil || !finitePositive(scale) || scale > effectScaleLimit {
		return errEffectLimit
	}
	if !finitePositive(g.width) || !finitePositive(g.height) || g.width*scale > effectSideLimit || g.height*scale > effectSideLimit {
		return errEffectLimit
	}
	if err := compileEffectPrograms(); err != nil {
		return err
	}
	r.clock++
	for _, e := range r.entries {
		if e.geometry == g && e.scale == scale {
			e.used, e.pinned = r.clock, true
			if dimOnly != e.dimOnly {
				if r.rasterPinned(e.cache.identity) {
					return errEffectLimit
				}
				replacement, err := prepareEffectLine(g, scale, standalone, dimOnly)
				if err != nil {
					return err
				}
				r.discardRasterGroups(e.cache.identity)
				if !r.makeRoom(replacement.bytes-e.bytes, false, false) {
					return errEffectLimit
				}
				replacement.now, replacement.media, replacement.style, replacement.synced = e.now, e.media, e.style, e.synced
				replacement.animation = e.animation
				replacement.used, replacement.pinned = r.clock, true
				for i, old := range r.entries {
					if old == e {
						r.entries[i] = replacement
						break
					}
				}
				r.bytes += replacement.bytes - e.bytes
				return nil
			}
			if standalone != e.standalone {
				// A retained group scene still owns the old child image operands.
				// Never replace them behind an already pinned group in this frame;
				// normal preparation unpins all groups before changing line modes.
				if r.rasterPinned(e.cache.identity) {
					return errEffectLimit
				}
				area, width, height := effectLineBounds(e.inkArea, scale, standalone)
				before := effectCanvasBytes(e.width, e.height, e.standalone, e.dimOnly)
				after := effectCanvasBytes(width, height, standalone, e.dimOnly)
				if width > effectSideLimit || height > effectSideLimit {
					return errEffectLimit
				}
				r.discardRasterGroups(e.cache.identity)
				if !r.makeRoom(after-before, false, false) {
					return errEffectLimit
				}
				// A formerly grouped line needs the parent's Gaussian support when
				// it becomes a singleton after its background reaches zero alpha.
				// Reusing its tight group source would crop that blur at the edges.
				e.area, e.width, e.height = area, width, height
				e.source, e.blurX = nil, nil
				e.cache.sourceReady, e.cache.blurReady = false, false
				e.cache.revision++
				if !e.dimOnly {
					e.source = ui.NewCanvas(width, height)
				}
				if standalone {
					e.blurX = ui.NewCanvas(width, height)
				}
				e.bytes += after - before
				r.bytes += after - before
				e.standalone = standalone
			}
			return nil
		}
	}
	e, err := prepareEffectLine(g, scale, standalone, dimOnly)
	if err != nil {
		return err
	}
	if !r.makeRoom(e.bytes, true, false) {
		return errEffectLimit
	}
	e.used, e.pinned = r.clock, true
	r.entries = append(r.entries, e)
	r.bytes += e.bytes
	return nil
}

func effectBucket(value int) int { return max(64, (value+63)/64*64) }

func effectCanvasBytes(width, height int, standalone, dimOnly bool) int {
	planes := 0
	if !dimOnly {
		planes++
	}
	if standalone {
		planes++
	}
	return width * height * planes * 4
}

func (e *effectLine) sourceBitmap() *ui.Bitmap {
	if e.source == nil {
		return nil
	}
	return e.source.Bitmap()
}

func effectLineBounds(ink ui.Rect, scale float32, standalone bool) (ui.Rect, int, int) {
	margin := float32(2) / scale
	if standalone {
		margin += 3 * effectBlurLimit
	}
	x := float32(math.Floor(float64((ink.X-margin)*scale))) / scale
	y := float32(math.Floor(float64((ink.Y-margin)*scale))) / scale
	area := ui.Rect{X: x, Y: y, W: ink.X + ink.W + margin - x, H: ink.Y + ink.H + margin - y}
	return area, effectBucket(int(math.Ceil(float64(area.W * scale)))), effectBucket(int(math.Ceil(float64(area.H * scale))))
}

func effectFinite(values ...float32) bool {
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
	}
	return true
}

func validEffectLayer(layer effectGroupLayer) bool {
	return finitePositive(layer.Scale) && layer.Scale <= 4 && effectFinite(layer.X, layer.Y, layer.OriginX, layer.OriginY) && validEffectStyle(layer.Style) && layer.Style.Blur == 0
}

type effectGroupLayer struct {
	Geometry                      *lineGeometry
	X, Y, Scale, OriginX, OriginY float32
	Style                         effectStyle
}

// effectGroupBackground is the wrapper's own background, before its children.
// The complete wrapper then receives the same parent Gaussian and opacity.
type effectGroupBackground struct {
	Rect   ui.Rect
	Radius float32
	Color  ui.Color
}

func effectBackground(values []effectGroupBackground) (effectGroupBackground, error) {
	if len(values) > 1 {
		return effectGroupBackground{}, errEffectLimit
	}
	if len(values) == 0 {
		return effectGroupBackground{}, nil
	}
	value := values[0]
	if !effectFinite(value.Rect.X, value.Rect.Y, value.Rect.W, value.Rect.H, value.Radius) || value.Rect.W < 0 || value.Rect.H < 0 || value.Radius < 0 || value.Color.A > 0 && (value.Rect.W == 0 || value.Rect.H == 0) {
		return effectGroupBackground{}, errEffectLimit
	}
	return value, nil
}

type effectGroup struct {
	single               bool
	empty                bool
	key                  *lineGeometry
	scale                float32
	area                 ui.Rect
	width, height, bytes int
	source, blurX        *ui.Canvas
	values               *shader.Values
	used                 uint64
	pinned               bool
	cache                effectGroupCache
}

// PrepareGroup reserves the largest expanded content box the host supplies.
// Word/character overhang and the parent's maximum blur are added here. Child
// styles contain only child opacity; the parent's filter and opacity apply once.
func (r *effectRenderer) PrepareGroup(key *lineGeometry, layers []effectGroupLayer, scale float32, bounds ui.Rect, backgrounds ...effectGroupBackground) error {
	if key == nil || len(layers) == 0 || len(layers) > effectEntryLimit || !finitePositive(bounds.W) || !finitePositive(bounds.H) || !effectFinite(bounds.X, bounds.Y) || !finitePositive(scale) || scale > effectScaleLimit {
		return errEffectLimit
	}
	background, err := effectBackground(backgrounds)
	if err != nil {
		return err
	}
	hasBackground := background.Color.A > 0
	if hasBackground {
		if err := compileEffectPrograms(); err != nil {
			return err
		}
	}
	visible, singleIndex := effectVisibleLayers(layers)
	single := !hasBackground && visible == 1 && layers[singleIndex].Scale >= .95 && layers[singleIndex].Scale <= 1
	margin := float32(3*effectBlurLimit) + 2/scale
	for _, layer := range layers {
		if !validEffectLayer(layer) {
			return errEffectLimit
		}
		if layer.Style.Opacity == 0 {
			continue
		}
		dimOnly := layer.Geometry != nil && layer.Geometry.dynamic && layer.Style.BrightOpacity == 0
		if err := r.prepare(layer.Geometry, scale, single, dimOnly); err != nil {
			return err
		}
		for _, e := range r.entries {
			if e.geometry == layer.Geometry && e.scale == scale {
				overhang := max(-e.inkArea.X, -e.inkArea.Y, e.inkArea.X+e.inkArea.W-e.geometry.width, e.inkArea.Y+e.inkArea.H-e.geometry.height)
				margin = max(margin, float32(3*effectBlurLimit)+2/scale+overhang*max(float32(1.25), layer.Scale))
			}
		}
	}
	if single || visible == 0 && !hasBackground {
		r.clock++
		for _, group := range r.groups {
			if group.key == key && group.scale == scale && group.single == single && group.empty == (visible == 0) {
				group.used, group.pinned = r.clock, true
				return nil
			}
		}
		if !r.makeRoom(effectGroupCacheBytes, false, true) {
			return errEffectLimit
		}
		r.groups = append(r.groups, &effectGroup{key: key, scale: scale, single: single, empty: visible == 0, bytes: effectGroupCacheBytes, used: r.clock, pinned: true})
		r.bytes += effectGroupCacheBytes
		return nil
	}
	if hasBackground {
		left, top := min(bounds.X, background.Rect.X), min(bounds.Y, background.Rect.Y)
		right, bottom := max(bounds.X+bounds.W, background.Rect.X+background.Rect.W), max(bounds.Y+bounds.H, background.Rect.Y+background.Rect.H)
		bounds = ui.Rect{X: left, Y: top, W: right - left, H: bottom - top}
	}
	area := ui.Rect{X: float32(math.Floor(float64((bounds.X-margin)*scale))) / scale, Y: float32(math.Floor(float64((bounds.Y-margin)*scale))) / scale, W: bounds.W + 2*margin, H: bounds.H + 2*margin}
	w, h := effectBucket(int(math.Ceil(float64(area.W*scale)))), effectBucket(int(math.Ceil(float64(area.H*scale))))
	if w > effectSideLimit || h > effectSideLimit {
		return errEffectLimit
	}
	r.clock++
	for _, group := range r.groups {
		if group.key == key && group.scale == scale && !group.single && group.area == area {
			group.used, group.pinned = r.clock, true
			return nil
		}
	}
	bytes := w*h*8 + effectGroupCacheBytes
	if !r.makeRoom(bytes, false, true) {
		return errEffectLimit
	}
	group := &effectGroup{key: key, scale: scale, area: area, width: w, height: h, bytes: bytes, source: ui.NewCanvas(w, h), blurX: ui.NewCanvas(w, h), values: effectPrograms.gaussian.NewValues(), used: r.clock, pinned: true}
	r.groups = append(r.groups, group)
	r.bytes += bytes
	return nil
}

// A zero-alpha child contributes no pixels to source-over or the parent's
// Gaussian. Its persistent animation clock belongs to the scene, so omitting
// its raster resources neither pauses nor resets an offscreen/background line.
func effectVisibleLayers(layers []effectGroupLayer) (count, last int) {
	for i, layer := range layers {
		if layer.Style.Opacity > 0 {
			count++
			last = i
		}
	}
	return count, last
}

func (r *effectRenderer) preparedEntry(g *lineGeometry, scale float32) *effectLine {
	for _, e := range r.entries {
		if e.geometry == g && e.scale == scale && e.pinned {
			return e
		}
	}
	return nil
}

// drawEffectShader keeps the destination's fractional origin in the painter's
// transform. The pinned mygo Shader.Draw snaps rectangle edges to physical
// pixels before applying transforms; passing an animated X/Y directly loses
// subpixel motion even though imageSample itself uses bilinear filtering.
// The retained raster and shader coordinates stay local to that origin.
func drawEffectShader(program *shader.Shader, p *ui.Painter, box ui.Rect, options shader.Options) {
	p.Transform(ui.Translation(box.X, box.Y), 0, 0, func() {
		program.Draw(p, ui.Rect{W: box.W, H: box.H}, options)
	})
}

func (r *effectRenderer) DrawGroup(p *ui.Painter, key *lineGeometry, x, y float32, layers []effectGroupLayer, opacity, blur float32, backgrounds ...effectGroupBackground) error {
	if !validEffectStyle(effectStyle{Opacity: opacity, Blur: blur}) || !effectFinite(x, y) || len(layers) > effectEntryLimit {
		return errEffectLimit
	}
	if opacity == 0 {
		return nil
	}
	background, err := effectBackground(backgrounds)
	if err != nil {
		return err
	}
	for _, layer := range layers {
		if !validEffectLayer(layer) {
			return errEffectLimit
		}
	}
	var group *effectGroup
	for _, g := range r.groups {
		if g.key == key && g.scale == p.Scale() && g.pinned {
			group = g
			break
		}
	}
	if group == nil {
		return errEffectMissing
	}
	if group.empty {
		if visible, _ := effectVisibleLayers(layers); visible != 0 || background.Color.A != 0 {
			return errEffectMissing
		}
		return nil
	}
	if group.single {
		if background.Color.A != 0 {
			return errEffectMissing
		}
		visible, singleIndex := effectVisibleLayers(layers)
		if visible != 1 || layers[singleIndex].Scale < .95 || layers[singleIndex].Scale > 1 {
			return errEffectLimit
		}
		layer := layers[singleIndex]
		style := layer.Style
		style.Opacity *= opacity
		style.Blur = blur
		e := r.preparedEntry(layer.Geometry, group.scale)
		if e == nil {
			return errEffectMissing
		}
		return r.drawLine(p, layer.Geometry, x+layer.X, y+layer.Y, e.media, style, &layer)
	}
	var entries [effectEntryLimit]*effectLine
	for i, layer := range layers {
		if layer.Style.Opacity == 0 {
			continue
		}
		if !p.Accelerated() && !layer.Style.AllowSoftware {
			return errEffectSoftware
		}
		if !validEffectStyle(layer.Style) || !finitePositive(layer.Scale) {
			return errEffectLimit
		}
		e := r.preparedEntry(layer.Geometry, group.scale)
		if e == nil {
			return errEffectMissing
		}
		if e.dimOnly && layer.Style.BrightOpacity != 0 {
			return errEffectMissing
		}
		entries[i] = e
		if !e.dimOnly {
			e.drawSource(p, e.media, layer.Style)
		}
	}
	state := effectGroupState(layers, entries[:len(layers)], background)
	if !group.cache.sourceReady || group.cache.source != state {
		group.source.Draw(p, func(cp *ui.Painter) {
			cp.Transform(ui.Translation(-group.area.X, -group.area.Y).Then(ui.Scaling(group.scale, group.scale)), 0, 0, func() {
				if background.Color.A != 0 {
					cp.Fill(background.Rect, background.Color, background.Radius)
				}
				for i, layer := range layers {
					if layer.Style.Opacity == 0 {
						continue
					}
					e := entries[i]
					e.contentScale, e.contentPivot = 1, ui.Point{}
					style := layer.Style
					if !e.geometry.dynamic {
						style.BrightOpacity, style.DimOpacity = 1, 0
					}
					e.setComposite(style, 0, e.scale, style.Opacity, ui.Point{}, false)
					box := ui.Rect{X: layer.X + e.area.X, Y: layer.Y + e.area.Y, W: float32(e.width) / e.scale, H: float32(e.height) / e.scale}
					cp.Transform(ui.Scaling(layer.Scale, layer.Scale), layer.OriginX, layer.OriginY, func() {
						drawEffectShader(effectPrograms.composite, cp, box, shader.Options{Images: [4]*ui.Bitmap{e.sourceBitmap(), e.white, e.black}, Values: e.compositeValues})
					})
				}
			})
		})
		group.cache.source, group.cache.sourceReady = state, true
		group.cache.revision++
	}
	box := ui.Rect{X: x + group.area.X, Y: y + group.area.Y, W: float32(group.width) / group.scale, H: float32(group.height) / group.scale}
	source := group.source.Bitmap()
	rect := ui.Rect{W: float32(group.width), H: float32(group.height)}
	if blur > 0 {
		sigma := blur * group.scale
		if !group.cache.blurReady || group.cache.blurRevision != group.cache.revision || group.cache.sigma != sigma {
			group.blurX.Draw(p, func(cp *ui.Painter) {
				effectGaussian(cp, rect, source, rect, ui.Point{}, ui.Point{X: 1}, sigma, 1, 1, false, group.values, &group.cache.gaussianKernel)
			})
			group.cache.blurRevision, group.cache.sigma, group.cache.blurReady = group.cache.revision, sigma, true
		}
		// The second, linear convolution writes directly into the parent target.
		// Its alpha is applied after both axes, avoiding an otherwise redundant
		// full-group RGBA8 intermediate while retaining the complete filter.
		effectGaussian(p, box, group.blurX.Bitmap(), rect, ui.Point{}, ui.Point{Y: 1}, sigma, group.scale, opacity, false, group.values, &group.cache.gaussianKernel)
		return nil
	}
	effectGaussian(p, box, source, rect, ui.Point{}, ui.Point{}, 0, group.scale, opacity, false, group.values, &group.cache.gaussianKernel)
	return nil
}

func effectPhysicalFont(font ui.Font, scale float32) ui.Font {
	font.Size *= scale
	return font
}

func effectPhysicalRaster(text string, font ui.Font, scale, x, y float32, providers ...*referenceFontProvider) effectRaster {
	physicalFont := effectPhysicalFont(font, scale)
	var provider *referenceFontProvider
	if len(providers) != 0 {
		provider = providers[0]
	}
	shape := provider.Shape(text, physicalFont)
	return effectRasterFromShape(text, physicalFont, x, y, shape)
}

func effectRasterFromShape(text string, physicalFont ui.Font, x, y float32, shape referenceTextShape) effectRaster {
	// Row-only layout supplies the existing native baseline, while glyph
	// shaping may retain a full paragraph's script and joining context.
	layout := ui.ShapeText(text, physicalFont, 0)
	baseline := float32(0)
	if rows := layout.Lines(); len(rows) != 0 {
		baseline = rows[0].Baseline
	}
	return effectRaster{layout: layout, glyphs: shape.Glyphs, colors: shape.Colors, err: shape.Err, baseline: baseline, x: x, y: y}
}

func effectRowRasters(rows []cssTextRow, text string, font ui.Font, scale, x, y, drawY float32, providers ...*referenceFontProvider) []effectRaster {
	if len(rows) == 0 {
		return []effectRaster{effectPhysicalRaster(text, font, scale, x, y+drawY, providers...)}
	}
	paints := make([]effectRaster, 0, len(rows))
	for _, row := range rows {
		if row.context != "" {
			var provider *referenceFontProvider
			if len(providers) != 0 {
				provider = providers[0]
			}
			physicalFont := effectPhysicalFont(font, scale)
			shape := provider.ShapeRange(row.context, row.startRune, row.endRune, physicalFont)
			paints = append(paints, effectRasterFromShape(row.text, physicalFont, x+row.x, y+row.y+row.drawY, shape))
			continue
		}
		paints = append(paints, effectPhysicalRaster(row.text, font, scale, x+row.x, y+row.y+row.drawY, providers...))
	}
	return paints
}

func effectPack(requests []*effectTileRequest) (int, int, error) {
	var largest int
	for _, request := range requests {
		if request.width < 1 || request.height < 1 || request.width > effectSideLimit || request.height > effectSideLimit {
			return 0, 0, errEffectLimit
		}
		largest = max(largest, request.width+2)
	}
	minimum := effectBucket(largest)
	if minimum > effectSideLimit {
		return 0, 0, errEffectLimit
	}
	// Static clones and translations can be much wider than individual glyphs.
	// Height-ordered shelves avoid stranding them below a mostly empty glyph
	// shelf. At most 64 candidate widths are checked during cold preparation;
	// selecting the smallest bucketed area also accounts for physical padding.
	ordered := append([]*effectTileRequest(nil), requests...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].height != ordered[j].height {
			return ordered[i].height > ordered[j].height
		}
		return ordered[i].width > ordered[j].width
	})
	pack := func(width int, place bool) int {
		x, y, shelf := 1, 1, 0
		for _, request := range ordered {
			if x+request.width+1 > width {
				x, y, shelf = 1, y+shelf+2, 0
			}
			if place {
				request.packed = image.Rect(x, y, x+request.width, y+request.height)
			}
			x += request.width + 2
			shelf = max(shelf, request.height)
		}
		return effectBucket(y + shelf + 1)
	}
	width, height, best := 0, 0, effectByteLimit
	for candidate := minimum; candidate <= effectSideLimit; candidate += 64 {
		h := pack(candidate, false)
		if h <= effectSideLimit && candidate*h < best {
			width, height, best = candidate, h, candidate*h
		}
	}
	if width == 0 || width*height*8 > effectByteLimit {
		return 0, 0, errEffectLimit
	}
	pack(width, true)
	return width, height, nil
}

// RootEffect implements outer plus-lighter. The host background belongs
// outside the transparent lyrics element receiving this effect.
func RootEffect() (ui.Effect, error) {
	if err := compileEffectPrograms(); err != nil {
		return nil, err
	}
	return effectPrograms.root.Effect(shader.Options{}), nil
}

func effectInk(img *image.RGBA, rect image.Rectangle) image.Rectangle {
	ink := image.Rectangle{}
	found := false
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] == 0 {
				continue
			}
			pixel := image.Rect(x, y, x+1, y+1)
			if !found {
				ink, found = pixel, true
			} else {
				ink = ink.Union(pixel)
			}
		}
	}
	return ink
}

// Color glyphs keep the source text's fractional horizontal position in their
// outline/gradient raster, avoiding a second bilinear image translation. The
// separately probed Chrome baseline snaps vertically to the physical grid;
// HarfBuzz's per-glyph offsets remain untouched. The borrowed glyph slice is
// immutable, including when the same shape supplies the white and black atlas.
func effectColorGlyphPhase(raster effectRaster) ([]referenceColorGlyph, image.Point) {
	x := float32(math.Floor(float64(raster.x)))
	y := int(math.Floor(float64(raster.y+raster.baseline) + .5))
	phase := raster.x - x
	if phase == 0 {
		return raster.colors, image.Pt(int(x), y)
	}
	glyphs := append([]referenceColorGlyph(nil), raster.colors...)
	for i := range glyphs {
		glyphs[i].X += phase
	}
	return glyphs, image.Pt(int(x), y)
}

// Atlas preparation renders one bounded tile at a time. Packing every padded
// word before trimming wastes a large temporary surface even for empty slots.
// Raw tile pairs, tight tile pairs, packed planes and the color crop have a
// conservative 32 MiB base cold-plane bound, separate from retained textures.
// Optional color-font preparation adds at most 32 MiB of working planes; the
// combined cold-plane allowance is 64 MiB, excluding the separately admitted
// parsed font and upstream UI/font runtime caches.
func effectAtlas(requests []*effectTileRequest, scale float32) (*image.RGBA, *image.RGBA, ui.Point, error) {
	type pair struct{ white, black *image.RGBA }
	tiles := make([]pair, len(requests))
	pixels := 0
	for i, request := range requests {
		w, h := request.width, request.height
		if w < 1 || h < 1 || w > effectSideLimit || h > effectSideLimit || w*h > 1<<20 {
			return nil, nil, ui.Point{}, errEffectLimit
		}
		paint := func(foreground ui.Color) (*image.RGBA, error) {
			// Color paints are rasterized before entering ui.Render: each call
			// is cold, errors propagate, and nested rendering does not happen in
			// a Painter callback. Keep at most one color bitmap per input row.
			colorOrigins := make([]image.Point, len(request.paint))
			colorSizes := make([]image.Point, len(request.paint))
			colorBitmaps := make([]*ui.Bitmap, len(request.paint))
			var colorPixels int64
			for j, raster := range request.paint {
				if raster.err != nil {
					return nil, raster.err
				}
				if len(raster.colors) == 0 {
					continue
				}
				glyphs, baseline := effectColorGlyphPhase(raster)
				colorRaster, err := rasterEmojiColorGlyphsWithin(glyphs, foreground, (1<<20)-colorPixels)
				if err != nil {
					return nil, err
				}
				if colorRaster.Pixels != nil {
					colorPixels += int64(colorRaster.Pixels.Rect.Dx()) * int64(colorRaster.Pixels.Rect.Dy())
					colorOrigins[j], colorSizes[j] = colorRaster.Origin.Add(baseline), colorRaster.Pixels.Rect.Size()
					colorBitmaps[j] = ui.NewBitmap(colorRaster.Pixels)
					// NewBitmap owns converted CPU pixels. Keep no second source
					// image during the rest of the atlas request's preparation.
					colorRaster.Pixels = nil
				}
			}
			return ui.Render(func(c *ui.Context) {
				c.Root().Background(ui.Transparent)
				ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
					for j, raster := range request.paint {
						if raster.glyphs != nil {
							p.Glyphs(raster.glyphs, raster.x, raster.y+raster.baseline, foreground)
						} else if len(raster.colors) == 0 && raster.layout != nil {
							p.TextLayout(raster.layout, raster.x, raster.y, foreground)
						}
						if bitmap := colorBitmaps[j]; bitmap != nil {
							rect := ui.Rect{W: float32(colorSizes[j].X), H: float32(colorSizes[j].Y)}
							box := rect
							box.X, box.Y = float32(colorOrigins[j].X), float32(colorOrigins[j].Y)
							p.ImageRegion(bitmap, rect, box, ui.ImageOptions{})
						}
					}
				})
			}, w, h, 1), nil
		}
		white, err := paint(ui.RGB(255, 255, 255))
		if err != nil {
			return nil, nil, ui.Point{}, err
		}
		black, err := paint(ui.RGB(0, 0, 0))
		if err != nil {
			return nil, nil, ui.Point{}, err
		}
		ink := effectInk(white, white.Rect)
		if ink.Empty() {
			request.empty = true
			ink = image.Rect(0, 0, 1, 1)
		}
		request.origin.X += float32(ink.Min.X) / scale
		request.origin.Y += float32(ink.Min.Y) / scale
		request.width, request.height = ink.Dx(), ink.Dy()
		pixels += ink.Dx() * ink.Dy()
		if pixels > 1<<20 {
			return nil, nil, ui.Point{}, errEffectLimit
		}
		tiles[i] = pair{image.NewRGBA(image.Rect(0, 0, ink.Dx(), ink.Dy())), image.NewRGBA(image.Rect(0, 0, ink.Dx(), ink.Dy()))}
		draw.Draw(tiles[i].white, tiles[i].white.Rect, white, ink.Min, draw.Src)
		draw.Draw(tiles[i].black, tiles[i].black.Rect, black, ink.Min, draw.Src)
	}
	width, height, err := effectPack(requests)
	if err != nil {
		return nil, nil, ui.Point{}, err
	}
	if width*height > effectPreparePixels {
		return nil, nil, ui.Point{}, errEffectLimit
	}
	w, b := image.NewRGBA(image.Rect(0, 0, width, height)), image.NewRGBA(image.Rect(0, 0, width, height))
	for i, request := range requests {
		draw.Draw(w, request.packed, tiles[i].white, image.Point{}, draw.Src)
		draw.Draw(b, request.packed, tiles[i].black, image.Point{}, draw.Src)
		request.tile.rect = ui.Rect{X: float32(request.packed.Min.X), Y: float32(request.packed.Min.Y), W: float32(request.width), H: float32(request.height)}
		request.tile.origin = request.origin
		if request.empty {
			request.tile.rect = ui.Rect{}
			request.tile.origin = ui.Point{}
		}
	}
	ink := image.Rectangle{}
	for y := 0; y < b.Rect.Dy(); y++ {
		for x := 0; x < b.Rect.Dx(); x++ {
			p := b.RGBAAt(x, y)
			if p.R|p.G|p.B != 0 {
				r := image.Rect(x, y, x+1, y+1)
				if ink.Empty() {
					ink = r
				} else {
					ink = ink.Union(r)
				}
			}
		}
	}
	if ink.Empty() {
		return w, image.NewRGBA(image.Rect(0, 0, 1, 1)), ui.Point{}, nil
	}
	ink = ink.Inset(-1).Intersect(b.Rect)
	compact := image.NewRGBA(image.Rect(0, 0, ink.Dx(), ink.Dy()))
	draw.Draw(compact, compact.Rect, b, ink.Min, draw.Src)
	return w, compact, ui.Point{X: float32(ink.Min.X), Y: float32(ink.Min.Y)}, nil
}

func prepareEffectLine(g *lineGeometry, scale float32, standalone, dimOnly bool) (*effectLine, error) {
	entry := &effectLine{standalone: standalone, dimOnly: dimOnly, geometry: g, scale: scale, words: make([]effectWord, len(g.blocks)), path: newEffectMaskPath(g)}
	var requests []*effectTileRequest
	add := func(tile *effectTile, box ui.Rect, paints []effectRaster) {
		if len(paints) == 0 {
			*tile = effectTile{}
			return
		}
		request := &effectTileRequest{width: max(1, int(math.Ceil(float64(box.W*scale)))), height: max(1, int(math.Ceil(float64(box.H*scale)))), origin: ui.Point{X: box.X, Y: box.Y}, tile: tile}
		for _, paint := range paints {
			paint.x = (paint.x - box.X) * scale
			paint.y = (paint.y - box.Y) * scale
			request.paint = append(request.paint, paint)
		}
		requests = append(requests, request)
	}
	var dimPaint []effectRaster
	maxEm := max(float32(10), g.font.Size)
	for i := range g.blocks {
		block, word := &g.blocks[i], &entry.words[i]
		maxEm = max(maxEm, block.fontSize)
		if len(block.characters) > effectCharacterLimit {
			return nil, errEffectLimit
		}
		word.float = motion.NewFloat(block.start, block.end, false)
		if block.shouldEmphasize && block.emphasisGroup >= 0 && block.emphasisGroup < len(g.groups) {
			group := g.groups[block.emphasisGroup]
			word.emphasis = motion.NewEmphasis(motion.EmphasisOptions{Start: group.start, End: group.end, Characters: group.characterCount, RubyCharacters: group.anchorCharCount, Last: group.isLastWord})
		}
		box := ui.Rect{X: -block.padding, Y: -block.padding, W: max(1, block.width+2*block.padding), H: max(1, block.height+2*block.padding)}
		var under, over []effectRaster
		for _, ruby := range block.rubyParts {
			under = append(under, effectRowRasters(ruby.rows, ruby.text, ruby.font, scale, ruby.x, ruby.y, ruby.drawY, g.fontProvider)...)
		}
		if len(block.characters) == 0 {
			under = append(under, effectRowRasters(block.bodyRows, block.text, block.font, scale, block.bodyX, block.bodyY, block.bodyDrawY, g.fontProvider)...)
		}
		if block.romanLayout != nil {
			over = append(over, effectRowRasters(block.romanRows, block.romanText, block.romanFont, scale, block.romanX, block.romanY, block.romanDrawY, g.fontProvider)...)
		}
		if !dimOnly {
			add(&word.under, box, under)
			add(&word.over, box, over)
		}
		for _, paint := range under {
			paint.x, paint.y = paint.x+block.x, paint.y+block.y
			dimPaint = append(dimPaint, paint)
		}
		word.characters = make([]effectCharacter, len(block.characters))
		for j, character := range block.characters {
			char := &word.characters[j]
			char.x, char.y, char.width, char.height, char.em, char.index = character.x, character.y, character.width, character.height, character.fontSize, character.index
			char.lastSigma = -1
			if word.emphasis != nil && !dimOnly {
				char.glowSigma = float32(word.emphasis.ShadowBlurEm()) * char.em * scale * .5
			}
			if char.glowSigma > effectSigmaLimit {
				return nil, errEffectLimit
			}
			paint := effectPhysicalRaster(character.text, character.font, scale, 0, character.drawY, g.fontProvider)
			pad := max(float32(1), character.fontSize)
			if !dimOnly {
				add(&char.tile, ui.Rect{X: -pad, Y: -pad, W: max(1, character.width+2*pad), H: max(1, character.height+2*pad)}, []effectRaster{paint})
			}
			paint.x, paint.y = paint.x+block.x+character.x, paint.y+block.y+character.y
			dimPaint = append(dimPaint, paint)
		}
		for _, paint := range over {
			paint.x, paint.y = paint.x+block.x, paint.y+block.y
			dimPaint = append(dimPaint, paint)
		}
	}
	// The full static clone retains the original's separate grapheme boxes.
	fullBox := ui.Rect{X: -maxEm, Y: -maxEm, W: max(1, g.width+2*maxEm), H: max(1, g.height+2*maxEm)}
	add(&entry.dim, fullBox, dimPaint)
	var annotations []effectRaster
	for _, annotation := range g.annotations {
		if len(annotation.rows) > 0 {
			annotations = append(annotations, effectRowRasters(annotation.rows, annotation.text, annotation.font, scale, 0, annotation.boxY, 0, g.fontProvider)...)
			continue
		}
		units := utf16.Encode([]rune(annotation.text))
		for _, line := range annotation.layout.Lines() {
			text := string(utf16.Decode(units[line.Start:line.End]))
			annotations = append(annotations, effectPhysicalRaster(text, annotation.font, scale, line.Rect.X, annotation.y+line.Rect.Y, g.fontProvider))
		}
	}
	add(&entry.annotations, fullBox, annotations)
	var err error
	entry.whitePixels, entry.blackPixels, entry.blackOffset, err = effectAtlas(requests, scale)
	if err != nil {
		return nil, err
	}
	entry.white, entry.black = ui.NewBitmap(entry.whitePixels), ui.NewBitmap(entry.blackPixels)
	// Both retained CPU source pixels and their GPU upload estimates count.
	entry.bytes = (entry.whitePixels.Rect.Dx()*entry.whitePixels.Rect.Dy() + entry.blackPixels.Rect.Dx()*entry.blackPixels.Rect.Dy()) * 8
	var glowRequests []*effectTileRequest
	for i := range entry.words {
		for j := range entry.words[i].characters {
			char := &entry.words[i].characters[j]
			if char.glowSigma <= 0 {
				continue
			}
			pad := float32(math.Ceil(float64(3*char.glowSigma))) + 2
			char.glow.origin = ui.Point{X: char.tile.origin.X - pad/scale, Y: char.tile.origin.Y - pad/scale}
			glowRequests = append(glowRequests, &effectTileRequest{width: int(char.tile.rect.W + 2*pad), height: int(char.tile.rect.H + 2*pad), tile: &char.glow})
		}
	}
	if len(glowRequests) > 0 {
		w, h, err := effectPack(glowRequests)
		if err != nil {
			return nil, err
		}
		for _, request := range glowRequests {
			request.tile.rect = ui.Rect{X: float32(request.packed.Min.X), Y: float32(request.packed.Min.Y), W: float32(request.width), H: float32(request.height)}
		}
		entry.glowX, entry.glowY = ui.NewCanvas(w, h), ui.NewCanvas(w, h)
		entry.bytes += w * h * 8
	}
	// Tight raster extents plus proven motion bounds, clipped by each word's
	// own 1em mask, avoid retaining a blank 1em border around the whole line.
	minX, minY, maxX, maxY := float32(0), float32(0), float32(1), float32(1)
	include := func(x, y, w, h float32) {
		minX = min(minX, x)
		minY = min(minY, y)
		maxX = max(maxX, x+w)
		maxY = max(maxY, y+h)
	}
	includeTile := func(tile effectTile, x, y float32) {
		if tile.rect.W > 0 {
			include(x+tile.origin.X, y+tile.origin.Y, tile.rect.W/scale, tile.rect.H/scale)
		}
	}
	includeTile(entry.dim, 0, 0)
	includeTile(entry.annotations, 0, 0)
	for i, block := range g.blocks {
		if dimOnly {
			break
		}
		word := &entry.words[i]
		if len(word.characters) == 0 {
			for _, tile := range []effectTile{word.under, word.over} {
				if tile.rect.W > 0 {
					include(block.x+tile.origin.X, block.y+tile.origin.Y-block.fontSize*.1, tile.rect.W/scale, tile.rect.H/scale+block.fontSize*.1)
				}
			}
			continue
		}
		includeTile(word.under, block.x, block.y)
		includeTile(word.over, block.x, block.y)
		for _, char := range word.characters {
			if word.emphasis == nil {
				continue
			}
			group := g.groups[block.emphasisGroup]
			duration := word.emphasis.CharEnd(0, false) - group.start
			charStart := word.emphasis.CharEnd(char.index, false) - duration
			peak := word.emphasis.Character(charStart+duration/2, char.index)
			floatDuration := word.emphasis.CharEnd(0, true) - group.start
			minFloat := float64(0)
			for j := 0; j <= 32; j++ {
				minFloat = min(minFloat, word.emphasis.Character(charStart+time.Duration(float64(floatDuration)*float64(j)/32), char.index).FloatYEm*2)
			}
			tile := char.tile
			if char.glow.rect.W > 0 {
				tile = char.glow
			}
			cx, cy := char.x+char.width*.5, char.y+char.height*.5
			for _, z := range []float32{1, float32(peak.Scale)} {
				for _, dx := range []float32{0, float32(peak.TranslateXEm) * char.em} {
					for _, dy := range []float32{0, float32(peak.TranslateYEm+minFloat) * char.em} {
						x0 := cx + z*(char.x+tile.origin.X-cx+dx)
						y0 := cy + z*(char.y+tile.origin.Y-cy+dy)
						x1, y1 := x0+z*tile.rect.W/scale, y0+z*tile.rect.H/scale
						x0, y0 = max(-block.padding, x0), max(-block.padding, y0)
						x1, y1 = min(block.width+block.padding, x1), min(block.height+block.padding, y1)
						if x1 > x0 && y1 > y0 {
							include(block.x+x0, block.y+y0, x1-x0, y1-y0)
						}
					}
				}
			}
		}
	}
	entry.inkArea = ui.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
	entry.area, entry.width, entry.height = effectLineBounds(entry.inkArea, scale, standalone)
	if entry.width > effectSideLimit || entry.height > effectSideLimit {
		return nil, errEffectLimit
	}
	canvasBytes := effectCanvasBytes(entry.width, entry.height, standalone, dimOnly)
	entry.bytes += canvasBytes + len(entry.path.frames)*24 + len(entry.path.before)*8 + len(entry.words)*128 + effectLineCacheBytes
	if entry.bytes > effectByteLimit {
		return nil, errEffectLimit
	}
	if !dimOnly {
		entry.source = ui.NewCanvas(entry.width, entry.height)
	}
	if standalone {
		entry.blurX = ui.NewCanvas(entry.width, entry.height)
	}
	entry.wordValues = effectPrograms.word.NewValues()
	entry.compositeValues = effectPrograms.composite.NewValues()
	entry.gaussianValues = effectPrograms.gaussian.NewValues()
	// NewBitmap converts the source and keeps its converted CPU pixels. Drop
	// our preparation images; retaining them too would add another atlas copy
	// beyond the counted CPU bitmap + GPU upload estimate.
	entry.whitePixels, entry.blackPixels = nil, nil
	entry.initPaintCache()
	return entry, nil
}

// SyncFrame owns animation state changes. The host calls it once for its
// timestamp/revision before painting; repeated paints never consume an event.
func (r *effectRenderer) SyncFrame(g *lineGeometry, now time.Time, position time.Duration, style effectStyle) error {
	if g == nil || !validEffectStyle(style) {
		return errEffectLimit
	}
	if style.Clock != nil {
		style.Clock.Sync(now, position, g.start, g.end, style.Enabled, style.Playing, style.Revision)
	}
	found := false
	for _, entry := range r.entries {
		if entry.geometry == g && entry.pinned {
			entry.now, entry.media, entry.style, entry.synced = now, position, style, true
			if style.Clock != nil {
				entry.animation = *style.Clock
			} else {
				entry.animation.Sync(now, position, g.start, g.end, style.Enabled, style.Playing, style.Revision)
			}
			found = true
		}
	}
	if !found && style.Opacity != 0 {
		return errEffectMissing
	}
	return nil
}

func effectRect(v *shader.Values, u shader.Uniform, r ui.Rect) { v.Vec4(u, r.X, r.Y, r.W, r.H) }

func effectGaussian(p *ui.Painter, dst ui.Rect, source *ui.Bitmap, rect ui.Rect, origin, axis ui.Point, sigma, scale, opacity float32, alpha bool, values *shader.Values, kernels ...*effectKernel) {
	u := effectPrograms.gaussianUniforms
	effectRect(values, u["SourceRect"], rect)
	values.Vec2(u["SourceOrigin"], origin.X, origin.Y).Vec2(u["Axis"], axis.X, axis.Y).Float(u["Sigma"], sigma).Float(u["RasterScale"], scale).Float(u["Opacity"], opacity)
	flag := float32(0)
	if alpha {
		flag = 1
	}
	values.Float(u["AlphaOnly"], flag)
	var kernel *effectKernel
	if len(kernels) > 0 {
		kernel = kernels[0]
	}
	setEffectKernel(values, u, sigma, kernel)
	drawEffectShader(effectPrograms.gaussian, p, dst, shader.Options{Images: [4]*ui.Bitmap{source}, Values: values})
}

func (entry *effectLine) updateGlows(p *ui.Painter, position time.Duration) {
	if entry.glowX == nil {
		return
	}
	changed := false
	for i := range entry.words {
		word := &entry.words[i]
		if word.emphasis == nil {
			continue
		}
		for j := range word.characters {
			char := &word.characters[j]
			at := entry.animationPosition(position, motion.EmphasisGlowAnimation, word.emphasis.CharEnd(char.index, false))
			sigma := float32(word.emphasis.Character(at, char.index).ShadowBlurEm) * char.em * entry.scale * .5
			if sigma != char.lastSigma {
				changed = true
				char.lastSigma = sigma
			}
		}
	}
	if !changed {
		return
	}
	entry.glowX.Draw(p, func(cp *ui.Painter) {
		for i := range entry.words {
			for j := range entry.words[i].characters {
				char := &entry.words[i].characters[j]
				if char.glow.rect.W <= 0 {
					continue
				}
				origin := ui.Point{X: char.tile.rect.X + (char.glow.origin.X-char.tile.origin.X)*entry.scale, Y: char.tile.rect.Y + (char.glow.origin.Y-char.tile.origin.Y)*entry.scale}
				effectGaussian(cp, char.glow.rect, entry.white, char.tile.rect, origin, ui.Point{X: 1}, char.lastSigma, 1, 1, true, entry.gaussianValues, &entry.cache.gaussianKernel)
			}
		}
	})
	entry.glowY.Draw(p, func(cp *ui.Painter) {
		for i := range entry.words {
			for j := range entry.words[i].characters {
				char := &entry.words[i].characters[j]
				if char.glow.rect.W <= 0 {
					continue
				}
				effectGaussian(cp, char.glow.rect, entry.glowX.Bitmap(), char.glow.rect, ui.Point{X: char.glow.rect.X, Y: char.glow.rect.Y}, ui.Point{Y: 1}, char.lastSigma, 1, 1, true, entry.gaussianValues, &entry.cache.gaussianKernel)
			}
		}
	})
}

func (entry *effectLine) drawWords(p *ui.Painter, position time.Duration, style effectStyle) {
	u, v := effectPrograms.wordUniforms, entry.wordValues
	var glyphRects, glyphOrigins, glyphBoxes, poses, glowRects, glowOrigins [effectCharacterLimit * 4]float32
	color := style.Color
	if !style.HasColor && color == (ui.Color{}) {
		color = ui.RGB(255, 255, 255)
	}
	v.Float(u["RasterScale"], entry.scale).Color(u["Foreground"], color)
	v.Vec2(u["BlackOffset"], entry.blackOffset.X, entry.blackOffset.Y)
	for i, block := range entry.geometry.blocks {
		word := &entry.words[i]
		v.Float(u["Padding"], block.padding).Vec2(u["WordSize"], block.width, block.height)
		floatY := float32(0)
		if word.emphasis == nil && entry.geometry.dynamic {
			at := entry.animationPosition(position, motion.FloatWordAnimation, word.float.End())
			floatY = float32(word.float.Sample(at)) * block.fontSize
			if style.Background {
				floatY *= 2
			}
		}
		v.Float(u["WordFloat"], floatY)
		fadeWidth := style.WordFadeWidth
		if fadeWidth == 0 {
			fadeWidth = .5
		}
		fade := effectMaskHeight(block) * max(.0001, fadeWidth)
		maskPosition := entry.path.position(i, effectMaskWidth(block), block.padding, fade, entry.animationPosition(position, motion.MaskAnimation, entry.geometry.end))
		left := maskPosition + block.width + block.padding
		fade *= (block.width + 2*block.padding) / max(.00001, effectMaskWidth(block)+2*block.padding)
		bright, dark := style.BrightMaskAlpha, style.DarkMaskAlpha
		if !entry.geometry.dynamic {
			bright, dark = 1, 1
		}
		v.Vec4(u["Mask"], left, left+fade, bright, dark)
		effectRect(v, u["UnderRect"], word.under.rect)
		v.Vec2(u["UnderOrigin"], word.under.origin.X, word.under.origin.Y)
		effectRect(v, u["OverRect"], word.over.rect)
		v.Vec2(u["OverOrigin"], word.over.origin.X, word.over.origin.Y)
		v.Int(u["Count"], int32(len(word.characters)))
		for j, char := range word.characters {
			at := 4 * j
			copy(glyphRects[at:at+4], []float32{char.tile.rect.X, char.tile.rect.Y, char.tile.rect.W, char.tile.rect.H})
			copy(glyphOrigins[at:at+4], []float32{char.tile.origin.X, char.tile.origin.Y, 0, 0})
			copy(glyphBoxes[at:at+4], []float32{char.x, char.y, char.width, char.height})
			pose := motion.CharacterPose{Scale: 1}
			if word.emphasis != nil {
				glowAt := entry.animationPosition(position, motion.EmphasisGlowAnimation, word.emphasis.CharEnd(char.index, false))
				floatAt := entry.animationPosition(position, motion.EmphasisFloatAnimation, word.emphasis.CharEnd(char.index, true))
				pose = word.emphasis.Character(glowAt, char.index)
				pose.FloatYEm = word.emphasis.Character(floatAt, char.index).FloatYEm
			}
			floatY := pose.FloatYEm
			if style.Background {
				floatY *= 2
			}
			copy(poses[at:at+4], []float32{float32(pose.Scale), float32(pose.TranslateXEm) * char.em, float32(pose.TranslateYEm+floatY) * char.em, 0})
			copy(glowRects[at:at+4], []float32{char.glow.rect.X, char.glow.rect.Y, char.glow.rect.W, char.glow.rect.H})
			copy(glowOrigins[at:at+4], []float32{char.glow.origin.X, char.glow.origin.Y, float32(pose.ShadowAlpha), 0})
		}
		// Count bounds every character-array read in the shader. Leave unused
		// tails alone rather than copying six full 64-character arrays per word.
		count := 4 * len(word.characters)
		v.Floats(u["GlyphRects"], glyphRects[:count]...).Floats(u["GlyphOrigins"], glyphOrigins[:count]...).Floats(u["GlyphBoxes"], glyphBoxes[:count]...).Floats(u["Poses"], poses[:count]...).Floats(u["GlowRects"], glowRects[:count]...).Floats(u["GlowOrigins"], glowOrigins[:count]...)
		rect := ui.Rect{X: (block.x - block.padding - entry.area.X) * entry.scale, Y: (block.y - block.padding - entry.area.Y) * entry.scale, W: (block.width + 2*block.padding) * entry.scale, H: (block.height + 2*block.padding) * entry.scale}
		// A floating word moves its entire mask box; include its travel in the
		// draw primitive, while the fragment maps back to the original box.
		if floatY < 0 {
			rect.Y += floatY * entry.scale
			rect.H -= floatY * entry.scale
			v.Float(u["WordFloat"], 0)
		}
		images := [4]*ui.Bitmap{entry.white, entry.black}
		if entry.glowY != nil {
			images[2] = entry.glowY.Bitmap()
		}
		drawEffectShader(effectPrograms.word, p, rect, shader.Options{Images: images, Values: v})
	}
}

func (entry *effectLine) setComposite(style effectStyle, sigma, scale, opacity float32, axis ui.Point, blurOnly bool) {
	u, v := effectPrograms.compositeUniforms, entry.compositeValues
	effectRect(v, u["SourceRect"], ui.Rect{W: float32(entry.width), H: float32(entry.height)})
	effectRect(v, u["DimRect"], entry.dim.rect)
	effectRect(v, u["AnnotationRect"], entry.annotations.rect)
	v.Vec2(u["DimOrigin"], (entry.dim.origin.X-entry.area.X)*entry.scale, (entry.dim.origin.Y-entry.area.Y)*entry.scale)
	v.Vec2(u["AnnotationOrigin"], (entry.annotations.origin.X-entry.area.X)*entry.scale, (entry.annotations.origin.Y-entry.area.Y)*entry.scale)
	foreground := style.Color
	if !style.HasColor && foreground == (ui.Color{}) {
		foreground = ui.RGB(255, 255, 255)
	}
	sub := style.SubOpacity
	if sub == 0 {
		sub = .3
	}
	v.Float(u["RasterScale"], scale).Color(u["Foreground"], foreground).Float(u["BrightOpacity"], style.BrightOpacity).Float(u["DimOpacity"], style.DimOpacity).Float(u["SubOpacity"], sub).Float(u["Opacity"], opacity).Float(u["Sigma"], sigma).Vec2(u["Axis"], axis.X, axis.Y)
	v.Vec2(u["BlackOffset"], entry.blackOffset.X, entry.blackOffset.Y)
	z := entry.contentScale
	if z <= 0 {
		z = 1
	}
	v.Float(u["ContentScale"], z).Vec2(u["ContentPivot"], entry.contentPivot.X, entry.contentPivot.Y)
	clip := entry.geometry.paintBounds
	if clip.W > 0 && clip.H > 0 {
		clip.X, clip.Y = (clip.X-entry.area.X)*entry.scale, (clip.Y-entry.area.Y)*entry.scale
		clip.W, clip.H = clip.W*entry.scale, clip.H*entry.scale
	}
	effectRect(v, u["ContentClip"], clip)
	flag := float32(0)
	if blurOnly {
		flag = 1
	}
	v.Float(u["BlurOnly"], flag)
	setEffectKernel(v, u, sigma, &entry.cache.compositeKernel)
}

func (entry *effectLine) animationPosition(fallback time.Duration, kind motion.AnimationKind, end time.Duration) time.Duration {
	if !entry.synced {
		return fallback
	}
	return entry.animation.Position(entry.now, kind, end)
}

// Running includes disabled residual emphasis and reverse ordinary floats.
func (r *effectRenderer) Running(now time.Time) bool {
	for _, entry := range r.entries {
		if !entry.pinned || !entry.synced {
			continue
		}
		if entry.geometry.dynamic && entry.animation.Running(now, motion.MaskAnimation, entry.geometry.end) {
			return true
		}
		for _, word := range entry.words {
			if word.emphasis == nil {
				if entry.geometry.dynamic && entry.animation.Running(now, motion.FloatWordAnimation, word.float.End()) {
					return true
				}
				continue
			}
			for _, char := range word.characters {
				if entry.animation.Running(now, motion.EmphasisGlowAnimation, word.emphasis.CharEnd(char.index, false)) || entry.animation.Running(now, motion.EmphasisFloatAnimation, word.emphasis.CharEnd(char.index, true)) {
					return true
				}
			}
		}
	}
	return false
}

func validEffectStyle(style effectStyle) bool {
	for _, value := range []float32{style.Opacity, style.BrightOpacity, style.DimOpacity, style.BrightMaskAlpha, style.DarkMaskAlpha, style.SubOpacity} {
		if math.IsNaN(float64(value)) || value < 0 || value > 1 {
			return false
		}
	}
	if !effectFinite(style.WordFadeWidth) || style.WordFadeWidth < 0 {
		return false
	}
	return !math.IsNaN(float64(style.Blur)) && style.Blur >= 0 && style.Blur <= effectBlurLimit
}

// DrawLine submits real native shader passes. Software is opt-in for small,
// controlled pixel tests; a host fallback must remain explicitly different.
func (r *effectRenderer) DrawLine(p *ui.Painter, g *lineGeometry, x, y float32, position time.Duration, style effectStyle) error {
	return r.drawLine(p, g, x, y, position, style, nil)
}
func (r *effectRenderer) drawLine(p *ui.Painter, g *lineGeometry, x, y float32, position time.Duration, style effectStyle, layer *effectGroupLayer) error {
	if !validEffectStyle(style) || !effectFinite(x, y) {
		return fmt.Errorf("native effects: invalid style or blur greater than %d DIP", effectBlurLimit)
	}
	if style.Opacity == 0 {
		return nil
	}
	if !p.Accelerated() && !style.AllowSoftware {
		return errEffectSoftware
	}
	var entry *effectLine
	for _, cached := range r.entries {
		if cached.geometry == g && cached.scale == p.Scale() && cached.pinned {
			entry = cached
			break
		}
	}
	if entry == nil {
		return errEffectMissing
	}
	if entry.dimOnly && style.BrightOpacity != 0 {
		return errEffectMissing
	}
	if style.Blur > 0 && entry.blurX == nil {
		return errEffectMissing
	}
	if entry.synced {
		position = entry.media
	}
	if !g.dynamic {
		style.BrightOpacity, style.DimOpacity = 1, 0
	}
	entry.contentScale, entry.contentPivot = 1, ui.Point{}
	if layer != nil {
		entry.contentScale = layer.Scale
		entry.contentPivot = ui.Point{X: (layer.OriginX - layer.X - entry.area.X) * entry.scale, Y: (layer.OriginY - layer.Y - entry.area.Y) * entry.scale}
	}
	if !entry.dimOnly {
		entry.drawSource(p, position, style)
	}
	box := ui.Rect{X: x + entry.area.X, Y: y + entry.area.Y, W: float32(entry.width) / entry.scale, H: float32(entry.height) / entry.scale}
	if style.Blur <= 0 {
		entry.setComposite(style, 0, entry.scale, style.Opacity, ui.Point{}, false)
		drawEffectShader(effectPrograms.composite, p, box, shader.Options{Images: [4]*ui.Bitmap{entry.sourceBitmap(), entry.white, entry.black}, Values: entry.compositeValues})
		return nil
	}
	composite := effectCompositeState(style, g.dynamic)
	composite.opacity = 1 // The final pass applies parent opacity after both axes.
	blurKey := effectLineBlurKey{revision: entry.cache.revision, style: composite, sigma: style.Blur * entry.scale, scale: entry.contentScale, pivot: entry.contentPivot}
	if !entry.cache.blurReady || entry.cache.blur != blurKey {
		entry.blurX.Draw(p, func(cp *ui.Painter) {
			entry.setComposite(style, blurKey.sigma, 1, 1, ui.Point{X: 1}, false)
			drawEffectShader(effectPrograms.composite, cp, ui.Rect{W: float32(entry.width), H: float32(entry.height)}, shader.Options{Images: [4]*ui.Bitmap{entry.sourceBitmap(), entry.white, entry.black}, Values: entry.compositeValues})
		})
		entry.cache.blur, entry.cache.blurReady = blurKey, true
	}
	effectGaussian(p, box, entry.blurX.Bitmap(), ui.Rect{W: float32(entry.width), H: float32(entry.height)}, ui.Point{}, ui.Point{Y: 1}, blurKey.sigma, entry.scale, style.Opacity, false, entry.gaussianValues, &entry.cache.gaussianKernel)
	return nil
}

// The shared, unclamped path retains the original cursor's time behavior,
// including zero-duration moves and overlapping words. Target-specific clamp
// crossings are exactly clamp(linear(raw)), avoiding O(words²) retained frames.
type effectMaskFrame struct{ offset, pixels, fades float64 }
type effectMaskPath struct {
	start, end time.Duration
	frames     []effectMaskFrame
	before     []float64
}

func effectMaskWidth(block wordBlock) float32 {
	if block.maskWidth != 0 || block.width == 0 {
		return block.maskWidth
	}
	return block.width
}

func effectMaskHeight(block wordBlock) float32 {
	if block.maskHeight != 0 || block.height == 0 {
		return block.maskHeight
	}
	return block.height
}

func newEffectMaskPath(g *lineGeometry) effectMaskPath {
	path := effectMaskPath{start: g.start, end: g.end, frames: []effectMaskFrame{{}}, before: make([]float64, len(g.blocks))}
	var cursor effectMaskFrame
	var stamp float64
	total := float64(max(time.Nanosecond, g.end-g.start))
	advance := func(duration, pixels, fades float64) {
		cursor.offset += duration / total
		cursor.pixels += pixels
		cursor.fades += fades
		if duration > 0 {
			frame := cursor
			frame.offset = min(max(frame.offset, 0), 1)
			path.frames = append(path.frames, frame)
		}
	}
	widthBefore := float64(0)
	for i, block := range g.blocks {
		path.before[i] = widthBefore
		widthBefore += float64(effectMaskWidth(block))
		start := float64(block.start - g.start)
		if gap := start - stamp; gap > 0 {
			advance(gap, 0, 0)
		}
		stamp = start
		var rubyChars int
		for _, ruby := range block.ruby {
			if referenceTrim(ruby.Text) != "" {
				rubyChars += len(utf16.Encode([]rune(ruby.Text)))
			}
		}
		if rubyChars == 0 {
			fade := float64(0)
			if i == 0 {
				fade += 1.5
			}
			if i == len(g.blocks)-1 {
				fade += .5
			}
			duration := float64(max(0, block.end-block.start))
			advance(duration, float64(effectMaskWidth(block)), fade)
			stamp += duration
			continue
		}
		charIndex := 0
		for _, ruby := range block.ruby {
			count := len(utf16.Encode([]rune(ruby.Text)))
			if count == 0 || referenceTrim(ruby.Text) == "" {
				continue
			}
			a := max(ruby.Start, block.start)
			b := min(max(ruby.End, a), block.end)
			start := float64(a - g.start)
			if gap := start - stamp; gap > 0 {
				advance(gap, 0, 0)
			}
			stamp = start
			duration := float64(max(0, b-a)) / float64(count)
			for range count {
				fade := float64(0)
				if i == 0 && charIndex == 0 {
					fade += 1.5
				}
				if i == len(g.blocks)-1 && charIndex == rubyChars-1 {
					fade += .5
				}
				advance(duration, float64(effectMaskWidth(block))/float64(rubyChars), fade)
				stamp += duration
				charIndex++
			}
		}
		end := max(float64(block.end-g.start), stamp)
		if gap := end - stamp; gap > 0 {
			advance(gap, 0, 0)
		}
		stamp = end
	}
	return path
}

func (path *effectMaskPath) position(block int, width, padding, fade float32, position time.Duration) float32 {
	initial := -path.before[block] - float64(width+padding+2*fade)
	minimum := -float64(width + 2*padding + fade)
	clamp := func(value float64) float64 { return min(max(value, minimum), 0) }
	p := float64(position-path.start) / float64(max(time.Nanosecond, path.end-path.start))
	last := path.frames[len(path.frames)-1]
	if p > last.offset && last.offset < 1 {
		// Web Animations supplies the underlying mask-position (0% 0%)
		// at a missing offset 1; retaining the last explicit frame is wrong.
		value := clamp(initial + last.pixels + float64(fade)*last.fades)
		return float32(value * (1 - min(1, (p-last.offset)/(1-last.offset))))
	}
	next := sort.Search(len(path.frames), func(i int) bool { return path.frames[i].offset > p })
	if next == 0 {
		return float32(clamp(initial))
	}
	if next == len(path.frames) {
		return float32(clamp(initial + last.pixels + float64(fade)*last.fades))
	}
	a, b := path.frames[next-1], path.frames[next]
	rawA := initial + a.pixels + float64(fade)*a.fades
	rawB := initial + b.pixels + float64(fade)*b.fades
	type point struct{ offset, value float64 }
	points := [4]point{{a.offset, clamp(rawA)}}
	count := 1
	move := rawB - rawA
	perPixel := float64(0)
	if move != 0 {
		perPixel = math.Abs((b.offset - a.offset) / move)
	}
	if rawB > minimum && rawA < minimum {
		points[count] = point{a.offset + math.Abs(rawA-minimum)*perPixel, clamp(rawA)}
		count++
	}
	if rawB > 0 && rawA < 0 {
		points[count] = point{a.offset + math.Abs(rawA)*perPixel, clamp(rawB)}
		count++
	}
	points[count] = point{b.offset, clamp(rawB)}
	count++
	for i := 1; i < count; i++ {
		left, right := points[i-1], points[i]
		if p < right.offset {
			t := (p - left.offset) / (right.offset - left.offset)
			return float32(left.value + (right.value-left.value)*t)
		}
	}
	return float32(clamp(rawB))
}
