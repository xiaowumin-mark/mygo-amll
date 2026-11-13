// SPDX-License-Identifier: AGPL-3.0-only
// The reference word-box geometry is derived from AMLL commit
// f32285864056587c4afa59d310208b0aa829cb53; source paths and adaptations
// are recorded in word_layout.go. Legacy cluster geometry remains solely
// for compatibility diagnostics and is not an AMLL-equivalent word renderer.
// CSS box parameters: packages/core/src/styles/lyric-player.module.css;
// reference construction: packages/core/src/lyric-player/dom/lyric-line.ts.
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
	fontProvider      *referenceFontProvider
	textScale         float32
	maskSignature     [32]byte
	paintBounds       ui.Rect // CSS paint-containment box, relative to content
	layout            *ui.TextLayout
	words             []wordGeometry
	clusters          []clusterGeometry
	annotations       []annotationGeometry
	blocks            []wordBlock
	groups            []wordGroup
	font              ui.Font
	dynamic           bool
	bodyHeight        float32
	segmenterFallback bool
	bidiFallback      bool
	start, end        time.Duration
	width, height     float32
	bytes             int
}

// A cluster owns wrapped selection geometry, in logical text order. The
// direction belongs to the cluster, not to the complete timed word: a word
// can cross several bidi runs. Shared clusters are indivisible and wait for
// the latest end of every timed word touching them.
type clusterGeometry struct {
	start, end time.Duration
	from, to   float64
	rects      []ui.Rect
	rtl        bool
	atomic     bool
}

type annotationGeometry struct {
	layout          *ui.TextLayout
	y               float32 // native drawing origin, including CSS baseline offset
	boxY, boxHeight float32 // CSS flow box, independent of native minimum leading
	font            ui.Font
	text            string
	rows            []cssTextRow
}

func finitePositive(value float32) bool {
	return value > 0 && !math.IsInf(float64(value), 0) && !math.IsNaN(float64(value))
}

// buildLineGeometry retains reference word/character boxes, plus a whole-line
// diagnostic layout for compatibility tests. Actual effects consume blocks,
// not cropped copies of that diagnostic line. Timed text must match in order;
// only untimed whitespace may separate source words. Public system-font
// fallback is used throughout. Explicit line language overrides Font.Lang.
func buildLineGeometry(line lyric.LineView, font ui.Font, width float32) (*lineGeometry, error) {
	return buildLineGeometryWithStyle(line, font, width, geometryStyle{Dynamic: line.WordCount() > 1})
}

