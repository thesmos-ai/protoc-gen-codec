---
package: protoc-gen-codec (Go target)
audited: 2026-04-25
auditor: roy.klopper@stealthscale.io
status: elite
---

# Go Codec — Package Audit

This audit covers the three Go-target surfaces of `protoc-gen-codec`:

- `lang/go/codec/` — runtime (wire primitives, sentinels, interfaces)
- `internal/core/` — language-neutral schema analyzer
- `internal/lang/golang/` — Go code emission (output: `*.codec.go`)

The contract source-of-truth is [`docs/architecture.md`](../../architecture.md)
and [`docs/generators/go.md`](../../generators/go.md). Every assertable
promise from those documents is registered below as a `REQ-PKG-CODEC-NNN`
entry with its covering test(s) so future audits can re-verify by table
lookup rather than by re-reading test source.

## Scope

| Question                                              | Answer                                                                                       |
|-------------------------------------------------------|----------------------------------------------------------------------------------------------|
| Public interface                                      | `codec.Sizer` / `Marshaler` / `Unmarshaler` / `Resetter` / `Codec` (composed)                |
| `[]byte` boundary                                     | yes — `UnmarshalCodec(data []byte) error` and `MarshalToCodec(buf []byte) (int, error)`      |
| Stateful                                              | partial — receivers are mutable; pointer-pooling and slab reuse persist across calls         |
| Allocation contracts                                  | `MarshalToCodec` is zero-alloc except where map sort requires it (`Spec.MarshalToAllocsMax`) |
| Latency contracts                                     | not declared per-method; bench regressions gated via `make bench-compare`                    |
| Authoritative reference                               | none — wire-compat tests vs `google.golang.org/protobuf` explicitly out of scope             |
| MC/DC                                                 | not applicable (no safety-critical avionics designation)                                     |
| Distributed invariants                                | none — codec is a leaf, no cross-service or cross-region state                               |

## REQ inventory

### Contract REQs (per-method / per-feature guarantees)

| ID  | Type     | Promise                                                                                                                  |
|-----|----------|--------------------------------------------------------------------------------------------------------------------------|
| 001 | contract | `MarshalCodec` returns a buffer of exactly `SizeCodec()` bytes containing the proto3 wire-format encoding of the receiver |
| 002 | contract | `MarshalToCodec(buf)` writes into the caller-provided buffer and returns the byte count; allocates nothing in the loop body |
| 003 | contract | `MarshalCodecInternal(buf)` is the unchecked body shared by the public wrappers and cross-package callers; assumes `len(buf) >= SizeCodec()` |
| 004 | contract | `UnmarshalCodec(data)` reads proto3 wire format field-by-field into the receiver's fields                                |
| 005 | contract | `UnmarshalCodecInternal(data, slab, slabOff)` threads the cross-message string slab through nested decode calls          |
| 006 | contract | `SizeCodec()` returns the exact serialized byte length without allocating                                                |
| 007 | contract | `ResetCodec()` zeroes every serialized field so the receiver can be returned to a `sync.Pool`                            |
| 008 | contract | `len(MarshalCodec()) == SizeCodec()` — invariant exploited by `MarshalToCodec` for buffer pre-sizing                     |
| 009 | contract | proto3 packed scalars (`repeated int32` etc.) are encoded packed by default                                              |
| 010 | contract | proto3 unpacked alternates of packed-eligible repeated fields decode without error (dual-encoding compatibility)         |
| 011 | contract | `(codec.cast) = "T"` emits `T(v)` on decode and `cast(v)` on encode — required for enum / typedef'd integer fields       |
| 012 | contract | `(codec.fixed_len) = N` enforces strict N-byte length on decode for `bytes` fields                                       |
| 013 | contract | `(codec.field) = "Name"` overrides PascalCase auto-derivation of the target Go field name                                |
| 014 | contract | Singular nested message field defaults to `*T` (proto3 absence semantics)                                                |
| 015 | contract | `repeated MsgType` defaults to `[]T` (value slice — contiguous backing, GC-friendly)                                     |
| 016 | contract | Self-referential message fields are forced to `[]*T` regardless of `(codec.use_pointer)` — value semantics would be infinite-size |
| 017 | contract | `(codec.use_pointer)` overrides the default cardinality for non-self-ref message fields                                  |
| 018 | contract | proto enums map to Go integer enums via `(codec.cast)`; the leading `*_UNSPECIFIED = 0` is required and maps to Go zero  |
| 019 | contract | Non-synthetic `oneof` requires a matching `(codec.oneof) = {name, discriminator, cast}` annotation                       |
| 020 | contract | Oneof discriminator is **Go-only** — no proto tag number, no wire bytes; values must equal branch field numbers          |
| 021 | contract | On encode, the generator emits the branch matching `m.<Discriminator>` unconditionally (default-value branch still emitted) |
| 022 | contract | On decode, receipt of a branch tag sets both the branch field and the discriminator `m.<Discriminator> = Cast(branchNum)` |
| 023 | contract | `ResetCodec` zeroes the discriminator field along with the branch fields                                                 |
| 024 | contract | proto3 synthetic oneofs (proto3 `optional`) decode to nullable Go pointers (`*int32`, `*bool`, …)                        |
| 025 | contract | Strings and byte slices are **always** copied out of the input buffer — no `unsafe` aliasing, no lifetime coupling       |
| 026 | contract | Map fields encode in sorted-key order — `MarshalCodec` output is byte-stable across calls with the same input            |
| 027 | contract | Bool-keyed maps encode in `false → true` sequence (no slice allocation needed)                                           |
| 028 | contract | Generator output is byte-identical across runs — `make verify-deterministic-gen` enforces                                |
| 029 | contract | `SizeCodec` / `MarshalCodec` / `MarshalToCodec` / `ResetCodec` on a `nil` receiver is safe and returns the documented zero-equivalent |

