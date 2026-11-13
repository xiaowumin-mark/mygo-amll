// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

func presentationBool(value bool) *bool       { return &value }
func presentationString(value string) *string { return &value }

func TestPresentationOptionsDefaultsCloningAndMaskVectors(t *testing.T) {
	options := presentationOptions{Optimize: presentationOptimizeOptions{NormalizeSpaces: presentationBool(true)}, MaskChar: presentationString("*")}
	if !presentationOptionsEqual(options, presentationOptions{}) {
		t.Fatal("explicit upstream defaults differ from omitted options")
	}
	clone := clonePresentationOptions(options)
	*options.Optimize.NormalizeSpaces, *options.MaskChar = false, "#"
	if !presentationOptionsEqual(clone, presentationOptions{}) {
		t.Fatal("external pointer mutation changed cloned options")
	}
	if _, err := resolvePresentationOptions(presentationOptions{MaskMode: "unknown"}); err == nil {
		t.Fatal("invalid mask mode accepted")
	}
	for _, test := range []struct {
		text string
		mode presentationMaskMode
		char *string
		want string
	}{
		{" A😀B ", presentationMaskFull, nil, " **** "},
		{" A😀B ", presentationMaskPartial, nil, " A**B "},
		{"😀", presentationMaskPartial, nil, "**"},
		{"😀abc😀", presentationMaskPartial, nil, "�*****�"},
		{"a\u0085\ufeffb", presentationMaskFull, nil, "**\ufeff*"},
		{"abcd", presentationMaskFull, presentationString("😀tail"), "����"},
		{"😀ab😀", presentationMaskPartial, presentationString("😀"), "����😀"},
		{"ab cd", presentationMaskFull, presentationString(""), " "},
		{"ab", presentationMaskFull, presentationString("$&"), "$$"},
	} {
		config, err := resolvePresentationOptions(presentationOptions{MaskMode: test.mode, MaskChar: test.char})
		if err != nil {
			t.Fatal(err)
		}
		if got := maskPresentationText(test.text, config); got != test.want {
			t.Fatalf("mask(%q,%s)=%q want %q", test.text, test.mode, got, test.want)
		}
	}
}

func TestPresentationOptionsRecordedTimingSwitches(t *testing.T) {
	line := presentationLine("main", 1000, 5000, presentationWord("word", 2000, 3000))
	for _, test := range []struct {
		name       string
		options    presentationOptions
		start, end time.Duration
	}{
		{"default", presentationOptions{}, 1400 * time.Millisecond, 3 * time.Second},
		{"keep line timestamps", presentationOptions{Optimize: presentationOptimizeOptions{ResetLineTimestamps: presentationBool(false)}}, 400 * time.Millisecond, 5 * time.Second},
		{"no advance", presentationOptions{Optimize: presentationOptimizeOptions{TryAdvanceStartTime: presentationBool(false)}}, 2 * time.Second, 3 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, err := newPresentationWithOptions(presentationDocument(t, []lyric.Line{line}), test.options)
			if err != nil {
				t.Fatal(err)
			}
			if p.groups[0].start != test.start || p.groups[0].end != test.end {
				t.Fatalf("timing=%+v want %v..%v", p.groups[0], test.start, test.end)
			}
		})
	}
	overlap := []lyric.Line{presentationLine("A", 0, 3000, presentationWord("A", 0, 3000)), presentationLine("B", 2950, 3950, presentationWord("B", 2950, 3950))}
	p, err := newPresentationWithOptions(presentationDocument(t, overlap), presentationOptions{Optimize: presentationOptimizeOptions{CleanUnintentionalOverlaps: presentationBool(false)}})
	if err != nil {
		t.Fatal(err)
	}
	if p.groups[0].end != 3*time.Second {
		t.Fatal("disabled overlap cleanup still clipped main line")
	}
	p, err = newPresentation(presentationDocument(t, overlap))
	if err != nil {
		t.Fatal(err)
	}
	if p.lineTimes[p.groups[0].mainLayout].end != 2950*time.Millisecond || p.document.Line(p.groups[0].mainLayout).End() != 3*time.Second {
		t.Fatal("processed timing was conflated with strict geometry bounds")
	}
	bg := presentationLine("bg", 1500, 5500, presentationWord("echo", 1500, 5500))
	bg.Background = true
	p, err = newPresentationWithOptions(presentationDocument(t, []lyric.Line{line, bg}), presentationOptions{Optimize: presentationOptimizeOptions{SyncMainAndBackgroundLines: presentationBool(false)}})
	if err != nil {
		t.Fatal(err)
	}
	g := p.groups[0]
	if g.start != 1400*time.Millisecond || g.end != 3*time.Second || p.lineTimes[g.backgroundLayout] != (presentationLineTime{1500 * time.Millisecond, 5500 * time.Millisecond}) {
		t.Fatalf("disabled synchronization was ignored: %+v %+v", g, p.lineTimes)
	}
	orphan := presentationLine("orphan", 0, 1000, presentationWord("(word)", 0, 1000))
	orphan.Background = true
	p, err = newPresentationWithOptions(presentationDocument(t, []lyric.Line{orphan}), presentationOptions{Optimize: presentationOptimizeOptions{ConvertExcessiveBackgroundLines: presentationBool(false)}})
	if err != nil {
		t.Fatal(err)
	}
	if p.groups[0].background >= 0 || p.document.Line(0).Background() {
		t.Fatal("deprecated conversion switch unexpectedly changed upstream's unconditional conversion")
	}
}

