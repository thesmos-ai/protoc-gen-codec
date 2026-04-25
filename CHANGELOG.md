# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Per-release notes (with full commit lists) are also published on
[GitHub Releases](https://github.com/thesmos-ai/protoc-gen-codec/releases).

## [1.0.0]

First stable release. The runtime API, annotation surface, generated
method shapes, and `codectest.Spec[T]` are now versioned per
[`README.md` § Stability](README.md#stability).

### Added

- **Stability commitments** — explicit semver and deprecation policy
  documented in `README.md` and `lang/go/codec/doc.go`.
- **Quick Start guide** — end-to-end walkthrough in `README.md`
  covering install, annotation, generation, and consumption.
- **Unsupported Features section** — `docs/generators/go.md`
  enumerates the proto3 subset (and explicit non-goals: proto2 groups,
  extensions, Any, FieldMask, Struct, Empty, services).
- **SECURITY.md** — vulnerability disclosure policy.
- **Cosign keyless signing** for release artifacts via Sigstore.
- **UPX-compressed binaries** for `linux/*` and `windows/amd64`.

### Changed

- Improved analysis error message for `(codec.cast)` on message-typed
  fields — now points at `(codec.use_pointer)` and `(codec.type)` as
  the right knobs.

## [0.5.0]

Released alongside the v1.0.0 docs/governance work. Goreleaser
pipeline hardened with cosign + UPX; CI workflow stabilized.

## Pre-1.0

For pre-v1.0 history, see the
[GitHub Releases page](https://github.com/thesmos-ai/protoc-gen-codec/releases)
or the commit log. Notable milestones:

- **v0.4.0** — 100% aggregate coverage, in-bench `StartContract` alloc/latency gate, per-fixture LatencyMax.
- **v0.3.0** — analyzer mutation testing reaches 100% effective kill rate.
- **v0.2.0** — gremlins mutation gate added; runtime kill rate hits 100% effective.
- **v0.1.0** — initial public release.

[1.0.0]: https://github.com/thesmos-ai/protoc-gen-codec/releases/tag/v1.0.0
[0.5.0]: https://github.com/thesmos-ai/protoc-gen-codec/releases/tag/v0.5.0
