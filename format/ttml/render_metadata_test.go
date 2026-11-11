package ttml

import (
	"context"
	"errors"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
)

const rubyFixture = `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:tts="http://www.w3.org/ns/ttml#styling" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" xmlns:amll="http://www.example.com/ns/amll"><body><div><p xml:id="main" begin="1s" end="4s"><span tts:ruby="container" amll:obscene="true" amll:empty-beat="2"><span tts:ruby="base">日本</span><span tts:ruby="textContainer"><span tts:ruby="text" begin="1s" end="2s">に</span><span tts:ruby="text" begin="2s" end="3s">ほん</span></span></span><span ttm:role="x-bg" begin="3s" end="4s"><span begin="3s" end="4s">answer</span></span></p></div></body></tt>`

func TestTimedRubyAndBackgroundParent(t *testing.T) {
	result, err := Parse(context.Background(), []byte(rubyFixture), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Len() != 2 || len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected output: %d lines, %+v", result.Document.Len(), result.Diagnostics)
	}
	line := result.Document.Line(0)
	word := line.Word(0)
	if line.Text() != "日本" || word.Text != "日本" || len(word.Ruby) != 2 || word.Ruby[1].Text != "ほん" || word.End != 3*time.Second || !word.Obscene || word.EmptyBeat != 2 {
		t.Fatalf("Ruby or explicit word metadata lost: %+v", word)
	}
	if result.Document.Line(1).ParentID() != "main" {
		t.Fatal("background no longer identifies its source main paragraph")
	}
	word.Ruby[0].Text = "changed"
	if line.Ruby(0, 0).Text != "に" {
		t.Fatal("Ruby read view aliases caller storage")
	}
}

func TestRubyRespectsSharedBudgetsAndCancellation(t *testing.T) {
	for _, limits := range []common.Limits{{MaxWords: 2}, {MaxTextBytes: 10}, {MaxDepth: 5}} {
		result, err := Parse(context.Background(), []byte(rubyFixture), common.Options{Limits: limits})
		var parseErr *common.ParseError
		if !errors.As(err, &parseErr) || limits.MaxDepth == 0 && parseErr.Code != "limit" || result.Document != nil {
			t.Fatalf("Ruby escaped budget %+v: %v", limits, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := Parse(ctx, []byte(rubyFixture), common.Options{}); !errors.Is(err, context.Canceled) || result.Document != nil {
		t.Fatal("Ruby ignored cancellation")
	}
}
