package yrc_test

import (
	"context"
	"os"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/format/yrc"
)

func TestSyntheticGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/synthetic.yrc")
	if err != nil {
		t.Fatal(err)
	}
	result, err := yrc.Parse(context.Background(), data, common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 3 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "zero-word" {
		t.Fatalf("unexpected result: %+v", result)
	}
	first, last := result.Document.Line(0), result.Document.Line(2)
	if first.Text() != "星✨" || first.Start() != 1010*time.Millisecond || first.End() != 2010*time.Millisecond || first.WordCount() != 2 || first.Word(1).Text != "✨" || first.Word(1).Start != 1410*time.Millisecond {
		t.Fatalf("unexpected first line: %+v", first)
	}
	if result.Document.Line(1).Text() != "合" || last.Text() != "零声" || last.WordCount() != 1 || last.Word(0).Text != "声" {
		t.Fatal("overlap order or zero-word text was lost")
	}
	if result.Document.PropertyCount() != 3 || result.Document.Property(0).Name != "ti" || result.Document.Property(0).Value != "Synthetic vectors" {
		t.Fatal("metadata was lost")
	}
}
