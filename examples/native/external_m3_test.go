package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/format"
)

type externalM3Stats struct {
	Lines           int            `json:"lines"`
	Words           int            `json:"timed_words"`
	Translations    int            `json:"translated_lines"`
	Romanizations   int            `json:"romanized_lines"`
	Duets           int            `json:"duet_lines"`
	Backgrounds     int            `json:"background_lines"`
	Overlaps        int            `json:"lines_overlapping_prior_intervals"`
	DiagnosticCodes map[string]int `json:"diagnostic_codes"`
	Preparations    int            `json:"viewport_preparations"`
	PeakEntries     int            `json:"peak_cached_entries"`
	PeakBytes       int            `json:"peak_cached_estimated_bytes"`
	PeakPinned      int            `json:"peak_pinned_entries"`
	PeakPinnedBytes int            `json:"peak_pinned_estimated_bytes"`
	OS              string         `json:"os"`
	Architecture    string         `json:"architecture"`
	Go              string         `json:"go"`
	Renderer        string         `json:"renderer"`
	ReproducibleCI  bool           `json:"repository_fixture_reproducible_in_ci"`
}

// TestM3ExternalTTML is an opt-in local integration check. No private input
// path or lyric text belongs in this repository. Run with MYGO_AMLL_TEST_TTML
// pointing at an authorized local file. MYGO_AMLL_SNAPSHOT_DIR optionally keeps
// CPU review PNGs and a statistics-only JSON report outside the checkout.
// Passing this test is evidence for that local file and installed fonts, not a
// reproducible repository fixture, a reference golden, or native GPU behavior.
func TestM3ExternalTTML(t *testing.T) {
	path := os.Getenv("MYGO_AMLL_TEST_TTML")
	if path == "" {
		t.Skip("set MYGO_AMLL_TEST_TTML to an authorized local TTML file")
	}
	result, err := loadTTML(context.Background(), path)
	if err != nil {
		var parseErr *format.ParseError
		if errors.As(err, &parseErr) {
			t.Fatalf("external TTML parse failed at source line %d column %d, code %q (input content omitted)", parseErr.Line, parseErr.Column, parseErr.Code)
		}
		t.Fatalf("external TTML load failed (%T; path and input content omitted)", err)
	}
	doc := result.Document
	if doc == nil || doc.Len() == 0 {
		t.Fatal("external TTML did not produce a nonempty document")
	}
	stats := externalM3Stats{
		Lines: doc.Len(), DiagnosticCodes: make(map[string]int),
		OS: runtime.GOOS, Architecture: runtime.GOARCH, Go: runtime.Version(), Renderer: "MyGo CPU Tester; installed platform fonts",
	}
	for _, diagnostic := range result.Diagnostics {
		stats.DiagnosticCodes[diagnostic.Code]++
	}
	firstDuet, firstBackground, firstOverlap := -1, -1, -1
	var maximumEnd time.Duration
	for i := range doc.Len() {
		line := doc.Line(i)
		stats.Words += line.WordCount()
		if line.Translation() != "" {
			stats.Translations++
		}
		if line.Romanization() != "" {
			stats.Romanizations++
		}
		if line.Duet() {
			stats.Duets++
			if firstDuet < 0 {
				firstDuet = i
			}
		}
		if line.Background() {
			stats.Backgrounds++
			if firstBackground < 0 {
				firstBackground = i
			}
		}
		if line.Start() < maximumEnd {
			stats.Overlaps++
			if firstOverlap < 0 {
				firstOverlap = i
			}
		}
		maximumEnd = max(maximumEnd, line.End())
	}
	t.Logf("external document: lines=%d words=%d translated=%d romanized=%d duet=%d background=%d overlap=%d diagnostics=%v",
		stats.Lines, stats.Words, stats.Translations, stats.Romanizations, stats.Duets, stats.Backgrounds, stats.Overlaps, stats.DiagnosticCodes)
	// Independent width/DPI combinations cover all requested dimensions without
	// repeatedly measuring the same logical geometry in the full cross product.
	for _, config := range []struct {
		width int
		scale float32
	}{{320, 0.75}, {720, 1}, {1100, 2}} {
		if !t.Run(fmt.Sprintf("width-%d-scale-%g", config.width, config.scale), func(t *testing.T) {
			now := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC)
			s := newScene(doc, now)
			defer func() { _ = s.Close() }()
			s.forceStatic, s.scale = true, config.scale
			box := ui.Rect{W: float32(config.width), H: 740}
			for _, reverse := range []bool{false, true} {
				for step := range doc.Len() {
					index := step
					if reverse {
						index = doc.Len() - 1 - step
					}
					line := doc.Line(index)
					// Exercise actual host seek reconstruction, then browse each
					// requested line explicitly; overlapping focus must not cause
					// another line to hide an untested mapping error.
					s.seek(now, line.Start())
					s.manualLine = index
					s.prepare(line.Start(), box)
					stats.Preparations++
					var anchor *lineGeometry
					for _, row := range s.rows[:s.rowCount] {
						if row.index == index {
							anchor = row.geometry
							break
						}
					}
					if anchor == nil {
						width := float32(config.width - 48)
						_, geometryErr := buildLineGeometry(line, s.font, width)
						if errors.Is(geometryErr, errGeometryLimit) {
							t.Fatalf("line %d ID %q exceeded the geometry budget", index, line.ID())
						}
						if geometryErr != nil {
							t.Fatalf("line %d ID %q failed timed text mapping or shaping (input content omitted)", index, line.ID())
						}
						t.Fatalf("line %d ID %q was not retained as a prepared anchor", index, line.ID())
					}
					wantAnnotations := 0
					if line.Translation() != "" {
						wantAnnotations++
					}
					if line.Romanization() != "" {
						wantAnnotations++
					}
					if len(anchor.annotations) != wantAnnotations {
						t.Fatalf("line %d ID %q lost annotations: got %d want %d", index, line.ID(), len(anchor.annotations), wantAnnotations)
					}
					checkExternalM3Frame(t, s, index, &stats)
				}
			}
		}) {
			return
		}
	}
	dir := os.Getenv("MYGO_AMLL_SNAPSHOT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if dir, err = filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seen := make(map[int]bool)
	for _, selection := range []struct {
		name  string
		index int
	}{{"duet", firstDuet}, {"background", firstBackground}, {"overlap", firstOverlap}, {"last-line", doc.Len() - 1}} {
		if selection.index < 0 || seen[selection.index] {
			continue
		}
		seen[selection.index] = true
		line := doc.Line(selection.index)
		now := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC)
		s := newScene(doc, now)
		s.forceStatic, s.showBackground, s.manualLine = true, true, selection.index
		s.clock.seek(now, line.Start()+(line.End()-line.Start())/2)
		tt := ui.NewTesterWith(func(c *ui.Context) { ui.Box(c).Fill().Draw(s.draw) }, 900, 740,
			ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
		checkExternalM3Frame(t, s, selection.index, &stats)
		writeSnapshot(t, filepath.Join(dir, runtime.GOOS+"-"+runtime.GOARCH+"-external-"+selection.name+".png"), tt.Image())
		_ = s.Close()
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "external-test-report.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("external checks: preparations=%d peak-cache=%d entries/%d B peak-pinned=%d entries/%d B; CPU artifacts=%s; external fixture is not reproducible CI evidence",
		stats.Preparations, stats.PeakEntries, stats.PeakBytes, stats.PeakPinned, stats.PeakPinnedBytes, dir)
}

