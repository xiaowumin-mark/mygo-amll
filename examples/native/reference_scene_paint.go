// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"errors"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

var errReferenceFallbackCapability = errors.New("native static text: font, bidi or word-segmentation capability is not verified; using readable retained layouts")

func (s *scene) positionReferenceRow(row *referenceRow, g *referenceGroupMotion, step referenceStep, static bool) {
	r := s.reference
	pg, config := r.presentation.groups[row.index], step.frameOptions()
	row.y = float32(referenceRound(g.y.Sample(step.now).Position, 1))
	if config.disableSpring && !static {
		row.y = float32(g.cssY.sample(step.now))
	}
	row.height = float32(g.height)
	if row.main != nil {
		// The source layout index is updated by ResizeObserver later. The actual
		// wrapper's top-background margin already uses this frame's spring pose.
		row.height = float32(referenceGroupFlowHeight(row.main, row.background, r.font.Size, referenceBackgroundTop(g, pg, config), referenceBackgroundHasMargin(g, pg, config), g.active || !step.playing, g.bg.Sample(step.now).Position, r.scale))
	}
	pad := float32(config.widthPadding)
	row.x = pad
	duet := r.presentation.document.Line(pg.mainLayout).Duet()
	contentWidth := cssLayoutLength(max(1, r.box.W-2*pad), r.scale)
	if r.presentation.hasDuet && duet {
		row.x += cssLayoutLength(.15*contentWidth, r.scale)
	}
	row.opacity, row.blur = float32(g.opacity.sample(step.now)), float32(g.blur.sample(step.now))
	if static {
		row.blur = 0
	}
	row.backgroundStyle = effectGroupBackground{Rect: ui.Rect{X: -row.x, W: r.box.W, H: row.height}, Radius: .25 * r.font.Size, Color: referenceColorValue(g.background.sample(step.now))}
	s.buildReferenceLayers(row, g, step, static)
	row.hitCount = 1
	row.hitBounds[0] = ui.Rect{Y: row.y, W: r.box.W, H: row.height}
	for i, layer := range row.layers[:row.layerCount] {
		if i == 1 {
			hidden := float64(-80)
			if pg.backgroundFirst && !config.postposition {
				hidden = 80
			}
			if !g.active && absReference(g.bg.Sample(step.now).Position-hidden) < .1 {
				continue // visibility:hidden disables the entire background subtree.
			}
			// The transparent BG wrapper also receives pointer events. Its own
			// scale encloses more space than the separately scaled lyric child.
			slide := g.bg.Sample(step.now).Position
			progress := min(1, max(0, 1-absReference(slide)/80))
			ws := float32(referenceRound(.8+.2*progress, 3))
			flowY, originY := row.height, float32(0)
			margin := float32(0)
			if referenceBackgroundHasMargin(g, pg, config) {
				margin = float32(referenceRound(-float64(row.background.height)*(1-progress), 1))
			}
			if referenceBackgroundTop(g, pg, config) {
				flowY = cssLayoutLength(.4*r.font.Size, r.scale) + margin
				originY = row.background.height
			} else if g.active || !step.playing {
				flowY = cssLayoutLength(.4*r.font.Size, r.scale) + row.main.height + cssLayoutLength(.3*r.font.Size, r.scale) + margin
			} else {
				flowY += margin
			}
			wrapperX := pad
			if duet {
				wrapperX += contentWidth * (1 - ws)
			}
			row.hitBounds[row.hitCount] = ui.Rect{X: wrapperX, Y: row.y + flowY + float32(referenceRound(slide/100*float64(row.background.height), 1)) + originY*(1-ws), W: contentWidth * ws, H: row.background.height * ws}
			row.hitCount++
		}
		font := layer.Geometry.font.Size
		smallPadding := cssLayoutLength(.2*font, r.scale)
		x := -smallPadding
		if duet {
			x = layer.Geometry.width + smallPadding - contentWidth
		}
		bounds := ui.Rect{X: x, Y: -smallPadding, W: contentWidth, H: layer.Geometry.height + 2*smallPadding}
		bounds.X = row.x + layer.X + layer.OriginX + (bounds.X-layer.OriginX)*layer.Scale
		bounds.Y = row.y + layer.Y + layer.OriginY + (bounds.Y-layer.OriginY)*layer.Scale
		bounds.W *= layer.Scale
		bounds.H *= layer.Scale
		row.hitBounds[row.hitCount] = bounds
		row.hitCount++
	}
	row.visible = false
	if g.connected {
		for _, bounds := range row.hitBounds[:row.hitCount] {
			if bounds.X < r.box.W && bounds.X+bounds.W > 0 && bounds.Y < r.box.H && bounds.Y+bounds.H > 0 {
				row.visible = true
				break
			}
		}
	}
}

