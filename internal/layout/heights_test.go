package layout

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func mustHeights(t testing.TB, count int, estimate float64) *Heights {
	t.Helper()
	h, err := NewHeights(count, estimate)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHeightsHalfOpenAndUpdates(t *testing.T) {
	h := mustHeights(t, 5, 20)
	for index, height := range []float64{10, 30, 0.25, 100, 8} {
		if err := h.SetHeight(index, height); err != nil {
			t.Fatal(err)
		}
	}
	if h.Len() != 5 || h.Total() != 148.25 {
		t.Fatalf("len=%d total=%g", h.Len(), h.Total())
	}
	for index, top := range []float64{0, 10, 40, 40.25, 140.25, 148.25} {
		if got := h.Prefix(index); got != top {
			t.Errorf("Prefix(%d)=%g want %g", index, got, top)
		}
		if got := h.LineAt(top); got != index {
			t.Errorf("LineAt(%g)=%d want %d", top, got, index)
		}
		if index > 0 {
			if got := h.LineAt(math.Nextafter(top, math.Inf(-1))); got != index-1 {
				t.Errorf("just before %g: got %d want %d", top, got, index-1)
			}
		}
	}
	for index := range h.Len() {
		if got := h.LineAt(h.Prefix(index) + h.Height(index)/2); got != index {
			t.Errorf("middle of row %d: got %d", index, got)
		}
	}
	if got := h.LineAt(-100); got != 0 {
		t.Errorf("negative offset=%d", got)
	}
	if got := h.LineAt(10000); got != h.Len() {
		t.Errorf("past bottom=%d", got)
	}
}

func TestHeightsRandomMeasurementsMatchReference(t *testing.T) {
	const count = 257 // Exercise unused leaves in the last tree level.
	h := mustHeights(t, count, 64)
	heights := make([]float64, count)
	for i := range heights {
		heights[i] = 64
	}
	rng := rand.New(rand.NewSource(50423))
	for step := 0; step < 1000; step++ {
		index, height := rng.Intn(count), float64(1+rng.Intn(2048))/4
		if err := h.SetHeight(index, height); err != nil {
			t.Fatal(err)
		}
		heights[index] = height
		var prefix float64
		for i, wantHeight := range heights {
			if got := h.Height(i); got != wantHeight {
				t.Fatalf("step %d row %d height=%g want %g", step, i, got, wantHeight)
			}
			if got := h.Prefix(i); got != prefix {
				t.Fatalf("step %d Prefix(%d)=%g want %g", step, i, got, prefix)
			}
			if got := h.LineAt(prefix); got != i {
				t.Fatalf("step %d LineAt(%g)=%d want %d", step, prefix, got, i)
			}
			if got := h.LineAt(prefix + wantHeight/2); got != i {
				t.Fatalf("step %d middle of row %d maps to %d", step, i, got)
			}
			prefix += wantHeight
		}
		if got := h.Total(); got != prefix {
			t.Fatalf("step %d total=%g want %g", step, got, prefix)
		}
		if got := h.LineAt(prefix); got != count {
			t.Fatalf("step %d bottom maps to %d", step, got)
		}
	}
}

func TestHeightsRejectInvalidWithoutMutation(t *testing.T) {
	for _, count := range []int{-1, int(^uint(0) >> 1)} {
		if _, err := NewHeights(count, 1); err == nil {
			t.Errorf("accepted count %d", count)
		}
	}
	for _, height := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, count := range []int{0, 2} {
			if _, err := NewHeights(count, height); err == nil {
				t.Errorf("accepted count %d estimate %g", count, height)
			}
		}
	}
	if _, err := NewHeights(2, math.MaxFloat64); err == nil {
		t.Error("accepted overflowing total")
	}
	h := mustHeights(t, 3, 20)
	for _, index := range []int{-1, 3} {
		if err := h.SetHeight(index, 40); err == nil {
			t.Errorf("accepted index %d", index)
		}
	}
	for _, height := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := h.SetHeight(1, height); err == nil {
			t.Errorf("accepted height %g", height)
		}
	}
	if h.Total() != 60 || h.Height(1) != 20 || h.Prefix(2) != 40 {
		t.Fatal("invalid update changed tree")
	}
	large := mustHeights(t, 2, math.MaxFloat64/4)
	if err := large.SetHeight(0, math.MaxFloat64); err == nil {
		t.Fatal("accepted overflowing update")
	}
	if large.Height(0) != math.MaxFloat64/4 || large.Total() != math.MaxFloat64/2 {
		t.Fatal("overflowing update changed tree")
	}
}

