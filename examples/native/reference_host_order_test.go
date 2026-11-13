// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

type referenceHostInput struct {
	Wall, Media, Delta, Height, Wheel float64
	Seek                              bool
	Visibility                        string
}
type referenceHostCase struct {
	Name          string
	Initial       float64
	InitialHeight float64
	Cold          bool
	AutoSeek      bool
	Frames        []referenceHostInput
}
type referenceHostGroup struct{ Y, BGSlide, MainScale, Top, Delay float64 }
type referenceHostDots struct {
	Y, RenderY, Opacity, Scale float64
	Dots                       [3]float64
	Active                     bool
}
type referenceHostFrame struct {
	Focus     int
	Interlude bool
	Groups    []referenceHostGroup
	Dots      *referenceHostDots
	Params    motion.RefParams
}

func referenceHostCases() []referenceHostCase {
	var cases []referenceHostCase
	forward := referenceHostCase{Name: "focus-forward", Initial: 0}
	for wall := 0; wall <= 1152; wall += 16 {
		delta := 16.0
		if wall == 0 {
			delta = 0
		}
		forward.Frames = append(forward.Frames, referenceHostInput{Wall: float64(wall), Media: float64(wall), Delta: delta, Seek: wall == 0})
	}
	cases = append(cases, forward)
	cold := forward
	cold.Name, cold.Cold, cold.InitialHeight = "cold-host-shorter-than-component", true, 560
	cold.Frames = append([]referenceHostInput(nil), forward.Frames...)
	cold.Frames[0].Seek = false
	cases = append(cases, cold)
	cases = append(cases, referenceHostCase{Name: "fractional-media-rounding", Initial: 0, Frames: []referenceHostInput{
		{Wall: 0, Media: 999.49, Seek: true},
		{Wall: 16, Media: 999.5, Delta: 16},
		{Wall: 32, Media: 1000.49, Delta: 16},
		{Wall: 48, Media: 1000.5, Delta: 16},
	}})
	cases = append(cases, referenceHostCase{Name: "visibilitychange-end-resume-tick", Initial: 0, AutoSeek: true, Frames: []referenceHostInput{
		{Wall: 0, Media: 500, Seek: true},
		{Wall: 16, Media: 516, Delta: 16},
		{Wall: 32, Media: 532, Delta: 16, Visibility: "hidden"},
		{Wall: 10432, Media: 11000, Delta: 100, Visibility: "visible"},
	}})
	cases = append(cases, referenceHostCase{Name: "normal-tick-skips-playing-intervals", Initial: 0, Frames: []referenceHostInput{
		{Wall: 0, Media: 500, Seek: true},
		{Wall: 16, Media: 2500, Delta: 16},
		{Wall: 32, Media: 10050, Delta: 16},
		{Wall: 48, Media: 11000, Delta: 16},
	}})
	seek := referenceHostCase{Name: "seeks", Initial: 500}
	media := 500.0
	for wall := 0; wall <= 576; wall += 16 {
		delta := 16.0
		if wall == 0 {
			delta = 0
		}
		jump := wall == 0 || wall == 192 || wall == 384
		if wall == 192 {
			media = 1200
		} else if wall == 384 {
			media = 200
		} else {
			media += delta
		}
		seek.Frames = append(seek.Frames, referenceHostInput{Wall: float64(wall), Media: media, Delta: delta, Seek: jump})
	}
	cases = append(cases, seek)
	dots := referenceHostCase{Name: "interlude-resize-wheel", Initial: 2000}
	for wall := 0; wall <= 864; wall += 16 {
		delta := 16.0
		if wall == 0 {
			delta = 0
		}
		frame := referenceHostInput{Wall: float64(wall), Media: 2000 + float64(wall), Delta: delta, Seek: wall == 0}
		if wall == 640 {
			frame.Height = 560
		}
		if wall == 704 {
			frame.Wheel = 40
		}
		dots.Frames = append(dots.Frames, frame)
	}
	return append(cases, dots)
}

