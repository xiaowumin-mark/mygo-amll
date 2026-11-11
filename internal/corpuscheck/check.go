// Package corpuscheck performs offline, bounded validation of a tracked TTML
// corpus. It is tooling, not part of the lyric parser or its per-frame path.
package corpuscheck

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/format/ttml"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Config controls one run. Corpus and ParserRoot are Git checkouts. Workers is
// bounded to 1..32; zero selects at most four workers. Records receives sorted
// JSONL, one entry per tracked TTML path. Canonical includes lyric text in that
// local output; the default records contain only hashes, counts and errors.
type Config struct {
	Corpus               string
	ParserRoot           string
	ExpectedCorpusCommit string
	Workers              int
	Records              io.Writer
	Canonical            bool
}

// Revision identifies the source measured by this run. Dirty is especially
// relevant for the parser worktree, where candidate fixes may be uncommitted.
type Revision struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

// Failure identifies a path without retaining its document or input bytes.
type Failure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// TimingStats reports authored timing anomalies without repairing or dropping
// their source fields. Words and Ruby retain source text order and endpoints.
type TimingStats struct {
	PointLines        int `json:"pointLines"`
	ReversedLines     int `json:"reversedLines"`
	PointWords        int `json:"pointWords"`
	ReversedWords     int `json:"reversedWords"`
	NonmonotonicWords int `json:"nonmonotonicWords"`
	OutsideWords      int `json:"outsideWords"`
	PointRuby         int `json:"pointRuby"`
	ReversedRuby      int `json:"reversedRuby"`
	NonmonotonicRuby  int `json:"nonmonotonicRuby"`
	OutsideRuby       int `json:"outsideRuby"`
}

// Report is aggregate evidence, not a claim that every source feature has an
// equivalent representation. Reference comparisons remain a separate gate.
type Report struct {
	Corpus       Revision       `json:"corpus"`
	Parser       Revision       `json:"parser"`
	Limits       common.Limits  `json:"limits"`
	Workers      int            `json:"workers"`
	Files        int            `json:"files"`
	UniqueInputs int            `json:"uniqueInputs"`
	Bytes        int64          `json:"bytes"`
	CheckoutCRLF int            `json:"checkoutCRLF"`
	Lines        int64          `json:"lines"`
	Words        int64          `json:"words"`
	Ruby         int64          `json:"ruby"`
	Diagnostics  map[string]int `json:"diagnostics"`
	Failures     []Failure      `json:"failures"`
	SourceTiming TimingStats    `json:"sourceTiming"`
}

// Record summarizes one tracked path. The three canonical hashes use compact
// UTF-8 JSON with HTML escaping disabled. Canonical is emitted only on request.
type Record struct {
	Path            string              `json:"path"`
	Blob            string              `json:"blob"`
	Bytes           int                 `json:"bytes"`
	CheckoutCRLF    bool                `json:"checkoutCRLF,omitempty"`
	SourceSHA256    string              `json:"sourceSHA256,omitempty"`
	CanonicalSHA256 string              `json:"canonicalSHA256,omitempty"`
	LinesSHA256     string              `json:"linesSHA256,omitempty"`
	MetadataSHA256  string              `json:"metadataSHA256,omitempty"`
	SnapshotSHA256  string              `json:"snapshotSHA256,omitempty"`
	LineCount       int                 `json:"lineCount"`
	WordCount       int                 `json:"wordCount"`
	RubyCount       int                 `json:"rubyCount"`
	MetadataCount   int                 `json:"metadataCount"`
	Diagnostics     []common.Diagnostic `json:"diagnostics,omitempty"`
	Error           string              `json:"error,omitempty"`
	ErrorCode       string              `json:"errorCode,omitempty"`
	PartialDocument bool                `json:"partialDocument,omitempty"`
	Canonical       *CanonicalDocument  `json:"canonical,omitempty"`
	SourceTiming    TimingStats         `json:"sourceTiming"`
}

// CanonicalDocument contains the fields shared with AMLL's simplified lyric
// output. Source identity, full line text, language and parent links participate
// in SnapshotSHA256 instead. Metadata groups duplicate keys in source order and
// keeps each distinct value once. No whitespace or Unicode normalization is
// applied here; time fields are exact integer milliseconds.
type CanonicalDocument struct {
	Lines    []CanonicalLine `json:"lines"`
	Metadata [][]any         `json:"metadata"`
}

