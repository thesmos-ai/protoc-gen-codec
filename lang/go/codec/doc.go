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
//
// # Stability
//
// The following surfaces are stable as of v1.0.0 and follow semantic
// versioning — minor releases (v1.x) preserve compatibility, breaking
// changes move to v2:
//
//   - The Codec / Marshaler / Unmarshaler / Sizer / Resetter interfaces
//   - The error sentinels (ErrInvalidLength, ErrInvalidWireType,
//     ErrInvalidTag, ErrInvalidVarint, ErrBufferTooShort)
//   - The exported wire primitives (EncodeVarint, DecodeVarint,
//     SizeVarint, SkipField, zigzag helpers, Timestamp/Duration
//     encode/decode)
//   - The annotation surface defined in codec/options.proto
//     (codec.type, codec.oneof, codec.field, codec.cast,
//     codec.fixed_len, codec.use_pointer)
//   - The codectest.Spec[T] structure and the three runners
//     (RunSuite, RunBenchSuite, RunFuzzSuite)
//
// Generated code (the *MarshalCodec / *UnmarshalCodec / *SizeCodec /
// *ResetCodec methods on annotated user types) is also stable: code
// emitted by v1.0.0 will continue to compile and behave identically
// against any v1.x runtime.
//
// Deprecations are announced at least one minor version before
// removal and carry a // Deprecated: comment. Sov is the only
// currently-deprecated symbol; it remains as an alias for SizeVarint.
//
// # Concurrency
//
// The runtime in this package has no mutable state: every exported
// function (EncodeVarint, DecodeVarint, SizeVarint, SkipField,
// zigzag helpers, Timestamp / Duration encode + decode) operates
// purely on caller-provided buffers and is safe for arbitrary
// concurrent use. The only package-level state is the immutable
// sentinel-error variables (ErrInvalidLength, …); each is created
// once at init and never reassigned.
//
// The concurrency contract for generated methods on consumer types
// is shape-driven and worth stating explicitly:
//
//   - SizeCodec, MarshalCodec, MarshalToCodec, MarshalCodecInternal:
//     read the receiver only. Multiple goroutines may invoke them
//     concurrently against the same receiver, provided no goroutine
//     is concurrently mutating that receiver via UnmarshalCodec /
//     ResetCodec or via direct field writes.
//
//   - UnmarshalCodec, UnmarshalCodecInternal, ResetCodec: write the
//     receiver (every serialized field is assigned to). Each
//     receiver may be in flight in at most one such call at a time;
//     concurrent calls on the same receiver are a data race.
//     Concurrent calls on distinct receivers are safe.
//
// In practice this maps to two common patterns:
//
//  1. Marshal-side fan-out: one struct produced once, marshalled
//     concurrently from many goroutines (e.g. a request payload
//     fanning out to many subscribers). Safe.
//
//  2. Pooled unmarshal: each goroutine drains a fresh receiver from
//     a sync.Pool, decodes into it, returns it. The pool guarantees
//     no two goroutines hold the same receiver. Safe.
//
// Wiring an Unmarshaler/Resetter callable from multiple goroutines
// without per-goroutine ownership of the receiver requires explicit
// caller-side synchronization (e.g. wrapping in a mutex). The
// runtime does not provide one; the receiver is the caller's data.
package codec
