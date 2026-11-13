//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

// Other platforms retain public system shaping. Their color font providers and
// Native visual parity have not been validated by the Windows implementation.
func openNativeReferenceFonts() (*referenceFontProvider, func(), error) {
	return nil, func() {}, nil
}
