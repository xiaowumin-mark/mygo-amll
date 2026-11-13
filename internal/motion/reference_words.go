package motion

import (
	"math"
	"time"
)

// This file adapts the fixed AMLL float/emphasize keyframe generators. The
// Cardano easing solver follows bezier-easing 3.0.1; licenses and sources are in
// NOTICE.md. Poses describe CSS transform components before transform-origin.

type refBezier struct{ a, b, c, d, ay, by, cy float64 }

func makeRefBezier(x1, y1, x2, y2 float64) refBezier {
	a := 6 * (3*x1 - 3*x2 + 1)
	b := 6 * (x2 - 2*x1)
	c := 3 * x1
	a2, b2 := a*a, b*b
	w1 := 2*c/a - b2/a2
	return refBezier{a: 3*b*c/a2 - b2*b/(a2*a), b: 3 / a, c: w1 * w1 * w1,
		d: b / a, ay: 3*y1 - 3*y2 + 1, by: y2 - 2*y1, cy: 3 * y1}
}

func (curve refBezier) sample(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	q := curve.a + curve.b*x
	s := q*q + curve.c
	var t float64
	if s > 0 {
		root := math.Sqrt(s)
		t = math.Cbrt(q+root) + math.Cbrt(q-root) - curve.d
	} else {
		l := math.Cbrt(math.Sqrt(q*q - s))
		angle := -math.Pi / 2
		if q != 0 {
			angle = math.Atan(math.Sqrt(-s) / q)
		}
		var phi float64
		if curve.b < 0 {
			phi = math.Pi - angle
			if q > 0 {
				phi = 2*math.Pi - angle
			}
		} else if curve.d < 0 {
			phi = -3*math.Pi + angle
			if q > 0 {
				phi = 2*math.Pi + angle
			}
		} else {
			phi = math.Pi + angle
			if q > 0 {
				phi = angle
			}
		}
		t = 2*l*math.Cos(phi/3) - curve.d
	}
	return ((curve.ay*t+3*curve.by)*t + curve.cy) * t
}

var (
	refEaseCurve    = makeRefBezier(0.25, 0.1, 0.25, 1)
	refEaseOutCurve = makeRefBezier(0, 0, 0.58, 1)
	refEmpInCurve   = makeRefBezier(0.2, 0.4, 0.58, 1)
	refEmpOutCurve  = makeRefBezier(0.3, 0, 0.58, 1)
)

// Ease and EaseOut are the CSS curves used by the fixed reference. Inputs are
// clamped to [0,1]. They are stateless and allocation-free.
func Ease(progress float64) float64    { return refEaseCurve.sample(progress) }
func EaseOut(progress float64) float64 { return refEaseOutCurve.sample(progress) }

// Float is a retained ordinary-word float animation. Times are absolute song
// positions; Sample returns a vertical translation in that word's font em.
// Playback direction, pause and line disable/reverse belong to the host's
// animation clock, not to this immutable effect description.
type Float struct {
	start, duration float64 // nanoseconds, permitting reference fractional timings
	up              float64
}

func NewFloat(start, end time.Duration, background bool) Float {
	up := 0.05
	if background {
		up *= 2
	}
	return Float{start: float64(start), duration: max(float64(time.Second), float64(end)-float64(start)), up: up}
}

func (effect Float) Sample(position time.Duration) float64 {
	if effect.duration <= 0 {
		return 0
	}
	return -effect.up * EaseOut((float64(position)-effect.start)/effect.duration)
}

func (effect Float) End() time.Duration { return refDuration(effect.start + effect.duration) }
func (effect Float) Active(position time.Duration) bool {
	return float64(position) >= effect.start && float64(position) < effect.start+effect.duration
}

// EmphasisOptions describes one merged emphasis chunk. Characters counts the
// renderer's grapheme elements, not UTF-8 bytes or UTF-16 units. Last is the exact
// merged-chunk predicate selected by the reference-compatible layout layer.
type EmphasisOptions struct {
	Start, End                 time.Duration
	Characters, RubyCharacters int
	Background, Last           bool
}

// CharacterPose preserves the ordered CSS transform components. Glow is
// matrix3d(Scale) translate(TranslateXEm,TranslateYEm); FloatYEm is a separate
// additive translateY animation created after glow. ShadowBlurEm/ShadowAlpha
// describe the white text-shadow. The host applies the character's CSS-equivalent
// transform-origin and the effect-stack composition order when rendering.
type CharacterPose struct {
	Scale, TranslateXEm, TranslateYEm   float64
	FloatYEm, ShadowBlurEm, ShadowAlpha float64
}

type emphasisGlowFrame struct {
	scale, factor, y, shadowBlur, alpha float64
}

// Emphasis retains the 32 generated keyframes plus the implicit neutral offset-0
// frame. Storage is constant regardless of character count: only X translation
// and start delay vary by character. It can be shared for concurrent reads.
type Emphasis struct {
	start, duration, charDelay float64 // nanoseconds
	characters                 int
	glow                       [33]emphasisGlowFrame
	float                      [33]float64
	shadowBlur                 float64
}