func runReferenceHostOracle(t *testing.T, source string, cases any) []byte {
	t.Helper()
	root, bezier := os.Getenv("AMLL_REF_ROOT"), os.Getenv("AMLL_BEZIER_SOURCE")
	if root == "" || bezier == "" {
		t.Skip("set AMLL_REF_ROOT and AMLL_BEZIER_SOURCE for original BasePlayer/DOM host-order oracle")
	}
	if head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").CombinedOutput(); err != nil || strings.TrimSpace(string(head)) != "f32285864056587c4afa59d310208b0aa829cb53" {
		t.Fatalf("host oracle requires fixed reference: %s %v", head, err)
	}
	if output, err := exec.Command("git", "-C", root, "diff", "--quiet", "HEAD", "--").CombinedOutput(); err != nil {
		t.Fatalf("host oracle requires unmodified tracked reference files: %s %v", output, err)
	}
	bezierSource, err := os.ReadFile(bezier)
	if err != nil {
		t.Fatal(err)
	}
	const bezierSHA256 = "9d8228a3583b1afb96fc2d037d0bccc01afa4e247bab992899513fb85c922f01"
	if got := fmt.Sprintf("%x", sha256.Sum256(bezierSource)); got != bezierSHA256 {
		t.Fatalf("host oracle requires fixed bezier source: SHA-256 %s, want %s", got, bezierSHA256)
	}
	temp := t.TempDir()
	script := filepath.Join(temp, "host.mjs")
	loader := filepath.Join(temp, "loader.mjs")
	if err := os.WriteFile(script, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loader, []byte(referenceHostLoader), 0600); err != nil {
		t.Fatal(err)
	}
	loaderPath := filepath.ToSlash(loader)
	if !strings.HasPrefix(loaderPath, "/") {
		loaderPath = "/" + loaderPath
	}
	input, _ := json.Marshal(struct {
		Root  string
		Cases any
	}{root, cases})
	command := exec.Command("node", "--experimental-transform-types", "--experimental-loader", (&url.URL{Scheme: "file", Path: loaderPath}).String(), script)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("original player oracle: %v\n%s", err, stderr.String())
	}
	return output
}