### Failure-mode REQs (errors and rejections)

| ID  | Type   | Rejection                                                                                                          |
|-----|--------|--------------------------------------------------------------------------------------------------------------------|
| 050 | error  | Unterminated tag varint → `ErrInvalidTag` (DoS resistance: bounded tag-decode)                                     |
| 051 | error  | Malformed varint body in any field → `ErrInvalidVarint`                                                            |
| 052 | error  | Buffer shorter than declared length-delimited field body → `ErrBufferTooShort`                                     |
| 053 | error  | Buffer shorter than fixed-width body (`fixed32` / `fixed64`) → `ErrBufferTooShort`                                 |
| 054 | error  | `MarshalToCodec(buf)` with `len(buf) < SizeCodec()` → `ErrBufferTooShort`, `n=0` (no partial write)                |
| 055 | error  | Unknown wire type (3, 4, 6, 7) on known-field tag → `ErrInvalidWireType`                                           |
| 056 | error  | Unknown wire type on unknown-field tag → `ErrInvalidWireType` (via `SkipField`)                                    |
| 057 | error  | `(codec.fixed_len)` byte field with declared-length-mismatched payload → `ErrInvalidLength`                        |
| 058 | error  | `(codec.fixed_len)` byte field with body shorter than declared length → `ErrBufferTooShort`                        |
| 059 | error  | Inflated length varint cannot force a large allocation before bytes arrive (DoS resistance for length-delimited)   |
| 060 | gen    | `(codec.fixed_len) = 0` on any field → generation fails                                                            |
| 061 | gen    | `(codec.fixed_len)` on a non-bytes field → generation fails                                                        |
| 062 | gen    | `(codec.cast)` on a message-kind field → generation fails                                                          |
| 063 | gen    | `(codec.cast) = "<invalid Go identifier>"` → generation fails                                                       |
| 064 | gen    | `(codec.cast)` referencing an unresolved alias → generation fails                                                  |
| 065 | gen    | Non-synthetic oneof without matching `(codec.oneof)` → generation fails                                            |
| 066 | gen    | `(codec.oneof)` missing `name`, `discriminator`, or `cast` → generation fails                                      |
| 067 | gen    | Error messages are not double-prefixed (regression guard for layered `fmt.Errorf` calls)                           |

