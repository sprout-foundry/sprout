# SP-123 — User Command Policies

**Status:** ✅ Shipped (Phases 1–3, 2026-07-16)  
**Created:** 2026-07-15  
**Effort:** Phase 1 (~2 days), Phase 2 (~2 days), Phase 3 (~1 day)

## Problem

Users have no unified way to say "allow everything, but always ask before git
push." The codebase already has **five separate config surfaces** for command
policy, but they're fragmented, inconsistently applied, and largely invisible
in the UI:

| Surface | Purpose | Exposed in UI? |
|---------|---------|----------------|
| `approved_shell_commands` | Literal allowlist | ✅ Settings > Security |
| `approved_shell_command_patterns` | Glob allowlist | ❌ Config only |
| `security_policy.denied_commands` | Workspace denylist | ⚠️ Workspace JSON only |
| `security_policy.rules` | Pattern rules (allow/deny/prompt) | ⚠️ Partial |
| `shell.user_safe/dangerous_patterns` | Classifier overrides | ❌ Config only |

**The gap:** There is no "always prompt" action that overrides the classifier
+ allowlist. A user in `permissive` mode can allowlist `git push origin main`,
but once allowlisted, they can't force it back to "always ask." The approval
dialog offers "Always approve" and "Elevate session" but not "Always ask for
this."

**Concrete example:** A user wants:
- All commands auto-approved (permissive profile)
- `git push*` → always prompt, every time
- `rm -rf /tmp/*` → always allowed
- `kubectl delete*` → never allowed, hard block

This is impossible today. `permissive` auto-approves `git push`. The allowlist
can't create a "prompt" entry. `security_policy.denied_commands` is checked too
late in the flow and doesn't support glob patterns.

## Goal

A unified command policy layer with three actions — **Always Allow**, **Always
Ask**, **Never Allow** — evaluated before the classifier and risk profile, with
glob pattern support, configurable from both the Settings UI and the approval
dialog.

## Non-Goals

- **Replacing the risk profile system** — profiles (readonly/cautious/default/
  permissive/unrestricted) remain the fallback when no command policy matches.
  Command policies are overrides on top of the profile.
- **Replacing `IsCriticalOperation`** — `rm -rf /`, fork bombs, mkfs, etc. are
  always hard-blocked regardless of any user policy. No config can override
  Critical-tier blocks.
- **Non-shell tools** — `git_push`, `create_pull_request`, `run_automate`, etc.
  go through their own approval flows and are intentionally out of scope.
  Command policies apply only to `shell_command` tool calls, consistent with
  the existing `approved_shell_commands` / `security_policy.rules` surfaces
  which are all shell-only.
- **Per-part policy for chained commands** — if `cmd1 && cmd2` is submitted,
  the policy matches against each subcommand independently (same as
  `classifyChainedCommand`), but the policy action applies to the whole
  command. Per-part "allow this part, ask for that part" is a future
  enhancement (SP-093 per-part approval already handles interactive
  per-command decisions).
- **Time-based or conditional rules** — no "only during business hours" or
  "only on branch main." Static pattern matching only.
- **Session-scoped rules** — Phase 1 supports only persisted (global/workspace)
  rules. Session-scoped policies (in-memory, cleared on restart) are a future
  enhancement.

## Design

### Config Model

New top-level config field, replacing the fragmented surfaces:

```go
// pkg/configuration/config_command_policy.go

// CommandPolicyAction determines what happens when a command matches a rule.
type CommandPolicyAction string

const (
    CommandPolicyAllow CommandPolicyAction = "allow"  // Auto-approve
    CommandPolicyAsk   CommandPolicyAction = "ask"    // Force prompt
    CommandPolicyDeny  CommandPolicyAction = "deny"   // Hard block
)

// CommandRule is a single user-defined command policy rule.
type CommandRule struct {
    Pattern string               `json:"pattern"`         // Glob: "git push*", "rm -rf *"
    Action  CommandPolicyAction  `json:"action"`          // allow, ask, deny
    Reason  string               `json:"reason,omitempty"`// Optional user note
}

// CommandPolicies is the top-level config structure.
type CommandPolicies struct {
    Rules []CommandRule `json:"rules"`
}
```

In `Config` (`pkg/configuration/config.go`):
```go
CommandPolicies *CommandPolicies `json:"command_policies,omitempty"`
```

