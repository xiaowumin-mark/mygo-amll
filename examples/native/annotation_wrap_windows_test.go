//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

type referenceAnnotationDOMCase struct {
	Text          string
	Width, Height float32
	Scale         float32
	Duet          bool
	Ranges        []struct {
		Text string
		Row  int
		W, X float32
	}
}

func TestAnnotationNativeDOMAudit(t *testing.T) {
	path := os.Getenv("AMLL_ANNOTATION_DOM_ORACLE")
	if path == "" {
		t.Skip("external actual-DOM audit, not a replacement for recorded regression vectors")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []referenceAnnotationDOMCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	auditReferenceAnnotationDOMCases(t, cases)
}

func auditReferenceAnnotationDOMCases(t *testing.T, cases []referenceAnnotationDOMCase) {
	t.Helper()
	f := ui.Font{Family: "Segoe UI", Size: 15, Weight: 600, LineHeight: 1.5}
	different := 0
	newDifferent := 0
	p, _ := testReferenceEmojiProvider(t)
	mono := newReferenceMonoFallback()
	p.monoFallback = mono
	t.Cleanup(func() { p.monoFallback = nil; mono.Close() })
	for _, test := range cases {
		layout := ui.ShapeText(test.Text, f, test.Width)
		rows := layout.Lines()
		if float32(len(rows))*22.5 != test.Height {
			different++
			if different <= 4 {
				t.Logf("%q width=%v duet=%v Native rows=%d DOM rows=%v", test.Text, test.Width, test.Duet, len(rows), test.Height/22.5)
			}
		}
		wantRows := make([]string, int(test.Height/22.5))
		wantX := make([]float32, len(wantRows))
		hasX := make([]bool, len(wantRows))
		for _, r := range test.Ranges {
			if r.Row < 0 || r.Row >= len(wantRows) || r.W == 0 {
				continue
			}
			ch := r.Text
			if strings.ContainsAny(ch, " \t\r\n\f") {
				ch = " "
			}
			wantRows[r.Row] += ch
			if ch != " " && (!hasX[r.Row] || r.X < wantX[r.Row]) {
				wantX[r.Row], hasX[r.Row] = r.X, true
			}
		}
		for i := range wantRows {
			wantRows[i] = strings.Trim(wantRows[i], " ")
			if !hasX[i] && test.Duet {
				wantX[i] = test.Width
			}
		}
		scale := test.Scale
		if scale == 0 {
			scale = 1
		}
		got, h, exact, err := referenceAnnotationRows(test.Text, f, test.Width, test.Duet, p, scale)
		bad := err != nil || !exact || h != test.Height || len(got) != len(wantRows)
		for i := range min(len(got), len(wantRows)) {
			bad = bad || annotationAuditVisible(got[i].text) != annotationAuditVisible(wantRows[i])
			if !strings.ContainsRune(test.Text, '\u00ad') {
				bad = bad || math.Abs(float64(got[i].x-wantX[i])) > .06
			}
			// SHY-generated RTL hyphens are absent from DOM Range rects; their
			// advance is instead visible in the actual letters' displaced X.
			// Compare those original-rune glyph clusters directly, including
			// normal bidi and distinct LS/PS paragraph behavior.
			if strings.HasPrefix(test.Text, got[i].context) {
				shaped := p.ShapeRange(got[i].context, got[i].startRune, got[i].endRune, f)
				if shaped.Err != nil || !shaped.Exact {
					bad = true
					continue
				}
				type cluster struct{ from, to int }
				type extent struct {
					left, right float32
					set         bool
				}
				boxes := map[cluster]extent{}
				for _, g := range shaped.Glyphs {
					if g.Advance <= 0 {
						continue
					}
					from, to := got[i].startRune+g.Cluster, got[i].startRune+g.Cluster+g.Runes
					if from < 0 || to > len(test.Ranges) || from >= to {
						bad = true
						continue
					}
					if test.Ranges[from].Text == "\u00ad" {
						continue
					}
					key := cluster{from, to}
					b := boxes[key]
					if !b.set {
						b = extent{got[i].x + g.X, got[i].x + g.X + g.Advance, true}
					} else {
						b.left = min(b.left, got[i].x+g.X)
						b.right = max(b.right, got[i].x+g.X+g.Advance)
					}
					boxes[key] = b
				}
				for key, box := range boxes {
					var left, right float32
					found := false
					for at := key.from; at < key.to; at++ {
						r := test.Ranges[at]
						if r.W <= 0 || r.Text == "\u00ad" || referenceBidiControl([]rune(r.Text)[0]) {
							continue
						}
						if r.Row != i {
							bad = true
						}
						if !found {
							left, right, found = r.X, r.X+r.W, true
						} else {
							left = min(left, r.X)
							right = max(right, r.X+r.W)
						}
					}
					if found && (math.Abs(float64(box.left-left)) > .12 || math.Abs(float64(box.right-right)) > .12) {
						bad = true
					}
				}
			}
		}
		if bad {
			newDifferent++
			if newDifferent <= 24 {
				var texts []string
				for _, r := range got {
					texts = append(texts, r.text)
				}
				t.Logf("provider %q width=%v duet=%v got=%q height=%v want=%q height=%v err=%v", test.Text, test.Width, test.Duet, texts, h, wantRows, test.Height, err)
			}
		}
	}
	t.Logf("actual DOM row-count mismatches: %d/%d", different, len(cases))
	if newDifferent != 0 {
		t.Fatalf("new provider row/position mismatches: %d/%d", newDifferent, len(cases))
	}
}

func annotationAuditVisible(text string) string {
	return strings.Map(func(r rune) rune {
		if referenceBidiControl(r) || r == '\u00ad' || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, text)
}
