//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import "github.com/egoist/mygo/ui"

// Public Shape remains readable on other platforms. No unverified forced-LTR
// claim is made; the owner exposes this fallback in its retained geometry state.
func referenceTextGlyphs(text string, font ui.Font) ([]ui.Glyph, float32, bool) {
	glyphs := ui.Shape(text, font)
	var width float32
	for _, glyph := range glyphs {
		width += glyph.Advance
	}
	return glyphs, width, false
}

func referenceResolvedTextGlyphs(text string, font ui.Font, _ bool) ([]ui.Glyph, float32, bool) {
	return referenceTextGlyphs(text, font)
}
