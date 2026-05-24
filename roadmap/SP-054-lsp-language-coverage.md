# SP-054: LSP Language Coverage Expansion

**Status:** 📋 Proposed
**Date:** 2026-05-23
**Depends on:** SP-003 (WebUI architecture), existing `pkg/lsp/proxy` and `pkg/lsp/semantic` packages
**Priority:** Medium
**Effort Estimate:** Phase 1 ~1 week · Phase 2 ~2 weeks · Phase 3 ~3 weeks (~6 weeks total)

## Current State

Two complementary LSP systems already exist:

### 1. LSP Proxy (`pkg/lsp/proxy/`)

Full-featured LSP proxy that manages language server processes with stdio framing and WebSocket bridging to CodeMirror. Handles: process lifecycle, health checks, idle eviction, reference counting, per-workspace isolation, JSON-RPC message framing.

**Currently configured for:**
- Go (`gopls`)
- TypeScript / JavaScript (`typescript-language-server`)

Configuration is a simple array in `DefaultLanguageServers()` — adding a language requires only a `LanguageServerConfig` entry with binary name, args, and language IDs.

### 2. Semantic Adapters (`pkg/lsp/semantic/`)

Agent-facing semantic analysis system with per-language adapters (Go, TypeScript). These are callable from the agent's tool system and provide: diagnostics, go-to-definition, hover, references, rename, code actions, inlay hints, signature help. Some methods shell out to CLI tools directly (e.g., `gofmt -e`, `go vet`, `gopls definition`), others start a full LSP server for complex requests (e.g., inlay hints via unix socket).

### 3. Frontend (`webui/src/services/lspClientService.ts`)

CodeMirror 6 integration with LSP client per language. WebSocket URLs are constructed as `/api/lsp/ws?language={langId}&workspace={encodedPath}`. The `LSP_SUPPORTED_LANGUAGES` set determines which languages get LSP features in the editor.

## Goal

Expand language coverage to cover the most commonly-used programming languages, using the existing proxy infrastructure. Each new language follows the same pattern: configuration entry + optional semantic adapter.

## Language Servers

### Tier 1 — Add Configuration Only (~1 day each)

These servers speak LSP over stdio. Adding them requires only a `LanguageServerConfig` entry in `DefaultLanguageServers()` and the language ID in `LSP_SUPPORTED_LANGUAGES`.

| Language | Server Binary | Args | Language IDs | Install |
|---|---|---|---|---|
| Python | `pyright-langserver` | `--stdio` | `python` | `npm install -g pyright` |
| Rust | `rust-analyzer` | | `rust` | `brew install rust-analyzer` or `rustup component add rust-analyzer` |
| C/C++ | `clangd` | | `c`, `cpp` | ships with LLVM/clang |
| C# | `omnisharp` | `--stdio` | `csharp` | Download from OmniSharp releases |
| Java | `jdtls` | | `java` | Eclipse JDT Language Server |
| Ruby | `solargraph` | `stdio` | `ruby` | `gem install solargraph` |
| PHP | `intelephense` | `--stdio` | `php` | `npm install -g intelephense` |
| Swift | `sourcekit-lsp` | | `swift` | ships with Xcode / Swift toolchain |
| Kotlin | `kotlin-language-server` | | `kotlin` | GitHub releases |
| Dart | `dart` | `language-server --protocol=lsp` | `dart` | ships with Dart SDK |
| Lua | `lua-language-server` | | `lua` | `brew install lua-language-server` |
| Shell | `bash-language-server` | `start` | `shellscript` | `npm install -g bash-language-server` |

### Tier 2 — Semantic Adapters (optional enhancement, ~2-3 days each)

Some languages benefit from a dedicated `pkg/lsp/semantic` adapter for agent-facing tool calls. The LSP proxy handles all editor features; these adapters are for the *agent* to query language intelligence outside of the editor context.

Priority candidates:
1. **Python** — highest usage, diagnostics via `ruff check` or Pyright CLI
2. **Rust** — `cargo check` for diagnostics, `rust-analyzer` CLI for analysis
3. **C/C++** — `clang-tidy` for diagnostics, `clangd` CLI fallback

Non-priority: Java, C#, Ruby, PHP, Swift, Kotlin. These can rely on the LSP proxy for editor features. Agent-facing semantic adapters can be added later based on demand.

## Implementation Plan

### Phase 1: Configuration Expansion (~1 week)

**Goal:** Add LSP server configs for all Tier 1 languages. Editor gets completions, diagnostics, hover, go-to-definition, references, rename, code actions for each language where the server binary is present on PATH.

#### 1.1: Expand `DefaultLanguageServers()`

File: `pkg/lsp/proxy/discovery.go`

