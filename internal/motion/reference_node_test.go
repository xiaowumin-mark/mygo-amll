package motion

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
)

const referenceCommit = "f32285864056587c4afa59d310208b0aa829cb53"

func nodeReference(t *testing.T, script string, input any, output any, bezier bool) {
	t.Helper()
	root := os.Getenv("AMLL_REF_ROOT")
	if root == "" {
		t.Skip("set AMLL_REF_ROOT to the ignored fixed reference checkout for the Node source oracle")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("explicit reference oracle needs Node 22.19+ with TypeScript stripping")
	}
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(head)) != referenceCommit {
		t.Fatalf("reference oracle requires commit %s: %s, %v", referenceCommit, head, err)
	}
	if diff, err := exec.Command("git", "-C", root, "diff", "--exit-code", "--", "packages/core/src/utils", "packages/core/src/lyric-player/base", "packages/core/src/lyric-player/dom/animation").CombinedOutput(); err != nil {
		t.Fatalf("reference source has uncommitted changes: %s, %v", diff, err)
	}
	temp := t.TempDir()
	file := filepath.Join(temp, "oracle.mjs")
	if err := os.WriteFile(file, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--experimental-transform-types"}
	if bezier {
		path := os.Getenv("AMLL_BEZIER_SOURCE")
		if path == "" {
			t.Skip("word oracle also needs AMLL_BEZIER_SOURCE pointing to bezier-easing 3.0.1 src/index.js as an .mjs file")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if sum := fmt.Sprintf("%x", sha256.Sum256(data)); sum != "9d8228a3583b1afb96fc2d037d0bccc01afa4e247bab992899513fb85c922f01" {
			t.Fatalf("bezier oracle requires the reviewed 3.0.1 source SHA-256, got %s", sum)
		}
		loader := filepath.Join(temp, "loader.mjs")
		if err := os.WriteFile(loader, []byte(`import {pathToFileURL} from 'node:url';
export function resolve(specifier, context, nextResolve) {
 if (specifier === 'bezier-easing') return {url:pathToFileURL(process.env.AMLL_BEZIER_SOURCE).href,shortCircuit:true};
 return nextResolve(specifier,context);
}`), 0600); err != nil {
			t.Fatal(err)
		}
		loaderURLPath := filepath.ToSlash(loader)
		if !strings.HasPrefix(loaderURLPath, "/") {
			loaderURLPath = "/" + loaderURLPath
		}
		args = append(args, "--experimental-loader", (&url.URL{Scheme: "file", Path: loaderURLPath}).String())
	}
	args = append(args, file)
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", args...)
	command.Stdin = bytes.NewReader(encoded)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil {
		t.Fatalf("reference Node execution: %v\n%s", err, stderr.String())
	}
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatalf("reference JSON: %v\n%s", err, data)
	}
}

type referenceAction struct {
	Time, Position, Delay float64
	Kind                  string
	Params                RefParams
}

type referenceSpringCase struct {
	Params   RefParams
	Position float64
	Actions  []referenceAction
}

