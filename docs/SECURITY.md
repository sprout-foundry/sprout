# Security Architecture

This document describes Sprout's security model, trust boundaries, and data handling practices.

## Trust Boundaries

> **See also:** [Persona System](PERSONAS.md) for the two-gate risk model and depth-based delegation that enforces trust boundaries at the subagent level.

### Local-First Execution

Sprout runs entirely on the user's machine. The daemon process executes under the installing user's UID with no root privileges. All file operations, command execution, and tool invocations happen within the user's security context.

### Network Trust

| Boundary | Default | Override |
|---|---|---|
| Web UI bind address | `127.0.0.1` (localhost only) | `--bind` flag / `SPROUT_BIND_ADDR` |
| Non-localhost auth | Required (`SPROUT_AUTH_TOKEN`) | Refuses to start without it |
| Provider API calls | Outbound TLS only | N/A |
| LLM provider | User-selected API endpoint | Configured per provider |

### Security Classification

Sprout uses a three-tier security classifier for tool execution (see `pkg/agent_tools/security_classifier.go`):

- **Safe**: Automatic execution (read operations, file creation in workspace)
- **Caution**: Prompts user with preview and reasoning
- **Dangerous**: Blocked by default (system-level operations, files outside workspace)

**Limitations**: The classifier uses heuristic pattern matching. It cannot guarantee perfect classification. Users should review caution-classified operations before approving.

## Risk Profiles

When a tool wants to run a shell command (or git operation), the result of the static classifier above is one input to a second gate: the **risk cascade**. The cascade decides between four outcomes per command:

| Tier | Outcome |
|---|---|
| **Low** | Auto-approve, run immediately. |
| **Medium** | Auto-approve. The persona's system prompt is expected to reason about whether it's a good idea. |
| **High** | If interactive → prompt the user (`y/N`). If non-interactive → reject. If the agent is a subagent → auto-approve (the root accepted responsibility when it spawned the work). |
| **Critical** | Always reject. No persona, profile, or interactive prompt can override. Reserved for catastrophic patterns: `rm -rf /` (and variants targeting `/`, `/*`, `~`, `$HOME`), fork bombs (`:(){:|:&};:`). |

Which tier a command lands in depends on the **active risk profile**. Five profiles ship out of the box:

| Profile | What it allows | When to use |
|---|---|---|
| `readonly` | Only `git status`/`log`/`diff` and `read_file`. **Every** write, edit, shell command, or git mutation → Critical (blocked, no prompt). | Audits, code review, untrusted agents. The agent literally cannot mutate anything. |
| `cautious` | Reads auto-approve. Everything else → High (prompts user). | Sensitive workspaces; you want a chance to review each action. |
| `default` | Reads + common edits/commits auto-approve. Destructive ops (force flags, `rm -rf`, lossy git) → High. **Backward-compatible with pre-SP-058 behavior.** | Daily driver. |
| `permissive` | Almost everything auto-approves; only force-flagged or recursive-destructive patterns → High. | High-trust agents in throwaway / recoverable workspaces. |
| `unrestricted` | Nothing prompts. Only the Critical tier blocks. | Sandboxed runs, ephemeral containers, "I know what I'm doing". |

### Selecting a profile

In order of precedence (highest wins):

1. **Workflow JSON `risk_profile` field** — per step, in [`docs/AGENT_WORKFLOW.md`](AGENT_WORKFLOW.md):
   ```json
   { "name": "deploy", "prompt": "...", "risk_profile": "cautious" }
   ```
2. **CLI `--risk-profile` flag** — per session:
   ```bash
   sprout agent --risk-profile=permissive "implement feature X"
   ```
3. **Config `risk_profile` field** — your persistent default, in `~/.config/sprout/config.json`:
   ```json
   { "risk_profile": "default" }
   ```
4. **Built-in default** — `default` when nothing is set.

A persona that defines its own `AutoApproveRules` (today: only `executive_assistant`) always wins over the profile. That's how EA keeps its tighter cascade independent of what profile you select.

> **Gotcha**: this means `sprout agent --persona executive_assistant --risk-profile=readonly` does NOT make EA readonly — EA uses its own rules. If you want to lock EA down to readonly, edit `~/.config/sprout/config.json` and override EA's persona rules under `subagent_types.executive_assistant.auto_approve_rules`. The `/status` slash command shows the active risk profile so you can verify which set is in effect.

### Approval dialog: Approve / Deny / Always / Elevate

When a shell command trips the high-risk cascade and there's an interactive
surface (CLI prompt or WebUI tab), you get four options instead of yes/no:

