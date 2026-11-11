package timeline_test

import (
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func TestReversedSourceLinesAreNeverActiveAcrossSeeks(t *testing.T) {
	doc, err := lyric.NewDocumentWithOptions([]lyric.Line{
		{ID: "normal", Start: 0, End: 2},
		{ID: "reversed", Start: 3, End: 1},
		{ID: "after", Start: 5, End: 8},
	}, nil, lyric.DocumentOptions{PreserveSourceTiming: true})
	if err != nil {
		t.Fatal(err)
	}
	index, err := timeline.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	playing, highlighted := make([]int, 0, doc.Len()), make([]int, 0, doc.Len())
	for _, at := range []time.Duration{0, 1, 2, 3, 4, 5, 8, 4, 3, 2, 1, 0} {
		state := index.VisualAt(at, playing, highlighted)
		for _, active := range state.Playing {
			if active == 1 {
				t.Fatalf("reversed source line active at %v", at)
			}
		}
		for _, active := range state.Highlighted {
			if active == 1 {
				t.Fatalf("reversed source line highlighted at %v", at)
			}
		}
		playing, highlighted = state.Playing, state.Highlighted
	}
	if allocations := testing.AllocsPerRun(100, func() { index.VisualAt(3, playing, highlighted) }); allocations != 0 {
		t.Fatalf("reversed-line seek allocated %v times", allocations)
	}
}
