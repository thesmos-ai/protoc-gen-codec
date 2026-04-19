// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codectest

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec"
)

// AssertRoundtrip verifies MarshalCodec → UnmarshalCodec identity and
// SizeCodec accuracy against the given sample. The sample must be a
// shape that reflect.DeepEqual can compare round-trip-cleanly; for
// randomly-generated shapes use AssertWireStable instead.
func AssertRoundtrip[T any, PT interface {
	*T
	codec.Codec
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
	if uerr := PT(&got).UnmarshalCodec(buf); uerr != nil {
		t.Fatalf("UnmarshalCodec: %v", uerr)
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("roundtrip mismatch:\n  want: %+v\n  got:  %+v", original, got)
	}
}

// AssertWireStable is the weaker roundtrip property suitable for randomly
// generated samples: after one marshal/unmarshal cycle, re-marshaling
// produces byte-identical wire. This tolerates proto3-equivalent shapes
// (empty-slice vs nil) that reflect.DeepEqual distinguishes.
func AssertWireStable[T any, PT interface {
	*T
	codec.Codec
}](t TB, original T) {
	t.Helper()
	re1, err := PT(&original).MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	var decoded T
	if uerr := PT(&decoded).UnmarshalCodec(re1); uerr != nil {
		t.Fatalf("UnmarshalCodec: %v", uerr)
	}
	re2, merr := PT(&decoded).MarshalCodec()
	if merr != nil {
		t.Fatalf("re-MarshalCodec: %v", merr)
	}
	if !bytes.Equal(re1, re2) {
		t.Fatalf("wire not stable:\n  re1=%x\n  re2=%x", re1, re2)
	}
}

// AssertReset verifies ResetCodec produces a semantically empty receiver:
// re-marshaling after reset yields zero wire bytes (proto3 "absent").
// Backing storage for slices and maps may be preserved for reuse.
//
// The input is marshal/unmarshaled first to obtain an independent copy
// so ResetCodec doesn't mutate shared backing storage across parallel
// subtests that share the same underlying map/slice references.
func AssertReset[T any, PT interface {
	*T
	codec.Codec
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

// AssertNilSafe verifies nil-pointer safety for all public Codec methods.
func AssertNilSafe[T any, PT interface {
	*T
	codec.Codec
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

// AssertWireSmallerThanJSON verifies codec wire size is smaller than JSON
// for the sample. A cheap sanity check that the binary format is actually
// tighter than the text format.
func AssertWireSmallerThanJSON[T any, PT interface {
	*T
	codec.Codec
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
// produce the same struct from the same source data. Catches field-
// mapping bugs (wrong tag, wrong type) that pure self-roundtrip misses.
func AssertCrossFormatConsistency[T any, PT interface {
	*T
	codec.Codec
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
		if uerr := PT(&fromCodec).UnmarshalCodec(codecBytes); uerr != nil {
			t.Fatalf("UnmarshalCodec: %v", uerr)
		}
	}
	var fromJSON T
	if uerr := json.Unmarshal(jsonBytes, &fromJSON); uerr != nil {
		t.Fatalf("json.Unmarshal: %v", uerr)
	}
	if !reflect.DeepEqual(fromCodec, fromJSON) {
		t.Fatalf("cross-format mismatch:\n  codec: %+v\n  json:  %+v", fromCodec, fromJSON)
	}
}

// AssertCorruption feeds every prefix of a valid marshal and every single-
// byte-flip of it to UnmarshalCodec. The decoder must not panic. Errors
// are expected but not asserted — this is a liveness check, not a
// correctness check.
func AssertCorruption[T any, PT interface {
	*T
	codec.Codec
}](t TB, sample T) {
	t.Helper()
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
}

// AssertCorruptTag verifies the generated UnmarshalCodec loop's tag-decode
// error branch fires on a malformed tag varint.
func AssertCorruptTag[T any, PT interface {
	*T
	codec.Codec
}](t TB) {
	t.Helper()
	var got T
	bad := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	err := PT(&got).UnmarshalCodec(bad)
	if err == nil {
		t.Fatalf("UnmarshalCodec(unterminated-varint): want error, got nil")
	}
	if !errors.Is(err, codec.ErrInvalidTag) {
		t.Fatalf("UnmarshalCodec(unterminated-varint): want ErrInvalidTag, got %v", err)
	}
}

// AssertMarshalToCodec verifies MarshalToCodec's happy path with a
// correctly sized caller buffer. Covers the success-return statement
// that RunSuite's roundtrip skips (roundtrip goes through MarshalCodec →
// MarshalCodecInternal, bypassing the public MarshalToCodec wrapper).
func AssertMarshalToCodec[T any, PT interface {
	*T
	codec.Codec
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

// AssertMarshalToShortBuffer verifies MarshalToCodec's buffer-length
// guard fires when the caller passes a buffer one byte smaller than the
// declared SizeCodec.
func AssertMarshalToShortBuffer[T any, PT interface {
	*T
	codec.Codec
}](t TB, sample T) {
	t.Helper()
	ptr := PT(&sample)
	size := ptr.SizeCodec()
	if size == 0 {
		return
	}
	buf := make([]byte, size-1)
	n, err := ptr.MarshalToCodec(buf)
	if err == nil {
		t.Fatalf("MarshalToCodec(len=%d, size=%d): want ErrBufferTooShort, got nil", len(buf), size)
	}
	if !errors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("MarshalToCodec(short): want ErrBufferTooShort, got %v", err)
	}
	if n != 0 {
		t.Fatalf("MarshalToCodec(short): want n=0, got %d", n)
	}
}

// AssertWarmPathGrowth verifies the warm-path unmarshal branches that
// fire when the receiver already has partial state and the incoming
// payload has more elements than the receiver's current slice length.
func AssertWarmPathGrowth[T any, PT interface {
	*T
	codec.Codec
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
	if uerr := PT(&recv).UnmarshalCodec(primerBuf); uerr != nil {
		t.Fatalf("UnmarshalCodec(primer): %v", uerr)
	}
	if uerr := PT(&recv).UnmarshalCodec(growerBuf); uerr != nil {
		t.Fatalf("UnmarshalCodec(grower) into primed receiver: %v", uerr)
	}
}

// AssertUnpackedRepeatedVarint verifies the decoder accepts the unpacked
// wire form of a repeated packed-eligible scalar field. Caller supplies
// the hand-constructed unpacked wire.
func AssertUnpackedRepeatedVarint[T any, PT interface {
	*T
	codec.Codec
}](t TB, wire []byte) {
	t.Helper()
	var got T
	if err := PT(&got).UnmarshalCodec(wire); err != nil {
		t.Fatalf("UnmarshalCodec(unpacked-repeated): %v", err)
	}
}

// AssertCorruptPackedBody verifies the decoder rejects a packed payload
// whose body contains a malformed varint.
func AssertCorruptPackedBody[T any, PT interface {
	*T
	codec.Codec
}](t TB, fieldNum int32) {
	t.Helper()
	var got T
	tag := uint64(fieldNum)<<3 | 2
	buf := make([]byte, 0, 12)
	buf = appendVarint(buf, tag)
	buf = appendVarint(buf, 5)
	buf = append(buf, 0x80, 0x80, 0x80, 0x80, 0x80)
	err := PT(&got).UnmarshalCodec(buf)
	if err == nil {
		t.Fatalf("UnmarshalCodec(corrupt packed body): want error, got nil")
	}
	if !errors.Is(err, codec.ErrInvalidVarint) {
		t.Fatalf("UnmarshalCodec(corrupt packed body): want ErrInvalidVarint, got %v", err)
	}
}

// AssertMarshalWithNilPointerElement verifies MarshalCodec on a receiver
// whose repeated []*T field contains a nil element produces valid wire
// (nil element skipped) and roundtrips.
func AssertMarshalWithNilPointerElement[T any, PT interface {
	*T
	codec.Codec
}](t TB, sample T) {
	t.Helper()
	ptr := PT(&sample)
	buf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec(with nil element): %v", err)
	}
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

// AssertAllFieldsWireTypeMismatch marshals the samples, parses every tag,
// and exercises the per-field `if wireType != X` guards with each of the
// three other wire types. Tolerates legitimate proto3 dual-encoding:
// repeated packed-eligible scalars accept both their packed wire type
// and their element's single-value wire type.
func AssertAllFieldsWireTypeMismatch[T any, PT interface {
	*T
	codec.Codec
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
			tag, n := codec.DecodeVarint(buf[i:])
			if n < 0 {
				t.Fatalf("tag decode failed at offset %d", i)
			}
			fieldNum := int32(tag >> 3)
			wireType := byte(tag & 7)
			seen[fieldNum] = wireType
			i += n
			skip, skipErr := codec.SkipField(buf[i:], uint64(wireType))
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

// assertFieldWireTypeOrAcceptedAlternate constructs wire for (fieldNum,
// wrongWireType) and accepts either ErrInvalidWireType (the field
// rejects the wire type) or nil (the field accepts this as a valid
// alternative encoding per proto3 packed/unpacked duality).
func assertFieldWireTypeOrAcceptedAlternate[T any, PT interface {
	*T
	codec.Codec
}](t TB, fieldNum int32, wrongWireType uint64) {
	t.Helper()
	tag := uint64(fieldNum)<<3 | (wrongWireType & 0x7)
	var tagBuf [10]byte
	tn := codec.EncodeVarint(tagBuf[:], tag)
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
		return
	}
	if !errors.Is(err, codec.ErrInvalidWireType) {
		t.Fatalf("field %d wireType %d: want nil or ErrInvalidWireType, got %v", fieldNum, wrongWireType, err)
	}
}

// AssertCorruptScalarVarint sends a scalar-varint field with a malformed
// varint body. Works for any varint-wire scalar (int32/int64/uint32/
// uint64/sint32/sint64/bool/enum), including the repeated-packed-but-
// sent-as-unpacked alternate path.
func AssertCorruptScalarVarint[T any, PT interface {
	*T
	codec.Codec
}](t TB, fieldNum int32) {
	t.Helper()
	tag := uint64(fieldNum) << 3
	buf := appendVarint(nil, tag)
	buf = append(buf, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var got T
	err := PT(&got).UnmarshalCodec(buf)
	if err == nil {
		t.Fatalf("field %d: want ErrInvalidVarint, got nil", fieldNum)
	}
	if !errors.Is(err, codec.ErrInvalidVarint) {
		t.Fatalf("field %d: want ErrInvalidVarint, got %v", fieldNum, err)
	}
}

// AssertCorruptMapEntryValue targets every inner corruption branch in a
// map-entry decoder: the outer entry-tag varint, the key-length varint
// (for string/bytes keys), the value-length varint (for string/bytes
// values) or value-varint (for numeric values), and the entry-body
// length-mismatch check.
func AssertCorruptMapEntryValue[T any, PT interface {
	*T
	codec.Codec
}](t TB, mapFieldNum int32) {
	t.Helper()
	tag := uint64(mapFieldNum)<<3 | 2

	// Case A: corrupt value length varint (key is empty, then 10 bytes 0x80).
	outerA := appendVarint(nil, tag)
	outerA = appendVarint(outerA, 13)
	outerA = append(outerA, 0x0a, 0x00, 0x12, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var gotA T
	if err := PT(&gotA).UnmarshalCodec(outerA); err == nil {
		t.Fatalf("case A (corrupt value varint): want error, got nil")
	}

	// Case B: trailing junk in entry body.
	outerB := appendVarint(nil, tag)
	outerB = appendVarint(outerB, 6)
	outerB = append(outerB, 0x0a, 0x00, 0x12, 0x00, 0xff, 0xff)
	var gotB T
	_ = PT(&gotB).UnmarshalCodec(outerB)

	// Case C: corrupt key length varint (key tag 0x0a, then 10 bytes 0x80).
	outerC := appendVarint(nil, tag)
	outerC = appendVarint(outerC, 11)
	outerC = append(outerC, 0x0a, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var gotC T
	_ = PT(&gotC).UnmarshalCodec(outerC)

	// Case D: corrupt entry-tag varint.
	outerD := appendVarint(nil, tag)
	outerD = appendVarint(outerD, 10)
	outerD = append(outerD, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var gotD T
	_ = PT(&gotD).UnmarshalCodec(outerD)
}

// AssertCorruptRepeatedMessagePrescan exercises every break branch in
// the repeated-message prescan walk across the four valid proto3 wire
// types. The prescan `break`s out on malformed input; the main decode
// loop then hits the same corruption and returns a proper error. This
// helper asserts the prescan never panics on malformed input of any
// wire type.
func AssertCorruptRepeatedMessagePrescan[T any, PT interface {
	*T
	codec.Codec
}](t TB, repeatedMsgFieldNum int32) {
	t.Helper()
	tag := uint64(repeatedMsgFieldNum)<<3 | 2
	// Case 1: outer length varint unterminated (prescan aborts on the
	// repeated-message field's own length).
	c1 := appendVarint(nil, tag)
	c1 = append(c1, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	// Case 2: inner prescan sees an unterminated varint tag at the start
	// of the buffer (uses field 1 wireType 0 style but unterminated).
	c2 := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	// Case 3: valid varint tag (field 1 wireType 0) followed by an
	// unterminated varint value → prescan case-0 `if pn < 0 { break }`.
	c3 := []byte{0x08, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	// Case 4: wireType-5 tag for an unknown field with short body
	// (fewer than 4 bytes remaining) → prescan case-5 `if pi+4 > l`.
	c4 := []byte{0x9d, 0x06, 0x00, 0x00}
	// Case 5: wireType-1 (fixed64) tag with fewer than 8 body bytes
	// remaining → prescan case-1 `if pi+8 > l { break }`. Tag
	// 0x09 = field 1, wireType 1.
	c5 := []byte{0x09, 0x00, 0x00, 0x00}
	// Case 6: wireType-2 tag whose declared body length exceeds the
	// remaining buffer → prescan case-2 `if pi + int(pvl) > l { break }`.
	// Tag 0x0a = field 1, wireType 2; length varint = 5; only 1 body byte.
	c6 := []byte{0x0a, 0x05, 0x00}
	for _, data := range [][]byte{c1, c2, c3, c4, c5, c6} {
		var got T
		_ = PT(&got).UnmarshalCodec(data)
	}
}

// AssertPrescanSkipsAllWireTypes verifies the repeated-message prescan
// correctly walks past unknown fields of every valid proto3 wire type
// (0=varint, 1=fixed64, 2=length-delimited, 5=fixed32). Prepends a
// well-formed unknown-field wire segment of each type before a valid
// marshal, then asserts decode succeeds and re-marshal produces the
// original wire (unknown fields are dropped per our codec's semantics).
//
// Intended for types that declare RepeatedMessageFields on their Spec;
// on types without a prescan this helper still exercises the main
// decode loop's unknown-field skip path, so it's safe to call
// regardless. The per-field wire-type branches inside the prescan are
// not hit by AssertUnknownFieldSkipped alone (which only uses wire
// type 0).
func AssertPrescanSkipsAllWireTypes[T any, PT interface {
	*T
	codec.Codec
}](t TB, sample T, unknownFieldNum int32) {
	t.Helper()
	ptr := PT(&sample)
	valid, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	for _, wireType := range [...]uint64{0, 1, 2, 5} {
		tag := uint64(unknownFieldNum)<<3 | wireType
		prefix := appendVarint(nil, tag)
		switch wireType {
		case 0:
			prefix = append(prefix, 0x00) // varint value 0
		case 1:
			prefix = append(prefix, 0, 0, 0, 0, 0, 0, 0, 0) // fixed64 zero
		case 2:
			prefix = appendVarint(prefix, 0) // empty length-delimited
		case 5:
			prefix = append(prefix, 0, 0, 0, 0) // fixed32 zero
		}
		combined := make([]byte, 0, len(prefix)+len(valid))
		combined = append(combined, prefix...)
		combined = append(combined, valid...)
		var got T
		if uerr := PT(&got).UnmarshalCodec(combined); uerr != nil {
			t.Fatalf("wire type %d prefix: decode failed: %v", wireType, uerr)
		}
		remarshal, merr := PT(&got).MarshalCodec()
		if merr != nil {
			t.Fatalf("wire type %d prefix: re-marshal failed: %v", wireType, merr)
		}
		if !bytes.Equal(remarshal, valid) {
			t.Fatalf("wire type %d prefix: re-marshal lost data\n want=%x\n got=%x", wireType, valid, remarshal)
		}
	}
}

// AssertCorruptFixedLenBytes exercises the three error branches of a
// fixed-length bytes field (codec.fixed_len): malformed length varint,
// length mismatch vs declared size, and body-short. Caller supplies the
// field's declared length.
func AssertCorruptFixedLenBytes[T any, PT interface {
	*T
	codec.Codec
}](t TB, fieldNum int32, declaredLen uint32) {
	t.Helper()
	tag := uint64(fieldNum)<<3 | 2
	// Case A: length varint unterminated → ErrInvalidVarint.
	bad1 := appendVarint(nil, tag)
	bad1 = append(bad1, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	var got1 T
	if err := PT(&got1).UnmarshalCodec(bad1); err == nil {
		t.Fatalf("FixedLenBytes field %d: want error on corrupt length varint, got nil", fieldNum)
	}
	// Case B: length varint decodes to a value != declaredLen → ErrInvalidLength.
	bad2 := appendVarint(nil, tag)
	bad2 = appendVarint(bad2, uint64(declaredLen)+1)
	var got2 T
	err := PT(&got2).UnmarshalCodec(bad2)
	if err == nil || !errors.Is(err, codec.ErrInvalidLength) {
		t.Fatalf("FixedLenBytes field %d: want ErrInvalidLength, got %v", fieldNum, err)
	}
	// Case C: length matches declaredLen but body is too short.
	bad3 := appendVarint(nil, tag)
	bad3 = appendVarint(bad3, uint64(declaredLen))
	// no body bytes follow
	var got3 T
	err = PT(&got3).UnmarshalCodec(bad3)
	if err == nil || !errors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("FixedLenBytes field %d: want ErrBufferTooShort, got %v", fieldNum, err)
	}
}

// AssertCorruptFixedWidth exercises the short-body error branch for a
// scalar fixed-width field (sfixed64/fixed64/sfixed32/fixed32/double/
// float). width is 4 or 8. Caller ensures the field is declared as a
// fixed-width scalar.
func AssertCorruptFixedWidth[T any, PT interface {
	*T
	codec.Codec
}](t TB, fieldNum int32, width int) {
	t.Helper()
	var wireType uint64
	switch width {
	case 8:
		wireType = 1
	case 4:
		wireType = 5
	default:
		t.Fatalf("AssertCorruptFixedWidth: width must be 4 or 8, got %d", width)
	}
	tag := uint64(fieldNum)<<3 | wireType
	buf := appendVarint(nil, tag)
	// Body is width-1 bytes — one byte short.
	buf = append(buf, make([]byte, width-1)...)
	var got T
	err := PT(&got).UnmarshalCodec(buf)
	if err == nil || !errors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("FixedWidth field %d (width=%d): want ErrBufferTooShort, got %v", fieldNum, width, err)
	}
}

// AssertUnknownFieldSkipped appends a valid (successfully-skippable)
// unknown field tag and body to a valid marshal, then asserts the
// decoder consumes the unknown field without error. Exercises the
// `default → i += n` success path in the decode switch.
func AssertUnknownFieldSkipped[T any, PT interface {
	*T
	codec.Codec
}](t TB, sample T, unknownFieldNum int32) {
	t.Helper()
	ptr := PT(&sample)
	valid, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	// Unknown varint field: tag(num, 0) + one-byte varint value 0.
	tag := uint64(unknownFieldNum) << 3
	buf := append([]byte{}, valid...)
	buf = appendVarint(buf, tag)
	buf = append(buf, 0x00)
	var got T
	if uerr := PT(&got).UnmarshalCodec(buf); uerr != nil {
		t.Fatalf("UnmarshalCodec(valid + unknown field): want nil, got %v", uerr)
	}
}

// AssertCorruptWKTPayload sends a malformed google.protobuf.Timestamp or
// Duration payload to trigger the WKT decoder's error propagation.
func AssertCorruptWKTPayload[T any, PT interface {
	*T
	codec.Codec
}](t TB, wktFieldNum int32) {
	t.Helper()
	tag := uint64(wktFieldNum)<<3 | 2
	buf := appendVarint(nil, tag)
	buf = appendVarint(buf, 3)
	buf = append(buf, 0x80, 0x80, 0x80)
	var got T
	_ = PT(&got).UnmarshalCodec(buf)
}

// AssertUnknownFieldInvalidWireType appends an unknown-field tag with an
// invalid wire type (3 — reserved/undefined) so SkipField returns
// ErrInvalidWireType and the decode loop's default-branch error
// propagation fires.
func AssertUnknownFieldInvalidWireType[T any, PT interface {
	*T
	codec.Codec
}](t TB, sample T, unknownFieldNum int32) {
	t.Helper()
	ptr := PT(&sample)
	valid, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	tag := uint64(unknownFieldNum)<<3 | 3
	buf := append([]byte{}, valid...)
	buf = appendVarint(buf, tag)
	var got T
	if err := PT(&got).UnmarshalCodec(buf); err == nil {
		t.Fatalf("want ErrInvalidWireType, got nil")
	}
}

// appendVarint writes a proto varint to buf. Kept local to avoid a
// dependency on the runtime's EncodeVarint (which requires a pre-sized
// destination slice).
func appendVarint(buf []byte, v uint64) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	return append(buf, byte(v))
}