### Invariant REQs (across operation traces)

| ID  | Type      | Property                                                                                                            |
|-----|-----------|---------------------------------------------------------------------------------------------------------------------|
| 080 | invariant | **Roundtrip**: `Unmarshal(Marshal(x)) == x` for every fully-populated sample (reflect.DeepEqual)                    |
| 081 | invariant | **Wire stability**: `Marshal(Unmarshal(Marshal(x))) == Marshal(x)` byte-for-byte (deterministic encoding)           |
| 082 | invariant | **Reset completeness**: post-`ResetCodec`, `SizeCodec() == 0` and `len(MarshalCodec()) == 0`                        |
| 083 | invariant | **Cross-format consistency**: `Unmarshal(MarshalCodec(x))` and `json.Unmarshal(json.Marshal(x))` produce equal structs |
| 084 | invariant | **Wire size**: codec output is strictly smaller than `json.Marshal` output for the same sample                      |
| 085 | invariant | **Pointer pooling**: optional `*T` slots are reused across `UnmarshalCodec` calls into the same receiver            |
| 086 | invariant | **Bytes capacity preservation**: `[]byte` backing array survives `ResetCodec` and subsequent re-`UnmarshalCodec`    |
| 087 | invariant | **Map bucket preservation**: map fields cleared via `clear()` on `ResetCodec` retain bucket storage                 |
| 088 | invariant | **Repeated `[]*T` cursor reuse**: existing element pointers reused; no per-element allocation on warm-path unmarshal |
| 089 | invariant | **Slab allocation count**: ≤1 alloc per top-level `UnmarshalCodec` for messages with string/bytes fields            |
| 090 | invariant | **Pre-scan capacity hint**: cold-path `UnmarshalCodec` of `repeated MsgType` allocates the slice with exact capacity |
| 091 | invariant | **Warm-path slice growth**: a primed receiver decoding a larger payload grows the existing slice rather than reallocating |
| 092 | invariant | **Corruption liveness**: every prefix and every single-byte-flip of a valid marshal must not panic (errors allowed) |
| 093 | invariant | **Forward compatibility**: unknown fields with valid wire types are skipped without error                           |
| 094 | invariant | **`MarshalToCodec` is zero-alloc** in steady state for non-map types; map-bearing types declare a per-consumer ceiling via `Spec.MarshalToAllocsMax` |

## Design notes (Phase 2)

### Spec design

`codectest.Spec[T]` is field-driven and category-bucketed: required field
is `Sample`; everything else is opt-in. Categories explicitly reflect the
proto3 wire-format taxonomy so the runner can drive the right error path
per field number:

- `ScalarVarintFields` — varint-element scalars (REQ-051 surface)
- `PackedVarintFields` / `Fixed64Fields` / `Fixed32Fields` — packed-eligible
  repeated fields, split by element wire type so the unpacked-alternate
  test (REQ-010) sends a wire-correct body
- `MapFields` — drives `AssertCorruptMapEntryValue`
- `RepeatedMessageFields` — drives the prescan-skip-all-wire-types
  exercise (REQ-090)
- `WKTFields` — Timestamp/Duration corruption probe
- `FixedLenBytesFields` — three-branch corruption (REQs 057-058)
- `MarshalToAllocsMax` — per-consumer alloc ceiling (REQ-094)

`Variants` exists to discriminate-by-shape types (oneofs): one entry per
branch ensures `AllFieldsWireTypeMismatch` observes every declared field
on the wire across the sample plus variants.

### Test-double taxonomy

Codec is a pure leaf. There are no ports to fake, so the
simulator/stub/chaos taxonomy from the testing standard does not apply.
The closest analogue is `codectest.StartContract` which is an alloc/
latency-gating *helper*, not a double.

### Fixture design

Integration fixtures (`lang/go/integration/fixture.go`) are domain-named
proto messages exercising every codegen path:

