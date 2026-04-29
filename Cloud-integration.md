# Cloud Integration — Sprout (formerly ledit)

This document tracks all changes required in the sprout binary/repo to support Sprout Foundry's cloud platform (Lambda task runner, Fargate workspaces, browser IDE, and billing). Each item includes context, technical detail, and acceptance criteria so it can be worked through independently.

---

## 1. Complete ledit → sprout Rename

**Priority:** High — partially done, remaining work is user-facing strings and env vars.

### What's Already Done

- ✅ Go module: `github.com/sprout-foundry/sprout` (in `go.mod`)
- ✅ Go import paths updated across all `.go` files
- ✅ WASM global: `SproutWasm` (in `cmd/wasm/main.go`)
- ✅ IndexedDB: `sprout-wasm-fs` (in `webui/src/services/wasmShell.ts`)
- ✅ Logos updated to sprout design

### What's NOT Done

The rename commit (`88d69e7`) missed ~650 references across source files. The `.ledit` config directory is intentionally kept unchanged.

#### 1a. Environment Variables (73 unique `LEDIT_*` vars)

All `LEDIT_*` env vars must become `SPROUT_*` with backward-compatible fallback. The full list of 73 env vars currently in use:

| Category | `LEDIT_*` vars (rename to `SPROUT_*`) |
|----------|---------------------------------------|
| **Core config** | `LEDIT_CONFIG`, `LEDIT_DEBUG`, `LEDIT_DEBUG_OUTPUT`, `LEDIT_COLOR`, `LEDIT_JSON_LOGS`, `LEDIT_CORRELATION_ID` |
| **API keys** | `LEDIT_API_KEYS_JSON`, `LEDIT_KEY_PASSPHRASE`, `LEDIT_CREDENTIAL_BACKEND` |
| **Provider/model** | `LEDIT_PROVIDER`, `LEDIT_MODEL`, `LEDIT_PROVIDER_CATALOG_URL`, `LEDIT_PROXY_BASE`, `LEDIT_OLLAMA_FOLD_SYSTEM`, `LEDIT_OLLAMA_MAX_PREDICT` |
| **Agent behavior** | `LEDIT_NO_STREAM`, `LEDIT_NO_SUBAGENTS`, `LEDIT_NO_SUBAGENT_MODE`, `LEDIT_NO_CONNECTION_CHECK`, `LEDIT_SKIP_CONNECTION_CHECK`, `LEDIT_DRY_RUN`, `LEDIT_UNSAFE_MODE`, `LEDIT_SHOW_REASONING_TERMINAL`, `LEDIT_INTERACTIVE`, `LEDIT_CI_MODE`, `LEDIT_FROM_AGENT`, `LEDIT_PERSONA`, `LEDIT_SELF_REVIEW_MODE`, `LEDIT_SKIP_SELF_REVIEW_GATE` |
| **Subagents** | `LEDIT_SUBAGENT`, `LEDIT_SUBAGENT_MODEL`, `LEDIT_SUBAGENT_PROVIDER`, `LEDIT_SUBAGENT_MAX_TOKENS`, `LEDIT_SUBAGENT_TIMEOUT` |
| **Token/resource limits** | `LEDIT_MAX_REQUEST_COMPLETION_TOKENS`, `LEDIT_READ_FILE_MAX_BYTES`, `LEDIT_SEARCH_MAX_BYTES`, `LEDIT_FETCH_URL_MAX_CHARS`, `LEDIT_USER_INPUT_MAX_CHARS`, `LEDIT_INTERACTIVE_INPUT_MAX_CHARS`, `LEDIT_AUTOMATION_INPUT_MAX_CHARS`, `LEDIT_VISION_MAX_TEXT_CHARS`, `LEDIT_SHELL_HEAD_TOKENS`, `LEDIT_SHELL_TAIL_TOKENS` |
| **Logging/tracing** | `LEDIT_TRACE_DATASET_DIR`, `LEDIT_LOG_TOOL_CALLS`, `LEDIT_LOG_TURNS`, `LEDIT_LOG_API_RESPONSES`, `LEDIT_TURN_LOG_FILE`, `LEDIT_COPY_LOGS_TO_CWD`, `LEDIT_FETCH_URL_ARCHIVE_DIR`, `LEDIT_USER_INPUT_ARCHIVE_DIR` |
| **Tool/resource** | `LEDIT_RESOURCE_DIRECTORY`, `LEDIT_TOOL_TIMEOUT`, `LEDIT_CONTEXT_DIAG`, `LEDIT_AGENT_CONSOLE`, `LEDIT_MCP_ENABLED`, `LEDIT_MCP_AUTO_DISCOVER`, `LEDIT_MCP_AUTO_START` |
| **Service mode** | `LEDIT_SERVICE`, `LEDIT_ISOLATED_CONFIG`, `LEDIT_INITIAL_WORKSPACE` |
| **SSH** | `LEDIT_SSH_HOST_ALIAS`, `LEDIT_SSH_SESSION_KEY`, `LEDIT_SSH_LAUNCHER_URL`, `LEDIT_SSH_HOME` |
| **Terminal/webui** | `LEDIT_TERMINAL`, `LEDIT_WEB_TERMINAL`, `LEDIT_TAB`, `LEDIT_TERM_HEIGHT`, `LEDIT_HOST_PLATFORM`, `LEDIT_DESKTOP_BACKEND_MODE` |
| **Test** | `LEDIT_TEST_ENV`, `LEDIT_ALLOW_REAL_PROVIDER` |

