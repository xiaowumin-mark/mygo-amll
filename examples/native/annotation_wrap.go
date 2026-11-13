// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/segmenter"
)

var (
	errReferenceAnnotationLimit   = errors.New("native annotation wrapping: preparation budget exceeded")
	errReferenceAnnotationContext = errors.New("native annotation wrapping: paragraph line shaping is unavailable")
)

type referenceAnnotationShaper interface {
	shape(start, end int) referenceTextShape         // half-open original paragraph rune indices
	shapeUnbroken(start, end int) referenceTextShape // no discretionary break glyph
	close()
}

// ShapeRange shapes a line against its original LTR paragraph. Bidi and script
// context survive a wrap; the returned coordinates begin at this line's left.
// The consumed end may include trailing collapsed ASCII spaces; Glyph.Cluster
// is relative to start, and an actual SHY break emits a separate visible glyph.
// This is cold preparation and owns no retained OS handle or paragraph cache.
func (p *referenceFontProvider) ShapeRange(paragraph string, start, end int, font ui.Font) referenceTextShape {
	if !utf8.ValidString(paragraph) || len(paragraph) > lineByteLimit {
		return referenceTextShape{Err: errReferenceAnnotationLimit}
	}
	shaper, err := newReferenceAnnotationShaper(paragraph, font, p)
	if err != nil {
		return referenceTextShape{Err: err}
	}
	defer shaper.close()
	return shaper.shape(start, end)
}

// referenceAnnotationRows implements CSS white-space:normal with the public
// UAX14 segmenter. Long unbreakable segments overflow; NBSP is not collapsed or
// an emergency break. A fixed row/work budget bounds pathological annotations.
func referenceAnnotationRows(text string, font ui.Font, width float32, duet bool, provider *referenceFontProvider, scales ...float32) ([]cssTextRow, float32, bool, error) {
	if !utf8.ValidString(text) || len(text) > lineByteLimit {
		return nil, 0, false, errReferenceAnnotationLimit
	}
	return referenceWrappedRows(referenceAnnotationWhitespace(text), nil, font, width, duet, provider, scales...)
}

// referenceStaticBodyRows preserves the original paragraph context across the
// balancing algorithm's explicit BR boundaries. Each hard interval may also
// wrap naturally. The caller has already collapsed CSS ASCII whitespace; ends
// are increasing half-open rune offsets in that exact paragraph, ending at EOF.
// A hard BR never activates a trailing SHY. No font or OS state is retained.
func referenceStaticBodyRows(paragraph string, hardBreakRuneEnds []int, font ui.Font, width float32, duet bool, provider *referenceFontProvider, scales ...float32) ([]cssTextRow, float32, bool, error) {
	return referenceWrappedRows(paragraph, hardBreakRuneEnds, font, width, duet, provider, scales...)
}

