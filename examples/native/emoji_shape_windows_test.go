//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/language"
)

func testReferenceEmojiProvider(t *testing.T) (*referenceFontProvider, *emojiColorFont) {
	t.Helper()
	owner, err := openEmojiColorFont(filepath.Join(os.Getenv("WINDIR"), "Fonts", "seguiemj.ttf"))
	if err != nil {
		t.Skipf("system Segoe UI Emoji unavailable: %v", err)
	}
	p := newReferenceFontProvider(owner)
	t.Cleanup(func() { p.Close(); owner.Close() })
	return p, owner
}

func TestReferenceEmojiDOMWidths(t *testing.T) {
	p, _ := testReferenceEmojiProvider(t)
	// Actual Chromium DOM Range/inline-flex widths, captured with system Segoe
	// UI 600 30px and paragraph direction:ltr. Canvas alone loses inherited
	// script context in the Hebrew family-emoji cases and is not this oracle.
	cases := []struct {
		text string
		want float32
	}{
		{"😀", 41.203125},
		{"👩‍👩‍👧‍👦", 37.59375},
		{"אבג😀!", 101.96875},
		{"😀אבג!", 101.984375},
		{"אבג👩‍👩‍👧‍👦!", 88.921875},
		{"👩‍👩‍👧‍👦אבג!", 88.9375},
		{"A\u200b👩‍👩‍👧‍👦", 57.734375},
		{"ע\u200b👩‍👩‍👧‍👦", 46.296875},
		{"ב👩‍👩‍👧‍👦", 46.203125},
		{"👩‍👩‍👧‍👦ב", 46.203125},
		// Further actual DOM vectors cover script transitions, paired punctuation,
		// isolates/overrides and emoji presentation selectors. The system font
		// oracle is recorded independently of this provider's segmentation.
		{"aאבג👩‍👩‍👧‍👦!", 104.59375},
		{"אבג👩‍👩‍👧‍👦a", 95.484375},
		{"(אבג)👩‍👩‍👧‍👦", 99.75},
		{"אבג(abc)👩‍👩‍👧‍👦", 147.578125},
		{"abc(אבג)👩‍👩‍👧‍👦", 157.03125},
		{"مرحبا👩‍👩‍👧‍👦!", 111.859375},
		{"👩‍👩‍👧‍👦مرحبا!", 111.875},
		{"aمرحبا👩‍👩‍👧‍👦!", 127.53125},
		{"مرحبا(abc)👩‍👩‍👧‍👦", 170.515625},
		{"abc(مرحبا)👩‍👩‍👧‍👦", 170.53125},
		{"अक्षर👩‍👩‍👧‍👦!", 112.71875},
		{"👩‍👩‍👧‍👦अक्षर!", 112.71875},
		{"汉字👩‍👩‍👧‍👦!", 106.703125},
		{"👩‍👩‍👧‍👦汉字!", 106.703125},
		{"한글👩‍👩‍👧‍👦!", 106.703125},
		{"👩‍👩‍👧‍👦한글!", 106.703125},
		{"ע\u2066👩‍👩‍👧‍👦\u2069", 46.296875},
		{"a\u2067אבג👩‍👩‍👧‍👦\u2069!", 104.609375},
		{"\u202eA👩‍👩‍👧‍👦אבג\u202c", 109.390625},
		{"😀😀", 82.390625},
		{"a😀 b👩🏽‍💻!", 133.484375},
		{"A❤️ B", 87.703125},
		{"A☀️ B", 87.703125},
		{"A1️⃣ B", 87.703125},
		{"A🇨🇳 B", 77.953125},
		{"A🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f B", 87.703125},
	}
	for _, test := range cases {
		t.Run(test.text, func(t *testing.T) {
			got := p.Shape(test.text, ui.Font{Family: "Segoe UI", Size: 30, Weight: 600})
			if got.Err != nil || !got.Exact || len(got.Colors) == 0 {
				t.Fatalf("shape: exact=%v colors=%d err=%v", got.Exact, len(got.Colors), got.Err)
			}
			if delta := math.Abs(float64(got.Width - test.want)); delta > .06 {
				t.Fatalf("width %.8f, DOM %.8f, delta %.8f", got.Width, test.want, delta)
			}
		})
	}
}

