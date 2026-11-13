// SPDX-License-Identifier: AGPL-3.0-only
// Derived from AMLL f32285864056587c4afa59d310208b0aa829cb53:
// packages/core/src/utils/{lyric-split-words,lyric-line-break,line-balancer,is-cjk}.ts
// and packages/core/src/lyric-player/{base/line,dom/lyric-line}.ts.
// Adapted to bounded, retained Native geometry; no upstream lyric assets copied.

package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/segmenter"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// All coordinates below are unscaled DIPs, local to the line's content box.
// The retained layouts model the reference's actual inline boxes. Emphasized
// graphemes own independent layouts, as its separate DOM spans do; cutting a
// whole-line SelectionRects result is not the same rendering operation.
type wordBlock struct {
	bodyDrawY, romanDrawY        float32
	bodyRows, romanRows          []cssTextRow
	bidiFallback                 bool
	maskWidth, maskHeight        float32
	text                         string
	start, end                   time.Duration
	x, y, width, height          float32
	padding, fontSize            float32
	font, romanFont, rubyFont    ui.Font
	layout                       *ui.TextLayout
	characters                   []characterGeometry
	romanLayout, rubyLayout      *ui.TextLayout
	romanText                    string
	romanX, romanY, rubyX, rubyY float32
	bodyX, bodyY                 float32
	shouldEmphasize              bool
	emphasisGroup                int
	ruby                         []lyric.RubyWord
	rubyParts                    []rubyGeometry
	domText                      string
}

type rubyGeometry struct {
	drawY               float32
	rows                []cssTextRow
	text                string
	layout              *ui.TextLayout
	x, y, width, height float32
	font                ui.Font
	start, end          time.Duration
}

type characterGeometry struct {
	drawY               float32
	text                string
	layout              *ui.TextLayout
	x, y, width, height float32
	fontSize            float32
	font                ui.Font
	index               int // grapheme's index across its emphasis wrapper
}

type wordGroup struct {
	start, end                      time.Duration
	isLastWord                      bool
	characterCount, anchorCharCount int
}

type geometryStyle struct {
	// FontProvider is explicitly initialized by the host before cold layout.
	// It borrows read-only font data; it never opens fonts during Draw.
	FontProvider *referenceFontProvider
	// The reference Core has no per-line language property. Host language models
	// its inherited page lang; the historical diagnostic path keeps model language.
	UseHostLanguage bool
	// TextScale is the Native system device scale, distinct from browser-only
	// raster emulation. Zero keeps the historical 1x metric context.
	TextScale float32
	// BorderWidth is the resolved CSS lyricLine border width. Zero preserves
	// historical content-only geometry without claiming paint containment.
	BorderWidth float32
	// Dynamic is a document-wide choice, after normalization. The reference
	// considers a nonempty document non-dynamic iff every line has <= 1 word.
	Dynamic bool
	// BackgroundFontSize is the host-resolved CSS max(scale*base,10px), in
	// DIPs. Zero preserves the reference's default .7 scale for legacy callers.
	BackgroundFontSize float32
	// CSSPaddingSides accounts for the .lyricLine border-box padding left
	// after the scene removes the 15% duet-side padding: one side with duets,
	// two without. Legacy content-width callers leave zero.
	CSSPaddingSides int
}

type wordChunk struct {
	words []lyric.Word
	space bool
	text  string
}

// ECMAScript \s / String.trim, deliberately not Go unicode.IsSpace: U+FEFF
// is whitespace in the reference and U+0085 is not.
func referenceSpace(r rune) bool {
	return r >= '\t' && r <= '\r' || r == ' ' || r == 0x00a0 || r == 0x1680 ||
		r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 ||
		r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
}

func referenceTrim(text string) string { return strings.TrimFunc(text, referenceSpace) }

func utf16Count(text string) int {
	count := 0
	for _, r := range text {
		count += utf16.RuneLen(r)
	}
	return count
}

// This reproduces the reference's broad /^([\p{Unified_Ideograph}\u0800-\u9FFC])+$/u
// predicate, including scripts other than Han inside its explicit range.
func referenceCJK(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		unified := unicode.Properties["Unified_Ideograph"]
		if !(r >= 0x0800 && r <= 0x9ffc || unified != nil && unicode.Is(unified, r)) {
			return false
		}
	}
	return true
}

func referenceEmphasize(word lyric.Word) bool {
	if word.End-word.Start < time.Second {
		return false
	}
	if referenceCJK(word.Text) {
		return true
	}
	n := utf16Count(referenceTrim(word.Text))
	return n > 1 && n <= 7
}

func splitSpaceParts(text string) []string {
	var parts []string
	start, previousSpace := 0, false
	for at, r := range text {
		space := referenceSpace(r)
		if at != 0 && space != previousSpace {
			parts = append(parts, text[start:at])
			start = at
		}
		previousSpace = space
	}
	if start < len(text) {
		parts = append(parts, text[start:])
	}
	return parts
}

