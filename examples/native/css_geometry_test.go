// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Recorded from Chrome 157 Windows DOM, not values calculated by these helpers.
// My漢 deliberately exercises CJK fallback under each Latin primary family.
// Unitless inherited leading, explicit em leading, and anonymous flex-item text
// were measured separately; their identical-looking computed styles can have
// different layout heights. The original standalone 120-vector fixture stays
// in baseline_reference_test.go and is intentionally not redefined here.
var cssInlineDOMSizes = []struct {
	size            float32
	unitless, typed [3]float32
}{
	{12, [3]float32{12, 14.390625, 18}, [3]float32{12, 14.40625, 18}},
	{13.5, [3]float32{13.5, 16.1875, 20.25}, [3]float32{13.5, 16.203125, 20.25}},
	{15, [3]float32{15, 18, 22.5}, [3]float32{15, 18, 22.5}},
	{15.15, [3]float32{15.15625, 18.1875, 22.734375}, [3]float32{15.15625, 18.1875, 22.71875}},
	{21, [3]float32{21, 25.1875, 31.5}, [3]float32{21, 25.203125, 31.5}},
	{21.21, [3]float32{21.203125, 25.4375, 31.796875}, [3]float32{21.203125, 25.453125, 31.8125}},
	{30, [3]float32{30, 36, 45}, [3]float32{30, 36, 45}},
	{30.3, [3]float32{30.296875, 36.34375, 45.4375}, [3]float32{30.296875, 36.359375, 45.453125}},
	{37, [3]float32{37, 44.390625, 55.5}, [3]float32{37, 44.40625, 55.5}},
	{48, [3]float32{48, 57.59375, 72}, [3]float32{48, 57.59375, 72}},
}

func TestCSSInlineRecordedLineHeights(t *testing.T) {
	for _, c := range cssInlineDOMSizes {
		for i, lh := range []float32{1, 1.2, 1.5} {
			f := ui.Font{Size: c.size, LineHeight: lh}
			if got := cssInlineLineHeight(f, true); got != c.unitless[i] {
				t.Errorf("unitless fs=%g lh=%g got=%g DOM=%g", c.size, lh, got, c.unitless[i])
			}
			if got := cssInlineLineHeight(f, false); got != c.typed[i] {
				t.Errorf("typed fs=%g lh=%g got=%g DOM=%g", c.size, lh, got, c.typed[i])
			}
		}
	}
}

func cssInlineRequireWindowsFonts(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("recorded Windows/Chromium primary-font struts")
	}
	for _, file := range []string{"segoeui.ttf", "arial.ttf", "msyh.ttc", "times.ttf"} {
		if _, err := os.Stat(filepath.Join(os.Getenv("WINDIR"), "Fonts", file)); err != nil {
			t.Skip("recorded fixture requires installed Windows fonts")
		}
	}
}

func TestCSSInlineRecordedWindowsBaselines(t *testing.T) {
	cssInlineRequireWindowsFonts(t)
	families := []struct {
		family   string
		baseline [10][3]float32
	}{
		{"Segoe UI", [10][3]float32{{11, 12, 14}, {12, 14, 16}, {13, 15, 17}, {13, 15, 17}, {19, 21, 24}, {19, 21, 24}, {27, 30, 34}, {27, 30, 35}, {34, 37, 43}, {44, 48, 56}}},
		{"Arial", [10][3]float32{{10, 11, 13}, {11, 12, 14}, {13, 14, 16}, {13, 14, 16}, {18, 20, 23}, {18, 20, 23}, {25, 28, 33}, {25, 28, 33}, {31, 34, 40}, {40, 45, 52}}},
		{"Microsoft YaHei", [10][3]float32{{11, 12, 14}, {11, 13, 15}, {13, 15, 17}, {13, 15, 17}, {19, 21, 24}, {18, 20, 23}, {27, 30, 34}, {27, 30, 34}, {33, 36, 42}, {43, 47, 55}}},
		{"Times New Roman", [10][3]float32{{10, 11, 13}, {11, 12, 14}, {12, 14, 16}, {13, 14, 16}, {17, 19, 22}, {17, 19, 22}, {25, 28, 33}, {25, 28, 32}, {31, 34, 40}, {40, 45, 52}}},
	}
	for _, f := range families {
		for j, c := range cssInlineDOMSizes {
			for i, lh := range []float32{1, 1.2, 1.5} {
				font := ui.Font{Family: f.family, Size: c.size, Weight: 600, LineHeight: lh, Lang: "en"}
				layout := ui.ShapeText("My漢", font, 600)
				nativeWidth, _ := layout.Size()
				for _, unitless := range []bool{true, false} {
					width, height, dy := cssInlineLineBox(layout, font, unitless)
					wantHeight := c.typed[i]
					if unitless {
						wantHeight = c.unitless[i]
					}
					baseline := layout.Lines()[0].Baseline + dy
					if width != nativeWidth || height != wantHeight || baseline != f.baseline[j][i] {
						t.Errorf("family=%s fs=%g lh=%g unitless=%v baseline=%g height=%g DOM=%g/%g", f.family, c.size, lh, unitless, baseline, height, f.baseline[j][i], wantHeight)
					}
				}
			}
		}
	}
}