### Evaluation Order

Command policies are checked **first** in `RequestApproval`
(`pkg/agent/approval_broker.go`), before the Low-risk early return, the existing
allowlist, unsafe-mode, and classifier checks. This is critical: the existing
code has a `RiskLevelLow` early-return at the top of `RequestApproval` that
bypasses all subsequent checks — policy evaluation must precede it or "deny"
and "ask" actions silently fail for safe commands.

```
0. Command policy match (shell_command only)? → Apply matched action (allow/ask/deny)
1. Low risk + no intent confirmation? → Auto-approve (existing early return)
2. Critical operation? → Hard block (never overridden)
3. Existing allowlist? → Auto-approve (if !skipAllowlist)
4. Risk profile + classifier → Normal flow
5. Approval broker → Interactive prompt if needed
```

The policy check is gated to `toolName == "shell_command"` only — non-shell
tools (`write_file`, `git_push`, `run_automate`, etc.) do not have a command
string and skip policy evaluation entirely.

**Matching algorithm** (`pkg/agent/command_policy.go`):

```go
// EvaluateCommandPolicy checks user-defined command policies against a
// shell command. Returns the matched action + matched rule, or empty
// if no rule matches.
func EvaluateCommandPolicy(command string, policies *configuration.CommandPolicies) (CommandPolicyAction, string)
```

- Splits chained commands on `&&`, `||`, `;`, `|` (reuses the same quote-aware
  splitting logic from `classifyChainedCommand`).
- For each subcommand, checks rules in order — **first match wins**.
- Returns the highest-severity action across all subcommands:
  `deny > ask > allow`. So if `echo hi && git push` matches "allow" for echo
  and "ask" for git push, the overall action is "ask."
- Pattern matching uses Go `path.Match` (glob) — same as
  `approved_shell_command_patterns` already uses. Case-insensitive.
  **Limitation:** `*` does not match `/` (path separator). So `kubectl delete*`
  matches `kubectl delete mypod` but NOT `kubectl delete deployment/nginx-app`.
  For paths with slashes, users must add multiple patterns or use `kubectl delete*`
  to match the prefix (since `path.Match` anchors the pattern at the end when
  there's no `*`, but `kubectl delete*` matches any suffix that doesn't contain
  `/`). This is documented and consistent with existing glob behavior. A future
  enhancement could add `**` for cross-slash matching.

### Action Semantics

| Action | Behavior | Overrides |
|--------|----------|-----------|
| **allow** | Auto-approve. Skip classifier, risk profile, and interactive prompt. | Everything except Critical ops |
| **ask** | Force interactive prompt. Skip allowlist and classifier auto-approve. Classifier risk is still computed for display. | Allowlist, classifier SAFE, risk profile LowOps |
| **deny** | Hard block. Return error immediately. | Everything (user explicitly forbids) |

**Critical operations are never overridden.** `rm -rf /` is blocked regardless
of any "allow" rule. This is enforced by the existing `IsCriticalOperation`
check that runs before policy evaluation.

### Integration Points

**`pkg/agent/approval_broker.go` — `RequestApproval`** (the central approval
funnel):

Insert policy evaluation as the **very first check** in the function, before
the Low-risk early return:

```go
// 0. NEW: Command policy (shell_command only)
if toolName == "shell_command" {
    if cmd, ok := args["command"].(string); ok && cmd != "" {
        if action, matchedPattern := EvaluateCommandPolicy(cmd, cfg.CommandPolicies); action != "" {
            switch action {
            case CommandPolicyDeny:
                return Denied, fmt.Errorf("blocked by policy: %s", matchedPattern)
            case CommandPolicyAllow:
                return Approved, nil  // surface: "command-policy"
            case CommandPolicyAsk:
                // Fall through to interactive prompt, skipping allowlist
                skipAllowlist = true
            }
        }
    }
}
// 1. Low risk → auto-approve (existing, unchanged but now after policy)
// 2. Critical → hard block (existing, unchanged)
// 3. Existing allowlist (if !skipAllowlist)
// 4. Existing risk profile + classifier flow
// 5. Existing interactive prompt
```

