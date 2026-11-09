package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

const (
	geometryEntryLimit = 32
	geometryByteLimit  = 16 << 20
	lineByteLimit      = 16 << 10
	lineWordLimit      = 512
)

var errGeometryLimit = errors.New("native spike: line exceeds geometry budget")

// The three text offset units are deliberately different types. Only the cold
// geometry path translates the lyric's UTF-8 text into shaping's rune offsets
// and TextLayout's UTF-16 offsets.
type byteOffset int
type runeOffset int
type utf16Offset int

type wordTextRange struct {
	startByte, endByte   byteOffset
	startRune, endRune   runeOffset
	startUTF16, endUTF16 utf16Offset
}

type wordGeometry struct {
	start, end time.Duration
	text       wordTextRange
	rects      []ui.Rect
	rtl        bool
	atomic     bool
}

type lineGeometry struct {
	layout        *ui.TextLayout
	words         []wordGeometry
	width, height float32
	bytes         int
}

func finitePositive(value float32) bool {
	return value > 0 && !math.IsInf(float64(value), 0) && !math.IsNaN(float64(value))
}

// buildLineGeometry shapes the complete line, never its individual words.
// The spike requires word texts (including spaces) to concatenate exactly to
// line.Text. Formats with other conventions must normalize before this layer.
// An unsegmented line is treated as one segment lasting the line's interval.
func buildLineGeometry(line lyric.LineView, font ui.Font, width float32) (*lineGeometry, error) {
	if !finitePositive(width) {
		return nil, errors.New("native spike: layout width must be positive and finite")
	}
	text := line.Text()
	if len(text) > lineByteLimit || line.WordCount() > lineWordLimit {
		return nil, errGeometryLimit
	}
	if !utf8.ValidString(text) {
		return nil, errors.New("native spike: line text is not valid UTF-8")
	}
	count := max(1, line.WordCount())
	geometry := &lineGeometry{words: make([]wordGeometry, count)}
	var bytes byteOffset
	var runes runeOffset
	var units utf16Offset
	for i := range geometry.words {
		word := lyric.Word{Text: text, Start: line.Start(), End: line.End()}
		if line.WordCount() != 0 {
			word = line.Word(i)
		}
		if !utf8.ValidString(word.Text) {
			return nil, fmt.Errorf("native spike: word %d splits an invalid UTF-8 boundary", i)
		}
		if !strings.HasPrefix(text[bytes:], word.Text) {
			return nil, fmt.Errorf("native spike: word %d does not match line text at UTF-8 byte %d", i, bytes)
		}
		part := &geometry.words[i]
		part.start, part.end = word.Start, word.End
		part.text.startByte, part.text.startRune, part.text.startUTF16 = bytes, runes, units
		bytes += byteOffset(len(word.Text))
		for _, r := range word.Text {
			runes++
			units += utf16Offset(utf16.RuneLen(r))
		}
		part.text.endByte, part.text.endRune, part.text.endUTF16 = bytes, runes, units
	}
	if int(bytes) != len(text) {
		return nil, errors.New("native spike: timed words do not cover the complete line text")
	}

	geometry.layout = ui.ShapeText(text, font, width)
	geometry.width, geometry.height = geometry.layout.Size()
	// TextLayout deliberately hides its glyphs. A second, complete-line shape
	// audits cluster boundaries and run directions once, without depending on
	// upstream internal packages. These glyph positions are never used to draw
	// the wrapped layout. M3 will need a richer retained cluster mapping.
	glyphs := ui.Shape(text, font)
	for i := range geometry.words {
		word := &geometry.words[i]
		auditWordGlyphs(word, glyphs)
		// SelectionRects expands to whole graphemes. Checking its public
		// boundary API also covers clusters not represented by a drawable glyph.
		for _, offset := range []utf16Offset{word.text.startUTF16, word.text.endUTF16} {
			if offset > 0 && geometry.layout.NextBoundary(int(offset)-1) != int(offset) {
				word.atomic = true
			}
		}
		word.rects = geometry.layout.SelectionRects(ui.TextInputRange{
			Start: int(word.text.startUTF16), End: int(word.text.endUTF16),
		})
		// Progress follows wrapped rows in reading order, without assuming a
		// logical word owns a single rectangle or monotonically increasing X.
		slices.SortFunc(word.rects, func(a, b ui.Rect) int {
			if a.Y < b.Y {
				return -1
			}
			if a.Y > b.Y {
				return 1
			}
			if a.X == b.X {
				return 0
			}
			if (a.X < b.X) != word.rtl {
				return -1
			}
			return 1
		})
	}

	// A conservative component estimate includes opaque layout/glyph storage;
	// it is not an accounting of mygo's global fonts or the process's RSS.
	geometry.bytes = 4096 + len(text)*8 + int(runes)*256 + len(glyphs)*64 + count*256
	for _, word := range geometry.words {
		geometry.bytes += cap(word.rects) * 64
	}
	if geometry.bytes > geometryByteLimit {
		return nil, errGeometryLimit
	}
	return geometry, nil
}

