// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCSSInlineAuthoredFloatBoundary(t *testing.T) {
	// These decimal paddings land exactly on a physical layout unit at
	// system scale 1.25. Their float32 encodings must not lose that unit.
	for _, value := range []float32{7.575, 15.15, 30.3} {
		if got := cssLayoutLength(value, 1.25); got != value {
			t.Fatalf("authored padding=%g became %g at the physical grid boundary", value, got)
		}
	}
	below := math.Nextafter32(15.15, float32(math.Inf(-1)))
	if got := cssLayoutLength(below, 1.25); got != 15.1375 {
		t.Fatal("input-precision handling rounded a distinct value up across the physical grid")
	}
}

// Independent Chrome 157 Windows captures launched with actual system-scale
// flags 1.25 and 2. CDP deviceScaleFactor emulation preserves the system-1
// layout measurements and is a separate mechanism, not the source of this
// fixture. Font struts, explicit em leading and padding all use physical units.
func TestCSSInlineRecordedSystemDPIHeightsAndBaselines(t *testing.T) {
	heights := []struct {
		scale, size     float32
		unitless, typed [3]float32
	}{
		{1.25, 12, [3]float32{12, 14.40000057, 18}, [3]float32{12, 14.40000057, 18}},
		{1.25, 13.5, [3]float32{13.5, 16.20000076, 20.25}, [3]float32{13.5, 16.20000076, 20.25}},
		{1.25, 15, [3]float32{15, 18, 22.5}, [3]float32{15, 18, 22.5}},
		{1.25, 15.15, [3]float32{15.15000057, 18.17500114, 22.72500038}, [3]float32{15.15000057, 18.17500114, 22.72500038}},
		{1.25, 21, [3]float32{21, 25.20000076, 31.5}, [3]float32{21, 25.20000076, 31.5}},
		{1.25, 21.21, [3]float32{21.21249962, 25.45000076, 31.8125}, [3]float32{21.21249962, 25.45000076, 31.8125}},
		{1.25, 30, [3]float32{30, 36, 45}, [3]float32{30, 36, 45}},
		{1.25, 30.3, [3]float32{30.30000114, 36.35000229, 45.45000076}, [3]float32{30.30000114, 36.36249924, 45.45000076}},
		{1.25, 37, [3]float32{37, 44.40000153, 55.5}, [3]float32{37, 44.40000153, 55.5}},
		{1.25, 48, [3]float32{48, 57.60000229, 72}, [3]float32{48, 57.60000229, 72}},
		{2, 12, [3]float32{12, 14.3984375, 18}, [3]float32{12, 14.3984375, 18}},
		{2, 13.5, [3]float32{13.5, 16.1953125, 20.25}, [3]float32{13.5, 16.203125, 20.25}},
		{2, 15, [3]float32{15, 18, 22.5}, [3]float32{15, 18, 22.5}},
		{2, 15.15, [3]float32{15.1484375, 18.171875, 22.71875}, [3]float32{15.1484375, 18.1796875, 22.7265625}},
		{2, 21, [3]float32{21, 25.1953125, 31.5}, [3]float32{21, 25.203125, 31.5}},
		{2, 21.21, [3]float32{21.2109375, 25.453125, 31.8125}, [3]float32{21.2109375, 25.453125, 31.8125}},
		{2, 30, [3]float32{30, 36, 45}, [3]float32{30, 36, 45}},
		{2, 30.3, [3]float32{30.296875, 36.3515625, 45.4453125}, [3]float32{30.296875, 36.359375, 45.453125}},
		{2, 37, [3]float32{37, 44.3984375, 55.5}, [3]float32{37, 44.3984375, 55.5}},
		{2, 48, [3]float32{48, 57.59375, 72}, [3]float32{48, 57.6015625, 72}},
	}
	for _, c := range heights {
		for i, lh := range []float32{1, 1.2, 1.5} {
			f := ui.Font{Size: c.size, LineHeight: lh}
			if got := cssInlineLineHeight(f, true, c.scale); math.Abs(float64(got-c.unitless[i])) > 1e-5 {
				t.Errorf("system=%g fs=%g lh=%g unitless=%g DOM=%g", c.scale, c.size, lh, got, c.unitless[i])
			}
			if got := cssInlineLineHeight(f, false, c.scale); math.Abs(float64(got-c.typed[i])) > 1e-5 {
				t.Errorf("system=%g fs=%g lh=%g typed=%g DOM=%g", c.scale, c.size, lh, got, c.typed[i])
			}
		}
	}
	cssInlineRequireWindowsFonts(t)
	families := []struct {
		scale    float32
		family   string
		baseline [10][3]float32
	}{
		{1.25, "Segoe UI", [10][3]float32{{10.40000057, 12, 13.60000038}, {12, 13.60000038, 15.19999981}, {12.80000019, 14.40000057, 16.80000114}, {12.80000019, 14.40000057, 16.80000114}, {18.39999962, 20.80000114, 24}, {19.20000076, 20.80000114, 24}, {27.20000076, 30.39999962, 34.40000153}, {27.20000076, 30.39999962, 34.40000153}, {33.60000229, 36.79999924, 42.40000153}, {44, 48.79999924, 56}}},
		{1.25, "Arial", [10][3]float32{{10.40000057, 11.19999981, 12.80000019}, {10.40000057, 12, 14.40000057}, {12, 13.60000038, 16}, {12, 13.60000038, 16}, {17.60000038, 19.20000076, 22.39999962}, {17.60000038, 19.20000076, 22.39999962}, {24.80000114, 28, 32.79999924}, {24.80000114, 28, 32.79999924}, {31.20000076, 34.40000153, 40}, {40, 44.79999924, 52}}},
		{1.25, "Microsoft YaHei", [10][3]float32{{10.40000057, 12, 13.60000038}, {12, 13.60000038, 15.19999981}, {12.80000019, 14.40000057, 16.80000114}, {12.80000019, 14.40000057, 16.80000114}, {18.39999962, 20.80000114, 24}, {18.39999962, 20.80000114, 24}, {26.39999962, 29.60000038, 34.40000153}, {26.39999962, 29.60000038, 34.40000153}, {32.79999924, 36.79999924, 42.40000153}, {42.40000153, 47.20000076, 54.40000153}}},
		{1.25, "Times New Roman", [10][3]float32{{9.600000381, 11.19999981, 12.80000019}, {10.40000057, 12, 14.40000057}, {12, 13.60000038, 16}, {12, 13.60000038, 16}, {16.80000114, 19.20000076, 22.39999962}, {17.60000038, 19.20000076, 22.39999962}, {24.80000114, 28, 32}, {24.80000114, 28, 32.79999924}, {30.39999962, 34.40000153, 40}, {40, 44.79999924, 52}}},
		{2, "Segoe UI", [10][3]float32{{11, 12, 14}, {12, 13.5, 15.5}, {13.5, 15, 17}, {13.5, 15, 17.5}, {19, 21, 24}, {19, 21, 24.5}, {27.5, 30.5, 35}, {27.5, 30.5, 35}, {33.5, 37, 43}, {44, 48.5, 56}}},
		{2, "Arial", [10][3]float32{{10, 11, 13}, {11, 12.5, 14.5}, {12.5, 14, 16.5}, {12.5, 14, 16.5}, {17.5, 19.5, 23}, {17.5, 19.5, 23}, {25, 28, 32.5}, {25.5, 28.5, 33}, {31, 34.5, 40.5}, {40.5, 45.5, 52.5}}},
		{2, "Microsoft YaHei", [10][3]float32{{10.5, 11.5, 13.5}, {12, 13.5, 15.5}, {13.5, 15, 17}, {13.5, 15, 17}, {18.5, 20.5, 24}, {19, 21, 24}, {26.5, 29.5, 34}, {27, 30, 34.5}, {33, 36.5, 42.5}, {43, 48, 55}}},
		{2, "Times New Roman", [10][3]float32{{10, 11, 13}, {11, 12.5, 14.5}, {12.5, 14, 16.5}, {12.5, 14, 16}, {17.5, 19.5, 22.5}, {17.5, 19.5, 23}, {25, 28, 32.5}, {25, 28, 32.5}, {31, 34.5, 40}, {40, 45, 52}}},
	}
	for _, f := range families {
		for j, c := range cssInlineDOMSizes {
			for i, lh := range []float32{1, 1.2, 1.5} {
				font := ui.Font{Family: f.family, Size: c.size, Weight: 600, LineHeight: lh, Lang: "en"}
				layout := ui.ShapeText("My漢", font, 600)
				for _, unitless := range []bool{true, false} {
					_, _, dy := cssInlineLineBox(layout, font, unitless, f.scale)
					baseline := layout.Lines()[0].Baseline + dy
					if math.Abs(float64(baseline-f.baseline[j][i])) > 1e-5 {
						t.Errorf("system=%g family=%s fs=%g lh=%g unitless=%v baseline=%g DOM=%g", f.scale, f.family, c.size, lh, unitless, baseline, f.baseline[j][i])
					}
				}
				// Effects shape native glyphs at physical size and apply drawY*scale. The
				// public native baseline must scale linearly for this coordinate conversion.
				physical := font
				physical.Size *= f.scale
				physicalLayout := ui.ShapeText("My漢", physical, 600*f.scale)
				if math.Abs(float64(physicalLayout.Lines()[0].Baseline/f.scale-layout.Lines()[0].Baseline)) > 1e-5 {
					t.Fatal("physical native baseline does not match the logical drawY coordinate conversion")
				}
			}
		}
	}
}

