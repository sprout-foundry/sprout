# SP-041: Tool Execution Sandboxing — Making the README's Disclaimer Untrue

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** HIGH (the project's own README says "use at your own risk, ideally in a container" — this spec closes that gap)
**Depends on:** SP-033 (Agent Trust Boundary Hardening) — complementary; this is the runtime-isolation arm of trust boundaries
**Related:** SP-031 (MCP Input Validation), SP-035 (Persona two-gate risk model)

## Problem

`README.md:5` warns: *"Using `sprout` involves interactions with LLMs and external services which may incur costs. Currently there are limited safety checks — use at your own risk, ideally in a container."*

The runtime backs this up. Tool execution is host-level with no isolation:

- **`pkg/agent/tool_handlers_shell.go`** — runs arbitrary `exec.Command` against the host shell. Lines `264, 355` invoke git directly; the user-driven shell tool runs whatever the model emits (subject to the two-gate risk model from SP-035, which gates *which* commands run, not *how* they run).
- **`pkg/pythonruntime/runtime.go:65`** — `exec.Command(python, …)` with no timeout, no resource limits, no filesystem isolation, no network restrictions. Whatever Python script the model writes runs with the full privileges of the sprout process.
- **`pkg/mcp/client.go:97`** — uses `exec.CommandContext` (good, has cancellation), but spawns MCP server subprocesses with full host access.
- **No seccomp, no namespaces, no chroot, no cgroup limits, no read-only filesystem, no network policy.** A tool can `curl`, write anywhere the user can write, fork unbounded children, exhaust memory.

### Concrete attack/failure scenarios

1. **Model-emitted destructive command slips past gates.** SP-035 force-flag detection is good but not exhaustive. A novel combination (`rm` via shell function alias, `find ... -delete`, `dd if=…of=/dev/sda`) classified as `medium_risk_ops` (which auto-approve under EA defaults) is fully executed on the host.
2. **Python tool hangs.** The model emits a Python script with an infinite loop. `pkg/pythonruntime/runtime.go:65` has no timeout — the process runs until OOM or sprout shutdown.
3. **MCP server escape.** A community MCP server downloaded from a third party (a real workflow per `docs/MCP_INTEGRATION.md`) has full host access. A malicious one (or one compromised upstream) can exfiltrate credentials from `~/.config/sprout/` or `~/.aws/`.
4. **Workspace escape.** Tools that should be workspace-scoped (`write_file`, `edit_file`) operate on absolute paths if given them. The validation today is logical (path checks) not enforced by the OS.
5. **Network exfiltration.** Any tool can curl/wget/Python-`urllib` to an attacker-controlled host. There is no egress policy.

### Why "just use Docker" isn't enough

- The README suggests it but doesn't ship it. There's no `Dockerfile` for an isolated agent runtime, no `docker run` recipe for safe operation, no compose file.
- A user who runs sprout *outside* a container (the default path; the installer at `scripts/install.sh` does not containerize) gets zero isolation.
- Even inside a container, all tools share that container's environment. Per-tool sandboxing is finer-grained and protects against a compromised tool (e.g., MCP server) from affecting other tools.

## Goals / Non-Goals

**Goals**
- A pluggable sandbox interface (`SandboxRunner`) that tool execution paths can opt into.
- Three concrete backends: `none` (current behavior, opt-in only), `bubblewrap` (Linux), `firejail` (Linux alternative). Document containerization (Docker/Podman) as an external wrapper, not an in-process backend.
- A default sandbox policy per tool family:
  - `shell` family → bubblewrap with workspace bind-mount, no network, no `/home` except workspace, CPU/memory cgroup limits.
  - `python_runtime` → bubblewrap with stricter filesystem (only workspace + Python stdlib), 30s default timeout.
  - `mcp` server subprocess → bubblewrap with no host filesystem access except a per-MCP scratch dir, no network unless declared in MCP config.
- A timeout on every tool exec (`pkg/pythonruntime/runtime.go:65` is the most egregious gap).
- An egress policy: tools that need network declare it; default is no-network.
- A `--sandbox=none|bubblewrap|firejail` CLI flag, also configurable per-tool in `config.json`.
- A documented threat model in `docs/SECURITY.md` (depends on SP-033 5b).

**Non-Goals**
- Cross-platform sandbox parity from day one. Linux (bubblewrap/firejail) is the priority. macOS (sandbox-exec) and Windows are follow-ups (Phase 5).
- Full VM-level isolation (gVisor, Firecracker, Kata Containers). Out of scope for a developer tool; left to users who need it.
- Sandboxing the sprout *binary itself* — that's the user's container/VM choice.
- Replacing the SP-035 two-gate model — sandboxing is defense-in-depth, not a replacement for risk classification.

## Current State

| Surface | File:Line | Isolation Today |
|---------|-----------|-----------------|
| Shell tool | `pkg/agent/tool_handlers_shell.go` | None — direct `exec.Command` |
| Git via shell tool | `pkg/agent/tool_handlers_shell.go:264, 355` | None |
| Python runtime | `pkg/pythonruntime/runtime.go:65` | None; no timeout; uses `exec.Command` (no context) |
| MCP server subprocess | `pkg/mcp/client.go:97` | Context-cancellable but no filesystem/network isolation |
| Browser/Rod | `pkg/webcontent/browser_rod.go` | None |
| Network egress | (everywhere) | Unrestricted |
| Resource limits | None | No cgroups, no rlimit, no timeouts |

## Proposed Solution

### Track A — Define the sandbox interface

A1. **`SandboxRunner` interface** in a new package `pkg/sandbox/sandbox.go`:
```go
type SandboxRunner interface {
    Name() string                              // "none", "bubblewrap", "firejail"
    Available() bool                           // is the backend installed?
    Run(ctx context.Context, spec ExecSpec) (*ExecResult, error)
}

type ExecSpec struct {
    Command      []string
    Env          []string
    WorkDir      string
    StdinReader  io.Reader
    StdoutWriter io.Writer
    StderrWriter io.Writer

    // Sandbox policy
    Policy SandboxPolicy
}

type SandboxPolicy struct {
    WorkspaceRoot   string          // bind-mount RW
    ReadOnlyPaths   []string        // bind-mount RO (e.g., system Python)
    AllowNetwork    bool            // false by default
    Timeout         time.Duration   // 0 = no timeout (refuse for sandbox != "none")
    MemLimitMB      int             // cgroup memory.max
    CPUQuotaPercent int             // cgroup cpu.max
}

type ExecResult struct {
    ExitCode int
    Stdout   []byte           // if not streamed
    Stderr   []byte
    Duration time.Duration
    Timeout  bool             // did the timeout fire?
    OOM      bool             // did cgroup OOM-kill?
}
```

A2. **`pkg/sandbox/none.go`** — no-op backend; runs `exec.CommandContext` with the timeout but no isolation.

A3. **`pkg/sandbox/bubblewrap.go`** — translates `SandboxPolicy` into a `bwrap` invocation. Validates `bwrap` is installed and the kernel supports user namespaces.

A4. **`pkg/sandbox/firejail.go`** — same, for firejail.

A5. **Backend selection.** `SandboxFromConfig(cfg)` returns the configured backend, falling back to `none` with a startup warning if the chosen backend is unavailable.

### Track B — Wire tools through the sandbox

B1. **`pkg/agent/tool_handlers_shell.go`** — wrap `exec.Command` calls with `sandbox.Run` using a `shell` policy. Default timeout: 60s. Default mem limit: 1GB. Network: per-config.

B2. **`pkg/pythonruntime/runtime.go:65`** — convert to `exec.CommandContext` first (close the no-timeout gap immediately as a sub-fix); then wrap with sandbox. Default Python timeout: 30s. Bind-mount workspace + system Python stdlib RO.

B3. **`pkg/mcp/client.go`** — wrap subprocess launch with sandbox using an `mcp_server` policy. Each MCP server gets a scratch dir at `~/.config/sprout/mcp_scratch/<server_id>/`. Network: per-MCP-config opt-in.

B4. **Browser/Rod** — Chromium has its own sandbox; document the layering. No additional wrapping unless threat-modeling reveals gaps.

### Track C — Per-tool policy in config

C1. **Extend `pkg/configuration/config.go`** with a `SandboxConfig` block:
```json
{
  "sandbox": {
    "backend": "bubblewrap",         // or "none", "firejail"
    "policies": {
      "shell":          {"timeout_s": 60, "mem_mb": 1024, "network": false},
      "python_runtime": {"timeout_s": 30, "mem_mb": 512,  "network": false},
      "mcp_server":     {"timeout_s": 0,  "mem_mb": 512,  "network": false}
    }
  }
}
```

C2. **Per-MCP-server overrides.** MCP server configs can opt into network for that server only: `"sandbox": {"network": true}`. Surface this in the WebUI MCP settings tab — checkbox per server.

C3. **Sane defaults.** On first run, if `bubblewrap` is detected, default backend = `bubblewrap`; otherwise `none` with a startup notice pointing at install instructions.

### Track D — CLI + observability

D1. **`--sandbox=<none|bubblewrap|firejail>` CLI flag** for one-off overrides.

D2. **Startup notice.** On agent startup, log the active sandbox backend and any policy gaps (e.g., "shell policy network=true — egress is unrestricted").

D3. **Per-execution telemetry.** Sandbox events (start, end, timeout, OOM-kill) emitted via `pkg/events/` and written to runlog. Useful for post-mortems.

D4. **WebUI sandbox indicator.** Status bar shows `Sandbox: bubblewrap` or `Sandbox: off (insecure)` so the user can see the runtime mode at a glance.

### Track E — Tests + threat model

E1. **`pkg/sandbox/bubblewrap_test.go`** — table tests asserting that the generated `bwrap` invocation matches expected args for various policies. Skip on platforms without `bwrap`.

E2. **`TestSandbox_NetworkBlocked`** — runs `curl https://example.com` under each backend with `AllowNetwork=false`; asserts non-zero exit. Skip if backend unavailable.

E3. **`TestSandbox_TimeoutKills`** — runs `sleep 60` with timeout 1s; asserts the process is killed within 2s and `ExecResult.Timeout == true`.

E4. **`TestSandbox_MemLimitKills`** — runs a memory-allocating script with `MemLimitMB=64`; asserts OOM kill.

E5. **`TestSandbox_WorkspaceRoot_Isolated`** — runs `ls /home/<user>/.aws` under sandbox with `WorkspaceRoot=/tmp/sandbox-test`; asserts no access.

E6. **Threat model document.** `docs/SANDBOX_THREAT_MODEL.md` — what each backend protects against, residual risks, when to use VM-level isolation instead.

## Implementation Phases

### Phase 1: Close the easy gaps first
[ ] SP-041-1a: Convert `pkg/pythonruntime/runtime.go:65` to `exec.CommandContext` with a default 30s timeout. (Sub-fix that ships value before the sandbox lands.)
[ ] SP-041-1b: Add timeouts to any other contextless `exec.Command` in `pkg/agent/tool_handlers_shell.go`. Grep for `exec.Command(` (no `Context`) across the codebase.

### Phase 2: Sandbox interface
[ ] SP-041-2a: Create `pkg/sandbox/sandbox.go` with `SandboxRunner`, `ExecSpec`, `SandboxPolicy`, `ExecResult`.
[ ] SP-041-2b: Implement `pkg/sandbox/none.go` (no-op runner with timeout enforcement).
[ ] SP-041-2c: Implement `pkg/sandbox/bubblewrap.go` (Linux primary backend).
[ ] SP-041-2d: Implement `pkg/sandbox/firejail.go` (Linux fallback).
[ ] SP-041-2e: Add `SandboxFromConfig()` selector with availability detection and fallback warning.

### Phase 3: Wire tools
[ ] SP-041-3a: Wrap shell tool exec sites (`pkg/agent/tool_handlers_shell.go`) with `sandbox.Run`.
[ ] SP-041-3b: Wrap `pkg/pythonruntime/runtime.go` with `sandbox.Run`.
[ ] SP-041-3c: Wrap `pkg/mcp/client.go:97` subprocess launch with `sandbox.Run`; per-MCP scratch dir creation.
[ ] SP-041-3d: Decide on browser/Rod handling — document the layering even if no code change.

### Phase 4: Config + CLI
[ ] SP-041-4a: Add `SandboxConfig` to `pkg/configuration/config.go`. Defaults + per-tool policies.
[ ] SP-041-4b: Add `--sandbox=<backend>` CLI flag in `cmd/`.
[ ] SP-041-4c: WebUI MCP settings tab: per-server network checkbox.
[ ] SP-041-4d: Startup notice listing active backend and any "insecure" policies.

### Phase 5: Tests
[ ] SP-041-5a: `pkg/sandbox/bubblewrap_test.go` — argument-generation table tests.
[ ] SP-041-5b: `TestSandbox_NetworkBlocked` (skips if backend unavailable).
[ ] SP-041-5c: `TestSandbox_TimeoutKills`.
[ ] SP-041-5d: `TestSandbox_MemLimitKills`.
[ ] SP-041-5e: `TestSandbox_WorkspaceRoot_Isolated`.

### Phase 6: Documentation + threat model
[ ] SP-041-6a: Write `docs/SANDBOX_THREAT_MODEL.md`.
[ ] SP-041-6b: Update `README.md` — replace "use at your own risk, ideally in a container" with concrete sandboxing guidance and a link to the threat model.
[ ] SP-041-6c: Cross-link from `docs/SECURITY.md` (SP-033 5b) to the sandbox doc.

### Phase 7: Future platforms (deferred)
[ ] SP-041-7a: macOS `sandbox-exec` backend — separate spec or follow-up.
[ ] SP-041-7b: Windows AppContainer / Job Object backend — separate spec.

## Success Criteria

| Metric | Target |
|--------|--------|
| Tools without a timeout | 0 |
| Default backend on Linux with bubblewrap installed | `bubblewrap` |
| Sandbox bypass in `TestSandbox_NetworkBlocked` | None |
| `README.md` disclaimer "use at your own risk, ideally in a container" | Replaced with concrete guidance |
| `docs/SANDBOX_THREAT_MODEL.md` | Exists and is accurate |

## Files Reference

| File | Action |
|------|--------|
| `pkg/sandbox/sandbox.go` | Create: interface + types |
| `pkg/sandbox/none.go` | Create: no-op backend |
| `pkg/sandbox/bubblewrap.go` | Create: bwrap backend |
| `pkg/sandbox/firejail.go` | Create: firejail backend |
| `pkg/sandbox/bubblewrap_test.go` | Create: arg-generation table tests |
| `pkg/sandbox/sandbox_integration_test.go` | Create: network/timeout/mem/isolation tests |
| `pkg/pythonruntime/runtime.go` | Modify: line 65 use `CommandContext` + sandbox |
| `pkg/agent/tool_handlers_shell.go` | Modify: wrap exec sites with sandbox |
| `pkg/mcp/client.go` | Modify: line 97 wrap subprocess with sandbox |
| `pkg/configuration/config.go` | Modify: add `SandboxConfig` block |
| `cmd/` | Modify: add `--sandbox` flag |
| `webui/src/components/SettingsPanel.../MCPSettingsTab.tsx` | Modify: per-server network checkbox |
| `docs/SANDBOX_THREAT_MODEL.md` | Create |
| `docs/SECURITY.md` | Modify (when SP-033 lands): cross-link |
| `README.md` | Modify: replace the disclaimer line |

## Risks

- **bubblewrap not installed on user systems.** Mitigation: detect on startup, fall back to `none` with a loud warning + install instructions printed to stderr. Document in `docs/SANDBOX_THREAT_MODEL.md`.
- **User namespaces disabled by kernel hardening.** Some distros disable unprivileged user namespaces. Mitigation: bubblewrap availability check includes a quick `bwrap --version` exec; fall back to `firejail` or `none`.
- **Sandbox breaks tools that depend on host paths.** E.g., a Python tool that imports a user-installed package outside the workspace. Mitigation: per-tool `ReadOnlyPaths` lets users add the necessary site-packages dir; document the recipe.
- **Performance overhead.** bubblewrap adds ~50ms startup per exec. Mitigation: document; for high-frequency shell exec (rare), consider keeping a long-lived sandboxed shell session.
- **False sense of security.** Users may assume bubblewrap = container = perfect isolation. Mitigation: the threat model document is explicit about residual risks (shared kernel, syscall surface, side-channel attacks, …).
- **macOS/Windows users get nothing on day one.** Mitigation: explicitly call out in README that Linux is primary; cross-platform parity is a follow-up.
