// SPDX-License-Identifier: AGPL-3.0-only
// Source oracle: AMLL f32285864056587c4afa59d310208b0aa829cb53,
// packages/core/src/lyric-player/base/interlude-dots.ts and dom/interlude-dots.ts.
package motion

import (
	"math"
	"os"
	"testing"
	"time"
)

type interludeAction struct {
	Kind                           string
	Time, Delta, Start, End, Media float64
	Force, Immediate               bool
	Anchor                         int
	X, Y                           float64
	Params                         RefParams
}

type interludeOracleCase struct {
	Name    string
	Actions []interludeAction
}

type interludeOraclePose struct {
	Raw              InterludeSnapshot
	X, Y, RawX, RawY float64
	Opacity, Scale   float64
	Dots             [3]float64
	Phase            InterludePhase
	Mode             InterludeMode
	Accepted         bool
}

func TestInterludeAgainstFixedTypeScript(t *testing.T) {
	cases := interludeSourceCases()
	input := struct {
		Root  string
		Cases []interludeOracleCase
	}{os.Getenv("AMLL_REF_ROOT"), cases}
	var oracle [][]interludeOraclePose
	nodeReference(t, interludeOracleScript, input, &oracle, true)
	base := time.Unix(100, 0)
	maximum := 0.0
	for i, test := range cases {
		controller := NewInterlude(base)
		for j, action := range test.Actions {
			now := base.Add(interludeMS(action.Time))
			accepted := applyInterludeAction(&controller, now, action)
			pose, expected := controller.Pose(), oracle[i][j]
			pairs := [][2]float64{{pose.Raw.Scale, expected.Raw.Scale}, {pose.Raw.Opacity, expected.Raw.Opacity},
				{pose.RawX, expected.RawX}, {pose.RawY, expected.RawY}, {pose.X, expected.X}, {pose.Y, expected.Y},
				{pose.Scale, expected.Scale}, {pose.Opacity, expected.Opacity}}
			for k := range 3 {
				pairs = append(pairs, [2]float64{pose.Raw.DotOpacities[k], expected.Raw.DotOpacities[k]}, [2]float64{pose.DotOpacities[k], expected.Dots[k]})
			}
			for k, pair := range pairs {
				err := math.Abs(pair[0] - pair[1])
				maximum = max(maximum, err)
				if err > 1e-8 {
					t.Fatalf("%s action%d %+v field%d Go=%g TS=%g; rawGo=%+v rawTS=%+v", test.Name, j, action, k, pair[0], pair[1], pose.Raw, expected.Raw)
				}
			}
			if pose.Raw.Active != expected.Raw.Active || pose.Phase != expected.Phase || pose.Mode != expected.Mode || action.Kind == "set" && accepted != expected.Accepted {
				t.Fatalf("%s action%d %+v Go=%+v accepted%v TS=%+v", test.Name, j, action, pose, accepted, expected)
			}
		}
	}
	t.Logf("original TS InterludeDotsBase: %d cases, max raw/DOM/spring error %.3g", len(cases), maximum)
	for _, point := range [][2]int{{0, 8}, {0, 28}, {0, 50}, {1, 16}, {2, 20}, {3, 50}, {17, 4}} {
		pose := oracle[point[0]][point[1]]
		t.Logf("original fixed point case=%d action=%d raw=%+v", point[0], point[1], pose.Raw)
	}
}

func interludeMS(ms float64) time.Duration { return refDuration(ms * float64(time.Millisecond)) }

func applyInterludeAction(controller *Interlude, now time.Time, action interludeAction) bool {
	switch action.Kind {
	case "set":
		return controller.SetInterlude(now, interludeMS(action.Start), interludeMS(action.End), interludeMS(action.Media), action.Force, action.Anchor)
	case "sync":
		controller.SyncClock(interludeMS(action.Media))
	case "update":
		controller.AdvanceFrame(now, interludeMS(action.Delta))
	case "pause":
		controller.Pause()
	case "resume":
		controller.Resume()
	case "dismiss":
		controller.Dismiss(action.Immediate)
	case "clear":
		controller.ClearInterlude(action.Immediate)
	case "transform":
		controller.SetTransform(now, action.X, action.Y, action.Immediate)
	case "params":
		controller.SetYParams(now, action.Params)
	}
	return false
}