func TestPresentationOptionsIndependentTimingAndSourceOwnership(t *testing.T) {
	main := presentationLine("main", 1000, 5000, presentationWord(" A\tB ", 2000, 3000))
	main.Words[0].Obscene = true
	main.Words[0].Romanization = "roman"
	main.Words[0].Ruby = []lyric.RubyWord{{Text: "ruby", Start: 2 * time.Second, End: 3 * time.Second}}
	bg := presentationLine("bg", 1500, 5500, presentationWord("echo", 1500, 5500))
	bg.Background = true
	source := presentationDocument(t, []lyric.Line{main, bg})
	before := []lyric.Line{snapshotLine(source.Line(0)), snapshotLine(source.Line(1))}
	options := presentationOptions{Optimize: presentationOptimizeOptions{NormalizeSpaces: presentationBool(false), ResetLineTimestamps: presentationBool(false), SyncMainAndBackgroundLines: presentationBool(false), CleanUnintentionalOverlaps: presentationBool(false), TryAdvanceStartTime: presentationBool(false)}, MaskMode: presentationMaskFull}
	p, err := newPresentationWithOptions(source, options)
	if err != nil {
		t.Fatal(err)
	}
	g := p.groups[0]
	if g.start != time.Second || g.end != 5*time.Second || !g.backgroundFirst || p.lineTimes[g.mainLayout] != (presentationLineTime{time.Second, 5 * time.Second}) || p.lineTimes[g.backgroundLayout] != (presentationLineTime{1500 * time.Millisecond, 5500 * time.Millisecond}) {
		t.Fatalf("independent original timing lost: group=%+v times=%+v", g, p.lineTimes)
	}
	w := p.document.Line(g.mainLayout).Word(0)
	if w.Text != " *\t* " || w.Romanization != "roman" || w.Ruby[0].Text != "ruby" {
		t.Fatalf("mask changed whitespace or pronunciation metadata: %+v", w)
	}
	if after := []lyric.Line{snapshotLine(source.Line(0)), snapshotLine(source.Line(1))}; !reflect.DeepEqual(before, after) {
		t.Fatal("configuration changed source document")
	}
	defaultA, err := newPresentation(source)
	if err != nil {
		t.Fatal(err)
	}
	defaultB, err := newPresentationWithOptions(source, presentationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaultA, defaultB) {
		t.Fatal("default wrapper changed behavior")
	}
}

type presentationOptionOracleInput struct {
	Lines   []map[string]any
	Options map[string]any
}

type presentationOptionOracleOutput struct {
	Dynamic, HasDuet bool
	Groups           []struct {
		Main, Background string
		Start, End       float64
		BackgroundFirst  bool
	}
	Lines []struct {
		ID         string
		Start, End float64
		Words      []struct {
			Word  string
			Units []uint16
		}
	}
}