// referenceChunks preserves timed syllables while grouping adjacent non-CJK,
// non-ruby syllables into the same unbreakable emphasis wrapper. Splitting a
// word at spaces apportions time by UTF-16 length, exactly as the TS source.
func referenceChunks(input []lyric.Word) []wordChunk {
	var chunks []wordChunk
	var group []lyric.Word
	var groupUnits []uint16
	flush := func() {
		if len(group) == 0 {
			return
		}
		chunks = append(chunks, wordChunk{words: group, text: string(utf16.Decode(groupUnits))})
		group, groupUnits = nil, nil
	}
	atomWithUnits := func(word lyric.Word, units []uint16) {
		space := referenceTrim(word.Text) == ""
		if !space && len(word.Ruby) == 0 && !referenceCJK(word.Text) {
			group = append(group, word)
			groupUnits = append(groupUnits, units...)
			return
		}
		flush()
		chunks = append(chunks, wordChunk{words: []lyric.Word{word}, space: space, text: word.Text})
	}
	atom := func(word lyric.Word) { atomWithUnits(word, utf16.Encode([]rune(word.Text))) }
	for _, word := range input {
		content := referenceTrim(word.Text)
		if content == "" {
			atom(word)
			continue
		}
		if len(word.Ruby) > 0 {
			// TrimLeft/TrimRight locate only the edge runs, not internal spaces.
			left := word.Text[:len(word.Text)-len(strings.TrimLeftFunc(word.Text, referenceSpace))]
			right := word.Text[len(strings.TrimRightFunc(word.Text, referenceSpace)):]
			if left != "" {
				atom(lyric.Word{Text: left, Start: word.Start, End: word.Start})
			}
			word.Text = content
			atom(word)
			if right != "" {
				atom(lyric.Word{Text: right, Start: word.End, End: word.End})
			}
			continue
		}
		var total int
		for _, r := range word.Text {
			if !referenceSpace(r) {
				total += utf16.RuneLen(r)
			}
		}
		perUnit := float64(word.End-word.Start) / float64(max(total, 1))
		var romanParts []string
		for _, part := range splitSpaceParts(referenceTrim(word.Romanization)) {
			if referenceTrim(part) != "" {
				romanParts = append(romanParts, part)
			}
		}
		parts := splitSpaceParts(word.Text)
		wordCount := 0
		for _, part := range parts {
			if referenceTrim(part) != "" {
				wordCount++
			}
		}
		offset, nonSpaceIndex := 0, 0
		at := func(offset int) time.Duration { return word.Start + time.Duration(float64(offset)*perUnit) }
		for _, part := range parts {
			if referenceTrim(part) == "" {
				atom(lyric.Word{Text: part, Start: at(offset), End: at(offset)})
				continue
			}
			roman := ""
			if len(romanParts) == wordCount {
				roman = romanParts[nonSpaceIndex]
			} else if len(romanParts) != 0 && nonSpaceIndex == 0 {
				roman = word.Romanization
			}
			nonSpaceIndex++
			if referenceCJK(part) && utf16Count(part) > 1 && referenceTrim(word.Romanization) == "" {
				// JS split("") splits UTF-16 units, including surrogate pairs.
				// Isolated surrogate units display as replacement characters in
				// the reference; keep this observable rule rather than silently
				// substituting rune-based timing for supplementary ideographs.
				for _, unit := range utf16.Encode([]rune(part)) {
					text := string(utf16.Decode([]uint16{unit}))
					atomWithUnits(lyric.Word{Text: text, Start: at(offset), End: at(offset + 1)}, []uint16{unit})
					offset++
				}
			} else {
				n := utf16Count(part)
				atom(lyric.Word{Text: part, Romanization: roman, Start: at(offset), End: at(offset + n)})
				offset += n
			}
		}
	}
	flush()
	return chunks
}

type balancedChild struct {
	text  string
	width float64
	space bool
}

