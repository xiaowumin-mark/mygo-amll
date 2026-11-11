package timedtext_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/format/qrc"
	"github.com/xiaowumin-mark/mygo-amll/format/yrc"
)

type parserCase struct {
	name  string
	parse func(context.Context, []byte, common.Options) (common.Result, error)
	line  func(string, string) string
}

var parsers = []parserCase{
	{"yrc", yrc.Parse, func(header, text string) string { return "[" + header + "](0,1000,0)" + text }},
	{"qrc", qrc.Parse, func(header, text string) string { return "[" + header + "]" + text + "(0,1000)" }},
}

func TestEncodingNewlinesAndEmpty(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			for _, input := range []string{"", "\ufeff", "\n\r\n\r"} {
				result, err := parser.parse(context.Background(), []byte(input), common.Options{})
				if err != nil || result.Document == nil || result.Document.Len() != 0 {
					t.Fatalf("empty input: result=%+v err=%v", result, err)
				}
			}
			for _, newline := range []string{"\n", "\r\n", "\r"} {
				input := "\ufeff[ti:Example]" + newline + parser.line("0,1000", "e\u0301✨")
				result, err := parser.parse(context.Background(), []byte(input), common.Options{Strict: true})
				if err != nil || result.Document.Line(0).Text() != "e\u0301✨" {
					t.Fatalf("newline=%q: result=%+v err=%v", newline, result, err)
				}
			}
			if result, err := parser.parse(context.Background(), []byte{0xff}, common.Options{}); err == nil || result.Document != nil {
				t.Fatal("invalid UTF-8 accepted")
			}
		})
	}
}

func TestMalformedAndUnsupportedNeverDegrade(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			inputs := []string{
				"[00:01.00]not LRC", "<QrcInfos/>", "{\"content\":\"lyrics\"}",
				"[0,1000", "[0,1000,0]", "[-1,1000]", "[+1,1000]", "[0,-1]",
				"[ti:value]trailing text", "[offset:bad]", "[offset:9223372036854775807]",
				parser.line("9223372036854,1000", "overflow"), parser.line("9223372036855,1", "overflow"),
				"[offset:-1]\n" + parser.line("0,1000", "negative offset"),
				"[offset:9223372036854]\n" + parser.line("0,1000", "offset overflow"),
			}
			for _, input := range inputs {
				for _, strict := range []bool{false, true} {
					result, err := parser.parse(context.Background(), []byte(input), common.Options{Strict: strict})
					if err == nil || result.Document != nil {
						t.Fatalf("strict=%v accepted %q: %+v, %v", strict, input, result, err)
					}
					var located *common.ParseError
					if !errors.As(err, &located) || located.Line < 1 || located.Column < 1 {
						t.Fatalf("unlocated error for %q: %v", input, err)
					}
				}
			}
		})
	}
}

