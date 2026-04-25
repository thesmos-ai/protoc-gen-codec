// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codectest

import (
	"runtime"
	"testing"
	"time"
)

// Contract is a benchmark-scope allocation + latency gate.
//
// Usage:
//
//	c := codectest.StartContract(b).
//	    AllocsMax(0).
//	    LatencyMax(50 * time.Nanosecond)
//
//	for c.Loop() {
//	    subject()
//	}
//
//	c.End()
//
// At c.End the measured allocations-per-op and mean per-iteration
// latency are reported via b.ReportMetric. If either exceeds the
// declared ceiling the benchmark fails with a message that cites
// both the ceiling and the measurement.
//
// A Contract with neither AllocsMax nor LatencyMax set is a true
// no-op: nothing is measured, nothing reported, nothing asserted.
// Pay-for-what-you-use — opting in to a ceiling is what activates
// the corresponding measurement.
//
// The mean is reported instead of p99 because computing a percentile
// requires either a per-iteration sample slice (allocates, breaks
// AllocsMax(0) honesty) or a streaming estimator (state, randomness).
// Mean catches order-of-magnitude regressions, which is the failure
// class the contract gate is designed to prevent.
type Contract struct {
	tb     contractTB
	loopFn func() bool

	allocsMax     uint64
	allocsEnabled bool

	latencyMax     time.Duration
	latencyEnabled bool

	started    bool
	iterations uint64
	iterStart  time.Time
	totalLat   time.Duration
	preMem     runtime.MemStats
}

// contractTB is the testing.TB-shaped subset Contract needs. It
// exists so contract_test.go can drive the validation logic with a
// recording mock, avoiding 1s-per-case testing.Benchmark runs.
type contractTB interface {
	Helper()
	Fatalf(format string, args ...any)
	ReportMetric(n float64, unit string)
	ResetTimer()
}

// StartContract begins a contract scope on b. Configure ceilings via
// the chainable AllocsMax / LatencyMax setters, then drive the loop
// with c.Loop and finalize with c.End.
func StartContract(b *testing.B) *Contract {
	return newContract(b, b.Loop)
}

// newContract is the testable internal constructor.
func newContract(tb contractTB, loop func() bool) *Contract {
	return &Contract{tb: tb, loopFn: loop}
}

// AllocsMax declares the per-iteration allocation ceiling. AllocsMax(0)
// forbids any heap allocation in the measured loop body.
func (c *Contract) AllocsMax(maxAllocs uint64) *Contract {
	c.allocsMax = maxAllocs
	c.allocsEnabled = true
	return c
}

// LatencyMax declares the per-iteration latency ceiling. The measured
// quantity is the arithmetic mean across all loop iterations.
func (c *Contract) LatencyMax(maxLatency time.Duration) *Contract {
	c.latencyMax = maxLatency
	c.latencyEnabled = true
	return c
}

// Loop wraps b.Loop, capturing the per-iteration latency of the
// just-completed iteration and seeding the alloc baseline on the
// first call. Drop-in replacement for b.Loop:
//
//	for c.Loop() { ... }
func (c *Contract) Loop() bool {
	if c.started && c.latencyEnabled {
		c.totalLat += time.Since(c.iterStart)
	}
	if !c.started {
		if c.allocsEnabled {
			runtime.ReadMemStats(&c.preMem)
		}
		c.tb.ResetTimer()
		c.started = true
	}
	next := c.loopFn()
	if next {
		c.iterations++
		if c.latencyEnabled {
			c.iterStart = time.Now()
		}
	}
	return next
}

// End reports the measured metrics via b.ReportMetric and fails the
// benchmark if any declared ceiling is exceeded. If no iterations
// ran (e.g. -benchtime=0x), End is a no-op so a degenerate run
// doesn't divide by zero.
func (c *Contract) End() {
	c.tb.Helper()
	if c.iterations == 0 {
		return
	}
	if c.allocsEnabled {
		var post runtime.MemStats
		runtime.ReadMemStats(&post)
		allocs := (post.Mallocs - c.preMem.Mallocs) / c.iterations
		c.tb.ReportMetric(float64(allocs), "contract-allocs/op")
		if allocs > c.allocsMax {
			c.tb.Fatalf("contract violation: allocs/op=%d > AllocsMax=%d", allocs, c.allocsMax)
		}
	}
	if c.latencyEnabled {
		meanNs := float64(c.totalLat.Nanoseconds()) / float64(c.iterations)
		c.tb.ReportMetric(meanNs, "contract-ns/op-mean")
		if time.Duration(meanNs) > c.latencyMax {
			c.tb.Fatalf("contract violation: mean ns/op=%.0f > LatencyMax=%v", meanNs, c.latencyMax)
		}
	}
}