func (s *scene) buildReferenceLayers(row *referenceRow, g *referenceGroupMotion, step referenceStep, static bool) {
	r := s.reference
	if row.main == nil {
		return
	}
	pg := r.presentation.groups[row.index]
	config := step.frameOptions()
	style := effectStyle{Opacity: 1, BrightOpacity: float32(g.bright.sample(step.now)), DimOpacity: float32(g.dim.sample(step.now)), BrightMaskAlpha: 1, DarkMaskAlpha: .4, SubOpacity: .3, Enabled: g.enabled, Playing: step.playing, AllowSoftware: r.allowSoftware, Revision: r.seekRevision}
	style.Clock = &r.clocks[pg.mainLayout]
	style.WordFadeWidth = float32(config.fadeWidth)
	style.Color = referenceColorValue(config.foreground)
	style.HasColor = true
	if !g.copy {
		style.BrightMaskAlpha, style.DarkMaskAlpha = .2, .2
		if g.active {
			style.BrightMaskAlpha, style.DarkMaskAlpha = 1, .4
		}
	}
	if !r.presentation.dynamic {
		style.BrightOpacity, style.DimOpacity = 1, 0
		style.BrightMaskAlpha, style.DarkMaskAlpha = 1, 1
	}
	mainY := cssLayoutLength(.4*r.font.Size, r.scale)
	bgY := row.height
	ws := float32(1)
	if row.background != nil {
		slide := g.bg.Sample(step.now).Position
		progress := min(1, max(0, 1-absReference(slide)/80))
		ws = float32(referenceRound(.8+.2*progress, 3))
		translate := float32(referenceRound(slide/100*float64(row.background.height), 1))
		margin := float32(0)
		if referenceBackgroundHasMargin(g, pg, config) {
			margin = float32(referenceRound(-float64(row.background.height)*(1-progress), 1))
		}
		if referenceBackgroundTop(g, pg, config) {
			bgY = cssLayoutLength(.4*r.font.Size, r.scale) + margin
			mainY = bgY + row.background.height + cssLayoutLength(.3*r.font.Size, r.scale)
		} else if g.active || !step.playing {
			bgY = mainY + row.main.height + cssLayoutLength(.3*r.font.Size, r.scale) + margin
		} else {
			bgY += margin
		}
		bgY += translate
	}
	duet := r.presentation.document.Line(pg.mainLayout).Duet()
	mainX := float32(0)
	smallPadding := cssLayoutLength(.2*row.main.font.Size, r.scale)
	originX := -smallPadding
	if duet {
		mainX = smallPadding
		originX = row.main.width + smallPadding
	}
	mainScale := float32(referenceRound(g.mainScale.Sample(step.now).Position/100, 3))
	if config.disableSpring {
		mainScale = float32(g.cssMainScale.sample(step.now) / 100)
	}
	if static {
		mainScale = 1
	}
	row.layers[0] = effectGroupLayer{Geometry: row.main, X: mainX, Y: mainY, Scale: mainScale, OriginX: originX, OriginY: row.main.height / 2, Style: style}
	row.layerCount = 1
	if row.background != nil {
		bgStyle := style
		bgStyle.Clock = &r.clocks[pg.backgroundLayout]
		bgStyle.Background = true
		bgStyle.Opacity = .4 * float32(g.bgOpacity.sample(step.now))
		if !step.playing {
			bgStyle.Opacity = .4
		}
		bs := float32(referenceRound(g.bgScale.Sample(step.now).Position/100, 3))
		if config.disableSpring {
			bs = float32(g.cssBGScale.sample(step.now) / 100)
		}
		lineOrigin := row.background.height / 2
		bgX := float32(0)
		bgPadding := cssLayoutLength(.2*row.background.font.Size, r.scale)
		lineOriginX := -bgPadding
		wrapperOriginX := float32(0)
		if duet {
			bgX = bgPadding
			lineOriginX = row.background.width + bgPadding
			wrapperOriginX = row.background.width
		}
		wrapperOrigin := float32(0)
		if referenceBackgroundTop(g, pg, config) {
			wrapperOrigin = row.background.height
		}
		bgY += (1 - ws) * (wrapperOrigin - lineOrigin)
		bgX += (1 - ws) * (wrapperOriginX - lineOriginX)
		if static {
			bs, ws = 1, 1
		}
		row.layers[1] = effectGroupLayer{Geometry: row.background, X: bgX, Y: bgY, Scale: ws * bs, OriginX: lineOriginX, OriginY: lineOrigin, Style: bgStyle}
		row.layerCount = 2
	}
}

