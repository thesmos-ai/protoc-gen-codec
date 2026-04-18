# Go Codec Generator Gap Closure — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every gap identified in the 2026-04-18 gap analysis of `protoc-gen-codec-go`, bringing the Go emitter to a v1-worthy state and preparing `internal/core` to host future emitters (TypeScript, Rust).

**Architecture:** Six ordered phases. Early phases fix correctness and harden the annotation contract; the middle phase refactors `internal/core` to be language-neutral; later phases add missing proto features on the cleaned-up foundation, then upgrade tests and docs. Each task is TDD: failing test → minimal implementation → passing test → commit. Every structural refactor is landed behind `make test && make test-race` green.

**Tech Stack:** Go 1.23+, `google.golang.org/protobuf/compiler/protogen`, `pgregory.net/rapid`, `buf`, `golangci-lint`.

**Execution constraint:** Work on a feature branch or worktree. Do not push to `main` until the full plan passes `make test && make test-race && make lint`.

---

## Phase Dependency Map

```
Phase 0 (hygiene) ──┐
Phase 1 (bugs) ─────┼──► Phase 2 (hardening) ──► Phase 3 (core refactor) ──► Phase 4 (features)
                    │                                                              │
                    └──────────────────────────────────────────────────────────────┴──► Phase 5 (test discipline) ──► Phase 6 (docs)
```

- Phase 0 and Phase 1 can proceed in parallel; both land before Phase 2.
- Phase 3 (core refactor) **must** land before Phase 4 so new features are implemented on the neutral core.
- Phase 5 installs the law suite, fuzz matrix, alloc tiers, wire-compat tests, and CI gates — enforcing the **100% coverage** policy on generated code. All Phase 4 fixture tests must land inside this discipline.
- Phase 6 is last so documentation reflects the shipped behaviour and testing strategy.

## Quality Gates Enforced by This Plan

Hard stops (any failure blocks merge):

| Gate | Surface | Threshold |
|---|---|---|
| Allocation tier | T0/T1/T2/T3 benchmark tiers | T0: 0 allocs; T1: ≤1; T2: ≤3; T3: ≤1 (architectural contract) |
| Baseline regression | Every benchmark vs `.bench-baseline/main.txt` | Any allocs/op or B/op increase → reject; >5% ns/op increase → reject (baseline refresh requires reviewer sign-off) |
| Fuzz | Every `Fuzz*` target | No panic; 60s per PR, 24h on `main` |
| Coverage | `lang/go/integration/*.codec.go` | **100% line coverage** |
| Determinism | `MarshalCodec(m)`, `make generate` | Byte-identical across invocations |
| Wire compat | Every fixture | Round-trips through `google.golang.org/protobuf` |

---

## Phase 0 — Repo Hygiene

### Task 0.1: Wire `make generate` to the actual generation command

**Why:** `Makefile:1` declares `generate` in `.PHONY` but ships no recipe. `make generate` is a no-op today. Buf generation is run ad-hoc.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/Makefile`

- [ ] **Step 1: Inspect current generation flow**

Run: `cat buf.gen.yaml buf.yaml`
Expected: existing `buf.gen.yaml` references the Go plugin and output directory.

- [ ] **Step 2: Add generate recipe**

Edit `Makefile`: add a `generate` target immediately after `build`:

```make
generate: build
	PATH="$(CURDIR)/bin:$$PATH" buf generate
```

The `build` dependency guarantees the plugin binary is fresh. Prepending `bin` to `PATH` lets `buf` discover `protoc-gen-codec-go` without an absolute path in `buf.gen.yaml`.

- [ ] **Step 3: Ensure `make build` produces `bin/protoc-gen-codec-go`**

Edit the `build` target:

```make
build:
	mkdir -p bin
	$(GO) build -o bin/protoc-gen-codec-go ./cmd/protoc-gen-codec-go/
```

- [ ] **Step 4: Fix `make clean` to target `bin/`**

Replace the `clean` target:

```make
clean:
	rm -rf bin/
```

- [ ] **Step 5: Verify round-trip**

Run: `make clean && make generate && git diff --stat lang/go/integration/fixture.codec.go`
Expected: generation completes with exit code 0; no diff against the current `fixture.codec.go` (or a diff only from already-merged changes).

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -m "build: wire make generate to buf generate and fix bin paths"
```

---

### Task 0.2: Wrap remaining unmarshal errors with field context

