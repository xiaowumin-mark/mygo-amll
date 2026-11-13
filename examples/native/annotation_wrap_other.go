//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import "github.com/egoist/mygo/ui"

func newReferenceAnnotationShaper(string, ui.Font, *referenceFontProvider) (referenceAnnotationShaper, error) {
	return nil, errReferenceAnnotationContext
}
