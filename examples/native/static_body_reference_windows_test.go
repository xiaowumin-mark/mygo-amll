//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func staticBodyReferenceGeometry(t *testing.T, text string, width float32, duet bool, provider *referenceFontProvider) *lineGeometry {
	t.Helper()
	if provider == nil {
		provider = newReferenceFontProvider(nil)
		t.Cleanup(provider.Close)
	}
	doc, err := lyric.NewDocument([]lyric.Line{{Text: text, End: 20 * time.Second, Duet: duet, Words: []lyric.Word{{Text: text, End: 20 * time.Second}}}})
	if err != nil {
		t.Fatal(err)
	}
	presentation, err := newPresentation(doc)
	if err != nil || presentation.dynamic {
		t.Fatalf("one-word document did not select the source nonDynamic contract: %v", err)
	}
	font := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600, LineHeight: 1.2, Lang: "en"}
	g, err := buildLineGeometryWithStyle(presentation.document.Line(0), font, width, geometryStyle{Dynamic: false, FontProvider: provider, UseHostLanguage: true})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestStaticBodyReferenceDuetOverflowUsesStartEdge(t *testing.T) {
	// Actual original DomLyricPlayer, one word in the complete document,
	// available width 60. Both Latin and RTL unbreakable rows start at x=0;
	// text-align:right does not move an overflowing normal-flow line negative.
	for _, text := range []string{"Supercalifragilisticexpialidocious", "אבגדהוזחטיכלמנסעפצקרשת"} {
		g := staticBodyReferenceGeometry(t, text, 60, true, nil)
		if len(g.blocks) != 1 {
			t.Fatalf("static overflow=%q source has one row; Native=%d", text, len(g.blocks))
		}
		if g.blocks[0].x != 0 || g.bodyHeight != 36 {
			t.Errorf("static overflow=%q x=%g height=%g; source=0/36", text, g.blocks[0].x, g.bodyHeight)
		}
	}
}

func TestStaticBodyReferenceNaturalSoftHyphenAfterBalancedBreaks(t *testing.T) {
	// Real source LineBalancer inserts BR at original rune indices [10,11].
	// The first hard row still wraps naturally at SHY, producing two physical
	// rows; the explicit whitespace-only row and final "now" make four total.
	for _, duet := range []bool{false, true} {
		g := staticBodyReferenceGeometry(t, "co\u00adoperate now", 65, duet, nil)
		if g.bodyHeight != 144 {
			t.Errorf("duet=%v static balanced rows lost their natural SHY wrap: height=%g source=144", duet, g.bodyHeight)
		}
	}
}

func TestStaticBodyReferenceHardBreakDoesNotPaintSoftHyphen(t *testing.T) {
	// Source BR [1] is a hard DOM break, not a discretionary SHY break. The
	// first source rune's Range has zero advance; no visible hyphen is added.
	for _, duet := range []bool{false, true} {
		g := staticBodyReferenceGeometry(t, "\u00adabc", 10, duet, nil)
		if len(g.blocks) != 2 || g.blocks[0].width != 0 || g.bodyHeight != 72 {
			t.Errorf("duet=%v explicit BR must keep an invisible SHY row and two physical lines", duet)
		}
	}
}

func TestStaticBodyReferenceHebrewFamilyAcrossBalancedBreak(t *testing.T) {
	_, provider := explicitEmojiFont(t)
	const family = "👩‍👩‍👧‍👦"
	for _, duet := range []bool{false, true} {
		g := staticBodyReferenceGeometry(t, "אבג "+family+" abc", 65, duet, provider)
		// Original source BRs are [4,12], while the family on the second
		// physical row still receives Hebrew script inherited across the BR.
		if len(g.blocks) != 3 || g.blocks[1].text != family {
			t.Fatal("changed the original source balanced-break partition")
		}
		row := g.blocks[1]
		wantX := float32(0)
		if duet {
			wantX = 36.84375
		}
		if math.Abs(float64(row.width-28.15625)) > 1.0/64 || math.Abs(float64(row.x-wantX)) > 1.0/64 {
			t.Errorf("duet=%v family row advance=%g x=%g; source=28.15625/%g", duet, row.width, row.x, wantX)
		}
	}
}

