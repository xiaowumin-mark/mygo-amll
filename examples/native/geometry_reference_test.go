// SPDX-License-Identifier: AGPL-3.0-only
// Golden cases executed against AMLL f32285864056587c4afa59d310208b0aa829cb53,
// packages/core/src/utils/{lyric-split-words,lyric-line-break}.ts, using
// Node v22.19.0 type stripping, ICU 77.1, Intl.Segmenter locale zh-CN.
// Tests consume fixed results; Node/ICU are not required to run the Go suite.
package main

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestReferenceSplitActualUpstreamGoldens(t *testing.T) {
	tests := []struct {
		name  string
		words []lyric.Word
		want  string
	}{
		{"cjk", []lyric.Word{{Text: "你好 世界", End: 4 * time.Second}},
			`[{"word":"你","romanWord":"","startTime":0,"endTime":1000},{"word":"好","romanWord":"","startTime":1000,"endTime":2000},{"word":" ","romanWord":"","startTime":2000,"endTime":2000},{"word":"世","romanWord":"","startTime":2000,"endTime":3000},{"word":"界","romanWord":"","startTime":3000,"endTime":4000}]`},
		{"roman-matched", []lyric.Word{{Text: "你 好", Romanization: "ni hao", End: 2 * time.Second}},
			`[{"word":"你","romanWord":"ni","startTime":0,"endTime":1000},{"word":" ","romanWord":"","startTime":1000,"endTime":1000},{"word":"好","romanWord":"hao","startTime":1000,"endTime":2000}]`},
		{"roman-unmatched", []lyric.Word{{Text: "你好", Romanization: "ni hao", End: 2 * time.Second}},
			`[{"word":"你好","romanWord":"ni hao","startTime":0,"endTime":2000}]`},
		{"non-cjk-syllables", []lyric.Word{{Text: " su", End: 2 * time.Second}, {Text: "gar so", Start: 2 * time.Second, End: 4 * time.Second}, {Text: "sweet", Start: 4 * time.Second, End: 6 * time.Second}},
			`[{"word":" ","romanWord":"","startTime":0,"endTime":0},[{"word":"su","romanWord":"","startTime":0,"endTime":2000},{"word":"gar","romanWord":"","startTime":2000,"endTime":3200}],{"word":" ","romanWord":"","startTime":3200,"endTime":3200},[{"word":"so","romanWord":"","startTime":3200,"endTime":4000},{"word":"sweet","romanWord":"","startTime":4000,"endTime":6000}]]`},
	}
	type goldenWord struct {
		Text  string  `json:"word"`
		Roman string  `json:"romanWord"`
		Start float64 `json:"startTime"`
		End   float64 `json:"endTime"`
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []any
			for _, chunk := range referenceChunks(test.words) {
				var words []goldenWord
				for _, word := range chunk.words {
					words = append(words, goldenWord{word.Text, word.Romanization, float64(word.Start) / float64(time.Millisecond), float64(word.End) / float64(time.Millisecond)})
				}
				if len(words) == 1 {
					got = append(got, words[0])
				} else {
					got = append(got, words)
				}
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected any
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.want), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("normalized words differ from executed reference:\n%s\nwant:\n%s", encoded, test.want)
			}
		})
	}
}

