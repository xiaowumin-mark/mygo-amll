package main

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func geometryDocument(t testing.TB, lines []lyric.Line) *lyric.Document {
	t.Helper()
	document, err := lyric.NewDocument(lines)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestGeometryTextUnits(t *testing.T) {
	words := []string{"A", "😊", "e\u0301", "漢"}
	line := lyric.Line{Text: strings.Join(words, ""), End: 4 * time.Second}
	for i, text := range words {
		line.Words = append(line.Words, lyric.Word{Text: text, Start: time.Duration(i) * time.Second, End: time.Duration(i+1) * time.Second})
	}
	document := geometryDocument(t, []lyric.Line{line})
	geometry, err := buildLineGeometry(document.Line(0), ui.Font{Size: 20}, 300)
	if err != nil {
		t.Fatal(err)
	}
	want := []wordTextRange{
		{0, 1, 0, 1, 0, 1},
		{1, 5, 1, 2, 1, 3},
		{5, 8, 2, 4, 3, 5},
		{8, 11, 4, 5, 5, 6},
	}
	for i, word := range geometry.words {
		if word.text != want[i] {
			t.Errorf("word %d ranges = %+v, want %+v", i, word.text, want[i])
		}
		if len(word.rects) == 0 {
			t.Errorf("word %d has no visual rectangles", i)
		}
	}
	if geometry.bytes <= 0 || geometry.bytes > geometryByteLimit {
		t.Fatalf("invalid estimate: %d", geometry.bytes)
	}
}

func TestGeometryRejectsAmbiguousAndOversizedText(t *testing.T) {
	for _, test := range []struct {
		name string
		line lyric.Line
	}{
		{"different text", lyric.Line{Text: "whole", End: 1, Words: []lyric.Word{{Text: "other", End: 1}}}},
		{"uncovered text", lyric.Line{Text: "whole", End: 1, Words: []lyric.Word{{Text: "who", End: 1}}}},
		{"invalid UTF8", lyric.Line{Text: "\xff", End: 1}},
		{"split UTF8", lyric.Line{Text: "é", End: 1, Words: []lyric.Word{{Text: "\xc3", End: 1}, {Text: "\xa9", End: 1}}}},
		{"byte budget", lyric.Line{Text: strings.Repeat("a", lineByteLimit+1), End: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := geometryDocument(t, []lyric.Line{test.line})
			if geometry, err := buildLineGeometry(document.Line(0), ui.Font{}, 300); geometry != nil || err == nil {
				t.Fatalf("invalid input returned %v, %v", geometry, err)
			}
		})
	}
	line := lyric.Line{Text: "a", End: 1, Words: make([]lyric.Word, lineWordLimit+1)}
	for i := range line.Words {
		line.Words[i] = lyric.Word{End: 1}
	}
	document := geometryDocument(t, []lyric.Line{line})
	if _, err := buildLineGeometry(document.Line(0), ui.Font{}, 300); !errors.Is(err, errGeometryLimit) {
		t.Fatalf("word budget: %v", err)
	}
	for _, width := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		if _, err := buildLineGeometry(document.Line(0), ui.Font{}, width); err == nil {
			t.Fatalf("accepted width %v", width)
		}
	}
}

func TestGeometryWrapAndClusterPolicy(t *testing.T) {
	document := geometryDocument(t, []lyric.Line{{Text: "wrap this complete line across several rows", End: 1}})
	wide, err := buildLineGeometry(document.Line(0), ui.Font{Size: 20}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := buildLineGeometry(document.Line(0), ui.Font{Size: 20}, 80)
	if err != nil {
		t.Fatal(err)
	}
	if narrow.height <= wide.height || len(narrow.words[0].rects) <= 1 {
		t.Fatalf("expected wrapped full-line geometry: wide %v, narrow %v, rects %v", wide.height, narrow.height, narrow.words[0].rects)
	}
	// A timing boundary within a combining-character grapheme must not cause
	// partial grapheme coloring, regardless of which system font is selected.
	document = geometryDocument(t, []lyric.Line{{Text: "e\u0301", End: 2, Words: []lyric.Word{
		{Text: "e", End: 1}, {Text: "\u0301", Start: 1, End: 2},
	}}})
	combined, err := buildLineGeometry(document.Line(0), ui.Font{Size: 20}, 300)
	if err != nil {
		t.Fatal(err)
	}
	if !combined.words[0].atomic || !combined.words[1].atomic {
		t.Fatalf("split grapheme must be atomic: %+v", combined.words)
	}
	// Synthetic public glyph metadata makes the ligature and mixed bidi policy
	// deterministic without requiring a platform-specific font or glyph IDs.
	for _, test := range []struct {
		name   string
		glyphs []ui.Glyph
		range_ wordTextRange
		atomic bool
		rtl    bool
	}{
		{"ligature boundary", []ui.Glyph{{Cluster: 0, Runes: 3}}, wordTextRange{startRune: 1, endRune: 2}, true, false},
		{"whole ligature", []ui.Glyph{{Cluster: 0, Runes: 3}}, wordTextRange{endRune: 3}, false, false},
		{"RTL visual order", []ui.Glyph{{Cluster: 1, Runes: 1, RTL: true}, {Cluster: 0, Runes: 1, RTL: true}}, wordTextRange{endRune: 2}, false, true},
		{"mixed directions", []ui.Glyph{{Cluster: 0, Runes: 1}, {Cluster: 1, Runes: 1, RTL: true}}, wordTextRange{endRune: 2}, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			word := wordGeometry{text: test.range_}
			auditWordGlyphs(&word, test.glyphs)
			if word.atomic != test.atomic || word.rtl != test.rtl {
				t.Fatalf("policy = atomic %v, RTL %v", word.atomic, word.rtl)
			}
		})
	}
}

