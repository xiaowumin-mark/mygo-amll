//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unicode"
	"unicode/utf16"
	"unsafe"
)

// Windows exposes ICU's public break-iterator API. This bridge only passes
// caller-owned UTF-16 and typed scalar pointers; it never reads UI internals.
// ICU's dictionary version is an environment input for reference comparisons.
var windowsICU = syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", "icu.dll"))
var icuOpen = windowsICU.NewProc("ubrk_open")
var icuFirst = windowsICU.NewProc("ubrk_first")
var icuNext = windowsICU.NewProc("ubrk_next")
var icuStatus = windowsICU.NewProc("ubrk_getRuleStatus")
var icuClose = windowsICU.NewProc("ubrk_close")

func platformWordSegments(text string) ([]nativeWordSegment, bool) {
	for _, proc := range []*syscall.LazyProc{icuOpen, icuFirst, icuNext, icuStatus, icuClose} {
		if proc.Find() != nil {
			return nil, false
		}
	}
	units := utf16.Encode([]rune(text))
	if len(units) == 0 {
		return nil, true
	}
	if len(units) > 1<<31-1 {
		return nil, false
	}
	// Match the reference harness locale. The word dictionary, rather than the
	// glyph shaping language, determines CJK segment boundaries.
	locale := []byte("en_US\x00")
	var status int32
	iterator, _, _ := icuOpen.Call(1, uintptr(unsafe.Pointer(&locale[0])), uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)), uintptr(unsafe.Pointer(&status)))
	if iterator == 0 || status > 0 {
		if iterator != 0 {
			icuClose.Call(iterator)
		}
		return nil, false
	}
	startRaw, _, _ := icuFirst.Call(iterator)
	start := int(int32(startRaw))
	var result []nativeWordSegment
	for {
		nextRaw, _, _ := icuNext.Call(iterator)
		next := int(int32(nextRaw))
		if next < 0 {
			break
		}
		rule, _, _ := icuStatus.Call(iterator)
		result = append(result, nativeWordSegment{start: start, end: next, wordLike: int32(rule) >= 100})
		start = next
	}
	icuClose.Call(iterator)
	runtime.KeepAlive(units)
	runtime.KeepAlive(locale)
	return result, true
}

func platformCJKBoundaries(text string) (map[int]bool, bool) {
	segments, ok := platformWordSegments(text)
	if !ok {
		return nil, false
	}
	units := utf16.Encode([]rune(text))
	result := make(map[int]bool)
	for _, segment := range segments {
		if segment.start <= 0 || !segment.wordLike {
			continue
		}
		for _, r := range utf16.Decode(units[segment.start:segment.end]) {
			unified := unicode.Properties["Unified_Ideograph"]
			if r >= 0x0800 && r <= 0x9ffc || unified != nil && unicode.Is(unified, r) {
				result[segment.start] = true
				break
			}
		}
	}
	return result, true
}
