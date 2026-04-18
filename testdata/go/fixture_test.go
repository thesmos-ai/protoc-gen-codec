// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package testdata_test

import (
	"encoding/json"
	stderrors "errors"
	"reflect"
	"strings"
	"testing"

	"go.stealthscale.io/protoc-gen-codec/codec"
	testdata "go.stealthscale.io/protoc-gen-codec/testdata/go"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Builders
// ---------------------------------------------------------------------------

func digest(seed byte) testdata.Digest {
	var d testdata.Digest
	for i := range d {
		d[i] = seed ^ byte(i)
	}
	return d
}

func sampleFixture() testdata.Fixture {
	return testdata.Fixture{
		ID: "fix-001", Kind: 42, Status: testdata.StatusRunning,
		Score: -85_000_000, Sequence: 12345, Enabled: true,
		Timestamp: 1713400000000, Ref: digest(0x01),
		Tags: []string{"alpha", "beta", "gamma"},
		Data: []byte{0xde, 0xad, 0xbe, 0xef},
	}
}

func samplePatchText() testdata.Patch {
	return testdata.Patch{
		Kind: testdata.PatchKindText, VertexID: 7, Sequence: 1001,
		Source: testdata.SourceInference, TextVal: "hello world",
	}
}

func samplePatchFixed64() testdata.Patch {
	return testdata.Patch{
		Kind: testdata.PatchKindFixed64, VertexID: 3, Sequence: 2002,
		Source: testdata.SourceGate, Fixed64Val: 85_000_000,
	}
}

func samplePatchBlob() testdata.Patch {
	return testdata.Patch{
		Kind: testdata.PatchKindBlob, VertexID: 12, Sequence: 3003,
		Source: testdata.SourceExternal, BlobRef: digest(0xAB),
	}
}

func sampleEvidence() testdata.Evidence {
	return testdata.Evidence{
		Kind: testdata.EvidenceDecision, Durability: testdata.DurabilityHard,
		Access: testdata.AccessTierDualKey, TraceID: "trace-abc",
		FederationTraceID: "fed-xyz", JobID: "job-001",
		ThreadID: "thread-42", TenantID: "tenant-acme",
		TimestampMs: 1713400000000, PayloadRef: digest(0xFF),
		Jurisdictions:     []string{"EU", "US-CA", "UK"},
		RetentionPolicyID: "policy-gdpr-7y",
	}
}

func sampleMinimal() testdata.Minimal {
	return testdata.Minimal{ID: "min-001"}
}

func sampleNumericOnly() testdata.NumericOnly {
	return testdata.NumericOnly{A: 42, B: 1_000_000_000, C: -999_999, D: 85_000_000, E: true}
}

// ---------------------------------------------------------------------------
// Roundtrip tests
// ---------------------------------------------------------------------------

func TestFixture_Roundtrip(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.Fixture](t, sampleFixture())
}

func TestFixture_Roundtrip_Zero(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.Fixture](t, testdata.Fixture{})
}

func TestFixture_Reset(t *testing.T) {
	t.Parallel()
	codec.AssertReset[testdata.Fixture](t, sampleFixture())
}

func TestFixture_NilSafe(t *testing.T) {
	t.Parallel()
	codec.AssertNilSafe[testdata.Fixture](t)
}

func TestFixture_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var ref testdata.Digest
		for i := range ref {
			ref[i] = rapid.Byte().Draw(t, "b")
		}
		nTags := rapid.IntRange(0, 5).Draw(t, "nTags")
		var tags []string
		if nTags > 0 {
			tags = make([]string, nTags)
			for i := range tags {
				tags[i] = rapid.String().Draw(t, "tag")
			}
		}
		nData := rapid.IntRange(0, 32).Draw(t, "nData")
		var data []byte
		if nData > 0 {
			data = make([]byte, nData)
			for i := range data {
				data[i] = rapid.Byte().Draw(t, "db")
			}
		}
		f := testdata.Fixture{
			ID: rapid.String().Draw(t, "id"), Kind: rapid.Uint32().Draw(t, "kind"),
			Status: testdata.Status(rapid.Uint8Range(0, 3).Draw(t, "status")),
			Score:  rapid.Int64().Draw(t, "score"), Sequence: rapid.Uint64().Draw(t, "seq"),
			Enabled: rapid.Bool().Draw(t, "enabled"), Timestamp: rapid.Int64().Draw(t, "ts"),
			Ref: ref, Tags: tags, Data: data,
		}
		codec.AssertRoundtrip[testdata.Fixture](t, f)
	})
}

