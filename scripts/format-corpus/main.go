package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/format/lrc"
	"github.com/xiaowumin-mark/mygo-amll/format/qrc"
	"github.com/xiaowumin-mark/mygo-amll/format/yrc"
)

type record struct {
	path, format, sha, kind string
	diagnostics             []common.Diagnostic
	lines, words            int
	canonicalSHA            string
	canonical               []canonicalLine
}

type canonicalWord struct {
	Text    string `json:"text"`
	StartMS int64  `json:"startMS"`
	EndMS   int64  `json:"endMS"`
}

type canonicalLine struct {
	StartMS    int64           `json:"startMS"`
	EndMS      int64           `json:"endMS"`
	Words      []canonicalWord `json:"words"`
	Background bool            `json:"background"`
}

type statistics struct {
	Files          int                 `json:"files"`
	Unique         int                 `json:"unique"`
	Failures       int                 `json:"failures"`
	UniqueFailures int                 `json:"uniqueFailures"`
	Lines          int                 `json:"lines"`
	Words          int                 `json:"words"`
	Kinds          map[string]int      `json:"failureKinds"`
	Diagnostics    map[string]int      `json:"diagnostics"`
	Examples       map[string][]string `json:"examples"`
	inputs         map[string]bool
	failures       map[string]bool
}

func main() {
	root := flag.String("root", "../amll-ttml-db", "corpus root")
	records := flag.String("records", "", "optional per-path canonical summary JSONL")
	includeCanonical := flag.Bool("canonical", false, "include fields in JSONL")
	workers := flag.Int("workers", 4, "parallel workers (1..32)")
	expected := flag.String("expect-corpus", "6007fcdf33bf838bea0304292992bbdc1d6e2bea", "required corpus commit")
	flag.Parse()
	if *workers < 1 || *workers > 32 {
		panic("workers must be 1..32")
	}
	corpusCommit := strings.TrimSpace(string(git(*root, "rev-parse", "--verify", "HEAD")))
	if corpusCommit != *expected {
		panic("corpus commit differs from -expect-corpus")
	}
	if len(git(*root, "status", "--porcelain", "--untracked-files=no")) != 0 {
		panic("corpus tracked files are modified")
	}
	parserCommit := strings.TrimSpace(string(git(".", "rev-parse", "--verify", "HEAD")))
	parserDirty := len(git(".", "status", "--porcelain", "--untracked-files=no")) != 0
	var paths []string
	for _, path := range strings.Split(string(git(*root, "ls-files", "-z", "--", "*.lrc", "*.eslrc", "*.yrc", "*.qrc")), "\x00") {
		if path != "" {
			paths = append(paths, filepath.Join(*root, filepath.FromSlash(path)))
		}
	}
	if len(paths) == 0 {
		panic("no tracked supported-format paths found")
	}
	sort.Strings(paths)
	jobs := make(chan string)
	results := make(chan record, *workers)
	var group sync.WaitGroup
	for range *workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for path := range jobs {
				results <- parse(path)
			}
		}()
	}
	go func() {
		for _, path := range paths {
			jobs <- path
		}
		close(jobs)
		group.Wait()
		close(results)
	}()
	stats := make(map[string]*statistics)
	var recordEncoder *json.Encoder
	if *records != "" {
		file, err := os.Create(*records)
		if err != nil {
			panic(err)
		}
		defer file.Close()
		recordEncoder = json.NewEncoder(file)
		recordEncoder.SetEscapeHTML(false)
	}
	seen := make(map[string]string)
	identicalInputMismatch := 0
	for result := range results {
		s := stats[result.format]
		if s == nil {
			s = &statistics{Kinds: map[string]int{}, Diagnostics: map[string]int{}, Examples: map[string][]string{}, inputs: map[string]bool{}, failures: map[string]bool{}}
			stats[result.format] = s
		}
		s.Files++
		s.inputs[result.sha] = true
		s.Lines += result.lines
		s.Words += result.words
		for _, diag := range result.diagnostics {
			s.Diagnostics[diag.Code]++
		}
		if result.kind != "" {
			s.Failures++
			s.failures[result.sha] = true
			s.Kinds[result.kind]++
			if len(s.Examples[result.kind]) < 5 {
				s.Examples[result.kind] = append(s.Examples[result.kind], result.path)
			}
		}
		key := result.format + ":" + result.sha
		previousHash, duplicate := seen[key]
		if duplicate && previousHash != result.canonicalSHA {
			identicalInputMismatch++
		}
		seen[key] = result.canonicalSHA
		if recordEncoder != nil {
			relative, err := filepath.Rel(*root, result.path)
			if err != nil {
				panic(err)
			}
			entry := map[string]any{"path": filepath.ToSlash(relative), "format": result.format, "sourceSHA256": result.sha, "canonicalSHA256": result.canonicalSHA, "error": result.kind, "lineCount": result.lines}
			if *includeCanonical && !duplicate {
				entry["canonical"] = result.canonical
			}
			if err := recordEncoder.Encode(entry); err != nil {
				panic(err)
			}
		}
	}
	for _, s := range stats {
		s.Unique = len(s.inputs)
		s.UniqueFailures = len(s.failures)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"corpusCommit": corpusCommit, "parserCommit": parserCommit, "parserDirty": parserDirty, "workers": *workers, "formats": stats, "identicalInputMismatches": identicalInputMismatch}); err != nil {
		panic(err)
	}
	for _, stat := range stats {
		if stat.Failures != 0 {
			os.Exit(1)
		}
	}
	if identicalInputMismatch != 0 {
		os.Exit(1)
	}
}

