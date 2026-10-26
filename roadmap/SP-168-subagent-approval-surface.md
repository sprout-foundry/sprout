# SP-168 — Subagent Approval Surface & Auto Risk Mode

Status: In progress (2026-10-25). Phase 1 (hang fix) + Phase 2 (auto LLM risk
mode). Split from a reported defect: a subagent that attempts a
security-gated `shell_command` sits until the approval timeout (30 min) or its
own run budget, instead of either prompting (answerable) or failing fast.

## Problem

A subagent that attempts a security-gated `shell_command` "just sits until it
times out." That is not acceptable. Desired behavior depends on the kind of
interaction:

- **Fully autonomous run** (`sprout automate`, one-shot `sprout agent "msg"`):
  fail fast — the agent tries a different path or stops. No multi-minute wait.
- **WebUI or interactive terminal**: prompt — and the prompt must be
  answerable.
- **Auto mode (new, default)**: a purpose-built LLM judges the real risk of a
  Medium/High `shell_command` and returns a risk-profile answer, with no human
  in the loop.

## Root cause (Phase 1 target)

Two independent defects compound into the hang:

1. **Manager identity mismatch.** `createSubagent`
   (`pkg/agent/subagent_creation.go`) builds the subagent with a **fresh**
   `AgentSecurityManager` → a **fresh** `security.ApprovalManager`. The WebUI
   only injects **its** manager into the **root** agent
   (`InjectWebUIManagers`, `pkg/agent/agent.go`). The shell handler's Gate 2
   re-gates the command via `env.ApprovalManager`
   (`pkg/agent_tools/shell_handler.go`), and for a subagent the adapter
   (`pkg/agent/tools_approval_adapter.go`) falls to "Surface 3: publish-and-wait,"
   which blocks up to `security.DefaultTimeout` (30 min). The request **is**
   published on the shared event bus (inherited `client_id`/`chat_id`), so the
   dialog renders in the parent's tab — but the pending channel lives in the
   **subagent's** manager. The WebUI response
   (`handleSecurityApprovalResponse` → `ws.GetSecurityPromptMgr()`,
   `pkg/webui/websocket_security_handlers.go`) and the CLI response both resolve
   against the **WebUI-owned / parent** manager, where that request id does not
   exist → "No pending security request." The subagent's wait is never
   satisfied → 30-min timeout → `timed_out` deny. The adapter comment
   "Subagents still wait: their requests are answered through the parent's
   interface" is false: the parent's interface answers a *different* manager.

2. **No fail-fast for a subagent with no answerable surface.** The adapter's
   fast-deny guard (`!a.agent.IsSubagent() && (!WebUIAttached() ||
   IsWorkflowRun())`) excludes subagents, so a subagent under an autonomous
   parent (workflow/automate run, or one-shot with no WebUI client) cannot take
   the fast-deny path and always falls to the 30-min wait. A subagent also does
   not inherit the parent's `workflowRun` flag, so `IsWorkflowRun()` is false
   inside the subagent even when the parent is an automate run.

## Decisions (user-confirmed)

- **Keep the existing autonomous default.** The broker's non-interactive
  permissive auto-approve (Gate 1, `approvals/broker.go`) is NOT flipped to a
  deny-by-default. Explicit opt-ins (`requires_approval: false`,
  `--risk-profile=permissive`, `--unsafe-shell`, unsafe/elevated) stay
  permissive.
- **Auto mode is `shell_command` only.** Filesystem-gated writes and git ops
  keep the static tier; the LLM judge is shell-shaped.
- **Auto mode is the default for all modes**, runs only on Medium/High
  (never Low, never hard-block/Critical), and must be fast enough to be the
  default (validated by timing tests). Hard blocks stay absolute — no LLM
  verdict overrides Critical.

## Design

### Phase 1 — answerable dialogs + fail-fast (the hang fix)

- **Share the parent's managers with the subagent.** In `createSubagent`, after
  constructing the subagent's `securityMgr`, copy the parent's
  `ApprovalManager` and `AskUserManager` into the subagent (when the parent
  has them). The subagent and the parent/WebUI then share one `pending` map, so
  a WebUI/CLI answer resolves the subagent's pending request. The dialog is
  answerable when the parent is interactive (WebUI client or TTY).
- **Fail fast when no one can answer.** In the adapter's `RequestApproval`, a
  subagent whose parent has no answerable surface (workflow run, or
  non-interactive with no WebUI client) returns an immediate result instead of
  waiting the full timeout — mirroring the parent's fast-deny
  (`noApprovalSurfaceReason`), so the subagent's LLM reads the reason and
  adapts. No 30-min wait in the autonomous case.
- **Propagate `workflowRun` to the subagent** so an automate run's subagents
  are non-interactive (fail-fast) rather than waiting.

Behavior matrix (shell_command, Medium/High, not hard-blocked):

| Parent mode            | Gate 1 (broker)      | Gate 2 (shell handler)                          |
|------------------------|----------------------|-------------------------------------------------|
| WebUI (client attached) | WebUI dialog (answerable) | answerable via shared manager; no hang        |
| Interactive TTY        | fail-safe deny / prompt | answerable via shared manager; no hang          |
| Autonomous (automate / one-shot, no surface) | permissive auto-approve (unchanged) | fail fast (immediate), no 30-min wait |

### Phase 2 — auto LLM risk mode (default)

- **Purpose-built verdict prompt** (the existing security-analyzer prompt
  `approvals/security_analyzer.go` is the starting shape) →
  `{risk: low|moderate|high, verdict: approve|reject|review, reason}`,
  per-subcommand for chains (already chain-aware).
- **Where it runs:** at the gate when the static classifier says Medium/High and
  it is not a hard block. Hard blocks / Critical stay absolute.
- **Model:** the cheap/worker model, not the session model — this is
  classification, not task reasoning.
- **Cost/bound:** one small call per *distinct* gated command; reuse
  `SecurityAnalysisCache` (command-hash keyed); a 2 s budget so it cannot stall
  a turn.
- **Verdict mapping:** `approve` → run; `reject` → immediate deny with the LLM
  reason (the subagent's LLM reads it and pivots); `review` → prompt if a
  surface exists, else immediate deny.
- **Fallback (analyzer error/timeout):** fall back to the interaction branch —
  interactive → prompt (answerable via Phase 1); autonomous → immediate deny.
  Never silently approve on analyzer failure.

## Phasing

1. **Phase 1** — share the parent's managers with the subagent; fail-fast when
   no answerable surface; propagate `workflowRun`. ~small; testable.
2. **Phase 2** — auto LLM verdict prompt + contract, gate-1/gate-2 wiring,
   config knob, cache reuse, timing tests.

## Compatibility

Security-gating behavior change: subagents under an interactive parent now
surface an answerable dialog (previously: unanswerable, 30-min wait). Subagents
under an autonomous parent now fail fast (previously: 30-min wait). No change
to the parent's own approval flow or to the autonomous permissive default.
