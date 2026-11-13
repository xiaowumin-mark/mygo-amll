//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Actual Chrome 157 DOM Range vectors from separate startup processes using
// --force-device-scale-factor=1.25/2 (verified window.devicePixelRatio), rather
// than CDP emulation. These straddle physical 1/64 layout-unit fit boundaries.
// In particular the exact-grid Latin/CJK cases exercise Blink's AddEpsilon;
// the fractional emoji cases also require ceil-snapping the shaped width.
var referenceAnnotationScaleVectors = []struct {
	text         string
	width, scale float32
	rows         []string
	rightX       []float32
	height       float32
}{
	{"A A", 24.25, 1.25, []string{"A", "A"}, []float32{14.1750011, 14.1750011}, 45},
	{"A A", 24.2625, 1.25, []string{"A A"}, []float32{0}, 22.5},
	{"A A", 24.275, 1.25, []string{"A A"}, []float32{0}, 22.5},
	{"A A", 24.2875, 1.25, []string{"A A"}, []float32{0.0124998093}, 22.5},
	{"B B", 22.225, 1.25, []string{"B", "B"}, []float32{13.1625004, 13.1625004}, 45},
	{"B B", 22.2375, 1.25, []string{"B B"}, []float32{0}, 22.5},
	{"B B", 22.25, 1.25, []string{"B B"}, []float32{0}, 22.5},
	{"B B", 22.2625, 1.25, []string{"B B"}, []float32{0.0124998093}, 22.5},
	{"f f", 14.45, 1.25, []string{"f", "f"}, []float32{9.27499962, 9.27499962}, 45},
	{"f f", 14.4625, 1.25, []string{"f f"}, []float32{0}, 22.5},
	{"f f", 14.475, 1.25, []string{"f f"}, []float32{0}, 22.5},
	{"f f", 14.4875, 1.25, []string{"f f"}, []float32{0.0124998093}, 22.5},
	{"AV AV", 41.8125, 1.25, []string{"AV", "AV"}, []float32{22.9500008, 22.9500008}, 45},
	{"AV AV", 41.825, 1.25, []string{"AV AV"}, []float32{0}, 22.5},
	{"AV AV", 41.8375, 1.25, []string{"AV AV"}, []float32{0}, 22.5},
	{"AV AV", 41.85, 1.25, []string{"AV AV"}, []float32{0.0124998093}, 22.5},
	{"To To", 35.6625, 1.25, []string{"To", "To"}, []float32{19.875, 19.875}, 45},
	{"To To", 35.675, 1.25, []string{"To To"}, []float32{0}, 22.5},
	{"To To", 35.6875, 1.25, []string{"To To"}, []float32{0}, 22.5},
	{"To To", 35.7, 1.25, []string{"To To"}, []float32{0.0124998093}, 22.5},
	{"אבג אבג", 55.75, 1.25, []string{"אבג", "אבג"}, []float32{29.9125023, 29.9125023}, 45},
	{"אבג אבג", 55.7625, 1.25, []string{"אבג אבג"}, []float32{0}, 22.5},
	{"אבג אבג", 55.775, 1.25, []string{"אבג אבג"}, []float32{0}, 22.5},
	{"אבג אבג", 55.7875, 1.25, []string{"אבג אבג"}, []float32{0.0124998093}, 22.5},
	{"中文", 29.9875, 1.25, []string{"中文"}, []float32{0}, 22.5},
	{"中文", 30, 1.25, []string{"中文"}, []float32{0}, 22.5},
	{"中文", 30.0125, 1.25, []string{"中文"}, []float32{0.0124998093}, 22.5},
	{"中文", 30.025, 1.25, []string{"中文"}, []float32{0.0250005722}, 22.5},
	{"😀😀", 41.175, 1.25, []string{"😀", "😀"}, []float32{20.5750008, 20.5750008}, 45},
	{"😀😀", 41.1875, 1.25, []string{"😀😀"}, []float32{0}, 22.5},
	{"😀😀", 41.2, 1.25, []string{"😀😀"}, []float32{0}, 22.5},
	{"😀😀", 41.2125, 1.25, []string{"😀😀"}, []float32{0.0124998093}, 22.5},
	{"😀😀😀", 61.7625, 1.25, []string{"😀😀", "😀"}, []float32{20.5625, 41.1625023}, 45},
	{"😀😀😀", 61.775, 1.25, []string{"😀😀😀"}, []float32{0}, 22.5},
	{"😀😀😀", 61.7875, 1.25, []string{"😀😀😀"}, []float32{0}, 22.5},
	{"😀😀😀", 61.8, 1.25, []string{"😀😀😀"}, []float32{0.0124998093}, 22.5},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.575, 1.25, []string{"👩‍👩‍👧‍👦", "👩‍👩‍👧‍👦"}, []float32{18.7749996, 18.7749996}, 45},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.5875, 1.25, []string{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦"}, []float32{0}, 22.5},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.6, 1.25, []string{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦"}, []float32{0}, 22.5},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.6125, 1.25, []string{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦"}, []float32{0.0124998093}, 22.5},
	{"A A", 24.25, 2, []string{"A", "A"}, []float32{14.171875, 14.171875}, 45},
	{"A A", 24.2578125, 2, []string{"A A"}, []float32{0}, 22.5},
	{"A A", 24.265625, 2, []string{"A A"}, []float32{0}, 22.5},
	{"A A", 24.2734375, 2, []string{"A A"}, []float32{0.0078125}, 22.5},
	{"B B", 22.234375, 2, []string{"B", "B"}, []float32{13.171875, 13.171875}, 45},
	{"B B", 22.2421875, 2, []string{"B B"}, []float32{0}, 22.5},
	{"B B", 22.25, 2, []string{"B B"}, []float32{0}, 22.5},
	{"B B", 22.2578125, 2, []string{"B B"}, []float32{0.0078125}, 22.5},
	{"f f", 14.453125, 2, []string{"f", "f"}, []float32{9.28125, 9.28125}, 45},
	{"f f", 14.4609375, 2, []string{"f f"}, []float32{0}, 22.5},
	{"f f", 14.46875, 2, []string{"f f"}, []float32{0}, 22.5},
	{"f f", 14.4765625, 2, []string{"f f"}, []float32{0.0078125}, 22.5},
	{"AV AV", 41.8203125, 2, []string{"AV", "AV"}, []float32{22.9609375, 22.9609375}, 45},
	{"AV AV", 41.828125, 2, []string{"AV AV"}, []float32{0}, 22.5},
	{"AV AV", 41.8359375, 2, []string{"AV AV"}, []float32{0}, 22.5},
	{"AV AV", 41.84375, 2, []string{"AV AV"}, []float32{0.0078125}, 22.5},
	{"To To", 35.6640625, 2, []string{"To", "To"}, []float32{19.8828125, 19.8828125}, 45},
	{"To To", 35.671875, 2, []string{"To To"}, []float32{0}, 22.5},
	{"To To", 35.6796875, 2, []string{"To To"}, []float32{0}, 22.5},
	{"To To", 35.6875, 2, []string{"To To"}, []float32{0.0078125}, 22.5},
	{"אבג אבג", 55.765625, 2, []string{"אבג", "אבג"}, []float32{29.9375, 29.9375}, 45},
	{"אבג אבג", 55.7734375, 2, []string{"אבג אבג"}, []float32{0}, 22.5},
	{"אבג אבג", 55.78125, 2, []string{"אבג אבג"}, []float32{0}, 22.5},
	{"אבג אבג", 55.7890625, 2, []string{"אבג אבג"}, []float32{0.0078125}, 22.5},
	{"中文", 29.9921875, 2, []string{"中文"}, []float32{0}, 22.5},
	{"中文", 30, 2, []string{"中文"}, []float32{0}, 22.5},
	{"中文", 30.0078125, 2, []string{"中文"}, []float32{0.0078125}, 22.5},
	{"中文", 30.015625, 2, []string{"中文"}, []float32{0.015625}, 22.5},
	{"😀😀", 41.1796875, 2, []string{"😀", "😀"}, []float32{20.578125, 20.578125}, 45},
	{"😀😀", 41.1875, 2, []string{"😀😀"}, []float32{0}, 22.5},
	{"😀😀", 41.1953125, 2, []string{"😀😀"}, []float32{0}, 22.5},
	{"😀😀", 41.203125, 2, []string{"😀😀"}, []float32{0.0078125}, 22.5},
	{"😀😀😀", 61.7734375, 2, []string{"😀😀", "😀"}, []float32{20.578125, 41.171875}, 45},
	{"😀😀😀", 61.78125, 2, []string{"😀😀😀"}, []float32{0}, 22.5},
	{"😀😀😀", 61.7890625, 2, []string{"😀😀😀"}, []float32{0}, 22.5},
	{"😀😀😀", 61.796875, 2, []string{"😀😀😀"}, []float32{0.0078125}, 22.5},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.578125, 2, []string{"👩‍👩‍👧‍👦", "👩‍👩‍👧‍👦"}, []float32{18.78125, 18.78125}, 45},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.5859375, 2, []string{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦"}, []float32{0}, 22.5},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.59375, 2, []string{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦"}, []float32{0}, 22.5},
	{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦", 37.6015625, 2, []string{"👩‍👩‍👧‍👦👩‍👩‍👧‍👦"}, []float32{0.0078125}, 22.5},
	{"f f f f f f f", 60.9296875, 2, []string{"f f f f f f f"}, []float32{0}, 22.5},
	{"f f f f f f f", 60.9375, 2, []string{"f f f f f f f"}, []float32{0}, 22.5},
	{"f f f f f f f", 60.9453125, 2, []string{"f f f f f f f"}, []float32{0.0078125}, 22.5},
	{"f f f f f f f", 60.953125, 2, []string{"f f f f f f f"}, []float32{0.015625}, 22.5},
	{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV", 432.414062, 2, []string{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV"}, []float32{0}, 22.5},
	{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV", 432.421875, 2, []string{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV"}, []float32{0}, 22.5},
	{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV", 432.429688, 2, []string{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV"}, []float32{0.0078125}, 22.5},
	{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV", 432.4375, 2, []string{"AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV AV"}, []float32{0.015625}, 22.5},
	{"To To To To To To To To To To To To To To To", 294.367188, 2, []string{"To To To To To To To To To To To To To To To"}, []float32{0}, 22.5},
	{"To To To To To To To To To To To To To To To", 294.375, 2, []string{"To To To To To To To To To To To To To To To"}, []float32{0}, 22.5},
	{"To To To To To To To To To To To To To To To", 294.382812, 2, []string{"To To To To To To To To To To To To To To To"}, []float32{0.0078125}, 22.5},
	{"To To To To To To To To To To To To To To To", 294.390625, 2, []string{"To To To To To To To To To To To To To To To"}, []float32{0.015625}, 22.5},
}

