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
| `codec.use_pointer`   | Field   | Override pointer vs. value representation for nested messages |
| `codec.keep_capacity` | Field   | Intent to preserve slice backing array on reset (advisory per-language) |

Messages without `codec.type` are silently skipped — a single `.proto`
file may mix codec-annotated messages alongside schema-only ones. For
messages that *are* annotated, fields with non-obvious mappings must
declare a cast: missing casts on ambiguous types, mismatched
fixed-length values, or conflicting `use_pointer` requirements fail at
generation, not at run time.

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

`codec.keep_capacity` is an advisory hint: slice-heavy messages can be
recycled without releasing their backing buffers. Individual generators
may treat preservation as the unconditional default (the Go generator
does), in which case the annotation is accepted for source compatibility
but has no additional effect.

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
codec/                        Language-neutral annotation schema
  options.proto               codec.type, codec.field, codec.cast, ...

lang/<language>/codec/        Per-language runtime imported by generated code
  (e.g. lang/go/codec/)       wire primitives, sentinel errors, interfaces

internal/core/                Language-agnostic schema analysis
  analysis.go                 AnalyzeMessage, analyzeField
  options.go                  Extracts codec.* annotations from descriptors

internal/lang/<language>/     Per-language code emission
cmd/protoc-gen-codec-<language>/  Per-language plugin binary
lang/<language>/integration/  Integration fixtures and property-based tests
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
- pointer vs. value representation for nested messages
  (`codec.use_pointer`, with cardinality-dependent defaults and forced
  pointer for self-referential types)
- reset behaviour (`codec.keep_capacity`)
- repeated / packed / map / oneof metadata
- well-known-type recognition (Timestamp, Duration)

The analyser is the point at which the schema is validated against the
annotation contract — missing casts on ambiguous types or mismatched
fixed-length values fail here, before any code is written.

## Runtime Library

Each target language has a small runtime package at
`lang/<language>/codec/`, imported by generated code. It holds:

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
- **Cross-format consistency** — decoded objects agree with an
  independent serialization (e.g. JSON) of the same source data, giving
  field-mapping verification without relying on a parallel generator.
- **Property-based fuzzing** — randomised inputs exercise varint
  boundaries, packed encoding, length prefixes, and fixed-length guards.
- **Corruption handling** — truncated buffers, invalid wire types, and
  unknown fields produce typed errors or are skipped per proto3 rules.

Generators also benchmark marshal/unmarshal paths with an explicit
zero-allocation target for the pre-allocated marshal path.

CI enforces the suite through standing regression gates:

- **Bench regression** — `benchstat` compares against a committed
  baseline and fails on any allocation-count increase or wall-clock
  regression above a small threshold.
- **Coverage floor** — per-file coverage on generated `.codec.*` sources
  must stay at or above a declared minimum (95% for Go).
- **Deterministic generation** — running the generator twice produces
  byte-identical output.
