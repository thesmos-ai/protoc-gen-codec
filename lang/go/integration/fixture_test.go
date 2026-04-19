// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	stderrors "errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec"
	"go.stealthscale.io/protoc-gen-codec/lang/go/integration"
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

func sampleMinimal() integration.Minimal { return integration.Minimal{ID: "min-001"} }
func sampleNumericOnly() integration.NumericOnly {
	h := int32(-7)
	i := true
	j := integration.Fixed64(123456789)
	return integration.NumericOnly{
		A: 42, B: 1_000_000_000, C: -999_999, D: 85_000_000, E: true,
		F: -42, G: -9_000_000_000, H: &h, I: &i, J: &j,
	}
}

func samplePackedZigzag() integration.PackedZigzag {
	return integration.PackedZigzag{
		Values32: []int32{-1, 0, 1, -2147483648, 2147483647},
		Values64: []int64{-1, 0, 1, -9_000_000_000, 9_000_000_000},
	}
}

func sampleContainer() integration.Container {
	return integration.Container{
		Name:  "alpha",
		Inner: &integration.Inner{Label: "x", Count: 7},
		Children: []*integration.Inner{
			{Label: "c1", Count: 1},
			{Label: "c2", Count: 2},
			{Label: "c3", Count: 3},
		},
	}
}

func sampleValueContainer() integration.ValueContainer {
	return integration.ValueContainer{
		Name:  "alpha",
		Inner: integration.Inner{Label: "center", Count: 0},
		Items: []integration.Inner{
			{Label: "first", Count: 1},
			{Label: "second", Count: 2},
			{Label: "third", Count: 3},
		},
	}
}

func sampleTree() integration.Tree {
	return integration.Tree{
		Label: "root",
		Children: []*integration.Tree{
			{Label: "a", Children: []*integration.Tree{{Label: "a1"}}},
			{Label: "b"},
		},
	}
}

func sampleInner() integration.Inner {
	return integration.Inner{Label: "inner", Count: 42}
}

func sampleMapHolder() integration.MapHolder {
	return integration.MapHolder{
		Attrs:  map[string]string{"region": "eu-west", "tier": "premium"},
		Counts: map[string]int64{"retries": 3, "errors": 0},
	}
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

func TestPackedZigzag_Codec(t *testing.T) {
	codec.RunTestSuite[integration.PackedZigzag](t, samplePackedZigzag())
}

func TestInner_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Inner](t, sampleInner())
}

func TestContainer_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Container](t, sampleContainer())
}

// TestContainer_SlabCorrectness guards the cross-message string slab
// introduced in Phase 4.9. The top-level UnmarshalCodec allocates a single
// string(data) slab that every nested UnmarshalCodecInternal call indexes
// into with an absolute slabOff+i offset. A bug in the offset math would
// either truncate a string, bleed neighbor bytes, or panic — this test
// exercises strings at the outer level, in a singular nested message, and
// across multiple repeated nested elements to catch any such regression.
func TestContainer_SlabCorrectness(t *testing.T) {
	t.Parallel()
	c := integration.Container{
		Name:  "alpha",
		Inner: &integration.Inner{Label: "child", Count: 1},
		Children: []*integration.Inner{
			{Label: "first", Count: 2},
			{Label: "second", Count: 3},
		},
	}
	buf, err := c.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	var got integration.Container
	if err := got.UnmarshalCodec(buf); err != nil {
		t.Fatalf("UnmarshalCodec: %v", err)
	}
	if got.Name != "alpha" {
		t.Errorf("Name: want %q, got %q", "alpha", got.Name)
	}
	if got.Inner == nil || got.Inner.Label != "child" {
		t.Errorf("Inner.Label: want %q, got %+v", "child", got.Inner)
	}
	if len(got.Children) != 2 {
		t.Fatalf("Children: want 2, got %d", len(got.Children))
	}
	if got.Children[0] == nil || got.Children[0].Label != "first" {
		t.Errorf("Children[0].Label: want %q, got %+v", "first", got.Children[0])
	}
	if got.Children[1] == nil || got.Children[1].Label != "second" {
		t.Errorf("Children[1].Label: want %q, got %+v", "second", got.Children[1])
	}
}

func TestValueContainer_Codec(t *testing.T) {
	codec.RunTestSuite[integration.ValueContainer](t, sampleValueContainer())
}

func TestTree_Codec(t *testing.T) {
	codec.RunTestSuite[integration.Tree](t, sampleTree())
}

func TestMapHolder_Codec(t *testing.T) {
	codec.RunTestSuite[integration.MapHolder](t, sampleMapHolder())
}

func BenchmarkMapHolder_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.MapHolder](b, sampleMapHolder())
}

