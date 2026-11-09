package ttml

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
)

func TestSyntheticGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/synthetic.ttml")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse(context.Background(), data, common.Options{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || result.Document.Len() != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	doc := result.Document
	main, bg, last := doc.Line(0), doc.Line(1), doc.Line(2)
	if main.ID() != "main" || main.Text() != "晨 光 & 风" || main.Translation() != "Morning" || main.Romanization() != "chen guang" || main.Language() != "zh" || main.Agent() != "v2" || !main.Duet() || main.Background() {
		t.Fatalf("main line metadata mismatch: %q %q %q", main.Text(), main.Translation(), main.Romanization())
	}
	if main.Start() != time.Second || main.End() != 5*time.Second || main.WordCount() != 2 || main.Word(1).Text != "光 & 风" {
		t.Fatal("main word timing mismatch")
	}
	if bg.ID() != "back" || bg.Text() != "和声" || !bg.Background() || !bg.Duet() || bg.Start() != 1500*time.Millisecond || bg.End() != 4*time.Second {
		t.Fatal("background line mismatch")
	}
	if last.ID() != "last" || last.Text() != "A new day" || last.Start() != 6*time.Second || last.End() != 8*time.Second {
		t.Fatal("last line or dur mismatch")
	}
	if doc.PropertyCount() != 3 || doc.Property(0).Name != "title" || doc.Property(0).Value != "合成测试" || doc.Property(2).Name != "album" {
		t.Fatal("metadata properties mismatch")
	}
	data[0] = '!'
	if doc.Line(0).Text() != "晨 光 & 风" {
		t.Fatal("document aliases input bytes")
	}
}

