//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

var referenceBidiOpen = windowsICU.NewProc("ubidi_open")
var referenceBidiClose = windowsICU.NewProc("ubidi_close")
var referenceBidiSetPara = windowsICU.NewProc("ubidi_setPara")
var referenceBidiCountRuns = windowsICU.NewProc("ubidi_countRuns")
var referenceBidiVisualRun = windowsICU.NewProc("ubidi_getVisualRun")

type referenceBidiRun struct {
	start, length int // UTF-16 code units in the original single paragraph
	rtl           bool
}

// The bridge owns only an ICU handle and borrows the caller's UTF-16 until close.
// Its interface also makes failure/handle-ownership tests independent of an OS.
type referenceBidiEngine interface {
	available() bool
	open() uintptr
	close(uintptr)
	paragraph(uintptr, []uint16) bool
	runCount(uintptr) (int, bool)
	visualRun(uintptr, int) (referenceBidiRun, bool)
}

type referenceWindowsBidiEngine struct{}

func (referenceWindowsBidiEngine) available() bool {
	for _, proc := range []*syscall.LazyProc{referenceBidiOpen, referenceBidiClose, referenceBidiSetPara, referenceBidiCountRuns, referenceBidiVisualRun} {
		if proc.Find() != nil {
			return false
		}
	}
	return true
}
func (referenceWindowsBidiEngine) open() uintptr {
	handle, _, _ := referenceBidiOpen.Call()
	return handle
}
func (referenceWindowsBidiEngine) close(handle uintptr) { referenceBidiClose.Call(handle) }
func (referenceWindowsBidiEngine) runCount(handle uintptr) (int, bool) {
	var status int32
	count, _, _ := referenceBidiCountRuns.Call(handle, uintptr(unsafe.Pointer(&status)))
	return int(int32(count)), status <= 0
}
func (referenceWindowsBidiEngine) paragraph(handle uintptr, units []uint16) bool {
	var status int32
	// An explicit level zero is CSS direction:ltr, not first-strong/default-LTR.
	referenceBidiSetPara.Call(handle, uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)), 0, 0, uintptr(unsafe.Pointer(&status)))
	return status <= 0
}
func (referenceWindowsBidiEngine) visualRun(handle uintptr, index int) (referenceBidiRun, bool) {
	var start, length int32
	direction, _, _ := referenceBidiVisualRun.Call(handle, uintptr(index), uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&length)))
	return referenceBidiRun{start: int(start), length: int(length), rtl: direction == 1}, direction <= 1
}

// referenceTextGlyphs is cold, UI-owned single-line preparation. Glyph X starts
// at the content's left; Cluster/Runes use original-text rune indices. Glyphs
// retain the public Shape-created font resources and draw at a caller baseline.
// exact reports successful forced-LTR bidi resolution and rune mapping, not
// identical browser font fallback, OpenType shaping, advances or raster pixels.
// Newlines, unavailable ICU and bridge/shape failures preserve public Shape as a
// readable fallback and return exact=false rather than claiming DOM parity.
func referenceTextGlyphs(text string, font ui.Font) ([]ui.Glyph, float32, bool) {
	return referenceTextGlyphsWithEngine(text, font, referenceWindowsBidiEngine{})
}

func referenceTextGlyphsWithEngine(text string, font ui.Font, engine referenceBidiEngine) ([]ui.Glyph, float32, bool) {
	fallback := func() ([]ui.Glyph, float32, bool) {
		glyphs := ui.Shape(text, font)
		var width float32
		for _, glyph := range glyphs {
			width += glyph.Advance
		}
		return glyphs, width, false
	}
	if !utf8.ValidString(text) || strings.ContainsAny(text, "\r\n\u2028\u2029") || engine == nil || !engine.available() {
		return fallback()
	}
	if text == "" {
		return nil, 0, true
	}
	if len(text) > 1<<31-1 {
		return fallback()
	}
	units := utf16.Encode([]rune(text))
	unitRunes := make([]int, len(units)+1)
	unitAt, runeAt := 0, 0
	for _, r := range text {
		unitRunes[unitAt] = runeAt
		if r > 0xffff {
			unitRunes[unitAt+1] = -1
			unitAt++
		}
		unitAt++
		runeAt++
	}
	unitRunes[len(units)] = runeAt
	handle := engine.open()
	if handle == 0 {
		return fallback()
	}
	defer func() { engine.close(handle); runtime.KeepAlive(units) }()
	if !engine.paragraph(handle, units) {
		return fallback()
	}
	count, ok := engine.runCount(handle)
	if !ok || count < 1 || count > len(units) {
		return fallback()
	}
	covered := make([]bool, len(units))
	glyphs := make([]ui.Glyph, 0, len([]rune(text)))
	var cursor float32
	for i := 0; i < count; i++ {
		run, ok := engine.visualRun(handle, i)
		if !ok || run.start < 0 || run.start >= len(units) || run.length < 1 || run.length > len(units)-run.start {
			return fallback()
		}
		end := run.start + run.length
		if unitRunes[run.start] < 0 || unitRunes[end] < 0 {
			return fallback()
		}
		for at := run.start; at < end; at++ {
			if covered[at] {
				return fallback()
			}
			covered[at] = true
		}
		original := []rune(string(utf16.Decode(units[run.start:end])))
		part := make([]rune, 0, len(original))
		originalRunes := make([]int, 0, len(original))
		for at, r := range original {
			if referenceBidiControl(r) {
				continue
			}
			part = append(part, r)
			originalRunes = append(originalRunes, unitRunes[run.start]+at)
		}
		if len(part) == 0 {
			continue
		}
		// ICU has already resolved embeddings, isolates and overrides. Remove
		// those controls and shape exactly this resolved direction, rather than
		// replaying a partial embedding stack inside each visual run. ZWSP keeps
		// the strong sentinel out of the original text's kerning/ligature context.
		prefix := "A\u200b\u202d"
		if run.rtl {
			prefix = "ע\u200b\u202e"
		}
		shaped := ui.Shape(prefix+string(part)+"\u202c", font)
		const prefixRunes = 3
		limit := prefixRunes + len(part)
		left, width := float32(0), float32(0)
		found := false
		for _, glyph := range shaped {
			if glyph.Cluster < prefixRunes || glyph.Cluster >= limit {
				if glyph.Cluster < prefixRunes && glyph.Cluster+glyph.Runes > prefixRunes {
					return fallback()
				}
				continue
			}
			if !found || glyph.X < left {
				left = glyph.X
				found = true
			}
			width += glyph.Advance
		}
		for _, glyph := range shaped {
			if glyph.Cluster < prefixRunes || glyph.Cluster >= limit {
				continue
			}
			at := glyph.Cluster - prefixRunes
			if glyph.Runes < 1 || at+glyph.Runes > len(part) || glyph.Advance > 0 && glyph.RTL != run.rtl {
				return fallback()
			}
			glyph.X += cursor - left
			glyph.Cluster = originalRunes[at]
			glyph.Runes = originalRunes[at+glyph.Runes-1] - originalRunes[at] + 1
			glyphs = append(glyphs, glyph)
		}
		cursor += width
	}
	for _, present := range covered {
		if !present {
			return fallback()
		}
	}
	return glyphs, cursor, true
}

func referenceBidiControl(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069
}
