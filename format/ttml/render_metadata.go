package ttml

import (
	"strconv"
	"strings"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

const stylingNS = "http://www.w3.org/ns/ttml#styling"

func rubyRole(n *node) string {
	role, _ := attribute(n.attrs, stylingNS, "ruby")
	return role
}

func isRubyContainer(n *node) bool { return rubyRole(n) == "container" }

func readWordAnnotations(n *node, word *lyric.Word) error {
	if value, ok := attribute(n.attrs, amllNS, "obscene"); ok {
		word.Obscene = value == "true"
	}
	if value, ok := attribute(n.attrs, amllNS, "empty-beat"); ok {
		beat, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return sourceError(n, "invalid empty-beat: %v", err)
		}
		word.EmptyBeat = beat
	}
	return nil
}

// rubyWord preserves a timed TTML ruby container as one base word with its
// pronunciation segments. Reading only the base keeps ruby text out of lyrics.
// The structural convention follows the fixed AMLL TTML parser; all strings and
// intervals still pass this library's shared budgets and immutable snapshot.
func (d *documentParser) rubyWord(n *node) (string, lyric.Word, error) {
	var base textOutput
	var ruby []lyric.RubyWord
	var visit func(*node) error
	visit = func(current *node) error {
		if err := d.p.Check(); err != nil {
			return err
		}
		for _, part := range current.parts {
			child := part.child
			if child == nil {
				continue
			}
			switch rubyRole(child) {
			case "base":
				if err := d.plainText(child, false, &base); err != nil {
					return err
				}
			case "text":
				t, err := d.nodeTiming(child)
				if err != nil {
					return err
				}
				if !t.hasStart || !t.hasEnd {
					return sourceError(child, "ruby text requires explicit begin and end")
				}
				if err := d.p.AddWords(1); err != nil {
					return err
				}
				var label textOutput
				if err := d.plainText(child, false, &label); err != nil {
					return err
				}
				ruby = append(ruby, lyric.RubyWord{Text: strings.TrimFunc(label.text.String(), ttmlWhitespace), Start: t.start, End: t.end})
			default:
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(n); err != nil {
		return "", lyric.Word{}, err
	}
	if base.text.Len() == 0 || len(ruby) == 0 {
		return "", lyric.Word{}, sourceError(n, "ruby container requires base text and timed pronunciation")
	}
	start, end := time.Duration(1<<63-1), time.Duration(0)
	for _, part := range ruby {
		start, end = min(start, part.Start), max(end, part.End)
	}
	if err := d.p.AddWords(1); err != nil {
		return "", lyric.Word{}, err
	}
	word := lyric.Word{Text: strings.TrimFunc(base.text.String(), ttmlWhitespace), Start: start, End: end, Ruby: ruby}
	if err := readWordAnnotations(n, &word); err != nil {
		return "", lyric.Word{}, err
	}
	return base.text.String(), word, nil
}
