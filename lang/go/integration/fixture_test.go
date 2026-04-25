// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec/codectest"
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

// patchAllFields populates every declared field of Patch so the
// AllFieldsWireTypeMismatch helper observes all field tags.
func patchAllFields() integration.Patch {
	return integration.Patch{
		Kind: integration.PatchKindText, VertexID: 7, Sequence: 1001,
		Source:  integration.SourceInference,
		TextVal: "t", IntVal: 42, Fixed64Val: 100, BlobRef: digest(0xAB),
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

func sampleInner() integration.Inner { return integration.Inner{Label: "inner", Count: 42} }

func sampleMapHolder() integration.MapHolder {
	return integration.MapHolder{
		Attrs:  map[string]string{"region": "eu-west", "tier": "premium"},
		Counts: map[string]int64{"retries": 3, "errors": 0},
	}
}

func sampleTimeHolder() integration.TimeHolder {
	return integration.TimeHolder{
		CreatedAt: time.Unix(1713400000, 500_000_000).UTC(),
		Timeout:   7*time.Second + 123*time.Nanosecond,
	}
}

func sampleBytesPool() integration.BytesPool {
	return integration.BytesPool{Payload: []byte{1, 2, 3}}
}

func sampleOneofPayloadText() integration.OneofPayload {
	return integration.OneofPayload{
		Label: "banner",
		Kind:  integration.OneofPayloadKindText,
		Text:  "hello",
	}
}

func sampleOneofPayloadNumber() integration.OneofPayload {
	return integration.OneofPayload{
		Label:  "counter",
		Kind:   integration.OneofPayloadKindNumber,
		Number: 42,
	}
}

func sampleOneofPayloadNested() integration.OneofPayload {
	return integration.OneofPayload{
		Label:  "wrapper",
		Kind:   integration.OneofPayloadKindNested,
		Nested: integration.Inner{Label: "inside", Count: 7},
	}
}

func sampleOneofPayloadBlob() integration.OneofPayload {
	return integration.OneofPayload{
		Label: "payload",
		Kind:  integration.OneofPayloadKindBlob,
		Blob:  []byte{0xca, 0xfe, 0xba, 0xbe},
	}
}

func sampleOneofPayloadAmount() integration.OneofPayload {
	return integration.OneofPayload{
		Label:  "ledger",
		Kind:   integration.OneofPayloadKindAmount,
		Amount: 100_000,
	}
}

// numericWithFalseBool covers the `else` branch of `if *m.I { buf[n]=1 }`
// by marshaling a NumericOnly whose optional *bool I is explicitly false.
func numericWithFalseBool() integration.NumericOnly {
	s := sampleNumericOnly()
	f := false
	s.I = &f
	return s
}

// ---------------------------------------------------------------------------
// Rapid generators for PBT subtests
// ---------------------------------------------------------------------------

func genMapHolder(rt *rapid.T) integration.MapHolder {
	nA := rapid.IntRange(0, 4).Draw(rt, "nA")
	var attrs map[string]string
	if nA > 0 {
		attrs = make(map[string]string, nA)
		for range nA {
			attrs[rapid.String().Draw(rt, "k")] = rapid.String().Draw(rt, "v")
		}
	}
	nC := rapid.IntRange(0, 4).Draw(rt, "nC")
	var counts map[string]int64
	if nC > 0 {
		counts = make(map[string]int64, nC)
		for range nC {
			counts[rapid.String().Draw(rt, "ck")] = rapid.Int64().Draw(rt, "cv")
		}
	}
	return integration.MapHolder{Attrs: attrs, Counts: counts}
}

func genNumericOnly(rt *rapid.T) integration.NumericOnly {
	return integration.NumericOnly{
		A: rapid.Uint32().Draw(rt, "a"),
		B: rapid.Uint64().Draw(rt, "b"),
		C: rapid.Int64().Draw(rt, "c"),
		D: integration.Fixed64(rapid.Int64().Draw(rt, "d")),
		E: rapid.Bool().Draw(rt, "e"),
		F: rapid.Int32().Draw(rt, "f"),
		G: rapid.Int64().Draw(rt, "g"),
	}
}

// ---------------------------------------------------------------------------
// Grower helpers — used by Spec.Grower to exercise warm-path slice growth
// ---------------------------------------------------------------------------

func ptrFixtureGrower() *integration.Fixture {
	g := sampleFixture()
	g.Tags = append(g.Tags, "delta", "epsilon")
	return &g
}

func ptrEvidenceGrower() *integration.Evidence {
	g := sampleEvidence()
	g.Jurisdictions = append(g.Jurisdictions, "JP", "AU")
	return &g
}

func ptrPackedZigzagGrower() *integration.PackedZigzag {
	g := samplePackedZigzag()
	g.Values32 = append(g.Values32, 7, 8, 9)
	g.Values64 = append(g.Values64, 100, 200)
	return &g
}

func ptrContainerGrower() *integration.Container {
	g := sampleContainer()
	g.Children = append(g.Children,
		&integration.Inner{Label: "c4", Count: 4},
		&integration.Inner{Label: "c5", Count: 5})
	return &g
}

func ptrContainerNilElement() *integration.Container {
	s := integration.Container{
		Name: "with-nil",
		Children: []*integration.Inner{
			{Label: "first", Count: 1},
			nil,
			{Label: "third", Count: 3},
		},
	}
	return &s
}

func ptrValueContainerGrower() *integration.ValueContainer {
	g := sampleValueContainer()
	g.Items = append(g.Items,
		integration.Inner{Label: "fourth", Count: 4},
		integration.Inner{Label: "fifth", Count: 5})
	return &g
}

func ptrTreeGrower() *integration.Tree {
	g := sampleTree()
	g.Children = append(g.Children, &integration.Tree{Label: "c"})
	return &g
}

func ptrTreeNilElement() *integration.Tree {
	s := integration.Tree{
		Label: "root",
		Children: []*integration.Tree{
			{Label: "a"},
			nil,
			{Label: "b"},
		},
	}
	return &s
}

func ptrMapHolderGrower() *integration.MapHolder {
	return &integration.MapHolder{
		Attrs:  map[string]string{"region": "eu-west", "tier": "premium", "extra1": "a", "extra2": "b"},
		Counts: map[string]int64{"retries": 3, "errors": 0, "hits": 99},
	}
}

// ---------------------------------------------------------------------------
// Per-fixture sections.
//
// Each section declares a Spec then exposes one Test<T>, one Benchmark<T>,
// and one Fuzz<T>_Codec. Subtests hang off the consolidated parents so a
// reader sees every guarantee for a given type in one place. Fuzz keeps the
// trailing `_Codec` because committed corpora live at
// testdata/fuzz/Fuzz<T>_Codec/ and renaming would orphan them.
// ---------------------------------------------------------------------------

// === Fixture =================================================================

var specFixture = codectest.Spec[integration.Fixture]{
	Sample:              sampleFixture(),
	Grower:              ptrFixtureGrower(),
	ScalarVarintFields:  []int32{2, 3, 4, 5, 6, 7},                       // Kind, Status, Score, Sequence, Enabled, Timestamp
	FixedLenBytesFields: []codectest.FixedLenField{{Num: 8, Length: 32}}, // Ref (Digest, fixed_len=32)
}

func TestFixture(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.Fixture](t, specFixture)
	})
}