func TestQRCMatchesAMLLRegexAndBackgroundWords(t *testing.T) {
	input := " \t[0,20]  A(x)B(0,10) C(10,10) ignored trailing words \ufeff "
	result, err := qrc.Parse(context.Background(), []byte(input), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 1 {
		t.Fatalf("document lines=%d, want 1", result.Document.Len())
	}
	line := result.Document.Line(0)
	if line.Text() != "A(x)B C" || line.WordCount() != 2 || line.Word(0).Text != "A(x)B" || line.Word(1).Text != " C" {
		t.Fatalf("regex-compatible words: text=%q count=%d words=%q/%q", line.Text(), line.WordCount(), line.Word(0).Text, line.Word(1).Text)
	}

	background, err := qrc.Parse(context.Background(), []byte("[0,20]（lead(0,10) tail）(10,10)"), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	bg := background.Document.Line(0)
	if !bg.Background() || bg.Text() != "lead tail" || bg.Word(0).Text != "lead" || bg.Word(1).Text != " tail" {
		t.Fatalf("background wrapper: background=%v text=%q words=%q/%q", bg.Background(), bg.Text(), bg.Word(0).Text, bg.Word(1).Text)
	}

	mixed, err := qrc.Parse(context.Background(), []byte("[0,20](left(0,10) right)(10,10)"), common.Options{})
	if err != nil || !mixed.Document.Line(0).Background() || mixed.Document.Line(0).Text() != "left right" {
		t.Fatalf("mixed ordinary/fullwidth wrappers should match AMLL: result=%+v err=%v", mixed, err)
	}
}

func TestQRCFiltersEmptyAndUntimedTailLikeAMLL(t *testing.T) {
	input := "[0,10] \t\n[10,20]plain text\n[30,20]x(30,10) untimed trailing text (also ignored)"
	result, err := qrc.Parse(context.Background(), []byte(input), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 2 {
		t.Fatalf("document lines=%d, want 2", result.Document.Len())
	}
	empty := result.Document.Line(0)
	if empty.Start() != 10*time.Millisecond || empty.Text() != "" || empty.WordCount() != 0 {
		t.Fatalf("nonempty untimed line should preserve AMLL's empty line result: start=%v text=%q words=%d", empty.Start(), empty.Text(), empty.WordCount())
	}
	word := result.Document.Line(1)
	if word.Start() != 30*time.Millisecond || word.Text() != "x" || word.WordCount() != 1 {
		t.Fatalf("trailing untimed text was not ignored: start=%v text=%q words=%d", word.Start(), word.Text(), word.WordCount())
	}
}

func TestQRCOrdinaryParenthesesBeforeTimingAndMalformedOnlyPayloads(t *testing.T) {
	input := "[0,20]A(x)B(0,10) C(10,10)"
	result, err := qrc.Parse(context.Background(), []byte(input), common.Options{})
	if err != nil || result.Document.Line(0).Text() != "A(x)B C" {
		t.Fatalf("ordinary parentheses before a later valid marker: result=%+v err=%v", result, err)
	}
	for _, input := range []string{"[0,20]x(0,10", "[0,20](invalid)"} {
		result, err := qrc.Parse(context.Background(), []byte(input), common.Options{})
		if err != nil || result.Document.Len() != 1 || result.Document.Line(0).WordCount() != 0 || result.Document.Line(0).Text() != "" {
			t.Fatalf("malformed-only payload should produce AMLL's empty-word line: %q result=%+v err=%v", input, result, err)
		}
	}
}

func TestYRCBackgroundParentheses(t *testing.T) {
	result, err := yrc.Parse(context.Background(), []byte("[100,100](100,50,0)（echo(150,50,0)）"), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if !line.Background() || line.Text() != "echo" {
		t.Fatalf("background wrappers: background=%v text=%q", line.Background(), line.Text())
	}
}

func TestOffsetSortingAndStrictWarnings(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			var second string
			if parser.name == "yrc" {
				second = "[1000,1000](1000,1000,0)later"
			} else {
				second = "[1000,1000]later(1000,1000)"
			}
			input := "[offset:5]\n[custom:retained]\n" + second + "\n" + parser.line("0,1000", "first") + "\n[offset:10]"
			result, err := parser.parse(context.Background(), []byte(input), common.Options{})
			if err != nil || len(result.Diagnostics) != 2 || result.Document.Line(0).Text() != "first" || result.Document.Line(0).Start() != 10*time.Millisecond || result.Document.Line(1).Start() != 1010*time.Millisecond {
				t.Fatalf("sorting/offset: result=%+v err=%v", result, err)
			}
			if result, err := parser.parse(context.Background(), []byte(input), common.Options{Strict: true}); err == nil || result.Document != nil {
				t.Fatal("strict mode accepted duplicate offset/sorting warning")
			}
		})
	}
}

func TestLimitsAndCanceledInput(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			line := parser.line("0,1000", "x")
			for _, options := range []common.Options{
				{Limits: common.Limits{MaxBytes: len(line) - 1}},
				{Limits: common.Limits{MaxLines: 1}},
				{Limits: common.Limits{MaxWords: 1}},
				{Limits: common.Limits{MaxBytes: -1}},
			} {
				input := line
				if options.Limits.MaxLines == 1 || options.Limits.MaxWords == 1 {
					input += "\n" + line
				}
				result, err := parser.parse(context.Background(), []byte(input), options)
				if err == nil || result.Document != nil {
					t.Fatalf("limit was ignored: %+v", options)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			result, err := parser.parse(ctx, []byte(strings.Repeat(line+"\n", 10000)), common.Options{})
			if !errors.Is(err, context.Canceled) || result.Document != nil {
				t.Fatalf("cancellation lost: %+v, %v", result, err)
			}
			if _, err := parser.parse(nil, []byte(line), common.Options{}); err == nil {
				t.Fatal("nil context accepted")
			}
		})
	}
}