func TestRefSpringAgainstFixedTypeScript(t *testing.T) {
	base := time.Unix(100, 0)
	var cases []referenceSpringCase
	for _, params := range []RefParams{RefDefaultParams(), RefYParams(true, false, false, 0, false), RefYParams(false, false, false, 100*time.Millisecond, true), RefYParams(false, false, false, 800*time.Millisecond, true), RefMainScaleParams(), RefBGScaleParams(), {Mass: 2, Damping: 1, Stiffness: 50, Soft: true}} {
		actions := []referenceAction{{Kind: "target", Position: 100}}
		for _, ms := range []float64{16, 32, 50, 100, 116, 200, 400, 600, 1000, 1500, 2000, 5000} {
			actions = append(actions, referenceAction{Time: ms})
		}
		cases = append(cases, referenceSpringCase{Params: params, Actions: actions})
	}
	cases = append(cases,
		referenceSpringCase{Params: RefDefaultParams(), Actions: []referenceAction{
			{Kind: "target", Position: 100, Delay: 50}, {Time: 16}, {Time: 49}, {Time: 50}, {Time: 66},
			{Time: 100, Kind: "target", Position: -50}, {Time: 116},
			{Time: 150, Kind: "params", Params: RefMainScaleParams()}, {Time: 166},
			{Time: 166, Kind: "target", Position: 200, Delay: 40}, {Time: 200}, {Time: 230}, {Time: 250},
		}},
		referenceSpringCase{Params: RefDefaultParams(), Actions: []referenceAction{
			{Kind: "target", Position: 100, Delay: 100}, {Time: 20, Kind: "position", Position: 30},
			{Time: 100}, {Time: 116}, {Time: 200, Kind: "target", Position: 500},
			{Time: 200, Kind: "target", Position: -500}, {Time: 216}, {Time: 500},
		}},
		referenceSpringCase{Params: RefDefaultParams(), Actions: []referenceAction{
			{Kind: "target", Position: 100}, {Time: 30, Kind: "params", Params: RefBGScaleParams(), Delay: 50},
			{Time: 60}, {Time: 90}, {Time: 106}, {Time: 122}, {Time: 200},
		}},
	)
	input := struct {
		Root  string
		Cases []referenceSpringCase
	}{os.Getenv("AMLL_REF_ROOT"), cases}
	var oracle [][]RefSample
	nodeReference(t, springOracleScript, input, &oracle, false)
	var maximum [3]float64
	for i, test := range cases {
		spring := NewRefSpring(base, test.Position, test.Params)
		for j, action := range test.Actions {
			now := base.Add(time.Duration(math.Round(action.Time * float64(time.Millisecond))))
			spring.Advance(now)
			switch action.Kind {
			case "target":
				spring.SetTarget(now, action.Position, time.Duration(action.Delay*float64(time.Millisecond)))
			case "position":
				spring.SetPosition(now, action.Position)
			case "params":
				spring.SetParams(now, action.Params, time.Duration(action.Delay*float64(time.Millisecond)))
			}
			actual, expected := spring.Sample(now), oracle[i][j]
			differences := [3]float64{math.Abs(actual.Position - expected.Position), math.Abs(actual.Velocity - expected.Velocity), math.Abs(actual.Acceleration - expected.Acceleration)}
			for k := range maximum {
				maximum[k] = max(maximum[k], differences[k])
			}
			if differences[0] > 1e-8 || differences[1] > 2e-7 || differences[2] > 2e-4 || actual.Settled != expected.Settled {
				t.Fatalf("case %d action %d: Go=%+v TS=%+v differences=%v", i, j, actual, expected, differences)
			}
		}
	}
	t.Logf("original TS spring, %d cases; max |position,velocity,acceleration| errors=%v", len(cases), maximum)
}

const springOracleScript = `import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const {Spring}=await import(pathToFileURL(path.join(input.Root,'packages/core/src/utils/spring.ts')).href);
const params=p=>({mass:p.Mass,damping:p.Damping,stiffness:p.Stiffness,soft:p.Soft});
const results=input.Cases.map(c=>{
 const s=new Spring(c.Position); s.updateParams(params(c.Params)); let now=0;
 return c.Actions.map(a=>{
  if(a.Time>now) s.update(a.Time-now); now=a.Time;
  if(a.Kind==='target')s.setTargetPosition(a.Position,a.Delay);
  if(a.Kind==='position')s.setPosition(a.Position);
  if(a.Kind==='params')s.updateParams(params(a.Params),a.Delay);
  return {Position:s.getCurrentPosition(),Velocity:s.getV(s.currentTime),Acceleration:s.getV2(s.currentTime),Settled:s.arrived()};
 });
});
process.stdout.write(JSON.stringify(results));`

type wordReferenceCase struct {
	Options   EmphasisOptions
	Positions []time.Duration
}

