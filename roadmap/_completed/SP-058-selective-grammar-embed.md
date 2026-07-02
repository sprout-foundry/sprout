# SP-058: Selective Grammar Embedding for WASM and Daemon

**Status:** ✅ Implemented (Daemon 149 MB / 171 MB target; WASM ~40 MB / 53 MB)

`gotreesitter` ships 206 grammar blobs (~21 MB) as embedded `.bin` files, but `pkg/ast.SupportedLanguages` uses only 5 languages (go, typescript, tsx, javascript, python) totaling ~717 KB. This spec stripped the unused grammars from both WASM and daemon builds without losing functionality, reducing the daemon binary by ~22 MB and WASM by ~13 MB.

## Key decisions

- Switched both WASM and daemon builds to `grammar_blobs_external` build tag — excludes `gotreesitter`'s entire `//go:embed grammar_blobs/*.bin` from the binary. The 206 broken language registrations are inert since no caller looks up unsupported languages.
- Selective embed in `pkg/ast/grammars_embed.go` — only the 5 used grammar blobs are embedded via `//go:embed`, registered lazily with `sync.Once` guards.
- Build-time grammar copy from module cache (`scripts/prepare-grammars.sh`) instead of vendoring `.bin` files — avoids synchronization burden and clone bloat. `make prepare-grammars` is a dependency of `build`, `build-wasm`, and `test-unit`.
- `grammars/bin/*.bin` added to `.gitignore` — no binary blobs committed to the repo.
- `grammars.Register()` replaces entries by name, so our 5 registrations override the library's built-ins for those names.

## Artifacts

- code: `pkg/ast/grammars_embed.go` — selective embed + lazy registration for 5 grammar blobs
- code: `scripts/prepare-grammars.sh` — copies grammar blobs from Go module cache at build time
- code: `Makefile` — `prepare-grammars` target wired as dependency of build/test
- code: `scripts/build-wasm.sh` — switched from `grammar_set_core` to `grammar_blobs_external` tag
- code: `pkg/ast/grammars/bin/` — 5 grammar blobs (go, typescript, tsx, javascript, python)

Full specification archived — see git history for original content.