**Why:** `gen_unmarshal.go:190-203` returns bare `codec.ErrInvalidWireType` and `codec.ErrInvalidLength` without field number context, while varint and short-buffer errors include `"field %d: %w"`. Inconsistent error quality; harder to diagnose failures in production.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_unmarshal.go`
- Test: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Write failing test requiring field context on wire-type error**

Add to `fixture_test.go`:

```go
func TestFixture_WrongWireType_IncludesFieldNumber(t *testing.T) {
	t.Parallel()
	// field 9 (Tags, repeated string, wire type 2) sent as varint
	data := []byte{0x48, 0x00}
	var f integration.Fixture
	err := f.UnmarshalCodec(data)
	if !stderrors.Is(err, codec.ErrInvalidWireType) {
		t.Fatalf("expected ErrInvalidWireType, got %v", err)
	}
	if !strings.Contains(err.Error(), "field 9") {
		t.Fatalf("error should include field number, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test and verify it fails**

Run: `go test ./lang/go/integration/ -run TestFixture_WrongWireType_IncludesFieldNumber -v`
Expected: FAIL with "error should include field number".

- [ ] **Step 3: Add helper for consistent wrapping**

In `gen_unmarshal.go`, above the existing `emitErrVarint`:

```go
func emitErrWireType(g *protogen.GeneratedFile, fieldNum int32) {
	g.P("return ", identFmtErrorf, `("field %d: %w", `, fieldNum, ", ", identErrInvalidWireType, ")")
}

func emitErrFixedLen(g *protogen.GeneratedFile, fieldNum int32) {
	g.P("return ", identFmtErrorf, `("field %d: %w", `, fieldNum, ", ", identErrInvalidLength, ")")
}
```

- [ ] **Step 4: Replace bare returns at each callsite**

In `generateFieldUnmarshal`, replace:

```go
g.P("return ", identErrInvalidWireType)
```

with:

```go
emitErrWireType(g, f.ProtoNum)
```

Same for `generateRepeatedFieldUnmarshal`. For `ErrInvalidLength` at line 202 and 314, use `emitErrFixedLen(g, f.ProtoNum)`.

- [ ] **Step 5: Regenerate and re-run test**

Run: `make generate && go test ./lang/go/integration/ -run TestFixture_WrongWireType_IncludesFieldNumber -v`
Expected: PASS.

- [ ] **Step 6: Run full test suite to confirm no regressions**

Run: `make test`
Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/lang/golang/gen_unmarshal.go lang/go/integration/fixture.codec.go lang/go/integration/fixture_test.go
git commit -m "feat(go): wrap wire-type and fixed-len errors with field number context"
```

---

## Phase 1 — Critical Correctness Bugs

### Task 1.1: Investigate and fix the checked-in rapid `.fail` reproductions

**Why:** Two `.fail` files exist (`TestFixture_Roundtrip_PBT` and `TestEvidence_Roundtrip_PBT` dated 2026-04-18). These will reproduce on re-run and represent an active correctness bug. Root cause may differ from the gap-analysis hypothesis; do not pre-judge.

**Files:**
- Investigate: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/testdata/rapid/**/*.fail`
- Likely modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_unmarshal.go`
- Test: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Reproduce the failure**

Run: `go test ./lang/go/integration/ -run 'TestFixture_Roundtrip_PBT|TestEvidence_Roundtrip_PBT' -v`
Expected: FAIL with a specific seed and `want:`/`got:` diff. Save the seed printed in the failure.

- [ ] **Step 2: Capture the minimal failing input**

Rapid prints the shrunk inputs. Extract the exact `Fixture` / `Evidence` value that fails. Add a focused unit test that asserts the failure deterministically:

```go
func TestFixture_Roundtrip_ReproFailFile(t *testing.T) {
	t.Parallel()
	f := integration.Fixture{
		// paste fields from .fail file
	}
	codec.AssertRoundtrip[integration.Fixture](t, f)
}
```

- [ ] **Step 3: Run the new test and confirm it reproduces the bug**

Run: `go test ./lang/go/integration/ -run TestFixture_Roundtrip_ReproFailFile -v`
Expected: FAIL.

- [ ] **Step 4: Manually hex-dump marshal output and trace through generated unmarshal**

Add a temporary debug test that prints `data, _ := f.MarshalCodec(); fmt.Printf("%x\n", data)` and inspect field-by-field. Compare against `generateSmartSlab` in `gen_unmarshal.go:152-178` pre-scan and the main decode loop. Identify where the decoded value diverges from the source.

Candidate root causes to investigate in order:
  1. Interaction between `slabSmart`'s pre-scan and repeated-string fields (the slab must receive *every* instance of a repeated string).
  2. Empty-string handling (`len == 0` inside slab: does `slabOff` advance correctly when the same field number appears multiple times?).
  3. Non-ASCII UTF-8 in strings (invalid `strings.Builder` writes — unlikely but worth ruling out).
  4. Subscribe-array retention: `dataStr := string(data)` in `slabNaive` creates a single string allocation; subslices share it. Fine unless the wire data is later mutated — ensure the test doesn't do that.

- [ ] **Step 5: Write a minimal regression test encoding the root cause**

Example shape (exact content depends on root cause):

```go
func TestFixture_Roundtrip_ReorderedStringFields(t *testing.T) {
	t.Parallel()
	f := integration.Fixture{
		ID:   "",                 // empty first string
		Tags: []string{"a", "", "c"}, // empty element in repeated string
	}
	codec.AssertRoundtrip[integration.Fixture](t, f)
}
```

- [ ] **Step 6: Fix the bug in `gen_unmarshal.go`**

Apply the minimal change that makes the regression test pass. Do not refactor beyond the fix. Re-run `make generate` to regenerate `lang/go/integration/fixture.codec.go`.

- [ ] **Step 7: Verify all tests pass including PBT and the new regression tests**

Run: `make test`
Expected: all PASS.

- [ ] **Step 8: Delete the stale `.fail` files**

```bash
rm -rf lang/go/integration/testdata/rapid
```

These files capture the failing seed; once fixed, they are stale.

- [ ] **Step 9: Commit**

```bash
git add internal/lang/golang/gen_unmarshal.go lang/go/integration/fixture.codec.go lang/go/integration/fixture_test.go
git rm -r lang/go/integration/testdata/rapid
git commit -m "fix(go): resolve slab-mode unmarshal bug captured in rapid .fail files"
```

---

### Task 1.2: Implement zigzag encoding/decoding for `sint32`/`sint64`

**Why:** `gen_unmarshal.go:469-484` casts raw varint values directly to signed int without zigzag decode. `gen_marshal.go:73` uses `uint64(accessor)` without zigzag encode. Negative values in `sint*` fields round-trip incorrectly. Also affects `gen_size.go:49` (size computed on pre-zigzag value).

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/codec/wire.go` (add zigzag helpers)
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/codegen.go` (new idents)
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_marshal.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_unmarshal.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_size.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture.proto`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture.go`
- Test: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Add zigzag field to `NumericOnly` fixture proto**

Edit `lang/go/integration/fixture.proto`, append to the `NumericOnly` message:

```protobuf
  sint32 f = 6 [(codec.field) = "F"];
  sint64 g = 7 [(codec.field) = "G"];
```

- [ ] **Step 2: Add matching fields to the Go struct**

Edit `lang/go/integration/fixture.go`:

```go
type NumericOnly struct {
	A uint32  `json:"a"`
	B uint64  `json:"b"`
	C int64   `json:"c"`
	D Fixed64 `json:"d"`
	E bool    `json:"e"`
	F int32   `json:"f"`
	G int64   `json:"g"`
}
```

- [ ] **Step 3: Write failing test with negative values**

Add to `fixture_test.go`:

```go
func TestNumericOnly_Zigzag_NegativeValues(t *testing.T) {
	t.Parallel()
	n := integration.NumericOnly{F: -42, G: -9_000_000_000}
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}

func TestNumericOnly_Zigzag_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := integration.NumericOnly{
			F: rapid.Int32().Draw(t, "f"),
			G: rapid.Int64().Draw(t, "g"),
		}
		codec.AssertRoundtrip[integration.NumericOnly](t, n)
	})
}
```

- [ ] **Step 4: Add zigzag helpers to runtime**

Edit `codec/wire.go`:

```go
// ZigzagEncode32 encodes a signed int32 to a uint32 varint using zigzag.
func ZigzagEncode32(v int32) uint32 {
	return uint32(v<<1) ^ uint32(v>>31)
}

// ZigzagEncode64 encodes a signed int64 to a uint64 varint using zigzag.
func ZigzagEncode64(v int64) uint64 {
	return uint64(v<<1) ^ uint64(v>>63)
}

// ZigzagDecode32 decodes a zigzag-encoded uint32 varint to signed int32.
func ZigzagDecode32(v uint32) int32 {
	return int32(v>>1) ^ -int32(v&1)
}

// ZigzagDecode64 decodes a zigzag-encoded uint64 varint to signed int64.
func ZigzagDecode64(v uint64) int64 {
	return int64(v>>1) ^ -int64(v&1)
}
```

- [ ] **Step 5: Add runtime unit tests for zigzag**

Create `codec/wire_test.go`:

```go
package codec

import "testing"

func TestZigzag64_RoundtripSamples(t *testing.T) {
	t.Parallel()
	cases := []int64{0, 1, -1, 2, -2, 2147483647, -2147483648, 1<<62, -(1<<62)}
	for _, v := range cases {
		if got := ZigzagDecode64(ZigzagEncode64(v)); got != v {
			t.Fatalf("zigzag64(%d): got %d", v, got)
		}
	}
}

func TestZigzag32_RoundtripSamples(t *testing.T) {
	t.Parallel()
	cases := []int32{0, 1, -1, 2, -2, 2147483647, -2147483648}
	for _, v := range cases {
		if got := ZigzagDecode32(ZigzagEncode32(v)); got != v {
			t.Fatalf("zigzag32(%d): got %d", v, got)
		}
	}
}
```

- [ ] **Step 6: Register new idents in codegen**

Edit `internal/lang/golang/codegen.go` — add to the `var (...)` block:

```go
identZigzagEncode32 = protogen.GoIdent{GoName: "ZigzagEncode32", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identZigzagEncode64 = protogen.GoIdent{GoName: "ZigzagEncode64", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identZigzagDecode32 = protogen.GoIdent{GoName: "ZigzagDecode32", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identZigzagDecode64 = protogen.GoIdent{GoName: "ZigzagDecode64", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
```

- [ ] **Step 7: Update marshal for sint fields**

In `gen_marshal.go:generateFieldMarshal`, before the `f.Wire == core.WireVarint` switch arm, branch on `f.ProtoKind`:

```go
case f.Wire == core.WireVarint:
	if f.ProtoKind == protoreflect.BoolKind {
		g.P("if ", accessor, " {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("buf[n] = 1")
		g.P("n++")
	} else if f.ProtoKind == protoreflect.Sint32Kind {
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", identZigzagEncode32, "(int32(", accessor, "))))")
	} else if f.ProtoKind == protoreflect.Sint64Kind {
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],", identZigzagEncode64, "(int64(", accessor, ")))")
	} else {
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", accessor, "))")
	}
	g.P("}")
```

Apply the same pattern to `generateRepeatedFieldMarshal` for the packed-varint arm (`f.Wire == core.WireVarint`): the packed inner loop must zigzag-encode each element.

- [ ] **Step 8: Update unmarshal cast logic**

In `gen_unmarshal.go`, change `defaultCast`:

```go
func defaultCast(k protoreflect.Kind, varName string) string {
	switch k {
	case protoreflect.Int32Kind:
		return fmt.Sprintf("int32(%s)", varName)
	case protoreflect.Sint32Kind:
		// Zigzag is applied at the callsite, so defaultCast receives a decoded int32-sized value.
		return varName
	case protoreflect.Uint32Kind, protoreflect.EnumKind:
		return fmt.Sprintf("uint32(%s)", varName)
	case protoreflect.Int64Kind:
		return fmt.Sprintf("int64(%s)", varName)
	case protoreflect.Sint64Kind:
		return varName
	case protoreflect.Uint64Kind:
		return varName
	case protoreflect.BoolKind:
		return fmt.Sprintf("%s != 0", varName)
	default:
		return varName
	}
}
```

Modify `generateFieldUnmarshal` for `WireVarint` to apply zigzag before cast:

```go
case f.Wire == core.WireVarint:
	g.P("v, n := ", identDecodeVarint, "(data[i:])")
	g.P("if n < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += n")
	switch {
	case f.ProtoKind == protoreflect.BoolKind:
		g.P(accessor, " = v != 0")
	case f.ProtoKind == protoreflect.Sint32Kind:
		g.P(accessor, " = ", castExpr(g, f, fmt.Sprintf("%s(uint32(v))", identZigzagDecode32)))
	case f.ProtoKind == protoreflect.Sint64Kind:
		g.P(accessor, " = ", castExpr(g, f, fmt.Sprintf("%s(v)", identZigzagDecode64)))
	default:
		g.P(accessor, " = ", castExpr(g, f, "v"))
	}
```

Apply the same branch to the packed-varint path in `generateRepeatedFieldUnmarshal`.

- [ ] **Step 9: Update size computation for sint fields**

Edit `gen_size.go:generateFieldSize` for `WireVarint`:

```go
case f.Wire == core.WireVarint:
	if f.ProtoKind.String() == "bool" {
		g.P("if ", accessor, " {")
		g.P("n += ", ts+1)
	} else if f.ProtoKind == protoreflect.Sint32Kind {
		g.P("if ", accessor, " != 0 {")
		g.P("n += ", ts, " + ", identSov, "(uint64(", identZigzagEncode32, "(int32(", accessor, "))))")
	} else if f.ProtoKind == protoreflect.Sint64Kind {
		g.P("if ", accessor, " != 0 {")
		g.P("n += ", ts, " + ", identSov, "(", identZigzagEncode64, "(int64(", accessor, ")))")
	} else {
		g.P("if ", accessor, " != 0 {")
		g.P("n += ", ts, " + ", identSov, "(uint64(", accessor, "))")
	}
	g.P("}")
```

Add `"google.golang.org/protobuf/reflect/protoreflect"` import to `gen_size.go` if not already present.

Apply the same to the packed path in `generateRepeatedFieldSize`.

- [ ] **Step 10: Regenerate and run tests**

Run: `make generate && make test`
Expected: all PASS including the new zigzag tests.

- [ ] **Step 11: Commit**

```bash
git add codec/wire.go codec/wire_test.go internal/lang/golang/codegen.go internal/lang/golang/gen_marshal.go internal/lang/golang/gen_unmarshal.go internal/lang/golang/gen_size.go lang/go/integration/fixture.proto lang/go/integration/fixture.go lang/go/integration/fixture.codec.go lang/go/integration/fixture_test.go
git commit -m "feat(go): implement zigzag for sint32/sint64 marshal, size, unmarshal"
```

---

### Task 1.3: Guard `MarshalToCodec` against short buffers

**Why:** `gen_marshal.go:30-43` writes into the caller's buffer with no length check. Passing a buffer shorter than `SizeCodec()` panics with index-out-of-range. The runtime should return `ErrBufferTooShort` instead.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_marshal.go`
- Test: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Write failing test**

Add to `fixture_test.go`:

```go
func TestFixture_MarshalToCodec_ShortBuffer(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	size := f.SizeCodec()
	short := make([]byte, size-1)
	_, err := f.MarshalToCodec(short)
	if !stderrors.Is(err, codec.ErrBufferTooShort) {
		t.Fatalf("expected ErrBufferTooShort, got %v", err)
	}
}
```

- [ ] **Step 2: Run test and verify it fails with panic or wrong error**

Run: `go test ./lang/go/integration/ -run TestFixture_MarshalToCodec_ShortBuffer -v`
Expected: FAIL or panic.

- [ ] **Step 3: Emit a size guard at the top of `MarshalToCodec`**

Edit `gen_marshal.go:generateMarshalToCodec`:

```go
func generateMarshalToCodec(g *protogen.GeneratedFile, info *core.MessageInfo) {
	g.P("func (m *", info.GoType, ") MarshalToCodec(buf []byte) (int, error) {")
	g.P("if m == nil {")
	g.P("return 0, nil")
	g.P("}")
	g.P("if len(buf) < m.SizeCodec() {")
	g.P("return 0, ", identErrBufferTooShort)
	g.P("}")
	g.P("n := 0")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldMarshal(g, f)
	}

	g.P("return n, nil")
	g.P("}")
}
```

**Note:** this introduces a `SizeCodec()` call at the top of `MarshalToCodec`, which re-walks the struct. Acceptable for the safety guarantee; if profiling shows this is hot, a follow-up can add a caller-provided `MarshalToCodecUnchecked` variant. Not part of this task.

- [ ] **Step 4: Regenerate and re-run tests**

Run: `make generate && go test ./lang/go/integration/ -run TestFixture_MarshalToCodec_ShortBuffer -v`
Expected: PASS.

- [ ] **Step 5: Run full suite**

Run: `make test`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/lang/golang/gen_marshal.go lang/go/integration/fixture.codec.go lang/go/integration/fixture_test.go
git commit -m "fix(go): return ErrBufferTooShort from MarshalToCodec when buffer is undersized"
```

---

### Task 1.4: Reset scalar fields at the top of `UnmarshalCodec`

**Why:** `gen_unmarshal.go:107-114` only zero-initialises repeated and variable-length bytes fields before decoding. Scalar fields from a prior use of a pooled receiver survive if that field is absent from the new wire data. This violates the "pool-friendly" contract (docs promise "identical semantics" regardless of receiver reuse).

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_unmarshal.go`
- Test: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Write failing test**

Add to `fixture_test.go`:

```go
func TestFixture_UnmarshalCodec_ClearsStalePopulatedScalars(t *testing.T) {
	t.Parallel()
	// Marshal a Fixture with only ID set; unmarshal into a populated receiver.
	// Expect the receiver's unset scalar fields (Kind, Score, etc.) to be zero.
	minimal := integration.Fixture{ID: "new"}
	buf, err := minimal.MarshalCodec()
	if err != nil {
		t.Fatal(err)
	}
	receiver := integration.Fixture{
		ID: "stale", Kind: 99, Score: 7, Sequence: 5, Enabled: true,
		Timestamp: 123, Status: integration.StatusRunning,
	}
	if err := receiver.UnmarshalCodec(buf); err != nil {
		t.Fatal(err)
	}
	if receiver.Kind != 0 || receiver.Score != 0 || receiver.Sequence != 0 ||
		receiver.Enabled || receiver.Timestamp != 0 || receiver.Status != 0 {
		t.Fatalf("stale scalars survived unmarshal: %+v", receiver)
	}
	if receiver.ID != "new" {
		t.Fatalf("new value not applied: ID=%q", receiver.ID)
	}
}
```

- [ ] **Step 2: Run test — expect fail**

Run: `go test ./lang/go/integration/ -run TestFixture_UnmarshalCodec_ClearsStalePopulatedScalars -v`
Expected: FAIL.

- [ ] **Step 3: Reset every serialized field at the top of `UnmarshalCodec`**

Replace the current limited-reset block in `gen_unmarshal.go:generateUnmarshalCodec` (lines 107-114) with a full reset:

```go
for i := range info.Fields {
	f := &info.Fields[i]
	generateFieldReset(g, f)
}
```

Import the reset logic by calling the existing `generateFieldReset` from `gen_reset.go` directly (they live in the same package). This reuses one implementation of "zero this field."

- [ ] **Step 4: Verify generated code handles `ResetInternal`**

Do **not** invoke `ResetInternal()` here — the contract is that `UnmarshalCodec` clears serialized state, not internal state. If a user wants full reset semantics before unmarshal, they call `ResetCodec` first.

- [ ] **Step 5: Regenerate and re-run tests**

Run: `make generate && make test`
Expected: all PASS.

- [ ] **Step 6: Update `TestFixture_PooledReuse` if normalization is no longer needed**

Check `fixture_test.go:431-442` and `normalizeFixture`. With the reset, pooled reuse should produce exact equality without normalization. Simplify the test if so.

- [ ] **Step 7: Commit**

```bash
git add internal/lang/golang/gen_unmarshal.go lang/go/integration/fixture.codec.go lang/go/integration/fixture_test.go
git commit -m "fix(go): UnmarshalCodec resets all serialized fields before decoding"
```

---

## Phase 2 — Generation-Time Hardening

### Task 2.1: Fail generation when `codec.type` is missing on a message

**Why:** `analysis.go:48-50` silently returns `nil, nil` for messages without `codec.type`. The architecture doc promises generation-time errors for missing annotations. Silent skip hides schema mistakes.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/codegen.go` (call-site — if it filters on nil today)
- Test: new `internal/core/analysis_test.go`

- [ ] **Step 1: Create internal/core unit-test scaffold**

Create `internal/core/analysis_test.go`:

```go
package core

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// newTestPlugin compiles a set of .proto files using protoc at test time
// and returns the *protogen.Plugin.
func newTestPlugin(t *testing.T, sources map[string]string) *protogen.Plugin {
	t.Helper()
	// Exercise: use protoc via exec, feed the CodeGeneratorRequest back via stdin.
	// For simplicity, we instead build a descriptor pool in-process.
	// See appendix for full helper — for this initial test we use a canned descriptor.
	t.Skip("descriptor harness not yet built; see Task 2.7")
	return nil
}

func TestAnalyzeMessage_MissingCodecType_Errors(t *testing.T) {
	t.Skip("pending Task 2.7 harness")
}
```

This test is deliberately skipped until Task 2.7 builds the descriptor test harness; we add the test stub so later wiring reminds us.

- [ ] **Step 2: Add non-harness unit test that exercises `messageGoType` directly on a fake descriptor**

In the same file, add a test using a manually-constructed `descriptorpb`:

```go
func TestMessageGoType_MissingReturnsEmpty(t *testing.T) {
	t.Parallel()
	// A MessageDescriptorProto with no options should produce empty go-type.
	md := &descriptorpb.DescriptorProto{
		Name: proto.String("M"),
	}
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("t.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("t"),
		MessageType: []*descriptorpb.DescriptorProto{md},
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"t.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
	}
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatal(err)
	}
	msg := plugin.Files[0].Messages[0]
	if got := messageGoType(msg); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
```

- [ ] **Step 3: Change `AnalyzeMessage` to return an error instead of nil-nil**

Edit `internal/core/analysis.go:43-64`:

```go
func AnalyzeMessage(
	msg *protogen.Message,
	fileMap map[string]*protogen.File,
	file *protogen.File,
) (*MessageInfo, error) {
	goType := messageGoType(msg)
	if goType == "" {
		return nil, fmt.Errorf(
			"message %s in %s: missing (codec.type) option",
			msg.Desc.Name(), file.Desc.Path(),
		)
	}
	// ... rest unchanged
}
```

- [ ] **Step 4: Update caller in `codegen.go`**

Edit `internal/lang/golang/codegen.go:generateFileImpl`:

Replace the `if info == nil { continue }` block with propagation:

```go
info, err := core.AnalyzeMessage(msg, fileMap, file)
if err != nil {
	return err
}
messages = append(messages, info)
```

- [ ] **Step 5: Add fixture that would have previously been silently skipped**

Create `lang/go/integration/missing_type.proto`:

```protobuf
syntax = "proto3";
package integration;
option go_package = "go.stealthscale.io/protoc-gen-codec/lang/go/integration";

import "codec/options.proto";

message MissingType {
  string id = 1;
}
```

Add a generator-level test that runs `buf generate` and asserts the error. For now, a simpler unit-level test:

Edit `fixture_test.go`:

```go
func TestGenerator_RejectsMessageWithoutCodecType(t *testing.T) {
	// This test is run by Task 2.7's harness; for now, the buf generate
	// invocation in CI will fail, which is the expected behaviour.
	t.Skip("exercised by CI buf generate; unit test arrives with Task 2.7")
}
```

Delete `lang/go/integration/missing_type.proto` so the rest of the test suite still builds. The "real" coverage arrives in Task 2.7.

- [ ] **Step 6: Regenerate existing fixture and run tests**

Run: `make generate && make test`
Expected: all PASS (no new messages added that lack `codec.type`).

- [ ] **Step 7: Commit**

```bash
git add internal/core/analysis.go internal/core/analysis_test.go internal/lang/golang/codegen.go lang/go/integration/fixture_test.go
git commit -m "feat(core): error when proto message lacks (codec.type) annotation"
```

---

### Task 2.2: Reject `codec.fixed_len = 0` and fixed-len on non-bytes fields

**Why:** `analysis.go:35-38`'s `fieldFixedLen` returns 0 both when the annotation is absent and when explicitly set to 0 — a user typo silently disables the guard. Fixed-len is also accepted on string/numeric fields, which produces broken generated code.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/options.go`
- Test: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis_test.go`

- [ ] **Step 1: Change `fieldFixedLen` to return a presence bool**

Edit `internal/core/options.go`:

```go
func fieldFixedLen(field *protogen.Field) (uint32, bool) {
	return extractUint32(field.Desc.Options(), optFixedLen)
}
```

- [ ] **Step 2: Validate at analysis time**

Edit `internal/core/analysis.go:analyzeField`:

Replace:

```go
FixedLen: fieldFixedLen(field),
```

with:

```go
// (inside analyzeField, after building fi)
if v, present := fieldFixedLen(field); present {
	if v == 0 {
		return fi, fmt.Errorf(
			"field %s: (codec.fixed_len) must be > 0",
			field.Desc.Name(),
		)
	}
	if field.Desc.Kind() != protoreflect.BytesKind {
		return fi, fmt.Errorf(
			"field %s: (codec.fixed_len) only valid on bytes fields, got %s",
			field.Desc.Name(), field.Desc.Kind(),
		)
	}
	fi.FixedLen = v
}
```

- [ ] **Step 3: Write failing unit tests**

Add to `internal/core/analysis_test.go`:

```go
func TestAnalyzeField_FixedLenZero_Errors(t *testing.T) {
	t.Parallel()
	// Use a canned descriptor for a bytes field with (codec.fixed_len)=0.
	// See helper scaffold at bottom of this file.
	_, err := runAnalyzeFieldFixture(t, fixedLenZeroFixture)
	if err == nil {
		t.Fatal("expected error for fixed_len=0")
	}
}

func TestAnalyzeField_FixedLenOnString_Errors(t *testing.T) {
	t.Parallel()
	_, err := runAnalyzeFieldFixture(t, fixedLenOnStringFixture)
	if err == nil {
		t.Fatal("expected error for fixed_len on string")
	}
}
```

Add the descriptor-construction helpers at the bottom of the file (referenced in Task 2.7 for the full harness).

- [ ] **Step 4: Run tests — expect pass**

Run: `go test ./internal/core/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/analysis.go internal/core/options.go internal/core/analysis_test.go
git commit -m "feat(core): reject codec.fixed_len=0 and codec.fixed_len on non-bytes fields"
```

---

### Task 2.3: Raise generation errors for unresolved `codec.cast` aliases

**Why:** `analysis.go:126-129`'s `resolveCast` returns the raw dotted string when the package alias is not a direct dependency. The resulting Go code fails at `go build` — the generator should catch it.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Test: `internal/core/analysis_test.go`

- [ ] **Step 1: Change `resolveCast` to return an error**

Edit `internal/core/analysis.go`:

```go
func resolveCast(
	fileMap map[string]*protogen.File,
	file *protogen.File,
	cast string,
) (string, *protogen.GoIdent, error) {
	dotIdx := strings.IndexByte(cast, '.')
	if dotIdx < 0 {
		return cast, nil, nil
	}

	pkgAlias := cast[:dotIdx]
	typeName := cast[dotIdx+1:]

	for _, dep := range file.Proto.GetDependency() {
		depFile, ok := fileMap[dep]
		if !ok {
			continue
		}
		if string(depFile.GoPackageName) == pkgAlias {
			ident := protogen.GoIdent{
				GoName:       typeName,
				GoImportPath: depFile.GoImportPath,
			}
			return "", &ident, nil
		}
	}

	return "", nil, fmt.Errorf(
		"unresolved cast alias %q in file %s: no imported proto has go_package alias %q",
		cast, file.Desc.Path(), pkgAlias,
	)
}
```

- [ ] **Step 2: Propagate error from `analyzeField`**

Edit `internal/core/analysis.go:analyzeField`:

```go
cast := fieldGoCast(field)
fi.Cast = cast
if cast != "" {
	local, ident, err := resolveCast(fileMap, file, cast)
	if err != nil {
		return fi, err
	}
	fi.CastLocal = local
	fi.CastIdent = ident
}
```

- [ ] **Step 3: Failing unit test**

Add to `internal/core/analysis_test.go`:

```go
func TestResolveCast_UnresolvedAlias_Errors(t *testing.T) {
	t.Parallel()
	// Construct a file that imports nothing but references pkg.Type in a cast.
	_, err := runAnalyzeFieldFixture(t, unresolvedCastFixture)
	if err == nil {
		t.Fatal("expected error for unresolved cast alias")
	}
	if !strings.Contains(err.Error(), "unresolved cast alias") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/core/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/analysis.go internal/core/analysis_test.go
git commit -m "feat(core): error on unresolved codec.cast package aliases"
```

---

### Task 2.4: Reject `codec.cast` on message-kind fields

**Why:** `analysis.go:66-92` stores a `codec.cast` on a message field but never uses it. User intent is unclear — better to error than silently ignore.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Test: `internal/core/analysis_test.go`

- [ ] **Step 1: Failing test**

```go
func TestAnalyzeField_CastOnMessage_Errors(t *testing.T) {
	t.Parallel()
	_, err := runAnalyzeFieldFixture(t, castOnMessageFixture)
	if err == nil {
		t.Fatal("expected error for cast on message field")
	}
}
```

- [ ] **Step 2: Add guard in `analyzeField`**

Edit `internal/core/analysis.go`: after reading `cast := fieldGoCast(field)`, before resolution:

```go
if cast != "" && field.Desc.Kind() == protoreflect.MessageKind {
	return fi, fmt.Errorf(
		"field %s: (codec.cast) is not valid on message-type fields",
		field.Desc.Name(),
	)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/core/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/core/analysis.go internal/core/analysis_test.go
git commit -m "feat(core): reject codec.cast on message-kind fields"
```

---

### Task 2.5: Validate `codec.cast` identifier syntax

**Why:** `analysis.go:102-130` accepts any string. A user typo like `codec.cast = "PatchKind "` (trailing whitespace) would produce invalid Go. Catch obviously broken identifiers at generation time.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Test: `internal/core/analysis_test.go`

- [ ] **Step 1: Add identifier validator**

Edit `internal/core/analysis.go`:

```go
// validCastIdent allows letters, digits, underscore, and one dot for pkg.Type.
var validCastIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
```

Add the `regexp` import.

- [ ] **Step 2: Apply in `analyzeField`**

After reading `cast`:

```go
if cast != "" && !validCastIdent.MatchString(cast) {
	return fi, fmt.Errorf(
		"field %s: (codec.cast) = %q is not a valid identifier",
		field.Desc.Name(), cast,
	)
}
```

- [ ] **Step 3: Failing test**

```go
func TestAnalyzeField_InvalidCastIdent_Errors(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"Status ", " Status", "123Status", "pkg..Type", "pkg. Type"} {
		if _, err := runAnalyzeFieldFixture(t, invalidCastIdentFixture(bad)); err == nil {
			t.Fatalf("expected error for cast %q", bad)
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/core/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/analysis.go internal/core/analysis_test.go
git commit -m "feat(core): validate codec.cast identifier syntax"
```

---

### Task 2.6: Surface plugin errors from `protogen.Plugin.Error`

**Why:** `plugin.go:13-25` returns the first error but doesn't use `plugin.Error` to collect multiple issues. One bad annotation aborts before the user sees all problems. Improve the developer loop.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/plugin.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/codegen.go`

- [ ] **Step 1: Change flow to collect errors per file**

Edit `internal/lang/golang/plugin.go`:

```go
func Run() {
	protogen.Options{}.Run(func(plugin *protogen.Plugin) error {
		fileMap := buildFileMap(plugin)
		var firstErr error
		for _, f := range plugin.Files {
			if !f.Generate {
				continue
			}
			if err := generateFile(plugin, f, fileMap); err != nil {
				plugin.Error(fmt.Errorf("%s: %w", f.Desc.Path(), err))
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		return firstErr
	})
}
```

Add the `fmt` import if not already present.

- [ ] **Step 2: Run tests to confirm no regression**

Run: `make test`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/lang/golang/plugin.go
git commit -m "feat(go): collect per-file errors via plugin.Error for better DX"
```

---

### Task 2.7: Build descriptor-based test harness for `internal/core`

**Why:** Tasks 2.1-2.5 stub several tests because constructing proto descriptors by hand is verbose. A reusable helper unlocks thorough core-layer coverage.

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/testutil_test.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis_test.go`

- [ ] **Step 1: Create harness file**

Create `internal/core/testutil_test.go`:

```go
package core

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// fieldFixture describes a single field in a synthetic message.
type fieldFixture struct {
	name     string
	num      int32
	kind     descriptorpb.FieldDescriptorProto_Type
	label    descriptorpb.FieldDescriptorProto_Label
	typeName string       // for message/enum fields
	options  *uninterpretedOptions
}

type uninterpretedOptions struct {
	codecField       string
	codecCast        string
	codecFixedLen    uint32
	hasFixedLen      bool
	codecKeepCap     bool
	hasKeepCap       bool
}

// buildField constructs a FieldDescriptorProto with the given options
// encoded into the "uninterpreted" options so extractString/extractUint32
// pick them up via the same code path protoc would use.
func buildField(f fieldFixture) *descriptorpb.FieldDescriptorProto {
	fd := &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(f.name),
		Number:   proto.Int32(f.num),
		Type:     f.kind.Enum(),
		Label:    f.label.Enum(),
	}
	if f.typeName != "" {
		fd.TypeName = proto.String(f.typeName)
	}
	if f.options != nil {
		fd.Options = encodeFieldOptions(f.options)
	}
	return fd
}

// encodeFieldOptions returns a FieldOptions with codec.* annotations
// placed in the unknown-field region (matching how protoc delivers
// custom options).
func encodeFieldOptions(o *uninterpretedOptions) *descriptorpb.FieldOptions {
	opts := &descriptorpb.FieldOptions{}
	// Build raw unknown-field bytes.
	var raw []byte
	if o.codecField != "" {
		raw = appendString(raw, int32(optGoField), o.codecField)
	}
	if o.codecCast != "" {
		raw = appendString(raw, int32(optGoCast), o.codecCast)
	}
	if o.hasFixedLen {
		raw = appendVarint(raw, int32(optFixedLen), uint64(o.codecFixedLen))
	}
	if o.hasKeepCap {
		v := uint64(0)
		if o.codecKeepCap {
			v = 1
		}
		raw = appendVarint(raw, int32(optKeepCap), v)
	}
	proto.SetExtension(opts, nil, nil) // no-op, ensures initialisation
	// Store raw into the unknown-fields tail.
	opts.ProtoReflect().SetUnknown(raw)
	return opts
}

func appendString(raw []byte, fieldNum int32, val string) []byte {
	tag := uint64(fieldNum)<<3 | 2
	raw = appendUvarint(raw, tag)
	raw = appendUvarint(raw, uint64(len(val)))
	return append(raw, val...)
}

func appendVarint(raw []byte, fieldNum int32, val uint64) []byte {
	tag := uint64(fieldNum)<<3 | 0
	raw = appendUvarint(raw, tag)
	return appendUvarint(raw, val)
}

func appendUvarint(raw []byte, v uint64) []byte {
	for v >= 0x80 {
		raw = append(raw, byte(v)|0x80)
		v >>= 7
	}
	return append(raw, byte(v))
}

// runAnalyzeField compiles a synthetic file containing a single message
// with the given fields and returns the analysed MessageInfo (or error).
func runAnalyzeField(t *testing.T, fields ...fieldFixture) (*MessageInfo, error) {
	t.Helper()
	var fds []*descriptorpb.FieldDescriptorProto
	for _, f := range fields {
		fds = append(fds, buildField(f))
	}
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("t.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("t"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/t")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:  proto.String("M"),
				Field: fds,
				Options: encodeMessageOptions("M"),
			},
		},
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"t.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
	}
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatal(err)
	}
	file := plugin.Files[0]
	fileMap := map[string]*protogen.File{file.Proto.GetName(): file}
	return AnalyzeMessage(file.Messages[0], fileMap, file)
}

func encodeMessageOptions(goType string) *descriptorpb.MessageOptions {
	opts := &descriptorpb.MessageOptions{}
	raw := appendString(nil, int32(optGoType), goType)
	opts.ProtoReflect().SetUnknown(raw)
	return opts
}

// Convenience fixtures used by tests in Tasks 2.1-2.5.
var (
	fixedLenZeroFixture = fieldFixture{
		name:  "ref", num: 1,
		kind:  descriptorpb.FieldDescriptorProto_TYPE_BYTES,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{hasFixedLen: true, codecFixedLen: 0},
	}
	fixedLenOnStringFixture = fieldFixture{
		name: "id", num: 1,
		kind: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{hasFixedLen: true, codecFixedLen: 32},
	}
	unresolvedCastFixture = fieldFixture{
		name: "x", num: 1,
		kind: descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{codecCast: "unknown.Type"},
	}
	castOnMessageFixture = fieldFixture{
		name: "m", num: 1,
		kind: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		typeName: ".t.Inner",
		options: &uninterpretedOptions{codecCast: "Foo"},
	}
)

func invalidCastIdentFixture(cast string) fieldFixture {
	return fieldFixture{
		name: "x", num: 1,
		kind: descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{codecCast: cast},
	}
}

func runAnalyzeFieldFixture(t *testing.T, f fieldFixture) (*MessageInfo, error) {
	return runAnalyzeField(t, f)
}

func init() {
	_ = fmt.Sprintf // silence unused-import warnings in scaffolds
}
```

- [ ] **Step 2: Remove `t.Skip("pending Task 2.7 harness")` from Task 2.1-2.5 tests**

Go through `internal/core/analysis_test.go` and remove the `t.Skip(...)` lines in tests that reference `runAnalyzeFieldFixture`. They should now execute.

- [ ] **Step 3: Run core tests**

Run: `go test ./internal/core/ -v`
Expected: all core tests PASS (the harness tests from Tasks 2.2-2.5 now exercise real descriptors).

- [ ] **Step 4: Commit**

```bash
git add internal/core/testutil_test.go internal/core/analysis_test.go
git commit -m "test(core): descriptor-based harness + enable skipped annotation tests"
```

---

## Phase 3 — Language-Neutral Core Refactor

**Goal:** Remove every `protogen.GoIdent`, `GoName`, `GoType`, and other Go-specific identifier from `internal/core`. Introduce language-neutral primitives that later emitters (TS, Rust) can consume. Push Go-specific decoration into `internal/lang/golang`.

This phase will produce a temporarily broken state in the middle of the work. Land the whole phase as a series of commits on a feature branch; do not release between commits.

### Task 3.1: Introduce neutral `CastRef` type

**Why:** `FieldInfo.CastIdent *protogen.GoIdent` ties `internal/core` to `protogen`. Replace with a neutral `CastRef` that carries enough information for any language emitter to qualify the identifier in its own way.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`

- [ ] **Step 1: Define neutral type**

Add to `internal/core/analysis.go`:

```go
// CastRef identifies a target-language type referenced by a codec.cast
// annotation. Package is the proto file's go_package option (for the Go
// emitter; other emitters may reuse the same field and interpret it per
// their language). Name is the unqualified type name.
type CastRef struct {
	// ProtoFile is the .proto file that declares the go_package/other-lang
	// package alias this cast belongs to. Empty if the cast is in the same
	// file as the referencing field.
	ProtoFile string
	// PackageAlias is the alias the user wrote before the dot, e.g. "hash"
	// in "hash.Digest". Empty for same-file casts.
	PackageAlias string
	// Name is the unqualified cast target, e.g. "Digest" or "Status".
	Name string
}
```

- [ ] **Step 2: Populate from resolver without Go-specific fields**

Rewrite `resolveCast` signature to return `CastRef`:

```go
func resolveCast(
	fileMap map[string]*protogen.File,
	file *protogen.File,
	cast string,
) (CastRef, error) {
	dotIdx := strings.IndexByte(cast, '.')
	if dotIdx < 0 {
		return CastRef{Name: cast}, nil
	}
	pkgAlias := cast[:dotIdx]
	typeName := cast[dotIdx+1:]
	for _, dep := range file.Proto.GetDependency() {
		depFile, ok := fileMap[dep]
		if !ok {
			continue
		}
		if string(depFile.GoPackageName) == pkgAlias {
			return CastRef{
				ProtoFile:    depFile.Desc.Path(),
				PackageAlias: pkgAlias,
				Name:         typeName,
			}, nil
		}
	}
	return CastRef{}, fmt.Errorf(
		"unresolved cast alias %q in %s: no imported proto with go_package alias %q",
		cast, file.Desc.Path(), pkgAlias,
	)
}
```

Note: the resolver still uses `depFile.GoPackageName` (Go-specific). In Task 3.3 we'll abstract the alias lookup.

- [ ] **Step 3: Replace `CastIdent`/`CastLocal` with `CastRef`**

Edit `FieldInfo`:

```go
type FieldInfo struct {
	ProtoNum     int32
	TargetName   string  // renamed from GoName
	Wire         WireKind
	ProtoKind    protoreflect.Kind
	Cast         string  // raw cast string as written
	CastRef      *CastRef // nil if no cast
	FixedLen     uint32
	KeepCapacity bool
	IsRepeated   bool
	IsBytes      bool
	IsString     bool
}
```

- [ ] **Step 4: Delete `QualifiedZeroType` from `FieldInfo`**

Remove the method `func (f *FieldInfo) QualifiedZeroType(g *protogen.GeneratedFile) string` from `analysis.go` entirely. Its role moves to the Go emitter (Task 3.2).

- [ ] **Step 5: Update `MessageInfo` field name**

```go
type MessageInfo struct {
	TargetType string  // renamed from GoType
	Fields     []FieldInfo
}
```

- [ ] **Step 6: This step intentionally leaves the Go emitter broken**

The Go emitter still references `CastIdent`, `CastLocal`, `GoName`, `GoType`, `QualifiedZeroType`. Task 3.2 fixes it.

- [ ] **Step 7: Commit (WIP)**

```bash
git add internal/core/
git commit -m "refactor(core): introduce neutral CastRef, rename TargetName/TargetType (WIP)"
```

---

### Task 3.2: Port Go emitter to neutral types

**Why:** Task 3.1 deliberately broke the Go emitter. This task repairs it using an adapter layer inside `internal/lang/golang`.

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/goident.go`
- Modify: every `internal/lang/golang/gen_*.go` referencing `GoName`, `GoType`, `CastIdent`, `CastLocal`, `QualifiedZeroType`

- [ ] **Step 1: Create adapter**

Create `internal/lang/golang/goident.go`:

```go
// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
)

// goCast returns a Go expression that casts v to the field's target type.
// For no-cast fields it returns v unchanged.
func goCast(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo, v string) string {
	if f.CastRef == nil {
		return v
	}
	if f.CastRef.ProtoFile == "" {
		return f.CastRef.Name + "(" + v + ")"
	}
	depFile, ok := fileMap[f.CastRef.ProtoFile]
	if !ok {
		// Should never happen — core resolved this already. Defensive fallback.
		return f.CastRef.PackageAlias + "." + f.CastRef.Name + "(" + v + ")"
	}
	ident := protogen.GoIdent{GoName: f.CastRef.Name, GoImportPath: depFile.GoImportPath}
	return g.QualifiedGoIdent(ident) + "(" + v + ")"
}

// goZeroType returns the Go zero-value expression for a fixed-length bytes field.
func goZeroType(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) string {
	if f.CastRef == nil {
		return ""
	}
	if f.CastRef.ProtoFile == "" {
		return f.CastRef.Name
	}
	depFile, ok := fileMap[f.CastRef.ProtoFile]
	if !ok {
		return f.CastRef.PackageAlias + "." + f.CastRef.Name
	}
	return g.QualifiedGoIdent(protogen.GoIdent{GoName: f.CastRef.Name, GoImportPath: depFile.GoImportPath})
}
```

- [ ] **Step 2: Thread `fileMap` through emitter functions**

`emitGoFile` already has access to `file`/`fileMap` via the caller. Pass `fileMap` into `generateUnmarshalCodec`, `generateMarshalCodec`, `generateMarshalToCodec`, `generateSizeCodec`, `generateResetCodec`. Update signatures accordingly.

- [ ] **Step 3: Replace `f.GoName` → `f.TargetName` across the Go emitter**

Global edit in `internal/lang/golang/`:

```bash
# Inspect callsites first
grep -rn "\.GoName" internal/lang/golang/
# Then update each
```

Every `accessor := "m." + f.GoName` becomes `accessor := "m." + f.TargetName`.

- [ ] **Step 4: Replace `info.GoType` → `info.TargetType`**

Same pattern for messages.

- [ ] **Step 5: Replace `castExpr*`/`QualifiedZeroType` usage**

`castExpr`, `castExpr32`, `castExpr64`, and callers that used `f.QualifiedZeroType(g)` should now use the adapter:

- `castExpr(g, f, "v")` → `goCast(g, fileMap, f, "v")` (or keep the wrappers but reimplement them against `CastRef`)
- `f.QualifiedZeroType(g)` → `goZeroType(g, fileMap, f)`

- [ ] **Step 6: Regenerate and run tests**

Run: `make generate && make test`
Expected: all PASS. Generated `fixture.codec.go` should be byte-identical to the pre-refactor version modulo cosmetic changes. If output drifts, diff carefully and confirm only comments/spacing differ.

- [ ] **Step 7: Run core unit tests to confirm Go-ism removal**

Run: `go test ./internal/core/ -v`
Expected: PASS. Also verify the core package doesn't import `protogen` for any reason other than reading descriptors (which is unavoidable).

- [ ] **Step 8: Grep for leftover Go-isms in core**

Run: `grep -n "GoName\|GoType\|GoIdent\|GeneratedFile" internal/core/*.go`
Expected: no matches in analysis.go for the `FieldInfo`/`MessageInfo` types. `protogen.*` may still appear where core reads proto descriptors, but no Go-flavored *output* type names.

- [ ] **Step 9: Commit**

```bash
git add internal/core/ internal/lang/golang/
git commit -m "refactor(go): adapt Go emitter to neutral core types (TargetName, CastRef)"
```

---

### Task 3.3: Abstract dependency-alias lookup behind an emitter hook

**Why:** `resolveCast` still calls `depFile.GoPackageName`. Other emitters need a different alias (TS module, Rust crate). Inject the lookup as a function.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/codegen.go`

- [ ] **Step 1: Change `AnalyzeMessage` signature**

Edit `internal/core/analysis.go`:

```go
// AliasLookup returns the emitter-specific alias for a dependency file.
// For Go this is GoPackageName; for other emitters it maps to the matching
// package/module/namespace identifier.
type AliasLookup func(dep *protogen.File) string

func AnalyzeMessage(
	msg *protogen.Message,
	fileMap map[string]*protogen.File,
	file *protogen.File,
	aliasOf AliasLookup,
) (*MessageInfo, error) {
	// ... unchanged up to field loop
	for _, field := range msg.Fields {
		fi, err := analyzeField(field, fileMap, file, aliasOf)
		// ...
	}
}

func analyzeField(
	field *protogen.Field,
	fileMap map[string]*protogen.File,
	file *protogen.File,
	aliasOf AliasLookup,
) (FieldInfo, error) {
	// ... same, but pass aliasOf to resolveCast
}

func resolveCast(
	fileMap map[string]*protogen.File,
	file *protogen.File,
	cast string,
	aliasOf AliasLookup,
) (CastRef, error) {
	// ... same logic, but use aliasOf(depFile) instead of depFile.GoPackageName
	if string(aliasOf(depFile)) == pkgAlias {
		// ...
	}
}
```

- [ ] **Step 2: Pass Go-specific alias from the Go emitter**

Edit `internal/lang/golang/codegen.go:generateFileImpl`:

```go
aliasOf := func(dep *protogen.File) string { return string(dep.GoPackageName) }
info, err := core.AnalyzeMessage(msg, fileMap, file, aliasOf)
```

- [ ] **Step 3: Update internal tests**

Edit `internal/core/testutil_test.go:runAnalyzeField`:

```go
aliasOf := func(dep *protogen.File) string { return string(dep.GoPackageName) }
return AnalyzeMessage(file.Messages[0], fileMap, file, aliasOf)
```

- [ ] **Step 4: Run all tests**

Run: `make test && go test ./internal/core/`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/ internal/lang/golang/
git commit -m "refactor(core): inject alias lookup instead of hardcoding GoPackageName"
```

---

### Task 3.4: Document the stable core API

**Why:** The core is now language-neutral. Future emitters will consume `AnalyzeMessage`, `MessageInfo`, `FieldInfo`, `CastRef`, `WireKind`, and the varint primitives. Add package-level doc comments explaining the contract and stability expectations.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`

- [ ] **Step 1: Add package doc**

At the top of `analysis.go` (after the copyright header):

```go
// Package core implements language-neutral proto descriptor analysis
// shared by every protoc-gen-codec-<lang> emitter.
//
// AnalyzeMessage consumes a proto descriptor and returns a MessageInfo
// whose fields identify wire kinds, cast targets, and repeated/bytes/string
// metadata independently of any output language. Emitters convert
// MessageInfo into language-specific code.
//
// Stability: MessageInfo, FieldInfo, CastRef, and WireKind are part of the
// cross-emitter contract. Changes that add fields are non-breaking; changes
// that remove or repurpose fields require updating every emitter.
package core
```

- [ ] **Step 2: Doc-comment each exported type**

Add concise doc comments on `MessageInfo`, `FieldInfo`, `CastRef`, `WireKind`, `AliasLookup`, `AnalyzeMessage`, `TagValue`, `TagSize`, `TagBytes`, `SovLocal`, `WireKindOf`.

- [ ] **Step 3: Run `go vet` to ensure comments are well-formed**

Run: `go vet ./internal/core/`
Expected: no warnings.

- [ ] **Step 4: Commit**

```bash
git add internal/core/analysis.go
git commit -m "docs(core): package-level stability contract for multi-emitter API"
```

---

## Phase 4 — Missing Proto Features

Each feature is big enough to deserve its own task. Implementation order: proto3 `optional` (simplest), then nested messages (unlocks compositional fixture), then oneof, then map, then WKT wiring. All new features are expressed in the neutral `internal/core` + Go emitter only.

### Task 4.1: proto3 `optional` presence

**Why:** `analysis.go` never checks `field.Desc.HasPresence()`. A proto3 `optional` scalar should distinguish unset from zero; today it is indistinguishable.

**Design decision:** The target Go type must use a **pointer** (e.g. `*int32`) or a dedicated `Optional[T]` wrapper. For v1, require the target type to be a pointer — detected by the existing struct field at generation time. The generator does not construct a wrapper.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_*.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture.proto`, `fixture.go`
- Test: `fixture_test.go`

- [ ] **Step 1: Add `HasPresence` to FieldInfo**

Edit `internal/core/analysis.go`:

```go
type FieldInfo struct {
	// ... existing fields
	HasPresence bool // proto3 optional or message-kind
}
```

In `analyzeField`:

```go
fi.HasPresence = field.Desc.HasPresence()
```

- [ ] **Step 2: Add `optional` field to fixture**

Edit `lang/go/integration/fixture.proto`:

```protobuf
message NumericOnly {
  // ... existing fields
  optional int32 h = 8 [(codec.field) = "H"];
}
```

Edit `lang/go/integration/fixture.go`:

```go
type NumericOnly struct {
	// ... existing fields
	H *int32 `json:"h,omitempty"`
}
```

- [ ] **Step 3: Write failing test**

```go
func TestNumericOnly_Optional_Unset(t *testing.T) {
	t.Parallel()
	n := integration.NumericOnly{A: 1} // H is nil
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}

func TestNumericOnly_Optional_SetToZero(t *testing.T) {
	t.Parallel()
	zero := int32(0)
	n := integration.NumericOnly{A: 1, H: &zero}
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}

func TestNumericOnly_Optional_SetToValue(t *testing.T) {
	t.Parallel()
	v := int32(-42)
	n := integration.NumericOnly{A: 1, H: &v}
	codec.AssertRoundtrip[integration.NumericOnly](t, n)
}
```

- [ ] **Step 4: Emit pointer-presence marshal**

In `generateFieldMarshal`, add at the top (after the repeated check):

```go
if f.HasPresence && !f.IsRepeated && !f.IsMessage {
	derefAccessor := "*" + accessor
	g.P("if ", accessor, " != nil {")
	switch f.Wire {
	case core.WireVarint:
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		if f.ProtoKind == protoreflect.BoolKind {
			g.P("if ", derefAccessor, " { buf[n] = 1 } else { buf[n] = 0 }")
			g.P("n++")
		} else {
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", derefAccessor, "))")
		}
	case core.WireFixed64:
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(", derefAccessor, "))")
		g.P("n += 8")
	case core.WireFixed32:
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(", derefAccessor, "))")
		g.P("n += 4")
	}
	g.P("}")
	return
}
```

In `generateFieldSize`, analogous guard:

```go
if f.HasPresence && !f.IsRepeated && !f.IsMessage {
	derefAccessor := "*" + accessor
	g.P("if ", accessor, " != nil {")
	switch f.Wire {
	case core.WireVarint:
		g.P("n += ", ts, " + ", identSov, "(uint64(", derefAccessor, "))")
	case core.WireFixed64:
		g.P("n += ", ts+8)
	case core.WireFixed32:
		g.P("n += ", ts+4)
	}
	g.P("}")
	return
}
```

- [ ] **Step 5: Emit pointer-presence unmarshal**

In `generateFieldUnmarshal`, add at the top (after the `case f.ProtoNum` emission):

```go
if f.HasPresence && !f.IsRepeated && !f.IsMessage {
	g.P("if wireType != ", int(f.Wire), " {")
	emitErrWireType(g, f.ProtoNum)
	g.P("}")
	switch f.Wire {
	case core.WireVarint:
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("tmp := ", goCast(g, fileMap, f, "v"))
		g.P(accessor, " = &tmp")
	case core.WireFixed64:
		g.P("if l-i < 8 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P("tmp := ", goCast(g, fileMap, f, fmt.Sprintf("%s.Uint64(data[i:])", g.QualifiedGoIdent(identBinaryLE))))
		g.P(accessor, " = &tmp")
		g.P("i += 8")
	case core.WireFixed32:
		g.P("if l-i < 4 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P("tmp := ", goCast(g, fileMap, f, fmt.Sprintf("%s.Uint32(data[i:])", g.QualifiedGoIdent(identBinaryLE))))
		g.P(accessor, " = &tmp")
		g.P("i += 4")
	}
	return
}
```

In `generateFieldReset`, add:

```go
if f.HasPresence && !f.IsRepeated && !f.IsMessage {
	g.P(accessor, " = nil")
	return
}
```

- [ ] **Step 6: Regenerate and run tests**

Run: `make generate && make test`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "feat: proto3 optional scalar presence via pointer fields"
```

---

### Task 4.2: Nested messages

**Why:** `gen_marshal.go`/`gen_unmarshal.go` have no path for `MessageKind`. Fields of message type are silently dropped.

**Design:** Nested messages delegate to the child type's own `MarshalToCodec`/`UnmarshalCodec`/`SizeCodec`. The wire format is length-delimited.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/core/analysis.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/internal/lang/golang/gen_*.go`
- Modify: `lang/go/integration/fixture.proto`, `fixture.go`, `fixture_test.go`

- [ ] **Step 1: Add `IsMessage` + `MessageRef` to FieldInfo**

```go
type FieldInfo struct {
	// ... existing
	IsMessage  bool
	MessageRef *MessageRef // non-nil iff IsMessage
}

type MessageRef struct {
	// ProtoFile is the .proto file declaring the referenced message.
	// Empty if same-file.
	ProtoFile string
	// FullName is the dotted full name, e.g. "t.Inner".
	FullName string
	// TargetType is the target-language type name extracted from the
	// referenced message's codec.type annotation.
	TargetType string
}
```

- [ ] **Step 2: Populate in `analyzeField`**

```go
if field.Desc.Kind() == protoreflect.MessageKind {
	fi.IsMessage = true
	msgDesc := field.Message
	if msgDesc == nil {
		return fi, fmt.Errorf("field %s: message kind with nil Message descriptor", field.Desc.Name())
	}
	targetType := messageGoType(msgDesc)
	if targetType == "" {
		return fi, fmt.Errorf(
			"field %s references message %s which lacks (codec.type)",
			field.Desc.Name(), msgDesc.Desc.FullName(),
		)
	}
	// Detect if the message lives in a different file
	declFile := msgDesc.Desc.ParentFile().Path()
	ref := &MessageRef{
		FullName:   string(msgDesc.Desc.FullName()),
		TargetType: targetType,
	}
	if declFile != file.Desc.Path() {
		ref.ProtoFile = declFile
	}
	fi.MessageRef = ref
}
```

- [ ] **Step 3: Add fixture with nested message**

Edit `lang/go/integration/fixture.proto`:

```protobuf
message Inner {
  option (codec.type) = "Inner";
  string label = 1 [(codec.field) = "Label"];
  int64 count = 2 [(codec.field) = "Count"];
}

message Container {
  option (codec.type) = "Container";
  string name = 1 [(codec.field) = "Name"];
  Inner inner = 2 [(codec.field) = "Inner"];
  repeated Inner children = 3 [(codec.field) = "Children"];
}
```

Edit `lang/go/integration/fixture.go`:

```go
type Inner struct {
	Label string
	Count int64
}

type Container struct {
	Name     string
	Inner    *Inner
	Children []*Inner
}
```

- [ ] **Step 4: Failing tests**

```go
func TestContainer_Roundtrip_WithInner(t *testing.T) {
	t.Parallel()
	c := integration.Container{
		Name:  "alpha",
		Inner: &integration.Inner{Label: "x", Count: 7},
	}
	codec.AssertRoundtrip[integration.Container](t, c)
}

func TestContainer_Roundtrip_InnerNil(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.Container](t, integration.Container{Name: "alpha"})
}

func TestContainer_Roundtrip_RepeatedChildren(t *testing.T) {
	t.Parallel()
	c := integration.Container{
		Name: "alpha",
		Children: []*integration.Inner{
			{Label: "x"}, {Label: "y", Count: 99},
		},
	}
	codec.AssertRoundtrip[integration.Container](t, c)
}
```

- [ ] **Step 5: Emit marshal for nested single message**

In `generateFieldMarshal`, add a case before the existing switch:

```go
if f.IsMessage {
	g.P("if ", accessor, " != nil {")
	emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
	g.P("sz := ", accessor, ".SizeCodec()")
	g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
	g.P("wn, err := ", accessor, ".MarshalToCodec(buf[n:])")
	g.P("if err != nil { return 0, err }")
	g.P("n += wn")
	g.P("}")
	return
}
```

- [ ] **Step 6: Emit size for nested**

In `generateFieldSize`:

```go
if f.IsMessage {
	g.P("if ", accessor, " != nil {")
	g.P("sz := ", accessor, ".SizeCodec()")
	g.P("n += ", ts, " + ", identSov, "(uint64(sz)) + sz")
	g.P("}")
	return
}
```

- [ ] **Step 7: Emit unmarshal for nested**

In `generateFieldUnmarshal`:

```go
if f.IsMessage {
	g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
	g.P("if n < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += n")
	g.P("if uint64(l-i) < vLen {")
	emitErrShort(g, f.ProtoNum)
	g.P("}")
	// Target field type is *InnerType; allocate if nil
	g.P("if ", accessor, " == nil {")
	g.P(accessor, " = new(", f.MessageRef.TargetType, ")")
	g.P("}")
	g.P("if err := ", accessor, ".UnmarshalCodec(data[i:i+int(vLen)]); err != nil {")
	g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
	g.P("}")
	g.P("i += int(vLen)")
	return
}
```

(The adapter for cross-file target types will need `goIdentFor(messageRef)` similar to `goCast` — add to `goident.go`.)

- [ ] **Step 8: Emit repeated nested messages**

In `generateRepeatedFieldMarshal`, prepend:

```go
if f.IsMessage {
	g.P("for _, elem := range ", accessor, " {")
	g.P("if elem == nil { continue }")
	emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
	g.P("sz := elem.SizeCodec()")
	g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
	g.P("wn, err := elem.MarshalToCodec(buf[n:])")
	g.P("if err != nil { return 0, err }")
	g.P("n += wn")
	g.P("}")
	return
}
```

In `generateRepeatedFieldSize`, prepend:

```go
if f.IsMessage {
	g.P("for _, elem := range ", accessor, " {")
	g.P("if elem == nil { continue }")
	g.P("sz := elem.SizeCodec()")
	g.P("n += ", ts, " + ", identSov, "(uint64(sz)) + sz")
	g.P("}")
	return
}
```

In `generateRepeatedFieldUnmarshal`, prepend:

```go
if f.IsMessage {
	g.P("if wireType != 2 {")
	emitErrWireType(g, f.ProtoNum)
	g.P("}")
	g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
	g.P("if n < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += n")
	g.P("if uint64(l-i) < vLen {")
	emitErrShort(g, f.ProtoNum)
	g.P("}")
	g.P("elem := new(", goIdentForMessage(g, fileMap, f), ")")
	g.P("if err := elem.UnmarshalCodec(data[i:i+int(vLen)]); err != nil {")
	g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
	g.P("}")
	g.P(accessor, " = append(", accessor, ", elem)")
	g.P("i += int(vLen)")
	return
}
```

Where `goIdentForMessage` is a helper on `goident.go`:

```go
func goIdentForMessage(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) string {
	if f.MessageRef == nil {
		return ""
	}
	if f.MessageRef.ProtoFile == "" {
		return f.MessageRef.TargetType
	}
	depFile, ok := fileMap[f.MessageRef.ProtoFile]
	if !ok {
		return f.MessageRef.TargetType
	}
	return g.QualifiedGoIdent(protogen.GoIdent{GoName: f.MessageRef.TargetType, GoImportPath: depFile.GoImportPath})
}
```

Replace the raw `f.MessageRef.TargetType` references in Step 7's singular-message unmarshal with `goIdentForMessage(g, fileMap, f)` so cross-file message refs are properly qualified.

- [ ] **Step 9: Emit reset for nested**

In `generateFieldReset`:

```go
if f.IsMessage {
	g.P(accessor, " = nil")
	return
}
```

For repeated nested, the existing repeated branch already handles `[:0]` / `nil`.

- [ ] **Step 10: Regenerate and run all tests**

Run: `make generate && make test`
Expected: all PASS.

- [ ] **Step 11: Add PBT coverage**

```go
func TestContainer_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nChildren := rapid.IntRange(0, 3).Draw(t, "n")
		var children []*integration.Inner
		for i := 0; i < nChildren; i++ {
			children = append(children, &integration.Inner{
				Label: rapid.String().Draw(t, "l"),
				Count: rapid.Int64().Draw(t, "c"),
			})
		}
		c := integration.Container{
			Name:     rapid.String().Draw(t, "n"),
			Children: children,
		}
		codec.AssertRoundtrip[integration.Container](t, c)
	})
}
```

- [ ] **Step 12: Commit**

```bash
git add .
git commit -m "feat: nested message marshal/unmarshal/size/reset with length-delimited wire format"
```

---

### Task 4.3: `oneof` support

**Why:** `analysis.go` never inspects `msg.Oneofs`. All fields inside a oneof are iterated as ordinary fields — if two branches are non-zero the output is double-encoded.

**Design:** Follow the `optional` pattern. The target Go struct carries a single interface field per oneof (e.g. `Value isPatch_Value`), with generated branch types. Since protoc-gen-codec's ethos is "no generated types", we instead rely on the user defining the branch types and an interface by hand; the generator only emits the dispatch. For v1, adopt a simpler pattern: **represent oneof as a sibling enum discriminator + parallel fields**, matching what the existing `Patch` fixture already does (Kind + TextVal/IntVal/Fixed64Val/BlobRef). The generator serializes only the branch selected by the discriminator.

Since the existing `Patch` message does not use proto `oneof` but achieves the same effect manually, we need to wire actual `oneof` support. Design:

- `codec.type` on a `oneof` inside a message declares the discriminator field name in the target struct.
- Each branch field carries its own `codec.field` for the underlying Go field.
- Marshal emits only the branch whose discriminator matches; unmarshal sets both discriminator and field.

This requires a new annotation on `oneof`. Use `codec.oneof` as a **message-scoped option**: a list of `(oneof_name, discriminator_field, values)` triples. Alternatively, keep the existing manual approach and explicitly document that proto `oneof` is unsupported; require users to flatten.

**Decision for v1:** Start with option B — document that proto `oneof` is **not** supported yet, and raise a generation-time error when encountered. This un-sticks the silent double-encoding bug immediately. Full `oneof` support is deferred to a follow-up once the annotation design is validated.

- [ ] **Step 1: Detect oneof at analysis time and error**

Edit `internal/core/analysis.go:AnalyzeMessage`:

```go
for _, oneof := range msg.Oneofs {
	if oneof.Desc.IsSynthetic() {
		continue // proto3 optional is represented as a synthetic oneof; allowed
	}
	return nil, fmt.Errorf(
		"message %s: oneof %q is not yet supported by protoc-gen-codec",
		msg.Desc.Name(), oneof.Desc.Name(),
	)
}
```

- [ ] **Step 2: Add test that confirms oneof produces a generation error**

Add to `internal/core/analysis_test.go`:

```go
func TestAnalyzeMessage_OneofIsRejected(t *testing.T) {
	t.Parallel()
	// Construct a message with a non-synthetic oneof.
	_, err := runAnalyzeMessageFixture(t, messageWithOneof)
	if err == nil {
		t.Fatal("expected error for oneof")
	}
}
```

Extend `testutil_test.go` with `messageWithOneof` helper.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/core/ -v && make test`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/core/
git commit -m "feat(core): explicit rejection of non-synthetic oneof until design stabilizes"
```

**Follow-up (not in this plan):** Full oneof support with an annotation design RFC.

---

### Task 4.4: `map<K,V>` support

**Why:** Maps are documented in the README but silently dropped. Implement as proto3 spec defines — wire-encoded as repeated message of synthesized `entry` type.

**Design:** `map<K,V>` on the wire is a `repeated` field where each element is a synthetic message `{key, value}`. The target Go type is `map[K]V`. The generator emits:
- Size: walk the map, compute per-entry size (tag+key+tag+value).
- Marshal: for each entry, emit the outer tag+length, then the entry body (key field then value field).
- Unmarshal: read a length-delimited entry, decode its inner key and value, assign `m[k] = v`.
- Reset: `m = nil` (or `clear(m)` when `keep_capacity` is set — add map support to `keep_capacity`).

**Files:**
- Modify: `internal/core/analysis.go`
- Modify: `internal/lang/golang/gen_*.go`
- Modify: `lang/go/integration/fixture.proto`, `fixture.go`, `fixture_test.go`

- [ ] **Step 1: Add `IsMap`, `MapKey`, `MapValue` to FieldInfo**

```go
type FieldInfo struct {
	// ... existing
	IsMap    bool
	MapKey   *FieldInfo
	MapValue *FieldInfo
}
```

- [ ] **Step 2: Populate in `analyzeField`**

```go
if field.Desc.IsMap() {
	fi.IsMap = true
	fi.IsRepeated = false // override; map is wire-repeated but target is map[K]V
	keyFI, err := analyzeField(field.Message.Fields[0], fileMap, file, aliasOf)
	if err != nil {
		return fi, fmt.Errorf("field %s map key: %w", field.Desc.Name(), err)
	}
	valFI, err := analyzeField(field.Message.Fields[1], fileMap, file, aliasOf)
	if err != nil {
		return fi, fmt.Errorf("field %s map value: %w", field.Desc.Name(), err)
	}
	fi.MapKey = &keyFI
	fi.MapValue = &valFI
}
```

- [ ] **Step 3: Fixture**

Edit `lang/go/integration/fixture.proto`:

```protobuf
message MapHolder {
  option (codec.type) = "MapHolder";
  map<string, string> attrs = 1 [(codec.field) = "Attrs"];
  map<string, int64> counts = 2 [(codec.field) = "Counts"];
}
```

Edit `lang/go/integration/fixture.go`:

```go
type MapHolder struct {
	Attrs  map[string]string
	Counts map[string]int64
}
```

- [ ] **Step 4: Failing tests**

```go
func TestMapHolder_Roundtrip_Empty(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.MapHolder](t, integration.MapHolder{})
}

func TestMapHolder_Roundtrip_Populated(t *testing.T) {
	t.Parallel()
	h := integration.MapHolder{
		Attrs:  map[string]string{"a": "1", "b": "2"},
		Counts: map[string]int64{"x": 99},
	}
	codec.AssertRoundtrip[integration.MapHolder](t, h)
}
```

- [ ] **Step 5: Add map-entry helper for scalars**

Create `internal/lang/golang/gen_map.go`:

```go
// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
)

// scalarSizeExpr returns a Go expression computing the wire size of a
// singular scalar field (excluding its tag). Used for synthetic map entry
// keys and values.
func scalarSizeExpr(f *core.FieldInfo, v string) string {
	switch {
	case f.IsString, f.IsBytes:
		return fmt.Sprintf("codec.Sov(uint64(len(%s))) + len(%s)", v, v)
	case f.Wire == core.WireFixed64:
		return "8"
	case f.Wire == core.WireFixed32:
		return "4"
	default: // WireVarint
		return fmt.Sprintf("codec.Sov(uint64(%s))", v)
	}
}

// emitScalarWrite emits code that writes a tag+value for a synthetic
// map entry sub-field into buf[n:].
func emitScalarWrite(g *protogen.GeneratedFile, f *core.FieldInfo, v string) {
	emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
	switch {
	case f.IsString:
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", v, ")))")
		g.P("n += copy(buf[n:], ", v, ")")
	case f.IsBytes:
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", v, ")))")
		g.P("n += copy(buf[n:], ", v, ")")
	case f.Wire == core.WireFixed64:
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(", v, "))")
		g.P("n += 8")
	case f.Wire == core.WireFixed32:
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(", v, "))")
		g.P("n += 4")
	default: // WireVarint
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", v, "))")
	}
}
```

- [ ] **Step 6: Emit map marshal**

In `generateFieldMarshal`, add before the existing switch:

```go
if f.IsMap {
	keySize := scalarSizeExpr(f.MapKey, "k")
	valSize := scalarSizeExpr(f.MapValue, "v")
	keyTagSize := core.TagSize(f.MapKey.ProtoNum)
	valTagSize := core.TagSize(f.MapValue.ProtoNum)
	g.P("for k, v := range ", accessor, " {")
	emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
	g.P("entrySz := ", keyTagSize, " + ", keySize, " + ", valTagSize, " + ", valSize)
	g.P("n += ", identEncodeVarint, "(buf[n:],uint64(entrySz))")
	emitScalarWrite(g, f.MapKey, "k")
	emitScalarWrite(g, f.MapValue, "v")
	g.P("}")
	return
}
```

- [ ] **Step 7: Emit map size**

In `generateFieldSize`:

```go
if f.IsMap {
	keySize := scalarSizeExpr(f.MapKey, "k")
	valSize := scalarSizeExpr(f.MapValue, "v")
	keyTagSize := core.TagSize(f.MapKey.ProtoNum)
	valTagSize := core.TagSize(f.MapValue.ProtoNum)
	g.P("for k, v := range ", accessor, " {")
	g.P("entrySz := ", keyTagSize, " + ", keySize, " + ", valTagSize, " + ", valSize)
	g.P("n += ", ts, " + ", identSov, "(uint64(entrySz)) + entrySz")
	g.P("}")
	return
}
```

- [ ] **Step 8: Emit map unmarshal**

In `generateFieldUnmarshal`, add before the existing switch (uses the same field-switch structure as the outer decoder, but nested over the entry body):

```go
if f.IsMap {
	g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
	g.P("if n < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += n")
	g.P("if uint64(l-i) < vLen {")
	emitErrShort(g, f.ProtoNum)
	g.P("}")
	g.P("if ", accessor, " == nil {")
	g.P(accessor, " = make(map[", mapKeyGoType(f), "]", mapValueGoType(f), ")")
	g.P("}")
	g.P("entryEnd := i + int(vLen)")
	g.P("var mk ", mapKeyGoType(f))
	g.P("var mv ", mapValueGoType(f))
	g.P("for i < entryEnd {")
	g.P("etag, en := ", identDecodeVarint, "(data[i:])")
	g.P("if en < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += en")
	g.P("switch etag >> 3 {")
	g.P("case ", f.MapKey.ProtoNum, ":")
	emitScalarRead(g, f.MapKey, "mk")
	g.P("case ", f.MapValue.ProtoNum, ":")
	emitScalarRead(g, f.MapValue, "mv")
	g.P("default:")
	g.P("sn, err := ", identSkipField, "(data[i:], etag & 0x7)")
	g.P("if err != nil { return err }")
	g.P("i += sn")
	g.P("}")
	g.P("}")
	g.P(accessor, "[mk] = mv")
	return
}
```

Add `mapKeyGoType` and `mapValueGoType` helpers to `gen_map.go`:

```go
func mapKeyGoType(f *core.FieldInfo) string {
	return scalarGoType(f.MapKey)
}
func mapValueGoType(f *core.FieldInfo) string {
	return scalarGoType(f.MapValue)
}

func scalarGoType(f *core.FieldInfo) string {
	switch {
	case f.IsString:
		return "string"
	case f.IsBytes:
		return "[]byte"
	case f.Wire == core.WireFixed64:
		return "int64" // refined by cast if present
	case f.Wire == core.WireFixed32:
		return "int32"
	default:
		return "uint64" // refined by cast if present
	}
}
```

Add `emitScalarRead`:

```go
func emitScalarRead(g *protogen.GeneratedFile, f *core.FieldInfo, dst string) {
	switch {
	case f.IsString:
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 { return ", identErrInvalidVarint, " }")
		g.P("i += n")
		g.P(dst, " = string(data[i:i+int(vLen)])")
		g.P("i += int(vLen)")
	case f.IsBytes:
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 { return ", identErrInvalidVarint, " }")
		g.P("i += n")
		g.P(dst, " = append([]byte(nil), data[i:i+int(vLen)]...)")
		g.P("i += int(vLen)")
	case f.Wire == core.WireFixed64:
		g.P(dst, " = int64(", identBinaryLE, ".Uint64(data[i:]))")
		g.P("i += 8")
	case f.Wire == core.WireFixed32:
		g.P(dst, " = int32(", identBinaryLE, ".Uint32(data[i:]))")
		g.P("i += 4")
	default:
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 { return ", identErrInvalidVarint, " }")
		g.P("i += n")
		g.P(dst, " = v")
	}
}
```

- [ ] **Step 9: Emit map reset**

In `generateFieldReset`:

```go
if f.IsMap {
	g.P(accessor, " = nil")
	return
}
```

- [ ] **Step 10: Regenerate and run tests**

Run: `make generate && make test`
Expected: all PASS.

- [ ] **Step 11: Rapid PBT**

```go
func TestMapHolder_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nA := rapid.IntRange(0, 4).Draw(t, "nA")
		attrs := make(map[string]string, nA)
		for i := 0; i < nA; i++ {
			attrs[rapid.String().Draw(t, "k")] = rapid.String().Draw(t, "v")
		}
		codec.AssertRoundtrip[integration.MapHolder](t, integration.MapHolder{Attrs: attrs})
	})
}
```

- [ ] **Step 12: Commit**

```bash
git add .
git commit -m "feat: map<K,V> marshal/unmarshal with synthesized entry messages"
```

---

### Task 4.5: Well-Known Types (Timestamp, Duration)

**Why:** WKT references in a .proto currently produce nothing. Provide first-class support for `google.protobuf.Timestamp` and `google.protobuf.Duration` by treating them as nested messages with known structure.

**Design:** When the analysis layer sees a message field whose full name matches a known WKT, flag `fi.WellKnown = WKTTimestamp` etc. The Go emitter delegates to a runtime helper that marshals `time.Time` or `time.Duration` into the protobuf WKT wire format.

**Files:**
- Modify: `internal/core/analysis.go`
- Create: `codec/wkt.go`
- Modify: `internal/lang/golang/gen_*.go`
- Modify: `lang/go/integration/fixture.proto`, `fixture.go`, `fixture_test.go`

- [ ] **Step 1: Define WKT enum in core**

```go
type WellKnownKind int

const (
	WKTNone WellKnownKind = iota
	WKTTimestamp
	WKTDuration
)
```

Add `WellKnown WellKnownKind` to `FieldInfo`.

- [ ] **Step 2: Detect WKT in `analyzeField`**

```go
if fi.IsMessage {
	switch string(field.Message.Desc.FullName()) {
	case "google.protobuf.Timestamp":
		fi.WellKnown = WKTTimestamp
		fi.IsMessage = false // override; emit dedicated code
	case "google.protobuf.Duration":
		fi.WellKnown = WKTDuration
		fi.IsMessage = false
	}
}
```

- [ ] **Step 3: Runtime helpers**

Create `codec/wkt.go`:

```go
// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import "time"

// Timestamp/Duration share the same wire body:
//   field 1 (int64 seconds, varint)
//   field 2 (int32 nanos, varint)

// SizeTimestamp returns the wire size of the body (without the outer tag
// or length prefix) for a google.protobuf.Timestamp.
func SizeTimestamp(t time.Time) int {
	secs := t.Unix()
	nanos := int32(t.Nanosecond())
	return sizeTSBody(secs, nanos)
}

// SizeDuration returns the wire size of the body for a google.protobuf.Duration.
func SizeDuration(d time.Duration) int {
	secs := int64(d / time.Second)
	nanos := int32(d % time.Second)
	return sizeTSBody(secs, nanos)
}

func sizeTSBody(secs int64, nanos int32) int {
	n := 0
	if secs != 0 {
		n += 1 + Sov(uint64(secs))
	}
	if nanos != 0 {
		n += 1 + Sov(uint64(nanos))
	}
	return n
}

// EncodeTimestamp writes a Timestamp body into buf and returns bytes written.
func EncodeTimestamp(buf []byte, t time.Time) int {
	return encodeTSBody(buf, t.Unix(), int32(t.Nanosecond()))
}

// EncodeDuration writes a Duration body into buf and returns bytes written.
func EncodeDuration(buf []byte, d time.Duration) int {
	return encodeTSBody(buf, int64(d/time.Second), int32(d%time.Second))
}

func encodeTSBody(buf []byte, secs int64, nanos int32) int {
	n := 0
	if secs != 0 {
		buf[n] = 0x08 // tag: field 1, wire type 0
		n++
		n += EncodeVarint(buf[n:], uint64(secs))
	}
	if nanos != 0 {
		buf[n] = 0x10 // tag: field 2, wire type 0
		n++
		n += EncodeVarint(buf[n:], uint64(nanos))
	}
	return n
}

// DecodeTimestamp parses a Timestamp body into time.Time (UTC).
func DecodeTimestamp(data []byte) (time.Time, error) {
	secs, nanos, err := decodeTSBody(data)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(secs, int64(nanos)).UTC(), nil
}

// DecodeDuration parses a Duration body into time.Duration.
func DecodeDuration(data []byte) (time.Duration, error) {
	secs, nanos, err := decodeTSBody(data)
	if err != nil {
		return 0, err
	}
	return time.Duration(secs)*time.Second + time.Duration(nanos), nil
}

func decodeTSBody(data []byte) (int64, int32, error) {
	var secs int64
	var nanos int32
	i := 0
	l := len(data)
	for i < l {
		tag, tn := DecodeVarint(data[i:])
		if tn < 0 {
			return 0, 0, ErrInvalidVarint
		}
		i += tn
		switch tag >> 3 {
		case 1:
			v, vn := DecodeVarint(data[i:])
			if vn < 0 {
				return 0, 0, ErrInvalidVarint
			}
			i += vn
			secs = int64(v)
		case 2:
			v, vn := DecodeVarint(data[i:])
			if vn < 0 {
				return 0, 0, ErrInvalidVarint
			}
			i += vn
			nanos = int32(v)
		default:
			sn, err := SkipField(data[i:], tag&0x7)
			if err != nil {
				return 0, 0, err
			}
			i += sn
		}
	}
	return secs, nanos, nil
}
```

- [ ] **Step 4: Target Go types**

Edit `lang/go/integration/fixture.proto`:

```protobuf
import "google/protobuf/timestamp.proto";

message TimestampHolder {
  option (codec.type) = "TimestampHolder";
  google.protobuf.Timestamp created_at = 1 [(codec.field) = "CreatedAt"];
}
```

Edit `lang/go/integration/fixture.go`:

```go
type TimestampHolder struct {
	CreatedAt time.Time
}
```

- [ ] **Step 5: Failing test**

```go
func TestTimestampHolder_Roundtrip(t *testing.T) {
	t.Parallel()
	h := integration.TimestampHolder{CreatedAt: time.Unix(1713400000, 500_000_000).UTC()}
	codec.AssertRoundtrip[integration.TimestampHolder](t, h)
}
```

- [ ] **Step 6: Emit WKT marshal**

In `generateFieldMarshal`, add before the existing switch:

```go
if f.WellKnown == core.WKTTimestamp {
	g.P("{ // google.protobuf.Timestamp")
	g.P("var zero ", identTimeTime)
	g.P("if ", accessor, " != zero {")
	emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
	g.P("sz := ", identSizeTimestamp, "(", accessor, ")")
	g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
	g.P("n += ", identEncodeTimestamp, "(buf[n:], ", accessor, ")")
	g.P("}")
	g.P("}")
	return
}
if f.WellKnown == core.WKTDuration {
	g.P("if ", accessor, " != 0 {")
	emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
	g.P("sz := ", identSizeDuration, "(", accessor, ")")
	g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
	g.P("n += ", identEncodeDuration, "(buf[n:], ", accessor, ")")
	g.P("}")
	return
}
```

Add idents to `codegen.go`:

```go
identTimeTime      = protogen.GoIdent{GoName: "Time", GoImportPath: "time"}
identSizeTimestamp = protogen.GoIdent{GoName: "SizeTimestamp", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identSizeDuration  = protogen.GoIdent{GoName: "SizeDuration", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identEncodeTimestamp = protogen.GoIdent{GoName: "EncodeTimestamp", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identEncodeDuration  = protogen.GoIdent{GoName: "EncodeDuration", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identDecodeTimestamp = protogen.GoIdent{GoName: "DecodeTimestamp", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
identDecodeDuration  = protogen.GoIdent{GoName: "DecodeDuration", GoImportPath: "go.stealthscale.io/protoc-gen-codec/lang/go/codec"}
```

- [ ] **Step 7: Emit WKT size**

In `generateFieldSize`:

```go
if f.WellKnown == core.WKTTimestamp {
	g.P("{ var zero ", identTimeTime)
	g.P("if ", accessor, " != zero {")
	g.P("sz := ", identSizeTimestamp, "(", accessor, ")")
	g.P("n += ", ts, " + ", identSov, "(uint64(sz)) + sz")
	g.P("} }")
	return
}
if f.WellKnown == core.WKTDuration {
	g.P("if ", accessor, " != 0 {")
	g.P("sz := ", identSizeDuration, "(", accessor, ")")
	g.P("n += ", ts, " + ", identSov, "(uint64(sz)) + sz")
	g.P("}")
	return
}
```

- [ ] **Step 8: Emit WKT unmarshal**

In `generateFieldUnmarshal`:

```go
if f.WellKnown == core.WKTTimestamp || f.WellKnown == core.WKTDuration {
	g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
	g.P("if n < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += n")
	g.P("if uint64(l-i) < vLen {")
	emitErrShort(g, f.ProtoNum)
	g.P("}")
	if f.WellKnown == core.WKTTimestamp {
		g.P("v, err := ", identDecodeTimestamp, "(data[i:i+int(vLen)])")
	} else {
		g.P("v, err := ", identDecodeDuration, "(data[i:i+int(vLen)])")
	}
	g.P("if err != nil { return err }")
	g.P(accessor, " = v")
	g.P("i += int(vLen)")
	return
}
```

- [ ] **Step 9: Emit WKT reset**

In `generateFieldReset`:

```go
if f.WellKnown == core.WKTTimestamp {
	g.P(accessor, " = ", identTimeTime, "{}")
	return
}
if f.WellKnown == core.WKTDuration {
	g.P(accessor, " = 0")
	return
}
```

- [ ] **Step 10: Regenerate and run tests**

Run: `make generate && make test`
Expected: all PASS.

- [ ] **Step 11: Commit**

```bash
git add .
git commit -m "feat: google.protobuf.Timestamp and Duration as time.Time/time.Duration"
```

---

## Phase 5 — Testing Discipline & Coverage Gates

This phase adapts the project's established testing practices (Rapid PBT, fuzz, benchmark tiers, CI hard stops) to protoc-gen-codec and enforces **100% line coverage on generated code** (`lang/go/integration/*.codec.go`).

### Test Helpers Already Available

`lang/go/codec/testing.go` ships these generic helpers; use them everywhere instead of rolling per-fixture code:

- `codec.RunTestSuite[T](t, sample)` — roundtrip, zero-value roundtrip, reset, nil-safety, corruption injection (truncation + byte-flip). One call per fixture replaces ~5 explicit subtests.
- `codec.RunBenchSuite[T](b, sample)` — `Codec/MarshalTo`, `Codec/Unmarshal`, `JSON/Marshal`, `JSON/Unmarshal` sub-benchmarks.
- `codec.RunFuzzRoundtrip[T](f, samples...)` — seeds + fuzz target with full-struct `reflect.DeepEqual` equality on roundtrip.
- `codec.AssertRoundtrip[T](t, sample)` — single-input roundtrip + size-identity check.
- `codec.AssertReset[T](t, populated)`, `codec.AssertNilSafe[T](t)`, `codec.AssertWireSmallerThanJSON[T](t, sample)`.

Standard test trio per fixture after Phase 4/5:

```go
func TestFoo_Codec(t *testing.T)   { codec.RunTestSuite[pkg.Foo](t, sampleFoo()) }
func BenchmarkFoo_Codec(b *testing.B) { codec.RunBenchSuite[pkg.Foo](b, sampleFoo()) }
func FuzzFoo_Codec(f *testing.F)   { codec.RunFuzzRoundtrip[pkg.Foo](f, sampleFoo()) }
```

Tasks 5.6-5.10 extend this set with PBT (`AssertLaws`), alloc-budget (`AssertAllocBudget`), wire-compat (`AssertWireCompatGoogle`), and CI wiring.

### Task 5.0: Freeze benchmark baseline and add regression comparator

**Why:** We want two independent guarantees, not one:

1. **Architectural tier gates** (Task 5.10) — T0/T1/T2/T3 encode the design contract (e.g. `MarshalToCodec` must be zero-alloc forever). These are absolute caps that the design guarantees.
2. **Per-benchmark baseline** (this task) — the *current measured numbers* are frozen. Any PR that makes a specific benchmark slower or higher-alloc than baseline fails CI, even if it still sits under the tier cap. This catches the "death by a thousand 3% regressions" pattern that tier gates miss.

The baseline is a living artefact: it is refreshed whenever an intentional perf change ships (e.g. the Phase 1 slab fix will tighten Fixture unmarshal allocs from 6 → ≤3, and the baseline is re-snapshotted to lock in that improvement).

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/.bench-baseline/main.txt`
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/scripts/bench-compare.sh`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/Makefile`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/.github/workflows/ci.yml`

- [ ] **Step 1: Capture the initial baseline**

Run:

```bash
mkdir -p .bench-baseline
make test-bench BENCHTIME=3s BENCHCOUNT=10 > .bench-baseline/main.txt
```

Three-second benchtime × ten runs gives benchstat enough samples to compute a stable variance. The file is committed to the repo.

Inspect the output — it should contain every `Benchmark*` target in both `lang/go/codec/` and `lang/go/integration/` with `ns/op`, `B/op`, and `allocs/op`. Reference numbers at the time of this plan (AMD Ryzen 9 9950X3D, Go 1.23, BENCHTIME=100x — re-capture under BENCHTIME=3s × 10 for the real baseline):

| Benchmark                                          | ns/op  | B/op | allocs/op |
|----------------------------------------------------|--------|------|-----------|
| `Fixture_Codec/Codec/MarshalTo`                    | ~30    | 0    | **0**     |
| `Fixture_Codec/Codec/Unmarshal`                    | ~300   | 288  | 6         |
| `Patch_Codec/Codec/MarshalTo`                      | ~18    | 0    | **0**     |
| `Patch_Codec/Codec/Unmarshal`                      | ~150   | 112  | 2         |
| `Evidence_Codec/Codec/MarshalTo`                   | ~33    | 0    | **0**     |
| `Evidence_Codec/Codec/Unmarshal`                   | ~340   | 368  | 5         |
| `NumericOnly_Codec/Codec/MarshalTo`                | ~14    | 0    | **0**     |
| `NumericOnly_Codec/Codec/Unmarshal`                | ~28    | 48   | **1**     |

Any PR that pushes any of these numbers higher must fail the regression gate.

- [ ] **Step 2: Add `bench-compare.sh`**

Create `scripts/bench-compare.sh`:

```bash
#!/usr/bin/env bash
# Compare current benchmarks against .bench-baseline/main.txt using benchstat.
# Fails if any ns/op regresses >5% OR any B/op or allocs/op increases at all.
set -euo pipefail

BASELINE=.bench-baseline/main.txt
CURRENT=$(mktemp)
trap "rm -f $CURRENT" EXIT

if [[ ! -f "$BASELINE" ]]; then
    echo "missing $BASELINE — run: make bench-baseline"
    exit 1
fi

# Match baseline capture settings.
go test -run='^$' -bench=. -benchmem -benchtime=3s -count=10 ./... > "$CURRENT"

# benchstat exits non-zero only for parse errors, not for regressions;
# we parse its output ourselves to enforce policy.
BENCHSTAT_OUT=$(go run golang.org/x/perf/cmd/benchstat@latest \
    -col '.file' -row '.name' "$BASELINE" "$CURRENT")

echo "$BENCHSTAT_OUT"

# Allocation regression: any row where B/op or allocs/op delta is positive.
ALLOC_REGRESSIONS=$(echo "$BENCHSTAT_OUT" | awk '
    /B\/op|allocs\/op/ { in_alloc = 1; next }
    /ns\/op/           { in_alloc = 0; next }
    in_alloc && /\+[0-9]+\.[0-9]+%/ { print }
')
if [[ -n "$ALLOC_REGRESSIONS" ]]; then
    echo "ALLOCATION REGRESSION:"
    echo "$ALLOC_REGRESSIONS"
    exit 2
fi

# Time regression: any ns/op row with >5% positive delta (and statistical significance).
TIME_REGRESSIONS=$(echo "$BENCHSTAT_OUT" | awk '
    /ns\/op/ { in_time = 1; next }
    /B\/op|allocs\/op/ { in_time = 0; next }
    in_time && match($0, /\+([0-9]+)\.[0-9]+%/, m) && m[1]+0 > 5 { print }
')
if [[ -n "$TIME_REGRESSIONS" ]]; then
    echo "TIME REGRESSION (>5% ns/op):"
    echo "$TIME_REGRESSIONS"
    exit 3
fi

echo "no regressions vs $BASELINE"
```

Mark executable: `chmod +x scripts/bench-compare.sh`.

- [ ] **Step 3: Add Makefile targets**

Append to `Makefile`:

```make
# Refresh the committed baseline. Run after intentional perf changes land.
bench-baseline:
	mkdir -p .bench-baseline
	$(GO) test -run='^$$' -bench=. -benchmem -benchtime=3s -count=10 ./... > .bench-baseline/main.txt

# Compare current benchmarks against the committed baseline. Fails on regressions.
bench-compare:
	./scripts/bench-compare.sh
```

Add `bench-baseline bench-compare` to `.PHONY`.

- [ ] **Step 4: Verify locally**

Run: `make bench-compare`
Expected: "no regressions vs .bench-baseline/main.txt".

Sanity-check by deliberately introducing a regression in a throwaway branch (e.g. adding a useless `make([]byte, 16)` in `MarshalToCodec`) — `make bench-compare` should exit non-zero with the offending benchmark listed. Then revert.

- [ ] **Step 5: Wire into CI (depends on Task 5.13)**

Task 5.13's `performance-gate` job is rewritten to:

```yaml
  performance-gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: make bench-compare
```

No `|| true`. A regression blocks the PR until either the code is fixed or the baseline is deliberately refreshed via `make bench-baseline` (which requires a reviewer to understand *why* the number changed).

- [ ] **Step 6: Document refresh policy**

Append to `docs/testing.md` (created in Task 6.0) a **Baseline refresh** section:

> The benchmark baseline at `.bench-baseline/main.txt` is committed to the repo and enforced by CI. A PR may refresh it with `make bench-baseline` when:
>
> 1. The PR intentionally changes performance (e.g. Phase 1 slab fix, Phase 5 alloc reduction).
> 2. The hardware changed (CI runner upgrade).
> 3. A measurement-noise PR that proves the regression was spurious (re-run with `BENCHCOUNT=20`).
>
> Never refresh the baseline just to make CI pass. The baseline is a contract; if CI says a change regresses, either the change is wrong or the baseline needs to move for a documented reason.

- [ ] **Step 7: Commit**

```bash
git add .bench-baseline/main.txt scripts/bench-compare.sh Makefile
git commit -m "ci: freeze benchmark baseline and add regression comparator"
```

---

### Task 5.1: Benchmark allocation assertions

**Why:** Docs promise "zero allocations" for `MarshalToCodec` and "single slab allocation" for slab-eligible `UnmarshalCodec`. Benchmarks exist but do not verify.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Add a benchmark-as-test using `testing.AllocsPerRun`**

Add:

```go
func TestFixture_MarshalToCodec_ZeroAllocs(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	buf := make([]byte, f.SizeCodec())
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = f.MarshalToCodec(buf)
	})
	if allocs != 0 {
		t.Fatalf("MarshalToCodec allocated %.0f times per call, expected 0", allocs)
	}
}

func TestFixture_UnmarshalCodec_SingleAlloc(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	data, _ := f.MarshalCodec()
	var got integration.Fixture
	allocs := testing.AllocsPerRun(100, func() {
		got.UnmarshalCodec(data)
	})
	// Slab-eligible message (strings + bytes) should allocate 1-2 times:
	// the slab and the repeated-slice backing. Tune empirically.
	if allocs > 3 {
		t.Fatalf("UnmarshalCodec allocated %.1f times per call, want <= 3", allocs)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./lang/go/integration/ -run 'ZeroAllocs|SingleAlloc' -v`
Expected: PASS. If not, investigate — the slab implementation may have a hidden allocation (e.g. `strings.Builder` internal growth).

- [ ] **Step 3: Commit**

```bash
git add lang/go/integration/fixture_test.go
git commit -m "test(go): assert zero-alloc marshal and bounded-alloc unmarshal"
```

---

### Task 5.2: `keep_capacity` coverage

**Why:** No fixture field currently uses `(codec.keep_capacity) = true`. The relevant emitter branches in `gen_reset.go:34-38` and `gen_reset.go:51-55` have no test coverage.

**Files:**
- Modify: `lang/go/integration/fixture.proto`, `fixture.go`, `fixture_test.go`

- [ ] **Step 1: Add a field with `keep_capacity`**

Edit `fixture.proto` — add a new message:

```protobuf
message PooledBuf {
  option (codec.type) = "PooledBuf";
  repeated string tags = 1 [(codec.field) = "Tags", (codec.keep_capacity) = true];
  bytes data = 2 [(codec.field) = "Data", (codec.keep_capacity) = true];
}
```

Add the Go struct:

```go
type PooledBuf struct {
	Tags []string
	Data []byte
}
```

- [ ] **Step 2: Test that ResetCodec preserves capacity**

```go
func TestPooledBuf_ResetCodec_PreservesCapacity(t *testing.T) {
	t.Parallel()
	p := integration.PooledBuf{
		Tags: []string{"a", "b", "c", "d", "e"},
		Data: []byte{1, 2, 3, 4, 5, 6, 7, 8},
	}
	tagsCap := cap(p.Tags)
	dataCap := cap(p.Data)
	p.ResetCodec()
	if len(p.Tags) != 0 || cap(p.Tags) != tagsCap {
		t.Fatalf("tags: len=%d cap=%d want len=0 cap=%d", len(p.Tags), cap(p.Tags), tagsCap)
	}
	if len(p.Data) != 0 || cap(p.Data) != dataCap {
		t.Fatalf("data: len=%d cap=%d want len=0 cap=%d", len(p.Data), cap(p.Data), dataCap)
	}
}

func TestPooledBuf_Roundtrip(t *testing.T) {
	t.Parallel()
	codec.AssertRoundtrip[integration.PooledBuf](t, integration.PooledBuf{
		Tags: []string{"a", "b"},
		Data: []byte{0x01, 0x02, 0x03},
	})
}
```

- [ ] **Step 3: Regenerate and run**

Run: `make generate && make test`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "test(go): keep_capacity preserves slice backing arrays on reset"
```

---

### Task 5.3: Nil-data `UnmarshalCodec` coverage

**Why:** `codec/testing.go:AssertNilSafe` never calls `UnmarshalCodec(nil)`. Empty input should produce no error and a zero value.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/codec/testing.go`

- [ ] **Step 1: Extend `AssertNilSafe`**

```go
if err := nilPtr.UnmarshalCodec(nil); err == nil {
	// nil pointer — the call should either no-op or panic gracefully
	// (which this pattern already returns an error when m == nil).
}
// Also test a zero pointer with empty input:
var zero T
if err := PT(&zero).UnmarshalCodec(nil); err != nil {
	t.Fatalf("UnmarshalCodec(nil) on zero: %v", err)
}
```

Actually the nilPtr case is tricky because UnmarshalCodec on a nil pointer dereferences. Reword:

```go
// AssertNilSafe verifies nil pointer safety for Size/Marshal/Reset and
// empty-data safety for Unmarshal on a zero-value receiver.
func AssertNilSafe[T any, PT interface {
	*T
	Marshaler
}](t TB) {
	t.Helper()
	var nilPtr PT
	if nilPtr.SizeCodec() != 0 {
		t.Fatalf("nil SizeCodec should be 0")
	}
	buf, err := nilPtr.MarshalCodec()
	if err != nil || buf != nil {
		t.Fatalf("nil MarshalCodec: buf=%v err=%v", buf, err)
	}
	n, err := nilPtr.MarshalToCodec(nil)
	if err != nil || n != 0 {
		t.Fatalf("nil MarshalToCodec: n=%d err=%v", n, err)
	}
	nilPtr.ResetCodec()

	// Empty-data unmarshal on a zero-value receiver must not error.
	var zero T
	if err := PT(&zero).UnmarshalCodec(nil); err != nil {
		t.Fatalf("UnmarshalCodec(nil): %v", err)
	}
	if err := PT(&zero).UnmarshalCodec([]byte{}); err != nil {
		t.Fatalf("UnmarshalCodec([]byte{}): %v", err)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `make test`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add codec/testing.go
git commit -m "test: AssertNilSafe now exercises empty-data unmarshal"
```

---

### Task 5.4: Size-equals-marshal PBT invariant

**Why:** Every fixture manually calls `AssertRoundtrip` which checks size accuracy as a side effect, but there is no standalone PBT that asserts `SizeCodec() == len(MarshalCodec())` across all inputs.

**Files:**
- Modify: `lang/go/integration/fixture_test.go`

- [ ] **Step 1: Add invariant test**

```go
func TestSizeCodecMatchesMarshal_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		f := buildFixturePBT(t) // helper reusing the existing Draw calls
		size := f.SizeCodec()
		buf, err := f.MarshalCodec()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if len(buf) != size && !(size == 0 && buf == nil) {
			t.Fatalf("SizeCodec()=%d vs len(MarshalCodec())=%d", size, len(buf))
		}
	})
}
```

Factor the fixture-construction from `TestFixture_Roundtrip_PBT` into `buildFixturePBT(t *rapid.T)`.

- [ ] **Step 2: Run tests**

Run: `go test ./lang/go/integration/ -run TestSizeCodecMatchesMarshal_PBT -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add lang/go/integration/fixture_test.go
git commit -m "test(go): PBT for SizeCodec==len(MarshalCodec) invariant"
```

---

### Task 5.5: Fuzz roundtrip covers full struct

**Why:** `FuzzFixture_Roundtrip` at `fixture_test.go:461-483` only compares three fields (`ID`, `Kind`, `Ref`). Extend to full equality.

**Files:**
- Modify: `lang/go/integration/fixture_test.go`

- [ ] **Step 1: Replace partial checks with `reflect.DeepEqual`**

In `FuzzFixture_Roundtrip`, `FuzzPatch_Roundtrip`, `FuzzEvidence_Roundtrip`:

```go
if !reflect.DeepEqual(first, second) {
	t.Fatalf("roundtrip mismatch:\n  first:  %+v\n  second: %+v", first, second)
}
```

- [ ] **Step 2: Run seeded fuzz to ensure current corpus still passes**

Run: `go test ./lang/go/integration/ -run 'FuzzFixture_Roundtrip|FuzzPatch_Roundtrip|FuzzEvidence_Roundtrip' -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add lang/go/integration/fixture_test.go
git commit -m "test(go): fuzz roundtrip checks full struct equality"
```

---

### Task 5.6: Rapid PBT law suite for every fixture

**Why:** Current PBT is limited to roundtrip. Prove the full algebraic law set (roundtrip, size identity, reset identity, reset idempotence, marshal determinism, receiver-reuse equivalence) for every generated message.

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/codec/laws.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Create a reusable law-check helper**

Create `codec/laws.go`:

```go
// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"bytes"
	"reflect"
)

// AssertLaws proves every codec algebraic law for a single input.
// It is intended to be called inside rapid.Check with a drawn value of T.
func AssertLaws[T any, PT interface {
	*T
	Marshaler
}](t TB, original T) {
	t.Helper()
	ptr := PT(&original)

	// Law: SizeCodec == len(MarshalCodec)
	size := ptr.SizeCodec()
	buf, err := ptr.MarshalCodec()
	if err != nil {
		t.Fatalf("MarshalCodec: %v", err)
	}
	if !(buf == nil && size == 0) && len(buf) != size {
		t.Fatalf("SizeCodec()=%d vs len(MarshalCodec())=%d", size, len(buf))
	}

	// Law: MarshalCodec is deterministic.
	buf2, _ := ptr.MarshalCodec()
	if !bytes.Equal(buf, buf2) {
		t.Fatalf("marshal not deterministic:\n  first:  %x\n  second: %x", buf, buf2)
	}

	// Law: Unmarshal(Marshal(m)) == m (roundtrip).
	var got T
	if err := PT(&got).UnmarshalCodec(buf); err != nil {
		t.Fatalf("UnmarshalCodec: %v", err)
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("roundtrip mismatch:\n  want: %+v\n  got:  %+v", original, got)
	}

	// Law: receiver-reuse equivalence.
	// Unmarshal into a stale receiver must produce the same value as a fresh one.
	stale := original // copy — contains populated fields
	if err := PT(&stale).UnmarshalCodec(buf); err != nil {
		t.Fatalf("reuse UnmarshalCodec: %v", err)
	}
	if !reflect.DeepEqual(got, stale) {
		t.Fatalf("reuse mismatch:\n  fresh: %+v\n  reuse: %+v", got, stale)
	}

	// Law: ResetCodec produces the zero value.
	populated := original
	PT(&populated).ResetCodec()
	var zero T
	if !reflect.DeepEqual(populated, zero) {
		t.Fatalf("ResetCodec did not produce zero:\n  got: %+v", populated)
	}

	// Law: ResetCodec is idempotent.
	populated2 := original
	PT(&populated2).ResetCodec()
	PT(&populated2).ResetCodec()
	if !reflect.DeepEqual(populated2, zero) {
		t.Fatalf("ResetCodec not idempotent:\n  got: %+v", populated2)
	}
}
```

- [ ] **Step 2: Replace `AssertRoundtrip` callsites in `*_PBT` tests with `AssertLaws`**

Edit `lang/go/integration/fixture_test.go` — for every existing `*_PBT` function, replace `codec.AssertRoundtrip[...]` with `codec.AssertLaws[...]`. Example:

```go
func TestFixture_Roundtrip_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		f := buildFixturePBT(t)
		codec.AssertLaws[integration.Fixture](t, f)
	})
}
```

- [ ] **Step 3: Add PBT law tests for fixtures that currently lack them**

Fixtures added in Phase 4 (`Container`, `Inner`, `MapHolder`, `PooledBuf`, `TimestampHolder`) need PBT coverage:

```go
func TestContainer_Laws_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		c := buildContainerPBT(t)
		codec.AssertLaws[integration.Container](t, c)
	})
}

// buildContainerPBT and peers are small helpers that draw the struct with rapid.
```

- [ ] **Step 4: Run full PBT suite**

Run: `go test ./lang/go/integration/ -run '_PBT$' -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add codec/laws.go lang/go/integration/fixture_test.go
git commit -m "test: AssertLaws enforces roundtrip, size, reset, determinism, reuse invariants"
```

---

### Task 5.7: Wire-primitive PBT

**Why:** `DecodeVarint`, `EncodeVarint`, `Sov`, and zigzag helpers are the foundation — prove their bijections and size relations.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/codec/wire_test.go`

- [ ] **Step 1: Add PBT for varint bijection**

Add to `codec/wire_test.go`:

```go
import "pgregory.net/rapid"

func TestVarint_Bijection_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Uint64().Draw(t, "v")
		var buf [10]byte
		n := EncodeVarint(buf[:], v)
		got, rn := DecodeVarint(buf[:n])
		if rn != n {
			t.Fatalf("bytes mismatch: wrote %d read %d", n, rn)
		}
		if got != v {
			t.Fatalf("roundtrip: want %d got %d", v, got)
		}
	})
}

func TestSov_MatchesEncodedLength_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Uint64().Draw(t, "v")
		var buf [10]byte
		n := EncodeVarint(buf[:], v)
		if Sov(v) != n {
			t.Fatalf("Sov(%d)=%d but EncodeVarint wrote %d", v, Sov(v), n)
		}
	})
}

