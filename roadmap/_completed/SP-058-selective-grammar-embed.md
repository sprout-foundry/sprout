# SP-058: Selective Grammar Embedding for WASM and Daemon

**Status:** ✅ Implemented (Daemon binary 149 MB per 899d667f; 22 MB below 171 MB target)
**Depends on:** None (self-contained build infrastructure change)  
**Priority:** Medium — binary size / cold-load latency for WASM users  
**Author:** Sprout Core Team  
**Created:** 2026-05-26

---

## Overview

`github.com/odvcencio/gotreesitter` ships every grammar it supports as an embedded `.bin` blob:

- Default build: **206 grammars, ~21 MB** of blobs (`//go:embed grammar_blobs/*.bin`)
- `grammar_set_core` build tag (currently used by WASM): **100 grammars, ~13.8 MB**

`pkg/ast.SupportedLanguages` lists exactly five: `go`, `typescript`, `tsx`, `javascript`, `python`. Total size of the five blobs we actually use: **~717 KB**.

The remaining ~13 MB (WASM) or ~20 MB (daemon) of embedded grammars is unused weight in every shipped binary. This spec strips it out *without* losing any current functionality.

## Motivation

- **Cold-load latency for cloud-mode users.** WASM is 53 MB stripped today. The grammar slice is roughly a quarter of that — visible time to interactive when serving `/wasm/sprout.wasm` over the network.
- **Daemon binary size.** 171 MB → ~151 MB. Less dramatic in percent terms, but the same one-time fix.
- **Eliminates an architectural smell.** Carrying 200 grammars to parse 5 languages is the kind of thing that looks alarming in audits.

## Constraints

- **No degraded functionality.** Every language `pkg/ast` parses today must keep working in every build target (daemon, WASM, tests).
- **No vendored binary blobs.** The `.bin` files come from `gotreesitter`; checking them into our repo creates a synchronization burden and bloats clones. Generate them at build time from the module cache.
- **No upstream dependency.** We don't wait on a PR to `gotreesitter`. Use APIs the library already exposes.

## Approach

`gotreesitter` already exposes two stable APIs:

1. `func LoadLanguage(data []byte) (*Language, error)` — parses a `.bin` blob into a usable `Language`.
2. `func grammars.Register(entry LangEntry)` — adds (or **replaces**, by `Name`) a language entry in the registry. The `Language` field is `func() *gotreesitter.Language`, called lazily.

The plan, in three pieces:

### 1. Build with `grammar_blobs_external`

Switch both the WASM and daemon builds from "embed grammars" to "external blob source":

| Target       | Current tags             | New tags                  |
| ------------ | ------------------------ | ------------------------- |
| `build`      | _(none)_                 | `grammar_blobs_external`  |
| `build-wasm` | `grammar_set_core`       | `grammar_blobs_external`  |

This excludes `gotreesitter`'s entire `//go:embed grammar_blobs/*.bin` from the binary. The library's built-in language registration still runs at first `Register()` call — registering all 206 entries pointing at a blob source that can't find anything — but since no caller ever looks up an unregistered-by-us language, the broken loaders are inert. (The `external` blob source tries `os.Open()` on `$GOTREESITTER_GRAMMAR_BLOB_DIR/<name>.bin`; without the env var set, it returns an error, and we never call into it.)

### 2. Selective embed in `pkg/ast`

New file `pkg/ast/grammars_embed.go` (no build tag — applies to every target):

