// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func cacheEvictionRenderer() effectRenderer {
	var renderer effectRenderer
	for i := range effectEntryLimit {
		entry := &effectLine{bytes: effectLineCacheBytes, used: uint64(i + 1)}
		entry.cache.identity = new(effectRasterIdentity)
		renderer.entries = append(renderer.entries, entry)
		renderer.bytes += entry.bytes
	}
	return renderer
}

func cacheEvictionGroup(entry *effectLine, pinned bool) *effectGroup {
	group := &effectGroup{bytes: effectGroupCacheBytes, used: 1000, pinned: pinned}
	group.cache.source.layers[0].identity = entry.cache.identity
	group.cache.source.count = 1
	return group
}

func TestEffectsCacheEvictionDropsDependentCommandScenes(t *testing.T) {
	renderer := cacheEvictionRenderer()
	old := renderer.entries[0]
	first, second := cacheEvictionGroup(old, false), cacheEvictionGroup(old, false)
	other := cacheEvictionGroup(renderer.entries[2], false)
	renderer.groups = []*effectGroup{first, other, second}
	renderer.bytes += first.bytes + other.bytes + second.bytes
	before := renderer.bytes
	if !renderer.makeRoom(0, true, false) {
		t.Fatal("entry-limit eviction failed")
	}
	if len(renderer.entries) != effectEntryLimit-1 || len(renderer.groups) != 1 || renderer.groups[0] != other {
		t.Fatal("eviction retained dependent Canvas command scenes or removed an unrelated group")
	}
	if renderer.bytes != before-old.bytes-first.bytes-second.bytes {
		t.Fatal("eviction did not subtract both dependent groups and the child")
	}
	for _, entry := range renderer.entries {
		if entry == old {
			t.Fatal("oldest unpinned raster survived eviction")
		}
	}
}

func TestEffectsCacheEvictionProtectsPinnedGroupOperands(t *testing.T) {
	renderer := cacheEvictionRenderer()
	protected, next := renderer.entries[0], renderer.entries[1]
	group := cacheEvictionGroup(protected, true)
	renderer.groups = []*effectGroup{group}
	renderer.bytes += group.bytes
	before := renderer.bytes
	if !renderer.makeRoom(0, true, false) {
		t.Fatal("an independent unpinned line should still be evictable")
	}
	if len(renderer.groups) != 1 || renderer.groups[0] != group || renderer.bytes != before-next.bytes {
		t.Fatal("pinned group or its resource accounting changed")
	}
	found := false
	for _, entry := range renderer.entries {
		found = found || entry == protected
		if entry == next {
			t.Fatal("next-oldest independent raster was not evicted")
		}
	}
	if !found {
		t.Fatal("pinned Canvas operand was evicted while still referenced")
	}
}

func TestEffectsCacheEvictionWaitsForPinnedDependencies(t *testing.T) {
	renderer := cacheEvictionRenderer()
	for _, entry := range renderer.entries {
		group := cacheEvictionGroup(entry, true)
		renderer.groups = append(renderer.groups, group)
		renderer.bytes += group.bytes
	}
	before := renderer.bytes
	if renderer.makeRoom(0, true, false) || len(renderer.entries) != effectEntryLimit || renderer.bytes != before {
		t.Fatal("entry limit evicted graphics still required by a pinned command scene")
	}
	// The next preparation frame can release those obsolete command scenes.
	renderer.BeginFrame()
	if !renderer.makeRoom(0, true, false) || len(renderer.entries) != effectEntryLimit-1 || len(renderer.groups) != effectEntryLimit-1 {
		t.Fatal("unpinning did not allow the raster and its dependent scene to be evicted together")
	}
}

func TestEffectsCacheModeReplacementReleasesUnpinnedScenes(t *testing.T) {
	g := &lineGeometry{font: ui.Font{Size: 10}, width: 1, height: 1, end: time.Second}
	var renderer effectRenderer
	if err := renderer.Prepare(g, 1); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []struct{ standalone, dimOnly bool }{{false, false}, {true, false}, {false, true}, {true, false}, {false, false}} {
		old := renderer.entries[0]
		oldSource, oldIdentity := old.source, old.cache.identity
		group := cacheEvictionGroup(old, true)
		renderer.groups = append(renderer.groups, group)
		renderer.bytes += group.bytes
		renderer.BeginFrame()
		if err := renderer.prepare(g, 1, mode.standalone, mode.dimOnly); err != nil {
			t.Fatal(err)
		}
		entry := renderer.entries[0]
		if len(renderer.groups) != 0 || renderer.bytes != entry.bytes || entry.cache.sourceReady || entry.cache.blurReady {
			t.Fatal("mode replacement retained a stale command scene or uncharged image operands")
		}
		if old.dimOnly != entry.dimOnly {
			if entry.cache.identity == oldIdentity {
				t.Fatal("replacement entry reused an old atlas token")
			}
		} else if entry.source == oldSource || entry.cache.identity != oldIdentity {
			t.Fatal("surface replacement failed to replace its Canvas while preserving the entry token")
		}
	}
}

func TestEffectsCacheModeReplacementWaitsForPinnedScene(t *testing.T) {
	for _, mode := range []struct{ standalone, dimOnly bool }{{false, false}, {true, true}} {
		g := &lineGeometry{font: ui.Font{Size: 10}, width: 1, height: 1, end: time.Second}
		var renderer effectRenderer
		if err := renderer.Prepare(g, 1); err != nil {
			t.Fatal(err)
		}
		old := renderer.entries[0]
		old.cache.sourceReady, old.cache.blurReady = true, true
		group := cacheEvictionGroup(old, true)
		renderer.groups = []*effectGroup{group}
		renderer.bytes += group.bytes
		oldBytes, oldSource, oldBlur, oldCache := renderer.bytes, old.source, old.blurX, old.cache
		if err := renderer.prepare(g, 1, mode.standalone, mode.dimOnly); !errors.Is(err, errEffectLimit) {
			t.Fatalf("pinned scene mode replacement returned %v", err)
		}
		if renderer.entries[0] != old || old.source != oldSource || old.blurX != oldBlur || old.cache != oldCache || renderer.bytes != oldBytes || len(renderer.groups) != 1 {
			t.Fatal("refused replacement changed a pinned scene's owned images or accounting")
		}
		renderer.BeginFrame()
		if err := renderer.prepare(g, 1, mode.standalone, mode.dimOnly); err != nil {
			t.Fatalf("unpinning did not permit mode replacement: %v", err)
		}
		if len(renderer.groups) != 0 || renderer.bytes != renderer.entries[0].bytes {
			t.Fatal("released scene retained stale operands after a successful replacement")
		}
	}
}