func TestZigzag64_Bijection_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Int64().Draw(t, "v")
		if got := ZigzagDecode64(ZigzagEncode64(v)); got != v {
			t.Fatalf("zigzag64(%d)=%d", v, got)
		}
	})
}

func TestZigzag32_Bijection_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Int32().Draw(t, "v")
		if got := ZigzagDecode32(ZigzagEncode32(v)); got != v {
			t.Fatalf("zigzag32(%d)=%d", v, got)
		}
	})
}
```

Note: `lang/go/codec` and `lang/go/integration` both depend on `pgregory.net/rapid`; it is already a transitive dependency via the integration fixture.

- [ ] **Step 2: Run**

Run: `go test ./codec/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add codec/wire_test.go
git commit -m "test(codec): PBT for varint bijection, Sov correctness, zigzag bijections"
```

---

### Task 5.8: Wire-format compatibility with `google.golang.org/protobuf`

**Why:** We claim protobuf wire compatibility. Prove it by round-tripping our output through the standard library's `proto.Unmarshal` using generated go structs.

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/wirecompat_test.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/buf.gen.yaml` (add `protoc-gen-go` output)

- [ ] **Step 1: Add a separate `buf.gen.yaml` target for google-protoc-gen-go**

Edit `buf.gen.yaml` to additionally invoke `protoc-gen-go` into a sibling package `lang/go/integration/pb`:

