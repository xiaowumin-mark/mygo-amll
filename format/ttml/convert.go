package ttml

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

type timing struct {
	start, end       time.Duration
	hasStart, hasEnd bool
}

func sourceError(n *node, message string, args ...any) error {
	return formatutil.Error(n.line, n.column, "ttml", fmt.Sprintf(message, args...))
}

func stripBackgroundDelimiters(text string) string {
	text = strings.TrimLeft(text, "(（")
	text = strings.TrimRight(text, ")）")
	return strings.TrimFunc(text, ttmlWhitespace)
}

type textOutput struct {
	text     strings.Builder
	pending  bool
	canSpace bool
	leading  bool
}

func (out *textOutput) write(value string) {
	for _, r := range value {
		if ttmlWhitespace(r) {
			if !out.canSpace {
				out.leading = true
			}
			out.pending = true
			continue
		}
		if out.pending && out.canSpace {
			out.text.WriteByte(' ')
		}
		out.pending = false
		out.text.WriteRune(r)
		out.canSpace = true
	}
}

func (out *textOutput) word() string {
	value := out.text.String()
	if out.pending && out.canSpace {
		value += " "
	}
	return value
}

func appendWordSpace(word *lyric.Word) {
	last, _ := utf8.DecodeLastRuneInString(word.Text)
	if word.Text != "" && !ttmlWhitespace(last) {
		word.Text += " "
	}
}

func (d *documentParser) diagnoseReversedWords(words []lyric.Word, line, column int) error {
	reversed := false
	for index := range words {
		word := &words[index]
		if word.End >= 0 && word.End < word.Start {
			reversed = true
		}
		for rubyIndex := range word.Ruby {
			ruby := &word.Ruby[rubyIndex]
			if ruby.End < ruby.Start {
				reversed = true
			}
		}
	}
	if !reversed {
		return nil
	}
	for _, diagnostic := range d.p.Diagnostics {
		if diagnostic.Code == "reversed-word" {
			return nil
		}
	}
	return d.p.Warn(line, column, "reversed-word", "source contains reversed word timing; original values retained")
}

func (d *documentParser) nodeTiming(n *node) (timing, error) {
	return d.readTiming(n, false, 0)
}

func (d *documentParser) readTiming(n *node, implicitStart bool, base time.Duration) (timing, error) {
	var result timing
	var err error
	if value, ok := timingAttribute(n.attrs, "begin"); ok {
		result.start, err = d.parseTime(value)
		if err != nil {
			return result, sourceError(n, "begin: %v", err)
		}
		result.hasStart = true
	}
	if value, ok := timingAttribute(n.attrs, "end"); ok {
		result.end, err = d.parseTime(value)
		if err != nil {
			return result, sourceError(n, "end: %v", err)
		}
		result.hasEnd = true
	}
	if value, ok := timingAttribute(n.attrs, "dur"); ok {
		if !result.hasStart && implicitStart {
			result.start, result.hasStart = base, true
		}
		if !result.hasStart {
			return result, sourceError(n, "dur requires begin in this profile")
		}
		duration, err := d.parseTime(value)
		if err != nil {
			return result, sourceError(n, "dur: %v", err)
		}
		end, err := formatutil.Shift(result.start, duration)
		if err != nil {
			return result, sourceError(n, "dur: %v", err)
		}
		if result.hasEnd && end != result.end {
			return result, sourceError(n, "end and dur disagree")
		}
		result.end, result.hasEnd = end, true
	}
	return result, nil
}

// timingAttribute prefers the standard unqualified timing attribute, then
// mirrors AMLL's fallback to an attribute with the same local name.
func timingAttribute(attrs []xml.Attr, local string) (string, bool) {
	if value, ok := attribute(attrs, "", local); ok {
		return value, true
	}
	for _, attr := range attrs {
		if attr.Name.Local == local {
			return attr.Value, true
		}
	}
	return "", false
}