// referenceBalancedBreaks is the reference dynamic program, including loop
// order, strict tie-breaking, UTF-16 offsets, and squared boundary rewards.
// Segmentation is supplied separately so it can be compared against ICU
// without replacing its actual dictionary boundaries with native guesses.
func referenceBalancedBreaks(children []balancedChild, width float64, cjkBoundaries map[int]bool) []int {
	n := len(children)
	if n == 0 || width <= 0 {
		return nil
	}
	prefix := make([]float64, n+1)
	offsets := make([]int, n+1)
	for i, child := range children {
		prefix[i+1] = prefix[i] + child.width
		offsets[i+1] = offsets[i] + utf16Count(child.text)
	}
	if prefix[n] <= width {
		return nil
	}
	dp := make([]float64, n+1)
	next := make([]int, n+1)
	for i := range n {
		dp[i], next[i] = math.Inf(1), -1
	}
	for i := n - 1; i >= 0; i-- {
		for j := i + 1; j <= n; j++ {
			w := prefix[j] - prefix[i]
			cost := (width - w) * (width - w)
			if w > width {
				if j != i+1 {
					continue
				}
				cost *= 1000
			}
			if j < n {
				previous := children[j-1]
				switch {
				case referencePunctuation(previous.text):
					cost -= math.Pow(width*0.6, 2)
				case previous.space:
					cost -= math.Pow(width*0.4, 2)
				case cjkBoundaries[offsets[j]]:
					cost += math.Pow(width*0.15, 2)
				default:
					cost += math.Pow(width*0.5, 2)
				}
			}
			cost += dp[j]
			if cost < dp[i] {
				dp[i], next[i] = cost, j
			}
		}
	}
	var breaks []int
	for at := next[0]; at > 0 && at < n; at = next[at] {
		breaks = append(breaks, at)
	}
	return breaks
}

func referencePunctuation(text string) bool {
	r, _ := utf8.DecodeLastRuneInString(text)
	return strings.ContainsRune(",;:!?，。；：！？、）】》」』’”)[]>}~…", r)
}

type nativeWordSegment struct {
	start, end int // half-open UTF-16 offsets
	wordLike   bool
}

func referenceWordSegments(text string) ([]nativeWordSegment, bool) {
	if segments, ok := platformWordSegments(text); ok {
		return segments, false
	}
	// Explicit fallback: Unicode word boundaries, not Intl.Segmenter's
	// locale dictionary. Callers retain this fact with their geometry.
	runes := []rune(text)
	units := make([]int, len(runes)+1)
	for i, r := range runes {
		units[i+1] = units[i] + utf16.RuneLen(r)
	}
	var tokenizer segmenter.Segmenter
	tokenizer.Init(runes)
	iter := tokenizer.WordIterator()
	var words []nativeWordSegment
	for iter.Next() {
		word := iter.Word()
		words = append(words, nativeWordSegment{start: units[word.Offset], end: units[word.Offset+len(word.Text)], wordLike: true})
	}
	var result []nativeWordSegment
	at := 0
	appendGap := func(end int) {
		for at < end {
			start := at
			space := referenceSpace(runes[at])
			at++
			if space {
				for at < end && referenceSpace(runes[at]) {
					at++
				}
			}
			result = append(result, nativeWordSegment{start: units[start], end: units[at]})
		}
	}
	for _, word := range words {
		start, _ := slices.BinarySearch(units, word.start)
		end, _ := slices.BinarySearch(units, word.end)
		appendGap(start)
		result = append(result, word)
		at = end
	}
	appendGap(len(runes))
	return result, true
}

func referenceCJKBoundaries(text string) (map[int]bool, bool) {
	if boundaries, ok := platformCJKBoundaries(text); ok {
		return boundaries, false
	}
	segments, fallback := referenceWordSegments(text)
	return referenceCJKBoundariesFromSegments(text, segments), fallback
}

func referenceCJKBoundariesFromSegments(text string, segments []nativeWordSegment) map[int]bool {
	units := utf16.Encode([]rune(text))
	boundaries := make(map[int]bool)
	for _, segment := range segments {
		if segment.start == 0 || !segment.wordLike {
			continue
		}
		for _, r := range utf16.Decode(units[segment.start:segment.end]) {
			if referenceCJK(string(r)) {
				boundaries[segment.start] = true
				break
			}
		}
	}
	return boundaries
}

type referenceLayoutUnit struct {
	text                 string
	firstBlock, endBlock int
	width, height        float32
	space                bool
	spaceWidths          [4]float32 // untrimmed, row-start, row-end, both edges
	nonCollapsible       bool
}

// Count through read-only scalar access before Word can copy a Ruby slice.
// The raw model may be larger than a renderable line; rejecting it must not
// first allocate that oversized slice just to discover the geometry limit.
func preflightReferenceRuby(line lyric.LineView) error {
	count := 0
	bytes := len(line.Text()) + len(line.Translation()) + len(line.Romanization())
	for i := range line.WordCount() {
		count += line.RubyCount(i)
		if count > lineWordLimit {
			return errGeometryLimit
		}
		for j := range line.RubyCount(i) {
			ruby := line.Ruby(i, j)
			bytes += len(ruby.Text)
			if bytes > lineByteLimit {
				return errGeometryLimit
			}
			if !utf8.ValidString(ruby.Text) {
				return fmt.Errorf("native: word %d ruby %d is not valid UTF-8", i, j)
			}
		}
	}
	return nil
}