func FuzzMapHolder_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.MapHolder](f, sampleMapHolder(), integration.MapHolder{})
}

func TestMapHolder_Roundtrip_Empty(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.MapHolder](t, integration.MapHolder{})
}

func TestMapHolder_Roundtrip_Populated(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.MapHolder](t, integration.MapHolder{
		Attrs:  map[string]string{"a": "1", "b": "2"},
		Counts: map[string]int64{"x": 99},
	})
}

func TestMapHolder_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nA := rapid.IntRange(0, 4).Draw(t, "nA")
		var attrs map[string]string
		if nA > 0 {
			attrs = make(map[string]string, nA)
			for range nA {
				attrs[rapid.String().Draw(t, "k")] = rapid.String().Draw(t, "v")
			}
		}
		nC := rapid.IntRange(0, 4).Draw(t, "nC")
		var counts map[string]int64
		if nC > 0 {
			counts = make(map[string]int64, nC)
			for range nC {
				counts[rapid.String().Draw(t, "ck")] = rapid.Int64().Draw(t, "cv")
			}
		}
		codec.AssertRoundtrip[integration.MapHolder](t, integration.MapHolder{
			Attrs: attrs, Counts: counts,
		})
	})
}

func TestMapHolder_KeepCapacity_Reuse(t *testing.T) {
	t.Parallel()
	// First unmarshal allocates the map bucket storage.
	first := sampleMapHolder()
	buf, _ := first.MarshalCodec()
	var receiver integration.MapHolder
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if receiver.Counts == nil {
		t.Fatal("expected Counts to be non-nil after first unmarshal")
	}
	if receiver.Attrs == nil {
		t.Fatal("expected Attrs to be non-nil after first unmarshal")
	}
	// Capture map identities so we can verify clear() preserves them.
	countsBefore := receiver.Counts
	attrsBefore := receiver.Attrs
	// Reset — Phase 4.10 always clears in place; both maps keep their backing
	// storage regardless of the (now-deprecated) keep_capacity annotation.
	receiver.ResetCodec()
	if receiver.Counts == nil {
		t.Fatal("Counts should be non-nil after ResetCodec (Phase 4.10 clear()-in-place)")
	}
	if len(receiver.Counts) != 0 {
		t.Fatalf("Counts should be empty after Reset, got len=%d", len(receiver.Counts))
	}
	if receiver.Attrs == nil {
		t.Fatal("Attrs should be non-nil after ResetCodec (Phase 4.10 clear()-in-place)")
	}
	if len(receiver.Attrs) != 0 {
		t.Fatalf("Attrs should be empty after Reset, got len=%d", len(receiver.Attrs))
	}
	// Same map header (i.e. same backing buckets) — clear() preserves identity.
	if reflect.ValueOf(receiver.Counts).Pointer() != reflect.ValueOf(countsBefore).Pointer() {
		t.Errorf("Counts map identity changed after Reset (clear() should reuse buckets)")
	}
	if reflect.ValueOf(receiver.Attrs).Pointer() != reflect.ValueOf(attrsBefore).Pointer() {
		t.Errorf("Attrs map identity changed after Reset (clear() should reuse buckets)")
	}
}

func TestNumericOnly_Zigzag_NegativeValues(t *testing.T) {
	t.Parallel()
	n := integration.NumericOnly{F: -42, G: -9_000_000_000}
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}

func TestNumericOnly_Optional_Unset(t *testing.T) {
	t.Parallel()
	n := integration.NumericOnly{A: 1} // H is nil
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}

func TestNumericOnly_Optional_SetToZero(t *testing.T) {
	t.Parallel()
	zero := int32(0)
	n := integration.NumericOnly{A: 1, H: &zero}
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}

func TestNumericOnly_Optional_SetToValue(t *testing.T) {
	t.Parallel()
	v := int32(-42)
	n := integration.NumericOnly{A: 1, H: &v}
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}

func TestNumericOnly_OptionalBool_Unset(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.NumericOnly](t, integration.NumericOnly{})
}

func TestNumericOnly_OptionalBool_SetFalse(t *testing.T) {
	t.Parallel()
	f := false
	codec.AssertRoundtrip[integration.NumericOnly](t, integration.NumericOnly{I: &f})
}

func TestNumericOnly_OptionalBool_SetTrue(t *testing.T) {
	t.Parallel()
	v := true
	codec.AssertRoundtrip[integration.NumericOnly](t, integration.NumericOnly{I: &v})
}

func TestNumericOnly_OptionalFixed64_SetMin(t *testing.T) {
	t.Parallel()
	v := integration.Fixed64(-1 << 62)
	codec.AssertRoundtrip[integration.NumericOnly](t, integration.NumericOnly{J: &v})
}

