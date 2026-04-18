// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// TB is the common interface between *testing.T and *rapid.T.
type TB interface {
	Helper()
	Fatalf(string, ...any)
}

// AssertRoundtrip verifies MarshalCodec → UnmarshalCodec identity and SizeCodec accuracy.
func AssertRoundtrip[T any, PT interface {
	*T
	Marshaler
}](t TB, original T) {
	t.Helper()
	ptr := PT(&original)
	size := ptr.SizeCodec()
	buf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	if buf == nil && size == 0 {
		return
	}
	if len(buf) != size {
		t.Fatalf("SizeCodec()=%d len(MarshalCodec())=%d", size, len(buf))
	}
	var got T
	if err := PT(&got).UnmarshalCodec(buf); err != nil {
		t.Fatalf("UnmarshalCodec: %v", err)
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("roundtrip mismatch:\n  want: %+v\n  got:  %+v", original, got)
	}
}

// AssertReset verifies ResetCodec produces a semantically empty receiver:
// re-marshaling after reset must yield zero wire bytes (the proto3 equivalent
// of "absent"). Backing storage for slices and maps may be preserved for
// reuse — this is an optimization invisible on the wire.
//
// The input is marshal/unmarshaled first to obtain an independent copy so
// ResetCodec doesn't mutate shared backing storage (e.g. clear(map) on a
// keep_capacity map), which would otherwise race with other parallel
// subtests that share the same underlying map/slice references.
func AssertReset[T any, PT interface {
	*T
	Marshaler
}](t TB, populated T) {
	t.Helper()
	buf, err := PT(&populated).MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	var owned T
	if buf != nil {
		if uerr := PT(&owned).UnmarshalCodec(buf); uerr != nil {
			t.Fatalf("UnmarshalCodec: %v", uerr)
		}
	}
	ptr := PT(&owned)
	ptr.ResetCodec()
	if sz := ptr.SizeCodec(); sz != 0 {
		t.Fatalf("ResetCodec did not produce empty wire: SizeCodec()=%d", sz)
	}
	reBuf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec after ResetCodec: %v", err)
	}
	if len(reBuf) != 0 {
		t.Fatalf("ResetCodec did not produce empty wire: MarshalCodec len=%d", len(reBuf))
	}
}

// AssertNilSafe verifies nil pointer safety for all Codec methods.
func AssertNilSafe[T any, PT interface {
	*T
	Marshaler
}](t TB) {
	t.Helper()
	var nilPtr PT
	if nilPtr.SizeCodec() != 0 {
		t.Fatalf("nil SizeCodec should be 0")
	}
	buf, err := nilPtr.MarshalCodec()
	if err != nil || buf != nil {
		t.Fatalf("nil MarshalCodec: buf=%v err=%v", buf, err)
	}
	n, err := nilPtr.MarshalToCodec(nil)
	if err != nil || n != 0 {
		t.Fatalf("nil MarshalToCodec: n=%d err=%v", n, err)
	}
	nilPtr.ResetCodec()
}

// RunTestSuite runs the standard codec test battery for a type.
// Covers: roundtrip, zero-value roundtrip, reset, nil safety,
// cross-format consistency with JSON, and corruption injection
// (truncation + byte-flip at every offset).
func RunTestSuite[T any, PT interface {
	*T
	Marshaler
}](t *testing.T, sample T) {
	t.Helper()

	t.Run("Roundtrip", func(t *testing.T) {
		t.Parallel()
		AssertRoundtrip[T, PT](t, sample)
	})

	t.Run("Roundtrip/Zero", func(t *testing.T) {
		t.Parallel()
		var zero T
		AssertRoundtrip[T, PT](t, zero)
	})

	t.Run("Reset", func(t *testing.T) {
		t.Parallel()
		AssertReset[T, PT](t, sample)
	})

	t.Run("NilSafe", func(t *testing.T) {
		t.Parallel()
		AssertNilSafe[T, PT](t)
	})

	t.Run("CrossFormat", func(t *testing.T) {
		t.Parallel()
		AssertCrossFormatConsistency[T, PT](t, sample)
	})

	t.Run("WireSize", func(t *testing.T) {
		t.Parallel()
		AssertWireSmallerThanJSON[T, PT](t, sample)
	})

	t.Run("Corruption", func(t *testing.T) {
		t.Parallel()
		ptr := PT(&sample)
		valid, err := ptr.MarshalCodec()
		if err != nil {
			t.Fatalf("MarshalCodec: %v", err)
		}
		if valid == nil {
			return
		}
		for i := range valid {
			var got T
			_ = PT(&got).UnmarshalCodec(valid[:i])
		}
		for i := range valid {
			corrupted := make([]byte, len(valid))
			copy(corrupted, valid)
			corrupted[i] ^= 0xFF
			var got T
			_ = PT(&got).UnmarshalCodec(corrupted)
		}
	})
}

