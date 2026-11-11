package lyric

import (
	"fmt"
	"time"
)

// Word is a timed text segment. Times are absolute positions in the track,
// rather than offsets within the containing line.
type Word struct {
	Text         string
	Romanization string
	Start        time.Duration
	End          time.Duration
	// Ruby holds timed pronunciation segments above this word. NewDocument
	// snapshots the slice, and LineView.Word returns an independent copy.
	Ruby []RubyWord
	// Obscene and EmptyBeat retain explicit source annotations. EmptyBeat is
	// zero when absent; presentation and filtering remain renderer concerns.
	Obscene   bool
	EmptyBeat int
}

// RubyWord is one pronunciation segment, using absolute track time. The default
// constructors require its interval to lie inside the parent word, with segments
// ordered by Start; point annotations and overlaps are allowed. Explicit source
// timing preservation retains nonnegative source endpoints without those checks.
type RubyWord struct {
	Text       string
	Start, End time.Duration
}

// Line is input to NewDocument. Equal start times and overlapping intervals
// are supported, including duet and background vocals.
type Line struct {
	// ID identifies a line within this document, independently of playback and
	// sorting. Empty IDs receive deterministic document-local IDs at construction.
	ID           string
	Text         string
	Translation  string
	Romanization string
	Start        time.Duration
	End          time.Duration
	Words        []Word
	Duet         bool
	Background   bool
	Agent        string
	Language     string
	// ParentID associates a background line with its main line even if sorting
	// puts the background first. Empty retains the host's ungrouped input order.
	ParentID string
}

// Property is a source metadata entry. Names are format-specific; duplicate
// names and their order are preserved. Values are immutable strings.
type Property struct {
	Name  string
	Value string
}

// Document owns copies of its input slices. Text strings remain shared and
// immutable. A constructed document can be shared between goroutines.
type Document struct {
	lines      []Line
	properties []Property
}

// DocumentOptions controls explicit compatibility with imperfect source timing.
// The zero value keeps the strict timing rules of NewDocument.
type DocumentOptions struct {
	// PreserveSourceTiming retains the textual order and nonnegative endpoints
	// of words and Ruby segments, including points, reversed intervals, overlaps,
	// decreasing starts and intervals outside their parent. Lines retain their
	// nonnegative endpoints and must remain ordered by Start. Point and reversed
	// lines are retained as data, but are never active half-open intervals. Rendering and correction
	// of imperfect word timing remain the caller's responsibility.
	PreserveSourceTiming bool
}

// NewDocument validates and snapshots lines. Lines and words must be ordered
// by start time; simultaneous starts are allowed. End times must be explicit,
// greater than start times, and words must lie within their containing line.
// An empty document is valid. Invalid input is never silently sorted or fixed.
func NewDocument(lines []Line) (*Document, error) {
	return NewDocumentWithMetadata(lines, nil)
}

// NewDocumentWithMetadata snapshots lines and metadata under the same timing
// rules as NewDocument. Explicit IDs must be unique. Generated IDs are stable
// for this input order, not a promise of identity across independently edited
// documents. Callers needing that promise should supply their own IDs.
func NewDocumentWithMetadata(lines []Line, metadata []Property) (*Document, error) {
	return NewDocumentWithOptions(lines, metadata, DocumentOptions{})
}

