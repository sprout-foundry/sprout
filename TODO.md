# TODO

---

## SP-026: Executive Assistant Persona

Spec: `roadmap/SP-026-executive-assistant.md`

[x] - SP-026 Phase A: Replace `isSubagent bool` with `subagentDepth int` on Agent struct — enables 3-level nesting: EA (depth=0) → orchestrator (depth=1) → coder/tester (depth=2). Update `getOptimizedToolDefinitions()` to filter delegation tools at depth >= 2. Add `MaxSubagentDepth` config (default: 2). Update all references. `pkg/agent/agent.go`, `pkg/agent/agent_getters.go`, `pkg/agent/conversation.go`, `pkg/agent/subagent_runner.go`, `pkg/configuration/config.go`
[x] - SP-026 Phase B: Add `working_dir` parameter to `run_subagent` tool — allows spawning subagents at any directory under `$HOME`. Add `WorkingDir` to `SubagentOptions` and `SubagentTask`. Validate target exists and is within `$HOME`. `pkg/agent/subagent_runner.go`, `pkg/agent/tool_handlers_subagent.go`
[x] - SP-026 Phase C: File-based task queue tools — `task_queue_read`, `task_queue_publish`, `task_queue_add` with atomic writes, file locking, and persistent storage at `~/.config/sprout/task_queue.json`. `pkg/agent_tools/task_queue.go`, `pkg/agent/tool_definitions.go`
[x] - SP-026 Phase D: Persona infrastructure — `LocalOnly bool` on `SubagentType`, `IsLocalMode()` detection, sliding risk cascade for EA approvals (auto-approve low-risk, reason about medium-risk, escalate high-risk), `-f`/`--force` auto-reject. `pkg/configuration/config.go`, `pkg/agent/persona.go`, `pkg/agent/tool_handlers_shell.go`
[] - SP-026 Phase E: Executive Assistant persona definition — full replacement system prompt, project discovery (AGENTS.md → git scan → memory → organic), auto-activate when started from `~`, commit tool with strict rules (reject force, require meaningful message), EA-spawned subagents get depth=1, two startup modes (queue mode for autonomous processing, interactive mode for standard chat). `subagent_prompts/executive_assistant.md`, `pkg/agent/project_discovery.go`, `pkg/agent/agent_creation.go`, `cmd/sprout/main.go`
