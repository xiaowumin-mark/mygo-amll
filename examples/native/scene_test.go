package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func testScene(t testing.TB, count int) *scene {
	t.Helper()
	doc, err := demoDocument(count)
	if err != nil {
		t.Fatal(err)
	}
	s := newScene(doc, time.Unix(0, 0))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSceneCullingAndWarmPreparation(t *testing.T) {
	for _, count := range []int{200, 2000, 10000} {
		s := testScene(t, count)
		box := ui.Rect{W: 900, H: 600}
		s.prepare(0, box)
		if s.rowCount == 0 || s.rowCount > maxPreparedLines || len(s.cache.entries) > maxPreparedLines {
			t.Fatalf("%d lines: prepared %d rows, %d layouts", count, s.rowCount, len(s.cache.entries))
		}
		shapes := s.cache.shapes
		allocs := testing.AllocsPerRun(100, func() { s.prepare(100*time.Millisecond, box) })
		if s.cache.shapes != shapes || allocs != 0 {
			t.Fatalf("warm preparation reshaped or allocated: shapes %d -> %d, allocs %g", shapes, s.cache.shapes, allocs)
		}
		s.prepare(s.duration-time.Second, box)
		if s.rows[0].index < count-2 || s.rowCount > maxPreparedLines {
			t.Fatalf("seek did not reconstruct the visible window: focus %d, count %d", s.rows[0].index, s.rowCount)
		}
		for _, row := range s.rows[:s.rowCount] {
			if row.geometry == nil {
				continue
			}
			found := false
			for _, entry := range s.cache.entries {
				found = found || entry.geometry == row.geometry
			}
			if !found {
				t.Fatal("prepared rows retain a layout outside the cache budget")
			}
		}
	}
}

func TestSceneRepaintPlan(t *testing.T) {
	doc, err := lyric.NewDocument([]lyric.Line{
		{Text: "one", Start: 0, End: time.Second},
		{Text: "two", Start: 20 * time.Second, End: 21 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(0, 0)
	s := newScene(doc, now)
	defer s.Close()
	box := ui.Rect{W: 800, H: 500}
	s.prepare(500*time.Millisecond, box)
	if plan := s.repaintAt(500*time.Millisecond, true, box); plan != (repaintPlan{}) {
		t.Fatalf("paused scene schedules frames: %+v", plan)
	}
	s.clock.setPlaying(now, true)
	if plan := s.repaintAt(500*time.Millisecond, true, box); !plan.continuous {
		t.Fatalf("active smooth highlight needs animation: %+v", plan)
	}
	if plan := s.repaintAt(500*time.Millisecond, false, box); plan.continuous || plan.after != 500*time.Millisecond {
		t.Fatalf("software/reduced-motion path should step: %+v", plan)
	}
	if plan := s.repaintAt(2*time.Second, true, box); plan.continuous || plan.after != 18*time.Second {
		t.Fatalf("long gap should sleep until next boundary: %+v", plan)
	}
	if err := s.clock.setRate(now, 2); err != nil {
		t.Fatal(err)
	}
	if plan := s.repaintAt(2*time.Second, true, box); plan.after != 9*time.Second {
		t.Fatalf("wakeup did not respect playback rate: %+v", plan)
	}
	if plan := s.repaintAt(s.duration, true, box); plan != (repaintPlan{}) {
		t.Fatalf("ended scene schedules frames: %+v", plan)
	}
}

func TestSceneTesterPaintLifecycle(t *testing.T) {
	s := testScene(t, 200)
	tt := ui.NewTester(s.view, 960, 740)
	if image := tt.Image(); image == nil || image.Bounds().Dx() != 960 {
		t.Fatal("native view did not produce a CPU image")
	}
	if err := tt.Click("Play"); err != nil {
		t.Fatal(err)
	}
	builds := s.viewBuilds
	shapes := s.cache.shapes
	tt.Advance(2 * time.Second)
	if s.viewBuilds != builds || s.cache.shapes != shapes {
		t.Fatalf("time-only paints rebuilt views or reshaped text: builds %d -> %d, shapes %d -> %d", builds, s.viewBuilds, shapes, s.cache.shapes)
	}
	if err := tt.Click("Pause"); err != nil {
		t.Fatal(err)
	}
	paused := s.clock.positionAt(tt.Now())
	tt.Advance(60 * time.Second)
	if tt.FramePending() || s.clock.positionAt(tt.Now()) != paused {
		t.Fatal("paused scene kept moving or requesting continuous frames")
	}
	old := s.rows[0].geometry
	tt.SetSize(740, 580)
	if s.rows[0].geometry == old {
		t.Fatal("width change retained old geometry")
	}
	old = s.rows[0].geometry
	tt.SetScale(2)
	if s.scale != 2 || s.rows[0].geometry == old {
		t.Fatal("DPI change did not invalidate geometry")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if !s.reduceMotion {
		t.Fatal("reduced-motion preference was not observed")
	}
	if err := tt.Click("Play"); err != nil {
		t.Fatal(err)
	}
	before := s.clock.positionAt(tt.Now())
	tt.SetWindowHidden(true)
	shapes = s.cache.shapes
	tt.Advance(30 * time.Second)
	if s.cache.shapes != shapes {
		t.Fatal("hidden view prepared new layouts")
	}
	tt.SetWindowHidden(false)
	if s.clock.positionAt(tt.Now()) != before+30*time.Second {
		t.Fatal("hidden recovery did not reconstruct host time")
	}
	if s.rows[0].index != s.focusAt(s.clock.positionAt(tt.Now())) {
		t.Fatal("hidden recovery kept stale visible rows")
	}
}

func TestSceneCloseAndEmpty(t *testing.T) {
	s := testScene(t, 0)
	tt := ui.NewTester(s.view, 640, 480)
	if s.rowCount != 0 {
		t.Fatal("empty document produced rows")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.document != nil || s.cache.document != nil || len(s.cache.entries) != 0 || s.boundaries != nil || s.fallback != nil || s.highlights != nil || s.rowCount != 0 {
		t.Fatal("Close retained resources")
	}
	if plan := s.repaintAt(0, true, ui.Rect{}); plan != (repaintPlan{}) {
		t.Fatal("closed scene schedules paints")
	}
}

func TestSceneSoftwareBackgroundAndText(t *testing.T) {
	s := testScene(t, 1)
	s.showBackground = true
	tt := ui.NewTester(func(c *ui.Context) { ui.Box(c).Fill().Draw(s.draw) }, 600, 400)
	img := tt.Image()
	if s.accelerated || img.RGBAAt(1, 1) == img.RGBAAt(1, 390) {
		t.Fatal("software path did not paint a static gradient")
	}
	textPixels := 0
	for y := 140; y < 220; y++ {
		for x := 24; x < 576; x++ {
			pixel := img.RGBAAt(x, y)
			if pixel.R > 90 && pixel.G > 90 && pixel.B > 90 {
				textPixels++
			}
		}
	}
	if textPixels == 0 || tt.FramePending() {
		t.Fatal("paused software path lacks readable text or continues requesting frames")
	}
}

func TestSceneClippedWordDoesNotAnimate(t *testing.T) {
	s := testScene(t, 1)
	s.clock.setPlaying(time.Unix(0, 0), true)
	s.rowCount = 1
	s.rows[0] = preparedLine{geometry: &lineGeometry{height: 500, words: []wordGeometry{
		{start: 0, end: time.Second, rects: []ui.Rect{{Y: 400, W: 50, H: 40}}},
	}}}
	if plan := s.repaintAt(500*time.Millisecond, true, ui.Rect{W: 600, H: 100}); plan.continuous || plan.after <= 0 {
		t.Fatalf("word below the viewport should use a boundary wakeup: %+v", plan)
	}
	s.rows[0].geometry.words[0].rects = nil
	if plan := s.repaintAt(500*time.Millisecond, true, ui.Rect{W: 600, H: 100}); plan.continuous {
		t.Fatal("empty geometry should not request continuous frames")
	}
}

func BenchmarkScenePreparation(b *testing.B) {
	for _, count := range []int{200, 2000, 10000} {
		b.Run("lines="+strconv.Itoa(count), func(b *testing.B) {
			s := testScene(b, count)
			box := ui.Rect{W: 900, H: 600}
			s.prepare(0, box)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s.prepare(100*time.Millisecond, box)
			}
		})
	}
}
