// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"

	"github.com/egoist/mygo/ui/shader"
)

// effectKernel is owned by one UI-thread renderer entry. It retains only the
// most recently used sigma; its fixed storage covers the full supported 3-sigma
// radius. Sampling order, radius and normalization match the original shader.
type effectKernel struct {
	weights [193]float32
	sigma   float32
	radius  int32
	total   float32
	ready   bool
}

// setEffectKernel moves position-independent Gaussian work out of each shader
// fragment. Callers validate sigma against effectSigmaLimit before drawing.
// Float32 conversions preserve the shader's intermediate rounding and its
// original left-to-right summation. A nil cache uses bounded stack storage.
// Always set Values: another pass may have used the same Values with a different
// sigma even when this entry's numerical kernel itself has not changed.
func setEffectKernel(values *shader.Values, uniforms map[string]shader.Uniform, sigma float32, cache *effectKernel) {
	if sigma > effectSigmaLimit || math.IsNaN(float64(sigma)) || math.IsInf(float64(sigma), 0) {
		panic("native effects: invalid Gaussian kernel sigma")
	}
	if cache == nil {
		var local effectKernel
		cache = &local
	}
	if !cache.ready || cache.sigma != sigma {
		cache.sigma, cache.ready = sigma, true
		cache.radius, cache.total = 0, 1
		cache.weights[0] = 1
		if sigma > .00001 {
			cache.radius = int32(math.Ceil(float64(float32(3 * sigma))))
			denominator := float32(float32(2*sigma) * sigma)
			for i := int32(0); i <= cache.radius; i++ {
				exponent := float32(-float32(i*i) / denominator)
				cache.weights[i] = float32(math.Exp(float64(exponent)))
			}
			cache.total = 0
			for i := -cache.radius; i <= cache.radius; i++ {
				at := i
				if at < 0 {
					at = -at
				}
				cache.total = float32(cache.total + cache.weights[at])
			}
		}
	}
	values.Floats(uniforms["Kernel"], cache.weights[:cache.radius+1]...).Int(uniforms["KernelRadius"], cache.radius).Float(uniforms["KernelTotal"], cache.total)
}
