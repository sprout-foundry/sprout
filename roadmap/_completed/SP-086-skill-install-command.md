# SP-086: Skill Install — Pull Skills from Git, URLs, and Registries

**Status:** ✅ Implemented (2026-06-30)
**Date:** 2026-06-27
**Depends on:** none (the skill discovery layer at `pkg/configuration/config_skills.go::discoverUserSkills` already handles local filesystem skills; this just adds a *fetch* step)
**Priority:** Medium-High (real UX gap; current flow forces users to manually copy SKILL.md files)
**Effort Estimate:** ~1–2 days

## Problem

Today, installing a skill requires the user to manually create `~/.config/sprout/skills/<id>/SKILL.md` with valid frontmatter. From `pkg/configuration/config_skills.go::discoverUserSkills`:

```go
// discoverUserSkills scans the ~/.config/sprout/skills/ directory for user-level skills
// and adds them to the config. This allows users to create custom skills that
// are available across all projects.
```

This is fine for users who write their own skills. It's a problem when a user wants to install a skill from:
- A teammate's git repo (`git@github.com:acme/sprout-skills.git`)
- A URL to a single SKILL.md (`https://gist.github.com/.../SKILL.md`)
- A shared registry (e.g., a `sprout-skills` collection at `sprout.dev/skills/`)
- A skill someone pasted into a chat message

For each of these, the user has to manually clone/copy/paste the file into the right directory and verify the frontmatter is correct. That's friction.

## Goals

1. CLI: `sprout skill install <source>` accepts git URLs, plain URLs (single SKILL.md), local paths, and registry IDs.
2. CLI: `sprout skill list --source user` shows installed user-level skills with their origin.
3. CLI: `sprout skill update [id]` refreshes installed skills from their source.
4. CLI: `sprout skill remove <id>` cleanly uninstalls.
5. WebUI: Settings → Skills panel gains "Install from URL" input + a list of installed skills with remove/update buttons.
6. Installation validates the SKILL.md frontmatter (`name`, `description` required) before committing — refuse to install malformed skills.
7. Installation records origin metadata so `update` knows where to re-fetch from.

## Design

### Source formats

```
# Local path (single file or directory)
sprout skill install ./my-skill/
sprout skill install ./SKILL.md

# Git URL (cloned into ~/.config/sprout/skills/<id>/)
sprout skill install https://github.com/acme/sprout-skills.git
sprout skill install git@github.com:acme/sprout-skills.git

# Plain URL to a single SKILL.md
sprout skill install https://example.com/skills/security-review/SKILL.md

# Registry shorthand (resolves via the sprout skill registry)
sprout skill install security-review       # looks up in the registry
sprout skill install @acme/security-review # scoped namespace
```

### Skill ID resolution

For each source, derive an `id`:
- Local path: directory basename
- Git URL: last path component (e.g., `sprout-skills` for `https://github.com/acme/sprout-skills.git`)
- Plain URL: parent directory name (e.g., `security-review` for `https://example.com/skills/security-review/SKILL.md`)
- Registry shorthand: the shorthand itself

If a skill with the same ID already exists, prompt the user to confirm overwrite (or `--force` to skip).

### Registry

For the registry shorthand, the simplest implementation is a **static registry file** at `pkg/skills/library/registry.json` checked into the repo:

```json
{
  "version": 1,
  "skills": {
    "security-review": {
      "source": "https://github.com/sprout-dev/curated-skills.git",
      "subdir": "security-review",
      "description": "Security review checklist for code changes"
    },
    "release-notes": {
      "source": "https://github.com/sprout-dev/curated-skills.git",
      "subdir": "release-notes",
      "description": "Generate user-facing release notes from commits"
    }
  }
}
```

The registry is updated via PR (same as provider-catalog). The list grows organically.

### Implementation

