// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	"testing"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec"
	"go.stealthscale.io/protoc-gen-codec/lang/go/integration"
	"go.stealthscale.io/protoc-gen-codec/lang/go/integration/external"
)

// sampleCrossContainer populates all three cross-package field shapes
// (singular pointer, value-slice, pointer-slice) so the codegen for each
// path is exercised under the standard suite.
func sampleCrossContainer() integration.CrossContainer {
	return integration.CrossContainer{
		Name: "cross",
		Item: &external.External{Tag: "single", Seq: 1},
		Items: []external.External{
			{Tag: "v1", Seq: 10},
			{Tag: "v2", Seq: 20},
		},
		PtrItems: []*external.External{
			{Tag: "p1", Seq: 100},
			{Tag: "p2", Seq: 200},
		},
	}
}

func TestExternal_Codec(t *testing.T) {
	codec.RunTestSuite[external.External](t, external.External{Tag: "x", Seq: 7})
}

func TestCrossContainer_Codec(t *testing.T) {
	codec.RunTestSuite[integration.CrossContainer](t, sampleCrossContainer())
}

func FuzzCrossContainer_Codec(f *testing.F) {
	codec.RunFuzzRoundtrip[integration.CrossContainer](f,
		sampleCrossContainer(),
		integration.CrossContainer{},
	)
}

func TestCrossContainer_Coverage(t *testing.T) {
	codec.RunCoverageSuite[integration.CrossContainer](t, sampleCrossContainer(), 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // string as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 0}, // singular cross-pkg message as varint
		codec.WireMismatch{FieldNum: 3, WrongWireType: 0}, // repeated value cross-pkg message as varint
		codec.WireMismatch{FieldNum: 4, WrongWireType: 0}, // repeated pointer cross-pkg message as varint
	)
}

func TestExternal_Coverage(t *testing.T) {
	codec.RunCoverageSuite[external.External](t, external.External{Tag: "x", Seq: 7}, 999,
		codec.WireMismatch{FieldNum: 1, WrongWireType: 0}, // string as varint
		codec.WireMismatch{FieldNum: 2, WrongWireType: 2}, // int64 as length-delimited
	)
}

func TestCrossContainer_CoverageExt(t *testing.T) {
	grower := sampleCrossContainer()
	grower.Items = append(grower.Items, external.External{Tag: "v3", Seq: 30})
	grower.PtrItems = append(grower.PtrItems, &external.External{Tag: "p3", Seq: 300})
	codec.RunExtendedCoverageSuite[integration.CrossContainer](t, sampleCrossContainer(), grower)
}

func TestExternal_CoverageExt(t *testing.T) {
	codec.RunExtendedCoverageSuite[external.External](t, external.External{Tag: "x", Seq: 7}, external.External{})
}

// TestCrossContainer_NilPointerElement exercises the "if elem == nil { continue }"
// branches in CrossContainer.SizeCodec and CrossContainer.MarshalCodecInternal
// for PtrItems ([]*external.External).
func TestCrossContainer_NilPointerElement(t *testing.T) {
	t.Parallel()
	c := integration.CrossContainer{
		Name: "with-nil",
		PtrItems: []*external.External{
			{Tag: "p1"},
			nil,
			{Tag: "p2"},
		},
	}
	codec.AssertMarshalWithNilPointerElement[integration.CrossContainer](t, c)
}

// TestCrossContainer_WarmPath primes the receiver with one decode, then
// re-decodes the same payload — exercises cursor-reuse on the value slice
// and *External pointer reuse that the cold-path decode skips. Then re-decodes
// a *larger* payload so the new-element-append branch (elem = new(...)) is
// also covered.
func TestCrossContainer_WarmPath(t *testing.T) {
	t.Parallel()
	s := sampleCrossContainer()
	data, err := s.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	var got integration.CrossContainer
	if uerr := got.UnmarshalCodec(data); uerr != nil {
		t.Fatalf("first UnmarshalCodec: %v", uerr)
	}
	if uerr := got.UnmarshalCodec(data); uerr != nil {
		t.Fatalf("second UnmarshalCodec: %v", uerr)
	}

	larger := s
	larger.Items = append(larger.Items, external.External{Tag: "v3", Seq: 30})
	larger.PtrItems = append(larger.PtrItems, &external.External{Tag: "p3", Seq: 300})
	bigData, merr := larger.MarshalCodec()
	if merr != nil {
		t.Fatalf("MarshalCodec(larger): %v", merr)
	}
	if uerr := got.UnmarshalCodec(bigData); uerr != nil {
		t.Fatalf("third UnmarshalCodec (grown): %v", uerr)
	}
}

// TestCrossContainer_BadTag exercises the tag-decode-fail branch
// (codec.DecodeVarint returning n<0 inside the field loop).
func TestCrossContainer_BadTag(t *testing.T) {
	t.Parallel()
	// 10 bytes of 0x80 form an unterminated varint — DecodeVarint returns -1.
	bad := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	var got integration.CrossContainer
	if err := got.UnmarshalCodec(bad); err == nil {
		t.Fatal("expected error from unterminated varint tag")
	}
}
