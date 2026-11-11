package ttml

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
)

func appleWordDocument(body string) string {
	return `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:a="http://music.apple.com/lyric-ttml-internal" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" a:timing="Word">` + body + `</tt>`
}

func TestAppleWordAbsoluteEnvelopeAndBareSeconds(t *testing.T) {
	data := appleWordDocument(`<body dur="12"><div begin="2" end="10"><div dur="4"><div begin="3" dur="2" end="5"><p xml:id="main" begin="3.25" end="4.75"><span begin="3.25" end="4">A</span><span begin="4" dur="0.75">B</span><span ttm:role="x-bg" begin="3.5" end="4.5">C</span></p></div></div></div></body>`)
	result, err := Parse(context.Background(), []byte(data), common.Options{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 2 || len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected profile result: lines=%d diagnostics=%+v", result.Document.Len(), result.Diagnostics)
	}
	main, bg := result.Document.Line(0), result.Document.Line(1)
	if main.Start() != 3250*time.Millisecond || main.End() != 4750*time.Millisecond ||
		main.WordCount() != 1 || main.Word(0).Text != "A" || main.Word(0).Start != 3250*time.Millisecond || main.Word(0).End != 4*time.Second {
		t.Fatal("absolute word/paragraph timing was shifted by a container")
	}
	if !bg.Background() || bg.Start() != 3500*time.Millisecond || bg.End() != 4500*time.Millisecond {
		t.Fatal("background absolute timing was not preserved")
	}
	// Exactly representable nanoseconds and the maximum Duration remain exact.
	data = appleWordDocument(`<body end="9223372036.854775807"><p begin="9223372036.854775806" end="9223372036.854775807">A</p></body>`)
	result, err = Parse(context.Background(), []byte(data), common.Options{})
	if err != nil || result.Document.Line(0).End() != time.Duration(1<<63-1) ||
		result.Document.Line(0).End()-result.Document.Line(0).Start() != time.Nanosecond {
		t.Fatalf("bare decimal precision changed: %v", err)
	}
}

func TestAppleWordIgnoresContainerAndBackgroundBounds(t *testing.T) {
	for name, body := range map[string]string{
		"body and div timing ignored":                  `<body begin="nonsense" end="1" dur="bad"><div begin="4" end="2" dur="bad"><p begin="1" end="3">A</p></div></body>`,
		"paragraph and word intervals remain positive": `<body end="1"><p begin="1" end="3"><span begin="1" end="2">A</span><span begin="2" end="3">B</span></p></body>`,
		"background after paragraph":                   `<body end="2"><p begin="0" end="2">A<span ttm:role="x-bg" begin="1" end="3">B</span></p></body>`,
		"background before paragraph":                  `<body end="2"><p begin="2" end="4"><span ttm:role="x-bg" begin="1" end="3">B</span>A</p></body>`,
		"duration after container end":                 `<body end="3"><p begin="2">A</p></body>`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Parse(context.Background(), []byte(appleWordDocument(body)), common.Options{})
			if err != nil || result.Document == nil {
				t.Fatalf("AMLL ignores ancestor and parent containment: result=%+v, error=%v", result, err)
			}
		})
	}
	data := []byte(appleWordDocument(`<body end="3"><p begin="2">A</p></body>`))
	if result, err := Parse(context.Background(), data, common.Options{Duration: 3 * time.Second}); err != nil || result.Document.Line(0).End() != 3*time.Second {
		t.Fatalf("host Duration was not retained: %v", err)
	}
	if result, err := Parse(context.Background(), data, common.Options{Duration: 4 * time.Second}); err != nil || result.Document.Line(0).End() != 4*time.Second {
		t.Fatalf("ignored container end constrained the host Duration: %v", err)
	}
}