func TestNamespaceWhitespaceAndEmpty(t *testing.T) {
	for _, data := range []string{"", " \r\n\t", "\ufeff", "<tt/>", `<x:tt xmlns:x="http://www.w3.org/ns/ttml"><x:body/></x:tt>`} {
		result, err := Parse(context.Background(), []byte(data), common.Options{})
		if err != nil || result.Document.Len() != 0 {
			t.Fatalf("empty %q: %v", data, err)
		}
	}
	data := `<t:tt xmlns:t="http://www.w3.org/ns/ttml" xml:space="preserve"><t:body><t:p begin="0s" end="1s"> A  <t:span>B</t:span> C </t:p><t:p begin="1s" end="2s" xml:space="default"> A  <t:span xml:space="preserve"> B  </t:span>C </t:p><t:p begin="2s" end="3s">A<t:br/>B</t:p></t:body></t:tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{" A  B C ", "A  B  C", "A\nB"}
	for i := range want {
		if got := result.Document.Line(i).Text(); got != want[i] {
			t.Errorf("line %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestTimingNormalization(t *testing.T) {
	data := `<tt><body><p xml:id="later" begin="3s">last</p><p xml:id="first" begin="0s" end="2s"><span begin="0s" end="0s">A</span><span begin="0s">B</span><span begin="1s" end="2s">C</span></p><p xml:id="same" begin="0s" end="1s">other</p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{Duration: 8 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Line(0).ID() != "first" || result.Document.Line(1).ID() != "same" || result.Document.Line(2).End() != 8*time.Second {
		t.Fatal("stable sorting or final duration mismatch")
	}
	line := result.Document.Line(0)
	if line.Text() != "ABC" || line.WordCount() != 2 || line.Word(0).Text != "B" || line.Word(0).End != time.Second {
		t.Fatal("zero or missing word end normalization mismatch")
	}
	var codes []string
	for _, diag := range result.Diagnostics {
		codes = append(codes, diag.Code)
	}
	if !reflect.DeepEqual(codes, []string{"sorted", "zero-word"}) {
		t.Fatalf("diagnostics = %v", codes)
	}
	if result, err := Parse(context.Background(), []byte(data), common.Options{Strict: true}); err == nil || result.Document != nil {
		t.Fatal("strict mode accepted normalization")
	}
	result, err = Parse(context.Background(), []byte(`<tt><body><p><span begin="1s" end="2s">A</span></p></body></tt>`), common.Options{})
	if err != nil || result.Document.Line(0).Start() != time.Second || result.Document.Line(0).End() != 2*time.Second || len(result.Diagnostics) != 2 {
		t.Fatalf("derived paragraph interval: %+v, %v", result, err)
	}
}

func TestUnsupportedAndMalformed(t *testing.T) {
	for _, data := range []string{
		`<html/>`, `<tt/><tt/>`, `<tt>`, `text<tt/>`, `<tt><body><p begin="0s" end="1s">&unknown;</p></body></tt>`,
		`<!DOCTYPE tt><tt/>`, `<!DOCTYPE tt [<!ENTITY x SYSTEM "file:///never-read">]><tt/>`, `<?fetch href="https://never-read"?><tt/>`,
		`<tt><body><p begin="0s" end="1s"><image src="https://never-read"/></p></body></tt>`,
		`<tt><body begin="1s"/></tt>`, `<tt><body timeContainer="seq"/></tt>`,
		`<tt><body><p begin="1f" end="2s">A</p></body></tt>`, `<tt><body><p begin="0s" end="2s" dur="3s">A</p></body></tt>`,
		`<tt><body><p end="2s">A</p></body></tt>`, `<tt><body><p begin="2s" end="1s">A</p></body></tt>`,
		`<tt><body><p begin="0s" end="2s"><span begin="0s"><span begin="1s">A</span></span></p></body></tt>`,
		`<tt><body><p begin="0s" end="2s"><span begin="0s"><span xmlns:ttm="http://www.w3.org/ns/ttml#metadata" ttm:role="x-roman">A</span></span></p></body></tt>`,
	} {
		t.Run(data, func(t *testing.T) {
			result, err := Parse(context.Background(), []byte(data), common.Options{})
			var located *common.ParseError
			if err == nil || result.Document != nil || !errors.As(err, &located) || located.Line < 1 || located.Column < 1 {
				t.Fatalf("expected located fatal error: %+v %v", result, err)
			}
		})
	}
	invalid := []byte("<tt>\xff</tt>")
	if _, err := Parse(context.Background(), invalid, common.Options{}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	styled := `<tt><body><p begin="0s" end="1s" xmlns:tts="http://www.w3.org/ns/ttml#styling" tts:color="red"><unknown>A</unknown></p></body></tt>`
	result, err := Parse(context.Background(), []byte(styled), common.Options{})
	if err != nil || len(result.Diagnostics) != 2 || result.Document.Line(0).Text() != "A" {
		t.Fatalf("unsupported style handling: %+v %v", result, err)
	}
	if _, err := Parse(context.Background(), []byte(styled), common.Options{Strict: true}); err == nil {
		t.Fatal("strict mode accepted unsupported extension")
	}
}

func TestLimits(t *testing.T) {
	data := `<tt><body><p begin="0s" end="2s"><span begin="0s" end="1s">A</span><span begin="1s" end="2s">B</span></p></body></tt>`
	for name, options := range map[string]common.Options{
		"bytes":   {Limits: common.Limits{MaxBytes: len(data) - 1}},
		"depth":   {Limits: common.Limits{MaxDepth: 3}},
		"words":   {Limits: common.Limits{MaxWords: 1}},
		"invalid": {Limits: common.Limits{MaxDepth: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Parse(context.Background(), []byte(data), options)
			if err == nil || result.Document != nil {
				t.Fatal("limit accepted")
			}
		})
	}
	if _, err := Parse(context.Background(), []byte(data), common.Options{Limits: common.Limits{MaxBytes: len(data), MaxWords: 2, MaxDepth: 4, MaxLines: 1}}); err != nil {
		t.Fatalf("exact limits rejected: %v", err)
	}
	data = `<tt><body><p begin="0s" end="2s" xmlns:ttm="http://www.w3.org/ns/ttml#metadata">A<span ttm:role="x-bg">B</span></p></body></tt>`
	if _, err := Parse(context.Background(), []byte(data), common.Options{Limits: common.Limits{MaxLines: 1}}); err == nil {
		t.Fatal("expanded background line limit accepted")
	}
	data = `<tt><body><p begin="0s" end="1s"><one/><two/>A</p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{Limits: common.Limits{MaxDiagnostics: 1}})
	if err == nil || len(result.Diagnostics) != 1 || result.Document != nil {
		t.Fatal("diagnostic limit accepted")
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

func TestCancellation(t *testing.T) {
	data := []byte(`<tt><body>` + strings.Repeat(`<p begin="0s" end="1s"><span begin="0s" end="1s">A</span></p>`, 20000) + `</body></tt>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(ctx, data, common.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel: %v", err)
	}
	// The deterministic context cancels after UTF-8 validation, during token work.
	ctxDuring := &cancelAfterChecks{Context: context.Background(), remaining: 100}
	result, err := Parse(ctxDuring, data, common.Options{})
	if !errors.Is(err, context.Canceled) || result.Document != nil {
		t.Fatalf("mid-parse cancel: %+v, %v", result, err)
	}
}

func TestTimeExpressions(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"01:02:03.004": time.Hour + 2*time.Minute + 3*time.Second + 4*time.Millisecond,
		"02:03.5":      2*time.Minute + 3500*time.Millisecond,
		"1.5h":         90 * time.Minute, "1.5m": 90 * time.Second, "1.25s": 1250 * time.Millisecond,
		"0.000001ms": time.Nanosecond, "9223372036.854775807s": time.Duration(1<<63 - 1),
	} {
		got, err := parseTime(value)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"", "+1s", "-1s", "1e2s", ".5s", "1.s", "1:60", "00:60:00", "00:00:60", "00:00:00:01", "1f", "1t", "0.000000001ms", "9223372036.854775808s", "999999999999999999999h"} {
		if _, err := parseTime(value); err == nil {
			t.Errorf("accepted invalid time %q", value)
		}
	}
}

func TestBackgroundOwnWordsAndSourceLocation(t *testing.T) {
	data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><body><p begin="0s" end="10s">Main<span ttm:role="x-bg"><span begin="3s" end="4s">Back</span></span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil || result.Document.Len() != 2 || result.Document.Line(1).Start() != 3*time.Second || result.Document.Line(1).End() != 4*time.Second {
		t.Fatalf("background own word interval: %+v %v", result, err)
	}
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		data = "\ufeff<tt><body>" + newline + "<p begin=\"invalid\">中</p></body></tt>"
		_, err := Parse(context.Background(), []byte(data), common.Options{})
		var located *common.ParseError
		if !errors.As(err, &located) || located.Line != 2 || located.Column != 1 {
			t.Fatalf("%q source location: %v", newline, err)
		}
	}
}

