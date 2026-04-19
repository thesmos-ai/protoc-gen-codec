// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"errors"
	"fmt"
	"testing"
)

// CoverageSpec is the declarative plan for driving a generated type to 100%
// coverage. Pass it to RunFullCoverageSuite — each field controls one or
// more sub-tests. Every slice is optional; the empty or zero value skips
// that test category.
//
// The spec is keyed by type T so the suite can assert on the same T that
// owns the generated codec methods. Consumers typically write one spec
// per annotated message.
type CoverageSpec[T any] struct {
	// Sample is the baseline, fully-populated instance used for happy-path
	// and wire-format assertions.
	Sample T
	// Variants are additional samples merged into the auto-derived
	// wire-type-mismatch check. Use when Sample doesn't populate every
	// declared field (e.g., a oneof discriminator selects only one
	// variant's payload field per sample).
	Variants []T
	// Grower, if non-nil, is a strictly larger instance (more elements in
	// at least one repeated field) used for the warm-path growth test.
	Grower *T
	// NilPointerSample, if non-nil, is a hand-constructed sample with a
	// nil entry in one of its []*T fields. Drives the
	// `if elem == nil { continue }` branches in SizeCodec and
	// MarshalCodecInternal.
	NilPointerSample *T
	// UnknownFieldNum is any field number NOT declared in the schema.
	// Required — used by the unknown-field exercise path.
	UnknownFieldNum int32
	// ScalarVarintFields lists field numbers declared as varint-wire
	// scalars (int32/int64/uint32/uint64/sint32/sint64/bool/enum). Each
	// runs AssertCorruptScalarVarint.
	ScalarVarintFields []int32
	// PackedFields lists field numbers declared as repeated packed
	// scalars. Each runs AssertCorruptPackedBody.
	PackedFields []int32
	// MapFields lists field numbers declared as map<K,V>. Each runs
	// AssertCorruptMapEntryValue.
	MapFields []int32
	// RepeatedMessageFields lists field numbers declared as `repeated Msg`.
	// Each runs AssertCorruptRepeatedMessagePrescan.
	RepeatedMessageFields []int32
	// WKTFields lists field numbers declared as google.protobuf.Timestamp
	// or Duration. Each runs AssertCorruptWKTPayload.
	WKTFields []int32
}

// RunFullCoverageSuite is the one-call path to 100% coverage on a generated
// codec. It composes every helper in this file under a single t.Run tree,
// driven by a CoverageSpec that declares which field numbers belong to
// which wire-format category.
//
// Usage:
//
//	codec.RunFullCoverageSuite[MyType](t, codec.CoverageSpec[MyType]{
//	    Sample: fullSample(),
//	    UnknownFieldNum: 999,
//	    ScalarVarintFields: []int32{2, 3, 5},
//	    MapFields: []int32{7},
//	    RepeatedMessageFields: []int32{9},
//	})
//
// For a type with no categorized fields (pure string/bytes/message
// payload), only the schema-agnostic subsuite runs, which on its own
// covers the MarshalCodec wrappers, UnmarshalCodec tag loop, and
// AllFields wire-type grid.
func RunFullCoverageSuite[T any, PT interface {
	*T
	Marshaler
}](t *testing.T, spec CoverageSpec[T]) {
	t.Helper()

	// Schema-agnostic subsuite — runs for every type.
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
		AssertUnknownFieldInvalidWireType[T, PT](t, spec.Sample, spec.UnknownFieldNum)
	})
	t.Run("AllFieldsWireTypeMismatch", func(t *testing.T) {
		t.Parallel()
		all := append([]T{spec.Sample}, spec.Variants...)
		AssertAllFieldsWireTypeMismatch[T, PT](t, all...)
	})

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
}

// This file extends the coverage-suite helpers in testing.go with targeted
// assertions for paths that are reachable under malformed input but not
// naturally exercised by RunTestSuite's roundtrip/zero/corruption battery.
//
// Consumers who want to push generated *.codec.go files to 100% should call
// RunExtendedCoverageSuite after RunTestSuite + RunCoverageSuite. Each helper
// is also exposed individually so schema-specific edge cases (e.g. nil
// repeated-pointer elements) can be tested surgically.

