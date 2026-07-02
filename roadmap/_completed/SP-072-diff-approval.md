# SP-072: Per-Hunk Diff Approval — Optional Approve-Before-Apply for Agent Edits

**Status:** ✅ Implemented (2026-06-14; opt-in diff review with per-hunk accept/reject)

The agent applied file edits immediately with after-the-fact review only. This spec added an opt-in "diff approval" mode where `write_file`/`edit_file`/`patch_structured_file` compute a diff and route it to an approval broker before writing. Users can accept all, reject all, or select individual hunks. Only accepted hunks are written, and the tool result tells the model exactly what landed so it can react. Default is off — preserving the current apply-then-review flow.

## Key decisions

- Mode is opt-in with three settings: "off" (default), "all", "paths" (glob allowlist)
- Hunking via unified-diff splitter; partial apply patches only accepted hunks into original
- Tool result to the model is explicit about what was applied vs rejected
- Non-interactive runs (`--skip-prompt`, automate, daemon) treat the mode as approve-all
- Reuses existing approval-delivery plumbing from SP-049/SP-068 rather than inventing new transport

## Artifacts

- code: `pkg/agent/edit_approval.go` — proposal, hunking, partial apply, broker call
- code: `pkg/agent/edit_approval_test.go` — hunk split + partial apply + reject-all tests
- code: `webui/src/components/EditApprovalPanel.tsx` — diff review with per-hunk toggles
- code: `pkg/console/` — terminal diff + per-hunk prompt rendering
- code: `pkg/agent/tool_handlers_file.go` — route writes through approval when mode active

Full specification archived — see git history for original content.