func BenchmarkFixture(b *testing.B) {
	codectest.RunBenchSuite[integration.Fixture](b, specFixture)
	b.Run("MarshalCodec", func(b *testing.B) {
		s := sampleFixture()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = s.MarshalCodec()
		}
	})
}

func FuzzFixture_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.Fixture](f, specFixture)
}

// === Patch ===================================================================

var specPatch = codectest.Spec[integration.Patch]{
	Sample:              samplePatchText(),
	Variants:            []integration.Patch{patchAllFields(), samplePatchFixed64(), samplePatchBlob()},
	ScalarVarintFields:  []int32{1, 2, 3, 4, 6},                          // Kind, VertexID, Sequence, Source, IntVal
	Fixed64Fields:       []int32{7},                                      // Fixed64Val (sfixed64)
	FixedLenBytesFields: []codectest.FixedLenField{{Num: 8, Length: 32}}, // BlobRef (Digest, fixed_len=32)
}

func TestPatch(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.Patch](t, specPatch)
	})
}

func BenchmarkPatch(b *testing.B) {
	codectest.RunBenchSuite[integration.Patch](b, specPatch)
}

func FuzzPatch_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.Patch](f, specPatch)
}

// === Evidence ================================================================

var specEvidence = codectest.Spec[integration.Evidence]{
	Sample:              sampleEvidence(),
	Grower:              ptrEvidenceGrower(),
	ScalarVarintFields:  []int32{1, 2, 3, 9},                              // Kind, Durability, Access, TimestampMs
	FixedLenBytesFields: []codectest.FixedLenField{{Num: 10, Length: 32}}, // PayloadRef
}