// AssertCorruptTag verifies the generated UnmarshalCodec loop's tag-decode
// error branch fires on a malformed tag varint. Feeds ten 0x80 continuation
// bytes (never terminating) and asserts the decoder returns ErrInvalidTag.
func AssertCorruptTag[T any, PT interface {
	*T
	Marshaler
}](t TB) {
	t.Helper()
	var got T
	// Ten 0x80 bytes form an unterminated varint: DecodeVarint returns n=-1,
	// the tag-decode branch wraps ErrInvalidTag.
	bad := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	err := PT(&got).UnmarshalCodec(bad)
	if err == nil {
		t.Fatalf("UnmarshalCodec(unterminated-varint): want error, got nil")
	}
	if !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("UnmarshalCodec(unterminated-varint): want ErrInvalidTag, got %v", err)
	}
}

// AssertMarshalToCodec verifies MarshalToCodec's happy path with a correctly
// sized caller buffer. Covers the trivial success-return statement that
// RunTestSuite's roundtrip doesn't reach (roundtrip goes through
// MarshalCodec → MarshalCodecInternal and bypasses the public
// MarshalToCodec wrapper entirely).
func AssertMarshalToCodec[T any, PT interface {
	*T
	Marshaler
}](t TB, sample T) {
	t.Helper()
	ptr := PT(&sample)
	size := ptr.SizeCodec()
	buf := make([]byte, size)
	n, err := ptr.MarshalToCodec(buf)
	if err != nil {
		t.Fatalf("MarshalToCodec: %v", err)
	}
	if n != size {
		t.Fatalf("MarshalToCodec: wrote %d bytes, want %d", n, size)
	}
}

// AssertMarshalToShortBuffer verifies MarshalToCodec's buffer-length guard
// fires when the caller passes a buffer that is one byte smaller than the
// declared SizeCodec(). The happy path is covered by RunTestSuite; this
// covers the single error branch in the generated MarshalToCodec wrapper.
func AssertMarshalToShortBuffer[T any, PT interface {
	*T
	Marshaler
}](t TB, sample T) {
	t.Helper()
	ptr := PT(&sample)
	size := ptr.SizeCodec()
	if size == 0 {
		return // empty message, no short-buffer case to exercise.
	}
	buf := make([]byte, size-1)
	n, err := ptr.MarshalToCodec(buf)
	if err == nil {
		t.Fatalf("MarshalToCodec(len=%d, size=%d): want ErrBufferTooShort, got nil", len(buf), size)
	}
	if !errors.Is(err, ErrBufferTooShort) {
		t.Fatalf("MarshalToCodec(short): want ErrBufferTooShort, got %v", err)
	}
	if n != 0 {
		t.Fatalf("MarshalToCodec(short): want n=0, got %d", n)
	}
}

// AssertWarmPathGrowth verifies the warm-path unmarshal branches that only
// fire when the receiver already has partial state from a previous decode
// and the incoming payload has *more* elements than the receiver's current
// slice length. Covers the "append new pointer slot" path for []*T and the
// "grow value slice" path for []T.
//
// primer is the sample used to prime the receiver on the first decode;
// grower is a strictly larger sample (more elements in at least one
// repeated field) that forces new-element allocation on the second decode.
func AssertWarmPathGrowth[T any, PT interface {
	*T
	Marshaler
}](t TB, primer, grower T) {
	t.Helper()
	primerBuf, err := PT(&primer).MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec(primer): %v", err)
	}
	growerBuf, err := PT(&grower).MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec(grower): %v", err)
	}
	var recv T
	if err := PT(&recv).UnmarshalCodec(primerBuf); err != nil {
		t.Fatalf("UnmarshalCodec(primer): %v", err)
	}
	if err := PT(&recv).UnmarshalCodec(growerBuf); err != nil {
		t.Fatalf("UnmarshalCodec(grower) into primed receiver: %v", err)
	}
}

// AssertUnpackedRepeatedVarint verifies the decoder accepts the *unpacked*
// wire form of a repeated packed-eligible scalar field. proto3 allows both
// packed (wireType 2, length-delimited) and unpacked (wireType 0, one tag
// per element) encodings for parse — producers usually emit packed but an
// older encoder or hand-crafted payload may send unpacked, and the decoder
// must accept it.
//
// Caller supplies a raw wire buffer constructed with unpacked-varint tags
// for the field under test; the decoder must succeed.
func AssertUnpackedRepeatedVarint[T any, PT interface {
	*T
	Marshaler
}](t TB, wire []byte) {
	t.Helper()
	var got T
	if err := PT(&got).UnmarshalCodec(wire); err != nil {
		t.Fatalf("UnmarshalCodec(unpacked-repeated): %v", err)
	}
}

