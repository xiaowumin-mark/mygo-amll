// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

// Synthetic scalar fixtures deliberately contain no documents or UI resources.
func referenceCheckpointFixture(groups, count int, first uint64) *referenceScene {
	ring := max(4096, count)
	r := &referenceScene{
		groups: make([]referenceGroupMotion, groups), clocks: make([]motion.LineAnimationClock, groups*2),
		metadata: make([]referenceGroupMetadata, groups), history: make([]referenceStep, ring), motionFrames: make([]motion.RefFrame, ring),
		firstStep: first, lastStep: first + uint64(count) - 1,
	}
	origin := time.Unix(0, 0)
	for i := range r.groups {
		start := time.Duration(i) * 3 * time.Second
		r.metadata[i] = referenceGroupMetadata{index: i, group: presentationGroup{mainLayout: 2 * i, backgroundLayout: 2*i + 1, start: start, end: start + 2*time.Second, backgroundFirst: i%2 != 0}, mainStart: start, mainEnd: start + 2*time.Second, bgStart: start + time.Second/2, bgEnd: start + 3*time.Second/2}
		g := &r.groups[i]
		g.prefix, g.height, g.step = float64(i*90), 90, first-1
		g.y = motion.NewRefSpring(origin, float64(1200+i*90), motion.RefYParams(true, false, false, 0, false))
		g.bg = motion.NewRefSpring(origin, -80, motion.RefYParams(true, false, false, 0, false))
		g.mainScale = motion.NewRefSpring(origin, 100, motion.RefMainScaleParams())
		g.bgScale = motion.NewRefSpring(origin, 100, motion.RefBGScaleParams())
		g.opacity.snap(1)
		g.dim.snap(.2)
	}
	for k := 0; k < count; k++ {
		id := first + uint64(k)
		now := origin.Add(time.Duration(id) * 3 * time.Millisecond)
		focus := k / 127 % max(1, groups)
		step := referenceStep{id: id, now: now, delta: 3 * time.Millisecond, media: time.Duration(k) * 3 * time.Millisecond, revision: uint64(k / 701), params: motion.RefYParams(false, false, false, time.Second, true), paramsChanged: k%301 == 0, layout: k%127 == 0, base: float64(-focus * 90), height: 500, width: 800, focus: focus, latest: focus, firstVisible: focus, windowFrom: focus - 2, windowTo: focus + 2, latestStart: time.Duration(focus) * 3 * time.Second, highlighting: true, playing: k%503 < 450, dynamic: true, stagger: true, interludeAnchor: -2}
		if k > 0 && k%97 == 0 {
			step.estimateDelta = 7.25
			step.layout = true
		}
		if k%193 == 0 {
			step.changes[0] = referenceHeightChange{index: k / 193 % max(1, groups), delta: .25, firstMeasurement: k/193 < groups}
			step.changeCount = 1
		}
		offset := (id - 1) % uint64(ring)
		r.history[offset] = step
		r.motionFrames[offset] = motion.RefFrame{Now: now, Delta: step.delta}
	}
	return r
}

func referenceCheckpointEager(task *referenceCheckpointTask) {
	for i := range task.groups {
		g, metadata := &task.groups[i], task.metadata[i]
		for _, step := range task.steps {
			if step.id <= g.step {
				continue
			}
			var bg *motion.LineAnimationClock
			if metadata.group.backgroundLayout >= 0 {
				bg = &task.clocks[metadata.group.backgroundLayout]
			}
			referenceApplyStep(g, &task.clocks[metadata.group.mainLayout], bg, metadata, step)
		}
	}
}

func TestReferenceCheckpointMatchesEagerAcrossRingBoundary(t *testing.T) {
	r := referenceCheckpointFixture(7, 257, 4000)
	task, eager := snapshotReferenceCheckpoint(r), snapshotReferenceCheckpoint(r)
	if task == nil || eager == nil {
		t.Fatal("valid snapshot rejected")
	}
	// The owner continues independently after the snapshot; none of these changes
	// may become visible to the worker or be overwritten by its computation.
	r.groups[0].prefix += 123
	r.clocks[0].Sync(time.Unix(5, 0), 5*time.Second, 0, 10*time.Second, true, true, 123)
	r.metadata[0].mainEnd++
	clear(r.history)
	clear(r.motionFrames)
	ownerGroups := append([]referenceGroupMotion(nil), r.groups...)
	ownerClocks := append([]motion.LineAnimationClock(nil), r.clocks...)
	referenceCheckpointEager(eager)
	task.run()
	if !task.complete || !reflect.DeepEqual(task.groups, eager.groups) || !reflect.DeepEqual(task.clocks, eager.clocks) {
		t.Fatal("bounded worker replay differs from eager source-order replay")
	}
	if !reflect.DeepEqual(ownerGroups, r.groups) || !reflect.DeepEqual(ownerClocks, r.clocks) {
		t.Fatal("worker changed owner storage")
	}
	if task.steps[0].id != 4000 || task.steps[len(task.steps)-1].id != 4256 {
		t.Fatal("ring snapshot did not preserve chronological IDs")
	}
}

