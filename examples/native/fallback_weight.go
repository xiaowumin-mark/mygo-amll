// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"runtime"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
)

// CJK family ordering is adapted from Chromium font_fallback_win.cc at
// 1aba5922274bbff7e4a15411c48daa980e2cb31b (2026-10-20), Google copyright
// 2006-2012, BSD-3-Clause; see LICENSES/chromium-font-fallback.txt and THIRD_PARTY_NOTICES.md.
// This is a deliberate public-family selection policy, not a claim that the
// pinned native backend's own fallback matches Chrome. Only known installed
// Latin primaries use it; unknown, registered and explicit CJK lists are left
// intact and reported inexact. The host creates a fresh resolver after changing
// font registrations. Shape owns no file I/O, pixel probes, or persistent glyphs.
type referenceMonoFallback struct {
	families     [5]string
	latin        []string
	registered   []string
	systemHan    referenceCJKFamily
	reliable     bool
	coverage     map[string]*referenceBMPCoverage
	emojiRunes   []uint16   // shared immutable ICU Emoji BMP keys, sorted
	emojiPrimary [9][]uint8 // each 100..900 weight has one bit per installed primary
	primaryKnown [9]uint8
}

type referenceBMPCoverage [8192]byte

type referenceMonoEmojiSpan struct{ start, end int }

type referenceCJKFamily uint8

const (
	referenceCJKUnknown referenceCJKFamily = iota
	referenceCJKSC
	referenceCJKTC
	referenceCJKJP
	referenceCJKKR
)

var referenceCJKFamilyCandidates = [5][]string{
	nil,
	{"Noto Sans SC", "Noto Sans CJK SC", "Microsoft YaHei", "simsun"},
	{"Noto Sans TC", "Noto Sans CJK TC", "Microsoft JhengHei", "pmingli"},
	{"Noto Sans JP", "Noto Sans CJK JP", "Meiryo", "Yu Gothic", "MS PGothic", "Microsoft YaHei"},
	{"Noto Sans KR", "Noto Sans CJK KR", "Malgun Gothic", "Gulim"},
}

func newReferenceMonoFallback() *referenceMonoFallback {
	r := &referenceMonoFallback{systemHan: referenceHanFamily(platformReferenceFontLocale(), referenceCJKSC)}
	if runtime.GOOS != "windows" {
		return r
	}
	installed := ui.FontFamilies()
	r.reliable = true
	nameBytes := 0
	for _, f := range installed {
		if f.Registered {
			if len(r.registered) == 64 || nameBytes+len(f.Name) > 2048 {
				r.reliable = false
				return r
			}
			r.registered = append(r.registered, strings.Clone(f.Name))
			nameBytes += len(f.Name)
		}
	}
	for _, candidates := range referenceCJKFamilyCandidates {
		for _, candidate := range candidates {
			if slices.ContainsFunc(r.registered, func(name string) bool { return strings.EqualFold(name, candidate) }) {
				r.reliable = false
				return r
			}
		}
	}
	present := func(name string) bool {
		return !slices.ContainsFunc(r.registered, func(f string) bool { return strings.EqualFold(f, name) }) && slices.ContainsFunc(installed, func(f ui.FontFamily) bool { return !f.Registered && strings.EqualFold(f.Name, name) })
	}
	for kind, candidates := range referenceCJKFamilyCandidates {
		for _, name := range candidates {
			if present(name) {
				r.families[kind] = name
				break
			}
		}
	}
	for _, name := range []string{"Segoe UI", "Arial", "Times New Roman", "Calibri", "Consolas"} {
		if present(name) {
			r.latin = append(r.latin, name)
		}
	}
	// One shared 64-KiB GLYPHSET scratch buffer is discarded after init. The
	// retained coverage is at most five 8-KiB BMP bitsets (40 KiB), plus <16 KiB
	// of bounded alias/family/emoji metadata (including shared 1-KiB ICU keys).
	// No raw range buffer or GDI handle lives
	// in this resolver, and the independent 64-MiB effect budget is unchanged.
	r.coverage = make(map[string]*referenceBMPCoverage)
	buffer := make([]byte, 64<<10)
	for _, family := range r.families {
		if family == "" || len(r.coverage) == 4 {
			continue
		}
		if _, exists := r.coverage[family]; exists {
			continue
		}
		if coverage, ok := platformReferenceFontCoverage(family, buffer); ok {
			r.coverage[family] = coverage
		}
	}
	if present("Segoe UI Symbol") {
		if coverage, ok := platformReferenceFontCoverage("Segoe UI Symbol", buffer); ok {
			r.coverage["Segoe UI Symbol"] = coverage
		}
	}
	if keys, ok := platformReferenceTextEmojiBMP(); ok {
		r.emojiRunes = keys
		// Primary coverage is weight-specific: Segoe UI Semibold, for example,
		// lacks arrows owned by its regular face. Immediately reduce each cold
		// temporary bitset to <=512 masks. Equal weight rows share their masks.
		for weight := range 9 {
			r.emojiPrimary[weight] = make([]uint8, len(keys))
			for i, family := range r.latin {
				selected := referencePrimaryCoverageFamily(family, (weight+1)*100)
				if slices.ContainsFunc(r.registered, func(name string) bool { return strings.EqualFold(name, selected) }) {
					continue
				}
				if coverage, ok := platformReferenceFontCoverage(selected, buffer, (weight+1)*100); ok {
					r.primaryKnown[weight] |= 1 << uint(i)
					for j, sample := range keys {
						if coverage[sample>>3]&(1<<uint(sample&7)) != 0 {
							r.emojiPrimary[weight][j] |= 1 << uint(i)
						}
					}
				}
			}
			for previous := 0; previous < weight; previous++ {
				if r.primaryKnown[previous] == r.primaryKnown[weight] && slices.Equal(r.emojiPrimary[previous], r.emojiPrimary[weight]) {
					r.emojiPrimary[weight] = r.emojiPrimary[previous]
					break
				}
			}
		}
	}
	return r
}

