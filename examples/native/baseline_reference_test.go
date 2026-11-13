// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestReferenceHostLanguageMatchesCoreInputContract(t *testing.T) {
	// The pinned Core interfaces.ts LyricLine carries no language field and the
	// DOM line constructor never sets lang; XML language stays TTML metadata.
	// Its page language therefore survives unrelated source model metadata.
	doc := geometryDocument(t, []lyric.Line{{Text: "日本語かな", Language: "ja", End: time.Second}})
	font := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600, Lang: "en"}
	reference, err := buildLineGeometryWithStyle(doc.Line(0), font, 600, geometryStyle{UseHostLanguage: true})
	if err != nil {
		t.Fatal(err)
	}
	if reference.font.Lang != "en" || doc.Line(0).Language() != "ja" {
		t.Fatal("reference rendering changed inherited host language or immutable model metadata")
	}
	legacy, err := buildLineGeometry(doc.Line(0), font, 600)
	if err != nil || legacy.font.Lang != "ja" {
		t.Fatalf("historical model-language behavior changed: %v", err)
	}
}

// These are actual Chrome 157 Windows DOM measurements of a zero-height inline
// baseline marker after My漢, not numbers produced by cssLineBox. Each vector
// was measured at both DPR1 and DPR2; all 120 boxes/baselines were identical.
// The sample deliberately includes CJK fallback under Latin primary families.
func TestReferenceWindowsCSSBaselines(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("recorded Windows/Chromium font metrics")
	}
	for _, file := range []string{"segoeui.ttf", "arial.ttf", "msyh.ttc", "times.ttf"} {
		if _, err := os.Stat(filepath.Join(os.Getenv("WINDIR"), "Fonts", file)); err != nil {
			t.Skip("recorded fixture requires installed Windows fonts")
		}
	}
	cases := []struct {
		family   string
		size     float32
		baseline [3]float32
		height   [3]float32
	}{
		{"Segoe UI", 12, [3]float32{11, 12, 14}, [3]float32{12, 14.40625, 18}},
		{"Segoe UI", 13.5, [3]float32{12, 14, 16}, [3]float32{13.5, 16.203125, 20.25}},
		{"Segoe UI", 15, [3]float32{13, 15, 17}, [3]float32{15, 18, 22.5}},
		{"Segoe UI", 15.15, [3]float32{13, 15, 17}, [3]float32{15.15625, 18.1875, 22.71875}},
		{"Segoe UI", 21, [3]float32{19, 21, 24}, [3]float32{21, 25.203125, 31.5}},
		{"Segoe UI", 21.21, [3]float32{19, 21, 24}, [3]float32{21.203125, 25.453125, 31.8125}},
		{"Segoe UI", 30, [3]float32{27, 30, 34}, [3]float32{30, 36, 45}},
		{"Segoe UI", 30.3, [3]float32{27, 30, 35}, [3]float32{30.296875, 36.359375, 45.453125}},
		{"Segoe UI", 37, [3]float32{34, 37, 43}, [3]float32{37, 44.40625, 55.5}},
		{"Segoe UI", 48, [3]float32{44, 48, 56}, [3]float32{48, 57.59375, 72}},
		{"Arial", 12, [3]float32{10, 11, 13}, [3]float32{12, 14.40625, 18}},
		{"Arial", 13.5, [3]float32{11, 12, 14}, [3]float32{13.5, 16.203125, 20.25}},
		{"Arial", 15, [3]float32{13, 14, 16}, [3]float32{15, 18, 22.5}},
		{"Arial", 15.15, [3]float32{13, 14, 16}, [3]float32{15.15625, 18.1875, 22.71875}},
		{"Arial", 21, [3]float32{18, 20, 23}, [3]float32{21, 25.203125, 31.5}},
		{"Arial", 21.21, [3]float32{18, 20, 23}, [3]float32{21.203125, 25.453125, 31.8125}},
		{"Arial", 30, [3]float32{25, 28, 33}, [3]float32{30, 36, 45}},
		{"Arial", 30.3, [3]float32{25, 28, 33}, [3]float32{30.296875, 36.359375, 45.453125}},
		{"Arial", 37, [3]float32{31, 34, 40}, [3]float32{37, 44.40625, 55.5}},
		{"Arial", 48, [3]float32{40, 45, 52}, [3]float32{48, 57.59375, 72}},
		{"Microsoft YaHei", 12, [3]float32{11, 12, 14}, [3]float32{12, 14.40625, 18}},
		{"Microsoft YaHei", 13.5, [3]float32{11, 13, 15}, [3]float32{13.5, 16.203125, 20.25}},
		{"Microsoft YaHei", 15, [3]float32{13, 15, 17}, [3]float32{15, 18, 22.5}},
		{"Microsoft YaHei", 15.15, [3]float32{13, 15, 17}, [3]float32{15.15625, 18.1875, 22.71875}},
		{"Microsoft YaHei", 21, [3]float32{19, 21, 24}, [3]float32{21, 25.203125, 31.5}},
		{"Microsoft YaHei", 21.21, [3]float32{18, 20, 23}, [3]float32{21.203125, 25.453125, 31.8125}},
		{"Microsoft YaHei", 30, [3]float32{27, 30, 34}, [3]float32{30, 36, 45}},
		{"Microsoft YaHei", 30.3, [3]float32{27, 30, 34}, [3]float32{30.296875, 36.359375, 45.453125}},
		{"Microsoft YaHei", 37, [3]float32{33, 36, 42}, [3]float32{37, 44.40625, 55.5}},
		{"Microsoft YaHei", 48, [3]float32{43, 47, 55}, [3]float32{48, 57.59375, 72}},
		{"Times New Roman", 12, [3]float32{10, 11, 13}, [3]float32{12, 14.40625, 18}},
		{"Times New Roman", 13.5, [3]float32{11, 12, 14}, [3]float32{13.5, 16.203125, 20.25}},
		{"Times New Roman", 15, [3]float32{12, 14, 16}, [3]float32{15, 18, 22.5}},
		{"Times New Roman", 15.15, [3]float32{13, 14, 16}, [3]float32{15.15625, 18.1875, 22.71875}},
		{"Times New Roman", 21, [3]float32{17, 19, 22}, [3]float32{21, 25.203125, 31.5}},
		{"Times New Roman", 21.21, [3]float32{17, 19, 22}, [3]float32{21.203125, 25.453125, 31.8125}},
		{"Times New Roman", 30, [3]float32{25, 28, 33}, [3]float32{30, 36, 45}},
		{"Times New Roman", 30.3, [3]float32{25, 28, 32}, [3]float32{30.296875, 36.359375, 45.453125}},
		{"Times New Roman", 37, [3]float32{31, 34, 40}, [3]float32{37, 44.40625, 55.5}},
		{"Times New Roman", 48, [3]float32{40, 45, 52}, [3]float32{48, 57.59375, 72}},
	}
	for _, c := range cases {
		for i, lh := range []float32{1, 1.2, 1.5} {
			font := ui.Font{Family: c.family, Size: c.size, Weight: 600, LineHeight: lh, Lang: "en"}
			layout := ui.ShapeText("My漢", font, 600)
			_, height, dy := cssLineBox(layout, font)
			baseline := layout.Lines()[0].Baseline + dy
			if math.Abs(float64(baseline-c.baseline[i])) > .00001 || math.Abs(float64(height-c.height[i])) > .00001 {
				t.Errorf("%s size=%g lineHeight=%g: baseline=%g height=%g; measured DOM baseline=%g height=%g", c.family, c.size, lh, baseline, height, c.baseline[i], c.height[i])
			}
		}
	}
}

