// Command native is the M1 Native UI spike, using synthetic lyrics and a
// simulated host clock. It does not open an audio device or load song files.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	lineCount := flag.Int("lines", 200, "synthetic line count (1..10000; compare 200, 2000, 10000)")
	software := flag.Bool("software", false, "use MyGo's software renderer (MYGO_GPU=0)")
	smoke := flag.Bool("smoke", false, "exercise a real window and exit after 6 seconds")
	smokeDuration := flag.Duration("smoke-duration", 0, "exercise a real window for this duration, then exit")
	flag.Parse()
	if *lineCount < 1 || *lineCount > 10000 || *smokeDuration < 0 {
		log.Fatal("native: require 1 <= lines <= 10000 and nonnegative smoke-duration")
	}
	if *software {
		if err := os.Setenv("MYGO_GPU", "0"); err != nil {
			log.Fatal(err)
		}
	}
	if *smoke && *smokeDuration == 0 {
		*smokeDuration = 6 * time.Second
	}
	document, err := demoDocument(*lineCount)
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.WhenReady(func() {
		now := time.Now()
		s := newScene(document, now)
		s.clock.setPlaying(now, true)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "mygo-amll · M1 Native spike", Width: 960, Height: 740,
			MinWidth: 600, MinHeight: 440, Content: ui.View(s.view),
		})
		ctx, cancel := context.WithCancel(context.Background())
		var off func()
		off = win.OnClosed(func() {
			cancel()
			if *smokeDuration > 0 {
				log.Printf("native smoke: accelerated=%t scale=%.2f draws=%d view-builds=%d cached-layouts=%d cache-estimate=%dB", s.accelerated, s.scale, s.drawCalls, s.viewBuilds, len(s.cache.entries), s.cache.bytes)
			}
			_ = s.Close()
			off()
		})
		if *smokeDuration > 0 {
			go runWindowSmoke(ctx, win, s, *smokeDuration)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// view owns input and playback changes on the UI thread. Draw derives its
// position from the anchor and requests paint-only frames while needed.
func (s *scene) view(c *ui.Context) {
	if s.closed {
		return
	}
	s.viewBuilds++
	s.reduceMotion = c.Preferences().ReduceMotion
	now := c.Now()
	if c.Shortcut(0, ui.KeySpace) {
		s.clock.setPlaying(now, !s.clock.playing)
	}
	if c.Shortcut(0, ui.KeyLeft) {
		s.clock.seek(now, s.clock.positionAt(now)-5*time.Second)
	}
	if c.Shortcut(0, ui.KeyRight) {
		s.clock.seek(now, s.clock.positionAt(now)+5*time.Second)
	}
	if c.Shortcut(0, ui.KeyHome) {
		s.manualLine = -1
	}
	for _, scroll := range []struct {
		key   ui.Key
		lines int
	}{{ui.KeyUp, -1}, {ui.KeyDown, 1}, {ui.KeyPageUp, -5}, {ui.KeyPageDown, 5}} {
		if c.Shortcut(0, scroll.key) {
			s.scrollLines(now, scroll.lines)
		}
	}

	theme := c.Theme()
	ui.Column(c).Fill().Padding(16).Gap(10).Children(func() {
		ui.Text(c, "Native lyric rendering · M1").FontSize(22).Bold()
		ui.Row(c).WidthPercent(100).Wrap().Gap(8).Children(func() {
			playLabel := "Play"
			if s.clock.playing {
				playLabel = "Pause"
			}
			if ui.Button(c.Key("play"), playLabel).Width(68).Clicked() {
				s.clock.setPlaying(now, !s.clock.playing)
			}
			if ui.Button(c, "−5s").Label("Seek backward").Clicked() {
				s.clock.seek(now, s.clock.positionAt(now)-5*time.Second)
			}
			if ui.Button(c, "+5s").Label("Seek forward").Clicked() {
				s.clock.seek(now, s.clock.positionAt(now)+5*time.Second)
			}
			if ui.Button(c, "Restart").Clicked() {
				s.clock.seek(now, 0)
				s.manualLine = -1
			}
			if ui.Button(c, "Follow").Clicked() {
				s.manualLine = -1
			}
			for _, rate := range []float64{0.5, 1, 2} {
				if ui.Button(c.Key(rate), fmt.Sprintf("%g×", rate)).Disabled(s.clock.rate == rate).Clicked() {
					_ = s.clock.setRate(now, rate)
				}
			}
		})
		ui.Row(c).WidthPercent(100).Wrap().Gap(12).Children(func() {
			ui.Checkbox(c, &s.showBackground, "Static background")
			ui.Checkbox(c, &s.forceStatic, "Static lyrics")
		})
		ui.Text(c, "Space: play/pause · ← →: seek · ↑ ↓ / PgUp PgDn: browse · Home: follow").
			FontSize(12).TextColor(theme.TextMuted)
		ui.Box(c.Key("lyrics")).Grow(1).WidthPercent(100).MinHeight(160).
			Role(ui.RoleGroup).Label("Synthetic lyrics").Draw(s.draw)
	})
}

// scrollLines performs a single input action; the per-frame draw path never
// scans or changes this manual scroll anchor.
func (s *scene) scrollLines(now time.Time, delta int) {
	if s.document == nil || s.document.Len() == 0 {
		return
	}
	if s.manualLine < 0 {
		position := s.clock.positionAt(now)
		s.manualLine = sort.Search(s.document.Len(), func(i int) bool {
			return s.document.Line(i).Start() > position
		}) - 1
	}
	s.manualLine = min(max(s.manualLine+delta, 0), s.document.Len()-1)
}

// runWindowSmoke is opt-in native-window integration work, not a timing-based
// unit test. Its only goroutine is canceled at close; all model writes are
// submitted to the UI thread. It exercises pause, seek, resize and hide/show.
func runWindowSmoke(ctx context.Context, win *mygo.Window, s *scene, duration time.Duration) {
	started := time.Now()
	for step := 1; step <= 6; step++ {
		offset := duration/6*time.Duration(step) + duration%6*time.Duration(step)/6
		timer := time.NewTimer(time.Until(started.Add(offset)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		step := step
		win.Update(func() {
			if ctx.Err() != nil {
				return
			}
			now := time.Now()
			switch step {
			case 1:
				s.clock.setPlaying(now, false)
				s.clock.seek(now, 9*time.Second)
			case 2:
				win.SetSize(740, 580)
			case 3:
				win.Hide()
			case 4:
				win.Show()
				s.clock.setPlaying(now, true)
				_ = s.clock.setRate(now, 2)
			case 5:
				s.showBackground = true
				win.SetSize(960, 740)
			case 6:
				win.Close()
			}
		})
	}
}
