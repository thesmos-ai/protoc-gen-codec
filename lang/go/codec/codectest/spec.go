// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Package codectest is the testing-support package for the codec runtime.
//
// The public surface is a single declarative Spec[T] consumed by three
// role-specific runners:
//
//	codectest.RunSuite[T, PT]       // from a Test* function
//	codectest.RunBenchSuite[T, PT]  // from a Benchmark* function
//	codectest.RunFuzzSuite[T, PT]   // from a Fuzz* function
//
// Consumers author one Spec per annotated type and feed it to all three
// entry points. The Spec declares the baseline sample plus optional
// coverage hints (field-number categories), a warm-path grower, a
// nil-pointer sample, and a rapid-based generator for property-based
// testing. Every optional field defaults to nil/empty — runners skip the
// corresponding subtests so trivial types need only Sample + (optionally)
// UnknownFieldNum.
//
// Individual assertions (AssertRoundtrip, AssertCorruptPackedBody, …)
// live in assertions.go. The runners here are declarative compositions
// of those assertions; they do not carry their own assertion logic.
//
// This package depends on pgregory.net/rapid for property-based testing.
// Consumers of the runtime-only codec package are not transitively
// exposed to rapid unless they import codectest explicitly.
package codectest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec"
)

// TB is the common interface between *testing.T, *testing.B, and *rapid.T.
// Assertions accept TB so they can run under both regular Go tests and
// rapid property-based checks.
type TB interface {
	Helper()
	Fatalf(string, ...any)
}

// Spec declares the testing plan for a codec-annotated type. One spec per
// type, reused across Test/Bench/Fuzz entry points.
//
// Required: Sample. Everything else is optional; zero/nil values cause
// the corresponding subtests to be skipped. UnknownFieldNum defaults to
// 9999 when unset (a field number not present in any realistic schema).
type Spec[T any] struct {
	// Sample is the baseline fully-populated instance used by most
	// assertions.
	Sample T

	// Variants are extra samples merged into AllFieldsWireTypeMismatch so
	// discriminated-union-shape types (where one sample populates only
	// one payload field) still see every declared field on the wire.
	Variants []T

	// Grower, if non-nil, is a strictly-larger instance (at least one
	// more element in a repeated field) that exercises the warm-path
	// slice-growth branches in UnmarshalCodec.
	Grower *T

	// NilPointerSample, if non-nil, has a nil entry in one of its []*T
	// fields. Exercises the `if elem == nil { continue }` branches in
	// SizeCodec and MarshalCodecInternal.
	NilPointerSample *T

	// Generator, if non-nil, draws a T from a rapid random source. When
	// set, RunSuite runs property-based wire-stability, reset, and
	// size-accuracy subtests over spec.Generator's distribution.
	Generator func(*rapid.T) T

	// UnknownFieldNum is any field number NOT declared in the type's
	// schema. Used for the unknown-field-skip and unknown-field-bad-
	// wire-type exercises. Defaults to 9999 if zero.
	UnknownFieldNum int32

	// ScalarVarintFields lists field numbers declared as varint-wire
	// scalars (int32 / int64 / uint32 / uint64 / sint32 / sint64 / bool
	// / enum). Each gets an AssertCorruptScalarVarint exercise.
	ScalarVarintFields []int32

	// PackedFields lists field numbers declared as repeated packed
	// scalars. Each gets an AssertCorruptPackedBody exercise.
	PackedFields []int32

	// MapFields lists field numbers declared as map<K,V>. Each gets an
	// AssertCorruptMapEntryValue exercise.
	MapFields []int32

	// RepeatedMessageFields lists field numbers declared as
	// `repeated MsgType`. Each gets an AssertCorruptRepeatedMessagePrescan
	// exercise.
	RepeatedMessageFields []int32

	// WKTFields lists field numbers declared as google.protobuf.Timestamp
	// or Duration. Each gets an AssertCorruptWKTPayload exercise.
	WKTFields []int32

	// Fixed64Fields lists field numbers declared as scalar fixed64 /
	// sfixed64 / double. Each gets an AssertCorruptFixedWidth exercise
	// with width=8.
	Fixed64Fields []int32

	// Fixed32Fields lists field numbers declared as scalar fixed32 /
	// sfixed32 / float. Each gets an AssertCorruptFixedWidth exercise
	// with width=4.
	Fixed32Fields []int32

	// FixedLenBytesFields lists (field number, declared length) for
	// bytes fields annotated with codec.fixed_len. Each gets an
	// AssertCorruptFixedLenBytes exercise covering the three error
	// branches (bad length varint, wrong length, short body).
	FixedLenBytesFields []FixedLenField
}

