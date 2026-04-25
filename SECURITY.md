# Security Policy

## Supported versions

Security fixes are issued for the latest minor release on each
supported major version line.

| Version | Supported          |
|---------|--------------------|
| 1.x     | :white_check_mark: |
| 0.x     | :x:                |

## Reporting a vulnerability

**Please do not file public GitHub issues for security vulnerabilities.**

Instead, report privately via one of:

- GitHub's private vulnerability reporting:
  <https://github.com/thesmos-ai/protoc-gen-codec/security/advisories/new>
- Email: security@thesmos.sh

Please include:

- The affected version (commit hash or tag).
- A clear description of the vulnerability.
- Steps to reproduce, ideally with a minimal `.proto` + Go fixture
  that demonstrates the issue.
- The impact you've observed (panic, OOM, infinite loop, incorrect
  decode, etc.).

We aim to acknowledge reports within 72 hours and to issue a fix or
mitigation within 14 days for confirmed high-severity issues.

## Scope

In scope:

- The runtime under `lang/go/codec/` (wire-format parsing, decode-time
  bounds checks, allocation behaviour on adversarial input).
- The generator under `internal/lang/golang/` (emission of unsafe or
  incorrect code from valid `.proto` input).
- The analysis layer under `internal/core/` (validation gaps that let
  bad annotations through and produce incorrect code).

Out of scope:

- Vulnerabilities in dependencies (report upstream).
- Issues that require attacker-controlled `.proto` input at code-
  generation time. The generator runs as a build-time tool on
  developer-controlled schemas; it is not designed to defend against
  hostile input at the protoc layer.
- Issues in unsupported features (see "Unsupported Features" in
  [`docs/generators/go.md`](docs/generators/go.md)).
