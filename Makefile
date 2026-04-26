# protoc-gen-codec — make targets for build, test, lint, coverage, release.
#
#   make help            list every target with a short description
#   make build           build the protoc-gen-codec-go binary
#   make check           per-PR umbrella (lint + race + coverage + ...)
#   make ci              full CI-equivalent (check + mutation testing)
#
# All commands are safe to override at the command line, e.g.
#   make test-fuzz FUZZTIME=5m
#   make test-bench BENCHTIME=3s BENCHCOUNT=10

# Strict bash so any pipeline failure breaks the recipe instead of silently
# masking errors mid-pipe.
SHELL       := bash
.SHELLFLAGS := -eu -o pipefail -c

# ---- Tools (overridable) ---------------------------------------------------
# Each tool is a variable so an environment can swap implementations
# without editing the Makefile (e.g. GO=go1.27 make test).
GO            ?= go
GOLANGCI_LINT ?= golangci-lint
GREMLINS      ?= gremlins
BUF           ?= buf

# ---- Knobs (overridable) ---------------------------------------------------
FUZZTIME    ?= 30s
BENCHTIME   ?= 1s
BENCHCOUNT  ?= 1

# ---- Paths -----------------------------------------------------------------
BIN_DIR        := bin
COVERAGE_DIR   := build/coverage
COVERAGE_OUT   := $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML  := $(COVERAGE_DIR)/coverage.html

# ---- Defaults --------------------------------------------------------------
.DEFAULT_GOAL := build

# ===========================================================================
# Help
# ===========================================================================

help: ## Show this help message
	@printf "\nUsage: \033[36mmake \033[0;32m<target>\033[0m\n\nTargets:\n"
	@awk -F ':.*?## ' \
	  '/^[a-zA-Z][a-zA-Z0-9_-]*:.*## / { printf "  \033[36m%-26s\033[0m %s\n", $$1, $$2 }' \
	  $(MAKEFILE_LIST) \
	  | sort
	@echo

# ===========================================================================
# Build / generate
# ===========================================================================

build: ## Build the protoc-gen-codec-go binary into $(BIN_DIR)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/protoc-gen-codec-go ./cmd/protoc-gen-codec-go/

generate: build ## Regenerate *.codec.go files via buf
	PATH="$(CURDIR)/$(BIN_DIR):$$PATH" $(BUF) generate

clean: ## Remove build artifacts and coverage output
	rm -rf $(BIN_DIR) $(COVERAGE_DIR)

# ===========================================================================
# Testing
# ===========================================================================

test: ## Run unit tests
	$(GO) test -count=1 ./...

test-race: ## Run unit tests under the race detector
	$(GO) test -count=1 -race ./...

# Discover fuzz targets per package and run each for FUZZTIME.
test-fuzz: ## Run every fuzz target for FUZZTIME (default: 30s)
	@for pkg in $$($(GO) list ./...); do \
		targets=$$($(GO) test -list '^Fuzz' $$pkg 2>/dev/null | grep '^Fuzz' || true); \
		for target in $$targets; do \
			echo "==> Fuzzing $$target in $$pkg for $(FUZZTIME)"; \
			$(GO) test -run='^$$' -fuzz="^$$target$$" -fuzztime=$(FUZZTIME) $$pkg || exit 1; \
		done; \
	done

test-bench: ## Run all benchmarks with allocation reporting
	$(GO) test -run='^$$' -bench=. -benchmem -benchtime=$(BENCHTIME) -count=$(BENCHCOUNT) ./...

# ===========================================================================
# Coverage
# ===========================================================================

# coverage and coverage-gate are distinct on purpose:
#   - coverage      : produces a profile + textual summary, doesn't gate
#   - coverage-gate : enforces the per-file 100% floor on *.codec.go
#                     (the strict gate that runs in CI)

coverage: ## Generate a combined cross-package coverage profile + summary
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -coverprofile=$(COVERAGE_OUT) -covermode=atomic -coverpkg=./... ./...
	@echo
	@$(GO) tool cover -func=$(COVERAGE_OUT) | tail -1
	@echo "Profile: $(COVERAGE_OUT)"

coverage-html: coverage ## Generate an HTML coverage report (opens in a browser)
	$(GO) tool cover -html=$(COVERAGE_OUT) -o $(COVERAGE_HTML)
	@echo "Report: $(COVERAGE_HTML)"
	@command -v xdg-open >/dev/null 2>&1 && xdg-open $(COVERAGE_HTML) || \
	  command -v open >/dev/null 2>&1 && open $(COVERAGE_HTML) || \
	  echo "Open the file above in your browser."

coverage-gate: ## Enforce per-file 100% coverage on generated *.codec.go (CI gate)
	./scripts/coverage-gate.sh

# ===========================================================================
# Lint / format
# ===========================================================================

lint: fmt ## Run go vet + buf lint + golangci-lint (auto-formats first)
	$(GO) vet ./...
	$(BUF) lint
	$(GOLANGCI_LINT) run ./...

fmt: ## Apply gofmt to all Go sources
	$(GO) fmt ./...

# ===========================================================================
# Benchmark regression
# ===========================================================================

# Pin GOMAXPROCS=8 so the baseline is reproducible across dev machines and
# stays within memory headroom on high-core boxes. bench-compare uses .name
# projection so the GOMAXPROCS suffix doesn't break comparisons across envs.
bench-baseline: ## Refresh .bench-baseline/main.txt from the current tree
	@mkdir -p .bench-baseline
	GOMAXPROCS=8 $(GO) test -run='^$$' -bench=. -benchmem -benchtime=3s -count=10 \
		./lang/go/codec/ \
		./lang/go/integration/ \
		./lang/go/integration/external/ \
		> .bench-baseline/main.txt
	@echo "Baseline refreshed: .bench-baseline/main.txt"

