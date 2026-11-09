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
				"[0,1000]untimed text", "[00:01.00]not LRC", "<QrcInfos/>", "{\"content\":\"lyrics\"}",
				"[0,1000", "[0,1000,0]", "[-1,1000]", "[+1,1000]", "[0,-1]", "[0,0]",
				"[ti:value]trailing text", "[0,1000](invalid)", "[offset:bad]", "[offset:9223372036854775807]",
				parser.line("9223372036854,1000", "overflow"), parser.line("9223372036855,1", "overflow"),
				"[offset:-1]\n" + parser.line("0,1000", "negative offset"),
				"[offset:9223372036854]\n" + parser.line("0,1000", "offset overflow"),
			}
			if parser.name == "yrc" {
				inputs = append(inputs, "[0,1000](0,1000)missing flags", "[0,1000](0,1000,0", "[0,1000](0,1000,0,0)x", "[0,1000](0,1001,0)outside")
			} else {
				inputs = append(inputs, "[0,1000]x(0,1000,0)", "[0,1000]x(0,1000", "[0,1000]x(0,1000)tail", "[0,1000]x(0,1001)")
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

func TestYRCFlagsAndDiagnosticLimit(t *testing.T) {
	input := "[0,1000](0,400,1)a(400,600,2)b"
	result, err := yrc.Parse(context.Background(), []byte(input), common.Options{})
	if err != nil || len(result.Diagnostics) != 2 {
		t.Fatalf("flags: %+v, %v", result, err)
	}
	for _, options := range []common.Options{{Strict: true}, {Limits: common.Limits{MaxDiagnostics: 1}}} {
		result, err := yrc.Parse(context.Background(), []byte(input), options)
		if err == nil || result.Document != nil {
			t.Fatalf("warning policy ignored: %+v", options)
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
			if err != nil || result.Document.Line(0).WordCount() != 1 || result.Document.Line(0).Text() != "zerofull" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "zero-word" {
				t.Fatalf("zero at origin: %+v, %v", result, err)
			}
			if _, err := parser.parse(context.Background(), []byte(zero), common.Options{Strict: true}); err == nil {
				t.Fatal("strict mode accepted zero-duration timing")
			}
			if _, err := parser.parse(context.Background(), []byte(reversed), common.Options{}); err == nil {
				t.Fatal("unordered word times silently sorted")
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
			malformed := "[0,1000](bad)text"
			if parser.name == "qrc" {
				malformed = "[0,1000]中(bad)"
			}
			for _, prefix := range []string{"", "\ufeff", "\ufeff[ti:head]\r\n"} {
				_, err := parser.parse(context.Background(), []byte(prefix+malformed), common.Options{})
				var located *common.ParseError
				if !errors.As(err, &located) {
					t.Fatalf("missing source diagnostic: %v", err)
				}
				wantLine, wantColumn := 1, strings.Index(malformed, "bad")+1
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
			zero := "[0,1000](0,0,0)zero(0,1000,0)word"
			if parser.name == "qrc" {
				zero = "[0,1000]zero(0,0)word(0,1000)"
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