func TestEvidence(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.Evidence](t, specEvidence)
	})
}

func BenchmarkEvidence(b *testing.B) {
	codectest.RunBenchSuite[integration.Evidence](b, specEvidence)
}

func FuzzEvidence_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.Evidence](f, specEvidence)
}

// === Minimal =================================================================

var specMinimal = codectest.Spec[integration.Minimal]{Sample: sampleMinimal()}

func TestMinimal(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.Minimal](t, specMinimal)
	})
}

func BenchmarkMinimal(b *testing.B) {
	codectest.RunBenchSuite[integration.Minimal](b, specMinimal)
	b.Run("MarshalCodec", func(b *testing.B) {
		s := sampleMinimal()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = s.MarshalCodec()
		}
	})
}

func FuzzMinimal_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.Minimal](f, specMinimal)
}

// === NumericOnly =============================================================

var specNumericOnly = codectest.Spec[integration.NumericOnly]{
	Sample:             sampleNumericOnly(),
	Variants:           []integration.NumericOnly{numericWithFalseBool()},
	Generator:          genNumericOnly,
	ScalarVarintFields: []int32{1, 2, 3, 5, 6, 7, 8, 9}, // A,B,C,E,F,G,H,I (all varint-wire)
	Fixed64Fields:      []int32{4, 10},                  // D (sfixed64), J (optional sfixed64)
}

func TestNumericOnly(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.NumericOnly](t, specNumericOnly)
	})
	t.Run("PointerPooling reuses optional-pointer slots across resets", func(t *testing.T) {
		t.Parallel()
		// Verifies the seen-bitmap pointer-pooling path: once H/I/J have
		// been allocated by the first unmarshal, subsequent unmarshals
		// into the same receiver must reuse the existing *int32 / *bool
		// / *Fixed64 slots instead of allocating fresh.
		s := sampleNumericOnly()
		data, _ := s.MarshalCodec()
		var got integration.NumericOnly
		if err := got.UnmarshalCodec(data); err != nil {
			t.Fatal(err)
		}
		primedH, primedI, primedJ := got.H, got.I, got.J
		for range 5 {
			if err := got.UnmarshalCodec(data); err != nil {
				t.Fatal(err)
			}
			if got.H != primedH || got.I != primedI || got.J != primedJ {
				t.Errorf("pointer slot reallocated across reset")
			}
		}
	})
}

func BenchmarkNumericOnly(b *testing.B) {
	codectest.RunBenchSuite[integration.NumericOnly](b, specNumericOnly)
	b.Run("PooledUnmarshal", func(b *testing.B) {
		s := sampleNumericOnly()
		data, _ := s.MarshalCodec()
		var got integration.NumericOnly
		if err := got.UnmarshalCodec(data); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = got.UnmarshalCodec(data)
		}
	})
	b.Run("MarshalCodec", func(b *testing.B) {
		s := sampleNumericOnly()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = s.MarshalCodec()
		}
	})
}

func FuzzNumericOnly_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.NumericOnly](f, specNumericOnly)
}

// === PackedZigzag ============================================================

var specPackedZigzag = codectest.Spec[integration.PackedZigzag]{
	Sample:             samplePackedZigzag(),
	Grower:             ptrPackedZigzagGrower(),
	PackedVarintFields: []int32{1, 2}, // sint32 + sint64 → varint elements
}

func TestPackedZigzag(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.PackedZigzag](t, specPackedZigzag)
	})
}

func BenchmarkPackedZigzag(b *testing.B) {
	codectest.RunBenchSuite[integration.PackedZigzag](b, specPackedZigzag)
	b.Run("PooledUnmarshal", func(b *testing.B) {
		s := samplePackedZigzag()
		data, _ := s.MarshalCodec()
		var got integration.PackedZigzag
		if err := got.UnmarshalCodec(data); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = got.UnmarshalCodec(data)
		}
	})
}

func FuzzPackedZigzag_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.PackedZigzag](f, specPackedZigzag)
}

// === Inner ===================================================================

var specInner = codectest.Spec[integration.Inner]{
	Sample:             sampleInner(),
	ScalarVarintFields: []int32{2}, // Count
}

func TestInner(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.Inner](t, specInner)
	})
}

