package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
	"github.com/xiaowumin-mark/mygo-amll/timeline"
)

func presentationWord(text string, start, end int) lyric.Word {
	return lyric.Word{Text: text, Start: time.Duration(start) * time.Millisecond, End: time.Duration(end) * time.Millisecond}
}

func presentationLine(id string, start, end int, words ...lyric.Word) lyric.Line {
	var text strings.Builder
	for _, word := range words {
		text.WriteString(word.Text)
	}
	return lyric.Line{ID: id, Text: text.String(), Start: time.Duration(start) * time.Millisecond, End: time.Duration(end) * time.Millisecond, Words: words}
}

func presentationDocument(t *testing.T, lines []lyric.Line) *lyric.Document {
	t.Helper()
	document, err := lyric.NewDocument(lines)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestPresentationOwnershipSpacingAndBackgrounds(t *testing.T) {
	main := presentationLine("main", 1000, 5000, presentationWord("Alpha", 2000, 2500), presentationWord("beta", 2500, 3000))
	main.Text = "Alpha \t beta"
	background := presentationLine("back", 1500, 4500, presentationWord("（  echo  ）", 1500, 4500))
	background.Background, background.ParentID = true, "main"
	source := presentationDocument(t, []lyric.Line{main, background})
	before := []lyric.Line{snapshotLine(source.Line(0)), snapshotLine(source.Line(1))}
	p, err := newPresentation(source)
	if err != nil {
		t.Fatal(err)
	}
	g := p.groups[0]
	if len(p.groups) != 1 || !g.backgroundFirst || g.start != 900*time.Millisecond || g.end != 4500*time.Millisecond ||
		p.document.Line(g.mainLayout).Word(0).Text != "Alpha " || p.document.Line(g.backgroundLayout).Word(0).Text != "echo" {
		t.Fatalf("presentation did not restore spaces, sanitize background or sync timing: %+v", g)
	}
	if after := []lyric.Line{snapshotLine(source.Line(0)), snapshotLine(source.Line(1))}; !reflect.DeepEqual(before, after) {
		t.Fatal("presentation changed immutable source text, words or times")
	}
	// A background sorted before its parent still belongs to that parent.
	background.Start, background.End = 0, 2*time.Second
	background.Words = []lyric.Word{presentationWord("（echo）", 0, 2000)}
	background.Text = "（echo）"
	p, err = newPresentation(presentationDocument(t, []lyric.Line{background, main}))
	if err != nil || len(p.groups) != 1 || p.groups[0].main != 1 || p.groups[0].background != 0 || p.groupForLine[0] != p.groupForLine[1] {
		t.Fatalf("ParentID was lost through chronological source order: %+v, %v", p, err)
	}
	invalid := main
	invalid.Text = "Alpha, beta"
	if _, err := newPresentation(presentationDocument(t, []lyric.Line{invalid})); err == nil {
		t.Fatal("presentation silently discarded untimed non-whitespace text")
	}
	if _, err := newPresentation(nil); err == nil {
		t.Fatal("nil source was not rejected")
	}
}

func TestPresentationClippedAndZeroGroupTimeline(t *testing.T) {
	lines := []lyric.Line{
		presentationLine("long", 0, 3000, presentationWord("Long", 0, 3000)),
		presentationLine("inside", 1000, 2500, presentationWord("Inside", 1000, 2500)),
		presentationLine("near", 2950, 3950, presentationWord("Near", 2950, 3950)),
	}
	p, err := newPresentation(presentationDocument(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	if p.groups[0].end != 2950*time.Millisecond || p.document.Line(p.groups[0].mainLayout).End() != 3*time.Second || p.timeline.Line(0).End() != 2950*time.Millisecond {
		t.Fatal("reference cleanup was lost or strict word geometry was made invalid")
	}
	zero := []lyric.Line{
		presentationLine("zero-after-cleanup", 0, 50, presentationWord("A", 0, 50)),
		presentationLine("live", 0, 100, presentationWord("B", 0, 100)),
		presentationLine("last", 100, 200, presentationWord("C", 100, 200)),
	}
	p, err = newPresentation(presentationDocument(t, zero))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.groups) != 3 || p.groups[0].start != 0 || p.groups[0].end != 0 || !reflect.DeepEqual(p.indexToGroup, []int{1, 2}) {
		t.Fatalf("zero group was lost or timeline mapping shifted: groups=%+v mapping=%v", p.groups, p.indexToGroup)
	}
	index, err := timeline.New(p.timeline)
	if err != nil {
		t.Fatal(err)
	}
	state := index.VisualAt(10*time.Millisecond, nil, nil)
	if len(state.Playing) != 1 || p.indexToGroup[state.Playing[0]] != 1 {
		t.Fatal("active timeline index did not map back to its retained group")
	}
}

type presentationOracleCase struct {
	name  string
	lines []lyric.Line
	// ParentID is a native adapter contract; the reference uses adjacent source
	// main/background entries. These explicit fixtures state the canonical order.
	oracleOrder []string
}

func presentationOracleCases() []presentationOracleCase {
	bg := func(line lyric.Line) lyric.Line { line.Background = true; return line }
	cases := []presentationOracleCase{
		{"empty", nil, nil},
		{"spaces", []lyric.Line{presentationLine("space", 0, 3000, presentationWord("A\t\u00a0\ufeff ", 1000, 1500), presentationWord("B\u0085", 1500, 2000))}, nil},
		{"reset sort", []lyric.Line{presentationLine("later-word", 0, 3000, presentationWord("A", 2000, 3000)), presentationLine("earlier-word", 1000, 2100, presentationWord("B", 1500, 2100))}, nil},
		{"background first", []lyric.Line{presentationLine("main", 1000, 4000, presentationWord("Main", 2000, 3500)), bg(presentationLine("bg", 1500, 4500, presentationWord("（ echo ）", 1500, 4500)))}, nil},
		{"separate timed brackets", []lyric.Line{presentationLine("main", 0, 5000, presentationWord("Main", 1000, 2000)), bg(presentationLine("bg", 500, 4000, presentationWord("(", 500, 1000), presentationWord("echo", 1000, 3000), presentationWord(")", 3000, 4000)))}, nil},
		{"excessive background", []lyric.Line{bg(presentationLine("orphan", 0, 1000, presentationWord("(orphan)", 0, 1000))), bg(presentationLine("extra", 100, 1100, presentationWord("（ extra ）", 100, 1100))), presentationLine("main", 2000, 3000, presentationWord("Main", 2000, 3000)), bg(presentationLine("bg", 2100, 3100, presentationWord("(echo)", 2100, 3100)))}, nil},
		{"intentional then cleanup", []lyric.Line{presentationLine("A", 0, 3000, presentationWord("A", 0, 3000)), presentationLine("B", 1000, 2500, presentationWord("B", 1000, 2500)), presentationLine("C", 2950, 3950, presentationWord("C", 2950, 3950))}, nil},
		{"overlap advance 70 percent", []lyric.Line{presentationLine("A", 0, 2000, presentationWord("A", 0, 2000)), presentationLine("B", 1800, 2500, presentationWord("B", 1800, 2500))}, nil},
		{"overlap cohort gap", []lyric.Line{presentationLine("A", 0, 10000, presentationWord("A", 0, 10000)), presentationLine("B", 1000, 2000, presentationWord("B", 1000, 2000)), presentationLine("C", 2100, 3000, presentationWord("C", 2100, 3000))}, nil},
		{"zero cleanup", []lyric.Line{presentationLine("A", 0, 50, presentationWord("A", 0, 50)), presentationLine("B", 0, 100, presentationWord("B", 0, 100)), presentationLine("C", 100, 200, presentationWord("C", 100, 200))}, nil},
		{"last word end is not maximum", []lyric.Line{presentationLine("A", 0, 5000, presentationWord("A", 0, 5000), presentationWord("B", 1000, 2000))}, nil},
	}
	for _, overlap := range []int{99, 100, 101, 499, 500} {
		for _, duration := range []int{200, 5000} {
			cases = append(cases, presentationOracleCase{name: fmt.Sprintf("overlap-%dms-next-%dms", overlap, duration), lines: []lyric.Line{
				presentationLine("A", 0, 1000, presentationWord("A", 0, 1000)),
				presentationLine("B", 1000-overlap, 1000-overlap+duration, presentationWord("B", 1000-overlap, 1000-overlap+duration)),
			}})
		}
	}
	mainNoWords := presentationLine("main", 1000, 3000)
	mainNoWords.Text = "Untimed main"
	cases = append(cases, presentationOracleCase{"main fallback ordering", []lyric.Line{mainNoWords, bg(presentationLine("bg", 1500, 3000, presentationWord("echo", 1500, 3000)))}, nil})
	bgNoWords := bg(presentationLine("bg", 2700, 3000))
	bgNoWords.Text = "（echo）"
	cases = append(cases, presentationOracleCase{"bg fallback ordering", []lyric.Line{presentationLine("main", 0, 3000, presentationWord("Main", 2000, 2500)), bgNoWords}, nil})
	parent := presentationLine("parent", 1000, 3000, presentationWord("Main", 1000, 3000))
	child := bg(presentationLine("child", 0, 2000, presentationWord("(echo)", 0, 2000)))
	child.ParentID = "parent"
	cases = append(cases, presentationOracleCase{"explicit parent before-main child", []lyric.Line{child, parent}, []string{"parent", "child"}})
	parentNoWords := presentationLine("parent-plain", 1000, 3000)
	parentNoWords.Text = "Main"
	child = bg(presentationLine("child-early", 0, 2000, presentationWord("echo", 500, 2000)))
	child.ParentID = "parent-plain"
	cases = append(cases, presentationOracleCase{"parent no-words fallback", []lyric.Line{child, parentNoWords}, []string{"parent-plain", "child-early"}})
	parent = presentationLine("parent", 0, 2000, presentationWord("Main", 0, 1000))
	child = bg(presentationLine("child-later", 1500, 3000, presentationWord("echo", 1500, 3000)))
	child.ParentID = "parent"
	cases = append(cases, presentationOracleCase{"explicit nonadjacent parent", []lyric.Line{parent, presentationLine("between", 1000, 2000, presentationWord("Between", 1000, 2000)), child}, []string{"parent", "child-later", "between"}})
	return cases
}

func TestPresentationAgainstOriginalOptimizer(t *testing.T) {
	root := os.Getenv("AMLL_REF_ROOT")
	if root == "" {
		t.Skip("set AMLL_REF_ROOT to run the fixed TypeScript optimizer oracle")
	}
	if head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").CombinedOutput(); err != nil || strings.TrimSpace(string(head)) != "f32285864056587c4afa59d310208b0aa829cb53" {
		t.Fatalf("optimizer oracle requires the fixed reference commit: %s %v", head, err)
	}
	cases := presentationOracleCases()
	inputs := make([][]map[string]any, len(cases))
	for i, test := range cases {
		lines := test.lines
		if len(test.oracleOrder) > 0 {
			lines = nil
			for _, id := range test.oracleOrder {
				for _, line := range test.lines {
					if line.ID == id {
						lines = append(lines, line)
					}
				}
			}
		}
		inputs[i] = make([]map[string]any, 0, len(lines))
		for _, line := range lines {
			words := make([]map[string]any, 0, len(line.Words))
			for _, word := range line.Words {
				words = append(words, map[string]any{"word": word.Text, "startTime": float64(word.Start) / float64(time.Millisecond), "endTime": float64(word.End) / float64(time.Millisecond)})
			}
			inputs[i] = append(inputs[i], map[string]any{"id": line.ID, "text": line.Text, "isBG": line.Background, "isDuet": line.Duet, "startTime": float64(line.Start) / float64(time.Millisecond), "endTime": float64(line.End) / float64(time.Millisecond), "words": words})
		}
	}
	input, err := json.Marshal(struct {
		Root  string
		Cases [][]map[string]any
	}{root, inputs})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "presentation-oracle.mjs")
	if err := os.WriteFile(file, []byte(presentationOptimizerOracle), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "--experimental-transform-types", file)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil {
		t.Fatalf("Node optimizer oracle: %v\n%s", err, stderr.String())
	}
	var results []struct {
		Dynamic, HasDuet bool
		Groups           []struct {
			Main, Background string
			Start, End       float64
			BackgroundFirst  bool
		}
		Lines []struct {
			ID    string
			Words []struct {
				Word string
			}
		}
	}
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	for i, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := presentationDocument(t, test.lines)
			p, err := newPresentation(source)
			if err != nil {
				t.Fatal(err)
			}
			want := results[i]
			if p.dynamic != want.Dynamic || p.hasDuet != want.HasDuet || len(p.groups) != len(want.Groups) {
				t.Fatalf("presentation flags/group count differ: native=%+v oracle=%+v", p, want)
			}
			for j, group := range p.groups {
				expected := want.Groups[j]
				background := ""
				if group.background >= 0 {
					background = source.Line(group.background).ID()
				}
				if source.Line(group.main).ID() != expected.Main || background != expected.Background || group.backgroundFirst != expected.BackgroundFirst ||
					math.Abs(float64(group.start)/float64(time.Millisecond)-expected.Start) > 0.000001 || math.Abs(float64(group.end)/float64(time.Millisecond)-expected.End) > 0.000001 {
					t.Fatalf("group %d differs: native=%+v ids=(%s,%s), TS=%+v", j, group, source.Line(group.main).ID(), background, expected)
				}
			}
			for _, expected := range want.Lines {
				for j := 0; j < p.document.Len(); j++ {
					line := p.document.Line(j)
					if line.ID() != expected.ID {
						continue
					}
					if line.WordCount() != len(expected.Words) {
						t.Fatalf("word count differs for %s", expected.ID)
					}
					for k, word := range expected.Words {
						if line.Word(k).Text != word.Word {
							t.Fatalf("word %s/%d: native=%q TS=%q", expected.ID, k, line.Word(k).Text, word.Word)
						}
					}
				}
			}
		})
	}
	t.Logf("%d synthetic cases agree with original optimizeLyricLines, original TTML background sanitizer, and original processPipeline flags", len(cases))
}

