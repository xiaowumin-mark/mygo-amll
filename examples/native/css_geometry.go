// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"runtime"

	"github.com/egoist/mygo/ui"
)

// System DPI scales CSS padding and containing widths before 26.6 physical
// layout quantization. Results remain DIPs; the optional scale is actual window
// DPI, distinct from CDP's capture-DPR emulation. This concerns box dimensions,
// not the raw computed-style padding used by the reference mask animator.
func cssLayoutLength(value float32, scales ...float32) float32 {
	scale := cssGeometryScale(scales)
	return float32(math.Floor(cssPhysicalLayoutUnits(value, scale)) / (64 * scale))
}

// Inputs retain authored CSS values in float32. At 1.25x, decimal 15.15 is
// exactly 1212 physical layout units, while its float32 encoding is just below
// 1212. Half an input ULP removes that representation bias without allocating
// a decimal string or changing a value a full representable step below a grid
// boundary. This is numerical input precision, not a visual error tolerance.
func cssPhysicalLayoutUnits(value float32, scale float64) float64 {
	next := math.Nextafter32(value, float32(math.Inf(1)))
	halfULP := float64(next-value) / 2
	return (float64(value) + halfULP) * scale * 64
}

func cssGeometryScale(scales []float32) float64 {
	if len(scales) != 0 && scales[0] > 0 && !math.IsInf(float64(scales[0]), 0) {
		return float64(scales[0])
	}
	return 1
}

// Inline unitless leading is resolved against Blink's quantized physical font
// size and truncated to a layout unit. Explicit em leading (Roman/Ruby) resolves
// against the authored size and rounds instead. Empty Ruby min-height is a box
// length, so it uses cssLayoutLength(font.Size, scale), not this text-line height.
func cssInlineLineHeight(font ui.Font, unitless bool, scales ...float32) float32 {
	scale := cssGeometryScale(scales)
	return float32(cssPhysicalLineHeight(font, unitless, scale) / scale)
}

func cssPhysicalLineHeight(font ui.Font, unitless bool, scale float64) float64 {
	if unitless {
		size := float32(math.Round(cssPhysicalLayoutUnits(font.Size, scale)) / 64)
		return math.Floor(cssPhysicalLayoutUnits(size*font.LineHeight, 1)) / 64
	}
	return math.Round(cssPhysicalLayoutUnits(font.Size*font.LineHeight, scale)) / 64
}

// cssInlineLineBox describes nonempty inline text slots, separately from the
// standalone caption boxes retained by cssLineBox. Width remains the measured
// public layout width: the line-height evidence does not establish a different
// horizontal rounding rule. Drawing offsets move glyphs, never the CSS box.
func cssInlineLineBox(layout *ui.TextLayout, font ui.Font, unitless bool, scales ...float32) (width, height, drawY float32) {
	width, actualHeight := layout.Size()
	rows := layout.Lines()
	lines := max(1, len(rows))
	scale := cssGeometryScale(scales)
	physicalLineHeight := cssPhysicalLineHeight(font, unitless, scale)
	lineHeight := float32(physicalLineHeight / scale)
	height = lineHeight * float32(lines)
	if runtime.GOOS == "windows" {
		if len(rows) != 0 {
			// An inline font uses its rounded physical 26.6 size for the primary-font
			// strut too. At YaHei 21.21/30.3 this changes a rounded metric by
			// one pixel; fallback glyph maxima must not replace this strut.
			metricsFont := font
			metricsFont.Size = float32(math.Round(cssPhysicalLayoutUnits(font.Size, scale)) / 64)
			metrics := metricsFont.Metrics()
			ascent, descent := math.Round(float64(metrics.Ascent)), math.Round(float64(metrics.Descent))
			baseline := ascent + math.Floor((physicalLineHeight-ascent-descent)/2)
			drawY = float32(baseline/scale) - rows[0].Baseline
		}
		return width, height, drawY
	}
	drawY = (lineHeight - actualHeight/float32(lines)) / 2
	return width, height, drawY
}

// Standalone captions use explicit CSS lengths and the raw primary font size,
// unlike inline text's quantized-font strut. Keep the historical non-Windows
// centering fallback; the exact primary-font evidence is Windows-specific.
func cssBlockLineHeight(font ui.Font, scales ...float32) float32 {
	if runtime.GOOS != "windows" {
		return font.Size * font.LineHeight
	}
	scale := cssGeometryScale(scales)
	return float32(cssPhysicalLineHeight(font, false, scale) / scale)
}

func cssBlockLineBox(layout *ui.TextLayout, font ui.Font, scales ...float32) (width, height, drawY float32) {
	width, actualHeight := layout.Size()
	rows := layout.Lines()
	lines := max(1, len(rows))
	lineHeight := cssBlockLineHeight(font, scales...)
	height = lineHeight * float32(lines)
	if runtime.GOOS == "windows" {
		if len(rows) != 0 {
			scale := cssGeometryScale(scales)
			metricsFont := font
			metricsFont.Size *= float32(scale)
			metrics := metricsFont.Metrics()
			ascent, descent := math.Round(float64(metrics.Ascent)), math.Round(float64(metrics.Descent))
			physicalLineHeight := cssPhysicalLineHeight(font, false, scale)
			baseline := ascent + math.Floor((physicalLineHeight-ascent-descent)/2)
			drawY = float32(baseline/scale) - rows[0].Baseline
		}
		return width, height, drawY
	}
	drawY = (lineHeight - actualHeight/float32(lines)) / 2
	return width, height, drawY
}

// The BG main wrapper's 1.2em padding cancels a separate -1em margin. Each box
// length is quantized independently; rounding their raw .2em difference loses
// a layout unit for several supported font sizes.
func cssBackgroundMainInset(size float32, scales ...float32) float32 {
	return cssLayoutLength(1.2*size, scales...) - cssLayoutLength(size, scales...)
}