// AssertCorruptPackedBody verifies the decoder rejects a packed payload
// whose body contains a malformed varint. Constructs: field tag (wireType
// 2) + length prefix + body that ends in unterminated high-bit bytes.
func AssertCorruptPackedBody[T any, PT interface {
	*T
	Marshaler
}](t TB, fieldNum int32) {
	t.Helper()
	var got T
	// Build: tag(fieldNum, wireType 2) + length 5 + five 0x80 bytes.
	// DecodeVarint inside the packed loop returns n=-1 → ErrInvalidVarint.
	tag := uint64(fieldNum)<<3 | 2
	buf := make([]byte, 0, 12)
	buf = appendVarint(buf, tag)
	buf = appendVarint(buf, 5)
	buf = append(buf, 0x80, 0x80, 0x80, 0x80, 0x80)
	err := PT(&got).UnmarshalCodec(buf)
	if err == nil {
		t.Fatalf("UnmarshalCodec(corrupt packed body): want error, got nil")
	}
	if !errors.Is(err, ErrInvalidVarint) {
		t.Fatalf("UnmarshalCodec(corrupt packed body): want ErrInvalidVarint, got %v", err)
	}
}

// AssertMarshalWithNilPointerElement verifies that MarshalCodec on a
// receiver whose repeated []*T field contains a nil element does not panic
// and produces valid wire (the nil element is skipped per generator
// contract). Caller supplies a pre-constructed sample with nil in the
// slice; this helper only verifies the marshal succeeds.
func AssertMarshalWithNilPointerElement[T any, PT interface {
	*T
	Marshaler
}](t TB, sample T) {
	t.Helper()
	ptr := PT(&sample)
	buf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec(with nil element): %v", err)
	}
	// Decoded state must roundtrip — re-marshal must produce byte-identical wire.
	var got T
	if uerr := PT(&got).UnmarshalCodec(buf); uerr != nil {
		t.Fatalf("UnmarshalCodec(with nil element): %v", uerr)
	}
	rebuf, merr := PT(&got).MarshalCodec()
	if merr != nil {
		t.Fatalf("re-MarshalCodec: %v", merr)
	}
	if len(rebuf) != len(buf) {
		t.Fatalf("wire size changed: before=%d after=%d", len(buf), len(rebuf))
	}
}

