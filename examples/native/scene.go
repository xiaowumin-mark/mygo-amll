package main

import (
	"math"
	"slices"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/layout"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

const (
	maxPreparedLines = geometryEntryLimit
	// Springs use 128 units per row so their settling tolerance is subpixel.
	scrollUnit = 128.0
)

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
	font     ui.Font
	anchor   float64
}

type heightKey struct {
	document *lyric.Document
	font     ui.Font
	width    float32
	scale    float32
}

type seekRequest struct {
	LineID   string
	Position time.Duration
}

// scene owns UI-thread resources. Mutable interaction/follow/spring anchors are
// updated by the view or input handler. Draw only samples them and prepares a
// disposable frame/cache; repeated draws at one timestamp have identical output.
// This remains an experimental host integration, not a stable component API.
type scene struct {
	document       *lyric.Document
	index          *timeline.Index
	state          timeline.VisualState
	clock          playbackClock
	duration       time.Duration
	boundaries     []time.Duration
	focusEvents    []time.Duration
	cache          geometryCache
	rows           [maxPreparedLines]preparedLine
	rowCount       int
	prepared       preparationKey
	preparedValid  bool
	heights        *layout.Heights
	heightKey      heightKey
	highlights     []ui.Rect
	fallback       *ui.TextLayout
	interlude      *ui.TextLayout
	font           ui.Font
	fontProvider   *referenceFontProvider
	scale          float32
	viewport       ui.Rect
	origin         time.Time
	follow         timeline.FollowController
	scroll         motion.Spring
	motionReady    bool
	manualLine     int
	pointerDown    bool
	dragged        bool
	pressY, lastY  float32
	onSeek         func(time.Time, seekRequest)
	selectedLine   int
	reduceMotion   bool
	forceStatic    bool
	showBackground bool
	viewBuilds     int
	viewClientSize ui.Point
	viewFrameAsked bool
	drawCalls      uint64
	accelerated    bool
	hidden         bool
	closed         bool
	reference      *referenceScene
}

func newScene(document *lyric.Document, now time.Time) *scene {
	s := &scene{
		document: document, origin: now,
		manualLine: -1, selectedLine: -1, scale: 1,
		font: ui.Font{Size: 30, Weight: 600, LineHeight: 1.35},
	}
	s.index, _ = timeline.New(document) // NewDocument already validated this input.
	// Cold document preparation only. Queries own separate, reusable buffers.
	s.state.Playing = make([]int, 0, document.Len())
	s.state.Highlighted = make([]int, 0, document.Len())
	for i := 0; i < document.Len(); i++ {
		line := document.Line(i)
		s.duration = max(s.duration, line.End())
		s.focusEvents = append(s.focusEvents, line.Start(), line.End())
		s.boundaries = append(s.boundaries, line.Start(), line.End())
		for j := 0; j < line.WordCount(); j++ {
			word := line.Word(j)
			s.boundaries = append(s.boundaries, word.Start, word.End)
		}
	}
	slices.Sort(s.boundaries)
	s.boundaries = slices.Compact(s.boundaries)
	slices.Sort(s.focusEvents)
	s.focusEvents = slices.Compact(s.focusEvents)
	s.clock = newPlaybackClock(s.duration, now)
	s.fallback = ui.ShapeText("Line exceeds text budget or has unmatched timing text", ui.Font{Size: 16}, 360)
	s.interlude = ui.ShapeText("•  •  •", ui.Font{Size: 24}, 0)
	return s
}

// referenceViewFrame reserves one complete view update after an idle input or
// client resize. The actual lyric box and scale are discovered later in paint;
// any motion started there must get a fresh prepared timestamp. This also
// covers an idle DPI change whose DIP client size stays the same.
// An animation-frame chain consumes the prior reservation without making a new
// one once it settles, so a paused player does not keep waking itself.
func (s *scene) referenceViewFrame(now time.Time, width, height float32) bool {
	size := ui.Point{X: width, Y: height}
	confirm := !s.viewFrameAsked || s.viewClientSize != size
	s.viewClientSize = size
	s.viewFrameAsked = false
	r := s.reference
	if r == nil || s.closed || s.hidden {
		return false
	}
	continuous := s.referenceWake(now) == referenceFrameInterval
	dynamic := !s.forceStatic && !s.reduceMotion && (r.accelerated || r.allowSoftware || !r.ready)
	s.viewFrameAsked = continuous || confirm && dynamic
	return s.viewFrameAsked
}

