package formatutil

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestInputBudgetsAndEncoding(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		limits      common.Limits
		fail        bool
	}{
		{"empty", "", common.Limits{}, false},
		{"bytes exact", "abc", common.Limits{MaxBytes: 3}, false},
		{"bytes exceeded", "abcd", common.Limits{MaxBytes: 3}, true},
		{"LF trailing", "a\nb\n", common.Limits{MaxLines: 2}, false},
		{"CRLF", "a\r\nb\r\n", common.Limits{MaxLines: 2}, false},
		{"CR", "a\rb\r", common.Limits{MaxLines: 2}, false},
		{"physical exceeded", "a\n\nb", common.Limits{MaxLines: 2}, true},
		{"bad utf8", "a\n\xff", common.Limits{}, true},
		{"negative option", "", common.Limits{MaxDepth: -1}, true},
		{"negative text budget", "", common.Limits{MaxTextBytes: -1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(context.Background(), []byte(tc.input), common.Options{Limits: tc.limits})
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v, want failure=%v", err, tc.fail)
			}
		})
	}
	_, err := New(context.Background(), []byte("汉\n中\xff"), common.Options{})
	var diagnostic *common.ParseError
	if !errors.As(err, &diagnostic) || diagnostic.Line != 2 || diagnostic.Column != 4 {
		t.Fatalf("encoding error coordinates: %v", err)
	}
	if _, err := New(nil, nil, common.Options{}); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestExpandedTextBudget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lines    []Line
		metadata []lyric.Property
		limit    int
		fail     bool
	}{
		{"exact", []Line{{Line: lyric.Line{Text: "abc", Start: 0, End: 1}, SourceLine: 4}}, nil, 3, false},
		{"repeated text", []Line{{Line: lyric.Line{Text: "abc", Start: 0, End: 1}, SourceLine: 4}, {Line: lyric.Line{Text: "abc", Start: 0, End: 1}, SourceLine: 4}}, nil, 5, true},
		{"word text counted separately", []Line{{Line: lyric.Line{Text: "abc", Start: 0, End: 1, Words: []lyric.Word{{Text: "abc", Start: 0, End: 1}}}, SourceLine: 4}}, nil, 5, true},
		{"metadata", nil, []lyric.Property{{Name: "ti", Value: "abc"}}, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(context.Background(), nil, common.Options{Limits: common.Limits{MaxTextBytes: tc.limit}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.Finish(tc.lines, tc.metadata, 0)
			if (err != nil) != tc.fail || tc.fail && result.Document != nil {
				t.Fatalf("error=%v, want failure=%v", err, tc.fail)
			}
		})
	}
}

