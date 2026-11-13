//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Independent DOM/CDP facts captured in the same pinned Chrome/system-font
// environment as font_replacement_reference_test.go. These are text/default
// and VS15 selections, not Native-generated widths. In particular U+2600 uses
// Noto Sans SC before Symbol, and an existing primary glyph wins for 1/# and
// Arial/Times/Consolas U+263A. Chrome synthesizes Symbol 600 without widening.
func referenceTextEmojiVectors() []referenceReplacementVector {
	type primary struct {
		regular, bold string
		smile         float64
		one, hash     [2]float64
	}
	primaries := map[string]primary{
		"Segoe UI":        {"SegoeUI", "SegoeUI-Semibold", 48, [2]float64{25.875, 19.3125}, [2]float64{28.359375, 28.390625}},
		"Arial":           {"ArialMT", "Arial-BoldMT", 49.015625, [2]float64{26.703125, 26.703125}, [2]float64{26.703125, 26.703125}},
		"Times New Roman": {"TimesNewRomanPSMT", "TimesNewRomanPS-BoldMT", 49.015625, [2]float64{24, 24}, [2]float64{24, 24}},
		"Calibri":         {"Calibri", "Calibri-Bold", 48, [2]float64{24.328125, 24.328125}, [2]float64{23.90625, 23.90625}},
		"Consolas":        {"Consolas", "Consolas-Bold", 26.390625, [2]float64{26.390625, 26.390625}, [2]float64{26.390625, 26.390625}},
	}
	var out []referenceReplacementVector
	for _, variant := range referenceReplacementVariants {
		if variant.Italic {
			continue
		}
		name := variant.Family
		if name == "" {
			name = "Segoe UI"
		}
		p := primaries[name]
		index, postscript, family := 0, p.regular, name
		if variant.Weight == 600 {
			index, postscript = 1, p.bold
			if name == "Segoe UI" {
				family = "Segoe UI Semibold"
			}
		}
		main := referenceReplacementFont{family, postscript, 1, false}
		symbol := referenceReplacementFont{"Segoe UI Symbol", "SegoeUISymbol", 1, false}
		smile := main
		if name == "Segoe UI" || name == "Calibri" {
			smile = symbol
		}
		sun := referenceReplacementFont{"Noto Sans SC", "Noto-Sans-SC", 1, false}
		if variant.Weight == 600 {
			sun.PostScriptName = "Noto-Sans-SC-Bold"
		}
		arrowWidth, arrow := float64(48), sun
		if name == "Segoe UI" && variant.Weight == 400 {
			arrowWidth, arrow = 35.15625, main
		} else if name == "Calibri" {
			arrowWidth, arrow = [2]float64{42.328125, 40.296875}[index], main
		}
		for _, sample := range []struct {
			name, text string
			width      float64
			font       referenceReplacementFont
		}{
			{"heart", "❤", 46.640625, symbol}, {"heart15", "❤\ufe0e", 46.640625, symbol},
			{"smile", "☺", p.smile, smile}, {"smile15", "☺\ufe0e", p.smile, smile},
			{"sun", "☀", 48, sun}, {"sun15", "☀\ufe0e", 48, sun},
			{"plane", "✈", 47.90625, symbol}, {"plane15", "✈\ufe0e", 47.90625, symbol},
			{"one", "1", p.one[index], main}, {"one15", "1\ufe0e", p.one[index], main},
			{"hash", "#", p.hash[index], main}, {"hash15", "#\ufe0e", p.hash[index], main},
			{"up-arrow", "↗", arrowWidth, arrow}, {"up-arrow15", "↗\ufe0e", arrowWidth, arrow},
			{"down-arrow", "↙", arrowWidth, arrow}, {"down-arrow15", "↙\ufe0e", arrowWidth, arrow},
			{"wave", "〰", 48, sun}, {"wave15", "〰\ufe0e", 48, sun},
			{"part", "〽", 48, sun}, {"part15", "〽\ufe0e", 48, sun},
		} {
			out = append(out, referenceReplacementVector{Family: variant.Family, Weight: variant.Weight,
				Text: sample.text, Sample: sample.name, Fonts: []referenceReplacementFont{sample.font},
				Measure: referenceReplacementMeasure{Width: sample.width, Height: 57.59375,
					Font: variant.Font, Family: variant.ComputedFamily, Weight: fmt.Sprint(variant.Weight), Style: "normal", Direction: "ltr"}})
		}
	}
	return out
}