// Close is idempotent. Unmount before calling on the UI thread. It drops our
// document, callbacks and layout references, without promising synchronous
// reclamation of mygo font atlases or GPU resources. The reference path also
// cancels its bounded pure-numeric checkpoint worker and drops pending results.
func (s *scene) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.clock.playing = false
	if s.reference != nil {
		_ = s.reference.close()
		s.reference = nil
	}
	s.cache.clear()
	clear(s.rows[:])
	s.rowCount = 0
	s.prepared, s.heightKey = preparationKey{}, heightKey{}
	s.preparedValid = false
	s.heights, s.highlights = nil, nil
	s.fallback, s.interlude = nil, nil
	s.boundaries, s.focusEvents = nil, nil
	s.state = timeline.VisualState{}
	s.document, s.index, s.onSeek = nil, nil, nil
	s.fontProvider = nil
	return nil
}

// invalidateGeometry is the host's UI-thread hook after reloading font data
// under an unchanged family name. Drop frame pins before clearing the cache.
func (s *scene) invalidateGeometry() {
	if s.reference != nil {
		r := s.reference
		clear(r.rows[:])
		r.rowCount = 0
		r.geometryInvalid = true
		r.effects.Clear()
		r.dirty = true
		r.revision++
	}
	clear(s.rows[:])
	s.rowCount = 0
	s.preparedValid = false
	s.prepared, s.heightKey = preparationKey{}, heightKey{}
	s.heights = nil
	s.cache.clear()
}

func (s *scene) visualAt(position time.Duration) timeline.VisualState {
	s.state = s.index.VisualAt(position, s.state.Playing, s.state.Highlighted)
	return s.state
}

func (s *scene) focusAt(position time.Duration) int {
	if s.reference != nil {
		return s.referenceFocusAt(position)
	}
	if s.manualLine >= 0 {
		return min(s.manualLine, s.document.Len()-1)
	}
	return s.visualAt(position).Focus
}

func (s *scene) anchorAt(now time.Time, position time.Duration) float64 {
	if !s.motionReady {
		return float64(max(0, s.focusAt(position)))
	}
	anchor, _ := s.scroll.Sample(now)
	return anchor / scrollUnit
}

// update runs only outside Draw. Full view rebuilds are scheduled at line/end
// boundaries; word animation uses paint-only frames between them.
func (s *scene) update(now time.Time, instant bool) {
	if s.reference != nil {
		s.updateReference(now, instant)
		return
	}
	state := s.visualAt(s.clock.positionAt(now))
	visible := false
	for _, row := range s.rows[:s.rowCount] {
		if row.index == state.Focus && row.geometry != nil {
			visible = !s.hidden && row.y+row.geometry.height > s.viewport.Y && row.y < s.viewport.Y+s.viewport.H
			break
		}
	}
	if s.follow.Update(state, max(0, now.Sub(s.origin)), visible) {
		s.manualLine = -1
		s.selectedLine = -1
		target := float64(max(0, state.Focus)) * scrollUnit
		if !s.motionReady || instant || s.forceStatic || s.reduceMotion || !s.accelerated {
			s.scroll.Snap(now, target)
		} else {
			s.scroll.Retarget(now, target)
		}
		s.motionReady = true
	}
}

func (s *scene) seek(now time.Time, position time.Duration) {
	s.clock.seek(now, position)
	if s.reference != nil {
		s.reference.seekRevision++
		s.reference.revision++
		s.reference.seeking = true
		s.reference.continuous = false
		s.reference.dirty = true
		if !s.reference.touch {
			s.reference.resetInteraction()
			s.manualLine, s.selectedLine = -1, -1
		}
		s.updateReference(now, true)
		return
	}
	s.follow.Seek()
	s.update(now, true)
}

func (s *scene) resume(now time.Time) {
	if s.reference != nil {
		s.reference.resetInteraction()
		s.reference.dirty = true
		s.reference.revision++
		s.manualLine, s.selectedLine = -1, -1
		s.updateReference(now, false)
		return
	}
	s.follow.Resume()
	s.manualLine = -1
	s.update(now, false)
}

// prepare is also used by CPU benchmarks to measure a settled, deterministic
// viewport. The animated draw path passes its sampled fractional anchor instead.
func (s *scene) prepare(position time.Duration, box ui.Rect) {
	s.prepareWindow(float64(max(0, s.focusAt(position))), box)
}

