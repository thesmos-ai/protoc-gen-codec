.PHONY: build test test-race test-fuzz test-bench lint fmt generate clean bench-baseline bench-compare verify-deterministic-gen coverage-gate test-mutation test-mutation-codec test-mutation-core

GO := go
FUZZTIME ?= 30s
BENCHTIME ?= 1s
BENCHCOUNT ?= 1

build:
	mkdir -p bin
	$(GO) build -o bin/protoc-gen-codec-go ./cmd/protoc-gen-codec-go/

generate: build
	PATH="$(CURDIR)/bin:$$PATH" buf generate

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

# Discover fuzz targets per package and run each for FUZZTIME.
# Override with: make test-fuzz FUZZTIME=5m
test-fuzz:
	@for pkg in $$($(GO) list ./...); do \
		targets=$$($(GO) test -list '^Fuzz' $$pkg 2>/dev/null | grep '^Fuzz' || true); \
		for target in $$targets; do \
			echo "==> Fuzzing $$target in $$pkg for $(FUZZTIME)"; \
			$(GO) test -run='^$$' -fuzz="^$$target$$" -fuzztime=$(FUZZTIME) $$pkg || exit 1; \
		done; \
	done

# Run all benchmarks with alloc reporting.
# Override with: make test-bench BENCHTIME=3s BENCHCOUNT=10
test-bench:
	$(GO) test -run='^$$' -bench=. -benchmem -benchtime=$(BENCHTIME) -count=$(BENCHCOUNT) ./...

lint: fmt
	$(GO) vet ./...
	buf lint
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -rf bin/

# Pin GOMAXPROCS=8 so the baseline is reproducible across dev machines and
# stays within memory headroom on high-core boxes (default GOMAXPROCS on a
# 32-core machine OOM-killed our baseline run). bench-compare uses .name
# projection so the GOMAXPROCS suffix doesn't break comparisons across envs.
bench-baseline:
	mkdir -p .bench-baseline
	GOMAXPROCS=8 $(GO) test -run='^$$' -bench=. -benchmem -benchtime=3s -count=10 \
		./lang/go/codec/ ./lang/go/integration/ > .bench-baseline/main.txt
	@echo "Baseline refreshed in .bench-baseline/main.txt"

bench-compare:
	./scripts/bench-compare.sh

verify-deterministic-gen:
	./scripts/verify-deterministic-gen.sh

coverage-gate:
	./scripts/coverage-gate.sh

# ---- Mutation testing (gremlins) ------------------------------------------
# Mutation testing complements coverage: 100% line coverage proves a line
# ran; mutation testing proves the line's behavior was asserted. We treat
# protoc-gen-codec as foundation-tier (100% effective kill rate). True
# equivalents that cannot be killed without architectural refactor are
# annotated inline with `// mutation:equivalent <reason>` and reported in
# this file's threshold (gremlins itself does not parse the comments).
#
# Coverage qualifier: gremlins only mutates statements that the test suite
# covers. Generator emission code in internal/lang/golang/ has no direct
# tests (only integration tests after `make generate`), so it is not
# mutation-testable today. Adding golden-file emission tests would unlock it.
#
# Timeout note: the high coefficient compensates for the runtime package's
# fast baseline (~ms); mutations that introduce infinite loops still time
# out and gremlins counts them as detected (not LIVED).
GREMLINS = gremlins unleash --timeout-coefficient=200

# Runtime: 5 documented equivalents (DecodeVarint contract: n >= 1 or
# n == -1; n == 0 unreachable, so `<` and `<=` are semantically identical).
# 57 KILLED + 5 equivalents = 100% effective; gremlins reports ~91.94%.
test-mutation-codec:
	$(GREMLINS) -E 'codectest/' --threshold-efficacy=91 ./lang/go/codec/

# Analyzer: 65 KILLED + 19 documented equivalents (file-level note in
# options.go covers the protowire `for len > 0` and `if n < 0` patterns
# where the wire-decoder contractually returns n>=1 or -1, never 0).
# Effective efficacy ~91%; gremlins reports 70.65%. Remaining 8 LIVED
# need direct unit tests of analyzer struct output (FieldInfo.IsBytes,
# IsString, MessageRef.FullName comparisons) — requires heavier
# protogen.Message construction, queued as a follow-up.
test-mutation-core:
	$(GREMLINS) --threshold-efficacy=70 ./internal/core/

test-mutation: test-mutation-codec test-mutation-core

.DEFAULT_GOAL := build