// FixedLenField identifies a bytes field declared with codec.fixed_len.
// Used in Spec.FixedLenBytesFields to drive AssertCorruptFixedLenBytes.
type FixedLenField struct {
	Num    int32
	Length uint32
}

func (s Spec[T]) unknownFieldNum() int32 {
	if s.UnknownFieldNum == 0 {
		return 9999
	}
	return s.UnknownFieldNum
}

// RunSuite runs the full Test-suite subtree for T: behavioral (roundtrip,
// reset, nil-safe, cross-format, wire-size, corruption), coverage
// (per-field wire-type mismatch, short buffer, tag corruption, per-field
// category probes), and property-based (if spec.Generator is set).
func RunSuite[T any, PT interface {
	*T
	codec.Marshaler
}](t *testing.T, spec Spec[T]) {
	t.Helper()

	// Behavioral
	t.Run("Roundtrip", func(t *testing.T) {
		t.Parallel()
		AssertRoundtrip[T, PT](t, spec.Sample)
	})
	t.Run("Roundtrip/Zero", func(t *testing.T) {
		t.Parallel()
		var zero T
		AssertRoundtrip[T, PT](t, zero)
	})
	t.Run("Reset", func(t *testing.T) {
		t.Parallel()
		AssertReset[T, PT](t, spec.Sample)
	})
	t.Run("NilSafe", func(t *testing.T) {
		t.Parallel()
		AssertNilSafe[T, PT](t)
	})
	t.Run("CrossFormat", func(t *testing.T) {
		t.Parallel()
		AssertCrossFormatConsistency[T, PT](t, spec.Sample)
	})
	t.Run("WireSize", func(t *testing.T) {
		t.Parallel()
		AssertWireSmallerThanJSON[T, PT](t, spec.Sample)
	})
	t.Run("Corruption", func(t *testing.T) {
		t.Parallel()
		for _, s := range append([]T{spec.Sample}, spec.Variants...) {
			AssertCorruption[T, PT](t, s)
		}
	})

	// Coverage — schema-agnostic
	t.Run("CorruptTag", func(t *testing.T) {
		t.Parallel()
		AssertCorruptTag[T, PT](t)
	})
	t.Run("MarshalToCodec", func(t *testing.T) {
		t.Parallel()
		AssertMarshalToCodec[T, PT](t, spec.Sample)
	})
	t.Run("MarshalToShortBuffer", func(t *testing.T) {
		t.Parallel()
		AssertMarshalToShortBuffer[T, PT](t, spec.Sample)
	})
	t.Run("UnknownFieldInvalidWireType", func(t *testing.T) {
		t.Parallel()
		AssertUnknownFieldInvalidWireType[T, PT](t, spec.Sample, spec.unknownFieldNum())
	})
	t.Run("UnknownFieldSkipped", func(t *testing.T) {
		t.Parallel()
		AssertUnknownFieldSkipped[T, PT](t, spec.Sample, spec.unknownFieldNum())
	})
	t.Run("AllFieldsWireTypeMismatch", func(t *testing.T) {
		t.Parallel()
		all := append([]T{spec.Sample}, spec.Variants...)
		AssertAllFieldsWireTypeMismatch[T, PT](t, all...)
	})

	// Coverage — schema-aware (opt-in)
	if spec.Grower != nil {
		t.Run("WarmPathGrowth", func(t *testing.T) {
			t.Parallel()
			AssertWarmPathGrowth[T, PT](t, spec.Sample, *spec.Grower)
		})
	}
	if spec.NilPointerSample != nil {
		t.Run("NilPointerElement", func(t *testing.T) {
			t.Parallel()
			AssertMarshalWithNilPointerElement[T, PT](t, *spec.NilPointerSample)
		})
	}
	if len(spec.RepeatedMessageFields) > 0 {
		t.Run("PrescanSkipsAllWireTypes", func(t *testing.T) {
			t.Parallel()
			AssertPrescanSkipsAllWireTypes[T, PT](t, spec.Sample, spec.unknownFieldNum())
		})
	}
	runFieldCategoryTests[T, PT](t, spec)

	// Property-based (opt-in)
	if spec.Generator != nil {
		runPBT[T, PT](t, spec.Generator)
	}
}