func TestHighlightClipsSeekRTLAndReuse(t *testing.T) {
	geometry := lineGeometry{words: []wordGeometry{
		{start: 0, end: 10, rects: []ui.Rect{{X: 2, Y: 0, W: 20, H: 10}, {X: 0, Y: 10, W: 20, H: 10}}},
		{start: 10, end: 20, rtl: true, rects: []ui.Rect{{X: 50, W: 20, H: 10}}},
		{start: 20, end: 30, atomic: true, rects: []ui.Rect{{X: 80, W: 20, H: 10}}},
	}}
	buffer := make([]ui.Rect, 0, 4)
	for _, test := range []struct {
		position time.Duration
		want     []ui.Rect
	}{
		{-1, nil},
		{0, nil},
		{5, []ui.Rect{{X: 2, W: 20, H: 10}}},
		{15, []ui.Rect{{X: 2, W: 20, H: 10}, {Y: 10, W: 20, H: 10}, {X: 60, W: 10, H: 10}}},
		{25, []ui.Rect{{X: 2, W: 20, H: 10}, {Y: 10, W: 20, H: 10}, {X: 50, W: 20, H: 10}}},
		{30, []ui.Rect{{X: 2, W: 20, H: 10}, {Y: 10, W: 20, H: 10}, {X: 50, W: 20, H: 10}, {X: 80, W: 20, H: 10}}},
		{5, []ui.Rect{{X: 2, W: 20, H: 10}}},
	} {
		got := geometry.appendHighlights(buffer[:0], test.position)
		if !slices.Equal(got, test.want) {
			t.Fatalf("at %v: got %v, want %v", test.position, got, test.want)
		}
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		buffer = geometry.appendHighlights(buffer[:0], 15)
	}); allocs != 0 {
		t.Fatalf("cached highlighting allocated %v times", allocs)
	}
}

