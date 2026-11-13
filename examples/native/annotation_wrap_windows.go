//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/harfbuzz"
)

var referenceBidiSetLine = windowsICU.NewProc("ubidi_setLine")
var referenceBidiCountParagraphs = windowsICU.NewProc("ubidi_countParagraphs")
var referenceBidiParagraphByIndex = windowsICU.NewProc("ubidi_getParagraphByIndex")
var referenceBidiLevelAt = windowsICU.NewProc("ubidi_getLevelAt")

type referenceAnnotationParagraph struct{ start, end int }
type referenceAnnotationPart struct {
	referenceEmojiSpan
	zero, hyphen bool
}

type referenceWindowsAnnotationShaper struct {
	provider             *referenceFontProvider
	font                 ui.Font
	text                 []rune
	units                []uint16
	runeUnits, unitRunes []int
	spans                []referenceEmojiSpan
	features             []harfbuzz.Feature
	paragraph, line      uintptr
	paragraphs           []referenceAnnotationParagraph
}

func newReferenceAnnotationShaper(paragraph string, font ui.Font, p *referenceFontProvider) (referenceAnnotationShaper, error) {
	if p == nil || strings.ContainsAny(paragraph, "\r\n") || !(referenceWindowsBidiEngine{}).available() || referenceBidiSetLine.Find() != nil || referenceBidiCountParagraphs.Find() != nil || referenceBidiParagraphByIndex.Find() != nil || referenceBidiLevelAt.Find() != nil {
		return nil, errReferenceAnnotationContext
	}
	s := &referenceWindowsAnnotationShaper{provider: p, font: font, text: []rune(paragraph)}
	if len(s.text) == 0 {
		return s, nil
	}
	spans, hasColor, err := p.emojiSpans(s.text, font)
	if err != nil {
		return nil, err
	}
	if hasColor {
		if p.color == nil || p.face == nil || p.hb == nil || p.color.face == nil || !p.supportsEmojiFamily(font) {
			return nil, errReferenceEmojiCapability
		}
		for _, span := range spans {
			if span.color && !p.provesEmojiSelection(s.text[span.start:span.end], font) {
				return nil, errReferenceEmojiCapability
			}
		}
		scripts := referenceEmojiScripts(s.text, p.face)
		for i := range spans {
			spans[i].script = scripts[spans[i].start]
		}
	}
	s.spans = spans
	s.features, err = referenceEmojiFeatures(font.Features)
	if err != nil {
		return nil, err
	}
	s.units = utf16.Encode(s.text)
	s.runeUnits = make([]int, len(s.text)+1)
	s.unitRunes = make([]int, len(s.units)+1)
	at := 0
	for i, r := range s.text {
		s.runeUnits[i], s.unitRunes[at] = at, i
		if r > 0xffff {
			at++
			s.unitRunes[at] = -1
		}
		at++
	}
	s.runeUnits[len(s.text)], s.unitRunes[len(s.units)] = len(s.units), len(s.text)
	engine := referenceWindowsBidiEngine{}
	s.paragraph, s.line = engine.open(), engine.open()
	if s.paragraph == 0 || s.line == 0 || !engine.paragraph(s.paragraph, s.units) {
		s.close()
		return nil, errReferenceAnnotationContext
	}
	count, _, _ := referenceBidiCountParagraphs.Call(s.paragraph)
	if int32(count) < 1 || int32(count) > int32(len(s.units)+1) {
		s.close()
		return nil, errReferenceEmojiMapping
	}
	previous := 0
	for i := 0; i < int(int32(count)); i++ {
		var first, last, status int32
		var level uint8
		referenceBidiParagraphByIndex.Call(s.paragraph, uintptr(i), uintptr(unsafe.Pointer(&first)), uintptr(unsafe.Pointer(&last)), uintptr(unsafe.Pointer(&level)), uintptr(unsafe.Pointer(&status)))
		if status > 0 || int(first) != previous || last < first || int(last) > len(s.units) || s.unitRunes[int(first)] < 0 || s.unitRunes[int(last)] < 0 {
			s.close()
			return nil, errReferenceEmojiMapping
		}
		s.paragraphs = append(s.paragraphs, referenceAnnotationParagraph{s.unitRunes[int(first)], s.unitRunes[int(last)]})
		previous = int(last)
	}
	if previous != len(s.units) {
		s.close()
		return nil, errReferenceEmojiMapping
	}
	return s, nil
}

func (s *referenceWindowsAnnotationShaper) close() {
	engine := referenceWindowsBidiEngine{}
	if s.line != 0 {
		engine.close(s.line)
		s.line = 0
	}
	if s.paragraph != 0 {
		engine.close(s.paragraph)
		s.paragraph = 0
	}
	runtime.KeepAlive(s.units)
}

func (s *referenceWindowsAnnotationShaper) shape(start, end int) referenceTextShape {
	return s.shapeMode(start, end, true)
}

func (s *referenceWindowsAnnotationShaper) shapeUnbroken(start, end int) referenceTextShape {
	return s.shapeMode(start, end, false)
}