// RunBenchSuite runs Codec vs JSON marshal/unmarshal benchmarks for T,
// using spec.Sample as the fixture. Consumers typically pair this with
// type-specific PooledUnmarshal benchmarks (warm-path, reused receiver)
// since those are shape-specific and not captured by the spec.
func RunBenchSuite[T any, PT interface {
	*T
	codec.Marshaler
}](b *testing.B, spec Spec[T]) {
	b.Helper()

	b.Run("Codec/MarshalTo", func(b *testing.B) {
		s := spec.Sample
		ptr := PT(&s)
		buf := make([]byte, ptr.SizeCodec())
		b.ResetTimer()
		for b.Loop() {
			_, _ = ptr.MarshalToCodec(buf)
		}
	})

	b.Run("Codec/Unmarshal", func(b *testing.B) {
		s := spec.Sample
		ptr := PT(&s)
		data, _ := ptr.MarshalCodec()
		b.ResetTimer()
		for b.Loop() {
			var got T
			_ = PT(&got).UnmarshalCodec(data)
		}
	})

	b.Run("JSON/Marshal", func(b *testing.B) {
		s := spec.Sample
		for b.Loop() {
			_, _ = json.Marshal(s)
		}
	})

	b.Run("JSON/Unmarshal", func(b *testing.B) {
		s := spec.Sample
		data, _ := json.Marshal(s)
		b.ResetTimer()
		for b.Loop() {
			var got T
			_ = json.Unmarshal(data, &got)
		}
	})
}

// RunFuzzSuite registers spec.Sample + spec.Variants as corpus seeds and
// runs the wire-stability fuzz invariant: for any input that successfully
// decodes, a second marshal/unmarshal cycle produces byte-identical wire.
func RunFuzzSuite[T any, PT interface {
	*T
	codec.Marshaler
}](f *testing.F, spec Spec[T]) {
	f.Helper()

	seeds := append([]T{spec.Sample}, spec.Variants...)
	for i := range seeds {
		if buf, _ := PT(&seeds[i]).MarshalCodec(); buf != nil {
			f.Add(buf)
		}
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		var first T
		if err := PT(&first).UnmarshalCodec(data); err != nil {
			return
		}
		re1, err := PT(&first).MarshalCodec()
		if err != nil {
			t.Fatalf("first re-MarshalCodec: %v", err)
		}
		var second T
		if uerr := PT(&second).UnmarshalCodec(re1); uerr != nil {
			t.Fatalf("second UnmarshalCodec: %v", uerr)
		}
		re2, merr := PT(&second).MarshalCodec()
		if merr != nil {
			t.Fatalf("second re-MarshalCodec: %v", merr)
		}
		if !bytes.Equal(re1, re2) {
			t.Fatalf("wire not stable after one cycle:\n  re1=%x\n  re2=%x", re1, re2)
		}
	})
}

