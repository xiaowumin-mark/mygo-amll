//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"github.com/egoist/mygo/ui"
	"math"
	"testing"
	"unicode/utf16"
)

// Recorded from actual Chrome 157 DOM Range.getClientRects, Windows, DPR 1,
// Segoe UI 600 30px/36px. Plain AMLL-style inline-block word spans inherit LTR;
// there is no direction/unicode-bidi override. These facts were captured before
// the Native helper existed; they are not numbers produced by its algorithm.
var referenceBidiDOMVectors = []struct {
	text       string
	width      float64
	x, w       []float64
	colorEmoji bool
}{
	{"\u05d0\u05d1\u05d2!", 60.78125, []float64{31.90625, 13.875, 0, 51.65625}, []float64{19.75, 18.046875, 13.890625, 9.125}, false},
	{"\u05d0\u05d1\u05d2?", 64.984375, []float64{31.90625, 13.875, 0, 51.65625}, []float64{19.75, 18.046875, 13.890625, 13.328125}, false},
	{"\u05d0\u05d1\u05d2(12)", 100.328125, []float64{80.578125, 62.546875, 48.65625, 38.703125, 9.96875, 22.03125, 0}, []float64{19.75, 18.046875, 13.90625, 9.96875, 12.078125, 16.671875, 9.96875}, false},
	{"(\u05d0\u05d1\u05d2) 12!", 117.703125, []float64{0, 41.875, 23.84375, 9.96875, 61.625, 71.578125, 79.84375, 91.90625, 108.578125}, []float64{9.96875, 19.75, 18.046875, 13.890625, 9.96875, 8.265625, 12.078125, 16.671875, 9.125}, false},
	{"\u05d0\u05d1\u05d2 12!", 97.765625, []float64{68.890625, 50.859375, 36.96875, 28.734375, 0, 12.0625, 88.640625}, []float64{19.75, 18.046875, 13.90625, 8.25, 12.078125, 16.671875, 9.125}, false},
	{"\u0645\u0631\u062d\u0628\u0627!", 74.28125, []float64{47.578125, 35.0625, 17.53125, 7.5, 0, 65.15625}, []float64{17.578125, 12.53125, 17.546875, 10.046875, 7.515625, 9.125}, false},
	{"\u0645\u0631\u062d\u0628\u0627 (12)!", 131.1875, []float64{104.5, 91.96875, 74.4375, 64.421875, 56.90625, 48.65625, 38.703125, 9.96875, 22.03125, 0, 122.0625}, []float64{17.5625, 12.546875, 17.546875, 10.03125, 7.53125, 8.265625, 9.96875, 12.078125, 16.671875, 9.96875, 9.125}, false},
	{"(\u0645\u0631\u062d\u0628\u0627) 12!", 131.203125, []float64{0, 57.546875, 45.03125, 27.5, 17.46875, 9.96875, 75.125, 85.078125, 93.34375, 105.40625, 122.078125}, []float64{9.96875, 17.578125, 12.53125, 17.546875, 10.046875, 7.515625, 9.96875, 8.265625, 12.078125, 16.671875, 9.125}, false},
	{"\u0645\u0631\u062d\u0628\u0627 ABC!", 139.421875, []float64{47.578125, 35.0625, 17.53125, 7.5, 0, 65.15625, 73.390625, 93.53125, 111.65625, 130.296875}, []float64{17.578125, 12.53125, 17.546875, 10.046875, 7.515625, 8.25, 20.15625, 18.140625, 18.65625, 9.125}, false},
	{"ABC \u05d0\u05d1\u05d2!", 125.9375, []float64{0, 20.140625, 38.25, 56.890625, 97.0625, 79.03125, 65.15625, 116.8125}, []float64{20.15625, 18.125, 18.65625, 8.265625, 19.75, 18.046875, 13.890625, 9.125}, false},
	{"\u2067\u05d0\u05d1\u05d2\u2069!", 60.78125, []float64{0, 31.90625, 13.875, 0, 51.65625, 51.65625}, []float64{0, 19.75, 18.046875, 13.890625, 0, 9.125}, false},
	{"(\u0645\u0631\u062d\u0628\u0627)", 85.09375, []float64{0, 57.546875, 45.03125, 27.5, 17.46875, 9.96875, 75.125}, []float64{9.96875, 17.578125, 12.53125, 17.546875, 10.046875, 7.515625, 9.96875}, false},
	{"(\u05d0\u05d1\u05d2)", 71.59375, []float64{0, 41.875, 23.84375, 9.96875, 61.625}, []float64{9.96875, 19.75, 18.046875, 13.890625, 9.96875}, false},
	{"\u05d0\u05d1\u05d2(12)!", 109.453125, []float64{80.578125, 62.546875, 48.65625, 38.703125, 9.96875, 22.03125, 0, 100.328125}, []float64{19.75, 18.046875, 13.90625, 9.96875, 12.078125, 16.671875, 9.96875, 9.125}, false},
	{"\u0645\u0631\u062d\u0628\u0627(12)", 113.8125, []float64{96.25, 83.71875, 66.1875, 56.171875, 48.65625, 38.703125, 9.96875, 22.03125, 0}, []float64{17.5625, 12.546875, 17.546875, 10.03125, 7.53125, 9.96875, 12.078125, 16.671875, 9.96875}, false},
	{"\u0645\u0631\u062d\u0628\u0627(12)!", 122.9375, []float64{96.25, 83.71875, 66.1875, 56.171875, 48.65625, 38.703125, 9.96875, 22.03125, 0, 113.8125}, []float64{17.5625, 12.546875, 17.546875, 10.03125, 7.53125, 9.96875, 12.078125, 16.671875, 9.96875, 9.125}, false},
	{"\u05d0\u05d1\u05d2\U0001f600!", 101.96875, []float64{31.90625, 13.875, 0, 51.65625, 51.65625, 92.84375}, []float64{19.75, 18.046875, 13.890625, 41.203125, 41.203125, 9.125}, true},
	{"\U0001f600\u05d0\u05d1\u05d2!", 101.984375, []float64{0, 0, 73.109375, 55.078125, 41.203125, 92.859375}, []float64{41.203125, 41.203125, 19.75, 18.046875, 13.890625, 9.125}, true},
	{"\u05d0\u05d1\u05d2\u0301!", 60.78125, []float64{31.90625, 13.875, 0, 0, 51.65625}, []float64{19.75, 18.046875, 13.890625, 13.890625, 9.125}, false},
	{"\u05d0\u05d1\u05d2\U0001f469\u200d\U0001f469\u200d\U0001f467\u200d\U0001f466!", 88.921875, []float64{31.90625, 13.875, 0, 51.65625, 51.65625, 51.65625, 51.65625, 51.65625, 51.65625, 51.65625, 51.65625, 51.65625, 51.65625, 51.65625, 79.796875}, []float64{19.75, 18.046875, 13.890625, 28.15625, 28.15625, 28.15625, 28.15625, 28.15625, 28.15625, 28.15625, 28.15625, 28.15625, 28.15625, 28.15625, 9.125}, true},
	{"\u2066\u05d0\u05d1\u05d2(12)!\u2069", 109.453125, []float64{0, 80.578125, 62.546875, 48.65625, 38.703125, 9.96875, 22.03125, 0, 100.328125, 109.453125}, []float64{0, 19.75, 18.046875, 13.90625, 9.96875, 12.078125, 16.671875, 9.96875, 9.125, 0}, false},
	{"\u2067ABC(12)!\u2069", 114.671875, []float64{0, 9.125, 29.265625, 47.375, 66.015625, 75.96875, 88.046875, 104.703125, 0, 114.671875}, []float64{0, 20.15625, 18.125, 18.65625, 9.96875, 12.09375, 16.671875, 9.96875, 9.125, 0}, false},
	{"\u202a\u05d0\u05d1\u05d2(12)!\u202c", 109.453125, []float64{100.3125, 80.578125, 62.546875, 48.65625, 38.703125, 9.96875, 22.03125, 0, 100.328125, 109.453125}, []float64{0, 19.75, 18.046875, 13.90625, 9.96875, 12.078125, 16.671875, 9.96875, 9.125, 0}, false},
	{"\u202bABC(12)!\u202c", 114.671875, []float64{9.125, 9.125, 29.265625, 47.375, 66.015625, 75.96875, 88.046875, 104.703125, 0, 114.671875}, []float64{0, 20.15625, 18.125, 18.65625, 9.96875, 12.09375, 16.671875, 9.96875, 9.125, 0}, false},
	{"\u202d\u05d0\u05d1\u05d2(12)!\u202c", 109.421875, []float64{0, 0, 19.71875, 37.75, 51.640625, 61.609375, 73.671875, 90.328125, 100.296875, 109.40625}, []float64{0, 19.734375, 18.046875, 13.90625, 9.984375, 12.078125, 16.671875, 9.984375, 9.125, 0}, false},
	{"\u202eABC(12)!\u202c", 114.65625, []float64{114.640625, 94.5, 76.390625, 57.75, 47.796875, 35.71875, 19.0625, 9.109375, 0, 114.65625}, []float64{0, 20.15625, 18.125, 18.65625, 9.96875, 12.09375, 16.671875, 9.96875, 9.125, 0}, false},
	{"a\u202e12(\u05d0\u05d1\u05d2)\u202cb!", 143.171875, []float64{0, 115.96875, 103.890625, 87.234375, 77.28125, 57.546875, 39.515625, 25.625, 15.671875, 115.984375, 115.984375, 134.046875}, []float64{15.671875, 0, 12.09375, 16.671875, 9.96875, 19.75, 18.046875, 13.90625, 9.96875, 0, 18.078125, 9.125}, false},
	{"\u2068\u05d0\u05d1\u05d2! \u2069 XYZ", 130.765625, []float64{0, 49.265625, 31.234375, 17.34375, 8.234375, 0, 69.015625, 69.015625, 77.25, 95.828125, 113.140625}, []float64{0, 19.75, 18.046875, 13.90625, 9.125, 8.25, 0, 8.25, 18.59375, 17.328125, 17.625}, false},
	{"\u05d0\u05d1\u05d2 [12(34)]!", 171.5625, []float64{142.703125, 124.671875, 110.78125, 102.53125, 92.578125, 63.84375, 75.90625, 53.875, 19.921875, 36.5625, 9.953125, 0, 162.4375}, []float64{19.734375, 18.046875, 13.90625, 8.265625, 9.96875, 12.078125, 16.671875, 9.96875, 16.65625, 17.3125, 9.96875, 9.96875, 9.125}, false},
	{"AV!", 46.828125, []float64{0, 18.453125, 37.703125}, []float64{18.46875, 19.265625, 9.125}, false},
	{"To?", 44.875, []float64{0, 13.625, 31.546875}, []float64{13.640625, 17.9375, 13.328125}, false},
	{"VAV \u05d0\u05d1\u05d2!", 124.34375, []float64{0, 17.59375, 36.0625, 55.3125, 95.46875, 77.4375, 63.5625, 115.21875}, []float64{17.609375, 18.484375, 19.25, 8.25, 19.75, 18.046875, 13.890625, 9.125}, false},
	{"(!12)", 57.765625, []float64{0, 9.953125, 19.0625, 31.140625, 47.796875}, []float64{9.96875, 9.125, 12.09375, 16.671875, 9.96875}, false},
}