func staticBodyReferenceDynamicGeometry(t *testing.T, first string, width float32, duet bool) *lineGeometry {
	t.Helper()
	provider := newReferenceFontProvider(nil)
	t.Cleanup(provider.Close)
	doc, err := lyric.NewDocument([]lyric.Line{{Text: first + " abc", End: 20 * time.Second, Duet: duet, Words: []lyric.Word{{Text: first, End: 10 * time.Second}, {Text: " abc", Start: 10 * time.Second, End: 20 * time.Second}}}})
	if err != nil {
		t.Fatal(err)
	}
	presentation, err := newPresentation(doc)
	if err != nil || !presentation.dynamic {
		t.Fatalf("two-word document did not select the source dynamic contract: %v", err)
	}
	font := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600, LineHeight: 1.2, Lang: "en"}
	g, err := buildLineGeometryWithStyle(presentation.document.Line(0), font, width, geometryStyle{Dynamic: true, FontProvider: provider, UseHostLanguage: true})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestStaticBodyReferenceDynamicWordSpanOverflow(t *testing.T) {
	// Separate actual-player captures use two timed words, enabling the real
	// dynamic path, masks and brightness copies. Its overflowing inline-block
	// word still uses start alignment in a right-aligned duet line. This is not
	// inferred from the nonDynamic text-node behavior tested above.
	for _, first := range []string{"Supercalifragilisticexpialidocious", "אבגדהוזחטיכלמנסעפצקרשת"} {
		for _, duet := range []bool{false, true} {
			g := staticBodyReferenceDynamicGeometry(t, first, 60, duet)
			if len(g.blocks) == 0 || g.blocks[0].text != first || g.blocks[0].x != 0 || g.bodyHeight != 72 {
				t.Errorf("duet=%v dynamic overflowing span=%q must keep source x=0 and bodyHeight=72", duet, first)
			}
		}
	}
}

func TestStaticBodyReferenceDynamicRightAlignmentControl(t *testing.T) {
	g := staticBodyReferenceDynamicGeometry(t, "hello", 100, true)
	wants := map[string]float32{"hello": 32.984375, "abc": 52.140625}
	found := 0
	for _, block := range g.blocks {
		if want, ok := wants[block.text]; ok {
			found++
			if math.Abs(float64(block.x-want)) > 1.0/32 {
				t.Errorf("fitting dynamic span %q x=%g original=%g", block.text, block.x, want)
			}
		}
	}
	if found != 2 || g.bodyHeight != 72 {
		t.Fatal("dynamic control changed the actual source two-row partition")
	}
}

