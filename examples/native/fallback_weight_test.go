// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"runtime"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestReferenceWindowsFallbackKeepsRequestedCJKWeight(t *testing.T) {
	if runtime.GOOS != "windows" || !slices.ContainsFunc(ui.FontFamilies(), func(f ui.FontFamily) bool {
		return f.Name == "Noto Sans SC" && !f.Registered
	}) {
		t.Skip("recorded Chrome 157 font vector requires Windows Noto Sans SC")
	}
	// Actual CSS.getPlatformFontsForNode for Segoe UI/600/15px and this text:
	// SegoeUI-Semibold spaces plus Noto-Sans-SC-Bold Han, DOM width 53.25.
	// The pinned native fallback used the regular Han outline at weight 600.
	resolver := newReferenceMonoFallback()
	font := ui.Font{Family: "Segoe UI", Size: 15, Weight: 600, LineHeight: 1.5}
	if name := resolver.family(font, '我'); name != "Noto Sans SC" {
		t.Fatalf("actual regular fallback resolved to %q; recorded host font changed", name)
	}
	glyphs, width, exact := resolver.Shape("我 我 我", font)
	if !exact || width < 53.23 || width > 53.26 {
		t.Fatalf("width=%g exact=%t; recorded DOM width=53.25", width, exact)
	}
	if len(glyphs) != 5 || glyphs[0].Cluster != 0 || glyphs[4].Cluster != 4 {
		t.Fatalf("lost source spaces/clusters: %+v", glyphs)
	}
	bold := font
	bold.Family = "Noto Sans SC"
	bold.Weight = 700 // CDP reports the Bold face for CSS weight 600.
	expected, _, _ := referenceTextGlyphs("我", bold)
	a := referenceGlyphProbeAtBaseline(glyphs[:1])
	b := referenceGlyphProbeAtBaseline(expected)
	if !slices.Equal(a, b) {
		t.Fatal("corrected Han raster differs from the actual DOM-selected bold family")
	}
	// Direction overrides and surrounding scripts must preserve their source
	// clusters and visual direction while the fallback resource changes.
	for _, text := range []string{"A我B", "אב我!", "\u202e我我\u202c", "我\u0301"} {
		got, _, ok := resolver.Shape(text, font)
		if !ok || len(got) == 0 {
			t.Fatalf("text=%q lost exact bidi resolution", text)
		}
		for _, g := range got {
			if g.Cluster < 0 || g.Cluster+g.Runes > len([]rune(text)) {
				t.Fatalf("text=%q invalid cluster %+v", text, g)
			}
		}
	}
}

func referenceGlyphProbeAtBaseline(glyphs []ui.Glyph) []byte {
	im := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
			p.Glyphs(glyphs, 8, 30, ui.RGB(255, 255, 255))
		})
	}, 64, 64, 1)
	return im.Pix
}

func TestReferenceCJKFallbackRecordedChromeWeights(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("recorded Windows Chrome 157 font vectors")
	}
	r := newReferenceMonoFallback()
	if r.family(ui.Font{Family: "Segoe UI"}, '我') != "Noto Sans SC" {
		t.Skip("recorded host requires the installed Noto Sans SC family")
	}
	for _, vector := range []struct {
		weight int
		width  float32
	}{{400, 53.21875}, {600, 53.25}, {700, 53.28125}} {
		font := ui.Font{Family: "Segoe UI", Size: 15, Weight: vector.weight, LineHeight: 1.5}
		_, width, exact := r.Shape("我 我 我", font)
		if !exact || math.Abs(float64(width-vector.width)) > 1.0/64 {
			t.Errorf("weight%d Native=%g exact=%v recorded DOM=%g", vector.weight, width, exact, vector.width)
		}
	}
}

