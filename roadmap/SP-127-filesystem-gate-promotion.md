# SP-127 — Promote Filesystem Gate to Gate-1 (Static Classifier)

**Status:** 🟢 M1–M3 Shipped on `main` (M1 commit `a06a3f8a`, M2 commits `1acff1cb`–`ac969831`, M3 commit `<HEAD>`). M4 (performance cache) deferred.

M1: `staticGateAutoApprove` now decides both bypass AND path-tier; `classifyFileAccess` is Gate 1's path-tier classifier.
M2: All 6 file-touching handlers (`write_file`, `edit_file`, `read_file`, `list_directory`, `write_structured_file`, `patch_structured_file`) migrated to `PrecheckFileAccess` so Deny paths return typed errors and Allow paths bypass `withFilesystemApproval`.
M3: Audit trail (M3.2), documentation (M3.3), conformance test extension (M3.4), integration test (M3.5).
**Created:** 2026-07-20
**Type:** Architecture follow-up (security model unification)

## TL;DR

The dual-path filesystem approval flow (CLI picker + WebUI event-bus
dialog) added by `fix/file-tools-filesystem-gate` (this branch) works
correctly, but the architecture has a wart: file-touching tool handlers
carry the gate on `ToolEnv.FilesystemGate` and consult it *after* the
resolve step throws `ErrOutsideWorkingDirectory`. This means:

1. **Two mental models coexist.** Gate 1 (the static classifier) and
   the filesystem gate share responsibility for the same class of
   decision ("is this file access allowed?"). Today Gate 1
   auto-approves based on tool + risk class, while the filesystem
   gate classifies by path tier. Both routes can fire for the same
   call, and the order of operations is non-obvious.
2. **The Go live dispatch path duplicates a check `pkg/filesystem`
   already does.** `SafeResolvePath[ForWrite]WithBypass` is the
   source of truth for the off-workspace error; the handler's
   `withFilesystemApproval` is a recovery wrapper that fires *only*
   when that check fails. This is correct but adds a layer that
   exists solely to translate the same error into a UI prompt.
3. **`patch_structured_file` had to be retrofitted.** The migration
   revealed that the handler used raw `os.ReadFile` / `os.WriteFile`,
   bypassing the resolver entirely. Future handlers will hit the
   same trap unless the check is centralized.
