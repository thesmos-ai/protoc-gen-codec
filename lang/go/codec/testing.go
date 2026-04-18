// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"encoding/json"
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
		if err := PT(&owned).UnmarshalCodec(buf); err != nil {
			t.Fatalf("UnmarshalCodec: %v", err)
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
		for i := range len(valid) {
			var got T
			PT(&got).UnmarshalCodec(valid[:i])
		}
		for i := range len(valid) {
			corrupted := make([]byte, len(valid))
			copy(corrupted, valid)
			corrupted[i] ^= 0xFF
			var got T
			PT(&got).UnmarshalCodec(corrupted)
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
			ptr.MarshalToCodec(buf)
		}
	})

	b.Run("Codec/Unmarshal", func(b *testing.B) {
		ptr := PT(&sample)
		data, _ := ptr.MarshalCodec()
		b.ResetTimer()
		for range b.N {
			var got T
			PT(&got).UnmarshalCodec(data)
		}
	})

	b.Run("JSON/Marshal", func(b *testing.B) {
		for range b.N {
			json.Marshal(sample)
		}
	})

	b.Run("JSON/Unmarshal", func(b *testing.B) {
		data, _ := json.Marshal(sample)
		b.ResetTimer()
		for range b.N {
			var got T
			json.Unmarshal(data, &got)
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