**Note on the legacy dual-gate path:** When `UnifiedRiskResolver` is `false`,
Medium-risk shell commands go through inline prompts in `tool_security.go` and
`seed_tool_registry.go` that do NOT call `RequestApproval`. Command policies
require `UnifiedRiskResolver: true` (the default since the flag shipped). The
legacy path's allowlist check in `seed_tool_registry.go:~800` should also be
patched to respect "deny" policies as a safety net, but full legacy-path
support is out of scope for Phase 1. The config loader should log a warning if
`command_policies` are set while `UnifiedRiskResolver: false`.

**Exporting the chain splitter:** `classifyChainedCommand` in
`pkg/agent_tools/security_classifier.go` is unexported. To reuse the quote-aware
splitting logic from `pkg/agent/command_policy.go`, export it as
`SplitChainedCommand(cmd string) []string` in `pkg/agent_tools/`. Both
`classifyChainedCommand` and `EvaluateCommandPolicy` call the exported version,
preventing drift between the two splitters.

**`pkg/agent/approval_allowlist.go`**:

The existing `approved_shell_commands` and `approved_shell_command_patterns`
continue to work — they're subsumed by `command_policies` with `action: "allow"`.
A migration helper converts old config to the new format on first load. The
old fields are not removed (backward compat), but the UI shows only the
unified policy panel.

**Precedence when both old and new config exist:** If a command matches both
an old `approved_shell_commands` entry (allow) and a new `command_policies`
rule (deny), the policy check runs first and wins. This is correct — the new
system takes precedence over the legacy allowlist.

**Relationship with `security_policy.denied_commands`:** The existing
`security_policy` workspace overlay promotes denied commands to Critical
(checked inside `ResolveToolRisk`), which runs before policy evaluation.
When both are set for the same command, both result in denial — no conflict.
The UI replaces the `security_policy.denied_commands` editor with the unified
policy panel to avoid confusion from two editors managing the same concept.
The `security_policy` JSON file (`.sprout/security-policy.json`) continues to
work as a workspace-level overlay for teams that version-control it.

**UI migration:** The existing `approved_shell_commands` editor in
`SecuritySettingsTab.tsx:26-48` is replaced by the new unified policy panel.
Migrated entries appear read-only in the "Always Allow" list until the user
edits them (at which point they become native `command_policies` rules).

### Settings UI

New subsection in Settings > Agent > Security (the `SecuritySettingsTab`):

**Three lists, side by side or stacked:**

```
┌─────────────────────────────────────────────────────────┐
│ Command Policies                                         │
│                                                          │
│ Rules are checked first-match-wins. Patterns are glob.   │
│ Critical operations (rm -rf /) are never overridable.    │
│                                                          │
│ ┌─ Always Allow ────┐ ┌─ Always Ask ─────┐ ┌─ Never Allow ──┐ │
│ │ rm -rf /tmp/*     │ │ git push*        │ │ kubectl delete* │ │
│ │ npm *             │ │ git push --force*│ │ rm -rf /etc/*   │ │
│ │ make *            │ │                  │ │                 │ │
│ │ [+ Add]           │ │ [+ Add]          │ │ [+ Add]         │ │
│ └───────────────────┘ └──────────────────┘ └───────────────┘ │
└──────────────────────────────────────────────────────────┘
```

- Each rule shows pattern + optional reason. Trash icon to remove.
- Add via text input with glob hint (`git push*` matches `git push origin main`).
- Drag-to-reorder within each list (rule order matters for first-match-wins).
- Existing `approved_shell_commands` entries are migrated into "Always Allow"
  automatically (read-only display until the user edits them).

### Approval Dialog Enhancement

The WebUI `SecurityApprovalDialog` and CLI `AskForApprovalWithOptions` gain a
new option when the user denies or approves a command:

**Current options:**
```
[y] Approve once    [n] Deny    [a] Always approve    [e] Elevate (session)
```

**New options:**
```
[y] Approve once    [n] Deny
[a] Always approve  [s] Always ask     ← NEW
[e] Elevate (session)
```

Adding "Always ask" cascades through the full approval decision pipeline:

**New CLI constant:** `ApprovalChoiceAlwaysAsk` in `pkg/utils/logger_approval.go`.

**Downstream type changes (all required for Phase 2):**