func TestYRCLazyFlagsZeroMatchingAndTrim(t *testing.T) {
	input := " \t[0,100] (0,40,0)a(bad)(40,20,1)ignored(40,60,0)b \t"
	result, err := yrc.Parse(context.Background(), []byte(input), common.Options{})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("regex-compatible YRC input: %+v, %v", result, err)
	}
	line := result.Document.Line(0)
	if line.Text() != "a(bad)(40,20,1)ignoredb" || line.WordCount() != 2 || line.Word(0).Text != "a(bad)(40,20,1)ignored" || line.Word(1).Text != "b" {
		t.Fatalf("lazy flags-0 token matching: text=%q words=%d first=%q second=%q", line.Text(), line.WordCount(), line.Word(0).Text, line.Word(1).Text)
	}

	noSupportedFlags, err := yrc.Parse(context.Background(), []byte("[0,100](0,100,1)unmatched"), common.Options{})
	if err != nil || noSupportedFlags.Document.Len() != 1 || noSupportedFlags.Document.Line(0).WordCount() != 0 || len(noSupportedFlags.Diagnostics) != 0 {
		t.Fatalf("nonzero YRC flags should not match AMLL's token regex: %+v, %v", noSupportedFlags, err)
	}

	for _, input := range []string{"[0,100] \t", " \t[0,100](0,100,0)word \t"} {
		trimmed, err := yrc.Parse(context.Background(), []byte(input), common.Options{})
		if err != nil {
			t.Fatalf("trimmed YRC %q: %v", input, err)
		}
		if input == "[0,100] \t" {
			if trimmed.Document.Len() != 0 {
				t.Fatalf("empty YRC payload should be skipped, got %d lines", trimmed.Document.Len())
			}
		} else if trimmed.Document.Len() != 1 || trimmed.Document.Line(0).Text() != "word" {
			t.Fatalf("whole-line and payload trim did not match AMLL: %+v", trimmed.Document)
		}
	}
}

func TestZeroAtOriginAndWordOrdering(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			var zero, reversed string
			if parser.name == "yrc" {
				zero = "[0,1000](0,0,0)zero(0,1000,0)full"
				reversed = "[0,1000](500,500,0)b(0,500,0)a"
			} else {
				zero = "[0,1000]zero(0,0)full(0,1000)"
				reversed = "[0,1000]b(500,500)a(0,500)"
			}
			result, err := parser.parse(context.Background(), []byte(zero), common.Options{})
			if err != nil || result.Document.Line(0).WordCount() != 2 || result.Document.Line(0).Text() != "zerofull" || len(result.Diagnostics) != 0 || result.Document.Line(0).Word(0).Start != 0 || result.Document.Line(0).Word(0).End != 0 {
				t.Fatalf("zero at origin: %+v, %v", result, err)
			}
			if _, err := parser.parse(context.Background(), []byte(zero), common.Options{Strict: true}); err != nil {
				t.Fatalf("source point timing is supported in strict mode: %v", err)
			}
			result, err = parser.parse(context.Background(), []byte(reversed), common.Options{})
			if err != nil || result.Document.Line(0).WordCount() != 2 || result.Document.Line(0).Word(0).Text != "b" || result.Document.Line(0).Word(0).Start != 500*time.Millisecond || result.Document.Line(0).Word(1).Text != "a" || result.Document.Line(0).Word(1).Start != 0 {
				t.Fatalf("source-order words were sorted, dropped or rejected: %+v %v", result, err)
			}
		})
	}
}

