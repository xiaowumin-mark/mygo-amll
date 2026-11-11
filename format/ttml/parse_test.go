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
	"github.com/xiaowumin-mark/mygo-amll/lyric"
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
	if main.ID() != "main" || main.Text() != "晨 光 & 风" || main.Translation() != "Morning" || main.Romanization() != "chen guang" || main.Language() != "zh" || main.Agent() != "v2" || main.Duet() || main.Background() {
		t.Fatalf("main line metadata mismatch: %q %q %q", main.Text(), main.Translation(), main.Romanization())
	}
	if main.Start() != time.Second || main.End() != 5*time.Second || main.WordCount() != 2 || main.Word(1).Text != "光 & 风" {
		t.Fatal("main word timing mismatch")
	}
	if bg.ID() != "back" || bg.Text() != "和声" || !bg.Background() || bg.Duet() || bg.Start() != 1500*time.Millisecond || bg.End() != 4*time.Second {
		t.Fatal("background line mismatch")
	}
	if last.ID() != "last" || last.Text() != "A new day" || last.Start() != 6*time.Second || last.End() != 8*time.Second {
		t.Fatal("last line or dur mismatch")
	}
	wantMetadata := []lyric.Property{{Name: "language", Value: "zh"}, {Name: "title", Value: "合成测试"}, {Name: "name", Value: "Second voice"}, {Name: "album", Value: "Synthetic"}, {Name: "timingMode", Value: "Word"}}
	var metadata []lyric.Property
	for index := 0; index < doc.PropertyCount(); index++ {
		metadata = append(metadata, doc.Property(index))
	}
	if !reflect.DeepEqual(metadata, wantMetadata) {
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
	want := []string{"A B C", "A B C", "AB"}
	for i := range want {
		if got := result.Document.Line(i).Text(); got != want[i] {
			t.Errorf("line %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestITunesMetadataSidecarDoesNotBlockMainLyrics(t *testing.T) {
	data := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:itunes="http://music.apple.com/lyric-ttml-internal"><head><metadata><iTunesMetadata xmlns="http://music.apple.com/lyric-ttml-internal"><transliterations><transliteration><text for="L1"><span begin="1s" end="2s">yi</span></text></transliteration></transliterations></iTunesMetadata></metadata></head><body><p begin="1s" end="2s" itunes:key="L1"><span begin="1s" end="2s">一</span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil || result.Document.Len() != 1 || result.Document.Line(0).Text() != "一" {
		t.Fatalf("sidecar prevented primary lyrics from loading: result=%+v err=%v", result, err)
	}
	if len(result.Diagnostics) != 0 || result.Document.Line(0).Word(0).Romanization != "yi" {
		t.Fatalf("sidecar transliteration was not retained cleanly: diagnostics=%v word=%+v", result.Diagnostics, result.Document.Line(0).Word(0))
	}
	if result, err := Parse(context.Background(), []byte(data), common.Options{Strict: true}); err != nil || result.Document == nil {
		t.Fatalf("strict mode rejected supported iTunes sidecar data: result=%+v err=%v", result, err)
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
	if line.Text() != "ABC" || line.WordCount() != 2 || line.Word(0).Text != "A" || line.Word(0).Start != 0 || line.Word(0).End != 0 || line.Word(1).Text != "C" || line.Word(1).Start != time.Second || line.Word(1).End != 2*time.Second {
		t.Fatal("zero or missing word end normalization mismatch")
	}
	var codes []string
	for _, diag := range result.Diagnostics {
		codes = append(codes, diag.Code)
	}
	if !reflect.DeepEqual(codes, []string{"sorted"}) {
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

func TestWordTextOrderSpacesAndLegacyTiming(t *testing.T) {
	data := `<tt><body><p begin="2s" end="3s"><span begin="2s" end="2.2s"> one </span> <span begin="1s" end="1s">two</span><span begin="4s" end="5s"> three </span><span begin="6s" end="5s">back</span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if line.Start() != time.Second || line.End() != 5*time.Second || line.WordCount() != 4 {
		t.Fatalf("range or point word lost: start=%v end=%v words=%d", line.Start(), line.End(), line.WordCount())
	}
	want := []struct {
		text       string
		start, end time.Duration
	}{{"one ", 2 * time.Second, 2200 * time.Millisecond}, {"two ", time.Second, time.Second}, {"three ", 4 * time.Second, 5 * time.Second}, {"back", 6 * time.Second, 5 * time.Second}}
	for i, expected := range want {
		word := line.Word(i)
		if word.Text != expected.text || word.Start != expected.start || word.End != expected.end {
			t.Fatalf("word %d = %+v, want %+v", i, word, expected)
		}
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "reversed-word" {
		t.Fatalf("reversed timing normalization was not diagnosed once: %+v", result.Diagnostics)
	}
}

func TestParagraphRangeExpandsToTimedChildren(t *testing.T) {
	data := `<tt><body><p xml:id="zero-end" begin="1s" end="0s"><span begin="1s" end="2s">A</span></p><p xml:id="word-overrun" begin="3s" end="3.999s"><span begin="2.5s" end="4s">B</span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	zeroEnd := result.Document.Line(0)
	if zeroEnd.Start() != time.Second || zeroEnd.End() != 2*time.Second {
		t.Fatalf("zero p end was not inferred from its timed child: [%v,%v)", zeroEnd.Start(), zeroEnd.End())
	}
	overrun := result.Document.Line(1)
	if overrun.Start() != 2500*time.Millisecond || overrun.End() != 4*time.Second {
		t.Fatalf("p interval did not expand to cover its timed child: [%v,%v)", overrun.Start(), overrun.End())
	}
}

func TestNonASCIIWordBoundarySpace(t *testing.T) {
	// Both final UTF-8 bytes are A0, but neither scalar is U+00A0. A byte
	// mistaken for a rune used to suppress authored word separators here.
	for _, text := range []string{"你", "だ", "synthetic"} {
		data := `<tt><body><p begin="0s" end="2s"><span begin="0s" end="1s">` + text + `</span> <span begin="1s" end="2s">B</span></p></body></tt>`
		result, err := Parse(context.Background(), []byte(data), common.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := result.Document.Line(0).Word(0).Text; got != text+" " {
			t.Fatalf("separator after %q: %q", text, got)
		}
	}
}

func TestAMLLMetadataWhitespaceAndEmptyValues(t *testing.T) {
	data := `<tt xmlns:amll="http://www.example.com/ns/amll"><head><metadata><amll:meta key="musicName" value="&#xA0; Synthetic &#x9;"/><amll:meta key="album" value=" "/><amll:meta key="custom" value="x  y"/></metadata></head><body/></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.PropertyCount() != 3 || result.Document.Property(0).Value != "Synthetic" || result.Document.Property(1) != (lyric.Property{Name: "custom", Value: "x  y"}) {
		t.Fatalf("metadata whitespace/empty value mismatch: %+v", result)
	}
}

func TestInlineAnnotationSelectionAndBackground(t *testing.T) {
	data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><body><p begin="0s" end="2s"><span begin="0s" end="2s">A</span><span ttm:role="x-translation">first<span ttm:role="x-bg">background translation</span></span><span ttm:role="x-translation">second</span><span ttm:role="x-roman">roman<span ttm:role="x-bg">background roman</span></span><span ttm:role="x-bg" begin="0s" end="2s"><span begin="0s" end="2s">B</span></span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{Strict: true, Limits: common.Limits{MaxLines: 2}})
	if err != nil {
		t.Fatal(err)
	}
	main, background := result.Document.Line(0), result.Document.Line(1)
	if main.Translation() != "first" || main.Romanization() != "roman" || background.Translation() != "background translation" || background.Romanization() != "background roman" {
		t.Fatalf("annotation selection: main=%q/%q background=%q/%q", main.Translation(), main.Romanization(), background.Translation(), background.Romanization())
	}
}

func TestSourceReversedParagraphIsRetainedAndDiagnosed(t *testing.T) {
	data := []byte(`<tt><body><p begin="2s" end="1s">Synthetic</p></body></tt>`)
	result, err := Parse(context.Background(), data, common.Options{})
	if err != nil || result.Document == nil {
		t.Fatalf("source paragraph rejected: %+v %v", result, err)
	}
	line := result.Document.Line(0)
	word := line.Word(0)
	if line.Start() != 2*time.Second || line.End() != time.Second || word.Start != 2*time.Second || word.End != time.Second || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "reversed-word" {
		t.Fatalf("source interval changed or not diagnosed: word=%+v diagnostics=%v", word, result.Diagnostics)
	}
	if strict, err := Parse(context.Background(), data, common.Options{Strict: true}); err == nil || strict.Document != nil {
		t.Fatal("strict mode accepted reversed source timing")
	}
}

func TestXMLDocumentWhitespaceAndDeclarationPosition(t *testing.T) {
	for _, source := range []string{"\u00a0", "<tt/>\u00a0", "\u3000<tt/>", `<?xml version="1.0"?><?xml version="1.0"?><tt/>`, ` <!--comment--><?xml version="1.0"?><tt/>`, "\n<?xml version=\"1.0\"?><tt/>", `<?xml?><tt/>`, `<?xml encoding="UTF-8"?><tt/>`, `<?xml version="1.0" unexpected="x"?><tt/>`, `<?xml version="1.0" standalone="yes" encoding="UTF-8"?><tt/>`} {
		result, err := Parse(context.Background(), []byte(source), common.Options{})
		if err == nil || result.Document != nil {
			t.Fatalf("accepted XML document grammar violation: %q", source)
		}
	}
	for _, source := range []string{" \t\r\n", "\ufeff<?xml version=\"1.0\"?><tt/>", "<?xml version=\"1.0\"?><!--comment--><tt/> \t\n", `<?xml version='1.0' encoding='UTF-8' standalone='yes'?><tt/>`} {
		if _, err := Parse(context.Background(), []byte(source), common.Options{}); err != nil {
			t.Fatalf("valid XML whitespace/declaration: %q: %v", source, err)
		}
	}
}

func TestUnsupportedAndMalformed(t *testing.T) {
	for _, data := range []string{
		`<html/>`, `<tt/><tt/>`, `<tt>`, `text<tt/>`, `<tt><body><p begin="0s" end="1s">&unknown;</p></body></tt>`,
		`<!DOCTYPE tt><tt/>`, `<!DOCTYPE tt [<!ENTITY x SYSTEM "file:///never-read">]><tt/>`, `<?fetch href="https://never-read"?><tt/>`,
		`<tt><body><p begin="0s" end="1s"><image src="https://never-read"/></p></body></tt>`,
		`<tt><body timeContainer="seq"/></tt>`,
		`<tt><body><p begin="1f" end="2s">A</p></body></tt>`, `<tt><body><p begin="0s" end="2s" dur="3s">A</p></body></tt>`,
		`<tt><body><p end="2s">A</p></body></tt>`,
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
		"0.000001ms": time.Nanosecond, "9223372036.854s": 9223372036854 * time.Millisecond,
		"1.2344": 1234 * time.Millisecond, "1.2345": 1235 * time.Millisecond,
		"1.2345s": 1235 * time.Millisecond, "1.234499999999999": 1234 * time.Millisecond,
		"1.234500000000001":  1235 * time.Millisecond,
		"02:03.0005":         2*time.Minute + 3*time.Second + time.Millisecond,
		"\u00a01.2345\u00a0": 1235 * time.Millisecond,
	} {
		got, err := parseTime(value)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"", "+1s", "-1s", "1e2s", ".5s", "1.s", "1:60", "00:60:00", "00:00:60", "00:00:00:01", "1f", "1t", "0.000000001ms", "9223372036.855s", "999999999999999999999h"} {
		if _, err := parseTime(value); err == nil {
			t.Errorf("accepted invalid time %q", value)
		}
	}
}

func TestOrdinaryBareSecondsRoundLikeAMLL(t *testing.T) {
	data := `<tt><body><p begin="0.0005" end="1.2345"><span begin="0.0005" end="1.2345">A</span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if line.Start() != time.Millisecond || line.End() != 1235*time.Millisecond || line.Word(0).Start != time.Millisecond || line.Word(0).End != 1235*time.Millisecond {
		t.Fatalf("ordinary bare seconds did not use source millisecond rounding: line=%+v word=%+v", line, line.Word(0))
	}
}

func TestAMLLTimingAttributesAndFallbackWords(t *testing.T) {
	data := `<tt xmlns:x="urn:timing"><body>` +
		`<p begin="1s" end="2s" x:begin="8s" x:end="9s">Plain line</p>` +
		`<p x:begin="3s" x:end="4s">Namespaced line</p>` +
		`<p x:begin="4s" x:dur="2s">Namespaced duration</p>` +
		`<p begin="5s" end="7s"><span begin="5s">A</span><span end="7s">B</span><span dur="1s">C</span></p>` +
		`<p begin="7s" end="8s"><span begin="7s" end="8s" dur="0s">Timed span</span></p>` +
		`</body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 5 {
		t.Fatalf("got %d lines", result.Document.Len())
	}

	plain := result.Document.Line(0)
	if plain.Start() != time.Second || plain.End() != 2*time.Second || plain.Text() != "Plain line" || plain.WordCount() != 1 {
		t.Fatalf("unqualified timing precedence or fallback word mismatch: %+v", plain)
	}
	if word := plain.Word(0); word.Text != plain.Text() || word.Start != plain.Start() || word.End != plain.End() {
		t.Fatalf("plain paragraph word does not span line: %+v", word)
	}

	namespaced := result.Document.Line(1)
	if namespaced.Start() != 3*time.Second || namespaced.End() != 4*time.Second || namespaced.WordCount() != 1 {
		t.Fatalf("namespaced timing fallback mismatch: %+v", namespaced)
	}
	if word := namespaced.Word(0); word.Text != "Namespaced line" || word.Start != namespaced.Start() || word.End != namespaced.End() {
		t.Fatalf("namespaced paragraph fallback word mismatch: %+v", word)
	}

	duration := result.Document.Line(2)
	if duration.Start() != 4*time.Second || duration.End() != 6*time.Second || duration.WordCount() != 1 {
		t.Fatalf("namespaced duration fallback mismatch: %+v", duration)
	}
	if word := duration.Word(0); word.Text != duration.Text() || word.Start != duration.Start() || word.End != duration.End() {
		t.Fatalf("namespaced duration word mismatch: %+v", word)
	}

	partial := result.Document.Line(3)
	if partial.Text() != "ABC" || partial.WordCount() != 1 {
		t.Fatalf("partial timing attrs should leave a whole-line fallback word: text=%q words=%d", partial.Text(), partial.WordCount())
	}
	if word := partial.Word(0); word.Text != "ABC" || word.Start != 5*time.Second || word.End != 7*time.Second {
		t.Fatalf("partial span timing was incorrectly treated as word timing: %+v", word)
	}

	timed := result.Document.Line(4)
	if timed.WordCount() != 1 || timed.Word(0).Start != 7*time.Second || timed.Word(0).End != 8*time.Second {
		t.Fatalf("AMLL timed span did not prefer begin/end over dur: %+v", timed)
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
		{"xml space is ignored", `A<span xml:space="preserve">  B  </span>C`, "A B C"},
		{"nested xml space is ignored", `<span xml:space="preserve">A<span xml:space="default">  B  </span>C</span>`, "A B C"},
		{"br contributes no text", `A<br/>B`, "AB"},
		{"br and indentation", "A<br/> \n B", "A B"},
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

func TestAMLLKeyFormattingWhitespaceAndBackgroundDelimiters(t *testing.T) {
	data := `<tt xmlns:i="http://music.apple.com/lyric-ttml-internal" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" xml:space="preserve"><body>` +
		`<p i:key="amll-key" xml:id="xml-extension" begin="0s" end="3s">` +
		"\n <span begin=\"0s\" end=\"1s\">A</span>\n <span begin=\"1s\" end=\"2s\">\u00a0B\u00a0</span>" +
		`<br/> C<span i:key="amll-bg" ttm:role="x-bg" begin="0s" end="2s"><span begin="0s" end="2s">（和声）</span></span></p>` +
		`<p xml:id="xml-only" begin="3s" end="4s">D</p><p begin="4s" end="5s">E</p>` +
		`</body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 4 {
		t.Fatalf("unexpected rows: %d", result.Document.Len())
	}
	main, background, xmlFallback, generated := result.Document.Line(0), result.Document.Line(1), result.Document.Line(2), result.Document.Line(3)
	if main.ID() != "amll-key" || main.Text() != "A B C" {
		t.Fatalf("AMLL key or normalized text mismatch: id=%q text=%q", main.ID(), main.Text())
	}
	if background.ID() != "amll-bg" || !background.Background() || background.Text() != "和声" || background.WordCount() != 1 || background.Word(0).Text != "和声" {
		t.Fatalf("background wrapper punctuation was not removed: %q", background.Text())
	}
	if xmlFallback.ID() != "xml-only" || generated.ID() == "" {
		t.Fatalf("xml:id/no-key compatibility extension was lost: %q %q", xmlFallback.ID(), generated.ID())
	}
	if main.WordCount() != 2 || main.Word(1).Text != "B" {
		t.Fatalf("normalized timed words mismatch: %d %+v", main.WordCount(), main.Word(1))
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
