//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
// Emoji classification adapts Chromium 1aba5922274bbff7e4a15411c48daa980e2cb31b,
// third_party/blink/renderer/platform/text/emoji_segmentation_category_inline_header.h
// (Copyright 2019 The Chromium Authors, BSD-3-Clause), and the presentation
// categories of google/emoji-segmenter 955936be8b391e00835257059607d7c5b72ce744,
// emoji_presentation_scanner.rl. This Go implementation uses public ICU
// properties and font-coverage proofs; it does not copy the generated scanner.
//
// Copyright 2019 Google LLC
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License. Full source notices are in
// LICENSES/emoji-segmenter.txt and LICENSES/chromium-emoji-segmentation.txt.
package main

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/harfbuzz"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
)

var (
	referencePropertyEnum = windowsICU.NewProc("u_getPropertyEnum")
	referenceHasProperty  = windowsICU.NewProc("u_hasBinaryProperty")
	referenceEmojiInit    sync.Once
	referenceEmojiIDs     = map[string]int32{}
)

type referenceEmojiSpan struct {
	start, end int // original rune indices, half-open
	script     language.Script
	color      bool
}

type referenceFontRun struct {
	start, end int // original rune indices, half-open
	rtl        bool
}

// shapeColor is cold preparation. It retains no text or size-dependent shape
// cache: the scene owns bounded retained geometry and the provider owns one
// independent mutable Face over the font owner's immutable tables.
func (p *referenceFontProvider) shapeColor(text string, requested ui.Font, base referenceTextShape) referenceTextShape {
	fail := func(err error) referenceTextShape {
		base.Exact, base.Err = false, err
		return base
	}
	if !base.Exact || text == "" {
		return base
	}
	if !strings.EqualFold(strings.TrimSpace(requested.Family), "Segoe UI Emoji") {
		ascii := true
		for i := range len(text) {
			if text[i] >= utf8.RuneSelf {
				ascii = false
				break
			}
		}
		if ascii {
			return base
		}
	}
	if !utf8.ValidString(text) || len(text) > 1<<20 || strings.ContainsAny(text, "\r\n\u2028\u2029") {
		return fail(errReferenceEmojiMapping)
	}
	runes := []rune(text)
	spans, hasColor, err := p.emojiSpans(runes, requested)
	if err != nil {
		return fail(err)
	}
	if !hasColor {
		return base
	}
	if p == nil || p.color == nil || p.face == nil || p.hb == nil || p.color.face == nil {
		return fail(fmt.Errorf("%w: color font is unavailable or closed", errReferenceEmojiCapability))
	}
	if !p.supportsEmojiFamily(requested) {
		return fail(fmt.Errorf("%w: family %q", errReferenceEmojiCapability, requested.Family))
	}
	features, err := referenceEmojiFeatures(requested.Features)
	if err != nil {
		return fail(err)
	}
	for _, span := range spans {
		if span.color && !p.provesEmojiSelection(runes[span.start:span.end], requested) {
			return fail(fmt.Errorf("%w: public glyphs do not match the system emoji candidate", errReferenceEmojiCapability))
		}
	}
	scripts := referenceEmojiScripts(runes, p.face)
	for i := range spans {
		spans[i].script = scripts[spans[i].start]
		for at := spans[i].start + 1; at < spans[i].end; at++ {
			if spans[i].color && scripts[at] != spans[i].script {
				return fail(fmt.Errorf("%w: script boundary inside emoji grapheme", errReferenceEmojiMapping))
			}
		}
	}
	runs, err := referenceEmojiBidiRuns(runes)
	if err != nil {
		return fail(err)
	}
	result := referenceTextShape{Exact: true}
	for _, run := range runs {
		parts := make([]referenceEmojiSpan, 0, min(8, len(spans)))
		first := sort.Search(len(spans), func(i int) bool { return spans[i].end > run.start })
		for spanAt := first; spanAt < len(spans) && spans[spanAt].start < run.end; spanAt++ {
			span := spans[spanAt]
			if span.color && (span.start < run.start || span.end > run.end) {
				return fail(fmt.Errorf("%w: bidi boundary inside emoji grapheme", errReferenceEmojiMapping))
			}
			span.start, span.end = max(span.start, run.start), min(span.end, run.end)
			if len(parts) != 0 {
				previous := &parts[len(parts)-1]
				if previous.end == span.start && previous.color == span.color && (!span.color || previous.script == span.script) {
					previous.end = span.end
					continue
				}
			}
			parts = append(parts, span)
		}
		for partAt := range parts {
			if run.rtl {
				partAt = len(parts) - partAt - 1
			}
			part := parts[partAt]
			if part.color {
				colors, width, err := p.shapeEmojiPart(runes, part, run.rtl, requested, features)
				if err != nil {
					return fail(err)
				}
				for i := range colors {
					colors[i].X += result.Width
				}
				result.Colors = append(result.Colors, colors...)
				result.Width += width
			} else {
				glyphs, width, ok := p.shapeEmojiMonoPart(runes, part, run.rtl, requested)
				if !ok {
					return fail(errReferenceEmojiMapping)
				}
				for i := range glyphs {
					glyphs[i].X += result.Width
				}
				result.Glyphs = append(result.Glyphs, glyphs...)
				result.Width += width
			}
		}
	}
	return result
}

