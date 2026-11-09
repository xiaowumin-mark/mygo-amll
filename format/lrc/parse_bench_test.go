package lrc

import (
	"context"
	"fmt"
	"strings"
	"testing"

	common "github.com/xiaowumin-mark/mygo-amll/format"
)

func BenchmarkParse(b *testing.B) {
	for _, count := range []int{200, 2000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			var source strings.Builder
			for i := 0; i < count; i++ {
				fmt.Fprintf(&source, "[%02d:%02d]<%02d:%02d>合成 lyric<%02d:%02d>\n", i/60, i%60, i/60, i%60, (i+1)/60, (i+1)%60)
			}
			data := []byte(source.String())
			ctx := context.Background()
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := Parse(ctx, data, common.Options{})
				if err != nil || result.Document.Len() != count {
					b.Fatalf("parse failed: %v", err)
				}
			}
		})
	}
}