func TestDefaultBudgetsEnforced(t *testing.T) {
	ctx := context.Background()
	if _, err := New(ctx, make([]byte, (8<<20)+1), common.Options{}); err == nil {
		t.Fatal("default input byte budget not enforced")
	}
	if _, err := New(ctx, bytes.Repeat([]byte("x\n"), 100001), common.Options{}); err == nil {
		t.Fatal("default physical line budget not enforced")
	}
	p, _ := New(ctx, nil, common.Options{})
	if p.Options.Limits.MaxTextBytes != 16<<20 || p.Options.Limits.MaxDepth != 32 || p.Options.FinalLineDuration != 5*time.Second {
		t.Fatal("default output/depth/end policy changed")
	}
	if p.AddWords(1000000) != nil || p.AddWords(1) == nil {
		t.Fatal("default word budget not enforced")
	}
	for range 100 {
		if err := p.Warn(1, 1, "test", "synthetic warning"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Warn(1, 1, "test", "one warning over budget"); err == nil {
		t.Fatal("default diagnostic budget not enforced")
	}
}

type cancelDuringScan struct {
	context.Context
	calls int
}

func (c *cancelDuringScan) Err() error {
	c.calls++
	if c.calls >= 4 {
		return context.Canceled
	}
	return nil
}

func TestCancellationInsideMultibyteInput(t *testing.T) {
	ctx := &cancelDuringScan{Context: context.Background()}
	_, err := New(ctx, bytes.Repeat([]byte("汉"), 100000), common.Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan did not observe cancellation: %v", err)
	}
}

func TestNormalizeStableGroupsAndOffsets(t *testing.T) {
	p, _ := New(context.Background(), nil, common.Options{Duration: 20 * time.Second})
	result, err := p.Finish([]Line{
		{Line: lyric.Line{ID: "last", Text: "c", Start: 10 * time.Second}, InferEnd: true, SourceLine: 1},
		{Line: lyric.Line{ID: "a", Text: "zero then timed", Start: 0, Words: []lyric.Word{{Text: "zero", Start: 0, End: 0}, {Text: "timed", Start: time.Second, End: -1}}}, InferEnd: true, SourceLine: 2},
		{Line: lyric.Line{ID: "b", Start: 0}, InferEnd: true, SourceLine: 3},
	}, nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	doc := result.Document
	if doc.Line(0).ID() != "a" || doc.Line(1).ID() != "b" || doc.Line(2).ID() != "last" {
		t.Fatal("equal-time source order changed")
	}
	if doc.Line(0).Start() != time.Second || doc.Line(0).End() != 11*time.Second || doc.Line(1).End() != 11*time.Second || doc.Line(2).End() != 20*time.Second {
		t.Fatal("offset or equal-time end inference incorrect")
	}
	if doc.Line(0).WordCount() != 1 || doc.Line(0).Word(0).Start != 2*time.Second || doc.Line(0).Word(0).End != 11*time.Second || doc.Line(0).Text() != "zero then timed" {
		t.Fatal("zero timing or missing word end normalization incorrect")
	}
	if len(result.Diagnostics) != 2 || result.Diagnostics[0].Code != "sorted" || result.Diagnostics[1].Code != "zero-word" {
		t.Fatalf("normalization must be diagnosed: %+v", result.Diagnostics)
	}
}

func TestDiagnosticsAndOutputBudgets(t *testing.T) {
	p, _ := New(context.Background(), nil, common.Options{Limits: common.Limits{MaxLines: 1, MaxWords: 1, MaxDiagnostics: 1}})
	if p.AddLine() != nil || p.AddWords(1) != nil {
		t.Fatal("exact output budget rejected")
	}
	if p.AddLine() == nil || p.AddWords(math.MaxInt) == nil {
		t.Fatal("expanded output exceeded budget")
	}
	if p.Warn(1, 1, "first", "first warning") != nil || p.Warn(1, 1, "second", "second warning") == nil || len(p.Diagnostics) != 1 {
		t.Fatal("diagnostic budget not bounded")
	}
	p, _ = New(context.Background(), nil, common.Options{Strict: true})
	if p.Warn(1, 1, "ambiguous", "must fail") == nil || len(p.Diagnostics) != 0 {
		t.Fatal("strict warning was not promoted")
	}
}

func TestOverflowAndInvalidIntervals(t *testing.T) {
	for _, tc := range []struct {
		value, offset time.Duration
	}{
		{math.MaxInt64, 1}, {0, -1}, {math.MinInt64, -1},
	} {
		if _, err := Shift(tc.value, tc.offset); err == nil {
			t.Fatal("invalid offset accepted")
		}
	}
	for _, line := range []Line{
		{Line: lyric.Line{Start: 10, End: 10}, SourceLine: 9},
		{Line: lyric.Line{Start: math.MaxInt64 - 1}, SourceLine: 9, InferEnd: true},
		{Line: lyric.Line{Start: 0, End: 10, Words: []lyric.Word{{Start: 0, End: 11}}}, SourceLine: 9},
		{Line: lyric.Line{Start: 1, End: 10, Words: []lyric.Word{{Start: 20, End: 20}}}, SourceLine: 9},
		{Line: lyric.Line{Start: 0, End: 10, Words: []lyric.Word{{Start: 3, End: 3}, {Start: 1, End: 4}}}, SourceLine: 9},
	} {
		p, _ := New(context.Background(), nil, common.Options{})
		result, err := p.Finish([]Line{line}, nil, 0)
		if err == nil || result.Document != nil || !strings.Contains(err.Error(), "9:1") {
			t.Fatalf("invalid interval should locate source line: %v", err)
		}
	}
}