```yaml
plugins:
  - remote: buf.build/protocolbuffers/go
    out: lang/go/integration/pb
    opt: paths=source_relative
```

The `pb` directory will contain the standard-library-generated types (`pb.Fixture`, `pb.Evidence`, ...).

- [ ] **Step 2: Regenerate**

Run: `make generate`
Expected: `lang/go/integration/pb/fixture.pb.go` is created.

- [ ] **Step 3: Write the compatibility test**

Create `lang/go/integration/wirecompat_test.go`:

```go
// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package testdata_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"go.stealthscale.io/protoc-gen-codec/lang/go/integration"
	pb "go.stealthscale.io/protoc-gen-codec/lang/go/integration/pb"
)

func TestFixture_WireCompat_CodecToGoogle(t *testing.T) {
	t.Parallel()
	f := sampleFixture()
	buf, err := f.MarshalCodec()
	if err != nil {
		t.Fatal(err)
	}
	var got pb.Fixture
	if err := proto.Unmarshal(buf, &got); err != nil {
		t.Fatalf("google proto.Unmarshal rejected our output: %v", err)
	}
	if got.GetId() != f.ID {
		t.Fatalf("ID mismatch: want %q got %q", f.ID, got.GetId())
	}
	// ... compare each field
}

func TestFixture_WireCompat_GoogleToCodec(t *testing.T) {
	t.Parallel()
	pbf := &pb.Fixture{
		Id: "fix-001", Kind: 42, Status: 2,
		Score: -85_000_000, Sequence: 12345, Enabled: true,
	}
	buf, err := proto.Marshal(pbf)
	if err != nil {
		t.Fatal(err)
	}
	var got integration.Fixture
	if err := got.UnmarshalCodec(buf); err != nil {
		t.Fatalf("our UnmarshalCodec rejected google output: %v", err)
	}
	if got.ID != pbf.Id {
		t.Fatalf("ID mismatch")
	}
}
```

