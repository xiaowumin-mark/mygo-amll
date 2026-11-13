//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/binary"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

var referenceFallbackGDI = syscall.NewLazyDLL("gdi32.dll")

var referenceTextEmojiBMP struct {
	sync.Once
	keys []uint16
	ok   bool
}

// The immutable sparse Unicode property keys are shared by all resolvers. The
// 512-key ceiling bounds this independent metadata to 1 KiB; no copied Unicode
// character list or extra per-primary BMP coverage map is retained.
func platformReferenceTextEmojiBMP() ([]uint16, bool) {
	referenceTextEmojiBMP.Do(func() {
		property, ok := referenceEmojiProperty("Emoji")
		if !ok {
			return
		}
		keys := make([]uint16, 0, 512)
		for sample := rune(0); sample <= 0xffff; sample++ {
			value, _, _ := referenceHasProperty.Call(uintptr(sample), uintptr(property))
			if uint8(value) == 0 {
				continue
			}
			if len(keys) == cap(keys) {
				return
			}
			keys = append(keys, uint16(sample))
		}
		referenceTextEmojiBMP.keys, referenceTextEmojiBMP.ok = keys, true
	})
	return referenceTextEmojiBMP.keys, referenceTextEmojiBMP.ok
}

func platformReferenceTextEmojiSpans(text []rune, font ui.Font, resolver *referenceMonoFallback) ([]referenceMonoEmojiSpan, bool) {
	ascii := true
	for _, sample := range text {
		if sample >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return nil, true // Ordinary ASCII digits/# retain their selected primary.
	}
	spans, _, err := referenceEmojiSpans(text, font.Family)
	if err != nil {
		return nil, false
	}
	property, ok := referenceEmojiProperty("Emoji")
	if !ok {
		return nil, false
	}
	presentation, presentationOK := referenceEmojiProperty("Emoji_Presentation")
	modifier, modifierOK := referenceEmojiProperty("Emoji_Modifier_Base")
	if !presentationOK || !modifierOK {
		return nil, false
	}
	has := func(sample rune, property int32) bool {
		value, _, _ := referenceHasProperty.Call(uintptr(sample), uintptr(property))
		return uint8(value) != 0
	}
	var out []referenceMonoEmojiSpan
	for _, span := range spans {
		if span.end-span.start == 1 && text[span.start] <= 0xffff && !strings.EqualFold(strings.TrimSpace(font.Family), "Segoe UI Emoji") {
			sample := text[span.start]
			if has(sample, presentation) || has(sample, modifier) {
				// The original emoji scanner marks modifier bases and default
				// presentation as auto emoji, but font-group glyphs precede its
				// emoji priority fallback. VS15/16 are separate explicit paths.
				_, covered, known := resolver.primaryOrCJKTextEmojiFamily(font, sample)
				if !known {
					return nil, false
				}
				span.color = !covered
			}
		}
		if span.color {
			continue
		}
		for _, sample := range text[span.start:span.end] {
			value, _, _ := referenceHasProperty.Call(uintptr(sample), uintptr(property))
			if uint8(value) != 0 {
				out = append(out, referenceMonoEmojiSpan{span.start, span.end})
				break
			}
		}
	}
	return out, true
}

// GetFontUnicodeRanges is a public, BMP-only cmap coverage query. It is called
// only while constructing a resolver. At most five fallback bitsets survive;
// five known primary bitsets are immediately reduced to sparse emoji masks.
// The caller lends one GLYPHSET buffer capped at 64 KiB/8192 ranges. Its data
// becomes an owned 8192-byte bitset, and no raw range array survives. The face
// name must match, so silent substitution cannot become coverage proof.
func platformReferenceFontCoverage(family string, scratch []byte, weights ...int) (*referenceBMPCoverage, bool) {
	createDC := referenceFallbackGDI.NewProc("CreateCompatibleDC")
	deleteDC := referenceFallbackGDI.NewProc("DeleteDC")
	createFont := referenceFallbackGDI.NewProc("CreateFontW")
	deleteObject := referenceFallbackGDI.NewProc("DeleteObject")
	selectObject := referenceFallbackGDI.NewProc("SelectObject")
	textFace := referenceFallbackGDI.NewProc("GetTextFaceW")
	unicodeRanges := referenceFallbackGDI.NewProc("GetFontUnicodeRanges")
	for _, proc := range []*syscall.LazyProc{createDC, deleteDC, createFont, deleteObject, selectObject, textFace, unicodeRanges} {
		if proc.Find() != nil {
			return nil, false
		}
	}
	name, err := syscall.UTF16PtrFromString(family)
	if err != nil {
		return nil, false
	}
	dc, _, _ := createDC.Call(0)
	if dc == 0 {
		return nil, false
	}
	defer deleteDC.Call(dc)
	height := int32(-16)
	weight := 400
	if len(weights) > 0 {
		weight = weights[0]
	}
	if weight < 100 || weight > 900 {
		return nil, false
	}
	font, _, _ := createFont.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(name)))
	if font == 0 {
		return nil, false
	}
	defer deleteObject.Call(font)
	previous, _, _ := selectObject.Call(dc, font)
	if previous == 0 || previous == ^uintptr(0) {
		return nil, false
	}
	defer selectObject.Call(dc, previous)
	var actual [128]uint16
	n, _, _ := textFace.Call(dc, uintptr(len(actual)), uintptr(unsafe.Pointer(&actual[0])))
	if n == 0 || n > uintptr(len(actual)) || !strings.EqualFold(syscall.UTF16ToString(actual[:]), family) {
		return nil, false
	}
	size, _, _ := unicodeRanges.Call(dc, 0)
	if size < 16 || size > 64<<10 || size > uintptr(len(scratch)) {
		return nil, false
	}
	buffer := scratch[:int(size)]
	binary.LittleEndian.PutUint32(buffer[:4], uint32(size))
	written, _, _ := unicodeRanges.Call(dc, uintptr(unsafe.Pointer(&buffer[0])))
	if written != size || binary.LittleEndian.Uint32(buffer[:4]) != uint32(size) {
		return nil, false
	}
	count := binary.LittleEndian.Uint32(buffer[12:16])
	if count > 8192 || uint64(16)+uint64(count)*4 > uint64(len(buffer)) {
		return nil, false
	}
	result := new(referenceBMPCoverage)
	previousEnd := rune(-1)
	for i := uint32(0); i < count; i++ {
		raw := buffer[16+i*4:]
		first, length := rune(binary.LittleEndian.Uint16(raw[:2])), rune(binary.LittleEndian.Uint16(raw[2:4]))
		if length == 0 || first+length > 0x10000 || first <= previousEnd {
			return nil, false
		}
		for c := first; c < first+length; c++ {
			result[c>>3] |= 1 << uint(c&7)
		}
		previousEnd = first + length - 1
	}
	return result, true
}

// The public Windows user locale is sampled only by the host's cold resolver
// initializer. Explicit host/page CJK language takes priority for BMP Han.
func platformReferenceFontLocale() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	if proc.Find() != nil {
		return ""
	}
	var name [85]uint16 // LOCALE_NAME_MAX_LENGTH
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if n < 1 || n > uintptr(len(name)) {
		return ""
	}
	return string(utf16.Decode(name[:n-1]))
}