func TestReferenceHostOrderAgainstOriginalPlayer(t *testing.T) {
	cases := referenceHostCases()
	output := runReferenceHostOracle(t, referenceHostOracle, cases)
	var oracle [][]referenceHostFrame
	if err := json.Unmarshal(output, &oracle); err != nil {
		t.Fatalf("host JSON: %v: %s", err, output)
	}
	maximum := 0.0
	for caseIndex, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			lines := make([]lyric.Line, 3)
			for i, start := range []time.Duration{0, time.Second, 10 * time.Second} {
				lines[i] = lyric.Line{Text: "AA BB", Start: start, End: start + time.Second, Duet: i == 2, Words: []lyric.Word{{Text: "AA ", Start: start, End: start + 500*time.Millisecond}, {Text: "BB", Start: start + 500*time.Millisecond, End: start + time.Second}}}
			}
			s := newScene(geometryDocument(t, lines), time.Unix(0, 0))
			defer s.Close()
			options := defaultReferenceOptions()
			advance := false
			options.Presentation.Optimize.TryAdvanceStartTime = &advance
			if err := s.enableAMLLWithOptions(false, options, s.origin); err != nil {
				t.Fatal(err)
			}
			s.setReferenceHostSize(800, 500)
			s.clock.setPlaying(s.origin, true)
			box := ui.Rect{W: 800, H: 500}
			if test.InitialHeight > 0 {
				box.H = float32(test.InitialHeight)
			}
			// Settle only the numerical baseline. Original DOM mask/shaping are not
			// replaced by this oracle: both sides use the same measured 60-DIP rows.
			for k := 0; !test.Cold && k <= 120; k++ {
				now := s.origin.Add(time.Duration(k) * 16 * time.Millisecond)
				s.clock.seek(now, time.Duration(test.Initial)*time.Millisecond)
				s.updateReference(now, false)
				s.prepareReferenceFrame(now, box, 1, true)
			}
			base := s.origin.Add(120 * 16 * time.Millisecond)
			if test.Cold {
				base = s.origin
			}
			for j, frame := range test.Frames {
				now := base.Add(time.Duration(frame.Wall) * time.Millisecond)
				if frame.Visibility != "" {
					s.hidden = frame.Visibility != "visible"
				}
				if frame.Height > 0 {
					box.H = float32(frame.Height)
				}
				if frame.Wheel != 0 {
					s.input(ui.InputEvent{Kind: ui.InputScroll, Time: now, DY: float32(frame.Wheel)})
				}
				if frame.Seek {
					s.seek(now, time.Duration(frame.Media*float64(time.Millisecond)))
				} else {
					s.clock.seek(now, time.Duration(frame.Media*float64(time.Millisecond)))
					s.updateReference(now, false)
				}
				s.prepareReferenceFrame(now, box, 1, true)
				r := s.reference
				expected := oracle[caseIndex][j]
				if r.state.Focus != expected.Focus || r.state.InInterlude != expected.Interlude {
					t.Fatalf("frame%d timeline Go=%+v original=%+v", j, r.state, expected)
				}
				if r.lastParams != expected.Params {
					t.Fatalf("frame%d spring policy Native %+v original %+v", j, r.lastParams, expected.Params)
				}
				for i, want := range expected.Groups {
					r.replay(i, r.lastStep)
					g := &r.groups[i]
					values := [][2]float64{{g.y.Sample(now).Position, want.Y}, {g.bg.Sample(now).Position, want.BGSlide}, {g.mainScale.Sample(now).Position, want.MainScale}}
					for field, pair := range values {
						difference := math.Abs(pair[0] - pair[1])
						maximum = max(maximum, difference)
						if difference > 1e-6 {
							t.Logf("native top %.12g height %.12g params %+v original top %.12g delta %g", r.history[(r.lastStep-1)%uint64(len(r.history))].base+g.prefix, g.height, r.lastParams, want.Top, frame.Delta)
							t.Errorf("frame%d wall%g group%d field%d native %.12g original %.12g diff %.9g", j, frame.Wall, i, field, pair[0], pair[1], difference)
							return
						}
					}
				}
				if expected.Dots != nil && expected.Dots.Active {
					pose := r.interludePose
					values := [][2]float64{{pose.RawY, expected.Dots.RenderY}, {pose.Raw.Opacity, expected.Dots.Opacity}, {pose.Raw.Scale, expected.Dots.Scale}}
					for k := range 3 {
						values = append(values, [2]float64{pose.Raw.DotOpacities[k], expected.Dots.Dots[k]})
					}
					for field, pair := range values {
						difference := math.Abs(pair[0] - pair[1])
						maximum = max(maximum, difference)
						if difference > 1e-6 {
							t.Errorf("frame%d wall%g dots field%d native %.12g original %.12g diff %.9g", j, frame.Wall, field, pair[0], pair[1], difference)
							return
						}
					}
				}
			}
		})
	}
	t.Logf("original BasePlayer + DOM numeric methods, setCurrentTime→update: max difference %.12g", maximum)
}