// RunBenchSuite runs codec vs JSON marshal/unmarshal benchmarks.
func RunBenchSuite[T any, PT interface {
	*T
	Marshaler
}](b *testing.B, sample T) {
	b.Helper()

	b.Run("Codec/MarshalTo", func(b *testing.B) {
		ptr := PT(&sample)
		buf := make([]byte, ptr.SizeCodec())
		b.ResetTimer()
		for range b.N {
			_, _ = ptr.MarshalToCodec(buf)
		}
	})

	b.Run("Codec/Unmarshal", func(b *testing.B) {
		ptr := PT(&sample)
		data, _ := ptr.MarshalCodec()
		b.ResetTimer()
		for range b.N {
			var got T
			_ = PT(&got).UnmarshalCodec(data)
		}
	})

	b.Run("JSON/Marshal", func(b *testing.B) {
		for range b.N {
			_, _ = json.Marshal(sample)
		}
	})

	b.Run("JSON/Unmarshal", func(b *testing.B) {
		data, _ := json.Marshal(sample)
		b.ResetTimer()
		for range b.N {
			var got T
			_ = json.Unmarshal(data, &got)
		}
	})
}

// RunFuzzRoundtrip registers seeds and runs a roundtrip fuzz target.
// Usage: func FuzzVertexDef(f *testing.F) { codec.RunFuzzRoundtrip[T](f, sample1, sample2) }
func RunFuzzRoundtrip[T any, PT interface {
	*T
	Marshaler
}](f *testing.F, samples ...T) {
	f.Helper()
	for i := range samples {
		if buf, _ := PT(&samples[i]).MarshalCodec(); buf != nil {
			f.Add(buf)
		}
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		var first T
		if err := PT(&first).UnmarshalCodec(data); err != nil {
			return
		}
		re, err := PT(&first).MarshalCodec()
		if err != nil {
			t.Fatalf("re-MarshalCodec: %v", err)
		}
		var second T
		if err := PT(&second).UnmarshalCodec(re); err != nil {
			t.Fatalf("second UnmarshalCodec: %v", err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("roundtrip mismatch")
		}
	})
}

// AssertWireSmallerThanJSON verifies codec wire size is smaller than JSON.
func AssertWireSmallerThanJSON[T any, PT interface {
	*T
	Marshaler
}](t TB, sample T) {
	t.Helper()
	ptr := PT(&sample)
	vtBuf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	jsonBuf, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(vtBuf) >= len(jsonBuf) {
		t.Fatalf("codec %d bytes >= JSON %d bytes", len(vtBuf), len(jsonBuf))
	}
}

// AssertCrossFormatConsistency verifies that codec and JSON roundtrips
// produce the same struct from the same input.
func AssertCrossFormatConsistency[T any, PT interface {
	*T
	Marshaler
}](t TB, original T) {
	t.Helper()

	codecBytes, err := PT(&original).MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}

	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var fromCodec T
	if codecBytes != nil {
		if err := PT(&fromCodec).UnmarshalCodec(codecBytes); err != nil {
			t.Fatalf("UnmarshalCodec: %v", err)
		}
	}

	var fromJSON T
	if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if !reflect.DeepEqual(fromCodec, fromJSON) {
		t.Fatalf("cross-format mismatch:\n  codec: %+v\n  json:  %+v", fromCodec, fromJSON)
	}
}

// AssertZeroMarshal verifies that a zero-value message marshals to the
// empty wire and unmarshals back cleanly. Exercises the size==0 branch
// of MarshalCodec and the all-absent decode path of UnmarshalCodec.
func AssertZeroMarshal[T any, PT interface {
	*T
	Marshaler
}](t TB) {
	t.Helper()
	var zero T
	ptr := PT(&zero)
	if sz := ptr.SizeCodec(); sz != 0 {
		t.Fatalf("SizeCodec on zero value: want 0, got %d", sz)
	}
	buf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec on zero value: %v", err)
	}
	if len(buf) != 0 {
		t.Fatalf("MarshalCodec on zero value produced %d bytes", len(buf))
	}
	// Decode empty wire must succeed and produce zero value.
	var got T
	if err := PT(&got).UnmarshalCodec(nil); err != nil {
		t.Fatalf("UnmarshalCodec(nil): %v", err)
	}
}

