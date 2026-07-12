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
   | Android, arm64     | `libonnxruntime.so`       |

3. **Auto-download from the official `microsoft/onnxruntime` release** —
   when step 2 is empty, sprout fetches the platform-appropriate archive
   from `github.com/microsoft/onnxruntime/releases/download/v<ver>/...`,
   extracts the shared library, and atomically writes it to the path from
   step 2. Version is pinned in `pkg/embedding/onnx_runtime_install.go`
   (currently 1.20.1, matching the yalue/onnxruntime_go v1.30.x ABI). This
   is the production-grade fallback — the source is the same one Microsoft
   distributes everywhere else, the writes are atomic, and the bytes can
   be hash-verified by pinning `SHA256` in `onnxRuntimeReleaseConfig`.

4. **Dev fallback: bootstrap from `yalue/onnxruntime_go` test_data** — if
   step 3 also fails (e.g. no network in an air-gapped CI runner) and the
   `yalue/onnxruntime_go` Go module is present in the local module cache,
   sprout copies the bundled test library from
   `$GOPATH/pkg/mod/github.com/yalue/onnxruntime_go@<ver>/test_data/<libname>`.

   This step exists strictly for developer convenience so that
   `go test ./...` and `go run` work out of the box without network. It is
   NOT appropriate for production — the `test_data` directory is not a
   public surface of the upstream library and the file there isn't pinned
   in any reproducible way. Set `SPROUT_DISABLE_YALUE_BOOTSTRAP=1` in
   production environments to lock the resolver down to steps 1-3 and
   fail closed if none succeed.

5. **Yalue's default** — if all of the above fail, sprout doesn't call
   `SetSharedLibraryPath` and yalue falls back to looking for plain
   `onnxruntime.so` in `LD_LIBRARY_PATH` / the current directory. This is
   the last-resort path for deployments that install ONNX Runtime via the
   system package manager.

If every step fails, `EmbeddingManager.initONNX` returns an error containing
the resolution order and concrete next-steps. The static embedding path
continues to work unaffected.

## Production deployment

Pick whichever of these fits your distribution model best:

### Option A — let sprout auto-download (recommended for default installs)

Do nothing. On first ONNX use, sprout downloads the pinned ONNX Runtime
release archive from `github.com/microsoft/onnxruntime/releases`, extracts
the shared library, and stages it at `~/.config/sprout/models/onnxruntime/<libname>`
for all future runs. The pin is in `pkg/embedding/onnx_runtime_install.go`
(`onnxRuntimeVersion`) and the per-archive SHA-256 lives in
`onnxRuntimeReleaseFor()` (pin per-platform when you cut a release).
Subsequent launches load straight from the staged file at step 2 of the
resolution order — no network needed.

### Option B — pre-stage manually (recommended for air-gapped / locked-down hosts)

Download the archive yourself (URL printed in the install error message or
visible in `pkg/embedding/onnx_runtime_install.go:onnxRuntimeReleaseFor`),
extract `libonnxruntime.so.X.Y.Z` (or `.dylib`/`.dll`) into
`~/.config/sprout/models/onnxruntime/<platform-lib-name>`, and set
`SPROUT_DISABLE_YALUE_BOOTSTRAP=1` to lock the resolver to your install.

### Option C — sysadmin-managed install (recommended for shared servers)

Install ONNX Runtime via the system package manager (or place the `.so` in
a standard library path), then set `SPROUT_ONNX_RUNTIME_LIB` to the
absolute path in the sprout process's environment. This decouples ONNX
upgrades from sprout upgrades.

### Option D — Skip ONNX entirely

If a deployment doesn't need ONNX-quality semantic search, no setup is
required — sprout transparently falls back to the static embedding provider
when ONNX is unavailable. All workspace search, duplicate detection, and
memory retrieval continue to function with reduced retrieval precision (see
`pkg/embedding/retrieval_eval.go` for measured deltas on this codebase: 42%
static vs. 75% ONNX hit rate on the curated test queries).

## Termux / Android

Android (via Termux or a native NDK build) is treated as a first-class
supported platform by the resolver, but the underlying Bionic-libc CGO
chain has not been end-to-end verified from a CI environment. Treat the
following as best-effort:

- The resolver looks for `libonnxruntime.so` (no `_arm64` suffix — the
  Android AAR layout puts per-arch variants in different directories, not
  different filenames).
- **Auto-download is NOT available for Android.** Microsoft distributes
  the Android ONNX Runtime as a Maven AAR
  (`com.microsoft.onnxruntime:onnxruntime-android:1.25.1`), not as an
  asset on the GitHub releases page. sprout's release map therefore has
  no Android entry — `resolveSharedLibraryPath` falls through to the
  environment override / staged-file steps only. To use ONNX on Android,
  manually extract `libonnxruntime.so` from the Android AAR (it lives at
  `jni/arm64-v8a/libonnxruntime.so` inside the .aar) and either:
  - place it at `~/.config/sprout/models/onnxruntime/libonnxruntime.so`,
    or
  - set `SPROUT_ONNX_RUNTIME_LIB=/path/to/libonnxruntime.so` in the
    process environment.
- CGO linking against Bionic requires Termux's `clang` toolchain; build
  with `CGO_ENABLED=1 CC=clang CXX=clang++`. Static glibc-linked Go
  binaries will not load the Bionic `.so` even when the resolver finds
  it. If `dlopen` still fails after staging, that's a Bionic/glibc
  mismatch, not a resolver bug.
- When in doubt, use **Option D** (skip ONNX) on Termux — the static
  embedding provider works there without any external dependencies.

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