func TestPatch_Roundtrip_Text(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.Patch](t, samplePatchText())
}

func TestPatch_Roundtrip_Fixed64(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.Patch](t, samplePatchFixed64())
}

func TestPatch_Roundtrip_Blob(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.Patch](t, samplePatchBlob())
}

func TestPatch_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var d testdata.Digest
		for i := range d {
			d[i] = rapid.Byte().Draw(t, "b")
		}
		p := testdata.Patch{
			Kind:     testdata.PatchKind(rapid.Uint8Range(0, 4).Draw(t, "kind")),
			VertexID: rapid.Uint32().Draw(t, "vid"), Sequence: rapid.Uint64().Draw(t, "seq"),
			Source:  testdata.Source(rapid.Uint8Range(0, 3).Draw(t, "src")),
			TextVal: rapid.String().Draw(t, "tv"), IntVal: rapid.Int64().Draw(t, "iv"),
			Fixed64Val: testdata.Fixed64(rapid.Int64().Draw(t, "fv")), BlobRef: d,
		}
		codec.AssertRoundtrip[testdata.Patch](t, p)
	})
}

func TestPatch_NegativeFixed64(t *testing.T) {
	t.Parallel()
	p := testdata.Patch{Kind: testdata.PatchKindFixed64, Fixed64Val: -85_000_000}
	codec.AssertRoundtrip[testdata.Patch](t, p)
}

func TestPatch_Reset(t *testing.T) {
	t.Parallel()
	codec.AssertReset[testdata.Patch](t, samplePatchBlob())
}

func TestPatch_NilSafe(t *testing.T) {
	t.Parallel()
	codec.AssertNilSafe[testdata.Patch](t)
}

func TestEvidence_Roundtrip(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.Evidence](t, sampleEvidence())
}

func TestEvidence_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var d testdata.Digest
		for i := range d {
			d[i] = rapid.Byte().Draw(t, "b")
		}
		nJ := rapid.IntRange(0, 5).Draw(t, "nJ")
		var js []string
		if nJ > 0 {
			js = make([]string, nJ)
			for i := range js {
				js[i] = rapid.String().Draw(t, "j")
			}
		}
		e := testdata.Evidence{
			Kind:       testdata.EvidenceKind(rapid.Uint8Range(0, 3).Draw(t, "kind")),
			Durability: testdata.Durability(rapid.Uint8Range(0, 1).Draw(t, "dur")),
			Access:     testdata.AccessTier(rapid.Uint8Range(0, 4).Draw(t, "acc")),
			TraceID:    rapid.String().Draw(t, "tid"), FederationTraceID: rapid.String().Draw(t, "ftid"),
			JobID: rapid.String().Draw(t, "jid"), ThreadID: rapid.String().Draw(t, "thid"),
			TenantID: rapid.String().Draw(t, "tnid"), TimestampMs: rapid.Int64().Draw(t, "ts"),
			PayloadRef: d, Jurisdictions: js,
			RetentionPolicyID: rapid.String().Draw(t, "rpid"),
		}
		codec.AssertRoundtrip[testdata.Evidence](t, e)
	})
}

func TestEvidence_LongStrings(t *testing.T) {
	t.Parallel()
	e := sampleEvidence()
	e.TraceID = strings.Repeat("a", 4096)
	e.JobID = strings.Repeat("b", 8192)
	codec.AssertRoundtrip[testdata.Evidence](t, e)
}

func TestEvidence_Reset(t *testing.T) {
	t.Parallel()
	codec.AssertReset[testdata.Evidence](t, sampleEvidence())
}