// This diagnostic consumes an independently captured, actual DomLyricPlayer
// fixture. Every capture document contains one line and one word, so the real
// optimizer selects nonDynamic; its original LineBalancer supplies the BRs.
// No synthetic div, annotation-greedy wrapper, or Native output is the oracle.
// Set MYGO_AMLL_STATIC_BODY_DOM to the JSON and MYGO_AMLL_TEST_EMOJI_FONT to an
// authorized installed font. There is no implicit large color-font load.
func TestStaticBodyActualPlayerDiagnostic(t *testing.T) {
	filename := os.Getenv("MYGO_AMLL_STATIC_BODY_DOM")
	if filename == "" {
		t.Skip("set MYGO_AMLL_STATIC_BODY_DOM to the independent actual-player capture")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	type rect struct{ X, Y, W, H float32 }
	type runeRange struct {
		Rune  int
		Text  string
		Rects []rect
	}
	var cases []struct {
		Name, Text, BodyText  string
		Duet, NonDynamic      bool
		Available, MainHeight float32
		BR                    []int
		Ranges                []runeRange
		Mask                  struct {
			Words, Animations int
			MainMask          string
		}
		Brightness struct {
			Enabled, Ready, Dim bool
			MainOpacity         string
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	_, provider := explicitEmojiFont(t)
	font := ui.Font{Family: "Segoe UI", Size: 30, Weight: 600, LineHeight: 1.2, Lang: "en"}
	for _, c := range cases {
		t.Run(c.Name+map[bool]string{false: "/normal", true: "/duet"}[c.Duet], func(t *testing.T) {
			if !c.NonDynamic || c.Mask.Words != 0 || c.Mask.Animations != 0 || c.Mask.MainMask != "none" || c.Brightness.Enabled || c.Brightness.Ready || c.Brightness.Dim || c.Brightness.MainOpacity != "1" {
				t.Fatal("capture did not retain the real nonDynamic mask/brightness contract")
			}
			source, err := lyric.NewDocument([]lyric.Line{{Text: c.Text, End: 20 * time.Second, Duet: c.Duet, Words: []lyric.Word{{Text: c.Text, End: 20 * time.Second}}}})
			if err != nil {
				t.Fatal(err)
			}
			presentation, err := newPresentation(source)
			if err != nil {
				t.Fatal(err)
			}
			if presentation.dynamic || presentation.document.Line(0).Text() != c.BodyText {
				t.Fatal("the fixture's original optimizer and Native presentation normalization differ")
			}
			g, err := buildLineGeometryWithStyle(presentation.document.Line(0), font, c.Available, geometryStyle{Dynamic: false, FontProvider: provider, UseHostLanguage: true})
			if err != nil {
				t.Fatal(err)
			}
			runes := []rune(c.BodyText)
			bounds := append(append([]int{0}, c.BR...), len(runes))
			var hardTexts []string
			for i := 1; i < len(bounds); i++ {
				hardTexts = append(hardTexts, strings.Trim(string(runes[bounds[i-1]:bounds[i]]), " \t\n\r\f"))
			}
			// Source BRs are the balancer's hard partition, not its physical
			// line count: natural SHY wraps can occur inside a text node. Derive
			// physical text and origins from the original player's DOM Ranges.
			// Blink can expose an extra preceding hyphen rect on the next rune;
			// the last rect belongs to that rune's own physical line.
			lineHeight := float32(36)
			firstY := float32(math.Inf(1))
			for _, r := range c.Ranges {
				for _, box := range r.Rects {
					firstY = min(firstY, box.Y)
				}
			}
			wantTexts := make([]string, int(math.Round(float64(c.MainHeight/lineHeight))))
			lefts := make([]float32, len(wantTexts))
			familyWidths := make([]float32, len(wantTexts))
			for i := range lefts {
				lefts[i] = float32(math.Inf(1))
			}
			for _, r := range c.Ranges {
				if len(r.Rects) == 0 {
					continue
				}
				box := r.Rects[len(r.Rects)-1]
				i := int(math.Round(float64((box.Y - firstY) / lineHeight)))
				if i < 0 || i >= len(wantTexts) {
					t.Fatal("source Range does not belong to a physical source row")
				}
				wantTexts[i] += r.Text
				if box.W > 0 && strings.Trim(r.Text, " \t\n\r\f") != "" {
					lefts[i] = min(lefts[i], box.X)
				}
				if r.Text == "👩" {
					familyWidths[i] = box.W
				}
			}
			for i := range wantTexts {
				wantTexts[i] = strings.Trim(wantTexts[i], " \t\n\r\f")
			}
			var gotTexts []string
			for _, block := range g.blocks {
				gotTexts = append(gotTexts, block.text)
				t.Logf("native row text=%q x=%.6f y=%.6f advance=%.6f h=%.6f", block.text, block.x, block.y, block.width, block.height)
			}
			t.Logf("source width=%.6f BR(runes)=%v hardRows=%q physicalRows=%q bodyHeight=%.6f; nativeHeight=%.6f", c.Available, c.BR, hardTexts, wantTexts, c.MainHeight, g.bodyHeight)
			if strings.Join(gotTexts, "\x00") != strings.Join(wantTexts, "\x00") {
				t.Errorf("original physical rows differ: Native=%q original=%q", gotTexts, wantTexts)
			}
			if math.Abs(float64(g.bodyHeight-c.MainHeight)) > 1e-4 {
				t.Errorf("body height=%g original=%g; explicit balanced BRs may still contain browser natural SHY wraps", g.bodyHeight, c.MainHeight)
			}
			if len(g.blocks) != len(wantTexts) {
				return
			}
			for i, block := range g.blocks {
				if block.y != float32(i)*lineHeight {
					t.Errorf("row=%d top=%g original=%g", i, block.y, float32(i)*lineHeight)
				}
				left := lefts[i]
				if !math.IsInf(float64(left), 1) && math.Abs(float64(block.x-left)) > 1.0/32 {
					t.Errorf("row=%d origin=%g original Range left=%g", i, block.x, left)
				}
				if block.text == "👩‍👩‍👧‍👦" {
					if math.Abs(float64(block.width-familyWidths[i])) > 1.0/32 {
						t.Errorf("contextual emoji advance=%g original=%g", block.width, familyWidths[i])
					}
				}
			}
		})
	}
}
