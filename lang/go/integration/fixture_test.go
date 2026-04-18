// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	stderrors "errors"
	"reflect"
	"strings"
	"testing"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec"
	"go.stealthscale.io/protoc-gen-codec/lang/go/integration"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Sample builders
// ---------------------------------------------------------------------------

func digest(seed byte) integration.Digest {
	var d integration.Digest
	for i := range d {
		d[i] = seed ^ byte(i)
	}
	return d
}

func sampleFixture() integration.Fixture {
	return integration.Fixture{
		ID: "fix-001", Kind: 42, Status: integration.StatusRunning,
		Score: -85_000_000, Sequence: 12345, Enabled: true,
		Timestamp: 1713400000000, Ref: digest(0x01),
		Tags: []string{"alpha", "beta", "gamma"},
		Data: []byte{0xde, 0xad, 0xbe, 0xef},
	}
}

func samplePatchText() integration.Patch {
	return integration.Patch{
		Kind: integration.PatchKindText, VertexID: 7, Sequence: 1001,
		Source: integration.SourceInference, TextVal: "hello world",
	}
}

func samplePatchFixed64() integration.Patch {
	return integration.Patch{
		Kind: integration.PatchKindFixed64, VertexID: 3, Sequence: 2002,
		Source: integration.SourceGate, Fixed64Val: 85_000_000,
	}
}

func samplePatchBlob() integration.Patch {
	return integration.Patch{
		Kind: integration.PatchKindBlob, VertexID: 12, Sequence: 3003,
		Source: integration.SourceExternal, BlobRef: digest(0xAB),
	}
}

func sampleEvidence() integration.Evidence {
	return integration.Evidence{
		Kind: integration.EvidenceDecision, Durability: integration.DurabilityHard,
		Access: integration.AccessTierDualKey, TraceID: "trace-abc",
		FederationTraceID: "fed-xyz", JobID: "job-001",
		ThreadID: "thread-42", TenantID: "tenant-acme",
		TimestampMs: 1713400000000, PayloadRef: digest(0xFF),
		Jurisdictions:     []string{"EU", "US-CA", "UK"},
		RetentionPolicyID: "policy-gdpr-7y",
	}
}

func sampleMinimal() integration.Minimal   { return integration.Minimal{ID: "min-001"} }
func sampleNumericOnly() integration.NumericOnly {
	return integration.NumericOnly{A: 42, B: 1_000_000_000, C: -999_999, D: 85_000_000, E: true}
}

// ---------------------------------------------------------------------------
// Standard codec suite per fixture
// ---------------------------------------------------------------------------

func TestFixture_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Fixture](t, sampleFixture())
}

func TestPatch_Text_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Patch](t, samplePatchText())
}

func TestPatch_Fixed64_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Patch](t, samplePatchFixed64())
}

func TestPatch_Blob_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Patch](t, samplePatchBlob())
}

func TestEvidence_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Evidence](t, sampleEvidence())
}

func TestMinimal_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Minimal](t, sampleMinimal())
}

func TestNumericOnly_Codec(t *testing.T) {
	codec.RunTestSuite[integration.NumericOnly](t, sampleNumericOnly())
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkFixture_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.Fixture](b, sampleFixture())
}

func BenchmarkPatch_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.Patch](b, samplePatchText())
}

func BenchmarkEvidence_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.Evidence](b, sampleEvidence())
}

func BenchmarkNumericOnly_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.NumericOnly](b, sampleNumericOnly())
}

// ---------------------------------------------------------------------------
// Fuzz targets
// ---------------------------------------------------------------------------

func FuzzFixture_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.Fixture](f, sampleFixture())
}

func FuzzPatch_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.Patch](f, samplePatchText(), samplePatchFixed64(), samplePatchBlob())
}

func FuzzEvidence_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.Evidence](f, sampleEvidence())
}

func FuzzNumericOnly_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.NumericOnly](f, sampleNumericOnly())
}

// ---------------------------------------------------------------------------
// Property-based tests (draws random inputs)
// ---------------------------------------------------------------------------

func TestFixture_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var ref integration.Digest
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
		f := integration.Fixture{
			ID: rapid.String().Draw(t, "id"), Kind: rapid.Uint32().Draw(t, "kind"),
			Status: integration.Status(rapid.Uint8Range(0, 3).Draw(t, "status")),
			Score:  rapid.Int64().Draw(t, "score"), Sequence: rapid.Uint64().Draw(t, "seq"),
			Enabled: rapid.Bool().Draw(t, "enabled"), Timestamp: rapid.Int64().Draw(t, "ts"),
			Ref: ref, Tags: tags, Data: data,
		}
		codec.AssertRoundtrip[integration.Fixture](t, f)
	})
}

func TestPatch_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var d integration.Digest
		for i := range d {
			d[i] = rapid.Byte().Draw(t, "b")
		}
		p := integration.Patch{
			Kind:     integration.PatchKind(rapid.Uint8Range(0, 4).Draw(t, "kind")),
			VertexID: rapid.Uint32().Draw(t, "vid"), Sequence: rapid.Uint64().Draw(t, "seq"),
			Source:  integration.Source(rapid.Uint8Range(0, 3).Draw(t, "src")),
			TextVal: rapid.String().Draw(t, "tv"), IntVal: rapid.Int64().Draw(t, "iv"),
			Fixed64Val: integration.Fixed64(rapid.Int64().Draw(t, "fv")), BlobRef: d,
		}
		codec.AssertRoundtrip[integration.Patch](t, p)
	})
}

