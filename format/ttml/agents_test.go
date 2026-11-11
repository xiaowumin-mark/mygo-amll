package ttml

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestTypedAgentsAlternateInSourceOrder(t *testing.T) {
	data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><head><metadata>` +
		`<ttm:agent xml:id="a" type="person"/><ttm:agent xml:id="b" type="other"/>` +
		`<ttm:agent xml:id="c" type="person"/><ttm:agent xml:id="g" type="group"/>` +
		`</metadata></head><body>`
	agents := []string{"a", "g", "a", "b", "g", "b", "c", "", "", "a"}
	want := []bool{false, false, false, true, false, true, false, true, true, false}
	for index, agent := range agents {
		// The first two starts reverse source order, so a time-sorted pass would
		// incorrectly update the alternation state.
		start := index + 1
		if index == 0 {
			start = 3
		} else if index == 2 {
			start = 1
		}
		agentAttr := ""
		if agent != "" {
			agentAttr = ` ttm:agent="` + agent + `"`
		}
		data += fmt.Sprintf(`<p xml:id="line%d" begin="%ds" end="%ds"%s>word</p>`, index, start, start+1, agentAttr)
	}
	data += `</body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != len(want) {
		t.Fatalf("line count = %d, want %d", result.Document.Len(), len(want))
	}
	for index := 0; index < result.Document.Len(); index++ {
		line := result.Document.Line(index)
		var sourceIndex int
		if _, err := fmt.Sscanf(line.ID(), "line%d", &sourceIndex); err != nil {
			t.Fatal(err)
		}
		if line.Duet() != want[sourceIndex] || line.Agent() != agents[sourceIndex] {
			t.Fatalf("%s agent=%q duet=%v, want agent=%q duet=%v", line.ID(), line.Agent(), line.Duet(), agents[sourceIndex], want[sourceIndex])
		}
	}
}

func TestFirstOtherAndGroupsDoNotChangeAlternation(t *testing.T) {
	data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><head><metadata>` +
		`<ttm:agent xml:id="g" ttm:type="group"/><ttm:agent xml:id="o" ttm:type="other"/><ttm:agent xml:id="p" type="person"/>` +
		`</metadata></head><body>`
	agents := []string{"g", "o", "g", "o", "p", "g", "p"}
	want := []bool{false, true, false, true, false, false, false}
	for index, agent := range agents {
		data += fmt.Sprintf(`<p begin="%ds" end="%ds" ttm:agent="%s">word</p>`, index+1, index+2, agent)
	}
	data += `</body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for index, expected := range want {
		if line := result.Document.Line(index); line.Duet() != expected {
			t.Fatalf("line %d agent=%q duet=%v, want %v", index, line.Agent(), line.Duet(), expected)
		}
	}
}

func TestUnknownAgentAndBackgroundInheritance(t *testing.T) {
	data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><body>` +
		`<p xml:id="first" begin="2s" end="3s" ttm:agent="v2">main<span xml:id="back" ttm:role="x-bg" begin="1s" end="3s" ttm:agent="different">back</span></p>` +
		`<p xml:id="default" begin="4s" end="5s">default</p>` +
		`<p xml:id="again" begin="6s" end="7s" ttm:agent="v2">again</p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"first": false, "back": false, "default": true, "again": false}
	for index := 0; index < result.Document.Len(); index++ {
		line := result.Document.Line(index)
		if line.Duet() != want[line.ID()] {
			t.Fatalf("line %q duet=%v, want %v", line.ID(), line.Duet(), want[line.ID()])
		}
	}
}

func TestLastAgentDeclarationWins(t *testing.T) {
	data := `<tt xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><head><metadata>` +
		`<ttm:agent xml:id="v2" type="other"/><ttm:agent xml:id="v2" type="group"/>` +
		`</metadata></head><body><p begin="1s" end="2s" ttm:agent="v2">main</p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Line(0).Duet() {
		t.Fatal("earlier Other declaration overrode the final Group declaration")
	}
}

func TestAgentDuetPassCancellation(t *testing.T) {
	ctx := &cancelAfterChecks{Context: context.Background(), remaining: 100}
	parser, err := formatutil.New(ctx, nil, common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := documentParser{p: parser}
	for index := range 1000 {
		d.lines = append(d.lines, formatutil.Line{Line: lyric.Line{ID: fmt.Sprint(index), Agent: strings.Repeat("v", index%2+1)}})
	}
	ctx.remaining = 2
	if err := d.applyAgentDuets(); !errors.Is(err, context.Canceled) {
		t.Fatalf("agent pass ignored cancellation: %v", err)
	}
}
