// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// BenchmarkReferenceWarmPrepare measures a bounded warm playback window, not
// GPU rendering or checkpoint throughput. Each 1024-frame batch ends before a
// checkpoint starts; resetting history IDs happens outside the measured timer.
// Checkpoint snapshot/replay/publication have their own separate benchmarks.
func BenchmarkReferenceWarmPrepare(b *testing.B) {
	fonts, releaseFonts, err := openNativeReferenceFonts()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(releaseFonts)
	for _, count := range []int{200, 2000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			document, err := demoDocument(count)
			if err != nil {
				b.Fatal(err)
			}
			now := time.Unix(0, 0)
			s := newScene(document, now)
			s.fontProvider = fonts
			b.Cleanup(func() { _ = s.Close() })
			if err := s.enableAMLL(false); err != nil {
				b.Fatal(err)
			}
			s.setReferenceHostSize(960, 740)
			position := document.Line(count / 2).Start()
			s.clock.seek(now, position)
			s.clock.setPlaying(now, true)
			s.prepareReferenceFrame(now, ui.Rect{W: 960, H: 740}, 1, true)
			for range 180 {
				now = now.Add(time.Second / 60)
				s.clock.seek(now, position)
				s.update(now, false)
			}
			r := s.reference
			if r.fallback != "" {
				b.Fatal(r.fallback)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if r.lastStep >= 1024 {
					b.StopTimer()
					for j := range r.groups {
						r.groups[j].step = 0
					}
					r.firstStep, r.lastStep = 1, 0
					b.StartTimer()
				}
				now = now.Add(time.Second / 60)
				s.clock.seek(now, position)
				s.update(now, false)
			}
			b.StopTimer()
			if r.fallback != "" {
				b.Fatal(r.fallback)
			}
			b.ReportMetric(float64(r.effects.bytes), "effect-B")
		})
	}
}
