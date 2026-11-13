// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

// These conservative fixed charges include the new comparable signatures,
// Gaussian kernels and identity tokens. They share the existing 64 MiB budget;
// caching never allocates another image plane or retains an evicted line.
const (
	effectLineCacheBytes  = 4096
	effectGroupCacheBytes = 4096
)

// A token distinguishes replacement atlases without keeping their geometry,
// document, bitmap or Canvas alive through another cache's signature.
type effectRasterIdentity struct{ marker byte }

type effectSourceKey struct {
	color              ui.Color
	bright, dark, fade float32
	background, synced bool
	position           time.Duration
	samples            [4]motion.AnimationSampleKey
}

type effectCompositeKey struct {
	color                     ui.Color
	bright, dim, sub, opacity float32
}

type effectLineBlurKey struct {
	revision     uint64
	style        effectCompositeKey
	sigma, scale float32
	pivot        ui.Point
}

type effectLineCache struct {
	identity                        *effectRasterIdentity
	source                          effectSourceKey
	ends                            [4]time.Duration
	used                            [4]bool
	revision                        uint64
	sourceReady, blurReady          bool
	blur                            effectLineBlurKey
	compositeKernel, gaussianKernel effectKernel
}

type effectLayerKey struct {
	identity                      *effectRasterIdentity
	revision                      uint64
	x, y, scale, originX, originY float32
	style                         effectCompositeKey
}

type effectGroupSourceKey struct {
	layers     [effectEntryLimit]effectLayerKey
	count      int
	background effectGroupBackground
}

type effectGroupCache struct {
	source                 effectGroupSourceKey
	revision, blurRevision uint64
	sourceReady, blurReady bool
	sigma                  float32
	gaussianKernel         effectKernel
}

func effectForeground(style effectStyle) ui.Color {
	if !style.HasColor && style.Color == (ui.Color{}) {
		return ui.RGB(255, 255, 255)
	}
	return style.Color
}

func effectCompositeState(style effectStyle, dynamic bool) effectCompositeKey {
	sub := style.SubOpacity
	if sub == 0 {
		sub = .3
	}
	key := effectCompositeKey{color: effectForeground(style), bright: style.BrightOpacity, dim: style.DimOpacity, sub: sub, opacity: style.Opacity}
	if !dynamic {
		key.bright, key.dim = 1, 0
	}
	return key
}

func (entry *effectLine) initPaintCache() {
	c := &entry.cache
	c.identity = new(effectRasterIdentity)
	c.ends[motion.MaskAnimation], c.used[motion.MaskAnimation] = entry.geometry.end, entry.geometry.dynamic
	for _, word := range entry.words {
		if word.emphasis == nil {
			if entry.geometry.dynamic {
				c.ends[motion.FloatWordAnimation] = max(c.ends[motion.FloatWordAnimation], word.float.End())
				c.used[motion.FloatWordAnimation] = true
			}
			continue
		}
		for _, char := range word.characters {
			c.ends[motion.EmphasisGlowAnimation] = max(c.ends[motion.EmphasisGlowAnimation], word.emphasis.CharEnd(char.index, false))
			c.ends[motion.EmphasisFloatAnimation] = max(c.ends[motion.EmphasisFloatAnimation], word.emphasis.CharEnd(char.index, true))
			c.used[motion.EmphasisGlowAnimation], c.used[motion.EmphasisFloatAnimation] = true, true
		}
	}
}

func (entry *effectLine) sourceKey(position time.Duration, style effectStyle) effectSourceKey {
	key := effectSourceKey{color: effectForeground(style), bright: style.BrightMaskAlpha, dark: style.DarkMaskAlpha, fade: style.WordFadeWidth, background: style.Background, synced: entry.synced}
	if entry.synced {
		for kind, used := range entry.cache.used {
			if used {
				key.samples[kind] = entry.animation.SampleKey(entry.now, motion.AnimationKind(kind), entry.cache.ends[kind])
			}
		}
	} else {
		// The standalone diagnostic API has no persistent animation clock.
		// Retain its exact caller position rather than guessing seek history.
		key.position = position
	}
	return key
}

func (entry *effectLine) drawSource(p *ui.Painter, position time.Duration, style effectStyle) {
	key := entry.sourceKey(position, style)
	if entry.cache.sourceReady && entry.cache.source == key {
		return
	}
	entry.updateGlows(p, position)
	entry.source.Draw(p, func(cp *ui.Painter) { entry.drawWords(cp, position, style) })
	entry.cache.source, entry.cache.sourceReady = key, true
	entry.cache.revision++
}

func effectGroupState(layers []effectGroupLayer, entries []*effectLine, background effectGroupBackground) effectGroupSourceKey {
	var key effectGroupSourceKey
	if background.Color.A != 0 {
		key.background = background
	}
	for i, layer := range layers {
		if layer.Style.Opacity == 0 {
			continue
		}
		entry := entries[i]
		key.layers[key.count] = effectLayerKey{identity: entry.cache.identity, revision: entry.cache.revision, x: layer.X, y: layer.Y, scale: layer.Scale, originX: layer.OriginX, originY: layer.OriginY, style: effectCompositeState(layer.Style, entry.geometry.dynamic)}
		key.count++
	}
	return key
}