Add `LanguageServerConfig` entries for all Tier 1 languages. Each entry maps language IDs to server binary and args. Servers that aren't installed are simply not started — `ResolveBinaryPath()` returns an error and the proxy reports the language as unavailable.

#### 1.2: Expand `LSP_SUPPORTED_LANGUAGES`

File: `webui/src/services/lspClientService.ts`

Add all new language IDs to the frontend set so CodeMirror activates LSP client connections for these languages.

#### 1.3: Language Server Status API

Add an endpoint (`GET /api/lsp/status`) that returns which language servers are available (binary found on PATH) vs not installed. The frontend can use this to show status in the UI (e.g., "Python LSP: available" or "Python LSP: install pyright").

#### 1.4: Graceful Missing-Server UX

When a language server binary is not found:
- Log a clear message: `"pyright-langserver not found on PATH — Python LSP unavailable. Install with: npm install -g pyright"`
- Return a structured error to the frontend
- Show a non-intrusive hint in the editor footer or status bar when a file is opened in a language without a server

### Phase 2: Auto-Install & Configuration (~2 weeks)

**Goal:** Reduce friction for getting language servers running. Users shouldn't need to manually install binaries.

#### 2.1: Install Command

Add a `lsp install` CLI command and API endpoint:

```
sprout lsp install python    # runs: npm install -g pyright
sprout lsp install rust      # runs: rustup component add rust-analyzer
sprout lsp install --all     # install all available servers
sprout lsp list               # show installed/available status
```

Each `LanguageServerConfig` gets an `InstallHint` field documenting the install command. Optional: an `InstallCmd` field that the daemon can execute.

#### 2.2: User-Configurable Servers

Allow users to add custom language servers via configuration:

```json
{
  "languageServers": {
    "elixir": {
      "binary": "elixir-ls",
      "args": ["--stdio"],
      "languageIds": ["elixir"]
    }
  }
}
```

The `Manager.SetConfig()` method already exists — this just needs config file loading and merging with defaults.

#### 2.3: Workspace Activation Hints

Detect workspace indicators and suggest language servers:
- `requirements.txt` / `pyproject.toml` → suggest Python server
- `Cargo.toml` → suggest Rust server
- `*.sln` / `*.csproj` → suggest C# server
- etc.

Show suggestions in the WebUI on workspace open.

### Phase 3: Semantic Adapters for Top Languages (~3 weeks)

**Goal:** Agent-facing semantic analysis for the top 3 languages (Python, Rust, C/C++), following the same pattern as the existing Go and TypeScript adapters.

#### 3.1: Python Semantic Adapter

File: `pkg/lsp/semantic/python_adapter.go`

Methods:
- `diagnostics` — shell out to `ruff check --output-format=json` (fast, no server needed)
- `hover` — via Pyright CLI or LSP proxy query
- `definition` — via Pyright CLI or LSP proxy query
- `references` — via LSP proxy query
- `rename` — via LSP proxy query

Prefer CLI tools (`ruff`) for diagnostics (zero startup cost). Use the LSP proxy for everything else (definition, hover, references, rename) since `pyright-langserver` is already running via Phase 1.

Register in the semantic registry alongside Go and TypeScript.

#### 3.2: Rust Semantic Adapter

File: `pkg/lsp/semantic/rust_adapter.go`

Methods:
- `diagnostics` — `cargo check --message-format=json` (captures compiler errors/warnings)
- `hover`, `definition`, `references` — via LSP proxy query to `rust-analyzer`
- `code_actions` — `cargo fix --allow-dirty` for quick fixes (opt-in only)
- `inlay_hints` — via `rust-analyzer` LSP (types, parameter names, chaining)

#### 3.3: C/C++ Semantic Adapter

File: `pkg/lsp/semantic/cpp_adapter.go`

Methods:
- `diagnostics` — `clang-tidy --export-fixes` (lint + static analysis)
- `hover`, `definition`, `references` — via LSP proxy query to `clangd`
- `formatting` — `clang-format` CLI (widely used, fast)

#### 3.4: Shared LSP Proxy Query Helper

The semantic adapters for Python, Rust, and C++ need to query their running LSP servers. Add a shared helper in `pkg/lsp/semantic/` that:
1. Takes a language ID + LSP method + params
2. Routes through the existing `pkg/lsp/proxy/Manager` to send a JSON-RPC request
3. Waits for the response (with timeout)
4. Returns the parsed result

This avoids each adapter reinventing LSP client logic. The Go adapter already does this for inlay hints (spawns gopls, connects via unix socket) — the shared helper makes this pattern reusable.

## Architecture