func buildLineGeometryWithStyle(line lyric.LineView, font ui.Font, width float32, style geometryStyle) (*lineGeometry, error) {
	if !finitePositive(width) {
		return nil, errors.New("native spike: layout width must be positive and finite")
	}
	text := line.Text()
	if len(text)+len(line.Translation())+len(line.Romanization()) > lineByteLimit || line.WordCount() > lineWordLimit {
		return nil, errGeometryLimit
	}
	if !utf8.ValidString(text) {
		return nil, errors.New("native spike: line text is not valid UTF-8")
	}
	if err := preflightReferenceRuby(line); err != nil {
		return nil, err
	}
	count := max(1, line.WordCount())
	textScale := style.TextScale
	if textScale == 0 {
		textScale = 1
	}
	if !finitePositive(textScale) {
		return nil, errors.New("native: text scale must be positive and finite")
	}
	geometry := &lineGeometry{words: make([]wordGeometry, count), fontProvider: style.FontProvider, textScale: textScale}
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
		// TTML can keep separator spaces in the paragraph while omitting them
		// from timed spans. Match the next exact occurrence only across spaces;
		// skipping other text would silently assign the wrong timing.
		for !strings.HasPrefix(text[bytes:], word.Text) && int(bytes) < len(text) {
			r, size := utf8.DecodeRuneInString(text[bytes:])
			if !referenceSpace(r) {
				break
			}
			bytes += byteOffset(size)
			runes++
			units += utf16Offset(utf16.RuneLen(r))
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
	if referenceTrim(text[bytes:]) != "" {
		return nil, errors.New("native spike: timed words do not cover the complete line text")
	}

	if !style.UseHostLanguage && line.Language() != "" {
		font.Lang = line.Language()
	}
	if font.Size == 0 {
		font.Size = 14
	}
	if !finitePositive(font.Size) {
		return nil, errors.New("native: font size must be positive and finite")
	}
	font.LineHeight = 1.2
	if line.Background() {
		font.Size = max(font.Size*0.7, 10)
		if style.BackgroundFontSize != 0 {
			if !finitePositive(style.BackgroundFontSize) {
				return nil, errors.New("native: background font size must be positive and finite")
			}
			font.Size = max(style.BackgroundFontSize, 10)
		}
	}
	if style.CSSPaddingSides < 0 || style.CSSPaddingSides > 2 {
		return nil, errors.New("native: CSS padding sides must be within 0..2")
	}
	if style.BorderWidth != 0 && !finitePositive(style.BorderWidth) {
		return nil, errors.New("native: line border width must be positive and finite")
	}
	padding := cssLayoutLength(font.Size*.2, textScale)
	if style.BorderWidth > 0 {
		borderWidth := cssLayoutLength(style.BorderWidth, textScale)
		width = borderWidth - float32(style.CSSPaddingSides)*padding
		if style.CSSPaddingSides == 1 {
			width -= cssLayoutLength(borderWidth*.15, textScale)
		}
	} else {
		width -= float32(style.CSSPaddingSides) * padding
	}
	width = max(1, width)
	geometry.font, geometry.dynamic = font, style.Dynamic
	geometry.start, geometry.end = line.Start(), line.End()
	geometry.layout = shapeLyricText(text, font, width, line.Duet())
	geometry.width, geometry.height = geometry.layout.Size()
	if line.Duet() {
		// Retained right-aligned geometry already contains the per-row X
		// offsets. Size reports text widths without these offsets, so expose
		// the complete shaping box to the scene instead of shifting it twice.
		geometry.width = width
	}
	// TextLayout deliberately hides its glyphs. A second complete-line shape
	// supplies cluster boundaries and directions, never drawing positions.
	// Unioning these boundaries with wrapped graphemes is conservative if
	// wrapping happens to disable a ligature on one of the retained rows.
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
	}
	geometry.buildClusters(text, glyphs)
	geometry.bytes = 4096 + len(text)*8 + utf8.RuneCountInString(text)*256 + len(glyphs)*64 + count*256 + cap(geometry.clusters)*96
	for _, word := range geometry.words {
		geometry.bytes += cap(word.rects) * 64
	}
	for _, cluster := range geometry.clusters {
		geometry.bytes += cap(cluster.rects) * 64
	}
	if err := geometry.buildReferenceBoxes(line, font, width, style); err != nil {
		return nil, err
	}
	for _, block := range geometry.blocks {
		geometry.end = max(geometry.end, block.end)
		geometry.bidiFallback = geometry.bidiFallback || block.bidiFallback
	}
	geometry.height = geometry.bodyHeight
	annotationFont := font
	annotationFont.Size = max(font.Size*0.5, 10)
	annotationFont.LineHeight = 1.5
	for _, annotation := range []string{line.Translation(), line.Romanization()} {
		if annotation == "" {
			continue
		}
		if !utf8.ValidString(annotation) {
			return nil, errors.New("native spike: annotation is not valid UTF-8")
		}
		// Subtitles are ordinary HTML block text in the reference, unlike the
		// pre-wrap word/Ruby spans. Collapse HTML whitespace only; NBSP and other
		// non-collapsible characters retain their measured advances.
		annotation = strings.Trim(collapseHTMLWhitespace(annotation), " ")
		if annotation == "" {
			continue
		}
		layout := shapeLyricText(annotation, annotationFont, width, line.Duet())
		_, height, drawY := cssLineBox(layout, annotationFont, textScale)
		rows, exact := cssAnnotationRowsAtScale(annotation, layout, annotationFont, width, line.Duet(), textScale, geometry.fontProvider)
		if geometry.fontProvider != nil {
			var wrapErr error
			rows, height, exact, wrapErr = referenceAnnotationRows(annotation, annotationFont, width, line.Duet(), geometry.fontProvider, textScale)
			if wrapErr != nil {
				return nil, wrapErr
			}
		}
		geometry.bidiFallback = geometry.bidiFallback || !exact
		geometry.annotations = append(geometry.annotations, annotationGeometry{layout: layout, y: geometry.height + drawY, boxY: geometry.height, boxHeight: height, font: annotationFont, text: annotation, rows: rows})
		geometry.height += height
		geometry.bytes += 4096 + len(annotation)*8 + utf8.RuneCountInString(annotation)*384
		geometry.bytes += cap(rows) * 64
	}

	// A conservative component estimate includes opaque layout/glyph storage;
	// it is not an accounting of mygo's global fonts or the process's RSS.
	if geometry.bytes > geometryByteLimit {
		return nil, errGeometryLimit
	}
	geometry.maskSignature = geometry.computeMaskSignature()
	if style.BorderWidth != 0 {
		borderWidth := cssLayoutLength(style.BorderWidth, textScale)
		x := -padding
		if line.Duet() {
			x = geometry.width + padding - borderWidth
		}
		geometry.paintBounds = ui.Rect{X: x, Y: -padding, W: borderWidth, H: geometry.height + 2*padding}
	}
	return geometry, nil
}

