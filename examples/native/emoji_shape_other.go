//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import "github.com/egoist/mygo/ui"

func initializeReferenceEmojiPlatform() {}

func (p *referenceFontProvider) shapeColor(_ string, _ ui.Font, base referenceTextShape) referenceTextShape {
	// System color-font selection and LTR paragraph shaping are currently
	// validated only on Windows. The public UI fallback remains readable.
	base.Exact = false
	return base
}