func (geometry *lineGeometry) buildReferenceBoxes(line lyric.LineView, font ui.Font, width float32, style geometryStyle) error {
	if !style.Dynamic {
		return geometry.buildNonDynamicBoxes(line, font, width)
	}
	input := make([]lyric.Word, 0, max(1, line.WordCount()))
	lastByte := byteOffset(0)
	lineHasRoman, lineHasRuby := false, false
	inputBytes := len(line.Text()) + len(line.Translation()) + len(line.Romanization())
	rubyCount := 0
	for i := range line.WordCount() {
		word := line.Word(i)
		mapped := geometry.words[i].text
		if mapped.startByte > lastByte {
			input = append(input, lyric.Word{Text: line.Text()[lastByte:mapped.startByte], Start: word.Start, End: word.Start})
		}
		lastByte = mapped.endByte
		lineHasRoman = lineHasRoman || referenceTrim(word.Romanization) != ""
		lineHasRuby = lineHasRuby || len(word.Ruby) != 0
		inputBytes += len(word.Romanization)
		if !utf8.ValidString(word.Romanization) {
			return fmt.Errorf("native: word %d romanization is not valid UTF-8", i)
		}
		rubyCount += len(word.Ruby)
		if rubyCount > lineWordLimit {
			return errGeometryLimit
		}
		for j, ruby := range word.Ruby {
			inputBytes += len(ruby.Text)
			if !utf8.ValidString(ruby.Text) {
				return fmt.Errorf("native: word %d ruby %d is not valid UTF-8", i, j)
			}
		}
		if inputBytes > lineByteLimit {
			return errGeometryLimit
		}
		input = append(input, word)
	}
	if line.WordCount() == 0 {
		input = append(input, lyric.Word{Text: line.Text(), Start: line.Start(), End: line.End()})
	} else if int(lastByte) < len(line.Text()) {
		input = append(input, lyric.Word{Text: line.Text()[lastByte:], Start: line.End(), End: line.End()})
	}
	chunks := referenceChunks(input)
	count := 0
	for _, chunk := range chunks {
		if !chunk.space {
			count += len(chunk.words)
		}
	}
	if count > lineWordLimit || len(chunks) > lineWordLimit*2+1 {
		return errGeometryLimit
	}
	spaceLayout := ui.ShapeText(" ", font, 0)
	spaceWidth, _ := spaceLayout.Size()
	baseHeight := cssInlineLineHeight(font, true, geometry.textScale)
	spaceMeasurements := map[string]float32{"": 0, " ": spaceWidth}
	measureSpace := func(text string) float32 {
		if value, ok := spaceMeasurements[text]; ok {
			return value
		}
		layout := ui.ShapeText(text, font, 0)
		value, _ := layout.Size()
		spaceMeasurements[text] = value
		return value
	}
	var units []referenceLayoutUnit
	originalLast := ""
	if line.WordCount() != 0 {
		originalLast = line.Word(line.WordCount() - 1).Text
	}
	for i, chunk := range chunks {
		unit := referenceLayoutUnit{text: chunk.text, firstBlock: len(geometry.blocks), space: chunk.space}
		if chunk.space {
			collapsed := collapseHTMLWhitespace(chunk.text)
			spaceTexts := [4]string{collapsed, strings.TrimLeft(collapsed, " \t\n\r\f"), strings.TrimRight(collapsed, " \t\n\r\f"), strings.Trim(collapsed, " \t\n\r\f")}
			for j, text := range spaceTexts {
				unit.spaceWidths[j] = measureSpace(text)
			}
			unit.nonCollapsible = spaceTexts[3] != ""
			unit.width = unit.spaceWidths[rowEdgeIndex(i == 0, i == len(chunks)-1)]
			unit.height = baseHeight
			unit.endBlock = unit.firstBlock
			units = append(units, unit)
			continue
		}
		merged := lyric.Word{Text: chunk.text, Start: chunk.words[0].Start, End: chunk.words[0].End}
		for _, word := range chunk.words {
			merged.Start, merged.End = min(merged.Start, word.Start), max(merged.End, word.End)
		}
		emphasize := referenceEmphasize(merged)
		if referenceCJK(merged.Text) {
			emphasize = slices.ContainsFunc(chunk.words, referenceEmphasize)
		}
		group := wordGroup{start: merged.Start, end: merged.End, isLastWord: line.WordCount() > 0 && strings.Contains(merged.Text, originalLast)}
		groupIndex := len(geometry.groups)
		var domText strings.Builder
		for _, word := range chunk.words {
			block := buildReferenceWordAtScale(word, font, lineHasRoman, lineHasRuby, emphasize, groupIndex, geometry.fontProvider, geometry.textScale)
			for i := range block.characters {
				block.characters[i].index += group.characterCount
			}
			group.characterCount += len(block.characters)
			for _, ruby := range word.Ruby {
				group.anchorCharCount += utf16Count(ruby.Text)
			}
			block.x = unit.width
			unit.width += block.width
			unit.height = max(unit.height, block.height)
			domText.WriteString(block.domText)
			geometry.bytes += estimateWordBlockBytes(&block)
			if geometry.bytes > geometryByteLimit {
				return errGeometryLimit
			}
			geometry.blocks = append(geometry.blocks, block)
		}
		if group.anchorCharCount == 0 {
			group.anchorCharCount = max(1, group.characterCount)
		}
		geometry.groups = append(geometry.groups, group)
		unit.text, unit.endBlock = domText.String(), len(geometry.blocks)
		if !lineHasRoman && !slices.ContainsFunc(chunk.words, func(word lyric.Word) bool { return len(word.Ruby) > 0 }) {
			// textContent joins JS surrogate halves across adjacent span nodes.
			// Their individual layouts still display isolated replacements.
			unit.text = chunk.text
		}
		units = append(units, unit)
	}
	geometry.positionReferenceUnits(line, units, width, baseHeight)
	return nil
}