func TestPresentationOptionsAgainstOriginalDataManager(t *testing.T) {
	root := os.Getenv("AMLL_REF_ROOT")
	if root == "" {
		t.Skip("set AMLL_REF_ROOT to execute original optimizer and data-manager mask methods")
	}
	if head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").CombinedOutput(); err != nil || strings.TrimSpace(string(head)) != "f32285864056587c4afa59d310208b0aa829cb53" {
		t.Fatalf("wrong reference commit: %s %v", head, err)
	}
	if diff, err := exec.Command("git", "-C", root, "diff", "--exit-code", "--", "packages/core/src/utils/optimize-lyric.ts", "packages/core/src/lyric-player/base/lyric-data-manager.ts", "packages/ttml/src/parser.ts").CombinedOutput(); err != nil {
		t.Fatalf("modified reference source: %s %v", diff, err)
	}
	cases := presentationOracleCases()
	for _, text := range []string{" A😀B ", "😀abc😀", "a\u0085\ufeffb", "abc\u0301", "😀ab\u0301"} {
		line := presentationLine("masked", 1000, 4000, presentationWord(text, 1500, 3000))
		line.Words[0].Obscene = true
		cases = append(cases, presentationOracleCase{name: "mask " + text, lines: []lyric.Line{line}})
	}
	var inputs []presentationOptionOracleInput
	var configs []presentationOptions
	var sourceCases []presentationOracleCase
	for bits := 0; bits < 32; bits++ {
		o := presentationOptimizeOptions{NormalizeSpaces: presentationBool(bits&1 != 0), ResetLineTimestamps: presentationBool(bits&2 != 0), SyncMainAndBackgroundLines: presentationBool(bits&4 != 0), CleanUnintentionalOverlaps: presentationBool(bits&8 != 0), TryAdvanceStartTime: presentationBool(bits&16 != 0), ConvertExcessiveBackgroundLines: presentationBool(false)}
		for _, test := range cases {
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
			input := presentationOptionOracleInput{Lines: make([]map[string]any, 0, len(lines)), Options: map[string]any{"optimizeOptions": map[string]any{"normalizeSpaces": *o.NormalizeSpaces, "resetLineTimestamps": *o.ResetLineTimestamps, "syncMainAndBackgroundLines": *o.SyncMainAndBackgroundLines, "cleanUnintentionalOverlaps": *o.CleanUnintentionalOverlaps, "tryAdvanceStartTime": *o.TryAdvanceStartTime, "convertExcessiveBackgroundLines": false}}}
			config := presentationOptions{Optimize: o, MaskMode: presentationMaskPartial}
			if bits%3 == 0 {
				config.MaskMode = presentationMaskFull
				config.MaskChar = presentationString("😀tail")
			} else if bits%3 == 1 {
				config.MaskChar = presentationString("")
			}
			input.Options["maskMode"] = config.MaskMode
			if config.MaskChar != nil {
				input.Options["maskChar"] = *config.MaskChar
			}
			for _, line := range lines {
				words := make([]map[string]any, 0, len(line.Words))
				for _, word := range line.Words {
					words = append(words, map[string]any{"word": word.Text, "startTime": float64(word.Start) / float64(time.Millisecond), "endTime": float64(word.End) / float64(time.Millisecond), "obscene": word.Obscene})
				}
				input.Lines = append(input.Lines, map[string]any{"id": line.ID, "text": line.Text, "isBG": line.Background, "isDuet": line.Duet, "startTime": float64(line.Start) / float64(time.Millisecond), "endTime": float64(line.End) / float64(time.Millisecond), "words": words})
			}
			inputs = append(inputs, input)
			configs = append(configs, config)
			sourceCases = append(sourceCases, test)
		}
	}
	encoded, err := json.Marshal(struct {
		Root  string
		Cases []presentationOptionOracleInput
	}{root, inputs})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "options-oracle.mjs")
	if err := os.WriteFile(file, []byte(presentationOptionsOracle), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "--experimental-transform-types", file)
	command.Stdin = bytes.NewReader(encoded)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil {
		t.Fatalf("original TS oracle: %v\n%s", err, stderr.String())
	}
	var results []presentationOptionOracleOutput
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("oracle JSON: %v\n%s", err, data)
	}
	if len(results) != len(inputs) {
		t.Fatal("oracle result count differs")
	}
	for i, want := range results {
		test := sourceCases[i]
		source := presentationDocument(t, test.lines)
		p, err := newPresentationWithOptions(source, configs[i])
		if err != nil {
			t.Fatalf("case %d %s: %v", i, test.name, err)
		}
		if p.dynamic != want.Dynamic || p.hasDuet != want.HasDuet || len(p.groups) != len(want.Groups) {
			t.Fatalf("case %d %s: flags/groups differ", i, test.name)
		}
		for j, g := range p.groups {
			w := want.Groups[j]
			bg := ""
			if g.background >= 0 {
				bg = source.Line(g.background).ID()
			}
			if source.Line(g.main).ID() != w.Main || bg != w.Background || g.backgroundFirst != w.BackgroundFirst || math.Abs(float64(g.start)/float64(time.Millisecond)-w.Start) > 1e-6 || math.Abs(float64(g.end)/float64(time.Millisecond)-w.End) > 1e-6 {
				t.Fatalf("case %d %s group %d: Go=%+v IDs=%s/%s TS=%+v", i, test.name, j, g, source.Line(g.main).ID(), bg, w)
			}
		}
		for _, w := range want.Lines {
			for j := 0; j < p.document.Len(); j++ {
				line := p.document.Line(j)
				if line.ID() != w.ID {
					continue
				}
				timing := p.lineTimes[j]
				if math.Abs(float64(timing.start)/float64(time.Millisecond)-w.Start) > 1e-6 || math.Abs(float64(timing.end)/float64(time.Millisecond)-w.End) > 1e-6 || line.WordCount() != len(w.Words) {
					t.Fatalf("case %d %s line %s timing/count differs: Go=%+v TS=%+v", i, test.name, w.ID, timing, w)
				}
				for k, word := range w.Words {
					wantDisplay := string(utf16.Decode(word.Units))
					if line.Word(k).Text != wantDisplay {
						t.Fatalf("case %d %s word %s/%d: Go=%q TS display=%q units=%v", i, test.name, w.ID, k, line.Word(k).Text, wantDisplay, word.Units)
					}
				}
			}
		}
	}
	// Compare pre-canonical UTF-16 units separately; JSON's Go string decoder
	// intentionally replaces isolated surrogates and cannot prove this invariant.
	for i, want := range results {
		config, _ := resolvePresentationOptions(configs[i])
		for _, line := range want.Lines {
			for _, sourceLine := range sourceCases[i].lines {
				if sourceLine.ID != line.ID {
					continue
				}
				for k, word := range sourceLine.Words {
					if !word.Obscene {
						continue
					}
					text := word.Text
					if config.normalize {
						text = normalizePresentationSpaces(text)
					}
					got := maskPresentationUnits(utf16.Encode([]rune(text)), config)
					if !reflect.DeepEqual(got, line.Words[k].Units) {
						t.Fatalf("case %d raw UTF16 differs: Go=%v TS=%v", i, got, line.Words[k].Units)
					}
				}
			}
		}
	}
	t.Logf("%d option combinations/fixtures match original optimizeLyricLines and complete original LyricDataManager class methods", len(inputs))
}