func TestReferenceBidiRecordedDOMVectors(t *testing.T) {
	font := ui.Font{Family: "Segoe UI", Weight: 600, Size: 30, LineHeight: 1.2}
	var maxX, maxWidth float64
	for _, v := range referenceBidiDOMVectors {
		t.Run(v.text, func(t *testing.T) {
			glyphs, width, exact := referenceTextGlyphs(v.text, font)
			if v.colorEmoji {
				provider, _ := testReferenceEmojiProvider(t)
				shaped := provider.Shape(v.text, font)
				if shaped.Err != nil || len(shaped.Colors) == 0 {
					t.Fatalf("color provider rejected recorded emoji input: %v", shaped.Err)
				}
				glyphs, width, exact = shaped.Glyphs, shaped.Width, shaped.Exact
			}
			if !exact {
				t.Fatal("Windows ICU helper rejected recorded single-line input")
			}
			if math.Abs(float64(width)-v.width) > .12 {
				t.Fatalf("width=%v DOM=%v", width, v.width)
			}
			runes := []rune(v.text)
			offsets := make([]int, len(runes)+1)
			for i, r := range runes {
				offsets[i+1] = offsets[i] + len(utf16.Encode([]rune{r}))
			}
			type cluster struct{ from, to int }
			type extent struct {
				left, right float32
				set         bool
			}
			extents := map[cluster]extent{}
			for _, g := range glyphs {
				if g.Cluster < 0 || g.Runes < 0 || g.Cluster+g.Runes > len(runes) {
					t.Fatalf("glyph rune mapping outside original text: %+v len=%d", g, len(runes))
				}
				if g.Runes == 0 || g.Advance == 0 {
					continue
				}
				key := cluster{g.Cluster, g.Cluster + g.Runes}
				box := extents[key]
				if !box.set {
					box = extent{g.X, g.X + g.Advance, true}
				} else {
					box.left = min(box.left, g.X)
					box.right = max(box.right, g.X+g.Advance)
				}
				extents[key] = box
			}
			for key, box := range extents {
				left, right, found := float64(0), float64(0), false
				for at := offsets[key.from]; at < offsets[key.to]; at++ {
					if v.w[at] <= 0 {
						continue
					}
					if !found {
						left, right, found = v.x[at], v.x[at]+v.w[at], true
					} else {
						left = math.Min(left, v.x[at])
						right = math.Max(right, v.x[at]+v.w[at])
					}
				}
				if !found {
					continue
				}
				dx, dw := math.Abs(float64(box.left)-left), math.Abs(float64(box.right-box.left)-(right-left))
				maxX, maxWidth = math.Max(maxX, dx), math.Max(maxWidth, dw)
				if dx > .12 || dw > .12 {
					t.Fatalf("cluster runes[%d:%d] x/width=(%v,%v), DOM=(%v,%v); original text=%q", key.from, key.to, box.left, box.right-box.left, left, right-left, v.text)
				}
			}
		})
	}
	t.Logf("%d recorded DOM vectors (color emoji via explicit provider): max checked mono cluster x error %g DIP, width error %g DIP", len(referenceBidiDOMVectors), maxX, maxWidth)
}