func TestReferenceAnnotationPhysicalScaleWrapping(t *testing.T) {
	p, _ := testReferenceEmojiProvider(t)
	mono := newReferenceMonoFallback()
	p.monoFallback = mono
	t.Cleanup(func() { p.monoFallback = nil; mono.Close() })
	font := ui.Font{Family: "Segoe UI", Size: 15, Weight: 600, LineHeight: 1.5}
	for _, test := range referenceAnnotationScaleVectors {
		for _, duet := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%g/%g/%v", test.text, test.width, test.scale, duet), func(t *testing.T) {
				rows, height, exact, err := referenceAnnotationRows(test.text, font, test.width, duet, p, test.scale)
				if err != nil || !exact || height != test.height || len(rows) != len(test.rows) {
					t.Fatalf("rows=%d height=%g exact=%v err=%v; source rows=%d height=%g", len(rows), height, exact, err, len(test.rows), test.height)
				}
				for i, row := range rows {
					x := float32(0)
					if duet {
						x = test.rightX[i]
					}
					if annotationAuditVisible(row.text) != annotationAuditVisible(test.rows[i]) || math.Abs(float64(row.x-x)) > .06 {
						t.Errorf("row %d text=%q x=%g; source=%q/%g", i, row.text, row.x, test.rows[i], x)
					}
				}
			})
		}
	}
}
