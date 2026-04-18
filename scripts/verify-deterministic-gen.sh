#!/usr/bin/env bash
# Verifies `make generate` produces byte-identical output across runs.
set -euo pipefail

TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT

# Capture first generation.
make generate >/dev/null
cp lang/go/integration/fixture.codec.go "$TMP/first.codec.go"

# Delete and regenerate.
rm -f lang/go/integration/fixture.codec.go
make generate >/dev/null

# Diff.
if ! diff -q lang/go/integration/fixture.codec.go "$TMP/first.codec.go" >/dev/null; then
    echo "GENERATOR NOT DETERMINISTIC:"
    diff lang/go/integration/fixture.codec.go "$TMP/first.codec.go"
    exit 1
fi
echo "Generator output is byte-identical across runs."