func buildReferenceWord(word lyric.Word, font ui.Font, lineHasRoman, lineHasRuby, emphasize bool, groupIndex int, providers ...*referenceFontProvider) wordBlock {
	var provider *referenceFontProvider
	if len(providers) > 0 {
		provider = providers[0]
	}
	return buildReferenceWordAtScale(word, font, lineHasRoman, lineHasRuby, emphasize, groupIndex, provider, 1)
}

func buildReferenceWordAtScale(word lyric.Word, font ui.Font, lineHasRoman, lineHasRuby, emphasize bool, groupIndex int, provider *referenceFontProvider, scale float32) wordBlock {
	text := referenceTrim(word.Text)
	if lineHasRuby {
		text = collapseHTMLWhitespace(text)
	}
	block := wordBlock{
		text: text, start: word.Start, end: word.End, font: font,
		fontSize: font.Size, padding: font.Size, shouldEmphasize: emphasize,
		emphasisGroup: groupIndex, ruby: word.Ruby,
	}
	block.layout = ui.ShapeText(text, font, 0)
	bodyWidth, bodyHeight, bodyDrawY := cssInlineLineBox(block.layout, font, true, scale)
	measuredBodyWidth, bodyExact := referenceFlowWidth(text, block.layout, font, provider)
	bodyWidth = measuredBodyWidth
	block.bidiFallback = !bodyExact && !emphasize
	block.bodyDrawY = bodyDrawY
	block.bodyRows = cssTextRowsAtScale(text, block.layout, font, scale, true)
	if emphasize {
		units := utf16.Encode([]rune(text))
		for start := 0; start < len(units); {
			end := block.layout.NextBoundary(start)
			if end <= start {
				break
			}
			charText := string(utf16.Decode(units[start:end]))
			layout := ui.ShapeText(charText, font, 0)
			w, h, drawY := cssInlineLineBox(layout, font, true, scale)
			w, exact := referenceFlowWidth(charText, layout, font, provider)
			block.bidiFallback = block.bidiFallback || !exact
			block.characters = append(block.characters, characterGeometry{text: charText, layout: layout, x: block.width, width: w, height: h, drawY: drawY, fontSize: font.Size, font: font, index: len(block.characters)})
			block.width += w
			bodyHeight = max(bodyHeight, h)
			start = end
		}
		bodyWidth = block.width
	}
	block.width, block.height = bodyWidth, bodyHeight
	block.romanFont, block.rubyFont = font, font
	block.romanFont.Size, block.rubyFont.Size = font.Size*0.5, font.Size*0.5
	block.romanFont.LineHeight, block.rubyFont.LineHeight = 1, 1
	romanText := ""
	if lineHasRoman {
		romanText = referenceTrim(word.Romanization)
		if romanText == "" {
			romanText = "\u00a0"
		}
		block.romanLayout = ui.ShapeText(romanText, block.romanFont, 0)
		w, h, drawY := cssInlineLineBox(block.romanLayout, block.romanFont, false, scale)
		w, exact := referenceFlowWidth(romanText, block.romanLayout, block.romanFont, provider)
		block.bidiFallback = block.bidiFallback || !exact
		block.romanDrawY = drawY
		block.romanRows = cssTextRowsAtScale(romanText, block.romanLayout, block.romanFont, scale, false)
		block.width = max(block.width, w+block.romanFont.Size*0.3)
		block.romanY = bodyHeight
		block.height += h
	}
	block.romanText = romanText
	rubyText := ""
	if lineHasRuby {
		var rubyWidth, rubyHeight float32
		for _, ruby := range word.Ruby {
			if referenceTrim(ruby.Text) == "" {
				continue
			}
			layout := ui.ShapeText(ruby.Text, block.rubyFont, 0)
			w, h, drawY := cssInlineLineBox(layout, block.rubyFont, false, scale)
			w, exact := referenceFlowWidth(ruby.Text, layout, block.rubyFont, provider)
			block.bidiFallback = block.bidiFallback || !exact
			block.rubyParts = append(block.rubyParts, rubyGeometry{text: ruby.Text, layout: layout, rows: cssTextRowsAtScale(ruby.Text, layout, block.rubyFont, scale, false), x: rubyWidth, width: w, height: h, drawY: drawY, font: block.rubyFont, start: ruby.Start, end: ruby.End})
			rubyWidth += w
			rubyHeight = max(rubyHeight, h)
			rubyText += ruby.Text
		}
		rubyHeight = max(rubyHeight, cssLayoutLength(block.rubyFont.Size, scale)) // CSS min-height: 1em
		wordBodyWidth := block.width
		block.width = max(block.width, rubyWidth)
		block.bodyX = (block.width - bodyWidth) / 2
		block.romanX = (block.width - wordBodyWidth) / 2
		block.bodyY, block.romanY = rubyHeight, block.romanY+rubyHeight
		block.height += rubyHeight
		block.rubyX = (block.width - rubyWidth) / 2
		for i := range block.rubyParts {
			block.rubyParts[i].x += block.rubyX
		}
		if rubyText != "" {
			// Compatibility only: the effect backend should draw rubyParts,
			// whose independent spans preserve the measured fragment widths.
			block.rubyLayout = ui.ShapeText(rubyText, block.rubyFont, 0)
		}
	}
	for i := range block.characters {
		block.characters[i].x += block.bodyX
		block.characters[i].y += block.bodyY
	}
	block.domText = rubyText + referenceTrim(word.Text) + romanText
	// The reference measures mask dimensions with integer clientWidth/Height,
	// while line balancing measures fractional wrapper bounding boxes.
	block.maskWidth = float32(math.Round(float64(block.width+block.padding*2))) - block.padding*2
	block.maskHeight = float32(math.Round(float64(block.height+block.padding*2))) - block.padding*2
	return block
}

