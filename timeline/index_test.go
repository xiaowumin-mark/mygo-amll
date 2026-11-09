package timeline_test

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func newIndex(t testing.TB, lines []lyric.Line) *timeline.Index {
	t.Helper()
	doc, err := lyric.NewDocument(lines)
	if err != nil {
		t.Fatal(err)
	}
	index, err := timeline.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func TestActiveAt(t *testing.T) {
	index := newIndex(t, []lyric.Line{
		{Start: 0, End: 10},
		{Start: 2, End: 4},
		{Start: 2, End: 6},
		{Start: 12, End: 14},
	})
	tests := []struct {
		position time.Duration
		want     []int
	}{
		{-1, nil}, {0, []int{0}}, {2, []int{0, 1, 2}}, {4, []int{0, 2}},
		{6, []int{0}}, {10, nil}, {12, []int{3}}, {14, nil},
		{3, []int{0, 1, 2}}, // backward seek must work without resetting the index
	}
	buffer := make([]int, 0, 4)
	for _, test := range tests {
		buffer = index.ActiveAt(test.position, buffer)
		if !slices.Equal(buffer, test.want) {
			t.Fatalf("at %v: got %v, want %v", test.position, buffer, test.want)
		}
	}
}

func TestEmptyAndNilDocument(t *testing.T) {
	if _, err := timeline.New(nil); err == nil {
		t.Fatal("nil document should be rejected")
	}
	index := newIndex(t, nil)
	if result := index.ActiveAt(0, []int{99}); len(result) != 0 {
		t.Fatalf("empty document should reset the output, got %v", result)
	}
}

func TestActiveAtMatchesLinearScan(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 13))
	lines := make([]lyric.Line, 1000)
	var start time.Duration
	for i := range lines {
		start += time.Duration(random.IntN(4))
		lines[i] = lyric.Line{Start: start, End: start + time.Duration(1+random.IntN(100))}
	}
	index := newIndex(t, lines)
	buffer := make([]int, 0, len(lines))
	for range 2000 {
		position := time.Duration(random.IntN(int(start)+200) - 50)
		var want []int
		for i, line := range lines {
			if line.Start <= position && position < line.End {
				want = append(want, i)
			}
		}
		buffer = index.ActiveAt(position, buffer)
		if !slices.Equal(buffer, want) {
			t.Fatalf("at %v: got %v, want %v", position, buffer, want)
		}
	}
}

func TestActiveAtDoesNotAllocateWithReusableBuffer(t *testing.T) {
	index := newIndex(t, []lyric.Line{{Start: 0, End: 5}, {Start: 0, End: 3}})
	buffer := make([]int, 0, 2)
	if allocs := testing.AllocsPerRun(1000, func() {
		buffer = index.ActiveAt(1, buffer)
	}); allocs != 0 {
		t.Fatalf("query allocated %v times", allocs)
	}
}

func TestConcurrentQueries(t *testing.T) {
	index := newIndex(t, []lyric.Line{{Start: 0, End: 5}, {Start: 2, End: 8}})
	done := make(chan bool, 8)
	for range 8 {
		go func() {
			buffer := make([]int, 0, 2)
			for range 100 {
				buffer = index.ActiveAt(3, buffer)
				if !slices.Equal(buffer, []int{0, 1}) {
					done <- false
					return
				}
			}
			done <- true
		}()
	}
	for range 8 {
		if !<-done {
			t.Fatal("concurrent query returned incorrect indices")
		}
	}
}