```go
package ast

import (
    "embed"
    "sync"

    gotreesitter "github.com/odvcencio/gotreesitter"
    "github.com/odvcencio/gotreesitter/grammars"
)

//go:embed grammars/bin/go.bin grammars/bin/typescript.bin grammars/bin/tsx.bin grammars/bin/javascript.bin grammars/bin/python.bin
var grammarFS embed.FS

func init() {
    register("go",         []string{".go"},                    "grammars/bin/go.bin")
    register("typescript", []string{".ts", ".cts", ".mts"},    "grammars/bin/typescript.bin")
    register("tsx",        []string{".tsx"},                   "grammars/bin/tsx.bin")
    register("javascript", []string{".js", ".cjs", ".mjs", ".jsx"}, "grammars/bin/javascript.bin")
    register("python",     []string{".py", ".pyi"},            "grammars/bin/python.bin")
}

func register(name string, exts []string, path string) {
    var (
        once sync.Once
        lang *gotreesitter.Language
    )
    grammars.Register(grammars.LangEntry{
        Name:       name,
        Extensions: exts,
        Language: func() *gotreesitter.Language {
            once.Do(func() {
                data, err := grammarFS.ReadFile(path)
                if err != nil {
                    panic("ast: read embedded grammar " + path + ": " + err.Error())
                }
                l, err := gotreesitter.LoadLanguage(data)
                if err != nil {
                    panic("ast: LoadLanguage " + name + ": " + err.Error())
                }
                lang = l
            })
            return lang
        },
        GrammarSource: grammars.GrammarSourceTS2GoBlob,
    })
}
```

Notes:
- `grammars.Register()` documents that it replaces an entry with the same name. So even after `gotreesitter` registers its 206 built-ins (lazily, on first `Register()` call), our five wins for those names.
- `LoadLanguage(data)` is `sync.Once`-guarded so we pay the parse cost (a few ms per grammar) only on first use.
- Extensions are listed explicitly here because the `external` blob source mode strips them along with the embed; we re-supply them. `tsx.bin` registering `.tsx` will lose to `typescript.bin` if `Register` order matters — `typescript` is registered first and `tsx` second, so the `.tsx` extension ends up on the `tsx` entry. (`Register` invalidates `extIndex` on every call, then rebuilds on next `DetectLanguage`, walking entries in order.)

### 3. Build-time grammar copy (no vendored blobs)

Add `pkg/ast/grammars/bin/*.bin` to `.gitignore`. Add a Makefile target that copies the five blobs from the Go module cache:

```makefile
GRAMMAR_DIR := pkg/ast/grammars/bin
GRAMMAR_BLOBS := go.bin typescript.bin tsx.bin javascript.bin python.bin

# Copy grammar blobs from the gotreesitter module cache. Required before
# any `go build` / `go test` because pkg/ast's //go:embed references them.
prepare-grammars:
	@bash scripts/prepare-grammars.sh

build: prepare-grammars
build-wasm: prepare-grammars
test-unit: prepare-grammars
test-integration: prepare-grammars
```

`scripts/prepare-grammars.sh` implementation sketch:

```bash
#!/usr/bin/env bash
set -euo pipefail
VERSION="$(go list -m -f '{{.Version}}' github.com/odvcencio/gotreesitter)"
MODCACHE="$(go env GOMODCACHE)"
SRC="${MODCACHE}/github.com/odvcencio/gotreesitter@${VERSION}/grammars/grammar_blobs"
DST="$(dirname "$0")/../pkg/ast/grammars/bin"
mkdir -p "$DST"
for f in go.bin typescript.bin tsx.bin javascript.bin python.bin; do
    cp "${SRC}/${f}" "${DST}/${f}"
done
```

The script idempotently re-copies on every invocation. Bumping the `gotreesitter` version in `go.mod` automatically pulls fresh blobs on the next `make prepare-grammars`. If the upstream blob format changes, our `LoadLanguage` call panics at startup — loud failure, not silent breakage.

## Build-target wiring

| Target             | Change                                                     |
| ------------------ | ---------------------------------------------------------- |
| `make build`       | Add `-tags grammar_blobs_external`; depend on `prepare-grammars` |
| `make build-wasm`  | Change `WASM_TAGS=grammar_set_core` → `grammar_blobs_external`; depend on `prepare-grammars` |
| `make test-unit`   | Depend on `prepare-grammars`                              |
| `make build-all`   | Already chains the above three                             |
| CI workflow        | Add `make prepare-grammars` after `go mod download`        |

## gopls / IDE handling

