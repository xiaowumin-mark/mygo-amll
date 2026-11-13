// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"image"
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// This is the pre-optimization convolution, kept as an independent rendering
// oracle. Weight generation and normalization are intentionally per pixel.
const effectLegacyGaussianShader = `package main
var SourceRect vec4
var SourceOrigin vec2
var Axis vec2
var Sigma float
var AlphaOnly float
var RasterScale float
var Opacity float
func sample(p vec2) vec4 {
 if p.x<SourceRect.x || p.y<SourceRect.y || p.x>=SourceRect.x+SourceRect.z || p.y>=SourceRect.y+SourceRect.w { return vec4(0) }
 c:=image0Sample(p)
 if AlphaOnly>0 { return vec4(c.a,c.a,c.a,c.a) }
 return c
}
func Fragment(dst vec4,pos vec2) vec4 {
 p:=SourceOrigin+pos*RasterScale
 if Sigma<=.00001 { return sample(p)*Opacity }
 radius:=int(ceil(3*Sigma)); total:=float(0); c:=vec4(0)
 for j:=0; j<=384; j++ {
  i:=j-radius
  if i>radius { break }
  w:=exp(-float(i*i)/(2*Sigma*Sigma))
  c+=sample(p+Axis*float(i))*w; total+=w
 }
 return c/max(total,.00001)*Opacity
}`

const effectLegacyCompositeShader = `package main
var SourceRect vec4
var DimRect vec4
var AnnotationRect vec4
var DimOrigin vec2
var AnnotationOrigin vec2
var RasterScale float
var Foreground vec4
var BlackOffset vec2
var BrightOpacity float
var DimOpacity float
var SubOpacity float
var Opacity float
var Sigma float
var Axis vec2
var BlurOnly float
var ContentScale float
var ContentPivot vec2
var ContentClip vec4
func inside(p vec2,r vec4) bool { return r.z>0 && r.w>0 && p.x>=r.x && p.y>=r.y && p.x<r.x+r.z && p.y<r.y+r.w }
func staticPixel(q vec2,r vec4,origin vec2) vec4 {
 p:=r.xy+q-origin
 if !inside(p,r) { return vec4(0) }
 w:=image1Sample(p); b:=vec4(0); bp:=p-BlackOffset
 if bp.x>=0 && bp.y>=0 && bp.x<image2Size().x && bp.y<image2Size().y { b=image2Sample(bp) }
 return vec4(b.rgb+(w.rgb-b.rgb)*Foreground.rgb,w.a)*Foreground.a
}
func sample(q vec2) vec4 {
 if ContentScale>0 { q=(q-ContentPivot)/ContentScale+ContentPivot }
 if BlurOnly<=0 && ContentClip.z>0 && ContentClip.w>0 && !inside(q,ContentClip) { return vec4(0) }
 if !inside(q,SourceRect) { return vec4(0) }
 c:=image0Sample(q)
 if BlurOnly>0 { return c }
 d:=staticPixel(q,DimRect,DimOrigin)
 c=min(vec4(1),c*BrightOpacity+d*DimOpacity)
 a:=staticPixel(q,AnnotationRect,AnnotationOrigin)*SubOpacity
 return a+c*(1-a.a)
}
func Fragment(dst vec4,pos vec2) vec4 {
 q:=pos*RasterScale
 if Sigma<=.00001 { return sample(q)*Opacity }
 radius:=int(ceil(3*Sigma)); total:=float(0); c:=vec4(0)
 for j:=0; j<=384; j++ {
  i:=j-radius
  if i>radius { break }
  w:=exp(-float(i*i)/(2*Sigma*Sigma))
  c+=sample(q+Axis*float(i))*w; total+=w
 }
 return c/max(total,.00001)*Opacity
}`

func effectKernelTestUniforms(program *shader.Shader) map[string]shader.Uniform {
	return map[string]shader.Uniform{
		"Kernel":       program.Uniform("Kernel"),
		"KernelRadius": program.Uniform("KernelRadius"),
		"KernelTotal":  program.Uniform("KernelTotal"),
	}
}

