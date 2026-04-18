// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	stderrors "errors"
	"math"
	"testing"

	"pgregory.net/rapid"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec"
)

// ---------------------------------------------------------------------------
// Sov
// ---------------------------------------------------------------------------

func TestSov_KnownValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input uint64
		want  int
	}{
		{0, 1},
		{1, 1},
		{127, 1},
		{128, 2},
		{16383, 2},
		{16384, 3},
		{1<<21 - 1, 3},
		{1 << 21, 4},
		{1<<28 - 1, 4},
		{1 << 28, 5},
		{1<<35 - 1, 5},
		{1 << 35, 6},
		{1<<42 - 1, 6},
		{1 << 42, 7},
		{1<<49 - 1, 7},
		{1 << 49, 8},
		{1<<56 - 1, 8},
		{1 << 56, 9},
		{1<<63 - 1, 9},
		{1 << 63, 10},
		{math.MaxUint64, 10},
	}

	for _, tc := range cases {
		got := codec.Sov(tc.input)
		if got != tc.want {
			t.Errorf("Sov(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestSov_MatchesEncodeVarint(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Uint64().Draw(t, "v")
		size := codec.Sov(v)
		var buf [10]byte
		n := codec.EncodeVarint(buf[:], v)
		if n != size {
			t.Fatalf("Sov(%d)=%d but EncodeVarint wrote %d bytes", v, size, n)
		}
	})
}

// ---------------------------------------------------------------------------
// EncodeVarint / DecodeVarint roundtrip
// ---------------------------------------------------------------------------

func TestVarint_Roundtrip_Zero(t *testing.T) {
	t.Parallel()
	assertVarintRoundtrip(t, 0)
}

func TestVarint_Roundtrip_One(t *testing.T) {
	t.Parallel()
	assertVarintRoundtrip(t, 1)
}

func TestVarint_Roundtrip_MaxSingleByte(t *testing.T) {
	t.Parallel()
	assertVarintRoundtrip(t, 127)
}

func TestVarint_Roundtrip_MinTwoByte(t *testing.T) {
	t.Parallel()
	assertVarintRoundtrip(t, 128)
}

func TestVarint_Roundtrip_MaxUint64(t *testing.T) {
	t.Parallel()
	assertVarintRoundtrip(t, math.MaxUint64)
}

func TestVarint_Roundtrip_MaxInt64(t *testing.T) {
	t.Parallel()
	assertVarintRoundtrip(t, uint64(math.MaxInt64))
}

func TestVarint_Roundtrip_NegativeAsUint64(t *testing.T) {
	t.Parallel()
	assertVarintRoundtrip(t, uint64(math.MaxUint64)) // -1 as uint64
}

func TestVarint_Roundtrip_PBT(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Uint64().Draw(t, "v")
		var buf [10]byte
		n := codec.EncodeVarint(buf[:], v)
		got, consumed := codec.DecodeVarint(buf[:n])
		if consumed != n {
			t.Fatalf("EncodeVarint wrote %d bytes, DecodeVarint consumed %d", n, consumed)
		}
		if got != v {
			t.Fatalf("roundtrip failed: encoded %d, decoded %d", v, got)
		}
	})
}

func TestDecodeVarint_EmptyInput(t *testing.T) {
	t.Parallel()

	_, n := codec.DecodeVarint(nil)
	if n != -1 {
		t.Fatalf("expected -1 for empty input, got %d", n)
	}

	_, n = codec.DecodeVarint([]byte{})
	if n != -1 {
		t.Fatalf("expected -1 for empty slice, got %d", n)
	}
}

func TestDecodeVarint_Truncated(t *testing.T) {
	t.Parallel()

	_, n := codec.DecodeVarint([]byte{0x80})
	if n != -1 {
		t.Fatalf("expected -1 for truncated varint, got %d", n)
	}

	_, n = codec.DecodeVarint([]byte{0x80, 0x80, 0x80})
	if n != -1 {
		t.Fatalf("expected -1 for truncated 3-byte varint, got %d", n)
	}
}

func TestDecodeVarint_Overflow(t *testing.T) {
	t.Parallel()

	data := make([]byte, 11)
	for i := range 10 {
		data[i] = 0x80
	}
	data[10] = 0x01

	_, n := codec.DecodeVarint(data)
	if n != -1 {
		t.Fatalf("expected -1 for 11-byte varint, got %d", n)
	}
}

func TestDecodeVarint_ExtraTrailingBytes(t *testing.T) {
	t.Parallel()

	var buf [10]byte
	n := codec.EncodeVarint(buf[:], 300)
	data := append(buf[:n], 0xFF, 0xFF)

	got, consumed := codec.DecodeVarint(data)
	if consumed != n {
		t.Fatalf("consumed %d bytes, want %d (should not read trailing)", consumed, n)
	}
	if got != 300 {
		t.Fatalf("decoded %d, want 300", got)
	}
}

// ---------------------------------------------------------------------------
// SkipField
// ---------------------------------------------------------------------------