func TestReferenceBidiPrefixDoesNotAlterKerningOrSource(t *testing.T) {
	font := ui.Font{Family: "Segoe UI", Weight: 600, Size: 30, LineHeight: 1.2}
	for _, text := range []string{"AV!", "To?", "VAV", "ffi", "A\u0301V", "😀AV"} {
		before := text
		_, width, exact := referenceTextGlyphs(text, font)
		if !exact || text != before {
			t.Fatalf("failed/source changed for %q", text)
		}
		var direct float32
		for _, g := range ui.Shape(text, font) {
			direct += g.Advance
		}
		if math.Abs(float64(width-direct)) > 1e-5 {
			t.Fatalf("direction sentinel changed original kerning for %q: %v != %v", text, width, direct)
		}
	}
	for _, text := range []string{"abc\ndef", "abc\rdef", "abc\u2028def", "abc\u2029def", string([]byte{0xff})} {
		if _, _, exact := referenceTextGlyphs(text, font); exact {
			t.Fatalf("unsupported input claimed exact: %q", text)
		}
	}
}

func TestReferenceBidiUTF16OffsetsMapBackToOriginalRunes(t *testing.T) {
	font := ui.Font{Family: "Segoe UI", Weight: 600, Size: 30, LineHeight: 1.2}
	for _, test := range []struct {
		text           string
		cluster, runes int
	}{
		{"A😀B", 1, 1},
		{"אבג😀!", 3, 1},
		{"\u202aאבג😀!\u202c", 4, 1},
		{"אבג👩\u200d👩\u200d👧\u200d👦!", 3, 7},
	} {
		glyphs, _, exact := referenceTextGlyphs(test.text, font)
		if !exact {
			t.Fatalf("bidi resolution failed for %q", test.text)
		}
		found := false
		for _, g := range glyphs {
			if g.Cluster == test.cluster {
				found = true
				if g.Runes != test.runes {
					t.Fatalf("astral/ZWJ cluster uses UTF16 offsets instead of runes: cluster=%d runes=%d want %d", g.Cluster, g.Runes, test.runes)
				}
			}
		}
		if !found {
			t.Fatalf("original rune cluster %d missing for %q", test.cluster, test.text)
		}
	}
}

