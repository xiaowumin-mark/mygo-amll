// Package formatutil contains the bounded cold-path parser workspace.
package formatutil

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Line retains a source location across stable sorting and normalization.
type Line struct {
	lyric.Line
	SourceLine int
	InferEnd   bool
	// PreserveTiming retains source-order point and non-monotonic word timing.
	// AMLL-compatible parsers need this for historical corpus entries and
	// generated zero-timed spaces. Other callers retain strict normalization.
	PreserveTiming bool
}

// Parser is private to one parse call. It never retains source bytes.
type Parser struct {
	Options     common.Options
	Diagnostics []common.Diagnostic
	ctx         context.Context
	lines       int
	words       int
}

// Error creates a fatal diagnostic without appending it to the warning list.
func Error(line, column int, code, message string) error {
	return &common.ParseError{Diagnostic: common.Diagnostic{Code: code, Message: message, Line: line, Column: column}}
}

func New(ctx context.Context, data []byte, options common.Options) (*Parser, error) {
	if ctx == nil {
		return nil, Error(1, 1, "options", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defaults := common.DefaultLimits()
	fields := []*int{&options.Limits.MaxBytes, &options.Limits.MaxTextBytes, &options.Limits.MaxLines, &options.Limits.MaxWords, &options.Limits.MaxDepth, &options.Limits.MaxDiagnostics}
	values := []int{defaults.MaxBytes, defaults.MaxTextBytes, defaults.MaxLines, defaults.MaxWords, defaults.MaxDepth, defaults.MaxDiagnostics}
	for i, field := range fields {
		if *field < 0 {
			return nil, Error(1, 1, "options", "limits must be nonnegative")
		}
		if *field == 0 {
			*field = values[i]
		}
	}
	if options.Duration < 0 || options.FinalLineDuration < 0 {
		return nil, Error(1, 1, "options", "durations must be nonnegative")
	}
	if options.FinalLineDuration == 0 {
		options.FinalLineDuration = 5 * time.Second
	}
	if len(data) > options.Limits.MaxBytes {
		return nil, Error(1, 1, "limit", "input byte limit exceeded")
	}
	line, column, nextCheck := 1, 1, 0
	for i := 0; i < len(data); {
		if i >= nextCheck {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			nextCheck = i + 65536
		}
		if line > options.Limits.MaxLines {
			return nil, Error(line, column, "limit", "physical line limit exceeded")
		}
		b := data[i]
		if b == '\r' || b == '\n' {
			if b == '\r' && i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
			i++
			line++
			column = 1
			continue
		}
		_, size := utf8.DecodeRune(data[i:])
		if size == 1 && b >= utf8.RuneSelf {
			return nil, Error(line, column, "encoding", "input must be valid UTF-8")
		}
		i += size
		column += size
	}
	return &Parser{Options: options, ctx: ctx}, nil
}

func (p *Parser) Check() error { return p.ctx.Err() }

func (p *Parser) Warn(line, column int, code, message string) error {
	if p.Options.Strict {
		return Error(line, column, code, message)
	}
	if len(p.Diagnostics) >= p.Options.Limits.MaxDiagnostics {
		return Error(line, column, "limit", "diagnostic limit exceeded")
	}
	p.Diagnostics = append(p.Diagnostics, common.Diagnostic{Code: code, Message: message, Line: line, Column: column})
	return nil
}

func (p *Parser) AddLine() error {
	if err := p.Check(); err != nil {
		return err
	}
	if p.lines >= p.Options.Limits.MaxLines {
		return Error(1, 1, "limit", "output line limit exceeded")
	}
	p.lines++
	return nil
}

func (p *Parser) AddWords(n int) error {
	if n < 0 || n > p.Options.Limits.MaxWords-p.words {
		return Error(1, 1, "limit", "word limit exceeded")
	}
	p.words += n
	return p.Check()
}

// Shift checks duration arithmetic before applying an offset.
func Shift(value, offset time.Duration) (time.Duration, error) {
	if offset > 0 && value > time.Duration(math.MaxInt64)-offset || offset < 0 && value < time.Duration(math.MinInt64)-offset {
		return 0, fmt.Errorf("time offset overflow")
	}
	value += offset
	if value < 0 {
		return 0, fmt.Errorf("offset produces negative time")
	}
	return value, nil
}

// Finish normalizes parser-owned lines, then snapshots them into a document.
func (p *Parser) Finish(lines []Line, metadata []lyric.Property, offset time.Duration) (result common.Result, err error) {
	defer func() { result.Diagnostics = p.Diagnostics }()
	if err := p.Check(); err != nil {
		return result, err
	}
	if len(lines) > p.Options.Limits.MaxLines {
		return result, Error(1, 1, "limit", "output line limit exceeded")
	}
	// Repeated timestamps may share source strings until cloning below. Bound
	// expanded strings before any copy, independently of source byte and item
	// counts, so a compact repeated line cannot imply unbounded text snapshots.
	textBytes := 0
	countText := func(sourceLine int, values ...string) error {
		for _, value := range values {
			if len(value) > p.Options.Limits.MaxTextBytes-textBytes {
				return Error(sourceLine, 1, "limit", "expanded text byte limit exceeded")
			}
			textBytes += len(value)
		}
		return nil
	}
	for i, line := range lines {
		if i&255 == 0 {
			if err := p.Check(); err != nil {
				return result, err
			}
		}
		if err := countText(line.SourceLine, line.ID, line.ParentID, line.Text, line.Translation, line.Romanization, line.Agent, line.Language); err != nil {
			return result, err
		}
		for j, word := range line.Words {
			if j&255 == 0 {
				if err := p.Check(); err != nil {
					return result, err
				}
			}
			if err := countText(line.SourceLine, word.Text, word.Romanization); err != nil {
				return result, err
			}
			for _, ruby := range word.Ruby {
				if err := countText(line.SourceLine, ruby.Text); err != nil {
					return result, err
				}
			}
		}
	}
	for i, property := range metadata {
		if i&255 == 0 {
			if err := p.Check(); err != nil {
				return result, err
			}
		}
		if err := countText(1, property.Name, property.Value); err != nil {
			return result, err
		}
	}
	ordered := sort.SliceIsSorted(lines, func(i, j int) bool { return lines[i].Start < lines[j].Start })
	if !ordered {
		if err := p.Warn(1, 1, "sorted", "lines stably sorted by start time"); err != nil {
			return result, err
		}
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].Start < lines[j].Start })
	}
	for i := range lines {
		if i&255 == 0 {
			if err := p.Check(); err != nil {
				return result, err
			}
		}
		line := &lines[i]
		if line.Start, err = Shift(line.Start, offset); err != nil {
			return result, Error(line.SourceLine, 1, "time", err.Error())
		}
		if !line.InferEnd {
			if line.End, err = Shift(line.End, offset); err != nil {
				return result, Error(line.SourceLine, 1, "time", err.Error())
			}
		}
		for j := range line.Words {
			if j&255 == 0 {
				if err := p.Check(); err != nil {
					return result, err
				}
			}
			word := &line.Words[j]
			if word.Start, err = Shift(word.Start, offset); err != nil {
				return result, Error(line.SourceLine, 1, "time", err.Error())
			}
			if word.End >= 0 {
				if word.End, err = Shift(word.End, offset); err != nil {
					return result, Error(line.SourceLine, 1, "time", err.Error())
				}
			}
			for k := range word.Ruby {
				ruby := &word.Ruby[k]
				if ruby.Start, err = Shift(ruby.Start, offset); err != nil {
					return result, Error(line.SourceLine, 1, "time", err.Error())
				}
				if ruby.End, err = Shift(ruby.End, offset); err != nil {
					return result, Error(line.SourceLine, 1, "time", err.Error())
				}
			}
		}
	}
	// Each equal-start group shares the next distinct start: no quadratic scan.
	for first := 0; first < len(lines); {
		if err := p.Check(); err != nil {
			return result, err
		}
		next := first + 1
		for next < len(lines) && lines[next].Start == lines[first].Start {
			if next&255 == 0 {
				if err := p.Check(); err != nil {
					return result, err
				}
			}
			next++
		}
		for i := first; i < next; i++ {
			if i&255 == 0 {
				if err := p.Check(); err != nil {
					return result, err
				}
			}
			line := &lines[i]
			if !line.InferEnd {
				continue
			}
			if next < len(lines) {
				line.End = lines[next].Start
			} else if p.Options.Duration > 0 {
				line.End = p.Options.Duration
			} else {
				line.End, err = Shift(line.Start, p.Options.FinalLineDuration)
				if err != nil {
					return result, Error(line.SourceLine, 1, "time", err.Error())
				}
				if err := p.Warn(line.SourceLine, 1, "inferred-end", "final line end inferred using FinalLineDuration"); err != nil {
					return result, err
				}
			}
			for _, word := range line.Words {
				if !line.PreserveTiming && word.End > line.End {
					return result, Error(line.SourceLine, 1, "time", "word exceeds inferred line end")
				}
			}
		}
		first = next
	}
	output := make([]lyric.Line, len(lines))
	totalWords := 0
	for i := range lines {
		if err := p.Check(); err != nil {
			return result, err
		}
		line := &lines[i]
		if len(line.Words) > p.Options.Limits.MaxWords-totalWords {
			return result, Error(line.SourceLine, 1, "limit", "word limit exceeded")
		}
		totalWords += len(line.Words)
		for _, word := range line.Words {
			if len(word.Ruby) > p.Options.Limits.MaxWords-totalWords {
				return result, Error(line.SourceLine, 1, "limit", "Ruby segment limit exceeded")
			}
			totalWords += len(word.Ruby)
		}
		if !line.PreserveTiming {
			for j, word := range line.Words {
				if j > 0 && word.Start < line.Words[j-1].Start {
					return result, Error(line.SourceLine, 1, "time", fmt.Sprintf("word %d precedes previous word", j))
				}
			}
		}
		n := 0
		for j, word := range line.Words {
			if j&255 == 0 {
				if err := p.Check(); err != nil {
					return result, err
				}
			}
			if word.End < 0 {
				word.End = line.End
				if j+1 < len(line.Words) && line.Words[j+1].Start >= word.Start {
					word.End = line.Words[j+1].Start
				}
			}
			if word.End == word.Start && !line.PreserveTiming {
				if word.Start < line.Start || word.Start > line.End {
					return result, Error(line.SourceLine, 1, "time", "zero-duration word lies outside line interval")
				}
				if err := p.Warn(line.SourceLine, 1, "zero-word", "zero-duration word timing removed; line text retained"); err != nil {
					return result, err
				}
				continue
			}
			word.Text, word.Romanization = strings.Clone(word.Text), strings.Clone(word.Romanization)
			for k := range word.Ruby {
				word.Ruby[k].Text = strings.Clone(word.Ruby[k].Text)
			}
			line.Words[n] = word
			n++
		}
		clear(line.Words[n:])
		line.Words = line.Words[:n]
		line.ID, line.Text = strings.Clone(line.ID), strings.Clone(line.Text)
		line.ParentID = strings.Clone(line.ParentID)
		line.Translation, line.Romanization = strings.Clone(line.Translation), strings.Clone(line.Romanization)
		line.Agent, line.Language = strings.Clone(line.Agent), strings.Clone(line.Language)
		if !line.PreserveTiming && line.End <= line.Start {
			return result, Error(line.SourceLine, 1, "time", "require line start < end")
		}
		for j, word := range line.Words {
			if word.Start < 0 || word.End < 0 || !line.PreserveTiming && (word.Start < line.Start || word.End <= word.Start || word.End > line.End) {
				return result, Error(line.SourceLine, 1, "time", fmt.Sprintf("word %d requires line.start <= start < end <= line.end", j))
			}
			if !line.PreserveTiming && j > 0 && word.Start < line.Words[j-1].Start {
				return result, Error(line.SourceLine, 1, "time", fmt.Sprintf("word %d precedes previous word", j))
			}
		}
		output[i] = line.Line
	}
	for i := range metadata {
		if i&255 == 0 {
			if err := p.Check(); err != nil {
				return result, err
			}
		}
		metadata[i].Name, metadata[i].Value = strings.Clone(metadata[i].Name), strings.Clone(metadata[i].Value)
	}
	if err := p.Check(); err != nil {
		return result, err
	}
	var documentOptions lyric.DocumentOptions
	for _, line := range lines {
		documentOptions.PreserveSourceTiming = documentOptions.PreserveSourceTiming || line.PreserveTiming
	}
	result.Document, err = lyric.NewDocumentWithOptions(output, metadata, documentOptions)
	if err != nil {
		return result, Error(1, 1, "document", err.Error())
	}
	return result, nil
}
