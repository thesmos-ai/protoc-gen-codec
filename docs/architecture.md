# Architecture

`protoc-gen-codec` is a protocol-buffer code generator that emits
serialization methods on **existing, hand-written types** rather than
generating parallel types from the schema.

This document describes the architecture common to all target languages.
Language-specific code-generation rules live alongside each generator:

- [`generators/go.md`](generators/go.md) — the `protoc-gen-codec-go` binary

## Motivation

Standard protoc plugins generate new types from `.proto` files. In projects
that already own domain types carrying validation, invariants, and
behaviour, this forces a translation layer between generated wire types
and domain types — duplicating declarations and pushing copy cost onto
every read and write path.

`protoc-gen-codec` inverts the relationship:

- The `.proto` file is the **schema**.
- The source file is the **behaviour**.
- The generator bridges them by emitting serialization methods as
  receivers on the target type — never by declaring the type itself.

## Design Principles

### No generated types

The generator never emits struct, class, or record declarations. Types
are authored by hand in the target language and carry whatever validation
and helper logic the project needs. The generator only adds serialization.

### Annotations are the contract

The mapping between schema and target type is explicit, not inferred.
Each proto message names its target type; each field names its target
field and, where relevant, a cast. Annotations are defined in
`codec/options.proto`:

| Annotation            | Scope   | Purpose                                             |
|-----------------------|---------|-----------------------------------------------------|
| `codec.type`          | Message | Target type name for the emitted methods            |
| `codec.field`         | Field   | Target field name override                          |
| `codec.cast`          | Field   | Cast decoded wire value to a named type             |
| `codec.fixed_len`     | Field   | Strict byte-length guard on unmarshal               |
| `codec.keep_capacity` | Field   | Preserve slice backing array on reset               |

Missing annotations on fields with non-obvious mappings cause a
**generation error**. Silent fallbacks are deliberately avoided: schema
and source must agree, and the generator enforces that agreement at
build time.

### Memory safety over zero-copy

The generated code never aliases the input buffer into decoded strings
or byte slices via unsafe tricks. String and byte fields are always
copied out of the input during unmarshal. This prevents use-after-free
corruption when the input buffer is pooled and recycled — a category
of silent bug that is worse than the allocation cost it would save.

Each generator is free to minimise that allocation cost (e.g. by
allocating multiple fields from one contiguous backing buffer) as long
as the no-aliasing guarantee holds.

### Zero-alloc marshal

The marshal path writes into a caller-provided, pre-allocated buffer
whose size is computed by a dedicated `Size` method. No intermediate
buffers, no reflection, no dispatch tables. The buffer typically comes
from a pool; the generated code simply fills it.

### Pool-friendly reset

Every message gets a reset method that zeroes serialized fields so the
object can be returned to a pool. Because the reset is generated from
the proto field list, adding a new field cannot leave stale data behind
— the reset stays complete as the schema evolves.

Fields annotated `codec.keep_capacity` preserve their backing array on
reset so slice-heavy messages can be recycled without releasing their
buffers.

### DoS resistance

Unmarshal performs bounds-checked handling of length-delimited fields
so that an inflated length varint cannot force large allocations before
the wire actually delivers the bytes. Fixed-length fields annotated with
`codec.fixed_len` reject any length other than the declared value,
closing off silent truncation and zero-padding on cryptographic or
hashed types.

### Schema evolution

Wire compatibility follows standard proto3 rules: fields are identified
by number, adding a new number is backward compatible, removing a
number leaves a gap. The generator inherits the guarantees of the proto
wire format directly.

## Project Layout

```
codec/                        Runtime library + annotation definitions
  options.proto               Annotation schema (codec.type, codec.field, ...)
  wire.go                     Wire-format primitives
  errors.go                   Sentinel errors for errors.Is matching
  interface.go                Marshaler / Unmarshaler interfaces

internal/core/                Language-agnostic schema analysis
  analysis.go                 AnalyzeMessage, analyzeField
  options.go                  Extracts codec.* annotations from descriptors

internal/lang/<language>/     Per-language code emission
cmd/protoc-gen-codec-<language>/  Per-language plugin binary
testdata/<language>/          Fixtures and property-based tests
```

The important split is between `internal/core/` and
`internal/lang/<language>/`:

- **`internal/core/`** walks proto descriptors, resolves annotations,
  and produces a normalised representation of each message and field.
  It has no knowledge of the target language.
- **`internal/lang/<language>/`** consumes that representation and
  emits code. A new target language is added by introducing a sibling
  package and a matching `cmd/protoc-gen-codec-<language>/` binary.
  The annotation set and the analysis layer are shared.

## Schema Analysis Layer

`internal/core` reads each proto message descriptor and produces a
normalised field list. For each field it resolves:

- wire kind (varint / fixed32 / fixed64 / length-delimited)
- declared target field name (from `codec.field`, falling back to the
  proto field name)
- declared cast type (from `codec.cast`)
- fixed-length guards (`codec.fixed_len`)
- reset behaviour (`codec.keep_capacity`)
- repeated / packed / map / oneof metadata

The analyser is the point at which the schema is validated against the
annotation contract — missing casts on ambiguous types or mismatched
fixed-length values fail here, before any code is written.

## Runtime Library

Each target language has a small runtime package holding:

- wire primitives (varint read/write, length-delimited framing, field
  skip)
- sentinel errors for error matching
- marshaler / unmarshaler interfaces

The runtime is intentionally minimal: no reflection, no type registry,
no descriptor lookup. Everything the generator needs to produce is
determined statically at generation time.

## Testing Philosophy

Each generator is expected to verify, at minimum:

- **Roundtrip** — `Marshal` then `Unmarshal` returns an object equal
  to the input for every supported field combination.
- **Size accuracy** — the declared size exactly matches the length of
  the marshaled output.
- **Reset completeness** — after reset, the object is indistinguishable
  from its zero value for every serialized field.
- **Property-based fuzzing** — randomised inputs exercise varint
  boundaries, packed encoding, length prefixes, and fixed-length guards.

Generators also benchmark marshal/unmarshal paths, with an explicit
zero-allocation target for the pre-allocated marshal path.
