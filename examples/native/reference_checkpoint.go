// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"sync/atomic"
	"unsafe"

	"github.com/xiaowumin-mark/mygo-amll/internal/motion"
)

const (
	referenceCheckpointStart     = 2048
	referenceCheckpointMaxGroups = 20000
	referenceCheckpointMaxBytes  = 64 << 20
	referenceCheckpointBatch     = 64
)

// A task owns every value accessed by its worker. In particular it retains no
// scene, document, geometry, text layout or UI resource. Closing done publishes
// the arrays, which are updated in place rather than copied again on completion.
type referenceCheckpointTask struct {
	generation uint64
	endID      uint64
	groups     []referenceGroupMotion
	clocks     []motion.LineAnimationClock
	metadata   []referenceGroupMetadata
	steps      []referenceStep
	frames     []motion.RefFrame
	cancelled  atomic.Bool
	done       chan struct{}
	complete   bool
}

// startCheckpoint belongs to the UI owner. Snapshot work is bounded by both
// bytes and groups; all numerical replay runs on the single background task.
func (r *referenceScene) startCheckpoint() {
	if r.checkpoint != nil || r.historyBroken || r.lastStep < r.firstStep || r.lastStep-r.firstStep+1 < referenceCheckpointStart {
		return
	}
	if len(r.history) > 0 && r.history[(r.lastStep-1)%uint64(len(r.history))].now.Before(r.checkpointNotBefore) {
		return
	}
	task := snapshotReferenceCheckpoint(r)
	if task == nil {
		r.historyBroken = true
		return
	}
	r.checkpoint = task
	go task.run()
}

func snapshotReferenceCheckpoint(r *referenceScene) *referenceCheckpointTask {
	if r.firstStep == 0 || r.lastStep < r.firstStep || r.lastStep == ^uint64(0) || len(r.groups) != len(r.metadata) || len(r.history) == 0 || len(r.motionFrames) != len(r.history) {
		return nil
	}
	count := r.lastStep - r.firstStep + 1
	if count > uint64(len(r.history)) || count > uint64(int(^uint(0)>>1)) || !referenceCheckpointFits(len(r.groups), len(r.clocks), len(r.metadata), int(count), int(count)) {
		return nil
	}
	for i, metadata := range r.metadata {
		pg := metadata.group
		if metadata.index != i || pg.mainLayout < 0 || pg.mainLayout >= len(r.clocks) || pg.backgroundLayout < -1 || pg.backgroundLayout >= len(r.clocks) {
			return nil
		}
		if r.groups[i].step < r.lastStep && r.groups[i].step+1 < r.firstStep {
			return nil
		}
	}
	for id := r.firstStep; id <= r.lastStep; id++ {
		if r.history[(id-1)%uint64(len(r.history))].id != id {
			return nil
		}
	}
	task := &referenceCheckpointTask{
		generation: r.checkpointGeneration,
		endID:      r.lastStep,
		groups:     make([]referenceGroupMotion, len(r.groups)),
		clocks:     make([]motion.LineAnimationClock, len(r.clocks)),
		metadata:   make([]referenceGroupMetadata, len(r.metadata)),
		steps:      make([]referenceStep, int(count)),
		frames:     make([]motion.RefFrame, int(count)),
		done:       make(chan struct{}),
	}
	copy(task.groups, r.groups)
	copy(task.clocks, r.clocks)
	copy(task.metadata, r.metadata)
	for i := range task.steps {
		offset := (r.firstStep - 1 + uint64(i)) % uint64(len(r.history))
		task.steps[i] = r.history[offset]
		task.frames[i] = r.motionFrames[offset]
	}
	return task
}

func referenceCheckpointFits(groups, clocks, metadata, steps, frames int) bool {
	if groups < 0 || groups > referenceCheckpointMaxGroups || clocks < 0 || metadata < 0 || steps < 0 || frames < 0 {
		return false
	}
	// Divide before multiplying, so even hostile integer counts cannot overflow.
	// Reserve the task header and channel as well as every retained backing array.
	remaining := uint64(referenceCheckpointMaxBytes) - uint64(unsafe.Sizeof(referenceCheckpointTask{})) - 128
	for _, storage := range [...]struct{ count, size uint64 }{
		{uint64(groups), uint64(unsafe.Sizeof(referenceGroupMotion{}))},
		{uint64(clocks), uint64(unsafe.Sizeof(motion.LineAnimationClock{}))},
		{uint64(metadata), uint64(unsafe.Sizeof(referenceGroupMetadata{}))},
		{uint64(steps), uint64(unsafe.Sizeof(referenceStep{}))},
		{uint64(frames), uint64(unsafe.Sizeof(motion.RefFrame{}))},
	} {
		if storage.count > remaining/storage.size {
			return false
		}
		remaining -= storage.count * storage.size
	}
	return true
}

func (task *referenceCheckpointTask) run() {
	defer close(task.done)
	for i := range task.groups {
		if task.cancelled.Load() {
			return
		}
		g := &task.groups[i]
		if g.step >= task.endID {
			continue
		}
		metadata := task.metadata[i]
		var bgClock *motion.LineAnimationClock
		if metadata.group.backgroundLayout >= 0 {
			bgClock = &task.clocks[metadata.group.backgroundLayout]
		}
		for start := int(g.step + 1 - task.steps[0].id); start < len(task.steps); {
			if task.cancelled.Load() {
				return
			}
			end := min(start+referenceCheckpointBatch, len(task.steps))
			referenceReplaySteps(g, &task.clocks[metadata.group.mainLayout], bgClock, metadata, task.steps[start:end], task.frames[start:end])
			start = end
		}
	}
	task.complete = !task.cancelled.Load()
}

// pollCheckpoint never waits. A live group may have advanced beyond the cold
// snapshot while the worker ran; only older owner state is replaced.
func (r *referenceScene) pollCheckpoint() {
	task := r.checkpoint
	if task == nil {
		return
	}
	select {
	case <-task.done:
	default:
		return
	}
	r.checkpoint = nil
	if !task.complete || task.cancelled.Load() || task.generation != r.checkpointGeneration || len(task.groups) != len(r.groups) || len(task.clocks) != len(r.clocks) || len(task.metadata) != len(r.metadata) {
		return
	}
	for i := range r.groups {
		if r.groups[i].step >= task.endID {
			continue
		}
		r.groups[i] = task.groups[i]
		pg := task.metadata[i].group
		r.clocks[pg.mainLayout] = task.clocks[pg.mainLayout]
		if pg.backgroundLayout >= 0 {
			r.clocks[pg.backgroundLayout] = task.clocks[pg.backgroundLayout]
		}
	}
	r.firstStep = max(r.firstStep, task.endID+1)
}

// cancelCheckpoint is an owner-thread operation and deliberately does not wait
// for a worker. Keep its slot occupied until poll observes done: otherwise a
// cancelled worker can overlap the allocation and execution of its replacement.
// A final, non-reopening Close may drop the pointer after requesting cancellation.
func (r *referenceScene) cancelCheckpoint() {
	r.checkpointGeneration++
	if r.checkpoint != nil {
		r.checkpoint.cancelled.Store(true)
	}
}