4. **Symlink disclosure is handled in the helper, not the resolver.**
   `withFilesystemApproval` calls `filepath.EvalSymlinks` on the
   user-supplied path and passes the canonical target to the gate,
   so a workspace symlink to `/etc/passwd` shows the resolved
   target in the approval dialog instead of the benign-looking
   link name. The resolver already computes the canonical target
   (it's in the error message), but doesn't surface it as a
   structured return. Consolidating into Gate 1 should also unify
   the symlink disclosure so the resolver returns
   `(canonical, ok)` and Gate 1 reads `canonical` directly.

This spec scopes the consolidation: move filesystem policy out of
`pkg/filesystem`'s resolve helper and into the agent's Gate-1 static
classifier, so a single call site (`staticGateAutoApprove`) decides
whether the access proceeds, prompts, or hard-blocks based on
the **combined** view of (tool, args, path tier, session state).

## Background

`pkg/filesystem.SafeResolvePathWithBypass` enforces a strict "files
must live under the workspace root" rule and returns
`ErrOutsideWorkingDirectory` / `ErrWriteOutsideWorkingDirectory`
sentinels. Pre-fix, only the **legacy** func-style tool path (under
`pkg/agent/tool_handlers_file.go`) translated these sentinels into
prompts. The **new** ToolHandler-based seed path (write_file,
edit_file, read_file, list_directory, write_structured_file,
patch_structured_file, handlePDF) bypassed the translation and
hard-errored on off-workspace paths.

The fix introduced `pkg/agent_tools.FilesystemGate` (an interface),
`withFilesystemApproval` (a generic retry-on-approval helper), and
`pkg/agent.filesystemGateAdapter` (which delegates to
`handleFileSecurityError`). Both surfaces — CLI picker and WebUI
event-bus dialog — are now reachable from every file-touching
handler.

The fix is complete and tested, but it leaves an architectural smell:
the policy decision ("can this access proceed?") is split across two
packages and two layers.

## Current architecture (post-fix)

```
Tool call
  │
  ▼
[Gate 1: staticGateAutoApprove in seed_tool_security.go:235]
  │ class: SAFE / CAUTION / DANGEROUS / CRITICAL / HARD_BLOCK
  │ if auto-approve → skip approval flow
  │ if hard-block → reject
  │ otherwise → continue (Gate 2 may still prompt)
  ▼
[Handler Execute]
  │ ToolEnv.FilesystemGate carries the agent's approval flow
  ▼
[withFilesystemApproval helper]
  │ wraps the resolve step, retries on ErrOutsideWorkingDirectory
  │ if approved: ctx gets WithSecurityBypass → retry succeeds
  │ if denied: original error returned to caller
  ▼
[SafeResolvePathWithBypass] ─► ErrOutsideWorkingDirectory
  │ if ctx has WithSecurityBypass → allow
  │ if path is under workspace root → allow
  │ otherwise → ErrOutsideWorkingDirectory
  ▼
[Handler continues with cleanPath]
```

Two approval flows:

- **Gate 1** in `pkg/agent/seed_tool_security.go:235` — `staticGateAutoApprove` —
  consults `agent.GetUnsafeMode()`, `agent.IsSessionElevated()`,
  `secResult.IsHardBlock`. Routes through WebUI if a browser is
  attached, CLI otherwise. Decisions: `Approve / Deny / ApproveOnce
  / ApproveAlways / Elevate`.
- **Filesystem gate** in `pkg/agent_tools/filesystem_gate.go` —
  `withFilesystemApproval` — fires only on the two filesystem
  sentinels. Routes through the same WebUI/CLI dispatch via
  `filesystemGateAdapter → handleFileSecurityError`. Decisions:
  `Approve / Deny / ApproveOnce / AllowFolderSession / Elevate`.

## Proposed architecture

```
Tool call
  │
  ▼
[Gate 1: staticGateAutoApprove — extended]
  │ class: SAFE / CAUTION / DANGEROUS / CRITICAL / HARD_BLOCK
  │ NEW: path-tier classifier runs here too, BEFORE the tool layer
  │      Off-workspace External → CAUTION (with FS-specific dialog)
  │      Off-workspace Sensitive → CRITICAL (Sensitive never allowlists)
  │      In-workspace → unchanged
  │ NEW: resolver returns (canonicalPath, ok) — Gate 1 displays the
  │      canonical target in the dialog (security: a workspace symlink
  │      to /etc/passwd must NOT be approved under a benign-looking
  │      link name)
  │ if auto-approve → set ctx.WithSecurityBypass + skip approval flow
  │ if hard-block → reject
  │ otherwise → continue (filesystem gate dialog fires)
  ▼
[Handler Execute]
  │ No ToolEnv.FilesystemGate needed; ctx alone carries the bypass token
  ▼
[SafeResolvePathWithBypass]
  │ if ctx has WithSecurityBypass → allow (no error)
  │ if path is under workspace root → allow
  │ otherwise → ErrOutsideWorkingDirectory (rare: only if Gate 1 missed)
  ▼
[Handler continues with cleanPath]
```

Single approval flow, two roles:

- **Gate 1** owns the decision (allow / prompt / block) and writes
  the bypass token into ctx. It also owns the canonical-target
  display so the user sees the actual filesystem object that will
  be touched.
- **`SafeResolvePathWithBypass`** enforces the resolution and trusts
  the bypass token. Returns the canonical target alongside the
  verdict so the dialog and the audit log have consistent
  information.

## What changes

### 1. Extend `staticGateAutoApprove` with path-tier classification

Currently `staticGateAutoApprove` only inspects the tool name and
generic args (e.g., `command` for shell_command). Add a path-tier
classifier that runs for file-touching tools (`write_file`,
`edit_file`, `read_file`, `list_directory`, `write_structured_file`,
`patch_structured_file`, `handlePDF`).

The classifier needs the resolved absolute path, which means Gate 1
must call `SafeResolvePathWithBypass` *itself* before deciding. That
creates an ordering question: Gate 1 runs *before* the handler, but
the path resolution is inside the handler. Two options:

- **(a) Move resolve to Gate 1.** Gate 1 resolves the path, classifies
  it, sets the bypass token (or returns hard-block), and the
  handler's call to `SafeResolvePathWithBypass` is then a no-op (the
  path is already approved). Downside: Gate 1 now has filesystem
  knowledge, blurring the boundary between "policy" and "mechanism."

- **(b) Gate 1 inspects args only; the handler owns resolve.** Gate 1
  can't decide off-workspace vs. on-workspace without resolving, so
  it consults a new helper `ClassifyPathAccessFromArgs(args)` that
  does a lightweight `filepath.Clean` + `EvalSymlinks` lookup. This
  duplicates work the resolver already does, but keeps Gate 1
  filesystem-aware only by helper, not by direct import.

