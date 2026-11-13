// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

func TestEffectsCacheMetadataFitsChargedBudget(t *testing.T) {
	// Each retained shader.Values gained the kernel, radius and normalization
	// words. Include those backing words as well as the comparable cache data;
	// the typed kernel arrays are already included by reflect.Size.
	kernelWords := uintptr(len(effectKernel{}.weights)+2) * reflect.TypeFor[float32]().Size()
	identityBytes := reflect.TypeFor[effectRasterIdentity]().Size()
	lineBytes := reflect.TypeFor[effectLineCache]().Size() + 2*kernelWords + identityBytes
	groupBytes := reflect.TypeFor[effectGroupCache]().Size() + kernelWords + effectEntryLimit*identityBytes
	if lineBytes > effectLineCacheBytes || groupBytes > effectGroupCacheBytes {
		t.Fatalf("metadata charge too small: line=%d/%d group=%d/%d", lineBytes, effectLineCacheBytes, groupBytes, effectGroupCacheBytes)
	}
	if identityBytes == 0 {
		t.Fatal("identity tokens need distinct nonzero-size allocations")
	}
	t.Logf("retained cache/kernel metadata: line=%d/%dB group=%d/%dB", lineBytes, effectLineCacheBytes, groupBytes, effectGroupCacheBytes)
}

func TestEffectsCacheSignaturesCannotRetainRasterResources(t *testing.T) {
	identity := reflect.TypeFor[*effectRasterIdentity]()
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Pointer:
			if typ != identity {
				t.Fatalf("cache signature retains resource pointer %v", typ)
			}
		case reflect.Array:
			check(typ.Elem())
		case reflect.Struct:
			for i := range typ.NumField() {
				check(typ.Field(i).Type)
			}
		case reflect.Slice, reflect.Map, reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer, reflect.String:
			t.Fatalf("cache signature owns indirect state %v", typ)
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[effectSourceKey](), reflect.TypeFor[effectLineBlurKey](), reflect.TypeFor[effectLayerKey](), reflect.TypeFor[effectGroupSourceKey](), reflect.TypeFor[effectLineCache](), reflect.TypeFor[effectGroupCache]()} {
		check(typ)
	}
}

func TestEffectsGroupSignatureInputsAndReplacementIdentity(t *testing.T) {
	g := &lineGeometry{dynamic: true, end: 4 * time.Second}
	entry := &effectLine{geometry: g}
	entry.initPaintCache()
	entry.cache.revision = 7
	layer := effectGroupLayer{Geometry: g, X: 1, Y: 2, Scale: .97, OriginX: 3, OriginY: 4, Style: effectStyle{Opacity: .7, BrightOpacity: .6, DimOpacity: .2, SubOpacity: .3, Color: ui.RGB(20, 60, 100), HasColor: true}}
	background := effectGroupBackground{Rect: ui.Rect{X: -1, Y: 2, W: 30, H: 20}, Radius: 2, Color: ui.Color{R: 200, G: 100, B: 50, A: 10}}
	state := func(value effectGroupLayer, candidate *effectLine, bg effectGroupBackground) effectGroupSourceKey {
		return effectGroupState([]effectGroupLayer{value}, []*effectLine{candidate}, bg)
	}
	base := state(layer, entry, background)
	for _, test := range []struct {
		name   string
		change func(*effectGroupLayer)
	}{
		{"x", func(l *effectGroupLayer) { l.X += .125 }},
		{"y", func(l *effectGroupLayer) { l.Y += .125 }},
		{"scale", func(l *effectGroupLayer) { l.Scale += .001 }},
		{"origin-x", func(l *effectGroupLayer) { l.OriginX += .125 }},
		{"origin-y", func(l *effectGroupLayer) { l.OriginY += .125 }},
		{"opacity", func(l *effectGroupLayer) { l.Style.Opacity += .1 }},
		{"bright", func(l *effectGroupLayer) { l.Style.BrightOpacity += .1 }},
		{"dim", func(l *effectGroupLayer) { l.Style.DimOpacity += .1 }},
		{"annotations", func(l *effectGroupLayer) { l.Style.SubOpacity += .1 }},
		{"foreground", func(l *effectGroupLayer) { l.Style.Color.R++ }},
		{"foreground-alpha", func(l *effectGroupLayer) { l.Style.Color.A-- }},
		{"transparent-layer", func(l *effectGroupLayer) { l.Style.Opacity = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := layer
			test.change(&changed)
			if base == state(changed, entry, background) {
				t.Fatal("changed group input reused its source signature")
			}
		})
	}
	for _, test := range []struct {
		name   string
		change func(*effectGroupBackground)
	}{
		{"background-x", func(b *effectGroupBackground) { b.Rect.X++ }},
		{"background-y", func(b *effectGroupBackground) { b.Rect.Y++ }},
		{"background-width", func(b *effectGroupBackground) { b.Rect.W++ }},
		{"background-height", func(b *effectGroupBackground) { b.Rect.H++ }},
		{"background-radius", func(b *effectGroupBackground) { b.Radius++ }},
		{"background-color", func(b *effectGroupBackground) { b.Color.G++ }},
		{"background-alpha", func(b *effectGroupBackground) { b.Color.A++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := background
			test.change(&changed)
			if base == state(layer, entry, changed) {
				t.Fatal("changed wrapper input reused its source signature")
			}
		})
	}
	entry.cache.revision++
	if base == state(layer, entry, background) {
		t.Fatal("new child source revision reused the group source")
	}
	replacement := &effectLine{geometry: g}
	replacement.initPaintCache()
	replacement.cache.revision = 7
	if base == state(layer, replacement, background) || replacement.cache.identity == entry.cache.identity {
		t.Fatal("replacement atlas reused an old identity at the same revision")
	}
	// Hidden inputs do not read even a nil entry, nor preserve stale source
	// tokens. A fully transparent wrapper also has no raster contribution.
	layer.Style.Opacity = 0
	background.Color.A = 0
	if state(layer, nil, background) != (effectGroupSourceKey{}) {
		t.Fatal("invisible inputs retained a cache dependency")
	}
}