func TestReferenceFadeWidthReanchorsOriginalWordEffects(t *testing.T) {
	prefix, _, ok := strings.Cut(referenceHostOracle, "const lines=")
	if !ok {
		t.Fatal("host oracle setup missing")
	}
	output := runReferenceHostOracle(t, prefix+referenceFadeWidthOracle, nil)
	var oracle []float64
	if err := json.Unmarshal(output, &oracle); err != nil {
		t.Fatal(err)
	}
	base := time.Unix(0, 0)
	metadata := referenceGroupMetadata{index: 0, group: presentationGroup{start: 0, end: 2 * time.Second, backgroundLayout: -1}, mainEnd: 2 * time.Second}
	g := referenceGroupMotion{height: 60}
	var clock motion.LineAnimationClock
	step := referenceStep{now: base, media: 500 * time.Millisecond, playing: true, highlighting: true, layout: true, snap: true, height: 500, width: 800}
	referenceApplyStep(&g, &clock, nil, metadata, step)
	positions := []float64{float64(clock.Position(base.Add(300*time.Millisecond), motion.EmphasisGlowAnimation, 5*time.Second)) / float64(time.Millisecond)}
	step.now = base.Add(300 * time.Millisecond)
	step.layout, step.snap = false, false
	step.fadeRevision = 1
	referenceApplyStep(&g, &clock, nil, metadata, step)
	positions = append(positions, float64(clock.Position(step.now, motion.EmphasisGlowAnimation, 5*time.Second))/float64(time.Millisecond))
	step.now = base.Add(500 * time.Millisecond)
	referenceApplyStep(&g, &clock, nil, metadata, step)
	positions = append(positions, float64(clock.Position(step.now, motion.EmphasisGlowAnimation, 5*time.Second))/float64(time.Millisecond))
	if len(positions) != len(oracle) {
		t.Fatalf("positions %v oracle %v", positions, oracle)
	}
	for i, want := range oracle {
		if math.Abs(positions[i]-want) > 1e-6 {
			t.Fatalf("sample%d Native %gms original %gms", i, positions[i], want)
		}
	}
}

// The real Dom setter, mask signature/rebuild, line.enable and WebMaskAnimator
// execute unmodified. Only deterministic WAAPI time and DOM measurement are
// injected; the glow animation is an existing independent element animation.
const referenceFadeWidthOracle = `
class AnimationClock{constructor(id,end){this.id=id;this.end=end;this.p=0;this.at=wall;this.rate=1;this.paused=true;this.effect={getComputedTiming:()=>({delay:0,duration:end})}}get currentTime(){return this.paused?this.p:Math.min(this.end,Math.max(0,this.p+(wall-this.at)*this.rate))}set currentTime(p){this.p=p;this.at=wall}get playbackRate(){return this.rate}set playbackRate(r){this.p=this.currentTime;this.at=wall;this.rate=r}get playState(){return this.paused?'paused':this.currentTime>=this.end?'finished':'running'}pause(){this.p=this.currentTime;this.at=wall;this.paused=true}play(){this.p=this.currentTime;this.at=wall;this.paused=false}cancel(){this.pause()}}
const glow=new AnimationClock('emphasize-word-X-0',5000),el=new Element();el.isConnected=true;const main=new Element();el.children=[main];const wordEl=new Element();wordEl.clientWidth=80;wordEl.clientHeight=36;wordEl.animate=(_,o)=>new AnimationClock(o.id,o.duration);
const player={wordFadeWidth:.5,currentLyricGroups:[],supportMaskImage:true,_getIsNonDynamic:()=>false,getWordFadeWidth:LyricPlayerBase.prototype.getWordFadeWidth,getCurrentTime:()=>500,getIsPlaying:()=>true};
const l=Object.create(LyricLineEl.prototype);Object.assign(l,{lyricPlayer:player,lyricLine:{startTime:0,endTime:2000},element:el,splittedWords:[{word:'AA',startTime:0,endTime:2000,mainElement:wordEl,elementAnimations:[glow],width:80,height:36,padding:0}],isEnabled:false,brightness:{captureGeometry(){},sync(){}}});player.currentLyricGroups=[{mainLine:l}];
await l.enable();wall=300;const out=[glow.currentTime];DomLyricPlayer.prototype.setWordFadeWidth.call(player,.8);out.push(glow.currentTime);wall=500;out.push(glow.currentTime);process.stdout.write(JSON.stringify(out));`

