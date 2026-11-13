package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func TestM3VisibleRowsBeforeOverscan(t *testing.T) {
	lines := make([]lyric.Line, 100)
	for i := range lines {
		lines[i] = lyric.Line{Text: "short", Start: time.Duration(i) * time.Second, End: time.Duration(i+1) * time.Second}
	}
	doc, err := lyric.NewDocument(lines)
	if err != nil {
		t.Fatal(err)
	}
	s := newScene(doc, time.Unix(0, 0))
	defer s.Close()
	box := ui.Rect{W: 900, H: 1600}
	s.prepare(50*time.Second, box)
	anchorY := s.contentAt(50)
	visible := 0
	for i := range lines {
		y := box.H*0.35 + float32(s.heights.Prefix(i)-anchorY)
		if y >= box.H || y+float32(s.heights.Height(i)) <= 0 {
			continue
		}
		visible++
		found := false
		for _, row := range s.rows[:s.rowCount] {
			found = found || row.index == i && row.geometry != nil
		}
		if !found {
			t.Fatalf("visible row %d missing while overscan consumed the frame budget", i)
		}
	}
	if visible < 20 || s.rowCount > geometryEntryLimit || s.cache.bytes > geometryByteLimit {
		t.Fatalf("invalid large viewport coverage/budget: visible=%d rows=%d bytes=%d", visible, s.rowCount, s.cache.bytes)
	}
}

func TestM3FontReloadInvalidatesFrameAndHeights(t *testing.T) {
	s := testScene(t, 20)
	box := ui.Rect{W: 800, H: 600}
	s.prepare(0, box)
	old := s.rows[0].geometry
	s.invalidateGeometry()
	if s.heights != nil || s.rowCount != 0 || len(s.cache.entries) != 0 || s.cache.document != nil {
		t.Fatal("font reload retained geometry or row heights")
	}
	for _, row := range s.rows {
		if row.geometry != nil {
			t.Fatal("font reload retained an old frame layout")
		}
	}
	s.prepare(0, box)
	if s.rows[0].geometry == nil || s.rows[0].geometry == old {
		t.Fatal("unchanged family name reused the preceding font's geometry")
	}
}

func TestM3MixedDirectionWordSchedulesClusterAnimation(t *testing.T) {
	doc, err := lyric.NewDocument([]lyric.Line{{Text: "Hello שלום English", End: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(0, 0)
	s := newScene(doc, now)
	defer s.Close()
	s.clock.setPlaying(now, true)
	box := ui.Rect{W: 800, H: 400}
	s.prepare(500*time.Millisecond, box)
	g := s.rows[0].geometry
	if g == nil || len(g.clusters) == 0 {
		t.Fatal("mixed text has no retained cluster geometry")
	}
	if plan := s.repaintAt(500*time.Millisecond, true, box); !plan.continuous {
		t.Fatalf("mixed-direction word still uses the M1 whole-word scheduler: %+v", plan)
	}
}