func NewEmphasis(options EmphasisOptions) *Emphasis {
	du := max(float64(time.Second), float64(options.End)-float64(options.Start))
	anchor := options.RubyCharacters
	if anchor <= 0 {
		anchor = max(1, options.Characters)
	}
	amount := du / float64(2*time.Second)
	if amount > 1 {
		amount = math.Sqrt(amount)
	} else {
		amount = math.Pow(amount, 3)
	}
	blur := du / float64(3*time.Second)
	if blur > 1 {
		blur = math.Sqrt(blur)
	} else {
		blur = math.Pow(blur, 3)
	}
	amount *= 0.6
	blur *= 0.5
	if options.Last {
		amount *= 1.6
		blur *= 1.5
		du *= 1.2
	}
	amount, blur = min(1.2, amount), min(0.8, blur)
	effect := &Emphasis{start: max(0, float64(options.Start)), duration: du,
		charDelay: du / 2.5 / float64(anchor), characters: max(0, options.Characters), shadowBlur: min(0.3, blur*0.3)}
	effect.glow[0].scale = 1
	for j := 1; j <= 32; j++ {
		x := float64(j) / 32
		trans := 0.0
		if x < 0.5 {
			trans = refEmpInCurve.sample(min(1, max(0, x/0.5)))
		} else {
			trans = 1 - refEmpOutCurve.sample(min(1, max(0, (x-0.5)/0.5)))
		}
		// matrix4ToCSS(...,4) rounds the matrix before Web Animations receives
		// it. Sampling the ideal scale curve instead is observably different.
		scale := math.Round((1+trans*0.1*amount)*10000) / 10000
		effect.glow[j] = emphasisGlowFrame{scale: scale, factor: -trans * 0.03 * amount,
			y: -trans * 0.025 * amount, shadowBlur: effect.shadowBlur, alpha: trans * blur}
		y, up := math.Sin(x*math.Pi), 0.05
		if options.Background {
			y, up = y*2, up*2
		}
		effect.float[j] = -(y*0.05 + x*up)
	}
	return effect
}

func framePosition(progress float64) (int, int, float64) {
	progress = min(1, max(0, progress)) * 32
	left := int(progress)
	right := min(32, left+1)
	return left, right, progress - float64(left)
}

func mixRef(a, b, ratio float64) float64 { return a + (b-a)*ratio }

// Character linearly interpolates generated keyframes, as the reference's
// default Web Animations easing does. It does not sample the ideal continuous
// emphasis or sine curves between the 32 actual frames. Invalid indices produce
// an identity pose. No ordinary-word float is added to an emphasized character.
func (effect *Emphasis) Character(position time.Duration, index int) CharacterPose {
	if effect == nil || index < 0 || index >= effect.characters || effect.duration <= 0 {
		return CharacterPose{Scale: 1}
	}
	local := float64(position) - effect.start - effect.charDelay*float64(index)
	left, right, ratio := framePosition(local / effect.duration)
	a, b := effect.glow[left], effect.glow[right]
	fl, fr, ft := framePosition(local / (effect.duration * 1.4))
	return CharacterPose{
		Scale:        mixRef(a.scale, b.scale, ratio),
		TranslateXEm: mixRef(a.factor, b.factor, ratio) * (float64(effect.characters)/2 - float64(index)),
		TranslateYEm: mixRef(a.y, b.y, ratio), FloatYEm: mixRef(effect.float[fl], effect.float[fr], ft),
		ShadowBlurEm: mixRef(a.shadowBlur, b.shadowBlur, ratio), ShadowAlpha: mixRef(a.alpha, b.alpha, ratio),
	}
}

// ShadowBlurEm returns the generated keyframes' constant radius for cold glyph
// shadow preparation. The first implicit-neutral-to-first-frame segment samples
// a smaller radius; Character exposes it for an exact backend when required.
func (effect *Emphasis) ShadowBlurEm() float64 {
	if effect == nil {
		return 0
	}
	return effect.shadowBlur
}

func (effect *Emphasis) End() time.Duration {
	if effect == nil {
		return 0
	}
	return refDuration(effect.start + effect.charDelay*float64(max(0, effect.characters-1)) + effect.duration*1.4)
}

// CharEnd returns a single generated animation's absolute end, including its
// character stagger. extra selects the additive float (1.4× duration), otherwise
// the glow/scale animation. It is the bound used by LineAnimationClock queries.
func (effect *Emphasis) CharEnd(index int, extra bool) time.Duration {
	if effect == nil || index < 0 || index >= effect.characters {
		return 0
	}
	duration := effect.duration
	if extra {
		duration *= 1.4
	}
	return refDuration(effect.start + effect.charDelay*float64(index) + duration)
}

func (effect *Emphasis) Active(position time.Duration) bool {
	return effect != nil && effect.characters > 0 && float64(position) >= effect.start && position < effect.End()
}

func refDuration(value float64) time.Duration {
	if value >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	if value <= float64(math.MinInt64) {
		return time.Duration(math.MinInt64)
	}
	return time.Duration(math.Round(value))
}
