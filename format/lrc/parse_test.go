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
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/synthetic.lrc")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`ti=Synthetic fixture
ar=Example author
offset=100
lrc:5:0 [1000,3000) "Opening"
  [1000,3000) "Opening"
lrc:4:0 [3000,4000) "山 and 👩\u200d💻"
  translation "Second voice"
  [3000,3500) "山"
  [3500,4000) " and 👩\u200d💻"
lrc:7:0 [9500,59999999) "Ending"
  [9500,59999999) "Ending"
diagnostic sorted
`)
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
		if line.Translation() != "" {
			fmt.Fprintf(&got, "  translation %q\n", line.Translation())
		}
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
		{"\ufeff[00:xx]x", "invalid-timestamp", 4},
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
	result, err := Parse(context.Background(), []byte("\ufeff[offset:bad]\n[00:01]line"), common.Options{Duration: 3 * time.Second})
	if err != nil || result.Document.Line(0).Start() != time.Second || result.Document.Property(0).Value != "bad" {
		t.Fatalf("LRC offset must remain AMLL metadata rather than shift timestamps: %+v %v", result, err)
	}
	var located *common.ParseError
	_, err = Parse(context.Background(), []byte("\ufeff[00:01][00:02]x"), common.Options{Limits: common.Limits{MaxLines: 1}})
	if !errors.As(err, &located) || located.Column != 11 {
		t.Fatalf("repeated timestamp should count BOM bytes in its source column: %v", err)
	}
	result, err = Parse(context.Background(), []byte("\ufeff\nuntimed"), common.Options{})
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
	if result.Document.Len() != 1 || result.Document.Line(0).Text() != "AB" || result.Document.Line(0).WordCount() != 2 || result.Document.Line(0).Word(0).Text != "A" || result.Document.Line(0).Word(0).Start != 0 || result.Document.Line(0).Word(0).End != 0 || result.Document.Line(0).Word(1).Text != "B" {
		t.Fatalf("zero-duration normalization lost text: %#v", result)
	}
	if result.Document.Line(0).Translation() != "another voice" {
		t.Fatal("same-start LRC text should attach as translation to the first matching row")
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("source point timing must be retained without cleanup: %#v", result.Diagnostics)
	}
	if result.Document.Line(0).Duet() || result.Document.Line(0).Background() {
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
		{"negative-metadata", "[offset:-100]\n[00:01]a", common.Options{}, time.Second, 59_999*time.Second + 999*time.Millisecond, nil},
		{"positive-metadata", "[offset:+100]\n[00:01]a", common.Options{Duration: 3 * time.Second}, time.Second, 3 * time.Second, nil},
		{"duplicate-metadata", "[offset:100]\n[offset:200]\n[00:01]a", common.Options{Duration: 3 * time.Second}, time.Second, 3 * time.Second, nil},
		{"duration", "[00:01]last", common.Options{Duration: 6 * time.Second}, time.Second, 6 * time.Second, nil},
		{"amll-plain-sentinel", "[00:01]last", common.Options{}, time.Second, 59_999*time.Second + 999*time.Millisecond, nil},
		{"terminal", "[00:01]<00:01>last<00:03>", common.Options{Strict: true}, time.Second, 3 * time.Second, nil},
		{"word-sync-final-one-second", "[00:01]<00:01>A<00:01.5>B", common.Options{}, time.Second, 2500 * time.Millisecond, nil},
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

func TestAMLLDefaultSPLValidSemantics(t *testing.T) {
	input := strings.Join([]string{
		"[TI:  Title  ]",
		"[ti:Title]", // case-normalized duplicate metadata value
		"[Ar: Author ]",
		"[empty:]",      // empty metadata values are skipped
		"[offset:-500]", // metadata only; AMLL does not shift parsed LRC time
		"# comment",
		"// comment",
		"[00:01][00:02][00:02]Main", // duplicate start times produce unique rows
		"Implicit translation",      // attach to the whole previous row group
		"[00:01]Timed translation",  // same-start translation targets first match
		"[00:03]",                   // explicit end applies to the group's last row
		"[00:04](echo)",             // full-line parentheses signal background vocal
		"[00:05]<00:05.123456>first[00:06:654321]second[00:07]",
		"[00:08]closing",
		"[00:10]<00:10>first<00:10.5>last", // final word-sync adds one second
	}, "\n")
	result, err := Parse(context.Background(), []byte(input), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 6 {
		t.Fatalf("lines=%d, want 6: %+v", result.Document.Len(), result.Diagnostics)
	}
	first, second := result.Document.Line(0), result.Document.Line(1)
	if first.Start() != time.Second || first.Text() != "Main" || first.Translation() != "Implicit translation\nTimed translation" {
		t.Fatalf("first repeated-start row: text=%q translation=%q start=%v", first.Text(), first.Translation(), first.Start())
	}
	if second.Start() != 2*time.Second || second.End() != 3*time.Second || second.Translation() != "Implicit translation" {
		t.Fatalf("second repeated-start row lost group translation/end: %+v", second)
	}
	background := result.Document.Line(2)
	if !background.Background() || background.Text() != "echo" || background.Word(0).Text != "echo" {
		t.Fatalf("full-line parentheses were not normalized as background vocal: %+v", background)
	}
	wordSync := result.Document.Line(3)
	if wordSync.Text() != "firstsecond" || wordSync.WordCount() != 2 || wordSync.Word(0).Start != 5*time.Second+123*time.Millisecond || wordSync.Word(0).End != 6*time.Second+654*time.Millisecond || wordSync.Word(1).End != 7*time.Second {
		t.Fatalf("square-bracket/precision word tokens diverged: %+v", wordSync)
	}
	plainFinal := result.Document.Line(4)
	if plainFinal.End() != 10*time.Second {
		t.Fatalf("next row should close preceding plain line, got [%v,%v)", plainFinal.Start(), plainFinal.End())
	}
	wordFinal := result.Document.Line(5)
	if wordFinal.End() != 11*time.Second+500*time.Millisecond || wordFinal.Word(1).End != wordFinal.End() {
		t.Fatalf("final word-synced line should end one second after its last word begins: [%v,%v)", wordFinal.Start(), wordFinal.End())
	}
	if result.Document.PropertyCount() != 3 {
		t.Fatalf("metadata key/value normalization or dedup failed: count=%d", result.Document.PropertyCount())
	}
	wantMetadata := []lyric.Property{{Name: "ti", Value: "Title"}, {Name: "ar", Value: "Author"}, {Name: "offset", Value: "-500"}}
	for i, want := range wantMetadata {
		if got := result.Document.Property(i); got != want {
			t.Errorf("metadata[%d]=%+v, want %+v", i, got, want)
		}
	}
}

func TestAMLLTimestampWidthsAndFractionTruncation(t *testing.T) {
	result, err := Parse(context.Background(), []byte("[0:1.123456]short\n[000:02]next\n[0:60]sixty"), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second + 123*time.Millisecond, 2 * time.Second, 60 * time.Second}
	if result.Document.Len() != len(want) {
		t.Fatalf("AMLL minute/second widths or fractional truncation differ: %+v", result)
	}
	for i, at := range want {
		if result.Document.Line(i).Start() != at {
			t.Errorf("line %d start=%v; want %v", i, result.Document.Line(i).Start(), at)
		}
	}
}

func TestAMLLScannerResumesAfterMalformedBracketText(t *testing.T) {
	result, err := Parse(context.Background(), []byte("[00:01]A<bad<00:02>B<00:03>"), common.Options{Duration: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if line.Text() != "A<badB" || line.WordCount() != 2 || line.Word(0).Text != "A<bad" || line.Word(0).Start != time.Second || line.Word(0).End != 2*time.Second || line.Word(1).Text != "B" || line.Word(1).Start != 2*time.Second || line.Word(1).End != 3*time.Second {
		t.Fatalf("scanner did not resume at the later regex-style timestamp: line=%q words=%+v", line.Text(), []lyric.Word{line.Word(0), line.Word(1)})
	}
}

func TestAMLLFinalInferenceAppliesOnlyToLastEqualStartLine(t *testing.T) {
	result, err := Parse(context.Background(), []byte("[00:01]plain\n[00:01](background)"), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 2 {
		t.Fatalf("equal-start group produced %d lines", result.Document.Len())
	}
	plain, background := result.Document.Line(0), result.Document.Line(1)
	if plain.End() != time.Second || background.End() != 59_999*time.Second+999*time.Millisecond || !background.Background() {
		t.Fatalf("last-line sentinel leaked across equal starts: plain=[%v,%v), background=[%v,%v) bg=%v", plain.Start(), plain.End(), background.Start(), background.End(), background.Background())
	}
}

func TestErrorsAndStrictMode(t *testing.T) {
	for _, input := range []string{
		"[00:00]<00:00>x<00:00>",
		"[00:02]<00:01>x<00:03>",
		"[00:01]<00:03>x<00:02>",
		"[00:01][00:00]<00:00>x<00:00.5>",
		"[00:01]<00:01>x<00:02><00:01.5>y<00:03>",
	} {
		result, err := Parse(context.Background(), []byte(input), common.Options{Duration: 4 * time.Second})
		if err != nil || result.Document == nil {
			t.Fatalf("source word timing was rejected for %q: %#v, %v", input, result, err)
		}
	}
	for _, input := range []string{
		"untimed", "[999999999999999999:00]x",
		"[00:02]second\n[00:01]first", "[00:01]<oops>x",
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

func TestGeneratedZeroTimeSpaceAndReversedLineRetainSource(t *testing.T) {
	input := "[00:03]A[00:03.5] [00:00.000]B[00:04]\n[00:05]C[00:00]"
	result, err := Parse(context.Background(), []byte(input), common.Options{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	first, reversed := result.Document.Line(0), result.Document.Line(1)
	if first.Text() != "A B" || first.WordCount() != 3 || first.Word(0).Start != 3*time.Second || first.Word(0).End != 3500*time.Millisecond || first.Word(1).Text != " " || first.Word(1).Start != 3500*time.Millisecond || first.Word(1).End != 0 || first.Word(2).Text != "B" || first.Word(2).Start != 0 || first.Word(2).End != 4*time.Second {
		t.Fatalf("generated whitespace or its source endpoints changed: %+v", []lyric.Word{first.Word(0), first.Word(1), first.Word(2)})
	}
	if reversed.Start() != 5*time.Second || reversed.End() != 0 || reversed.Word(0).Start != 5*time.Second || reversed.Word(0).End != 0 {
		t.Fatalf("reversed source line changed: start=%v end=%v word=%+v", reversed.Start(), reversed.End(), reversed.Word(0))
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
	result, err := Parse(context.Background(), []byte("[00:01]<00:01.5>x<00:02><00:03>"), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if line.End() != 3*time.Second || line.WordCount() != 1 || line.Word(0).Start != 1500*time.Millisecond || line.Word(0).End != 2*time.Second {
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
			if line.ID() == "" || ids[line.ID()] || line.Start() < 0 || line.End() < 0 {
				t.Fatal("document violated line invariants")
			}
			ids[line.ID()] = true
		}
	})
}