- [ ] **Step 4: Extend to every fixture**

Repeat for `Patch`, `Evidence`, `Minimal`, `NumericOnly`, and Phase-4 fixtures (`Container`, `MapHolder`, `PooledBuf`, `TimestampHolder`).

- [ ] **Step 5: PBT version**

```go
func TestFixture_WireCompat_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		f := buildFixturePBT(t)
		buf, err := f.MarshalCodec()
		if err != nil {
			t.Fatal(err)
		}
		var got pb.Fixture
		if err := proto.Unmarshal(buf, &got); err != nil {
			t.Fatalf("google rejected us: %v", err)
		}
	})
}
```

- [ ] **Step 6: Run**

Run: `make test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add buf.gen.yaml lang/go/integration/pb/ lang/go/integration/wirecompat_test.go
git commit -m "test: cross-check wire compatibility with google.golang.org/protobuf"
```

---

### Task 5.9: Fuzz coverage for every wire primitive and generated type

**Why:** Existing fuzz targets cover only `Fixture`, `Patch`, `Evidence`, `NumericOnly` unmarshal. Every `[]byte`-consuming surface must have a fuzz target with a ≥5-entry seed corpus and a no-panic assertion.

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/codec/fuzz_test.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Runtime fuzz targets**

Create `codec/fuzz_test.go`:

```go
package codec

import "testing"

func FuzzDecodeVarint(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x01})
	f.Add([]byte{0x80, 0x01})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0x0f})
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = DecodeVarint(data)
	})
}

func FuzzSkipField(f *testing.F) {
	// Seed: tag+payload for each wire type.
	f.Add(byte(0), []byte{0x00})
	f.Add(byte(1), []byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add(byte(2), []byte{0x03, 1, 2, 3})
	f.Add(byte(5), []byte{1, 2, 3, 4})
	f.Add(byte(7), []byte{})
	f.Fuzz(func(_ *testing.T, wt byte, data []byte) {
		_, _ = SkipField(data, uint64(wt))
	})
}

func FuzzDecodeTimestamp(f *testing.F) {
	f.Add([]byte{}) //nolint:errcheck
	f.Add([]byte{0x08, 0x00})
	f.Add([]byte{0x08, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x10, 0x01})
	f.Add([]byte{0xff})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = DecodeTimestamp(data)
	})
}

// Same pattern for DecodeDuration.
```

- [ ] **Step 2: Generated-type fuzz targets**

Every `<T>.UnmarshalCodec` needs a `Fuzz<T>_Unmarshal` target. Pattern (already present for `Fixture`):

```go
func FuzzPatch_Unmarshal(f *testing.F) {
	seedWith(f, samplePatchText, samplePatchFixed64, samplePatchBlob)
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff})
	f.Fuzz(func(_ *testing.T, data []byte) {
		var p integration.Patch
		_ = p.UnmarshalCodec(data) //nolint:errcheck
	})
}

func seedWith[T any, PT interface {
	*T
	Marshaler
}](f *testing.F, samples ...func() T) {
	for _, s := range samples {
		v := s()
		if buf, _ := PT(&v).MarshalCodec(); buf != nil {
			f.Add(buf)
		}
	}
}
```