| Choice | Effect | Persistence |
|---|---|---|
| **Approve once** | Runs this single invocation. | None — next run prompts again. |
| **Deny** | Rejects with a security error. | None. |
| **Always approve** | Runs and adds this exact command to `config.approved_shell_commands`. Future runs (across restarts) skip the prompt for the literal string. | Persistent. Edit `config.json` to revoke. |
| **Elevate (session)** | Runs and bumps the session risk profile to `permissive`. No more high-risk prompts until restart. | Session only. Use `/risk-profile permissive` if you want this to survive restart. |

The allowlist is **literal-match** — allowlisting `rm -rf /tmp/build` does
NOT cover `rm -rf /tmp/build-cache` or anything else. The Critical tier
(`rm -rf /`, fork bombs) blocks regardless of allowlist or profile.

### Behavior change for non-EA personas (SP-058)

Before SP-058, the high-risk persona cascade only fired for personas that
declared `AutoApproveRules` (in practice: EA). Other personas — including
`orchestrator` and unset/default sessions — had no second gate, so a
high-risk command that cleared the static security classifier ran without
prompting.

With SP-058, every session runs under a profile (default = `default`),
which means non-EA personas now get prompted (interactive) or rejected
(non-interactive) on high-risk shell operations like `rm_recursive` and
`force_flag`. If you used to rely on the previous ungated behavior for
non-interactive automation, switch the session to `--risk-profile=permissive`
or define a custom profile that allows the categories you need. The CLI
flag and the `/risk-profile` slash command set the same field; see
[CLI_REFERENCE.md](CLI_REFERENCE.md) for the matrix.

### EA & subagent delegation

The Executive Assistant persona, when running **as the root agent**, follows the profile like anyone else: high-risk commands prompt you interactively, get rejected non-interactively, and Critical is blocked.

What EA — and any other root persona — *does* control is its **subagents**. When EA (or `orchestrator`, or any root) spawns a subagent and the subagent hits a high-risk gate, that gate auto-approves at the subagent. The reasoning: the root agent accepted responsibility when it spawned the work, and routing each subagent prompt back through the user would break autonomous orchestration. The Critical tier still blocks at every depth, so a subagent still can't `rm -rf /` no matter who spawned it.

Practical effect: when you use EA in queue mode, you set the profile once at startup and EA's subagents run within those rules without further prompts.

### Customizing profiles

You can override any profile's rules — including the five built-ins — by adding a `risk_profiles` block to `~/.config/sprout/config.json`:

```json
{
  "risk_profile": "default",
  "risk_profiles": {
    "default": {
      "low_risk":  ["git_add", "git_status", "git_log", "git_diff", "read_file"],
      "medium_risk": ["git_commit", "write_file", "edit_file", "shell_command"],
      "high_risk_never": [
        "force_flag", "rm_recursive", "git_reset_hard",
        "git_clean", "docker_prune", "git_push_force",
        "git_checkout", "git_switch", "git_restore", "git_branch_delete"
      ],
      "default_risk": "medium"
    },
    "my_strict": {
      "low_risk":  ["read_file", "git_status", "git_diff"],
      "high_risk_never": ["rm_recursive", "force_flag"],
      "default_risk": "high"
    }
  }
}
```

Key fields per profile:

| Field | Effect |
|---|---|
| `low_risk` | Operation categories that auto-approve. Matched against the output of `categorizeCommand` (see `pkg/configuration/config.go`): `git_status`, `git_log`, `git_diff`, `git_add`, `git_commit`, `git_push`, `git_pull`, `git_fetch`, `read_file`, `write_file`, `edit_file`, `shell_command`, `rm_command`, `docker`, `subagent_spawn`. |
| `medium_risk` | Operation categories that auto-approve but the persona's system prompt is expected to weigh them. |
| `high_risk_never` | **Named patterns** (not categories) that always gate. Available patterns: `force_flag`, `rm_recursive`, `git_reset_hard`, `git_clean`, `docker_prune`, `git_push_force`, `git_checkout`, `git_switch`, `git_restore`, `git_branch_delete`. |
| `default_risk` | Tier for unrecognized operations. One of `low`, `medium`, `high`, `critical`. Empty defaults to `medium` (backward-compat). Set to `critical` to make the profile truly readonly. |

The override **replaces** the built-in rules for that name — it's not a merge — so list every category you want allowed. You can also define entirely new profile names (e.g. `my_strict` above) and select them via `--risk-profile=my_strict` or the workflow JSON.

### What the Critical tier catches

The Critical tier is hard-coded in `pkg/configuration/config.go:IsCriticalOperation` and is **not** profile-overridable. Currently:

- `rm -rf` targeting `/`, `/*`, `~`, `~/`, `$HOME`, `${HOME}`.
- Fork-bomb pattern `:(){:|:&};:` (the literal `:()` shell-function-named-colon).
- Matching is tokenized: a path that happens to *contain* one of these patterns (e.g. `rm -rf /tmp/sprout-foundry/` — has `rm` and `-rf` but targets `/tmp/...`, not `/`) is NOT Critical. It still routes to the cascade normally.