// Measured using the original lyricMainLine/wordWithRuby/wordBody CSS with
// nonempty typed Roman/Ruby text, and an empty Ruby min-height slot. The 0.5em
// children use a typed 1em line-height, while the body inherits unitless 1.2.
func TestCSSInlineRecordedRomanRubySlots(t *testing.T) {
	cssInlineRequireWindowsFonts(t)
	cases := []struct {
		family                                                                                               string
		size, bodyHeight, bodyBaseline, subHeight, subBaseline, emptyRubyHeight, wordHeight, emptyWordHeight float32
	}{
		{"Segoe UI", 12, 14.390625, 12, 6, 5, 6, 26.390625, 26.390625},
		{"Segoe UI", 13.5, 16.1875, 14, 6.75, 5, 6.75, 29.6875, 29.6875},
		{"Segoe UI", 15, 18, 15, 7.5, 6, 7.5, 33, 33},
		{"Segoe UI", 15.15, 18.1875, 15, 7.578125, 6, 7.5625, 33.34375, 33.328125},
		{"Segoe UI", 21, 25.1875, 21, 10.5, 9, 10.5, 46.1875, 46.1875},
		{"Segoe UI", 21.21, 25.4375, 21, 10.609375, 9, 10.59375, 46.65625, 46.640625},
		{"Segoe UI", 30, 36, 30, 15, 13, 15, 66, 66},
		{"Segoe UI", 30.3, 36.34375, 30, 15.15625, 13, 15.140625, 66.65625, 66.640625},
		{"Segoe UI", 37, 44.390625, 37, 18.5, 16, 18.5, 81.390625, 81.390625},
		{"Segoe UI", 48, 57.59375, 48, 24, 22, 24, 105.59375, 105.59375},
		{"Arial", 12, 14.390625, 11, 6, 5, 6, 26.390625, 26.390625},
		{"Arial", 13.5, 16.1875, 12, 6.75, 5, 6.75, 29.6875, 29.6875},
		{"Arial", 15, 18, 14, 7.5, 6, 7.5, 33, 33},
		{"Arial", 15.15, 18.1875, 14, 7.578125, 6, 7.5625, 33.34375, 33.328125},
		{"Arial", 21, 25.1875, 20, 10.5, 9, 10.5, 46.1875, 46.1875},
		{"Arial", 21.21, 25.4375, 20, 10.609375, 9, 10.59375, 46.65625, 46.640625},
		{"Arial", 30, 36, 28, 15, 13, 15, 66, 66},
		{"Arial", 30.3, 36.34375, 28, 15.15625, 13, 15.140625, 66.65625, 66.640625},
		{"Arial", 37, 44.390625, 34, 18.5, 15, 18.5, 81.390625, 81.390625},
		{"Arial", 48, 57.59375, 45, 24, 20, 24, 105.59375, 105.59375},
		{"Microsoft YaHei", 12, 14.390625, 12, 6, 5, 6, 26.390625, 26.390625},
		{"Microsoft YaHei", 13.5, 16.1875, 13, 6.75, 5, 6.75, 29.6875, 29.6875},
		{"Microsoft YaHei", 15, 18, 15, 7.5, 6, 7.5, 33, 33},
		{"Microsoft YaHei", 15.15, 18.1875, 15, 7.578125, 6, 7.5625, 33.34375, 33.328125},
		{"Microsoft YaHei", 21, 25.1875, 21, 10.5, 9, 10.5, 46.1875, 46.1875},
		{"Microsoft YaHei", 21.21, 25.4375, 20, 10.609375, 9, 10.59375, 46.65625, 46.640625},
		{"Microsoft YaHei", 30, 36, 30, 15, 13, 15, 66, 66},
		{"Microsoft YaHei", 30.3, 36.34375, 30, 15.15625, 13, 15.140625, 66.65625, 66.640625},
		{"Microsoft YaHei", 37, 44.390625, 36, 18.5, 16, 18.5, 81.390625, 81.390625},
		{"Microsoft YaHei", 48, 57.59375, 47, 24, 21, 24, 105.59375, 105.59375},
		{"Times New Roman", 12, 14.390625, 11, 6, 5, 6, 26.390625, 26.390625},
		{"Times New Roman", 13.5, 16.1875, 12, 6.75, 5, 6.75, 29.6875, 29.6875},
		{"Times New Roman", 15, 18, 14, 7.5, 6, 7.5, 33, 33},
		{"Times New Roman", 15.15, 18.1875, 14, 7.578125, 6, 7.5625, 33.34375, 33.328125},
		{"Times New Roman", 21, 25.1875, 19, 10.5, 8, 10.5, 46.1875, 46.1875},
		{"Times New Roman", 21.21, 25.4375, 19, 10.609375, 8, 10.59375, 46.65625, 46.640625},
		{"Times New Roman", 30, 36, 28, 15, 12, 15, 66, 66},
		{"Times New Roman", 30.3, 36.34375, 28, 15.15625, 13, 15.140625, 66.65625, 66.640625},
		{"Times New Roman", 37, 44.390625, 34, 18.5, 15, 18.5, 81.390625, 81.390625},
		{"Times New Roman", 48, 57.59375, 45, 24, 20, 24, 105.59375, 105.59375},
	}
	for _, c := range cases {
		body := ui.Font{Family: c.family, Size: c.size, Weight: 600, LineHeight: 1.2, Lang: "en"}
		sub := body
		sub.Size *= .5
		sub.LineHeight = 1
		bodyLayout, subLayout := ui.ShapeText("My漢", body, 600), ui.ShapeText("My漢", sub, 600)
		_, bh, bdy := cssInlineLineBox(bodyLayout, body, true)
		_, sh, sdy := cssInlineLineBox(subLayout, sub, false)
		if bh != c.bodyHeight || sh != c.subHeight || bodyLayout.Lines()[0].Baseline+bdy != c.bodyBaseline || subLayout.Lines()[0].Baseline+sdy != c.subBaseline {
			t.Errorf("family=%s mainfs=%g body=%g/%g sub=%g/%g DOM body=%g/%g sub=%g/%g", c.family, c.size, bh, bodyLayout.Lines()[0].Baseline+bdy, sh, subLayout.Lines()[0].Baseline+sdy, c.bodyHeight, c.bodyBaseline, c.subHeight, c.subBaseline)
		}
		empty := cssLayoutLength(sub.Size)
		if empty != c.emptyRubyHeight || bh+2*sh != c.wordHeight || bh+sh+empty != c.emptyWordHeight {
			t.Errorf("family=%s fs=%g Ruby min-height or complete slot height changed", c.family, c.size)
		}
	}
}

