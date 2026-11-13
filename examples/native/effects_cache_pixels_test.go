// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func discardEffectPaintCache(r *effectRenderer) {
	for _, entry := range r.entries {
		entry.cache.sourceReady, entry.cache.blurReady = false, false
	}
	for _, group := range r.groups {
		group.cache.sourceReady, group.cache.blurReady = false, false
	}
}

type compactPixelGroup struct {
	layout, paint ui.Rect
}

type compactPixelResources map[*effectGroup]compactPixelGroup

// Production reserves whole texture buckets and support for 5-DIP blur. This
// software oracle needs only its actual ink and .75-DIP blur. Retain the original
// lookup area during PrepareGroup, then restore the compact drawing area without
// recreating any surface unless its actual ink bounds change.
func (resources compactPixelResources) prepare(r *effectRenderer) {
	for _, group := range r.groups {
		if previous, ok := resources[group]; ok {
			group.area = previous.layout
		}
	}
}

func compactPixelUnion(a, b ui.Rect) ui.Rect {
	if a.W <= 0 || a.H <= 0 {
		return b
	}
	x, y := min(a.X, b.X), min(a.Y, b.Y)
	return ui.Rect{X: x, Y: y, W: max(a.X+a.W, b.X+b.W) - x, H: max(a.Y+a.H, b.Y+b.H) - y}
}

func compactPixelArea(ink ui.Rect, scale, margin float32) (ui.Rect, int, int) {
	x := float32(math.Floor(float64((ink.X - margin) * scale)))
	y := float32(math.Floor(float64((ink.Y - margin) * scale)))
	right := float32(math.Ceil(float64((ink.X + ink.W + margin) * scale)))
	bottom := float32(math.Ceil(float64((ink.Y + ink.H + margin) * scale)))
	w, h := max(1, int(right-x)), max(1, int(bottom-y))
	return ui.Rect{X: x / scale, Y: y / scale, W: float32(w) / scale, H: float32(h) / scale}, w, h
}

func compactPixelLayerInk(ink ui.Rect, layer effectGroupLayer) ui.Rect {
	return ui.Rect{X: layer.OriginX + (layer.X+ink.X-layer.OriginX)*layer.Scale, Y: layer.OriginY + (layer.Y+ink.Y-layer.OriginY)*layer.Scale, W: ink.W * layer.Scale, H: ink.H * layer.Scale}
}