func TestReferenceEmojiPresentationAndCapabilities(t *testing.T) {
	p, owner := testReferenceEmojiProvider(t)
	f := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600}
	for _, text := range []string{"😀", "😀\u200d", "😀\ufe0f\u2060", "👩‍👩‍👧‍👦", "👩🏽‍💻", "🇨🇳", "1️⃣", "❤️", "☀️", "🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f"} {
		got := p.Shape(text, f)
		if got.Err != nil || !got.Exact || len(got.Colors) == 0 || got.Width <= 0 {
			t.Errorf("%q: width=%v colors=%d exact=%v err=%v", text, got.Width, len(got.Colors), got.Exact, got.Err)
		}
		for _, glyph := range got.Colors {
			if glyph.Font != owner || glyph.Size != f.Size || glyph.ID == 0 {
				t.Errorf("%q: invalid borrowed color glyph %+v", text, glyph)
			}
		}
	}
	for _, text := range []string{"ordinary AV ffi", "אבג عرَبِيّ", "♥"} {
		got := p.Shape(text, f)
		baseGlyphs, baseWidth, baseExact := referenceTextGlyphs(text, f)
		if got.Err != nil || got.Exact != baseExact || got.Width != baseWidth || len(got.Colors) != 0 || !referencePublicGlyphsEqual(got.Glyphs, baseGlyphs) {
			t.Errorf("text presentation %q changed: %+v", text, got)
		}
	}
	for _, family := range []string{"", "Segoe UI", "Segoe UI Emoji", "Arial", "Times New Roman", "Calibri", "Consolas"} {
		if family != "" && !p.installed[strings.ToLower(family)] {
			continue
		}
		font := f
		font.Family = family
		if got := p.Shape("😀", font); got.Err != nil || len(got.Colors) == 0 {
			t.Errorf("supported family %q: %v", family, got.Err)
		}
	}
	for _, font := range []ui.Font{{Family: "unverified-monospace", Size: 30}, {Family: "custom-emoji", Size: 30}, {Family: "Segoe UI", Size: 30, Italic: true}, {Family: "Segoe UI", Size: 30, Features: "dist[0:2]=0"}} {
		got := p.Shape("😀", font)
		if got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) || len(got.Colors) != 0 || len(got.Glyphs) == 0 {
			t.Errorf("unsupported font %+v: %+v", font, got)
		}
		plain := p.Shape("abc", font)
		if plain.Err != nil || !plain.Exact {
			t.Errorf("ordinary text with unsupported emoji family failed: %v", plain.Err)
		}
	}
	p.registered["segoe ui emoji"] = true
	if got := p.Shape("😀", f); got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) {
		t.Fatalf("registered font overlay must not be substituted: %+v", got)
	}
}

func TestReferenceEmojiScriptBeforeBidi(t *testing.T) {
	p, _ := testReferenceEmojiProvider(t)
	for _, test := range []struct {
		text string
		at   int
		want language.Script
	}{
		{"👩‍👩‍👧‍👦", 0, language.Common},
		{"אבג👩‍👩‍👧‍👦!", 3, language.Hebrew},
		{"👩‍👩‍👧‍👦אבג!", 0, language.Hebrew},
		{"אבג(abc)👩‍👩‍👧‍👦", 8, language.Hebrew},
		{"abc(אבג)👩‍👩‍👧‍👦", 8, language.Latin},
		{"aאבג👩‍👩‍👧‍👦", 4, language.Hebrew},
		{"ע\u200b👩‍👩‍👧‍👦", 2, language.Hebrew},
		{"ע\u2066👩‍👩‍👧‍👦\u2069", 2, language.Hebrew},
	} {
		scripts := referenceEmojiScripts([]rune(test.text), p.face)
		if scripts[test.at] != test.want {
			t.Errorf("%q at %d: script=%s, want %s", test.text, test.at, scripts[test.at], test.want)
		}
	}
}

func TestReferenceEmojiVS15RecordedDOM(t *testing.T) {
	p, _ := testReferenceEmojiProvider(t)
	mono := newReferenceMonoFallback()
	p.monoFallback = mono
	t.Cleanup(func() { p.monoFallback = nil; mono.Close() })
	// Actual original DOM: Segoe UI 48px, normal style, both weights use
	// Segoe UI Symbol for text presentation. This is not a native-baseline oracle.
	for _, weight := range []int{400, 600} {
		got := p.Shape("❤︎", ui.Font{Family: "Segoe UI", Size: 48, Weight: weight, LineHeight: 1.5})
		if got.Err != nil || !got.Exact || len(got.Colors) != 0 || math.Abs(float64(got.Width-46.640625)) > .03 {
			t.Fatalf("VS15 weight%d width=%v colors=%d exact=%v err=%v", weight, got.Width, len(got.Colors), got.Exact, got.Err)
		}
	}
}