const presentationOptionsOracle = `import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {stripTypeScriptTypes} from 'node:module';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const {optimizeLyricLines}=await import(pathToFileURL(path.join(input.Root,'packages/core/src/utils/optimize-lyric.ts')).href);
const {assertValidLyricTimestamps}=await import(pathToFileURL(path.join(input.Root,'packages/core/src/utils/validate-lyric.ts')).href);
const source=fs.readFileSync(path.join(input.Root,'packages/core/src/lyric-player/base/lyric-data-manager.ts'),'utf8');
const classSource=stripTypeScriptTypes(source.replace(/^import[^\n]*\n/gm,'').replace(/export /g,''));
const Manager=new Function('structuredClone','optimizeLyricLines','assertValidLyricTimestamps','MaskObsceneWordsMode',classSource+';return LyricDataManager')(structuredClone,optimizeLyricLines,assertValidLyricTimestamps,{Disabled:'',FullMask:'full-mask',PartialMask:'partial-mask'});
const parser=fs.readFileSync(path.join(input.Root,'packages/ttml/src/parser.ts'),'utf8');
const bgStart=parser.indexOf('\tprivate parseBackgroundVocal('),bgEnd=parser.indexOf('\n\tprivate ',bgStart+1);
if(bgStart<0||bgEnd<bgStart)throw Error('original background sanitizer not found');
const Sanitizer=new Function('return ('+stripTypeScriptTypes('class Sanitizer {\n'+parser.slice(bgStart,bgEnd)+'\n}')+')')();
const output=input.Cases.map(test=>{
 const lines=structuredClone(test.Lines);
 for(const line of lines)if(line.isBG){const sanitizer=new Sanitizer();sanitizer.parseCommonContent=()=>({text:line.text,words:line.words.map(w=>({text:w.word}))});const clean=sanitizer.parseBackgroundVocal(null);line.text=clean.text;clean.words.forEach((w,i)=>line.words[i].word=w.text);}
 const manager=new Manager();manager.setOriginalLines(lines);manager.setConfig(test.Options);const processed=manager.getProcessedLines(),groups=[];
 for(let i=0;i<processed.length;i++){const main=processed[i];let bg;if(!main.isBG&&processed[i+1]?.isBG)bg=processed[++i];groups.push({Main:main.id,Background:bg?.id??'',Start:main.startTime,End:main.endTime,BackgroundFirst:bg?((bg.words[0]?.startTime??bg.startTime)<(main.words[0]?.startTime??main.startTime)):false});}
 return {Dynamic:!manager.getIsNonDynamic(),HasDuet:manager.getHasDuetLine(),Groups:groups,Lines:processed.map(l=>({ID:l.id,Start:l.startTime,End:l.endTime,Words:l.words.map(w=>({Word:w.word,Units:Array.from({length:w.word.length},(_,i)=>w.word.charCodeAt(i))}))}))};
});process.stdout.write(JSON.stringify(output));`

