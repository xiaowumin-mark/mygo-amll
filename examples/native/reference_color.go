// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"time"
)

// Numeric premultiplied sRGB interpolation, including CSS reversal shortening.
// It contains no UI resource and can be copied into a scalar checkpoint.
type referenceColorTween struct {
	from, reverse [4]float64
	to            [4]uint8
	phase         referenceTween
	shortening    float64
}

func referencePremultiplied(color [4]uint8) [4]float64 {
	a := float64(color[3]) / 255
	return [4]float64{float64(color[0]) / 255 * a, float64(color[1]) / 255 * a, float64(color[2]) / 255 * a, a}
}

func (color referenceColorTween) samplePremultiplied(now time.Time) [4]float64 {
	target := referencePremultiplied(color.to)
	p := color.phase.sample(now)
	var result [4]float64
	for i := range result {
		result[i] = color.from[i] + (target[i]-color.from[i])*p
	}
	return result
}

func (color referenceColorTween) sample(now time.Time) [4]uint8 {
	p := color.samplePremultiplied(now)
	var result [4]uint8
	result[3] = uint8(math.Round(min(1, max(0, p[3])) * 255))
	if p[3] > 0 {
		for i := 0; i < 3; i++ {
			result[i] = uint8(math.Round(min(1, max(0, p[i]/p[3])) * 255))
		}
	}
	return result
}

func (color *referenceColorTween) snap(value [4]uint8) {
	color.from = referencePremultiplied(value)
	color.reverse = color.from
	color.to = value
	color.shortening = 1
	color.phase.snap(1)
}

func (color *referenceColorTween) set(now time.Time, target [4]uint8, duration time.Duration) {
	if color.to == target {
		return
	}
	current := color.samplePremultiplied(now)
	reverse, shortening := current, float64(1)
	if color.phase.running(now) && referencePremultiplied(target) == color.reverse {
		shortening = min(1, math.Abs(color.phase.sample(now)*color.shortening+(1-color.shortening)))
		reverse = referencePremultiplied(color.to)
		duration = time.Duration(math.Round(float64(duration) * shortening))
	}
	color.from, color.to, color.reverse, color.shortening = current, target, reverse, shortening
	color.phase.snap(0)
	color.phase.set(now, 1, duration, false)
}

func (color referenceColorTween) running(now time.Time) bool { return color.phase.running(now) }

// Dots put translate(x,y) and scale in one CSS transform property. A changing
// scale retargets the whole property, including an unfinished Y transition.
type referenceTransformTween struct {
	from, to, reverse [3]float64
	phase             referenceTween
	shortening        float64
}

func (transform referenceTransformTween) sample(now time.Time) [3]float64 {
	p := transform.phase.sample(now)
	var result [3]float64
	for i := range result {
		result[i] = transform.from[i] + (transform.to[i]-transform.from[i])*p
	}
	return result
}
func (transform *referenceTransformTween) snap(value [3]float64) {
	transform.from, transform.to, transform.reverse = value, value, value
	transform.shortening = 1
	transform.phase.snap(1)
}
func (transform *referenceTransformTween) set(now time.Time, target [3]float64, duration time.Duration) {
	if transform.to == target {
		return
	}
	current := transform.sample(now)
	reverse, shortening := current, float64(1)
	if transform.phase.running(now) && target == transform.reverse {
		shortening = min(1, math.Abs(transform.phase.sample(now)*transform.shortening+(1-transform.shortening)))
		reverse = transform.to
		duration = time.Duration(math.Round(float64(duration) * shortening))
	}
	transform.from, transform.to, transform.reverse, transform.shortening = current, target, reverse, shortening
	transform.phase.snap(0)
	transform.phase.set(now, 1, duration, false)
}
