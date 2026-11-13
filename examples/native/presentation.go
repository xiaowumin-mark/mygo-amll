// SPDX-License-Identifier: AGPL-3.0-only
// Presentation normalization follows AMLL f32285864056587c4afa59d310208b0aa829cb53,
// packages/core/src/utils/optimize-lyric.ts and packages/ttml/src/parser.ts
// background-vocal sanitization. This Go adapter preserves the reference rules;
// the immutable source document and its format timestamps are not changed.
package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

type presentationGroup struct {
	main, background             int // original document indices
	mainLayout, backgroundLayout int // retained presentation document indices
	start, end                   time.Duration
	backgroundFirst              bool
}

type presentation struct {
	document     *lyric.Document // safe geometry snapshots; words keep source times
	timeline     *lyric.Document // group timing only; may clip past word ends
	groups       []presentationGroup
	groupForLine []int
	indexToGroup []int
	lineTimes    []presentationLineTime // processed timing, before strict geometry bounds expand
	dynamic      bool
	hasDuet      bool
}

func snapshotLine(v lyric.LineView) lyric.Line {
	line := lyric.Line{ID: v.ID(), ParentID: v.ParentID(), Text: v.Text(), Translation: v.Translation(), Romanization: v.Romanization(), Start: v.Start(), End: v.End(), Duet: v.Duet(), Background: v.Background(), Agent: v.Agent(), Language: v.Language()}
	line.Words = make([]lyric.Word, v.WordCount())
	for j := range line.Words {
		line.Words[j] = v.Word(j)
	}
	return line
}

// Restore only separator whitespace that the XML paragraph retained outside its
// timed spans. Never search past non-whitespace to guess a word's text mapping.
func restoreTimedSpaces(line *lyric.Line) bool {
	if len(line.Words) == 0 || line.Text == "" {
		return true
	}
	words := append([]lyric.Word(nil), line.Words...)
	at := 0
	for i := range words {
		start := at
		for !strings.HasPrefix(line.Text[at:], words[i].Text) && at < len(line.Text) {
			r, size := utf8.DecodeRuneInString(line.Text[at:])
			if !referenceSpace(r) {
				return false
			}
			at += size
		}
		if !strings.HasPrefix(line.Text[at:], words[i].Text) {
			return false
		}
		if i > 0 {
			words[i-1].Text += line.Text[start:at]
		}
		at += len(words[i].Text)
	}
	if referenceTrim(line.Text[at:]) != "" {
		return false
	}
	words[len(words)-1].Text += line.Text[at:]
	line.Words = words
	return true
}

// The reference TTML adapter strips background parentheses before the core
// optimizer runs, including backgrounds later promoted to ordinary main lines.
func stripPresentationBackground(line *lyric.Line) {
	line.Text = strings.TrimRight(strings.TrimLeft(line.Text, "(（"), ")）")
	if len(line.Words) == 0 {
		return
	}
	line.Words[0].Text = strings.TrimLeftFunc(strings.TrimLeft(line.Words[0].Text, "(（"), referenceSpace)
	last := len(line.Words) - 1
	line.Words[last].Text = strings.TrimRightFunc(strings.TrimRight(line.Words[last].Text, ")）"), referenceSpace)
}

