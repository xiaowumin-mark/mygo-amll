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
}

// Line is input to NewDocument. Equal start times and overlapping intervals
// are supported, including duet and background vocals.
type Line struct {
	Text         string
	Translation  string
	Romanization string
	Start        time.Duration
	End          time.Duration
	Words        []Word
	Duet         bool
	Background   bool
}

// Document owns copies of its input slices. Text strings remain shared and
// immutable. A constructed document can be shared between goroutines.
type Document struct {
	lines []Line
}

// NewDocument validates and snapshots lines. Lines and words must be ordered
// by start time; simultaneous starts are allowed. End times must be explicit,
// greater than start times, and words must lie within their containing line.
// An empty document is valid. Invalid input is never silently sorted or fixed.
func NewDocument(lines []Line) (*Document, error) {
	for i, line := range lines {
		if line.Start < 0 || line.End <= line.Start {
			return nil, fmt.Errorf("lyric: line %d: require 0 <= start < end", i)
		}
		if i > 0 && line.Start < lines[i-1].Start {
			return nil, fmt.Errorf("lyric: line %d: start precedes previous line", i)
		}
		for j, word := range line.Words {
			if word.Start < line.Start || word.End <= word.Start || word.End > line.End {
				return nil, fmt.Errorf("lyric: line %d word %d: require line.start <= start < end <= line.end", i, j)
			}
			if j > 0 && word.Start < line.Words[j-1].Start {
				return nil, fmt.Errorf("lyric: line %d word %d: start precedes previous word", i, j)
			}
		}
	}

	doc := &Document{lines: make([]Line, len(lines))}
	for i, line := range lines {
		doc.lines[i] = line
		doc.lines[i].Words = append([]Word(nil), line.Words...)
	}
	return doc, nil
}

// Len returns the number of lines.
func (d *Document) Len() int { return len(d.lines) }

// Line returns a read-only view. Like slice indexing, an invalid index panics.
func (d *Document) Line(index int) LineView { return LineView{line: &d.lines[index]} }

// LineView provides allocation-free access without exposing mutable slices.
// It retains the backing document; caches should discard views on song changes.
type LineView struct {
	line *Line
}

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

// Word returns a value copy. Like slice indexing, an invalid index panics.
func (v LineView) Word(index int) Word { return v.line.Words[index] }
