// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"

	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

// CSS drawing and the reference player's ResizeObserver measurement have
// different precision under real system DPI. The embedded box describes the
// actual flex layout and transform origin; Measured describes borderBoxSize
// consumed by the original JS layout (26.6 DIPs, even at fractional system DPI).
// The adapter owns no clock or observer. The host retains a previous Measured
// pair when the source's content-box observation would not have fired.
type referenceInterludeMetrics struct {
	motion.InterludeMetrics
	MeasuredWidth, MeasuredHeight float64
}

// measureReferenceInterludeDots quantizes only CSS-resolved box pieces. Raw
// timeline poses, transforms, easing, and the JS caller's .4em margins remain
// governed by the unchanged UI-independent motion implementation.
func measureReferenceInterludeDots(fontSize, windowHeight, paddingX float64, scales ...float32) referenceInterludeMetrics {
	raw := motion.MeasureInterludeDots(fontSize, windowHeight, paddingX)
	if raw.DotDiameter <= 0 {
		return referenceInterludeMetrics{}
	}
	scale := cssGeometryScale(scales)
	units := func(value float64) float64 {
		return math.Floor(cssPhysicalLayoutUnits(float32(value), scale))
	}
	dot, gap := units(raw.DotDiameter), units(raw.Gap)
	px, py, content := units(raw.PaddingX), units(raw.PaddingY), units(raw.ContentHeight)
	width, height := 3*dot+2*gap+2*px, content+2*py
	denominator := 64 * scale
	return referenceInterludeMetrics{
		InterludeMetrics: motion.InterludeMetrics{
			Width: width / denominator, Height: height / denominator,
			ContentHeight: content / denominator, DotDiameter: dot / denominator,
			Gap: gap / denominator, PaddingX: px / denominator, PaddingY: py / denominator,
		},
		MeasuredWidth: math.Floor(width/scale) / 64, MeasuredHeight: math.Floor(height/scale) / 64,
	}
}
