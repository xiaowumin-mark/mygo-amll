package lyric

import (
	"testing"
	"time"
)

func TestRubySnapshotAndBackgroundIdentity(t *testing.T) {
	input := []Line{
		{ID: "bg", ParentID: "main", Background: true, Text: "answer", End: time.Second},
		{ID: "main", Text: "字", Start: time.Second, End: 3 * time.Second, Words: []Word{{Text: "字", Start: time.Second, End: 3 * time.Second, Ruby: []RubyWord{{Text: "zi", Start: time.Second, End: 2 * time.Second}}, Obscene: true, EmptyBeat: 2}}},
	}
	doc, err := NewDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	input[1].Words[0].Ruby[0].Text = "changed"
	word := doc.Line(1).Word(0)
	if word.Ruby[0].Text != "zi" || !word.Obscene || word.EmptyBeat != 2 || doc.Line(0).ParentID() != "main" {
		t.Fatal("render metadata was lost or aliased input")
	}
	word.Ruby[0].Text = "caller changed"
	if doc.Line(1).Word(0).Ruby[0].Text != "zi" {
		t.Fatal("Word exposed document Ruby storage")
	}
}

func TestRubyAndParentValidation(t *testing.T) {
	for _, ruby := range [][]RubyWord{
		{{Text: "x", Start: -1, End: time.Second}},
		{{Text: "x", Start: 2 * time.Second, End: time.Second}},
		{{Text: "x", End: 4 * time.Second}},
		{{Text: "x", Start: time.Second, End: time.Second}, {Text: "y", End: time.Second}},
	} {
		_, err := NewDocument([]Line{{End: 3 * time.Second, Words: []Word{{End: 3 * time.Second, Ruby: ruby}}}})
		if err == nil {
			t.Fatalf("invalid Ruby accepted: %+v", ruby)
		}
	}
	for _, lines := range [][]Line{
		{{ID: "x", End: time.Second, ParentID: "x"}},
		{{ID: "x", End: time.Second, ParentID: "missing", Background: true}},
		{{ID: "x", End: time.Second, Background: true}, {ID: "y", End: time.Second, Background: true, ParentID: "x"}},
	} {
		if _, err := NewDocument(lines); err == nil {
			t.Fatal("invalid parent relationship accepted")
		}
	}
}