Add this for every fixture type: `Fixture`, `Patch`, `Evidence`, `Minimal`, `NumericOnly`, `Container`, `Inner`, `MapHolder`, `PooledBuf`, `TimestampHolder`.

- [ ] **Step 3: Extend `make test-fuzz` to list every target**

The existing recipe auto-discovers `Fuzz*` targets, so no change needed — just confirm that the new targets run by inspecting `go test -list '^Fuzz' ./...`.

- [ ] **Step 4: Run a short fuzz smoke test**

Run: `go test -run '^$' -fuzz=FuzzDecodeVarint -fuzztime=10s ./codec/`
Expected: no panic; completes after 10s.

Repeat briefly for each new target (5-10s each) to ensure no immediate failures.

- [ ] **Step 5: Commit**

```bash
git add codec/fuzz_test.go lang/go/integration/fixture_test.go
git commit -m "test: fuzz targets for every wire primitive and generated type"
```

---

### Task 5.10: Benchmark tiers T0-T3 with automatic allocation gates

**Why:** Per the project's testing practice, allocation budgets are hard gates. Define tiers and enforce them in CI.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Document tier assignments as table-driven tests**

Add to `fixture_test.go`:

```go
// Tier assignments for allocation gates.
// T0: MarshalToCodec into pre-sized buffer  — 0 allocs/op
// T1: UnmarshalCodec on slabNaive / slabNone — ≤ 1 alloc/op
// T2: UnmarshalCodec on slabSmart            — ≤ 3 allocs/op
// T3: MarshalCodec (buffer allocator)        — ≤ 1 alloc/op

func TestAllocGates_T0_MarshalToCodec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func() float64
	}{
		{"Fixture", func() float64 {
			f := sampleFixture()
			buf := make([]byte, f.SizeCodec())
			return testing.AllocsPerRun(100, func() { _, _ = f.MarshalToCodec(buf) })
		}},
		{"Patch", func() float64 {
			p := samplePatchText()
			buf := make([]byte, p.SizeCodec())
			return testing.AllocsPerRun(100, func() { _, _ = p.MarshalToCodec(buf) })
		}},
		{"Evidence", func() float64 {
			e := sampleEvidence()
			buf := make([]byte, e.SizeCodec())
			return testing.AllocsPerRun(100, func() { _, _ = e.MarshalToCodec(buf) })
		}},
		{"NumericOnly", func() float64 {
			n := sampleNumericOnly()
			buf := make([]byte, n.SizeCodec())
			return testing.AllocsPerRun(100, func() { _, _ = n.MarshalToCodec(buf) })
		}},
	}
	for _, c := range cases {
		if allocs := c.run(); allocs != 0 {
			t.Errorf("T0 gate: %s allocated %.0f, want 0", c.name, allocs)
		}
	}
}

func TestAllocGates_T1_UnmarshalSlabNaiveOrNone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func() float64
	}{
		{"Minimal", func() float64 { // slabNaive
			s := sampleMinimal()
			data, _ := s.MarshalCodec()
			var got integration.Minimal
			return testing.AllocsPerRun(100, func() { _ = got.UnmarshalCodec(data) })
		}},
		{"NumericOnly", func() float64 { // slabNone
			s := sampleNumericOnly()
			data, _ := s.MarshalCodec()
			var got integration.NumericOnly
			return testing.AllocsPerRun(100, func() { _ = got.UnmarshalCodec(data) })
		}},
	}
	for _, c := range cases {
		if allocs := c.run(); allocs > 1 {
			t.Errorf("T1 gate: %s allocated %.1f, want ≤ 1", c.name, allocs)
		}
	}
}

func TestAllocGates_T2_UnmarshalSlabSmart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func() float64
	}{
		{"Fixture", func() float64 {
			s := sampleFixture()
			data, _ := s.MarshalCodec()
			var got integration.Fixture
			return testing.AllocsPerRun(100, func() { _ = got.UnmarshalCodec(data) })
		}},
		{"Evidence", func() float64 {
			s := sampleEvidence()
			data, _ := s.MarshalCodec()
			var got integration.Evidence
			return testing.AllocsPerRun(100, func() { _ = got.UnmarshalCodec(data) })
		}},
	}
	for _, c := range cases {
		if allocs := c.run(); allocs > 3 {
			t.Errorf("T2 gate: %s allocated %.1f, want ≤ 3", c.name, allocs)
		}
	}
}

func TestAllocGates_T3_MarshalCodec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func() float64
	}{
		{"Fixture", func() float64 {
			f := sampleFixture()
			return testing.AllocsPerRun(100, func() { _, _ = f.MarshalCodec() })
		}},
	}
	for _, c := range cases {
		if allocs := c.run(); allocs > 1 {
			t.Errorf("T3 gate: %s allocated %.1f, want ≤ 1", c.name, allocs)
		}
	}
}
```

- [ ] **Step 2: Delete the earlier ad-hoc `TestFixture_*Allocs` tests**

The Task 5.1 tests are superseded by the tier tests above. Remove them.

- [ ] **Step 3: Run**

Run: `go test ./lang/go/integration/ -run TestAllocGates_ -v`
Expected: PASS. If any tier fails, investigate per-message rather than loosening the gate.

- [ ] **Step 4: Commit**

```bash
git add lang/go/integration/fixture_test.go
git commit -m "test(go): T0-T3 allocation gates for MarshalToCodec / UnmarshalCodec / MarshalCodec"
```

---

### Task 5.11: Generated-code coverage instrumentation + 100% gate

