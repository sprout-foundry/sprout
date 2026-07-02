# SP-049: Shell Permission Overhaul — User-Configurable Policy & Headless Hardening

**Status:** ✅ Implemented (Phases 3a–3d complete)

The original shell permission system was a binary "allow once / deny" gate with
no user-configurable policy and no headless-mode awareness. Sprout users wanted
to pre-approve categories of commands (`git *`, `npm test`, `go build`) without
approving each individually, and headless (daemonized) runs needed a different
default posture than interactive CLI/WebUI sessions. SP-049 introduced a
tiered allow-list model (`allow` / `ask` / `deny` per pattern), a
`~/.config/sprout/shell_policy.yaml` user file that overrides defaults, a
dedicated headless mode that defaults to stricter rules with explicit logging,
and the unified `checkShellApproval` resolver that the broker consults for
every command. Phases 3a-3d shipped the resolver, the broker integration, the
user-config file, and headless hardening.

## Key decisions

- **Three tiers, not two.** `allow` / `ask` / `deny` per pattern. The old
  binary model couldn't represent "always allow `git status` but ask for
  `git push`" without a per-command policy file, which users wouldn't write.
- **User policy overrides defaults, defaults override built-in safe-list.**
  Layered config so a user can lock down `rm *` without losing the rest of
  the defaults.
- **Headless mode defaults to `ask` for everything not explicitly allowed.**
  Interactive CLI/WebUI keeps `allow` for safe-list patterns. The reasoning
  is that an unattended agent shouldn't quietly execute commands a user
  never reviewed.
- **The approval broker is the single chokepoint** — every shell command
  goes through `checkShellApproval` regardless of which tool invoked it.
  This makes the audit log meaningful and prevents tool-specific bypasses.
- **Decision is sticky for the lifetime of the process.** Once a command is
  approved in a session, the same command doesn't re-prompt — but a new
  process re-evaluates against the policy file.

## Artifacts

- code: `pkg/security/permissions.go` — `checkShellApproval` resolver
- code: `pkg/agent/approval_broker.go` — broker that consults the resolver
- code: `pkg/security/policy_loader.go` — `shell_policy.yaml` loader
- config: `~/.config/sprout/shell_policy.yaml` — user override file
- tests: `pkg/security/permissions_test.go`
- companion: SP-068 (`sprout explain`) and SP-093 (CLI approval picker)

Full specification archived — see git history for original content.