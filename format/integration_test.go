package format_test

import (
	"context"
	"slices"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/format/lrc"
	"github.com/xiaowumin-mark/mygo-amll/format/qrc"
	"github.com/xiaowumin-mark/mygo-amll/format/ttml"
	"github.com/xiaowumin-mark/mygo-amll/format/yrc"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

// The same synthetic song exercises the parser -> immutable model -> visual
// query boundary, including exact half-open endpoints and a seven-second gap.
func TestFormatVisualAgreement(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		parse       func(context.Context, []byte, common.Options) (common.Result, error)
	}{
		{"lrc", "[00:01]<00:01>A <00:02>B<00:03>\n[00:10]<00:10>C<00:12>", lrc.Parse},
		{"yrc", "[1000,2000](1000,1000,0)A (2000,1000,0)B\n[10000,2000](10000,2000,0)C", yrc.Parse},
		{"qrc", "[1000,2000]A (1000,1000)B(2000,1000)\n[10000,2000]C(10000,2000)", qrc.Parse},
		{"ttml", `<tt xmlns="http://www.w3.org/ns/ttml"><body><div><p begin="1s" end="3s" xml:space="preserve"><span begin="1s" end="2s">A </span><span begin="2s" end="3s">B</span></p><p begin="10s" end="12s"><span begin="10s" end="12s">C</span></p></div></body></tt>`, ttml.Parse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := []byte(tc.input)
			result, err := tc.parse(context.Background(), input, common.Options{Strict: true})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("parse: %v, diagnostics=%v", err, result.Diagnostics)
			}
			clear(input)
			doc := result.Document
			wantFirstWord := "A "
			if tc.name == "ttml" {
				// AMLL normalizes whitespace at a timed span boundary; the line
				// keeps its separating space while the first word does not.
				wantFirstWord = "A"
			}
			if doc.Len() != 2 || doc.Line(0).Text() != "A B" || doc.Line(0).WordCount() != 2 || doc.Line(0).Word(0).Text != wantFirstWord || doc.Line(0).Word(1).End != 3*time.Second {
				t.Fatal("format changed synthetic song semantics or retained source bytes")
			}
			if doc.Line(0).ID() == doc.Line(1).ID() {
				t.Fatal("line identities must be distinct")
			}
			index, err := timeline.New(doc)
			if err != nil {
				t.Fatal(err)
			}
			for _, query := range []struct {
				at               time.Duration
				playing          []int
				interlude, ended bool
			}{
				{0, nil, false, false},
				{time.Second, []int{0}, false, false},
				{3 * time.Second, nil, true, false},
				{10 * time.Second, []int{1}, false, false},
				{12 * time.Second, nil, false, true},
				{2 * time.Second, []int{0}, false, false},
			} {
				state := index.VisualAt(query.at, nil, nil)
				if !slices.Equal(state.Playing, query.playing) || state.InInterlude != query.interlude || state.Ended != query.ended {
					t.Fatalf("position %v: %+v", query.at, state)
				}
			}
		})
	}
}