func TestEvidence_NilSafe(t *testing.T) {
	t.Parallel()
	codec.AssertNilSafe[testdata.Evidence](t)
}

func TestMinimal_Roundtrip(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.Minimal](t, sampleMinimal())
}

func TestMinimal_NilSafe(t *testing.T) {
	t.Parallel()
	codec.AssertNilSafe[testdata.Minimal](t)
}

func TestNumericOnly_Roundtrip(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[testdata.NumericOnly](t, sampleNumericOnly())
}

func TestNumericOnly_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := testdata.NumericOnly{
			A: rapid.Uint32().Draw(t, "a"), B: rapid.Uint64().Draw(t, "b"),
			C: rapid.Int64().Draw(t, "c"), D: testdata.Fixed64(rapid.Int64().Draw(t, "d")),
			E: rapid.Bool().Draw(t, "e"),
		}
		codec.AssertRoundtrip[testdata.NumericOnly](t, n)
	})
}

func TestNumericOnly_NilSafe(t *testing.T) {
	t.Parallel()
	codec.AssertNilSafe[testdata.NumericOnly](t)
}

// ---------------------------------------------------------------------------
// Wire size
// ---------------------------------------------------------------------------

func TestEvidence_WireSize_SmallerThanJSON(t *testing.T) {
	t.Parallel()
	e := sampleEvidence()
	vt, _ := e.MarshalCodec()
	js, _ := json.Marshal(e)
	if len(vt) >= len(js) {
		t.Fatalf("Codec %d bytes >= JSON %d bytes", len(vt), len(js))
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestFixture_FixedLen_Reject31(t *testing.T) {
	t.Parallel()
	data := append([]byte{0x42, 31}, make([]byte, 31)...)
	var f testdata.Fixture
	if err := f.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidLength) {
		t.Fatalf("expected ErrInvalidLength, got %v", err)
	}
}

func TestFixture_WrongWireType(t *testing.T) {
	t.Parallel()
	data := []byte{0x09, 0, 0, 0, 0, 0, 0, 0, 0}
	var f testdata.Fixture
	if err := f.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidWireType) {
		t.Fatalf("expected ErrInvalidWireType, got %v", err)
	}
}

func TestFixture_InvalidTag(t *testing.T) {
	t.Parallel()
	data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	var f testdata.Fixture
	if err := f.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidTag) {
		t.Fatalf("expected ErrInvalidTag, got %v", err)
	}
}

func TestFixture_UnknownField(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	buf, _ := f.MarshalCodec()
	buf = append(buf, 0xf8, 0x06, 42)
	var got testdata.Fixture
	if err := got.UnmarshalCodec(buf); err != nil {
		t.Fatalf("should skip unknown: %v", err)
	}
	if got.ID != f.ID {
		t.Fatal("known fields corrupted")
	}
}

func TestPatch_BlobRef_FixedLen(t *testing.T) {
	t.Parallel()
	data := append([]byte{0x42, 16}, make([]byte, 16)...)
	var p testdata.Patch
	if err := p.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidLength) {
		t.Fatalf("expected ErrInvalidLength, got %v", err)
	}
}

func TestEvidence_PayloadRef_FixedLen(t *testing.T) {
	t.Parallel()
	data := []byte{0x52, 0}
	var e testdata.Evidence
	if err := e.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidLength) {
		t.Fatalf("expected ErrInvalidLength, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Security: zip bomb / DoS resistance
// ---------------------------------------------------------------------------

func TestFixture_ZipBomb_InflatedString(t *testing.T) {
	t.Parallel()
	data := []byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0x0f}
	var f testdata.Fixture
	if err := f.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated length prefix")
	}
}

func TestFixture_ZipBomb_InflatedBytes(t *testing.T) {
	t.Parallel()
	data := []byte{0x52, 0x80, 0x80, 0x80, 0x80, 0x08}
	var f testdata.Fixture
	if err := f.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated length prefix")
	}
}

func TestEvidence_ZipBomb_SmartSlab(t *testing.T) {
	t.Parallel()
	data := []byte{0x22, 0xff, 0xff, 0xff, 0xff, 0x0f}
	var e testdata.Evidence
	if err := e.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated string in smart slab")
	}
}