func TestReferencePostpositionRetainsOriginalWrapperClass(t *testing.T) {
	prefix, _, ok := strings.Cut(referenceHostOracle, "const lines=")
	if !ok {
		t.Fatal("host oracle setup missing")
	}
	output := runReferenceHostOracle(t, prefix+referencePostpositionOracle, nil)
	var oracle struct {
		TopBefore, TopAfter, BGFirst           bool
		MarginBefore, MarginAtSet, MarginAfter string
		Rebuilds                               int
	}
	if err := json.Unmarshal(output, &oracle); err != nil {
		t.Fatal(err)
	}
	if !oracle.TopBefore || !oracle.TopAfter || !oracle.BGFirst || oracle.MarginBefore != "-20.0px" || oracle.MarginAtSet != "-20.0px" || oracle.MarginAfter != "" || oracle.Rebuilds != 2 {
		t.Fatalf("original dynamic postposition lifecycle changed: %+v", oracle)
	}
	s := referenceTestScene(t, 2)
	now := s.origin
	s.prepareReferenceFrame(now, ui.Rect{W: 800, H: 500}, 1, true)
	r := s.reference
	r.presentation.groups[0].backgroundLayout = 1
	r.presentation.groups[0].backgroundFirst = true
	row := referenceRow{index: 0, main: &lineGeometry{font: ui.Font{Size: 30}, height: 80, width: 728}, background: &lineGeometry{font: ui.Font{Size: 21}, height: 40, width: 731.6}}
	g := referenceGroupMotion{connected: true, height: 104, bgTop: true, bgTopConfigured: true,
		y: motion.NewRefSpring(now, 150, motion.RefDefaultParams()), bg: motion.NewRefSpring(now, 40, motion.RefDefaultParams()),
		mainScale: motion.NewRefSpring(now, 97, motion.RefMainScaleParams()), bgScale: motion.NewRefSpring(now, 75, motion.RefBGScaleParams())}
	step := referenceStep{now: now, playing: true, config: r.frameOptions()}
	step.config.widthPadding = 30
	metadata := r.metadata[0]
	metadata.group = r.presentation.groups[0]
	var mainClock, bgClock motion.LineAnimationClock
	referenceApplyStep(&g, &mainClock, &bgClock, metadata, step)
	s.positionReferenceRow(&row, &g, step, false)
	if row.height != 133 {
		t.Fatalf("initial top flow height=%g", row.height)
	}
	step.config.postposition = true
	step.layout, step.base = true, 100
	referenceApplyStep(&g, &mainClock, &bgClock, metadata, step)
	s.positionReferenceRow(&row, &g, step, false)
	if row.height != 133 {
		t.Fatal("same-pose setter cleared the source's cached margin before slide rendering")
	}
	step.now, step.delta, step.layout = now.Add(16*time.Millisecond), 16*time.Millisecond, false
	referenceApplyStep(&g, &mainClock, &bgClock, metadata, step)
	s.positionReferenceRow(&row, &g, step, false)
	if row.height != 153 || row.layers[1].Y >= row.layers[0].Y {
		t.Fatalf("dynamic setting incorrectly rebuilt/moved the source's existing top BG wrapper: height=%g, mainY=%g bgY=%g", row.height, row.layers[0].Y, row.layers[1].Y)
	}
}