## Filesystem Access Tiers

When a tool (or the browser file editor) tries to read or write a path
outside the workspace, sprout classifies it into one of three tiers
and picks the approval flow accordingly.

| Tier | Examples | Approval flow |
|---|---|---|
| **Workspace** | Anything under the agent's workspace root (or `~/.sprout` config dir). | No approval. |
| **External** | An external folder that isn't a system dir, and isn't a home path while CWD is outside `$HOME`. E.g. `/tmp/scratch`, `/srv/shared`, a sibling project under your home when CWD is also in home. | Prompt with 3 options: **Allow once** / **Allow folder this session** / **Deny**. Picking the folder option adds the path's parent directory to the agent's in-memory session allowlist — future accesses under it auto-approve. |
| **Sensitive** | System dirs (`/etc`, `/usr`, `/var`, `/Library`, `C:\Windows`, …). OR a home-directory path while CWD is outside `$HOME` (e.g. agent in `/tmp/sandbox` tries to read `~/.ssh/id_rsa`). | Prompt with 2 options: **Allow once** / **Deny**. The folder-allowlist choice is suppressed; every access prompts. |

### Notes

- The session allowlist is **per-agent, in-memory only**. It does NOT
  persist across restarts. Folders are stored after `filepath.Clean()`
  and matched component-aware (so `/etcfoo` is not under `/etc`).
- The allowlist is the **single source of truth** for both the agent's
  own filesystem tools (`read_file`, `write_file`, etc.) AND the WebUI
  file API (editor file opens). One approval covers both surfaces.
- Subagents inherit a **snapshot** of the parent's allowlist at spawn
  time. Subagent-side additions don't leak back to the parent
  (intentional — temporary approvals inside a delegated task
  shouldn't outlive the delegation).
- Critical-tier operations (rm -rf /, fork bombs) still block at every
  tier regardless of allowlist or risk profile.

### Why this design

The previous flow had two bugs:
1. A **global** session-bypass flag — approving one external path
   (`/tmp/scratch/foo.txt`) silently auto-approved every subsequent
   external path for the rest of the session, including `/etc/passwd`
   and `~/.ssh/id_rsa`. The per-folder allowlist replaces it.
2. **Two parallel approval systems** that didn't share state — the
   WebUI file API used a 2-minute token TTL; the agent's filesystem
   tools used the global bypass. Approving in one didn't carry over
   to the other. The unified allowlist fixes that too.

## Data Handling

### Files on Disk

| Path | Contents | Sensitivity |
|---|---|---|
| `~/.sprout/config.json` | Provider settings, preferences | Low |
| `~/.sprout/api_keys.json` | API keys (encrypted at rest via OS keyring when available) | **High** |
| `~/.sprout/service.env` | Environment variables for daemon | **High** |
| `~/.sprout/logs/` | Daemon logs, rotated (10MB, 5 backups) | Medium |
| `~/.sprout/memories/` | Agent memory .md files | Medium |
| `.sprout/workspace.log` | Per-workspace run log | Medium |
| `.sprout/history/` | Change tracker revisions | Medium |
| `.sprout/embeddings/` | Conversation turn embeddings | Medium |

### Credential Redaction

All HTTP request payloads logged to disk are passed through `pkg/redact` which replaces recognised secret patterns (AWS keys, GitHub tokens, API keys, private keys, env-style secrets) with `[REDACTED:<kind>]` tokens. See the redaction package for the full list of patterns.

### Memory Files

Agent memory files (`.sprout/memories/*.md`) are redacted via `pkg/redact` before being written to disk.

## Clearing Persisted Data

### Run logs
```bash
rm -rf .sprout/workspace.log
```

### Embeddings
```bash
rm -rf .sprout/embeddings/
```

### Change history
```bash
rm -rf .sprout/history/
```

### Agent memories
```bash
rm -rf .sprout/memories/
```

### Full reset
```bash
# Per-workspace
rm -rf .sprout/

# Global config and keys
rm -rf ~/.sprout/
```

## Skill Allowlist

Project-local skills (`.sprout/skills/`) are discovered automatically. To control which skills are active:

1. Create `.sprout/allowed_skills` with one skill ID per line
2. Skills not in the allowlist are loaded as disabled
3. Use `--no-project-skills` flag to skip skill discovery entirely

## Authentication for Remote Access

When binding to a non-localhost address:

```bash
export SPROUT_AUTH_TOKEN="your-secret-token"
export SPROUT_BIND_ADDR="0.0.0.0"
sprout agent -d
```

All write endpoints require `Authorization: Bearer <token>` when auth is configured. Read-only endpoints (static assets, health) remain open.