func (p *referenceFontProvider) supportsEmojiFamily(f ui.Font) bool {
	family := strings.ToLower(strings.TrimSpace(f.Family))
	if family != "" && !referenceEmojiPrimaryFamily(family) {
		return false
	}
	if family == "" {
		family = "segoe ui"
	}
	return !p.colorVariable && !f.Italic && p.installed[family] && p.installed["segoe ui emoji"] && !p.registered[family] && !p.registered["segoe ui emoji"]
}

func (p *referenceFontProvider) provesEmojiSelection(text []rune, requested ui.Font) bool {
	if !strings.EqualFold(p.colorFamily, "Segoe UI Emoji") {
		return false
	}
	key := referenceEmojiSelectionKey{text: string(text), family: strings.ToLower(strings.TrimSpace(requested.Family)), features: requested.Features, lang: requested.Lang, weight: requested.Weight}
	if key.family == "" {
		key.family = "segoe ui"
	}
	if key.weight == 0 {
		key.weight = 400
	}
	for _, entry := range p.selections {
		if entry.valid && entry.key == key {
			return entry.result
		}
	}
	result := p.provesEmojiSelectionUncached(text, requested)
	p.rememberEmojiSelection(key, result)
	return result
}

func (p *referenceFontProvider) provesEmojiSelectionUncached(text []rune, requested ui.Font) bool {
	for _, r := range text {
		if harfbuzz.IsDefaultIgnorable(r) {
			continue
		}
		if _, ok := p.face.NominalGlyph(r); !ok {
			return false
		}
	}
	if !p.provesPublicEmojiFace(text) {
		return false
	}
	candidate := requested
	// Selection is independent of caller scale for this non-variable system
	// emoji face. Probe at one integer size so DPR changes reuse the decision.
	requested.Size, candidate.Size = 64, 64
	candidate.Family = "Segoe UI Emoji"
	a, b := ui.Shape(string(text), requested), ui.Shape(string(text), candidate)
	matched := len(a) != 0 && len(a) == len(b)
	if matched {
		for i := range a {
			if a[i].Cluster != b[i].Cluster || a[i].Runes != b[i].Runes || a[i].RTL != b[i].RTL || math.Abs(float64(a[i].X-b[i].X)) > 1e-4 || math.Abs(float64(a[i].Advance-b[i].Advance)) > 1e-4 {
				matched = false
				break
			}
		}
	}
	if matched {
		return true
	}
	// The source's auto-emoji grammar also covers Modifier_Base. Once the
	// shared font-group policy has proved a BMP miss, emoji priority is an
	// explicit selection decision, just like VS16 below; a native per-rune
	// fallback to Symbol is not evidence against that source decision.
	if len(text) == 1 && text[0] <= 0xffff && p.monoFallback != nil {
		auto, ok := referenceEmojiProperty("Emoji_Modifier_Base")
		if ok {
			value, _, _ := referenceHasProperty.Call(uintptr(text[0]), uintptr(auto))
			if uint8(value) != 0 {
				_, covered, known := p.monoFallback.primaryOrCJKTextEmojiFamily(requested, text[0])
				if known && !covered {
					return true
				}
			}
		}
	}
	// MyGo selects fonts per rune and may retain the primary ASCII digit in a
	// keycap or a text glyph before VS16. On these explicitly supported system
	// families, whole-grapheme emoji presentation is our font-selection rule.
	// It is admitted only after the actual owner's nominal coverage and the
	// public candidate's complete glyph geometry match our independently shaped
	// face. VS15 and unknown/custom families never reach this correction.
	for i, r := range text {
		if r == 0xfe0f && i > 0 {
			property, ok := referenceEmojiProperty("Emoji")
			if !ok {
				return false
			}
			value, _, _ := referenceHasProperty.Call(uintptr(text[i-1]), uintptr(property))
			if uint8(value) != 0 {
				return true
			}
		}
	}
	return len(text) == 2 && text[1] == 0x20e3 && (text[0] >= '0' && text[0] <= '9' || text[0] == '#' || text[0] == '*')
}

