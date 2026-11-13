// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"errors"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/harfbuzz"
)

var (
	errReferenceEmojiCapability = errors.New("native emoji shaping: unsupported font or platform capability")
	errReferenceEmojiMapping    = errors.New("native emoji shaping: inconsistent public glyph or text mapping")
)

// referenceTextShape keeps ordinary UI glyphs and separately rasterized color
// glyphs in the same baseline-relative DIP coordinate space. Exact describes
// the supported shaping path, rather than claiming raster equivalence.
type referenceTextShape struct {
	Glyphs []ui.Glyph
	Colors []referenceColorGlyph
	Width  float32
	Exact  bool
	Err    error
}

const referenceEmojiSelectionLimit = 64

// The allowance covers each entry plus conservative cloned-key allocation
// rounding. Font-family capability snapshots are separately fixed at six names.
const referenceEmojiSelectionEntryEstimate = 160
const referenceEmojiSelectionDataLimit = 16<<10 - referenceEmojiSelectionLimit*referenceEmojiSelectionEntryEstimate

type referenceEmojiSelectionKey struct {
	text, family, features, lang string
	weight                       int
}

type referenceEmojiSelection struct {
	key           referenceEmojiSelectionKey
	valid, result bool
	bytes         int
}

// referenceFontProvider borrows the immutable color font owner. It is confined
// to the UI thread; Close never closes the borrowed owner.
type referenceFontProvider struct {
	color          *emojiColorFont
	face           *font.Face
	hb             *harfbuzz.Font
	colorFamily    string
	colorVariable  bool
	registered     map[string]bool
	installed      map[string]bool
	monoFallback   *referenceMonoFallback
	selections     [referenceEmojiSelectionLimit]referenceEmojiSelection
	selectionAt    int
	selectionBytes int
}

func newReferenceFontProvider(color *emojiColorFont) *referenceFontProvider {
	initializeReferenceEmojiPlatform()
	p := &referenceFontProvider{color: color}
	if color != nil {
		p.face = color.newShapingFace()
		if p.face != nil {
			// A non-empty public variation request installs the default axis
			// coordinates even for an unknown tag. Detect axes without reading
			// private font tables, then restore our independent face before HB.
			p.face.SetVariations([]font.Variation{{}})
			p.colorVariable = len(p.face.Coords()) != 0
			p.face.SetVariations(nil)
			p.hb = harfbuzz.NewFont(p.face)
			p.colorFamily = p.face.Describe().Family
		}
	}
	// Enumerate once, during explicit cold construction. Retain only the six
	// supported names, so the capability snapshot cannot grow with host fonts.
	// Hosts registering a font afterwards must recreate the provider.
	p.registered = make(map[string]bool, 6)
	p.installed = make(map[string]bool, 6)
	for _, family := range ui.FontFamilies() {
		name := strings.ToLower(family.Name)
		if referenceEmojiPrimaryFamily(name) {
			if family.Registered {
				p.registered[name] = true
			} else {
				p.installed[name] = true
			}
		}
	}
	return p
}

func (p *referenceFontProvider) Shape(text string, font ui.Font) referenceTextShape {
	glyphs, width, exact := p.shapeMono(text, font)
	base := referenceTextShape{Glyphs: glyphs, Width: width, Exact: exact}
	return p.shapeColor(text, font, base)
}

func (p *referenceFontProvider) shapeMono(text string, font ui.Font) ([]ui.Glyph, float32, bool) {
	if p != nil && p.monoFallback != nil {
		return p.monoFallback.Shape(text, font)
	}
	return referenceTextGlyphs(text, font)
}

func (p *referenceFontProvider) Close() {
	if p != nil {
		p.color = nil
		p.face = nil
		p.hb = nil
		p.colorFamily = ""
		p.colorVariable = false
		p.registered = nil
		p.installed = nil
		p.monoFallback = nil
		clear(p.selections[:])
		p.selectionAt, p.selectionBytes = 0, 0
	}
}

func referenceEmojiPrimaryFamily(name string) bool {
	switch name {
	case "segoe ui", "segoe ui emoji", "arial", "times new roman", "calibri", "consolas":
		return true
	}
	return false
}

func (p *referenceFontProvider) rememberEmojiSelection(key referenceEmojiSelectionKey, result bool) {
	cost := len(key.text) + len(key.family) + len(key.features) + len(key.lang)
	if cost > referenceEmojiSelectionDataLimit {
		return
	}
	for {
		entry := &p.selections[p.selectionAt]
		p.selectionBytes -= entry.bytes
		*entry = referenceEmojiSelection{}
		if p.selectionBytes+cost <= referenceEmojiSelectionDataLimit {
			// Clone host configuration strings: a short slice must not keep a
			// larger host buffer or an old document alive through this cache.
			key.text, key.family = strings.Clone(key.text), strings.Clone(key.family)
			key.features, key.lang = strings.Clone(key.features), strings.Clone(key.lang)
			*entry = referenceEmojiSelection{key: key, valid: true, result: result, bytes: cost}
			p.selectionBytes += cost
			p.selectionAt = (p.selectionAt + 1) % len(p.selections)
			return
		}
		p.selectionAt = (p.selectionAt + 1) % len(p.selections)
	}
}