// NewDocumentWithOptions snapshots lines and metadata with an explicit timing
// policy. Options do not change identity validation, ownership or concurrency:
// input slices are deeply copied and the result is immutable and shareable.
// With PreserveSourceTiming, word and Ruby endpoints are retained exactly; no
// sorting, swapping, clipping or duration inference occurs in this constructor.
func NewDocumentWithOptions(lines []Line, metadata []Property, options DocumentOptions) (*Document, error) {
	ids := make(map[string]struct{}, len(lines))
	hasParents := false
	for i, line := range lines {
		hasParents = hasParents || line.ParentID != ""
		if line.ID != "" {
			if _, exists := ids[line.ID]; exists {
				return nil, fmt.Errorf("lyric: line %d: duplicate ID %q", i, line.ID)
			}
			ids[line.ID] = struct{}{}
		}
		if line.Start < 0 || line.End < 0 || !options.PreserveSourceTiming && line.End <= line.Start {
			return nil, fmt.Errorf("lyric: line %d: require a valid nonnegative line interval", i)
		}
		if i > 0 && line.Start < lines[i-1].Start {
			return nil, fmt.Errorf("lyric: line %d: start precedes previous line", i)
		}
		for j, word := range line.Words {
			if word.Start < 0 || word.End < 0 {
				return nil, fmt.Errorf("lyric: line %d word %d: require nonnegative endpoints", i, j)
			}
			if !options.PreserveSourceTiming {
				if word.Start < line.Start || word.End <= word.Start || word.End > line.End {
					return nil, fmt.Errorf("lyric: line %d word %d: require line.start <= start < end <= line.end", i, j)
				}
				if j > 0 && word.Start < line.Words[j-1].Start {
					return nil, fmt.Errorf("lyric: line %d word %d: start precedes previous word", i, j)
				}
			}
			for k, ruby := range word.Ruby {
				if ruby.Start < 0 || ruby.End < 0 {
					return nil, fmt.Errorf("lyric: line %d word %d ruby %d: require nonnegative endpoints", i, j, k)
				}
				if !options.PreserveSourceTiming && (ruby.Start < word.Start || ruby.End < ruby.Start || ruby.End > word.End) {
					return nil, fmt.Errorf("lyric: line %d word %d ruby %d: require word.start <= start <= end <= word.end", i, j, k)
				}
				if !options.PreserveSourceTiming && k > 0 && ruby.Start < word.Ruby[k-1].Start {
					return nil, fmt.Errorf("lyric: line %d word %d ruby %d: start precedes previous ruby", i, j, k)
				}
			}
		}
	}

	doc := &Document{lines: make([]Line, len(lines)), properties: append([]Property(nil), metadata...)}
	for i, line := range lines {
		doc.lines[i] = line
		if line.ID == "" {
			id := fmt.Sprintf("line:%d", i)
			for suffix := 1; ; suffix++ {
				if _, exists := ids[id]; !exists {
					break
				}
				id = fmt.Sprintf("line:%d:%d", i, suffix)
			}
			doc.lines[i].ID = id
			ids[id] = struct{}{}
		}
		doc.lines[i].Words = append([]Word(nil), line.Words...)
		for j := range doc.lines[i].Words {
			doc.lines[i].Words[j].Ruby = append([]RubyWord(nil), line.Words[j].Ruby...)
		}
	}
	if hasParents {
		indices := make(map[string]int, len(doc.lines))
		for i, line := range doc.lines {
			indices[line.ID] = i
		}
		for i, line := range doc.lines {
			if line.ParentID == "" {
				continue
			}
			parent, ok := indices[line.ParentID]
			if !line.Background || !ok || doc.lines[parent].Background {
				return nil, fmt.Errorf("lyric: line %d: ParentID must name a main line for a background line", i)
			}
		}
	}
	return doc, nil
}

// Len returns the number of lines.
func (d *Document) Len() int { return len(d.lines) }

// Line returns a read-only view. Like slice indexing, an invalid index panics.
func (d *Document) Line(index int) LineView { return LineView{line: &d.lines[index]} }

// PropertyCount returns the number of source metadata entries.
func (d *Document) PropertyCount() int { return len(d.properties) }

// Property returns a value copy. Like slice indexing, an invalid index panics.
func (d *Document) Property(index int) Property { return d.properties[index] }

// LineView provides scalar access without exposing mutable document slices.
// Word copies optional Ruby storage; ordinary words need no allocation.
// It retains the backing document; caches should discard views on song changes.
type LineView struct {
	line *Line
}

// ID returns the stable document-local line identity.
func (v LineView) ID() string { return v.line.ID }

// ParentID returns the explicit main-line identity for background vocals.
func (v LineView) ParentID() string { return v.line.ParentID }

// Agent returns the source vocal agent identifier, or an empty string.
func (v LineView) Agent() string { return v.line.Agent }

// Language returns the source language tag, or an empty string.
func (v LineView) Language() string { return v.line.Language }

// Text returns the original line text.
func (v LineView) Text() string { return v.line.Text }

// Translation returns the line translation, or an empty string.
func (v LineView) Translation() string { return v.line.Translation }

// Romanization returns the line romanization, or an empty string.
func (v LineView) Romanization() string { return v.line.Romanization }

// Start returns the inclusive start position in the track.
func (v LineView) Start() time.Duration { return v.line.Start }

// End returns the exclusive end position in the track.
func (v LineView) End() time.Duration { return v.line.End }

// Duet reports whether the line is assigned to the alternate duet side.
func (v LineView) Duet() bool { return v.line.Duet }

// Background reports whether the line represents background vocals.
func (v LineView) Background() bool { return v.line.Background }

// WordCount returns the number of timed segments in the line.
func (v LineView) WordCount() int { return len(v.line.Words) }

// RubyCount returns a word's annotation count without copying its storage. This
// permits callers to enforce preparation budgets before requesting a Word copy.
func (v LineView) RubyCount(wordIndex int) int { return len(v.line.Words[wordIndex].Ruby) }

// Ruby returns one pronunciation segment as a value, without allocating or
// exposing the document's backing slice. Invalid indices panic like slices.
func (v LineView) Ruby(wordIndex, index int) RubyWord { return v.line.Words[wordIndex].Ruby[index] }

// Word returns a copy whose optional Ruby slice belongs to the caller. Like
// slice indexing, an invalid index panics. Words without Ruby do not allocate.
func (v LineView) Word(index int) Word {
	word := v.line.Words[index]
	word.Ruby = append([]RubyWord(nil), word.Ruby...)
	return word
}