func TestCSSInlineRecordedSystemDPIRomanRubyAndPadding(t *testing.T) {
	cssInlineRequireWindowsFonts(t)
	cases := []struct {
		scale                                                                                    float32
		family                                                                                   string
		size, bodyHeight, bodyBaseline, subHeight, subBaseline, padding, subPadding, emptyHeight float32
	}{
		{1.25, "Segoe UI", 12, 14.40000057, 12, 6, 4.800001144, 12.00000048, 6, 6},
		{1.25, "Segoe UI", 13.5, 16.20000076, 13.60000038, 6.75, 5.600000381, 13.5, 6.75, 6.75},
		{1.25, "Segoe UI", 15, 18, 14.39999962, 7.5, 6.399999619, 15, 7.5, 7.5},
		{1.25, "Segoe UI", 15.15, 18.17500114, 14.4000001, 7.575000286, 6.400001526, 15.1499989, 7.575000048, 7.575000286},
		{1.25, "Segoe UI", 21, 25.20000076, 20.80000114, 10.5, 9.599998474, 21.00000191, 10.5, 10.5},
		{1.25, "Segoe UI", 21.21, 25.45000076, 20.79999924, 10.60000038, 9.600002289, 21.20000172, 10.60000038, 10.60000038},
		{1.25, "Segoe UI", 30, 36, 30.40000153, 15, 12.79999924, 30, 15, 15},
		{1.25, "Segoe UI", 30.3, 36.35000229, 30.39999866, 15.15000057, 12.80000305, 30.29999781, 15.1500001, 15.15000057},
		{1.25, "Segoe UI", 37, 44.40000153, 36.79999924, 18.5, 16.80000305, 37.00000381, 18.5, 18.5},
		{1.25, "Segoe UI", 48, 57.60000229, 48.80000305, 24, 21.6000061, 48.00000191, 24, 24},
		{1.25, "Arial", 12, 14.40000057, 11.20000076, 6, 4.800001144, 12.00000048, 6, 6},
		{1.25, "Arial", 13.5, 16.20000076, 12, 6.75, 5.600000381, 13.5, 6.75, 6.75},
		{1.25, "Arial", 15, 18, 13.60000038, 7.5, 5.600000381, 15, 7.5, 7.5},
		{1.25, "Arial", 15.15, 18.17500114, 13.60000086, 7.575000286, 6.400001526, 15.1499989, 7.575000048, 7.575000286},
		{1.25, "Arial", 21, 25.20000076, 19.20000076, 10.5, 8.799999237, 21.00000191, 10.5, 10.5},
		{1.25, "Arial", 21.21, 25.45000076, 19.20000076, 10.60000038, 8.800003052, 21.20000172, 10.60000038, 10.60000038},
		{1.25, "Arial", 30, 36, 28, 15, 12, 30, 15, 15},
		{1.25, "Arial", 30.3, 36.35000229, 28.00000095, 15.15000057, 12, 30.29999781, 15.1500001, 15.15000057},
		{1.25, "Arial", 37, 44.40000153, 34.40000153, 18.5, 15.19999695, 37.00000381, 18.5, 18.5},
		{1.25, "Arial", 48, 57.60000229, 44.80000305, 24, 20, 48.00000191, 24, 24},
		{1.25, "Microsoft YaHei", 12, 14.40000057, 12, 6, 4.800001144, 12.00000048, 6, 6},
		{1.25, "Microsoft YaHei", 13.5, 16.20000076, 13.60000038, 6.75, 5.600000381, 13.5, 6.75, 6.75},
		{1.25, "Microsoft YaHei", 15, 18, 14.39999962, 7.5, 6.399999619, 15, 7.5, 7.5},
		{1.25, "Microsoft YaHei", 15.15, 18.17500114, 14.4000001, 7.575000286, 6.400001526, 15.1499989, 7.575000048, 7.575000286},
		{1.25, "Microsoft YaHei", 21, 25.20000076, 20.80000114, 10.5, 9.599998474, 21.00000191, 10.5, 10.5},
		{1.25, "Microsoft YaHei", 21.21, 25.45000076, 20.79999924, 10.60000038, 9.600002289, 21.20000172, 10.60000038, 10.60000038},
		{1.25, "Microsoft YaHei", 30, 36, 29.60000229, 15, 12.79999924, 30, 15, 15},
		{1.25, "Microsoft YaHei", 30.3, 36.35000229, 29.59999943, 15.15000057, 12.80000305, 30.29999781, 15.1500001, 15.15000057},
		{1.25, "Microsoft YaHei", 37, 44.40000153, 36.79999924, 18.5, 16, 37.00000381, 18.5, 18.5},
		{1.25, "Microsoft YaHei", 48, 57.60000229, 47.20000458, 24, 21.6000061, 48.00000191, 24, 24},
		{1.25, "Times New Roman", 12, 14.40000057, 11.20000076, 6, 4.800001144, 12.00000048, 6, 6},
		{1.25, "Times New Roman", 13.5, 16.20000076, 12, 6.75, 5.600000381, 13.5, 6.75, 6.75},
		{1.25, "Times New Roman", 15, 18, 13.60000038, 7.5, 5.600000381, 15, 7.5, 7.5},
		{1.25, "Times New Roman", 15.15, 18.17500114, 13.60000086, 7.575000286, 5.600000381, 15.1499989, 7.575000048, 7.575000286},
		{1.25, "Times New Roman", 21, 25.20000076, 19.20000076, 10.5, 8.799999237, 21.00000191, 10.5, 10.5},
		{1.25, "Times New Roman", 21.21, 25.45000076, 19.20000076, 10.60000038, 8.800003052, 21.20000172, 10.60000038, 10.60000038},
		{1.25, "Times New Roman", 30, 36, 28, 15, 12, 30, 15, 15},
		{1.25, "Times New Roman", 30.3, 36.35000229, 28.00000095, 15.15000057, 12, 30.29999781, 15.1500001, 15.15000057},
		{1.25, "Times New Roman", 37, 44.40000153, 34.40000153, 18.5, 15.19999695, 37.00000381, 18.5, 18.5},
		{1.25, "Times New Roman", 48, 57.60000229, 44.80000305, 24, 20, 48.00000191, 24, 24},
		{2, "Segoe UI", 12, 14.3984375, 12, 6, 5.5, 12, 6, 6},
		{2, "Segoe UI", 13.5, 16.1953125, 13.5, 6.75, 6, 13.5, 6.75, 6.75},
		{2, "Segoe UI", 15, 18, 15, 7.5, 6.5, 15, 7.5, 7.5},
		{2, "Segoe UI", 15.15, 18.171875, 15, 7.578125, 6.5, 15.1484375, 7.5703125, 7.5703125},
		{2, "Segoe UI", 21, 25.1953125, 21, 10.5, 9.5, 21, 10.5, 10.5},
		{2, "Segoe UI", 21.21, 25.453125, 21, 10.6015625, 9.5, 21.203125, 10.6015625, 10.6015625},
		{2, "Segoe UI", 30, 36, 30.5, 15, 13.5, 30, 15, 15},
		{2, "Segoe UI", 30.3, 36.3515625, 30.5, 15.1484375, 13.5, 30.296875, 15.1484375, 15.1484375},
		{2, "Segoe UI", 37, 44.3984375, 37, 18.5, 17, 37, 18.5, 18.5},
		{2, "Segoe UI", 48, 57.59375, 48.5, 24, 22, 48, 24, 24},
		{2, "Arial", 12, 14.3984375, 11, 6, 5, 12, 6, 6},
		{2, "Arial", 13.5, 16.1953125, 12.5, 6.75, 5.5, 13.5, 6.75, 6.75},
		{2, "Arial", 15, 18, 14, 7.5, 6.5, 15, 7.5, 7.5},
		{2, "Arial", 15.15, 18.171875, 14, 7.578125, 6.5, 15.1484375, 7.5703125, 7.5703125},
		{2, "Arial", 21, 25.1953125, 19.5, 10.5, 9, 21, 10.5, 10.5},
		{2, "Arial", 21.21, 25.453125, 19.5, 10.6015625, 9, 21.203125, 10.6015625, 10.6015625},
		{2, "Arial", 30, 36, 28, 15, 12.5, 30, 15, 15},
		{2, "Arial", 30.3, 36.3515625, 28.5, 15.1484375, 12.5, 30.296875, 15.1484375, 15.1484375},
		{2, "Arial", 37, 44.3984375, 34.5, 18.5, 15.5, 37, 18.5, 18.5},
		{2, "Arial", 48, 57.59375, 45.5, 24, 20, 48, 24, 24},
		{2, "Microsoft YaHei", 12, 14.3984375, 11.5, 6, 5.5, 12, 6, 6},
		{2, "Microsoft YaHei", 13.5, 16.1953125, 13.5, 6.75, 5.5, 13.5, 6.75, 6.75},
		{2, "Microsoft YaHei", 15, 18, 15, 7.5, 6.5, 15, 7.5, 7.5},
		{2, "Microsoft YaHei", 15.15, 18.171875, 15, 7.578125, 6.5, 15.1484375, 7.5703125, 7.5703125},
		{2, "Microsoft YaHei", 21, 25.1953125, 20.5, 10.5, 9.5, 21, 10.5, 10.5},
		{2, "Microsoft YaHei", 21.21, 25.453125, 21, 10.6015625, 9, 21.203125, 10.6015625, 10.6015625},
		{2, "Microsoft YaHei", 30, 36, 29.5, 15, 13.5, 30, 15, 15},
		{2, "Microsoft YaHei", 30.3, 36.3515625, 30, 15.1484375, 13.5, 30.296875, 15.1484375, 15.1484375},
		{2, "Microsoft YaHei", 37, 44.3984375, 36.5, 18.5, 16.5, 37, 18.5, 18.5},
		{2, "Microsoft YaHei", 48, 57.59375, 48, 24, 21.5, 48, 24, 24},
		{2, "Times New Roman", 12, 14.3984375, 11, 6, 5, 12, 6, 6},
		{2, "Times New Roman", 13.5, 16.1953125, 12.5, 6.75, 5.5, 13.5, 6.75, 6.75},
		{2, "Times New Roman", 15, 18, 14, 7.5, 6, 15, 7.5, 7.5},
		{2, "Times New Roman", 15.15, 18.171875, 14, 7.578125, 6.5, 15.1484375, 7.5703125, 7.5703125},
		{2, "Times New Roman", 21, 25.1953125, 19.5, 10.5, 8.5, 21, 10.5, 10.5},
		{2, "Times New Roman", 21.21, 25.453125, 19.5, 10.6015625, 8.5, 21.203125, 10.6015625, 10.6015625},
		{2, "Times New Roman", 30, 36, 28, 15, 12.5, 30, 15, 15},
		{2, "Times New Roman", 30.3, 36.3515625, 28, 15.1484375, 12.5, 30.296875, 15.1484375, 15.1484375},
		{2, "Times New Roman", 37, 44.3984375, 34.5, 18.5, 15.5, 37, 18.5, 18.5},
		{2, "Times New Roman", 48, 57.59375, 45, 24, 20, 48, 24, 24},
	}
	for _, c := range cases {
		body := ui.Font{Family: c.family, Size: c.size, Weight: 600, LineHeight: 1.2, Lang: "en"}
		sub := body
		sub.Size *= .5
		sub.LineHeight = 1
		bl, sl := ui.ShapeText("My漢", body, 600), ui.ShapeText("My漢", sub, 600)
		_, bh, bdy := cssInlineLineBox(bl, body, true, c.scale)
		_, sh, sdy := cssInlineLineBox(sl, sub, false, c.scale)
		for _, v := range []struct {
			label     string
			got, want float32
		}{
			{"body height", bh, c.bodyHeight}, {"body baseline", bl.Lines()[0].Baseline + bdy, c.bodyBaseline}, {"sub height", sh, c.subHeight}, {"sub baseline", sl.Lines()[0].Baseline + sdy, c.subBaseline},
			{"main padding", cssLayoutLength(body.Size, c.scale), c.padding}, {"sub padding", cssLayoutLength(sub.Size, c.scale), c.subPadding}, {"empty Ruby min-height", cssLayoutLength(sub.Size, c.scale), c.emptyHeight},
		} {
			if math.Abs(float64(v.got-v.want)) > 1e-5 {
				t.Errorf("system=%g family=%s fs=%g %s=%g DOM=%g", c.scale, c.family, c.size, v.label, v.got, v.want)
			}
		}
	}
}

func TestCSSInlineRecordedSystemDPIWrappedStrut(t *testing.T) {
	cases := []struct{ scale, size, height float32 }{
		{1.25, 15.15, 36.35000229},
		{1.25, 21, 50.40000153},
		{1.25, 21.21, 50.90000153},
		{1.25, 30.3, 72.70000458},
		{1.25, 37, 88.80000305},
		{2, 15.15, 36.34375},
		{2, 21, 50.390625},
		{2, 21.21, 50.90625},
		{2, 30.3, 72.703125},
		{2, 37, 88.796875},
	}
	for _, c := range cases {
		if got := 2 * cssInlineLineHeight(ui.Font{Size: c.size, LineHeight: 1.2}, true, c.scale); math.Abs(float64(got-c.height)) > 1e-5 {
			t.Errorf("system=%g fs=%g wrapped strut=%g DOM=%g", c.scale, c.size, got, c.height)
		}
	}
}