func TestPresentationMaskCanonicalDisplayAgainstChrome(t *testing.T) {
	chrome := os.Getenv("AMLL_CHROME_BINARY")
	if chrome == "" {
		t.Skip("set AMLL_CHROME_BINARY for the actual browser surrogate/canonical display oracle")
	}
	file := filepath.Join(t.TempDir(), "canonical.html")
	if err := os.WriteFile(file, []byte(presentationCanonicalBrowserProbe), 0600); err != nil {
		t.Fatal(err)
	}
	url := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(presentationCanonicalBrowserProbe))
	command := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--user-data-dir="+filepath.Join(t.TempDir(), "profile"), "--dump-dom", url)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil {
		t.Fatalf("Chrome display oracle: %v\n%s", err, stderr.String())
	}
	match := regexp.MustCompile(`<pre id="result">([^<]*)</pre>`).FindSubmatch(data)
	if len(match) != 2 {
		t.Fatalf("Chrome oracle result missing: %s\n%s", data, stderr.String())
	}
	var result struct {
		UserAgent string
		Cases     []struct {
			RawUnits, CanonicalUnits       []uint16
			RawSegments, CanonicalSegments [][2]int
			Width, CanonicalWidth          float64
			PixelsEqual, DOMWidthEqual     bool
		}
	}
	if err := json.Unmarshal(match[1], &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Cases) != 10 {
		t.Fatalf("browser oracle returned %d cases", len(result.Cases))
	}
	for i, test := range result.Cases {
		if !reflect.DeepEqual(test.RawSegments, test.CanonicalSegments) || !test.PixelsEqual || !test.DOMWidthEqual || test.Width != test.CanonicalWidth || len(test.RawUnits) != len(test.CanonicalUnits) {
			t.Fatalf("canonical display changes browser behavior in case %d: %+v", i, test)
		}
		if got := utf16.Encode(utf16.Decode(test.RawUnits)); !reflect.DeepEqual(got, test.CanonicalUnits) {
			t.Fatalf("Go display canonicalization differs from Chrome toWellFormed: %v != %v", got, test.CanonicalUnits)
		}
	}
	t.Logf("%d actual Chrome DOM/Intl.Segmenter/canvas pixel cases agree after UTF16-to-display canonicalization; %s", len(result.Cases), result.UserAgent)
}

const presentationCanonicalBrowserProbe = `<!doctype html><meta charset="utf-8"><body><pre id="result"></pre><script>
const cases=[[0xD83D,0xD83D,0xD83D,0xD83D],[0xD83D,42,42,0xDE00],[0xD83D,0xD83D,0x0301],[0xD83D,0x0301],[0xD83D,0x200D,0xD83D],[0xD83D,0xFE0F],[0xD83D,0xD83D,97,0xDE00],[97,0xDE00,98],[0xDE00,0x0301],[0xD83D,0xDE00,97,0xD83D,0xDE00]];
const units=s=>Array.from({length:s.length},(_,i)=>s.charCodeAt(i));
const segmenter=new Intl.Segmenter('und',{granularity:'grapheme'});
const segments=s=>Array.from(segmenter.segment(s),v=>[v.index,v.segment.length]);
const raster=s=>{const c=document.createElement('canvas');c.width=400;c.height=80;const p=c.getContext('2d',{willReadFrequently:true});p.font='600 30px "Segoe UI"';p.fillStyle='white';p.fillText(s,10,45);return {width:p.measureText(s).width,pixels:p.getImageData(0,0,400,80).data};};
const dom=s=>{const el=document.createElement('span');el.style.cssText='display:inline-block;font:600 30px/36px "Segoe UI";white-space:pre';el.textContent=s;document.body.append(el);const range=document.createRange();range.selectNodeContents(el);const result={width:range.getBoundingClientRect().width,units:units(el.textContent)};el.remove();return result;};
const output=cases.map(u=>{const raw=String.fromCharCode(...u),canonical=raw.toWellFormed(),a=raster(raw),b=raster(canonical),da=dom(raw),db=dom(canonical);return {RawUnits:da.units,CanonicalUnits:db.units,RawSegments:segments(raw),CanonicalSegments:segments(canonical),Width:a.width,CanonicalWidth:b.width,PixelsEqual:a.pixels.every((v,i)=>v===b.pixels[i]),DOMWidthEqual:da.width===db.width};});
document.getElementById('result').textContent=JSON.stringify({UserAgent:navigator.userAgent,Cases:output});
</script>`