`//go:embed` errors at compile time if a referenced file is missing. Before someone runs `make prepare-grammars`, `gopls` will surface a red error in `pkg/ast/grammars_embed.go`.

**Mitigation:** add a one-line note to `CLAUDE.md` under the build section: "Run `make prepare-grammars` once after a fresh clone." This is the same UX as a project that requires `go mod download` before first build — a minor onboarding step, documented once.

## Expected size impact

| Target | Before     | After       | Delta            |
| ------ | ---------- | ----------- | ---------------- |
| WASM   | 53 MB      | ~40 MB      | −13 MB (−25 %)   |
| Daemon | 171 MB     | ~151 MB     | −20 MB (−12 %)   |

The WASM number is **not** the 5 MB claimed by the earlier (deleted) draft of this spec — that figure was wildly optimistic. Real savings come from removing only the unused grammar blobs; the Go runtime, `pkg/agent`, model SDKs, and the rest of the codebase make up the bulk of both binaries and are unchanged here.

## Risks and Mitigations

| Risk | Mitigation |
| ---- | ---------- |
| `grammar_blobs_external` build leaves 200 broken language registrations in memory | They're inert; `DetectLanguage` only walks registered entries on lookup, and no caller looks up the unsupported languages. Memory cost: a few KB of struct entries. |
| Module-cache path differs between machines (`GOPATH`, CI cache mount, etc.) | `go env GOMODCACHE` resolves the actual location on each machine. Script handles it. |
| Someone adds a new supported language to `pkg/ast.SupportedLanguages` and forgets the corresponding `register(...)` call | `parser.go`'s `init()` pre-warms each `SupportedLanguages` entry; a missing registration fails loudly at process start with "no grammar registered for language". |
| `gotreesitter` version bump changes the `.bin` format | `LoadLanguage` returns an error → panic at init. CI catches it on first build. Easy to diagnose. |
| The library starts registering grammars via package `init()` instead of lazily | Our `init()` runs after the library's by Go's package-import order (we import `grammars`, so it inits first). Our `Register()` overrides theirs, same as today. |
| Fresh clone gives the user red `//go:embed` errors in gopls before they run `make prepare-grammars` | Documented in `CLAUDE.md` under build setup. One-line fix. |

## Acceptance Criteria

- [ ] `make build-all` produces a WASM binary smaller than 45 MB (current: 53 MB).
- [ ] `make build` produces a daemon binary smaller than 160 MB (current: 171 MB).
- [ ] `go test ./pkg/ast/...` passes — all current parser tests still green.
- [ ] `go test ./pkg/embedding/...` passes — the TS / Python extractors still parse correctly.
- [ ] `pkg/ast/grammars/bin/*.bin` is in `.gitignore`; no `.bin` files are committed.
- [ ] A fresh `git clone` + `make build-all` succeeds without manual intervention beyond `make prepare-grammars` (which `build-all` depends on transitively).
- [ ] `CLAUDE.md` has a one-line note about `make prepare-grammars` for IDE/gopls users.

## Tasks

- [ ] Add `pkg/ast/grammars/bin/` to `.gitignore`
- [ ] Create `scripts/prepare-grammars.sh` (executable)
- [ ] Add `prepare-grammars` target to Makefile; wire `build`, `build-wasm`, `test-unit`, `test-integration` to depend on it
- [ ] Update `scripts/build-wasm.sh`: `WASM_TAGS=grammar_set_core` → `grammar_blobs_external`
- [ ] Update Makefile `build:` target: add `-tags grammar_blobs_external` to the `go build` invocation
- [ ] Create `pkg/ast/grammars_embed.go` with the `//go:embed` + `register()` setup described above
- [ ] Verify build-all succeeds and the binaries hit the size targets
- [ ] Run full test suite; fix any regressions
- [ ] Add the `make prepare-grammars` note to `CLAUDE.md`
- [ ] Update the WASM size-threshold comment in `build-wasm.sh` (the 100 MB warning) to a tighter 50 MB