Recommended: **(a)** for cleanliness. The "filesystem knowledge" Gate
1 gains is just the path-tier classification function, which is
already a separate function (`ClassifyPathAccess`). Gate 1 imports
`pkg/filesystem` for the resolve + tier classify helpers and applies
its existing CAUTION/DANGEROUS/CRITICAL logic on top.

### 2. Promote the filesystem dialog to a Gate-1 outcome

The current `handleFileSecurityError` already returns one of
`Deny / ApproveOnce / AllowFolderSession / Elevate`. The Gate-1
prompt manager (`security.ApprovalManager`) already understands these
decisions. The only work is to make `staticGateAutoApprove` route
through the dialog for off-workspace file paths, instead of relying
on the handler-side `withFilesystemApproval` recovery wrapper.

### 3. Remove the `FilesystemGate` interface and `withFilesystemApproval`

Once Gate 1 owns the decision, the handler-side machinery is dead
weight. The handler just calls `SafeResolvePathWithBypass` and
trusts the bypass token. The 4-option dialog still works the same
way; only its caller moves.

### 4. Tighten `SafeResolvePathWithBypass` semantics

Today the resolver accepts the bypass token on ctx and silently
allows off-workspace paths. With the new architecture, the bypass
token is **only** set by Gate 1's approval flow (or by `--unsafe` /
session elevation). The resolver's behavior stays the same; the
*source* of the token changes.

This also closes the `patch_structured_file` retrofit gap. New
handlers added in the future will automatically get Gate-1 coverage
because they only need to call `SafeResolvePathWithBypass` — there's
no `withFilesystemApproval` wrapper to forget.

## Non-goals

- **Not changing the user-facing dialog.** Same 2/3/4 options
  depending on tier, same "Allow folder this session" behavior, same
  Sensitive-tier demotion. The dialog shape is right; only its
  call site moves.
- **Not changing the agent's session allowlist data model.** The
  per-folder allowlist (`agent.sessionAllowedFolders`) is the right
  granularity; Gate 1 just reads and writes it directly.
- **Not changing `--unsafe` or session elevation semantics.** Both
  already bypass all gates; consolidating doesn't change that.
- **Not removing `handleFileSecurityError`.** The function still
  exists and is still the canonical place to apply a user decision
  to filesystem state (folder allowlist, ctx bypass). Only its
  caller changes.

## Risks

1. **Migration friction.** Six handlers currently use
   `withFilesystemApproval`. Removing the helper requires touching
   each handler. Mitigation: do it as a single PR with the
   consolidation, with the existing test suite as the safety net.

2. **Test surface growth.** Gate 1 currently has
   `staticGateAutoApprove` unit tests; the new path-tier integration
   needs equivalent coverage. The test agent's `SkipPrompt=true`
   default makes CLI tests deterministic; WebUI tests are heavier
   (already present in `filesystem_gate_adapter_test.go`).

