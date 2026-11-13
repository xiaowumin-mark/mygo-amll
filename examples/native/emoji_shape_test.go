// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

func TestReferenceEmojiSelectionCacheBudgetAndRelease(t *testing.T) {
	if unsafe.Sizeof(referenceEmojiSelection{}) > referenceEmojiSelectionEntryEstimate {
		t.Fatal("selection-entry estimate must cover this architecture")
	}
	p := new(referenceFontProvider)
	for i := range 256 {
		key := referenceEmojiSelectionKey{text: strings.Repeat("x", 512) + strconv.Itoa(i), family: "segoe ui", features: "liga=1", lang: "en", weight: 600}
		p.rememberEmojiSelection(key, i%2 == 0)
		var cost, count int
		for _, entry := range p.selections {
			if entry.valid {
				count++
				cost += len(entry.key.text) + len(entry.key.family) + len(entry.key.features) + len(entry.key.lang)
			}
		}
		if cost != p.selectionBytes || cost > referenceEmojiSelectionDataLimit || count > referenceEmojiSelectionLimit {
			t.Fatalf("unbounded cache: bytes=%d counted=%d entries=%d", p.selectionBytes, cost, count)
		}
	}
	before := p.selectionBytes
	p.rememberEmojiSelection(referenceEmojiSelectionKey{text: strings.Repeat("x", referenceEmojiSelectionDataLimit+1)}, true)
	if p.selectionBytes != before {
		t.Fatal("oversized selection key must bypass the cache")
	}
	p.Close()
	if p.selectionBytes != 0 || p.selectionAt != 0 {
		t.Fatal("Close retained cache accounting")
	}
	for _, entry := range p.selections {
		if entry != (referenceEmojiSelection{}) {
			t.Fatal("Close retained a selection key or result")
		}
	}
}