func TestNumericOnly_OptionalFixed64_Unset(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.NumericOnly](t, integration.NumericOnly{})
}

func TestNumericOnly_Zigzag_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := integration.NumericOnly{
			F: rapid.Int32().Draw(t, "f"),
			G: rapid.Int64().Draw(t, "g"),
		}
		codec.AssertRoundtrip[integration.NumericOnly](t, n)
	})
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

// BenchmarkNumericOnly_PooledUnmarshal measures the warm-path, pooled-receiver
// cost of UnmarshalCodec. After the first iteration primes H/I/J pointers,
// subsequent iterations reuse those *T slots via the seenOptional-bitmap
// pooling path, driving steady-state allocs to zero.
func BenchmarkNumericOnly_PooledUnmarshal(b *testing.B) {
	s := sampleNumericOnly()
	data, _ := s.MarshalCodec()
	var got integration.NumericOnly
	// Prime: first unmarshal allocates the three optional pointers so the
	// measured loop exercises the pooled-reuse path only.
	if err := got.UnmarshalCodec(data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = got.UnmarshalCodec(data)
	}
}

func TestNumericOnly_PointerPooling_AcrossResets(t *testing.T) {
	t.Parallel()
	s := sampleNumericOnly() // has H, I, J set
	buf, _ := s.MarshalCodec()

	var receiver integration.NumericOnly
	// First unmarshal allocates H, I, J pointers.
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	hPtr := receiver.H
	iPtr := receiver.I
	jPtr := receiver.J
	if hPtr == nil || iPtr == nil || jPtr == nil {
		t.Fatal("expected H, I, J non-nil after first unmarshal")
	}

	// Second unmarshal should reuse the same *T heap slots.
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if receiver.H != hPtr {
		t.Errorf("H pointer changed: pooling failed (got %p, want %p)", receiver.H, hPtr)
	}
	if receiver.I != iPtr {
		t.Errorf("I pointer changed: pooling failed (got %p, want %p)", receiver.I, iPtr)
	}
	if receiver.J != jPtr {
		t.Errorf("J pointer changed: pooling failed (got %p, want %p)", receiver.J, jPtr)
	}
}

func TestNumericOnly_PointerPooling_AbsentFieldsNilOut(t *testing.T) {
	t.Parallel()
	// Prime the receiver with all optional fields set.
	s := sampleNumericOnly()
	buf, _ := s.MarshalCodec()
	var receiver integration.NumericOnly
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if receiver.H == nil {
		t.Fatal("expected H non-nil after first unmarshal")
	}

	// Marshal a NumericOnly WITHOUT H/I/J set.
	minimal := integration.NumericOnly{A: 99}
	buf, _ = minimal.MarshalCodec()

	// Unmarshal into the primed receiver.
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}

	// Optional fields absent from the wire must be nilled out.
	if receiver.H != nil {
		t.Errorf("H should be nil after unmarshaling a message without it, got %v", *receiver.H)
	}
	if receiver.I != nil {
		t.Errorf("I should be nil, got %v", *receiver.I)
	}
	if receiver.J != nil {
		t.Errorf("J should be nil, got %v", *receiver.J)
	}
	if receiver.A != 99 {
		t.Errorf("A should be 99, got %d", receiver.A)
	}
}

func BenchmarkPackedZigzag_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.PackedZigzag](b, samplePackedZigzag())
}

func BenchmarkContainer_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.Container](b, sampleContainer())
}

func BenchmarkValueContainer_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.ValueContainer](b, sampleValueContainer())
}

func BenchmarkTree_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.Tree](b, sampleTree())
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

func FuzzPackedZigzag_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.PackedZigzag](f, samplePackedZigzag(), integration.PackedZigzag{})
}

func FuzzContainer_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.Container](f, sampleContainer(), integration.Container{}, integration.Container{Name: "empty"})
}

func FuzzValueContainer_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.ValueContainer](f, sampleValueContainer(), integration.ValueContainer{})
}

func FuzzTree_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.Tree](f, sampleTree(), integration.Tree{})
}

func TestTree_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Bounded-depth recursive tree generator.
		var gen func(depth int) *integration.Tree
		gen = func(depth int) *integration.Tree {
			n := integration.Tree{Label: rapid.String().Draw(t, "l")}
			if depth < 3 {
				nc := rapid.IntRange(0, 2).Draw(t, "nc")
				for range nc {
					n.Children = append(n.Children, gen(depth+1))
				}
			}
			return &n
		}
		codec.AssertRoundtrip[integration.Tree](t, *gen(0))
	})
}

func TestContainer_Roundtrip_WithInner(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.Container](t, integration.Container{
		Name:  "alpha",
		Inner: &integration.Inner{Label: "x", Count: 7},
	})
}