func TestEvidence_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var d integration.Digest
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
		e := integration.Evidence{
			Kind:       integration.EvidenceKind(rapid.Uint8Range(0, 3).Draw(t, "kind")),
			Durability: integration.Durability(rapid.Uint8Range(0, 1).Draw(t, "dur")),
			Access:     integration.AccessTier(rapid.Uint8Range(0, 4).Draw(t, "acc")),
			TraceID:    rapid.String().Draw(t, "tid"), FederationTraceID: rapid.String().Draw(t, "ftid"),
			JobID: rapid.String().Draw(t, "jid"), ThreadID: rapid.String().Draw(t, "thid"),
			TenantID: rapid.String().Draw(t, "tnid"), TimestampMs: rapid.Int64().Draw(t, "ts"),
			PayloadRef: d, Jurisdictions: js,
			RetentionPolicyID: rapid.String().Draw(t, "rpid"),
		}
		codec.AssertRoundtrip[integration.Evidence](t, e)
	})
}

func TestNumericOnly_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := integration.NumericOnly{
			A: rapid.Uint32().Draw(t, "a"), B: rapid.Uint64().Draw(t, "b"),
			C: rapid.Int64().Draw(t, "c"), D: integration.Fixed64(rapid.Int64().Draw(t, "d")),
			E: rapid.Bool().Draw(t, "e"),
		}
		codec.AssertRoundtrip[integration.NumericOnly](t, n)
	})
}

// ---------------------------------------------------------------------------
// Targeted cases
// ---------------------------------------------------------------------------

func TestPatch_NegativeFixed64(t *testing.T) {
	t.Parallel()
	p := integration.Patch{Kind: integration.PatchKindFixed64, Fixed64Val: -85_000_000}
	codec.AssertRoundtrip[integration.Patch](t, p)
}

func TestEvidence_LongStrings(t *testing.T) {
	t.Parallel()
	e := sampleEvidence()
	e.TraceID = strings.Repeat("a", 4096)
	e.JobID = strings.Repeat("b", 8192)
	codec.AssertRoundtrip[integration.Evidence](t, e)
}

// ---------------------------------------------------------------------------
// Wire size
// ---------------------------------------------------------------------------

func TestEvidence_WireSize_SmallerThanJSON(t *testing.T) {
	t.Parallel()
	codec.AssertWireSmallerThanJSON[integration.Evidence](t, sampleEvidence())
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestFixture_FixedLen_Reject31(t *testing.T) {
	t.Parallel()
	data := append([]byte{0x42, 31}, make([]byte, 31)...)
	var f integration.Fixture
	if err := f.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidLength) {
		t.Fatalf("expected ErrInvalidLength, got %v", err)
	}
}

func TestFixture_WrongWireType(t *testing.T) {
	t.Parallel()
	data := []byte{0x09, 0, 0, 0, 0, 0, 0, 0, 0}
	var f integration.Fixture
	if err := f.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidWireType) {
		t.Fatalf("expected ErrInvalidWireType, got %v", err)
	}
}

func TestFixture_InvalidTag(t *testing.T) {
	t.Parallel()
	data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	var f integration.Fixture
	if err := f.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidTag) {
		t.Fatalf("expected ErrInvalidTag, got %v", err)
	}
}

func TestFixture_UnknownField(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	buf, _ := f.MarshalCodec()
	buf = append(buf, 0xf8, 0x06, 42)
	var got integration.Fixture
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
	var p integration.Patch
	if err := p.UnmarshalCodec(data); !stderrors.Is(err, codec.ErrInvalidLength) {
		t.Fatalf("expected ErrInvalidLength, got %v", err)
	}
}

func TestEvidence_PayloadRef_FixedLen(t *testing.T) {
	t.Parallel()
	data := []byte{0x52, 0}
	var e integration.Evidence
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
	var f integration.Fixture
	if err := f.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated length prefix")
	}
}

func TestFixture_ZipBomb_InflatedBytes(t *testing.T) {
	t.Parallel()
	data := []byte{0x52, 0x80, 0x80, 0x80, 0x80, 0x08}
	var f integration.Fixture
	if err := f.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated length prefix")
	}
}

func TestEvidence_ZipBomb_SmartSlab(t *testing.T) {
	t.Parallel()
	data := []byte{0x22, 0xff, 0xff, 0xff, 0xff, 0x0f}
	var e integration.Evidence
	if err := e.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated string in smart slab")
	}
}

func TestNumericOnly_ZipBomb_PackedVarint(t *testing.T) {
	t.Parallel()
	data := []byte{0x0a, 0x80, 0x80, 0x80, 0x80, 0x04}
	var n integration.NumericOnly
	if err := n.UnmarshalCodec(data); err == nil {
		t.Fatal("should reject inflated packed length")
	}
}

// ---------------------------------------------------------------------------
// Pooled reuse
// ---------------------------------------------------------------------------

func TestFixture_PooledReuse(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	buf, _ := f.MarshalCodec()
	old := integration.Fixture{ID: "old", Tags: []string{"x", "y", "z", "w", "q"}}
	if err := old.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalizeFixture(f), normalizeFixture(old)) {
		t.Fatalf("pooled reuse mismatch:\n  want: %+v\n  got:  %+v", f, old)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func normalizeFixture(f integration.Fixture) integration.Fixture {
	if f.Tags == nil || len(f.Tags) == 0 {
		f.Tags = []string{}
	}
	if f.Data == nil || len(f.Data) == 0 {
		f.Data = []byte{}
	}
	return f
}