// AssertUnknownFieldSkipped appends a synthetic wire record for a field
// number the target doesn't know about and verifies the decoder skips it
// cleanly while preserving the known fields. Exercises the default case
// of the decode switch (SkipField code path).
//
// unknownFieldNum MUST not be declared in the target message — pick a
// number well above the highest field number in the schema (e.g., 999).
func AssertUnknownFieldSkipped[T any, PT interface {
	*T
	Marshaler
}](t TB, sample T, unknownFieldNum int32) {
	t.Helper()
	ptr := PT(&sample)
	buf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	// Append a varint-wire unknown field: tag = (num<<3 | 0), value = 0.
	tag := uint64(unknownFieldNum) << 3
	var tagBuf [10]byte
	tn := EncodeVarint(tagBuf[:], tag)
	buf = append(buf, tagBuf[:tn]...)
	buf = append(buf, 0x00) // varint value 0
	// Decode must succeed.
	var got T
	if err := PT(&got).UnmarshalCodec(buf); err != nil {
		t.Fatalf("UnmarshalCodec with unknown field: %v", err)
	}
	// Known fields must be intact.
	if !reflect.DeepEqual(sample, got) {
		t.Fatalf("unknown field corrupted known fields:\n  want: %+v\n  got:  %+v", sample, got)
	}
}

// AssertWireTypeMismatch crafts a wire record with the wrong wire type
// for the given field number and verifies ErrInvalidWireType is returned.
// Exercises the wire-type-check error arms in UnmarshalCodec case switches.
//
// fieldNum is the proto field number; wrongWireType is any wire type other
// than the one declared for that field (e.g., field is length-delimited but
// we send a varint).
func AssertWireTypeMismatch[T any, PT interface {
	*T
	Marshaler
}](t TB, fieldNum int32, wrongWireType uint64) {
	t.Helper()
	tag := uint64(fieldNum)<<3 | (wrongWireType & 0x7)
	var tagBuf [10]byte
	tn := EncodeVarint(tagBuf[:], tag)
	// Append a minimal payload for the wrong wire type.
	data := append([]byte{}, tagBuf[:tn]...)
	switch wrongWireType {
	case 0: // varint
		data = append(data, 0x00)
	case 1: // fixed64
		data = append(data, 0, 0, 0, 0, 0, 0, 0, 0)
	case 2: // len-delimited
		data = append(data, 0x00) // zero-length
	case 5: // fixed32
		data = append(data, 0, 0, 0, 0)
	}
	var got T
	err := PT(&got).UnmarshalCodec(data)
	if !errors.Is(err, ErrInvalidWireType) {
		t.Fatalf("field %d wire %d: want ErrInvalidWireType, got %v", fieldNum, wrongWireType, err)
	}
}

// AssertShortBuffer verifies MarshalToCodec returns ErrBufferTooShort when
// the caller provides a buffer smaller than SizeCodec().
func AssertShortBuffer[T any, PT interface {
	*T
	Marshaler
}](t TB, sample T) {
	t.Helper()
	ptr := PT(&sample)
	size := ptr.SizeCodec()
	if size <= 0 {
		// Sample marshals to empty — short-buffer test inapplicable.
		return
	}
	short := make([]byte, size-1)
	_, err := ptr.MarshalToCodec(short)
	if !errors.Is(err, ErrBufferTooShort) {
		t.Fatalf("MarshalToCodec into undersized buffer: want ErrBufferTooShort, got %v", err)
	}
}

// WireMismatch describes one (field number, wrong wire type) pair for
// RunCoverageSuite. The wrong wire type must differ from the declared
// wire type of the field — e.g., if field 4 is a string (wire 2), use
// WireMismatch{4, 0} to send a varint instead.
type WireMismatch struct {
	FieldNum      int32
	WrongWireType uint64
}

// RunCoverageSuite extends RunTestSuite with branch-coverage-focused
// sub-tests. Consumers add this to hit higher coverage on generated
// code without hand-rolling the sub-tests.
//
// unknownFieldNum should be a field number not declared on T (e.g., 999
// or any number above the highest declared field). For wire-type mismatch
// testing, provide (knownFieldNum, wrongWireType) pairs covering at least
// one varint field and one length-delimited field when applicable.
func RunCoverageSuite[T any, PT interface {
	*T
	Marshaler
}](t *testing.T, sample T, unknownFieldNum int32, wireMismatches ...WireMismatch) {
	t.Helper()
	t.Run("ZeroMarshal", func(t *testing.T) {
		t.Parallel()
		AssertZeroMarshal[T, PT](t)
	})
	t.Run("UnknownFieldSkipped", func(t *testing.T) {
		t.Parallel()
		AssertUnknownFieldSkipped[T, PT](t, sample, unknownFieldNum)
	})
	t.Run("ShortBuffer", func(t *testing.T) {
		t.Parallel()
		AssertShortBuffer[T, PT](t, sample)
	})
	for _, wm := range wireMismatches {
		t.Run(fmt.Sprintf("WireTypeMismatch_Field%d_Wire%d", wm.FieldNum, wm.WrongWireType), func(t *testing.T) {
			t.Parallel()
			AssertWireTypeMismatch[T, PT](t, wm.FieldNum, wm.WrongWireType)
		})
	}
}
