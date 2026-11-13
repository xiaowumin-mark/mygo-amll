package motion

import (
	"math"
	"os"
	"testing"
	"time"
)

func TestLineAnimationClockReferenceTransitions(t *testing.T) {
	now := time.Unix(100, 0)
	const lineStart = 5 * time.Second
	var clock LineAnimationClock
	clock.Sync(now, lineStart+time.Second, lineStart, lineStart+2*time.Second, false, true, 0)
	if clock.Position(now.Add(time.Second), FloatWordAnimation, lineStart+1300*time.Millisecond) != lineStart {
		t.Fatal("a never-enabled line advanced its newly created paused animations")
	}
	clock.Sync(now, lineStart+200*time.Millisecond, lineStart, lineStart+2*time.Second, true, true, 0)
	clock.Sync(now.Add(300*time.Millisecond), lineStart+500*time.Millisecond, lineStart, lineStart+2*time.Second, true, false, 0)
	if clock.Position(now.Add(time.Second), EmphasisGlowAnimation, lineStart+3*time.Second) != lineStart+500*time.Millisecond {
		t.Fatal("enabled-line pause did not freeze emphasis")
	}
	clock.Sync(now.Add(time.Second), lineStart+500*time.Millisecond, lineStart, lineStart+2*time.Second, true, true, 0)
	clock.Sync(now.Add(1200*time.Millisecond), lineStart+700*time.Millisecond, lineStart, lineStart+2*time.Second, false, true, 0)
	clock.Sync(now.Add(1300*time.Millisecond), lineStart+800*time.Millisecond, lineStart, lineStart+2*time.Second, false, false, 1)
	at := now.Add(1500 * time.Millisecond)
	if clock.Position(at, FloatWordAnimation, lineStart+1300*time.Millisecond) != lineStart+400*time.Millisecond ||
		clock.Position(at, MaskAnimation, lineStart+2*time.Second) != lineStart+700*time.Millisecond ||
		clock.Position(at, EmphasisFloatAnimation, lineStart+3*time.Second) != lineStart+time.Second {
		t.Fatal("disable/disabled pause/seek did not distinguish float, mask and actual emphasis IDs")
	}
	before := clock
	clock.Position(at, EmphasisGlowAnimation, lineStart+3*time.Second)
	clock.Running(at, EmphasisGlowAnimation, lineStart+3*time.Second)
	if clock != before {
		t.Fatal("animation position sampling mutated state")
	}
	if allocations := testing.AllocsPerRun(1000, func() {
		clock.Position(at, FloatWordAnimation, lineStart+1300*time.Millisecond)
		clock.Position(at, EmphasisGlowAnimation, lineStart+3*time.Second)
	}); allocations != 0 {
		t.Fatalf("wall clock samples allocated %g times", allocations)
	}
	// A paused past-end short float reverses from its own finish; long floats
	// reverse from their actual current position. Zero-time reverse auto-rewinds.
	clock.Sync(now.Add(2*time.Second), lineStart+1500*time.Millisecond, lineStart, lineStart+2*time.Second, true, false, 2)
	clock.Sync(now.Add(2100*time.Millisecond), lineStart+1500*time.Millisecond, lineStart, lineStart+2*time.Second, false, false, 2)
	if clock.Position(now.Add(2200*time.Millisecond), FloatWordAnimation, lineStart+1300*time.Millisecond) != lineStart+1200*time.Millisecond ||
		clock.Position(now.Add(2200*time.Millisecond), FloatWordAnimation, lineStart+2500*time.Millisecond) != lineStart+1400*time.Millisecond {
		t.Fatal("reverse start did not honor each effect's finish boundary")
	}
	clock.Sync(now.Add(3*time.Second), lineStart, lineStart, lineStart+2*time.Second, true, false, 3)
	clock.Sync(now.Add(3001*time.Millisecond), lineStart, lineStart, lineStart+2*time.Second, false, false, 3)
	if clock.Position(now.Add(3002*time.Millisecond), FloatWordAnimation, lineStart+1300*time.Millisecond) != lineStart+1299*time.Millisecond {
		t.Fatal("negative play at zero did not use Web Animations auto-rewind")
	}
}

type lineClockAction struct {
	Wall, Media      float64 // milliseconds, media relative to line start
	Enabled, Playing bool
	Revision         uint64
	Kind             string
}