// CDP's actual Windows primary families at weights 100..900 are not always a
// GDI logical family's weight match. Query the publicly named selected family:
// GDI Segoe UI/600 otherwise reports regular arrow coverage that the actual
// browser-selected Segoe UI Semibold face does not own.
func referencePrimaryCoverageFamily(family string, weight int) string {
	switch family {
	case "Segoe UI":
		switch {
		case weight <= 300:
			return "Segoe UI Light"
		case weight == 500 || weight == 600:
			return "Segoe UI Semibold"
		case weight >= 800:
			return "Segoe UI Black"
		}
	case "Arial":
		if weight >= 800 {
			return "Arial Black"
		}
	case "Calibri":
		if weight <= 300 {
			return "Calibri Light"
		}
	}
	return family
}

func referenceHanFamily(language string, fallback referenceCJKFamily) referenceCJKFamily {
	tag := strings.ToLower(strings.ReplaceAll(language, "_", "-"))
	parts := strings.Split(tag, "-")
	switch parts[0] {
	case "ja":
		return referenceCJKJP
	case "ko":
		return referenceCJKKR
	case "zh":
		for _, part := range parts[1:] {
			if part == "hans" {
				return referenceCJKSC
			}
			if part == "hant" {
				return referenceCJKTC
			}
		}
		for _, part := range parts[1:] {
			if part == "tw" || part == "hk" || part == "mo" {
				return referenceCJKTC
			}
		}
		return referenceCJKSC
	}
	return fallback
}

func referenceCJKScriptRune(c rune) bool {
	// These CJK-block characters are presentation-sensitive emoji. Keep their
	// original mono baseline so the provider can apply VS15/VS16 selection.
	if c == 0x3030 || c == 0x303d {
		return false
	}
	return unicode.In(c, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo) || c >= 0x3000 && c <= 0x303f || c > 0xff00 && c < 0xff5f || c >= 0xff61 && c <= 0xff65
}

func (r *referenceMonoFallback) supportsPrimary(font ui.Font) bool {
	if r == nil || !r.reliable || font.Italic || len(font.Family) > 512 {
		return false
	}
	if strings.TrimSpace(font.Family) == "" {
		return slices.Contains(r.latin, "Segoe UI")
	}
	first := true
	for _, name := range strings.Split(font.Family, ",") {
		name = strings.Trim(strings.TrimSpace(name), "\"'")
		if slices.ContainsFunc(r.registered, func(v string) bool { return strings.EqualFold(v, name) }) {
			return false
		}
		if first {
			if !slices.ContainsFunc(r.latin, func(v string) bool { return strings.EqualFold(v, name) }) {
				return false
			}
			first = false
		} else if !slices.ContainsFunc(r.latin, func(v string) bool { return strings.EqualFold(v, name) }) && name != "sans-serif" && name != "serif" && name != "monospace" && name != "system-ui" {
			return false
		}
	}
	return !first
}