**Implementation pattern** (create a helper and use it everywhere):
```go
// pkg/configuration/env.go
func GetEnv(sproutKey, legacyKey string) string {
    if v := os.Getenv(sproutKey); v != "" {
        return v
    }
    if v := os.Getenv(legacyKey); v != "" {
        log.Printf("[WARN] %s is deprecated; use %s instead.", legacyKey, sproutKey)
        return v
    }
    return ""
}
```

Replace every `os.Getenv("LEDIT_...")` call site with `configuration.GetEnv("SPROUT_...", "LEDIT_...")`. There are ~221 call sites across non-test Go files.

Also update `cmd/service_darwin.go` and `cmd/service_linux.go` where `LEDIT_SERVICE=1` is written into launchd/systemd config files.

#### 1b. WebUI Type Names

| Old | New |
|-----|-----|
| `LeditInstance` | `SproutInstance` |
| `LeditSettings` | `SproutSettings` |
| `LeditConfigDir` | `SproutConfigDir` |
| `LeditLogo` | `SproutLogo` |
| `LeditLogoProps` | `SproutLogoProps` |

These are in `webui/src/services/api.ts` and referenced from `webui/src/components/Sidebar.tsx`, `AppContent.tsx`, `LocationSwitcher.tsx`, and others.

#### 1c. WebUI Package Name

In `webui/package.json`: `"name": "ledit-webui"` → `"name": "sprout-webui"`

#### 1d. CLI Help Text and Comments

All `ledit agent`, `ledit custom add`, `ledit service install` etc. in CLI help strings (in `cmd/*.go`) need to become `sprout agent`, `sprout custom add`, etc.

Comments throughout `cmd/` and `pkg/` still reference "ledit" — bulk replace.

#### 1e. Desktop / Electron

- `desktop/main.js`: Update window title, app ID (`dev.alantheprice.ledit` → `dev.alantheprice.sprout`).
- `desktop/package.json`: Update `name`, `productName`, `build.appId`.

#### 1f. Install Script

- `scripts/install.sh`: Update binary name, GitHub raw URL paths.
- `scripts/install.ps1` (Windows).

### NOT in Scope

- **`.ledit` config directory** — stays as-is. No rename, no migration.
- **Go module path** — already `github.com/sprout-foundry/sprout`.
- **WASM shell** — already uses `SproutWasm`, `sprout-wasm-fs`.

### Acceptance Criteria

- [ ] All 73 `LEDIT_*` env vars have `SPROUT_*` equivalents via `GetEnv()` helper
- [ ] Old `LEDIT_*` env vars still work with deprecation warnings
- [ ] `sprout --version` prints the correct version
- [ ] `sprout agent "hello"` works
- [ ] CLI help text says `sprout` everywhere (no `ledit` in `--help` output)
- [ ] WebUI type names are `SproutInstance`, `SproutSettings`, etc.
- [ ] `webui/package.json` name is `sprout-webui`
- [ ] `go test ./...` passes
- [ ] Desktop app builds with updated branding
- [ ] No remaining `ledit`/`Ledit`/`LEDIT` references in source (excluding `.ledit` config dir paths and test files that test backward compat)

---

## 2. Add Token Metrics to Structured JSON Output

**Priority:** Required for Phase 4 (task runner E2E) and Phase 5 (usage tracking / billing).

### Context

The `--output-json` flag on `sprout agent` emits an `AgentResult` struct to stdout. Sprout Foundry's task runner parses this to record usage, compute LLM cost, and enforce billing limits. The current struct is missing token counts, provider, and model information.

### Current State

File: `cmd/agent_result.go`

```go
type AgentResultMetrics struct {
    ElapsedSeconds float64 `json:"elapsed_seconds"`
}
```

### Required Changes

**1. Extend `AgentResult` and `AgentResultMetrics`:**

```go
type AgentResult struct {
    Status        string             `json:"status"`
    Error         string             `json:"error,omitempty"`
    Query         string             `json:"query"`
    FilesModified []string           `json:"files_modified,omitempty"`
    GitDiff       string             `json:"git_diff,omitempty"`
    Metrics       AgentResultMetrics `json:"metrics"`
}

type AgentResultMetrics struct {
    ElapsedSeconds float64 `json:"elapsed_seconds"`
    TokensIn       int     `json:"tokens_in"`        // total input tokens across all LLM calls
    TokensOut      int     `json:"tokens_out"`       // total output tokens across all LLM calls
    LLMCalls       int     `json:"llm_calls"`        // number of LLM API calls made
    Provider       string  `json:"provider"`          // primary provider used
    Model          string  `json:"model"`             // primary model used
}
```