bench-compare: ## Compare current bench results against the pinned baseline
	./scripts/bench-compare.sh

# ===========================================================================
# Determinism
# ===========================================================================

verify-deterministic-gen: build ## Verify generator output is byte-identical across runs
	./scripts/verify-deterministic-gen.sh

# ===========================================================================
# Mutation testing (gremlins)
# ===========================================================================
# Mutation testing complements coverage: 100% line coverage proves a line
# ran; mutation testing proves the line's behavior was asserted. We treat
# protoc-gen-codec as foundation-tier (100% effective kill rate). True
# equivalents that cannot be killed without architectural refactor are
# annotated inline with `// mutation:equivalent <reason>` and reported in
# the threshold below (gremlins itself does not parse the comments).
#
# Timeout note: the high coefficient compensates for the runtime package's
# fast baseline (~ms); mutations that introduce infinite loops still time
# out and gremlins counts them as detected (not LIVED).

# Resource caps: gremlins defaults to (workers × test-cpu) = NumCPU²
# which on a 16-core machine spawns ~256 contended CPU slots and can
# spike load past 100. Cap to (workers × test-cpu) ≈ NumCPU so peak
# parallelism stays at one core per slot. Tuned for a 16-core dev box;
# override per invocation if needed:
#   GREMLINS_WORKERS=4 GREMLINS_TEST_CPU=2 make test-mutation-integration
GREMLINS_WORKERS ?= 8
GREMLINS_TEST_CPU ?= 2
GREMLINS_RUN = $(GREMLINS) unleash \
	--timeout-coefficient=200 \
	--workers=$(GREMLINS_WORKERS) \
	--test-cpu=$(GREMLINS_TEST_CPU)

# Runtime: 5 documented equivalents (DecodeVarint contract: n >= 1 or
# n == -1; n == 0 unreachable, so `<` and `<=` are semantically identical).
# Effective kill rate is 100%; gremlins reports ~91.94%.
test-mutation-codec: ## Mutation-test lang/go/codec/ (threshold 91% numerical)
	$(GREMLINS_RUN) -E 'codectest/' --threshold-efficacy=91 ./lang/go/codec/

# Analyzer: 19 documented equivalents (CONDITIONALS_BOUNDARY on protowire
# `for len > 0` / `if n < 0` patterns). Effective kill rate is 100%;
# gremlins reports ~80%.
test-mutation-core: ## Mutation-test internal/core/ (threshold 80% numerical)
	$(GREMLINS_RUN) --threshold-efficacy=80 ./internal/core/

# Integration: mutates the generated *.codec.go files (and hand-written
# fixture types) and runs the full codectest suite against each mutant.
# This tests the END artifact — what runs in production — rather than
# the emitter that produces it.
#
# Two invocations because gremlins is package-scoped and doesn't
# recurse cleanly via `./...`: external/ is its own package with its
# own tests (lang/go/integration/external/external_test.go) so its
# mutants are killed by tests in the same package.
test-mutation-integration: ## Mutation-test lang/go/integration/ + sub-pkgs (threshold 100%)
	$(GREMLINS_RUN) --threshold-efficacy=100 ./lang/go/integration/
	$(GREMLINS_RUN) --threshold-efficacy=100 ./lang/go/integration/external/

test-mutation: test-mutation-codec test-mutation-core test-mutation-integration ## Run all mutation-testing layers

# ===========================================================================
# Composite gates
# ===========================================================================

# Per-PR umbrella. Mutation testing is intentionally NOT here: gremlins
# runs are nightly-cadence per the testing standard, not per-PR — they'd
# add ~30s/run without proportional value on a typical change.
check: lint test-race coverage-gate verify-deterministic-gen bench-compare ## Per-PR gate (lint + race + cov + det + bench)
	@echo
	@echo "All gates passed."

# Full local equivalent of the CI matrix, including the nightly mutation
# tests. Slow (5+ min) but a complete green here means the PR is ready.
ci: check test-mutation ## Full CI-equivalent (check + mutation)
	@echo "Full CI-equivalent gates passed."

# ===========================================================================
# Tooling bootstrap
# ===========================================================================

# Versions are pinned so a fresh checkout produces reproducible results.
# Bump deliberately when newer versions are vetted.
TOOL_BUF        := github.com/bufbuild/buf/cmd/buf@v1.50.0
TOOL_GOLANGCI   := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.0.0
TOOL_GREMLINS   := github.com/go-gremlins/gremlins/cmd/gremlins@latest
TOOL_BENCHSTAT  := golang.org/x/perf/cmd/benchstat@latest

tools: ## Install developer tools (buf, golangci-lint, gremlins, benchstat) into $(GOBIN)
	$(GO) install $(TOOL_BUF)
	$(GO) install $(TOOL_GOLANGCI)
	$(GO) install $(TOOL_GREMLINS)
	$(GO) install $(TOOL_BENCHSTAT)
	@echo
	@echo "Tools installed. Ensure \$$(go env GOPATH)/bin is on \$$PATH."

# ===========================================================================
# Phony declarations
# ===========================================================================

.PHONY: \
	help \
	build generate clean \
	test test-race test-fuzz test-bench \
	coverage coverage-html coverage-gate \
	lint fmt \
	bench-baseline bench-compare \
	verify-deterministic-gen \
	test-mutation test-mutation-codec test-mutation-core test-mutation-integration \
	check ci \
	tools