// prepareWindow measures at most 32 rows around the sampled anchor, with no more
// than a viewport of overscan on either side. Heights of unseen rows are estimates;
// refining them cannot move the anchor because scrolling uses row coordinates.
func (s *scene) prepareWindow(anchor float64, box ui.Rect) {
	if s.document.Len() == 0 || box.W <= 0 || box.H <= 0 {
		s.rowCount = 0
		return
	}
	anchor = min(max(0, anchor), float64(s.document.Len())-1e-6)
	focus := int(anchor)
	width := max(float32(1), float32(math.Floor(float64(max(1, box.W-48))/4))*4)
	hkey := heightKey{s.document, s.font, width, s.scale}
	if s.heights == nil || hkey != s.heightKey {
		s.heights, _ = layout.NewHeights(s.document.Len(), 88)
		s.heightKey = hkey
		s.preparedValid = false
	}
	key := preparationKey{s.document, focus, box, s.scale, s.font, anchor}
	if s.preparedValid && key == s.prepared {
		return
	}
	clear(s.rows[:])
	s.rowCount = 0
	s.cache.beginFrame()
	s.prepared, s.preparedValid = key, true
	add := func(index int) float32 {
		g, err := s.cache.get(s.document, index, s.font, width, s.scale)
		height := float32(88)
		if err != nil {
			g = nil
		} else {
			height = max(64, g.height+24)
			s.cache.pin(g)
			if count := g.highlightRectCount(); count > cap(s.highlights) {
				s.highlights = make([]ui.Rect, 0, count)
			}
		}
		_ = s.heights.SetHeight(index, float64(height))
		s.rows[s.rowCount] = preparedLine{index: index, geometry: g}
		s.rowCount++
		return height
	}
	height := add(focus)
	top := box.H*0.35 - float32(anchor-float64(focus))*height
	bottom := top + height
	before, after := focus-1, focus+1
	// Fill visible content before spending any budget on overscan.
	for before >= 0 && top > 0 && s.rowCount < maxPreparedLines {
		top -= add(before)
		before--
	}
	for after < s.document.Len() && bottom < box.H && s.rowCount < maxPreparedLines {
		bottom += add(after)
		after++
	}
	for s.rowCount < maxPreparedLines {
		added := false
		if before >= 0 && top > -box.H {
			top -= add(before)
			before--
			added = true
		}
		if after < s.document.Len() && bottom < 2*box.H && s.rowCount < maxPreparedLines {
			bottom += add(after)
			after++
			added = true
		}
		if !added {
			break
		}
	}
	anchorY := s.contentAt(anchor)
	for i := range s.rows[:s.rowCount] {
		row := &s.rows[i]
		row.y = box.Y + box.H*0.35 + float32(s.heights.Prefix(row.index)-anchorY)
	}
	s.viewport = box
}

func (s *scene) contentAt(anchor float64) float64 {
	if s.heights == nil || s.heights.Len() == 0 {
		return 0
	}
	anchor = min(max(0, anchor), float64(s.heights.Len())-1e-6)
	i := int(anchor)
	return s.heights.Prefix(i) + (anchor-float64(i))*s.heights.Height(i)
}

func (s *scene) scrollPixels(now time.Time, delta float64) {
	if s.heights == nil || s.heights.Len() == 0 || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return
	}
	anchor := s.anchorAt(now, s.clock.positionAt(now))
	y := min(max(0, s.contentAt(anchor)+delta), s.heights.Total()-1e-6)
	i := s.heights.LineAt(y)
	anchor = float64(i) + (y-s.heights.Prefix(i))/s.heights.Height(i)
	s.manualLine = i
	s.selectedLine = i
	s.scroll.Snap(now, anchor*scrollUnit)
	s.motionReady = true
}

func (s *scene) scrollLines(now time.Time, delta int) {
	if s.reference != nil {
		s.referenceScrollLines(now, delta)
		return
	}
	if s.document.Len() == 0 {
		return
	}
	s.follow.BeginInteraction()
	anchor := s.anchorAt(now, s.clock.positionAt(now))
	s.manualLine = min(max(int(anchor)+delta, 0), s.document.Len()-1)
	s.selectedLine = s.manualLine
	s.scroll.Snap(now, float64(s.manualLine)*scrollUnit)
	s.motionReady = true
	s.follow.EndInteraction(max(0, now.Sub(s.origin)))
}

func (s *scene) hitLine(x, y float32) int {
	if x < 0 || x >= s.viewport.W || y < 0 || y >= s.viewport.H {
		return -1
	}
	y += s.viewport.Y
	for _, row := range s.rows[:s.rowCount] {
		if row.geometry != nil && y >= row.y && y < row.y+row.geometry.height {
			return row.index
		}
	}
	return -1
}

func (s *scene) requestSeek(now time.Time, index int) {
	if index >= 0 && index < s.document.Len() && s.onSeek != nil {
		line := s.document.Line(index)
		s.onSeek(now, seekRequest{LineID: line.ID(), Position: line.Start()})
	}
}

