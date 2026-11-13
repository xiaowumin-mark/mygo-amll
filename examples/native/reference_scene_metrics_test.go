// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestReferenceInterludeContentObserverKeepsPaddingOnlyMeasurement(t *testing.T) {
	var r referenceScene
	r.measureInterludeMetrics(30.3, 740, 30.3, 2)
	initial, content := r.interludeMetrics, r.interludeObservedContent
	// Actual original CSS/ResizeObserver vector at 200% system DPI. Border-box
	// drawing has 1/128 DIP precision; the source observer caches 26.6 DIPs.
	if initial.Width != 98.7578125 || initial.Height != 39.3828125 || initial.MeasuredWidth != 98.75 || initial.MeasuredHeight != 39.375 {
		t.Fatalf("original CSS/RO vector differs: %+v", initial)
	}
	r.dirty = false
	r.measureInterludeMetrics(30.3, 740, 20, 2)
	changed := r.interludeMetrics
	if changed.Width == initial.Width || changed.MeasuredWidth != initial.MeasuredWidth || changed.MeasuredHeight != initial.MeasuredHeight || r.interludeObservedContent != content || r.dirty {
		t.Fatalf("padding-only CSS update invented a content-box observer event: %+v", changed)
	}
	r.measureInterludeMetrics(31, 740, 20, 2)
	fresh := measureReferenceInterludeDots(31, 740, 20, 2)
	if r.interludeObservedContent == content || r.interludeMetrics.MeasuredWidth != fresh.MeasuredWidth || r.interludeMetrics.MeasuredHeight != fresh.MeasuredHeight || !r.dirty {
		t.Fatal("real content-size change failed to refresh the source's border-box measurement")
	}
}

func TestReferenceInterludeLayoutConsumesMeasuredBoxAtFractionalDPI(t *testing.T) {
	for _, scale := range []float32{1.25, 2} {
		t.Run(fmt.Sprintf("scale-%g", scale), func(t *testing.T) {
			doc := geometryDocument(t, []lyric.Line{{Text: "first", End: time.Second}, {Text: "next", Start: 12 * time.Second, End: 14 * time.Second, Duet: true}})
			s := newScene(doc, time.Unix(0, 0))
			defer s.Close()
			s.font.Size = 30.3
			if err := s.enableAMLL(false); err != nil {
				t.Fatal(err)
			}
			s.forceStatic = true
			s.setReferenceHostSize(960, 740)
			s.clock.seek(s.origin, 2*time.Second)
			s.prepareReferenceFrame(s.origin, ui.Rect{W: 800, H: 500}, scale, false)
			r := s.reference
			m := r.interludeMetrics
			// The controller retains raw layout targets before the original
			// renderStyles serializes its transform to two decimal places.
			if math.Abs(r.interludePose.RawX+m.MeasuredWidth-800) > 1e-8 || math.Abs(r.interludePose.RawY+m.MeasuredHeight/2-175) > 1e-8 {
				t.Fatalf("JS layout used CSS drawing size instead of measured box: pose=%+v metrics=%+v", r.interludePose, m)
			}
			if m.Width == m.MeasuredWidth {
				t.Fatal("fixture did not retain the real CSS transform-origin width")
			}
		})
	}
}
