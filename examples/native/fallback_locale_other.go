//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import "github.com/egoist/mygo/ui"

func platformReferenceFontLocale() string { return "" }

func platformReferenceFontCoverage(string, []byte, ...int) (*referenceBMPCoverage, bool) {
	return nil, false
}

func platformReferenceTextEmojiBMP() ([]uint16, bool) { return nil, false }

func platformReferenceTextEmojiSpans([]rune, ui.Font, *referenceMonoFallback) ([]referenceMonoEmojiSpan, bool) {
	return nil, true
}