```
                    ┌──────────────────────────────────────┐
                    │           CodeMirror 6 Editor        │
                    │  (completions, diagnostics, hover,   │
                    │   go-to-def, rename, code actions)   │
                    └──────────────┬───────────────────────┘
                                   │ WebSocket
                    ┌──────────────▼───────────────────────┐
                    │         LSP Proxy Manager            │
                    │         (pkg/lsp/proxy)              │
                    │                                       │
                    │  ┌─────────┐ ┌─────────────────────┐ │
                    │  │ gopls   │ │ typescript-lang-srv  │ │  ← existing
                    │  └─────────┘ └─────────────────────┘ │
                    │  ┌─────────────┐ ┌─────────────────┐ │
                    │  │ pyright-ls  │ │ rust-analyzer    │ │  ← Phase 1
                    │  └─────────────┘ └─────────────────┘ │
                    │  ┌─────────┐ ┌──────┐ ┌───────────┐ │
                    │  │ clangd  │ │ omnisharp │ jdtls  │ │
                    │  └─────────┘ └──────┘ └───────────┘ │
                    │  ┌───────────┐ ┌──────────────┐     │
                    │  │ solargraph│ │intelephense   │     │
                    │  └───────────┘ └──────────────┘     │
                    │  ... (+ all Tier 1 languages)        │
                    └──────────────────────────────────────┘

                    ┌──────────────────────────────────────┐
                    │      Semantic Adapters (agent tools)  │
                    │         (pkg/lsp/semantic)            │
                    │                                       │
                    │  ┌─────┐ ┌──────────┐                │
                    │  │ Go  │ │TypeScript│                │  ← existing
                    │  └─────┘ └──────────┘                │
                    │  ┌────────┐ ┌──────┐ ┌──────┐       │
                    │  │ Python │ │ Rust │ │ C++  │       │  ← Phase 3
                    │  └────────┘ └──────┘ └──────┘       │
                    └──────────────────────────────────────┘
```

## File Structure

```
pkg/lsp/
├── proxy/
│   ├── discovery.go          # LanguageServerConfig + DefaultLanguageServers()  ← expand
│   ├── manager.go            # Process lifecycle manager  ← no changes
│   ├── process.go            # LSPProcess (stdio framing) ← no changes
│   ├── bridge.go             # WebSocket ↔ LSP bridge    ← no changes
│   ├── framing.go            # Content-Length framing     ← no changes
│   └── manager_test.go       # existing tests
├── semantic/
│   ├── registry.go           # Adapter registry           ← no changes
│   ├── go_adapter.go         # Go semantic adapter        ← existing
│   ├── typescript_adapter.go # TypeScript semantic adapter ← existing
│   ├── lsp_query.go          # NEW: shared LSP proxy query helper
│   ├── python_adapter.go     # NEW: Phase 3
│   ├── rust_adapter.go       # NEW: Phase 3
│   └── cpp_adapter.go        # NEW: Phase 3

webui/src/services/
├── lspClientService.ts       # LSP_SUPPORTED_LANGUAGES    ← expand
```

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Language server startup latency (some servers are slow) | Medium | Lazy activation on file open; cache running servers; status indicator |
| Binary availability varies by platform | Medium | `ResolveBinaryPath()` already handles missing binaries gracefully; document install methods per platform |
| Some servers don't support all LSP features | Low | Feature capability is negotiated during LSP `initialize`; the proxy already handles partial support |
| Memory/CPU from many concurrent servers | Medium | Idle eviction already exists (10min timeout); configurable limits per server |
| Semantic adapters duplicating proxy logic | Low | Shared `lsp_query.go` helper routes through proxy rather than re-implementing |

## Success Criteria

### Phase 1
- [ ] `DefaultLanguageServers()` includes configs for 12+ languages
- [ ] `LSP_SUPPORTED_LANGUAGES` includes all new language IDs
- [ ] Opening a `.py` file starts `pyright-langserver` if available (editor gets completions, diagnostics, hover)
- [ ] Opening a `.rs` file starts `rust-analyzer` if available
- [ ] Opening a `.cpp` file starts `clangd` if available
- [ ] Missing server binary produces a clear log message with install instructions
- [ ] All existing Go and TypeScript LSP functionality still works
- [ ] `go test ./pkg/lsp/...` passes

### Phase 2
- [ ] `sprout lsp list` shows installed/available status for all configured languages
- [ ] `sprout lsp install <language>` installs the server binary
- [ ] Users can add custom language servers via configuration file
- [ ] Workspace detection suggests relevant language servers on open
- [ ] `go test ./...` passes

### Phase 3
- [ ] Python semantic adapter: agent can query diagnostics (`ruff`), hover, definition, references
- [ ] Rust semantic adapter: agent can query diagnostics (`cargo check`), hover, definition, references
- [ ] C/C++ semantic adapter: agent can query diagnostics (`clang-tidy`), hover, definition, references
- [ ] Shared `lsp_query.go` helper is used by all new adapters
- [ ] `go test ./pkg/lsp/semantic/...` passes with tests for all new adapters
