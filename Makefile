.PHONY: build test test-race test-fuzz test-bench lint fmt generate clean bench-baseline bench-compare verify-deterministic-gen coverage-gate

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

bench-baseline:
	mkdir -p .bench-baseline
	$(GO) test -run='^$$' -bench=. -benchmem -benchtime=3s -count=10 \
		./lang/go/codec/ ./lang/go/integration/ > .bench-baseline/main.txt
	@echo "Baseline refreshed in .bench-baseline/main.txt"

bench-compare:
	./scripts/bench-compare.sh

verify-deterministic-gen:
	./scripts/verify-deterministic-gen.sh

coverage-gate:
	./scripts/coverage-gate.sh

.DEFAULT_GOAL := build