func TestAppleWordExactProfileOptIn(t *testing.T) {
	for _, root := range []string{
		`<tt>`,
		`<tt xmlns:a="http://music.apple.com/lyric-ttml-internal">`,
		`<tt xmlns:a="http://music.apple.com/lyric-ttml-internal" a:timing="Line">`,
		`<tt xmlns:a="http://music.apple.com/lyric-ttml-internal" a:timing="word">`,
		`<tt xmlns:a="https://music.apple.com/lyric-ttml-internal" a:timing="Word">`,
		`<tt timing="Word">`,
	} {
		for _, body := range []string{
			`<body dur="3s"><p begin="0s" end="1s">A</p></body>`,
			`<body><div begin="0s" end="3s"><p begin="0s" end="1s">A</p></div></body>`,
		} {
			result, err := Parse(context.Background(), []byte(root+body+`</tt>`), common.Options{})
			if err != nil || result.Document == nil || result.Document.Len() != 1 || result.Document.Line(0).Start() != 0 || result.Document.Line(0).End() != time.Second {
				t.Fatalf("body/div timing should be ignored for every root declaration: root=%s result=%+v err=%v", root, result, err)
			}
		}
	}
	for _, root := range []string{`<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata">`, appleWordDocument("")} {
		rootStart := root
		if strings.HasSuffix(root, "</tt>") {
			rootStart = strings.TrimSuffix(root, `</tt>`)
		}
		input := rootStart + `<body><p begin="0s" end="2s">A<span ttm:role="x-bg" begin="1s" end="3s">B</span></p></body></tt>`
		result, err := Parse(context.Background(), []byte(input), common.Options{})
		if err != nil || result.Document.Len() != 2 || result.Document.Line(1).End() != 3*time.Second {
			t.Fatalf("background outside p interval should match AMLL: result=%+v err=%v", result, err)
		}
	}
}

func TestAppleWordKeepsUnsupportedTimingAndLimits(t *testing.T) {
	for _, body := range []string{
		`<body timeContainer="seq"/>`, `<body timeBase="clock"/>`,
		`<body><p begin="1f" end="2f">A</p></body>`, `<body><p begin="1t" end="2t">A</p></body>`,
		`<body><p begin="-1" end="1">A</p></body>`, `<body><p begin="+1" end="2">A</p></body>`, `<body><p begin="1e2" end="2">A</p></body>`,
		`<body><p begin=".5" end="1">A</p></body>`, `<body><p begin="1." end="2">A</p></body>`, `<body><p begin="0.0000000001" end="1">A</p></body>`,
		`<body><p begin="9223372036.854775808" end="9223372037">A</p></body>`,
		`<body><p begin="9223372036.854775807" dur="0.000000001">A</p></body>`,
		`<body end="5"><p begin="0" end="1"><image src="file:///never-read"/></p></body>`,
	} {
		result, err := Parse(context.Background(), []byte(appleWordDocument(body)), common.Options{})
		if err == nil || result.Document != nil {
			t.Fatalf("unsupported timing/resource accepted: %s", body)
		}
	}
	if _, err := Parse(context.Background(), []byte(`<!DOCTYPE tt [<!ENTITY x SYSTEM "file:///never-read">]>`+appleWordDocument(`<body/>`)), common.Options{}); err == nil {
		t.Fatal("explicit profile enabled DTD resources")
	}
	data := []byte(appleWordDocument(`<body dur="10"><div begin="1" end="9"><p begin="2" end="3"><span begin="2" end="2.5">A</span><span begin="2.5" end="3">B</span></p><p begin="4" end="5">C</p></div></body>`))
	for name, limits := range map[string]common.Limits{
		"bytes": {MaxBytes: len(data) - 1}, "lines": {MaxLines: 1},
		"depth": {MaxDepth: 4}, "text": {MaxTextBytes: 1},
		"words": {MaxWords: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if result, err := Parse(context.Background(), data, common.Options{Limits: limits}); err == nil || result.Document != nil {
				t.Fatal("profile bypassed a configured limit")
			}
		})
	}
	data = []byte(appleWordDocument(`<body dur="10"><p begin="1" end="2"><one/><two/>A</p></body>`))
	if result, err := Parse(context.Background(), data, common.Options{Limits: common.Limits{MaxDiagnostics: 1}}); err == nil || result.Document != nil || len(result.Diagnostics) != 1 {
		t.Fatal("profile bypassed the diagnostic budget")
	}
	large := []byte(appleWordDocument(`<body end="10">` + strings.Repeat(`<p begin="1" end="2">A</p>`, 1000) + `</body>`))
	ctx := &cancelAfterChecks{Context: context.Background(), remaining: 100}
	if result, err := Parse(ctx, large, common.Options{}); !errors.Is(err, context.Canceled) || result.Document != nil {
		t.Fatalf("profile did not preserve parser cancellation: %v", err)
	}
}