func TestReferenceMultiRowRubyAndSubtitleWhitespace(t *testing.T) {
	font := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600, LineHeight: 1.2}
	word := lyric.Word{Text: "漢", End: 2 * time.Second, Romanization: "kan\nji", Ruby: []lyric.RubyWord{{Text: "kan\nji", End: 2 * time.Second}}}
	block := buildReferenceWord(word, font, true, true, false, 0)
	// Measured with the actual reference wordWithRuby/romanWord/rubyWord CSS:
	// body36 + two Ruby rows30 + two roman rows30. Extra 1em span padding is
	// canceled by its negative margins; it belongs to the mask, not flow height.
	if block.height != 96 || len(block.rubyParts) != 1 || len(block.rubyParts[0].rows) != 2 || len(block.romanRows) != 2 {
		t.Fatalf("multi-row CSS slots: height=%g, ruby=%+v, roman=%+v", block.height, block.rubyParts, block.romanRows)
	}
	for name, rows := range map[string][]cssTextRow{"ruby": block.rubyParts[0].rows, "roman": block.romanRows} {
		if rows[0].text != "kan" || rows[1].text != "ji" || rows[0].y != 0 || rows[1].y != 15 {
			t.Fatalf("%s did not preserve independent 1em CSS rows: %+v", name, rows)
		}
	}
	doc := geometryDocument(t, []lyric.Line{{Text: "Body", End: time.Second, Translation: "  wrapped\t translation\nsecond row  ", Romanization: "\u00a0 keep\u00a0"}})
	g, err := buildLineGeometry(doc.Line(0), font, 600)
	if err != nil {
		t.Fatal(err)
	}
	if g.annotations[0].text != "wrapped translation second row" || g.annotations[1].text != "\u00a0 keep\u00a0" || g.annotations[0].boxHeight != 22.5 {
		t.Fatalf("subtitle white-space: normal: %+v", g.annotations)
	}
	if doc.Line(0).Translation() != "  wrapped\t translation\nsecond row  " {
		t.Fatal("display whitespace normalization changed the immutable source")
	}
	empty := geometryDocument(t, []lyric.Line{{Text: "Body", End: time.Second, Translation: " \t\r\n ", Romanization: "\f  "}})
	emptyGeometry, err := buildLineGeometry(empty.Line(0), font, 600)
	if err != nil || len(emptyGeometry.annotations) != 0 || emptyGeometry.height != emptyGeometry.bodyHeight {
		t.Fatalf("collapsed empty HTML blocks must not reserve a subtitle line: geometry=%+v, err=%v", emptyGeometry, err)
	}
}