func TestReferenceWordsAgainstFixedTypeScript(t *testing.T) {
	var cases []wordReferenceCase
	for _, options := range []EmphasisOptions{
		{Start: time.Second, End: 1500 * time.Millisecond, Characters: 2},
		{Start: time.Second, End: 3 * time.Second, Characters: 4, Last: true},
		{Start: 100 * time.Millisecond, End: 5100 * time.Millisecond, Characters: 5, RubyCharacters: 2, Background: true},
		{Start: 100 * time.Millisecond, End: 15100 * time.Millisecond, Characters: 7, Last: true, Background: true},
	} {
		effect := NewEmphasis(options)
		positions := []time.Duration{0, options.Start, options.Start + time.Millisecond, options.Start + 10*time.Millisecond, options.Start + 100*time.Millisecond, options.Start + 500*time.Millisecond, options.Start + time.Second, effect.End()}
		// Compare every generated glow keyframe, including all character delays.
		for i := 0; i < options.Characters; i++ {
			for j := 1; j <= 32; j++ {
				positions = append(positions, refDuration(effect.start+float64(i)*effect.charDelay+float64(j)/32*effect.duration))
			}
		}
		cases = append(cases, wordReferenceCase{Options: options, Positions: positions})
	}
	input := struct {
		Root  string
		Cases []wordReferenceCase
	}{os.Getenv("AMLL_REF_ROOT"), cases}
	var oracle []struct {
		Poses  [][]CharacterPose
		Floats []float64
	}
	nodeReference(t, wordOracleScript, input, &oracle, true)
	maximum, floatMaximum := 0.0, 0.0
	for i, test := range cases {
		effect := NewEmphasis(test.Options)
		ordinary := NewFloat(test.Options.Start, test.Options.End, test.Options.Background)
		for j, position := range test.Positions {
			floatMaximum = max(floatMaximum, math.Abs(ordinary.Sample(position)-oracle[i].Floats[j]))
			for index := 0; index < test.Options.Characters; index++ {
				got, want := effect.Character(position, index), oracle[i].Poses[j][index]
				actual := [...]float64{got.Scale, got.TranslateXEm, got.TranslateYEm, got.FloatYEm, got.ShadowBlurEm, got.ShadowAlpha}
				expected := [...]float64{want.Scale, want.TranslateXEm, want.TranslateYEm, want.FloatYEm, want.ShadowBlurEm, want.ShadowAlpha}
				for k := range actual {
					difference := math.Abs(actual[k] - expected[k])
					maximum = max(maximum, difference)
					if difference > 2e-9 {
						t.Fatalf("case %d sample %d character %d field %d: Go %.15g TS %.15g", i, j, index, k, actual[k], expected[k])
					}
				}
			}
		}
	}
	if floatMaximum > 1e-12 {
		t.Fatalf("float ease-out differs by %g", floatMaximum)
	}
	t.Logf("original TS float/emphasis generators, %d cases; max emphasis error=%g, float error=%g", len(cases), maximum, floatMaximum)
}

const wordOracleScript = `import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import bezier from 'bezier-easing';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const root=p=>pathToFileURL(path.join(input.Root,p)).href;
const {createFloatAnimation}=await import(root('packages/core/src/lyric-player/dom/animation/float/index.ts'));
const {createEmphasizeAnimation}=await import(root('packages/core/src/lyric-player/dom/animation/emphasize/index.ts'));
const easeOut=bezier(0,0,.58,1);
const mock=()=>({textContent:'X',animate(frames,options){return {frames,options,pause(){}};}});
const lerp=(a,b,t)=>a+(b-a)*t;
function interpolate(frames,p,neutral){
 const all=[neutral,...frames]; const n=Math.min(1,Math.max(0,p))*32;
 const left=Math.floor(n),right=Math.min(32,left+1),t=n-left;
 return all[left].map((a,i)=>lerp(a,all[right][i],t));
}
const results=input.Cases.map(c=>{
 const o=c.Options, start=o.Start/1e6,end=o.End/1e6;
 const characters=Array.from({length:o.Characters},mock);
 const animations=createEmphasizeAnimation({word:{word:'SYNTHETIC',startTime:start,endTime:end},characterElements:characters,duration:end-start,delay:start,rubyCharCount:o.RubyCharacters,isBG:o.Background,isLastWord:o.Last});
 const plain=createFloatAnimation(mock(),{word:{startTime:start,endTime:end},lineStartTime:0,isBG:o.Background});
 const parsed=characters.map((_,i)=>{
  const glow=animations[i*2],float=animations[i*2+1];
  const frames=glow.frames.map(f=>{
   const m=f.transform.match(/matrix3d\(([^)]+)\)/)[1].split(',').map(Number);
   const xy=f.transform.match(/translate\(([-+\deE.]+)em, ([-+\deE.]+)em\)/).slice(1).map(Number);
   const sh=f.textShadow.match(/0 0 ([-+\deE.]+)em rgba\(255, 255, 255, ([-+\deE.]+)\)/).slice(1).map(Number);
   return [m[0],xy[0],xy[1],sh[0],sh[1]];
  });
  const floats=float.frames.map(f=>[Number(f.transform.match(/translateY\(([-+\deE.]+)em\)/)[1])]);
  return {glow,float,frames,floats};
 });
 return {Poses:c.Positions.map(ns=>parsed.map(a=>{
  const p=ns/1e6;
  const g=interpolate(a.frames,(p-a.glow.options.delay)/a.glow.options.duration,[1,0,0,0,0]);
  const f=interpolate(a.floats,(p-a.float.options.delay)/a.float.options.duration,[0]);
  return {Scale:g[0],TranslateXEm:g[1],TranslateYEm:g[2],FloatYEm:f[0],ShadowBlurEm:g[3],ShadowAlpha:g[4]};
 })),Floats:c.Positions.map(ns=>{
  const p=Math.min(1,Math.max(0,(ns/1e6-plain.options.delay)/plain.options.duration));
  return -(o.Background?.1:.05)*easeOut(p);
 })};
});
process.stdout.write(JSON.stringify(results));`