**2. Accumulate token counts during agent execution:**

The LLM client (likely in `pkg/agent/` or the provider abstraction layer) already receives token counts in API responses. During execution, accumulate these into a running total. The most likely place to hook this is in the completion/chat response handler.

Look for where the LLM response is parsed — there should be `usage.prompt_tokens` / `usage.completion_tokens` (OpenAI format) or equivalent. Add an atomic counter or a thread-safe accumulator:

```go
// pkg/agent/metrics.go (new file)
type ExecutionMetrics struct {
    mu        sync.Mutex
    TokensIn  int
    TokensOut int
    LLMCalls  int
    Provider  string
    Model     string
}

func (m *ExecutionMetrics) RecordCall(provider, model string, tokensIn, tokensOut int) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.TokensIn += tokensIn
    m.TokensOut += tokensOut
    m.LLMCalls++
    if m.Provider == "" {
        m.Provider = provider
        m.Model = model
    }
}
```

**3. Pass metrics to `emitJSONResult`:**

The `emitJSONResult` function in `cmd/agent_result.go` needs access to the accumulated metrics. Either:
- Pass the `ExecutionMetrics` as a parameter, or
- Store it on a package-level variable that the agent populates during execution

Update `emitJSONResult` to populate the new fields:
```go
func emitJSONResult(query string, startTime time.Time, runErr error, metrics *agent.ExecutionMetrics) {
    result := AgentResult{
        // ... existing fields ...
        Metrics: AgentResultMetrics{
            ElapsedSeconds: time.Since(startTime).Seconds(),
            TokensIn:       metrics.TokensIn,
            TokensOut:      metrics.TokensOut,
            LLMCalls:       metrics.LLMCalls,
            Provider:       metrics.Provider,
            Model:          metrics.Model,
        },
    }
    // ...
}
```

### Example Output

```json
{
  "status": "success",
  "query": "Add input validation to registration",
  "files_modified": ["handlers/register.go", "handlers/register_test.go"],
  "git_diff": "diff --git a/handlers/register.go ...",
  "metrics": {
    "elapsed_seconds": 45.2,
    "tokens_in": 12500,
    "tokens_out": 3200,
    "llm_calls": 4,
    "provider": "anthropic",
    "model": "claude-sonnet-4-20250514"
  }
}
```

### Acceptance Criteria

- [ ] `sprout agent --output-json "task"` includes `tokens_in`, `tokens_out`, `llm_calls`, `provider`, `model` in `metrics`
- [ ] Token counts are accurate (match API response usage fields)
- [ ] Multiple LLM calls (iterations, subagents) are summed correctly
- [ ] Provider/model reflect the primary model used (not subagent models)
- [ ] Existing tests still pass — output is backward-compatible (new fields only, no removed fields)

---

## 3. Fix `--port` vs `--web-port` Flag Inconsistency

**Priority:** Tiny — but blocks the Fargate workspace entrypoint.

### Context

Sprout Foundry's `docker/entrypoint.sh` invokes:
```bash
exec sprout agent -d --port 56000
```

But the CLI flag is actually `--web-port`, not `--port`. Either:

**Option A (preferred):** Add `--port` as an alias for `--web-port` in `cmd/agent.go`:
```go
agentCmd.Flags().IntVar(&webPort, "web-port", 56000, "Port for the web UI")
agentCmd.Flags().IntVar(&webPort, "port", 56000, "Port for the web UI (alias for --web-port)")
// Hide the alias from help output:
agentCmd.Flags().MarkHidden("port")
```

**Option B:** Fix the entrypoint to use `--web-port`. (But adding the alias is more user-friendly regardless.)

### Acceptance Criteria

- [ ] `sprout agent -d --port 56000` starts the web UI on port 56000
- [ ] `sprout agent -d --web-port 56000` still works (no regression)
- [ ] `sprout agent --help` shows `--web-port` as the primary flag

---

## 4. Service Mode: Bind Address, Origin Allowlist, and Auth Header Trust

**Priority:** Required for Phase 1 (ALB setup) and Phase 4 (workspace provisioning).

### Context

When sprout runs inside a Fargate container behind an ALB, three things break:

1. **Bind address**: The web UI binds to `localhost` (127.0.0.1), so the ALB health check and request forwarding can't reach it.
2. **Origin check**: The web UI rejects requests whose `Origin` header isn't `http://localhost:*`. ALB requests arrive with `Origin: https://workspaces.sprout.dev`.
3. **Auth context**: The ALB terminates TLS and can forward authenticated user info via headers, but sprout has no concept of a trusted upstream identity.

### 4a. Bind Address

**New flag and env var:**

```
--bind <addr>           Bind address for the web UI (default: 127.0.0.1)
SPROUT_BIND_ADDR        Environment variable equivalent
```

In `pkg/webui/server.go`, wherever the HTTP server is started (likely `http.ListenAndServe`), change:

```go
// Before:
addr := fmt.Sprintf("127.0.0.1:%d", port)

// After:
bindAddr := configuration.GetEnv("SPROUT_BIND_ADDR", "SPROUT_BIND_ADDR")
if bindAddr == "" {
    bindAddr = "127.0.0.1"
}
addr := fmt.Sprintf("%s:%d", bindAddr, port)
```