func TestReferenceBalancedBreaksActualUpstreamGoldens(t *testing.T) {
	for _, test := range []struct {
		name     string
		width    float64
		children []balancedChild
		cjk      map[int]bool
		want     []int
	}{
		{"ordinary", 100, []balancedChild{{"a", 40, false}, {"b", 40, false}, {"c", 40, false}, {"d", 40, false}}, nil, []int{2}},
		{"punctuation", 90, []balancedChild{{"foo,", 45, false}, {"bar", 35, false}, {"baz", 30, false}}, nil, []int{1}},
		{"space", 55, []balancedChild{{"a", 20, false}, {" ", 10, true}, {"b", 20, false}, {" ", 10, true}, {"c", 20, false}}, nil, []int{2}},
		{"overflow", 100, []balancedChild{{"long", 150, false}, {" ", 10, true}, {"tail", 20, false}}, nil, []int{1}},
		{"cjk-dictionary-boundaries", 55, []balancedChild{{"机", 15, false}, {"器", 15, false}, {"学", 15, false}, {"习", 15, false}, {"歌", 15, false}, {"词", 15, false}, {"显", 15, false}, {"示", 15, false}}, map[int]bool{2: true, 4: true, 6: true}, []int{2, 5}},
		{"fits", 100, []balancedChild{{"a", 30, false}, {"b", 30, false}}, nil, nil},
		{"empty", 100, nil, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := referenceBalancedBreaks(test.children, test.width, test.cjk); !slices.Equal(got, test.want) {
				t.Fatalf("breaks = %v, want executed reference %v", got, test.want)
			}
		})
	}
	for _, punctuation := range ",;:!?，。；：！？、）】》」』’”)[]>}~…" {
		if !referencePunctuation("word" + string(punctuation)) {
			t.Errorf("missed reference punctuation %q", punctuation)
		}
	}
	if referencePunctuation("word\\") {
		t.Fatal("reference regex does not include a backslash")
	}
}

func TestReferenceUnicodeAndEmphasisRules(t *testing.T) {
	if !referenceCJK("क") || !referenceCJK("漢字カナ") || !referenceCJK("𠀀") || referenceCJK("אבג") || referenceCJK("a漢") {
		t.Fatal("reference's broad CJK predicate was replaced with a different script test")
	}
	if !referenceSpace('\ufeff') || referenceSpace('\u0085') {
		t.Fatal("ECMAScript whitespace was replaced with Go unicode.IsSpace")
	}
	for _, test := range []struct {
		text string
		dur  time.Duration
		want bool
	}{
		{"a", time.Second, false}, {"ab", time.Second - 1, false}, {"ab", time.Second, true},
		{"1234567", time.Second, true}, {"12345678", time.Second, false},
		{"漢", time.Second, true}, {"क", time.Second, true}, {"😊", time.Second, true},
		{"👩‍👩‍👧‍👦", 2 * time.Second, false},
	} {
		if got := referenceEmphasize(lyric.Word{Text: test.text, End: test.dur}); got != test.want {
			t.Errorf("emphasis(%q,%s) = %v want %v", test.text, test.dur, got, test.want)
		}
	}
	// JS split("") exposes two isolated surrogate span contents, but the
	// wrapper's concatenated text is the original supplementary ideograph.
	chunks := referenceChunks([]lyric.Word{{Text: "𠀀", End: 2 * time.Second}})
	if len(chunks) != 1 || chunks[0].text != "𠀀" || len(chunks[0].words) != 2 || chunks[0].words[0].Text != "�" {
		t.Fatalf("supplementary CJK differs from reference UTF-16 grouping: %+v", chunks)
	}
}