func (p *referenceFontProvider) provesPublicEmojiFace(text []rune) bool {
	// A fixed integer probe size avoids comparing two different caller-size
	// rounding policies. The requested size and weight are applied only after
	// font selection; browser color emoji do not receive MyGo's fake bold.
	const probeSize = 64
	public := ui.Shape(string(text), ui.Font{Family: "Segoe UI Emoji", Size: probeSize, Weight: 400})
	// The public shaper can preserve a ligature's grapheme cluster, while
	// separately unsupported RI letters retain individual character clusters.
	// Admit either public HarfBuzz policy only when every visible slot matches.
	for _, flags := range []harfbuzz.ShappingOptions{0, harfbuzz.RemoveDefaultIgnorables} {
		for _, level := range []harfbuzz.ClusterLevel{harfbuzz.MonotoneGraphemes, harfbuzz.MonotoneCharacters} {
			buffer := harfbuzz.NewBuffer()
			buffer.ClusterLevel = level
			buffer.Flags = flags
			buffer.AddRunes(text, 0, len(text))
			buffer.Props = harfbuzz.SegmentProperties{Direction: harfbuzz.LeftToRight, Script: language.Common}
			buffer.Shape(p.hb, nil)
			if referencePublicEmojiSlotsMatch(public, text, buffer, float32(probeSize)/float32(p.face.Upem())) {
				return true
			}
		}
	}
	return false
}

func referencePublicEmojiSlotsMatch(public []ui.Glyph, text []rune, buffer *harfbuzz.Buffer, scale float32) bool {
	if len(public) == 0 || len(public) != len(buffer.Info) {
		return false
	}
	clusterEnds := make(map[int]int, len(buffer.Info))
	starts := make([]int, 0, len(buffer.Info))
	for _, info := range buffer.Info {
		if _, seen := clusterEnds[info.Cluster]; !seen {
			clusterEnds[info.Cluster] = len(text)
			starts = append(starts, info.Cluster)
		}
	}
	sort.Ints(starts)
	for i := 0; i+1 < len(starts); i++ {
		clusterEnds[starts[i]] = starts[i+1]
	}
	var pen, shift float32
	for i, info := range buffer.Info {
		pos := buffer.Pos[i]
		x, advance := pen+float32(pos.XOffset)*scale, float32(pos.XAdvance)*scale
		if i == 0 {
			shift = public[i].X - x
		}
		end := clusterEnds[info.Cluster]
		if info.Glyph == 0 || public[i].Cluster != info.Cluster || public[i].Runes < 1 || public[i].Runes > end-info.Cluster || public[i].RTL || math.Abs(float64(public[i].X-x-shift)) > 1e-4 || math.Abs(float64(public[i].Advance-advance)) > 1e-4 {
			return false
		}
		for _, r := range text[info.Cluster+public[i].Runes : end] {
			if !harfbuzz.IsDefaultIgnorable(r) {
				return false
			}
		}
		pen += advance
	}
	return true
}

