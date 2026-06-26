# SP-072: Per-Hunk Diff Approval — Optional Approve-Before-Apply for Agent Edits

**Status:** ✅ Implemented
**Date:** 2026-06-14
**Depends on:** Change Tracking (`pkg/agent/change_tracking*.go`), SP-049 (security/approval surfaces), SP-003 (WebUI)
**Priority:** Medium
**Effort Estimate:** ~4-5 days (3 phases)

## Problem

The agent applies file edits **immediately**, then the user reviews after the
fact (git panel, `list_changes`, `recover_file`). There is no
**approve-before-apply** path: no way to see the proposed diff and accept or
reject it — ideally hunk-by-hunk — *before* it touches the working tree.
Confirmed: no `acceptHunk`/`rejectHunk` anywhere in `webui/src/components/`.

`enable_pre_write_validation` exists but it's a *validation* gate (does the
write look sane), not an interactive *diff review* gate. Users coming from
Cursor's per-hunk accept/reject, or anyone doing high-stakes edits, want to
keep a hand on the wheel: see exactly what will change and veto parts of it
before it lands.

The strong change-tracking/undo story (revert after the fact) covers the
"oops" case but not the "let me look before you write" preference — which is a
different, opt-in workflow.

## Current State

| Capability | Where | Status |
|---|---|---|
| Apply edits then track | `write_file`/`edit_file` → `TrackFileWrite`/`TrackFileEdit` | ✅ |
| Review after apply | git panel, `list_changes` | ✅ |
| Revert after apply | `recover_file`, `rollback_changes` | ✅ |
| Pre-write validation gate | `enable_pre_write_validation` | ✅ (sanity, not review) |
| **Approve diff before apply** | — | ❌ missing |
| **Per-hunk accept/reject** | — | ❌ missing |

## Proposed Solution

An **opt-in** "diff approval" mode. When enabled, `write_file` / `edit_file` /
`patch_structured_file` compute the diff and route it to an approval broker
**before** writing. The user accepts all, rejects all, or selects hunks; only
accepted hunks are written, and the tool result tells the model exactly what
was applied vs. rejected so it can react.

### Phase 1: Backend gate + partial apply

**New file:** `pkg/agent/edit_approval.go`

```go
type EditProposal struct {
    Path     string
    Original string
    Proposed string
    Hunks    []Hunk   // unified-diff hunks with stable IDs
}

type EditDecision struct {
    Approved     bool
    AcceptedHunks []string // hunk IDs; empty + Approved=false => reject all
}

// RequestEditApproval is invoked by the file-write tools when diff-approval
// mode is active. It builds the proposal, asks the broker (CLI or WebUI),
// applies only accepted hunks, and returns a summary for the tool result.
func (a *Agent) RequestEditApproval(ctx context.Context, p EditProposal) (applied string, summary string, err error)
```

- Hunking via a unified-diff splitter over `Original`→`Proposed`.
- Partial apply = original with only accepted hunks patched in.
- The tool result returned to the model is explicit:
  `"applied 2/3 hunks to foo.go; rejected hunk #2 (lines 40-55). The file now
  contains …"` — so the model doesn't assume its full edit landed.
- Reuses the existing approval-delivery plumbing (timeout, webui↔CLI fallback)
  from SP-049/SP-068 rather than inventing a new transport.

### Phase 2: CLI review UI

- When diff-approval mode is on, a write tool renders a colored unified diff in
  the terminal with a per-hunk prompt: `[a]ccept all / [r]eject all /
  [s]elect hunks / [e]dit`. Selecting hunks toggles them; `edit` opens
  `$EDITOR` on the proposed content.
- Built on `pkg/console` rendering + the `select_list`/prompt primitives
  (SP-057). Honors `--skip-prompt` (mode is a no-op in non-interactive runs —
  approve-all — since there's no human to ask).

### Phase 3: WebUI review UI

- A diff-approval panel: when an edit is pending, show the unified diff with
  per-hunk Accept/Reject toggles, "Apply selected" / "Reject all" actions
  (`webui/src/components/EditApprovalPanel.tsx`). Wire into the chat/tool event
  stream so a pending edit blocks visibly (and triggers an `input_required`
  notification — SP-070).
- Endpoint `POST /api/edits/{id}/decision`.

### Configuration & scoping

```go
type EditApprovalConfig struct {
    Mode string `json:"mode,omitempty"` // "off" (default) | "all" | "paths"
    Paths []string `json:"paths,omitempty"` // glob allowlist requiring approval when Mode="paths"
}
```

- Default **off** — the current apply-then-review flow is unchanged for users
  who like it.
- `"paths"` mode lets a user require approval only for sensitive globs (e.g.
  `**/*.tf`, `migrations/**`) while letting routine edits flow.

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent/edit_approval.go` | **New** — proposal, hunking, partial apply, broker call |
| `pkg/agent/edit_approval_test.go` | **New** — hunk split + partial apply + reject-all tests |
| `pkg/agent/tool_handlers_file.go` | Modify — route writes through approval when mode active |
| `pkg/configuration/config.go` | Modify — add `EditApprovalConfig` |
| `pkg/console/` | Modify — terminal diff + per-hunk prompt |
| `webui/src/components/EditApprovalPanel.tsx` | **New** — diff review w/ per-hunk toggles |
| `pkg/webui/` | Modify — `POST /api/edits/{id}/decision` |

## Success Criteria

- With `mode: "all"`, every agent file write shows a diff and applies only
  accepted hunks; the model's tool result reflects exactly what landed.
- `mode: "paths"` gates only matching globs; other edits apply directly.
- `mode: "off"` (default) preserves today's behavior with zero overhead.
- Non-interactive runs (`--skip-prompt`, automate, daemon) treat the mode as
  approve-all (no silent hangs).
- Rejected hunks never touch the working tree.

## Out of Scope

- Editing hunks inline in the WebUI diff (accept/reject only for v1; `$EDITOR`
  covers free-form editing on the CLI).
- Approval for shell-driven mutations (`sed -i`, formatters) — those are
  captured by Change Tracking for *after-the-fact* revert; pre-approval there
  is a much larger surface and stays out of scope.
- Replacing `enable_pre_write_validation` — the two are complementary
  (validation = "is this sane", approval = "do I want this").

## Open Questions

1. Hunk granularity: standard unified-diff hunks, or finer line-level
   selection? Recommendation: diff hunks for v1.
2. Should diff-approval auto-enable for DANGEROUS-classified writes even when
   `mode: "off"`? Possibly — ties into SP-068's unified resolver.
