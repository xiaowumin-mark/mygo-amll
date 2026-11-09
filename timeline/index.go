package timeline

import (
	"errors"
	"sort"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Index is immutable and safe for concurrent queries with separate buffers.
// It retains its document and stores one duration per line as a search index.
type Index struct {
	document     *lyric.Document
	prefixMaxEnd []time.Duration
}

// New builds an interval index. A nil document is an error; an empty one is valid.
func New(document *lyric.Document) (*Index, error) {
	if document == nil {
		return nil, errors.New("timeline: nil document")
	}
	index := &Index{document: document, prefixMaxEnd: make([]time.Duration, document.Len())}
	var maxEnd time.Duration
	for i := range index.prefixMaxEnd {
		if end := document.Line(i).End(); end > maxEnd {
			maxEnd = end
		}
		index.prefixMaxEnd[i] = maxEnd
	}
	return index, nil
}

// ActiveAt resets dst and returns all active line indices in document order.
// The returned slice reuses dst when capacity permits. Its buffer belongs to
// the caller and must not be shared by concurrent calls without synchronization.
// Reusing the buffer overwrites earlier results that refer to that buffer.
// Seeks work in either direction; gaps and negative positions return no lines.
//
// Each query costs O(log n + c), where c is the candidate interval window.
// Pathological long overlaps can make c = n, even when few lines are active.
// The query allocates no memory when dst has sufficient capacity.
func (index *Index) ActiveAt(position time.Duration, dst []int) []int {
	dst = dst[:0]
	upper := sort.Search(len(index.prefixMaxEnd), func(i int) bool {
		return index.document.Line(i).Start() > position
	})
	lower := sort.Search(upper, func(i int) bool {
		return index.prefixMaxEnd[i] > position
	})
	for i := lower; i < upper; i++ {
		if index.document.Line(i).End() > position {
			dst = append(dst, i)
		}
	}
	return dst
}