func TestGeometryCacheBoundsInvalidationAndRelease(t *testing.T) {
	lines := make([]lyric.Line, 40)
	for i := range lines {
		lines[i] = lyric.Line{Text: "cache test", Start: time.Duration(i), End: time.Duration(i + 1)}
	}
	document := geometryDocument(t, lines)
	font := ui.Font{Size: 20}
	var cache geometryCache
	first, err := cache.get(document, 0, font, 300, 1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := cache.get(document, 0, font, 300, 1)
	if err != nil || again != first || cache.shapes != 1 {
		t.Fatalf("cache miss for unchanged layout: %v, %d", err, cache.shapes)
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		if _, err := cache.get(document, 0, font, 300, 1); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("warm cache lookup allocated %v times", allocs)
	}
	for i := 1; i < len(lines); i++ {
		if _, err := cache.get(document, i, font, 300, 1); err != nil {
			t.Fatal(err)
		}
		if len(cache.entries) > geometryEntryLimit || cache.bytes > geometryByteLimit {
			t.Fatalf("cache exceeded budget: %d entries, %d bytes", len(cache.entries), cache.bytes)
		}
	}
	for _, entry := range cache.entries {
		if entry.line == 0 {
			t.Fatal("oldest unused layout was not evicted")
		}
	}
	// Each independent geometry key change drops the preceding generation.
	for _, test := range []struct {
		doc   *lyric.Document
		font  ui.Font
		width float32
		scale float32
	}{
		{document, font, 301, 1},
		{document, font, 301, 2},
		{document, ui.Font{Size: 22}, 301, 2},
		{geometryDocument(t, lines), ui.Font{Size: 22}, 301, 2},
	} {
		if _, err := cache.get(test.doc, 0, test.font, test.width, test.scale); err != nil {
			t.Fatal(err)
		}
		if len(cache.entries) != 1 || cache.shapes != 1 {
			t.Fatal("key change retained geometry from previous generation")
		}
	}
	cache.clear()
	if cache.document != nil || cache.entries != nil || cache.bytes != 0 {
		t.Fatalf("clear retained document or geometry: %+v", cache)
	}
	if _, err := cache.get(nil, 0, font, 300, 1); err == nil {
		t.Fatal("nil document accepted")
	}
	if _, err := cache.get(document, -1, font, 300, 1); err == nil {
		t.Fatal("invalid line accepted")
	}
}

func TestGeometryCacheByteBudget(t *testing.T) {
	lines := make([]lyric.Line, 16)
	for i := range lines {
		lines[i] = lyric.Line{Text: strings.Repeat("a", 4096), Start: time.Duration(i), End: time.Duration(i + 1)}
	}
	document := geometryDocument(t, lines)
	var cache geometryCache
	for i := range lines {
		if _, err := cache.get(document, i, ui.Font{Size: 20}, 300, 1); err != nil {
			t.Fatal(err)
		}
		if cache.bytes > geometryByteLimit {
			t.Fatalf("cache exceeded byte budget: %d", cache.bytes)
		}
	}
	if len(cache.entries) >= len(lines) {
		t.Fatal("expected byte budget to evict layouts before entry budget")
	}
}

func TestGeometryCacheFramePins(t *testing.T) {
	lines := make([]lyric.Line, geometryEntryLimit+2)
	for i := range lines {
		lines[i] = lyric.Line{Text: "frame pin", Start: time.Duration(i), End: time.Duration(i + 1)}
	}
	document := geometryDocument(t, lines)
	font := ui.Font{Size: 20}
	var cache geometryCache
	cache.beginFrame()
	first, err := cache.get(document, 0, font, 300, 1)
	if err != nil {
		t.Fatal(err)
	}
	cache.pin(first)
	for i := 1; i <= geometryEntryLimit; i++ {
		if _, err := cache.get(document, i, font, 300, 1); err != nil {
			t.Fatal(err)
		}
	}
	got, err := cache.get(document, 0, font, 300, 1)
	if err != nil || got != first {
		t.Fatalf("pinned oldest layout was evicted: %v", err)
	}
	for _, entry := range cache.entries {
		cache.pin(entry.geometry)
	}
	if geometry, err := cache.get(document, geometryEntryLimit+1, font, 300, 1); geometry != nil || !errors.Is(err, errGeometryLimit) {
		t.Fatalf("all pinned entries must reject admission: %v, %v", geometry, err)
	}
	if len(cache.entries) != geometryEntryLimit {
		t.Fatal("rejected admission changed the frame's retained entries")
	}
	cache.beginFrame()
	if _, err := cache.get(document, geometryEntryLimit+1, font, 300, 1); err != nil {
		t.Fatalf("next frame did not release pins: %v", err)
	}
	cache.clear()
	if cache.entries != nil || cache.document != nil || cache.bytes != 0 {
		t.Fatal("clear retained frame layouts")
	}
}

func TestGeometryCachePinnedByteBudget(t *testing.T) {
	lines := make([]lyric.Line, 4)
	for i := range lines {
		lines[i] = lyric.Line{Text: strings.Repeat("a", lineByteLimit), Start: time.Duration(i), End: time.Duration(i + 1)}
	}
	document := geometryDocument(t, lines)
	font := ui.Font{Size: 20}
	var cache geometryCache
	cache.beginFrame()
	pinned := 0
	for i := range lines {
		geometry, err := cache.get(document, i, font, 300, 1)
		if errors.Is(err, errGeometryLimit) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		cache.pin(geometry)
		pinned++
	}
	if pinned == 0 || pinned == len(lines) {
		t.Fatalf("expected admission followed by byte-budget rejection; admitted %d", pinned)
	}
	previousBytes := cache.bytes
	if geometry, err := cache.get(document, pinned, font, 300, 1); geometry != nil || !errors.Is(err, errGeometryLimit) {
		t.Fatalf("pinned geometry must share the byte budget: %v, %v", geometry, err)
	}
	if cache.bytes != previousBytes || len(cache.entries) != pinned {
		t.Fatal("rejected layout evicted the current frame's geometry")
	}
	cache.beginFrame()
	if geometry, err := cache.get(document, pinned, font, 300, 1); err != nil || geometry == nil {
		t.Fatalf("unpin should allow bounded eviction next frame: %v", err)
	}
	if cache.bytes > geometryByteLimit || len(cache.entries) > geometryEntryLimit {
		t.Fatal("next frame exceeded the shared budget")
	}
}

func BenchmarkCachedHighlights(b *testing.B) {
	geometry := lineGeometry{words: []wordGeometry{
		{end: time.Second, rects: []ui.Rect{{W: 200, H: 30}, {Y: 30, W: 100, H: 30}}},
	}}
	buffer := make([]ui.Rect, 0, 2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer = geometry.appendHighlights(buffer[:0], time.Duration(i%1000)*time.Millisecond)
	}
}