| File | Change |
|------|--------|
| `pkg/utils/logger_approval.go` | New `ApprovalChoiceAlwaysAsk` constant + `[s]` key in legacy prompt |
| `pkg/console/security_prompt.go` | New SelectList item for "Always ask" |
| `pkg/security/approval_manager.go` | New `ApprovalAlwaysAsk` constant in `ApprovalDecision` + `String()` + `ApprovalDecisionFromString()` — critical: without `FromString`, WebUI's `"always_ask"` action silently maps to `ApprovalDeny` |
| `pkg/agent/risk_prompt.go` | New case in `approvalDecisionFromCLIChoice` + `applyApprovalDecision` persists the rule |
| `webui/src/hooks/useSecurityApproval.ts` | `'always_ask'` added to `SecurityApprovalAction` union |
| `webui/src/components/SecurityApprovalDialog.tsx` | New button + event payload |

### Migration

On config load (`ConfigManager.Load`), if `command_policies` is nil but
`approved_shell_commands` or `approved_shell_command_patterns` exist, auto-
migrate them:

```go
func MigrateCommandPolicies(cfg *Config) {
    if cfg.CommandPolicies != nil { return }  // Already migrated
    var rules []CommandRule
    for _, cmd := range cfg.ApprovedShellCommands {
        rules = append(rules, CommandRule{Pattern: cmd, Action: CommandPolicyAllow})
    }
    for _, pattern := range cfg.ApprovedShellCommandPatterns {
        rules = append(rules, CommandRule{Pattern: pattern, Action: CommandPolicyAllow})
    }
    if len(rules) > 0 {
        cfg.CommandPolicies = &CommandPolicies{Rules: rules}
    }
}
```

Old fields stay in config for backward compatibility but are hidden from the UI
once migration occurs. No breaking change.

## Phases

### Phase 1: Core Policy Engine + CLI (Backend) — ~2 days

- [ ] **SP-123-1a:** Define `CommandPolicies`, `CommandRule`,
      `CommandPolicyAction` types in `pkg/configuration/config_command_policy.go`.
      Add `CommandPolicies` field to `Config` struct. Unit tests for
      serialization/deserialization.
      **Acceptance:** Types compile, JSON round-trip test passes.

- [ ] **SP-123-1b:** Export `SplitChainedCommand(cmd string) []string` from
      `pkg/agent_tools/security_classifier.go` (refactor existing
      `classifyChainedCommand` to call it). Then implement
      `EvaluateCommandPolicy` in `pkg/agent/command_policy.go` using the
      exported splitter. Handles glob matching via `path.Match`,
      first-match-wins, deny > ask > allow severity.
      Comprehensive unit tests (pattern matching, chained commands, edge cases).
      **Acceptance:** `go test ./pkg/agent_tools/...` and `go test ./pkg/agent/...`
      pass. Tests cover allow/ask/deny actions, glob patterns, chained commands,
      and no-match fallback.

- [ ] **SP-123-1c:** Wire into `RequestApproval` in
      `pkg/agent/approval_broker.go`. Insert policy evaluation as the **first
      check** in the function (before the Low-risk early return), gated to
      `toolName == "shell_command"` only. Add `skipAllowlist` flag for "ask"
      action. Log a warning if `command_policies` are set while
      `UnifiedRiskResolver: false`.
      **Acceptance:** Integration test: "allow" rule auto-approves a CAUTION
      command; "ask" rule forces prompt on a SAFE/Low-risk command; "deny"
      rule blocks a SAFE command. Critical operations still hard-block even
      with "allow" rule.

- [ ] **SP-123-1d:** Implement migration helper
      `MigrateCommandPolicies` in `pkg/configuration/config_command_policy.go`.
      Call from config load path. Test with fixtures containing old
      `approved_shell_commands` / `approved_shell_command_patterns`.
      **Acceptance:** Migration converts old fields to new rules. Config file
      with only old fields produces equivalent `CommandPolicies`. Config with
      `command_policies` already set is untouched.

### Phase 2: Settings UI + Approval Dialog — ~2 days

- [ ] **SP-123-2a:** Add command policy editor as a new
      `webui/src/components/settings/CommandPolicyEditor.tsx` component
      (not inline in SecuritySettingsTab — that file is already 374 lines
      and the editor would push it past the 500-line limit). Three lists
      (Always Allow / Always Ask / Never Allow) with add/remove/reorder.
      Reads/writes via `updateSetting('command_policies', ...)`. Replace
      the existing `approved_shell_commands` editor in SecuritySettingsTab
      with this component.
      **Acceptance:** E2E test: add a rule in each category, verify it
      persists across reload. Verify migrated `approved_shell_commands`
      appear in "Always Allow" list.