- `Fixture` — scalar mix + repeated string + bytes + Digest
- `Patch` — sparse struct with sfixed64 and fixed-len digest
- `Evidence` — string-heavy with repeated strings + Digest
- `Minimal` — single-string baseline (naive slab path)
- `NumericOnly` — no string fields (no-slab path, optional-pointer pooling)
- `PackedZigzag` — packed sint32/sint64 elements
- `Container` / `ValueContainer` — singular + repeated nested messages,
  pointer vs value semantics
- `Tree` — self-referential (forces `[]*Tree`)
- `MapHolder` — `map<string,V>` with two value types
- `TimeHolder` — WKT (`Timestamp` + `Duration`)
- `BytesPool` — `(codec.keep_capacity)` source-compat survivor
- `OneofPayload` — non-synthetic oneof with five branches
- `External` / `CrossContainer` (in `lang/go/integration/external/`) —
  cross-package nested message resolution

### State-machine PBT

Not applied. Codec stateful aspects (pointer pooling, capacity
preservation across resets) are tested imperatively in
`TestX_PointerPooling_AcrossResets` / `TestX_KeepCapacity_*` —
state-machine PBT would be over-engineered for the linear "marshal → reset →
unmarshal" sequences these aspects are sensitive to.

## Evidence (Phase 3)