func TestReferenceTextEmojiRecordedChromeAllShapePaths(t *testing.T) {
	requireReferenceReplacementFonts(t)
	r := newReferenceMonoFallback()
	defer r.Close()
	if r.families[referenceCJKSC] != "Noto Sans SC" {
		t.Skip("recorded text-emoji matrix requires installed Noto Sans SC")
	}
	p, _ := testReferenceEmojiProvider(t)
	p.monoFallback = r // The production host lends this same shared resolver.
	for _, vector := range referenceTextEmojiVectors() {
		t.Run(fmt.Sprintf("%s/%d/%s", vector.Family, vector.Weight, vector.Sample), func(t *testing.T) {
			f := ui.Font{Family: vector.Family, Weight: vector.Weight, Size: 48, Lang: "en"}
			glyphs, width, exact := r.Shape(vector.Text, f)
			check := func(path string, gotWidth float32, gotExact bool, err error) {
				if !gotExact || err != nil || math.Abs(float64(gotWidth)-vector.Measure.Width) > 1.0/64 {
					t.Errorf("%s: width=%g exact=%v err=%v; independent DOM=%g", path, gotWidth, gotExact, err, vector.Measure.Width)
				}
			}
			check("Shape", width, exact, nil)
			for _, rtl := range []bool{false, true} {
				_, resolvedWidth, resolvedExact := r.ShapeResolved(vector.Text, f, rtl)
				check(fmt.Sprintf("ShapeResolved/%v", rtl), resolvedWidth, resolvedExact, nil)
			}
			got := p.Shape(vector.Text, f)
			check("provider", got.Width, got.Exact, got.Err)
			if len(got.Colors) != 0 {
				t.Fatal("text presentation was sent to color rasterization")
			}
			line := p.ShapeRange(vector.Text, 0, len([]rune(vector.Text)), f)
			check("ShapeRange", line.Width, line.Exact, line.Err)
			// The actual CDP-selected public family also owns the retained stroke.
			// Advances above come from independent DOM facts, so this raster check
			// cannot authorize the old, overly wide synthetic-bold positioning.
			selected := f
			selected.Family = vector.Fonts[0].FamilyName
			if selected.Family == "Segoe UI Semibold" {
				selected.Family = "Segoe UI"
			}
			want, _, _ := referenceTextGlyphs(strings.ReplaceAll(vector.Text, "\ufe0e", ""), selected)
			if !slices.Equal(referenceGlyphProbeAtBaseline(glyphs), referenceGlyphProbeAtBaseline(want)) {
				t.Fatal("retained glyph raster differs from the actual DOM-selected public face/weight")
			}
			if len(glyphs) != 1 || glyphs[0].Cluster != 0 || glyphs[0].Runes != len([]rune(vector.Text)) {
				t.Fatalf("single browser glyph lost its original selector cluster: %+v", glyphs)
			}
		})
	}
}

func TestReferenceTextEmojiUnknownFontsAndClose(t *testing.T) {
	r := newReferenceMonoFallback()
	for _, font := range []ui.Font{{Family: "Unknown Font", Size: 48}, {Family: "Segoe UI", Size: 48, Italic: true}, {Family: "Segoe UI Symbol", Size: 48}} {
		original, width, _ := referenceTextGlyphs("❤\ufe0e", font)
		got, gotWidth, exact := r.Shape("❤\ufe0e", font)
		if exact || width != gotWidth || !referencePublicGlyphsEqual(original, got) || !slices.Equal(referenceGlyphProbeAtBaseline(original), referenceGlyphProbeAtBaseline(got)) {
			t.Errorf("unproven font %+v changed or was reported exact", font)
		}
		original, width, originalExact := referenceTextGlyphs("ordinary 123#", font)
		got, gotWidth, exact = r.Shape("ordinary 123#", font)
		if exact != originalExact || width != gotWidth || !referencePublicGlyphsEqual(original, got) {
			t.Errorf("ordinary ASCII digits for %+v changed", font)
		}
	}
	r.registered = append(r.registered, "Segoe UI")
	if _, _, exact := r.Shape("❤\ufe0e", ui.Font{Family: "Segoe UI", Size: 48}); exact {
		t.Fatal("registered primary shadow was guessed")
	}
	r.Close()
	r.Close()
	if r.coverage != nil || r.emojiRunes != nil || r.primaryKnown != [9]uint8{} {
		t.Fatal("Close retained coverage or sparse emoji metadata")
	}
	for _, masks := range r.emojiPrimary {
		if masks != nil {
			t.Fatal("Close retained a weight's sparse emoji masks")
		}
	}
}

