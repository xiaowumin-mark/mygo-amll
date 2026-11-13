//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// referenceResolvedTextGlyphs is for a single already-resolved visual run.
// Its caller removes Bidi_Control while retaining its own original-rune map.
// The public Shape-created glyph resources are preserved; coordinates and
// Cluster/Runes are relative to this body. This helper never opens an ICU handle.
func referenceResolvedTextGlyphs(text string, font ui.Font, rtl bool) ([]ui.Glyph, float32, bool) {
	fallback := func() ([]ui.Glyph, float32, bool) {
		glyphs := ui.Shape(text, font)
		var width float32
		for _, g := range glyphs {
			width += g.Advance
		}
		return glyphs, width, false
	}
	if !utf8.ValidString(text) || strings.ContainsAny(text, "\r\n\u2028\u2029") {
		return fallback()
	}
	count := 0
	for _, r := range text {
		if referenceBidiControl(r) {
			return fallback()
		}
		count++
	}
	if count == 0 {
		return nil, 0, true
	}
	prefix := "A\u200b\u202d"
	if rtl {
		prefix = "ע\u200b\u202e"
	}
	shaped := ui.Shape(prefix+text+"\u202c", font)
	const prefixRunes = 3
	limit := prefixRunes + count
	left, width, found := float32(0), float32(0), false
	for _, g := range shaped {
		if g.Cluster < prefixRunes || g.Cluster >= limit {
			if g.Cluster < prefixRunes && g.Cluster+g.Runes > prefixRunes {
				return fallback()
			}
			continue
		}
		if g.Runes < 1 || g.Cluster+g.Runes > limit || g.Advance > 0 && g.RTL != rtl {
			return fallback()
		}
		if !found || g.X < left {
			left, found = g.X, true
		}
		width += g.Advance
	}
	out := shaped[:0]
	for _, g := range shaped {
		if g.Cluster < prefixRunes || g.Cluster >= limit {
			continue
		}
		g.Cluster -= prefixRunes
		g.X -= left
		out = append(out, g)
	}
	return out, width, true
}