func collapseHTMLWhitespace(text string) string {
	var out strings.Builder
	space := false
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '\f' {
			if !space {
				out.WriteByte(' ')
			}
			space = true
		} else {
			out.WriteRune(r)
			space = false
		}
	}
	return out.String()
}

// CSS permits negative half-leading when line-height is less than the font's
// natural metrics; mygo retains the natural minimum. Keep the CSS box size
// and offset only its glyph drawing, so transform origins and Roman/Ruby
// slot heights remain those of the reference instead of stretching the text.
func cssLineBox(layout *ui.TextLayout, font ui.Font, scales ...float32) (width, height, drawY float32) {
	return cssBlockLineBox(layout, font, scales...)
}

// cssTextRow retains independent wrapped/explicit rows for cold atlas raster.
// A single y offset cannot compress native minimum line leading to CSS leading
// on every row, especially Ruby's 1em line-height. TextLine indices are UTF-16.
type cssTextRow struct {
	text        string
	x, y, drawY float32
	width       float32 // physical row's logical advance, in DIP
	// Wrapped block text inherits its paragraph's script and bidi context.
	// An empty context keeps the independent span behavior used by words/Ruby.
	context            string
	startRune, endRune int // half-open range in context, never UTF-16 or bytes
}

func cssTextRows(text string, layout *ui.TextLayout, font ui.Font, inline ...bool) []cssTextRow {
	return cssTextRowsAtScale(text, layout, font, 1, inline...)
}

func cssTextRowsAtScale(text string, layout *ui.TextLayout, font ui.Font, scale float32, inline ...bool) []cssTextRow {
	lines := layout.Lines()
	if len(lines) <= 1 {
		return nil
	}
	units := utf16.Encode([]rune(text))
	_, height, _ := cssLineBox(layout, font, scale)
	if len(inline) > 0 {
		_, height, _ = cssInlineLineBox(layout, font, inline[0], scale)
	}
	lineHeight := height / float32(len(lines))
	rows := make([]cssTextRow, 0, len(lines))
	for i, line := range lines {
		rowText := string(utf16.Decode(units[line.Start:line.End]))
		rowLayout := ui.ShapeText(rowText, font, 0)
		_, _, drawY := cssLineBox(rowLayout, font, scale)
		if len(inline) > 0 {
			_, _, drawY = cssInlineLineBox(rowLayout, font, inline[0], scale)
		}
		// The reference word spans inherit direction:ltr and text-align:start.
		// Native first-strong paragraph alignment must not shift shorter RTL rows.
		rows = append(rows, cssTextRow{text: rowText, y: float32(i) * lineHeight, drawY: drawY})
	}
	return rows
}

func referenceFlowWidth(text string, layout *ui.TextLayout, font ui.Font, providers ...*referenceFontProvider) (float32, bool) {
	measure := func(text string) (float32, bool) {
		if len(providers) > 0 && providers[0] != nil {
			shape := providers[0].Shape(text, font)
			return shape.Width, shape.Exact && shape.Err == nil
		}
		_, width, exact := referenceTextGlyphs(text, font)
		return width, exact
	}
	lines := layout.Lines()
	if len(lines) <= 1 {
		return measure(text)
	}
	units := utf16.Encode([]rune(text))
	var width float32
	exact := true
	for _, line := range lines {
		rowWidth, rowExact := measure(string(utf16.Decode(units[line.Start:line.End])))
		width, exact = max(width, rowWidth), exact && rowExact
	}
	return width, exact
}

