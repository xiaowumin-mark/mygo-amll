// SPDX-License-Identifier: AGPL-3.0-only
// Derived from AMLL f32285864056587c4afa59d310208b0aa829cb53,
// packages/core/src/lyric-player/base/lyric-data-manager.ts and
// packages/core/src/utils/optimize-lyric.ts. This is an internal Native adapter.
package main

import (
	"fmt"
	"time"
	"unicode/utf16"
)

type presentationMaskMode string

const (
	presentationMaskDisabled presentationMaskMode = ""
	presentationMaskFull     presentationMaskMode = "full-mask"
	presentationMaskPartial  presentationMaskMode = "partial-mask"
)

// Nil switches select upstream defaults. ConvertExcessiveBackgroundLines is a
// deprecated no-op upstream: conversion always runs, including when false.
type presentationOptimizeOptions struct {
	NormalizeSpaces, ResetLineTimestamps            *bool
	ConvertExcessiveBackgroundLines                 *bool
	SyncMainAndBackgroundLines                      *bool
	CleanUnintentionalOverlaps, TryAdvanceStartTime *bool
}

type presentationOptions struct {
	Optimize presentationOptimizeOptions
	MaskMode presentationMaskMode
	// Nil selects "*"; a pointer to "" exactly models setConfig's empty mask.
	// The public JS single-character setter substitutes "*" for an empty input.
	MaskChar *string
}

type presentationResolvedOptions struct {
	normalize, reset, syncBackground, clean, advance bool
	maskMode                                         presentationMaskMode
	maskChar                                         uint16
	maskEmpty                                        bool
}

func resolvePresentationOptions(options presentationOptions) (presentationResolvedOptions, error) {
	if options.MaskMode != presentationMaskDisabled && options.MaskMode != presentationMaskFull && options.MaskMode != presentationMaskPartial {
		return presentationResolvedOptions{}, fmt.Errorf("native presentation: unknown mask mode %q", options.MaskMode)
	}
	on := func(value *bool) bool { return value == nil || *value }
	r := presentationResolvedOptions{normalize: on(options.Optimize.NormalizeSpaces), reset: on(options.Optimize.ResetLineTimestamps), syncBackground: on(options.Optimize.SyncMainAndBackgroundLines), clean: on(options.Optimize.CleanUnintentionalOverlaps), advance: on(options.Optimize.TryAdvanceStartTime), maskMode: options.MaskMode, maskChar: '*'}
	if options.MaskChar != nil {
		units := utf16.Encode([]rune(*options.MaskChar))
		r.maskEmpty = len(units) == 0
		if len(units) > 0 {
			r.maskChar = units[0] // JavaScript String.charAt(0), not a Unicode rune.
		}
	}
	return r, nil
}

func clonePresentationOptions(options presentationOptions) presentationOptions {
	cloneBool := func(value *bool) *bool {
		if value == nil {
			return nil
		}
		copy := *value
		return &copy
	}
	o := options.Optimize
	options.Optimize = presentationOptimizeOptions{NormalizeSpaces: cloneBool(o.NormalizeSpaces), ResetLineTimestamps: cloneBool(o.ResetLineTimestamps), ConvertExcessiveBackgroundLines: cloneBool(o.ConvertExcessiveBackgroundLines), SyncMainAndBackgroundLines: cloneBool(o.SyncMainAndBackgroundLines), CleanUnintentionalOverlaps: cloneBool(o.CleanUnintentionalOverlaps), TryAdvanceStartTime: cloneBool(o.TryAdvanceStartTime)}
	if options.MaskChar != nil {
		copy := *options.MaskChar
		options.MaskChar = &copy
	}
	return options
}

func presentationOptionsEqual(a, b presentationOptions) bool {
	ra, ea := resolvePresentationOptions(a)
	rb, eb := resolvePresentationOptions(b)
	return ea == nil && eb == nil && ra == rb
}

// Mask in UTF-16 before converting the final display text to Unicode. This
// preserves JS non-u /\S/g, trim, slice and charAt semantics, including the two
// units of an astral character. Isolated surrogates become U+FFFD only at the
// Native display boundary; the source document is never rewritten.
func maskPresentationText(text string, options presentationResolvedOptions) string {
	return string(utf16.Decode(maskPresentationUnits(utf16.Encode([]rune(text)), options)))
}

func maskPresentationUnits(units []uint16, options presentationResolvedOptions) []uint16 {
	if options.maskMode == presentationMaskDisabled {
		return units
	}
	first, last := 0, len(units)
	for first < last && referenceSpace(rune(units[first])) {
		first++
	}
	for last > first && referenceSpace(rune(units[last-1])) {
		last--
	}
	partial := options.maskMode == presentationMaskPartial && last-first > 2
	result := make([]uint16, 0, len(units))
	for i, unit := range units {
		if referenceSpace(rune(unit)) || partial && (i <= first || i >= last-1) {
			result = append(result, unit)
		} else if !options.maskEmpty {
			result = append(result, options.maskChar)
		}
	}
	return result
}

type presentationLineTime struct{ start, end time.Duration }