func BenchmarkInner(b *testing.B) {
	codectest.RunBenchSuite[integration.Inner](b, specInner)
}

func FuzzInner_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.Inner](f, specInner)
}

// === Container ===============================================================

var specContainer = codectest.Spec[integration.Container]{
	Sample:                sampleContainer(),
	Grower:                ptrContainerGrower(),
	NilPointerSample:      ptrContainerNilElement(),
	RepeatedMessageFields: []int32{3},
}

func TestContainer(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.Container](t, specContainer)
	})
	t.Run("SlabCorrectness preserves nested-string offsets", func(t *testing.T) {
		t.Parallel()
		// Guards the cross-message string slab: every nested
		// UnmarshalCodecInternal call indexes into the top-level slab
		// via an absolute slabOff+i offset. Offset-math bugs would
		// either truncate a string, bleed neighbor bytes, or panic.
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
		if got.Children[0].Label != "first" || got.Children[1].Label != "second" {
			t.Errorf("Children labels: want [first second], got [%s %s]",
				got.Children[0].Label, got.Children[1].Label)
		}
	})
	t.Run("PreScanCapacityHint sizes Children to wire element count", func(t *testing.T) {
		t.Parallel()
		// Verifies the cold-path pre-scan sizes the Children slice
		// correctly based on wire element count.
		s := integration.Container{
			Name: "prescan-test",
			Children: func() []*integration.Inner {
				out := make([]*integration.Inner, 20)
				for i := range out {
					out[i] = &integration.Inner{Label: "child", Count: int64(i)}
				}
				return out
			}(),
		}
		data, _ := s.MarshalCodec()
		var got integration.Container
		if err := got.UnmarshalCodec(data); err != nil {
			t.Fatal(err)
		}
		if len(got.Children) != 20 {
			t.Errorf("Children: want 20, got %d", len(got.Children))
		}
		if cap(got.Children) != 20 {
			t.Errorf("Children cap: want 20 (pre-scan sized), got %d", cap(got.Children))
		}
	})
}

func BenchmarkContainer(b *testing.B) {
	codectest.RunBenchSuite[integration.Container](b, specContainer)
	b.Run("PooledUnmarshal", func(b *testing.B) {
		s := sampleContainer()
		data, _ := s.MarshalCodec()
		var got integration.Container
		if err := got.UnmarshalCodec(data); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = got.UnmarshalCodec(data)
		}
	})
	b.Run("MarshalCodec", func(b *testing.B) {
		s := sampleContainer()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = s.MarshalCodec()
		}
	})
}

func FuzzContainer_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.Container](f, specContainer)
}

// === ValueContainer ==========================================================

var specValueContainer = codectest.Spec[integration.ValueContainer]{
	Sample:                sampleValueContainer(),
	Grower:                ptrValueContainerGrower(),
	RepeatedMessageFields: []int32{3},
}

func TestValueContainer(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.ValueContainer](t, specValueContainer)
	})
}

func BenchmarkValueContainer(b *testing.B) {
	codectest.RunBenchSuite[integration.ValueContainer](b, specValueContainer)
	b.Run("PooledUnmarshal", func(b *testing.B) {
		s := sampleValueContainer()
		data, _ := s.MarshalCodec()
		var got integration.ValueContainer
		if err := got.UnmarshalCodec(data); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = got.UnmarshalCodec(data)
		}
	})
}

func FuzzValueContainer_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.ValueContainer](f, specValueContainer)
}

// === Tree ====================================================================

var specTree = codectest.Spec[integration.Tree]{
	Sample:                sampleTree(),
	Grower:                ptrTreeGrower(),
	NilPointerSample:      ptrTreeNilElement(),
	RepeatedMessageFields: []int32{2},
}

func TestTree(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.Tree](t, specTree)
	})
}

func BenchmarkTree(b *testing.B) {
	codectest.RunBenchSuite[integration.Tree](b, specTree)
	b.Run("PooledUnmarshal", func(b *testing.B) {
		s := sampleTree()
		data, _ := s.MarshalCodec()
		var got integration.Tree
		if err := got.UnmarshalCodec(data); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = got.UnmarshalCodec(data)
		}
	})
}

func FuzzTree_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.Tree](f, specTree)
}

// === MapHolder ===============================================================

