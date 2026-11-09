package lrc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
)

func TestGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/synthetic.lrc")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/synthetic.golden")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse(context.Background(), data, common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for i := 0; i < result.Document.PropertyCount(); i++ {
		property := result.Document.Property(i)
		fmt.Fprintf(&got, "%s=%s\n", property.Name, property.Value)
	}
	for i := 0; i < result.Document.Len(); i++ {
		line := result.Document.Line(i)
		fmt.Fprintf(&got, "%s [%d,%d) %q\n", line.ID(), line.Start().Milliseconds(), line.End().Milliseconds(), line.Text())
		for j := 0; j < line.WordCount(); j++ {
			word := line.Word(j)
			fmt.Fprintf(&got, "  [%d,%d) %q\n", word.Start.Milliseconds(), word.End.Milliseconds(), word.Text)
		}
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(&got, "diagnostic %s\n", diagnostic.Code)
	}
	if got.String() != string(want) {
		t.Fatalf("golden mismatch\ngot:\n%s\nwant:\n%s", got.String(), want)
	}
	// The caller may immediately recycle input storage after parsing.
	for i := range data {
		data[i] = 0
	}
	if result.Document.Line(1).Text() != "山 and 👩‍💻" {
		t.Fatal("document retains mutable input bytes")
	}
}

