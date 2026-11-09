package lyric_test

import (
	"testing"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestIdentityAndMetadataSnapshot(t *testing.T) {
	lines := []lyric.Line{
		{ID: "line:1", Start: 0, End: 20, Agent: "voice-a", Language: "zh-Hans"},
		{Start: 2, End: 3},
	}
	properties := []lyric.Property{{Name: "title", Value: "Synthetic"}, {Name: "tag", Value: "one"}, {Name: "tag", Value: "two"}}
	doc, err := lyric.NewDocumentWithMetadata(lines, properties)
	if err != nil {
		t.Fatal(err)
	}
	properties[0].Value = "changed"
	lines[0].ID, lines[0].Agent = "changed", "changed"
	p := doc.Property(0)
	p.Value = "changed again"
	if doc.Line(0).ID() != "line:1" || doc.Line(1).ID() == "" || doc.Line(1).ID() == "line:1" {
		t.Fatal("line identities not preserved or generated uniquely")
	}
	if doc.PropertyCount() != 3 || doc.Property(0).Value != "Synthetic" || doc.Property(2).Value != "two" {
		t.Fatal("metadata lost order, duplicates or snapshot ownership")
	}
	if doc.Line(0).Agent() != "voice-a" || doc.Line(0).Language() != "zh-Hans" {
		t.Fatal("agent or language was not retained")
	}
	second, err := lyric.NewDocument([]lyric.Line{{Start: 0, End: 1}, {Start: 2, End: 3}})
	third, err2 := lyric.NewDocument([]lyric.Line{{Start: 0, End: 1}, {Start: 2, End: 3}})
	if err != nil || err2 != nil || second.Line(1).ID() != third.Line(1).ID() {
		t.Fatal("generated identities must be deterministic for the same input order")
	}
}

func TestDuplicateIdentityRejected(t *testing.T) {
	doc, err := lyric.NewDocument([]lyric.Line{{ID: "same", Start: 0, End: 2}, {ID: "same", Start: 1, End: 3}})
	if err == nil || doc != nil {
		t.Fatal("duplicate explicit identity accepted")
	}
}