func TestReferenceSizeChangeReanchorsOriginalWordEffects(t *testing.T) {
	prefix, _, ok := strings.Cut(referenceHostOracle, "const lines=")
	if !ok {
		t.Fatal("host setup missing")
	}
	setup, _, ok := strings.Cut(referenceFadeWidthOracle, "await l.enable();")
	if !ok {
		t.Fatal("line effect setup missing")
	}
	output := runReferenceHostOracle(t, prefix+setup+`
await l.enable();l.updateMaskImageSync();l.onLineSizeChange([80,36]);wall=300;const out=[glow.currentTime];wordEl.clientWidth=100;l.onLineSizeChange([100,36]);out.push(glow.currentTime);wall=500;l.onLineSizeChange([100,37]);out.push(glow.currentTime);process.stdout.write(JSON.stringify(out));`, nil)
	var oracle []float64
	if err := json.Unmarshal(output, &oracle); err != nil {
		t.Fatal(err)
	}
	base := time.Unix(0, 0)
	metadata := referenceGroupMetadata{index: 0, group: presentationGroup{start: 0, end: 2 * time.Second, backgroundLayout: -1}, mainEnd: 2 * time.Second}
	g := referenceGroupMotion{height: 60}
	var clock motion.LineAnimationClock
	step := referenceStep{now: base, media: 500 * time.Millisecond, playing: true, highlighting: true, layout: true, snap: true, height: 500, width: 800}
	referenceApplyStep(&g, &clock, nil, metadata, step)
	got := []float64{float64(clock.Position(base.Add(300*time.Millisecond), motion.EmphasisGlowAnimation, 5*time.Second)) / float64(time.Millisecond)}
	step.now, step.layout, step.snap = base.Add(300*time.Millisecond), false, false
	step.changeCount = 1
	step.changes[0] = referenceHeightChange{index: 0, mainMaskReset: true}
	referenceApplyStep(&g, &clock, nil, metadata, step)
	got = append(got, float64(clock.Position(step.now, motion.EmphasisGlowAnimation, 5*time.Second))/float64(time.Millisecond))
	step.now, step.changeCount = base.Add(500*time.Millisecond), 0
	referenceApplyStep(&g, &clock, nil, metadata, step)
	got = append(got, float64(clock.Position(step.now, motion.EmphasisGlowAnimation, 5*time.Second))/float64(time.Millisecond))
	if len(got) != len(oracle) {
		t.Fatalf("positions %v oracle %v", got, oracle)
	}
	for i, want := range oracle {
		if math.Abs(got[i]-want) > 1e-6 {
			t.Fatalf("sample%d Native %gms original onLineSizeChange %gms", i, got[i], want)
		}
	}
}

const referencePostpositionOracle = `
function classes(el){const values=new Set();el.classList={add(...v){v.forEach(x=>values.add(x))},remove(...v){v.forEach(x=>values.delete(x))},contains(v){return values.has(v)},toggle(v,on){if(on===undefined)on=!values.has(v);on?values.add(v):values.delete(v);return on}};return el}
document.createElement=()=>{const e=classes(new Element());e.clientHeight=40;return e};Element.prototype.insertBefore=function(el,before){const i=this.children.indexOf(before);this.children.splice(i<0?this.children.length:i,0,el);return el};
const {LyricLineGroup}=await load('lyric-player/dom/lyric-group.ts');let rebuilt=0;
const mk=(start)=>({el:classes(new Element()),getElement(){return this.el},getLine(){return{words:[{startTime:start}],isDuet:false}},rebuildElement(){rebuilt++},setTransform(){},update(){}});
const main=mk(1000),bg=mk(0),player={alwaysPostpositionBackground:false,resizeObserver:new ResizeObserver(),lyricGroupElementMap:new WeakMap(),getAlwaysPostpositionBackground:LyricPlayerBase.prototype.getAlwaysPostpositionBackground,getEnableSpring:()=>true,getEnableScale:()=>true,getIsPlaying:()=>true,rebuildLyricLines:LyricPlayerBase.prototype.rebuildLyricLines,calcLayout(){}};
const g=new LyricLineGroup(player,main);g.addBgLine(bg);player.currentLyricGroups=[g];g.bgSlideY.setPosition(40);g.renderStyles();const before=g.bgWrapper.classList.contains('bgWrapperTop'),margin=g.bgWrapper.style.marginTop;
LyricPlayerBase.prototype.setAlwaysPostpositionBackground.call(player,true);g.setTransform(100,false,Duration.ZERO,false,1,0,true);g.renderStyles();const atSet=g.bgWrapper.style.marginTop;g.update(Duration.fromMillis(16));g.renderStyles();process.stdout.write(JSON.stringify({TopBefore:before,TopAfter:g.bgWrapper.classList.contains('bgWrapperTop'),BGFirst:g.element.children[0]===g.bgWrapper,MarginBefore:margin,MarginAtSet:atSet,MarginAfter:g.bgWrapper.style.marginTop,Rebuilds:rebuilt}));`