The Fargate entrypoint will set `SPROUT_BIND_ADDR=0.0.0.0` or pass `--bind 0.0.0.0`.

### 4b. Origin Allowlist

**New env var:**

```
SPROUT_ALLOWED_ORIGINS   Comma-separated list of allowed origins (in addition to localhost)
```

In the origin-check middleware (likely in `pkg/webui/server.go` or a middleware file), modify the validation:

```go
func isAllowedOrigin(origin string) bool {
    // Always allow localhost
    if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
        return true
    }

    // Check allowlist
    allowed := configuration.GetEnv("SPROUT_ALLOWED_ORIGINS", "")
    if allowed == "" {
        return false
    }
    for _, a := range strings.Split(allowed, ",") {
        if strings.TrimSpace(a) == origin {
            return true
        }
    }
    return false
}
```

In production, the Fargate task will have:
```
SPROUT_ALLOWED_ORIGINS=https://workspaces.sprout.dev,https://app.sprout.dev
```

### 4c. Auth Header Trust (Service Mode)

When `SPROUT_SERVICE=1` is set and the request arrives with trusted headers from the ALB, sprout should recognize the upstream identity without its own auth.

**New env var:**

```
SPROUT_TRUSTED_USER_HEADER   Header name containing the authenticated user ID (e.g., X-Sprout-User-ID)
```

When set, the web UI middleware reads the user ID from this header and makes it available for logging and access control. This is ONLY active when `SPROUT_SERVICE=1` — in local mode, this header is ignored to prevent spoofing.

```go
func extractUserID(r *http.Request) string {
    if os.Getenv("SPROUT_SERVICE") != "1" {
        return "" // not in service mode, ignore
    }
    header := configuration.GetEnv("SPROUT_TRUSTED_USER_HEADER", "")
    if header == "" {
        return ""
    }
    return r.Header.Get(header)
}
```

The ALB is configured to set this header after validating the Cognito JWT. Since the ALB-to-container traffic is within the VPC (private subnet + security group), the header cannot be spoofed by external clients.

### Files to Modify

- `pkg/webui/server.go` — Bind address, origin check, auth header extraction
- `cmd/agent.go` — Add `--bind` flag
- `pkg/configuration/env.go` — If created in step 1, add the new env var lookups

### Acceptance Criteria

- [ ] `sprout agent -d --bind 0.0.0.0 --web-port 56000` listens on all interfaces
- [ ] Requests with `Origin: https://workspaces.sprout.dev` are accepted when that origin is in `SPROUT_ALLOWED_ORIGINS`
- [ ] Requests with arbitrary origins are still rejected when not in the allowlist
- [ ] `SPROUT_TRUSTED_USER_HEADER=X-Sprout-User-ID` extracts the user ID in service mode
- [ ] In local mode (no `SPROUT_SERVICE`), the trusted header is ignored
- [ ] `GET /health` is always allowed regardless of origin (for ALB health checks)

---

## 5. Git Diff Robustness — Handle Missing HEAD

**Priority:** Small — required for Phase 4 (task runner).

### Context

In `cmd/agent_result.go`, the `emitJSONResult` function runs:
```go
exec.Command("git", "diff", "HEAD").Output()
exec.Command("git", "diff", "--name-only", "HEAD").Output()
```

In the Lambda task runner, sprout operates on a freshly cloned repo. If the agent makes changes but hasn't committed, `git diff HEAD` works correctly (compares working tree to HEAD). However, edge cases exist:

1. **Shallow clone with no commits** (e.g., `git init` + work): `HEAD` doesn't exist, so `git diff HEAD` fails.
2. **Detached HEAD state**: Works fine, no change needed.
3. **Uncommitted new files**: `git diff HEAD` doesn't show untracked files.

### Required Changes

Update `emitJSONResult` to handle these cases:

```go
func collectGitChanges() (diff string, filesModified []string) {
    // Try diff against HEAD first (normal case)
    if out, err := exec.Command("git", "diff", "HEAD").Output(); err == nil {
        diff = strings.TrimSpace(string(out))
    } else {
        // HEAD doesn't exist — try diffing the index
        if out, err := exec.Command("git", "diff").Output(); err == nil {
            diff = strings.TrimSpace(string(out))
        }
    }

    // Collect modified files (tracked)
    if out, err := exec.Command("git", "diff", "--name-only", "HEAD").Output(); err == nil {
        for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
            if l = strings.TrimSpace(l); l != "" {
                filesModified = append(filesModified, l)
            }
        }
    }

    // Also include untracked new files (important for agent-created files)
    if out, err := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output(); err == nil {
        for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
            if l = strings.TrimSpace(l); l != "" {
                filesModified = append(filesModified, l)
            }
        }
    }

    return diff, filesModified
}
```

### Acceptance Criteria

- [ ] `--output-json` correctly reports diff when HEAD exists (normal case, no regression)
- [ ] `--output-json` reports changes when HEAD doesn't exist (empty repo)
- [ ] Untracked new files appear in `files_modified`
- [ ] No duplicate entries in `files_modified`