func TestContainer_Roundtrip_InnerNil(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.Container](t, integration.Container{Name: "alpha"})
}

func TestContainer_Roundtrip_RepeatedChildren(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.Container](t, integration.Container{
		Name: "alpha",
		Children: []*integration.Inner{
			{Label: "x"}, {Label: "y", Count: 99},
		},
	})
}

func TestContainer_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nChildren := rapid.IntRange(0, 3).Draw(t, "n")
		var children []*integration.Inner
		if nChildren > 0 {
			children = make([]*integration.Inner, nChildren)
			for i := range nChildren {
				children[i] = &integration.Inner{
					Label: rapid.String().Draw(t, "l"),
					Count: rapid.Int64().Draw(t, "c"),
				}
			}
		}
		// Singular *Inner: when present, force at least one non-default field.
		// Phase 4.10 normalizes proto3 "all-defaults message" to absent on the
		// wire (SizeCodec==0 ⇒ skip), so a present-but-empty &Inner{} would
		// roundtrip back as nil and trip DeepEqual.
		var inner *integration.Inner
		if rapid.Bool().Draw(t, "hasInner") {
			label := rapid.String().Draw(t, "il")
			count := rapid.Int64().Draw(t, "ic")
			if label == "" && count == 0 {
				count = 1 // ensure non-default content
			}
			inner = &integration.Inner{Label: label, Count: count}
		}
		codec.AssertRoundtrip[integration.Container](t, integration.Container{
			Name:     rapid.String().Draw(t, "name"),
			Inner:    inner,
			Children: children,
		})
	})
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

func TestPackedZigzag_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n32 := rapid.IntRange(0, 8).Draw(t, "n32")
		var v32 []int32
		if n32 > 0 {
			v32 = make([]int32, n32)
			for i := range v32 {
				v32[i] = rapid.Int32().Draw(t, "v32")
			}
		}
		n64 := rapid.IntRange(0, 8).Draw(t, "n64")
		var v64 []int64
		if n64 > 0 {
			v64 = make([]int64, n64)
			for i := range v64 {
				v64[i] = rapid.Int64().Draw(t, "v64")
			}
		}
		codec.AssertRoundtrip[integration.PackedZigzag](t, integration.PackedZigzag{
			Values32: v32,
			Values64: v64,
		})
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

func TestFixture_Roundtrip_NilSlicesStayNil(t *testing.T) {
	t.Parallel()
	// Regression: a sparse Fixture (only Timestamp set, Tags/Data nil) must
	// roundtrip back to a value that reflect.DeepEqual's equal to itself.
	// UnmarshalCodec must not materialize empty-but-non-nil slices from a
	// wire stream that carried no length-delimited records for those fields.
	f := integration.Fixture{Timestamp: 1}
	codec.AssertRoundtrip[integration.Fixture](t, f)
}

func TestFixture_Roundtrip_TagsWithEmptyString(t *testing.T) {
	t.Parallel()
	// Regression: empty strings inside a repeated string field encode as
	// zero-length LEN records. A decoder that treats "no bytes" as "no
	// element" would silently shorten the slice on unmarshal.
	f := integration.Fixture{Tags: []string{"a", "", "c"}}
	codec.AssertRoundtrip[integration.Fixture](t, f)
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

func TestFixture_WrongWireType_IncludesFieldNameAndNumber(t *testing.T) {
	t.Parallel()
	// field 9 (Tags, repeated string, wire type 2) sent as varint
	data := []byte{0x48, 0x00}
	var f integration.Fixture
	err := f.UnmarshalCodec(data)
	if !stderrors.Is(err, codec.ErrInvalidWireType) {
		t.Fatalf("expected ErrInvalidWireType, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "Tags") || !strings.Contains(msg, "(9)") {
		t.Fatalf("error should include field name Tags and number (9), got: %v", err)
	}
}

func TestFixture_MarshalToCodec_ShortBuffer(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	size := f.SizeCodec()
	short := make([]byte, size-1)
	_, err := f.MarshalToCodec(short)
	if !stderrors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("expected ErrBufferTooShort, got %v", err)
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
	// Verifies repeated slice is reset (not appended to) on unmarshal into a used receiver.
	t.Parallel()
	f := sampleFixture()
	buf, _ := f.MarshalCodec()
	old := integration.Fixture{ID: "old", Tags: []string{"x", "y", "z", "w", "q"}}
	if err := old.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f, old) {
		t.Fatalf("pooled reuse mismatch:\n  want: %+v\n  got:  %+v", f, old)
	}
}

func TestFixture_UnmarshalCodec_ClearsStalePopulatedScalars(t *testing.T) {
	t.Parallel()
	// Marshal a Fixture with only ID set; unmarshal into a populated receiver.
	// Expect the receiver's unset scalar fields (Kind, Score, etc.) to be zero.
	minimal := integration.Fixture{ID: "new"}
	buf, err := minimal.MarshalCodec()
	if err != nil {
		t.Fatal(err)
	}
	receiver := integration.Fixture{
		ID: "stale", Kind: 99, Score: 7, Sequence: 5, Enabled: true,
		Timestamp: 123, Status: integration.StatusRunning,
	}
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if receiver.Kind != 0 || receiver.Score != 0 || receiver.Sequence != 0 ||
		receiver.Enabled || receiver.Timestamp != 0 || receiver.Status != 0 {
		t.Fatalf("stale scalars survived unmarshal: %+v", receiver)
	}
	if receiver.ID != "new" {
		t.Fatalf("new value not applied: ID=%q", receiver.ID)
	}
}

// ---------------------------------------------------------------------------
// Well-known types: Timestamp + Duration
// ---------------------------------------------------------------------------

func sampleTimeHolder() integration.TimeHolder {
	return integration.TimeHolder{
		CreatedAt: time.Unix(1713400000, 500_000_000).UTC(),
		Timeout:   7*time.Second + 123*time.Nanosecond,
	}
}

func TestTimeHolder_Codec(t *testing.T) {
	codec.RunTestSuite[integration.TimeHolder](t, sampleTimeHolder())
}

func BenchmarkTimeHolder_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.TimeHolder](b, sampleTimeHolder())
}