func (resources compactPixelResources) compact(r *effectRenderer, layers []effectGroupLayer, background effectGroupBackground) {
	const maxBlur = float32(.75)
	for _, entry := range r.entries {
		if !entry.pinned {
			continue
		}
		ink, margin := entry.inkArea, 2/entry.scale
		if entry.standalone {
			margin += 3 * maxBlur
			for _, layer := range layers {
				if layer.Geometry == entry.geometry && layer.Style.Opacity != 0 {
					transformed := compactPixelLayerInk(entry.inkArea, layer)
					transformed.X, transformed.Y = transformed.X-layer.X, transformed.Y-layer.Y
					ink = compactPixelUnion(ink, transformed)
				}
			}
		}
		area, width, height := compactPixelArea(ink, entry.scale, margin)
		if area != entry.area || width != entry.width || height != entry.height {
			before := effectCanvasBytes(entry.width, entry.height, entry.standalone, entry.dimOnly)
			after := effectCanvasBytes(width, height, entry.standalone, entry.dimOnly)
			entry.area, entry.width, entry.height = area, width, height
			entry.source, entry.blurX = nil, nil
			if !entry.dimOnly {
				entry.source = ui.NewCanvas(width, height)
			}
			if entry.standalone {
				entry.blurX = ui.NewCanvas(width, height)
			}
			entry.cache.sourceReady, entry.cache.blurReady = false, false
			entry.cache.revision++
			entry.bytes, r.bytes = entry.bytes+after-before, r.bytes+after-before
		}
		if entry.glowX != nil {
			width, height := 1, 1
			for _, word := range entry.words {
				for _, character := range word.characters {
					width = max(width, int(math.Ceil(float64(character.glow.rect.X+character.glow.rect.W)))+1)
					height = max(height, int(math.Ceil(float64(character.glow.rect.Y+character.glow.rect.H)))+1)
				}
			}
			oldWidth, oldHeight := entry.glowX.Size()
			width, height = min(width, oldWidth), min(height, oldHeight)
			if width != oldWidth || height != oldHeight {
				entry.glowX, entry.glowY = ui.NewCanvas(width, height), ui.NewCanvas(width, height)
				for i := range entry.words {
					for j := range entry.words[i].characters {
						entry.words[i].characters[j].lastSigma = -1
					}
				}
				entry.cache.sourceReady, entry.cache.blurReady = false, false
				entry.cache.revision++
				delta := (width*height - oldWidth*oldHeight) * 8
				entry.bytes, r.bytes = entry.bytes+delta, r.bytes+delta
			}
		}
	}
	for _, group := range r.groups {
		if !group.pinned || group.source == nil {
			continue
		}
		previous, known := resources[group]
		if !known {
			previous.layout = group.area
		}
		var ink ui.Rect
		for _, layer := range layers {
			if layer.Style.Opacity != 0 {
				entry := r.preparedEntry(layer.Geometry, group.scale)
				ink = compactPixelUnion(ink, compactPixelLayerInk(entry.inkArea, layer))
			}
		}
		if background.Color.A != 0 {
			ink = compactPixelUnion(ink, background.Rect)
		}
		area, width, height := compactPixelArea(ink, group.scale, 3*maxBlur+2/group.scale)
		if !known || previous.paint != area || group.width != width || group.height != height {
			delta := (width*height - group.width*group.height) * 8
			group.source, group.blurX = ui.NewCanvas(width, height), ui.NewCanvas(width, height)
			group.width, group.height = width, height
			group.bytes, r.bytes = group.bytes+delta, r.bytes+delta
			group.cache.sourceReady, group.cache.blurReady = false, false
			group.cache.revision++
		}
		group.area = area
		previous.paint = area
		resources[group] = previous
	}
	for group := range resources {
		alive := false
		for _, current := range r.groups {
			alive = alive || current == group
		}
		if !alive {
			delete(resources, group)
		}
	}
}

