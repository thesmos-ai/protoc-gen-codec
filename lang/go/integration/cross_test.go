// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	"testing"

	"go.stealthscale.io/protoc-gen-codec/lang/go/codec/codectest"
	"go.stealthscale.io/protoc-gen-codec/lang/go/integration"
	"go.stealthscale.io/protoc-gen-codec/lang/go/integration/external"
)

// sampleCrossContainer populates all three cross-package field shapes
// (singular pointer, value-slice, pointer-slice).
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

func ptrCrossContainerGrower() *integration.CrossContainer {
	g := sampleCrossContainer()
	g.Items = append(g.Items, external.External{Tag: "v3", Seq: 30})
	g.PtrItems = append(g.PtrItems, &external.External{Tag: "p3", Seq: 300})
	return &g
}

func ptrCrossContainerNilElement() *integration.CrossContainer {
	s := integration.CrossContainer{
		Name: "with-nil",
		PtrItems: []*external.External{
			{Tag: "p1"},
			nil,
			{Tag: "p2"},
		},
	}
	return &s
}

// === External ================================================================

var specExternal = codectest.Spec[external.External]{
	Sample:             external.External{Tag: "x", Seq: 7},
	ScalarVarintFields: []int32{2}, // Seq (int64)
}

func TestExternal(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[external.External](t, specExternal)
	})
}

func BenchmarkExternal(b *testing.B) {
	codectest.RunBenchSuite[external.External](b, specExternal)
}

func FuzzExternal_Codec(f *testing.F) {
	codectest.RunFuzzSuite[external.External](f, specExternal)
}

// === CrossContainer ==========================================================

var specCrossContainer = codectest.Spec[integration.CrossContainer]{
	Sample:                sampleCrossContainer(),
	Grower:                ptrCrossContainerGrower(),
	NilPointerSample:      ptrCrossContainerNilElement(),
	RepeatedMessageFields: []int32{3, 4}, // Items (value), PtrItems (pointer)
}

func TestCrossContainer(t *testing.T) {
	t.Run("Codec", func(t *testing.T) {
		codectest.RunSuite[integration.CrossContainer](t, specCrossContainer)
	})
	t.Run("PrescanFixed32Short skips short unknown wireType-5 field", func(t *testing.T) {
		t.Parallel()
		// Exercises the prescan's `if pi+4 > l` branch via an unknown-
		// field wireType-5 tag with a short body.
		data := []byte{0x9d, 0x06, 0x00, 0x00}
		var got integration.CrossContainer
		_ = got.UnmarshalCodec(data)
	})
}

func BenchmarkCrossContainer(b *testing.B) {
	codectest.RunBenchSuite[integration.CrossContainer](b, specCrossContainer)
}

func FuzzCrossContainer_Codec(f *testing.F) {
	codectest.RunFuzzSuite[integration.CrossContainer](f, specCrossContainer)
}
