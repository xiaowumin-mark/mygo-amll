// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

func effectAlphaCentroid(img *image.RGBA) (x, y, mass float64) {
	for py := img.Bounds().Min.Y; py < img.Bounds().Max.Y; py++ {
		for px := img.Bounds().Min.X; px < img.Bounds().Max.X; px++ {
			a := float64(img.RGBAAt(px, py).A)
			mass += a
			x += a * (float64(px) + .5)
			y += a * (float64(py) + .5)
		}
	}
	return x / mass, y / mass, mass
}

// This exercises the ordinary word animator and its real shader submission,
// including the growing primitive used when a word floats above its mask box.
// An impulse substitutes for font rasterization, so slow motion is checked
// without platform fonts or a screenshot golden. The painter's unit scale
// models a Canvas: entry.scale already converts the word to physical pixels.
func TestEffectsWordFloatMovesEveryFrame(t *testing.T) {
	if err := compileEffectPrograms(); err != nil {
		t.Fatal(err)
	}
	impulse := image.NewRGBA(image.Rect(0, 0, 12, 12))
	impulse.SetRGBA(6, 6, color.RGBA{255, 255, 255, 255})
	white, black := ui.NewBitmap(impulse), ui.NewBitmap(image.NewRGBA(impulse.Bounds()))
	for _, scale := range []float32{1, 1.25, 2} {
		for _, fps := range []int{60, 120} {
			for _, background := range []bool{false, true} {
				t.Run(fmt.Sprintf("scale=%g/fps=%d/background=%t", scale, fps, background), func(t *testing.T) {
					g := &lineGeometry{dynamic: true, end: time.Second, blocks: []wordBlock{{x: 4 / scale, y: 4 / scale, width: 12 / scale, height: 12 / scale, fontSize: 32, end: time.Second}}}
					animation := motion.NewFloat(0, time.Second, false)
					entry := effectLine{geometry: g, scale: scale, white: white, black: black, path: newEffectMaskPath(g), wordValues: effectPrograms.word.NewValues(), words: []effectWord{{under: effectTile{rect: ui.Rect{W: 12, H: 12}}, float: animation}}}
					style := effectStyle{BrightMaskAlpha: 1, DarkMaskAlpha: 1, Background: background}
					previous := math.Inf(1)
					for frame := range 13 {
						position := time.Duration(frame) * time.Second / time.Duration(fps)
						img := ui.Render(func(c *ui.Context) {
							c.Root().Background(ui.Transparent)
							ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) { entry.drawWords(p, position, style) })
						}, 24, 24, 1)
						_, got, mass := effectAlphaCentroid(img)
						travel := animation.Sample(position) * 32 * float64(scale)
						if background {
							travel *= 2
						}
						want := 10.5 + travel
						if mass < 250 || math.Abs(got-want) > .015 {
							t.Fatalf("frame=%d: centroid=%g want=%g alpha=%g", frame, got, want, mass)
						}
						if got >= previous {
							t.Fatalf("frame=%d: ordinary-word float stalled at %g (previous %g)", frame, got, previous)
						}
						previous = got
					}
				})
			}
		}
	}
}

// The subpixel draw must retain its parent's scale and pivot as well as its
// translation. A uniform rectangle has an analytic centroid even when its
// antialiased edges fall between pixels; no shader-internal coordinate math is
// needed to predict the result.
func TestEffectsSubpixelDrawComposesParentTransforms(t *testing.T) {
	program := shader.MustCompile("package main\nfunc Fragment(dst vec4, pos vec2) vec4 { return vec4(1) }")
	box := ui.Rect{X: 3.25, Y: 4.375, W: 4, H: 8}
	for _, scale := range []float32{1, 1.25, 2} {
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				p.Transform(ui.Translation(.35, .65), 0, 0, func() {
					p.Transform(ui.Scaling(1.5, .75), 2, 3, func() {
						drawEffectShader(program, p, box, shader.Options{})
					})
				})
			})
		}, 32, 32, scale)
		x, y, mass := effectAlphaCentroid(img)
		wantX := (2 + (float64(box.X+box.W/2)-2)*1.5 + .35) * float64(scale)
		wantY := (3 + (float64(box.Y+box.H/2)-3)*.75 + .65) * float64(scale)
		// Raster coverage of transformed hard rectangle edges is less precise
		// than sampling the impulse above. An eighth of a physical pixel still
		// distinguishes this composition from snapping the animated origin.
		if mass == 0 || math.Abs(x-wantX) > .125 || math.Abs(y-wantY) > .125 {
			t.Fatalf("scale=%g: centroid=(%g,%g) want=(%g,%g), alpha=%g", scale, x, y, wantX, wantY, mass)
		}
	}
}
