// SPDX-License-Identifier: AGPL-3.0-only
// Internal experimental configuration; this is not a stable Native component API.
package main

import (
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

type referenceAlignAnchor string

const (
	referenceAlignTop    referenceAlignAnchor = "top"
	referenceAlignCenter referenceAlignAnchor = "center"
	referenceAlignBottom referenceAlignAnchor = "bottom"
)

// Options is a complete batch value. Begin with defaultReferenceOptions;
// optional spring members merge only when that field changes. Later focus/seek
// policy changes overwrite Y stiffness/damping, as the reference does, while
// preserving custom mass/soft. Scale overrides apply to both main and BG lines.
type referenceOptions struct {
	EnableScale, EnableBlur, EnableSpring         bool
	HidePassedLines, AlwaysPostpositionBackground bool
	AlignAnchor                                   referenceAlignAnchor
	AlignPosition, Overscan, WordFadeWidth        float64
	BackgroundFontScale                           float64
	YSpring, ScaleSpring                          referenceSpringOptions
	Foreground                                    ui.Color
	HoverColor                                    *ui.Color // nil keeps the reference's distinct hover/pressed defaults
	Presentation                                  presentationOptions
}

type referenceSpringOptions struct {
	Mass, Damping, Stiffness *float64
	Soft                     *bool
}

func defaultReferenceOptions() referenceOptions {
	return referenceOptions{EnableScale: true, EnableBlur: true, EnableSpring: true, AlignAnchor: referenceAlignCenter, AlignPosition: .35, Overscan: 300, WordFadeWidth: .5, BackgroundFontScale: .7, Foreground: ui.RGB(255, 255, 255)}
}

func cloneReferenceSpringOptions(input referenceSpringOptions) referenceSpringOptions {
	output := input
	for _, pair := range [][2]**float64{{&input.Mass, &output.Mass}, {&input.Damping, &output.Damping}, {&input.Stiffness, &output.Stiffness}} {
		if *pair[0] != nil {
			value := **pair[0]
			*pair[1] = &value
		}
	}
	if input.Soft != nil {
		value := *input.Soft
		output.Soft = &value
	}
	return output
}

func cloneReferenceOptions(options referenceOptions) referenceOptions {
	options.YSpring = cloneReferenceSpringOptions(options.YSpring)
	options.ScaleSpring = cloneReferenceSpringOptions(options.ScaleSpring)
	if options.HoverColor != nil {
		value := options.HoverColor.SRGB()
		options.HoverColor = &value
	}
	options.Foreground = options.Foreground.SRGB()
	options.Presentation = clonePresentationOptions(options.Presentation)
	return options
}

func applyReferenceSpringOptions(base motion.RefParams, options referenceSpringOptions) (motion.RefParams, error) {
	if options.Mass != nil {
		base.Mass = *options.Mass
	}
	if options.Damping != nil {
		base.Damping = *options.Damping
	}
	if options.Stiffness != nil {
		base.Stiffness = *options.Stiffness
	}
	if options.Soft != nil {
		base.Soft = *options.Soft
	}
	if !referenceFinite(base.Mass) || !referenceFinite(base.Damping) || !referenceFinite(base.Stiffness) || base.Mass <= 0 || base.Damping < 0 || base.Stiffness <= 0 {
		return motion.RefParams{}, fmt.Errorf("native options: spring mass/stiffness must be positive and damping nonnegative, all finite")
	}
	return base, nil
}

func normalizeReferenceOptions(options referenceOptions) (referenceOptions, error) {
	if options.AlignAnchor != referenceAlignTop && options.AlignAnchor != referenceAlignCenter && options.AlignAnchor != referenceAlignBottom {
		return options, fmt.Errorf("native options: unknown alignment anchor %q", options.AlignAnchor)
	}
	if !referenceFinite(options.AlignPosition) || !referenceFinite(options.Overscan) || !referenceFinite(options.WordFadeWidth) || !referenceFinite(options.BackgroundFontScale) {
		return options, fmt.Errorf("native options: alignment, overscan and word fade width must be finite")
	}
	options.WordFadeWidth = max(.0001, options.WordFadeWidth)
	// Original setOverscanPx applies JavaScript ToInt32 before clampPositive.
	integer := math.Mod(math.Trunc(options.Overscan), 4294967296)
	if integer < 0 {
		integer += 4294967296
	}
	if integer >= 2147483648 {
		integer -= 4294967296
	}
	options.Overscan = max(0, integer)
	if _, err := applyReferenceSpringOptions(motion.RefYParams(true, false, false, 0, false), options.YSpring); err != nil {
		return options, err
	}
	if _, err := applyReferenceSpringOptions(motion.RefMainScaleParams(), options.ScaleSpring); err != nil {
		return options, err
	}
	if _, err := applyReferenceSpringOptions(motion.RefBGScaleParams(), options.ScaleSpring); err != nil {
		return options, err
	}
	return cloneReferenceOptions(options), nil
}

func referenceFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func (s *scene) setReferenceOptions(now time.Time, options referenceOptions) error {
	if s.closed || s.reference == nil {
		return fmt.Errorf("native options: reference scene is not mounted")
	}
	options, err := normalizeReferenceOptions(options)
	if err != nil {
		return err
	}
	r := s.reference
	previous := r.options
	if !presentationOptionsEqual(r.options.Presentation, options.Presentation) {
		return s.enableAMLLWithOptions(r.allowSoftware, options, now)
	}
	if !reflect.DeepEqual(r.options.YSpring, options.YSpring) {
		r.yParams, err = applyReferenceSpringOptions(r.yParams, options.YSpring)
		if err != nil {
			return err
		}
		r.yParamsDirty = true
	}
	if !reflect.DeepEqual(r.options.ScaleSpring, options.ScaleSpring) {
		r.mainScaleParams, _ = applyReferenceSpringOptions(r.mainScaleParams, options.ScaleSpring)
		r.bgScaleParams, _ = applyReferenceSpringOptions(r.bgScaleParams, options.ScaleSpring)
		r.scaleParamsDirty = true
	}
	r.options = options
	if previous.WordFadeWidth != options.WordFadeWidth {
		r.fadeRevision++
	}
	if previous.AlwaysPostpositionBackground != options.AlwaysPostpositionBackground {
		r.elementRevision++
	}
	// CSS foreground/hover colors and Dom.setWordFadeWidth do not calcLayout.
	// Retargeting the unchanged Y here would cancel an in-flight stagger queue.
	layoutChanged := previous.EnableScale != options.EnableScale || previous.EnableBlur != options.EnableBlur || previous.EnableSpring != options.EnableSpring || previous.HidePassedLines != options.HidePassedLines || previous.AlwaysPostpositionBackground != options.AlwaysPostpositionBackground || previous.AlignAnchor != options.AlignAnchor || previous.AlignPosition != options.AlignPosition || previous.Overscan != options.Overscan || previous.BackgroundFontScale != options.BackgroundFontScale || r.yParamsDirty || r.scaleParamsDirty
	r.dirty = r.dirty || layoutChanged
	r.revision++
	s.updateReference(now, false)
	return nil
}

// Every frame/worker snapshot receives only self-contained numeric values.
// Optional caller pointers and UI resources never cross into the worker.
type referenceFrameOptions struct {
	configured                               bool
	disableScale, disableBlur, disableSpring bool
	hidePassed, postposition                 bool
	overscan, fadeWidth                      float64
	foreground, hoverColor                   [4]uint8
	hasHoverColor                            bool
	mainScaleParams, bgScaleParams           motion.RefParams
	widthPadding                             float64
}

func (r *referenceScene) frameOptions() referenceFrameOptions {
	o := r.options
	frame := referenceFrameOptions{configured: true, disableScale: !o.EnableScale, disableBlur: !o.EnableBlur, disableSpring: !o.EnableSpring, hidePassed: o.HidePassedLines, postposition: o.AlwaysPostpositionBackground, overscan: o.Overscan, fadeWidth: o.WordFadeWidth, foreground: referenceColorBytes(o.Foreground), mainScaleParams: r.mainScaleParams, bgScaleParams: r.bgScaleParams, widthPadding: float64(r.font.Size)}
	if o.HoverColor != nil {
		frame.hasHoverColor = true
		frame.hoverColor = referenceColorBytes(*o.HoverColor)
	}
	return frame
}

func (step referenceStep) frameOptions() referenceFrameOptions {
	if step.config.configured {
		return step.config
	}
	return referenceFrameOptions{configured: true, overscan: 300, fadeWidth: .5, foreground: [4]uint8{255, 255, 255, 255}, mainScaleParams: motion.RefMainScaleParams(), bgScaleParams: motion.RefBGScaleParams()}
}

func referenceColorBytes(color ui.Color) [4]uint8 {
	color = color.SRGB()
	return [4]uint8{color.R, color.G, color.B, color.A}
}
func referenceColorValue(color [4]uint8) ui.Color {
	return ui.RGBA(color[0], color[1], color[2], float32(color[3])/255)
}

func referenceAnchorOffset(anchor referenceAlignAnchor, height float64) float64 {
	switch anchor {
	case referenceAlignTop:
		return 0
	case referenceAlignBottom:
		return height
	default:
		return height / 2
	}
}