func FuzzTimeHolder_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.TimeHolder](f, sampleTimeHolder(), integration.TimeHolder{})
}

func TestTimeHolder_ZeroRoundtrip(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.TimeHolder](t, integration.TimeHolder{})
}

func TestTimeHolder_NegativeDuration(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.TimeHolder](t, integration.TimeHolder{Timeout: -3 * time.Second})
}

func TestTimeHolder_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// secs starts at 1: time.Unix(0, 0).UTC() and time.Time{} encode to
		// identical wire bytes (empty body) but differ by struct identity,
		// so DeepEqual distinguishes them. The zero case is covered by
		// TestTimeHolder_ZeroRoundtrip.
		secs := rapid.Int64Range(1, 253402300799).Draw(t, "secs")
		nanos := rapid.Int32Range(0, 999_999_999).Draw(t, "nanos")
		ds := rapid.Int64Range(-(1<<40), 1<<40).Draw(t, "ds")
		dn := rapid.Int32Range(-999_999_999, 999_999_999).Draw(t, "dn")
		codec.AssertRoundtrip[integration.TimeHolder](t, integration.TimeHolder{
			CreatedAt: time.Unix(secs, int64(nanos)).UTC(),
			Timeout:   time.Duration(ds)*time.Second + time.Duration(dn),
		})
	})
}

// ---------------------------------------------------------------------------
// Phase 4.8: Tier-1 deep pooling
// ---------------------------------------------------------------------------

// TestContainer_MessagePointerPooling_AcrossResets verifies that a singular
// nested-message pointer field (*Inner) is reused across unmarshals into the
// same receiver. This exercises the seenOptional-bitmap pooling path extended
// to cover message-kind pointers in Phase 4.8.
func TestContainer_MessagePointerPooling_AcrossResets(t *testing.T) {
	t.Parallel()
	s := sampleContainer() // has Inner non-nil
	buf, _ := s.MarshalCodec()

	var receiver integration.Container
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	innerPtr := receiver.Inner
	if innerPtr == nil {
		t.Fatal("expected Inner non-nil after first unmarshal")
	}
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if receiver.Inner != innerPtr {
		t.Errorf("Inner pointer changed: pooling failed (want %p, got %p)", innerPtr, receiver.Inner)
	}
}

// TestContainer_MessagePointerPooling_AbsentFieldNilsOut verifies that a
// pooled *Inner field correctly nils out when the subsequent wire payload
// does not include it. Without this the pooling path would leak stale data.
func TestContainer_MessagePointerPooling_AbsentFieldNilsOut(t *testing.T) {
	t.Parallel()
	s := sampleContainer()
	buf, _ := s.MarshalCodec()
	var receiver integration.Container
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if receiver.Inner == nil {
		t.Fatal("expected Inner non-nil after first unmarshal")
	}

	// Marshal a Container without Inner.
	minimal := integration.Container{Name: "alpha"}
	buf, _ = minimal.MarshalCodec()
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}

	if receiver.Inner != nil {
		t.Errorf("Inner should be nil after unmarshaling a message without it, got %v", receiver.Inner)
	}
	if receiver.Name != "alpha" {
		t.Errorf("Name should be alpha, got %q", receiver.Name)
	}
}

