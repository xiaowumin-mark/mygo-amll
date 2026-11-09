// Package format defines shared parser options and diagnostics. Parsers accept
// in-memory UTF-8 input, perform no I/O, and produce immutable lyric documents.
// Each call owns its result and workspace; calls may run concurrently.
package format

import (
	"fmt"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Limits bounds both source and expanded output. Zero fields select defaults;
// negative fields are errors. MaxLines bounds physical source lines and output
// lines separately. MaxWords bounds all output timed segments, even dropped ones.
type Limits struct {
	MaxBytes       int
	MaxTextBytes   int
	MaxLines       int
	MaxWords       int
	MaxDepth       int
	MaxDiagnostics int
}

// DefaultLimits returns independent default budgets: 8 MiB input, 16 MiB of
// expanded output strings, 100,000 lines, 1,000,000 words, XML depth 32, and
// 100 diagnostics. Text counts line and word text separately, including source
// IDs, annotations, agents, languages and metadata; generated IDs are bounded
// separately by MaxLines. Zero-duration words count before removal.
func DefaultLimits() Limits {
	return Limits{MaxBytes: 8 << 20, MaxTextBytes: 16 << 20, MaxLines: 100000, MaxWords: 1000000, MaxDepth: 32, MaxDiagnostics: 100}
}

// Options applies to one parse. Strict promotes every warning to an error.
// Duration is an optional absolute track end used only for inferred final ends
// and is not shifted by a source offset. FinalLineDuration defaults to five
// seconds when Duration is unknown. Negative durations are errors.
type Options struct {
	Strict            bool
	Limits            Limits
	Duration          time.Duration
	FinalLineDuration time.Duration
}

// Diagnostic locates a warning or error. Line and Column are one-based source
// coordinates; columns count UTF-8 bytes, not runes or UTF-16 code units.
type Diagnostic struct {
	Code    string
	Message string
	Line    int
	Column  int
}

// ParseError is a fatal source or configuration diagnostic. Context errors are
// returned directly so errors.Is works with context.Canceled/DeadlineExceeded.
type ParseError struct{ Diagnostic }

func (e *ParseError) Error() string {
	return fmt.Sprintf("format: %s at %d:%d: %s", e.Code, e.Line, e.Column, e.Message)
}

// Result owns its diagnostics. Document is nil on failure; diagnostics emitted
// before a fatal error may still be returned. No result aliases the input bytes.
type Result struct {
	Document    *lyric.Document
	Diagnostics []Diagnostic
}
