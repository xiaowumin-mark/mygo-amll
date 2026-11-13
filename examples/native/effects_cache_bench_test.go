// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// effectSubmissionShader keeps the complete uniform layout while replacing
// pixel evaluation in this benchmark process. Tester.Frame synchronously runs
// the CPU pixel interpreter after paint; evaluating the original seven blurred
// rows there would obscure command construction and make calibration impractical.
// Real DrawLine/DrawGroup, Canvas.Draw and shader input copying are retained.
func effectSubmissionShader(b *testing.B, source string) *shader.Shader {
	b.Helper()
	start := strings.Index(source, "func Fragment(")
	if start < 0 {
		b.Fatal("submission benchmark needs a fragment shader")
	}
	open := start + strings.IndexByte(source[start:], '{')
	depth, end := 1, open+1
	for ; end < len(source) && depth > 0; end++ {
		switch source[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	program, err := shader.Compile(source[:open+1] + " return vec4(0) " + source[end-1:])
	if err != nil {
		b.Fatal(err)
	}
	return program
}

func effectSubmissionPrograms(b *testing.B) func() {
	b.Helper()
	if err := compileEffectPrograms(); err != nil {
		b.Fatal(err)
	}
	word, composite, gaussian := effectPrograms.word, effectPrograms.composite, effectPrograms.gaussian
	wordUniforms, compositeUniforms, gaussianUniforms := effectPrograms.wordUniforms, effectPrograms.compositeUniforms, effectPrograms.gaussianUniforms
	bind := func(program *shader.Shader, previous map[string]shader.Uniform) map[string]shader.Uniform {
		next := make(map[string]shader.Uniform, len(previous))
		for name := range previous {
			next[name] = program.Uniform(name)
		}
		return next
	}
	effectPrograms.word = effectSubmissionShader(b, effectWordShader)
	effectPrograms.composite = effectSubmissionShader(b, effectCompositeShader)
	effectPrograms.gaussian = effectSubmissionShader(b, effectGaussianShader)
	effectPrograms.wordUniforms = bind(effectPrograms.word, wordUniforms)
	effectPrograms.compositeUniforms = bind(effectPrograms.composite, compositeUniforms)
	effectPrograms.gaussianUniforms = bind(effectPrograms.gaussian, gaussianUniforms)
	return func() {
		effectPrograms.word, effectPrograms.composite, effectPrograms.gaussian = word, composite, gaussian
		effectPrograms.wordUniforms, effectPrograms.compositeUniforms, effectPrograms.gaussianUniforms = wordUniforms, compositeUniforms, gaussianUniforms
	}
}

// BenchmarkEffectsSceneSubmission measures CPU work inside the actual draw
// callback: retained source updates, Gaussian setup and mygo scene commands.
// It excludes preparation, view/layout and CPU pixel evaluation, and does not
// measure a GPU driver, GPU execution or presentation. Shaders keep all original
// uniforms but return transparent pixels only in this benchmark process.
//
// A public Canvas bounds each recorded batch. It is cleared between submitted
// frames, preventing an O(b.N) scene, while the Tester renders once per <=256
// submissions outside the timer. Paint-only scene submission is the measured
// unit, not a 256-frame batch. The same file works with the pre-cache overlay.
func BenchmarkEffectsSceneSubmission(b *testing.B) {
	restore := effectSubmissionPrograms(b)
	defer restore()
	for _, method := range []string{"DrawLine", "DrawGroup"} {
		for _, mode := range []string{"settled-blur", "playback-blur", "changing-blur", "hover"} {
			b.Run(method+"/"+mode, func(b *testing.B) {
				var r effectRenderer
				defer r.Close()
				var geometries [7]*lineGeometry
				var layers [7][1]effectGroupLayer
				var lines [7]lyric.Line
				for i := range lines {
					text := fmt.Sprintf("Line %d moves gently", i)
					lines[i] = lyric.Line{Text: text, End: 4 * time.Second, Words: []lyric.Word{{Text: "Line ", End: time.Second}, {Text: fmt.Sprintf("%d ", i), Start: time.Second, End: 2 * time.Second}, {Text: "moves ", Start: 2 * time.Second, End: 3 * time.Second}, {Text: "gently", Start: 3 * time.Second, End: 4 * time.Second}}}
				}
				document, err := lyric.NewDocument(lines[:])
				if err != nil {
					b.Fatal(err)
				}
				for i := range geometries {
					g, err := buildLineGeometryWithStyle(document.Line(i), ui.Font{Size: 24, Weight: 600, LineHeight: 1.2}, 300, geometryStyle{Dynamic: true})
					if err != nil {
						b.Fatal(err)
					}
					geometries[i] = g
					style := effectStyle{Opacity: 1, DimOpacity: 1, BrightMaskAlpha: 1, DarkMaskAlpha: .4, AllowSoftware: true, Playing: true}
					if i == 6 {
						style.Enabled, style.BrightOpacity, style.DimOpacity = true, 1, 0
					}
					contentScale := float32(.97)
					if method == "DrawGroup" {
						// Exercise the actual offscreen group path, rather than its
						// single-child >=.95 optimization that delegates to drawLine.
						contentScale = .94
					}
					layers[i][0] = effectGroupLayer{Geometry: g, Scale: contentScale, Style: style}
					if err := r.PrepareGroup(g, layers[i][:], 1, ui.Rect{W: 300, H: g.height}); err != nil {
						b.Fatal(err)
					}
				}
				// Each call resets this scene; only the top-level Canvas references
				// grow during a batch, capped by 256 submissions and seven rows.
				recorder := ui.NewCanvas(960, 740)
				start := time.Unix(0, 0)
				frame, pending, completed := 0, 1, 0
				measuring := false
				var drawErr error
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
						for range pending {
							now := start.Add(time.Duration(frame) * time.Second / 60)
							position := 2*time.Second + time.Duration(frame%60)*time.Second/60
							for i, g := range geometries {
								at := 20 * time.Second
								if i == 6 && mode != "settled-blur" {
									at = position
									// The short active fixture repeats its one-second window;
									// make that reset explicit instead of accumulating clocks.
									layers[i][0].Style.Revision = uint64(frame / 60)
								}
								if err := r.SyncFrame(g, now, at, layers[i][0].Style); err != nil {
									drawErr = err
									return
								}
							}
							recorder.Draw(p, func(cp *ui.Painter) {
								if measuring {
									b.StartTimer()
								}
								for i, g := range geometries {
									blur := float32(1 + i%5)
									if mode == "hover" || i == 6 {
										blur = 0
									} else if mode == "changing-blur" {
										blur *= .5 + float32(frame%60)/120
									}
									style := layers[i][0].Style
									style.Blur = blur
									if method == "DrawLine" {
										drawErr = r.DrawLine(cp, g, 32, 20+float32(i)*90, position, style)
									} else {
										drawErr = r.DrawGroup(cp, g, 32, 20+float32(i)*90, layers[i][:], 1, blur)
									}
									if drawErr != nil {
										break
									}
								}
								if measuring {
									b.StopTimer()
								}
							})
							if drawErr != nil {
								return
							}
							frame++
						}
					})
				}, 960, 740)
				if drawErr != nil {
					b.Fatal(drawErr)
				}
				b.ReportAllocs()
				b.ResetTimer()
				b.StopTimer()
				measuring = true
				for completed < b.N {
					pending = min(256, b.N-completed)
					tt.Frame()
					if drawErr != nil {
						b.Fatal(drawErr)
					}
					completed += pending
				}
				b.ReportMetric(7, "rows/op")
				b.ReportMetric(float64(r.bytes), "effect-B")
			})
		}
	}
}
