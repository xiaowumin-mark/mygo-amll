// Command native demonstrates experimental M3 rendering with a simulated host
// clock. It uses synthetic lyrics or a bounded local TTML file, without audio.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	lineCount := flag.Int("lines", 200, "synthetic line count (1..10000; compare 200, 2000, 10000)")
	software := flag.Bool("software", false, "use MyGo's software renderer (MYGO_GPU=0)")
	static := flag.Bool("static", false, "disable lyric and scroll animation for deterministic inspection")
	font := flag.String("font", "", "font family fallback list (empty uses platform defaults)")
	fontSize := flag.Float64("font-size", 0, "fixed lyric DIP size (0 uses the reference viewport sizing; maximum 512)")
	effectsSoftware := flag.Bool("effects-software", false, "diagnostic: evaluate the full effect shaders in software (slow)")
	ttmlPath := flag.String("ttml", "", "load a local TTML file instead of synthetic lyrics (host I/O only)")
	startAt := flag.Duration("at", 0, "initial absolute track position, for example 90s")
	smoke := flag.Bool("smoke", false, "exercise a real window and exit after 6 seconds")
	smokeDuration := flag.Duration("smoke-duration", 0, "exercise a real window for this duration, then exit")
	flag.Parse()
	if *lineCount < 1 || *lineCount > 10000 || *smokeDuration < 0 || *startAt < 0 || *fontSize < 0 || *fontSize > 512 || math.IsNaN(*fontSize) || math.IsInf(*fontSize, 0) {
		log.Fatal("native: require 1 <= lines <= 10000, nonnegative smoke-duration/at and 0 <= font-size <= 512")
	}
	if *software {
		if err := os.Setenv("MYGO_GPU", "0"); err != nil {
			log.Fatal(err)
		}
	}
	if *smoke && *smokeDuration == 0 {
		*smokeDuration = 6 * time.Second
	}
	document, _ := demoDocument(0)
	if *ttmlPath != "" {
		result, err := loadTTML(context.Background(), *ttmlPath)
		if err != nil {
			log.Fatal(err)
		}
		document = result.Document
		log.Printf("native TTML: lines=%d diagnostics=%d", document.Len(), len(result.Diagnostics))
		for _, diagnostic := range result.Diagnostics {
			log.Printf("TTML %s at %d:%d: %s", diagnostic.Code, diagnostic.Line, diagnostic.Column, diagnostic.Message)
		}
	} else {
		var err error
		document, err = demoDocument(*lineCount)
		if err != nil {
			log.Fatal(err)
		}
	}
	fonts, releaseFonts, err := openNativeReferenceFonts()
	if err != nil {
		log.Fatal(err)
	}
	defer releaseFonts()
	mygo.App.WhenReady(func() {
		now := time.Now()
		s := newScene(document, now)
		s.fontProvider = fonts
		s.forceStatic, s.font.Family = *static, *font
		s.font.Size = float32(*fontSize)
		if err := s.enableAMLL(*effectsSoftware); err != nil {
			log.Fatal(err)
		}
		s.onSeek = func(at time.Time, request seekRequest) { s.seek(at, request.Position) }
		s.clock.seek(now, *startAt)
		s.clock.setPlaying(now, true)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "mygo-amll · M3 Lyrics", Width: 960, Height: 740,
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

// view owns input and clock updates on the UI thread. The reference path
// prepares one timestamp here and requests animation-frame view updates;
// Draw consumes that frozen state, including when it runs more than once.
func (s *scene) view(c *ui.Context) {
	if s.closed {
		return
	}
	s.viewBuilds++
	restored := s.hidden && !c.WindowHidden()
	s.hidden = c.WindowHidden()
	s.reduceMotion = c.Preferences().ReduceMotion
	now := c.Now()
	if c.Shortcut(0, ui.KeySpace) {
		s.clock.setPlaying(now, !s.clock.playing)
	}
	if c.Shortcut(0, ui.KeyLeft) {
		s.seek(now, s.clock.positionAt(now)-5*time.Second)
	}
	if c.Shortcut(0, ui.KeyRight) {
		s.seek(now, s.clock.positionAt(now)+5*time.Second)
	}
	if c.Shortcut(0, ui.KeyHome) {
		s.resume(now)
	}
	if c.Shortcut(0, ui.KeyEnter) {
		index := s.selectedLine
		if index < 0 {
			index = s.focusAt(s.clock.positionAt(now))
		}
		s.requestSeek(now, index)
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
	ui.Column(c).Fill().Padding(16).Gap(10).Draw(func(_ *ui.Painter, box ui.Rect) {
		if s.hasReference() {
			s.setReferenceHostSize(box.W, box.H)
		}
	}).Children(func() {
		ui.Text(c, "Native lyric rendering · M3").FontSize(22).Bold()
		ui.Row(c).WidthPercent(100).Wrap().Gap(8).Children(func() {
			playLabel := "Play"
			if s.clock.playing {
				playLabel = "Pause"
			}
			if ui.Button(c.Key("play"), playLabel).Width(68).Clicked() {
				s.clock.setPlaying(now, !s.clock.playing)
			}
			if ui.Button(c, "−5s").Label("Seek backward").Clicked() {
				s.seek(now, s.clock.positionAt(now)-5*time.Second)
			}
			if ui.Button(c, "+5s").Label("Seek forward").Clicked() {
				s.seek(now, s.clock.positionAt(now)+5*time.Second)
			}
			if ui.Button(c, "Restart").Clicked() {
				s.seek(now, 0)
			}
			if ui.Button(c, "Follow").Clicked() {
				s.resume(now)
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
			ui.TextInput(c, &s.font.Family).Label("Font families").Placeholder("System font fallback").Width(260)
		})
		ui.Text(c, "Drag / wheel: scroll · Click / Enter: seek · ↑ ↓ / PgUp PgDn: browse · Home: follow").
			FontSize(12).TextColor(theme.TextMuted)
		if s.hasReference() {
			ui.Box(c.Key("lyrics-background")).Grow(1).WidthPercent(100).MinHeight(160).
				Draw(func(p *ui.Painter, box ui.Rect) {
					p.Fill(box, ui.RGB(18, 21, 31), 0)
					if s.showBackground {
						p.FillGradient(box, ui.LinearGradient{From: ui.RGB(34, 49, 67), To: ui.RGB(38, 24, 51), Angle: 150}, 0)
					}
				}).Children(func() {
				lyrics := ui.Box(c.Key("lyrics")).Fill().Focusable().Role(ui.RoleGroup).Label("Lyrics").
					Description(s.accessibleLyrics(now)).HandleInput(s.input).Effect(s.referenceRootEffect()).Draw(s.draw)
				s.setReferenceHover(lyrics.Hovered())
				pointerX, pointerY, pointerOver := lyrics.PointerPosition()
				s.setReferencePointer(pointerX, pointerY, pointerOver)
			})
		} else {
			ui.Box(c.Key("lyrics")).Grow(1).WidthPercent(100).MinHeight(160).
				Focusable().Role(ui.RoleGroup).Label("Lyrics").
				Description(s.accessibleLyrics(now)).HandleInput(s.input).Draw(s.draw)
		}
	})
	s.update(now, c.Instant() || restored && !s.hasReference())
	clientW, clientH := c.Size()
	if s.referenceViewFrame(now, clientW, clientH) {
		c.AnimationFrame()
	} else if delay := s.referenceWake(now); delay > 0 && !s.hidden {
		c.After(delay)
	}
	if !s.hidden && s.clock.playing {
		if delay := nextDelay(s.focusEvents, s.clock.positionAt(now), s.clock.rate); delay > 0 {
			c.After(delay)
		}
	}
}

// Expose the browsed/focused line and its annotations without broadcasting word
// animation or rebuilding the whole accessibility tree on each paint-only frame.
func (s *scene) accessibleLyrics(now time.Time) string {
	index := s.focusAt(s.clock.positionAt(now))
	if index < 0 {
		return "No lyrics"
	}
	line := s.document.Line(index)
	return line.Text() + "\n" + line.Romanization() + "\n" + line.Translation()
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
				s.seek(now, 9*time.Second)
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
