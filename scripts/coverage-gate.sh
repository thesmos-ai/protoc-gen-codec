#!/usr/bin/env bash
# Enforces per-file coverage floor on lang/go/integration/*.codec.go.
#
# We aggregate per-function coverage entries from `go tool cover -func`
# into per-file totals. A handful of generated functions (notably
# MarshalCodec) contain structurally unreachable defensive error branches
# (`if err != nil { return nil, err }` after MarshalToCodec when the size
# and the buffer length are computed in lockstep). Forcing per-function
# 100% would require contrived tests or removing defensive code, so we
# enforce per-file 100% — dead branches have been eliminated from the
# generator (nested-call error propagation, nil-receiver check inside
# MarshalCodecInternal, map entry-length post-check) and the
# lang/go/codec/coverage.go helpers cover every reachable defensive path.
# A drop below 100% signals either a new code path needs a test or the
# generator emitted unreachable code — both are actionable.
set -euo pipefail

FLOOR="${COVERAGE_FLOOR:-100.0}"
OUT=$(mktemp)
trap "rm -f $OUT" EXIT

go test -coverprofile="$OUT" ./lang/go/integration/ > /dev/null

# Aggregate per-function statement counts into per-file totals from the
# raw coverprofile. Each non-mode line is:
#   path/to/file.go:startLine.startCol,endLine.endCol numStatements count
# A statement counts as covered if count > 0; the file's coverage is
# (covered statements) / (total statements).
report=$(awk '
    NR == 1 { next }                           # skip "mode:" line
    {
        n = split($1, a, ":"); file = a[1]     # everything before the first ":"
        stmts = $2 + 0
        cov   = ($3 + 0 > 0) ? stmts : 0
        total[file] += stmts
        hit[file]   += cov
    }
    END {
        for (f in total) {
            if (total[f] == 0) continue
            printf "%s %.1f\n", f, (hit[f] / total[f]) * 100
        }
    }
' "$OUT" | sort)

FAIL=0
while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    path=$(echo "$line" | awk '{print $1}')
    pct=$(echo "$line" | awk '{print $2}')
    if [[ "$path" != *.codec.go ]]; then continue; fi
    if [[ $(awk -v a="$pct" -v b="$FLOOR" 'BEGIN{print (a<b)?1:0}') -eq 1 ]]; then
        echo "FAIL: $path coverage ${pct}% < floor ${FLOOR}%"
        FAIL=1
    else
        echo "PASS: $path coverage ${pct}%"
    fi
done <<< "$report"

if [[ $FAIL -eq 1 ]]; then
    echo ""
    echo "Coverage floor: ${FLOOR}%"
    echo "Refresh floor: COVERAGE_FLOOR=<new> scripts/coverage-gate.sh (once all pass)"
    exit 1
fi

# Report aggregate.
TOTAL=$(go tool cover -func="$OUT" | tail -1 | awk '{print $NF}')
echo ""
echo "Per-file coverage on *.codec.go: ≥${FLOOR}% (total: $TOTAL)"