3. **Performance.** `SafeResolvePathWithBypass` now runs twice per
   file op (once in Gate 1, once in the handler). The duplicate work
   is bounded (one symlink eval + one prefix check), but for very
   large batch operations it adds up. Mitigation: cache the
   resolver's verdict on ctx (e.g., `WithFilesystemResolved(ctx,
   path, allow bool)`), so the handler's call is a hashmap lookup.

4. **Subagents.** The current architecture already short-circuits
   subagent filesystem errors (they propagate without prompting —
   `handleFileSecurityError` returns `(ctx, false)` for subagents).
   The new Gate-1 path must preserve that. Mitigation: subagent
   check in `staticGateAutoApprove` mirrors the existing one in
   `handleFileSecurityError`.

## Acceptance criteria

- [x] `staticGateAutoApprove` runs `ClassifyPathAccess` for file-touching tools and routes through the same dialog as the current handler-side gate. *(M1: `a06a3f8a`)*
- [x] `FileAccessClassifier` interface in `pkg/agent_tools`; `PrecheckFileAccess` in `pkg/agent_tools/security_precheck.go`. *(M2: `1acff1cb`)*
- [x] All 6 file-touching handlers (`write_file`, `edit_file`, `read_file`, `list_directory`, `write_structured_file`, `patch_structured_file`) migrated to `PrecheckFileAccess` pre-check. *(M2: `1acff1cb`–`ac969831`)*
- [x] Gate-1/Gate-2 conformance test pins path-tier agreement for 16+ path/mode combinations. *(M1: `a06a3f8a`; extended M3)*
- [x] Audit trail: every `ClassifyFileAccess` verdict (allow/prompt/deny) writes a JSONL entry via the audit logger on ctx. *(M3.2)*
- [x] Documentation updated. *(M3.3)*
- [ ] `withFilesystemApproval`, `FilesystemGate` interface, and `filesystemGateAdapter` are removed. *(Deferred — still used as fallback for Prompt paths; removal is M4 if regression-free)*
- [ ] Performance benchmark shows ≤10% regression for the common case. Cache the resolved verdict if needed. *(M4)*
- [ ] `docs/SECURITY.md` updated to describe the single-flow architecture. *(Deferred — M4 or follow-up)*

## Migration plan (M1–M4)

The four migration phases below remain on the path to a single-gate
architecture. They are now labeled **M1–M4** to leave room for the
adjacent **Phase 2 (Workflow `allowed_paths` runtime extensions)**
which lands in parallel but ships independently.

1. **M1 — bring path-tier into Gate 1.** ✅ Done (`a06a3f8a`).
   `staticGateAutoApprove` now classifies file paths; both Gate-1
   entry points (`ExecuteTool`, seed pre-execute hook) consult the
   same classifier. `withFilesystemApproval` stays as a fallback for
   Prompt paths.
2. **M2 — migrate handlers.** ✅ Done (`1acff1cb`–`ac969831`).
   All 6 file-touching handlers call `PrecheckFileAccess` at the
   top of `Execute`. Deny paths return typed errors immediately;
   Allow paths bypass `withFilesystemApproval`; Prompt paths fall
   through to the dialog.
3. **M3 — housekeeping.** ✅ Done (this commit set).
   Dead-code audit (none found), audit trail additions, documentation
   updates, conformance test extension, integration test.
4. **M4 — remove the old machinery.** ⏳ Deferred.
   Delete `FilesystemGate`, `withFilesystemApproval`,
   `filesystemGateAdapter` only after M3 integration tests confirm
   no regression. Benchmark first; add resolved-verdict cache if
   needed.

## Open questions

- Should the dialog title change to reflect that Gate 1 now owns
  the decision? Today it's "Filesystem Security Warning" — that's
  still accurate.
- Does `IsHardBlock` need a new tier for off-workspace Sensitive
  paths, or is the existing Sensitive handling (always prompts,
  never allowlists) sufficient? My read: sufficient. Sensitive-tier
  paths already get the most-restrictive dialog; promoting them to
  hard-block would prevent legitimate one-off reads.
- Should Gate 1 cache resolved paths within a single turn to avoid
  duplicate symlink evals? The benchmark will tell us.

## Phase 2 — Workflow `allowed_paths` runtime extensions

**Status:** 🟢 Shipped (Phase 2.1–2.6 + Phase 2.6 LogJSON fix, commits `389a9d31`, `e5dc181e`, `1e1e4bb9`, `0a0a93e3`, `3396f0c3`, `dc167b03`).
**Depends on:** Phase 1 of the original SP-127 work (the JSON schema,
validation, session seeding, and `read_only` mode enforcement at
`handleFileSecurityError` are all already shipped in this branch's
parent commit set).
**Parallel to:** M1–M4 above (this work ships independently of the
gate-consolidation migration).

### Why this phase exists

A workflow author can declare extra folders in `allowed_paths` and the
runtime correctly **pre-seeds the agent's session allowlist** at launch
(`pkg/agent/tool_handlers_automate.go:91–94` iterates `summary.AllowedPaths`
and calls `AddSessionAllowedFolder` + `SetSessionAllowedFolderMode`).
File tools that take an **absolute path** work end-to-end: the gate
recognizes the entry, honors the mode (`read_only` blocks writes with
a workflow-specific denial reason), and skips the per-call approval
prompt.

The agent, however, cannot **operate across multiple folders the way
the workflow author intended**. Three concrete gaps:

1. **No `cd`-target validation.** `pkg/agent/agent_shell.go::updateShellCwd`
   tracks every `cd <path>` shell command and stores the result as the
   agent's "logical cwd." Subsequent shell commands and the
   `pkg/filesystem` resolver both consult `effectiveCwd()`, so a
   successful `cd /etc && cat passwd` reads `/etc/passwd` as if it
   were an in-workspace file. The workflow may legitimately declare
   `/tmp/workspace` as `read_write`, but the agent can still pivot to
   any other directory with `cd <that-dir>` and operate there. There
   is no validation that the cd target is the workspace root or a
   declared `allowed_paths` entry.

2. **Absolute paths required.** Because the resolver's off-workspace
   check is keyed to the original workspace root, the only way to
   reach an allowed_path today is to spell out its absolute path on
   every file tool call. A workflow that wants the agent to "refactor
   `lib/foo.go` and its twin under `/home/user/projects/sibling`" has
   no way to declare that second folder as the agent's *working
   context* for the relevant steps — the model has to remember to use
   the prefix on every call.

3. **No per-step scoping.** The schema only declares `allowed_paths`
   at the **workflow level**. A workflow whose initial step needs
   `/opt/marketing-data` but whose later steps should not have access
   cannot narrow the scope without re-launching under a different
   workflow file.

4. **No audit surface.** When a file tool call lands in an allowed_path
   entry, the event bus sees a normal tool dispatch with no marker
   distinguishing "user approved this access right now" from
   "workflow pre-declared this folder." The WebUI's automation panel
   can't show the user "this workflow's allowed_paths: 3 declared,
   2 used in this run."

5. **Symlink escape not re-validated.** The gate resolves symlinks
   for the dialog display, but once an allowed folder is granted, a
   symlink inside it pointing at `/etc` would still be writable. The
   mode check happens on the *original* path, not the resolved target.

### What Phase 2 changes

#### 2.1 — `cd`-target validation against the allowlist

`updateShellCwd` (in `pkg/agent/agent_shell.go`) becomes a gated
operation. The gate consults a new method on the agent,
`IsCdTargetAllowed(target string) bool`, which returns true only when
`target` is:

- the workspace root, OR
- a subdirectory of the workspace root, OR
- a declared `allowed_paths` entry (or subdirectory thereof) for the
  currently-running workflow, OR
- a session-allowlisted folder from a previous approval.

`cd` to any other path is **rejected** — the shell output reports
"cd refused: target is not in the workspace or the workflow's
declared allowed_paths" and the agent's tracked cwd is unchanged.
This closes gap #1.

The check uses `filepath.Clean` + `filepath.IsAbs` + prefix-match
against the workspace root and each allowlisted folder. Symlinks are
NOT resolved at this stage — the check is purely lexical, matching
the existing `IsFolderSessionAllowed` semantics. Symlink escape
re-validation is gap #5 (see 2.5 below).

#### 2.2 — Per-step `allowed_paths` override

Schema additions in `pkg/workflow/types.go`:

- `AgentWorkflowInitial` gains a new `AllowedPaths []AllowedPath`
  field (parallel to the existing `AgentWorkflowConfig.AllowedPaths`).
- `AgentWorkflowStep` gains the same `AllowedPaths []AllowedPath`
  field.
- `AgentWorkflowConfig.AllowedPaths` stays as the **workflow-wide**
  base set.

Why a field rather than embedding: `AgentWorkflowStep` is a flat
struct (all fields flat in the same namespace) and embedding
`AllowedPaths` would flatten its `Path` / `Mode` / `Reason` fields
into the step's own, producing confusing JSON like
`{"name": "...", "path": "..."}`. A named field is also clearer at
the call site (`step.AllowedPaths[i].Mode` vs `step.Mode`).

Semantics: the effective allowlist at step N is the **union** of the
workflow-level set and the step-level set. Step additions are
**additive** — they cannot narrow the workflow-level scope. (Narrowing
is a future Phase 3 capability; for now, restricting access means
splitting the workflow into two workflow files.) The workflow-level
set is the **base** so a step author can reason "what's already
allowed" without re-declaring the workspace.

Loader validation: each step-level entry runs the existing
`AllowedPath.Validate()` rules (absolute, cleaned, mode enum,
no `..`). Duplicates between workflow-level and step-level are
allowed and produce a single entry on the merged list (the step-level
mode wins on conflict — last write wins, with a loader warning if
the modes differ).

#### 2.3 — Step-aware allowlist application

When the workflow runner enters a step (initial or numbered), it
applies the step-level `allowed_paths` on top of the workflow-level
set. Specifically, `pkg/agent/tool_handlers_automate.go:91–94` becomes
the workflow-level seed; a new function
`applyStepAllowedPaths(a, step)` runs before each step executes to
add the step-level entries to the agent's session allowlist and call
`SetSessionAllowedFolderMode` for each.

Removing on step end is **not required** for v1 — workflow steps
are linear and the next step's apply call is idempotent. If a future
phase introduces parallel branches or retry loops, a
`ClearStepAddedAllowedPaths` helper will land alongside.

#### 2.4 — `effectiveCwd`-aware resolver scope

Once `cd` is gated (2.1), the resolver can be tightened so the
off-workspace check is bound to the **smaller** of (workspace root,
the allowlist covering `effectiveCwd()`). Concretely:

- If `effectiveCwd()` is under an `allowed_paths` entry, that entry's
  folder becomes the resolution scope for the duration of the
  session — `SafeResolvePathWithBypass` accepts paths under that
  folder without an approval prompt, just like the workspace root.
- If `effectiveCwd()` is back at the workspace root, scope reverts.
- If `effectiveCwd()` points at a non-allowed folder (which can't
  happen post-2.1, but defensive against bypass), the resolver
  falls back to the original "off-workspace → error" path.

This addresses gap #2 directly. After landing, a workflow with
`allowed_paths: [{path: /home/user/projects/sibling, mode: read_write}]`
lets the agent `cd /home/user/projects/sibling && cat README.md` and
`write_file notes/todo.md` both proceed without per-call prompts, as
long as the resolved path stays under that folder.

#### 2.5 — Symlink re-validation under allowed folders

`SafeResolvePathWithBypass` (in `pkg/filesystem`) gains a new mode:
when called with a ctx that has `WithFilesystemBypassScope(folder)
set, the resolver also calls `filepath.EvalSymlinks` on the input and
checks the resolved target against the same scope. If the resolved
target escapes the scope (e.g., a symlink inside an allowed folder
points at `/etc`), the resolver returns the off-workspace sentinel
even when the lexical path is in-scope.