func checkExternalM3Frame(t *testing.T, s *scene, anchor int, stats *externalM3Stats) {
	t.Helper()
	if s.rowCount > maxPreparedLines || len(s.cache.entries) > geometryEntryLimit || s.cache.bytes > geometryByteLimit || s.cache.bytes < 0 {
		t.Fatalf("line %d ID %q exceeded frame/cache budgets", anchor, s.document.Line(anchor).ID())
	}
	retained := make(map[*lineGeometry]bool)
	bytes, pinned, pinnedBytes := 0, 0, 0
	for _, entry := range s.cache.entries {
		if entry.geometry == nil {
			t.Fatalf("line %d has a nil cached layout", anchor)
		}
		bytes += entry.geometry.bytes
		retained[entry.geometry] = entry.pinned
		if entry.pinned {
			pinned++
			pinnedBytes += entry.geometry.bytes
		}
	}
	if bytes != s.cache.bytes || pinned > maxPreparedLines || pinnedBytes > geometryByteLimit {
		t.Fatalf("line %d ID %q has inconsistent cache/pin accounting", anchor, s.document.Line(anchor).ID())
	}
	rows := make(map[int]preparedLine)
	for _, row := range s.rows[:s.rowCount] {
		if row.geometry == nil {
			t.Fatalf("line %d ID %q fell back inside a prepared viewport", row.index, s.document.Line(row.index).ID())
		}
		if !retained[row.geometry] {
			t.Fatalf("line %d ID %q escaped pinned cache ownership", row.index, s.document.Line(row.index).ID())
		}
		if !finitePositive(row.geometry.height) {
			t.Fatalf("line %d ID %q has invalid height", row.index, s.document.Line(row.index).ID())
		}
		rows[row.index] = row
	}
	for index, row := range rows {
		if next, ok := rows[index+1]; ok && row.y+row.geometry.height > next.y+0.01 {
			t.Fatalf("line %d ID %q overlaps the next prepared line", index, s.document.Line(index).ID())
		}
	}
	stats.PeakEntries = max(stats.PeakEntries, len(s.cache.entries))
	stats.PeakBytes = max(stats.PeakBytes, bytes)
	stats.PeakPinned = max(stats.PeakPinned, pinned)
	stats.PeakPinnedBytes = max(stats.PeakPinnedBytes, pinnedBytes)
}