func TestHeightsReplacingTallEstimatePreservesShortRows(t *testing.T) {
	h := mustHeights(t, 2, 10)
	if err := h.SetHeight(0, 1e300); err != nil {
		t.Fatal(err)
	}
	if err := h.SetHeight(0, 1); err != nil {
		t.Fatal(err)
	}
	if h.Total() != 11 || h.LineAt(1) != 1 || h.Prefix(1) != 1 {
		t.Fatalf("replacement lost short rows: total=%g prefix=%g", h.Total(), h.Prefix(1))
	}
}

func TestHeightsEmptyAndSpecialOffsets(t *testing.T) {
	for _, h := range []*Heights{{}, mustHeights(t, 0, 10)} {
		if h.Len() != 0 || h.Total() != 0 || h.Prefix(0) != 0 {
			t.Fatal("nonempty zero index")
		}
		for _, offset := range []float64{-1, 0, 1, math.NaN(), math.Inf(-1), math.Inf(1)} {
			if got := h.LineAt(offset); got != 0 {
				t.Errorf("empty offset %g returned %d", offset, got)
			}
		}
		if err := h.SetHeight(0, 10); err == nil {
			t.Error("updated an empty index")
		}
	}
	h := mustHeights(t, 3, 10)
	for _, offset := range []float64{math.NaN(), math.Inf(-1)} {
		if got := h.LineAt(offset); got != 0 {
			t.Errorf("offset %g returned %d", offset, got)
		}
	}
	if got := h.LineAt(math.Inf(1)); got != h.Len() {
		t.Errorf("infinite offset returned %d", got)
	}
}

func TestHeightsInvalidReadIndexesPanic(t *testing.T) {
	h := mustHeights(t, 3, 10)
	for _, index := range []int{-1, 3} {
		t.Run(fmt.Sprintf("height_%d", index), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected index panic")
				}
			}()
			h.Height(index)
		})
	}
	for _, index := range []int{-1, 4} {
		t.Run(fmt.Sprintf("prefix_%d", index), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected index panic")
				}
			}()
			h.Prefix(index)
		})
	}
}

func TestHeightsWarmQueriesAndUpdatesDoNotAllocate(t *testing.T) {
	h := mustHeights(t, 10000, 64)
	var updateErr error
	var index int
	allocs := testing.AllocsPerRun(1000, func() {
		height := 80 + float64(index&1)
		updateErr = h.SetHeight(index, height)
		top := h.Prefix(index)
		lineBenchmarkSink = h.LineAt(top + h.Height(index)/2)
		heightBenchmarkSink = h.Total()
		index = (index + 97) % h.Len()
	})
	if updateErr != nil || allocs != 0 {
		t.Fatalf("allocs=%g update error=%v", allocs, updateErr)
	}
}

var heightBenchmarkSink float64
var lineBenchmarkSink int

func BenchmarkHeights(b *testing.B) {
	for _, count := range []int{200, 2000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			h := mustHeights(b, count, 64)
			b.Run("query", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					index := i % count
					top := h.Prefix(index)
					lineBenchmarkSink = h.LineAt(top + 32)
					heightBenchmarkSink = h.Total()
				}
			})
			b.Run("measure", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					// Change every row on every pass, including counts divisible by
					// eight; otherwise this accidentally measures the no-op path.
					height := 64 + float64(i&7) + 8*float64((i/count)&1)
					if err := h.SetHeight(i%count, height); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