func (s *referenceWindowsAnnotationShaper) shapeMode(start, end int, insertHyphen bool) referenceTextShape {
	fail := func(err error) referenceTextShape { return referenceTextShape{Err: err} }
	if start < 0 || end < start || end > len(s.text) {
		return fail(errReferenceEmojiMapping)
	}
	originalEnd := end
	for end > start && s.text[end-1] == ' ' {
		end--
	}
	if start == end {
		return referenceTextShape{Exact: true}
	}
	if s.paragraph == 0 || s.line == 0 {
		return fail(errReferenceAnnotationContext)
	}
	// Collapsible spaces following SHY are removed before the line-end
	// discretionary decision; the final consumed paragraph range has no break.
	hyphen := insertHyphen && originalEnd < len(s.text) && s.text[end-1] == '\u00ad'
	contentEnd := end
	if hyphen {
		contentEnd--
	}
	result := referenceTextShape{Exact: true}
	covered := make([]bool, end-start)
	for _, paragraph := range s.paragraphs {
		from, to := max(start, paragraph.start), min(contentEnd, paragraph.end)
		if from >= to {
			continue
		}
		if err := s.shapeParagraphLine(&result, covered, start, end, from, to, hyphen && to == contentEnd); err != nil {
			return fail(err)
		}
	}
	if hyphen && contentEnd == start {
		level, _, _ := referenceBidiLevelAt.Call(s.paragraph, uintptr(s.runeUnits[start]))
		part := referenceEmojiSpan{start: start, end: end}
		if err := s.appendAnnotationPart(&result, part, uint8(level)&1 != 0, start, true); err != nil {
			return fail(err)
		}
		covered[0] = true
	}
	for _, ok := range covered {
		if !ok {
			return fail(errReferenceEmojiMapping)
		}
	}
	return result
}

func (s *referenceWindowsAnnotationShaper) shapeParagraphLine(result *referenceTextShape, covered []bool, rowStart, rowEnd, from, to int, hyphen bool) error {
	firstUnit, lastUnit := s.runeUnits[from], s.runeUnits[to]
	var status int32
	referenceBidiSetLine.Call(s.paragraph, uintptr(firstUnit), uintptr(lastUnit), s.line, uintptr(unsafe.Pointer(&status)))
	runtime.KeepAlive(s.units)
	if status > 0 {
		return errReferenceAnnotationContext
	}
	engine := referenceWindowsBidiEngine{}
	count, ok := engine.runCount(s.line)
	if !ok || count < 1 || count > lastUnit-firstUnit {
		return errReferenceEmojiMapping
	}
	for runAt := range count {
		run, ok := engine.visualRun(s.line, runAt)
		if !ok || run.start < 0 || run.length < 1 || run.start+run.length > lastUnit-firstUnit {
			return errReferenceEmojiMapping
		}
		runStart, runEnd := s.unitRunes[firstUnit+run.start], s.unitRunes[firstUnit+run.start+run.length]
		if runStart < from || runEnd > to || runStart < 0 || runEnd < 0 {
			return errReferenceEmojiMapping
		}
		// The original SHY is BN and L1 would reset its line-end level. Attach
		// its visible replacement to the preceding logical run before that
		// reset, preserving RTL word-end placement at the visual left.
		if hyphen && runEnd == to {
			runEnd++
		}
		for at := runStart; at < runEnd; at++ {
			if covered[at-rowStart] {
				return errReferenceEmojiMapping
			}
			covered[at-rowStart] = true
		}
		var parts []referenceAnnotationPart
		first := sort.Search(len(s.spans), func(i int) bool { return s.spans[i].end > runStart })
		for spanAt := first; spanAt < len(s.spans) && s.spans[spanAt].start < runEnd; spanAt++ {
			span := s.spans[spanAt]
			if span.color && (span.start < runStart || span.end > runEnd) {
				return errReferenceEmojiMapping
			}
			span.start, span.end = max(span.start, runStart), min(span.end, runEnd)
			part := referenceAnnotationPart{referenceEmojiSpan: span}
			part.zero = span.end-span.start == 1 && (s.text[span.start] == '\u2028' || s.text[span.start] == '\u2029')
			part.hyphen = hyphen && span.end == rowEnd && span.end-span.start == 1 && s.text[span.start] == '\u00ad'
			// The browser's generated hyphen is a separate shape: merging it
			// with "f", for example, introduces kerning absent from the DOM.
			if len(parts) > 0 {
				previous := &parts[len(parts)-1]
				if !part.zero && !previous.zero && !part.hyphen && !previous.hyphen && previous.end == part.start && previous.color == part.color && (!part.color || previous.script == part.script) {
					previous.end = part.end
					continue
				}
			}
			parts = append(parts, part)
		}
		for partAt := range parts {
			if run.rtl {
				partAt = len(parts) - partAt - 1
			}
			part := parts[partAt]
			if part.zero {
				continue
			}
			if err := s.appendAnnotationPart(result, part.referenceEmojiSpan, run.rtl, rowStart, hyphen && part.end == rowEnd); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *referenceWindowsAnnotationShaper) appendAnnotationPart(result *referenceTextShape, part referenceEmojiSpan, rtl bool, rowStart int, hyphen bool) error {
	if part.color {
		colors, width, err := s.provider.shapeEmojiPart(s.text, part, rtl, s.font, s.features)
		if err != nil {
			return err
		}
		for i := range colors {
			colors[i].X += result.Width
		}
		result.Colors = append(result.Colors, colors...)
		result.Width += width
		return nil
	}
	var glyphs []ui.Glyph
	var width float32
	var exact bool
	if hyphen {
		display := append([]rune(nil), s.text[part.start:part.end]...)
		display[len(display)-1] = '\u2010'
		glyphs, width, exact = s.provider.shapeEmojiMonoPart(display, referenceEmojiSpan{start: 0, end: len(display)}, rtl, s.font)
		for i := range glyphs {
			glyphs[i].Cluster += part.start
		}
	} else {
		glyphs, width, exact = s.provider.shapeEmojiMonoPart(s.text, part, rtl, s.font)
	}
	if !exact {
		return errReferenceEmojiMapping
	}
	for i := range glyphs {
		glyphs[i].X += result.Width
		glyphs[i].Cluster -= rowStart
	}
	result.Glyphs = append(result.Glyphs, glyphs...)
	result.Width += width
	return nil
}