func (d *documentParser) wordTiming(n *node) (timing, error) {
	var result timing
	begin, hasBegin := timingAttribute(n.attrs, "begin")
	end, hasEnd := timingAttribute(n.attrs, "end")
	if !hasBegin || !hasEnd {
		return result, sourceError(n, "timed span requires begin and end")
	}
	var err error
	if result.start, err = d.parseTime(begin); err != nil {
		return result, sourceError(n, "begin: %v", err)
	}
	if result.end, err = d.parseTime(end); err != nil {
		return result, sourceError(n, "end: %v", err)
	}
	result.hasStart, result.hasEnd = true, true
	return result, nil
}

func (d *documentParser) convertParagraph(n *node) error {
	t, err := d.nodeTiming(n)
	if err != nil {
		return err
	}
	line := formatutil.Line{SourceLine: n.line, InferEnd: !t.hasEnd, PreserveTiming: true}
	// AMLL keys lines with the namespaced iTunes key. xml:id and generated IDs
	// remain accepted as a compatibility extension for existing local callers.
	line.ID, _ = attribute(n.attrs, itunesNS, "key")
	if line.ID == "" {
		line.ID, _ = attribute(n.attrs, xmlNS, "id")
	}
	if line.ID == "" {
		line.ID = fmt.Sprintf("ttml:p:%d", len(d.lines)+1)
	}
	line.Agent, _ = attributeEither(n.attrs, ttmNS, "agent")
	line.Duet = line.Agent == "v2"
	line.Language = n.lang
	line.Start, line.End = t.start, t.end
	role, _ := attributeEither(n.attrs, ttmNS, "role")
	line.Background = role == "x-bg"
	if role != "" && role != "x-bg" {
		if err := d.p.Warn(n.line, n.column, "unsupported-role", "paragraph role "+role+" is treated as original lyrics"); err != nil {
			return err
		}
	}
	var text textOutput
	var backgrounds []formatutil.Line
	if err := d.walk(n, &line, &text, &backgrounds); err != nil {
		return err
	}
	if err := d.diagnoseReversedWords(line.Words, n.line, n.column); err != nil {
		return err
	}
	if len(line.Words) > 0 {
		line.Words[0].Text = strings.TrimLeftFunc(line.Words[0].Text, ttmlWhitespace)
		last := len(line.Words) - 1
		line.Words[last].Text = strings.TrimRightFunc(line.Words[last].Text, ttmlWhitespace)
	}
	line.Text = text.text.String()
	// AMLL calculates paragraph bounds from the union of timed words and the
	// background vocal. A zero parsed end is its "missing" sentinel, even when
	// the XML attribute was present; child timing can therefore extend the p.
	minChildStart, maxChildEnd := time.Duration(1<<63-1), time.Duration(0)
	hasTimedChild := false
	includeChild := func(start, end time.Duration) {
		if start < 0 {
			return
		}
		minChildStart = min(minChildStart, start)
		if end >= 0 {
			maxChildEnd = max(maxChildEnd, end)
		}
		hasTimedChild = true
	}
	for _, word := range line.Words {
		includeChild(word.Start, word.End)
	}
	for _, background := range backgrounds {
		includeChild(background.Start, background.End)
	}
	if hasTimedChild {
		if line.Start == 0 || minChildStart > 0 && minChildStart < line.Start {
			line.Start = minChildStart
		}
		if line.End == 0 || maxChildEnd > line.End {
			line.End, line.InferEnd = maxChildEnd, false
			if !t.hasEnd || t.end == 0 {
				if err := d.p.Warn(n.line, n.column, "inferred-end", "paragraph end inferred from timed words"); err != nil {
					return err
				}
			}
		}
	}
	if !t.hasStart {
		if !hasTimedChild {
			return sourceError(n, "paragraph requires begin or timed words")
		}
		line.Start = minChildStart
		if err := d.p.Warn(n.line, n.column, "inferred-start", "paragraph begin inferred from timed words"); err != nil {
			return err
		}
	}
	if line.Text != "" && len(line.Words) == 0 {
		if err := d.p.AddWords(1); err != nil {
			return err
		}
		start, end := line.Start, line.End
		if t.start > 0 {
			start = t.start
		}
		if t.end > 0 {
			end = t.end
		} else if line.InferEnd {
			end = -1
		}
		line.Words = append(line.Words, lyric.Word{Text: line.Text, Start: start, End: end})
		if err := d.diagnoseReversedWords(line.Words, n.line, n.column); err != nil {
			return err
		}
	}
	if line.InferEnd && len(line.Words) > 0 {
		allKnown := true
		var maxEnd time.Duration
		for _, word := range line.Words {
			allKnown = allKnown && word.End >= 0
			maxEnd = max(maxEnd, word.End)
		}
		if allKnown && maxEnd > line.Start {
			line.End, line.InferEnd = maxEnd, false
			if err := d.p.Warn(n.line, n.column, "inferred-end", "paragraph end inferred from timed words"); err != nil {
				return err
			}
		}
	}
	if line.Text == "" {
		if err := d.p.Warn(n.line, n.column, "empty-line", "empty original paragraph omitted"); err != nil {
			return err
		}
	} else {
		d.lines = append(d.lines, line)
	}
	for i := range backgrounds {
		bg := &backgrounds[i]
		if bg.Start < 0 {
			bg.Start = line.Start
		}
		if bg.InferEnd && !line.InferEnd {
			bg.End, bg.InferEnd = line.End, false
		}
		if bg.Text != "" {
			if line.Text != "" && !line.Background {
				bg.ParentID = line.ID
			}
			d.lines = append(d.lines, *bg)
		}
	}
	return nil
}

