# Go Codec — Performance Profile

Snapshot of bench numbers for the Go target. Refreshed on demand
(typically per release). This document is the **canonical published
record** of the project's measured performance — the underlying
`.bench-baseline/main.txt` is `.gitignore`d (hardware-specific) and
regenerated locally via `make bench-baseline`, or in CI from `main`
on the runner before comparing against the PR head.

| Field | Value |
|---|---|
| **Last refreshed** | 2026-04-26 |
| **Source commit** | `1db68dc` (post-v1.0.0) |
| **Go version** | `go1.26.1 linux/amd64` |
| **CPU** | AMD Ryzen 9 9950X3D 16-Core |
| **GOMAXPROCS** | 8 (pinned by `make bench-baseline`) |
| **Bench params** | `-benchtime=3s -count=10 -benchmem` |
| **Samples** | 10 per benchmark, mean reported below |
| **Distinct benchmarks** | 86 |

This document is a snapshot, not a contract. The
[`StartContract`-gated benches](../../lang/go/codec/codectest/contract.go)
+ [`make bench-compare`](../../Makefile) gate are what enforce the
runtime contracts — see [Stability promise](../../README.md#stability).

## Headline: zero-alloc marshal

`MarshalToCodec` is **0 allocs/op across all 17 fixtures**, including
the two map-bearing fixtures (`MapHolder`, `BoolMapHolder`). Both
deterministic-key sorting and slab management amortise into the
caller-provided buffer.

```
Fixture          ns/op     B/op   allocs/op
BytesPool         51.5         0          0
Minimal           51.9         0          0
Inner             53.3         0          0
External_Codec    53.0         0          0
OneofPayload      54.5         0          0
Patch             59.0         0          0
TimeHolder        62.1         0          0
NumericOnly       62.5         0          0
Tree              71.6         0          0
Fixture           71.9         0          0
PackedZigzag      72.3         0          0
ValueContainer    74.6         0          0
Container         76.1         0          0
Evidence          78.7         0          0
CrossContainer    83.9         0          0
BoolMapHolder    101.0         0          0
MapHolder        203.1         0          0
```

`MapHolder` is the slowest at 203 ns/op — sorting two map-key sets
of strings before emit is the dominant cost. Still zero-alloc.

## Codec vs JSON (mean of 10 samples)

```
Fixture          Codec/MarshalTo   Codec/Unmarshal   JSON/Marshal   JSON/Unmarshal   M ratio   U ratio
Patch                  59.0 ns           34.7 ns       418.6 ns       2024.4 ns       7.1x     58.4x
Evidence               78.7 ns          119.5 ns       646.9 ns       3090.8 ns       8.2x     25.9x
OneofPayload           54.5 ns           31.2 ns       165.6 ns        733.3 ns       3.0x     23.5x
Fixture                71.9 ns          123.4 ns       540.2 ns       2589.9 ns       7.5x     21.0x
ValueContainer         74.6 ns           75.7 ns       230.0 ns       1441.4 ns       3.1x     19.0x
PackedZigzag           72.3 ns           61.5 ns       186.0 ns       1004.8 ns       2.6x     16.3x
NumericOnly            62.5 ns           59.7 ns       223.8 ns        923.7 ns       3.6x     15.5x
TimeHolder             62.1 ns           28.7 ns       242.1 ns        415.8 ns       3.9x     14.5x
Inner                  53.3 ns           23.7 ns        75.3 ns        319.3 ns       1.4x     13.5x
CrossContainer         83.9 ns          136.7 ns       305.3 ns       1713.5 ns       3.6x     12.5x
External_Codec         53.0 ns           24.0 ns        73.6 ns        289.9 ns       1.4x     12.1x
Container              76.1 ns          117.9 ns       243.3 ns       1446.5 ns       3.2x     12.3x
Tree                   71.6 ns          111.8 ns       200.8 ns       1191.0 ns       2.8x     10.7x
Minimal                51.9 ns           21.9 ns        61.9 ns        220.8 ns       1.2x     10.1x
MapHolder             203.1 ns          163.2 ns       433.4 ns       1109.5 ns       2.1x      6.8x
BytesPool              51.5 ns           22.4 ns        59.0 ns        205.4 ns       1.1x      9.1x
BoolMapHolder         101.0 ns           87.6 ns         (skipped)      (skipped)     —         —
```

`BoolMapHolder` has no JSON baseline because `encoding/json` rejects
`map[bool, V]` (non-string keys). The spec opts out via
`SkipJSONComparisons: true` so the JSON benches don't time
encoding/json's error path.

**Summary**: codec marshal is **1.1×–8.2×** faster than `encoding/json`
(geomean ~3×). Codec unmarshal is **6.8×–58.4×** faster (geomean
~17×). The biggest unmarshal win (`Patch`, 58×) is a sparse struct
where JSON's text parsing dominates; the smallest (`MapHolder`, 6.8×)
is bottlenecked on map insertion which both formats pay for.

## Cold vs warm-path Unmarshal (pointer-pool / slab reuse)

`PooledUnmarshal` benches reuse a primed receiver across iterations,
exercising the slab + pointer-pool reuse machinery. 2-3× speedup
across the board, with numeric-only fixtures dropping to **0 allocs**
on the warm path:

```
Fixture           Cold ns/op   Warm ns/op    Speedup    Warm allocs
Tree                111.8         35.5         3.15x          1
Container           117.9         39.3         3.00x          1
PackedZigzag         61.5         24.0         2.57x          0   ← all-numeric, no slab
MapHolder           163.2         70.0         2.33x          1
NumericOnly          59.7         27.1         2.20x          0   ← all-numeric, no slab
ValueContainer       75.7         39.0         1.94x          1
```

## Cold Unmarshal allocation budget

Each line accounts for shape-specific allocations: the string slab
(1 per top-level decode for any string-bearing message), nested
message pointers, and map buckets. No anomalies.

```
Fixture            B/op   allocs/op   Notes
TimeHolder           32          1     slab only — Timestamp/Duration use stack
External_Codec       29          2     slab + 1 string copy
Inner                40          2     slab + 1 string copy
Minimal              32          2     slab + 1 string copy
Patch               120          2
OneofPayload        128          2
BytesPool            32          2
ValueContainer      208          3
PackedZigzag        224          3
NumericOnly          96          4     optional-pointer slots
BoolMapHolder       288          4
Evidence            432          5
Fixture             360          6
MapHolder           672          6     map bucket allocations
Container           216          7     slab + nested-message pointers
Tree                240          7
CrossContainer      280          7
```

## Wire primitives

Sub-nanosecond on the hot path; zero-alloc throughout.

```
BenchmarkSov                       0.18 ns/op    SizeVarint, branch-predicted
BenchmarkDecodeVarint_SingleByte   0.18 ns/op
BenchmarkEncodeVarint_Small        0.18 ns/op
BenchmarkDecodeVarint_TwoByte      0.38 ns/op
BenchmarkSkipField_Varint          1.28 ns/op
BenchmarkSkipField_LenDelimited    1.29 ns/op
BenchmarkEncodeVarint_Large        2.05 ns/op
BenchmarkDecodeVarint_TenByte      3.10 ns/op    worst case (max-width 10-byte varint)
```

## Refreshing this report

```bash
make bench-baseline                       # ~5–10 min on a 16-core box; writes .bench-baseline/main.txt locally
# read .bench-baseline/main.txt; transcribe headline numbers above
# update the front-matter (date, commit, Go version, CPU)
```

The `.bench-baseline/main.txt` file is `.gitignore`d, so this doc is
the only published record of measured performance. Refresh on:

- Each minor release (so the doc tracks what's actually shipping).
- Any change to fixture shape, sample size, or bench params (the
  numbers won't compare cleanly otherwise).
- A change of dev hardware or Go toolchain (the front-matter
  becomes stale).

## Methodology notes

- **`GOMAXPROCS=8`** is pinned in `make bench-baseline` so the
  baseline is reproducible across dev machines and stays within
  memory headroom on high-core boxes. `make bench-compare` projects
  benchmark names with `-row .name` so the `-N` suffix doesn't break
  comparison across environments with different `GOMAXPROCS`.
- **Sample size**: `-count=10 -benchtime=3s` per benchmark gives
  benchstat enough samples to compute statistical significance.
  Tighter than that and `~` (no significant change) becomes the
  norm; looser and the gate gets noisy.
- **`StartContract` benches** wrap `Codec/MarshalTo` with an inline
  alloc + latency gate (`AllocsMax`, `LatencyMax`) so an
  order-of-magnitude regression fails the bench in-process — the
  benchstat diff catches finer p99 drift.
- **The JSON baseline is informational, not a contract**. Codec
  speedup over JSON is a function of payload shape; nothing in the
  CI gate depends on a particular ratio holding.