func TestReferencePolicyStaggerAndEasingFromSources(t *testing.T) {
	input := struct{ Root string }{os.Getenv("AMLL_REF_ROOT")}
	var oracle struct {
		Policies []struct{ Stiffness, Damping float64 }
		Stagger  [][]float64
		Ease     [][]float64
	}
	nodeReference(t, policyOracleScript, input, &oracle, true)
	policyArgs := []struct {
		seek, interlude, end bool
		interval             time.Duration
		has                  bool
	}{{true, false, false, 0, false}, {false, true, false, 0, false}, {false, false, true, 0, false},
		{false, false, false, 0, false}, {false, false, false, 50 * time.Millisecond, true},
		{false, false, false, 100 * time.Millisecond, true}, {false, false, false, 200 * time.Millisecond, true},
		{false, false, false, 500 * time.Millisecond, true}, {false, false, false, 800 * time.Millisecond, true},
		{false, false, false, 2 * time.Second, true}}
	for i, args := range policyArgs {
		actual := RefYParams(args.seek, args.interlude, args.end, args.interval, args.has)
		if math.Abs(actual.Stiffness-oracle.Policies[i].Stiffness) > 1e-12 || math.Abs(actual.Damping-oracle.Policies[i].Damping) > 1e-12 {
			t.Fatalf("policy %d: Go=%+v TS=%+v", i, actual, oracle.Policies[i])
		}
	}
	for scenario, data := range [][3]int{{1, 3, 0}, {1, 0, 0}, {1, 3, 1}} {
		for index, milliseconds := range oracle.Stagger[scenario] {
			actual := RefStagger(data[0], index, data[1])
			if data[2] == 1 {
				actual = 0
			}
			if math.Abs(float64(actual)/float64(time.Millisecond)-milliseconds) > 0.000001 {
				t.Fatalf("stagger scenario %d line %d: Go=%v TS=%gms", scenario, index, actual, milliseconds)
			}
		}
	}
	for i, x := range []float64{0, 0.001, 0.1, 0.2, 0.5, 0.9, 0.999, 1} {
		if math.Abs(Ease(x)-oracle.Ease[i][0]) > 1e-12 || math.Abs(EaseOut(x)-oracle.Ease[i][1]) > 1e-12 {
			t.Fatalf("easing differs at %g", x)
		}
	}
	t.Log("fixed TypeScript Y-policy and actual calcLayout stagger block agree; easing agrees with reviewed bezier-easing 3.0.1")
}

const policyOracleScript = `import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import bezier from 'bezier-easing';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const root=p=>pathToFileURL(path.join(input.Root,p)).href;
const {getPosYSpringPolicy}=await import(root('packages/core/src/lyric-player/base/spring.ts'));
const {Duration}=await import(root('packages/core/src/utils/time.ts'));
const policies=[[true,false,undefined,false],[false,true,undefined,false],[false,false,undefined,true],[false,false,undefined,false],[false,false,50,false],[false,false,100,false],[false,false,200,false],[false,false,500,false],[false,false,800,false],[false,false,2000,false]].map(a=>{const p=getPosYSpringPolicy(...a);return {Stiffness:p.stiffness,Damping:p.damping};});
const source=fs.readFileSync(path.join(input.Root,'packages/core/src/lyric-player/base/index.ts'),'utf8');
const begin=source.indexOf('let delay = Duration.ZERO;'),end=source.indexOf('const isBottomFocused',begin);
if(begin<0||end<begin)throw Error('reference stagger block not found');
const run=new Function('Duration','result','snapshot','strategy','brightnessWindowFrom','brightnessWindowTo','activeCount',source.slice(begin,end));
const stagger=[[3,false],[0,false],[3,true]].map(([focus,disabled])=>{
 const delays=[];const result={lineInstructions:Array.from({length:20},(_,i)=>({y:i*80-160,height:80,isInViewport:true}))};
 const context={currentLyricGroups:Array.from({length:20},(_,i)=>({setTransform(y,snap,delay){delays[i]=delay;}})),resolveIsActive(){return false;},resolveOpacity(){return 1;},resolveBlurLevel(){return 0;}};
 run.call(context,Duration,result,{scrollToIndex:focus},{disableStagger:disabled,snapPosY:false},-1,21,20);
 return delays;
});
const ease=bezier(.25,.1,.25,1),out=bezier(0,0,.58,1);
process.stdout.write(JSON.stringify({Policies:policies,Stagger:stagger,Ease:[0,.001,.1,.2,.5,.9,.999,1].map(x=>[ease(x),out(x)])}));`