func shapeLyricText(text string, font ui.Font, width float32, duet bool) *ui.TextLayout {
	if !duet {
		return ui.ShapeText(text, font, width)
	}
	// End is a logical alignment, so it places RTL paragraphs on the left.
	// A right-to-left container's Start fixes the physical right edge while
	// retaining each paragraph's first-strong-character reading direction.
	return ui.ShapeRichTextWith([]ui.Span{{Text: text}}, font, ui.TextShape{Width: width, Direction: ui.RightToLeft})
}

// buildClusters retains one geometry per union of a shaped cluster and a
// grapheme. No indexing conversion or geometry query remains in the hot path.
func (geometry *lineGeometry) buildClusters(text string, glyphs []ui.Glyph) {
	units := []utf16Offset{0}
	for _, r := range text {
		units = append(units, units[len(units)-1]+utf16Offset(utf16.RuneLen(r)))
	}
	boundaries := make([]bool, len(units))
	boundaries[0], boundaries[len(boundaries)-1] = true, true
	for at := 0; at < int(units[len(units)-1]); {
		next := geometry.layout.NextBoundary(at)
		if next <= at {
			break
		}
		i, ok := slices.BinarySearch(units, utf16Offset(next))
		if ok {
			boundaries[i] = true
		}
		at = next
	}
	for _, glyph := range glyphs {
		for i := max(1, glyph.Cluster+1); i < min(glyph.Cluster+glyph.Runes, len(boundaries)-1); i++ {
			boundaries[i] = false
		}
	}
	type logicalCluster struct {
		start, end runeOffset
		owners     int
		width      float32
	}
	var logical []logicalCluster
	for start, end := 0, 1; end < len(boundaries); end++ {
		if !boundaries[end] {
			continue
		}
		logical = append(logical, logicalCluster{start: runeOffset(start), end: runeOffset(end)})
		start = end
	}
	geometry.clusters = make([]clusterGeometry, len(logical))
	runeCluster := make([]int, len(units)-1)
	for i, span := range logical {
		for at := span.start; at < span.end; at++ {
			runeCluster[at] = i
		}
	}
	haveDirection := make([]bool, len(logical))
	for _, glyph := range glyphs {
		if glyph.Cluster < 0 || glyph.Cluster >= len(runeCluster) {
			continue
		}
		i := runeCluster[glyph.Cluster]
		cluster := &geometry.clusters[i]
		if haveDirection[i] && cluster.rtl != glyph.RTL {
			cluster.atomic = true
		}
		cluster.rtl, haveDirection[i] = glyph.RTL, true
	}
	for i := range logical {
		span := &logical[i]
		cluster := &geometry.clusters[i]
		cluster.rects = geometry.layout.SelectionRects(ui.TextInputRange{Start: int(units[span.start]), End: int(units[span.end])})
		// Use the retained layout's actual bidi context when both edges are
		// on one row. In particular, neutral-only duet paragraphs inherit the
		// right-to-left container while ui.Shape has no direction parameter.
		first := geometry.layout.CaretAt(ui.TextCaretPosition{Index: int(units[span.start]), Affinity: ui.TextDownstream})
		last := geometry.layout.CaretAt(ui.TextCaretPosition{Index: int(units[span.end]), Affinity: ui.TextUpstream})
		if first.Y == last.Y && first.X != last.X {
			cluster.rtl = last.X < first.X
		}
		sortRects(cluster.rects, cluster.rtl)
		for _, rect := range cluster.rects {
			span.width += rect.W
		}
		for _, word := range geometry.words {
			if word.text.endRune > span.start && word.text.startRune < span.end {
				span.owners++
				cluster.end = max(cluster.end, word.end)
			}
		}
		cluster.atomic = cluster.atomic || span.owners != 1
		// Untimed whitespace has no ink to fill, and must never borrow the
		// adjacent word's time. An empty owner set gets no highlight geometry.
		if span.owners == 0 {
			cluster.rects = nil
		}
	}
	for _, word := range geometry.words {
		var width float32
		for _, span := range logical {
			if word.text.endRune > span.start && word.text.startRune < span.end {
				width += span.width
			}
		}
		if width <= 0 {
			continue
		}
		var used float32
		for i, span := range logical {
			if word.text.endRune <= span.start || word.text.startRune >= span.end {
				continue
			}
			if span.owners == 1 {
				cluster := &geometry.clusters[i]
				cluster.start, cluster.end = word.start, word.end
				cluster.from, cluster.to = float64(used/width), float64((used+span.width)/width)
			}
			used += span.width
		}
	}
}