func TestReferenceRetainedSyllablesAndIndependentCharacters(t *testing.T) {
	line := lyric.Line{Text: "sugar 你", End: 5 * time.Second, Words: []lyric.Word{
		{Text: "su", End: time.Second}, {Text: "gar ", Start: time.Second, End: 3 * time.Second}, {Text: "你", Start: 3 * time.Second, End: 5 * time.Second},
	}}
	document := geometryDocument(t, []lyric.Line{line})
	geometry, err := buildLineGeometryWithStyle(document.Line(0), ui.Font{Family: "Arial", Size: 30}, 40, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(geometry.blocks) != 3 || len(geometry.groups) != 2 || geometry.blocks[0].text != "su" || geometry.blocks[1].text != "gar" {
		t.Fatalf("timed syllables were flattened or whitespace became a mask target: %+v", geometry.blocks)
	}
	if geometry.blocks[0].y != geometry.blocks[1].y || geometry.blocks[2].y <= geometry.blocks[0].y {
		t.Fatal("unbreakable emphasis wrapper was split to fit the native viewport")
	}
	if geometry.groups[0].characterCount != 5 || geometry.groups[0].anchorCharCount != 5 || geometry.groups[0].isLastWord || !geometry.groups[1].isLastWord {
		t.Fatalf("wrong emphasis wrapper context: %+v", geometry.groups)
	}
	for i, block := range geometry.blocks {
		if !block.shouldEmphasize || len(block.characters) == 0 || block.layout == nil || block.padding != block.fontSize || block.font.LineHeight != 1.2 {
			t.Fatalf("block %d lacks retained character boxes: %+v", i, block)
		}
	}
	if geometry.blocks[1].characters[0].index != 2 {
		t.Fatal("emphasis stagger index restarted at a timed syllable boundary")
	}
	if geometry.start != line.Start || geometry.end != line.End {
		t.Fatal("mask time origin was replaced with first word time")
	}
}

func TestReferenceRubyRomanSlotsAndSubtitleStyles(t *testing.T) {
	line := lyric.Line{Text: "漢字", Translation: "Translation", Romanization: "Line romanization", End: 4 * time.Second, Background: true, Words: []lyric.Word{
		{Text: "漢", Romanization: "kan", End: 2 * time.Second, Ruby: []lyric.RubyWord{{Text: "k", End: time.Second}, {Text: "an", Start: time.Second, End: 2 * time.Second}}},
		{Text: "字", Start: 2 * time.Second, End: 4 * time.Second},
	}}
	document := geometryDocument(t, []lyric.Line{line})
	geometry, err := buildLineGeometryWithStyle(document.Line(0), ui.Font{Family: "Arial", Size: 30, Weight: 700}, 500, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	if geometry.font.Size != 21 || geometry.font.LineHeight != 1.2 || len(geometry.blocks) != 2 {
		t.Fatalf("background CSS size or retained words differ: font=%+v blocks=%d", geometry.font, len(geometry.blocks))
	}
	first, second := geometry.blocks[0], geometry.blocks[1]
	if len(first.rubyParts) != 2 || first.rubyParts[1].x <= first.rubyParts[0].x || first.rubyFont.Size != 10.5 || first.romanFont.Size != 10.5 {
		t.Fatalf("ruby spans were flattened or inner fonts changed: %+v", first)
	}
	if second.bodyY < second.rubyFont.Size || second.romanLayout == nil || first.romanLayout == nil || second.height <= second.bodyY {
		t.Fatal("missing Ruby/roman slot changed neighboring word baselines")
	}
	if geometry.groups[0].anchorCharCount != 3 {
		t.Fatalf("Ruby character count must anchor emphasis: %+v", geometry.groups[0])
	}
	if len(geometry.annotations) != 2 || geometry.annotations[0].text != line.Translation || geometry.annotations[1].text != line.Romanization {
		t.Fatal("subtitle order must be translation before whole-line romanization")
	}
	for _, annotation := range geometry.annotations {
		if annotation.font.Size != 10.5 || annotation.font.LineHeight != 1.5 || annotation.font.Weight != 700 {
			t.Fatalf("subtitle styles did not inherit font weight/CSS sizes: %+v", annotation.font)
		}
	}
	if geometry.annotations[0].boxY != geometry.bodyHeight {
		t.Fatal("native-only subtitle gap was inserted")
	}
}

func TestReferenceDuetAndSubstringLastWord(t *testing.T) {
	line := lyric.Line{Text: "sugar ar", Duet: true, End: 4 * time.Second, Words: []lyric.Word{{Text: "sugar ", End: 2 * time.Second}, {Text: "ar", Start: 2 * time.Second, End: 4 * time.Second}}}
	document := geometryDocument(t, []lyric.Line{line})
	geometry, err := buildLineGeometryWithStyle(document.Line(0), ui.Font{Size: 30}, 130, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(geometry.groups) != 2 || !geometry.groups[0].isLastWord || !geometry.groups[1].isLastWord {
		t.Fatal("reference's substring-based isLastWord rule was replaced by a last-index check")
	}
	rows := make(map[float32]float32)
	for _, block := range geometry.blocks {
		rows[block.y] = max(rows[block.y], block.x+block.width)
	}
	for y, right := range rows {
		if math.Abs(float64(right-130)) > .1 {
			t.Fatalf("duet physical row at y=%g ends at %g, want130", y, right)
		}
	}
}

func TestReferenceGeometryStyleAndNormalizedBudget(t *testing.T) {
	document := geometryDocument(t, []lyric.Line{
		{Text: "first", End: 2 * time.Second, Words: []lyric.Word{{Text: "first", End: time.Second}}},
		{Text: "two words", Start: 2 * time.Second, End: 4 * time.Second, Words: []lyric.Word{{Text: "two ", Start: 2 * time.Second, End: 3 * time.Second}, {Text: "words", Start: 3 * time.Second, End: 4 * time.Second}}},
	})
	var cache geometryCache
	font := ui.Font{Size: 20}
	first, err := cache.get(document, 0, font, 300, 1)
	if err != nil || !first.dynamic || cache.styleDocument != document {
		t.Fatalf("document-wide dynamic classification: %v", err)
	}
	if _, err := cache.get(document, 0, font, 320, 1); err != nil || cache.styleDocument != document {
		t.Fatal("resize discarded cached document-wide classification")
	}
	static, err := cache.getWithStyle(document, 0, font, 320, 1, geometryStyle{})
	if err != nil || static.dynamic || len(static.blocks) != 1 || static.blocks[0].shouldEmphasize {
		t.Fatalf("explicit non-dynamic style was ignored: %v", err)
	}
	cache.clear()
	if cache.styleDocument != nil || cache.document != nil || cache.entries != nil {
		t.Fatal("clear retained derived-style document references")
	}
	long := geometryDocument(t, []lyric.Line{{Text: strings.Repeat("漢", lineWordLimit+1), End: 10 * time.Second}})
	if geometry, err := buildLineGeometryWithStyle(long.Line(0), font, 300, geometryStyle{Dynamic: true}); geometry != nil || !errors.Is(err, errGeometryLimit) {
		t.Fatalf("normalized block budget did not reject oversized input: %v %v", geometry, err)
	}
	tooManyRuby := make([]lyric.RubyWord, lineWordLimit+1)
	for i := range tooManyRuby {
		tooManyRuby[i] = lyric.RubyWord{Text: "r", End: time.Second}
	}
	rubyDocument := geometryDocument(t, []lyric.Line{{Text: "a", End: time.Second, Words: []lyric.Word{{Text: "a", End: time.Second, Ruby: tooManyRuby}}}})
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := buildLineGeometryWithStyle(rubyDocument.Line(0), font, 300, geometryStyle{Dynamic: true}); !errors.Is(err, errGeometryLimit) {
			t.Fatalf("Ruby count preflight: %v", err)
		}
	}); allocs != 0 {
		t.Fatalf("Ruby budget preflight copied the rejected model: %g allocations", allocs)
	}
}

func TestReferenceMixedTextUsesRetainedDOMCharacterBoxes(t *testing.T) {
	line := lyric.Line{Text: "אבג e\u0301 😊", End: 6 * time.Second, Words: []lyric.Word{
		{Text: "אבג ", End: 2 * time.Second},
		{Text: "e\u0301 ", Start: 2 * time.Second, End: 4 * time.Second},
		{Text: "😊", Start: 4 * time.Second, End: 6 * time.Second},
	}}
	document := geometryDocument(t, []lyric.Line{line})
	geometry, err := buildLineGeometryWithStyle(document.Line(0), ui.Font{Family: "Arial", Size: 30}, 500, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(geometry.blocks) != 3 {
		t.Fatalf("spaces became word boxes: %+v", geometry.blocks)
	}
	for i, want := range []int{3, 1, 1} {
		block := geometry.blocks[i]
		if !block.shouldEmphasize || len(block.characters) != want {
			t.Fatalf("block %d: grapheme elements=%d, want%d", i, len(block.characters), want)
		}
		var end float32
		for _, character := range block.characters {
			if character.layout == nil || character.width <= 0 || character.height <= 0 || character.x < end {
				t.Fatalf("invalid independent DOM-span geometry: %+v", character)
			}
			end = character.x + character.width
		}
	}
	// The reference constructs one emphasized element per timed word before
	// grapheme segmentation. A combining mark in the next timed word therefore
	// remains a separate DOM element; legacy shared-cluster clips must not be
	// substituted for this geometry by the effect backend.
	split := geometryDocument(t, []lyric.Line{{Text: "e\u0301", End: 2 * time.Second, Words: []lyric.Word{{Text: "e", End: time.Second}, {Text: "\u0301", Start: time.Second, End: 2 * time.Second}}}})
	geometry, err = buildLineGeometryWithStyle(split.Line(0), ui.Font{Size: 24}, 300, geometryStyle{Dynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(geometry.blocks) != 2 || len(geometry.blocks[0].characters) != 1 || len(geometry.blocks[1].characters) != 1 || geometry.blocks[0].characters[0].layout == geometry.blocks[1].characters[0].layout {
		t.Fatal("cross-timing grapheme was silently changed from the reference's separate spans")
	}
}

func TestReferenceEmptyLineAndSmallFontMinimums(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		document := geometryDocument(t, []lyric.Line{{Text: "  \t\n", End: time.Second}})
		geometry, err := buildLineGeometryWithStyle(document.Line(0), ui.Font{Size: 8}, 300, geometryStyle{Dynamic: dynamic})
		if err != nil {
			t.Fatal(err)
		}
		if len(geometry.blocks) != 0 || geometry.bodyHeight != 0 {
			t.Fatalf("collapsible-only content created an extra native line box: %+v", geometry)
		}
	}
	nbsp := geometryDocument(t, []lyric.Line{{Text: "\u00a0\u00a0", End: time.Second}})
	spaceGeometry, err := buildLineGeometryWithStyle(nbsp.Line(0), ui.Font{Size: 20}, 300, geometryStyle{Dynamic: true})
	if err != nil || spaceGeometry.bodyHeight == 0 {
		t.Fatalf("non-collapsible whitespace lost its CSS line box: %v", err)
	}
	document := geometryDocument(t, []lyric.Line{{Text: "漢", Translation: "Translation", Background: true, End: time.Second}})
	geometry, err := buildLineGeometryWithStyle(document.Line(0), ui.Font{Size: 8}, 300, geometryStyle{})
	if err != nil {
		t.Fatal(err)
	}
	if geometry.font.Size != 10 || len(geometry.annotations) != 1 || geometry.annotations[0].font.Size != 10 {
		t.Fatal("CSS max(...,10px) minimums were omitted")
	}
}

func TestReferenceCSSLineBoxesAndClientMaskMeasurements(t *testing.T) {
	font := ui.Font{Family: "Arial", Size: 30.3, LineHeight: 1.2}
	block := buildReferenceWord(lyric.Word{Text: "Fraction", Romanization: "roman"}, font, true, true, false, 0)
	// Actual reference inline CSS: unitless body36.34375 + empty Ruby min
	// height15.140625 + nonempty typed-em Roman15.15625 at main size30.3.
	// These are different rounding paths, not raw 2.2*font.Size.
	wantHeight := float32(66.640625)
	if math.Abs(float64(block.height-wantHeight)) > .01 {
		t.Fatalf("CSS height %g, want%g; natural font metrics must not stretch slots", block.height, wantHeight)
	}
	if block.romanDrawY >= 0 {
		t.Fatal("fixture must exercise CSS negative half-leading against the actual system font")
	}
	_, naturalRomanHeight := block.romanLayout.Size()
	if naturalRomanHeight <= block.romanFont.Size {
		t.Fatal("fixture font no longer exercises native minimum line-height")
	}
	for name, pair := range map[string][2]float32{
		"width": {block.width, block.maskWidth}, "height": {block.height, block.maskHeight},
	} {
		client := float64(pair[1] + block.padding*2)
		if math.Abs(client-math.Round(client)) > .0001 || math.Abs(float64(pair[0]-pair[1])) > .501 {
			t.Fatalf("%s mixed fractional flow metrics with integer CSS client metrics: %v", name, pair)
		}
	}
}

func TestReferenceWindowsArialDOMMeasurements(t *testing.T) {
	// Executed actual reference CSS in Chrome 157.0.0.0, Windows, DPR1,
	// Arial30.3, using synthetic text. Chrome's 1/64px layout quantization
	// differs from native float geometry; the bounds are explicit rather than
	// claiming pixel identity or silently accepting another platform's font.
	if runtime.GOOS != "windows" {
		t.Skip("fixed Windows/Arial DOM geometry fixture")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("WINDIR"), "Fonts", "arial.ttf")); err != nil {
		t.Skip("fixed DOM fixture requires the system Arial font")
	}
	font := ui.Font{Family: "Arial", Size: 30.3, LineHeight: 1.2}
	plain := buildReferenceWord(lyric.Word{Text: "Fraction", Romanization: "roman"}, font, true, false, false, 0)
	ruby := buildReferenceWord(lyric.Word{Text: "漢", Romanization: "kan", Ruby: []lyric.RubyWord{{Text: "k"}, {Text: "an"}}}, font, true, true, false, 0)
	rtl := buildReferenceWord(lyric.Word{Text: "אבג"}, font, false, false, true, 0)
	for name, measured := range map[string][3]float32{
		"Fraction-flow-width":         {plain.width, 109.453125, .1},
		"Fraction-content-height":     {plain.height, 51.5, .04},
		"Fraction-client-mask-width":  {plain.maskWidth, 109.4, .01},
		"Fraction-client-mask-height": {plain.maskHeight, 51.4, .01},
		"Ruby-content-width":          {ruby.width, 30.3125, .1},
		"Ruby-content-height":         {ruby.height, 66.65625, .04},
		"Ruby-client-mask-width":      {ruby.maskWidth, 30.4, .01},
		"Ruby-client-mask-height":     {ruby.maskHeight, 66.4, .01},
		"Hebrew-grapheme1-width":      {rtl.characters[0].width, 17.0625, .1},
		"Hebrew-grapheme2-x":          {rtl.characters[1].x, 17.0625, .1},
		"Hebrew-grapheme3-x":          {rtl.characters[2].x, 33.5, .1},
	} {
		if math.Abs(float64(measured[0]-measured[1])) > float64(measured[2]) {
			t.Errorf("%s native=%g, executed DOM=%g, tolerance=%g", name, measured[0], measured[1], measured[2])
		}
	}
}

func BenchmarkReferenceWordGeometry(b *testing.B) {
	line := lyric.Line{Text: "Sing it slowly 漢字", End: 7 * time.Second, Translation: "Synthetic translation", Romanization: "Synthetic romanization", Words: []lyric.Word{
		{Text: "Sing ", End: 1200 * time.Millisecond},
		{Text: "it ", Start: 1200 * time.Millisecond, End: 2200 * time.Millisecond},
		{Text: "slowly ", Start: 2200 * time.Millisecond, End: 4200 * time.Millisecond},
		{Text: "漢字", Start: 4200 * time.Millisecond, End: 6200 * time.Millisecond},
	}}
	document := geometryDocument(b, []lyric.Line{line})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buildLineGeometryWithStyle(document.Line(0), ui.Font{Family: "Arial", Size: 30}, 400, geometryStyle{Dynamic: true}); err != nil {
			b.Fatal(err)
		}
	}
}