func referenceWrappedRows(paragraph string, hardBreakRuneEnds []int, font ui.Font, width float32, duet bool, provider *referenceFontProvider, scales ...float32) ([]cssTextRow, float32, bool, error) {
	if provider == nil {
		return nil, 0, false, errReferenceAnnotationContext
	}
	if !utf8.ValidString(paragraph) || len(paragraph) > lineByteLimit || math.IsNaN(float64(width)) || math.IsInf(float64(width), 0) || width < 0 {
		return nil, 0, false, errReferenceAnnotationLimit
	}
	if !finitePositive(font.Size) || !finitePositive(font.LineHeight) {
		return nil, 0, false, errReferenceAnnotationLimit
	}
	scale := float32(1)
	if len(scales) > 0 {
		scale = scales[0]
	}
	if !finitePositive(scale) {
		return nil, 0, false, errReferenceAnnotationLimit
	}
	hasDiscretionary := strings.ContainsRune(paragraph, '\u00ad')
	if paragraph == "" {
		return nil, 0, true, nil
	}
	shaper, err := newReferenceAnnotationShaper(paragraph, font, provider)
	if err != nil {
		return nil, 0, false, err
	}
	defer shaper.close()
	runes := []rune(paragraph)
	hard := hardBreakRuneEnds != nil
	if !hard {
		hardBreakRuneEnds = []int{len(runes)}
	}
	lastEnd := 0
	for _, end := range hardBreakRuneEnds {
		if end <= lastEnd || end > len(runes) {
			return nil, 0, false, errReferenceAnnotationContext
		}
		lastEnd = end
	}
	if lastEnd != len(runes) {
		return nil, 0, false, errReferenceAnnotationContext
	}
	var seg segmenter.Segmenter
	seg.Init(runes)
	iter := seg.LineIterator()
	ends := make([]int, 0, min(len(runes), lineWordLimit*2))
	for iter.Next() {
		part := iter.Line()
		last := part.Text[len(part.Text)-1]
		// Actual CSS normal wrapping softens LS/PS without changing their bidi
		// characters. PS can separate bidi paragraphs on one physical CSS row.
		if part.IsMandatoryBreak && part.Offset+len(part.Text) < len(runes) && last != '\u2028' && last != '\u2029' {
			return nil, 0, false, errReferenceAnnotationContext
		}
		ends = append(ends, part.Offset+len(part.Text))
	}
	if len(ends) == 0 || ends[len(ends)-1] != len(runes) {
		return nil, 0, false, errReferenceAnnotationContext
	}
	// Blink snaps shaped width upward and fits against one extra physical
	// layout unit. See ShapeResultView::SnappedWidth and
	// LineBreaker::AvailableWidthToFit at Chromium 1aba5922274bbff7e4a15411c48daa980e2cb31b.
	// The one-unit rule is source behavior, not a visual-error tolerance; it
	// also admits an exactly grid-aligned Latin width one unit below that width.
	unitsPerDIP := float64(scale) * 64
	available := math.Floor(cssPhysicalLayoutUnits(width, float64(scale))) + 1
	var rows []cssTextRow
	start, work, measurements := 0, 0, 0
	lineHeight := cssBlockLineHeight(font, scale)
	for _, hardEnd := range hardBreakRuneEnds {
		firstRow := len(rows)
		first := sort.SearchInts(ends, start+1)
		last := sort.SearchInts(ends, hardEnd)
		segmentEnds := ends[first:last:last]
		// A BR may cut inside an otherwise unbreakable UAX14 segment. Keep it
		// as the final candidate without making later candidates observable.
		segmentEnds = append(segmentEnds, hardEnd)
		for start < hardEnd {
			for start < hardEnd && runes[start] == ' ' {
				start++
			}
			if start == hardEnd {
				break
			}
			if len(rows) >= lineWordLimit*2 {
				return nil, 0, false, errReferenceAnnotationLimit
			}
			first := sort.SearchInts(segmentEnds, start+1)
			measure := func(at int, hyphen bool) (referenceTextShape, int, error) {
				end := segmentEnds[at]
				for end > start && runes[end-1] == ' ' {
					end--
				}
				work += end - start
				measurements++
				if work > lineByteLimit*32 || measurements > lineWordLimit*64 {
					return referenceTextShape{}, 0, errReferenceAnnotationLimit
				}
				shape := referenceTextShape{}
				if hyphen && !(hard && segmentEnds[at] == hardEnd) {
					shape = shaper.shape(start, segmentEnds[at])
				} else {
					shape = shaper.shapeUnbroken(start, segmentEnds[at])
				}
				if shape.Err != nil {
					return shape, 0, shape.Err
				}
				if !shape.Exact {
					return shape, 0, errReferenceAnnotationContext
				}
				return shape, end, nil
			}
			best, bestEnd, err := measure(first, false)
			if err != nil {
				return nil, 0, false, err
			}
			chosen := first
			if math.Ceil(float64(best.Width)*unitsPerDIP) <= available {
				// Exponential probing and binary search avoid repeatedly shaping
				// every growing prefix of a long annotation.
				lo, hi, step := first, len(segmentEnds), 1
				for probe := first + step; probe < len(segmentEnds); probe = first + step {
					candidate, end, err := measure(probe, false)
					if err != nil {
						return nil, 0, false, err
					}
					if math.Ceil(float64(candidate.Width)*unitsPerDIP) > available {
						hi = probe
						break
					}
					lo, chosen, best, bestEnd = probe, probe, candidate, end
					step *= 2
				}
				for lo+1 < hi {
					mid := lo + (hi-lo)/2
					candidate, end, err := measure(mid, false)
					if err != nil {
						return nil, 0, false, err
					}
					if math.Ceil(float64(candidate.Width)*unitsPerDIP) <= available {
						lo, chosen, best, bestEnd = mid, mid, candidate, end
					} else {
						hi = mid
					}
				}
			}
			// A visible discretionary hyphen may be wider than the text added by
			// the next candidate. Search natural widths first, then validate this
			// actual break and retreat under the same bounded work budget.
			for hasDiscretionary {
				best, bestEnd, err = measure(chosen, true)
				if err != nil {
					return nil, 0, false, err
				}
				if chosen == first || math.Ceil(float64(best.Width)*unitsPerDIP) <= available {
					break
				}
				chosen--
			}
			rowText := string(runes[start:bestEnd])
			layout := ui.ShapeText(rowText, font, 0)
			_, _, drawY := cssLineBox(layout, font, scale)
			x := float32(0)
			if duet {
				// Blink keeps overflowing unbreakable LTR lines at the start edge
				// even under text-align:right; it does not shift them negatively.
				x = max(0, width-best.Width)
			}
			paintEnd := segmentEnds[chosen]
			if hard && paintEnd == hardEnd {
				for paintEnd > start && (runes[paintEnd-1] == ' ' || runes[paintEnd-1] == '\u00ad') {
					paintEnd--
				}
			}
			rows = append(rows, cssTextRow{text: rowText, x: x, y: float32(len(rows)) * lineHeight, drawY: drawY, width: best.Width, context: paragraph, startRune: start, endRune: paintEnd})
			start = segmentEnds[chosen]
		}
		if hard && len(rows) == firstRow {
			if len(rows) >= lineWordLimit*2 {
				return nil, 0, false, errReferenceAnnotationLimit
			}
			_, _, drawY := cssLineBox(ui.ShapeText("", font, 0), font, scale)
			rows = append(rows, cssTextRow{y: float32(len(rows)) * lineHeight, drawY: drawY, context: paragraph, startRune: start, endRune: start})
		}
	}
	if len(rows) == 0 {
		return nil, 0, true, nil
	}
	return rows, lineHeight * float32(len(rows)), true, nil
}

func referenceAnnotationWhitespace(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	space, visible := false, false
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			space = visible
			continue
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		out.WriteRune(r)
		visible = true
	}
	return out.String()
}