// TestContainer_CursorReuse_AcrossResets verifies that the keep_capacity +
// use_pointer repeated-message field reuses its *Inner slots across unmarshals.
// The second pass must not allocate fresh *Inner values when the backing
// slice still has capacity from the first pass.
func TestContainer_CursorReuse_AcrossResets(t *testing.T) {
	t.Parallel()
	s := sampleContainer() // 3 children
	buf, _ := s.MarshalCodec()

	var receiver integration.Container
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	// Capture the pointers from the first pass.
	childPtrs := make([]*integration.Inner, len(receiver.Children))
	copy(childPtrs, receiver.Children)

	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	// Second pass should reuse the same *Inner slots.
	if len(receiver.Children) != len(childPtrs) {
		t.Fatalf("child count changed: want %d got %d", len(childPtrs), len(receiver.Children))
	}
	for i := range receiver.Children {
		if receiver.Children[i] != childPtrs[i] {
			t.Errorf("Children[%d] pointer changed: want %p got %p (cursor reuse failed)", i, childPtrs[i], receiver.Children[i])
		}
	}
}

// BenchmarkContainer_PooledUnmarshal measures warm-path UnmarshalCodec cost
// once Inner and the Children []*Inner slots have been primed on the receiver.
// Phase 4.8 should drive the three slice-element pointer allocs and the Inner
// pointer alloc to zero; steady-state remains the name string alloc only.
func BenchmarkContainer_PooledUnmarshal(b *testing.B) {
	s := sampleContainer()
	data, _ := s.MarshalCodec()
	var got integration.Container
	// Prime: first unmarshal allocates Inner and the three *Inner children.
	if err := got.UnmarshalCodec(data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = got.UnmarshalCodec(data)
	}
}

// BenchmarkTree_PooledUnmarshal complements the Container bench: Tree uses
// self-reference, so Children is []*Tree and the cursor-reuse path does not
// apply (no keep_capacity annotation). Included to document the baseline for
// non-keep_capacity repeated *T pooling behavior.
func BenchmarkTree_PooledUnmarshal(b *testing.B) {
	s := sampleTree()
	data, _ := s.MarshalCodec()
	var got integration.Tree
	if err := got.UnmarshalCodec(data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = got.UnmarshalCodec(data)
	}
}

// BenchmarkValueContainer_PooledUnmarshal measures the warm-path cost of
// decoding a message with a value-inlined nested message and a value-slice of
// nested messages ([]Inner). Phase 4.10 cursor-reuse on the value slice and
// recursive ResetCodec drive steady-state allocs to ~1 (the top-level slab).
func BenchmarkValueContainer_PooledUnmarshal(b *testing.B) {
	s := sampleValueContainer()
	data, _ := s.MarshalCodec()
	var got integration.ValueContainer
	if err := got.UnmarshalCodec(data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = got.UnmarshalCodec(data)
	}
}

// BenchmarkPackedZigzag_PooledUnmarshal measures the warm-path cost of
// decoding a message containing only packed-repeated scalars. With the
// Phase 4.10 [:0] reset preserving the slice backing arrays, steady-state
// should be 0 allocs (no nested messages, no strings — no slab needed).
func BenchmarkPackedZigzag_PooledUnmarshal(b *testing.B) {
	s := samplePackedZigzag()
	data, _ := s.MarshalCodec()
	var got integration.PackedZigzag
	if err := got.UnmarshalCodec(data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = got.UnmarshalCodec(data)
	}
}

// BenchmarkMapHolder_PooledUnmarshal measures the warm-path cost of decoding
// a message with two map[string]X fields. The clear(m) reset preserves bucket
// storage, but each entry's key/value strings are still freshly assigned per
// call — map-entry string allocs are an unavoidable cost of the decode path.
func BenchmarkMapHolder_PooledUnmarshal(b *testing.B) {
	s := sampleMapHolder()
	data, _ := s.MarshalCodec()
	var got integration.MapHolder
	if err := got.UnmarshalCodec(data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = got.UnmarshalCodec(data)
	}
}

// TestContainer_PreScanCapacityHint verifies the cold-path pre-scan eliminates
// repeated-slice append-growth reallocs. Without the pre-scan, decoding 20
// children grows the backing slice log2(20) times; with it, a single make
// allocates the correct capacity up front. Does not mark Parallel because
// testing.AllocsPerRun panics inside a t.Parallel() test.
func TestContainer_PreScanCapacityHint(t *testing.T) {
	many := integration.Container{Name: "many"}
	for i := range 20 {
		many.Children = append(many.Children, &integration.Inner{Label: "x", Count: int64(i)})
	}
	buf, _ := many.MarshalCodec()

	allocs := testing.AllocsPerRun(10, func() {
		var got integration.Container
		_ = got.UnmarshalCodec(buf)
	})
	t.Logf("cold-path allocs for 20-child Container: %.1f", allocs)
	// Observed breakdown with pre-scan enabled: 20 children contribute
	// two allocs each (*Inner heap slot + the child's dataStr slab for
	// Label), plus one slice make for Children (pre-sized to 20) and one
	// for the outer dataStr. Without the pre-scan, Children grows through
	// ~5 append reallocations (1,2,4,8,16,32) as additional allocs. Measured
	// delta: 47 -> 42 allocs. Budget allows a small amount of headroom.
	if allocs > 44 {
		t.Errorf("too many allocs: %.1f (expected <= 44 with pre-scan; baseline without pre-scan is 47)", allocs)
	}
}