func TestLineClockAgainstOriginalLineMethods(t *testing.T) {
	actions := []lineClockAction{
		{0, 500, false, true, 0, ""},
		{100, 200, true, true, 0, "enable"}, {300, 400, true, true, 0, ""},
		{400, 500, true, false, 0, "pause"}, {900, 500, true, false, 0, ""},
		{1000, 500, true, true, 0, "resume"}, {1100, 600, true, true, 0, ""},
		{1200, 700, false, true, 0, "disable"}, {1500, 1000, false, false, 0, "pause"},
		{1900, 1400, false, true, 0, "resume"}, {2000, 8000, false, true, 1, ""},
		{2100, 100, true, false, 1, "enable"}, {2300, 100, true, false, 1, ""},
		{2400, 1500, true, false, 2, "enable"}, {2500, 1500, false, false, 2, "disable"},
		{2600, 1500, false, false, 2, ""},
		{3000, 0, true, false, 3, "enable"}, {3001, 0, false, false, 3, "disable"},
		{3002, 0, false, false, 3, ""},
	}
	input := struct {
		Root    string
		Actions []lineClockAction
	}{os.Getenv("AMLL_REF_ROOT"), actions}
	var oracle [][]float64
	nodeReference(t, lineClockOracleScript, input, &oracle, false)
	const start = 5 * time.Second
	base := time.Unix(100, 0)
	var clock LineAnimationClock
	for i, action := range actions {
		now := base.Add(time.Duration(action.Wall * float64(time.Millisecond)))
		clock.Sync(now, start+time.Duration(action.Media*float64(time.Millisecond)), start, start+2*time.Second, action.Enabled, action.Playing, action.Revision)
		for j, spec := range [...]struct {
			kind AnimationKind
			end  time.Duration
		}{{MaskAnimation, 2 * time.Second}, {FloatWordAnimation, 1300 * time.Millisecond}, {FloatWordAnimation, 2500 * time.Millisecond}, {EmphasisGlowAnimation, 2400 * time.Millisecond}, {EmphasisFloatAnimation, 3360 * time.Millisecond}, {FloatOnlyAnimation, 2 * time.Second}} {
			position := float64(clock.Position(now, spec.kind, start+spec.end)-start) / float64(time.Millisecond)
			if math.Abs(position-oracle[i][j]) > 0.000001 {
				t.Fatalf("action %d (%s), animation %d: Go=%gms original line method=%gms", i, action.Kind, j, position, oracle[i][j])
			}
		}
	}
	t.Log("original TypeScript line enable/disable/pause/resume methods and WebMaskAnimator run against an injected deterministic WAAPI clock; reverse edge rules separately verified in Chromium")
}

// The transition methods below are extracted verbatim and stripped with Node's
// built-in TypeScript stripper, not reimplemented for the oracle. Only the DOM
// surface and the browser's time transport are mocked; browser edge rules have
// their own opt-in Chromium probe.
const lineClockOracleScript = `import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {stripTypeScriptTypes} from 'node:module';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const root=p=>pathToFileURL(path.join(input.Root,p)).href;
const {clampPositive}=await import(root('packages/core/src/utils/clamp.ts'));
const {LyricLineRenderMode}=await import(root('packages/core/src/lyric-player/base/consts.ts'));
const {WebMaskAnimator}=await import(root('packages/core/src/lyric-player/dom/animation/mask/animator-web.ts'));
const source=fs.readFileSync(path.join(input.Root,'packages/core/src/lyric-player/dom/lyric-line.ts'),'utf8');
const section=(begin,end)=>{const a=source.indexOf(begin),b=source.indexOf(end,a);if(a<0||b<a)throw Error('method not found');return source.slice(a,b);};
const methods=section('\tasync enable(', '\n\tdisable():')+section('\tdisable():','\n\tprivate lastWord')+section('\tasync resume():','\n\tasync pause():')+section('\tasync pause():','\n\tgetLine():');
const clean=stripTypeScriptTypes('class Oracle {\n'+methods+'\n}');
const Oracle=new Function('clampPositive','styles','LyricLineRenderMode','return ('+clean+')')(clampPositive,{active:'active'},LyricLineRenderMode);
let wall=0,media=5000,playing=true;
class AnimationClock {
 constructor(id,end){this.id=id;this.end=end;this.p=0;this.at=wall;this.rate=1;this.paused=true;this.effect={getComputedTiming:()=>({delay:0,duration:this.end})};}
 get currentTime(){if(this.paused)return this.p;const p=this.p+(wall-this.at)*this.rate;return this.rate>0?Math.min(this.end,p):Math.max(0,p);}
 set currentTime(p){this.p=p;this.at=wall;}
 get playbackRate(){return this.rate;}
 set playbackRate(rate){this.p=this.currentTime;this.at=wall;this.rate=rate;}
 get playState(){if(this.paused)return 'paused';return (this.rate>0&&this.currentTime>=this.end)||(this.rate<0&&this.currentTime<=0)?'finished':'running';}
 pause(){this.p=this.currentTime;this.at=wall;this.paused=true;}
 play(){this.p=this.currentTime;if(this.rate>=0&&this.p>=this.end)this.p=0;if(this.rate<0&&(this.p<=0||this.p>this.end))this.p=this.end;this.at=wall;this.paused=false;}
}
const mask=new AnimationClock('mask',2000),a=new AnimationClock('float-word',1300),b=new AnimationClock('float-word',2500),g=new AnimationClock('emphasize-word-X-0',2400),f=new AnimationClock('emphasize-word-float',3360),only=new AnimationClock('emphasize-word-float-only',2000);
const oracle=new Oracle();
oracle.isEnabled=false;oracle.lyricLine={startTime:5000};oracle.lyricPlayer={getCurrentTime:()=>media,getIsPlaying:()=>playing};
const classes={add(){},remove(){}};oracle.element={classList:classes,children:[{classList:classes}]};oracle.setRenderMode=()=>{};
oracle.splittedWords=[{elementAnimations:[a,b,g,f,only]}];
const maskAnimator=new WebMaskAnimator([],{lineStartTime:5000,lineEndTime:7000});maskAnimator.animations=[mask];oracle.maskAnimator=maskAnimator;
const output=[];
for(const action of input.Actions){wall=action.Wall;media=5000+action.Media;playing=action.Playing;
 if(action.Kind==='enable')await oracle.enable();
 if(action.Kind==='disable')oracle.disable();
 if(action.Kind==='pause')await oracle.pause();
 if(action.Kind==='resume')await oracle.resume();
 output.push([mask,a,b,g,f,only].map(a=>Math.min(a.end,Math.max(0,a.currentTime))));
}
process.stdout.write(JSON.stringify(output));`