func TestEffectsPrecomputedGaussianMatchesLegacy(t *testing.T) {
	legacy := shader.MustCompile(effectLegacyGaussianShader)
	candidate := shader.MustCompile(effectGaussianShader)
	kernelUniforms := effectKernelTestUniforms(candidate)
	pixels := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := range 3 {
		for x := range 4 {
			a := uint8((x*57 + y*81) % 256)
			pixels.SetRGBA(x, y, color.RGBA{a / 2, a / 3, a / 4, a})
		}
	}
	bitmap := ui.NewBitmap(pixels)
	maxDelta, cases := 0, 0
	sigmaOutputs := make(map[string]bool)
	for _, sigma := range []float32{0, .000005, .00002, .1, .333, .49, .5, .999, 1.25, 5, 20, 64} {
		for _, alpha := range []float32{0, 1} {
			for _, origin := range []ui.Point{{}, {X: -.75, Y: -.25}, {X: .25, Y: .875}} {
				for _, axis := range []ui.Point{{X: 1}, {Y: 1}} {
					for _, scale := range []float32{1, 1.25, 2} {
						render := func(program *shader.Shader, kernel bool) *image.RGBA {
							v := program.NewValues()
							v.Vec4(program.Uniform("SourceRect"), 0, 0, 4, 3).Vec2(program.Uniform("SourceOrigin"), origin.X, origin.Y).Vec2(program.Uniform("Axis"), axis.X, axis.Y).Float(program.Uniform("Sigma"), sigma).Float(program.Uniform("AlphaOnly"), alpha).Float(program.Uniform("RasterScale"), scale).Float(program.Uniform("Opacity"), .73)
							if kernel {
								setEffectKernel(v, kernelUniforms, sigma, nil)
							}
							return ui.Render(func(c *ui.Context) {
								c.Root().Background(ui.Transparent)
								ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
									drawEffectShader(program, p, ui.Rect{W: 3, H: 2}, shader.Options{Images: [4]*ui.Bitmap{bitmap}, Values: v})
								})
							}, 3, 2, scale)
						}
						before, after := render(legacy, false), render(candidate, true)
						if alpha == 0 && origin == (ui.Point{}) && axis.X == 1 && scale == 1 {
							sigmaOutputs[string(before.Pix)] = true
						}
						cases++
						for i, want := range before.Pix {
							delta := int(after.Pix[i]) - int(want)
							if delta < 0 {
								delta = -delta
							}
							maxDelta = max(maxDelta, delta)
							if delta != 0 {
								t.Fatalf("sigma=%g alpha=%g origin=%+v axis=%+v scale=%g byte=%d: got=%d want=%d", sigma, alpha, origin, axis, scale, i, after.Pix[i], want)
							}
						}
					}
				}
			}
		}
	}
	if len(sigmaOutputs) < 3 {
		t.Fatalf("legacy probe failed to distinguish at least three Gaussian kernels: %d outputs", len(sigmaOutputs))
	}
	t.Logf("%d cases: max final 8-bit channel delta=%d", cases, maxDelta)
}