**Why:** Goal: every line of `lang/go/integration/*.codec.go` is exercised by the test suite. This both catches silent regressions in codegen and ensures the generator's emit paths are tested.

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/scripts/coverage-gate.sh`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/Makefile`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`

- [ ] **Step 1: Add adversarial coverage tests for each fixture**

For 100% coverage on the generated file, each generated error arm needs a targeted input. Add to `fixture_test.go`:

```go
// coverageHelper builds a wire record with (fieldNum, wireType, payload).
func wireRecord(fieldNum int32, wireType uint8, payload ...byte) []byte {
	tag := uint64(fieldNum)<<3 | uint64(wireType)
	var buf []byte
	// emit tag varint
	for tag >= 0x80 {
		buf = append(buf, byte(tag)|0x80)
		tag >>= 7
	}
	buf = append(buf, byte(tag))
	return append(buf, payload...)
}

func TestFixture_Coverage_EveryFieldPath(t *testing.T) {
	t.Parallel()
	// Each subtest hits one field's happy path; combined with zero-value and
	// error tests, every emitted case arm is exercised.
	type sub struct {
		name string
		f    integration.Fixture
	}
	cases := []sub{
		{"IDOnly", integration.Fixture{ID: "x"}},
		{"KindOnly", integration.Fixture{Kind: 1}},
		{"StatusOnly", integration.Fixture{Status: integration.StatusRunning}},
		{"ScoreOnly", integration.Fixture{Score: -1}},
		{"SequenceOnly", integration.Fixture{Sequence: 1}},
		{"EnabledOnly", integration.Fixture{Enabled: true}},
		{"TimestampOnly", integration.Fixture{Timestamp: 1}},
		{"RefOnly", integration.Fixture{Ref: digest(0xAA)}},
		{"TagsOnly", integration.Fixture{Tags: []string{"a"}}},
		{"DataOnly", integration.Fixture{Data: []byte{1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			codec.AssertRoundtrip[integration.Fixture](t, c.f)
		})
	}
}

func TestFixture_Coverage_ErrorArms(t *testing.T) {
	t.Parallel()
	// Each subtest triggers exactly one error arm in UnmarshalCodec.
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"bad varint in id length",   []byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, codec.ErrInvalidVarint},
		{"short buffer for id",       []byte{0x0a, 0x10},                                 codec.ErrBufferTooShort},
		{"wire type mismatch id",     []byte{0x09, 0, 0, 0, 0, 0, 0, 0, 0},              codec.ErrInvalidWireType},
		{"ref wrong length",          append([]byte{0x42, 31}, make([]byte, 31)...),     codec.ErrInvalidLength},
		// ... one entry per error arm per field
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got integration.Fixture
			if err := got.UnmarshalCodec(c.data); !stderrors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
}
```

Repeat `TestX_Coverage_EveryFieldPath` and `TestX_Coverage_ErrorArms` for every fixture. This is mechanical but necessary.

- [ ] **Step 2: MarshalToCodec short-buffer coverage for each message**

```go
func TestCoverage_MarshalToCodec_ShortBuffer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		do   func(buf []byte) error
	}{
		{"Fixture",     func(b []byte) error { f := sampleFixture();     _, err := f.MarshalToCodec(b); return err }},
		{"Patch",       func(b []byte) error { p := samplePatchText();   _, err := p.MarshalToCodec(b); return err }},
		// ... repeat for every fixture
	}
	for _, c := range cases {
		if err := c.do(nil); !stderrors.Is(err, codec.ErrBufferTooShort) {
			t.Fatalf("%s: want ErrBufferTooShort, got %v", c.name, err)
		}
	}
}
```

- [ ] **Step 3: Coverage of unknown-field skip**

```go
func TestCoverage_UnknownField_Skip(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Fixture", "Patch", "Evidence", "Minimal", "NumericOnly"} {
		t.Run(name, func(t *testing.T) {
			// Construct: tag 9999 (varint wire type) + payload 0.
			// Use a real marshal as base, append unknown field.
			// Omitted here for brevity — pattern mirrors TestFixture_UnknownField.
		})
	}
}
```

- [ ] **Step 4: Coverage-gate script**

Create `scripts/coverage-gate.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

THRESHOLD=100
OUT=$(mktemp)
trap "rm -f $OUT" EXIT

go test -coverpkg=go.stealthscale.io/protoc-gen-codec/lang/go/integration \
  -coverprofile="$OUT" ./lang/go/integration/ > /dev/null

# Aggregate coverage for files matching *.codec.go.
go tool cover -func="$OUT" | awk -v t=$THRESHOLD '
  /\.codec\.go:/ {
    n = split($3, parts, "%")
    pct = parts[1]
    total += pct
    count++
    if (pct < t) {
      printf "BELOW THRESHOLD: %s %.1f%% (< %d%%)\n", $1, pct, t
      fail = 1
    }
  }
  END {
    if (count == 0) { print "no generated files in coverage report"; exit 1 }
    printf "average coverage on generated code: %.1f%% across %d files\n", total/count, count
    if (fail) exit 2
  }
'
```

Mark executable: `chmod +x scripts/coverage-gate.sh`.

- [ ] **Step 5: Makefile target**

Append to `Makefile`:

```make
cover-gen:
	./scripts/coverage-gate.sh
```

- [ ] **Step 6: Run and iterate**

Run: `make cover-gen`
Expected: every `*.codec.go` at 100%. If any file is under, read the `go tool cover -html` output to find the uncovered lines and add focused tests.

Iterate until gate passes.

- [ ] **Step 7: Commit**

```bash
git add scripts/coverage-gate.sh Makefile lang/go/integration/fixture_test.go
git commit -m "test(go): 100% line-coverage gate on generated code"
```

---

### Task 5.12: Determinism gate

**Why:** `MarshalCodec(m)` must be byte-identical across invocations for the same input. `make generate` must be byte-identical across runs. Both are invariants.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/lang/go/integration/fixture_test.go`
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/Makefile`

- [ ] **Step 1: Marshal determinism PBT**

```go
func TestMarshal_Determinism_PBT(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		f := buildFixturePBT(t)
		a, _ := f.MarshalCodec()
		b, _ := f.MarshalCodec()
		if !bytes.Equal(a, b) {
			t.Fatalf("non-deterministic:\n  a: %x\n  b: %x", a, b)
		}
	})
}
```

This is already implied by `AssertLaws` (Task 5.6), but a dedicated test documents the invariant explicitly. Keep if `AssertLaws` hasn't been introduced yet; otherwise this is redundant and can be omitted.

- [ ] **Step 2: Generator determinism gate in Makefile**

Append:

```make
verify-deterministic-gen:
	@mkdir -p .gen-determinism
	$(MAKE) generate
	cp lang/go/integration/fixture.codec.go .gen-determinism/first.codec.go
	@rm lang/go/integration/fixture.codec.go
	$(MAKE) generate
	@if ! diff -q lang/go/integration/fixture.codec.go .gen-determinism/first.codec.go > /dev/null; then \
		echo "GENERATOR NOT DETERMINISTIC"; \
		diff lang/go/integration/fixture.codec.go .gen-determinism/first.codec.go; \
		exit 1; \
	fi
	@rm -rf .gen-determinism
	@echo "generator output is byte-identical across runs"
```

- [ ] **Step 3: Run both gates**

Run: `make verify-deterministic-gen && go test ./lang/go/integration/ -run TestMarshal_Determinism_PBT`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add lang/go/integration/fixture_test.go Makefile
git commit -m "test: determinism gates for MarshalCodec and make generate"
```

---

### Task 5.13: CI workflow enforces every gate

**Why:** Tests only matter if CI runs them. Wire the five hard stops (allocation, performance, fuzzing, determinism, coverage) plus the wire-format check into the CI workflow.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/.github/workflows/ci.yml`

- [ ] **Step 1: Inspect current CI**

Run: `cat .github/workflows/ci.yml`
Expected: existing jobs that run `make test` and `make lint`.

- [ ] **Step 2: Extend with the five gates**

Edit the workflow to add:

```yaml
  allocation-gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: make test
        env:
          GOFLAGS: '-run=TestAllocGates_'

  performance-gate:
    # Fails if any ns/op regresses >5% or any B/op / allocs/op regresses at all
    # vs .bench-baseline/main.txt. See Task 5.0.
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: make bench-compare

  fuzz-gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      # PR fuzz: 60s per target. main branch: 24h (separate workflow).
      - run: make test-fuzz
        env:
          FUZZTIME: ${{ github.ref == 'refs/heads/main' && '24h' || '60s' }}

  coverage-gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: make cover-gen

  determinism-gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: make verify-deterministic-gen
```

- [ ] **Step 3: Adjust `make test-fuzz` to honour FUZZTIME env var**

Edit `Makefile`:

```make
FUZZTIME ?= 30s

test-fuzz:
	@for target in $$($(GO) test -list '^Fuzz' ./... 2>/dev/null | grep '^Fuzz'); do \
		pkg=$$($(GO) test -list "^$$target$$" ./... 2>/dev/null | grep -v '^Fuzz' | head -1); \
		echo "Fuzzing $$target in $$pkg for $(FUZZTIME)..."; \
		$(GO) test -run="^$$target$$" -fuzz="^$$target$$" -fuzztime=$(FUZZTIME) $$pkg || exit 1; \
	done
```

- [ ] **Step 4: Baseline already captured in Task 5.0**

`.bench-baseline/main.txt` is committed as part of Task 5.0. Nothing to do here beyond confirming the file is present before this CI job runs.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml Makefile .bench-baseline/main.txt
git commit -m "ci: enforce allocation, performance, fuzz, coverage, determinism gates"
```

---

## Phase 6 — Docs Alignment

### Task 6.0: Document the testing strategy

**Why:** Phase 5 introduced the law suite, fuzz matrix, tier system, and CI gates. Future contributors need a single doc describing these disciplines and the 100%-coverage policy on generated code.

**Files:**
- Create: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/docs/testing.md`

- [ ] **Step 1: Write the testing doc**

Create `docs/testing.md` covering:
- **Laws we prove** (table: roundtrip, size identity, marshal determinism, reset identity, reset idempotence, receiver-reuse equivalence, wire compatibility).
- **Fuzz matrix** (list of `Fuzz*` targets and which surface each covers).
- **Benchmark tiers** (T0-T3 with their allocation caps and the commands to measure).
- **CI hard stops** (allocation / performance / fuzz / determinism / coverage / wire-format gates with the policy for each).
- **Coverage policy**: `lang/go/integration/*.codec.go` must reach 100% line coverage; the gate runs in CI via `scripts/coverage-gate.sh`.
- **Adding a new fixture**: checklist — proto message, Go struct, sample builder, `AssertLaws` PBT, `Fuzz<T>_Unmarshal`, tier-aware alloc gate, error-arm coverage tests, wire-compat test.

- [ ] **Step 2: Link from README**

Append to the Documentation section of `README.md`:

```markdown
- [`docs/testing.md`](docs/testing.md) — test disciplines, fuzz matrix, alloc tiers, CI gates
```

- [ ] **Step 3: Commit**

```bash
git add docs/testing.md README.md
git commit -m "docs: testing disciplines, alloc tiers, and CI gates"
```

---

### Task 6.1: Update `docs/generators/go.md` to reflect implementation

**Why:** The doc currently promises behaviour that didn't exist (full presence, maps, oneof) and doesn't describe behaviour that does (MarshalToCodec buffer guard, UnmarshalCodec reset semantics, zigzag handling). After Phases 1-5, update to match.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/docs/generators/go.md`

- [ ] **Step 1: Update the method table**

Document that `MarshalToCodec` returns `ErrBufferTooShort` for undersized buffers and that `UnmarshalCodec` clears serialized fields before decoding (so callers do not need to call `ResetCodec` first).

- [ ] **Step 2: Update the field-mapping table**

Add rows for `sint32`/`sint64` (zigzag-encoded), `optional` scalar (pointer target), message fields (nested codec delegation), `map<K,V>` (target is `map[K]V`), and `google.protobuf.Timestamp`/`Duration` (target is `time.Time`/`time.Duration`).

- [ ] **Step 3: Note that oneof is explicitly rejected pending RFC**

Add a short section under "Oneof":

> The generator currently rejects proto `oneof` constructs at generation time.
> Full support is tracked under a separate design RFC. Model sparse branches
> manually (enum discriminator + parallel fields) in the interim.

- [ ] **Step 4: Document generation-time errors**

Add a "Generation Errors" section listing every condition that now fails at generation (missing `codec.type`, `codec.fixed_len=0`, `codec.fixed_len` on non-bytes, unresolved cast alias, cast on message field, invalid cast identifier, non-synthetic oneof).

- [ ] **Step 5: Commit**

```bash
git add docs/generators/go.md
git commit -m "docs(go): align with implemented features and generation errors"
```

---

### Task 6.2: Update `docs/architecture.md` to reflect neutral core

**Why:** The architecture doc describes `internal/core` as language-agnostic — after Phase 3 this is actually true. Update the stability-contract wording and point to the exported types.

**Files:**
- Modify: `/var/home/rklopper/Projects/thesmos/protoc-gen-codec/docs/architecture.md`

- [ ] **Step 1: Describe the stable core API**

Under "Schema Analysis Layer", add:

> The exported surface of `internal/core` is:
> - `AnalyzeMessage(msg, fileMap, file, aliasOf)` — returns a `*MessageInfo`.
> - `MessageInfo` — `{TargetType string; Fields []FieldInfo}`.
> - `FieldInfo` — neutral field representation (wire kind, cast ref, presence, map/message/well-known flags).
> - `CastRef` — `{ProtoFile, PackageAlias, Name}` used by emitters to qualify named target types.
> - `WireKind`, `TagValue`, `TagBytes`, `SovLocal`, `WireKindOf` — wire-format primitives.
>
> Future emitters (TS, Rust) reuse the analysis output without modification;
> only the code-emission layer differs per language.

- [ ] **Step 2: Update README features list**

Check `README.md` — references to "Smart Slab unmarshal" still generically describe the behaviour. Add a bullet for optional / nested messages / maps / WKT support now that Phase 4 has shipped those.

- [ ] **Step 3: Commit**

```bash
git add docs/architecture.md README.md
git commit -m "docs: reflect language-neutral core API and Phase 4 feature set"
```

---

### Task 6.3: Final verification pass

**Why:** Before declaring the plan complete, run every tier of checks.

- [ ] **Step 1: Full test suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 2: Race detector**

Run: `make test-race`
Expected: PASS.

- [ ] **Step 3: Fuzz for 30 seconds each**

Run: `make test-fuzz`
Expected: PASS.

- [ ] **Step 4: Lint**

Run: `make lint`
Expected: PASS.

- [ ] **Step 5: Generation is deterministic**

Run: `make verify-deterministic-gen`
Expected: PASS.

- [ ] **Step 6: Coverage gate on generated code**

Run: `make cover-gen`
Expected: every `*.codec.go` at 100%.

- [ ] **Step 7: Allocation tier gates**

Run: `go test -run TestAllocGates_ ./lang/go/integration/ -v`
Expected: PASS for T0-T3.

- [ ] **Step 7b: Baseline regression gate**

Run: `make bench-compare`
Expected: "no regressions vs .bench-baseline/main.txt". Any allocation/time regression vs the committed baseline fails here.

- [ ] **Step 8: Wire-format compatibility**

Run: `go test -run WireCompat ./lang/go/integration/ -v`
Expected: PASS.

- [ ] **Step 9: No `protogen.*` identifiers leak from `internal/core`**

Run: `grep -rn "protogen\." internal/core/*.go | grep -v "_test.go"`
Expected: only descriptor-reading usages (`*protogen.Message`, `*protogen.File`), never `GoIdent`/`GeneratedFile`/`GoPackageName` in `FieldInfo`/`MessageInfo`.

- [ ] **Step 10: Tag a release candidate**

```bash
git tag -a v0.2.0-rc.1 -m "Go codec generator gap closure (Phases 0-6)"
```

Do not push the tag; leave it local for the user to inspect.

---

## Appendix — Out-of-Scope Follow-Ups

The plan intentionally does **not** cover:

1. **Full proto `oneof` support.** Task 4.3 only adds an explicit rejection. Designing the annotation shape (`codec.oneof` with a discriminator) is a follow-up RFC.
2. **Nested message support for recursive/self-referential types.** Task 4.2 handles straight nesting; cycle detection and lazy initialisation are open questions.
3. **TypeScript emitter.** Phase 3 unblocks it but the emitter itself is a separate plan.
4. **`MarshalToCodecUnchecked` for the hot path.** Task 1.3's `SizeCodec()` pre-walk may be too expensive under profiling; if so, add an unchecked variant.
5. **Size caching for nested messages.** Currently `SizeCodec` recurses on every call; for deep trees, a `cachedSize` field can amortize.
6. **`bytes` slab sharing.** Task 1.1 may re-open the slab design; the current `slabSmart` implementation is conservative and can likely be simplified.
7. **Wire-format fuzzing against `google.golang.org/protobuf`.** Cross-check that `MarshalCodec` output decodes under the standard library's `proto.Unmarshal`. Today's tests only roundtrip through our own codec.
8. **`slabNaive` retention tightening.** `slabNaive` currently uses `string(data)` which retains the entire wire buffer via the sub-string backing array. Not a correctness issue, but a memory-retention one; a follow-up can copy only the required range.
9. **`codec.keep_capacity` for maps.** Currently rejected implicitly (maps reset to `nil`). If callers want to reuse map backing storage, add generation support for `clear(m)` as the reset form.
10. **Emitter unit tests.** `internal/lang/golang/*.go` is exercised only indirectly through the fixture. Direct unit tests that assert the emitted Go source for small inputs would catch regressions faster and document intent.
11. **`options.go` uses `GetUnknown()` for annotation extraction.** This is Go-library-specific (it relies on `protoreflect` in `google.golang.org/protobuf`) but not Go-language-specific: any emitter written in Go reuses it freely. A TS emitter implemented in TypeScript would have its own parser and would not share `internal/core`. No action needed unless `internal/core` is ported to a non-Go host.

Each follow-up should get its own spec + plan cycle.