func sortRects(rects []ui.Rect, rtl bool) {
	slices.SortFunc(rects, func(a, b ui.Rect) int {
		if a.Y < b.Y {
			return -1
		}
		if a.Y > b.Y {
			return 1
		}
		if a.X == b.X {
			return 0
		}
		if (a.X < b.X) != rtl {
			return -1
		}
		return 1
	})
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
// Ordinary clusters fill in logical order and in their own bidi direction.
// A cluster crossing timed-word boundaries waits for their latest exclusive
// end; other clusters of those words keep their individual smooth progress.
func (geometry *lineGeometry) appendHighlights(dst []ui.Rect, position time.Duration) []ui.Rect {
	if geometry.clusters != nil {
		for _, cluster := range geometry.clusters {
			if position >= cluster.end {
				dst = appendProgressRects(dst, cluster.rects, 1, cluster.rtl)
			} else if !cluster.atomic && position > cluster.start && cluster.to > cluster.from {
				progress := float64(position-cluster.start) / float64(cluster.end-cluster.start)
				dst = appendProgressRects(dst, cluster.rects, (progress-cluster.from)/(cluster.to-cluster.from), cluster.rtl)
			}
		}
		return dst
	}
	for _, word := range geometry.words {
		dst = appendTimedRects(dst, word.rects, word.start, word.end, word.rtl, word.atomic, position)
	}
	return dst
}

// highlightRectCount is an upper bound before adjacent clips are coalesced.
// The scene reserves caller-owned scratch only while preparing cold geometry.
func (geometry *lineGeometry) highlightRectCount() int {
	count := 0
	if geometry.clusters != nil {
		for _, cluster := range geometry.clusters {
			count += len(cluster.rects)
		}
	} else {
		for _, word := range geometry.words {
			count += len(word.rects)
		}
	}
	return count
}

// appendStaticHighlights steps at complete-word ends. A cluster shared by
// several words still waits for the latest end, exactly as the animated path.
func (geometry *lineGeometry) appendStaticHighlights(dst []ui.Rect, position time.Duration) []ui.Rect {
	if geometry.clusters != nil {
		for _, cluster := range geometry.clusters {
			if position >= cluster.end {
				dst = appendProgressRects(dst, cluster.rects, 1, cluster.rtl)
			}
		}
	} else {
		for _, word := range geometry.words {
			if position >= word.end {
				dst = appendProgressRects(dst, word.rects, 1, word.rtl)
			}
		}
	}
	return dst
}

func appendTimedRects(dst, rects []ui.Rect, start, end time.Duration, rtl, atomic bool, position time.Duration) []ui.Rect {
	if position < start {
		return dst
	}
	if position >= end {
		return appendProgressRects(dst, rects, 1, rtl)
	}
	if atomic {
		return dst
	}
	return appendProgressRects(dst, rects, float64(position-start)/float64(end-start), rtl)
}

func appendProgressRects(dst, rects []ui.Rect, progress float64, rtl bool) []ui.Rect {
	if progress <= 0 {
		return dst
	}
	var width float32
	for _, rect := range rects {
		width += rect.W
	}
	remaining := width * float32(min(progress, 1))
	for _, rect := range rects {
		if remaining <= 0 {
			break
		}
		part := min(rect.W, remaining)
		if rtl {
			rect.X += rect.W - part
		}
		rect.W = part
		if rect.W > 0 && rect.H > 0 {
			dst = appendHighlightRect(dst, rect)
		}
		remaining -= part
	}
	return dst
}

func appendHighlightRect(dst []ui.Rect, rect ui.Rect) []ui.Rect {
	if len(dst) != 0 {
		last := &dst[len(dst)-1]
		if last.Y == rect.Y && last.H == rect.H && last.X <= rect.X+rect.W && rect.X <= last.X+last.W {
			right := max(last.X+last.W, rect.X+rect.W)
			last.X = min(last.X, rect.X)
			last.W = right - last.X
			return dst
		}
	}
	return append(dst, rect)
}

type geometryCacheEntry struct {
	line     int
	geometry *lineGeometry
	used     uint64
	pinned   bool
}

// geometryCache belongs to the UI thread. It owns at most 32 retained lines
// and 16 MiB of estimated geometry. Song/font/width/DPI changes drop all old
// document and layout references; no upstream GPU release guarantee is implied.
type geometryCache struct {
	document      *lyric.Document
	font          ui.Font
	width, scale  float32
	entries       []geometryCacheEntry
	bytes         int
	clock         uint64
	shapes        uint64
	style         geometryStyle
	styleDocument *lyric.Document
	defaultStyle  geometryStyle
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
	if cache.styleDocument != document {
		cache.styleDocument, cache.defaultStyle = document, geometryStyle{}
		for i := range document.Len() {
			if document.Line(i).WordCount() > 1 {
				cache.defaultStyle.Dynamic = true
				break
			}
		}
	}
	return cache.getWithStyle(document, index, font, width, scale, cache.defaultStyle)
}

func (cache *geometryCache) getWithStyle(document *lyric.Document, index int, font ui.Font, width, scale float32, style geometryStyle) (*lineGeometry, error) {
	if document == nil || index < 0 || index >= document.Len() {
		return nil, errors.New("native spike: invalid document or line index")
	}
	if !finitePositive(width) || !finitePositive(scale) {
		return nil, errors.New("native spike: width and scale must be positive and finite")
	}
	if cache.document != document || cache.font != font || cache.width != width || cache.scale != scale || cache.style != style {
		derivedDocument, derivedStyle := cache.styleDocument, cache.defaultStyle
		cache.clear()
		if derivedDocument == document {
			cache.styleDocument, cache.defaultStyle = derivedDocument, derivedStyle
		}
		cache.document, cache.font, cache.width, cache.scale = document, font, width, scale
		cache.style = style
	}
	cache.clock++
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.line == index {
			entry.used = cache.clock
			return entry.geometry, nil
		}
	}
	geometry, err := buildLineGeometryWithStyle(document.Line(index), font, width, style)
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