func interludeSourceCases() []interludeOracleCase {
	var cases []interludeOracleCase
	for _, item := range []struct {
		Name              string
		Start, End, Media float64
		Force             bool
		Anchor            int
	}{
		{"intro-breathe", 0, 12000, 0, false, -1}, {"middle-breathe", 5000, 12000, 5000, false, 3},
		{"seek-hold", 0, 12000, 8000, true, -1}, {"short-intro-hold", 0, 3000, 0, false, -1},
	} {
		actions := []interludeAction{{Kind: "set", Start: item.Start, End: item.End, Media: item.Media, Force: item.Force, Anchor: item.Anchor},
			{Kind: "transform", X: 1.125, Y: 100.005}, {Kind: "update"}}
		for i, offset := range []float64{0, 80, 90, 160, 180, 499, 500, 590, 680, 750, 910, 1000, 2000, 3000, 4000, 6000, 9000, 10000, 10750, 10875, 10999, 11000, 11750, 11875, 11999, 12000} {
			actions = append(actions, interludeAction{Kind: "sync", Time: float64(i + 1), Media: item.Media + offset}, interludeAction{Kind: "update", Time: float64(i + 1)})
		}
		cases = append(cases, interludeOracleCase{Name: item.Name, Actions: actions})
	}
	for _, end := range []float64{1909, 1910, 2409, 2410, 4499, 4500} {
		for _, anchor := range []int{-1, 0} {
			cases = append(cases, interludeOracleCase{Name: "threshold", Actions: []interludeAction{
				{Kind: "set", End: end, Anchor: anchor}, {Kind: "update"}, {Kind: "sync", Time: 1, Media: 1000}, {Kind: "update", Time: 1},
			}})
		}
	}
	cases = append(cases,
		interludeOracleCase{Name: "pause-resume-spring", Actions: []interludeAction{
			{Kind: "set", End: 12000, Anchor: -1}, {Kind: "transform", X: 20, Y: 200}, {Kind: "sync", Media: 2000}, {Kind: "update"},
			{Kind: "transform", X: 40, Y: 500}, {Kind: "update", Time: 16, Delta: 16}, {Kind: "pause", Time: 16},
			{Kind: "update", Time: 516, Delta: 500}, {Kind: "resume", Time: 516}, {Kind: "update", Time: 532, Delta: 16},
			{Kind: "params", Time: 532, Params: RefYParams(true, false, false, 0, false)},
			{Kind: "transform", Time: 532, X: -1.125, Y: -100.005}, {Kind: "update", Time: 548, Delta: 16},
			{Kind: "set", Time: 548, End: 20000, Media: 5000, Force: true, Anchor: 5}, {Kind: "transform", Time: 548, X: 0, Y: 800},
			{Kind: "update", Time: 564, Delta: 16},
		}},
		interludeOracleCase{Name: "dismiss-clock-freeze-and-clear", Actions: []interludeAction{
			{Kind: "set", End: 12000, Anchor: -1}, {Kind: "sync", Media: 3000}, {Kind: "update"},
			{Kind: "dismiss"}, {Kind: "update", Time: 50, Delta: 50}, {Kind: "sync", Time: 50, Media: 9000}, {Kind: "update", Time: 125, Delta: 75},
			{Kind: "update", Time: 150, Delta: 25}, {Kind: "set", Time: 150, End: 12000, Media: 4000, Anchor: -1},
			{Kind: "clear", Time: 150}, {Kind: "set", Time: 150, End: 12000, Media: 4000, Anchor: -1}, {Kind: "update", Time: 150},
			{Kind: "dismiss", Time: 150}, {Kind: "pause", Time: 150}, {Kind: "resume", Time: 150}, {Kind: "update", Time: 166, Delta: 16},
		}},
		interludeOracleCase{Name: "source-cancel-fade-edge", Actions: []interludeAction{
			{Kind: "set", End: 12000, Anchor: -1}, {Kind: "sync", Media: 3000}, {Kind: "update"},
			{Kind: "dismiss"}, {Kind: "update", Time: 50, Delta: 50}, {Kind: "dismiss", Time: 50, Immediate: true},
			{Kind: "set", Time: 50, End: 12000, Media: 3000, Anchor: -1}, {Kind: "update", Time: 50},
			{Kind: "dismiss", Time: 50}, {Kind: "update", Time: 100, Delta: 50},
			{Kind: "set", Time: 100, Start: 11000, End: 12000, Media: 11900, Force: true}, {Kind: "update", Time: 100},
		}},
	)
	return cases
}

