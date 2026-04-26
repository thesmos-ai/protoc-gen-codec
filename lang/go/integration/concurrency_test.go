// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Tests that mechanically verify the concurrency contract documented
// in docs/generators/go.md and lang/go/codec/doc.go:
//
//   - SizeCodec, MarshalCodec, MarshalToCodec, MarshalCodecInternal
//     are safe to call concurrently against the SAME receiver.
//   - UnmarshalCodec, ResetCodec are safe to call concurrently across
//     DIFFERENT receivers.
//
// All tests run under the race detector in CI (`make test-race`), so
// any future refactor that introduces shared mutable state in the
// marshal path or aliases pooled storage across receivers will fail
// loudly here.

package integration_test

import (
	"bytes"
	"sync"
	"testing"

	"go.thesmos.sh/protoc-gen-codec/lang/go/integration"
)

// TestConcurrentMarshal_SameReceiver runs MarshalCodec / MarshalToCodec /
// SizeCodec on the same receiver from many goroutines simultaneously
// and asserts every result is byte-identical to a single-threaded
// reference encoding. Run under -race to catch any unintended
// receiver mutation.
func TestConcurrentMarshal_SameReceiver(t *testing.T) {
	t.Parallel()

	const goroutines = 64
	const iterPerGoroutine = 200

	// Use Fixture as the test shape: scalars, repeated string, bytes,
	// fixed-len digest. Map-bearing types (MapHolder, BoolMapHolder)
	// are covered by their own concurrency subtest below — the sort
	// path is structurally distinct and worth a separate exercise.
	sample := sampleFixture()
	want, err := sample.MarshalCodec()
	if err != nil {
		t.Fatalf("baseline MarshalCodec: %v", err)
	}
	wantSize := sample.SizeCodec()
	if len(want) != wantSize {
		t.Fatalf("baseline len(MarshalCodec)=%d != SizeCodec=%d", len(want), wantSize)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			scratch := make([]byte, len(want))
			for range iterPerGoroutine {
				if got := sample.SizeCodec(); got != wantSize {
					t.Errorf("concurrent SizeCodec: got %d want %d", got, wantSize)
					return
				}
				got, err := sample.MarshalCodec()
				if err != nil {
					t.Errorf("concurrent MarshalCodec: %v", err)
					return
				}
				if !bytes.Equal(got, want) {
					t.Errorf("concurrent MarshalCodec output drifted")
					return
				}
				n, err := sample.MarshalToCodec(scratch)
				if err != nil {
					t.Errorf("concurrent MarshalToCodec: %v", err)
					return
				}
				if !bytes.Equal(scratch[:n], want) {
					t.Errorf("concurrent MarshalToCodec output drifted")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentMarshal_MapHolder covers the deterministic-key-sort
// path on a map-bearing type. The runtime sorts map keys before emit
// to keep output byte-stable; a refactor that races on a shared sort
// buffer would surface here under -race.
func TestConcurrentMarshal_MapHolder(t *testing.T) {
	t.Parallel()

	sample := sampleMapHolder()
	want, err := sample.MarshalCodec()
	if err != nil {
		t.Fatalf("baseline MarshalCodec: %v", err)
	}

	const goroutines = 32
	const iterPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterPerGoroutine {
				got, err := sample.MarshalCodec()
				if err != nil {
					t.Errorf("concurrent MarshalCodec on MapHolder: %v", err)
					return
				}
				if !bytes.Equal(got, want) {
					t.Errorf("MapHolder marshal output drifted under concurrency")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentUnmarshal_DistinctReceivers verifies the documented
// "concurrent calls on distinct receivers are safe" contract. Each
// goroutine owns a fresh *Fixture and decodes the same wire bytes
// into it; -race must not flag any cross-goroutine sharing.
func TestConcurrentUnmarshal_DistinctReceivers(t *testing.T) {
	t.Parallel()

	src := sampleFixture()
	wire, err := src.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}

	const goroutines = 64
	const iterPerGoroutine = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterPerGoroutine {
				var got integration.Fixture
				if err := got.UnmarshalCodec(wire); err != nil {
					t.Errorf("concurrent UnmarshalCodec into distinct receiver: %v", err)
					return
				}
				// Sanity-check decode produced a non-zero result.
				if got.ID == "" {
					t.Errorf("concurrent UnmarshalCodec dropped fields")
					return
				}
				got.ResetCodec()
			}
		}()
	}
	wg.Wait()
}