func git(root string, args ...string) []byte {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.Output()
	if err != nil {
		panic(fmt.Sprintf("git %v: %v: %s", args, err, output))
	}
	return output
}

func parse(path string) (result record) {
	result.path, result.format = path, strings.ToLower(filepath.Ext(path))
	defer func() {
		if value := recover(); value != nil {
			result.kind = fmt.Sprintf("panic:%T", value)
		}
	}()
	data, err := readInput(path)
	if err != nil {
		result.kind = "read-error"
		return
	}
	result.sha = fmt.Sprintf("%x", sha256.Sum256(data))
	var parsed common.Result
	switch result.format {
	case ".lrc", ".eslrc":
		parsed, err = lrc.Parse(context.Background(), data, common.Options{})
	case ".yrc":
		parsed, err = yrc.Parse(context.Background(), data, common.Options{})
	case ".qrc":
		parsed, err = qrc.Parse(context.Background(), data, common.Options{})
	}
	result.diagnostics = parsed.Diagnostics
	if err != nil {
		message := err.Error()
		switch {
		case strings.Contains(message, "diagnostic limit"):
			result.kind = "diagnostic-limit"
		case strings.Contains(message, "precedes previous word"):
			result.kind = "nonmonotonic-word"
		case strings.Contains(message, "nondecreasing"):
			result.kind = "nondecreasing-word"
		case strings.Contains(message, "line.start <= start < end <= line.end"):
			result.kind = "word-outside-line-or-reversed"
		case strings.Contains(message, "zero-duration word"):
			result.kind = "point-word-outside-line"
		case strings.Contains(message, "require line start < end"):
			result.kind = "point-or-reversed-line"
		case strings.Contains(message, "word exceeds inferred line end"):
			result.kind = "word-exceeds-inferred-end"
		case strings.Contains(message, "time offset"):
			result.kind = "offset-overflow"
		default:
			result.kind = message
		}
		return
	}
	if parsed.Document == nil {
		result.kind = "nil-document"
		return
	}
	result.lines = parsed.Document.Len()
	for index := 0; index < result.lines; index++ {
		line := parsed.Document.Line(index)
		result.words += line.WordCount()
		entry := canonicalLine{StartMS: line.Start().Milliseconds(), EndMS: line.End().Milliseconds(), Words: []canonicalWord{}, Background: line.Background()}
		for index := 0; index < line.WordCount(); index++ {
			word := line.Word(index)
			entry.Words = append(entry.Words, canonicalWord{Text: word.Text, StartMS: word.Start.Milliseconds(), EndMS: word.End.Milliseconds()})
		}
		result.canonical = append(result.canonical, entry)
		if result.format == ".lrc" || result.format == ".eslrc" {
			if translation := line.Translation(); translation != "" {
				for _, text := range strings.Split(translation, "\n") {
					result.canonical = append(result.canonical, canonicalLine{StartMS: entry.StartMS, EndMS: entry.EndMS, Words: []canonicalWord{{Text: text, StartMS: entry.StartMS, EndMS: entry.EndMS}}})
				}
			}
		}
	}
	if result.canonical == nil {
		result.canonical = []canonicalLine{}
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result.canonical); err != nil {
		panic(err)
	}
	result.canonicalSHA = fmt.Sprintf("%x", sha256.Sum256(bytes.TrimSuffix(output.Bytes(), []byte{'\n'})))
	return
}

func readInput(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("corpus input must be a regular file")
	}
	maximum := int64(common.DefaultLimits().MaxBytes)
	if info.Size() > maximum {
		return nil, fmt.Errorf("corpus file exceeds default input limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err == nil && int64(len(data)) > maximum {
		return nil, fmt.Errorf("corpus file exceeds default input limit")
	}
	return data, err
}