func TestEffectsSourceSignatureInputsAndFrozenClock(t *testing.T) {
	g := &lineGeometry{dynamic: true, end: 4 * time.Second}
	entry := &effectLine{geometry: g, words: []effectWord{{float: motion.NewFloat(0, time.Second, false)}}}
	entry.initPaintCache()
	style := effectStyle{Color: ui.RGB(20, 60, 100), HasColor: true, BrightMaskAlpha: 1, DarkMaskAlpha: .4, WordFadeWidth: .5}
	base := entry.sourceKey(time.Second, style)
	for _, test := range []struct {
		name   string
		change func(*effectStyle)
	}{
		{"color", func(s *effectStyle) { s.Color.R++ }},
		{"color-alpha", func(s *effectStyle) { s.Color.A-- }},
		{"bright-mask", func(s *effectStyle) { s.BrightMaskAlpha -= .1 }},
		{"dark-mask", func(s *effectStyle) { s.DarkMaskAlpha += .1 }},
		{"fade", func(s *effectStyle) { s.WordFadeWidth += .1 }},
		{"background-float", func(s *effectStyle) { s.Background = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := style
			test.change(&changed)
			if base == entry.sourceKey(time.Second, changed) {
				t.Fatal("changed word-pixel input reused the source signature")
			}
		})
	}
	if base == entry.sourceKey(time.Second+1, style) {
		t.Fatal("unsynchronized diagnostic source discarded an exact caller position")
	}
	if effectForeground(effectStyle{}) != ui.RGB(255, 255, 255) || effectForeground(effectStyle{HasColor: true}) != ui.Transparent {
		t.Fatal("explicit transparent foreground was merged with the default white")
	}
	entry.synced, entry.now = true, time.Unix(100, 0)
	entry.animation.Sync(entry.now, time.Second, 0, g.end, true, false, 0)
	base = entry.sourceKey(time.Second, style)
	entry.now = entry.now.Add(time.Second)
	entry.animation.Sync(entry.now, time.Second, 0, g.end, true, false, 0)
	if base != entry.sourceKey(2*time.Second, style) {
		t.Fatal("frozen animation cache depended on raw media, now or lastSync")
	}
}

