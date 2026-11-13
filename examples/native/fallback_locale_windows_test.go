//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"syscall"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestReferenceGDIColdCoverageOwnershipAndResources(t *testing.T) {
	scratch := make([]byte, 64<<10)
	coverage, ok := platformReferenceFontCoverage("Noto Sans SC", scratch)
	if !ok {
		t.Skip("recorded host requires installed, verified Noto Sans SC")
	}
	snapshot := *coverage
	for i := range scratch {
		scratch[i] = 0xff
	}
	if *coverage != snapshot {
		t.Fatal("retained BMP coverage aliases the caller's scratch buffer")
	}
	if _, ok := platformReferenceFontCoverage("Uninstalled Reference Font 4d940e4d", scratch); ok {
		t.Fatal("GDI silent font substitution became coverage proof")
	}
	if _, ok := platformReferenceFontCoverage("Noto Sans SC", make([]byte, 16)); ok {
		t.Fatal("oversized GLYPHSET ignored the caller's raw buffer bound")
	}
	process := syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentProcess")
	resources := syscall.NewLazyDLL("user32.dll").NewProc("GetGuiResources")
	if process.Find() != nil || resources.Find() != nil {
		t.Fatal("public Windows resource-count API unavailable")
	}
	handle, _, _ := process.Call()
	before, _, _ := resources.Call(handle, 0) // GR_GDIOBJECTS
	for range 16 {
		for _, family := range []string{"Noto Sans SC", "Microsoft JhengHei", "Yu Gothic", "Malgun Gothic", "Segoe UI Symbol", "Segoe UI", "Arial", "Times New Roman", "Calibri", "Consolas"} {
			if _, ok := platformReferenceFontCoverage(family, scratch); !ok {
				t.Fatalf("verified recorded family %q rejected", family)
			}
		}
	}
	after, _, _ := resources.Call(handle, 0)
	if after != before {
		t.Fatalf("cold coverage leaked GDI objects: before=%d after=%d", before, after)
	}
	r := newReferenceMonoFallback()
	if len(r.coverage) > 5 {
		t.Fatal("retained more than five 8-KiB BMP coverage bitsets")
	}
	if len(r.emojiRunes) > 512 || cap(r.emojiRunes) > 512 {
		t.Fatal("shared Unicode Emoji keys exceeded their 1-KiB metadata bound")
	}
	for _, masks := range r.emojiPrimary {
		if len(masks) != len(r.emojiRunes) || cap(masks) > 512 {
			t.Fatal("a weight's sparse primary coverage exceeded its 512-byte bound")
		}
	}
	r.Close()
	r.Close()
	if r.coverage != nil || r.registered != nil || r.latin != nil || r.reliable {
		t.Fatal("Close retained owned resolver metadata")
	}
	if _, _, exact := r.Shape("我", ui.Font{Family: "Segoe UI", Size: 15, Weight: 600}); exact {
		t.Fatal("closed resolver reported a new CJK decision as exact")
	}
}