func TestReferenceCJKFallbackLocaleAndCandidateOrder(t *testing.T) {
	r := &referenceMonoFallback{reliable: true, latin: []string{"Segoe UI"}, systemHan: referenceCJKSC}
	r.families = [5]string{"", "SC", "TC", "JP", "KR"}
	r.coverage = make(map[string]*referenceBMPCoverage)
	for family, ranges := range map[string][][2]rune{
		"SC": {{0x3000, 0x312f}, {0x4e00, 0x9fff}, {0xff01, 0xff5e}},
		"TC": {{0x3000, 0x312f}, {0x4e00, 0x9fff}},
		"JP": {{0x3040, 0x30ff}, {0x4e00, 0x9fff}},
		"KR": {{0x3040, 0x30ff}, {0x4e00, 0x9fff}, {0xac00, 0xd7af}},
	} {
		bits := new(referenceBMPCoverage)
		for _, span := range ranges {
			for c := span[0]; c <= span[1]; c++ {
				bits[c>>3] |= 1 << uint(c&7)
			}
		}
		r.coverage[family] = bits
	}

	for _, vector := range []struct {
		language string
		sample   rune
		want     string
	}{{"en", '我', "SC"}, {"zh-Hans", '我', "SC"}, {"zh-Hant", '我', "TC"}, {"zh-TW", '我', "TC"}, {"zh-HK", '我', "TC"}, {"ja-JP", '我', "JP"}, {"ko", '我', "KR"}, {"en", 'あ', "SC"}, {"zh-CN", '한', "KR"}, {"en", 'ㄅ', "SC"}, {"ja-JP", 'ㄅ', "TC"}, {"ko", 'あ', "KR"}, {"en", 'Ａ', "SC"}, {"en", '\U00020000', ""}, {"zh-Hant", '\U0002ebf0', ""}, {"en", '\U00030000', ""}} {
		if got := r.family(ui.Font{Family: "Segoe UI", Lang: vector.language}, vector.sample); got != vector.want {
			t.Errorf("language=%q rune=%U family=%q want %q", vector.language, vector.sample, got, vector.want)
		}
	}
	// The source table gives first-installed priority, not a closeness score.
	if got := referenceCJKFamilyCandidates[referenceCJKSC]; !slices.Equal(got, []string{"Noto Sans SC", "Noto Sans CJK SC", "Microsoft YaHei", "simsun"}) {
		t.Fatalf("Chromium Simplified Han order=%v", got)
	}
	delete(r.coverage, "SC")
	if got := r.family(ui.Font{Family: "Segoe UI"}, 'あ'); got != "" {
		t.Fatal("unknown preferred-face coverage was treated as a proven miss")
	}
}

func TestReferenceCJKFallbackPreservesUnprovenExplicitFamilies(t *testing.T) {
	r := newReferenceMonoFallback()
	for _, family := range []string{"Unverified Font", "Noto Sans SC", "Microsoft YaHei", "Segoe UI, Noto Sans SC"} {
		font := ui.Font{Family: family, Size: 15, Weight: 600}
		original, width, _ := referenceTextGlyphs("我", font)
		got, gotWidth, exact := r.Shape("我", font)
		if exact || gotWidth != width || !referencePublicGlyphsEqual(got, original) || !slices.Equal(referenceGlyphProbeAtBaseline(got), referenceGlyphProbeAtBaseline(original)) {
			t.Errorf("unproven explicit family %q was replaced or reported exact", family)
		}
	}
	font := ui.Font{Family: "Segoe UI", Size: 15, Weight: 600}
	r.registered = append(r.registered, font.Family)
	if _, _, exact := r.Shape("我", font); exact {
		t.Fatal("host registration shadowing a known Latin family was guessed")
	}
	for _, text := range []string{"〰\ufe0f", "〽\ufe0f", "ASCII"} {
		original, width, originalExact := referenceTextGlyphs(text, font)
		got, gotWidth, exact := r.Shape(text, font)
		if exact != originalExact || gotWidth != width || !referencePublicGlyphsEqual(got, original) {
			t.Errorf("presentation-sensitive non-CJK baseline %q changed", text)
		}
	}
	original, width, _ := referenceTextGlyphs("〰\ufe0e", font)
	got, gotWidth, exact := r.Shape("〰\ufe0e", font)
	if exact || gotWidth != width || !referencePublicGlyphsEqual(got, original) {
		t.Fatal("text presentation for a registered primary was guessed")
	}
}