func TestGeneratedZeroTimedSpacesRemainTextAndSourceTiming(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			input := "[3000,1000](3000,500,0)A(0,0,0) (3500,500,0)B"
			if parser.name == "qrc" {
				input = "[3000,1000]A(3000,500) (0,0)B(3500,500)"
			}
			result, err := parser.parse(context.Background(), []byte(input), common.Options{Strict: true})
			if err != nil {
				t.Fatal(err)
			}
			line := result.Document.Line(0)
			if line.Text() != "A B" || line.WordCount() != 3 || line.Word(0).Text != "A" || line.Word(0).Start != 3*time.Second || line.Word(0).End != 3500*time.Millisecond || line.Word(1).Text != " " || line.Word(1).Start != 0 || line.Word(1).End != 0 || line.Word(2).Text != "B" || line.Word(2).Start != 3500*time.Millisecond || line.Word(2).End != 4*time.Second {
				t.Fatalf("generated zero-timed separator was changed: text=%q words=%d", line.Text(), line.WordCount())
			}
		})
	}
}

type cancelOnCheck struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (ctx *cancelOnCheck) Err() error {
	ctx.checks++
	if ctx.checks == 20 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestCancellationDuringParsingAndInputOwnership(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &cancelOnCheck{Context: base, cancel: cancel}
			input := []byte(strings.Repeat(parser.line("0,1000", "unchanged")+"\n", 1000))
			result, err := parser.parse(ctx, input, common.Options{})
			if !errors.Is(err, context.Canceled) || result.Document != nil {
				t.Fatalf("mid-parse cancellation lost: %+v, %v", result, err)
			}
			input = []byte(parser.line("0,1000", "unchanged"))
			result, err = parser.parse(context.Background(), input, common.Options{})
			if err != nil {
				t.Fatal(err)
			}
			clear(input)
			line := result.Document.Line(0)
			if line.Text() != "unchanged" || line.Word(0).Text != "unchanged" || line.ID() != parser.name+":1" {
				t.Fatal("result aliased input or lost source identity")
			}
		})
	}
}

func TestOriginalByteColumnsAfterBOM(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			malformed := "[0,-1]text"
			for _, prefix := range []string{"", "\ufeff", "\ufeff[ti:head]\r\n"} {
				_, err := parser.parse(context.Background(), []byte(prefix+malformed), common.Options{})
				var located *common.ParseError
				if !errors.As(err, &located) {
					t.Fatalf("missing source diagnostic: %v", err)
				}
				wantLine, wantColumn := 1, 2
				if prefix == "\ufeff" {
					wantColumn += 3
				} else if prefix != "" {
					wantLine = 2
				}
				if located.Line != wantLine || located.Column != wantColumn {
					t.Fatalf("prefix %q: location=%d:%d, want %d:%d", prefix, located.Line, located.Column, wantLine, wantColumn)
				}
			}
			_, err := parser.parse(context.Background(), []byte("\ufeff\xff"), common.Options{})
			var located *common.ParseError
			if !errors.As(err, &located) || located.Column != 4 {
				t.Fatalf("UTF-8 validation column was adjusted twice: %v", err)
			}
			zero := "[1000,1000](1000,1000,0)later\n[0,1000](0,1000,0)first"
			if parser.name == "qrc" {
				zero = "[1000,1000]later(1000,1000)\n[0,1000]first(0,1000)"
			}
			result, err := parser.parse(context.Background(), []byte("\ufeff"+zero), common.Options{})
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Line != 1 || result.Diagnostics[0].Column != 4 {
				t.Fatalf("normalized warning omitted BOM byte base: %+v, %v", result, err)
			}
			_, err = parser.parse(context.Background(), []byte("\ufeff"+zero), common.Options{Strict: true})
			if !errors.As(err, &located) || located.Column != 4 {
				t.Fatalf("strict normalization diagnostic omitted BOM byte base: %v", err)
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	for _, parser := range parsers {
		b.Run(parser.name, func(b *testing.B) {
			var input strings.Builder
			for i := range 1000 {
				if parser.name == "yrc" {
					fmt.Fprintf(&input, "[%d,1000](%d,1000,0)合成\n", i*1000, i*1000)
				} else {
					fmt.Fprintf(&input, "[%d,1000]合成(%d,1000)\n", i*1000, i*1000)
				}
			}
			data := []byte(input.String())
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for range b.N {
				if _, err := parser.parse(context.Background(), data, common.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