func TestReferenceEmojiAutoBMPRecordedDOM(t *testing.T) {
	p, _ := testReferenceEmojiProvider(t)
	mono := newReferenceMonoFallback()
	p.monoFallback = mono
	t.Cleanup(func() { p.monoFallback = nil; mono.Close() })
	if mono.families[referenceCJKSC] != "Noto Sans SC" {
		t.Skip("recorded source profile requires installed Noto Sans SC")
	}
	for _, test := range []struct {
		text  string
		width float32
		color bool
	}{{"☝", 48, false}, {"⚽", 48, false}, {"⚾", 48, false}, {"⛹", 65.90625, true}, {"✌", 65.90625, true}, {"✍", 65.90625, true}} {
		for _, weight := range []int{400, 600} {
			got := p.Shape(test.text, ui.Font{Family: "Segoe UI", Size: 48, Weight: weight, LineHeight: 1.5})
			if got.Err != nil || !got.Exact || (len(got.Colors) != 0) != test.color || math.Abs(float64(got.Width-test.width)) > .03 {
				t.Errorf("%q weight%d width=%v color=%v exact=%v err=%v", test.text, weight, got.Width, len(got.Colors) != 0, got.Exact, got.Err)
			}
		}
	}
}

func TestReferenceEmojiBorrowedOwnerAndMapping(t *testing.T) {
	p, owner := testReferenceEmojiProvider(t)
	f := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600}
	for _, text := range []string{"A👩‍👩‍👧‍👦B", "אבג😀!", "a\u2067אבג👩🏽‍💻\u2069!", "\u202eA😀אבג\u202c", "😀😀", "a😀\u200e!"} {
		got := p.Shape(text, f)
		if got.Err != nil || !got.Exact || len(got.Colors) == 0 {
			t.Errorf("%q: exact=%v err=%v", text, got.Exact, got.Err)
		}
		for _, glyph := range got.Glyphs {
			if glyph.Cluster < 0 || glyph.Runes < 1 || glyph.Cluster+glyph.Runes > len([]rune(text)) {
				t.Errorf("%q: invalid original rune mapping %+v", text, glyph)
			}
		}
	}
	p.Close()
	p.Close()
	if owner.face == nil {
		t.Fatal("provider closed borrowed font owner")
	}
	if got := p.Shape("😀", f); got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) {
		t.Fatalf("closed provider: %+v", got)
	}
	if got := p.Shape("abc", f); got.Err != nil || !got.Exact {
		t.Fatalf("closed color provider should preserve ordinary shaping: %+v", got)
	}
	var absent *referenceFontProvider
	if got := absent.Shape("abc", f); got.Err != nil || !got.Exact {
		t.Fatalf("nil provider ordinary shaping: %+v", got)
	}
	if got := absent.Shape("😀", f); got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) {
		t.Fatalf("nil provider must expose unavailable color capability: %+v", got)
	}
}

func TestReferenceEmojiSelectionWitnessAndClosedOwner(t *testing.T) {
	p, owner := testReferenceEmojiProvider(t)
	f := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600}
	p.colorFamily = "unverified color font"
	if got := p.Shape("😀", f); got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) || len(got.Colors) != 0 {
		t.Fatalf("different owner family was silently substituted: %+v", got)
	}
	p.colorFamily = "Segoe UI Emoji"
	p.colorVariable = true
	if got := p.Shape("😀", f); got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) {
		t.Fatalf("variable font capability was silently guessed: %+v", got)
	}
	p.colorVariable = false
	for _, text := range []string{"♥\ufe0e\ufe0f", "😀\ufe0e\ufe0f"} {
		if got := p.Shape(text, f); got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) {
			t.Errorf("ambiguous presentation %q: %+v", text, got)
		}
	}
	owner.Close()
	if got := p.Shape("😀", f); got.Exact || !errors.Is(got.Err, errReferenceEmojiCapability) {
		t.Fatalf("closed owner: %+v", got)
	}
}

func BenchmarkReferenceEmojiPreparation(b *testing.B) {
	owner, err := openEmojiColorFont(filepath.Join(os.Getenv("WINDIR"), "Fonts", "seguiemj.ttf"))
	if err != nil {
		b.Skipf("system Segoe UI Emoji unavailable: %v", err)
	}
	p := newReferenceFontProvider(owner)
	b.Cleanup(func() { p.Close(); owner.Close() })
	f := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600}
	for _, text := range []string{"ordinary AV ffi", "😀", "אבג👩‍👩‍👧‍👦!"} {
		b.Run(text+"/public-bidi", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				referenceTextGlyphs(text, f)
			}
		})
		b.Run(text+"/provider", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				p.Shape(text, f)
			}
		})
	}
}