func normalizePresentationSpaces(text string) string {
	var out strings.Builder
	space := false
	for _, r := range text {
		if referenceSpace(r) {
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

func newPresentation(source *lyric.Document) (*presentation, error) {
	return newPresentationWithOptions(source, presentationOptions{})
}

func newPresentationWithOptions(source *lyric.Document, options presentationOptions) (*presentation, error) {
	if source == nil {
		return nil, fmt.Errorf("native presentation: nil source document")
	}
	config, err := resolvePresentationOptions(options)
	if err != nil {
		return nil, err
	}
	p := &presentation{groupForLine: make([]int, source.Len()), dynamic: source.Len() == 0}
	lines := make([]lyric.Line, source.Len())
	ids := make(map[string]int, source.Len())
	for i := range lines {
		lines[i] = snapshotLine(source.Line(i))
		ids[lines[i].ID] = i
		if !restoreTimedSpaces(&lines[i]) {
			return nil, fmt.Errorf("native presentation: line %q has non-whitespace text outside its timed words", lines[i].ID)
		}
		if lines[i].Background {
			stripPresentationBackground(&lines[i])
		}
		if config.normalize {
			for j := range lines[i].Words {
				lines[i].Words[j].Text = normalizePresentationSpaces(lines[i].Words[j].Text)
			}
		}
		if config.reset && len(lines[i].Words) > 0 {
			lines[i].Start = lines[i].Words[0].Start
			lines[i].End = lines[i].Words[len(lines[i].Words)-1].End
		}
		p.groupForLine[i] = -1
		p.dynamic = p.dynamic || len(lines[i].Words) > 1
		p.hasDuet = p.hasDuet || lines[i].Duet
	}
	// Explicit parent identities survive the format layer's chronological sort.
	// Unparented inputs retain the reference's adjacent-background convention.
	for i, line := range lines {
		if !line.Background {
			p.groupForLine[i] = len(p.groups)
			p.groups = append(p.groups, presentationGroup{main: i, background: -1, backgroundLayout: -1})
		}
	}
	lastMain, consecutive := -1, 0
	for i := range lines {
		line := &lines[i]
		if !line.Background {
			lastMain, consecutive = i, 0
			continue
		}
		consecutive++
		parent := lastMain
		if line.ParentID != "" {
			parent = ids[line.ParentID]
		} else if i == 0 || consecutive > 1 {
			parent = -1
		}
		group := -1
		if parent >= 0 {
			group = p.groupForLine[parent]
		}
		if group < 0 || p.groups[group].background >= 0 {
			line.Background, line.ParentID = false, ""
			p.groupForLine[i] = len(p.groups)
			p.groups = append(p.groups, presentationGroup{main: i, background: -1, backgroundLayout: -1})
			continue
		}
		p.groups[group].background = i
		p.groupForLine[i] = group
	}
	for i := range p.groups {
		g := &p.groups[i]
		main := &lines[g.main]
		g.start, g.end = main.Start, main.End
		if g.background >= 0 {
			bg := &lines[g.background]
			bg.ParentID = main.ID
			bg.Duet = bg.Duet || main.Duet
			if config.syncBackground {
				g.start, g.end = min(main.Start, bg.Start), max(main.End, bg.End)
				for _, line := range []*lyric.Line{main, bg} {
					for _, word := range line.Words {
						if referenceTrim(word.Text) != "" {
							g.start, g.end = min(g.start, word.Start), max(g.end, word.End)
						}
					}
				}
			}
		}
	}
	slices.SortStableFunc(p.groups, func(a, b presentationGroup) int {
		if a.start < b.start {
			return -1
		}
		if a.start > b.start {
			return 1
		}
		return a.main - b.main
	})
	// Preserve the reference's cleanup rule, including intentional long overlaps.
	if config.clean {
		for i := range p.groups {
			for j := i + 1; j < len(p.groups); j++ {
				overlap := p.groups[i].end - p.groups[j].start
				if overlap <= 0 {
					break
				}
				intentional := overlap >= 500*time.Millisecond || overlap > 100*time.Millisecond && float64(overlap) > .1*float64(p.groups[j].end-p.groups[j].start)
				if !intentional {
					p.groups[i].end = p.groups[j].start
					break
				}
			}
		}
	}
	if config.advance {
		var previousStart, previousEnd, cohortStart, cohortEnd time.Duration
		for i := range p.groups {
			g := &p.groups[i]
			originalStart, originalEnd := g.start, g.end
			advance, boundary := 600*time.Millisecond, time.Duration(0)
			if i > 0 {
				if originalStart >= previousEnd {
					boundary = cohortEnd
				} else {
					advance, boundary = 400*time.Millisecond, previousStart
					if overlap := previousEnd - originalStart; overlap < advance {
						advance = time.Duration(float64(overlap) * .7)
					}
				}
			}
			if start := max(boundary, g.start-advance); start < g.start {
				g.start = start
			}
			if i > 0 && originalStart < cohortEnd && originalEnd > cohortStart {
				cohortStart, cohortEnd = min(cohortStart, originalStart), max(cohortEnd, originalEnd)
			} else {
				cohortStart, cohortEnd = originalStart, originalEnd
			}
			previousStart, previousEnd = g.start, originalEnd
		}
	}
	processedTimes := make([]presentationLineTime, len(lines))
	for i := range lines {
		processedTimes[i] = presentationLineTime{start: lines[i].Start, end: lines[i].End}
	}
	var timeLines []lyric.Line
	for i, g := range p.groups {
		processedTimes[g.main] = presentationLineTime{start: g.start, end: g.end}
		if g.background >= 0 && config.syncBackground {
			processedTimes[g.background] = processedTimes[g.main]
		}
		if g.background >= 0 {
			// The DOM group makes this decision after optimization. Lines without
			// words fall back to the shared, already advanced group start.
			mainFirst, bgFirst := g.start, processedTimes[g.background].start
			if words := lines[g.main].Words; len(words) > 0 {
				mainFirst = words[0].Start
			}
			if words := lines[g.background].Words; len(words) > 0 {
				bgFirst = words[0].Start
			}
			p.groups[i].backgroundFirst = bgFirst < mainFirst
		}
		p.groupForLine[g.main] = i
		if g.background >= 0 {
			p.groupForLine[g.background] = i
		}
		if g.end > g.start {
			timeLines = append(timeLines, lyric.Line{ID: lines[g.main].ID, Start: g.start, End: g.end})
			p.indexToGroup = append(p.indexToGroup, i)
		}
		for _, sourceIndex := range []int{g.main, g.background} {
			if sourceIndex < 0 {
				continue
			}
			line := &lines[sourceIndex]
			processed := processedTimes[sourceIndex]
			line.Start, line.End = processed.start, max(processed.end, processed.start+time.Nanosecond)
			var text strings.Builder
			for j := range line.Words {
				word := &line.Words[j]
				if word.Obscene && config.maskMode != presentationMaskDisabled {
					word.Text = maskPresentationText(word.Text, config)
				}
				line.Start, line.End = min(line.Start, word.Start), max(line.End, word.End)
				text.WriteString(word.Text)
			}
			if len(line.Words) > 0 {
				line.Text = text.String()
			}
		}
	}
	slices.SortStableFunc(lines, func(a, b lyric.Line) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return 0
	})
	p.document, err = lyric.NewDocument(lines)
	if err != nil {
		return nil, fmt.Errorf("native presentation geometry: %w", err)
	}
	p.lineTimes = make([]presentationLineTime, p.document.Len())
	for i := 0; i < p.document.Len(); i++ {
		v := p.document.Line(i)
		sourceIndex := ids[v.ID()]
		p.lineTimes[i] = processedTimes[sourceIndex]
		g := &p.groups[p.groupForLine[sourceIndex]]
		if sourceIndex == g.main {
			g.mainLayout = i
		} else {
			g.backgroundLayout = i
		}
	}
	p.timeline, err = lyric.NewDocument(timeLines)
	if err != nil {
		return nil, fmt.Errorf("native presentation timeline: %w", err)
	}
	return p, nil
}
