// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"os"
	"path/filepath"
)

// The executable host opens system font data once, before creating its scene.
// The provider and its borrowed color font outlive every retained geometry and
// bitmap. The release callback runs after the window has closed its scene.
// No font asset is copied into the repository or opened by a drawing callback.
func openNativeReferenceFonts() (*referenceFontProvider, func(), error) {
	windowsDirectory := os.Getenv("WINDIR")
	if windowsDirectory == "" {
		windowsDirectory = `C:\Windows`
	}
	color, err := openEmojiColorFont(filepath.Join(windowsDirectory, "Fonts", "seguiemj.ttf"))
	if err != nil {
		return nil, nil, err
	}
	provider := newReferenceFontProvider(color)
	mono := newReferenceMonoFallback()
	provider.monoFallback = mono
	return provider, func() {
		provider.Close()
		mono.Close()
		color.Close()
	}, nil
}