// Both renderers run the same exact clock/input sequence. The oracle discards
// every derived paint result before drawing, preserving the original complete
// passes. Reuse must produce identical RGBA8 pixels, including fractional screen
// positions, fades and hover's changing sigma; no screenshot tolerance is used.
// Both layer arrangements exercise all 24 events at scale 1; scales 1.25 and 2
// render ten representative states while retaining the same input sequence.
func TestEffectsCachedPixelsMatchCompletePasses(t *testing.T) {
	registerSnapshotFonts(t)
	doc, err := lyric.NewDocument([]lyric.Line{
		{Text: "go A", Translation: "sub", End: 4 * time.Second, Words: []lyric.Word{
			{Text: "go ", End: 3 * time.Second, Romanization: "go", Ruby: []lyric.RubyWord{{Text: "g", End: time.Second}}},
			{Text: "A", Start: 3 * time.Second, End: 4 * time.Second},
		}},
		{Text: "BG", End: 4 * time.Second, Words: []lyric.Word{{Text: "BG", End: 4 * time.Second}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var geometries [2]*lineGeometry
	for i := range geometries {
		geometries[i], err = buildLineGeometryWithStyle(doc.Line(i), ui.Font{Family: snapshotRegularFamily, Size: 4, Weight: 600, LineHeight: 1.2}, 24, geometryStyle{Dynamic: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, grouped := range []bool{false, true} {
		for _, scale := range []float32{1, 1.25, 2} {
			t.Run(fmt.Sprintf("group=%t/scale=%g", grouped, scale), func(t *testing.T) {
				var cached, complete effectRenderer
				cachedResources, completeResources := make(compactPixelResources), make(compactPixelResources)
				defer cached.Close()
				defer complete.Close()
				now := time.Unix(0, 0)
				position := 10 * time.Second
				style := effectStyle{Opacity: 1, BrightOpacity: 1, BrightMaskAlpha: 1, DarkMaskAlpha: .4, Enabled: true, Playing: true, AllowSoftware: true}
				x, y, blur, parentOpacity := float32(8), float32(10), float32(.5), float32(.8)
				layerScale, childY, childOpacity := float32(.97), float32(14), float32(.4)
				var background effectGroupBackground
				var sourceRevision, blurRevision uint64
				var firstEntry *effectLine
				var firstGroup *effectGroup
				for frame := range 24 {
					now = now.Add(16 * time.Millisecond)
					switch frame {
					case 2:
						style.Enabled = false // ordinary reverses; emphasis remains finished.
					case 4:
						now = now.Add(6 * time.Second)
					case 6:
						position, style.Enabled, style.Revision = 500*time.Millisecond, true, 1
					case 8:
						style.Playing = false
					case 10:
						x, y, parentOpacity = 8.125, 10.375, .4
					case 11:
						style.Color, style.HasColor = ui.RGBA(120, 210, 240, .6), true
					case 12:
						style.BrightMaskAlpha, style.DarkMaskAlpha, style.WordFadeWidth = .8, .2, .9
					case 13:
						style.Background = true
					case 14:
						style.BrightOpacity, style.DimOpacity, style.SubOpacity = .7, .3, .6
					case 15:
						blur = 0
					case 16:
						blur = .75
					case 17:
						layerScale, childY = .96, 23.25
					case 18:
						background = effectGroupBackground{Rect: ui.Rect{X: -2, W: 42, H: 47}, Radius: 2, Color: ui.RGBA(40, 20, 60, .2)}
					case 19:
						background.Color.A = 0
						childOpacity = 0 // group -> singleton -> group replaces Canvas bounds.
					case 20:
						childOpacity = .4
					case 21:
						style.Color = ui.Color{} // explicitly transparent, not default white.
					case 22:
						style.Color, style.Playing = ui.RGB(255, 255, 255), true
					}
					if scale != 1 {
						switch frame {
						case 0, 1, 6, 8, 10, 16, 17, 20, 22, 23:
						default:
							continue
						}
					}
					render := func(r *effectRenderer, invalidate bool) *image.RGBA {
						r.BeginFrame()
						resources := cachedResources
						if invalidate {
							resources = completeResources
						}
						resources.prepare(r)
						layers := []effectGroupLayer{{Geometry: geometries[0], Scale: layerScale, OriginX: 3.25, OriginY: 7.5, Style: style}}
						if grouped {
							bg := style
							bg.Background, bg.Opacity = true, childOpacity
							layers = append(layers, effectGroupLayer{Geometry: geometries[1], X: 1.25, Y: childY, Scale: .98, OriginY: 5, Style: bg})
						}
						if err := r.PrepareGroup(geometries[0], layers, scale, ui.Rect{W: 24, H: 24}, background); err != nil {
							t.Fatal(err)
						}
						resources.compact(r, layers, background)
						for _, layer := range layers {
							if err := r.SyncFrame(layer.Geometry, now, position, layer.Style); err != nil {
								t.Fatal(err)
							}
						}
						if invalidate {
							discardEffectPaintCache(r)
						}
						ready := false
						tester := ui.NewTester(func(c *ui.Context) {
							c.Root().Background(ui.Transparent)
							ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
								if !ready {
									return
								}
								if err := r.DrawGroup(p, geometries[0], x, y, layers, parentOpacity, blur, background); err != nil {
									t.Fatal(err)
								}
							})
						}, 40, 48)
						tester.SetScale(scale)
						ready = true
						tester.Frame()
						return tester.Image()
					}
					got, want := render(&cached, false), render(&complete, true)
					if !bytes.Equal(got.Pix, want.Pix) {
						for i := range got.Pix {
							if got.Pix[i] != want.Pix[i] {
								t.Fatalf("frame=%d byte=%d cached=%d complete=%d", frame, i, got.Pix[i], want.Pix[i])
							}
						}
					}
					entry := cached.preparedEntry(geometries[0], scale)
					if frame == 0 {
						_, _, mass := effectAlphaCentroid(got)
						if mass < 100 {
							t.Fatal("pixel oracle fixture must contain visible text")
						}
						sourceRevision = entry.cache.revision
						blurRevision = cached.groups[0].cache.revision
						firstEntry, firstGroup = entry, cached.groups[0]
					}
					if frame == 1 && (entry != firstEntry || cached.groups[0] != firstGroup || entry.cache.revision != sourceRevision || cached.groups[0].cache.revision != blurRevision) {
						t.Fatal("finished content was unnecessarily redrawn as wall time advanced")
					}
				}
			})
		}
	}
}
