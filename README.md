# protoc-gen-codec

A custom protoc generator that emits high-performance binary serialization methods on **existing** hand-written types. No generated types — the `.proto` file is the schema, your source code is the behaviour, the generator bridges them.

## Features

- **Zero generated types** — emits marshal / unmarshal / size / reset methods on your existing types
- **Zero-alloc marshal** — a pre-allocated buffer variant writes directly into caller memory
- **Low-allocation unmarshal** — generation-time schema analysis picks a per-message strategy that minimises allocations without aliasing the input buffer
- **Packed encoding** — repeated scalars use proto3 packed format automatically
- **Fixed-length guards** — `codec.fixed_len` rejects truncated/padded byte arrays at unmarshal
- **Capacity-preserving reset** — `codec.keep_capacity` keeps slice backing arrays alive for pooled reuse
- **Typed errors** — unmarshal failures wrap language-appropriate sentinels for programmatic matching
- **DoS-resistant** — bounds-checked length handling prevents OOM from inflated length varints

## Annotations

Defined in `codec/options.proto`:

| Annotation | Scope | Purpose |
|---|---|---|
| `codec.type` | Message | Maps proto message → target type |
| `codec.field` | Field | Explicit field name override |
| `codec.cast` | Field | Type cast (enums, fixed-point, byte arrays) |
| `codec.fixed_len` | Field | Strict byte-length guard on unmarshal |
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

All binaries share `internal/core/` for schema analysis and `codec/options.proto` for annotations. Only the code emission differs per language.

## Runtime

Each generator ships a small runtime library that the generated code imports for wire primitives and error sentinels. Runtimes carry no dependencies beyond the target language's standard library.

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — language-neutral architecture, design principles, and project layout
- [`docs/generators/go.md`](docs/generators/go.md) — how `protoc-gen-codec-go` emits code (generated methods, field mapping, slab strategies, reset semantics)

## License

Apache License 2.0
