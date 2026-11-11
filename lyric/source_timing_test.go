package lyric_test

import (
	"reflect"
	"testing"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestSourceTimingIsExplicitAndRetainsEndpoints(t *testing.T) {
	input := []lyric.Line{{ID: "source", Start: 2, End: 2, Text: "ABC", Words: []lyric.Word{
		{Text: "A", Start: 9, End: 3, Ruby: []lyric.RubyWord{{Text: "ay", Start: 8, End: 1}, {Text: "a", Start: 0, End: 12}}},
		{Text: "B", Start: 1, End: 1},
		{Text: "C", Start: 0, End: 15},
	}}}
	properties := []lyric.Property{{Name: "source", Value: "synthetic"}}
	for _, constructor := range []struct {
		name string
		call func([]lyric.Line) (*lyric.Document, error)
	}{
		{"default", lyric.NewDocument},
		{"metadata", func(lines []lyric.Line) (*lyric.Document, error) {
			return lyric.NewDocumentWithMetadata(lines, properties)
		}},
		{"zero options", func(lines []lyric.Line) (*lyric.Document, error) {
			return lyric.NewDocumentWithOptions(lines, properties, lyric.DocumentOptions{})
		}},
	} {
		t.Run(constructor.name, func(t *testing.T) {
			if doc, err := constructor.call(input); err == nil || doc != nil {
				t.Fatalf("strict constructor accepted imperfect source timing: doc=%v error=%v", doc, err)
			}
		})
	}
	doc, err := lyric.NewDocumentWithOptions(input, properties, lyric.DocumentOptions{PreserveSourceTiming: true})
	if err != nil {
		t.Fatal(err)
	}
	line := doc.Line(0)
	if line.Start() != 2 || line.End() != 2 || line.WordCount() != len(input[0].Words) {
		t.Fatalf("source line changed: start=%v end=%v words=%d", line.Start(), line.End(), line.WordCount())
	}
	for index, want := range input[0].Words {
		if got := line.Word(index); !reflect.DeepEqual(got, want) {
			t.Fatalf("word %d changed: got %+v want %+v", index, got, want)
		}
	}
	input[0].Words[0].Ruby[0].Text = "changed"
	properties[0].Value = "changed"
	word := line.Word(0)
	word.Ruby[0].Text = "caller changed"
	if line.Ruby(0, 0).Text != "ay" || doc.Property(0).Value != "synthetic" {
		t.Fatal("source-preserving constructor exposed mutable backing storage")
	}
}

func TestSourceTimingStillValidatesLinesAndEndpoints(t *testing.T) {
	for _, test := range []struct {
		name  string
		lines []lyric.Line
	}{
		{"negative line", []lyric.Line{{Start: -1, End: 2}}},
		{"negative line end", []lyric.Line{{Start: 3, End: -1}}},
		{"unsorted lines", []lyric.Line{{Start: 3, End: 3}, {Start: 1, End: 1}}},
		{"negative word start", []lyric.Line{{End: 2, Words: []lyric.Word{{Start: -1, End: 1}}}}},
		{"negative word end", []lyric.Line{{End: 2, Words: []lyric.Word{{Start: 1, End: -1}}}}},
		{"negative ruby start", []lyric.Line{{End: 2, Words: []lyric.Word{{End: 1, Ruby: []lyric.RubyWord{{Start: -1, End: 1}}}}}}},
		{"negative ruby end", []lyric.Line{{End: 2, Words: []lyric.Word{{End: 1, Ruby: []lyric.RubyWord{{Start: 1, End: -1}}}}}}},
		{"duplicate identity", []lyric.Line{{ID: "same", End: 1}, {ID: "same", End: 2}}},
		{"invalid parent", []lyric.Line{{ID: "background", ParentID: "missing", Background: true, End: 1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if doc, err := lyric.NewDocumentWithOptions(test.lines, nil, lyric.DocumentOptions{PreserveSourceTiming: true}); err == nil || doc != nil {
				t.Fatalf("accepted invalid source data: doc=%v error=%v", doc, err)
			}
		})
	}
}

func TestSourceTimingRetainsReversedLineEndpoints(t *testing.T) {
	input := []lyric.Line{{ID: "source", Start: 3, End: 1, Text: "authored"}}
	if doc, err := lyric.NewDocument(input); err == nil || doc != nil {
		t.Fatal("default constructor accepted a reversed line")
	}
	doc, err := lyric.NewDocumentWithOptions(input, nil, lyric.DocumentOptions{PreserveSourceTiming: true})
	if err != nil || doc.Line(0).Start() != 3 || doc.Line(0).End() != 1 {
		t.Fatalf("source line endpoints were changed or rejected: %+v %v", doc, err)
	}
}
