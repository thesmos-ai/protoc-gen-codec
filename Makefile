.PHONY: build test test-race lint fmt generate clean

GO := go

build:
	$(GO) build ./cmd/protoc-gen-codec-go/

test:
	$(GO) test ./... ./testdata/go/

test-race:
	$(GO) test -race ./... ./testdata/go/

test-fuzz:
	@for target in $$($(GO) test -list '^Fuzz' ./... 2>/dev/null | grep '^Fuzz'); do \
		pkg=$$($(GO) test -list "^$$target$$" ./... 2>/dev/null | grep -v '^Fuzz' | head -1); \
		echo "Fuzzing $$target in $$pkg..."; \
		$(GO) test -run="^$$target$$" -fuzz="^$$target$$" -fuzztime=30s $$pkg || exit 1; \
	done

lint: fmt
	$(GO) vet ./... ./testdata/go/
	buf lint
	golangci-lint run ./... ./testdata/go/

fmt:
	$(GO) fmt ./...

clean:
	rm -f protoc-gen-codec-go

.DEFAULT_GOAL := build