func (s *scene) input(ev ui.InputEvent) bool {
	if s.reference != nil {
		return s.referenceInput(ev)
	}
	if s.closed {
		return false
	}
	switch ev.Kind {
	case ui.InputScroll:
		s.follow.BeginInteraction()
		s.scrollPixels(ev.Time, float64(ev.DY))
		s.follow.EndInteraction(max(0, ev.Time.Sub(s.origin)))
		return true
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		s.pointerDown, s.dragged = true, false
		s.pressY, s.lastY = ev.Y, ev.Y
		anchor := s.anchorAt(ev.Time, s.clock.positionAt(ev.Time))
		s.scroll.Snap(ev.Time, anchor*scrollUnit)
		s.motionReady = true
		s.follow.BeginInteraction()
		return true
	case ui.InputPointerMove:
		if !s.pointerDown {
			return false
		}
		if math.Abs(float64(ev.Y-s.pressY)) >= 4 {
			s.dragged = true
		}
		if s.dragged {
			s.scrollPixels(ev.Time, float64(s.lastY-ev.Y))
		}
		s.lastY = ev.Y
		return true
	case ui.InputPointerUp:
		if ev.Button != 0 || !s.pointerDown {
			return false
		}
		s.pointerDown = false
		s.follow.EndInteraction(max(0, ev.Time.Sub(s.origin)))
		if !s.dragged && !ev.Canceled {
			s.prepareWindow(s.anchorAt(ev.Time, s.clock.positionAt(ev.Time)), s.viewport)
			s.selectedLine = s.hitLine(ev.X, ev.Y)
			s.requestSeek(ev.Time, s.selectedLine)
		}
		return true
	}
	return false
}

type repaintPlan struct {
	continuous bool
	after      time.Duration
}

func nextDelay(events []time.Duration, position time.Duration, rate float64) time.Duration {
	next := sort.Search(len(events), func(i int) bool { return events[i] > position })
	if next == len(events) {
		return 0
	}
	ns := math.Ceil(float64(events[next]-position) / rate)
	if ns >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return max(time.Millisecond, time.Duration(ns))
}

// repaintAt consults only visible cached geometry and sorted event times.
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
			if g.clusters != nil {
				for _, cluster := range g.clusters {
					if cluster.atomic || position < cluster.start || position >= cluster.end {
						continue
					}
					for _, rect := range cluster.rects {
						if rect.W > 0 && row.y+rect.Y+rect.H > box.Y && row.y+rect.Y < box.Y+box.H {
							return repaintPlan{continuous: true}
						}
					}
				}
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
	return repaintPlan{after: nextDelay(s.boundaries, position, s.clock.rate)}
}

func (s *scene) draw(p *ui.Painter, box ui.Rect) {
	if s.reference != nil {
		s.drawReference(p, box)
		return
	}
	if s.closed || box.W <= 0 || box.H <= 0 {
		return
	}
	s.scale, s.accelerated = p.Scale(), p.Accelerated()
	s.drawCalls++
	p.Fill(box, ui.RGB(18, 21, 31), 0)
	if s.showBackground {
		p.FillGradient(box, ui.LinearGradient{From: ui.RGB(34, 49, 67), To: ui.RGB(38, 24, 51), Angle: 150}, 0)
	}
	position := s.clock.positionAt(p.Now())
	state := s.visualAt(position)
	s.prepareWindow(s.anchorAt(p.Now(), position), box)
	smooth := s.accelerated && !s.forceStatic && !s.reduceMotion
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
			line := s.document.Line(row.index)
			x := box.X + 24
			if line.Duet() {
				x = max(x, box.X+box.W-24-g.width)
			}
			base := ui.RGB(128, 140, 159)
			if line.Background() {
				base = ui.RGB(107, 121, 145)
			}
			p.TextLayout(g.layout, x, row.y, base)
			_, highlighted := slices.BinarySearch(state.Highlighted, row.index)
			if highlighted {
				if smooth {
					s.highlights = g.appendHighlights(s.highlights[:0], position)
				} else {
					s.highlights = g.appendStaticHighlights(s.highlights[:0], position)
				}
				for _, clip := range s.highlights {
					clip.X += x
					clip.Y += row.y
					p.Clip(clip, 0, func() { p.TextLayout(g.layout, x, row.y, ui.RGB(242, 247, 255)) })
				}
			}
			for _, annotation := range g.annotations {
				color := ui.RGB(116, 134, 157)
				if highlighted {
					color = ui.RGB(182, 210, 226)
				}
				p.TextLayout(annotation.layout, x, row.y+annotation.y, color)
			}
		}
		if state.InInterlude {
			p.TextLayout(s.interlude, box.X+24, box.Y+box.H*0.25, ui.RGB(182, 210, 226))
		}
	})
	if s.duration > 0 {
		p.Fill(ui.Rect{X: box.X, Y: box.Y + box.H - 4, W: box.W * float32(float64(position)/float64(s.duration)), H: 4}, ui.RGB(116, 202, 236), 0)
	}
	plan := s.repaintAt(position, smooth, box)
	if plan.continuous || smooth && !s.scroll.Settled(p.Now()) {
		p.AnimationFrame()
	} else if plan.after > 0 {
		p.After(plan.after)
	}
}