func referenceEmojiFeatures(settings string) ([]harfbuzz.Feature, error) {
	var features []harfbuzz.Feature
	for _, setting := range strings.Split(settings, ",") {
		setting = strings.TrimSpace(setting)
		if setting == "" {
			continue
		}
		feature, err := harfbuzz.ParseFeature(setting)
		if err != nil || strings.ContainsAny(setting, "[]") {
			return nil, fmt.Errorf("%w: unsupported OpenType feature range", errReferenceEmojiCapability)
		}
		features = append(features, feature)
	}
	return features, nil
}

func referenceEmojiProperty(name string) (int32, bool) {
	initializeReferenceEmojiPlatform()
	property, ok := referenceEmojiIDs[name]
	return property, ok
}

func initializeReferenceEmojiPlatform() {
	referenceEmojiInit.Do(func() {
		if referencePropertyEnum.Find() != nil || referenceHasProperty.Find() != nil {
			return
		}
		for _, name := range []string{"Emoji", "Emoji_Presentation", "Emoji_Modifier_Base"} {
			nameBytes := append([]byte(name), 0)
			value, _, _ := referencePropertyEnum.Call(uintptr(unsafe.Pointer(&nameBytes[0])))
			runtime.KeepAlive(nameBytes)
			if int32(value) >= 0 {
				referenceEmojiIDs[name] = int32(value)
			}
		}
	})
}

// Chromium's emoji segmenter includes auto Modifier_Base presentation, while
// its font group can satisfy an auto BMP symbol before emoji-priority fallback.
// Keep that decision aligned with the shared mono policy and its public face
// coverage. Explicit selectors/keycaps and established supplementary sequences
// continue through their existing whole-grapheme/public-candidate proof.
func (p *referenceFontProvider) emojiSpans(text []rune, font ui.Font) ([]referenceEmojiSpan, bool, error) {
	spans, _, err := referenceEmojiSpans(text, font.Family)
	if err != nil {
		return nil, false, err
	}
	modifier, ok := referenceEmojiProperty("Emoji_Modifier_Base")
	if !ok {
		return nil, false, errReferenceEmojiCapability
	}
	defaultEmoji, ok := referenceEmojiProperty("Emoji_Presentation")
	if !ok {
		return nil, false, errReferenceEmojiCapability
	}
	explicit := strings.EqualFold(strings.TrimSpace(font.Family), "Segoe UI Emoji")
	hasColor := false
	for i := range spans {
		span := &spans[i]
		if !explicit && span.end-span.start == 1 && text[span.start] <= 0xffff {
			sample := text[span.start]
			a, _, _ := referenceHasProperty.Call(uintptr(sample), uintptr(modifier))
			b, _, _ := referenceHasProperty.Call(uintptr(sample), uintptr(defaultEmoji))
			if uint8(a) != 0 || uint8(b) != 0 {
				if p == nil || p.monoFallback == nil {
					return nil, false, errReferenceEmojiCapability
				}
				_, covered, known := p.monoFallback.primaryOrCJKTextEmojiFamily(font, sample)
				if !known {
					return nil, false, errReferenceEmojiCapability
				}
				span.color = !covered
			}
		}
		hasColor = hasColor || span.color
	}
	return spans, hasColor, nil
}

