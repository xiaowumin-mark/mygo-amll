package timeline

import (
	"sort"
	"time"
)

// InterludeThreshold is the minimum gap between lyric intervals that receives
// an interlude focus. It also applies to the gap before the first line.
const InterludeThreshold = 7 * time.Second

// Interlude is a half-open [Start, End) interval in absolute track time.
// Anchor is the last preceding line in document order, or -1 for a prelude.
type Interlude struct {
	Start  time.Duration
	End    time.Duration
	Anchor int
}

// VisualState separates audible activity, retained highlighting and automatic
// scroll focus. Playing and Highlighted contain line indices in document order.
// Their buffers belong to the caller, which may change them without changing
// the index. Reusing those buffers invalidates the corresponding earlier slices.
type VisualState struct {
	Position          time.Duration
	Playing           []int
	Highlighted       []int
	Focus             int
	LatestHighlighted int
	Interlude         Interlude
	InInterlude       bool
	Ended             bool
}

// VisualAt reconstructs visual state from an absolute playback position, so a
// seek in either direction needs no reset or replay of earlier frames. Both
// destination slices are reset and reused; they must have nonoverlapping backing
// storage. Sufficient capacity makes the query allocation-free. The index is
// immutable and concurrent calls are safe when they use separate buffers.
//
// A completed line stays highlighted until the next distinct line start; a line
// still playing at that start stays highlighted too. Interludes and the end of
// all lyric intervals clear highlighting. Ended describes lyric completion,
// rather than an audio duration supplied by the host; an empty document is never
// ended. Gaps use the maximum end of all preceding lines, and the end of lyrics
// uses the maximum end of all lines, including long overlapping lines.
//
// Focus is the first member of the most recent highlight cohort, and remains
// there during an interlude or after completion. Before the first line it is 0;
// for an empty document it is -1. During InInterlude, the UI should align to the
// Interlude instead of Focus. LatestHighlighted is -1 when highlighting is empty.
// A source-preserving document can contain point lines; if its latest start has
// no active cohort, Focus is the first line at that start and highlighting is empty.
// Negative positions have no activity, interlude or highlighting.
//
// Queries cost O(log n + c), with c the candidate interval window at the current
// position and the most recent line start. Long overlaps can make c = n.
func (index *Index) VisualAt(position time.Duration, playingDst, highlightedDst []int) VisualState {
	state := VisualState{
		Position: position, Playing: playingDst[:0], Highlighted: highlightedDst[:0],
		Focus: -1, LatestHighlighted: -1,
	}
	n := index.document.Len()
	if n == 0 {
		return state
	}
	state.Focus = 0
	if position < 0 {
		return state
	}
	upper := sort.Search(n, func(i int) bool { return index.document.Line(i).Start() > position })
	state.Playing = index.ActiveAt(position, state.Playing)
	if upper > 0 {
		latestStart := index.document.Line(upper - 1).Start()
		state.Highlighted = index.ActiveAt(latestStart, state.Highlighted)
		if len(state.Highlighted) > 0 {
			state.Focus = state.Highlighted[0]
		} else {
			state.Focus = sort.Search(upper, func(i int) bool { return index.document.Line(i).Start() >= latestStart })
		}
	}
	state.Ended = position >= index.prefixMaxEnd[n-1]
	if upper < n {
		var gapStart time.Duration
		if upper > 0 {
			gapStart = index.prefixMaxEnd[upper-1]
		}
		gapEnd := index.document.Line(upper).Start()
		if gapEnd-gapStart >= InterludeThreshold && position >= gapStart {
			state.Interlude = Interlude{Start: gapStart, End: gapEnd, Anchor: upper - 1}
			state.InInterlude = true
		}
	}
	if state.Ended || state.InInterlude {
		state.Highlighted = state.Highlighted[:0]
	}
	if len(state.Highlighted) > 0 {
		state.LatestHighlighted = state.Highlighted[len(state.Highlighted)-1]
	}
	return state
}
