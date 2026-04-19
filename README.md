# protoc-gen-codec

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
- **100% test coverage** — dedicated testing sub-package ships `Spec[T]` + runners that drive generated code to 100% coverage without hand-rolling corruption payloads

## Annotations

Defined in `codec/options.proto`:

| Annotation | Scope | Purpose |
|---|---|---|
| `codec.type` | Message | Maps proto message → target type |
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
import "go.stealthscale.io/protoc-gen-codec/lang/go/codec/codectest"

var specMyType = codectest.Spec[MyType]{
    Sample:             sampleMyType(),
    ScalarVarintFields: []int32{2, 3, 5},
    // PackedFields, MapFields, RepeatedMessageFields, WKTFields,
    // Fixed64Fields, Fixed32Fields, FixedLenBytesFields,
    // Grower, NilPointerSample, Generator — all optional
}

func TestMyType_Codec(t *testing.T)      { codectest.RunSuite[MyType](t, specMyType) }
func BenchmarkMyType_Codec(b *testing.B) { codectest.RunBenchSuite[MyType](b, specMyType) }
func FuzzMyType_Codec(f *testing.F)      { codectest.RunFuzzSuite[MyType](f, specMyType) }
```

The `RunSuite` call expands into ~20 sub-tests (roundtrip, reset, nil-safety, cross-format, corruption, per-field wire-type mismatch, short-buffer handling, unknown-field skip, property-based via `rapid` when a generator is supplied). Configured correctly per the Spec, it drives generated code coverage to 100%. See [`docs/generators/go.md`](docs/generators/go.md) for the full field-category cheatsheet and [a complete worked example](docs/generators/go.md#testing).

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — language-neutral architecture, design principles, and project layout
- [`docs/generators/go.md`](docs/generators/go.md) — how `protoc-gen-codec-go` emits code (generated methods, field mapping, slab strategies, reset semantics, testing framework)

## License

Apache License 2.0
