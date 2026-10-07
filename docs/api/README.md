# The API contract

This directory is the versioned contract for the Sprout backend: an OpenAPI
document for the HTTP API and a JSON Schema for the WebSocket events. Both are
generated from code — neither is hand-written, so neither can drift from the
handlers that implement them. A Go test fails the build when a generated file
is stale.

## Files

- `openapi.yaml` — the HTTP API, generated from the Huma operations registered
  in `pkg/webui` (plus the non-generated metadata in `openapi.base.yaml`: the
  contract version in `info.version` and the family tag list). The current
  contract version is the `info.version` field.
- `openapi.base.yaml` — the hand-written seed. Only the metadata above lives
  here; every path and schema comes from code.
- `events.schema.json` — the WebSocket event envelope and every `data` payload
  type, generated from the `@sprout/events` TypeScript types
  (`packages/events/src/types.ts`). `x-eventTypes` maps each event `type` string
  to its payload schema.
- `undocumented.txt` — the routes that are not (yet) documented as Huma
  operations: the non-JSON, streaming, and WebSocket endpoints, each with a
  one-line reason. These are intentionally outside `openapi.yaml`.

## Regenerating

After changing the handlers or the event types, regenerate the documents:

```sh
# HTTP API (from the repo root)
go run ./cmd/genapi

# WebSocket events (from the repo root)
node packages/events/scripts/generate-events-schema.mjs
```

`go run ./cmd/genapi` also keeps the contract in sync with the `sprout api
conformance` probe set. A `cmd/genapi` test fails the build if `openapi.yaml`
does not match what the registered operations produce.

## The contract version

`info.version` in `openapi.yaml` is the contract version. Bump it deliberately
when the shape of the contract changes (a renamed endpoint, a changed
response shape, a new family), not on every code change. The Web UI reads it
at bootstrap and refuses to start when the daemon's major version is
incompatible; a newer minor version is a warning, not a refusal.

## Running the conformance suite

The conformance suite is published with the package. It loads this contract,
sends the safe, read-only probes to a live implementation, checks each
response status and body shape, and prints a per-family report. It never
mutates state, so it is safe to point at a running workspace.

```sh
# From a checked-out repo (locates docs/api/openapi.yaml automatically)
./sprout api conformance --base-url http://127.0.0.1:5199

# Or point at a built binary or an explicit contract document
sprout api conformance --base-url http://127.0.0.1:5199 --spec /path/to/openapi.yaml

# Restrict to the families a host actually serves
sprout api conformance --base-url https://host.example/api --families settings,git
```

Exit codes: `0` when every probe passes, `1` when any probe fails (the report
is printed to stdout, the failure line to stderr), `2` on a usage error (a
missing `--base-url`, or an unknown `--families` value).

### In CI

A host that serves part of the API adds a job that boots its endpoint and runs
the suite against it:

```sh
# Fail the build on any failing probe. --json keeps stdout machine-readable
# for CI log parsing (one line per probe).
sprout api conformance --base-url "$ENDPOINT_URL" --json --families settings,diagnostics
```

Because the probe set is read-only, the job can run against a normal
environment; it does not need a disposable workspace. The `--spec` flag is
only needed when the binary was built elsewhere and cannot locate the contract
document by walking up from its working directory.