// ---------------------------------------------------------------------------
// BytesPool: keep_capacity = true on a []byte field (Phase 4.8-D)
// ---------------------------------------------------------------------------

func TestBytesPool_Codec(t *testing.T) {
	codec.RunTestSuite[integration.BytesPool](t, integration.BytesPool{Payload: []byte{1, 2, 3}})
}

func BenchmarkBytesPool_Codec(b *testing.B) {
	codec.RunBenchSuite[integration.BytesPool](b, integration.BytesPool{Payload: []byte{0xde, 0xad, 0xbe, 0xef}})
}

// ---------------------------------------------------------------------------
// Coverage suite (Phase 5 Chunk B): RunCoverageSuite per fixture.
// ---------------------------------------------------------------------------

func TestFixture_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.Fixture](t, sampleFixture(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0},  // string field as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 2},  // uint32 as len-delim
		codec.WireMismatch{FieldNum: 8, WrongWireType: 0},  // bytes (fixed_len) as varint
		codec.WireMismatch{FieldNum: 9, WrongWireType: 0},  // repeated string as varint
		codec.WireMismatch{FieldNum: 10, WrongWireType: 0}, // bytes as varint
	)
}

func TestPatch_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.Patch](t, samplePatchText(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 2}, // uint32 as len-delim
		codec.WireMismatch{FieldNum: 5, WrongWireType: 0}, // string as varint
		codec.WireMismatch{FieldNum: 7, WrongWireType: 0}, // sfixed64 as varint
		codec.WireMismatch{FieldNum: 8, WrongWireType: 0}, // bytes as varint
	)
}

func TestEvidence_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.Evidence](t, sampleEvidence(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 2},  // uint32 as len-delim
		codec.WireMismatch{FieldNum: 4, WrongWireType: 0},  // string as varint
		codec.WireMismatch{FieldNum: 9, WrongWireType: 2},  // int64 as len-delim
		codec.WireMismatch{FieldNum: 10, WrongWireType: 0}, // bytes as varint
		codec.WireMismatch{FieldNum: 11, WrongWireType: 0}, // repeated string as varint
	)
}

func TestMinimal_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.Minimal](t, sampleMinimal(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // string as varint
	)
}

func TestNumericOnly_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.NumericOnly](t, sampleNumericOnly(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 2},  // uint32 as len-delim
		codec.WireMismatch{FieldNum: 4, WrongWireType: 0},  // sfixed64 as varint
		codec.WireMismatch{FieldNum: 6, WrongWireType: 2},  // sint32 as len-delim
		codec.WireMismatch{FieldNum: 8, WrongWireType: 2},  // optional int32 as len-delim
		codec.WireMismatch{FieldNum: 10, WrongWireType: 0}, // optional sfixed64 as varint
	)
}

func TestPackedZigzag_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.PackedZigzag](t, samplePackedZigzag(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 1}, // packed sint32 as fixed64
		codec.WireMismatch{FieldNum: 2, WrongWireType: 5}, // packed sint64 as fixed32
	)
}

func TestInner_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.Inner](t, sampleInner(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // string as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 2}, // int64 as len-delim
	)
}

func TestContainer_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.Container](t, sampleContainer(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // string as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 0}, // nested message as varint
		codec.WireMismatch{FieldNum: 3, WrongWireType: 0}, // repeated message as varint
	)
}

func TestValueContainer_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.ValueContainer](t, sampleValueContainer(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // string as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 0}, // value-inlined message as varint
		codec.WireMismatch{FieldNum: 3, WrongWireType: 0}, // value-slice message as varint
	)
}

func TestTree_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.Tree](t, sampleTree(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // string as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 0}, // self-referential repeated message as varint
	)
}

func TestMapHolder_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.MapHolder](t, sampleMapHolder(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // map as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 0}, // map as varint
	)
}

func TestTimeHolder_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.TimeHolder](t, sampleTimeHolder(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // Timestamp as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 0}, // Duration as varint
	)
}

func TestBytesPool_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.BytesPool](t, integration.BytesPool{Payload: []byte{1, 2, 3}}, 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // bytes as varint
	)
}