func BenchmarkReferenceBidiColdGlyphs(b *testing.B) {
	font := ui.Font{Family: "Segoe UI", Weight: 600, Size: 30, LineHeight: 1.2}
	for _, test := range []struct{ name, text string }{{"Latin", "A long synthetic lyric AV To"}, {"RTL", "אבג(12)! مرحبا!"}} {
		b.Run(test.name, func(b *testing.B) {
			b.Run("PublicShape", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					_ = ui.Shape(test.text, font)
				}
			})
			b.Run("ICUResolved", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, _, exact := referenceTextGlyphs(test.text, font); !exact {
						b.Fatal("ICU resolution failed")
					}
				}
			})
		})
	}
}

type referenceFakeBidiEngine struct {
	failure        string
	opened, closed int
	runs           []referenceBidiRun
}

func (e *referenceFakeBidiEngine) available() bool { return e.failure != "unavailable" }
func (e *referenceFakeBidiEngine) open() uintptr {
	if e.failure == "open" {
		return 0
	}
	e.opened++
	return 1
}
func (e *referenceFakeBidiEngine) close(uintptr)                    { e.closed++ }
func (e *referenceFakeBidiEngine) paragraph(uintptr, []uint16) bool { return e.failure != "paragraph" }
func (e *referenceFakeBidiEngine) runCount(uintptr) (int, bool) {
	switch e.failure {
	case "count":
		return 0, false
	case "oversized":
		return int(^uint(0) >> 1), true
	}
	return len(e.runs), true
}
func (e *referenceFakeBidiEngine) visualRun(_ uintptr, index int) (referenceBidiRun, bool) {
	if e.failure == "visual" {
		return referenceBidiRun{}, false
	}
	return e.runs[index], true
}

