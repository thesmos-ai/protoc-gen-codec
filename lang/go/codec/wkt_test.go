// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	"testing"
	"time"

	"pgregory.net/rapid"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec"
)

func TestTimestamp_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// secs starts at 1 to avoid the zero-preservation alias: secs==0 &&
		// nanos==0 encodes to an empty body, which DecodeTimestamp maps back
		// to time.Time{} rather than the 1970 epoch. See
		// TestTimestamp_ZeroRoundtrips for the zero case.
		secs := rapid.Int64Range(1, 253402300799).Draw(t, "secs") // 1970..year-9999
		nanos := rapid.Int32Range(0, 999_999_999).Draw(t, "nanos")
		ts := time.Unix(secs, int64(nanos)).UTC()
		sz := codec.SizeTimestamp(ts)
		buf := make([]byte, sz)
		n := codec.EncodeTimestamp(buf, ts)
		if n != sz {
			t.Fatalf("EncodeTimestamp wrote %d, SizeTimestamp said %d", n, sz)
		}
		got, err := codec.DecodeTimestamp(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(ts) {
			t.Fatalf("roundtrip mismatch: want %v got %v", ts, got)
		}
	})
}

func TestTimestamp_ZeroRoundtrips(t *testing.T) {
	t.Parallel()
	var zero time.Time
	if sz := codec.SizeTimestamp(zero); sz != 0 {
		t.Fatalf("zero Timestamp should size 0, got %d", sz)
	}
	got, err := codec.DecodeTimestamp(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Fatalf("empty decode should be zero, got %v", got)
	}
}

func TestDuration_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		secs := rapid.Int64Range(-315576000000, 315576000000).Draw(t, "secs") // ±10000 years
		nanos := rapid.Int32Range(-999_999_999, 999_999_999).Draw(t, "nanos")
		d := time.Duration(secs)*time.Second + time.Duration(nanos)
		sz := codec.SizeDuration(d)
		buf := make([]byte, sz)
		n := codec.EncodeDuration(buf, d)
		if n != sz {
			t.Fatalf("EncodeDuration wrote %d, SizeDuration said %d", n, sz)
		}
		got, err := codec.DecodeDuration(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		if got != d {
			t.Fatalf("roundtrip mismatch: want %v got %v", d, got)
		}
	})
}