// TestAll_Coverage_ShortInMiddle truncates a valid marshal at every offset
// and tries to unmarshal each truncated buffer. The unmarshal results don't
// matter — we exercise short-buffer error branches throughout the decoder
// to push UnmarshalCodecInternal coverage above 95%.
func TestAll_Coverage_ShortInMiddle(t *testing.T) {
	t.Parallel()
	type tc struct {
		name string
		buf  []byte
	}
	build := func(name string, m codec.Marshaler) tc {
		buf, err := m.MarshalCodec()
		if err != nil {
			t.Fatalf("%s MarshalCodec: %v", name, err)
		}
		return tc{name, buf}
	}
	f := sampleFixture()
	pt := samplePatchText()
	pf := samplePatchFixed64()
	pb := samplePatchBlob()
	e := sampleEvidence()
	n := sampleNumericOnly()
	pz := samplePackedZigzag()
	in := sampleInner()
	c := sampleContainer()
	vc := sampleValueContainer()
	tr := sampleTree()
	mh := sampleMapHolder()
	th := sampleTimeHolder()
	bp := integration.BytesPool{Payload: []byte{1, 2, 3, 4, 5, 6, 7, 8}}
	mn := sampleMinimal()
	cases := []tc{
		build("Fixture", &f),
		build("PatchText", &pt),
		build("PatchFixed64", &pf),
		build("PatchBlob", &pb),
		build("Evidence", &e),
		build("NumericOnly", &n),
		build("PackedZigzag", &pz),
		build("Inner", &in),
		build("Container", &c),
		build("ValueContainer", &vc),
		build("Tree", &tr),
		build("MapHolder", &mh),
		build("TimeHolder", &th),
		build("BytesPool", &bp),
		build("Minimal", &mn),
	}
	for _, tt := range cases {
		for i := 1; i < len(tt.buf); i++ {
			truncated := tt.buf[:i]
			switch tt.name {
			case "Fixture":
				var x integration.Fixture
				_ = x.UnmarshalCodec(truncated)
			case "PatchText", "PatchFixed64", "PatchBlob":
				var x integration.Patch
				_ = x.UnmarshalCodec(truncated)
			case "Evidence":
				var x integration.Evidence
				_ = x.UnmarshalCodec(truncated)
			case "NumericOnly":
				var x integration.NumericOnly
				_ = x.UnmarshalCodec(truncated)
			case "PackedZigzag":
				var x integration.PackedZigzag
				_ = x.UnmarshalCodec(truncated)
			case "Inner":
				var x integration.Inner
				_ = x.UnmarshalCodec(truncated)
			case "Container":
				var x integration.Container
				_ = x.UnmarshalCodec(truncated)
			case "ValueContainer":
				var x integration.ValueContainer
				_ = x.UnmarshalCodec(truncated)
			case "Tree":
				var x integration.Tree
				_ = x.UnmarshalCodec(truncated)
			case "MapHolder":
				var x integration.MapHolder
				_ = x.UnmarshalCodec(truncated)
			case "TimeHolder":
				var x integration.TimeHolder
				_ = x.UnmarshalCodec(truncated)
			case "BytesPool":
				var x integration.BytesPool
				_ = x.UnmarshalCodec(truncated)
			case "Minimal":
				var x integration.Minimal
				_ = x.UnmarshalCodec(truncated)
			}
		}
	}
}

// TestBytesPool_KeepCapacity_PooledReuse verifies that a bytes field annotated
// with keep_capacity reuses its backing array on warm-path unmarshal. The
// generated decoder emits `append(m.Payload[:0], data...)` which retains
// capacity when cap is sufficient, so the warm path must not allocate. Does
// not mark Parallel because testing.AllocsPerRun panics inside a t.Parallel()
// test.
func TestBytesPool_KeepCapacity_PooledReuse(t *testing.T) {
	s := integration.BytesPool{Payload: []byte{0xde, 0xad, 0xbe, 0xef}}
	buf, _ := s.MarshalCodec()

	var receiver integration.BytesPool
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	firstCap := cap(receiver.Payload)
	if firstCap < len(s.Payload) {
		t.Fatalf("first unmarshal did not allocate sufficient capacity: %d", firstCap)
	}

	allocs := testing.AllocsPerRun(50, func() {
		_ = receiver.UnmarshalCodec(buf)
	})
	// Warm path: backing array is reused, so the append(m.Payload[:0], ...)
	// path stays in-place. Expect zero allocs.
	if allocs > 0 {
		t.Errorf("expected 0 allocs on warm path, got %.1f", allocs)
	}
	if cap(receiver.Payload) != firstCap {
		t.Errorf("capacity changed: was %d now %d", firstCap, cap(receiver.Payload))
	}
}