func TestReferenceCheckpointPublishPreservesNewerOwnerState(t *testing.T) {
	r := referenceCheckpointFixture(3, 130, 1)
	task := snapshotReferenceCheckpoint(r)
	task.run()
	r.checkpoint = task
	// The first group has already consumed a newer UI frame while the checkpoint
	// was pending. Its corresponding animation clocks must stay newer as well.
	r.groups[0] = task.groups[0]
	r.clocks[0], r.clocks[1] = task.clocks[0], task.clocks[1]
	extra := task.steps[len(task.steps)-1]
	extra.id++
	extra.now = extra.now.Add(3 * time.Millisecond)
	extra.media += 3 * time.Millisecond
	extra.layout, extra.paramsChanged, extra.changeCount = false, false, 0
	referenceApplyStep(&r.groups[0], &r.clocks[0], &r.clocks[1], r.metadata[0], extra)
	newerGroup, newerMain, newerBG := r.groups[0], r.clocks[0], r.clocks[1]
	r.history[(extra.id-1)%uint64(len(r.history))] = extra
	r.motionFrames[(extra.id-1)%uint64(len(r.history))] = motion.RefFrame{Now: extra.now, Delta: extra.delta}
	r.lastStep = extra.id
	r.pollCheckpoint()
	if r.checkpoint != nil || r.firstStep != task.endID+1 || r.groups[0] != newerGroup || r.clocks[0] != newerMain || r.clocks[1] != newerBG {
		t.Fatal("publish replaced newer owner state or failed to retire the covered prefix")
	}
	if r.groups[1] != task.groups[1] || r.clocks[2] != task.clocks[2] || r.clocks[3] != task.clocks[3] {
		t.Fatal("publish failed to install dormant state and its clocks")
	}
	wantGroup, wantMain, wantBG := task.groups[1], task.clocks[2], task.clocks[3]
	referenceApplyStep(&wantGroup, &wantMain, &wantBG, r.metadata[1], extra)
	r.replay(1, r.lastStep)
	if r.groups[1] != wantGroup || r.clocks[2] != wantMain || r.clocks[3] != wantBG {
		t.Fatal("post-checkpoint ring tail replay differs")
	}
}

func TestReferenceCheckpointPendingStaleAndCancelled(t *testing.T) {
	r := referenceCheckpointFixture(3, 130, 1)
	task := snapshotReferenceCheckpoint(r)
	r.checkpoint = task
	r.pollCheckpoint()
	if r.checkpoint != task || r.firstStep != 1 || r.groups[0].step != 0 {
		t.Fatal("pending poll blocked or changed owner state")
	}
	task.run()
	r.checkpointGeneration++
	r.pollCheckpoint()
	if r.checkpoint != nil || r.firstStep != 1 || r.groups[0].step != 0 {
		t.Fatal("stale generation was published")
	}
	task = snapshotReferenceCheckpoint(r)
	r.checkpoint = task
	generation := r.checkpointGeneration
	r.cancelCheckpoint()
	if r.checkpoint != task || r.checkpointGeneration != generation+1 || !task.cancelled.Load() {
		t.Fatal("cancel failed to invalidate the task immediately")
	}
	task.run()
	if task.complete {
		t.Fatal("cancelled task reported a usable result")
	}
	select {
	case <-task.done:
	default:
		t.Fatal("cancelled worker did not finish")
	}
	r.pollCheckpoint()
	if r.checkpoint != nil || r.firstStep != 1 {
		t.Fatal("completed cancellation kept its slot or published stale history")
	}
}

func TestReferenceCheckpointCancelledPendingTaskBlocksReplacement(t *testing.T) {
	r := referenceCheckpointFixture(3, referenceCheckpointStart, 1)
	// A deliberately unstarted task has a guaranteed open done channel. This
	// proves the owner constraint independently of scheduler speed or deadlines.
	task := snapshotReferenceCheckpoint(r)
	r.checkpoint = task
	r.cancelCheckpoint()
	if r.checkpoint != task {
		t.Fatal("cancellation released an unfinished worker slot")
	}
	for range 3 {
		r.cancelCheckpoint()
		r.pollCheckpoint()
		r.startCheckpoint()
		if r.checkpoint != task || r.historyBroken {
			t.Fatal("pending cancelled worker was replaced or caused fallback")
		}
	}
	if allocations := testing.AllocsPerRun(25, func() { r.startCheckpoint() }); allocations != 0 {
		t.Fatalf("replacement allocated a concurrent snapshot: %g allocations", allocations)
	}
	task.run() // cancellation completes without replay, then the owner may retire it.
	r.pollCheckpoint()
	if r.checkpoint != nil || r.firstStep != 1 {
		t.Fatal("cancelled task result was not discarded after completion")
	}
	r.startCheckpoint()
	replacement := r.checkpoint
	if replacement == nil || replacement == task {
		t.Fatal("completed cancellation prevented a later checkpoint")
	}
	r.cancelCheckpoint()
	select {
	case <-replacement.done:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement worker failed to finish cancellation")
	}
	r.pollCheckpoint()
}