| Layer                                | Coverage                                                                                                                  |
|--------------------------------------|---------------------------------------------------------------------------------------------------------------------------|
| Generated code (`*.codec.go`)        | **100%** per-file, gated by `make coverage-gate`                                                                          |
| Runtime (`lang/go/codec/`)           | 90%+ (bench-only paths excluded by `go test -cover`); mutation-tested at **57 KILLED + 5 documented equivalents = 100% effective** |
| Analyzer (`internal/core/`)          | 85% direct + indirect via integration; mutation-tested at **76 KILLED + 19 documented equivalents = 100% effective**       |
| Generator emission (`internal/lang/golang/`) | exercised only via integration roundtrip; not yet directly mutation-testable (gremlins can't re-invoke `make generate`) |
| Bench baseline                       | pinned at `.bench-baseline/main.txt`; `make bench-compare` fails on any alloc regression or >5% wall-time regression       |
| Differential testing                 | none vs `google.golang.org/protobuf` (out of scope per project memo); `AssertCrossFormatConsistency` provides JSON parity   |
| State-machine PBT                    | not applied (codec is mostly stateless; sequencing tested imperatively)                                                    |
| Deterministic simulation             | not applicable (no distributed state)                                                                                      |

## REQ coverage (Phase 4)

Format: `REQ-PKG-CODEC-NNN` → covering test(s). When a test name appears
multiple times, the most direct cite is given.

### Contract REQs

| REQ | Covering test(s)                                                                                                          |
|-----|---------------------------------------------------------------------------------------------------------------------------|
| 001 | `AssertRoundtrip` (size/buf invariant), `AssertMarshalToCodec` happy path                                                 |
| 002 | `AssertMarshalToCodec`, `RunBenchSuite` Codec/MarshalTo + StartContract `AllocsMax(0)`                                    |
| 003 | exercised via `*.codec.go` calls from cross-package nested marshal (`integration.CrossContainer`)                         |
| 004 | every `RunSuite` Roundtrip subtest                                                                                        |
| 005 | `TestContainer/SlabCorrectness preserves nested-string offsets` (fixture_test.go)                                         |
| 006 | `AssertRoundtrip` (size accuracy), `AssertReset` (post-reset SizeCodec()==0)                                              |
| 007 | `AssertReset`, `TestNumericOnly/PointerPooling`, `TestBytesPool/KeepCapacity`, `TestMapHolder/KeepCapacity`               |
| 008 | `AssertRoundtrip` line `len(buf) != size`                                                                                 |
| 009 | every `RunSuite` Roundtrip subtest with `PackedVarintFields` set (e.g. `specPackedZigzag`)                                |
| 010 | `AssertPackedAcceptsUnpacked` (per spec.PackedVarintFields/Fixed64Fields/Fixed32Fields)                                   |
| 011 | every spec with `ScalarVarintFields` populated; e.g. `specFixture` Kind/Status casts                                      |
| 012 | `AssertCorruptFixedLenBytes` happy + 3 error branches                                                                     |
| 013 | `TestAnalyzeField_CodecFieldOverride_TargetName` (analyzer-side); roundtrip on every fixture using `(codec.field)`        |
| 014 | `Container.Inner` field shape; verified via `TestAnalyzeField_SameFileMessageRef_LeavesProtoFileEmpty`                    |
| 015 | `ValueContainer.Items` shape; `TestValueContainer/Codec/Roundtrip`                                                        |
| 016 | `TestAnalyzeField_SelfReference_ForcesUsePointer` + `Tree` integration roundtrip                                          |
| 017 | `TestAnalyzeField_NonSelfRef_RespectsUsePointerFalse` + `CrossContainer.PtrItems` roundtrip                               |
| 018 | enum casts in every spec with `(codec.cast)`; e.g. `Status` field on `Fixture`                                            |
| 019 | `TestAnalyzeMessage_OneofWithoutConfigIsRejected`, `TestAnalyzeMessage_WithOneofConfig_PopulatesOneofs`                   |
| 020 | `OneofPayloadKind` enum values match field numbers in `fixture.proto`; `TestOneofPayload/Codec/Roundtrip`                  |
| 021 | every `OneofPayload` variant in `RunSuite` exercises the unconditional branch emit                                        |
| 022 | every `OneofPayload` variant; `Kind` survives roundtrip via `Roundtrip` subtest                                           |
| 023 | `AssertReset` over `OneofPayload` variants (Kind included in zero-state check)                                            |
| 024 | `NumericOnly.H/I/J` (`*int32`, `*bool`, `*Fixed64` synthetic-oneof representation); `TestAnalyzeMessage_SyntheticOneofAllowed` |
| 025 | architectural — every Roundtrip after `valid` buffer is mutated would fail otherwise; explicit doc in `architecture.md` § Memory safety |
| 026 | `AssertWireStable` (PBT subtest under `Generator`-equipped specs); `MapHolder` Roundtrip (sample maps decode in stable order) |
| 027 | `TestBoolMapHolder/BoolKeysEncodeFalseBeforeTrue` (asserts wire-order; `BoolMapHolder` fixture in fixture.proto)            |
| 028 | `make verify-deterministic-gen` (CI gate)                                                                                 |
| 029 | `AssertNilSafe`                                                                                                           |

### Failure-mode REQs

| REQ | Covering test(s)                                                                                                           |
|-----|----------------------------------------------------------------------------------------------------------------------------|
| 050 | `AssertCorruptTag`                                                                                                         |
| 051 | `AssertCorruptScalarVarint` per `ScalarVarintFields`; `AssertCorruptPackedBody`                                            |
| 052 | `AssertMarshalToShortBuffer`, `TestSkipField_LenDelimited_TruncatedPayload`, `AssertPackedAcceptsUnpackedCorrupt` (wt 1/5) |
| 053 | `TestSkipField_Fixed64_TooShort`, `TestSkipField_Fixed32_TooShort`, `AssertCorruptFixedWidth`                              |
| 054 | `AssertMarshalToShortBuffer`                                                                                               |
| 055 | `AssertAllFieldsWireTypeMismatch` (every wrong wire type per declared field)                                               |
| 056 | `AssertUnknownFieldInvalidWireType`, `TestSkipField_UnknownWireType`                                                       |
| 057 | `AssertCorruptFixedLenBytes` case B (`ErrInvalidLength`)                                                                   |
| 058 | `AssertCorruptFixedLenBytes` case C (body too short)                                                                       |
| 059 | architectural — `TestSkipField_LenDelimited_OffByOnePayload` and `AssertCorruptFixedLenBytes` cover the bounds-check side  |
| 060 | `TestAnalyzeField_FixedLenZero_Errors`                                                                                     |
| 061 | `TestAnalyzeField_FixedLenOnString_Errors`                                                                                 |
| 062 | `TestAnalyzeField_CastOnMessage_Errors`                                                                                    |
| 063 | `TestAnalyzeField_InvalidCastIdent_Errors`                                                                                 |
| 064 | `TestResolveCast_UnresolvedAlias_Errors`                                                                                   |
| 065 | `TestAnalyzeMessage_OneofWithoutConfigIsRejected`                                                                          |
| 066 | `TestAnalyzeMessage_OneofConfigValidation` (table test covers missing name / missing discriminator / missing cast)         |
| 067 | `TestAnalyzeField_ErrorMessages_NotDoublePrefixed`                                                                         |

### Invariant REQs

| REQ | Covering test(s)                                                                                                          |
|-----|---------------------------------------------------------------------------------------------------------------------------|
| 080 | every `RunSuite` Roundtrip subtest                                                                                        |
| 081 | `AssertWireStable` (PBT subtest); `RunFuzzSuite`                                                                          |
| 082 | `AssertReset` (size + marshal-len assertions)                                                                             |
| 083 | `AssertCrossFormatConsistency`                                                                                            |
| 084 | `AssertWireSmallerThanJSON`                                                                                               |
| 085 | `TestNumericOnly/PointerPooling reuses optional-pointer slots across resets`                                              |
| 086 | `TestBytesPool/KeepCapacity reuses Payload backing array across resets`                                                   |
| 087 | `TestMapHolder/KeepCapacity preserves map bucket storage across reset`                                                    |
| 088 | warm-path benches (`Benchmark<T>/PooledUnmarshal`) at 0-1 allocs/op confirm no per-element allocation                     |
| 089 | warm-path benches; `BenchmarkContainer/PooledUnmarshal` shows 1 alloc (the slab)                                          |
| 090 | `TestContainer/PreScanCapacityHint sizes Children to wire element count`                                                  |
| 091 | `AssertWarmPathGrowth` (per `Spec.Grower`)                                                                                |
| 092 | `AssertCorruption` (every prefix + every single-byte-flip)                                                                |
| 093 | `AssertUnknownFieldSkipped`, `AssertPrescanSkipsAllWireTypes` (all four valid wire types)                                 |
| 094 | `RunBenchSuite` Codec/MarshalTo wrapped in `StartContract(b).AllocsMax(spec.MarshalToAllocsMax)`                          |

## REQ coverage status

- **Total REQs:** **62** (29 contract + 18 failure-mode + 15 invariant)
- **Direct test coverage:** **62/62**

## Follow-ups

These remain open beyond the per-REQ table — larger work items the
audit surfaced rather than per-line gaps.

1. **Generator emission mutation testing**: `internal/lang/golang/` is
   currently invisible to gremlins because tests don't re-invoke
   `make generate` per mutation. Adding golden-file emission tests in
   `internal/lang/golang/` would unlock the foundation-tier mutation
   gate for the generator code itself.
2. **Latency contracts** (REQ-094 sibling): currently only `AllocsMax`
   ceilings are declared per consumer. Adding `LatencyMax` ceilings on
   `MarshalTo` per fixture would close the latency-regression gap that
   benchstat currently catches statistically rather than as a hard gate.

## Exceptions

- **No wire-compat differential testing vs `google.golang.org/protobuf`.**
  Project decision (memory pinned 2026-04-19): JSON cross-format +
  property-based fuzz + roundtrip are deemed sufficient for v1. Revisit
  if a real consumer surfaces a wire-compat regression.
- **No state-machine PBT.** Codec is mostly stateless; the few stateful
  aspects (pointer pooling, capacity preservation) are tested
  imperatively. Re-evaluate if a future feature introduces multi-step
  invariants that linear tests can't reasonably express.

## References

- [`docs/architecture.md`](../../architecture.md) — language-neutral design rationale
- [`docs/generators/go.md`](../../generators/go.md) — Go-specific code emission and testing API
- [`Makefile`](../../../Makefile) — `make test`, `make test-mutation`, `make coverage-gate`,
  `make bench-compare`, `make verify-deterministic-gen`
- [`.bench-baseline/main.txt`](../../../.bench-baseline/main.txt) — pinned bench reference
- Project memory: `Wire-compat tests scoped out`, `v1 allocation model is the terminus`,
  `Mutation kill-rate bar is 100%`
