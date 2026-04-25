// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codectest

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"

	"go.thesmos.sh/protoc-gen-codec/lang/go/codec"
)

// Assertion-message convention (per testing-runbook Phase 4.3):
// every Fatalf cites the contract it defends, not the value that failed.
// "what guarantee was violated", not "which line fired". The dynamic
// value is included in parentheses so triage still has the data.

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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
	}
	if buf == nil && size == 0 {
		return
	}
	if len(buf) != size {
		t.Fatalf("SizeCodec must equal len(MarshalCodec) — callers pre-allocate from SizeCodec, so any divergence overflows the marshal buffer (size=%d, len=%d)",
			size, len(buf))
	}
	var got T
	if uerr := PT(&got).UnmarshalCodec(buf); uerr != nil {
		t.Fatalf("UnmarshalCodec must succeed on bytes produced by MarshalCodec (got: %v)", uerr)
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("Marshal then Unmarshal must reproduce the original — proto3 wire is total for declared fields\n  want: %+v\n  got:  %+v",
			original, got)
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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
	}
	var decoded T
	if uerr := PT(&decoded).UnmarshalCodec(re1); uerr != nil {
		t.Fatalf("UnmarshalCodec must succeed on bytes produced by MarshalCodec (got: %v)", uerr)
	}
	re2, merr := PT(&decoded).MarshalCodec()
	if merr != nil {
		t.Fatalf("re-MarshalCodec must succeed on a fresh decode of valid bytes (got: %v)", merr)
	}
	if !bytes.Equal(re1, re2) {
		t.Fatalf("wire must be byte-identical across one Marshal/Unmarshal/Marshal cycle — deterministic encoding contract\n  re1=%x\n  re2=%x",
			re1, re2)
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
		t.Fatalf("MarshalCodec must succeed on the input sample (got: %v)", err)
	}
	var owned T
	if buf != nil {
		if uerr := PT(&owned).UnmarshalCodec(buf); uerr != nil {
			t.Fatalf("UnmarshalCodec must succeed on bytes produced by MarshalCodec (got: %v)", uerr)
		}
	}
	ptr := PT(&owned)
	ptr.ResetCodec()
	if sz := ptr.SizeCodec(); sz != 0 {
		t.Fatalf("ResetCodec must zero every serialized field — non-zero SizeCodec=%d after reset means a field's reset is missing or incomplete (post-reset object must be indistinguishable from zero value on the wire)",
			sz)
	}
	reBuf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec must succeed on a reset (zero-equivalent) receiver (got: %v)", err)
	}
	if len(reBuf) != 0 {
		t.Fatalf("ResetCodec must zero every serialized field — non-zero MarshalCodec output after reset (len=%d) means a field's reset is incomplete",
			len(reBuf))
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
		t.Fatalf("SizeCodec on a nil receiver must return 0 — nil-pointer safety required for pool-friendly receiver patterns")
	}
	buf, err := nilPtr.MarshalCodec()
	if err != nil || buf != nil {
		t.Fatalf("MarshalCodec on a nil receiver must return (nil, nil) — nil-pointer safety required (got buf=%v err=%v)",
			buf, err)
	}
	n, err := nilPtr.MarshalToCodec(nil)
	if err != nil || n != 0 {
		t.Fatalf("MarshalToCodec on a nil receiver must return (0, nil) — nil-pointer safety required (got n=%d err=%v)",
			n, err)
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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
	}
	jsonBuf, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("json.Marshal must succeed on the same sample (sanity check the fixture is JSON-serializable; got: %v)", err)
	}
	if len(vtBuf) >= len(jsonBuf) {
		t.Fatalf("codec wire must be strictly smaller than JSON for any non-trivial sample — binary format value proposition (codec=%d, JSON=%d bytes)",
			len(vtBuf), len(jsonBuf))
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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
	}
	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal must succeed on the same sample (sanity check the fixture is JSON-serializable; got: %v)", err)
	}
	var fromCodec T
	if codecBytes != nil {
		if uerr := PT(&fromCodec).UnmarshalCodec(codecBytes); uerr != nil {
			t.Fatalf("UnmarshalCodec must succeed on bytes produced by MarshalCodec (got: %v)", uerr)
		}
	}
	var fromJSON T
	if uerr := json.Unmarshal(jsonBytes, &fromJSON); uerr != nil {
		t.Fatalf("json.Unmarshal must succeed on bytes produced by json.Marshal (got: %v)", uerr)
	}
	if !reflect.DeepEqual(fromCodec, fromJSON) {
		t.Fatalf("codec roundtrip and JSON roundtrip must produce structurally-equal results from the same source — catches field-mapping bugs (wrong tag, wrong type) that pure self-roundtrip misses\n  codec: %+v\n  json:  %+v",
			fromCodec, fromJSON)
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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
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
		t.Fatalf("UnmarshalCodec must reject a 10-byte unterminated tag varint — DoS resistance requires bounded tag-decode (got nil error)")
	}
	if !errors.Is(err, codec.ErrInvalidTag) {
		t.Fatalf("UnmarshalCodec on a malformed tag varint must wrap ErrInvalidTag for programmatic matching (got: %v)", err)
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
		t.Fatalf("MarshalToCodec must succeed when the caller buffer matches SizeCodec (got: %v)", err)
	}
	if n != size {
		t.Fatalf("MarshalToCodec must return n == SizeCodec — callers rely on n to track buffer offsets in batched marshals (got n=%d, want %d)",
			n, size)
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
		t.Fatalf("MarshalToCodec must reject a buffer smaller than SizeCodec — silent partial writes would corrupt downstream readers (size=%d, buf=%d, got nil error)",
			size, len(buf))
	}
	if !errors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("MarshalToCodec on a short buffer must wrap ErrBufferTooShort for programmatic matching (got: %v)", err)
	}
	if n != 0 {
		t.Fatalf("MarshalToCodec must return n=0 when rejecting a short buffer — non-zero would imply a partial write was committed (got n=%d)",
			n)
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
		t.Fatalf("MarshalCodec must succeed on the primer sample (got: %v)", err)
	}
	growerBuf, err := PT(&grower).MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec must succeed on the grower sample (got: %v)", err)
	}
	var recv T
	if uerr := PT(&recv).UnmarshalCodec(primerBuf); uerr != nil {
		t.Fatalf("UnmarshalCodec must succeed on the primer wire — establishes warm-path state (got: %v)", uerr)
	}
	if uerr := PT(&recv).UnmarshalCodec(growerBuf); uerr != nil {
		t.Fatalf("UnmarshalCodec must grow a primed receiver's slice when the new payload has more elements — warm-path slice-growth contract (got: %v)",
			uerr)
	}
}