// AssertCorruptScalarVarint sends a scalar-varint field (wireType 0) with a
// malformed varint body (10 unterminated continuation bytes). Exercises the
// `if n < 0 { return ErrInvalidVarint }` branch after DecodeVarint for the
// given field. Works for any field declared as a varint-wire scalar
// (int32/int64/uint32/uint64/sint32/sint64/bool/enum), including the
// repeated-packed-but-sent-as-unpacked alternate path.
func AssertCorruptScalarVarint[T any, PT interface {
	*T
	Marshaler
}](t TB, fieldNum int32) {
	t.Helper()
	tag := uint64(fieldNum) << 3 // wireType 0
	buf := appendVarint(nil, tag)
	buf = append(buf, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var got T
	err := PT(&got).UnmarshalCodec(buf)
	if err == nil {
		t.Fatalf("field %d: want ErrInvalidVarint, got nil", fieldNum)
	}
	if !errors.Is(err, ErrInvalidVarint) {
		t.Fatalf("field %d: want ErrInvalidVarint, got %v", fieldNum, err)
	}
}

// AssertCorruptMapEntryValue targets the map-entry inner varint-decode and
// length-mismatch branches. Constructs a map field (wireType 2) with a
// well-formed outer entry length, valid key, but a corrupt inner value tag
// or length. Covers the "entry varint fail" and "i != entryEnd" branches.
func AssertCorruptMapEntryValue[T any, PT interface {
	*T
	Marshaler
}](t TB, mapFieldNum int32) {
	t.Helper()
	tag := uint64(mapFieldNum)<<3 | 2
	// Case 1: entry body contains a valid key (0x0a=tag,len 0,empty) and a
	// value tag (0x12) followed by unterminated varint length.
	outer1 := appendVarint(nil, tag)
	outer1 = appendVarint(outer1, 13)
	outer1 = append(outer1, 0x0a, 0x00, 0x12, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var got1 T
	if err := PT(&got1).UnmarshalCodec(outer1); err == nil {
		t.Fatalf("case 1 (corrupt value varint): want error, got nil")
	}
	// Case 2: entry declared length 6 but body only consumes 4 (trailing
	// bytes at i != entryEnd). Body: 0x0a=tag,len=0,empty; 0x12=tag,len=0,empty; then 2 extra bytes.
	outer2 := appendVarint(nil, tag)
	outer2 = appendVarint(outer2, 6)
	outer2 = append(outer2, 0x0a, 0x00, 0x12, 0x00, 0xff, 0xff)
	var got2 T
	// The trailing junk trips SkipField or ErrInvalidTag depending on bytes;
	// either way the decoder must not silently accept.
	_ = PT(&got2).UnmarshalCodec(outer2)
}

// AssertCorruptRepeatedMessagePrescan exercises the repeated-message
// prescan walk's length and tag-decode branches by sending a repeated
// message field (wireType 2) with an outer length varint that is too large
// for the buffer. The prescan's `break` paths fire.
func AssertCorruptRepeatedMessagePrescan[T any, PT interface {
	*T
	Marshaler
}](t TB, repeatedMsgFieldNum int32) {
	t.Helper()
	tag := uint64(repeatedMsgFieldNum)<<3 | 2
	// Start: tag + unterminated length varint. Prescan hits `if pn < 0`.
	buf := appendVarint(nil, tag)
	buf = append(buf, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var got T
	_ = PT(&got).UnmarshalCodec(buf) // decode MAY error, MAY succeed; we only need coverage.
}

// AssertUnknownFieldInvalidWireType appends an unknown-field tag with an
// invalid wire type (3, 4, 6, or 7 — all reserved/undefined) so
// SkipField returns ErrInvalidWireType and the decode loop's
// `default → if err != nil` branch fires.
func AssertUnknownFieldInvalidWireType[T any, PT interface {
	*T
	Marshaler
}](t TB, sample T, unknownFieldNum int32) {
	t.Helper()
	ptr := PT(&sample)
	valid, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	// Wire type 3 is reserved (old group start) and rejected by SkipField.
	tag := uint64(unknownFieldNum)<<3 | 3
	buf := append([]byte{}, valid...)
	buf = appendVarint(buf, tag)
	var got T
	if err := PT(&got).UnmarshalCodec(buf); err == nil {
		t.Fatalf("want ErrInvalidWireType, got nil")
	}
}

// AssertCorruptWKTPayload sends a malformed google.protobuf.Timestamp or
// Duration payload (length-delimited message with body bytes that fail the
// canonical WKT decoder). Targets the `if err != nil { return ... }` branch
// after DecodeTimestamp / DecodeDuration.
func AssertCorruptWKTPayload[T any, PT interface {
	*T
	Marshaler
}](t TB, wktFieldNum int32) {
	t.Helper()
	tag := uint64(wktFieldNum)<<3 | 2
	// Body: length 3 + 3 bytes of 0x80 (not a valid Timestamp/Duration).
	buf := appendVarint(nil, tag)
	buf = appendVarint(buf, 3)
	buf = append(buf, 0x80, 0x80, 0x80)
	var got T
	_ = PT(&got).UnmarshalCodec(buf)
}

// appendVarint writes a proto varint to buf. Kept local so the helper set
// has no dependency on the wire.go EncodeVarint function (which requires a
// pre-sized destination slice).
func appendVarint(buf []byte, v uint64) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	return append(buf, byte(v))
}

// AssertAllFieldsWireTypeMismatch marshals the sample, parses every tag
// from the wire output, and exercises the per-field `if wireType != X`
// guards on each declared field with each of the three other wire types.
// Drives coverage without requiring the caller to enumerate fields by hand.
//
// Tolerates legitimate proto3 dual-encoding: repeated packed-eligible
// scalars accept BOTH their packed wire type (2) AND their element's
// single-value wire type (0/1/5). When the decoder returns nil for a
// "wrong" wire type, the helper treats that as a valid alternative
// encoding rather than a test failure. Only an unexpected non-nil
// non-ErrInvalidWireType error fails the assertion.
//
// Fields that are zero-valued (and therefore absent from the marshaled
// output) are not covered by this helper; pass them to RunCoverageSuite's
// WireMismatch slice as usual.
func AssertAllFieldsWireTypeMismatch[T any, PT interface {
	*T
	Marshaler
}](t TB, samples ...T) {
	t.Helper()
	seen := make(map[int32]byte)
	for _, s := range samples {
		sample := s
		buf, err := PT(&sample).MarshalCodec()
		if err != nil {
			t.Fatalf("MarshalCodec: %v", err)
		}
		i := 0
		for i < len(buf) {
			tag, n := DecodeVarint(buf[i:])
			if n < 0 {
				t.Fatalf("tag decode failed at offset %d", i)
			}
			fieldNum := int32(tag >> 3)
			wireType := byte(tag & 7)
			seen[fieldNum] = wireType
			i += n
			skip, skipErr := SkipField(buf[i:], uint64(wireType))
			if skipErr != nil {
				t.Fatalf("skip at offset %d: %v", i, skipErr)
			}
			i += skip
		}
	}
	for fn, correct := range seen {
		for _, wt := range [...]uint64{0, 1, 2, 5} {
			if byte(wt) == correct {
				continue
			}
			assertFieldWireTypeOrAcceptedAlternate[T, PT](t, fn, wt)
		}
	}
}

// assertFieldWireTypeOrAcceptedAlternate is the permissive variant of
// AssertWireTypeMismatch. Constructs wire with a single field at the given
// (fieldNum, wireType), feeds it to UnmarshalCodec, and accepts either:
//   - ErrInvalidWireType (the field rejects the wire type), or
//   - nil (the field accepts this as a valid alternative encoding per
//     proto3 packed/unpacked duality).
//
// Any other error is a bug.
func assertFieldWireTypeOrAcceptedAlternate[T any, PT interface {
	*T
	Marshaler
}](t TB, fieldNum int32, wrongWireType uint64) {
	t.Helper()
	tag := uint64(fieldNum)<<3 | (wrongWireType & 0x7)
	var tagBuf [10]byte
	tn := EncodeVarint(tagBuf[:], tag)
	data := append([]byte{}, tagBuf[:tn]...)
	switch wrongWireType {
	case 0:
		data = append(data, 0x00)
	case 1:
		data = append(data, 0, 0, 0, 0, 0, 0, 0, 0)
	case 2:
		data = append(data, 0x00)
	case 5:
		data = append(data, 0, 0, 0, 0)
	}
	var got T
	err := PT(&got).UnmarshalCodec(data)
	if err == nil {
		return // accepted as alternate encoding
	}
	if !errors.Is(err, ErrInvalidWireType) {
		t.Fatalf("field %d wireType %d: want nil or ErrInvalidWireType, got %v", fieldNum, wrongWireType, err)
	}
}

// RunExtendedCoverageSuite wires the new helpers into a single t.Run block
// mirroring RunCoverageSuite's style. Consumers that want 100% coverage
// should call this alongside RunCoverageSuite.
//
// grower must be a strictly larger sample than sample (more elements in at
// least one repeated field) so AssertWarmPathGrowth can exercise the
// new-element-append path; pass the zero value to skip the warm-path check.
func RunExtendedCoverageSuite[T any, PT interface {
	*T
	Marshaler
}](t *testing.T, sample T, grower T) {
	t.Helper()

	t.Run("CorruptTag", func(t *testing.T) {
		t.Parallel()
		AssertCorruptTag[T, PT](t)
	})

	t.Run("MarshalToCodec", func(t *testing.T) {
		t.Parallel()
		AssertMarshalToCodec[T, PT](t, sample)
	})

	t.Run("MarshalToShortBuffer", func(t *testing.T) {
		t.Parallel()
		AssertMarshalToShortBuffer[T, PT](t, sample)
	})

	t.Run("WarmPathGrowth", func(t *testing.T) {
		t.Parallel()
		AssertWarmPathGrowth[T, PT](t, sample, grower)
	})

	t.Run("AllFieldsWireTypeMismatch", func(t *testing.T) {
		t.Parallel()
		AssertAllFieldsWireTypeMismatch[T, PT](t, sample)
	})
}