const interludeOracleScript = `
import fs from 'node:fs';
import {pathToFileURL} from 'node:url';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const {InterludeDotsBase}=await import(pathToFileURL(input.Root+'/packages/core/src/lyric-player/base/interlude-dots.ts'));
class Capture extends InterludeDotsBase {
 render(s,x,y){this.last={Raw:{Active:s.isActive,DotOpacities:[...s.dotOpacities],Scale:s.scale,Opacity:s.opacity},RawX:x,RawY:y,X:Number(x.toFixed(2)),Y:Number(y.toFixed(2)),Opacity:Number(s.opacity.toFixed(3)),Scale:Number(s.scale.toFixed(4)),Dots:s.dotOpacities.map(v=>Number(v.toFixed(3)))};}
}
const output=input.Cases.map(test=>{const c=new Capture();return test.Actions.map(a=>{let accepted=false;switch(a.Kind){
 case 'set':accepted=c.setInterlude([a.Start,a.End],a.Media,a.Force,a.Anchor);break;
 case 'sync':c.syncClock(a.Media);break;
 case 'update':c.update(a.Delta);break;
 case 'pause':c.pause();break;case 'resume':c.resume();break;
 case 'dismiss':c.dismiss(a.Immediate);break;case 'clear':c.clearInterlude(a.Immediate);break;
 case 'transform':c.setTransform(a.X,a.Y,a.Immediate);break;
 case 'params':c.posY.updateParams({mass:a.Params.Mass,damping:a.Params.Damping,stiffness:a.Params.Stiffness,soft:a.Params.Soft});break;
 }
 const last=c.last??{Raw:{Active:false,DotOpacities:[0,0,0],Scale:1,Opacity:0},X:0,Y:0,RawX:0,RawY:0,Opacity:0,Scale:1,Dots:[0,0,0]};return {...last,Phase:c.phase,Mode:c.mode,Accepted:accepted};});});
process.stdout.write(JSON.stringify(output));
`

func TestInterludeFrameIdempotencyAndScheduling(t *testing.T) {
	base := time.Unix(100, 0)
	controller := NewInterlude(base)
	input := InterludeInput{Present: true, Playing: true, Start: 5 * time.Second, End: 12 * time.Second, Media: 5 * time.Second, AnchorLineIndex: 1}
	pose := controller.Frame(base, input)
	if pose.Opacity != 0 || pose.Occupancy != 1 || pose.NeedsFrame || pose.NextWake != 500*time.Millisecond {
		t.Fatalf("entry hold scheduling: %+v", pose)
	}
	for range 3 {
		if got := controller.Frame(base, input); got != pose {
			t.Fatal("same-time preparation changed interlude")
		}
	}
	input.Media = 5590 * time.Millisecond
	input.CappedDelta = 590 * time.Millisecond
	pose = controller.Frame(base.Add(590*time.Millisecond), input)
	if !pose.NeedsFrame || pose.Opacity <= 0 || pose.Opacity >= 1 {
		t.Fatalf("entry fade did not run: %+v", pose)
	}
	before := pose
	for range 3 {
		if controller.Pose() != before {
			t.Fatal("read-only Pose mutated state")
		}
	}
	input.Playing = false
	pose = controller.Frame(base.Add(600*time.Millisecond), input)
	if pose.NeedsFrame || pose.NextWake != 0 {
		t.Fatalf("paused static pose schedules frames: %+v", pose)
	}
	input.SeekRevision++
	input.Media = 10 * time.Second
	input.Playing = true
	pose = controller.Frame(base.Add(time.Second), input)
	if pose.Phase != InterludeIdle || pose.Active {
		t.Fatalf("too-late seek must hide: %+v", pose)
	}
	if allocs := testing.AllocsPerRun(1000, func() { _ = controller.Pose() }); allocs != 0 {
		t.Fatalf("pose allocated %g times", allocs)
	}
}

func TestInterludeRecordedReferenceVectors(t *testing.T) {
	// These are raw render callbacks emitted by the untouched fixed TS class,
	// using the actions in interludeSourceCases. They run without Node/ref files.
	vectors := []struct {
		caseIndex, action int
		want              InterludeSnapshot
	}{
		{0, 8, InterludeSnapshot{Active: true, DotOpacities: [3]float64{.002889882398546731, 3.555555555555557e-05, 0}, Scale: 1.0000541434209769, Opacity: .8074969107388739}},
		{0, 28, InterludeSnapshot{Active: true, DotOpacities: [3]float64{.5633359657458583, .2, .2}, Scale: 1.226030517250985, Opacity: 1}},
		{0, 50, InterludeSnapshot{Active: true, DotOpacities: [3]float64{.8999999999999999, .8999999999999999, .8999999999999999}, Scale: 1.0792612751585464, Opacity: .8045804321687667}},
		{1, 16, InterludeSnapshot{Active: true, Scale: 1}},
		{2, 20, InterludeSnapshot{Active: true, DotOpacities: [3]float64{.05183999999999999, .015999999999999997, .00064}, Scale: 1, Opacity: 1}},
		{3, 50, InterludeSnapshot{Scale: 1}},
		{17, 4, InterludeSnapshot{Active: true, DotOpacities: [3]float64{.8443496953258025, .2, .2}, Scale: 1.2489708516775773, Opacity: .8983372309890318}},
	}
	cases := interludeSourceCases()
	base := time.Unix(100, 0)
	for _, vector := range vectors {
		controller := NewInterlude(base)
		for _, action := range cases[vector.caseIndex].Actions[:vector.action+1] {
			applyInterludeAction(&controller, base.Add(interludeMS(action.Time)), action)
		}
		got := controller.Pose().Raw
		if got.Active != vector.want.Active || math.Abs(got.Scale-vector.want.Scale) > 1e-12 || math.Abs(got.Opacity-vector.want.Opacity) > 1e-12 {
			t.Fatalf("recorded point%+v got%+v", vector, got)
		}
		for i := range got.DotOpacities {
			if math.Abs(got.DotOpacities[i]-vector.want.DotOpacities[i]) > 1e-12 {
				t.Fatalf("recorded dot point%+v got%+v", vector, got)
			}
		}
	}
}