func (d *documentParser) walk(n *node, line *formatutil.Line, text *textOutput, backgrounds *[]formatutil.Line) error {
	if err := d.p.Check(); err != nil {
		return err
	}
	backgroundStart := len(*backgrounds)
	var backgroundTranslation, backgroundRomanization string
	for _, piece := range n.parts {
		if piece.child == nil {
			text.write(piece.text)
			if len(line.Words) > 0 && !strings.ContainsRune(piece.text, '\n') && strings.TrimFunc(piece.text, ttmlWhitespace) == "" && piece.text != "" {
				appendWordSpace(&line.Words[len(line.Words)-1])
			}
			continue
		}
		child := piece.child
		role, _ := attributeEither(child.attrs, ttmNS, "role")
		switch role {
		case "x-translation", "x-roman":
			annotation, backgroundAnnotation, err := d.parseSidecarContent(child)
			if err != nil {
				return err
			}
			dst := &line.Translation
			bgDst := &backgroundTranslation
			if role == "x-roman" {
				dst = &line.Romanization
				bgDst = &backgroundRomanization
			}
			// The simple lyric view chooses the first available annotation, as
			// toAmllLyrics does without a preferred language.
			if *dst == "" && annotation != nil {
				*dst = annotation.text
			}
			if *bgDst == "" && backgroundAnnotation != nil {
				*bgDst = backgroundAnnotation.text
			}
			continue
		case "x-bg":
			if !d.annotationOnly {
				if err := d.p.AddLine(); err != nil {
					return err
				}
			}
			d.bgCount++
			bg := formatutil.Line{SourceLine: child.line, InferEnd: true, PreserveTiming: true, Line: lyric.Line{ID: fmt.Sprintf("%s:bg:%d", line.ID, d.bgCount), Start: -1, Agent: line.Agent, Duet: line.Duet, Background: true, Language: child.lang}}
			if id, ok := attribute(child.attrs, itunesNS, "key"); ok {
				bg.ID = id
			} else if id, ok := attribute(child.attrs, xmlNS, "id"); ok {
				bg.ID = id
			}
			if agent, ok := attributeEither(child.attrs, ttmNS, "agent"); ok {
				bg.Agent, bg.Duet = agent, agent == "v2"
			}
			t, err := d.nodeTiming(child)
			if err != nil {
				return err
			}
			if t.hasStart {
				bg.Start = t.start
			}
			if t.hasEnd {
				bg.End, bg.InferEnd = t.end, false
			}
			var bgText textOutput
			if err := d.walk(child, &bg, &bgText, backgrounds); err != nil {
				return err
			}
			if err := d.diagnoseReversedWords(bg.Words, child.line, child.column); err != nil {
				return err
			}
			bg.Text = bgText.text.String()
			bg.Text = stripBackgroundDelimiters(bg.Text)
			if len(bg.Words) > 0 {
				bg.Words[0].Text = strings.TrimLeft(bg.Words[0].Text, "(（")
				bg.Words[0].Text = strings.TrimLeftFunc(bg.Words[0].Text, ttmlWhitespace)
				last := len(bg.Words) - 1
				bg.Words[last].Text = strings.TrimRight(bg.Words[last].Text, ")）")
				bg.Words[last].Text = strings.TrimRightFunc(bg.Words[last].Text, ttmlWhitespace)
			}
			if len(bg.Words) > 0 {
				if !t.hasStart {
					bg.Start = bg.Words[0].Start
					for _, word := range bg.Words[1:] {
						bg.Start = min(bg.Start, word.Start)
					}
				}
				if !t.hasEnd {
					allKnown := true
					var maxEnd time.Duration
					for _, word := range bg.Words {
						allKnown = allKnown && word.End >= 0
						maxEnd = max(maxEnd, word.End)
					}
					if allKnown && maxEnd > bg.Start {
						bg.End, bg.InferEnd = maxEnd, false
					}
				}
			}
			if bg.Text != "" && len(bg.Words) == 0 && (t.hasStart || t.hasEnd) {
				if err := d.p.AddWords(1); err != nil {
					return err
				}
				start, end := max(time.Duration(0), bg.Start), bg.End
				if bg.InferEnd {
					end = -1
				}
				bg.Words = append(bg.Words, lyric.Word{Text: bg.Text, Start: start, End: end})
			}
			*backgrounds = append(*backgrounds, bg)
			continue
		case "":
		default:
			if err := d.p.Warn(child.line, child.column, "unsupported-role", "unknown role "+role+" is treated as original lyrics"); err != nil {
				return err
			}
		}
		if isTT(child.name, "br") {
			continue
		}
		if isRubyContainer(child) {
			base, word, err := d.rubyWord(child)
			if err != nil {
				return err
			}
			text.write(base)
			line.Words = append(line.Words, word)
			continue
		}
		_, hasBegin := timingAttribute(child.attrs, "begin")
		_, hasEnd := timingAttribute(child.attrs, "end")
		if hasBegin && hasEnd {
			t, err := d.wordTiming(child)
			if err != nil {
				return err
			}
			var wordText textOutput
			if err := d.plainText(child, true, &wordText, text); err != nil {
				return err
			}
			if wordText.leading && len(line.Words) > 0 {
				appendWordSpace(&line.Words[len(line.Words)-1])
			}
			value := wordText.word()
			if strings.TrimFunc(value, ttmlWhitespace) == "" {
				continue
			}
			if err := d.p.AddWords(1); err != nil {
				return err
			}
			end := time.Duration(-1)
			if t.hasEnd {
				end = t.end
			}
			word := lyric.Word{Text: value, Start: t.start, End: end}
			if err := readWordAnnotations(child, &word); err != nil {
				return err
			}
			line.Words = append(line.Words, word)
		} else if err := d.walk(child, line, text, backgrounds); err != nil {
			return err
		}
	}
	if len(*backgrounds) > backgroundStart {
		bg := &(*backgrounds)[len(*backgrounds)-1]
		if bg.Translation == "" {
			bg.Translation = backgroundTranslation
		}
		if bg.Romanization == "" {
			bg.Romanization = backgroundRomanization
		}
	}
	return nil
}

// Emit the same whitespace and explicit-break events to each destination. A
// word's local output and the containing line therefore agree on internal text,
// while the line can still collapse whitespace across adjacent span boundaries.
func (d *documentParser) plainText(n *node, rejectNestedTiming bool, outputs ...*textOutput) error {
	var visit func(*node) error
	visit = func(current *node) error {
		if err := d.p.Check(); err != nil {
			return err
		}
		for _, piece := range current.parts {
			if piece.child == nil {
				for _, out := range outputs {
					out.write(piece.text)
				}
				continue
			}
			child := piece.child
			if rejectNestedTiming {
				for _, key := range []string{"begin", "end", "dur"} {
					if _, ok := timingAttribute(child.attrs, key); ok {
						return sourceError(child, "nested timed spans are unsupported")
					}
				}
				if role, _ := attributeEither(child.attrs, ttmNS, "role"); role != "" {
					return sourceError(child, "annotations inside timed spans are unsupported")
				}
			}
			if isTT(child.name, "br") {
				continue
			} else if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(n)
}