const presentationOptimizerOracle = `import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {stripTypeScriptTypes} from 'node:module';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const {optimizeLyricLines}=await import(pathToFileURL(path.join(input.Root,'packages/core/src/utils/optimize-lyric.ts')).href);
const parser=fs.readFileSync(path.join(input.Root,'packages/ttml/src/parser.ts'),'utf8');
const bgStart=parser.indexOf('\tprivate parseBackgroundVocal('),bgEnd=parser.indexOf('\n\tprivate ',bgStart+1);
if(bgStart<0||bgEnd<bgStart)throw Error('original background sanitizer not found');
const Sanitizer=new Function('return ('+stripTypeScriptTypes('class Sanitizer {\n'+parser.slice(bgStart,bgEnd)+'\n}')+')')();
const manager=fs.readFileSync(path.join(input.Root,'packages/core/src/lyric-player/base/lyric-data-manager.ts'),'utf8');
const header='\tprivate processPipeline(): void {';
const pipelineStart=manager.indexOf(header)+header.length,pipelineEnd=manager.indexOf('\n\tprivate applyMask',pipelineStart);
const body=manager.slice(pipelineStart,pipelineEnd);
const pipeline=new Function('structuredClone','optimizeLyricLines',body.slice(0,body.lastIndexOf('}')));
const output=input.Cases.map(raw=>{
 const lines=structuredClone(raw);
 for(const line of lines)if(line.isBG){
  const sanitizer=new Sanitizer();sanitizer.parseCommonContent=()=>({text:line.text,words:line.words.map(w=>({text:w.word}))});
  const clean=sanitizer.parseBackgroundVocal(null);line.text=clean.text;clean.words.forEach((w,i)=>line.words[i].word=w.text);
 }
 const context={rawLines:lines,optimizeOpts:{},applyMask(){}};pipeline.call(context,structuredClone,optimizeLyricLines);
 const processed=context.processedLines,groups=[];
 for(let i=0;i<processed.length;i++){
  const main=processed[i];let bg;
  if(!main.isBG&&processed[i+1]?.isBG)bg=processed[++i];
  groups.push({Main:main.id,Background:bg?.id??'',Start:main.startTime,End:main.endTime,BackgroundFirst:bg?((bg.words[0]?.startTime??bg.startTime)<(main.words[0]?.startTime??main.startTime)):false});
 }
 return {Dynamic:!context.isNonDynamic,HasDuet:context.hasDuetLine,Groups:groups,Lines:processed.map(l=>({ID:l.id,Words:l.words.map(w=>({Word:w.word}))}))};
});process.stdout.write(JSON.stringify(output));`