This closes gap #5. The mechanism reuses `resolveCanonicalTimeout` (3s)
to bound network-mount hangs on slow filesystems. On timeout, the
resolver falls back to the lexical path (no symlink eval available);
this is logged at WARN so an audit trail exists for the
non-strict-mode run.

#### 2.6 — Audit events for `allowed_path` hits

When a file tool call lands inside a declared `allowed_paths` entry
(not via per-call approval, but via pre-declaration), the agent
publishes a new event on the event bus:

```
EventTypeAllowedPathHit
  workflow        string  // workflow file basename (or "" for non-workflow runs)
  tool            string  // tool name: write_file, read_file, edit_file, etc.
  path            string  // the user-supplied path
  resolved_path   string  // canonical target after symlink eval (may equal path)
  mode            string  // "read_only" | "read_write"
  matched_entry   string  // the declared allowed_path entry that matched
  source          string  // "workflow" | "step" | "session_approval"
```

The WebUI Automations panel can show a per-run counter
"allowed_paths used: N (of M declared)." The event also feeds the
agent's own debug log when `agent.debug` is true.

The event is published **after** the gate decision (so a denied
access doesn't fire the event — only successful accesses do). This
addresses gap #4.

### Non-goals

- **Not narrowing.** Phase 2 is additive only; a step cannot REMOVE
  an entry from the workflow-level allowlist. Narrowing requires a
  separate "step-level deny" capability, which is more error-prone
  and out of scope for this phase.
- **Not changing the mode enum.** Still `read_only` / `read_write`.
  No "list_only" or "follow_only" tier in Phase 2.
- **Not changing the dialog UX.** The workflow-launch confirmation
  dialog already shows the declared `allowed_paths`. The per-step
  additions show up in the dialog too, appended after the workflow-
  level set with a "step X" label. No new dialog option.
- **Not removing or refactoring the existing migration plan (M1–M4).**
  This phase is orthogonal; it ships on top of the already-shipped
  schema/validation/seeding work and can land in any order relative
  to the gate-consolidation migration.

### Risks

1. **cd validation ergonomics.** A workflow author who declared
   `/home/user/projects/sibling` may not have realized the agent
   would `cd` there directly. Rejecting `cd /etc` is the safe
   default, but rejecting `cd` to a path that's a *sibling* of the
   declared folder (and thus a misconfiguration, not an attack) is
   harder to differentiate. Mitigation: the rejection message lists
   the currently-allowed cd targets so the user can see the gap.

2. **Step-level union semantics.** A step that adds
   `{path: /opt/marketing, mode: read_only}` does not narrow away
   `read_write` access to that path if the workflow-level set
   already declared it as `read_write`. Last-write-wins on mode
   conflicts, with a loader warning, is the v1 contract. A future
   phase can introduce an explicit "mode_pin" field.

3. ~~Resolver scope change.~~ Tightening the resolver to scope on
   `effectiveCwd()` is a standard cross-cutting change. All
   `SafeResolvePathWithBypass` callers already read from
   `agent.effectiveCwd()`, so no caller updates are needed; only
   the resolver's internal scope check changes. Move to
   implementation notes; not a tracked risk.

4. **Symlink eval perf.** Two `filepath.EvalSymlinks` calls per
   file op (one in the dialog display, one in the strict resolver)
   doubles the symlink cost. Mitigation: cache the resolved target
   in the new `WithFilesystemBypassScope` ctx so a second call in
   the same turn is a hashmap lookup. Also see M4 — the cache
   lands there.

5. **Audit volume.** Workflows with hundreds of file ops fire
   `AllowedPathHit` events at the same cadence. The event bus
   already handles high-volume `tool_*` events, but a per-event
   rate-limit prevents floods from making the WebUI lag. The
   rate-limit policy is **deferred** to the implementation phase;
   see Open questions below.

### Acceptance criteria

- [ ] `updateShellCwd` rejects any target not under the workspace
  root or a declared `allowed_paths` entry. Test asserts each
  rejection path (non-allowed absolute, non-allowed relative,
  under-but-allowed, allowed-but-non-existent).
- [ ] `AgentWorkflowStep` and `AgentWorkflowInitial` accept
  `allowed_paths` in JSON. Loader validates each entry with the
  existing `AllowedPath.Validate()` rules.
- [ ] Workflow runner applies step-level `allowed_paths` before
  each step executes; the agent's session allowlist reflects the
  union at step entry.
- [ ] When `effectiveCwd()` is under a declared `allowed_paths`
  entry, file tool calls (relative or absolute) resolve to that
  scope without per-call approval prompts.
- [ ] Symlinks inside an allowed folder are resolved and re-checked
  against the scope; an escape symlink returns the off-workspace
  sentinel.
- [ ] `EventTypeAllowedPathHit` is published after a successful
  access that lands in an allowed_path entry. The event payload
  matches the schema above.
- [ ] All existing tests (`filesystem_gate_test.go`,
  `filesystem_gate_adapter_test.go`, `path_tier_*_test.go`,
  `approval_allowlist_test.go`, `tool_handlers_automate_test.go`)
  pass without modification, except tests for symbols removed
  per AC #2 (which are removed alongside them per the M3
  removal step).