- `pkg/skills/install.go` (new) — core install logic. Pure functions: `InstallFromGit(url, id)`, `InstallFromURL(url, id)`, `InstallFromRegistry(shorthand, registry)`, `Uninstall(id)`, `Update(id)`. Each returns the new `SkillInfo` or an error.
- `pkg/skills/registry.go` (new) — load `registry.json` (embedded via `//go:embed`), resolve shorthands.
- `pkg/agent_commands/skill.go` (extend existing) — add `install`, `update`, `remove` subcommands. The existing `skill list` and `skill show` already exist; extend rather than replace.
- `pkg/webui/skills_api.go` (new) — HTTP handler for install/update/remove/list.
- WebUI: extend the existing `CredentialsSettingsTab`-style settings tabs with a new `SkillsSettingsTab`.

### Origin metadata

On install, write `~/.config/sprout/skills/<id>/.sprout-origin.json`:

```json
{
  "id": "security-review",
  "source": "https://github.com/sprout-dev/curated-skills.git",
  "type": "git",
  "installed_at": "2026-06-27T10:00:00-05:00",
  "ref": "main",
  "commit": "abc123"
}
```

For non-git sources, `type` is `"url"` or `"path"` and `commit` is omitted. `update` reads this file to know where to re-fetch from.

### Tests

- `pkg/skills/install_test.go`: each install path (local, git, URL, registry) creates a valid SKILL.md in the right location; malformed frontmatter is rejected; overwrite prompt logic; uninstall removes both SKILL.md and origin metadata.
- `pkg/skills/registry_test.go`: shorthand resolution; unknown shorthand error; registry file parse.
- `pkg/agent_commands/skill_install_test.go`: CLI flag parsing; `--force` skips overwrite prompt.
- `pkg/webui/skills_api_test.go`: install/update/remove endpoints; auth + ownership (only allow user-level, not project).
- `webui/src/components/SkillsSettingsTab.test.tsx`: install from URL input, list of installed skills, remove button confirmation.

### Phase plan

| Phase | Scope |
|-------|-------|
| 1 | `pkg/skills/install.go` core (install/update/uninstall + origin metadata). |
| 2 | `pkg/skills/registry.go` + seeded `registry.json` with 3-5 starter skills. |
| 3 | `pkg/agent_commands/skill.go` subcommand wiring. |
| 4 | `pkg/webui/skills_api.go` + WebUI `SkillsSettingsTab`. |

## Success Criteria

- `sprout skill install https://github.com/sprout-dev/curated-skills.git` clones the repo, finds SKILL.md files, and registers them with the sprout config.
- `sprout skill list --source user` shows installed user-level skills with their origin URL.
- `sprout skill update security-review` pulls the latest version from the recorded origin.
- `sprout skill remove security-review` cleans up both the SKILL.md and the origin metadata.
- Malformed SKILL.md (missing `name` or `description`) is rejected with a clear error.
- WebUI Skills tab shows installed skills with remove buttons; "Install from URL" input accepts git URLs, plain URLs, and registry shorthands.
- All tests green; `make build-all` clean.

## Risks

- **Git clone failures** (network, auth, missing repo) need graceful error messages. Mitigation: distinct error types (`ErrSourceUnreachable`, `ErrSourceAuth`, `ErrSourceMalformed`); the CLI formats them as user-facing messages.
- **Supply-chain risk** — installing a skill executes its instructions in the agent context. A malicious SKILL.md could exfiltrate data, install backdoors, etc. Mitigation: this is an inherent trust model (the user is opting in to install code-shaped instructions); document it clearly in the help text and the WebUI install dialog. Consider a `--trust=<author>` annotation for v2.
- **Registry as a single point of failure** — if `registry.json` is unreachable, registry shorthands don't work. Mitigation: the registry is embedded in the binary (`//go:embed`); a user can install from URL/git/path directly without it. The registry is a convenience, not a requirement.

## Open Questions

1. Should registry shorthands support **version pinning** (e.g., `security-review@1.2.0`)? **Recommendation:** yes for git sources (read the tag); no for plain URL sources. Adds minor complexity but is a common ask.
2. Should there be a **built-in skill pack** that ships with sprout (similar to how IDEs bundle common extensions)? **Recommendation:** keep the registry separate from built-ins. Built-ins live at `pkg/skills/library/<id>/SKILL.md` (current pattern); registry skills live in `~/.config/sprout/skills/`. The user opts into registry installs explicitly.