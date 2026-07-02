# SP-054: LSP Language Coverage Expansion

**Status:** ✅ Shipped (all 3 phases complete 2026-06)

The LSP integration originally supported a handful of languages
(TypeScript, Go, Rust) via hand-curated server configurations. SP-054
expanded to 12+ languages (Python, Rust, C/C++, C#, Java, Ruby, PHP,
Swift, Kotlin, Dart, Lua, Shell) with proper `InstallHint` metadata so
the CLI can guide users to install missing servers. Phase 2 added
auto-install via `lspInstallCmd` and user-configurable
`LanguageServerOverride` for custom setups. Phase 3 added semantic
adapters (`python_adapter.go`, `rust_adapter.go`, `cpp_adapter.go`)
for the languages that needed language-specific query patterns.
`/api/lsp/status` exposes the running servers and their health.

## Key decisions

- **Hand-curated configs over discovery.** Languages differ too much in
  their server binaries; discovery would need a per-language fallback
  chain anyway.
- **`InstallHint` is a structured field, not a CLI string.** The webui
  can render install buttons; the CLI can print install commands.
- **Auto-install is opt-in.** Sprout installs missing servers with
  user consent, not silently. The `lspInstallCmd` requires confirmation.
- **Semantic adapters are language-specific, not generic.** A
  generic adapter would either over-fit (false positives) or under-fit
  (miss language-specific patterns). Per-language adapters are honest.
- **`/api/lsp/status` is read-only** — no language auto-detection from
  output, no per-session server lifecycle. Simpler.

## Artifacts

- code: `pkg/lsp/proxy/discovery.go` — 12+ language configs with
  `InstallHint`
- code: `pkg/lsp/semantic/python_adapter.go`, `rust_adapter.go`,
  `cpp_adapter.go`, `lsp_query.go`
- code: `cmd/lsp.go::lspInstallCmd` — auto-install command
- code: `pkg/webui/api_lsp.go` — `/api/lsp/status` endpoint
- code: `webui/src/services/lspClientService.ts` — `LSP_SUPPORTED_LANGUAGES`
- config: `LanguageServerOverride` in `config_provider_custom.go`

Full specification archived — see git history for original content.