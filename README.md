# protoc-gen-codec

[![CI](https://github.com/thesmos-ai/protoc-gen-codec/actions/workflows/ci.yml/badge.svg)](https://github.com/thesmos-ai/protoc-gen-codec/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/thesmos-ai/protoc-gen-codec)](https://github.com/thesmos-ai/protoc-gen-codec/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/go.stealthscale.io/protoc-gen-codec.svg)](https://pkg.go.dev/go.stealthscale.io/protoc-gen-codec)
[![Go Report Card](https://goreportcard.com/badge/go.stealthscale.io/protoc-gen-codec)](https://goreportcard.com/report/go.stealthscale.io/protoc-gen-codec)
[![Go Version](https://img.shields.io/github/go-mod/go-version/thesmos-ai/protoc-gen-codec)](go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://www.conventionalcommits.org/en/v1.0.0/)
[![codecov](https://codecov.io/gh/thesmos-ai/protoc-gen-codec/graph/badge.svg)](https://codecov.io/gh/thesmos-ai/protoc-gen-codec)
[![Mutation](https://img.shields.io/badge/mutation-100%25%20effective-brightgreen.svg)](docs/compliance/golang/codec.md)

A custom protoc generator that emits high-performance binary serialization methods on **existing** hand-written types. No generated types — the `.proto` file is the schema, your source code is the behaviour, the generator bridges them.

## Features

- **Zero generated types** — emits marshal / unmarshal / size / reset methods on your existing types
- **Zero-alloc marshal** — a pre-allocated buffer variant writes directly into caller memory
- **Low-allocation unmarshal** — generation-time schema analysis picks a per-message strategy that minimises allocations without aliasing the input buffer
- **Packed encoding** — repeated scalars use proto3 packed format automatically
- **Deterministic output** — map fields marshal in sorted-key order (content-addressable storage, signing, caching)
- **Fixed-length guards** — `codec.fixed_len` rejects truncated/padded byte arrays at unmarshal
- **Capacity-preserving reset** — backing storage for slices/maps is preserved for pooled reuse
- **Typed errors** — unmarshal failures wrap language-appropriate sentinels for programmatic matching; error messages include field name + number
- **DoS-resistant** — bounds-checked length handling prevents OOM from inflated length varints
- **In-bench alloc + latency gate** — `StartContract(b).AllocsMax(n).LatencyMax(d)` fails benchmarks at the first regression instead of waiting for a benchstat baseline diff
- **100% test coverage + mutation testing** — every `*.codec.go` line covered, every aggregate test package at 100%, and 100% effective mutation kill rate on the runtime and analyzer layers (gremlins)

## Annotations

Defined in `codec/options.proto`:

| Annotation | Scope | Purpose |
|---|---|---|
| `codec.type` | Message | Maps proto message → target type |
| `codec.oneof` | Message | Declares Go-only discriminator + cast for a non-synthetic `oneof` |
| `codec.field` | Field | Explicit field name override |
| `codec.cast` | Field | Type cast (enums, fixed-point, byte arrays) |
| `codec.fixed_len` | Field | Strict byte-length guard on unmarshal |
| `codec.use_pointer` | Field (message) | Override pointer vs. value representation for nested messages |
| `codec.keep_capacity` | Field | Preserve slice capacity on reset |

## Usage

```protobuf
syntax = "proto3";
import "codec/options.proto";

message MyType {
  option (codec.type) = "MyType";

  string id = 1 [(codec.field) = "ID"];
  uint32 status = 2 [(codec.field) = "Status", (codec.cast) = "Status"];
  bytes ref = 3 [(codec.field) = "Ref", (codec.cast) = "hash.Digest", (codec.fixed_len) = 32];
}
```

Invoke the language-specific plugin through protoc, e.g.:

```bash
protoc --codec-<lang>_out=. mytype.proto
```

See the per-language documentation under [`docs/generators/`](docs/generators/) for exact plugin invocation and the generated API.

## Multi-language support

The generator is structured for multiple target languages:

```
cmd/protoc-gen-codec-go/     # Go code emitter
cmd/protoc-gen-codec-ts/     # TypeScript (future)
cmd/protoc-gen-codec-rust/   # Rust (future)
```

All binaries share `internal/core/` for schema analysis and `codec/options.proto` for annotations. Language runtimes live under `lang/<lang>/codec/`; only the code emission differs per language.

## Runtime

Each generator ships a small runtime library that the generated code imports for wire primitives and error sentinels. Runtimes carry no dependencies beyond the target language's standard library. Testing helpers live in a separate sub-package (`codec/codectest/` for Go) so the runtime stays dependency-free for production use.

## Testing your consumer types

Each language ships a testing sub-package with a declarative `Spec[T]` you write once per annotated type, plus three role-specific runners that read it. For Go:

```go
import (
    "time"
    "go.stealthscale.io/protoc-gen-codec/lang/go/codec/codectest"
)

var specMyType = codectest.Spec[MyType]{
    Sample:             sampleMyType(),
    ScalarVarintFields: []int32{2, 3, 5},
    // PackedVarintFields / PackedFixed64Fields / PackedFixed32Fields,
    // MapFields, RepeatedMessageFields, WKTFields,
    // Fixed64Fields, Fixed32Fields, FixedLenBytesFields,
    // Grower, NilPointerSample, Generator,
    // MarshalToAllocsMax, MarshalToLatencyMax, SkipJSONComparisons
    //   — all optional
}

func TestMyType_Codec(t *testing.T)      { codectest.RunSuite[MyType](t, specMyType) }
func BenchmarkMyType_Codec(b *testing.B) { codectest.RunBenchSuite[MyType](b, specMyType) }
func FuzzMyType_Codec(f *testing.F)      { codectest.RunFuzzSuite[MyType](f, specMyType) }
```

The `RunSuite` call expands into 30+ sub-tests (roundtrip, reset, nil-safety, cross-format, corruption, per-field wire-type mismatch, short-buffer handling, unknown-field skip, map-entry unknown-sub-field skip, property-based via `rapid` when a generator is supplied). `RunBenchSuite` wraps the `Codec/MarshalTo` subtest in a `StartContract` scope so any allocation regression (and any order-of-magnitude latency regression, when `MarshalToLatencyMax` is set) fails the bench in-process. Configured correctly per the Spec, the suite drives generated code coverage to 100%. See [`docs/generators/go.md`](docs/generators/go.md) for the full field-category cheatsheet and [a complete worked example](docs/generators/go.md#testing).

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — language-neutral architecture, design principles, and project layout
- [`docs/generators/go.md`](docs/generators/go.md) — how `protoc-gen-codec-go` emits code (generated methods, field mapping, slab strategies, reset semantics, testing framework, `StartContract` bench gating)
- [`docs/compliance/golang/codec.md`](docs/compliance/golang/codec.md) — Go-target package audit: REQ inventory, REQ→test mapping, evidence (coverage / mutation / bench / determinism), exceptions

## License

Apache License 2.0
