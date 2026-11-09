package main

import (
	"math"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

const maxPreparedLines = 16

type preparedLine struct {
	index    int
	geometry *lineGeometry
	y        float32
}

type preparationKey struct {
	document *lyric.Document
	focus    int
	box      ui.Rect
	scale    float32
}

// scene is an example-owned UI-thread resource, not a public component API.
// Draw may change its geometry cache, but never advances playback or emits events.
type scene struct {
	document       *lyric.Document
	clock          playbackClock
	duration       time.Duration
	boundaries     []time.Duration
	cache          geometryCache
	rows           [maxPreparedLines]preparedLine
	rowCount       int
	prepared       preparationKey
	preparedValid  bool
	highlights     []ui.Rect
	fallback       *ui.TextLayout
	scale          float32
	reduceMotion   bool
	forceStatic    bool
	showBackground bool
	viewBuilds     int
	drawCalls      uint64
	accelerated    bool
	manualLine     int
	closed         bool
}

func newScene(document *lyric.Document, now time.Time) *scene {
	s := &scene{document: document, manualLine: -1, scale: 1}
	for i := 0; i < document.Len(); i++ {
		line := document.Line(i)
		s.duration = max(s.duration, line.End())
		s.boundaries = append(s.boundaries, line.Start(), line.End())
		for j := 0; j < line.WordCount(); j++ {
			word := line.Word(j)
			s.boundaries = append(s.boundaries, word.Start, word.End)
		}
	}
	sort.Slice(s.boundaries, func(i, j int) bool { return s.boundaries[i] < s.boundaries[j] })
	n := 0
	for _, boundary := range s.boundaries {
		if n == 0 || boundary != s.boundaries[n-1] {
			s.boundaries[n] = boundary
			n++
		}
	}
	s.boundaries = s.boundaries[:n]
	s.clock = newPlaybackClock(s.duration, now)
	s.fallback = ui.ShapeText("Line outside M1 text limits", ui.Font{Size: 18}, 0)
	return s
}

// Close stops future paint scheduling and drops the document and all example-owned
// geometry references. It does not promise synchronous upstream/GPU reclamation.
// The host must unmount the view and call Close on the UI thread.
func (s *scene) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.clock.playing = false
	s.cache.clear()
	clear(s.rows[:])
	s.rowCount = 0
	s.prepared = preparationKey{}
	s.preparedValid = false
	s.highlights = nil
	s.fallback = nil
	s.boundaries = nil
	s.document = nil
	return nil
}

func (s *scene) focusAt(position time.Duration) int {
	if s.document.Len() == 0 {
		return -1
	}
	if s.manualLine >= 0 {
		return min(s.manualLine, s.document.Len()-1)
	}
	return max(0, sort.Search(s.document.Len(), func(i int) bool {
		return s.document.Line(i).Start() > position
	})-1)
}

// prepare visits at most 16 neighboring lines, never the whole document. A cold
// layout is prepared only for the viewport and at most one viewport of overscan.
// Cache misses on resize/seek are distinct from the warm playback hot path.
func (s *scene) prepare(position time.Duration, box ui.Rect) {
	focus := s.focusAt(position)
	key := preparationKey{document: s.document, focus: focus, box: box, scale: s.scale}
	if s.preparedValid && s.prepared == key {
		return
	}
	clear(s.rows[:])
	s.rowCount = 0
	s.cache.beginFrame()
	s.prepared, s.preparedValid = key, true
	if focus < 0 {
		return
	}
	font := ui.Font{Size: 30, Weight: 600, LineHeight: 1.35}
	width := max(float32(1), box.W-48)
	add := func(index int, y float32, above bool) float32 {
		g, err := s.cache.get(s.document, index, font, width, s.scale)
		height := float32(64)
		if err == nil {
			height = max(height, g.height+24)
			s.cache.pin(g)
		}
		if above {
			y -= height
		}
		s.rows[s.rowCount] = preparedLine{index: index, geometry: g, y: y}
		s.rowCount++
		return height
	}
	y := box.Y + box.H*0.35
	h := add(focus, y, false)
	// Split the fixed preparation budget so a very long previous line cannot
	// prevent the current and following lines from being drawn.
	top := y
	for i := focus - 1; i >= 0 && top > box.Y-box.H && s.rowCount < maxPreparedLines/2; i-- {
		top -= add(i, top, true)
	}
	bottom := y + h
	for i := focus + 1; i < s.document.Len() && bottom < box.Y+2*box.H && s.rowCount < maxPreparedLines; i++ {
		bottom += add(i, bottom, false)
	}
}

