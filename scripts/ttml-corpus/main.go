// Command ttml-corpus validates every tracked TTML file in a clean Git corpus.
// Run it from the parser checkout, for example:
//
//	go run ./scripts/ttml-corpus -corpus ../amll-ttml-db -records /tmp/corpus.jsonl
//
// JSONL with -canonical includes lyric text and belongs in local temporary
// storage, never in this repository. The tool performs no network operations.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/xiaowumin-mark/mygo-amll/internal/corpuscheck"
)

func main() { os.Exit(run()) }

func run() int {
	corpus := flag.String("corpus", "", "clean amll-ttml-db Git checkout")
	parserRoot := flag.String("parser-root", ".", "parser Git checkout for provenance")
	expected := flag.String("expect-corpus", "", "require this exact full corpus commit")
	workers := flag.Int("workers", 0, "parallel files, 1..32; default at most 4")
	records := flag.String("records", "", "write sorted per-path JSONL to this local file")
	canonical := flag.Bool("canonical", false, "include canonical lyric contents in records")
	flag.Parse()
	if *corpus == "" || *canonical && *records == "" {
		fmt.Fprintln(os.Stderr, "-corpus is required; -canonical also requires -records")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := corpuscheck.CheckCancellation(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	config := corpuscheck.Config{Corpus: *corpus, ParserRoot: *parserRoot,
		ExpectedCorpusCommit: *expected, Workers: *workers, Canonical: *canonical}
	if *records != "" {
		output, err := os.OpenFile(*records, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer output.Close()
		config.Records = output
	}
	report, err := corpuscheck.Run(ctx, config)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if encodeErr := encoder.Encode(report); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(report.Failures) != 0 {
		return 1
	}
	return 0
}
