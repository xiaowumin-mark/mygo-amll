// Command formats demonstrates bounded parsing and visual queries without a UI.
// The built-in lyrics are synthetic. File access belongs to this host command.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/format/lrc"
	"github.com/xiaowumin-mark/mygo-amll/format/qrc"
	"github.com/xiaowumin-mark/mygo-amll/format/ttml"
	"github.com/xiaowumin-mark/mygo-amll/format/yrc"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func main() {
	name := flag.String("format", "lrc", "lrc, yrc, qrc or ttml")
	input := flag.String("input", "", "optional UTF-8 lyric file; default is a synthetic sample")
	at := flag.Duration("at", 2500*time.Millisecond, "absolute song position")
	duration := flag.Duration("duration", 0, "optional absolute track end for inferred final line ends")
	strict := flag.Bool("strict", false, "promote parser warnings to errors")
	flag.Parse()
	parse := map[string]func(context.Context, []byte, common.Options) (common.Result, error){
		"lrc": lrc.Parse, "yrc": yrc.Parse, "qrc": qrc.Parse, "ttml": ttml.Parse,
	}[*name]
	if parse == nil {
		log.Fatal("format must be lrc, yrc, qrc or ttml")
	}
	samples := map[string]string{
		"lrc":  "[ti:Synthetic sample]\n[00:01]<00:01>A new <00:02>morning<00:03>\n[00:10]<00:10>Next line<00:12>\n",
		"yrc":  "[ti:Synthetic sample]\n[1000,2000](1000,1000,0)A new (2000,1000,0)morning\n[10000,2000](10000,2000,0)Next line\n",
		"qrc":  "[ti:Synthetic sample]\n[1000,2000]A new (1000,1000)morning(2000,1000)\n[10000,2000]Next line(10000,2000)\n",
		"ttml": `<tt xmlns="http://www.w3.org/ns/ttml"><body><div><p xml:id="first" begin="1s" end="3s" xml:space="preserve"><span begin="1s" end="2s">A new </span><span begin="2s" end="3s">morning</span></p><p begin="10s" end="12s"><span begin="10s" end="12s">Next line</span></p></div></body></tt>`,
	}
	data := []byte(samples[*name])
	if *input != "" {
		file, err := os.Open(*input)
		if err != nil {
			log.Fatal(err)
		}
		data, err = io.ReadAll(io.LimitReader(file, int64(common.DefaultLimits().MaxBytes)+1))
		closeErr := file.Close()
		if err != nil {
			log.Fatal(err)
		}
		if closeErr != nil {
			log.Fatal(closeErr)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	result, err := parse(ctx, data, common.Options{Strict: *strict, Duration: *duration})
	if err != nil {
		log.Fatal(err)
	}
	document := result.Document
	lines := make([]lyric.Line, document.Len())
	for i := range lines {
		v := document.Line(i)
		line := lyric.Line{ID: v.ID(), Text: v.Text(), Translation: v.Translation(), Romanization: v.Romanization(), Start: v.Start(), End: v.End(), Duet: v.Duet(), Background: v.Background(), Agent: v.Agent(), Language: v.Language()}
		for j := 0; j < v.WordCount(); j++ {
			line.Words = append(line.Words, v.Word(j))
		}
		lines[i] = line
	}
	properties := make([]lyric.Property, document.PropertyCount())
	for i := range properties {
		properties[i] = document.Property(i)
	}
	index, err := timeline.New(document)
	if err != nil {
		log.Fatal(err)
	}
	output := struct {
		TimeUnit    string
		Lines       []lyric.Line
		Metadata    []lyric.Property
		Diagnostics []common.Diagnostic
		Visual      timeline.VisualState
	}{"nanoseconds (Go time.Duration)", lines, properties, result.Diagnostics, index.VisualAt(*at, nil, nil)}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))
}