func TestEffectsPaintCacheTracksLatestIndividualEffectEnd(t *testing.T) {
	emphasis := motion.NewEmphasis(motion.EmphasisOptions{Start: time.Second, End: 5 * time.Second, Characters: 5})
	entry := effectLine{geometry: &lineGeometry{dynamic: true, end: 8 * time.Second}, words: []effectWord{
		{float: motion.NewFloat(time.Second, 3*time.Second, false)},
		{float: motion.NewFloat(time.Second, 2*time.Second, false)},
		{emphasis: emphasis, characters: []effectCharacter{{index: 4}, {index: 0}, {index: 2}}},
	}}
	entry.initPaintCache()
	if entry.cache.ends[motion.MaskAnimation] != entry.geometry.end || entry.cache.ends[motion.FloatWordAnimation] != entry.words[0].float.End() {
		t.Fatal("mask or ordinary-float cache scope ignored a longer earlier effect")
	}
	for _, kind := range []motion.AnimationKind{motion.EmphasisGlowAnimation, motion.EmphasisFloatAnimation} {
		extra := kind == motion.EmphasisFloatAnimation
		if !entry.cache.used[kind] || entry.cache.ends[kind] != emphasis.CharEnd(4, extra) {
			t.Fatal("emphasis cache scope assumed characters were ordered by finish time")
		}
	}
}

func TestEffectsCacheStandaloneReplacementAndEviction(t *testing.T) {
	g := &lineGeometry{font: ui.Font{Size: 10}, width: 1, height: 1, end: time.Second}
	var renderer effectRenderer
	if err := renderer.Prepare(g, 1); err != nil {
		t.Fatal(err)
	}
	entry := renderer.entries[0]
	identity, source := entry.cache.identity, entry.source
	entry.cache.sourceReady, entry.cache.blurReady, entry.cache.revision = true, true, 5
	if entry.bytes < effectLineCacheBytes {
		t.Fatal("prepared line omitted its metadata charge")
	}
	if err := renderer.prepare(g, 1, false, false); err != nil {
		t.Fatal(err)
	}
	if renderer.entries[0] != entry || entry.cache.identity != identity || entry.cache.revision <= 5 || entry.cache.sourceReady || entry.cache.blurReady || entry.source == source || entry.blurX != nil {
		t.Fatal("singleton-to-group surface change retained a valid old raster")
	}
	source, revision := entry.source, entry.cache.revision
	entry.cache.sourceReady, entry.cache.blurReady = true, true
	if err := renderer.prepare(g, 1, true, false); err != nil {
		t.Fatal(err)
	}
	if renderer.entries[0] != entry || entry.cache.identity != identity || entry.cache.revision <= revision || entry.cache.sourceReady || entry.cache.blurReady || entry.source == source || entry.blurX == nil {
		t.Fatal("group-to-singleton surface change retained a valid old raster")
	}
	entry.cache.sourceReady, entry.cache.blurReady = true, true
	entry.animation.Sync(time.Unix(100, 0), 0, 0, time.Second, true, false, 1)
	clock := entry.animation
	if err := renderer.prepare(g, 1, false, true); err != nil {
		t.Fatal(err)
	}
	replacement := renderer.entries[0]
	if replacement == entry || replacement.cache.identity == identity || replacement.cache.sourceReady || replacement.cache.blurReady || replacement.animation != clock {
		t.Fatal("dim-only replacement kept old surfaces or lost its animation clock")
	}
	renderer.Clear()
	// Exercise bounded LRU eviction without allocating graphics resources.
	for i := range effectEntryLimit {
		e := &effectLine{bytes: effectLineCacheBytes, used: uint64(i + 1)}
		e.cache.identity = new(effectRasterIdentity)
		renderer.entries = append(renderer.entries, e)
		renderer.bytes += e.bytes
	}
	old := renderer.entries[0]
	group := &effectGroup{bytes: effectGroupCacheBytes, used: 1000}
	group.cache.source.layers[0].identity = old.cache.identity
	group.cache.source.count = 1
	renderer.groups, renderer.bytes = []*effectGroup{group}, renderer.bytes+group.bytes
	beforeBytes := renderer.bytes
	if !renderer.makeRoom(0, true, false) {
		t.Fatal("unreferenced line could not be evicted at the entry limit")
	}
	for _, e := range renderer.entries {
		if e == old {
			t.Fatal("oldest unpinned entry survived eviction")
		}
	}
	if renderer.bytes != beforeBytes-old.bytes-group.bytes || len(renderer.groups) != 0 {
		t.Fatal("eviction retained a group's child image references or their budget charges")
	}
	renderer.Clear()
	if renderer.entries != nil || renderer.groups != nil || renderer.bytes != 0 || renderer.rootBytes != 0 {
		t.Fatal("Clear retained cached resources or budget charges")
	}
}
