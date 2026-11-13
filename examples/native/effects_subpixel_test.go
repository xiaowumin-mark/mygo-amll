// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Slow motion must move retained pixels at every fractional device position,
// including at nonintegral DPI. A correctly sampled atlas alone is insufficient
// if the final draw primitive rounds its destination before sampling.
func TestEffectsSubpixelTranslation(t *testing.T) {
	if err := compileEffectPrograms(); err != nil {
		t.Fatal(err)
	}
	impulse := image.NewRGBA(image.Rect(0, 0, 5, 5))
	impulse.SetRGBA(2, 2, color.RGBA{255, 80, 20, 255})
	bitmap := ui.NewBitmap(impulse)
	for _, scale := range []float32{1, 1.25, 2} {
		for axis := range 2 {
			var previous float64
			for step := range 9 {
				offset := float32(step) / 8
				x, y := float32(4), float32(4)
				if axis == 0 {
					x += offset / scale
				} else {
					y += offset / scale
				}
				values := effectPrograms.gaussian.NewValues()
				img := ui.Render(func(c *ui.Context) {
					c.Root().Background(ui.Transparent)
					ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
						effectGaussian(p, ui.Rect{X: x, Y: y, W: 5 / scale, H: 5 / scale}, bitmap, ui.Rect{W: 5, H: 5}, ui.Point{}, ui.Point{}, 0, scale, 1, false, values)
					})
				}, 16, 16, scale)
				var mass, moment float64
				for py := range img.Bounds().Dy() {
					for px := range img.Bounds().Dx() {
						a := float64(img.RGBAAt(px, py).A)
						position := float64(px) + .5
						if axis == 1 {
							position = float64(py) + .5
						}
						mass += a
						moment += a * position
					}
				}
				got, want := moment/mass, float64(4*scale)+2.5+float64(offset)
				if mass < 250 || math.Abs(got-want) > .015 {
					t.Fatalf("scale=%g axis=%d offset=%g: centroid=%g want=%g alpha=%g", scale, axis, offset, got, want, mass)
				}
				if step > 0 && got <= previous {
					t.Fatalf("scale=%g axis=%d offset=%g: motion stalled at %g", scale, axis, offset, got)
				}
				previous = got
			}
		}
	}
}
