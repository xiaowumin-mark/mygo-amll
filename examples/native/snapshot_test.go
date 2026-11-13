package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	snapshotRegularFamily = "AMLL M3 Snapshot Go Regular"
	snapshotBoldFamily    = "AMLL M3 Snapshot Go Bold"
)

var snapshotFonts struct {
	sync.Once
	err error
}

func registerSnapshotFonts(t *testing.T) {
	t.Helper()
	snapshotFonts.Do(func() {
		if snapshotFonts.err = ui.RegisterFont(goregular.TTF, snapshotRegularFamily); snapshotFonts.err != nil {
			return
		}
		snapshotFonts.err = ui.RegisterFont(gobold.TTF, snapshotBoldFamily)
	})
	if snapshotFonts.err != nil {
		t.Fatal(snapshotFonts.err)
	}
}

// This synthetic fixture has no song, artwork, or external font assets. Go's
// bundled fonts fix Latin coverage only: CJK, Arabic, Hebrew, and emoji still
// exercise the platform's fallback fonts and must not become cross-OS goldens.
func snapshotDocument(t *testing.T) *lyric.Document {
	t.Helper()
	words := []string{"A quiet ", "morning — ", "café e\u0301, ", "office ffi, ", "👩‍💻"}
	timed := make([]lyric.Word, len(words))
	for i, text := range words {
		timed[i] = lyric.Word{Text: text, Start: time.Duration(i) * time.Second, End: time.Duration(i+1) * time.Second}
	}
	document, err := lyric.NewDocument([]lyric.Line{
		{ID: "latin", Text: strings.Join(words, ""), Start: 0, End: 6 * time.Second, Words: timed,
			Translation: "A synthetic translation, with punctuation.", Romanization: "A quiet morning", Language: "en"},
		{ID: "wrapped", Text: strings.Repeat("Bright synthetic words wrap while the whole shaped line keeps its timing. ", 5),
			Start: 3 * time.Second, End: 9 * time.Second, Translation: "Long lines retain a separate translated row.", Language: "en"},
		{ID: "duet", Text: "An answer from the other side keeps its words together.", Start: 5 * time.Second, End: 10 * time.Second,
			Translation: "The second voice stays right aligned.", Romanization: "Answer", Duet: true, Agent: "v2", Language: "en"},
		{ID: "mixed", Text: "中文 日本語 Latin 👨‍👩‍👧‍👦 e\u0301", Start: 8 * time.Second, End: 14 * time.Second,
			Translation: "Mixed scripts use the system fallback.", Romanization: "zhong wen / nihongo", Language: "ja"},
		{ID: "rtl", Text: "עברית العربية English 123 שלום", Start: 11 * time.Second, End: 17 * time.Second,
			Translation: "Bidirectional text keeps its visual order.", Romanization: "shalom / al-arabiyya", Language: "ar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

type snapshotCase struct {
	Name     string        `json:"name"`
	Width    int           `json:"width_dip"`
	Height   int           `json:"height_dip"`
	Scale    float32       `json:"pixels_per_dip"`
	Position time.Duration `json:"position_ns"`
	Anchor   int           `json:"browse_anchor_line"`
	Bold     bool          `json:"bold_font"`
}

func snapshotScene(t *testing.T, document *lyric.Document, c snapshotCase) (*scene, *ui.Tester) {
	t.Helper()
	now := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC)
	s := newScene(document, now)
	s.font = ui.Font{Family: snapshotRegularFamily, Size: 30, Weight: 400, LineHeight: 1.35, Lang: "en"}
	if c.Bold {
		s.font.Family, s.font.Weight = snapshotBoldFamily, 700
	}
	s.forceStatic, s.showBackground = true, true
	s.manualLine = c.Anchor
	s.clock.seek(now, c.Position)
	t.Cleanup(func() { _ = s.Close() })
	tt := ui.NewTesterWith(func(ctx *ui.Context) {
		ui.Box(ctx).Fill().Draw(s.draw)
	}, c.Width, c.Height, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	if c.Scale != 1 {
		tt.SetScale(c.Scale)
	}
	return s, tt
}

func cloneSnapshot(source *image.RGBA) *image.RGBA {
	result := image.NewRGBA(source.Bounds())
	for y := source.Rect.Min.Y; y < source.Rect.Max.Y; y++ {
		start := source.PixOffset(source.Rect.Min.X, y)
		out := result.PixOffset(result.Rect.Min.X, y)
		copy(result.Pix[out:out+result.Rect.Dx()*4], source.Pix[start:start+source.Rect.Dx()*4])
	}
	return result
}

func sameSnapshot(a, b *image.RGBA) bool {
	if a == nil || b == nil || a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		aStart, bStart := a.PixOffset(a.Rect.Min.X, y), b.PixOffset(b.Rect.Min.X, y)
		if !bytes.Equal(a.Pix[aStart:aStart+a.Rect.Dx()*4], b.Pix[bStart:bStart+b.Rect.Dx()*4]) {
			return false
		}
	}
	return true
}

func writeSnapshot(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, img)
	closeErr := file.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	t.Logf("CPU snapshot: %s", path)
}

func validateSnapshotLayout(t *testing.T, s *scene) map[int]float32 {
	t.Helper()
	heights := make(map[int]float32)
	rows := make(map[int]preparedLine)
	for _, row := range s.rows[:s.rowCount] {
		if row.geometry == nil {
			t.Fatalf("fixture row %d fell back instead of shaping", row.index)
		}
		g := row.geometry
		if !finitePositive(g.height) || math.IsNaN(float64(row.y)) || math.IsInf(float64(row.y), 0) {
			t.Fatalf("row %d has invalid geometry", row.index)
		}
		// ShapeText keeps spaces, so a wrapped line's trailing whitespace can
		// hang beyond its width (the public TextShape contract). Check every
		// non-whitespace grapheme's actual advance rectangle instead of using
		// Size(), which includes those invisible trailing advances.
		units := 0
		for _, r := range s.document.Line(row.index).Text() {
			end := units + utf16.RuneLen(r)
			if !unicode.IsSpace(r) {
				for _, rect := range g.layout.SelectionRects(ui.TextInputRange{Start: units, End: end}) {
					if rect.X < -0.01 || rect.X+rect.W > s.viewport.W-48+0.01 {
						t.Errorf("row %d non-whitespace %q exceeds available width: %v within %g", row.index, r, rect, s.viewport.W-48)
					}
				}
			}
			units = end
		}
		rows[row.index] = row
		heights[row.index] = g.height
	}
	for index, row := range rows {
		if next, ok := rows[index+1]; ok && row.y+row.geometry.height > next.y+0.01 {
			t.Errorf("rows %d and %d overlap", index, index+1)
		}
	}
	return heights
}

// TestM3Snapshots compares repeated CPU renders at exactly the same paused song
// position with zero channel tolerance. It deliberately creates local artifacts
// instead of checking in platform-dependent PNGs. To retain the images, set
// MYGO_AMLL_SNAPSHOT_DIR to a temporary directory and run this test with -v.
// Same-state equality detects nondeterminism; it does not certify visual fidelity
// against a reference image, native GPU rendering, or system fallback coverage.
func TestM3Snapshots(t *testing.T) {
	registerSnapshotFonts(t)
	dir := os.Getenv("MYGO_AMLL_SNAPSHOT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	var err error
	if dir, err = filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Log("CPU Tester only; fixed GoRegular/GoBold Latin fonts; CJK/RTL/emoji use system fallback; repeated-render pixel tolerance=0; no cross-platform golden comparison")
	document := snapshotDocument(t)
	cases := []snapshotCase{
		{Name: "overview", Width: 960, Height: 740, Scale: 1, Position: 9500 * time.Millisecond, Anchor: 2},
		{Name: "narrow", Width: 400, Height: 740, Scale: 1, Position: 9500 * time.Millisecond, Anchor: 2},
		{Name: "high-dpi", Width: 960, Height: 740, Scale: 2, Position: 9500 * time.Millisecond, Anchor: 2},
		{Name: "rtl", Width: 800, Height: 640, Scale: 1, Position: 13 * time.Second, Anchor: 3},
		{Name: "latin-regular", Width: 960, Height: 740, Scale: 1, Position: 1500 * time.Millisecond},
		{Name: "latin-bold", Width: 960, Height: 740, Scale: 1, Position: 1500 * time.Millisecond, Bold: true},
	}
	images := make(map[string]*image.RGBA)
	heights := make(map[string]map[int]float32)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			s, tt := snapshotScene(t, document, c)
			first := cloneSnapshot(tt.Image())
			if first.Bounds().Dx() != int(float32(c.Width)*c.Scale) || first.Bounds().Dy() != int(float32(c.Height)*c.Scale) {
				t.Fatalf("unexpected physical image dimensions: %v", first.Bounds())
			}
			heights[c.Name] = validateSnapshotLayout(t, s)
			path := filepath.Join(dir, runtime.GOOS+"-"+runtime.GOARCH+"-"+c.Name+".png")
			writeSnapshot(t, path, first)
			shapes := s.cache.shapes
			tt.Frame()
			if !sameSnapshot(first, tt.Image()) {
				writeSnapshot(t, strings.TrimSuffix(path, ".png")+"-repeat.png", tt.Image())
				t.Fatal("repeated rendering changed pixels at the same paused position (tolerance 0)")
			}
			if s.cache.shapes != shapes || tt.FramePending() {
				t.Fatal("static repeated rendering reshaped text or kept requesting frames")
			}
			_, independent := snapshotScene(t, document, c)
			if !sameSnapshot(first, independent.Image()) {
				t.Fatal("independent rendering changed pixels for identical scene inputs (tolerance 0)")
			}
			images[c.Name] = first
		})
	}
	if wide, narrow := heights["overview"][1], heights["narrow"][1]; wide == 0 || narrow <= wide {
		t.Errorf("narrow long line should have more wrapped height: wide=%g narrow=%g", wide, narrow)
	}
	if sameSnapshot(images["latin-regular"], images["latin-bold"]) {
		t.Error("replacing the registered font produced identical pixels")
	}
	manifest := struct {
		OS              string         `json:"os"`
		Architecture    string         `json:"architecture"`
		Go              string         `json:"go"`
		Renderer        string         `json:"renderer"`
		RegularFont     string         `json:"regular_font"`
		BoldFont        string         `json:"bold_font"`
		PixelTolerance  int            `json:"same_state_pixel_tolerance"`
		Fallback        string         `json:"fallback_limitations"`
		ReferenceGolden bool           `json:"compared_with_reference_golden"`
		Cases           []snapshotCase `json:"cases"`
	}{runtime.GOOS, runtime.GOARCH, runtime.Version(), "MyGo CPU Tester", snapshotRegularFamily, snapshotBoldFamily, 0,
		"CJK, Arabic, Hebrew, and emoji depend on installed platform fonts; these PNGs are local review artifacts, not cross-platform goldens or GPU evidence.", false, cases}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot-report.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestM3SnapshotResizeRoundTrip(t *testing.T) {
	registerSnapshotFonts(t)
	c := snapshotCase{Width: 960, Height: 740, Scale: 1, Position: 9500 * time.Millisecond, Anchor: 2}
	s, tt := snapshotScene(t, snapshotDocument(t), c)
	before := cloneSnapshot(tt.Image())
	wide := validateSnapshotLayout(t, s)[1]
	for _, width := range []int{400, 920, 420, 880, 440, 960} {
		tt.SetSize(width, c.Height)
		validateSnapshotLayout(t, s)
		if width == 400 && s.heights.Height(1) <= float64(wide) {
			t.Error("resizing to narrow width did not increase long-line height")
		}
	}
	if !sameSnapshot(before, tt.Image()) {
		t.Fatal("returning to the original width changed paused pixels (tolerance 0)")
	}
}
