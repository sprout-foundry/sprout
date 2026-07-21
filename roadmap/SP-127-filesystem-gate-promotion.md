# SP-127 — Promote Filesystem Gate to Gate-1 (Static Classifier)

**Status:** 🔵 Scoping — not yet approved for implementation
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

- [ ] `staticGateAutoApprove` runs `ClassifyPathAccess` for
  file-touching tools and routes through the same dialog as the
  current handler-side gate.
- [ ] `withFilesystemApproval`, `FilesystemGate` interface, and
  `filesystemGateAdapter` are removed (or repurposed for the
  subagent short-circuit).
- [ ] Every file-touching handler (`write_file`, `edit_file`,
  `read_file`, `list_directory`, `write_structured_file`,
  `patch_structured_file`, `handlePDF`) calls
  `SafeResolvePathWithBypass` directly with no wrapper.
- [ ] All existing tests (`filesystem_gate_test.go`,
  `filesystem_gate_adapter_test.go`, `path_tier_*_test.go`,
  `approval_allowlist_test.go`) pass without modification, except
  for callers that referenced the removed interface.
- [ ] New Gate-1 unit test asserts path-tier classification for each
  file-touching tool (External, Sensitive, in-workspace).
- [ ] Performance benchmark shows ≤10% regression for the common
  case (in-workspace, no approval flow). Cache the resolved verdict
  if needed.
- [ ] `docs/SECURITY.md` updated to describe the single-flow
  architecture.

## Migration plan

1. **Phase 1 — bring path-tier into Gate 1.** Extend
   `staticGateAutoApprove` to classify file paths. Keep
   `withFilesystemApproval` as a fallback for handlers that haven't
   been migrated yet. Both paths must agree on the decision; cross-
   check via a conformance test.
2. **Phase 2 — migrate handlers.** One commit per handler. After each
   commit, run the full test suite + the Gate-1 conformance test.
3. **Phase 3 — remove the old machinery.** Once all handlers
   migrated, delete `FilesystemGate`, `withFilesystemApproval`,
   `filesystemGateAdapter`, and the corresponding tests. Update
   `docs/SECURITY.md`.
4. **Phase 4 — performance hardening.** Add the resolved-verdict
   cache if the benchmark regresses. Document the cache in
   `pkg/filesystem/context.go`.

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

## Related work

- This branch (`fix/file-tools-filesystem-gate`) — establishes the
  handler-side gate that this spec proposes to fold into Gate 1.
- SP-004 (Security, Validation & MCP) — the umbrella spec; this
  lands as a sub-section of its Gate architecture chapter.
- `pkg/agent/tool_security.go:235` (`newPreExecuteHook`) — where the
  consolidation will land.