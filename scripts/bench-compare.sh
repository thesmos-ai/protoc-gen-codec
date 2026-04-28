#!/usr/bin/env bash
# Compare current benchmarks against .bench-baseline/main.txt using benchstat.
# Fails if any allocs/op or B/op increases at all, or any sec/op regresses >5%.
#
# Exit codes:
#   0 - no regressions
#   1 - missing baseline / setup error
#   2 - allocation regression (any positive delta in B/op or allocs/op)
#   3 - time regression (>5% positive delta in sec/op)
set -euo pipefail

BASELINE="${BENCH_BASELINE:-.bench-baseline/main.txt}"
CURRENT=$(mktemp)
BENCHSTAT_OUT_FILE=$(mktemp)
trap "rm -f $CURRENT $BENCHSTAT_OUT_FILE" EXIT

if [[ ! -f "$BASELINE" ]]; then
    echo "missing $BASELINE — run: make bench-baseline"
    exit 1
fi

echo "Running benchmarks (this takes ~5 minutes)..."
go test -run='^$' -bench=. -benchmem -benchtime=3s -count=10 \
    ./lang/go/codec/ \
    ./lang/go/integration/ \
    ./lang/go/integration/external/ \
    > "$CURRENT"

# benchstat exits 0 regardless of deltas; parse its output ourselves.
# -row .name strips the -GOMAXPROCS suffix so baselines and CI runs compare
# even if the GOMAXPROCS values differ between environments.
go run golang.org/x/perf/cmd/benchstat@latest -col '.file' -row .name "$BASELINE" "$CURRENT" \
    > "$BENCHSTAT_OUT_FILE"
cat "$BENCHSTAT_OUT_FILE"

# Parser:
#   benchstat emits one table per metric (sec/op, B/op, allocs/op). Each table's
#   header row contains the metric name and "vs base". Per-row deltas appear as
#   "+N.NN%" or "-N.NN%" or "~" (not statistically significant). Geomean rows
#   summarize and are skipped.

# Fail on any positive delta in B/op or allocs/op (any increase is a regression).
ALLOC_REGRESSIONS=$(awk '
    /vs base/ {
        if (match($0, /sec\/op/))         { section = ""; next }
        else if (match($0, /B\/op/))      { section = "B/op"; next }
        else if (match($0, /allocs\/op/)) { section = "allocs/op"; next }
        else                              { section = ""; next }
    }
    /^geomean/ { section = ""; next }
    section && /\+[0-9.]+%/ { print "[" section "] " $0 }
' "$BENCHSTAT_OUT_FILE" | head -40)

if [[ -n "$ALLOC_REGRESSIONS" ]]; then
    echo ""
    echo "ALLOCATION REGRESSION vs $BASELINE:"
    echo "$ALLOC_REGRESSIONS"
    exit 2
fi

# Fail on sec/op positive delta:
#   - >15% on sub-10ns benchmarks (wire primitives — variance dominates
#     at this scale; a 5% gate produces noise-driven false positives
#     because individual sample variance regularly hits ±10%)
#   - >5% on everything else
# Statistical significance is implicit (benchstat prints "~" for
# non-significant deltas, not "+N.NN%").
TIME_REGRESSIONS=$(awk '
    /vs base/ {
        if (match($0, /sec\/op/))         { section = "sec/op"; next }
        else if (match($0, /B\/op/))      { section = ""; next }
        else if (match($0, /allocs\/op/)) { section = ""; next }
        else                              { section = ""; next }
    }
    /^geomean/ { section = ""; next }
    section && match($0, /\+[0-9]+\.[0-9]+%/) {
        tok = substr($0, RSTART+1, RLENGTH-2)   # strip leading + and trailing %
        delta = tok + 0
        # Field 2 is the baseline value (e.g. "3.121n", "538.3n", "12.34µ").
        # Sub-10ns benches end in "n" with a numeric portion < 10. Anything
        # that is not in this nanosecond regime gets the strict 5% gate.
        threshold = 5
        baseline = $2
        if (baseline ~ /n$/) {
            base_num = substr(baseline, 1, length(baseline) - 1) + 0
            if (base_num < 10) threshold = 15
        }
        if (delta > threshold) print "[" section " >" threshold "%] " $0
    }
' "$BENCHSTAT_OUT_FILE" | head -40)

if [[ -n "$TIME_REGRESSIONS" ]]; then
    echo ""
    echo "TIME REGRESSION vs $BASELINE (>5% sec/op, >15% on sub-10ns benches):"
    echo "$TIME_REGRESSIONS"
    exit 3
fi

echo ""
echo "No regressions vs $BASELINE."