type repaintPlan struct {
	continuous bool
	after      time.Duration
}

// repaintAt uses sorted cold-prepared event times, not a per-frame document scan.
// Software/reduced-motion rendering uses completed-word steps at those times.
func (s *scene) repaintAt(position time.Duration, smooth bool, box ui.Rect) repaintPlan {
	if s.closed || !s.clock.playing || position >= s.duration {
		return repaintPlan{}
	}
	if smooth {
		for _, row := range s.rows[:s.rowCount] {
			g := row.geometry
			if g == nil || row.y+g.height <= box.Y || row.y >= box.Y+box.H {
				continue
			}
			for _, word := range g.words {
				if !word.atomic && word.start <= position && position < word.end {
					for _, rect := range word.rects {
						if rect.W > 0 && row.y+rect.Y+rect.H > box.Y && row.y+rect.Y < box.Y+box.H {
							return repaintPlan{continuous: true}
						}
					}
				}
			}
		}
	}
	next := sort.Search(len(s.boundaries), func(i int) bool { return s.boundaries[i] > position })
	if next == len(s.boundaries) {
		return repaintPlan{}
	}
	ns := math.Ceil(float64(s.boundaries[next]-position) / s.clock.rate)
	delay := time.Duration(math.MaxInt64)
	if ns < float64(math.MaxInt64) {
		delay = max(time.Millisecond, time.Duration(ns))
	}
	return repaintPlan{after: delay}
}

func (s *scene) draw(p *ui.Painter, box ui.Rect) {
	if s.closed || box.W <= 0 || box.H <= 0 {
		return
	}
	s.scale = p.Scale()
	s.drawCalls++
	s.accelerated = p.Accelerated()
	p.Fill(box, ui.RGB(18, 21, 31), 0)
	if s.showBackground {
		p.FillGradient(box, ui.LinearGradient{From: ui.RGB(34, 49, 67), To: ui.RGB(38, 24, 51), Angle: 150}, 0)
	}
	position := s.clock.positionAt(p.Now())
	s.prepare(position, box)
	smooth := s.accelerated && !s.forceStatic && !s.reduceMotion
	highlightPosition := position
	if !smooth {
		previous := sort.Search(len(s.boundaries), func(i int) bool { return s.boundaries[i] > position }) - 1
		if previous >= 0 {
			highlightPosition = s.boundaries[previous]
		}
	}
	p.Clip(box, 0, func() {
		for _, row := range s.rows[:s.rowCount] {
			g := row.geometry
			if g == nil {
				p.TextLayout(s.fallback, box.X+24, row.y, ui.RGB(177, 185, 199))
				continue
			}
			if row.y+g.height <= box.Y || row.y >= box.Y+box.H {
				continue
			}
			x := box.X + 24
			p.TextLayout(g.layout, x, row.y, ui.RGB(117, 130, 151))
			if !smooth {
				// All words are atomic in the static path: no partially filled word
				// can remain visible merely because another line has an event.
				s.highlights = s.highlights[:0]
				for _, word := range g.words {
					if word.end <= highlightPosition {
						s.highlights = append(s.highlights, word.rects...)
					}
				}
			} else {
				s.highlights = g.appendHighlights(s.highlights[:0], highlightPosition)
			}
			for _, clip := range s.highlights {
				clip.X += x
				clip.Y += row.y
				p.Clip(clip, 0, func() { p.TextLayout(g.layout, x, row.y, ui.RGB(242, 247, 255)) })
			}
		}
	})
	if s.duration > 0 {
		p.Fill(ui.Rect{X: box.X, Y: box.Y + box.H - 4, W: box.W * float32(float64(position)/float64(s.duration)), H: 4}, ui.RGB(116, 202, 236), 0)
	}
	plan := s.repaintAt(position, smooth, box)
	if plan.continuous {
		p.AnimationFrame()
	} else if plan.after > 0 {
		p.After(plan.after)
	}
}