- [ ] **SP-123-2b:** Add "Always ask" to the full approval decision pipeline.
      New `ApprovalChoiceAlwaysAsk` constant in `logger_approval.go`,
      `ApprovalAlwaysAsk` in `approval_manager.go` (including
      `ApprovalDecisionFromString`!), new case in `risk_prompt.go`, new
      SelectList item in `security_prompt.go`.
      **Acceptance:** CLI test: prompt for a command, select "Always ask,"
      verify subsequent runs of matching command still prompt.

- [ ] **SP-123-2c:** Add "Always ask" button to WebUI approval dialog
      (`SecurityApprovalDialog.tsx`). New button in the 4-option shell mode
      (now 5-option). Sends `action: "always_ask"` in approval response.
      **Acceptance:** E2E test: approve dialog shows "Always ask" button. Click
      it, verify rule is persisted, verify subsequent matching commands still
      prompt.

### Phase 3: E2E Tests + Polish — ~1 day

- [ ] **SP-123-3a:** E2E test in `test/webui/settings.spec.ts`: configure a
      "deny" policy for `kubectl delete*`, verify a `kubectl delete` command
      is blocked. Configure an "ask" policy for `git push*`, verify it prompts
      even in permissive mode.
      **Acceptance:** Full round-trip: set policy in UI → run command in chat →
      verify behavior → reload → verify persistence.

- [ ] **SP-123-3b:** Settings E2E round-trip: add an "allow" rule for `echo *`,
      reload, verify it's still there. Remove it, reload, verify it's gone.
      **Acceptance:** CRUD lifecycle test passes.

- [ ] **SP-123-3c:** Verify migration: start with `approved_shell_commands`
      containing `["npm test"]`, open settings, verify it appears in "Always
      Allow" list. Edit it, verify old field is no longer consulted.
      **Acceptance:** Migration test passes.

## Key Files

| File | Role |
|------|------|
| `pkg/configuration/config_command_policy.go` | Types + migration (new) |
| `pkg/agent/command_policy.go` | `EvaluateCommandPolicy` engine (new) |
| `pkg/agent_tools/security_classifier.go` | Export `SplitChainedCommand` |
| `pkg/agent/approval_broker.go` | Integration into approval flow (first check) |
| `pkg/agent/approval_allowlist.go` | Existing allowlist (subsumed, not removed) |
| `pkg/agent/risk_prompt.go` | CLI "Always ask" persistence |
| `pkg/utils/logger_approval.go` | CLI prompt option |
| `pkg/console/security_prompt.go` | CLI SelectList item |
| `pkg/security/approval_manager.go` | `ApprovalDecision` constant + `FromString` |
| `pkg/agent/risk_assessment.go` | Risk assessment (unchanged — policy runs before) |
| `pkg/configuration/config.go` | `CommandPolicies` field on `Config` |
| `webui/src/components/settings/CommandPolicyEditor.tsx` | UI editor (new component) |
| `webui/src/components/settings/SecuritySettingsTab.tsx` | Wire editor, replace old approved-commands UI |
| `webui/src/components/SecurityApprovalDialog.tsx` | "Always ask" button |
| `webui/src/hooks/useSecurityApproval.ts` | Wire new action |

## Acceptance

1. A user in `permissive` mode can add `git push*` as "Always Ask" and every
   `git push` invocation prompts — even though the classifier says SAFE and the
   profile would auto-approve.
2. A user can add `kubectl delete*` as "Never Allow" and the agent returns an
   error immediately without prompting.
3. `rm -rf /tmp/*` as "Always Allow" auto-approves even though the classifier
   says DANGEROUS.
4. `rm -rf /` is blocked regardless of any "allow" rule (Critical tier).
5. Old `approved_shell_commands` entries migrate into "Always Allow"
   automatically.
6. All rules persist across reloads and are editable from Settings > Security.
7. The CLI approval prompt offers "Always ask" alongside "Always approve."
8. Chained commands are split — `echo hi && git push` prompts (git push matches
   "ask") even though echo is safe.
9. Policy decisions ("allow" / "deny") appear in the audit log with
   `Surface: "command-policy"` and the matched pattern.
10. If both `command_policies` (deny) and `approved_shell_commands` (allow) are
    set for the same command, the policy "deny" wins (checked first).