func TestNumericOnly_ZipBomb_PackedVarint(t *testing.T) {
	t.Parallel()
	data := []byte{0x0a, 0x80, 0x80, 0x80, 0x80, 0x04}
	var n testdata.NumericOnly
	if err := n.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated packed length")
	}
}

// ---------------------------------------------------------------------------
// Corruption injection
// ---------------------------------------------------------------------------

func TestFixture_CorruptionInjection(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	valid, _ := f.MarshalCodec()
	for i := range len(valid) {
		var got testdata.Fixture
		got.UnmarshalCodec(valid[:i])
	}
	for i := range len(valid) {
		corrupted := make([]byte, len(valid))
		copy(corrupted, valid)
		corrupted[i] ^= 0xFF
		var got testdata.Fixture
		got.UnmarshalCodec(corrupted)
	}
}

func TestEvidence_CorruptionInjection(t *testing.T) {
	t.Parallel()
	e := sampleEvidence()
	valid, _ := e.MarshalCodec()
	for i := range len(valid) {
		var got testdata.Evidence
		got.UnmarshalCodec(valid[:i])
	}
	for i := range len(valid) {
		corrupted := make([]byte, len(valid))
		copy(corrupted, valid)
		corrupted[i] ^= 0xFF
		var got testdata.Evidence
		got.UnmarshalCodec(corrupted)
	}
}

// ---------------------------------------------------------------------------
// Pooled reuse
// ---------------------------------------------------------------------------

func TestFixture_PooledReuse(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	buf, _ := f.MarshalCodec()
	old := testdata.Fixture{ID: "old", Tags: []string{"x", "y", "z", "w", "q"}}
	if err := old.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalizeFixture(f), normalizeFixture(old)) {
		t.Fatalf("pooled reuse mismatch:\n  want: %+v\n  got:  %+v", f, old)
	}
}

// ---------------------------------------------------------------------------
// Fuzz
// ---------------------------------------------------------------------------

func FuzzFixture_Unmarshal(f *testing.F) {
	s := sampleFixture()
	if buf, _ := s.MarshalCodec(); buf != nil {
		f.Add(buf)
	}
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff})
	f.Fuzz(func(_ *testing.T, data []byte) {
		var fix testdata.Fixture
		fix.UnmarshalCodec(data)
	})
}

func FuzzFixture_Roundtrip(f *testing.F) {
	s := sampleFixture()
	if buf, _ := s.MarshalCodec(); buf != nil {
		f.Add(buf)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var first testdata.Fixture
		if err := first.UnmarshalCodec(data); err != nil {
			return
		}
		re, err := first.MarshalCodec()
		if err != nil {
			t.Fatalf("re-MarshalCodec: %v", err)
		}
		var second testdata.Fixture
		if err := second.UnmarshalCodec(re); err != nil {
			t.Fatalf("second UnmarshalCodec: %v", err)
		}
		if first.ID != second.ID || first.Kind != second.Kind || first.Ref != second.Ref {
			t.Fatal("roundtrip mismatch")
		}
	})
}

func FuzzPatch_Roundtrip(f *testing.F) {
	for _, p := range []testdata.Patch{samplePatchText(), samplePatchFixed64(), samplePatchBlob()} {
		if buf, _ := p.MarshalCodec(); buf != nil {
			f.Add(buf)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var first testdata.Patch
		if err := first.UnmarshalCodec(data); err != nil {
			return
		}
		re, err := first.MarshalCodec()
		if err != nil {
			t.Fatalf("re-MarshalCodec: %v", err)
		}
		var second testdata.Patch
		if err := second.UnmarshalCodec(re); err != nil {
			t.Fatalf("second UnmarshalCodec: %v", err)
		}
		if first.Kind != second.Kind || first.TextVal != second.TextVal || first.BlobRef != second.BlobRef {
			t.Fatal("roundtrip mismatch")
		}
	})
}

