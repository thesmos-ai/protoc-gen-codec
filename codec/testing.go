// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import "reflect"

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

// AssertReset verifies ResetCodec produces the zero value.
func AssertReset[T any, PT interface {
	*T
	Marshaler
}](t TB, populated T) {
	t.Helper()
	ptr := PT(&populated)
	ptr.ResetCodec()
	var zero T
	if !reflect.DeepEqual(populated, zero) {
		t.Fatalf("ResetCodec did not produce zero value:\n  got: %+v", populated)
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
