// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Package codec is the Go runtime imported by code emitted from
// protoc-gen-codec-go. It provides the minimum shared surface needed
// for generated MarshalCodec / MarshalToCodec / MarshalCodecInternal /
// UnmarshalCodec / UnmarshalCodecInternal / SizeCodec / ResetCodec
// methods: proto3 wire primitives, sentinel errors, well-known-type
// encode/decode helpers, and the Codec / Marshaler / Unmarshaler /
// Sizer / Resetter interfaces.
//
// The package has no dependencies beyond the Go standard library. It
// uses no reflection, no type registry, and no descriptor lookup —
// every serialization decision is made at code-generation time by the
// plugin and baked into the emitted methods.
//
// Consumers typically import this package only to reference sentinel
// errors (errors.Is(err, codec.ErrInvalidVarint)) or to accept a
// narrow interface contract in their own code. Generated methods do
// not need to be called through an interface; the concrete methods
// emit zero-allocation code paths when invoked directly.
//
// Testing helpers live in the sibling sub-package
// go.thesmos.sh/protoc-gen-codec/lang/go/codec/codectest so that
// the runtime itself stays stdlib-only (no transitive pgregory.net/rapid
// for consumers that only need wire primitives).
package codec