func FuzzEvidence_Roundtrip(f *testing.F) {
	e := sampleEvidence()
	if buf, _ := e.MarshalCodec(); buf != nil {
		f.Add(buf)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var first testdata.Evidence
		if err := first.UnmarshalCodec(data); err != nil {
			return
		}
		re, err := first.MarshalCodec()
		if err != nil {
			t.Fatalf("re-MarshalCodec: %v", err)
		}
		var second testdata.Evidence
		if err := second.UnmarshalCodec(re); err != nil {
			t.Fatalf("second UnmarshalCodec: %v", err)
		}
		if first.TraceID != second.TraceID || first.PayloadRef != second.PayloadRef {
			t.Fatal("roundtrip mismatch")
		}
	})
}

func FuzzNumericOnly_Roundtrip(f *testing.F) {
	n := sampleNumericOnly()
	if buf, _ := n.MarshalCodec(); buf != nil {
		f.Add(buf)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var first testdata.NumericOnly
		if err := first.UnmarshalCodec(data); err != nil {
			return
		}
		re, err := first.MarshalCodec()
		if err != nil {
			t.Fatalf("re-MarshalCodec: %v", err)
		}
		var second testdata.NumericOnly
		if err := second.UnmarshalCodec(re); err != nil {
			t.Fatalf("second UnmarshalCodec: %v", err)
		}
		if first != second {
			t.Fatal("roundtrip mismatch")
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkFixture_Codec_MarshalTo(b *testing.B) {
	f := sampleFixture()
	buf := make([]byte, f.SizeCodec())
	for range b.N {
		f.MarshalToCodec(buf)
	}
}

func BenchmarkFixture_Codec_Unmarshal(b *testing.B) {
	f := sampleFixture()
	data, _ := f.MarshalCodec()
	for range b.N {
		var got testdata.Fixture
		got.UnmarshalCodec(data)
	}
}

func BenchmarkFixture_JSON_Marshal(b *testing.B) {
	f := sampleFixture()
	for range b.N {
		json.Marshal(f)
	}
}

func BenchmarkFixture_JSON_Unmarshal(b *testing.B) {
	data, _ := json.Marshal(sampleFixture())
	for range b.N {
		var got testdata.Fixture
		json.Unmarshal(data, &got)
	}
}

func BenchmarkPatch_Codec_MarshalTo(b *testing.B) {
	p := samplePatchText()
	buf := make([]byte, p.SizeCodec())
	for range b.N {
		p.MarshalToCodec(buf)
	}
}

func BenchmarkPatch_Codec_Unmarshal(b *testing.B) {
	p := samplePatchText()
	data, _ := p.MarshalCodec()
	for range b.N {
		var got testdata.Patch
		got.UnmarshalCodec(data)
	}
}

func BenchmarkEvidence_Codec_MarshalTo(b *testing.B) {
	e := sampleEvidence()
	buf := make([]byte, e.SizeCodec())
	for range b.N {
		e.MarshalToCodec(buf)
	}
}

func BenchmarkEvidence_Codec_Unmarshal(b *testing.B) {
	e := sampleEvidence()
	data, _ := e.MarshalCodec()
	for range b.N {
		var got testdata.Evidence
		got.UnmarshalCodec(data)
	}
}

func BenchmarkNumericOnly_Codec_MarshalTo(b *testing.B) {
	n := sampleNumericOnly()
	buf := make([]byte, n.SizeCodec())
	for range b.N {
		n.MarshalToCodec(buf)
	}
}

func BenchmarkNumericOnly_Codec_Unmarshal(b *testing.B) {
	n := sampleNumericOnly()
	data, _ := n.MarshalCodec()
	for range b.N {
		var got testdata.NumericOnly
		got.UnmarshalCodec(data)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func normalizeFixture(f testdata.Fixture) testdata.Fixture {
	if f.Tags == nil {
		f.Tags = []string{}
	}
	if len(f.Tags) == 0 {
		f.Tags = []string{}
	}
	if f.Data == nil {
		f.Data = []byte{}
	}
	if len(f.Data) == 0 {
		f.Data = []byte{}
	}
	return f
}