---

## 6. ✅ WASM Shell — Merge and Rebrand `browser-wasm-fileserver` Branch

> **Status: COMPLETE** — Branch merged (commit `f7efa5b2`), all files present on main, `SproutWasm` global and `sprout-wasm-fs` IndexedDB names already correct, `go test ./pkg/wasmshell/...` passes.

**Priority:** Required for Phase 3 (Browser IDE shell integration).

### Context

The `browser-wasm-fileserver` branch contains a fully implemented Go→WASM shell that compiles a subset of sprout into WebAssembly and runs in the browser. This gives the Browser IDE an instant, zero-latency terminal experience for file manipulation, exploration, and basic shell operations — without requiring a Fargate workspace to be running.

### Current State (branch: `origin/browser-wasm-fileserver`)

The branch is **substantially complete** with 5,170 lines across 13 files and 3 commits:

**Go WASM module (`cmd/wasm/`):**
- `main.go` — Entry point, registers `SproutWasm` global on `js.Global()` with 11 JS-callable functions: `init`, `executeCommand`, `autoComplete`, `getCwd`, `changeDir`, `writeFile`, `readFile`, `listDir`, `deleteFile`, `getHistory`, `getEnv`
- `store.go` — IndexedDB persistence bridge via `syscall/js`. Implements `wasmshell.StoreWriter`. On init, restores all files from IndexedDB into MEMFS. Writes are synced back to IndexedDB in real-time.
- Build tag: `//go:build js && wasm`

**Shell implementation (`pkg/wasmshell/`):**
- `shell.go` — Command parser with pipeline support (`|`), I/O redirects (`>`, `>>`, `<`, `2>`), environment variable expansion, tilde expansion, quote handling
- `commands.go` — 35 built-in commands: `ls`, `cd`, `pwd`, `cat`, `mkdir`, `rm`, `rmdir`, `cp`, `mv`, `touch`, `echo`, `head`, `tail`, `wc`, `grep`, `sort`, `find`, `tree`, `clear`, `help`, `date`, `whoami`, `env`, `export`, `which`, `type`, `history`, `println`, `basename`, `dirname`, `realpath`, `tr`, `uniq`, `cut`, `tee`
- `completions.go` — Tab completion for commands and file paths
- `store.go` — `StoreWriter` interface for pluggable persistence
- `shell_test.go` — **1,086 lines** of tests covering tokenization, redirects, pipelines, and all command behaviors

**Build script (`scripts/build-wasm.sh`):**
- Copies `wasm_exec.js` from GOROOT
- Compiles `cmd/wasm/` with `GOOS=js GOARCH=wasm`
- Output: `webui/public/wasm/sprout.wasm` (~4.4 MB)

**Frontend integration (`webui/`):**
- `src/services/wasmShell.ts` — Loads the WASM binary, initializes IndexedDB store (`sprout-wasm-fs`), provides typed `WasmShell` interface
- `src/hooks/useWasmShell.ts` — React hook managing WASM lifecycle, CWD state, command execution, tab completion, history, and reinitialization
- `src/components/TerminalPane.tsx` — Updated terminal component (414+ lines of changes) that uses the WASM shell as an offline fallback when no backend connection is available

### Required Changes to Merge

**6a. Rebase onto main and resolve conflicts:**

```bash
git checkout browser-wasm-fileserver
git rebase main
# Resolve any conflicts (primarily import paths if rename happened first)
```

If item 1 (rename) is done first, the rebase will conflict on every import path. Recommended order: merge this branch first, then do the rename across everything.

**6b. Rename all `sprout` references within the branch:**

After merge (or during rebase), update the WASM-specific references:

| File | Change |
|------|--------|
| `cmd/wasm/main.go` | `js.Global().Set("SproutWasm", sprout)` → `js.Global().Set("SproutWasm", sprout)` |
| `cmd/wasm/store.go` | Import path `github.com/alantheprice/sprout/pkg/wasmshell` → `github.com/alantheprice/sprout/pkg/wasmshell` |
| `pkg/wasmshell/commands.go` | `"SHELL": "/bin/sprout-sh"` → `"SHELL": "/bin/sprout-sh"` |
| `pkg/wasmshell/commands.go` | `"HOSTNAME": "sprout-wasm"` → `"HOSTNAME": "sprout-wasm"` |
| `pkg/wasmshell/commands.go` | `"EDITOR": "sprout"` → `"EDITOR": "sprout"` |
| `scripts/build-wasm.sh` | Output: `sprout.wasm` → `sprout.wasm`, all echo strings |
| `webui/public/wasm/sprout.wasm` | Rename to `sprout.wasm` |
| `webui/src/services/wasmShell.ts` | `DB_NAME = 'sprout-wasm-fs'` → `'sprout-wasm-fs'`, `SproutWasm` → `SproutWasm`, `__sproutStore` → `__sproutStore`, all comments |
| `webui/src/hooks/useWasmShell.ts` | Comment references only |
| `webui/src/components/TerminalPane.tsx` | Any `sprout` references in comments or strings |