type CanonicalLine struct {
	Words        []CanonicalWord `json:"words"`
	Translation  string          `json:"translation"`
	Romanization string          `json:"romanization"`
	StartMS      int64           `json:"startMS"`
	EndMS        int64           `json:"endMS"`
	Background   bool            `json:"background"`
	Duet         bool            `json:"duet"`
}

type CanonicalWord struct {
	Text         string          `json:"text"`
	Romanization string          `json:"romanization"`
	StartMS      int64           `json:"startMS"`
	EndMS        int64           `json:"endMS"`
	Obscene      bool            `json:"obscene"`
	EmptyBeat    int             `json:"emptyBeat"`
	Ruby         []CanonicalRuby `json:"ruby"`
}

type CanonicalRuby struct {
	Text    string `json:"text"`
	StartMS int64  `json:"startMS"`
	EndMS   int64  `json:"endMS"`
}

type trackedFile struct {
	path, blob, mode string
}

// Run checks every tracked .ttml path (case-insensitive extension), including
// historical and issue directories. Each file is parsed twice with default
// limits, independently checked, and checked again after its input is mutated.
// Only at most Workers inputs/documents are processed together; full outputs
// are discarded after a sorted batch is written. Report memory scales with
// distinct source hashes and failures, not with all parsed lyric contents.
func Run(ctx context.Context, config Config) (report Report, err error) {
	if ctx == nil {
		return report, fmt.Errorf("corpuscheck: nil context")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if config.Corpus == "" || config.ParserRoot == "" {
		return report, fmt.Errorf("corpuscheck: corpus and parser root are required")
	}
	workers := config.Workers
	if workers == 0 {
		workers = min(4, runtime.GOMAXPROCS(0))
	}
	if workers < 1 || workers > 32 {
		return report, fmt.Errorf("corpuscheck: workers must be between 1 and 32")
	}
	report.Workers, report.Limits = workers, common.DefaultLimits()
	report.Diagnostics = make(map[string]int)
	report.Failures = []Failure{}
	if report.Corpus, err = revision(ctx, config.Corpus); err != nil {
		return report, err
	}
	if report.Parser, err = revision(ctx, config.ParserRoot); err != nil {
		return report, err
	}
	if report.Corpus.Dirty {
		return report, fmt.Errorf("corpuscheck: corpus has tracked changes; use a clean pinned checkout")
	}
	if config.ExpectedCorpusCommit != "" && config.ExpectedCorpusCommit != report.Corpus.Commit {
		return report, fmt.Errorf("corpuscheck: corpus commit %s differs from expected %s", report.Corpus.Commit, config.ExpectedCorpusCommit)
	}
	files, err := discover(ctx, config.Corpus)
	if err != nil {
		return report, err
	}
	if len(files) == 0 {
		return report, fmt.Errorf("corpuscheck: no tracked TTML files")
	}
	var encoder *json.Encoder
	if config.Records != nil {
		encoder = json.NewEncoder(config.Records)
		encoder.SetEscapeHTML(false)
	}
	hashes := make(map[string]string)
	for first := 0; first < len(files); first += workers {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		batch := files[first:min(first+workers, len(files))]
		records := make([]Record, len(batch))
		var group sync.WaitGroup
		for index, file := range batch {
			group.Add(1)
			go func(index int, file trackedFile) {
				defer group.Done()
				records[index] = checkFile(ctx, config.Corpus, file, config.Canonical)
			}(index, file)
		}
		group.Wait()
		if err := ctx.Err(); err != nil {
			return report, err
		}
		for _, record := range records {
			if record.SourceSHA256 != "" {
				if previous, exists := hashes[record.SourceSHA256]; exists && record.Error == "" && previous != record.SnapshotSHA256 {
					record.Error = "identical source bytes produced different document snapshots"
				} else if !exists {
					hashes[record.SourceSHA256] = record.SnapshotSHA256
				}
			}
			report.Files++
			report.Bytes += int64(record.Bytes)
			if record.CheckoutCRLF {
				report.CheckoutCRLF++
			}
			report.Lines += int64(record.LineCount)
			report.Words += int64(record.WordCount)
			report.Ruby += int64(record.RubyCount)
			report.SourceTiming.add(record.SourceTiming)
			for _, diagnostic := range record.Diagnostics {
				report.Diagnostics[diagnostic.Code]++
			}
			if record.Error != "" {
				report.Failures = append(report.Failures, Failure{Path: record.Path, Error: record.Error})
			}
			if encoder != nil {
				if err := encoder.Encode(record); err != nil {
					return report, fmt.Errorf("corpuscheck: write records: %w", err)
				}
			}
		}
	}
	report.UniqueInputs = len(hashes)
	return report, nil
}

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("corpuscheck: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func revision(ctx context.Context, root string) (Revision, error) {
	commit, err := git(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Revision{}, err
	}
	status, err := git(ctx, root, "status", "--porcelain", "--untracked-files=no")
	return Revision{Commit: strings.TrimSpace(string(commit)), Dirty: len(status) != 0}, err
}

func discover(ctx context.Context, root string) ([]trackedFile, error) {
	output, err := git(ctx, root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	var files []trackedFile
	for _, entry := range bytes.Split(output, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		header, name, ok := bytes.Cut(entry, []byte{'\t'})
		fields := strings.Fields(string(header))
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return nil, fmt.Errorf("corpuscheck: invalid or conflicted Git index entry")
		}
		path := string(name)
		if !strings.EqualFold(filepath.Ext(path), ".ttml") {
			continue
		}
		if filepath.IsAbs(path) || filepath.Clean(filepath.FromSlash(path)) == ".." || strings.HasPrefix(filepath.Clean(filepath.FromSlash(path)), ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("corpuscheck: tracked path escapes corpus")
		}
		files = append(files, trackedFile{path: path, blob: fields[1], mode: fields[0]})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

func checkFile(ctx context.Context, root string, file trackedFile, includeCanonical bool) (record Record) {
	record.Path, record.Blob = file.path, file.blob
	defer func() {
		if value := recover(); value != nil {
			record.Error = fmt.Sprintf("parser/check panic: %v", value)
		}
	}()
	if file.mode != "100644" && file.mode != "100755" {
		record.Error = "tracked TTML is not a regular file"
		return record
	}
	path := filepath.Join(root, filepath.FromSlash(file.path))
	input, err := readInput(path)
	if err != nil {
		record.Error = err.Error()
		return record
	}
	record.Bytes, record.SourceSHA256 = len(input), digest(input)
	if blobHash(input, len(file.blob)) != file.blob {
		// A clean Windows checkout can contain CRLF where Git stores LF. This
		// proof only identifies the checkout transform: Parse still receives
		// the original bytes, and SourceSHA256 hashes those exact bytes.
		if bytes.Contains(input, []byte("\r\n")) && blobHash(bytes.ReplaceAll(input, []byte("\r\n"), []byte("\n")), len(file.blob)) == file.blob {
			record.CheckoutCRLF = true
		} else {
			record.Error = "input bytes differ from the tracked Git blob"
			return record
		}
	}
	parsed, err := ttml.Parse(ctx, input, common.Options{})
	record.Diagnostics = parsed.Diagnostics
	if err != nil {
		record.Error = err.Error()
		var parseError *common.ParseError
		if errors.As(err, &parseError) {
			record.ErrorCode = parseError.Code
		}
		record.PartialDocument = parsed.Document != nil
		return record
	}
	if parsed.Document == nil {
		record.Error = "parser returned a nil document without an error"
		return record
	}
	if err = invariants(parsed.Document); err != nil {
		record.Error = err.Error()
		return record
	}
	before := snapshot(parsed)
	repeated, err := ttml.Parse(ctx, input, common.Options{})
	if err != nil || repeated.Document == nil || !bytes.Equal(before, snapshot(repeated)) {
		record.Error = fmt.Sprintf("repeated parse differs: %v", err)
		return record
	}
	for index := range input {
		input[index] = '#'
	}
	if !bytes.Equal(before, snapshot(parsed)) || !bytes.Equal(before, snapshot(repeated)) {
		record.Error = "document retained mutable input bytes"
		return record
	}
	canonical := canonicalize(parsed.Document)
	record.SourceTiming = timingStats(parsed.Document)
	record.LineCount = parsed.Document.Len()
	for _, line := range canonical.Lines {
		record.WordCount += len(line.Words)
		for _, word := range line.Words {
			record.RubyCount += len(word.Ruby)
		}
	}
	record.MetadataCount = len(canonical.Metadata)
	record.LinesSHA256 = digest(compactJSON(canonical.Lines))
	record.MetadataSHA256 = digest(compactJSON(canonical.Metadata))
	record.CanonicalSHA256 = digest(compactJSON(canonical))
	record.SnapshotSHA256 = digest(before)
	if includeCanonical {
		record.Canonical = &canonical
	}
	return record
}

func readInput(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source path is not a regular file")
	}
	limit := common.DefaultLimits().MaxBytes
	if info.Size() > int64(limit) {
		return nil, fmt.Errorf("source exceeds default input budget of %d bytes", limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	input, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err == nil && len(input) > limit {
		err = fmt.Errorf("source exceeds default input budget of %d bytes", limit)
	}
	return input, err
}

func blobHash(input []byte, hashLength int) string {
	var digest hash.Hash
	if hashLength == sha256.Size*2 {
		digest = sha256.New()
	} else {
		digest = sha1.New()
	}
	io.WriteString(digest, "blob "+strconv.Itoa(len(input))+"\x00")
	digest.Write(input)
	return hex.EncodeToString(digest.Sum(nil))
}

func digest(input []byte) string {
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}

func compactJSON(value any) []byte {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		panic(err)
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'})
}

func snapshot(result common.Result) []byte {
	doc := result.Document
	lines := make([]lyric.Line, doc.Len())
	for index := range lines {
		line := doc.Line(index)
		lines[index] = lyric.Line{ID: line.ID(), ParentID: line.ParentID(), Text: line.Text(),
			Translation: line.Translation(), Romanization: line.Romanization(),
			Start: line.Start(), End: line.End(), Background: line.Background(), Duet: line.Duet(),
			Agent: line.Agent(), Language: line.Language(), Words: make([]lyric.Word, line.WordCount())}
		for word := range lines[index].Words {
			lines[index].Words[word] = line.Word(word)
		}
	}
	properties := make([]lyric.Property, doc.PropertyCount())
	for index := range properties {
		properties[index] = doc.Property(index)
	}
	return compactJSON(struct {
		Lines       []lyric.Line
		Properties  []lyric.Property
		Diagnostics []common.Diagnostic
	}{lines, properties, result.Diagnostics})
}

func canonicalize(doc *lyric.Document) CanonicalDocument {
	output := CanonicalDocument{Lines: make([]CanonicalLine, 0, doc.Len()), Metadata: [][]any{}}
	for index := 0; index < doc.Len(); index++ {
		line := doc.Line(index)
		entry := CanonicalLine{Words: make([]CanonicalWord, 0, line.WordCount()), Translation: line.Translation(),
			Romanization: line.Romanization(), StartMS: line.Start().Milliseconds(), EndMS: line.End().Milliseconds(),
			Background: line.Background(), Duet: line.Duet()}
		for wordIndex := 0; wordIndex < line.WordCount(); wordIndex++ {
			word := line.Word(wordIndex)
			entryWord := CanonicalWord{Text: word.Text, Romanization: word.Romanization,
				StartMS: word.Start.Milliseconds(), EndMS: word.End.Milliseconds(),
				Obscene: word.Obscene, EmptyBeat: word.EmptyBeat, Ruby: []CanonicalRuby{}}
			for _, ruby := range word.Ruby {
				entryWord.Ruby = append(entryWord.Ruby, CanonicalRuby{Text: ruby.Text, StartMS: ruby.Start.Milliseconds(), EndMS: ruby.End.Milliseconds()})
			}
			entry.Words = append(entry.Words, entryWord)
		}
		output.Lines = append(output.Lines, entry)
	}
	indices := make(map[string]int)
	for index := 0; index < doc.PropertyCount(); index++ {
		property := doc.Property(index)
		at, exists := indices[property.Name]
		if !exists {
			at = len(output.Metadata)
			indices[property.Name] = at
			output.Metadata = append(output.Metadata, []any{property.Name, []string{}})
		}
		values := output.Metadata[at][1].([]string)
		found := false
		for _, value := range values {
			found = found || value == property.Value
		}
		if !found {
			output.Metadata[at][1] = append(values, property.Value)
		}
	}
	return output
}

func invariants(doc *lyric.Document) error {
	indices := make(map[string]int, doc.Len())
	for index := 0; index < doc.Len(); index++ {
		line := doc.Line(index)
		if line.ID() == "" {
			return fmt.Errorf("line %d has an empty identity", index)
		}
		if _, exists := indices[line.ID()]; exists {
			return fmt.Errorf("line %d repeats an identity", index)
		}
		indices[line.ID()] = index
		if line.Start() < 0 || line.End() < 0 || index > 0 && line.Start() < doc.Line(index-1).Start() {
			return fmt.Errorf("line %d has invalid or unsorted timing", index)
		}
		for wordIndex := 0; wordIndex < line.WordCount(); wordIndex++ {
			word := line.Word(wordIndex)
			if word.Start < 0 || word.End < 0 {
				return fmt.Errorf("line %d word %d has a negative time endpoint", index, wordIndex)
			}
			for rubyIndex, ruby := range word.Ruby {
				if ruby.Start < 0 || ruby.End < 0 {
					return fmt.Errorf("line %d word %d ruby %d has a negative time endpoint", index, wordIndex, rubyIndex)
				}
			}
		}
	}
	for index := 0; index < doc.Len(); index++ {
		line := doc.Line(index)
		if line.ParentID() == "" {
			continue
		}
		parent, exists := indices[line.ParentID()]
		if !line.Background() || !exists || doc.Line(parent).Background() {
			return fmt.Errorf("line %d has an invalid background parent", index)
		}
	}
	return nil
}

func timingStats(doc *lyric.Document) (stats TimingStats) {
	for index := 0; index < doc.Len(); index++ {
		line := doc.Line(index)
		if line.Start() == line.End() {
			stats.PointLines++
		}
		if line.End() < line.Start() {
			stats.ReversedLines++
		}
		for wordIndex := 0; wordIndex < line.WordCount(); wordIndex++ {
			word := line.Word(wordIndex)
			if word.Start == word.End {
				stats.PointWords++
			}
			if word.End < word.Start {
				stats.ReversedWords++
			}
			if wordIndex > 0 && word.Start < line.Word(wordIndex-1).Start {
				stats.NonmonotonicWords++
			}
			if min(word.Start, word.End) < line.Start() || max(word.Start, word.End) > line.End() {
				stats.OutsideWords++
			}
			for rubyIndex, ruby := range word.Ruby {
				if ruby.Start == ruby.End {
					stats.PointRuby++
				}
				if ruby.End < ruby.Start {
					stats.ReversedRuby++
				}
				if rubyIndex > 0 && ruby.Start < word.Ruby[rubyIndex-1].Start {
					stats.NonmonotonicRuby++
				}
				if min(ruby.Start, ruby.End) < min(word.Start, word.End) || max(ruby.Start, ruby.End) > max(word.Start, word.End) {
					stats.OutsideRuby++
				}
			}
		}
	}
	return stats
}

func (stats *TimingStats) add(other TimingStats) {
	stats.PointLines += other.PointLines
	stats.ReversedLines += other.ReversedLines
	stats.PointWords += other.PointWords
	stats.ReversedWords += other.ReversedWords
	stats.NonmonotonicWords += other.NonmonotonicWords
	stats.OutsideWords += other.OutsideWords
	stats.PointRuby += other.PointRuby
	stats.ReversedRuby += other.ReversedRuby
	stats.NonmonotonicRuby += other.NonmonotonicRuby
	stats.OutsideRuby += other.OutsideRuby
}

// CheckCancellation confirms the parser's public cancellation contract with a
// fixed synthetic input. This does not claim to measure cancellation latency.
func CheckCancellation() error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ttml.Parse(ctx, []byte(`<tt><body><p begin="0s" end="1s">synthetic</p></body></tt>`), common.Options{})
	if err != context.Canceled {
		return fmt.Errorf("corpuscheck: canceled parse returned %v", err)
	}
	return nil
}
