package lyric_test

import (
	"strings"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestDocumentSnapshot(t *testing.T) {
	input := []lyric.Line{{
		Text: "Hello", Translation: "你好", Romanization: "hello",
		Start: time.Second, End: 3 * time.Second, Duet: true, Background: true,
		Words: []lyric.Word{{Text: "Hello", Start: time.Second, End: 2 * time.Second}},
	}}
	doc, err := lyric.NewDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Text = "changed"
	input[0].Words[0].Text = "changed"
	line := doc.Line(0)
	word := line.Word(0)
	word.Text = "changed again"
	if doc.Len() != 1 || line.Text() != "Hello" || line.Word(0).Text != "Hello" {
		t.Fatal("input or returned word mutated the document")
	}
	if line.Translation() != "你好" || line.Romanization() != "hello" || !line.Duet() || !line.Background() {
		t.Fatal("line metadata was not retained")
	}
	if line.Start() != time.Second || line.End() != 3*time.Second || line.WordCount() != 1 {
		t.Fatal("timing or word count was not retained")
	}
}

func TestDocumentValidation(t *testing.T) {
	tests := []struct {
		name  string
		lines []lyric.Line
		want  string
	}{
		{name: "empty"},
		{name: "negative", lines: []lyric.Line{{Start: -1, End: 2}}, want: "line 0"},
		{name: "zero interval", lines: []lyric.Line{{Start: 2, End: 2}}, want: "line 0"},
		{name: "reversed interval", lines: []lyric.Line{{Start: 3, End: 2}}, want: "line 0"},
		{name: "unsorted", lines: []lyric.Line{{Start: 2, End: 3}, {Start: 0, End: 1}}, want: "line 1"},
		{name: "equal starts", lines: []lyric.Line{{Start: 0, End: 3}, {Start: 0, End: 2}}},
		{name: "word before line", lines: []lyric.Line{{Start: 2, End: 5, Words: []lyric.Word{{Start: 1, End: 3}}}}, want: "word 0"},
		{name: "word after line", lines: []lyric.Line{{Start: 2, End: 5, Words: []lyric.Word{{Start: 3, End: 6}}}}, want: "word 0"},
		{name: "zero word", lines: []lyric.Line{{Start: 0, End: 5, Words: []lyric.Word{{Start: 2, End: 2}}}}, want: "word 0"},
		{name: "unsorted words", lines: []lyric.Line{{Start: 0, End: 5, Words: []lyric.Word{{Start: 2, End: 4}, {Start: 1, End: 3}}}}, want: "word 1"},
		{name: "negative word", lines: []lyric.Line{{Start: 0, End: 5, Words: []lyric.Word{{Start: -1, End: 3}}}}, want: "word 0"},
		{name: "reversed word", lines: []lyric.Line{{Start: 0, End: 5, Words: []lyric.Word{{Start: 3, End: 2}}}}, want: "word 0"},
		{name: "overlapping words", lines: []lyric.Line{{Start: 0, End: 5, Words: []lyric.Word{{Start: 0, End: 4}, {Start: 1, End: 5}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := lyric.NewDocument(test.lines)
			if test.want == "" {
				if err != nil || doc == nil {
					t.Fatalf("expected valid document, got %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) || doc != nil {
				t.Fatalf("expected error containing %q and nil document, got %v, %v", test.want, doc, err)
			}
		})
	}
}
