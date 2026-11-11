package timeline_test

import (
	"slices"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func TestVisualAtSourcePointLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []lyric.Line
		at    time.Duration
		focus int
		want  []int
		ended bool
	}{
		{"only point before", []lyric.Line{{Start: 2, End: 2}}, 1, 0, nil, false},
		{"only point at", []lyric.Line{{Start: 2, End: 2}}, 2, 0, nil, true},
		{"only point after", []lyric.Line{{Start: 2, End: 2}}, 3, 0, nil, true},
		{"point cohort", []lyric.Line{{End: 1}, {Start: 2, End: 2}, {Start: 2, End: 2}, {Start: 3, End: 4}}, 2, 1, nil, false},
		{"point then normal", []lyric.Line{{Start: 2, End: 2}, {Start: 3, End: 4}}, 3, 1, []int{1}, false},
		{"point with normal", []lyric.Line{{Start: 2, End: 2}, {Start: 2, End: 4}}, 2, 1, []int{1}, false},
		{"point during overlap", []lyric.Line{{End: 4}, {Start: 2, End: 2}}, 2, 0, []int{0}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := lyric.NewDocumentWithOptions(test.lines, nil, lyric.DocumentOptions{PreserveSourceTiming: true})
			if err != nil {
				t.Fatal(err)
			}
			index, err := timeline.New(doc)
			if err != nil {
				t.Fatal(err)
			}
			// Seek beyond the last interval and back to the requested position.
			index.VisualAt(time.Hour, nil, nil)
			state := index.VisualAt(test.at, nil, nil)
			if !slices.Equal(state.Playing, test.want) || !slices.Equal(state.Highlighted, test.want) || state.Focus != test.focus || state.Ended != test.ended {
				t.Fatalf("at %v: got %+v, expected indices=%v focus=%d ended=%v", test.at, state, test.want, test.focus, test.ended)
			}
			if len(test.want) == 0 && state.LatestHighlighted != -1 {
				t.Fatalf("point line was highlighted: %+v", state)
			}
			playing, highlighted := make([]int, 0, len(test.lines)), make([]int, 0, len(test.lines))
			if allocations := testing.AllocsPerRun(100, func() {
				state := index.VisualAt(test.at, playing, highlighted)
				playing, highlighted = state.Playing, state.Highlighted
			}); allocations != 0 {
				t.Fatalf("point-line query allocated %v times", allocations)
			}
		})
	}
}