- [ ] New tests cover each acceptance criterion, including the
  symlink-escape re-validation and the audit-event publication.

### File change list (estimate)

| File | Change |
|---|---|
| `pkg/workflow/types.go` | Add `AllowedPaths []AllowedPath` field on `AgentWorkflowInitial` and `AgentWorkflowStep` (parallel to the existing `AgentWorkflowConfig.AllowedPaths`). Update Validate. |
| `pkg/workflow/loader.go` | Validate step-level entries; emit "mode conflict" warning when the same path is declared at both levels with different modes. |
| `pkg/automate/discovery.go` | Add `AllowedPaths` to `InitialSummary` and `StepSummary`. Surface step-level paths in the summary. |
| `pkg/agent/agent_shell.go` | Add `IsCdTargetAllowed(target)`. Gate `updateShellCwd`. |
| `pkg/agent/agent_change_methods.go` | New `applyStepAllowedPaths(a, step)`. New `AddStepAllowedPaths` set for de-dup. |
| `pkg/agent/tool_handlers_automate.go` | Call `applyStepAllowedPaths` before each step executes; pass step-level paths through to the runner. |
| `pkg/agent/submanager_security.go` | New `WithFilesystemBypassScope(folder)` ctx setter; helper to scope a resolver call. |
| `pkg/filesystem/context.go` | Document the new scope semantics. |
| `pkg/filesystem/filesystem.go` | `SafeResolvePathWithBypass` reads `WithFilesystemBypassScope` and applies the symlink re-validation when set. |
| `pkg/events/events.go` | New `EventTypeAllowedPathHit` constant + `AllowedPathHitEvent` struct. |
| `pkg/agent/tool_security.go` | Publish `AllowedPathHit` after a successful file op that lands in an allowed entry. |
| `pkg/agent/tool_handlers_automate_test.go` | New tests for step-level `allowed_paths` apply. |
| `pkg/agent/agent_shell_test.go` | New tests for `IsCdTargetAllowed` + `updateShellCwd` gating. |
| `pkg/filesystem/filesystem_test.go` | New tests for symlink re-validation under `WithFilesystemBypassScope`. |
| `pkg/events/events_test.go` | New test for `EventTypeAllowedPathHit` payload shape. |

