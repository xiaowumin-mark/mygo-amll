// Package layout contains UI-independent geometry indexes used by the native
// example. It is internal implementation detail, not a stable component API.
package layout

import (
	"errors"
	"fmt"
	"math"
)

// Heights indexes positive line heights in logical pixels. Every line starts
// with an estimate; preparing a visible line replaces that estimate with its
// measured height. No text, document, or UI resource is retained.
//
// A segment tree gives O(log n) updates, prefix sums, and offset lookup without
// traversing unprepared lines. Its scalar storage is less than 32*n bytes for
// n > 0 (plus the slice/header), independent of text length. A zero Heights is
// an empty index. The owning UI thread must serialize mutations and queries.
type Heights struct {
	count int
	base  int
	tree  []float64
}

// NewHeights creates count estimated rows. The estimate must be positive and
// finite, including when count is zero. Construction takes O(count) time.
func NewHeights(count int, estimate float64) (*Heights, error) {
	if count < 0 || !positiveFinite(estimate) {
		return nil, errors.New("layout: require nonnegative count and positive finite height")
	}
	if math.IsInf(float64(count)*estimate, 0) {
		return nil, errors.New("layout: total height overflows")
	}
	h := &Heights{count: count}
	if count == 0 {
		return h, nil
	}
	base := 1
	maxInt := int(^uint(0) >> 1)
	for base < count {
		if base > maxInt/4 {
			return nil, errors.New("layout: line count overflows index size")
		}
		base *= 2
	}
	h.base = base
	h.tree = make([]float64, 2*base)
	for i := 0; i < count; i++ {
		h.tree[base+i] = estimate
	}
	for i := base - 1; i > 0; i-- {
		h.tree[i] = h.tree[2*i] + h.tree[2*i+1]
	}
	if math.IsInf(h.tree[1], 0) {
		return nil, errors.New("layout: total height overflows")
	}
	return h, nil
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

// Len returns the number of rows, including those not measured yet.
func (h *Heights) Len() int { return h.count }

// Height returns one measured or estimated height. Invalid indexes panic,
// following Go slice indexing conventions.
func (h *Heights) Height(index int) float64 {
	if index < 0 || index >= h.count {
		panic("layout: height index out of range")
	}
	return h.tree[h.base+index]
}

// SetHeight replaces one estimate or measurement. It rejects invalid indexes,
// nonpositive/nonfinite heights, or an overflowing total without changing the
// index. Recomputing ancestor sums avoids cancellation from delta updates when
// a very tall estimate is replaced by a short measurement.
func (h *Heights) SetHeight(index int, height float64) error {
	if index < 0 || index >= h.count {
		return fmt.Errorf("layout: height index %d out of range", index)
	}
	if !positiveFinite(height) {
		return errors.New("layout: height must be positive and finite")
	}
	leaf := h.base + index
	if h.tree[leaf] == height {
		return nil
	}
	// Validate all ancestor sums before publishing any mutation.
	sum := height
	for node := leaf; node > 1; node /= 2 {
		sum += h.tree[node^1]
		if math.IsInf(sum, 0) {
			return errors.New("layout: total height overflows")
		}
	}
	h.tree[leaf] = height
	for node := leaf / 2; node > 0; node /= 2 {
		h.tree[node] = h.tree[2*node] + h.tree[2*node+1]
	}
	return nil
}

// Prefix returns the total height of rows [0, index), i.e. the top of index.
// Index may equal Len to obtain the document bottom; other invalid indexes
// panic. Prefix(0) is zero, including on an empty index.
func (h *Heights) Prefix(index int) float64 {
	if index < 0 || index > h.count {
		panic("layout: prefix index out of range")
	}
	if index == h.count {
		return h.Total()
	}
	sum := float64(0)
	for node := h.base + index; node > 1; node /= 2 {
		if node&1 != 0 {
			sum += h.tree[node-1]
		}
	}
	return sum
}

// Total returns the sum of all row heights.
func (h *Heights) Total() float64 {
	if h.count == 0 {
		return 0
	}
	return h.tree[1]
}

// LineAt returns the row containing offset in its half-open [top, bottom)
// interval. Negative offsets and NaN clamp to row zero; offsets at or beyond
// Total return Len. An empty index always returns zero. Positive infinity
// therefore returns Len and negative infinity returns zero.
func (h *Heights) LineAt(offset float64) int {
	if h.count == 0 || offset <= 0 || math.IsNaN(offset) {
		return 0
	}
	if offset >= h.tree[1] {
		return h.count
	}
	node := 1
	for node < h.base {
		left := 2 * node
		if offset < h.tree[left] {
			node = left
		} else {
			offset -= h.tree[left]
			node = left + 1
		}
	}
	return node - h.base
}
