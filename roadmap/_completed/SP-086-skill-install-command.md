# SP-086: Skill Install — Pull Skills from Git, URLs, and Registries

**Status:** ✅ Implemented (2026-06-30; sprout skill install, update, remove + WebUI settings tab)

Installing a skill required manually creating `~/.config/sprout/skills/<id>/SKILL.md` with valid frontmatter. This spec added `sprout skill install <source>` accepting git URLs, plain URLs, local paths, and registry shorthands. Installation validates frontmatter (name + description required) and records origin metadata in `.sprout-origin.json` for later `sprout skill update`. A static registry file (`registry.json`) ships embedded in the binary for shorthand resolution. The WebUI Settings → Skills panel provides install-from-URL input and installed skill management.

## Key decisions

- Skill ID derived from source: directory basename (local), last path component (git), parent dir (URL), shorthand itself (registry)
- Origin metadata written to `~/.config/sprout/skills/<id>/.sprout-origin.json` for update tracking
- Registry is embedded via `//go:embed` — works offline, updated via PR to the repo
- Overwrite protection: prompts to confirm when skill ID already exists (`--force` to skip)
- Malformed SKILL.md (missing name/description) is rejected with clear error
- Supply-chain risk acknowledged: user opts in to install code-shaped instructions

## Artifacts

- code: `pkg/skills/install.go` — core install/update/uninstall logic
- code: `pkg/skills/registry.go` — embedded registry.json loading and shorthand resolution
- code: `pkg/skills/library/registry.json` — seeded registry with starter skills
- code: `webui/src/components/settings/SkillsSettingsTab.tsx` — WebUI install/manage UI
- tests: `pkg/skills/install_test.go` — each install path + malformed rejection + overwrite logic
- tests: `pkg/skills/registry_test.go` — shorthand resolution + unknown shorthand error

Full specification archived — see git history for original content.