**6c. Add IndexedDB migration for existing users:**

In `wasmShell.ts`, add a one-time migration from `sprout-wasm-fs` to `sprout-wasm-fs`:

```typescript
async function migrateOldDB(): Promise<void> {
  // Check if old DB exists
  const dbs = await indexedDB.databases();
  const oldExists = dbs.some(db => db.name === 'sprout-wasm-fs');
  const newExists = dbs.some(db => db.name === 'sprout-wasm-fs');

  if (oldExists && !newExists) {
    // Copy all records from old DB to new DB
    const oldDb = await openDB('sprout-wasm-fs');
    const newDb = await openDB('sprout-wasm-fs');
    // ... transfer all records from 'files' store ...
    console.log('[INFO] Migrated WASM filesystem from sprout-wasm-fs to sprout-wasm-fs');
  }
}
```

**6d. Wire the WASM shell into Sprout Foundry's browser IDE:**

The branch's `TerminalPane.tsx` and `useWasmShell.ts` live in sprout's own `webui/` directory (the desktop app's frontend). For the Sprout Foundry browser IDE (`sprout-foundry/browser-ide/src/`), these need to be either:

- **Option A (preferred):** Publish the WASM binary and `wasmShell.ts` service as a standalone package/artifact. The Sprout Foundry browser IDE imports and uses it.
- **Option B:** Copy `wasmShell.ts`, `useWasmShell.ts`, and the built `sprout.wasm` into the browser IDE's source tree during the build step.

Either way, the integration in `sprout-foundry/browser-ide/` involves:
1. Serving `sprout.wasm` and `wasm_exec.js` from the public directory
2. Importing `useWasmShell` hook in the terminal component
3. Using the WASM shell when no Fargate workspace is connected (offline/free-tier fallback)

**6e. Add the WASM build to the Makefile/CI:**

```makefile
build-wasm:
	./scripts/build-wasm.sh

build-all: build-ui build-wasm build
```

Ensure the WASM binary is rebuilt when `pkg/wasmshell/` or `cmd/wasm/` changes.

### What the WASM Shell Does NOT Cover

These remain the responsibility of the Fargate workspace (Mode B) or task runner (Mode A):

| Capability | WASM Shell | Fargate Workspace |
|-----------|-----------|-------------------|
| File operations (read, write, ls, etc.) | ✅ MEMFS + IndexedDB | ✅ Real filesystem + EFS |
| Shell commands (grep, find, sort, etc.) | ✅ 35 built-in commands | ✅ Full Linux shell |
| Git operations | ❌ No git | ✅ Real git binary |
| LLM agent execution | ❌ No network/LLM | ✅ Full sprout agent |
| Package managers (npm, pip, etc.) | ❌ No process exec | ✅ Real package managers |
| Subprocess execution | ❌ No os/exec | ✅ Full PTY support |

The WASM shell is the **file exploration and basic manipulation** layer. For anything requiring git, LLM calls, or real process execution, the user needs a connected Fargate workspace.

### Acceptance Criteria

- [ ] `browser-wasm-fileserver` branch merged into main cleanly
- [ ] All `sprout` references renamed to `sprout` (Go source, JS, WASM globals, IndexedDB)
- [ ] `scripts/build-wasm.sh` produces `sprout.wasm` successfully
- [ ] `pkg/wasmshell/` tests pass: `go test ./pkg/wasmshell/...`
- [ ] WASM binary loads in browser and `window.SproutWasm` is available
- [ ] `SproutWasm.executeCommand('ls -la')` returns valid JSON result
- [ ] `SproutWasm.writeFile` persists to IndexedDB (`sprout-wasm-fs`)
- [ ] Files survive page reload (IndexedDB → MEMFS restore on init)
- [ ] Tab completion works for commands and file paths
- [ ] Pipeline and redirect syntax works (`ls | grep foo > out.txt`)
- [ ] Old `sprout-wasm-fs` IndexedDB is migrated on first load if it exists
- [ ] WASM build integrated into `make build-all`
- [ ] Sprout Foundry browser IDE can consume the WASM shell artifact

---

## 7. Make WebUI Servable by Sprout Foundry via Service Worker Shim

**Priority:** Blocking — Sprout Foundry v1 browser IDE integration is gated on this.

### Context

Sprout Foundry's browser IDE (Mode C — free-tier client-side experience) needs to deliver the sprout editor in the browser. The key insight is that the sprout Go binary already embeds the complete webui (`pkg/webui/static/` — React app, CodeMirror editor, xterm.js terminal, WASM shell) and serves it via HTTP. Rather than duplicating the UI in Foundry, we can **serve the pre-built webui static bundle directly** and use a **Service Worker shim** to intercept the `/api/*` calls and route them appropriately:

- **File/shell operations** (`/api/files`, `/api/create`, `/api/delete`, `/api/rename`, `/api/workspace`, `/api/browse`, `/api/file`) → bridge to the WASM module (`SproutWasm.executeCommand`, `readFile`, `writeFile`, `listDir`, etc.)
- **LLM chat** (`/api/query`, `/api/query/steer`, `/api/query/stop`) → redirect to Foundry's CORS proxy (`POST /proxy/chat`)
- **Terminal** (`/terminal` WebSocket, `/api/terminal/*`) → bridge to WASM shell `executeCommand` (no real PTY, but the 35+ shell commands work)
- **Git operations** (`/api/git/*`) → proxy through Foundry to a server-side git backend (git is a primary use case)
- **Settings/credentials** (`/api/settings/*`, `/api/onboarding/*`) → bridge to Foundry's API for credential management
- **Stats/health** (`/api/stats`, `/health`) → return synthetic responses from WASM state

Features that don't apply in cloud mode (SSH, local instance management) are **hidden via feature flags** in the webui build — the UI never renders these panels rather than showing them with error stubs.

This means the webui React app runs with cloud-mode awareness — it thinks it's talking to the Go backend, but the Service Worker intercepts and routes, and local-only features are compiled out.

### What Currently Exists

- `pkg/webui/static/` contains the full embedded build: `index.html`, `css/`, `js/`, `wasm/sprout.wasm`, `wasm/wasm_exec.js`, icons, manifest, service worker
- `webui/src/services/wasmShell.ts` provides `initWasmShell()` → `WasmShell` interface
- `webui/src/services/api.ts` makes ~100 `clientFetch('/api/...')` calls to the Go backend
- `webui/src/services/websocket.ts` connects via WebSocket for real-time events
- The webui has full editor (CodeMirror `EditorPane`), file browser (`Sidebar`/`FileBrowser`), terminal (`Terminal`), git (`GitHistoryPanel`/`GitSidebarPanel`), chat (`ContextPanel`), and settings panels

### Changes Required in This Repo (sprout)

#### 1. Distributable static bundle (`scripts/build-wasm.sh --dist`)

Add a `--dist` flag that produces a self-contained directory/tarball with everything Foundry needs to serve the webui:

```bash
# scripts/build-wasm.sh --dist dist/sprout-webui-v1.2.3/
# Produces:
#   dist/sprout-webui-v1.2.3/
#     index.html
#     css/
#     js/
#     wasm/sprout.wasm
#     wasm/wasm_exec.js
#     manifest.json
#     icon-*.png
#     version.json        # {"sprout_version": "1.2.3", "build_date": "..."}
```

This is just a copy of `pkg/webui/static/` (or `webui/build/` after a fresh `REACT_APP_SPROUT_MODE=cloud npm run build` + WASM compile) with a version manifest added.

#### 2. Feature flags for cloud mode (`REACT_APP_SPROUT_MODE`)

Add a build-time environment variable `REACT_APP_SPROUT_MODE` that controls which features are rendered:

| Feature | `local` (default) | `cloud` |
|---------|-------------------|----------|
| Editor (CodeMirror) | ✓ | ✓ |
| File browser | ✓ | ✓ |
| Terminal (WASM shell) | ✓ | ✓ |
| LLM chat | ✓ | ✓ |
| Git panels | ✓ | ✓ |
| SSH connections | ✓ | ✘ hidden |
| Instance management | ✓ | ✘ hidden |
| Local terminal PTY | ✓ | ✘ hidden (WASM shell only) |
| Settings (local) | ✓ | ✘ (delegated to Foundry) |

Implementation: a `src/config/mode.ts` that reads `process.env.REACT_APP_SPROUT_MODE` and exports boolean flags (`isCloud`, `supportsSSH`, `supportsInstances`, `supportsLocalTerminal`, etc.). Components conditionally render based on these flags.

```ts
// webui/src/config/mode.ts
export const SPROUT_MODE = process.env.REACT_APP_SPROUT_MODE || 'local';
export const isCloud = SPROUT_MODE === 'cloud';
export const supportsSSH = !isCloud;
export const supportsInstances = !isCloud;
export const supportsLocalTerminal = !isCloud;
```

#### 3. Make `wasmShell.ts` paths configurable

Currently `initWasmShell()` hardcodes `/wasm/sprout.wasm` and `/wasm/wasm_exec.js`. Add optional config:

```ts
export async function initWasmShell(config?: {
  home?: string;
  wasmUrl?: string;       // default: '/wasm/sprout.wasm'
  wasmExecUrl?: string;   // default: '/wasm/wasm_exec.js'
}): Promise<WasmShell>
```

#### 4. Add `Makefile` targets

```makefile
build-webui-dist: build-ui
	REACT_APP_SPROUT_MODE=cloud npm --prefix webui run build
	./scripts/build-wasm.sh --dist dist/sprout-webui-$(VERSION)

build-webui-dist-local: build-ui
	./scripts/build-wasm.sh --dist dist/sprout-webui-$(VERSION)
```

### API Route Classification

For the Service Worker shim (built in Foundry, not here), the webui's ~100 API endpoints fall into these categories:

| Category | Routes | Shim Behavior |
|----------|--------|---------------|
| **WASM-bridgeable** | `/api/files`, `/api/create`, `/api/delete`, `/api/rename`, `/api/file`, `/api/browse`, `/api/workspace`, `/api/workspace/browse` | Call WASM `readFile`/`writeFile`/`listDir`/`deleteFile` and return JSON matching the Go handler's response format |
| **WASM terminal** | `/terminal` (WS), `/api/terminal/history`, `/api/terminal/sessions`, `/api/terminal/shells` | Bridge to WASM `executeCommand`; simulate terminal session via xterm.js ↔ WASM shell |
| **LLM redirect** | `/api/query`, `/api/query/steer`, `/api/query/stop` | Rewrite to Foundry CORS proxy (`POST /proxy/chat`), translate request/response format |
| **Git proxy** | `/api/git/*` | Proxy to Foundry git backend (`/proxy/git/*`) — full git support (clone, commit, push, pull, diff, log, branch) |
| **Foundry API** | `/api/settings/credentials/*`, `/api/onboarding/*`, `/api/providers` | Proxy to Foundry's own API endpoints (credential storage, tier info) |
| **Synthetic** | `/api/stats`, `/health`, `/api/config`, `/api/sessions`, `/api/chat-sessions/*` | Return static/computed responses (no real Go process to query) |
| **Hidden by feature flags** | `/api/instances/*`, `/ssh/*` | Never called — the UI components that would call these are not rendered in cloud mode |
| **Static** | `/static/*`, `/sw.js`, `/manifest.json`, icons | Serve directly from the static bundle (no shim needed) |

> **SSH note:** SSH and remote instance management are local-only features. In cloud mode, these panels are hidden by feature flags — the webui never renders them, so no API calls are made to these endpoints.

> **Git note:** Git is a primary use case. All `/api/git/*` calls are proxied through Foundry to a server-side git backend that operates on workspace storage.

### Files to Create or Modify

- `scripts/build-wasm.sh` — add `--dist` flag for distributable webui+WASM bundle
- `webui/src/config/mode.ts` — new: feature flag module (`REACT_APP_SPROUT_MODE`, `isCloud`, `supportsSSH`, etc.)
- `webui/src/services/wasmShell.ts` — make WASM/wasm_exec URLs configurable
- `webui/src/components/` — conditionally render SSH, instance management, local terminal panels based on mode flags
- `Makefile` — add `build-webui-dist` and `build-webui-dist-local` targets

### Acceptance Criteria

- [ ] `./scripts/build-wasm.sh --dist dist/sprout-webui/` produces a directory containing the full webui build + WASM binary + version.json
- [ ] `initWasmShell({ wasmUrl, wasmExecUrl })` works with custom paths
- [ ] `REACT_APP_SPROUT_MODE=cloud npm run build` produces a webui bundle that does not render SSH or instance management panels
- [ ] Feature flags are centralized in `webui/src/config/mode.ts` and used by all affected components
- [ ] The dist bundle serves correctly from a plain static HTTP server (all assets load, no 404s)
- [ ] `make build-webui-dist` produces a versioned cloud-mode bundle
- [ ] The webui React app loads in a browser when served statically (without the Go backend) — shows a connection error, but renders

### What Foundry Builds (not in this repo)

Foundry's `browser-ide/` will:
1. Serve the sprout webui static bundle from a pinned version
2. Register a Service Worker that intercepts `fetch()` events for `/api/*` and `/ws` routes
3. Initialize `SproutWasm` via `initWasmShell()` inside the Service Worker (or in the main thread, with message passing)
4. Bridge file/shell API calls to the WASM module, LLM calls to Foundry's CORS proxy, git calls to Foundry's git proxy
5. Inject Foundry-specific UI (auth prompts, upgrade CTAs, usage meters) via a thin wrapper around the served `index.html`

### Dependencies

- Section 6 (WASM Shell merge) must be completed first.
- Section 1 (rename) should be completed first so artifact names use `sprout`.

---

## Dependency Graph

```
[1] Rename ledit → sprout  (PARTIALLY DONE — go.mod/imports done, env vars/types/CLI remaining)
 │
 ├──► [2] Add Token Metrics to JSON output  (NOT STARTED)
 │         └──► (Sprout Foundry Phase 4: task runner, Phase 5: billing)
 │
 ├──► [3] Fix --port flag alias  (NOT STARTED)
 │         └──► (Sprout Foundry Phase 1: Docker entrypoint)
 │
 ├──► [4] Service mode (bind, origins, auth header)  (NOT STARTED)
 │         └──► (Sprout Foundry Phase 1: ALB, Phase 4: workspaces)
 │
 ├──► [5] Git diff robustness  (NOT STARTED)
 │         └──► (Sprout Foundry Phase 4: task runner)
 │
 └──► [6] ✅ WASM shell merge + rebrand  (COMPLETE)
           │
           └──► [7] Distributable webui+WASM bundle + Service Worker shim contract  (NOT STARTED)
                     └──► (Sprout Foundry V1: browser IDE serves webui with SW shim)
```

**Recommended order:** [6] is done. Complete [1] (remaining env vars/types/CLI), then [2]–[5] in parallel, then [7] once [1] is done.