func cssAnnotationRows(text string, layout *ui.TextLayout, font ui.Font, width float32, duet bool, providers ...*referenceFontProvider) ([]cssTextRow, bool) {
	return cssAnnotationRowsAtScale(text, layout, font, width, duet, 1, providers...)
}

func cssAnnotationRowsAtScale(text string, layout *ui.TextLayout, font ui.Font, width float32, duet bool, scale float32, providers ...*referenceFontProvider) ([]cssTextRow, bool) {
	lines := layout.Lines()
	units := utf16.Encode([]rune(text))
	_, height, _ := cssLineBox(layout, font, scale)
	lineHeight := height / float32(max(1, len(lines)))
	rows := make([]cssTextRow, 0, len(lines))
	exact := true
	for i, line := range lines {
		rowText := string(utf16.Decode(units[line.Start:line.End]))
		rowLayout := ui.ShapeText(rowText, font, 0)
		rowWidth, rowExact := referenceFlowWidth(rowText, rowLayout, font, providers...)
		_, _, drawY := cssLineBox(rowLayout, font, scale)
		x := float32(0)
		if duet {
			x = width - rowWidth
		}
		rows = append(rows, cssTextRow{text: rowText, x: x, y: float32(i) * lineHeight, drawY: drawY})
		exact = exact && rowExact
	}
	return rows, exact
}

func (geometry *lineGeometry) positionReferenceUnits(line lyric.LineView, units []referenceLayoutUnit, width, baseHeight float32) {
	if len(geometry.blocks) == 0 && !slices.ContainsFunc(units, func(unit referenceLayoutUnit) bool { return unit.nonCollapsible }) {
		geometry.width = width
		if line.Background() {
			geometry.bodyHeight = 2 * cssBackgroundMainInset(geometry.font.Size, geometry.textScale)
		}
		return
	}
	children := make([]balancedChild, len(units))
	var text strings.Builder
	for i, unit := range units {
		children[i] = balancedChild{text: unit.text, width: float64(unit.width), space: unit.space}
		text.WriteString(unit.text)
	}
	boundaries, fallback := referenceCJKBoundaries(text.String())
	geometry.segmenterFallback = fallback
	breaks := referenceBalancedBreaks(children, float64(width), boundaries)
	breaks = append(breaks, len(units))
	var y float32
	if line.Background() {
		y = cssBackgroundMainInset(geometry.font.Size, geometry.textScale)
	}
	start := 0
	for _, end := range breaks {
		rowWidth, rowHeight := float32(0), baseHeight
		for i := start; i < end; i++ {
			rowHeight = max(rowHeight, units[i].height)
			rowWidth += units[i].flowWidth(i == start, i == end-1)
		}
		x := float32(0)
		if line.Duet() {
			x = max(0, width-rowWidth)
		}
		for i := start; i < end; i++ {
			unit := units[i]
			for j := unit.firstBlock; j < unit.endBlock; j++ {
				block := &geometry.blocks[j]
				block.x += x
				block.y = y + rowHeight - block.height
			}
			x += unit.flowWidth(i == start, i == end-1)
		}
		y += rowHeight
		start = end
	}
	if len(units) == 0 {
		y += baseHeight
	}
	if line.Background() {
		y += cssBackgroundMainInset(geometry.font.Size, geometry.textScale)
	}
	geometry.bodyHeight, geometry.width = y, width
}

func rowEdgeIndex(first, last bool) int {
	index := 0
	if first {
		index++
	}
	if last {
		index += 2
	}
	return index
}

func (unit referenceLayoutUnit) flowWidth(first, last bool) float32 {
	if unit.space {
		return unit.spaceWidths[rowEdgeIndex(first, last)]
	}
	return unit.width
}

