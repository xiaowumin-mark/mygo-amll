// SPDX-License-Identifier: AGPL-3.0-only
// Reference CSS geometry parameters: AMLL commit
// f32285864056587c4afa59d310208b0aa829cb53,
// packages/core/src/styles/lyric-player.module.css.
package main

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestGeometryUntimedWhitespaceMapping(t *testing.T) {
	document := geometryDocument(t, []lyric.Line{{
		Text: " \t你  hello\n世界  ", End: 3 * time.Second,
		Words: []lyric.Word{
			{Text: "你", End: time.Second},
			{Text: "hello", Start: time.Second, End: 2 * time.Second},
			{Text: "世界", Start: 2 * time.Second, End: 3 * time.Second},
		},
	}})
	geometry, err := buildLineGeometry(document.Line(0), ui.Font{Size: 20}, 300)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []wordTextRange{
		{2, 5, 2, 3, 2, 3},
		{7, 12, 5, 10, 5, 10},
		{13, 19, 11, 13, 11, 13},
	} {
		if geometry.words[i].text != want {
			t.Errorf("word %d: got %+v, want %+v", i, geometry.words[i].text, want)
		}
	}
	for _, text := range []string{"你 x hello", "你 hello !", "! 你 hello"} {
		doc := geometryDocument(t, []lyric.Line{{Text: text, End: 2, Words: []lyric.Word{{Text: "你", End: 1}, {Text: "hello", Start: 1, End: 2}}}})
		if _, err := buildLineGeometry(doc.Line(0), ui.Font{}, 300); err == nil {
			t.Errorf("silently skipped untimed non-whitespace in %q", text)
		}
	}
}

func TestGeometrySharedGraphemeLatestEnd(t *testing.T) {
	for _, texts := range [][]string{{"e", "\u0301"}, {"👩", "\u200d👩\u200d👧\u200d👦"}} {
		line := lyric.Line{Text: strings.Join(texts, ""), End: 2 * time.Second, Words: []lyric.Word{
			{Text: texts[0], End: time.Second},
			{Text: texts[1], Start: time.Second, End: 2 * time.Second},
		}}
		document := geometryDocument(t, []lyric.Line{line})
		geometry, err := buildLineGeometry(document.Line(0), ui.Font{Size: 24}, 300)
		if err != nil {
			t.Fatal(err)
		}
		if len(geometry.clusters) != 1 || !geometry.clusters[0].atomic || geometry.clusters[0].end != 2*time.Second {
			t.Fatalf("%q shared grapheme split or assigned an early end: %+v", line.Text, geometry.clusters)
		}
		for _, position := range []time.Duration{0, time.Second, 2*time.Second - 1} {
			if got := geometry.appendHighlights(nil, position); len(got) != 0 {
				t.Fatalf("%q shared grapheme colored before latest end at %s: %v", line.Text, position, got)
			}
			if got := geometry.appendStaticHighlights(nil, position); len(got) != 0 {
				t.Fatalf("%q static shared grapheme colored early at %s: %v", line.Text, position, got)
			}
		}
		full := geometry.appendHighlights(nil, 2*time.Second)
		if len(full) == 0 || !slices.Equal(full, geometry.appendStaticHighlights(nil, 2*time.Second)) {
			t.Fatalf("%q final animated/static clips differ: %v", line.Text, full)
		}
		if got := geometry.appendHighlights(full[:0], time.Second); len(got) != 0 {
			t.Fatal("seek retained shared grapheme highlighting")
		}
	}
}

