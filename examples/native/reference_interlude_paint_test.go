// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"image"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

func interludeAlphaCentroid(img *image.RGBA) (x, y, mass float64) {
	for py := img.Bounds().Min.Y; py < img.Bounds().Max.Y; py++ {
		for px := img.Bounds().Min.X; px < img.Bounds().Max.X; px++ {
			alpha := float64(img.RGBAAt(px, py).A)
			mass += alpha
			x += alpha * (float64(px) + .5)
			y += alpha * (float64(py) + .5)
		}
	}
	return x / mass, y / mass, mass
}

func TestReferenceInterludeScaleUsesSubpixelTransform(t *testing.T) {
	metrics := motion.InterludeMetrics{Width: 60, Height: 36, ContentHeight: 20, DotDiameter: 12, Gap: 4, PaddingX: 8, PaddingY: 8}
	box := ui.Rect{W: 160, H: 100}
	for _, deviceScale := range []float32{1, 1.25, 2} {
		t.Run("dpi="+map[float32]string{1: "1", 1.25: "1.25", 2: "2"}[deviceScale], func(t *testing.T) {
			paint := func(scale float64) *image.RGBA {
				pose := motion.InterludePose{Active: true, Opacity: 1, Scale: scale, X: 20, Y: 20, DotOpacities: [3]float64{1, 1, 1}}
				return ui.Render(func(c *ui.Context) {
					c.Root().Background(ui.Transparent)
					ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
						paintReferenceInterludeDots(p, box, pose, metrics, ui.RGB(255, 255, 255))
					})
				}, int(box.W), int(box.H), deviceScale)
			}
			_, _, baseMass := interludeAlphaCentroid(paint(1))
			_, _, scaledMass := interludeAlphaCentroid(paint(1.015))
			areaGrowth := (scaledMass - baseMass) / 255 / float64(deviceScale*deviceScale)
			if areaGrowth < 3 {
				t.Fatalf("device scale=%g quantized dot growth: area change=%g DIP², need >3", deviceScale, areaGrowth)
			}
			previousX := math.Inf(-1)
			for frame := range 11 {
				scale := 1 + float64(frame)*.01
				pose := motion.InterludePose{Active: true, Opacity: 1, Scale: scale, X: 20, Y: 20, DotOpacities: [3]float64{0, 0, 1}}
				img := ui.Render(func(c *ui.Context) {
					c.Root().Background(ui.Transparent)
					ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
						paintReferenceInterludeDots(p, box, pose, metrics, ui.RGB(255, 255, 255))
					})
				}, int(box.W), int(box.H), deviceScale)
				x, y, mass := interludeAlphaCentroid(img)
				if mass < 250 {
					t.Fatalf("scale=%g lost the rendered dot: alpha mass=%g", scale, mass)
				}
				wantX := (pose.X + metrics.Width/2 + (metrics.PaddingX+2*(metrics.DotDiameter+metrics.Gap)+metrics.DotDiameter/2-metrics.Width/2)*scale) * float64(deviceScale)
				wantY := (pose.Y + metrics.Height/2 + (metrics.PaddingY+(metrics.ContentHeight-metrics.DotDiameter)/2+metrics.DotDiameter/2-metrics.Height/2)*scale) * float64(deviceScale)
				if math.Abs(x-wantX) > .35 || math.Abs(y-wantY) > .35 {
					t.Fatalf("scale=%g dpi=%g centroid=(%g,%g), want=(%g,%g)", scale, deviceScale, x, y, wantX, wantY)
				}
				if frame > 0 && x-previousX < .1*float64(deviceScale) {
					t.Fatalf("scale step stalled: dpi=%g frame=%d x=%g previous=%g", deviceScale, frame, x, previousX)
				}
				previousX = x
			}
		})
	}
}
