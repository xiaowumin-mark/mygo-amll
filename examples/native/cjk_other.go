//go:build !windows

package main

// A Unicode-only fallback is intentionally distinguishable from ICU dictionary
// segmentation. It is not evidence of reference parity on these platforms.
func platformWordSegments(string) ([]nativeWordSegment, bool) { return nil, false }
func platformCJKBoundaries(string) (map[int]bool, bool)       { return nil, false }