### Implementation summary

All Phase 2 sub-phases ship on `main`:

- **2.1 — cd-target validation** (`389a9d31`). `IsCdTargetAllowed(target)` checks workspace root and session-allowlisted folders via `filepath.Clean` + `isUnderPrefix`. `updateShellCwd` rejects non-allowed targets with a shell-output refusal message that lists the currently-allowed cd destinations. `cd -` validates the destination (previous cwd) too.
- **2.2 — Per-step `allowed_paths` schema** (`e5dc181e`). `AgentWorkflowInitial` and `AgentWorkflowStep` gain `AllowedPaths []AllowedPath`. Loader validates, normalizes, dedupes same-level duplicates (warn), and warns on workflow-vs-step / workflow-vs-initial mode conflicts (step wins). Discovery summary surfaces the entries in JSON.
- **2.3 — Step-aware apply** (`1e1e4bb9`). `ApplyWorkflowRuntimeAllowedPaths` snapshots the agent's allowlist, adds each path via `AddSessionAllowedFolder` + `SetSessionAllowedFolderMode`, returns the snapshot + added paths. `RestoreWorkflowRuntimeAllowedPaths` removes only paths in the added-set that weren't in the snapshot, restoring exactly the pre-step state. The runner wires apply+restore around both agent steps (defer in a function-scope closure with a named return) and shell steps (explicit apply+restore). `Initial.AllowedPaths` applies once at workflow start and persists for the entire run.
- **2.4 — Resolver scope** (`0a0a93e3`). `SafeResolvePathWithBypass` and `SafeResolvePathForWriteWithBypass` now also accept paths under the agent's effective cwd or any session-allowlisted folder. New context keys `WithAgentContext` / `WithEffectiveCwd` / `WithSessionAllowedFolders` plumb agent state through the tool-handler layer. The /tmp special case is preserved.
- **2.5 — Symlink re-validation** (`0a0a93e3`). `SafeResolvePathForWriteWithBypass` re-evaluates symlinks for existing target paths after parent resolution. The resolved target must also fall under an allowed root, or the write is rejected with a "symlink target outside allowlist" error. Multi-hop symlinks are handled via repeated `evalSymlinksWithTimeout`.
- **2.6 — Audit events** (`3396f0c3` + `dc167b03`). Every filesystem gate decision (allowed / denied / redirected) and every cd-gate denial emits one JSONL entry. Entries include tool, args, risk level, category, action, reasoning, source, session ID, and workspace root. The integration fix in `dc167b03` adds `LogJSON([]byte)` to `*tools.AuditLogger` to sidestep the import-cycle-induced type-identity problem that silently dropped filesystem entries in production.

