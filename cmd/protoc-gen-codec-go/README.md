# Buf Schema Registry plugin submission

This directory holds the artifacts needed to publish `protoc-gen-codec-go`
as a remote plugin on the [Buf Schema Registry](https://buf.build/plugins).
Once published, BSR users can invoke the plugin via:

```yaml
# buf.gen.yaml
version: v1
plugins:
  - plugin: buf.build/thesmos/codec-go
    out: gen/go
```

…without needing to install the binary locally.

## Files

- **`buf.plugin.yaml`** — plugin metadata (name, version, runtime
  module dep, license).
- **`Dockerfile`** — multi-stage Go build that produces a static binary
  in a `scratch` image. Buf's remote runner pipes stdin/stdout through
  the entrypoint.

## Submission flow

The Buf team gates additions to the public registry. The flow is:

1. **Open an issue** at <https://github.com/bufbuild/plugins/issues/new/choose>
   using the *"Plugin Request for Buf Schema Registry"* template.
   Link to:
   - This repository (`https://github.com/thesmos-ai/protoc-gen-codec`)
   - The latest release tag (e.g. `v1.0.0`)
   - This `cmd/protoc-gen-codec-go/` directory as the proposed submission.

2. **Wait for triage.** The Buf team prioritises plugins that are
   widely adopted, stable, well-documented, and well-maintained. They
   may ask for clarifications or request changes to the metadata.

3. **Open the PR** when invited. The expected target path is:

   ```
   plugins/thesmos/codec-go/v1.0.0/
   ├── buf.plugin.yaml
   └── Dockerfile
   ```

   Copy the two files from this directory into that path under a fork
   of `bufbuild/plugins`, push, and open the PR.

4. **Iterate on review.** The Buf team's CI will build the Docker
   image, run their plugin acceptance tests, and merge once green.

## Bumping the plugin version

On every released `v*` tag of `protoc-gen-codec`:

1. Update `plugin_version`, `license_url`, and `registry.go.deps[0].version`
   in `buf.plugin.yaml` to the new tag.
2. Open a PR to `bufbuild/plugins` adding a new
   `plugins/thesmos/codec-go/vN.N.N/` directory with the updated files.
   (Buf keeps every released version published — never edit a previous
   version's directory in place.)

## Local validation before submission

You can confirm the Dockerfile builds cleanly in your local working
copy before opening the issue:

```sh
docker build -t protoc-gen-codec-go:dev -f cmd/protoc-gen-codec-go/Dockerfile .

# Smoke test: feed an empty CodeGeneratorRequest and check the binary
# returns a valid (empty) CodeGeneratorResponse without panicking.
echo -n '' | docker run --rm -i protoc-gen-codec-go:dev || true
```

Buf's CI runs richer acceptance tests against a representative `.proto`
input — the local check above only verifies the image starts.

## Direct contact

For questions outside the issue tracker, the Buf team is reachable at
<dev@buf.build> or on their public Slack
(<https://buf.build/links/slack>).