var specMapHolder = codectest.Spec[integration.MapHolder]{
	Sample:    sampleMapHolder(),
	Grower:    ptrMapHolderGrower(),
	Generator: genMapHolder,
	MapFields: []int32{1, 2},
	// One pre-sized keys slice per map field — escape analysis stack-
	// allocates it when the map is tiny (Sample observes 0 allocs/op).
	// Budget = 2 (one alloc per map field) covers heap fallback for
	// larger samples without re-tripping on compiler-internals drift.
	MarshalToAllocsMax: 2,
}

func TestMapHolder(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.MapHolder](t, specMapHolder)
	})
	t.Run("KeepCapacity preserves map bucket storage across reset", func(t *testing.T) {
		t.Parallel()
		// Verifies the map bucket storage is preserved by ResetCodec
		// (via clear()) so re-unmarshal into the same receiver reuses
		// buckets.
		first := sampleMapHolder()
		buf, _ := first.MarshalCodec()
		var m integration.MapHolder
		if err := m.UnmarshalCodec(buf); err != nil {
			t.Fatal(err)
		}
		mCopy := reflect.ValueOf(m.Attrs)
		addr1 := mCopy.Pointer()
		m.ResetCodec()
		if err := m.UnmarshalCodec(buf); err != nil {
			t.Fatal(err)
		}
		addr2 := reflect.ValueOf(m.Attrs).Pointer()
		if addr1 != addr2 {
			t.Errorf("map bucket storage reallocated: first=%x second=%x", addr1, addr2)
		}
	})
}

func BenchmarkMapHolder(b *testing.B) {
	codectest.RunBenchSuite[integration.MapHolder](b, specMapHolder)
	b.Run("PooledUnmarshal", func(b *testing.B) {
		s := sampleMapHolder()
		data, _ := s.MarshalCodec()
		var got integration.MapHolder
		if err := got.UnmarshalCodec(data); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = got.UnmarshalCodec(data)
		}
	})
}

func FuzzMapHolder_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.MapHolder](f, specMapHolder)
}

// === TimeHolder ==============================================================

var specTimeHolder = codectest.Spec[integration.TimeHolder]{
	Sample:    sampleTimeHolder(),
	WKTFields: []int32{1, 2},
}

func TestTimeHolder(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.TimeHolder](t, specTimeHolder)
	})
}

func BenchmarkTimeHolder(b *testing.B) {
	codectest.RunBenchSuite[integration.TimeHolder](b, specTimeHolder)
}

func FuzzTimeHolder_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.TimeHolder](f, specTimeHolder)
}

// === OneofPayload ============================================================

var specOneofPayload = codectest.Spec[integration.OneofPayload]{
	Sample: sampleOneofPayloadText(),
	Variants: []integration.OneofPayload{
		sampleOneofPayloadNumber(),
		sampleOneofPayloadNested(),
		sampleOneofPayloadBlob(),
		sampleOneofPayloadAmount(),
	},
	ScalarVarintFields: []int32{11}, // Number (int64)
	Fixed64Fields:      []int32{14}, // Amount (sfixed64)
}

func TestOneofPayload(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.OneofPayload](t, specOneofPayload)
	})
}

func BenchmarkOneofPayload(b *testing.B) {
	codectest.RunBenchSuite[integration.OneofPayload](b, specOneofPayload)
}

func FuzzOneofPayload_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.OneofPayload](f, specOneofPayload)
}

// === BytesPool ===============================================================

var specBytesPool = codectest.Spec[integration.BytesPool]{Sample: sampleBytesPool()}

func TestBytesPool(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.BytesPool](t, specBytesPool)
	})
	t.Run("KeepCapacity reuses Payload backing array across resets", func(t *testing.T) {
		t.Parallel()
		// Verifies the Payload backing array is reused across
		// unmarshals into a primed receiver.
		s := integration.BytesPool{Payload: []byte{1, 2, 3, 4, 5, 6, 7, 8}}
		data, _ := s.MarshalCodec()
		var got integration.BytesPool
		if err := got.UnmarshalCodec(data); err != nil {
			t.Fatal(err)
		}
		firstCap := cap(got.Payload)
		for range 5 {
			if err := got.UnmarshalCodec(data); err != nil {
				t.Fatal(err)
			}
			if cap(got.Payload) != firstCap {
				t.Errorf("backing array re-allocated: firstCap=%d currentCap=%d", firstCap, cap(got.Payload))
			}
		}
	})
}

func BenchmarkBytesPool(b *testing.B) {
	codectest.RunBenchSuite[integration.BytesPool](b, specBytesPool)
}

func FuzzBytesPool_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.BytesPool](f, specBytesPool)
}
