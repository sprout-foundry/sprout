# ONNX Runtime Setup

Sprout's optional ONNX-based embedding path (`EmbeddingGemma-300M` via
`pkg/embedding`) requires the native [ONNX Runtime](https://onnxruntime.ai/)
shared library to be reachable by the Go process. Without it, the static
embedding provider is used and ONNX-quality semantic search is unavailable —
the rest of sprout continues to work.

This document covers where that library is expected to live, how sprout
auto-detects it on dev machines, and how to stage it for production
deployments.

## Resolution order

`pkg/embedding/onnx_runtime.go:resolveSharedLibraryPath` probes the following
locations in order, stopping at the first hit:

1. **`SPROUT_ONNX_RUNTIME_LIB` environment variable** — absolute path to the
   shared library. Highest priority; use this when you want explicit control
   (e.g. a sysadmin-managed install or a pinned version inside CI).

2. **`~/.config/sprout/models/onnxruntime/<platform-lib-name>`** — sprout's
   canonical "staged library" location. The expected filename is
   platform-specific:

   | OS / arch          | filename                  |
   |--------------------|---------------------------|
   | Linux, arm64       | `onnxruntime_arm64.so`    |
   | Linux, amd64       | `onnxruntime.so`          |
   | macOS, arm64       | `onnxruntime_arm64.dylib` |
   | macOS, amd64       | `onnxruntime.dylib`       |
   | Windows, any       | `onnxruntime.dll`         |

3. **Dev fallback (bootstrap from `yalue/onnxruntime_go` test_data)** — if
   step 2 is empty and the `yalue/onnxruntime_go` Go module is present in
   the local module cache, sprout copies the bundled test library from
   `$GOPATH/pkg/mod/github.com/yalue/onnxruntime_go@<ver>/test_data/<libname>`
   into the location from step 2 on first use.

   This step exists strictly for developer convenience so that
   `go test ./...` and `go run` work out of the box. **It is not appropriate
   for production:** the `test_data` directory is not a public surface of
   the upstream library, it's not pinned in any reproducible way, and the
   `.so`/`.dylib`/`.dll` shipped there is sized for unit tests rather than
   optimized for production inference.

4. **Yalue's default** — if all of the above fail, sprout doesn't call
   `SetSharedLibraryPath` and yalue falls back to looking for plain
   `onnxruntime.so` in `LD_LIBRARY_PATH` / the current directory. This is
   the last-resort path for deployments that install ONNX Runtime via the
   system package manager.

If every step fails, `EmbeddingManager.initONNX` returns an error containing
the resolution order and concrete next-steps. The static embedding path
continues to work unaffected.

## Production deployment

Pick whichever of these fits your distribution model best:

### Option A — sprout-managed install (recommended for self-contained binaries)

Stage the platform-specific library at
`~/.config/sprout/models/onnxruntime/<libname>` as part of your installer or
provisioning script. Sprout will find it at step 2 on first launch with no
additional configuration. Pin to a specific ONNX Runtime release version
(e.g. v1.20.0) and verify the SHA-256 before staging.

### Option B — sysadmin-managed install (recommended for shared servers)

Install ONNX Runtime via the system package manager (or place the `.so` in a
standard library path), then set `SPROUT_ONNX_RUNTIME_LIB` to the absolute
path in the sprout process's environment. This decouples ONNX upgrades from
sprout upgrades.

### Option C — Skip ONNX entirely

If a deployment doesn't need ONNX-quality semantic search, no setup is
required — sprout transparently falls back to the static embedding provider
when ONNX is unavailable. All workspace search, duplicate detection, and
memory retrieval continue to function with reduced retrieval precision (see
`pkg/embedding/retrieval_eval.go` for measured deltas on this codebase: 42%
static vs. 75% ONNX hit rate on the curated test queries).

## Where ONNX Runtime binaries come from

The upstream project publishes prebuilt binaries on
[GitHub Releases](https://github.com/microsoft/onnxruntime/releases) for
Linux (x64 / arm64), macOS (x64 / arm64), and Windows (x64). Microsoft also
distributes via NuGet and pip; either source produces a compatible `.so`,
`.dylib`, or `.dll` that sprout can load.

The `yalue/onnxruntime_go` Go binding's `test_data/` directory ships a
copy that is convenient for development but is not promised to be stable
across module versions. Don't depend on it for anything customer-facing.

## Verifying the install

```sh
# Set up the runtime
ls ~/.config/sprout/models/onnxruntime/  # should show the platform .so/.dylib/.dll

# Or set the env override
export SPROUT_ONNX_RUNTIME_LIB=/opt/onnxruntime/lib/libonnxruntime.so

# Run the gated ONNX e2e tests
SPROUT_RUN_ONNX_TESTS=1 go test ./pkg/embedding/ -run TestE2E_ONNX -v
```

A passing run prints something like `Provider: onnx-embeddinggemma-300m-256d`
and `Similarity(auth, login): 0.60+`.
