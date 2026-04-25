// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codectest

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBench is a recording stub for the contractTB interface. It
// captures every Fatalf and ReportMetric call so contract_test.go can
// assert on validation behavior without driving a real *testing.B.
type fakeBench struct {
	helperCalls int
	fatalfMsgs  []string
	metrics     []recordedMetric
	resetTimerN int
}

type recordedMetric struct {
	unit  string
	value float64
}

func (f *fakeBench) Helper() { f.helperCalls++ }

func (f *fakeBench) Fatalf(format string, args ...any) {
	f.fatalfMsgs = append(f.fatalfMsgs, fmt.Sprintf(format, args...))
}

func (f *fakeBench) ReportMetric(n float64, unit string) {
	f.metrics = append(f.metrics, recordedMetric{unit: unit, value: n})
}

func (f *fakeBench) ResetTimer() { f.resetTimerN++ }

// loopN returns a b.Loop-shaped function that returns true exactly
// n times then false thereafter.
func loopN(n int) func() bool {
	i := 0
	return func() bool {
		if i < n {
			i++
			return true
		}
		return false
	}
}

// drainLoop runs the Contract through its loop with a body that does
// no work, returning the number of iterations actually executed.
func drainLoop(c *Contract) int {
	n := 0
	for c.Loop() {
		n++
	}
	return n
}

func TestContract_NoCeilings_NoOp(t *testing.T) {
	t.Parallel()
	fb := &fakeBench{}
	c := newContract(fb, loopN(5))
	if got := drainLoop(c); got != 5 {
		t.Fatalf("drainLoop=%d, want 5", got)
	}
	c.End()
	if len(fb.fatalfMsgs) != 0 {
		t.Fatalf("Fatalf called on no-ceiling contract: %v", fb.fatalfMsgs)
	}
	if len(fb.metrics) != 0 {
		t.Fatalf("metrics reported on no-ceiling contract: %v", fb.metrics)
	}
	if fb.resetTimerN != 1 {
		t.Fatalf("ResetTimer called %d times, want 1", fb.resetTimerN)
	}
	if fb.helperCalls != 1 {
		t.Fatalf("Helper called %d times, want 1", fb.helperCalls)
	}
}

func TestContract_NoIterations_NoOp(t *testing.T) {
	t.Parallel()
	fb := &fakeBench{}
	c := newContract(fb, loopN(0)).AllocsMax(0).LatencyMax(time.Second)
	if got := drainLoop(c); got != 0 {
		t.Fatalf("drainLoop=%d, want 0", got)
	}
	c.End()
	if len(fb.fatalfMsgs) != 0 {
		t.Fatalf("Fatalf called on zero-iteration contract: %v", fb.fatalfMsgs)
	}
	if len(fb.metrics) != 0 {
		t.Fatalf("metrics reported on zero-iteration contract: %v", fb.metrics)
	}
}

func TestContract_AllocsMax_PassesUnderCeiling(t *testing.T) {
	t.Parallel()
	fb := &fakeBench{}
	// AllocsMax(1<<20) is so high it cannot be exceeded by the empty
	// loop body — ensures the verify path fires without false-failing.
	c := newContract(fb, loopN(10)).AllocsMax(1 << 20)
	_ = drainLoop(c)
	c.End()
	if len(fb.fatalfMsgs) != 0 {
		t.Fatalf("AllocsMax=high should pass, got: %v", fb.fatalfMsgs)
	}
	if got := metricValue(t, fb, "contract-allocs/op"); got > 1<<20 {
		t.Fatalf("reported allocs/op=%.0f exceeds the ceiling we just set", got)
	}
}

func TestContract_AllocsMax_FailsOverCeiling(t *testing.T) {
	t.Parallel()
	fb := &fakeBench{}
	c := newContract(fb, loopN(100)).AllocsMax(0)
	for c.Loop() {
		// Force a heap allocation per iteration. Storing the address
		// of `b` through atomic.Pointer.Store defeats any escape-
		// analysis stack promotion, and using atomic (vs a plain
		// package-level var) keeps this test compatible with the
		// race detector when other parallel tests also force allocs.
		b := make([]byte, 16)
		sink.Store(&b)
	}
	c.End()
	if len(fb.fatalfMsgs) != 1 {
		t.Fatalf("AllocsMax(0) with allocating body should fatal once, got: %v", fb.fatalfMsgs)
	}
	if !strings.Contains(fb.fatalfMsgs[0], "allocs/op=") || !strings.Contains(fb.fatalfMsgs[0], "AllocsMax=0") {
		t.Fatalf("Fatalf msg missing alloc citations: %q", fb.fatalfMsgs[0])
	}
}

