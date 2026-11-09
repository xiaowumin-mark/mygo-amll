package main

import (
	"math"
	"testing"
	"time"
)

func TestPlaybackClockPauseResumeAndSeek(t *testing.T) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	clock := newPlaybackClock(time.Minute, base)
	if clock.playing || clock.rate != 1 || clock.positionAt(base.Add(time.Hour)) != 0 {
		t.Fatal("new clock must remain paused at the start at normal speed")
	}
	clock.seek(base, 10*time.Second)
	clock.setPlaying(base.Add(time.Second), true)
	if got := clock.positionAt(base.Add(3 * time.Second)); got != 12*time.Second {
		t.Fatalf("playing position = %v, want 12s", got)
	}
	clock.setPlaying(base.Add(3*time.Second), false)
	if got := clock.positionAt(base.Add(time.Hour)); got != 12*time.Second {
		t.Fatalf("paused position = %v, want 12s", got)
	}
	clock.seek(base.Add(time.Hour), 40*time.Second)
	clock.setPlaying(base.Add(time.Hour), true)
	clock.seek(base.Add(time.Hour+time.Second), 5*time.Second)
	if got := clock.positionAt(base.Add(time.Hour + 2*time.Second)); got != 6*time.Second {
		t.Fatalf("backward seek = %v, want 6s", got)
	}
	clock.seek(base.Add(time.Hour+2*time.Second), 50*time.Second)
	if got := clock.positionAt(base.Add(time.Hour + 3*time.Second)); got != 51*time.Second {
		t.Fatalf("forward seek = %v, want 51s", got)
	}
}

func TestPlaybackClockRateAndQueriesAreReadOnly(t *testing.T) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	clock := newPlaybackClock(time.Minute, base)
	clock.setPlaying(base, true)
	if err := clock.setRate(base.Add(2*time.Second), 1.5); err != nil {
		t.Fatal(err)
	}
	before := clock
	now := base.Add(4 * time.Second)
	for range 20 {
		if got := clock.positionAt(now); got != 5*time.Second {
			t.Fatalf("repeated query = %v, want 5s", got)
		}
	}
	if clock != before {
		t.Fatal("query changed the playback anchor")
	}
	clock.setPlaying(now, false)
	if err := clock.setRate(now.Add(time.Second), 0.5); err != nil {
		t.Fatal(err)
	}
	clock.setPlaying(now.Add(2*time.Second), true)
	if got := clock.positionAt(now.Add(4 * time.Second)); got != 6*time.Second {
		t.Fatalf("resumed slow position = %v, want 6s", got)
	}
}

func TestPlaybackClockInvalidRateDoesNotChangeAnchor(t *testing.T) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	clock := newPlaybackClock(time.Minute, base)
	clock.setPlaying(base, true)
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		before := clock
		if err := clock.setRate(base.Add(time.Second), rate); err == nil {
			t.Fatalf("invalid rate %v was accepted", rate)
		}
		if clock != before {
			t.Fatalf("invalid rate %v changed clock", rate)
		}
	}
}

func TestPlaybackClockBoundsAndHiddenResume(t *testing.T) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	clock := newPlaybackClock(time.Minute, base)
	clock.seek(base, -time.Second)
	if got := clock.positionAt(base); got != 0 {
		t.Fatalf("negative seek = %v, want zero", got)
	}
	clock.seek(base, time.Hour)
	if got := clock.positionAt(base); got != time.Minute {
		t.Fatalf("seek beyond duration = %v, want 1m", got)
	}
	clock.seek(base, 10*time.Second)
	clock.setPlaying(base, true)
	if got := clock.positionAt(base.Add(35 * time.Second)); got != 45*time.Second {
		t.Fatalf("position after no intermediate frames = %v, want 45s", got)
	}
	if got := clock.positionAt(base.Add(time.Hour)); got != time.Minute {
		t.Fatalf("long hidden position = %v, want 1m", got)
	}
	if got := clock.positionAt(base.Add(-time.Hour)); got != 0 {
		t.Fatalf("position before anchor = %v, want zero", got)
	}
	for _, duration := range []time.Duration{0, -1} {
		empty := newPlaybackClock(duration, base)
		empty.setPlaying(base, true)
		if got := empty.positionAt(base.Add(time.Hour)); got != 0 {
			t.Fatalf("nonpositive duration position = %v, want zero", got)
		}
	}
}

func TestPlaybackClockOverflowAndNanosecondPrecision(t *testing.T) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	clock := newPlaybackClock(time.Duration(math.MaxInt64), base)
	clock.seek(base, time.Duration(math.MaxInt64-10))
	clock.setPlaying(base, true)
	if got := clock.positionAt(base.Add(3)); got != time.Duration(math.MaxInt64-7) {
		t.Fatalf("normal speed lost nanosecond precision: %v", got)
	}
	if got := clock.positionAt(base.Add(20)); got != clock.duration {
		t.Fatalf("addition overflowed duration: %v", got)
	}
	if err := clock.setRate(base, math.MaxFloat64); err != nil {
		t.Fatal(err)
	}
	if got := clock.positionAt(base.Add(time.Second)); got != clock.duration {
		t.Fatalf("huge positive rate overflowed: %v", got)
	}
	if got := clock.positionAt(base.Add(-time.Second)); got != 0 {
		t.Fatalf("huge negative elapsed overflowed: %v", got)
	}
}

func TestPlaybackClockQueryDoesNotAllocate(t *testing.T) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	clock := newPlaybackClock(time.Minute, base)
	clock.setPlaying(base, true)
	now := base.Add(time.Second)
	if allocs := testing.AllocsPerRun(1000, func() { clock.positionAt(now) }); allocs != 0 {
		t.Fatalf("position query allocated %v times", allocs)
	}
}

var benchmarkPosition time.Duration

func BenchmarkPlaybackPosition(b *testing.B) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	clock := newPlaybackClock(4*time.Minute, base)
	clock.seek(base, time.Minute)
	clock.setPlaying(base, true)
	now := base.Add(time.Second + 13*time.Nanosecond)
	b.ReportAllocs()
	for b.Loop() {
		benchmarkPosition = clock.positionAt(now)
	}
}
