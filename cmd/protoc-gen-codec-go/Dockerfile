# syntax=docker/dockerfile:1.7
#
# Build image for protoc-gen-codec-go as a Buf Schema Registry remote
# plugin. Buf invokes the entrypoint with a CodeGeneratorRequest on
# stdin and consumes a CodeGeneratorResponse on stdout, so the image
# only needs the binary — no shell, no init system.
#
# This Dockerfile is the artifact submitted to bufbuild/plugins under
# plugins/thesmos/codec-go/v1.0.0/Dockerfile alongside buf.plugin.yaml.
# See ./README.md for the full submission flow.

# ---- builder ----------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache module downloads in a separate layer so source-only changes
# don't re-fetch dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Reproducible build: -trimpath strips local paths, -s -w drops symbol
# tables and DWARF info, CGO_ENABLED=0 produces a fully-static binary
# that runs in the scratch image below.
RUN CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags='-s -w' \
        -o /protoc-gen-codec-go \
        ./cmd/protoc-gen-codec-go

# ---- runtime ----------------------------------------------------------------
# `scratch` for the smallest possible image: no shell, no libc, just
# the Go binary. Buf's remote-plugin runner exec()s the entrypoint
# directly with stdin/stdout piped — nothing else is required.
FROM scratch

COPY --from=builder /protoc-gen-codec-go /protoc-gen-codec-go

USER 1000:1000
ENTRYPOINT ["/protoc-gen-codec-go"]