const referenceHostLoader = `import fs from 'node:fs';import {pathToFileURL,fileURLToPath} from 'node:url';
export async function resolve(s,c,n){if(s.endsWith('.css'))return{url:'oracle:css',shortCircuit:true};if(s==='@ungap/structured-clone')return{url:'oracle:clone',shortCircuit:true};if(s==='bezier-easing')return{url:pathToFileURL(process.env.AMLL_BEZIER_SOURCE).href,shortCircuit:true};try{return await n(s,c)}catch(e){if(s.startsWith('.')){for(const suffix of ['.ts','/index.ts']){const u=new URL(s+suffix,c.parentURL);if(fs.existsSync(fileURLToPath(u)))return{url:u.href,shortCircuit:true}}}throw e}}
export async function load(u,c,n){if(u==='oracle:clone')return{format:'module',source:'export default globalThis.structuredClone',shortCircuit:true};if(u==='oracle:css')return{format:'module',source:'export default new Proxy({},{get:(_,k)=>String(k)})',shortCircuit:true};const r=await n(u,c);if(u.endsWith('.ts'))return{...r,source:String(r.source).replaceAll('import.meta.env','({DEV:false,PROD:true})')};return r}`

const referenceHostOracle = `import fs from 'node:fs';import path from 'node:path';import {pathToFileURL} from 'node:url';
const input=JSON.parse(fs.readFileSync(0,'utf8'));let wall=0;
class Element extends EventTarget{constructor(){super();this.innerHTML='';this.children=[];this.style=new Proxy({setProperty(){},removeProperty(){},getPropertyValue(){return ''}},{get:(o,k)=>o[k]??''});this.classList={add(){},remove(){},toggle(){},contains(){return false}};}appendChild(e){this.children.push(e);return e}remove(){}setAttribute(){}getBoundingClientRect(){return{width:800,height:500,x:0,y:0}}getContext(){return{measureText:s=>({width:s.length*18})}}}
globalThis.HTMLElement=globalThis.Element=globalThis.HTMLDivElement=Element;globalThis.MouseEvent=class extends Event{};
globalThis.document=Object.assign(new EventTarget(),{createElement:()=>new Element(),visibilityState:'visible',documentElement:new Element(),fonts:{ready:Promise.resolve()}});
globalThis.window=Object.assign(new EventTarget(),{innerHeight:500,innerWidth:800,setTimeout,clearTimeout,requestAnimationFrame:()=>0,cancelAnimationFrame(){},matchMedia:()=>({matches:false})});
Object.defineProperty(globalThis,'navigator',{configurable:true,value:{userAgent:'Node original numeric host oracle'}});Object.defineProperty(globalThis,'performance',{configurable:true,value:{now:()=>wall}});window.performance=performance;
globalThis.requestAnimationFrame=()=>0;globalThis.cancelAnimationFrame=()=>{};globalThis.CSS={supports:()=>true};globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}};globalThis.MutationObserver=class{observe(){}disconnect(){}};globalThis.WheelEvent={DOM_DELTA_PIXEL:0};globalThis.getComputedStyle=()=>({fontSize:'30px',lineHeight:'36px',paddingLeft:'0px',getPropertyValue:()=>''});
const load=s=>import(pathToFileURL(path.join(input.Root,'packages/core/src',s)).href);
const {LyricPlayerBase}=await load('lyric-player/base/index.ts');const {DomLyricPlayer}=await load('lyric-player/dom/index.ts');const {LyricLineEl}=await load('lyric-player/dom/lyric-line.ts');const {LyricLineGroupBase}=await load('lyric-player/base/group.ts');const {InterludeDotsBase}=await load('lyric-player/base/interlude-dots.ts');const {Spring}=await load('utils/spring.ts');const {Duration}=await load('utils/time.ts');
function line(player,data){const l=Object.create(LyricLineEl.prototype);Object.assign(l,{lyricPlayer:player,lyricLine:data,lineTransforms:{scale:new Spring(100)},renderMode:0,element:new Element(),brightness:{isReady:true,setInWindow(){},markHighlightExited(){},playActivationTransition(){}},top:0,scale:1,opacity:1,blur:0,delay:0,isEnabled:false,enable(){this.isEnabled=true},disable(){this.isEnabled=false},resume(){},pause(){},onLineSizeChange(){},commitChanges(){},dispose(){}});return l}
class Group extends LyricLineGroupBase{constructor(player,l){super(l);this.lyricPlayer=player;this.el=new Element();this.posY.setPosition(window.innerHeight*2)}getElement(){return this.el}renderStyles(){}isInRenderRange(extra=true){const i=this.lyricPlayer.currentLyricGroups.indexOf(this),h=this.lyricPlayer.getLineHeight(i),y=this.posY.getCurrentPosition(),over=extra?this.lyricPlayer.getOverscanPx():0;return y<this.lyricPlayer.size[1]+over&&y+h>-over}}
class Dots extends InterludeDotsBase{getElement(){return this.el??=new Element()}render(s,x,y){this.raw={Y:this.posY.getCurrentPosition(),RenderY:y,Opacity:s.opacity,Scale:s.scale,Dots:[...s.dotOpacities],Active:s.isActive}}}
class Host extends LyricPlayerBase{constructor(){super();this.size=[800,500];this.supportMaskImage=true;this.isPageVisible=true;this.layoutState.interludeDotsSize=[97.8,39]}get baseFontSize(){return 30}createInterludeDots(){return new Dots()}createBottomLine(){return{el:new Element(),lineSize:[0,0],lineTransforms:{posY:new Spring(0)},getElement(){return this.el},setTransform(){},setFocused(){},resetPosition(){},update(){},dispose(){}}}buildLyricGroups(){for(const data of this.processedLines){const l=line(this,data),g=new Group(this,l);this.lyricLinesIndexes.set(l,this.currentLyricGroups.length);this.currentLyricGroups.push(g);this.lyricGroupElementMap.set(g.el,g)}}rebuildLyricView(t){return DomLyricPlayer.prototype.rebuildLyricView.call(this,t)}update(d){return DomLyricPlayer.prototype.update.call(this,d)}resume(){return DomLyricPlayer.prototype.resume.call(this)}pause(){return DomLyricPlayer.prototype.pause.call(this)}}
const lines=[0,1000,10000].map((s,i)=>({words:[{word:'AA ',startTime:s,endTime:s+500},{word:'BB',startTime:s+500,endTime:s+1000}],startTime:s,endTime:s+1000,isBG:false,isDuet:i===2,translatedLyric:'',romanLyric:''}));
function trace(p){const s=p.timelineController.getSnapshot();return{Focus:s.scrollToIndex,Interlude:s.isFocusOnInterlude,Params:p.posYSpringParams,Groups:p.currentLyricGroups.map(g=>({Y:g.posY.getCurrentPosition(),BGSlide:g.bgSlideY.getCurrentPosition(),MainScale:g.mainLine.lineTransforms.scale.getCurrentPosition(),Top:g.top,Delay:Duration.asMillis(g.delay)})),Dots:p.interludeDots.raw??null}}
const results=input.Cases.map(test=>{wall=0;const p=new Host();p.setEnableAutoSeekDetection(test.AutoSeek);p.setOptimizeOptions({tryAdvanceStartTime:false});if(test.InitialHeight)p.size[1]=test.InitialHeight;if(!test.Cold)p.resume();p.setLyricLines(lines,test.Initial);if(test.Cold)p.resume();for(let i=0;i<3;i++)p.layoutCalculator.setLineHeight(i,60);p.calcLayout('config-change');if(!test.Cold)for(let i=0;i<120;i++){wall+=16;p.update(16)}return test.Frames.map(f=>{wall=(test.Cold?0:1920)+f.Wall;if(f.Visibility){document.visibilityState=f.Visibility;document.dispatchEvent(new Event('visibilitychange'))}p.setCurrentTime(f.Media,f.Seek);if(f.Height){p.size[1]=f.Height;p.calcLayout('resize')}if(f.Wheel)p.scrollEngine.onWheel({deltaY:f.Wheel,deltaMode:0,cancelable:true,preventDefault(){}});p.update(f.Delta);return trace(p)})});process.stdout.write(JSON.stringify(results));`