func TestSkipField_Varint(t *testing.T) {
	t.Parallel()

	var buf [10]byte
	n := codec.EncodeVarint(buf[:], 12345)
	skipped, err := codec.SkipField(buf[:n], 0)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != n {
		t.Fatalf("skipped %d, want %d", skipped, n)
	}
}

func TestSkipField_Varint_Truncated(t *testing.T) {
	t.Parallel()

	_, err := codec.SkipField([]byte{0x80}, 0)
	if !stderrors.Is(err, codec.ErrInvalidVarint) {
		t.Fatalf("expected codec.ErrInvalidVarint, got %v", err)
	}
}

func TestSkipField_Fixed64(t *testing.T) {
	t.Parallel()

	data := make([]byte, 8)
	skipped, err := codec.SkipField(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 8 {
		t.Fatalf("skipped %d, want 8", skipped)
	}
}

func TestSkipField_Fixed64_TooShort(t *testing.T) {
	t.Parallel()

	_, err := codec.SkipField(make([]byte, 7), 1)
	if !stderrors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("expected codec.ErrBufferTooShort, got %v", err)
	}
}

func TestSkipField_LenDelimited(t *testing.T) {
	t.Parallel()

	payload := []byte("hello world")
	var buf [20]byte
	n := codec.EncodeVarint(buf[:], uint64(len(payload)))
	copy(buf[n:], payload)
	total := n + len(payload)

	skipped, err := codec.SkipField(buf[:total], 2)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != total {
		t.Fatalf("skipped %d, want %d", skipped, total)
	}
}

func TestSkipField_LenDelimited_TruncatedLength(t *testing.T) {
	t.Parallel()

	_, err := codec.SkipField([]byte{0x80}, 2)
	if !stderrors.Is(err, codec.ErrInvalidVarint) {
		t.Fatalf("expected codec.ErrInvalidVarint, got %v", err)
	}
}

func TestSkipField_LenDelimited_TruncatedPayload(t *testing.T) {
	t.Parallel()

	var buf [5]byte
	codec.EncodeVarint(buf[:], 100)

	_, err := codec.SkipField(buf[:2], 2)
	if !stderrors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("expected codec.ErrBufferTooShort, got %v", err)
	}
}

func TestSkipField_Fixed32(t *testing.T) {
	t.Parallel()

	skipped, err := codec.SkipField(make([]byte, 4), 5)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 4 {
		t.Fatalf("skipped %d, want 4", skipped)
	}
}

func TestSkipField_Fixed32_TooShort(t *testing.T) {
	t.Parallel()

	_, err := codec.SkipField(make([]byte, 3), 5)
	if !stderrors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("expected codec.ErrBufferTooShort, got %v", err)
	}
}

func TestSkipField_UnknownWireType(t *testing.T) {
	t.Parallel()

	for _, wt := range []uint64{3, 4, 6, 7, 99} {
		_, err := codec.SkipField(make([]byte, 8), wt)
		if !stderrors.Is(err, codec.ErrInvalidWireType) {
			t.Fatalf("wire type %d: expected codec.ErrInvalidWireType, got %v", wt, err)
		}
	}
}

func TestSkipField_Empty(t *testing.T) {
	t.Parallel()

	_, err := codec.SkipField(nil, 0)
	if !stderrors.Is(err, codec.ErrInvalidVarint) {
		t.Fatalf("expected codec.ErrInvalidVarint for empty varint skip, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fuzz
// ---------------------------------------------------------------------------

func FuzzVarint_Roundtrip(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(1))
	f.Add(uint64(127))
	f.Add(uint64(128))
	f.Add(uint64(math.MaxUint32))
	f.Add(uint64(math.MaxInt64))
	f.Add(uint64(math.MaxUint64))

	f.Fuzz(func(t *testing.T, v uint64) {
		var buf [10]byte
		n := codec.EncodeVarint(buf[:], v)
		if n < 1 || n > 10 {
			t.Fatalf("EncodeVarint(%d) wrote %d bytes", v, n)
		}
		if n != codec.Sov(v) {
			t.Fatalf("Sov(%d)=%d but EncodeVarint wrote %d", v, codec.Sov(v), n)
		}
		got, consumed := codec.DecodeVarint(buf[:n])
		if consumed != n {
			t.Fatalf("DecodeVarint consumed %d, want %d", consumed, n)
		}
		if got != v {
			t.Fatalf("roundtrip: encoded %d, decoded %d", v, got)
		}
	})
}

func FuzzDecodeVarint_NoPanic(f *testing.F) {
	var buf [10]byte
	for _, v := range []uint64{0, 1, 127, 128, 300, math.MaxUint32, math.MaxUint64} {
		n := codec.EncodeVarint(buf[:], v)
		f.Add(buf[:n])
	}
	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80})

	f.Fuzz(func(t *testing.T, data []byte) {
		val, n := codec.DecodeVarint(data)
		if n > 0 {
			var buf [10]byte
			written := codec.EncodeVarint(buf[:], val)
			got, consumed := codec.DecodeVarint(buf[:written])
			if consumed != written || got != val {
				t.Fatalf("re-encode mismatch: decoded %d from fuzz, re-encoded and got %d", val, got)
			}
		}
	})
}

