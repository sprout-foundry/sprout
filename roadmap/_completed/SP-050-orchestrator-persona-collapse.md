# SP-050: Orchestrator Persona Collapse — One Persona, Configurable Git-Write

**Status:** ✅ Implemented (single `orchestrator` persona, git-write controlled by `AllowOrchestratorGitWrite` config flag)

Two near-identical orchestration personas existed (`orchestrator` and `repo_orchestrator`) causing code divergence at every git-write gate site (7 OR'd persona checks across the codebase). The `repo_orchestrator` variant had a ~2KB `system_prompt_append` with git policy trapped as an escaped JSON string. This spec collapsed both into a single `orchestrator` persona. Git-write capability is now controlled by the `AllowOrchestratorGitWrite` config flag (default `true` for fresh installs). The git policy markdown was extracted to `pkg/agent/prompts/persona_appends/orchestrator_git_policy.md` (embedded via `go:embed`) and conditionally appended when the flag is `true`. `repo_orchestrator` and `git_orchestrator` are kept as aliases for backwards compatibility. All 7 OR'd persona checks were simplified to single-persona checks. The EA prompt was updated to spawn `orchestrator` instead of `repo_orchestrator`.

## Key decisions

- Single persona ID (`orchestrator`) with config-flag-controlled git-write — cleaner than two diverging personas.
- Git policy extracted from JSON to a real `.md` file (edit-friendly, real diff, syntax highlighting).
- `repo_orchestrator` kept as an alias — existing session histories and configs resolve correctly.
- Alias resolution does NOT imply git-write was on — behavior follows the config flag, not the persona ID.
- Default `AllowOrchestratorGitWrite: true` for fresh installs (existing configs with the field set keep their value).
- No migration path needed — zero-value behavior handles existing configs.

## Artifacts

- code: `pkg/personas/configs/default_personas.json` — single `orchestrator` entry, `repo_orchestrator` as alias
- code: `pkg/agent/prompts/persona_appends/orchestrator_git_policy.md` — git policy markdown (go:embed)
- code: `pkg/agent/persona.go` — conditional prompt assembly based on `AllowOrchestratorGitWrite`
- code: `pkg/agent/tool_handlers_shell.go` — simplified persona checks (single `orchestrator` + flag)
- code: `pkg/agent/submanager_state.go` — default active persona is `orchestrator`
- code: `pkg/configuration/config.go` — `AllowOrchestratorGitWrite` default flipped to `true`
- tests: `pkg/agent/persona_test.go` — alias resolution and git policy append tests

Full specification archived — see git history for original content.
