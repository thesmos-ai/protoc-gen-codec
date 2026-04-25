// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Tests for the External fixture's generated codec methods.
//
// These tests live in the external/ package itself (rather than in
// the parent integration_test package) so gremlins can mutation-test
// external.codec.go: gremlins is package-scoped, and a test in
// lang/go/integration/ does not kill mutants in
// lang/go/integration/external/. Without this file, those mutants
// would be reported as NOT COVERED.
//
// CrossContainer (in the parent package) still drives External via
// the cross-package nested-message codegen path; this file is the
// in-package signal for mutation testing only.

package external_test

import (
	"testing"
	"time"

	"go.thesmos.sh/protoc-gen-codec/lang/go/codec/codectest"
	"go.thesmos.sh/protoc-gen-codec/lang/go/integration/external"
)

var specExternal = codectest.Spec[external.External]{
	Sample:              external.External{Tag: "x", Seq: 7},
	ScalarVarintFields:  []int32{2}, // Seq (int64)
	MarshalToLatencyMax: 50 * time.Nanosecond,
}

func TestExternal_Codec(t *testing.T) {
	codectest.RunSuite[external.External](t, specExternal)
}

func BenchmarkExternal_Codec(b *testing.B) {
	codectest.RunBenchSuite[external.External](b, specExternal)
}

func FuzzExternal_Codec(f *testing.F) {
	codectest.RunFuzzSuite[external.External](f, specExternal)
}