func (geometry *lineGeometry) buildNonDynamicBoxes(line lyric.LineView, font ui.Font, width float32) error {
	text := strings.Trim(collapseHTMLWhitespace(line.Text()), " \t\n\r\f")
	if text == "" {
		geometry.width = width
		if line.Background() {
			geometry.bodyHeight = 2 * cssBackgroundMainInset(font.Size, geometry.textScale)
		}
		return nil
	}
	segments, fallback := referenceWordSegments(text)
	geometry.segmenterFallback = fallback
	units := utf16.Encode([]rune(text))
	if len(segments) > lineWordLimit*2+1 {
		return errGeometryLimit
	}
	children := make([]balancedChild, len(segments))
	var total float64
	for i, segment := range segments {
		part := string(utf16.Decode(units[segment.start:segment.end]))
		layout := ui.ShapeText(part, font, 0)
		w, _ := referenceFlowWidth(part, layout, font, geometry.fontProvider)
		children[i] = balancedChild{text: part, width: float64(w), space: referenceTrim(part) == ""}
		total += float64(w)
	}
	whole := ui.ShapeText(text, font, 0)
	wholeWidth, _ := referenceFlowWidth(text, whole, font, geometry.fontProvider)
	_, baseHeight, _ := cssLineBox(ui.ShapeText("M", font, 0), font, geometry.textScale)
	if total > 0 && wholeWidth > 0 {
		for i := range children {
			children[i].width *= float64(wholeWidth) / total
		}
	}
	boundaries := referenceCJKBoundariesFromSegments(text, segments)
	breaks := referenceBalancedBreaks(children, float64(width), boundaries)
	breaks = append(breaks, len(children))
	if geometry.fontProvider != nil {
		// Keep the original balancer's hard BR partition. Each hard range can
		// still wrap naturally at SHY, and both kinds of line retain the full
		// paragraph's script and bidi context rather than shaping in isolation.
		hardEnds := make([]int, 0, len(breaks))
		child, runeEnd := 0, 0
		for _, end := range breaks {
			for ; child < end; child++ {
				runeEnd += utf8.RuneCountInString(children[child].text)
			}
			hardEnds = append(hardEnds, runeEnd)
		}
		rows, height, exact, err := referenceStaticBodyRows(text, hardEnds, font, width, line.Duet(), geometry.fontProvider, geometry.textScale)
		if err != nil {
			return err
		}
		inset := float32(0)
		if line.Background() {
			inset = cssBackgroundMainInset(font.Size, geometry.textScale)
		}
		for _, row := range rows {
			localRow := row
			localRow.x, localRow.y = 0, 0
			block := wordBlock{
				text: row.text, start: line.Start(), end: line.End(),
				x: row.x, y: inset + row.y, width: row.width, height: baseHeight,
				bodyRows: []cssTextRow{localRow}, bodyDrawY: row.drawY,
				font: font, fontSize: font.Size, layout: ui.ShapeText(row.text, font, 0),
				emphasisGroup: -1, bidiFallback: !exact,
			}
			geometry.blocks = append(geometry.blocks, block)
			geometry.bytes += estimateWordBlockBytes(&block)
			if geometry.bytes > geometryByteLimit {
				return errGeometryLimit
			}
		}
		geometry.bidiFallback = geometry.bidiFallback || !exact
		geometry.bodyHeight, geometry.width = height+2*inset, width
		return nil
	}
	start := 0
	var y float32
	if line.Background() {
		y = cssBackgroundMainInset(font.Size, geometry.textScale)
	}
	for _, end := range breaks {
		var row strings.Builder
		for i := start; i < end; i++ {
			row.WriteString(children[i].text)
		}
		rowText := strings.Trim(row.String(), " \t\n\r\f")
		layout := ui.ShapeText(rowText, font, 0)
		w, h, drawY := cssLineBox(layout, font, geometry.textScale)
		w, exact := referenceFlowWidth(rowText, layout, font, geometry.fontProvider)
		x := float32(0)
		if line.Duet() {
			x = max(0, width-w)
		}
		block := wordBlock{text: rowText, start: line.Start(), end: line.End(), x: x, y: y, width: w, height: max(h, baseHeight), bodyDrawY: drawY, font: font, fontSize: font.Size, layout: layout, emphasisGroup: -1, bidiFallback: !exact}
		geometry.blocks = append(geometry.blocks, block)
		geometry.bytes += estimateWordBlockBytes(&block)
		if geometry.bytes > geometryByteLimit {
			return errGeometryLimit
		}
		y += block.height
		start = end
	}
	if line.Background() {
		y += cssBackgroundMainInset(font.Size, geometry.textScale)
	}
	geometry.bodyHeight, geometry.width = y, width
	return nil
}

func estimateWordBlockBytes(block *wordBlock) int {
	bytes := 4096 + len(block.text)*8 + utf8.RuneCountInString(block.text)*384 + 512 + cap(block.ruby)*64
	bytes += (cap(block.bodyRows) + cap(block.romanRows)) * 64
	for _, character := range block.characters {
		bytes += 4096 + len(character.text)*8 + utf8.RuneCountInString(character.text)*384 + 256
	}
	if block.romanLayout != nil {
		bytes += 4096 + len(block.domText)*8 + utf8.RuneCountInString(block.domText)*384
	}
	for _, ruby := range block.rubyParts {
		bytes += 4096 + len(ruby.text)*8 + utf8.RuneCountInString(ruby.text)*384 + 128
		bytes += cap(ruby.rows) * 64
	}
	if block.rubyLayout != nil {
		bytes += 4096 + len(block.domText)*8 + utf8.RuneCountInString(block.domText)*384
	}
	return bytes
}
