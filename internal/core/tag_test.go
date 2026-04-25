// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package core

import "testing"

// TagValue is a pure bit-shuffle: (fieldNum << 3) | wireKind.
func TestTagValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		fieldNum int32
		wk       WireKind
		want     uint64
	}{
		{1, WireVarint, 0x08},  // (1<<3) | 0
		{1, WireFixed64, 0x09}, // (1<<3) | 1
		{1, WireLenDel, 0x0A},  // (1<<3) | 2
		{1, WireFixed32, 0x0D}, // (1<<3) | 5
		{2, WireLenDel, 0x12},  // (2<<3) | 2
		{15, WireLenDel, 0x7A}, // last single-byte field number
		{16, WireLenDel, 0x82}, // first two-byte field number (low byte; high bit set after shift)
		{2047, WireVarint, 16376},
	}
	for _, c := range cases {
		got := TagValue(c.fieldNum, c.wk)
		if got != c.want {
			t.Errorf("TagValue(%d, %d) = %#x, want %#x", c.fieldNum, c.wk, got, c.want)
		}
	}
}

// TagSize crosses the varint byte-boundaries at 16, 2048, 262144, etc.
// The boundary cases pin the behavior of the `for v >= 0x80; n++; v >>= 7` loop —
// off-by-one mutations in either the boundary or the increment surface here.
func TestTagSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		fieldNum int32
		want     int
	}{
		{0, 1},      // (0<<3) = 0; one byte
		{1, 1},      // smallest real field; 0x08 fits one byte
		{15, 1},     // largest 1-byte tag value (0x78 < 0x80)
		{16, 2},     // 0x80 — first 2-byte tag (boundary)
		{2047, 2},   // largest 2-byte tag (16376 < 2^14 == 16384)
		{2048, 3},   // 16384 — first 3-byte tag (boundary)
		{262143, 3}, // largest 3-byte tag (≈ 2^21-1)
		{262144, 4}, // first 4-byte tag (boundary)
	}
	for _, c := range cases {
		got := TagSize(c.fieldNum)
		if got != c.want {
			t.Errorf("TagSize(%d) = %d, want %d", c.fieldNum, got, c.want)
		}
	}
}

// TagBytes must round-trip through TagSize: len(TagBytes(n, wk)) == TagSize(n)
// for any field number, and the encoded byte sequence must decode back to the
// same TagValue. This pins both the byte layout and the loop termination.
func TestTagBytes_RoundtripsThroughTagSize(t *testing.T) {
	t.Parallel()
	for _, fieldNum := range []int32{1, 15, 16, 100, 2047, 2048, 100_000} {
		for _, wk := range []WireKind{WireVarint, WireFixed64, WireLenDel, WireFixed32} {
			b := TagBytes(fieldNum, wk)
			if len(b) != TagSize(fieldNum) {
				t.Errorf("TagBytes(%d, %d) len=%d != TagSize=%d", fieldNum, wk, len(b), TagSize(fieldNum))
			}
			// Decode the bytes back via the inverse of EncodeVarint.
			var x uint64
			var s uint
			for i, by := range b {
				x |= uint64(by&0x7f) << s
				s += 7
				if by < 0x80 {
					if i != len(b)-1 {
						t.Errorf("TagBytes(%d, %d) terminates at byte %d but slice has %d bytes",
							fieldNum, wk, i, len(b))
					}
					break
				}
			}
			if x != TagValue(fieldNum, wk) {
				t.Errorf("decode(TagBytes(%d, %d)) = %d, TagValue = %d", fieldNum, wk, x, TagValue(fieldNum, wk))
			}
		}
	}
}

// TagBytes for a single-byte tag has its high bit clear (proto-varint convention)
// and equals the raw TagValue. This pins the `if v >= 0x80` boundary specifically.
func TestTagBytes_SingleByte_HighBitClear(t *testing.T) {
	t.Parallel()
	b := TagBytes(15, WireLenDel) // tag value = 0x7A, fits one byte
	if len(b) != 1 {
		t.Fatalf("expected 1 byte, got %d", len(b))
	}
	if b[0] != 0x7A {
		t.Errorf("byte = %#x, want 0x7A", b[0])
	}
	if b[0]&0x80 != 0 {
		t.Errorf("single-byte tag must have high bit clear, got %#x", b[0])
	}
}

// SovLocal returns the byte length needed to varint-encode x. The boundaries
// at 0x80, 0x4000, ..., 2^63 are where the loop adds another byte; off-by-one
// mutations on `>= 0x80` or `>>= 7` surface here.
func TestSovLocal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		x    uint64
		want int
	}{
		{0, 1},
		{1, 1},
		{0x7F, 1},                // largest 1-byte
		{0x80, 2},                // first 2-byte
		{0x3FFF, 2},              // largest 2-byte
		{0x4000, 3},              // first 3-byte
		{0x1FFFFF, 3},            // largest 3-byte
		{0x200000, 4},            // first 4-byte
		{0xFFFFFFFF, 5},          // max uint32 needs 5
		{0x7FFFFFFFFFFFFFFF, 9},  // max int64 (positive) needs 9
		{0xFFFFFFFFFFFFFFFF, 10}, // max uint64 needs 10
	}
	for _, c := range cases {
		got := SovLocal(c.x)
		if got != c.want {
			t.Errorf("SovLocal(%#x) = %d, want %d", c.x, got, c.want)
		}
	}
}
