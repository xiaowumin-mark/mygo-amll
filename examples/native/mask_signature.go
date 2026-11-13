// SPDX-License-Identifier: AGPL-3.0-only
// Mask rebuild inputs follow AMLL f32285864056587c4afa59d310208b0aa829cb53,
// packages/core/src/lyric-player/dom/lyric-line.ts:computeMaskSignature and
// utils/line-balancer.ts:computeContainerWidth. The digest is a Native cold
// cache key; it is not the upstream string encoding.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// The scene compares this only within one immutable presentation generation.
// Untimed space nodes have constant zero measurement tuples in that generation;
// retained blocks cover every mutable measured word. Font names, weight, DPI
// and colors are deliberately absent: only actual CSS measurements can rebuild
// an unchanged mask. Fade changes have a separate replayable scene revision.
func (g *lineGeometry) computeMaskSignature() [32]byte {
	h := sha256.New()
	var data [8]byte
	write := func(value uint64) {
		binary.LittleEndian.PutUint64(data[:], value)
		_, _ = h.Write(data[:])
	}
	write(uint64(len(g.blocks)))
	write(uint64(g.start))
	write(uint64(g.end))
	// clientWidth is integer CSS pixels and includes the main line's 1em
	// horizontal padding; subtracting it reproduces the measured container.
	containerWidth := math.Round(float64(g.width+2*g.font.Size)) - 2*float64(g.font.Size)
	write(math.Float64bits(containerWidth))
	write(1) // the Native reference effect path supplies a mask
	if g.dynamic {
		write(0)
	} else {
		write(1)
	}
	for _, block := range g.blocks {
		write(uint64(math.Float32bits(block.maskWidth)))
		write(uint64(math.Float32bits(block.maskHeight)))
		write(uint64(math.Float32bits(block.padding)))
	}
	var signature [32]byte
	copy(signature[:], h.Sum(signature[:0]))
	return signature
}
