package timeline_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func BenchmarkActiveAt(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		for _, overlap := range []bool{false, true} {
			b.Run(fmt.Sprintf("lines=%d/long-overlap=%t", count, overlap), func(b *testing.B) {
				lines := make([]lyric.Line, count)
				for i := range lines {
					lines[i] = lyric.Line{Start: time.Duration(i) * time.Second, End: time.Duration(i+1) * time.Second}
				}
				if overlap {
					lines[0].End = time.Duration(count) * time.Second
				}
				index := newIndex(b, lines)
				buffer := make([]int, 0, count)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					position := time.Duration(i%count)*time.Second + 500*time.Millisecond
					buffer = index.ActiveAt(position, buffer)
				}
			})
		}
	}
}