Decisions resolved (vs. the open-questions list):
- **cd rejection = hard-block** (shell refuses, tracked cwd unchanged).
- **Subagent inheritance** = union of workflow + step's allowlist, propagated via `SnapshotSessionAllowedFolders`/`SnapshotSessionAllowedFolderModes` at subagent creation.
- **`AllowedPathHit` event** = deferred. The audit log carries the same info with simpler wire format. Adding a discriminated event type was more scope than necessary once the JSONL audit log was in place.

### Open questions

- Should the cd rejection be a hard-block (return error to the
  agent) or a soft-block (run the cd in a subshell that exits
  immediately)? My read: hard-block — the agent's tracked cwd
  doesn't change, the shell output reports the refusal, and the
  agent can reason about it. Soft-block via subshell would also
  work but complicates the semantic for `cd <allowed> && cat <off>`
  chains.
- Should step-level `allowed_paths` be inherited by subagents
  spawned during that step, or only the workflow-level set? My
  read: inherit the union (workflow + step) so the subagent has
  the same scope as its parent for that step.
- Should the `AllowedPathHit` event include the resolved mode
  (`read_only` / `read_write`) of the matched entry, or just the
  declared mode of the entry? My read: both — `mode` is the
  resolved mode, plus a `mode_match` field that is `"direct"` if
  the access matched the declared mode and `"upgraded"` if a
  `session_approval` previously widened a `read_only` entry to
  `read_write`. This gives the audit trail a paper trail for
  widening decisions.

## Related work

- This branch (`fix/file-tools-filesystem-gate`) — establishes the
  handler-side gate that this spec proposes to fold into Gate 1.
- SP-004 (Security, Validation & MCP) — the umbrella spec; this
  lands as a sub-section of its Gate architecture chapter.
- `pkg/agent/seed_tool_security.go:149` (`newPreExecuteHook`) and `seed_tool_security.go:208` (its `staticGateAutoApprove` call) — where the consolidation will land.- `roadmap/SP-128-compaction-drops-user-query.md` — unrelated to filesystem policy; fixes a rollup bug that drops the original user query and breaks strict-chat-template models (Qwen3.5).