func TestReferenceTextEmojiAgainstChromium(t *testing.T) {
	want := referenceTextEmojiVectors()
	got := captureReferenceTextEmojiRows(t, want, true)
	for i, vector := range want {
		row := got[i]
		if row.Measure != vector.Measure || !reflect.DeepEqual(referenceReplacementSortedFonts(row.Fonts), referenceReplacementSortedFonts(vector.Fonts)) {
			t.Errorf("%q/%d/%s original=%+v/%+v; recorded=%+v/%+v", vector.Family, vector.Weight, vector.Sample, row.Measure, row.Fonts, vector.Measure, vector.Fonts)
		}
	}
}

// Exercise every system ICU Emoji BMP scalar, plain and VS15, at both recorded
// weights and all supported normal primary families. The browser produces the
// expected widths live; no Native output is ever saved as the expected oracle.
func TestReferenceTextEmojiBMPAgainstChromium(t *testing.T) {
	if os.Getenv("AMLL_CHROME") == "" {
		t.Skip("set AMLL_CHROME for the complete original BMP emoji matrix")
	}
	r := newReferenceMonoFallback()
	defer r.Close()
	if r.families[referenceCJKSC] != "Noto Sans SC" || len(r.emojiRunes) == 0 {
		t.Skip("recorded BMP platform matrix requires Noto Sans SC and ICU Emoji properties")
	}
	var input []referenceReplacementVector
	for _, variant := range referenceReplacementVariants {
		if variant.Italic {
			continue
		}
		for _, key := range r.emojiRunes {
			for _, selector := range []string{"", "\ufe0e"} {
				input = append(input, referenceReplacementVector{Family: variant.Family, Weight: variant.Weight,
					Text: string(rune(key)) + selector, Sample: fmt.Sprintf("%04x/%t", key, selector != "")})
			}
		}
	}
	rows := captureReferenceTextEmojiRows(t, input, false)
	p, _ := testReferenceEmojiProvider(t)
	p.monoFallback = r
	for _, row := range rows {
		got := p.Shape(row.Text, ui.Font{Family: row.Family, Weight: row.Weight, Size: 48, Lang: "en"})
		if got.Err != nil || !got.Exact || math.Abs(float64(got.Width)-row.Measure.Width) > 1.0/64 {
			t.Errorf("%q/%d/%s: native=%g exact=%v err=%v; original=%g fonts=%+v", row.Family, row.Weight, row.Sample, got.Width, got.Exact, got.Err, row.Measure.Width, row.Fonts)
		}
	}
}

func captureReferenceTextEmojiRows(t *testing.T, want []referenceReplacementVector, grid bool) []referenceReplacementVector {
	t.Helper()
	chrome := os.Getenv("AMLL_CHROME")
	if chrome == "" {
		t.Skip("set AMLL_CHROME to rerun the isolated original text-emoji DOM/CDP oracle")
	}
	requireReferenceReplacementFonts(t)
	temp := t.TempDir()
	script := filepath.Join(temp, "text-emoji.py")
	probe := referenceReplacementProbe
	if !grid {
		// The complete BMP matrix is tall. Retain DOM/CDP facts without an
		// unnecessary multi-megapixel screenshot or pixel allocation.
		probe = strings.Replace(probe, "\n metrics=c.call('Page.getLayoutMetrics')", "\n if False: metrics=c.call('Page.getLayoutMetrics')", 1)
	}
	if err := os.WriteFile(script, []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python", script, chrome, temp)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("text emoji original oracle: %v\n%s", err, stderr.String())
	}
	var got struct {
		Browser struct{ Product string }
		DPR     float64
		Rows    []referenceReplacementVector
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("text emoji CDP JSON: %v", err)
	}
	if got.Browser.Product != referenceReplacementChrome {
		t.Skipf("captured %s, recorded facts are scoped to %s", got.Browser.Product, referenceReplacementChrome)
	}
	if got.DPR != 1 || len(got.Rows) != len(want) {
		t.Fatalf("DPR/count differs: %g / %d", got.DPR, len(got.Rows))
	}
	for i, vector := range want {
		row := got.Rows[i]
		if row.Family != vector.Family || row.Weight != vector.Weight || row.Text != vector.Text || row.Sample != vector.Sample {
			t.Fatalf("original row %d input mapping changed", i)
		}
	}
	t.Logf("%s: %d independent text-emoji default/VS15 DOM/platform-font vectors", got.Browser.Product, len(want))
	return got.Rows
}
