package ttml

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestITunesSidecarMetadataAndAnnotations(t *testing.T) {
	data := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:i="http://music.apple.com/lyric-ttml-internal" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" i:timing="Word"><head><metadata>` +
		`<i:iTunesMetadata><i:songwriters><i:songwriter>Writer</i:songwriter><i:songwriter>Writer</i:songwriter></i:songwriters>` +
		`<i:translations><i:translation xml:lang="zh-Hans"><i:text for="L1">译文<span ttm:role="x-bg">（背译）</span></i:text></i:translation></i:translations>` +
		`<i:transliterations><i:transliteration xml:lang="ja-Latn"><i:text for="L1"><span begin="1s" end="2s">ichi</span><span begin="2s" end="3s">ni</span><span ttm:role="x-bg"><span begin="1.5s" end="2.5s">sei</span></span></i:text></i:transliteration></i:transliterations>` +
		`<i:audio lyricOffset="0.070" role="spatial"/></i:iTunesMetadata></metadata></head>` +
		`<body><div><p begin="1s" end="3s" i:key="L1"><span begin="1s" end="2s">一</span><span begin="2s" end="3s">二</span>` +
		`<span ttm:role="x-bg" begin="1.5s" end="2.5s"><span begin="1.5s" end="2.5s">声</span></span></p></div></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	doc := result.Document
	if doc.Len() != 2 {
		t.Fatalf("line count = %d, want 2", doc.Len())
	}
	main, background := doc.Line(0), doc.Line(1)
	if main.ID() != "L1" || main.Translation() != "译文" || main.Romanization() != "" {
		t.Fatalf("main sidecar mismatch: id=%q translation=%q romanization=%q", main.ID(), main.Translation(), main.Romanization())
	}
	if main.Word(0).Romanization != "ichi" || main.Word(1).Romanization != "ni" {
		t.Fatalf("timed romanization was not aligned: %+v %+v", main.Word(0), main.Word(1))
	}
	if background.ParentID() != "L1" || background.Translation() != "背译" || background.Word(0).Romanization != "sei" {
		t.Fatalf("background sidecar mismatch: parent=%q translation=%q word=%+v", background.ParentID(), background.Translation(), background.Word(0))
	}
	properties := make(map[string][]string)
	for index := 0; index < doc.PropertyCount(); index++ {
		property := doc.Property(index)
		properties[property.Name] = append(properties[property.Name], property.Value)
	}
	if got := properties["timingMode"]; len(got) != 1 || got[0] != "Word" {
		t.Fatalf("timingMode = %v", got)
	}
	if got := properties["songwriters"]; len(got) != 1 || got[0] != "Writer" {
		t.Fatalf("songwriters = %v", got)
	}
	if got := properties["lyricOffset"]; len(got) != 1 || got[0] != "0.070" {
		t.Fatalf("lyricOffset = %v", got)
	}
	if got := properties["audioRole"]; len(got) != 1 || got[0] != "spatial" {
		t.Fatalf("audioRole = %v", got)
	}
}

func TestSidecarRomanizationUsesCleanSyllableText(t *testing.T) {
	data := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:i="http://music.apple.com/lyric-ttml-internal"><head><metadata>` +
		`<i:iTunesMetadata><i:transliterations><i:transliteration><i:text for="L1"><span begin="1s" end="2s">  ichi </span> <span begin="2s" end="3s">ni </span></i:text></i:transliteration></i:transliterations></i:iTunesMetadata>` +
		`</metadata></head><body><p begin="1s" end="3s" i:key="L1"><span begin="1s" end="2s">一 </span><span begin="2s" end="3s">二</span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if line.Word(0).Text != "一 " || line.Word(0).Romanization != "ichi" || line.Word(1).Romanization != "ni" {
		t.Fatalf("sidecar spaces leaked or main word space lost: %+v %+v", line.Word(0), line.Word(1))
	}
}

func TestSidecarParseErrorsAndBudgetsAreReturned(t *testing.T) {
	for _, strict := range []bool{false, true} {
		data := `<tt xmlns:i="http://music.apple.com/lyric-ttml-internal"><head><i:iTunesMetadata><i:transliterations><i:transliteration><i:text for="L1"><span begin="invalid" end="2s">one</span></i:text></i:transliteration></i:transliterations></i:iTunesMetadata></head><body><p begin="1s" end="2s" i:key="L1">main</p></body></tt>`
		result, err := Parse(context.Background(), []byte(data), common.Options{Strict: strict})
		if err == nil || result.Document != nil {
			t.Fatalf("sidecar parsing error was swallowed with strict=%v: result=%+v error=%v", strict, result, err)
		}
	}
	data := `<tt xmlns:i="http://music.apple.com/lyric-ttml-internal"><head><i:iTunesMetadata><i:transliterations><i:transliteration><i:text for="L1"><span begin="1s" end="2s">one</span><span begin="2s" end="3s">two</span></i:text></i:transliteration></i:transliterations></i:iTunesMetadata></head><body><p begin="1s" end="3s" i:key="L1">main</p></body></tt>`
	if _, err := Parse(context.Background(), []byte(data), common.Options{Limits: common.Limits{MaxWords: 3}}); err != nil {
		t.Fatalf("exact sidecar plus main word budget rejected: %v", err)
	}
	for _, strict := range []bool{false, true} {
		result, err := Parse(context.Background(), []byte(data), common.Options{Strict: strict, Limits: common.Limits{MaxWords: 1}})
		var parseError *common.ParseError
		if !errors.As(err, &parseError) || parseError.Code != "limit" || result.Document != nil {
			t.Fatalf("sidecar budget error was swallowed with strict=%v: result=%+v error=%v", strict, result, err)
		}
	}
}

func TestSidecarMetadataDedupIncludesInterleavedHeadEntries(t *testing.T) {
	parser, err := formatutil.New(context.Background(), nil, common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := documentParser{p: parser, metadata: []lyric.Property{{Name: "songwriters", Value: "one"}}}
	d.addMetadataUnique("songwriters", "one")
	d.addMetadataUnique("songwriters", "two")
	d.metadata = append(d.metadata, lyric.Property{Name: "songwriters", Value: "three"})
	d.addMetadataUnique("songwriters", "three")
	d.addMetadataUnique("songwriters", "two")
	if len(d.metadata) != 3 || len(d.metadataSeen) != 3 {
		t.Fatalf("deduplication missed direct head entries: %+v", d.metadata)
	}
}

func TestSidecarTraversalAndAlignmentCancelDuringWork(t *testing.T) {
	for _, operation := range []string{"descendants", "text", "alignment"} {
		t.Run(operation, func(t *testing.T) {
			ctx := &cancelAfterChecks{Context: context.Background(), remaining: 100}
			parser, err := formatutil.New(ctx, nil, common.Options{})
			if err != nil {
				t.Fatal(err)
			}
			d := documentParser{p: parser}
			ctx.remaining = 3
			root := &node{}
			for range 1000 {
				root.parts = append(root.parts, part{text: "x"}, part{child: &node{}})
			}
			switch operation {
			case "descendants":
				_, err = d.descendants(root, "songwriter")
			case "text":
				_, err = d.nodeText(root)
			case "alignment":
				roman := make([]lyric.Word, 1000)
				for index := range roman {
					roman[index] = lyric.Word{Text: "x", End: time.Second}
				}
				err = d.alignRomanization([]lyric.Word{{Start: 10 * time.Second, End: 20 * time.Second}}, roman)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s ignored cancellation during work: %v", operation, err)
			}
		})
	}
}

func TestSidecarAlignmentHasBoundedWork(t *testing.T) {
	parser, err := formatutil.New(context.Background(), nil, common.Options{Limits: common.Limits{MaxWords: 1}})
	if err != nil {
		t.Fatal(err)
	}
	d := documentParser{p: parser}
	roman := make([]lyric.Word, 33)
	for index := range roman {
		roman[index] = lyric.Word{Text: "x", End: time.Second}
	}
	err = d.alignRomanization([]lyric.Word{{Start: 10 * time.Second, End: 20 * time.Second}}, roman)
	var parseError *common.ParseError
	if !errors.As(err, &parseError) || parseError.Code != "limit" || !strings.Contains(parseError.Message, "alignment work") || d.alignmentWork != 32 {
		t.Fatalf("alignment work was not bounded: work=%d error=%v", d.alignmentWork, err)
	}
}

func TestLegacyITunesNamespaceAndInlinePrecedence(t *testing.T) {
	data := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:i="http://itunes.apple.com/lyric-ttml-extensions" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" i:timing="Word"><head><metadata>` +
		`<i:iTunesMetadata><i:songwriters><i:songwriter>Legacy</i:songwriter></i:songwriters><i:translations><i:translation><i:text for="L1">sidecar</i:text></i:translation></i:translations></i:iTunesMetadata>` +
		`</metadata></head><body><p begin="1s" end="2s" i:key="L1"><span begin="1s" end="2s">main</span><span ttm:role="x-translation">inline</span></p></body></tt>`
	result, err := Parse(context.Background(), []byte(data), common.Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := result.Document.Line(0)
	if line.ID() != "L1" || line.Translation() != "inline" {
		t.Fatalf("legacy key or inline precedence mismatch: id=%q translation=%q", line.ID(), line.Translation())
	}
	found := false
	for index := 0; index < result.Document.PropertyCount(); index++ {
		property := result.Document.Property(index)
		found = found || property.Name == "songwriters" && property.Value == "Legacy"
	}
	if !found {
		t.Fatal("legacy sidecar songwriter was not retained")
	}
}