func TestInterludeFrameClocksDoNotReconsumePhysicalTime(t *testing.T) {
	base := time.Unix(100, 0)
	controller := NewInterlude(base)
	input := InterludeInput{Present: true, Playing: true, End: 12 * time.Second, AnchorLineIndex: -1}
	controller.Frame(base, input)
	controller.SetTransform(base, 0, 0, false)
	controller.SetTransform(base, 0, 100, false)
	input.Media = 16 * time.Millisecond
	input.CappedDelta = 16 * time.Millisecond
	controller.Frame(base.Add(16*time.Millisecond), input)
	first := controller.Pose()
	if first.RawY <= 0 {
		t.Fatal("position spring did not advance")
	}
	input.Playing = false
	controller.Frame(base.Add(15*time.Millisecond), input) // earlier input consumes no frame
	if controller.Pose().RawY != first.RawY {
		t.Fatal("earlier UI time advanced the position spring")
	}
	controller.Frame(base.Add(16*time.Millisecond), input)
	if controller.Pose().RawY != first.RawY {
		t.Fatal("same physical frame was advanced twice")
	}
	if allocs := testing.AllocsPerRun(1000, func() { controller.Frame(base.Add(16*time.Millisecond), input) }); allocs != 0 {
		t.Fatalf("warm frame allocated %g", allocs)
	}
}

func TestInterludeFallbackHoldSleepsAndResumeRebuilds(t *testing.T) {
	base := time.Unix(100, 0)
	controller := NewInterlude(base)
	input := InterludeInput{Present: true, Playing: true, End: 3 * time.Second, AnchorLineIndex: -1, Media: time.Second}
	pose := controller.Frame(base, input)
	if pose.Mode != InterludeHold || pose.NeedsFrame || pose.NextWake != time.Second {
		t.Fatalf("fully entered fallback must sleep until exit: %+v", pose)
	}
	input.Playing = false
	_ = controller.Frame(base.Add(time.Millisecond), input)
	input.Present = false
	pose = controller.Frame(base.Add(2*time.Millisecond), input)
	if pose.Active || pose.Phase != InterludeIdle {
		t.Fatalf("paused dismissal must hide: %+v", pose)
	}
	input.Playing = true
	input.Present = true
	input.Media = 0
	pose = controller.Frame(base.Add(3*time.Millisecond), input)
	if pose.Phase != InterludePerforming || pose.Occupancy != 1 {
		t.Fatalf("clear must allow same-interval re-entry: %+v", pose)
	}
}

func TestInterludeCSSMetricsAndFixedRounding(t *testing.T) {
	metrics := MeasureInterludeDots(40, 1000, 40)
	if metrics.Width != 130.4 || metrics.Height != 52 || metrics.DotDiameter != 12 || metrics.Gap != 7.199999999999999 || metrics.PaddingY != 16 {
		t.Fatalf("CSS geometry: %+v", metrics)
	}
	for _, test := range []struct {
		value  float64
		digits int
		want   float64
	}{{1.125, 2, 1.13}, {100.005, 2, 100}, {-1.125, 2, -1.13}, {0.1255, 3, .126}, {1.25, 4, 1.25}} {
		if got := interludeFixed(test.value, test.digits); got != test.want {
			t.Errorf("JS toFixed(%g,%d)=%g want%g", test.value, test.digits, got, test.want)
		}
	}
}

func BenchmarkInterludePose(b *testing.B) {
	base := time.Unix(100, 0)
	controller := NewInterlude(base)
	controller.SetInterlude(base, 0, 20*time.Second, 5*time.Second, false, -1)
	controller.AdvanceFrame(base, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = controller.Pose()
	}
}

func BenchmarkInterludeFrame(b *testing.B) {
	base := time.Unix(100, 0)
	controller := NewInterlude(base)
	input := InterludeInput{Present: true, Playing: true, End: 20 * time.Second, AnchorLineIndex: -1, CappedDelta: 16 * time.Millisecond}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input.Media = time.Second + time.Duration(i%9000)*time.Millisecond
		controller.Frame(base.Add(time.Duration(i+1)*16*time.Millisecond), input)
	}
}
