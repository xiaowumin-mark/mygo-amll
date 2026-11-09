// Command timeline demonstrates the UI-independent core with synthetic lyrics.
package main

import (
	"fmt"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func main() {
	document, err := lyric.NewDocument([]lyric.Line{
		{Text: "A new morning", Start: 0, End: 3 * time.Second},
		{Text: "A second voice", Start: 2 * time.Second, End: 4 * time.Second, Duet: true},
	})
	if err != nil {
		panic(err)
	}
	index, err := timeline.New(document)
	if err != nil {
		panic(err)
	}
	active := make([]int, 0, document.Len())
	for _, lineIndex := range index.ActiveAt(2500*time.Millisecond, active) {
		fmt.Println(document.Line(lineIndex).Text())
	}
}