// Two lines of smaller bottom-aligned inline children retain the parent's
// unitless minimum strut. Values came from separate wrapping DOM measurements.
func TestCSSInlineRecordedWrappedMinimumStrut(t *testing.T) {
	for _, c := range []struct{ size, height float32 }{{15.15, 36.375}, {21, 50.375}, {21.21, 50.875}, {30.3, 72.6875}, {37, 88.78125}} {
		if got := 2 * cssInlineLineHeight(ui.Font{Size: c.size, LineHeight: 1.2}, true); got != c.height {
			t.Errorf("fs=%g wrapped strut=%g DOM=%g", c.size, got, c.height)
		}
	}
}

func TestCSSInlineRecordedBackgroundInsets(t *testing.T) {
	for _, c := range []struct{ size, inset, borderHeight float32 }{{21, 4.1875, 57.6875}, {25.9, 5.1875, 71.21875}, {21.21, 4.234375, 58.28125}} {
		if got := cssBackgroundMainInset(c.size); got != c.inset {
			t.Errorf("fs=%g BG inset=%g DOM=%g", c.size, got, c.inset)
		}
		body := cssInlineLineHeight(ui.Font{Size: c.size, LineHeight: 1.2}, true)
		caption := float32(math.Round(float64(c.size*.5*1.5)*64) / 64)
		border := body + 2*c.inset + caption + 2*cssLayoutLength(.2*c.size)
		if border != c.borderHeight {
			t.Errorf("fs=%g BG border=%g DOM=%g", c.size, border, c.borderHeight)
		}
	}
}