// AssertPackedAcceptsUnpacked verifies a repeated packed-eligible field
// accepts the unpacked wire form — a single element emitted as
// tag(fieldNum, wireType) + elementSize bytes. proto3 allows both
// packed (wireType 2, length-delimited) and unpacked (single-element
// tags) encodings for the same field; decoders must accept both.
//
// wireType must match the field's element type:
//   - 0 for varint elements (int32/int64/uint32/uint64/sint32/sint64/bool/enum)
//   - 1 for fixed64 elements (fixed64/sfixed64/double); elementSize = 8
//   - 5 for fixed32 elements (fixed32/sfixed32/float); elementSize = 4
//
// elementSize is the body size (0 for varint: the body is a varint
// whose length depends on value; a single-byte value=0 satisfies the
// test with elementSize=1).
func AssertPackedAcceptsUnpacked[T any, PT interface {
	*T
	codec.Codec
}](t TB, fieldNum int32, wireType uint64, elementSize int) {
	t.Helper()
	tag := uint64(fieldNum)<<3 | (wireType & 0x7)
	buf := appendVarint(nil, tag)
	buf = append(buf, make([]byte, elementSize)...)
	var got T
	if err := PT(&got).UnmarshalCodec(buf); err != nil {
		t.Fatalf("field %d (wireType %d, elemSize %d): repeated packed field must accept its element's unpacked wire form — proto3 dual-encoding compatibility (got: %v)",
			fieldNum, wireType, elementSize, err)
	}
}