func TestReferenceCJKFallbackRecordedChromeLocaleAndContext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("recorded Windows Chrome 157 CDP platform-font vectors")
	}
	r := newReferenceMonoFallback()
	if r.systemHan != referenceCJKSC || r.families[referenceCJKSC] != "Noto Sans SC" || r.families[referenceCJKTC] != "Microsoft JhengHei" || r.families[referenceCJKJP] != "Yu Gothic" || r.families[referenceCJKKR] != "Malgun Gothic" {
		t.Skip("recorded platform-font matrix requires the measured SC/TC/JP/KR families and locale")
	}
	for _, vector := range []struct {
		language, text, family string
		width                  float32
	}{
		{"zh-CN", "我", "Noto Sans SC", 15}, {"zh-TW", "我", "Microsoft JhengHei", 15}, {"ja-JP", "我", "Yu Gothic", 15}, {"ko-KR", "我", "Malgun Gothic", 15},
		{"en", "あ", "Noto Sans SC", 15}, {"zh-TW", "あ", "Microsoft JhengHei", 15}, {"ja-JP", "あ", "Yu Gothic", 15}, {"ko-KR", "あ", "Malgun Gothic", 15},
		{"en", "ㄅ", "Noto Sans SC", 15}, {"zh-TW", "ㄅ", "Microsoft JhengHei", 15}, {"ja-JP", "ㄅ", "Microsoft JhengHei", 15}, {"ko-KR", "ㄅ", "Microsoft JhengHei", 15},
		{"en", "A我B", "", 34.140625}, {"en", "אב我!", "", 38.453125}, {"en", "我\u0301", "Noto Sans SC", 15},
		{"en", "日本語かな", "Noto Sans SC", 75}, {"en", "かな日本", "Noto Sans SC", 60}, {"en", "한글", "Malgun Gothic", 30},
		{"en", "你好，世界。", "Noto Sans SC", 90}, {"en", "\u202e我我\u202c", "Noto Sans SC", 30},
		{"en", "我한", "", 30}, {"en", "한我", "", 30}, {"en", "あ한", "", 30},
	} {
		t.Run(vector.language+"/"+vector.text, func(t *testing.T) {
			font := ui.Font{Family: "Segoe UI", Lang: vector.language, Size: 15, Weight: 600, LineHeight: 1.5}
			got, width, exact := r.Shape(vector.text, font)
			if !exact || math.Abs(float64(width-vector.width)) > 1.0/64 {
				t.Fatalf("Native width=%g exact=%v recorded Chrome=%g", width, exact, vector.width)
			}
			if vector.family != "" {
				selected := font
				selected.Family, selected.Weight = vector.family, 700 // CDP's selected face is Bold.
				want, _, _ := referenceTextGlyphs(vector.text, selected)
				if !slices.Equal(referenceGlyphProbeAtBaseline(got), referenceGlyphProbeAtBaseline(want)) {
					t.Fatalf("public raster differs from CDP-selected %s Bold", vector.family)
				}
			}
			for _, glyph := range got {
				if glyph.Cluster < 0 || glyph.Cluster+glyph.Runes > len([]rune(vector.text)) {
					t.Fatalf("replacement retained a synthetic direction sentinel: %+v", glyph)
				}
			}
		})
	}
}

func TestReferenceCJKFallbackResolvedUsesSamePolicyAndOwnedGlyphs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("single visual-run public Windows shaper")
	}
	r := newReferenceMonoFallback()
	font := ui.Font{Family: "Segoe UI", Lang: "en", Size: 15, Weight: 600}
	if r.family(font, '我') != "Noto Sans SC" {
		t.Skip("recorded resolver environment requires Noto Sans SC")
	}
	for _, rtl := range []bool{false, true} {
		for _, text := range []string{"A我B", "日本語かな", "我\u0301", "我日"} {
			got, width, exact := r.ShapeResolved(text, font, rtl)
			forced, prefix := "\u202d"+text+"\u202c", 1
			if rtl {
				forced = "\u202e" + text + "\u202c"
			}
			full, wantWidth, wantExact := r.Shape(forced, font)
			var want []ui.Glyph
			left, found := float32(0), false
			for _, g := range full {
				if g.Cluster < prefix || g.Cluster >= prefix+len([]rune(text)) {
					continue
				}
				g.Cluster -= prefix
				if !found || g.X < left {
					left, found = g.X, true
				}
				want = append(want, g)
			}
			for i := range want {
				want[i].X -= left
			}
			if !exact || !wantExact || width != wantWidth || !referencePublicGlyphsEqual(got, want) || !slices.Equal(referenceGlyphProbeAtBaseline(got), referenceGlyphProbeAtBaseline(want)) {
				t.Errorf("resolved rtl=%v text=%q differs from the existing explicit-direction path: width %g/%g exact %v/%v", rtl, text, width, wantWidth, exact, wantExact)
			}
			if len(got) > 0 {
				original := got[0].X
				got[0].X = 999
				next, _, _ := r.ShapeResolved(text, font, rtl)
				if len(next) == 0 || next[0].X != original {
					t.Fatal("caller-owned resolved glyph mutation escaped into a subsequent shape")
				}
			}
		}
	}
	if _, _, exact := r.ShapeResolved("\u202e我", font, true); exact {
		t.Fatal("unremoved bidi control was accepted as an already-resolved body")
	}
}
