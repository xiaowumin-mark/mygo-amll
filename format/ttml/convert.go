package ttml

import (
	"fmt"
	"strings"
	"time"

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

type textOutput struct {
	text     strings.Builder
	pending  bool
	canSpace bool
}

func (out *textOutput) write(value string, preserve bool) {
	if preserve {
		if out.pending && out.canSpace {
			out.text.WriteByte(' ')
		}
		out.pending = false
		out.text.WriteString(value)
		if value != "" {
			out.canSpace = value[len(value)-1] != '\n'
		}
		return
	}
	for _, r := range value {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
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

func (out *textOutput) lineBreak() {
	out.pending = false
	out.text.WriteByte('\n')
	out.canSpace = false
}

func nodeTiming(n *node) (timing, error) {
	var result timing
	var err error
	if value, ok := attribute(n.attrs, "", "begin"); ok {
		result.start, err = parseTime(value)
		if err != nil {
			return result, sourceError(n, "begin: %v", err)
		}
		result.hasStart = true
	}
	if value, ok := attribute(n.attrs, "", "end"); ok {
		result.end, err = parseTime(value)
		if err != nil {
			return result, sourceError(n, "end: %v", err)
		}
		result.hasEnd = true
	}
	if value, ok := attribute(n.attrs, "", "dur"); ok {
		if !result.hasStart {
			return result, sourceError(n, "dur requires begin in this profile")
		}
		duration, err := parseTime(value)
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

func (d *documentParser) convertParagraph(n *node) error {
	t, err := nodeTiming(n)
	if err != nil {
		return err
	}
	line := formatutil.Line{SourceLine: n.line, InferEnd: !t.hasEnd}
	line.ID, _ = attribute(n.attrs, xmlNS, "id")
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
	line.Text = text.text.String()
	if !t.hasStart {
		if len(line.Words) == 0 {
			return sourceError(n, "paragraph requires begin or timed words")
		}
		line.Start = line.Words[0].Start
		for _, word := range line.Words[1:] {
			line.Start = min(line.Start, word.Start)
		}
		if err := d.p.Warn(n.line, n.column, "inferred-start", "paragraph begin inferred from timed words"); err != nil {
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
			d.lines = append(d.lines, *bg)
		}
	}
	return nil
}

func (d *documentParser) walk(n *node, line *formatutil.Line, text *textOutput, backgrounds *[]formatutil.Line) error {
	if err := d.p.Check(); err != nil {
		return err
	}
	for _, piece := range n.parts {
		if piece.child == nil {
			text.write(piece.text, n.preserve)
			continue
		}
		child := piece.child
		role, _ := attributeEither(child.attrs, ttmNS, "role")
		switch role {
		case "x-translation", "x-roman":
			var annotation textOutput
			if err := d.plainText(child, false, &annotation); err != nil {
				return err
			}
			value := annotation.text.String()
			dst := &line.Translation
			if role == "x-roman" {
				dst = &line.Romanization
			}
			if *dst != "" {
				if err := d.p.Warn(child.line, child.column, "multiple-annotations", "multiple inline annotations joined with a newline"); err != nil {
					return err
				}
				*dst += "\n"
			}
			*dst += value
			continue
		case "x-bg":
			if err := d.p.AddLine(); err != nil {
				return err
			}
			d.bgCount++
			bg := formatutil.Line{SourceLine: child.line, InferEnd: true, Line: lyric.Line{ID: fmt.Sprintf("%s:bg:%d", line.ID, d.bgCount), Start: -1, Agent: line.Agent, Duet: line.Duet, Background: true, Language: child.lang}}
			if id, ok := attribute(child.attrs, xmlNS, "id"); ok {
				bg.ID = id
			}
			if agent, ok := attributeEither(child.attrs, ttmNS, "agent"); ok {
				bg.Agent, bg.Duet = agent, agent == "v2"
			}
			t, err := nodeTiming(child)
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
			bg.Text = bgText.text.String()
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
			*backgrounds = append(*backgrounds, bg)
			continue
		case "":
		default:
			if err := d.p.Warn(child.line, child.column, "unsupported-role", "unknown role "+role+" is treated as original lyrics"); err != nil {
				return err
			}
		}
		if isTT(child.name, "br") {
			text.lineBreak()
			continue
		}
		t, err := nodeTiming(child)
		if err != nil {
			return err
		}
		if t.hasStart || t.hasEnd {
			if !t.hasStart {
				return sourceError(child, "timed span requires begin")
			}
			if err := d.p.AddWords(1); err != nil {
				return err
			}
			var wordText textOutput
			if err := d.plainText(child, true, &wordText, text); err != nil {
				return err
			}
			end := time.Duration(-1)
			if t.hasEnd {
				end = t.end
			}
			line.Words = append(line.Words, lyric.Word{Text: wordText.text.String(), Start: t.start, End: end})
		} else if err := d.walk(child, line, text, backgrounds); err != nil {
			return err
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
					out.write(piece.text, current.preserve)
				}
				continue
			}
			child := piece.child
			if rejectNestedTiming {
				for _, key := range []string{"begin", "end", "dur"} {
					if _, ok := attribute(child.attrs, "", key); ok {
						return sourceError(child, "nested timed spans are unsupported")
					}
				}
				if role, _ := attributeEither(child.attrs, ttmNS, "role"); role != "" {
					return sourceError(child, "annotations inside timed spans are unsupported")
				}
			}
			if isTT(child.name, "br") {
				for _, out := range outputs {
					out.lineBreak()
				}
			} else if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(n)
}