// AssertPackedAcceptsUnpackedCorrupt verifies the unpacked-alternate
// path's error handling for a repeated packed-eligible field. Sends a
// tag with the element wire type followed by a deliberately corrupt /
// truncated body:
//   - wireType 0: unterminated varint (10 continuation bytes)
//   - wireType 1: only elementSize-1 body bytes (short fixed64)
//   - wireType 5: only elementSize-1 body bytes (short fixed32)
//
// The decoder must return an error; the helper accepts either
// ErrInvalidVarint (for wireType 0) or ErrBufferTooShort (for
// wireType 1 / 5).
func AssertPackedAcceptsUnpackedCorrupt[T any, PT interface {
	*T
	codec.Codec
}](t TB, fieldNum int32, wireType uint64, elementSize int) {
	t.Helper()
	tag := uint64(fieldNum)<<3 | (wireType & 0x7)
	buf := appendVarint(nil, tag)
	var wantErr error
	switch wireType {
	case 0:
		buf = append(buf, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
		wantErr = codec.ErrInvalidVarint
	case 1, 5:
		// One byte short of elementSize triggers the bounds guard.
		buf = append(buf, make([]byte, elementSize-1)...)
		wantErr = codec.ErrBufferTooShort
	default:
		// Programmer error in the helper itself, not a contract violation.
		t.Fatalf("AssertPackedAcceptsUnpackedCorrupt: unsupported wireType %d (test bug, not an SUT contract)", wireType)
	}
	var got T
	err := PT(&got).UnmarshalCodec(buf)
	if err == nil {
		t.Fatalf("field %d (wireType %d): UnmarshalCodec must reject a corrupt unpacked-alternate body — DoS / corruption resistance (got nil error)",
			fieldNum, wireType)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("field %d (wireType %d): corrupt unpacked-alternate error must wrap %v for programmatic matching (got: %v)",
			fieldNum, wireType, wantErr, err)
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
		t.Fatalf("UnmarshalCodec must reject a packed-field body containing a malformed inner varint — DoS resistance (got nil error)")
	}
	if !errors.Is(err, codec.ErrInvalidVarint) {
		t.Fatalf("UnmarshalCodec on a malformed packed-body varint must wrap ErrInvalidVarint for programmatic matching (got: %v)", err)
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
		t.Fatalf("MarshalCodec must skip nil elements in repeated []*T fields — defensive against zero-value slot insertion (got: %v)", err)
	}
	var got T
	if uerr := PT(&got).UnmarshalCodec(buf); uerr != nil {
		t.Fatalf("UnmarshalCodec must succeed on wire produced by a nil-element-skipping Marshal (got: %v)", uerr)
	}
	rebuf, merr := PT(&got).MarshalCodec()
	if merr != nil {
		t.Fatalf("re-MarshalCodec on the decoded receiver must succeed (got: %v)", merr)
	}
	if len(rebuf) != len(buf) {
		t.Fatalf("re-MarshalCodec must produce wire of identical length — nil-skip Marshal output should already be canonical so a roundtrip cannot grow or shrink it (before=%d, after=%d)",
			len(buf), len(rebuf))
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
			t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
		}
		i := 0
		for i < len(buf) {
			tag, n := codec.DecodeVarint(buf[i:])
			if n < 0 {
				t.Fatalf("DecodeVarint must succeed on every tag in a valid Marshal output — internal helper invariant (offset %d)", i)
			}
			fieldNum := int32(tag >> 3)
			wireType := byte(tag & 7)
			seen[fieldNum] = wireType
			i += n
			skip, skipErr := codec.SkipField(buf[i:], uint64(wireType))
			if skipErr != nil {
				t.Fatalf("SkipField must succeed on every field in a valid Marshal output — internal helper invariant (offset %d, err: %v)",
					i, skipErr)
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
		t.Fatalf("field %d on wrong wireType %d: UnmarshalCodec must either accept (proto3 dual-encoding) or wrap ErrInvalidWireType — every other error class indicates a generated-code bug (got: %v)",
			fieldNum, wrongWireType, err)
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
		t.Fatalf("field %d: UnmarshalCodec must reject a malformed scalar-varint body — DoS resistance (got nil error)", fieldNum)
	}
	if !errors.Is(err, codec.ErrInvalidVarint) {
		t.Fatalf("field %d: malformed-varint error must wrap ErrInvalidVarint for programmatic matching (got: %v)", fieldNum, err)
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
		t.Fatalf("map-entry decode (case A): UnmarshalCodec must reject a corrupt value varint inside a map entry body — DoS resistance (got nil error)")
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

// AssertMapEntryUnknownSubFieldSkipped verifies the per-map default
// branch in the inner entry-decode switch — the path that consumes
// (via SkipField) any sub-field whose number is neither 1 (key) nor
// 2 (value). Forward compatibility with future schema additions
// requires this default to be reachable; without a covering test it
// only fires on lucky fuzz inputs and depends on whether a corpus has
// been seeded.
//
// The wire is: outer map field tag + entry length + entry body
// containing key sub-field 1 (varint=0), value sub-field 2 (zero-
// length bytes), and an unknown sub-field 3 (varint=0). The decoder
// must consume all three and continue.
func AssertMapEntryUnknownSubFieldSkipped[T any, PT interface {
	*T
	codec.Codec
}](t TB, mapFieldNum int32) {
	t.Helper()
	tag := uint64(mapFieldNum)<<3 | 2

	// Entry body: key (field 1, varint=0) + value (field 2, bytes len 0) +
	// unknown (field 3, varint=0). Six bytes total.
	body := []byte{0x08, 0x00, 0x12, 0x00, 0x18, 0x00}

	buf := appendVarint(nil, tag)
	buf = appendVarint(buf, uint64(len(body)))
	buf = append(buf, body...)

	var got T
	if err := PT(&got).UnmarshalCodec(buf); err != nil {
		t.Fatalf("map field %d: UnmarshalCodec must skip unknown sub-fields inside a map entry — proto3 forward compatibility (got: %v)",
			mapFieldNum, err)
	}
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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
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
			t.Fatalf("prescan must skip an unknown field of wire type %d before reaching the main decode loop — covers all four valid proto3 wire types (got: %v)",
				wireType, uerr)
		}
		remarshal, merr := PT(&got).MarshalCodec()
		if merr != nil {
			t.Fatalf("re-MarshalCodec must succeed after consuming a prefixed unknown field of wire type %d (got: %v)",
				wireType, merr)
		}
		if !bytes.Equal(remarshal, valid) {
			t.Fatalf("re-MarshalCodec must produce wire identical to the original valid Marshal — unknown fields are dropped per our codec semantics (wire type %d)\n  want=%x\n  got=%x",
				wireType, valid, remarshal)
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
		t.Fatalf("fixed_len bytes field %d: UnmarshalCodec must reject a malformed length varint — DoS resistance (got nil error)",
			fieldNum)
	}
	// Case B: length varint decodes to a value != declaredLen → ErrInvalidLength.
	bad2 := appendVarint(nil, tag)
	bad2 = appendVarint(bad2, uint64(declaredLen)+1)
	var got2 T
	err := PT(&got2).UnmarshalCodec(bad2)
	if err == nil || !errors.Is(err, codec.ErrInvalidLength) {
		t.Fatalf("fixed_len bytes field %d: a length mismatch must wrap ErrInvalidLength — closes silent truncation/zero-padding on cryptographic types (got: %v)",
			fieldNum, err)
	}
	// Case C: length matches declaredLen but body is too short.
	bad3 := appendVarint(nil, tag)
	bad3 = appendVarint(bad3, uint64(declaredLen))
	// no body bytes follow
	var got3 T
	err = PT(&got3).UnmarshalCodec(bad3)
	if err == nil || !errors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("fixed_len bytes field %d: a body shorter than the declared length must wrap ErrBufferTooShort — DoS resistance (got: %v)",
			fieldNum, err)
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
		// Programmer error in the helper itself, not a contract violation.
		t.Fatalf("AssertCorruptFixedWidth: width must be 4 or 8 (test bug, not an SUT contract; got: %d)", width)
	}
	tag := uint64(fieldNum)<<3 | wireType
	buf := appendVarint(nil, tag)
	// Body is width-1 bytes — one byte short.
	buf = append(buf, make([]byte, width-1)...)
	var got T
	err := PT(&got).UnmarshalCodec(buf)
	if err == nil || !errors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("fixed-width scalar field %d (width=%d): UnmarshalCodec must reject a body shorter than the declared width — DoS resistance (got: %v)",
			fieldNum, width, err)
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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
	}
	// Unknown varint field: tag(num, 0) + one-byte varint value 0.
	tag := uint64(unknownFieldNum) << 3
	buf := append([]byte{}, valid...)
	buf = appendVarint(buf, tag)
	buf = append(buf, 0x00)
	var got T
	if uerr := PT(&got).UnmarshalCodec(buf); uerr != nil {
		t.Fatalf("UnmarshalCodec must skip unknown fields without error — proto3 forward compatibility (got: %v)", uerr)
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
		t.Fatalf("MarshalCodec must succeed on a valid sample (got: %v)", err)
	}
	tag := uint64(unknownFieldNum)<<3 | 3
	buf := append([]byte{}, valid...)
	buf = appendVarint(buf, tag)
	var got T
	if err := PT(&got).UnmarshalCodec(buf); err == nil {
		t.Fatalf("UnmarshalCodec must reject an unknown field with reserved/undefined wire type 3 — wire-type 3 is not a valid proto3 wire type (got nil error)")
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