func FuzzSkipField_NoPanic(f *testing.F) {
	f.Add([]byte{0x08}, uint64(0))
	f.Add(make([]byte, 8), uint64(1))
	f.Add([]byte{5, 'h', 'e', 'l', 'l', 'o'}, uint64(2))
	f.Add(make([]byte, 4), uint64(5))
	f.Add([]byte{}, uint64(99))

	f.Fuzz(func(_ *testing.T, data []byte, wireType uint64) {
		//nolint:errcheck
		codec.SkipField(data, wireType%8)
	})
}

func FuzzSkipField_Varint_Consistency(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(127))
	f.Add(uint64(128))
	f.Add(uint64(math.MaxUint64))

	f.Fuzz(func(t *testing.T, v uint64) {
		var buf [10]byte
		n := codec.EncodeVarint(buf[:], v)
		skipped, err := codec.SkipField(buf[:n], 0)
		if err != nil {
			t.Fatalf("SkipField failed for valid varint: %v", err)
		}
		if skipped != n {
			t.Fatalf("SkipField skipped %d, EncodeVarint wrote %d", skipped, n)
		}
	})
}

func FuzzSkipField_LenDelimited_Consistency(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("hello"))
	f.Add(make([]byte, 128))
	f.Add(make([]byte, 1024))

	f.Fuzz(func(t *testing.T, payload []byte) {
		var buf [10 + 65536]byte
		n := codec.EncodeVarint(buf[:], uint64(len(payload)))
		copy(buf[n:], payload)
		total := n + len(payload)

		skipped, err := codec.SkipField(buf[:total], 2)
		if err != nil {
			t.Fatalf("SkipField failed for valid len-delimited: %v", err)
		}
		if skipped != total {
			t.Fatalf("SkipField skipped %d, expected %d", skipped, total)
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkSov(b *testing.B) {
	for range b.N {
		codec.Sov(123456789)
	}
}

func BenchmarkEncodeVarint_Small(b *testing.B) {
	var buf [10]byte
	for range b.N {
		codec.EncodeVarint(buf[:], 42)
	}
}

func BenchmarkEncodeVarint_Large(b *testing.B) {
	var buf [10]byte
	for range b.N {
		codec.EncodeVarint(buf[:], math.MaxUint64)
	}
}

func BenchmarkDecodeVarint_SingleByte(b *testing.B) {
	data := []byte{42}
	for range b.N {
		codec.DecodeVarint(data)
	}
}

func BenchmarkDecodeVarint_TwoByte(b *testing.B) {
	var buf [10]byte
	n := codec.EncodeVarint(buf[:], 300)
	data := buf[:n]
	for range b.N {
		codec.DecodeVarint(data)
	}
}

func BenchmarkDecodeVarint_TenByte(b *testing.B) {
	var buf [10]byte
	n := codec.EncodeVarint(buf[:], math.MaxUint64)
	data := buf[:n]
	for range b.N {
		codec.DecodeVarint(data)
	}
}

func BenchmarkSkipField_Varint(b *testing.B) {
	var buf [10]byte
	n := codec.EncodeVarint(buf[:], 12345)
	data := buf[:n]
	for range b.N {
		_, _ = codec.SkipField(data, 0)
	}
}

func BenchmarkSkipField_LenDelimited(b *testing.B) {
	var buf [20]byte
	n := codec.EncodeVarint(buf[:], 11)
	copy(buf[n:], "hello world")
	data := buf[:n+11]
	for range b.N {
		_, _ = codec.SkipField(data, 2)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestZigzag64_RoundtripSamples(t *testing.T) {
	t.Parallel()
	cases := []int64{0, 1, -1, 2, -2, 2147483647, -2147483648, 1 << 62, -(1 << 62), math.MaxInt64, math.MinInt64}
	for _, v := range cases {
		if got := codec.ZigzagDecode64(codec.ZigzagEncode64(v)); got != v {
			t.Fatalf("zigzag64(%d): got %d", v, got)
		}
	}
}

func TestZigzag32_RoundtripSamples(t *testing.T) {
	t.Parallel()
	cases := []int32{0, 1, -1, 2, -2, 2147483647, -2147483648}
	for _, v := range cases {
		if got := codec.ZigzagDecode32(codec.ZigzagEncode32(v)); got != v {
			t.Fatalf("zigzag32(%d): got %d", v, got)
		}
	}
}

func assertVarintRoundtrip(t *testing.T, v uint64) {
	t.Helper()
	var buf [10]byte
	n := codec.EncodeVarint(buf[:], v)
	got, consumed := codec.DecodeVarint(buf[:n])
	if consumed != n {
		t.Fatalf("EncodeVarint(%d) wrote %d bytes, DecodeVarint consumed %d", v, n, consumed)
	}
	if got != v {
		t.Fatalf("roundtrip failed: encoded %d, decoded %d", v, got)
	}
}