func TestGeometryClusterTimingDoesNotHideOtherLetters(t *testing.T) {
	document := geometryDocument(t, []lyric.Line{{Text: "ae\u0301b", End: 2 * time.Second, Words: []lyric.Word{
		{Text: "ae", End: time.Second},
		{Text: "\u0301b", Start: time.Second, End: 2 * time.Second},
	}}})
	geometry, err := buildLineGeometry(document.Line(0), ui.Font{Size: 24}, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(geometry.clusters) != 3 || geometry.clusters[0].atomic || !geometry.clusters[1].atomic || geometry.clusters[2].atomic {
		t.Fatalf("expected ordinary/shared/ordinary clusters: %+v", geometry.clusters)
	}
	if got := geometry.appendHighlights(nil, time.Second); len(got) == 0 {
		t.Fatal("first complete letter must remain visible while its neighbor waits for a shared cluster")
	}
	if got := geometry.appendHighlights(nil, 1900*time.Millisecond); highlightWidth(got) <= highlightWidth(geometry.appendHighlights(nil, time.Second)) {
		t.Fatal("last ordinary letter must fill before the shared cluster's latest end")
	}
}

func TestGeometryMixedBidiUsesClusterDirection(t *testing.T) {
	text := "ab אבג cd"
	document := geometryDocument(t, []lyric.Line{{Text: text, End: 9 * time.Second}})
	geometry, err := buildLineGeometry(document.Line(0), ui.Font{Family: "Arial", Size: 24}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(geometry.clusters) != 9 {
		t.Fatalf("unexpected clusters for %q: %+v", text, geometry.clusters)
	}
	for i, cluster := range geometry.clusters {
		if cluster.atomic {
			t.Fatalf("whole mixed-direction word must not become atomic: cluster %d", i)
		}
		if (i >= 3 && i <= 5) != cluster.rtl {
			t.Fatalf("cluster %d direction RTL=%v", i, cluster.rtl)
		}
	}
	if geometry.clusters[3].rects[0].X <= geometry.clusters[4].rects[0].X || geometry.clusters[4].rects[0].X <= geometry.clusters[5].rects[0].X {
		t.Fatal("Hebrew cluster positions must descend in logical reading order")
	}
	for _, i := range []int{0, 3} {
		cluster := geometry.clusters[i]
		if len(cluster.rects) != 1 {
			t.Fatalf("unexpected single-character geometry: %v", cluster.rects)
		}
		full := cluster.rects[0]
		half := appendProgressRects(nil, cluster.rects, .5, cluster.rtl)
		if len(half) != 1 || math.Abs(float64(half[0].W-full.W/2)) > .01 {
			t.Fatalf("half-fill geometry: full %v, half %v", full, half)
		}
		wantX := full.X
		if cluster.rtl {
			wantX += full.W / 2
		}
		if math.Abs(float64(half[0].X-wantX)) > .01 {
			t.Fatalf("cluster %d filled from the wrong edge: %v", i, half)
		}
	}
	if got := geometry.appendHighlights(nil, 4500*time.Millisecond); len(got) == 0 || highlightWidth(got) >= highlightWidth(geometry.appendHighlights(nil, 9*time.Second)) {
		t.Fatal("mixed bidi phrase did not retain smooth intermediate highlighting")
	}
}

func TestGeometrySharedLigatureUsesWholeSelection(t *testing.T) {
	// ShapeText supplies real retained wrapped geometry; synthetic public glyph
	// metadata makes a cross-timing ligature deterministic across system fonts.
	text := "fi"
	document := geometryDocument(t, []lyric.Line{{Text: text, End: 2, Words: []lyric.Word{{Text: "f", End: 1}, {Text: "i", Start: 1, End: 2}}}})
	geometry, err := buildLineGeometry(document.Line(0), ui.Font{Family: "Times New Roman", Size: 24}, 300)
	if err != nil {
		t.Fatal(err)
	}
	geometry.buildClusters(text, []ui.Glyph{{Cluster: 0, Runes: 2}})
	if len(geometry.clusters) != 1 || !geometry.clusters[0].atomic {
		t.Fatalf("ligature cluster was split: %+v", geometry.clusters)
	}
	if got := geometry.appendHighlights(nil, 1); len(got) != 0 {
		t.Fatalf("shared ligature colored at the first word's end: %v", got)
	}
	full := geometry.appendHighlights(nil, 2)
	want := geometry.layout.SelectionRects(ui.TextInputRange{Start: 0, End: 2})
	if math.Abs(float64(highlightWidth(full)-highlightWidth(want))) > .01 {
		t.Fatalf("ligature did not cover full retained selection: got %v want %v", full, want)
	}
}

func TestGeometryAnnotationsFallbackAndBudget(t *testing.T) {
	line := lyric.Line{Text: "漢字 カナ English 😊", Romanization: "kanji kana English", Translation: "A translated line with enough text to wrap", End: time.Second, Language: "ja"}
	document := geometryDocument(t, []lyric.Line{line})
	font := ui.Font{Family: "mygo-amll-deliberately-unavailable, Arial", Size: 24}
	geometry, err := buildLineGeometry(document.Line(0), font, 120)
	if err != nil {
		t.Fatal(err)
	}
	_, primaryHeight := geometry.layout.Size()
	if len(geometry.annotations) != 2 || geometry.annotations[0].boxY != geometry.bodyHeight || geometry.annotations[1].boxY != geometry.annotations[0].boxY+geometry.annotations[0].boxHeight || geometry.height != geometry.annotations[1].boxY+geometry.annotations[1].boxHeight {
		t.Fatalf("invalid annotation stacking: height=%g primary=%g annotations=%+v", geometry.height, primaryHeight, geometry.annotations)
	}
	for i, cluster := range geometry.clusters {
		if len(cluster.rects) == 0 || cluster.rects[0].H <= 0 {
			t.Fatalf("fallback did not provide usable geometry at cluster %d: %+v", i, cluster)
		}
	}
	for _, text := range []string{"\xff", strings.Repeat("x", lineByteLimit)} {
		line.Translation = text
		doc := geometryDocument(t, []lyric.Line{line})
		if _, err := buildLineGeometry(doc.Line(0), font, 120); err == nil {
			t.Fatalf("accepted invalid or over-budget annotation with length %d", len(text))
		}
	}
}

func TestGeometryActualHighlightReuseAndNanosecondTiming(t *testing.T) {
	document := geometryDocument(t, []lyric.Line{{Text: "mixed אבג\n漢字 e\u0301 😊", End: time.Second}})
	geometry, err := buildLineGeometry(document.Line(0), ui.Font{Size: 24}, 100)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]ui.Rect, 0, geometry.highlightRectCount())
	if allocs := testing.AllocsPerRun(1000, func() {
		buffer = geometry.appendHighlights(buffer[:0], 550*time.Millisecond)
		buffer = geometry.appendStaticHighlights(buffer[:0], time.Second)
	}); allocs != 0 {
		t.Fatalf("actual cached cluster highlighting allocated %g times", allocs)
	}
	short := geometryDocument(t, []lyric.Line{{Text: "several letters", End: 1}})
	geometry, err = buildLineGeometry(short.Line(0), ui.Font{Size: 20}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if clips := geometry.appendHighlights(nil, 0); len(clips) != 0 {
		t.Fatalf("cluster time quantization colored text at its start: %v", clips)
	}
	if clips := geometry.appendHighlights(nil, 1); len(clips) == 0 {
		t.Fatal("short-duration line failed to fill at its end")
	}
}

func TestGeometryBackgroundUsesSmallerRetainedLayout(t *testing.T) {
	for _, size := range []float32{0, 26} {
		document := geometryDocument(t, []lyric.Line{
			{Text: "Background phrase", End: time.Second},
			{Text: "Background phrase", End: time.Second, Background: true},
		})
		primary, err := buildLineGeometry(document.Line(0), ui.Font{Size: size}, 500)
		if err != nil {
			t.Fatal(err)
		}
		background, err := buildLineGeometry(document.Line(1), ui.Font{Size: size}, 500)
		if err != nil {
			t.Fatal(err)
		}
		if background.height >= primary.height {
			t.Fatalf("background height %g must be smaller than primary %g for font size %g", background.height, primary.height, size)
		}
	}
}

func TestGeometryDuetAlignsEveryPhysicalLineAndAnnotationRight(t *testing.T) {
	const width float32 = 160
	for _, text := range []string{
		"A longer phrase wraps across several physical rows with a short final row",
		"אבג דהו זחט יכל מנס עפר צקת אבג דהו זחט",
		"English words wrap here\nאבג דהו זחט יכל מנס עפר צקת\n!?",
	} {
		line := lyric.Line{
			Text: text, Duet: true, End: time.Second,
			Romanization: "Long annotation also wraps onto several rows",
			Translation:  "אבג דהו זחט יכל מנס עפר צקת\nTranslated words with another short last row",
		}
		document := geometryDocument(t, []lyric.Line{line})
		geometry, err := buildLineGeometry(document.Line(0), ui.Font{Family: "Arial", Size: 24}, width)
		if err != nil {
			t.Fatal(err)
		}
		if geometry.width != width {
			t.Fatalf("duet width must describe its complete local coordinate box: got %g want %g", geometry.width, width)
		}
		layouts := []*ui.TextLayout{geometry.layout}
		for _, annotation := range geometry.annotations {
			layouts = append(layouts, annotation.layout)
		}
		for i, layout := range layouts {
			lines := layout.Lines()
			if len(lines) < 2 {
				t.Fatalf("fixture must wrap each body/annotation: text %q layout %d has %d rows", text, i, len(lines))
			}
			for j, row := range lines {
				if math.Abs(float64(row.Rect.X+row.Rect.W-width)) > .1 {
					t.Fatalf("text %q layout %d row %d ends at %g instead of %g: %+v", text, i, j, row.Rect.X+row.Rect.W, width, row)
				}
			}
		}
		wantRTL := strings.HasPrefix(text, "אבג")
		if geometry.layout.Lines()[0].RTL != wantRTL {
			t.Fatalf("physical alignment changed the first paragraph's reading direction for %q", text)
		}
		full := geometry.appendHighlights(nil, time.Second)
		selection := geometry.layout.SelectionRects(ui.TextInputRange{End: int(utf16Length(text))})
		if math.Abs(float64(highlightWidth(full)-highlightWidth(selection))) > .1 {
			t.Fatalf("duet highlight did not use the aligned layout: got width %g want %g", highlightWidth(full), highlightWidth(selection))
		}
	}
}

func utf16Length(text string) utf16Offset {
	var length utf16Offset
	for _, r := range text {
		length++
		if r > 0xffff {
			length++
		}
	}
	return length
}

func highlightWidth(rects []ui.Rect) float32 {
	var width float32
	for _, rect := range rects {
		width += rect.W
	}
	return width
}

func BenchmarkClusterHighlights(b *testing.B) {
	document := geometryDocument(b, []lyric.Line{{Text: "English אבג 漢字 カナ e\u0301 😊 wraps across retained rows", End: time.Second}})
	geometry, err := buildLineGeometry(document.Line(0), ui.Font{Size: 24}, 160)
	if err != nil {
		b.Fatal(err)
	}
	buffer := make([]ui.Rect, 0, geometry.highlightRectCount())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer = geometry.appendHighlights(buffer[:0], time.Duration(i%1000)*time.Millisecond)
	}
}

func BenchmarkLineGeometry(b *testing.B) {
	document := geometryDocument(b, []lyric.Line{{Text: "English אבג 漢字 カナ e\u0301 😊 wraps across retained rows", Romanization: "Synthetic annotation", Translation: "合成翻译", End: time.Second}})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buildLineGeometry(document.Line(0), ui.Font{Size: 24}, 160); err != nil {
			b.Fatal(err)
		}
	}
}