func (r *referenceMonoFallback) family(font ui.Font, sample rune) string {
	if !r.supportsPrimary(font) {
		return ""
	}
	// The cold public GDI coverage is BMP-only. A family name alone does not
	// establish which supplementary ideographs its actual selected face owns.
	if sample > 0xffff {
		return ""
	}
	// Chrome first tries its locale's Pan-CJK family when that face covers the
	// character. Only remaining characters enter script-specific fallback. This
	// is observable for e.g. en/zh-CN Kana -> Noto SC, but Hangul -> Malgun Gothic.
	preferred := r.families[referenceHanFamily(font.Lang, r.systemHan)]
	if preferred != "" && r.coverage[preferred] == nil {
		return "" // A failed coverage query is unknown, not proof of a miss.
	}
	if r.covers(preferred, sample) {
		return preferred
	}
	var kind referenceCJKFamily
	switch {
	case unicode.In(sample, unicode.Hangul):
		kind = referenceCJKKR
	case unicode.In(sample, unicode.Hiragana, unicode.Katakana) || sample >= 0xff61 && sample <= 0xff65:
		kind = referenceCJKJP
	case unicode.In(sample, unicode.Bopomofo):
		kind = referenceCJKTC
	case unicode.In(sample, unicode.Han) || sample >= 0x3000 && sample <= 0x303f || sample > 0xff00 && sample < 0xff5f:
		kind = referenceHanFamily(font.Lang, r.systemHan)
	default:
		return ""
	}
	family := r.families[kind]
	if r.covers(family, sample) {
		return family
	}
	return ""
}

func (r *referenceMonoFallback) covers(family string, sample rune) bool {
	coverage := r.coverage[family]
	return coverage != nil && sample >= 0 && sample <= 0xffff && coverage[sample>>3]&(1<<uint(sample&7)) != 0
}

// textEmojiFamily keeps an explicit primary's real glyph. The measured Windows
// fallback group tries its locale's Pan-CJK font before mono emoji (observable
// for U+2600 -> Noto Sans SC), then Segoe UI Symbol. Unknown coverage never
// becomes a proven miss. The empty successful result means keep the primary.
func (r *referenceMonoFallback) textEmojiFamily(font ui.Font, sample rune) (string, bool) {
	family, covered, known := r.primaryOrCJKTextEmojiFamily(font, sample)
	if !known {
		return "", false
	}
	if covered {
		return family, true
	}
	if r.covers("Segoe UI Symbol", sample) {
		return "Segoe UI Symbol", true
	}
	return "", false
}

// Auto emoji modifier bases first visit the browser's ordinary font group.
// This separates a real primary/Pan-CJK glyph from the later Symbol fallback;
// the caller can choose color only after a known miss by that ordinary group.
func (r *referenceMonoFallback) primaryOrCJKTextEmojiFamily(font ui.Font, sample rune) (family string, covered, known bool) {
	if !r.supportsPrimary(font) || sample < 0 || sample > 0xffff {
		return "", false, false
	}
	key := sort.Search(len(r.emojiRunes), func(i int) bool { return rune(r.emojiRunes[i]) >= sample })
	if key == len(r.emojiRunes) || rune(r.emojiRunes[key]) != sample {
		return "", false, false
	}
	weight := font.Weight
	if weight == 0 {
		weight = 400
	}
	if weight < 100 || weight > 900 || weight%100 != 0 {
		return "", false, false
	}
	weightIndex := weight/100 - 1
	families := font.Family
	if strings.TrimSpace(families) == "" {
		families = "Segoe UI"
	}
	for _, name := range strings.Split(families, ",") {
		name = strings.Trim(strings.TrimSpace(name), "\"'")
		i := slices.IndexFunc(r.latin, func(v string) bool { return strings.EqualFold(v, name) })
		if i < 0 {
			continue // supportsPrimary already checked any generic tail.
		}
		if r.primaryKnown[weightIndex]&(1<<uint(i)) == 0 {
			return "", false, false
		}
		if r.emojiPrimary[weightIndex][key]&(1<<uint(i)) != 0 {
			return "", true, true
		}
	}
	preferred := r.families[referenceHanFamily(font.Lang, r.systemHan)]
	if preferred != "" {
		if r.coverage[preferred] == nil {
			return "", false, false
		}
		if r.covers(preferred, sample) {
			return preferred, true, true
		}
	}
	return "", false, true
}

func (r *referenceMonoFallback) Close() {
	if r != nil {
		*r = referenceMonoFallback{}
	}
}

// Shape preserves the resolved visual order. Each replacement must cover a
// complete contiguous visual span, with no shared cluster crossing its edges.
// Its public glyphs carry their original font resources; only public positions
// and rune offsets are changed. Other scripts keep their original shaping.
func (r *referenceMonoFallback) Shape(text string, font ui.Font) ([]ui.Glyph, float32, bool) {
	glyphs, width, exact := referenceTextGlyphs(text, font)
	return r.applyPolicy(text, font, glyphs, width, exact)
}

// ShapeResolved accepts one visual run whose caller has already resolved bidi
// and removed Bidi_Control. It owns the public Shape result and never opens a
// second ICU paragraph. The same coverage/locale policy applies to both paths.
func (r *referenceMonoFallback) ShapeResolved(text string, font ui.Font, rtl bool) ([]ui.Glyph, float32, bool) {
	glyphs, width, exact := referenceResolvedTextGlyphs(text, font, rtl)
	return r.applyPolicy(text, font, glyphs, width, exact)
}

