package motion

import (
	"reflect"
	"testing"
	"time"
)

func TestRefReplayOutsidePreservesQueueAndArrival(t *testing.T) {
	now := time.Unix(0, 0)
	initial := NewRefSpring(now, 1200, RefYParams(true, false, false, 0, false))
	initial.SetTarget(now, 9000, 50*time.Millisecond)
	frames := make([]RefFrame, 240)
	for i := range frames {
		delta := time.Duration(11+i%13) * time.Millisecond
		now = now.Add(delta)
		frames[i] = RefFrame{Now: now, Delta: delta}
	}
	eager, proof := initial, initial
	for _, frame := range frames {
		eager.AdvanceFrame(frame.Now, frame.Delta)
	}
	if !proof.ReplayOutside(frames, -300, 800) {
		t.Fatal("wholly offscreen path was rejected")
	}
	if !reflect.DeepEqual(eager, proof) {
		t.Fatalf("replay changed reference state\neager=%+v\nproof=%+v", eager, proof)
	}
	ordinary := initial
	ordinary.ReplayFrames(frames)
	if !reflect.DeepEqual(eager, ordinary) {
		t.Fatal("ReplayFrames differs from individual reference updates")
	}
}

func TestRefReplayOutsideFailureDoesNotMutate(t *testing.T) {
	now := time.Unix(0, 0)
	spring := NewRefSpring(now, 1200, RefYParams(true, false, false, 0, false))
	spring.SetTarget(now, 0, 0)
	before := spring
	frames := make([]RefFrame, 120)
	for i := range frames {
		now = now.Add(time.Second / 60)
		frames[i] = RefFrame{Now: now, Delta: time.Second / 60}
	}
	if spring.ReplayOutside(frames, -300, 800) {
		t.Fatal("viewport crossing accepted as offscreen")
	}
	if !reflect.DeepEqual(spring, before) {
		t.Fatal("failed visibility proof mutated receiver")
	}
}