func TestLineEndingsEncodingAndEmpty(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		t.Run(fmt.Sprintf("%q", ending), func(t *testing.T) {
			input := "\ufeff[00:00.1]中文" + ending + "[00:01.25]e\u0301 👩‍💻" + ending
			result, err := Parse(context.Background(), []byte(input), common.Options{Duration: 2 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if result.Document.Len() != 2 || result.Document.Line(0).Start() != 100*time.Millisecond || result.Document.Line(1).Text() != "e\u0301 👩‍💻" {
				t.Fatalf("unexpected normalized lines: %#v", result)
			}
		})
	}
	for _, input := range []string{"", "\ufeff", "\n\r\n\t", "[ti:Empty]"} {
		result, err := Parse(context.Background(), []byte(input), common.Options{})
		if err != nil || result.Document == nil || result.Document.Len() != 0 {
			t.Fatalf("empty %q: result=%#v err=%v", input, result, err)
		}
	}
	for _, input := range [][]byte{{0xff}, {0xff, 0xfe, 0x5b, 0}, []byte("[00:00]A\n\xc3\x28")} {
		result, err := Parse(context.Background(), input, common.Options{})
		if err == nil || result.Document != nil {
			t.Fatalf("accepted malformed UTF-8 %q", input)
		}
	}
}

func TestBOMSourceColumns(t *testing.T) {
	for _, tc := range []struct {
		input  string
		code   string
		column int
	}{
		{"\ufeff[bad", "invalid-tag", 4},
		{"\ufeff  untimed", "untimed-line", 6},
		{"\ufeff[ti:x]tail", "metadata-tail", 10},
		{"\ufeff[00:60]x", "invalid-timestamp", 4},
		{"\ufeff[00:01]<oops>", "invalid-word-timestamp", 11},
	} {
		result, err := Parse(context.Background(), []byte(tc.input), common.Options{Duration: 2 * time.Second})
		if err != nil || len(result.Diagnostics) != 1 {
			t.Fatalf("%q: result=%+v err=%v", tc.input, result, err)
		}
		diagnostic := result.Diagnostics[0]
		if diagnostic.Code != tc.code || diagnostic.Line != 1 || diagnostic.Column != tc.column {
			t.Fatalf("%q: got %+v, want %s at 1:%d", tc.input, diagnostic, tc.code, tc.column)
		}
		_, err = Parse(context.Background(), []byte(tc.input), common.Options{Strict: true, Duration: 2 * time.Second})
		var located *common.ParseError
		if !errors.As(err, &located) || located.Line != 1 || located.Column != tc.column {
			t.Fatalf("strict %q: expected source column %d, got %v", tc.input, tc.column, err)
		}
	}
	_, err := Parse(context.Background(), []byte("\ufeff[offset:bad]"), common.Options{})
	var located *common.ParseError
	if !errors.As(err, &located) || located.Column != 4 {
		t.Fatalf("invalid offset should locate the source tag after the BOM: %v", err)
	}
	_, err = Parse(context.Background(), []byte("\ufeff[00:01][00:02]x"), common.Options{Limits: common.Limits{MaxLines: 1}})
	if !errors.As(err, &located) || located.Column != 11 {
		t.Fatalf("repeated timestamp should count BOM bytes in its source column: %v", err)
	}
	result, err := Parse(context.Background(), []byte("\ufeff\nuntimed"), common.Options{})
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Line != 2 || result.Diagnostics[0].Column != 1 {
		t.Fatalf("BOM offset leaked into later lines: %+v %v", result, err)
	}
}

func TestDuplicateStartsAndZeroWords(t *testing.T) {
	input := "[00:00]<00:00>A<00:00>B<00:01>\n[00:00]another voice"
	result, err := Parse(context.Background(), []byte(input), common.Options{Duration: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 2 || result.Document.Line(0).Text() != "AB" || result.Document.Line(0).WordCount() != 1 || result.Document.Line(0).Word(0).Text != "B" {
		t.Fatalf("zero-duration normalization lost text: %#v", result)
	}
	if result.Document.Line(0).ID() == result.Document.Line(1).ID() || result.Document.Line(1).Text() != "another voice" {
		t.Fatal("equal starts must retain distinct line identities and text")
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "zero-word" {
		t.Fatalf("expected zero-word diagnostic, got %#v", result.Diagnostics)
	}
	if result.Document.Line(0).Duet() || result.Document.Line(1).Background() {
		t.Fatal("LRC must not invent voice roles from duplicate timestamps")
	}
}

func TestOffsetAndFinalLine(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		options     common.Options
		start, end  time.Duration
		codes       []string
	}{
		{"negative", "[offset:-100]\n[00:01]a", common.Options{}, 900 * time.Millisecond, 5900 * time.Millisecond, []string{"inferred-end"}},
		{"positive", "[offset:+100]\n[00:01]a", common.Options{Duration: 3 * time.Second}, 1100 * time.Millisecond, 3 * time.Second, nil},
		{"duplicate", "[offset:100]\n[offset:200]\n[00:01]a", common.Options{Duration: 3 * time.Second}, 1200 * time.Millisecond, 3 * time.Second, []string{"duplicate-offset"}},
		{"duration", "[00:01]last", common.Options{Duration: 6 * time.Second}, time.Second, 6 * time.Second, nil},
		{"fallback", "[00:01]last", common.Options{FinalLineDuration: 2 * time.Second}, time.Second, 3 * time.Second, []string{"inferred-end"}},
		{"terminal", "[00:01]<00:01>last<00:03>", common.Options{Strict: true}, time.Second, 3 * time.Second, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(context.Background(), []byte(tc.input), tc.options)
			if err != nil {
				t.Fatal(err)
			}
			line := result.Document.Line(0)
			if line.Start() != tc.start || line.End() != tc.end {
				t.Fatalf("got [%v,%v), want [%v,%v)", line.Start(), line.End(), tc.start, tc.end)
			}
			if len(result.Diagnostics) != len(tc.codes) {
				t.Fatalf("diagnostics=%#v, want codes %v", result.Diagnostics, tc.codes)
			}
			for i, code := range tc.codes {
				if result.Diagnostics[i].Code != code {
					t.Fatalf("diagnostic %d: got %#v want %s", i, result.Diagnostics[i], code)
				}
			}
		})
	}
}

func TestErrorsAndStrictMode(t *testing.T) {
	for _, input := range []string{
		"[offset:9223372036854775807]\n[00:01]x",
		"[offset:-1001]\n[00:01]x",
		"[offset:10]\n[153722867:16.854]x",
		"[00:00]<00:00>x<00:00>",
		"[00:02]<00:01>x<00:03>",
		"[00:01]<00:03>x<00:02>",
		"[00:01][00:00]<00:00>x<00:00.5>",
		"[00:01]<00:01>x<00:02><00:01.5>y<00:03>",
	} {
		result, err := Parse(context.Background(), []byte(input), common.Options{Duration: 4 * time.Second})
		if err == nil || result.Document != nil {
			t.Fatalf("expected error for %q, got %#v, %v", input, result, err)
		}
	}
	for _, input := range []string{
		"untimed", "[00:60]x", "[00:01.1234]x", "[999999999999999999:00]x", "[00:1]x", "[00:01]x",
		"[00:02]second\n[00:01]first", "[00:00]<00:00>A<00:00>B<00:01>", "[00:01]<oops>x",
	} {
		result, err := Parse(context.Background(), []byte(input), common.Options{Strict: true})
		if err == nil || result.Document != nil {
			t.Fatalf("strict accepted %q", input)
		}
	}
	// Ordinary angle brackets remain visible if they are not a supported timing.
	result, err := Parse(context.Background(), []byte("[00:01]a < b"), common.Options{Duration: 2 * time.Second})
	if err != nil || result.Document.Line(0).Text() != "a < b" || len(result.Diagnostics) != 1 {
		t.Fatalf("malformed enhanced fallback: %#v %v", result, err)
	}
}

func TestLimitsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		limits      common.Limits
	}{
		{"bytes", "[00:01]x", common.Limits{MaxBytes: 3}},
		{"physical-lines", "[ti:x]\n[00:01]x", common.Limits{MaxLines: 1}},
		{"repeated-lines", "[00:01][00:02]x", common.Limits{MaxLines: 1}},
		{"words", "[00:00]<00:00>a<00:01>b<00:02>", common.Limits{MaxWords: 1}},
		{"repeated-words", "[00:00][00:03]<00:00>a<00:01>", common.Limits{MaxWords: 1}},
		{"diagnostics", "bad\nbad", common.Limits{MaxDiagnostics: 1}},
		{"negative", "", common.Limits{MaxDepth: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(context.Background(), []byte(tc.input), common.Options{Limits: tc.limits})
			if err == nil || result.Document != nil {
				t.Fatalf("limit not enforced: %#v %v", result, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Parse(ctx, []byte("[00:01]x"), common.Options{})
	if !errors.Is(err, context.Canceled) || result.Document != nil {
		t.Fatalf("cancelled parse returned %#v %v", result, err)
	}
	// A deterministic context avoids relying on timers or scheduler speed when
	// exercising cancellation after scanning has already started.
	ctxDuring := &cancelAfterChecks{Context: context.Background(), remaining: 3}
	large := []byte("[00:00]" + strings.Repeat("歌", 100000))
	result, err = Parse(ctxDuring, large, common.Options{})
	if !errors.Is(err, context.Canceled) || result.Document != nil {
		t.Fatalf("in-flight cancellation returned %#v %v", result, err)
	}
	// Exactly one physical source line plus a final newline fits MaxLines=1.
	if _, err := Parse(context.Background(), []byte("[00:01]x\n"), common.Options{Limits: common.Limits{MaxLines: 1}, Duration: 2 * time.Second}); err != nil {
		t.Fatal(err)
	}
}

type cancelAfterChecks struct {
	context.Context
	remaining int
}

func (c *cancelAfterChecks) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestEmptyEnhancedMarkersDoNotExtendPreviousWord(t *testing.T) {
	result, err := Parse(context.Background(), []byte("[00:01]<00:01>x<00:02><00:03>"), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if line.End() != 3*time.Second || line.WordCount() != 1 || line.Word(0).End != 2*time.Second {
		t.Fatalf("an empty timing marker extended a completed word: %#v", line.Word(0))
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"", "[00:01]x", "[00:00]<00:00>A<00:00>B<00:01>", "\ufeff[ti:合成]\r\n[offset:-100]\r[00:01][00:02]x", "[00:00]a < b"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		result, err := Parse(context.Background(), data, common.Options{Limits: common.Limits{MaxBytes: 16384, MaxLines: 256, MaxWords: 1024, MaxDiagnostics: 64}})
		if err != nil {
			if result.Document != nil {
				t.Fatal("failed parse returned a partial document")
			}
			return
		}
		if result.Document == nil {
			t.Fatal("successful parse returned no document")
		}
		ids := make(map[string]bool)
		for i := 0; i < result.Document.Len(); i++ {
			line := result.Document.Line(i)
			if line.ID() == "" || ids[line.ID()] || line.Start() < 0 || line.End() <= line.Start() {
				t.Fatal("document violated line invariants")
			}
			ids[line.ID()] = true
		}
	})
}