func TestNestedWhitespaceAndExplicitBreakEvents(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"nested preserve", `A<span xml:space="preserve">  B  </span>C`, "A  B  C"},
		{"nested default", `<span xml:space="preserve">A<span xml:space="default">  B  </span>C</span>`, "A B C"},
		{"explicit break", `A<br/>B`, "A\nB"},
		{"break and indentation", "A<br/> \n B", "A\nB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><body><p begin="0s" end="1s"><span begin="0s" end="1s">` + tc.content + `</span><span ttm:role="x-translation">` + tc.content + `</span><span ttm:role="x-roman">` + tc.content + `</span></p></body></tt>`
			result, err := Parse(context.Background(), []byte(data), common.Options{Strict: true})
			if err != nil {
				t.Fatal(err)
			}
			line := result.Document.Line(0)
			if line.Text() != tc.want || line.Word(0).Text != tc.want || line.Translation() != tc.want || line.Romanization() != tc.want {
				t.Fatalf("got line=%q word=%q translation=%q roman=%q, want %q", line.Text(), line.Word(0).Text, line.Translation(), line.Romanization(), tc.want)
			}
		})
	}
	// A trailing default space belongs to the line's text stream even though the
	// individual word view trims its boundary. Span boundaries do not join words.
	result, err := Parse(context.Background(), []byte(`<tt><body><p begin="0s" end="2s"><span begin="0s" end="1s">A </span><span begin="1s" end="2s">B</span></p></body></tt>`), common.Options{})
	if err != nil || result.Document.Line(0).Text() != "A B" {
		t.Fatalf("cross-span whitespace: %+v %v", result, err)
	}
}

func TestRejectNestedBackgroundAndBOMColumns(t *testing.T) {
	for _, nested := range []string{
		`<span ttm:role="x-bg">Nested</span>`,
		`<span><span ttm:role="x-bg">Nested</span></span>`,
		`<span ttm:role="x-translation"><span ttm:role="x-bg">Nested</span></span>`,
	} {
		data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><body><p begin="0s" end="2s">Main<span ttm:role="x-bg">Outer` + nested + `</span></p></body></tt>`
		result, err := Parse(context.Background(), []byte(data), common.Options{})
		var located *common.ParseError
		if result.Document != nil || !errors.As(err, &located) || !strings.Contains(located.Message, "nested x-bg") {
			t.Fatalf("nested background accepted: %+v %v", result, err)
		}
		wantColumn := strings.LastIndex(data, `<span ttm:role="x-bg">`) + 1
		if located.Line != 1 || located.Column != wantColumn {
			t.Fatalf("nested role location = %d:%d, want 1:%d", located.Line, located.Column, wantColumn)
		}
	}
	for _, attrs := range []string{`begin="invalid"`, `begin="0s" end="1s" style="unsupported"`} {
		data := "\ufeff" + `<tt><body><p ` + attrs + `>中</p></body></tt>`
		_, err := Parse(context.Background(), []byte(data), common.Options{Strict: true})
		var located *common.ParseError
		if !errors.As(err, &located) || located.Line != 1 || located.Column != 14 {
			t.Fatalf("BOM first-line location: %v", err)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	data := []byte(`<tt><body>` + strings.Repeat(`<p begin="0s" end="1s"><span begin="0s" end="1s">Synthetic</span></p>`, 200) + `</body></tt>`)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(context.Background(), data, common.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