func (r *referenceMonoFallback) applyPolicy(text string, font ui.Font, glyphs []ui.Glyph, width float32, exact bool) ([]ui.Glyph, float32, bool) {
	units := []rune(text)
	type replacement struct {
		first, end int
		glyphs     []ui.Glyph
		width      float32
	}
	var replacements []replacement
	appendReplacement := func(start, end int, family string) bool {
		first, last := -1, -1
		valid := true
		for i, g := range glyphs {
			if g.Cluster >= end || g.Cluster+g.Runes <= start {
				continue
			}
			if g.Cluster < start || g.Cluster+g.Runes > end || last >= 0 && i != last+1 {
				valid = false
				break
			}
			if first < 0 {
				first = i
			}
			last = i
		}
		if first < 0 || !valid {
			return false
		}
		for _, c := range units[start:end] {
			if c == 0xfe0e { // The selector chooses presentation, not another glyph.
				continue
			}
			if !r.covers(family, c) {
				valid = false
				break
			}
		}
		if !valid {
			return false
		}
		part := string(units[start:end])
		selector := end-start == 2 && units[end-1] == 0xfe0e
		if selector {
			// Some public native CJK faces emit a missing glyph for VS15 even
			// though it is only presentation metadata. Shape the base and retain
			// both original runes in the returned cluster, as Chrome does.
			part = string(units[start : end-1])
		}
		candidate := font
		candidate.Family = family
		shaped, advance, ok := referenceResolvedTextGlyphs(part, candidate, glyphs[first].RTL)
		if family == "Segoe UI Symbol" && font.Weight != 0 && font.Weight != 400 {
			// Chrome synthesizes the requested stroke without changing advances.
			// The pinned public native shaper widens its synthetic bold. Keep its
			// requested-weight public glyph resources and use the same single-face
			// regular glyphs' public positions, clusters and advances.
			candidate.Weight = 400
			regular, regularAdvance, regularOK := referenceResolvedTextGlyphs(part, candidate, glyphs[first].RTL)
			if !regularOK || len(regular) != len(shaped) {
				ok = false
			} else {
				for i := range shaped {
					if shaped[i].Cluster != regular[i].Cluster || shaped[i].Runes != regular[i].Runes || shaped[i].RTL != regular[i].RTL {
						ok = false
					}
					shaped[i].X, shaped[i].Advance = regular[i].X, regular[i].Advance
				}
				advance = regularAdvance
			}
		}
		for i := range shaped {
			if selector {
				if shaped[i].Cluster != 0 || shaped[i].Runes != 1 {
					ok = false
				} else {
					shaped[i].Runes = 2
				}
			}
			if shaped[i].Cluster < 0 || shaped[i].Cluster+shaped[i].Runes > end-start {
				ok = false
			}
			shaped[i].Cluster += start
		}
		if !ok {
			return false
		}
		replacements = append(replacements, replacement{first, last + 1, shaped, advance})
		return true
	}
	for start := 0; start < len(units); {
		if !referenceCJKScriptRune(units[start]) {
			start++
			continue
		}
		end := start + 1
		family := r.family(font, units[start])
		for end < len(units) && (referenceCJKScriptRune(units[end]) && r.family(font, units[end]) == family || unicode.IsMark(units[end])) {
			end++
		}
		if family == "" || !appendReplacement(start, end, family) {
			exact = false
		}
		start = end
	}
	if spans, ok := platformReferenceTextEmojiSpans(units, font, r); ok {
		for _, span := range spans {
			family, bases, valid := "", 0, true
			for _, sample := range units[span.start:span.end] {
				if sample == 0xfe0e {
					continue
				}
				bases++
				selected, known := r.textEmojiFamily(font, sample)
				if !known || bases > 1 {
					valid = false
				}
				family = selected
			}
			if !valid || family != "" && !appendReplacement(span.start, span.end, family) {
				exact = false
			}
		}
	} else {
		exact = false
	}
	if len(replacements) == 0 {
		return glyphs, width, exact
	}
	slices.SortFunc(replacements, func(a, b replacement) int { return a.first - b.first })
	out := make([]ui.Glyph, 0, len(glyphs))
	delta, at := float32(0), 0
	for i := 0; i < len(glyphs); {
		if at < len(replacements) && i == replacements[at].first {
			span := replacements[at]
			left, oldWidth := glyphs[i].X, float32(0)
			for _, g := range glyphs[span.first:span.end] {
				left = min(left, g.X)
				oldWidth += g.Advance
			}
			for _, g := range span.glyphs {
				g.X += left + delta
				out = append(out, g)
			}
			delta += span.width - oldWidth
			i, at = span.end, at+1
			continue
		}
		g := glyphs[i]
		g.X += delta
		out = append(out, g)
		i++
	}
	return out, width + delta, exact
}