func referenceEmojiSpans(text []rune, family string) ([]referenceEmojiSpan, bool, error) {
	emoji, ok := referenceEmojiProperty("Emoji")
	if !ok {
		return nil, false, errReferenceEmojiCapability
	}
	presentation, ok := referenceEmojiProperty("Emoji_Presentation")
	if !ok {
		return nil, false, errReferenceEmojiCapability
	}
	has := func(r rune, property int32) bool {
		value, _, _ := referenceHasProperty.Call(uintptr(r), uintptr(property))
		// ICU UBool is an eight-bit C return value. The Windows x64 ABI does
		// not define the remaining register bits for this return type.
		return uint8(value) != 0
	}
	explicit := strings.EqualFold(strings.TrimSpace(family), "Segoe UI Emoji")
	var seg segmenter.Segmenter
	seg.Init(text)
	iter := seg.GraphemeIterator()
	var spans []referenceEmojiSpan
	hasColor := false
	for iter.Next() {
		cluster := iter.Grapheme()
		isEmoji, wantsColor, textSelector, colorSelector := false, false, false, false
		for _, r := range cluster.Text {
			isEmoji = isEmoji || has(r, emoji)
			wantsColor = wantsColor || has(r, presentation)
			textSelector = textSelector || r == 0xfe0e
			colorSelector = colorSelector || r == 0xfe0f || r == 0x20e3
		}
		// VS15 explicitly requests text. Mixed presentation selectors in a
		// single extended grapheme need a per-sequence font-selection API.
		if isEmoji && textSelector && colorSelector {
			return nil, false, fmt.Errorf("%w: mixed presentation selectors", errReferenceEmojiCapability)
		}
		color := isEmoji && !textSelector && (wantsColor || colorSelector || explicit)
		spans = append(spans, referenceEmojiSpan{start: cluster.Offset, end: cluster.Offset + len(cluster.Text), color: color})
		hasColor = hasColor || color
	}
	return spans, hasColor, nil
}

type referenceEmojiFaceMap struct{ face *font.Face }

func (m referenceEmojiFaceMap) ResolveFace(rune) *font.Face { return m.face }

// Script itemization runs before bidi/font boundaries. A direction override
// makes the public segmenter see one logical run without inserting a strong
// script sentinel. Existing bidi controls are Common placeholders here only;
// the actual paragraph is resolved independently, without those substitutions.
func referenceEmojiScripts(text []rune, face *font.Face) []language.Script {
	logical := make([]rune, len(text)+2)
	logical[0], logical[len(logical)-1] = 0x202d, 0x202c
	for i, r := range text {
		if referenceBidiControl(r) || r == '\u2028' || r == '\u2029' {
			r = 0x200b
		}
		logical[i+1] = r
	}
	var seg shaping.Segmenter
	runs := seg.Split(shaping.Input{Text: logical, RunEnd: len(logical), Direction: di.DirectionLTR}, referenceEmojiFaceMap{face})
	scripts := make([]language.Script, len(text))
	for _, run := range runs {
		for i := max(1, run.RunStart); i < min(len(logical)-1, run.RunEnd); i++ {
			scripts[i-1] = run.Script
		}
	}
	return scripts
}

func referenceEmojiBidiRuns(text []rune) ([]referenceFontRun, error) {
	engine := referenceWindowsBidiEngine{}
	if !engine.available() || len(text) == 0 {
		return nil, errReferenceEmojiCapability
	}
	units := utf16.Encode(text)
	indices := make([]int, len(units)+1)
	unitAt := 0
	for i, r := range text {
		indices[unitAt] = i
		if r > 0xffff {
			unitAt++
			indices[unitAt] = -1
		}
		unitAt++
	}
	indices[len(units)] = len(text)
	handle := engine.open()
	if handle == 0 {
		return nil, errReferenceEmojiCapability
	}
	defer func() { engine.close(handle); runtime.KeepAlive(units) }()
	if !engine.paragraph(handle, units) {
		return nil, errReferenceEmojiMapping
	}
	count, ok := engine.runCount(handle)
	if !ok || count < 1 || count > len(units) {
		return nil, errReferenceEmojiMapping
	}
	runs := make([]referenceFontRun, 0, count)
	covered := make([]bool, len(text))
	for i := range count {
		run, ok := engine.visualRun(handle, i)
		end := run.start + run.length
		if !ok || run.start < 0 || run.length < 1 || end > len(units) || indices[run.start] < 0 || indices[end] < 0 {
			return nil, errReferenceEmojiMapping
		}
		start, end := indices[run.start], indices[end]
		for at := start; at < end; at++ {
			if covered[at] {
				return nil, errReferenceEmojiMapping
			}
			covered[at] = true
		}
		runs = append(runs, referenceFontRun{start: start, end: end, rtl: run.rtl})
	}
	for _, ok := range covered {
		if !ok {
			return nil, errReferenceEmojiMapping
		}
	}
	return runs, nil
}