func TestReferenceCheckpointThresholdSingleTaskAndCancellation(t *testing.T) {
	r := referenceCheckpointFixture(8, referenceCheckpointStart-1, 1)
	r.startCheckpoint()
	if r.checkpoint != nil || r.historyBroken {
		t.Fatal("checkpoint started before the half-ring threshold")
	}
	r = referenceCheckpointFixture(8, referenceCheckpointStart, 1)
	r.startCheckpoint()
	task := r.checkpoint
	if task == nil {
		t.Fatal("checkpoint did not start at the threshold")
	}
	r.startCheckpoint()
	if r.checkpoint != task {
		t.Fatal("started a second concurrent task")
	}
	r.cancelCheckpoint()
	select {
	case <-task.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled worker did not observe cancellation")
	}
}

func TestReferenceCheckpointWaitsForResizeQuietPeriod(t *testing.T) {
	r := referenceCheckpointFixture(3, referenceCheckpointStart, 1)
	last := r.history[(r.lastStep-1)%uint64(len(r.history))].now
	r.checkpointNotBefore = last.Add(500 * time.Millisecond)
	r.startCheckpoint()
	if r.checkpoint != nil || r.historyBroken {
		t.Fatal("resize quiet period started a worker or forced fallback")
	}
	r.checkpointNotBefore = last
	r.startCheckpoint()
	task := r.checkpoint
	if task == nil {
		t.Fatal("checkpoint failed to start at the quiet-period deadline")
	}
	r.cancelCheckpoint()
	select {
	case <-task.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled quiet-period worker did not exit")
	}
}

func TestReferenceCheckpointBudgetsAndInvalidHistory(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, counts := range [][5]int{{referenceCheckpointMaxGroups + 1, 1, 1, 1, 1}, {1, maxInt, 1, 1, 1}, {1, 1, maxInt, 1, 1}, {1, 1, 1, maxInt, 1}, {1, 1, 1, 1, maxInt}, {-1, 1, 1, 1, 1}} {
		if referenceCheckpointFits(counts[0], counts[1], counts[2], counts[3], counts[4]) {
			t.Fatalf("accepted unbounded snapshot counts=%v", counts)
		}
	}
	if !referenceCheckpointFits(10000, 20000, 10000, referenceCheckpointStart, referenceCheckpointStart) {
		t.Fatal("ordinary 10,000-group snapshot exceeds the declared budget")
	}
	for _, test := range []struct {
		name string
		edit func(*referenceScene)
	}{
		{"gap", func(r *referenceScene) { r.history[10].id++ }},
		{"missingPrefix", func(r *referenceScene) { r.firstStep = 2 }},
		{"clockMapping", func(r *referenceScene) { r.metadata[0].group.mainLayout = len(r.clocks) }},
		{"metadataCount", func(r *referenceScene) { r.metadata = r.metadata[:1] }},
		{"frameCount", func(r *referenceScene) { r.motionFrames = r.motionFrames[:1] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := referenceCheckpointFixture(3, referenceCheckpointStart+1, 1)
			test.edit(r)
			r.startCheckpoint()
			if r.checkpoint != nil || !r.historyBroken {
				t.Fatal("invalid snapshot did not request explicit fallback")
			}
		})
	}
}

func BenchmarkReferenceCheckpoint(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			r := referenceCheckpointFixture(count, referenceCheckpointStart, 1)
			b.Run("OwnerSnapshot", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if snapshotReferenceCheckpoint(r) == nil {
						b.Fatal("snapshot rejected")
					}
				}
			})
			b.Run("OwnerPublish", func(b *testing.B) {
				task := snapshotReferenceCheckpoint(r)
				task.run()
				owner := &referenceScene{groups: make([]referenceGroupMotion, len(r.groups)), clocks: make([]motion.LineAnimationClock, len(r.clocks)), metadata: r.metadata}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					copy(owner.groups, r.groups)
					copy(owner.clocks, r.clocks)
					owner.firstStep, owner.checkpoint = 1, task
					b.StartTimer()
					owner.pollCheckpoint()
				}
			})
			for _, eager := range []bool{true, false} {
				name := "WorkerOptimized"
				if eager {
					name = "EagerSource"
				}
				b.Run(name, func(b *testing.B) {
					task := snapshotReferenceCheckpoint(r)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						copy(task.groups, r.groups)
						copy(task.clocks, r.clocks)
						task.done = make(chan struct{})
						b.StartTimer()
						if eager {
							referenceCheckpointEager(task)
						} else {
							task.run()
						}
					}
				})
			}
		})
	}
}