func auditWordGlyphs(word *wordGeometry, glyphs []ui.Glyph) {
	var haveDirection bool
	for _, glyph := range glyphs {
		start, end := runeOffset(glyph.Cluster), runeOffset(glyph.Cluster+glyph.Runes)
		if end <= word.text.startRune || start >= word.text.endRune {
			continue
		}
		if start < word.text.startRune || end > word.text.endRune {
			word.atomic = true
		}
		if haveDirection && glyph.RTL != word.rtl {
			word.atomic = true
		}
		word.rtl = glyph.RTL
		haveDirection = true
	}
}

// appendHighlights appends layout-local clip rectangles. The caller owns dst;
// sufficient capacity makes this query allocation-free. It does not mutate
// geometry or advance playback, and is safe to repeat at the same position.
// A timing boundary through a ligature/grapheme, or a mixed-direction word,
// conservatively highlights the entire word only at its exclusive end.
func (geometry *lineGeometry) appendHighlights(dst []ui.Rect, position time.Duration) []ui.Rect {
	for _, word := range geometry.words {
		if position < word.start {
			continue
		}
		if position >= word.end {
			dst = append(dst, word.rects...)
			continue
		}
		if word.atomic {
			continue
		}
		var width float32
		for _, rect := range word.rects {
			width += rect.W
		}
		remaining := width * float32(float64(position-word.start)/float64(word.end-word.start))
		for _, rect := range word.rects {
			if remaining <= 0 {
				break
			}
			part := min(rect.W, remaining)
			if word.rtl {
				rect.X += rect.W - part
			}
			rect.W = part
			dst = append(dst, rect)
			remaining -= part
		}
	}
	return dst
}

type geometryCacheEntry struct {
	line     int
	geometry *lineGeometry
	used     uint64
	pinned   bool
}

// geometryCache belongs to the UI thread. It owns at most 32 retained layouts
// and 16 MiB of estimated geometry. Song/font/width/DPI changes drop all old
// document and layout references; no upstream GPU release guarantee is implied.
type geometryCache struct {
	document     *lyric.Document
	font         ui.Font
	width, scale float32
	entries      []geometryCacheEntry
	bytes        int
	clock        uint64
	shapes       uint64
}

func (cache *geometryCache) clear() { *cache = geometryCache{} }

// beginFrame releases the preceding frame's pins. The caller must first drop
// its old frame rows, and pin each layout as it prepares the next frame. Frame
// references then remain a subset of cache entries and share the same budget.
func (cache *geometryCache) beginFrame() {
	for i := range cache.entries {
		cache.entries[i].pinned = false
	}
}

func (cache *geometryCache) pin(geometry *lineGeometry) {
	for i := range cache.entries {
		if cache.entries[i].geometry == geometry {
			cache.entries[i].pinned = true
			return
		}
	}
}

func (cache *geometryCache) get(document *lyric.Document, index int, font ui.Font, width, scale float32) (*lineGeometry, error) {
	if document == nil || index < 0 || index >= document.Len() {
		return nil, errors.New("native spike: invalid document or line index")
	}
	if !finitePositive(width) || !finitePositive(scale) {
		return nil, errors.New("native spike: width and scale must be positive and finite")
	}
	if cache.document != document || cache.font != font || cache.width != width || cache.scale != scale {
		cache.clear()
		cache.document, cache.font, cache.width, cache.scale = document, font, width, scale
	}
	cache.clock++
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.line == index {
			entry.used = cache.clock
			return entry.geometry, nil
		}
	}
	geometry, err := buildLineGeometry(document.Line(index), font, width)
	if err != nil {
		return nil, err
	}
	cache.shapes++
	var pinnedEntries, pinnedBytes int
	for _, entry := range cache.entries {
		if entry.pinned {
			pinnedEntries++
			pinnedBytes += entry.geometry.bytes
		}
	}
	if pinnedEntries >= geometryEntryLimit || pinnedBytes+geometry.bytes > geometryByteLimit {
		return nil, errGeometryLimit
	}
	for len(cache.entries) != 0 && (len(cache.entries) >= geometryEntryLimit || cache.bytes+geometry.bytes > geometryByteLimit) {
		oldest := -1
		for i := range cache.entries {
			if !cache.entries[i].pinned && (oldest == -1 || cache.entries[i].used < cache.entries[oldest].used) {
				oldest = i
			}
		}
		if oldest == -1 {
			return nil, errGeometryLimit
		}
		cache.bytes -= cache.entries[oldest].geometry.bytes
		last := len(cache.entries) - 1
		cache.entries[oldest] = cache.entries[last]
		cache.entries[last] = geometryCacheEntry{}
		cache.entries = cache.entries[:last]
	}
	cache.entries = append(cache.entries, geometryCacheEntry{line: index, geometry: geometry, used: cache.clock})
	cache.bytes += geometry.bytes
	return geometry, nil
}