func (p *referenceFontProvider) shapeEmojiPart(text []rune, part referenceEmojiSpan, rtl bool, requested ui.Font, features []harfbuzz.Feature) ([]referenceColorGlyph, float32, error) {
	buffer := harfbuzz.NewBuffer()
	buffer.AddRunes(text, part.start, part.end-part.start)
	direction := harfbuzz.LeftToRight
	if rtl {
		direction = harfbuzz.RightToLeft
	}
	buffer.Props = harfbuzz.SegmentProperties{Direction: direction, Script: part.script, Language: language.NewLanguage(requested.Lang)}
	buffer.Shape(p.hb, features)
	size := requested.Size
	if size <= 0 {
		size = 14
	}
	scale := float32(size) / float32(p.face.Upem())
	var penX, penY float32
	glyphs := make([]referenceColorGlyph, 0, len(buffer.Info))
	for i, info := range buffer.Info {
		if info.Glyph == 0 || info.Cluster < part.start || info.Cluster >= part.end {
			return nil, 0, errReferenceEmojiMapping
		}
		pos := buffer.Pos[i]
		glyphs = append(glyphs, referenceColorGlyph{Font: p.color, ID: info.Glyph, X: penX + float32(pos.XOffset)*scale, Y: -penY - float32(pos.YOffset)*scale, Size: size})
		penX += float32(pos.XAdvance) * scale
		penY += float32(pos.YAdvance) * scale
	}
	if math.IsNaN(float64(penX)) || math.IsInf(float64(penX), 0) || penX < 0 {
		return nil, 0, errReferenceEmojiMapping
	}
	return glyphs, penX, nil
}

func (p *referenceFontProvider) shapeEmojiMonoPart(text []rune, part referenceEmojiSpan, rtl bool, requested ui.Font) ([]ui.Glyph, float32, bool) {
	body := text[part.start:part.end]
	var original []int
	for _, r := range body {
		if !referenceBidiControl(r) {
			continue
		}
		// Ordinary spans borrow the prepared rune slice directly. Only actual
		// formatting controls require a filtered buffer and original-index map.
		original = make([]int, 0, len(body))
		filtered := make([]rune, 0, len(body))
		for i, r := range body {
			if referenceBidiControl(r) {
				continue
			}
			original = append(original, part.start+i)
			filtered = append(filtered, r)
		}
		body = filtered
		break
	}
	if len(body) == 0 {
		return nil, 0, true
	}
	var glyphs []ui.Glyph
	var width float32
	var exact bool
	if p != nil && p.monoFallback != nil {
		glyphs, width, exact = p.monoFallback.ShapeResolved(string(body), requested, rtl)
	} else {
		glyphs, width, exact = referenceResolvedTextGlyphs(string(body), requested, rtl)
	}
	if !exact {
		return nil, 0, false
	}
	for i, g := range glyphs {
		if g.Cluster < 0 || g.Runes < 1 || g.Cluster+g.Runes > len(body) || g.Advance > 0 && g.RTL != rtl {
			return nil, 0, false
		}
		if original == nil {
			glyphs[i].Cluster += part.start
		} else {
			glyphs[i].Cluster = original[g.Cluster]
			glyphs[i].Runes = original[g.Cluster+g.Runes-1] - original[g.Cluster] + 1
		}
	}
	return glyphs, width, true
}