func absReference(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func (s *scene) drawReference(p *ui.Painter, box ui.Rect) {
	if s.closed || box.W <= 0 || box.H <= 0 {
		return
	}
	r := s.reference
	// Layout/backend discovery may repeat at the same view timestamp. Preparation
	// is keyed by that timestamp/revision; painting never advances business time.
	s.prepareReferenceFrame(r.now, box, p.Scale(), p.Accelerated())
	s.drawCalls++
	p.Clip(box, 0, func() {
		for _, row := range r.rows[:r.rowCount] {
			if !row.visible {
				continue
			}
			if row.main == nil {
				p.TextLayout(s.fallback, box.X+row.x, box.Y+row.y, ui.RGB(180, 180, 180))
				continue
			}
			if row.err == nil && !r.lastStatic {
				if err := r.effects.DrawGroup(p, row.main, box.X+row.x, box.Y+row.y, row.layers[:row.layerCount], row.opacity, row.blur, row.backgroundStyle); err == nil {
					continue
				}
			}
			s.paintReferenceFallback(p, box, row)
		}
		s.paintReferenceInterlude(p, box)
		if r.fallbackLayout != nil {
			p.TextLayout(r.fallbackLayout, box.X+8, box.Y+box.H-22, ui.RGB(225, 185, 110))
		}
	})
	// The view owns animation scheduling because it also freezes the frame's
	// timestamp. A paint-only timer would redraw that same sample indefinitely
	// when no view update is pending (for example after layout discovery).
}

// The static fallback shares the admitted atlas/cache budget with the animated
// renderer. Its dim clone already contains the exact retained word, character,
// Ruby and Roman boxes; no animated canvas or Gaussian surface is requested.
// The host calls this during frame preparation, never from this file's paint.
func (r *effectRenderer) prepareReferenceFallback(g *lineGeometry, scale float32) error {
	if g != nil && (g.bidiFallback || g.segmenterFallback) {
		return errReferenceFallbackCapability
	}
	return r.prepare(g, scale, false, true)
}

// A static composite has no convolution or animation sampling. Two atlas
// samples preserve mono foreground tint, palette RGB and inherited color alpha
// on both software and accelerated painters. Annotations are painted separately
// so the body's existing stepped highlight clips cannot brighten subtitles.
func (entry *effectLine) paintReferenceFallbackAtlas(p *ui.Painter, x, y float32, color ui.Color, opacity float32, annotation bool) {
	entry.contentScale, entry.contentPivot = 1, ui.Point{}
	style := effectStyle{Color: color, HasColor: true, DimOpacity: 1, SubOpacity: 1}
	entry.setComposite(style, 0, entry.scale, opacity, ui.Point{}, false)
	u := effectPrograms.compositeUniforms
	if annotation {
		effectRect(entry.compositeValues, u["DimRect"], ui.Rect{})
	} else {
		effectRect(entry.compositeValues, u["AnnotationRect"], ui.Rect{})
	}
	box := ui.Rect{X: x + entry.area.X, Y: y + entry.area.Y, W: float32(entry.width) / entry.scale, H: float32(entry.height) / entry.scale}
	// image0 is an unused nonnil source: BrightOpacity is zero. This keeps the
	// public shader binding valid without allocating a transparent source canvas.
	drawEffectShader(effectPrograms.composite, p, box, shader.Options{Images: [4]*ui.Bitmap{entry.white, entry.white, entry.black}, Values: entry.compositeValues})
}

// Last-resort retained layouts keep every textual component readable when the
// atlas or font capability was rejected. The caller retains the original error
// and reports this approximation; it is not forced-LTR/color-font parity. Each
// CSS row is isolated from the retained native layout before being repositioned,
// with no shaping, I/O, UI construction, or generated glyph identities in Draw.
func paintReferenceReadableLayout(p *ui.Painter, layout *ui.TextLayout, rows []cssTextRow, x, y, drawY float32, color ui.Color) {
	if layout == nil {
		return
	}
	if len(rows) <= 1 {
		if len(rows) == 1 {
			x, y, drawY = x+rows[0].x, y+rows[0].y, rows[0].drawY
		}
		p.TextLayout(layout, x, y+drawY, color)
		return
	}
	_, nativeHeight := layout.Size()
	nativeLineHeight := nativeHeight / float32(len(rows))
	visible := p.Visible()
	for i, row := range rows {
		top := y + row.y + row.drawY
		clip := ui.Rect{X: visible.X, Y: top, W: visible.W, H: nativeLineHeight}
		p.Clip(clip, 0, func() {
			p.TextLayout(layout, x+row.x, top-float32(i)*nativeLineHeight, color)
		})
	}
}

func paintReferenceReadableBody(p *ui.Painter, g *lineGeometry, x, y float32, color ui.Color) {
	if len(g.blocks) == 0 {
		p.TextLayout(g.layout, x, y, color)
		return
	}
	for _, block := range g.blocks {
		bx, by := x+block.x, y+block.y
		for _, ruby := range block.rubyParts {
			paintReferenceReadableLayout(p, ruby.layout, ruby.rows, bx+ruby.x, by+ruby.y, ruby.drawY, color)
		}
		if len(block.characters) != 0 {
			for _, char := range block.characters {
				p.TextLayout(char.layout, bx+char.x, by+char.y+char.drawY, color)
			}
		} else {
			paintReferenceReadableLayout(p, block.layout, block.bodyRows, bx+block.bodyX, by+block.bodyY, block.bodyDrawY, color)
		}
		paintReferenceReadableLayout(p, block.romanLayout, block.romanRows, bx+block.romanX, by+block.romanY, block.romanDrawY, color)
	}
}

func (s *scene) paintReferenceFallback(p *ui.Painter, box ui.Rect, row referenceRow) {
	r := s.reference
	bg := row.backgroundStyle
	if bg.Color.A > 0 {
		rect := bg.Rect
		rect.X += box.X + row.x
		rect.Y += box.Y + row.y
		p.Fill(rect, ui.RGBA(bg.Color.R, bg.Color.G, bg.Color.B, float32(bg.Color.A)/255*row.opacity), bg.Radius)
	}
	for layerIndex, layer := range row.layers[:row.layerCount] {
		g := layer.Geometry
		x, y := box.X+row.x+layer.X, box.Y+row.y+layer.Y
		base := float32(.2) * row.opacity * layer.Style.Opacity
		if !r.presentation.dynamic {
			base = row.opacity * layer.Style.Opacity
		} else if r.groups[row.index].enabled {
			base = .4 * row.opacity * layer.Style.Opacity
		}
		color := layer.Style.Color
		entry := r.effects.preparedEntry(g, p.Scale())
		body := func(alpha float32) {
			if entry != nil {
				entry.paintReferenceFallbackAtlas(p, x, y, color, alpha, false)
			} else {
				paintReferenceReadableBody(p, g, x, y, ui.RGBA(color.R, color.G, color.B, alpha*float32(color.A)/255))
			}
		}
		body(base)
		if r.presentation.dynamic && r.groups[row.index].enabled {
			for _, clip := range row.highlights[layerIndex] {
				clip.X += x
				clip.Y += y
				p.Clip(clip, 0, func() {
					body(row.opacity * layer.Style.Opacity)
				})
			}
		}
		if entry != nil {
			entry.paintReferenceFallbackAtlas(p, x, y, color, .3*row.opacity*layer.Style.Opacity, true)
		} else {
			for _, annotation := range g.annotations {
				paintReferenceReadableLayout(p, annotation.layout, annotation.rows, x, y+annotation.boxY, annotation.y-annotation.boxY, ui.RGBA(color.R, color.G, color.B, .3*row.opacity*layer.Style.Opacity*float32(color.A)/255))
			}
		}
	}
}

func (s *scene) referenceWake(now time.Time) time.Duration {
	r := s.reference
	if r == nil || s.closed || s.hidden {
		return 0
	}
	if r.running || r.inertia || r.dirty {
		return referenceFrameInterval
	}
	if !r.wheelUntil.IsZero() {
		return max(time.Nanosecond, r.wheelUntil.Sub(now))
	}
	delay := time.Duration(0)
	if r.interludePose.NextWake > 0 && s.clock.playing {
		delay = max(time.Nanosecond, time.Duration(float64(r.interludePose.NextWake)/s.clock.rate))
	}
	if r.lastStatic && s.clock.playing {
		// Static highlighting changes at word ends, not just the group events
		// scheduled by the host view. Reuse the cold sorted source boundaries;
		// no animation frames or per-frame document scan are needed.
		if wordDelay := nextDelay(s.boundaries, s.clock.positionAt(now), s.clock.rate); wordDelay > 0 && (delay == 0 || wordDelay < delay) {
			delay = wordDelay
		}
	}
	return delay
}

func (s *scene) paintReferenceInterlude(p *ui.Painter, box ui.Rect) {
	r := s.reference
	paintReferenceInterludeDots(p, box, r.interludePose, r.interludeMetrics.InterludeMetrics, r.options.Foreground)
}

// paintReferenceInterludeDots keeps the circle geometry fixed while the
// reference scale is applied as a painter transform. Painter.Fill snaps its
// input rectangle to device pixels before transforms are applied; varying the
// rectangle itself therefore quantizes every animation sample independently.
func paintReferenceInterludeDots(p *ui.Painter, box ui.Rect, pose motion.InterludePose, metrics motion.InterludeMetrics, color ui.Color) {
	if !pose.Active || pose.Opacity <= 0 {
		return
	}
	centerX := box.X + float32(pose.X+metrics.Width/2)
	centerY := box.Y + float32(pose.Y+metrics.Height/2)
	transform := ui.Scaling(float32(pose.Scale), float32(pose.Scale))
	p.Transform(transform, centerX, centerY, func() {
		for i, opacity := range pose.DotOpacities {
			x := box.X + float32(pose.X+metrics.PaddingX+float64(i)*(metrics.DotDiameter+metrics.Gap))
			y := box.Y + float32(pose.Y+metrics.PaddingY+(metrics.ContentHeight-metrics.DotDiameter)/2)
			diameter := float32(metrics.DotDiameter)
			p.Fill(ui.Rect{X: x, Y: y, W: diameter, H: diameter}, ui.RGBA(color.R, color.G, color.B, float32(pose.Opacity*opacity)*float32(color.A)/255), diameter/2)
		}
	})
}
