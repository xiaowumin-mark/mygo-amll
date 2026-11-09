package timeline_test

import (
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func TestVisualAtRetainedHighlightsAndSeek(t *testing.T) {
	index := newIndex(t, []lyric.Line{
		{Start: time.Second, End: 5 * time.Second},
		{Start: 2 * time.Second, End: 3 * time.Second},
		{Start: 2 * time.Second, End: 4 * time.Second},
		{Start: 6 * time.Second, End: 8 * time.Second},
	})
	tests := []struct {
		position    time.Duration
		playing     []int
		highlighted []int
		focus       int
		latest      int
		ended       bool
	}{
		{-1, nil, nil, 0, -1, false},
		{0, nil, nil, 0, -1, false},
		{time.Second, []int{0}, []int{0}, 0, 0, false},
		{2 * time.Second, []int{0, 1, 2}, []int{0, 1, 2}, 0, 2, false},
		{3 * time.Second, []int{0, 2}, []int{0, 1, 2}, 0, 2, false},
		{4 * time.Second, []int{0}, []int{0, 1, 2}, 0, 2, false},
		{5 * time.Second, nil, []int{0, 1, 2}, 0, 2, false},
		{6 * time.Second, []int{3}, []int{3}, 3, 3, false},
		{8 * time.Second, nil, nil, 3, -1, true},
		{2 * time.Second, []int{0, 1, 2}, []int{0, 1, 2}, 0, 2, false},
		{7 * time.Second, []int{3}, []int{3}, 3, 3, false},
	}
	playing, highlighted := make([]int, 0, 4), make([]int, 0, 4)
	for _, test := range tests {
		state := index.VisualAt(test.position, playing, highlighted)
		playing, highlighted = state.Playing, state.Highlighted
		if !slices.Equal(state.Playing, test.playing) || !slices.Equal(state.Highlighted, test.highlighted) ||
			state.Focus != test.focus || state.LatestHighlighted != test.latest || state.Ended != test.ended || state.InInterlude || state.Position != test.position {
			t.Fatalf("at %v: got %+v, expected playing=%v highlighted=%v focus=%d latest=%d ended=%v", test.position, state, test.playing, test.highlighted, test.focus, test.latest, test.ended)
		}
	}
}

func TestVisualInterludeThresholdAndHalfOpenBounds(t *testing.T) {
	for _, gap := range []time.Duration{6999 * time.Millisecond, 7 * time.Second, 7001 * time.Millisecond} {
		t.Run(gap.String(), func(t *testing.T) {
			end := time.Second
			next := end + gap
			index := newIndex(t, []lyric.Line{{Start: 0, End: end}, {Start: next, End: next + time.Second}})
			for _, position := range []time.Duration{end, next - 1} {
				state := index.VisualAt(position, nil, nil)
				if state.InInterlude != (gap >= timeline.InterludeThreshold) {
					t.Fatalf("at %v: interlude=%v", position, state.InInterlude)
				}
				if state.InInterlude {
					if state.Interlude != (timeline.Interlude{Start: end, End: next, Anchor: 0}) || len(state.Highlighted) != 0 || state.LatestHighlighted != -1 {
						t.Fatalf("unexpected interlude state: %+v", state)
					}
				} else if !slices.Equal(state.Highlighted, []int{0}) {
					t.Fatalf("short gap lost highlighting: %+v", state)
				}
			}
			if state := index.VisualAt(end-1, nil, nil); state.InInterlude || !slices.Equal(state.Playing, []int{0}) {
				t.Fatalf("interlude started early: %+v", state)
			}
			if state := index.VisualAt(next, nil, nil); state.InInterlude || !slices.Equal(state.Playing, []int{1}) {
				t.Fatalf("interlude failed to end: %+v", state)
			}
		})
	}
}

func TestVisualInterludeUsesAllPrecedingEnds(t *testing.T) {
	index := newIndex(t, []lyric.Line{
		{Start: 0, End: 10 * time.Second},
		{Start: time.Second, End: 2 * time.Second},
		{Start: 12 * time.Second, End: 13 * time.Second},
		{Start: 20 * time.Second, End: 21 * time.Second},
	})
	for _, position := range []time.Duration{3 * time.Second, 10 * time.Second, 11 * time.Second} {
		if state := index.VisualAt(position, nil, nil); state.InInterlude {
			t.Fatalf("false interlude during overlap or short gap: %+v", state)
		}
	}
	for _, position := range []time.Duration{13 * time.Second, 19 * time.Second} {
		state := index.VisualAt(position, nil, nil)
		if !state.InInterlude || state.Interlude != (timeline.Interlude{Start: 13 * time.Second, End: 20 * time.Second, Anchor: 2}) {
			t.Fatalf("missing interlude: %+v", state)
		}
	}
}

func TestVisualPreludeEmptyAndGlobalEnd(t *testing.T) {
	index := newIndex(t, []lyric.Line{{Start: 7 * time.Second, End: 20 * time.Second}, {Start: 10 * time.Second, End: 11 * time.Second}})
	for _, position := range []time.Duration{0, 7*time.Second - 1} {
		state := index.VisualAt(position, nil, nil)
		if !state.InInterlude || state.Interlude != (timeline.Interlude{Start: 0, End: 7 * time.Second, Anchor: -1}) || state.Focus != 0 || len(state.Highlighted) != 0 {
			t.Fatalf("unexpected prelude: %+v", state)
		}
	}
	state := index.VisualAt(11*time.Second, nil, nil)
	if state.Ended || !slices.Equal(state.Playing, []int{0}) || !slices.Equal(state.Highlighted, []int{0, 1}) {
		t.Fatalf("last-starting line ended the song early: %+v", state)
	}
	state = index.VisualAt(20*time.Second, nil, nil)
	if !state.Ended || len(state.Highlighted) != 0 || len(state.Playing) != 0 || state.Focus != 0 {
		t.Fatalf("global end was not applied: %+v", state)
	}
	for _, position := range []time.Duration{-1, 0, time.Hour} {
		state := newIndex(t, nil).VisualAt(position, []int{99}, []int{98})
		if state.Ended || state.InInterlude || state.Focus != -1 || state.LatestHighlighted != -1 || len(state.Playing) != 0 || len(state.Highlighted) != 0 {
			t.Fatalf("invalid empty state: %+v", state)
		}
	}
}

func TestVisualAtMatchesLinearOracle(t *testing.T) {
	random := rand.New(rand.NewPCG(17, 31))
	lines := make([]lyric.Line, 1000)
	var start, maxEnd time.Duration
	for i := range lines {
		start += time.Duration(random.IntN(4)) * time.Second
		lines[i] = lyric.Line{Start: start, End: start + time.Duration(1+random.IntN(20))*time.Second}
		maxEnd = max(maxEnd, lines[i].End)
	}
	index := newIndex(t, lines)
	playing, highlighted := make([]int, 0, len(lines)), make([]int, 0, len(lines))
	for range 1000 {
		position := time.Duration(random.IntN(int(start/time.Second)+40)-10) * time.Second
		var latestStart time.Duration = -1
		var wantPlaying, wantHighlighted []int
		for i, line := range lines {
			if line.Start <= position {
				latestStart = line.Start
				if position < line.End {
					wantPlaying = append(wantPlaying, i)
				}
			}
		}
		if position < maxEnd {
			for i, line := range lines {
				if line.Start <= latestStart && latestStart < line.End {
					wantHighlighted = append(wantHighlighted, i)
				}
			}
		}
		state := index.VisualAt(position, playing, highlighted)
		playing, highlighted = state.Playing, state.Highlighted
		if !slices.Equal(state.Playing, wantPlaying) || !slices.Equal(state.Highlighted, wantHighlighted) {
			t.Fatalf("at %v: got %+v, want playing=%v highlighted=%v", position, state, wantPlaying, wantHighlighted)
		}
	}
}

func TestVisualAtConcurrentQueriesAndOwnedBuffers(t *testing.T) {
	index := newIndex(t, []lyric.Line{{Start: 0, End: 5}, {Start: 2, End: 8}})
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			playing, highlighted := make([]int, 0, 2), make([]int, 0, 2)
			for range 100 {
				state := index.VisualAt(3, playing, highlighted)
				playing, highlighted = state.Playing, state.Highlighted
				if !slices.Equal(playing, []int{0, 1}) || !slices.Equal(highlighted, []int{0, 1}) {
					t.Error("concurrent query returned incorrect indices")
					return
				}
				playing[0], highlighted[0] = 99, 98
			}
		})
	}
	group.Wait()
}

func TestVisualAtDoesNotAllocateWithReusableBuffers(t *testing.T) {
	index := newIndex(t, []lyric.Line{{Start: 0, End: 5}, {Start: 2, End: 8}})
	playing, highlighted := make([]int, 0, 2), make([]int, 0, 2)
	if allocs := testing.AllocsPerRun(1000, func() {
		state := index.VisualAt(6, playing, highlighted)
		playing, highlighted = state.Playing, state.Highlighted
	}); allocs != 0 {
		t.Fatalf("query allocated %v times", allocs)
	}
}