// runFieldCategoryTests emits one subtest per (category, field-number)
// pair declared on the spec.
func runFieldCategoryTests[T any, PT interface {
	*T
	codec.Marshaler
}](t *testing.T, spec Spec[T]) {
	for _, fn := range spec.ScalarVarintFields {
		t.Run(fmt.Sprintf("CorruptScalarVarint/Field%d", fn), func(t *testing.T) {
			t.Parallel()
			AssertCorruptScalarVarint[T, PT](t, fn)
		})
	}
	for _, fn := range spec.PackedFields {
		t.Run(fmt.Sprintf("CorruptPackedBody/Field%d", fn), func(t *testing.T) {
			t.Parallel()
			AssertCorruptPackedBody[T, PT](t, fn)
		})
	}
	for _, fn := range spec.MapFields {
		t.Run(fmt.Sprintf("CorruptMapEntry/Field%d", fn), func(t *testing.T) {
			t.Parallel()
			AssertCorruptMapEntryValue[T, PT](t, fn)
		})
	}
	for _, fn := range spec.RepeatedMessageFields {
		t.Run(fmt.Sprintf("CorruptRepeatedMessagePrescan/Field%d", fn), func(t *testing.T) {
			t.Parallel()
			AssertCorruptRepeatedMessagePrescan[T, PT](t, fn)
		})
	}
	for _, fn := range spec.WKTFields {
		t.Run(fmt.Sprintf("CorruptWKTPayload/Field%d", fn), func(t *testing.T) {
			t.Parallel()
			AssertCorruptWKTPayload[T, PT](t, fn)
		})
	}
	for _, fn := range spec.Fixed64Fields {
		t.Run(fmt.Sprintf("CorruptFixed64Short/Field%d", fn), func(t *testing.T) {
			t.Parallel()
			AssertCorruptFixedWidth[T, PT](t, fn, 8)
		})
	}
	for _, fn := range spec.Fixed32Fields {
		t.Run(fmt.Sprintf("CorruptFixed32Short/Field%d", fn), func(t *testing.T) {
			t.Parallel()
			AssertCorruptFixedWidth[T, PT](t, fn, 4)
		})
	}
	for _, flf := range spec.FixedLenBytesFields {
		t.Run(fmt.Sprintf("CorruptFixedLenBytes/Field%d", flf.Num), func(t *testing.T) {
			t.Parallel()
			AssertCorruptFixedLenBytes[T, PT](t, flf.Num, flf.Length)
		})
	}
}

// runPBT emits property-based subtests via rapid. Each property draws
// random samples through spec.Generator and asserts an invariant. The
// PBT invariant differs from RunSuite's example-based Roundtrip:
// wire-stability after one marshal/unmarshal cycle (the fuzz invariant),
// because rapid can generate proto3-equivalent shapes (empty-slice vs
// nil) that DeepEqual distinguishes but the wire does not.
func runPBT[T any, PT interface {
	*T
	codec.Marshaler
}](t *testing.T, gen func(*rapid.T) T) {
	t.Run("PBT/WireStability", func(t *testing.T) {
		t.Parallel()
		rapid.Check(t, func(rt *rapid.T) {
			sample := gen(rt)
			AssertWireStable[T, PT](rt, sample)
		})
	})
	t.Run("PBT/Reset", func(t *testing.T) {
		t.Parallel()
		rapid.Check(t, func(rt *rapid.T) {
			sample := gen(rt)
			AssertReset[T, PT](rt, sample)
		})
	})
	t.Run("PBT/SizeAccuracy", func(t *testing.T) {
		t.Parallel()
		rapid.Check(t, func(rt *rapid.T) {
			sample := gen(rt)
			ptr := PT(&sample)
			buf, err := ptr.MarshalCodec()
			if err != nil {
				rt.Fatalf("MarshalCodec: %v", err)
			}
			if len(buf) != ptr.SizeCodec() {
				rt.Fatalf("SizeCodec=%d, len(MarshalCodec)=%d", ptr.SizeCodec(), len(buf))
			}
		})
	})
}