func TestEffectsPrecomputedCompositeMatchesLegacy(t *testing.T) {
	legacy := shader.MustCompile(effectLegacyCompositeShader)
	candidate := shader.MustCompile(effectCompositeShader)
	kernelUniforms := effectKernelTestUniforms(candidate)
	var pixels [3]*image.RGBA
	var bitmaps [3]*ui.Bitmap
	for i := range pixels {
		pixels[i] = image.NewRGBA(image.Rect(0, 0, 4, 3))
	}
	for y := range 3 {
		for x := range 4 {
			a := uint8((x*57 + y*81) % 256)
			pixels[0].SetRGBA(x, y, color.RGBA{a / 2, a / 3, a / 4, a})
			white, black := color.RGBA{a, a, a, a}, color.RGBA{A: a}
			if (x+y)%2 == 0 {
				// Equal white/black RGB models foreground-independent color emoji.
				white, black = color.RGBA{a / 3, a / 2, a / 4, a}, color.RGBA{a / 3, a / 2, a / 4, a}
			}
			pixels[1].SetRGBA(x, y, white)
			pixels[2].SetRGBA(x, y, black)
		}
	}
	for i := range bitmaps {
		bitmaps[i] = ui.NewBitmap(pixels[i])
	}
	states := []struct {
		bright, dim, sub, blurOnly, contentScale float32
		clipWidth, clipHeight                    float32
	}{{1, 0, .3, 0, 1, 4, 3}, {0, 1, .3, 0, .97, 3, 2}, {.73, .51, .44, 0, 1.1, 4, 3}, {1, 1, .3, 1, .97, 3, 2}}
	cases, annotationsVisible := 0, false
	sigmaOutputs := make(map[string]bool)
	for _, sigma := range []float32{0, .000005, .1, .333, .5, 1.25, 5, 20, 64} {
		for _, axis := range []ui.Point{{X: 1}, {Y: 1}} {
			for _, scale := range []float32{1, 1.25, 2} {
				for stateIndex, state := range states {
					render := func(program *shader.Shader, kernel bool, subOpacity float32) *image.RGBA {
						v := program.NewValues()
						v.Vec4(program.Uniform("SourceRect"), 0, 0, 4, 3).Vec4(program.Uniform("DimRect"), 0, 0, 4, 3).Vec4(program.Uniform("AnnotationRect"), 0, 0, 2, 1)
						v.Vec2(program.Uniform("DimOrigin"), .25, .5).Vec2(program.Uniform("AnnotationOrigin"), 1, 1).Vec2(program.Uniform("BlackOffset"), .25, -.125)
						v.Float(program.Uniform("RasterScale"), scale).Color(program.Uniform("Foreground"), ui.Color{R: 200, G: 135, B: 75, A: 190})
						v.Float(program.Uniform("BrightOpacity"), state.bright).Float(program.Uniform("DimOpacity"), state.dim).Float(program.Uniform("SubOpacity"), subOpacity).Float(program.Uniform("Opacity"), .73)
						v.Float(program.Uniform("Sigma"), sigma).Vec2(program.Uniform("Axis"), axis.X, axis.Y).Float(program.Uniform("BlurOnly"), state.blurOnly)
						v.Float(program.Uniform("ContentScale"), state.contentScale).Vec2(program.Uniform("ContentPivot"), .75, 1.25).Vec4(program.Uniform("ContentClip"), .25, .125, state.clipWidth, state.clipHeight)
						if kernel {
							setEffectKernel(v, kernelUniforms, sigma, nil)
						}
						return ui.Render(func(c *ui.Context) {
							c.Root().Background(ui.Transparent)
							ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
								drawEffectShader(program, p, ui.Rect{W: 3, H: 2}, shader.Options{Images: [4]*ui.Bitmap{bitmaps[0], bitmaps[1], bitmaps[2]}, Values: v})
							})
						}, 3, 2, scale)
					}
					before, after := render(legacy, false, state.sub), render(candidate, true, state.sub)
					if axis.X == 1 && scale == 1 && stateIndex == 0 {
						sigmaOutputs[string(before.Pix)] = true
						if sigma == 0 {
							withoutAnnotations := render(legacy, false, 0)
							annotationsVisible = string(before.Pix) != string(withoutAnnotations.Pix)
						}
					}
					cases++
					for i, want := range before.Pix {
						if after.Pix[i] != want {
							t.Fatalf("sigma=%g axis=%+v scale=%g state=%d byte=%d: got=%d want=%d", sigma, axis, scale, stateIndex, i, after.Pix[i], want)
						}
					}
				}
			}
		}
	}
	if !annotationsVisible {
		t.Fatal("composite probe clipped or erased every annotation pixel")
	}
	if len(sigmaOutputs) < 3 {
		t.Fatalf("legacy probe failed to distinguish at least three composite kernels: %d outputs", len(sigmaOutputs))
	}
	t.Logf("%d cases: final 8-bit channel delta=0", cases)
}

func TestEffectsKernelCacheRestoresValuesAndStaysBounded(t *testing.T) {
	program := shader.MustCompile(`package main
var Kernel [193]float
var KernelRadius int
var KernelTotal float
func Fragment(dst vec4,pos vec2) vec4 {
 return vec4(Kernel[KernelRadius],KernelTotal/200,float(KernelRadius)/192,1)
}`)
	uniforms, values := effectKernelTestUniforms(program), program.NewValues()
	render := func() *image.RGBA {
		return ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
				drawEffectShader(program, p, ui.Rect{W: 1, H: 1}, shader.Options{Values: values})
			})
		}, 1, 1, 1)
	}
	var first, second effectKernel
	setEffectKernel(values, uniforms, 1.25, &first)
	want := first
	wantPixel := render().RGBAAt(0, 0)
	setEffectKernel(values, uniforms, 64, &second)
	if render().RGBAAt(0, 0) == wantPixel {
		t.Fatal("fixture must distinguish the two kernels in submitted Values")
	}
	setEffectKernel(values, uniforms, 1.25, &first)
	if first != want {
		t.Fatal("reusing a kernel changed its numerical state")
	}
	if render().RGBAAt(0, 0) != wantPixel {
		t.Fatal("reusing a cached kernel did not restore Values overwritten by another pass")
	}
	setEffectKernel(values, uniforms, .1, &first)
	if first.radius != 1 || first.sigma != .1 || first.total <= 0 {
		t.Fatal("changing sigma did not replace the cached kernel")
	}
	setEffectKernel(values, uniforms, 64, &first)
	if first.radius != 192 || len(first.weights) != 193 {
		t.Fatal("maximum supported sigma changed the bounded kernel storage")
	}
	if got := testing.AllocsPerRun(100, func() { setEffectKernel(values, uniforms, 64, &first) }); got != 0 {
		t.Fatalf("warm kernel allocated: %g", got)
	}
	if got := testing.AllocsPerRun(100, func() { setEffectKernel(values, uniforms, 1.25, nil) }); got != 0 {
		t.Fatalf("stack kernel allocated: %g", got)
	}
}