func TestContract_LatencyMax_PassesUnderCeiling(t *testing.T) {
	t.Parallel()
	fb := &fakeBench{}
	c := newContract(fb, loopN(10)).LatencyMax(time.Second)
	_ = drainLoop(c)
	c.End()
	if len(fb.fatalfMsgs) != 0 {
		t.Fatalf("LatencyMax=1s should pass, got: %v", fb.fatalfMsgs)
	}
	got := metricValue(t, fb, "contract-ns/op-mean")
	if got < 0 {
		t.Fatalf("reported mean ns/op=%.0f, want non-negative", got)
	}
}

func TestContract_LatencyMax_FailsOverCeiling(t *testing.T) {
	t.Parallel()
	fb := &fakeBench{}
	// Set the ceiling implausibly low (1ns) and force every iteration
	// to take longer than that with a sleep.
	c := newContract(fb, loopN(3)).LatencyMax(1 * time.Nanosecond)
	for c.Loop() {
		time.Sleep(50 * time.Microsecond)
	}
	c.End()
	if len(fb.fatalfMsgs) != 1 {
		t.Fatalf("LatencyMax(1ns) with slow body should fatal once, got: %v", fb.fatalfMsgs)
	}
	if !strings.Contains(fb.fatalfMsgs[0], "mean ns/op=") || !strings.Contains(fb.fatalfMsgs[0], "LatencyMax=") {
		t.Fatalf("Fatalf msg missing latency citations: %q", fb.fatalfMsgs[0])
	}
}

func TestContract_BothCeilings_BothReported(t *testing.T) {
	t.Parallel()
	fb := &fakeBench{}
	c := newContract(fb, loopN(20)).AllocsMax(1 << 20).LatencyMax(time.Second)
	_ = drainLoop(c)
	c.End()
	if len(fb.fatalfMsgs) != 0 {
		t.Fatalf("both-permissive contract should pass, got: %v", fb.fatalfMsgs)
	}
	if !hasMetric(fb, "contract-allocs/op") {
		t.Fatal("expected contract-allocs/op metric")
	}
	if !hasMetric(fb, "contract-ns/op-mean") {
		t.Fatal("expected contract-ns/op-mean metric")
	}
}

func TestContract_AllocsMax_FailsBeforeLatencyCheck(t *testing.T) {
	t.Parallel()
	// When both ceilings are violated, alloc validation runs first.
	// Under a real *testing.B, the alloc Fatalf halts the goroutine
	// and the latency check never executes; under the recording mock
	// both are observable, so we assert on order rather than count.
	fb := &fakeBench{}
	c := newContract(fb, loopN(50)).AllocsMax(0).LatencyMax(1 * time.Nanosecond)
	for c.Loop() {
		b := make([]byte, 16)
		sink.Store(&b)
		time.Sleep(10 * time.Microsecond)
	}
	c.End()
	if len(fb.fatalfMsgs) == 0 {
		t.Fatal("expected at least one Fatalf")
	}
	if !strings.Contains(fb.fatalfMsgs[0], "AllocsMax") {
		t.Fatalf("expected alloc Fatalf first, got: %q", fb.fatalfMsgs[0])
	}
}

// TestStartContract_WithRealTestingB exercises the public StartContract
// constructor through a real *testing.B, so the *testing.B → contractTB
// wiring is covered. Uses testing.Benchmark to spin up an isolated B.
func TestStartContract_WithRealTestingB(t *testing.T) {
	t.Parallel()
	r := testing.Benchmark(func(b *testing.B) {
		c := StartContract(b).AllocsMax(1 << 20).LatencyMax(time.Second)
		for c.Loop() {
			// no work
		}
		c.End()
	})
	if r.N == 0 {
		t.Fatal("benchmark did not run any iterations")
	}
}

// sink prevents escape analysis from stack-allocating the make([]byte,
// ...) calls in TestContract_AllocsMax_*. atomic.Pointer (rather than a
// plain package-level slice) keeps the alloc tests compatible with -race
// when they run in parallel.
var sink atomic.Pointer[[]byte]

func metricValue(t *testing.T, fb *fakeBench, unit string) float64 {
	t.Helper()
	for _, m := range fb.metrics {
		if m.unit == unit {
			return m.value
		}
	}
	t.Fatalf("metric %q not reported; got: %v", unit, fb.metrics)
	return 0
}

func hasMetric(fb *fakeBench, unit string) bool {
	for _, m := range fb.metrics {
		if m.unit == unit {
			return true
		}
	}
	return false
}
