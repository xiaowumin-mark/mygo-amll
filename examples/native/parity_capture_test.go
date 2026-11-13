// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestReferenceSceneCapture is an opt-in diagnostic, not a visual golden test.
// It records the complete software shader path at a held media position after
// three seconds of 60 Hz wall-clock updates. The reference browser must use the
// same viewport, font, media position and transport history for comparisons.
// Neither private lyrics nor generated images are repository fixtures.
func TestReferenceSceneCapture(t *testing.T) {
	directory := os.Getenv("MYGO_AMLL_PARITY_CAPTURE")
	if directory == "" {
		t.Skip("set MYGO_AMLL_PARITY_CAPTURE to a local artifact directory")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	document, err := demoDocument(200)
	if input := os.Getenv("MYGO_AMLL_TEST_TTML"); input != "" {
		result, loadErr := loadTTML(context.Background(), input)
		document, err = result.Document, loadErr
	}
	if err != nil {
		t.Fatal(err)
	}
	fonts, releaseFonts, err := openNativeReferenceFonts()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseFonts)
	position := 90 * time.Second
	if value := os.Getenv("MYGO_AMLL_PARITY_AT"); value != "" {
		position, err = time.ParseDuration(value)
		if err != nil || position < 0 {
			t.Fatal("MYGO_AMLL_PARITY_AT must be a nonnegative Go duration")
		}
	}
	const width, height = 960, 740
	fontSize := float32(30)
	if value := os.Getenv("MYGO_AMLL_PARITY_FONT_SIZE"); value != "" {
		parsed, parseErr := strconv.ParseFloat(value, 32)
		fontSize = float32(parsed)
		if parseErr != nil || !finitePositive(fontSize) || fontSize > 512 {
			t.Fatal("MYGO_AMLL_PARITY_FONT_SIZE must be finite and within 0..512 DIP")
		}
	}
	scale := float32(1)
	if value := os.Getenv("MYGO_AMLL_PARITY_SCALE"); value != "" {
		parsed, parseErr := strconv.ParseFloat(value, 32)
		scale = float32(parsed)
		if parseErr != nil || !finitePositive(scale) || scale > effectScaleLimit {
			t.Fatal("MYGO_AMLL_PARITY_SCALE must be finite and within the effect scale budget")
		}
	}
	now := time.Unix(0, 0)
	s := newScene(document, now)
	s.fontProvider = fonts
	t.Cleanup(func() { _ = s.Close() })
	s.font = ui.Font{Family: "Segoe UI", Size: fontSize, Weight: 600, LineHeight: 1.2, Lang: "en"}
	if err := s.enableAMLL(true); err != nil {
		t.Fatal(err)
	}
	s.setReferenceHostSize(width, height)
	s.clock.seek(now, position)
	s.clock.setPlaying(now, true)
	box := ui.Rect{W: width, H: height}
	s.prepareReferenceFrame(now, box, scale, false)
	for frame := 1; frame <= 180; frame++ {
		now = time.Unix(0, 0).Add(time.Duration(frame) * time.Second / 60)
		// Holding the host media position does not pause wall-clock word effects.
		s.clock.seek(now, position)
		s.update(now, false)
	}
	type blockTrace struct {
		X, Y, Width, Height, BodyX, BodyY, BodyDrawY float32
		FontSize, LineHeight                         float32
	}
	type annotationTrace struct {
		Y, BoxY, Height, FontSize float32
	}
	type layerTrace struct {
		X, Y, Scale, OriginX, OriginY float32
		Width, Height, BodyHeight     float32
		Style                         effectStyle
		TextFallback                  bool
		Blocks                        []blockTrace
		Annotations                   []annotationTrace
	}
	type rowTrace struct {
		Index, Main, Background int
		X, Y, Height            float32
		Opacity, Blur           float32
		Visible                 bool
		Error                   string `json:",omitempty"`
		Layers                  []layerTrace
	}
	r := s.reference
	rows := make([]rowTrace, 0, r.rowCount)
	for _, row := range r.rows[:r.rowCount] {
		pg := r.presentation.groups[row.index]
		out := rowTrace{Index: row.index, Main: pg.main, Background: pg.background, X: row.x, Y: row.y, Height: row.height, Opacity: row.opacity, Blur: row.blur, Visible: row.visible}
		if row.err != nil {
			out.Error = row.err.Error()
		}
		for _, layer := range row.layers[:row.layerCount] {
			g := layer.Geometry
			trace := layerTrace{X: layer.X, Y: layer.Y, Scale: layer.Scale, OriginX: layer.OriginX, OriginY: layer.OriginY, Width: g.width, Height: g.height, BodyHeight: g.bodyHeight, Style: layer.Style, TextFallback: g.bidiFallback}
			for _, block := range g.blocks {
				trace.Blocks = append(trace.Blocks, blockTrace{block.x, block.y, block.width, block.height, block.bodyX, block.bodyY, block.bodyDrawY, block.font.Size, block.font.LineHeight})
			}
			for _, annotation := range g.annotations {
				trace.Annotations = append(trace.Annotations, annotationTrace{annotation.y, annotation.boxY, annotation.boxHeight, annotation.font.Size})
			}
			out.Layers = append(out.Layers, trace)
		}
		rows = append(rows, out)
	}
	report := struct {
		Backend, Font, Fallback string
		Width, Height           int
		Scale                   float32
		Position, WallTime      time.Duration
		Lines, Groups, Focus    int
		EffectBytes             int
		Rows                    []rowTrace
	}{"software-shaders", fmt.Sprintf("Segoe UI 600 %gpx", fontSize), r.fallback, width, height, scale, position, 3 * time.Second, document.Len(), len(r.groups), r.state.Focus, r.effects.bytes, rows}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "native-layout.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	// A complete Gaussian/root shader pass through MyGo's CPU interpreter is
	// intentionally optional; it is far slower than the real GPU window.
	if os.Getenv("MYGO_AMLL_PARITY_LAYOUT_ONLY") == "" {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(func(p *ui.Painter, rect ui.Rect) {
				p.Fill(rect, ui.RGB(18, 21, 31), 0)
			}).Children(func() {
				ui.Box(c).Fill().Effect(s.referenceRootEffect()).Draw(s.draw)
			})
		}, width, height)
		if scale != 1 {
			tt.SetScale(scale)
		}
		tt.Frame()
		writeSnapshot(t, filepath.Join(directory, "native-software.png"), tt.Image())
	}
	if r.fallback != "" {
		t.Fatalf("capture entered a fallback: %s", r.fallback)
	}
	if r.effects.bytes > effectByteLimit {
		t.Fatalf("effect byte budget exceeded: %d", r.effects.bytes)
	}
	t.Logf("diagnostic only: %d lines, %d groups, focus %d, %d effect bytes; no visual-equivalence assertion", document.Len(), len(r.groups), r.state.Focus, r.effects.bytes)
}