func TestReferenceBidiFailuresCloseHandlesAndReturnPublicFallback(t *testing.T) {
	font := ui.Font{Family: "Segoe UI", Size: 20}
	for _, failure := range []string{"unavailable", "open", "paragraph", "count", "oversized", "visual", "bounds", "overlap", "missing", "surrogate"} {
		t.Run(failure, func(t *testing.T) {
			text := "ab"
			engine := &referenceFakeBidiEngine{failure: failure, runs: []referenceBidiRun{{start: 0, length: 2}}}
			switch failure {
			case "bounds":
				engine.runs[0].length = 3
			case "overlap":
				engine.runs = []referenceBidiRun{{start: 0, length: 1}, {start: 0, length: 1}}
			case "missing":
				engine.runs[0].length = 1
			case "surrogate":
				text = "A😀B"
				engine.runs = []referenceBidiRun{{start: 2, length: 1}}
			}
			for range 20 {
				got, width, exact := referenceTextGlyphsWithEngine(text, font, engine)
				if exact {
					t.Fatal("failed bridge claimed exact DOM shaping")
				}
				want := ui.Shape(text, font)
				var wantWidth float32
				for _, g := range want {
					wantWidth += g.Advance
				}
				if width != wantWidth || !referencePublicGlyphsEqual(got, want) {
					t.Fatal("failure did not preserve the public Shape fallback")
				}
			}
			if engine.closed != engine.opened {
				t.Fatalf("ICU handles leaked: opened=%d closed=%d", engine.opened, engine.closed)
			}
			if failure != "unavailable" && failure != "open" && engine.closed != 20 {
				t.Fatal("successful handles did not close exactly once per call")
			}
		})
	}
	engine := &referenceFakeBidiEngine{runs: []referenceBidiRun{{start: 0, length: 2}}}
	if _, _, exact := referenceTextGlyphsWithEngine("ab", font, engine); !exact || engine.opened != 1 || engine.closed != 1 {
		t.Fatal("successful bridge did not release its handle")
	}
	engine = &referenceFakeBidiEngine{}
	if _, _, exact := referenceTextGlyphsWithEngine("", font, engine); !exact || engine.opened != 0 {
		t.Fatal("empty string allocated an ICU handle")
	}
}

func referencePublicGlyphsEqual(a, b []ui.Glyph) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		y := b[i]
		if x.Cluster != y.Cluster || x.Runes != y.Runes || x.X != y.X || x.Advance != y.Advance || x.RTL != y.RTL {
			return false
		}
	}
	return true
}
