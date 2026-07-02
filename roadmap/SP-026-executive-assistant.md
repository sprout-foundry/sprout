# SP-026: Coordinator Persona (formerly "Executive Assistant")

**Status:** ✅ Implemented (renamed 2026-06-03, see commit `516a9d41`)
**Date:** 2026-06-03 (rename); original EA groundwork landed across 2026-04 → 2026-06-03
**Depends on:** SP-001 (Agent Core), SP-023 (In-Process Subagents), SP-022 (Workspace Management)
**Priority:** High
**Effort Estimate:** ~4-5 weeks (5 phases)

> **Note:** This persona shipped as **Coordinator** on 2026-06-03 (commit
> `516a9d41` — "refactor(personas): rename code_reviewer to reviewer and
> executive_assistant to coordinator"). The legacy name is preserved as an
> alias — `executive_assistant`, `ea`, and `assistant` all canonicalize to
> `coordinator` via `pkg/personas/configs/coordinator.json`. See
> `docs/PERSONAS.md` and `pkg/agent/persona_test.go:779` for the canonicalization
> test. The body of this spec describes the original EA design; the renames in
> the "Files Changed" section were applied as part of `516a9d41`.

## Problem

The current agent architecture is repo-scoped and code-focused. Every conversation targets a single `workspaceRoot` (typically a git repo), and the system prompt is tuned for software engineering tasks. This works well for code work but breaks down when users want a higher-level assistant that:

1. **Operates across multiple projects** — Cannot coordinate work across `~/projects/sprout` and `~/projects/sprout-foundry` in a single conversation. The agent is pinned to one `workspaceRoot`.
2. **Manages rather than implements** — The user wants natural-language task delegation ("update the changelog in sprout-foundry, then run the test suite in sprout"), not hands-on coding.
3. **Acts on the user's behalf** — The EA should be able to approve operations that normally require user confirmation, following the user's expressed intent rather than blocking for interactive approval.
4. **Delegates through orchestrators** — The EA can spawn `orchestrator` and `repo_orchestrator` subagents, which in turn spawn their own coding subagents. This creates a 3-level delegation chain: EA → orchestrator → coder/tester/debugger. This is the first time subagents need to spawn their own subagents.
5. **Runs persistently** — The EA operates in a loop, reading from a file-based task queue, delegating work, and publishing progress updates. It needs dedicated tools for task queue management.

This persona is **local-only at launch** with planned cloud/dev-container support ~6 months post-launch.

## Where Prompts Live

System prompts for personas are stored under `pkg/agent/prompts/subagent_prompts/`. The path in persona config JSON uses this directory as the root:

```
pkg/agent/prompts/subagent_prompts/
├── executive_assistant.md    # EA system prompt
├── orchestrator.md           # Orchestrator prompt
├── repo_orchestrator.md      # Repo orchestrator prompt
├── coder.md                  # Coder subagent prompt
├── tester.md                 # Tester subagent prompt
├── debugger.md               # Debugger subagent prompt
├── code_reviewer.md          # Code reviewer prompt
└── ...
```

In persona config, the `system_prompt` field uses the full path relative to the project root:
```json
{ "system_prompt": "pkg/agent/prompts/subagent_prompts/executive_assistant.md" }
```

## Proposed Solution

Introduce an **Executive Assistant** (EA) persona that:

- Lives at the **home directory level** (`~`), not at a single repo root
- Can **delegate to subagents in any directory** under `$HOME`
- Has **elevated approval authority** with a sliding risk cascade — auto-approves routine operations, reasons about moderate-risk operations like a cautious user, and escalates truly non-recoverable operations back to the user
- **Can write files and use shell commands** but its primary purpose is coordination, not implementation
- Gets the `commit` tool with strict rules (auto-reject `-f`/`--force` flags)
- Is **disabled in cloud environments at launch**, with a future path for dev containers
- Gets `run_subagent` and `run_parallel_subagents` tools
- Operates in a **task-driven loop** with a file-based todo queue and progress publishing tools

### Architecture

```
User → Executive Assistant Agent (workspaceRoot: ~)
         │
         ├── Task Queue (file-based, ~/tasks/ or similar)
         │     └── EA reads tasks, prioritizes, delegates
         │
         ├── run_subagent(persona: "repo_orchestrator", cwd: "~/projects/sprout")
         │     └── Orchestrator Agent (workspaceRoot: ~/projects/sprout)
         │           ├── run_subagent(persona: "coder") → Coder subagent
         │           └── run_subagent(persona: "tester") → Tester subagent
         │
         ├── run_subagent(persona: "repo_orchestrator", cwd: "~/projects/sprout-foundry")
         │     └── Orchestrator Agent (workspaceRoot: ~/projects/sprout-foundry)
         │           └── run_subagent(persona: "coder") → Coder subagent
         │
         ├── commit tool (strict rules, -f/--force auto-rejected)
         ├── shell_command (same rules as any agent)
         └── Progress publisher → writes status updates to task queue
```

The EA is NOT a subagent. It is a **top-level persona** activated via `/persona executive_assistant` or automatically when sprout is started from `~`. It runs as the primary agent with special capabilities.

## Key Design Decisions

### 1. EA is a Top-Level Persona, Not a Subagent

The EA runs as the **primary agent** with its own persona configuration. When active:
- The agent's `workspaceRoot` expands to `$HOME`
- The system prompt switches to the EA coordinator prompt (full replacement)
- The tool set includes delegation tools + read/write tools + commit with restrictions
- `run_subagent` / `run_parallel_subagents` remain available (they are only removed when `isSubagent=true`)

This means the EA does not need the `run_subagent` restriction lifted — it never sets `isSubagent=true`.

### 2. Three-Level Subagent Nesting

This is the first time subagents need to spawn their own subagents. The chain is:

```
Level 0: Executive Assistant (primary agent, NOT isSubagent)
Level 1: orchestrator / repo_orchestrator (isSubagent=true, but can delegate)
Level 2: coder / tester / debugger / etc. (isSubagent=true, cannot delegate)
```

**Current behavior:** `isSubagent=true` unconditionally removes `run_subagent` and `run_parallel_subagents` from the tool set (see `conversation.go:66`). This must change.

**New behavior:**
- Replace the boolean `isSubagent` with an integer `subagentDepth int` on the Agent struct
- `subagentDepth = 0` → primary agent (full tools)
- `subagentDepth = 1` → orchestrator-level subagent (gets `run_subagent`, can delegate)
- `subagentDepth >= 2` → worker-level subagent (no `run_subagent`, cannot delegate further)
- In `getOptimizedToolDefinitions()`, check `subagentDepth >= 2` instead of `isSubagent` to filter delegation tools
- Maximum depth enforced by config: `MaxSubagentDepth int` (default: 2)
- The EA persona sets its spawned subagents to `subagentDepth = 1`, which allows them to spawn level-2 subagents

**Security guard:** Only the EA persona can spawn depth-1 subagents. If a regular (non-EA) primary agent spawns a subagent, that subagent gets `subagentDepth = 2` (cannot delegate), preserving current behavior.

**⚠ Critical: Multiple code paths block subagent delegation.** The `isSubagent` boolean is checked in at least 4 locations, not just `getOptimizedToolDefinitions()`. All must be updated:

1. `conversation.go:66` — Tool definition filtering (hides tools from model)
2. `seed_tool_registry.go:1081-1091` — `PreExecuteHook` execution gate (rejects tool calls even if model somehow calls them)
3. `tool_definitions.go:485-496` — `ToolRegistry.ExecuteTool` execution gate (legacy/parallel path)
4. Security approval paths in `seed_tool_registry.go:1108`, `tool_definitions.go:516`, `tool_handlers_shell.go:267` — Use `!isSubagent` to decide whether to prompt user via WebUI

All four must change from `IsSubagent()` → `subagentDepth >= 2` (or `!CanDelegate()`) for depth-1 delegation to work.

**⚠ Critical: Security approval bypass for depth-1 subagents.** Current approval paths skip the WebUI approval for all subagents (`!isSubagent`). With depth-1 orchestrators, this means CAUTION operations are auto-allowed and SHOULD_BLOCK operations are unconditionally rejected with no way to approve. The risk cascade needs to hook into these security checks, not just operate at the EA level. Depth-1 subagents spawned by the EA need a way to route approvals back through the EA (or to the user) for medium-risk operations.

### 3. Subagent Working Directory Override

Currently, `SubagentOptions` inherits `WorkspaceRoot` from the parent agent's shared state (`r.shared.WorkspaceRoot`). The EA needs to spawn subagents with **different working directories** per task.

**Changes needed:**
- Add `WorkingDir string` to `SubagentOptions` and `SubagentTask`
- In `createSubagent()`, use `opts.WorkingDir` if set, falling back to `r.shared.WorkspaceRoot`
- In `handleRunSubagent()`, accept a new `working_dir` parameter
- Validate the target directory is within `$HOME` and exists
- The subagent's `workspaceRoot` is set to this directory, so all file operations are scoped correctly
- `working_dir` is separate from `files` — `working_dir` sets the root; `files` injects context

**Security: Sensitive directory denylist.** The EA operates at `$HOME` level. Without restrictions, it could spawn subagents in `~/.ssh/`, `~/.gnupg/`, `~/.aws/`, `~/.config/sprout/` (credentials). Add a `WorkingDirDenyList` config field with sensible defaults (`.ssh`, `.gnupg`, `.aws`, `.kubeconfig`, `.config/sprout`). Subagents spawned in denied directories are rejected. The EA can still read these paths via `read_file` but cannot delegate into them.

### 4. Sliding Risk Cascade for Approvals

The EA's approval system operates on a risk spectrum:

| Risk Level | Examples | EA Behavior |
|-----------|---------|-------------|
| **Low** (auto-approve) | `git add`, `git status`, `git log`, `git diff`, read file ops, subagent spawn in known project dir | Auto-approve. EA has full authority. |
| **Medium** (reason + decide) | `git commit`, `git push`, cross-directory file access, subagent in unfamiliar directory | EA reasons about intent. Gets context from subagent output. Decides like a cautious user. Can ask subagent for more details before approving. |
| **High** (escalate to user) | `rm -rf`, `git push --force`, `git reset --hard`, `docker system prune`, any `-f`/`--force` flag | Auto-reject. Never approve without explicit user consent. Report to user. |

**Implementation:**
- New config field on persona: `AutoApproveRules` — structured rules for what the EA can auto-approve vs reason-about vs escalate
- New method on Agent: `evaluateOperationRisk(operation, args) RiskLevel`
- Shell handler and git handler check this risk evaluation before auto-approving for EA persona
- `-f` and `--force` flag detection: parse git and shell command args; any force flag triggers automatic escalation regardless of operation type

### 5. Commit Tool Access with Strict Rules

The EA gets the `commit` tool but with additional restrictions:

- **Auto-reject any commit with `-f` or `--force`** — parse the command/args before execution
- **Require meaningful commit messages** — reject empty or single-word messages
- **Log all EA-initiated commits** — include EA persona tag in commit metadata for audit trail
- **Validate subagent output before committing** — EA should review `git diff --stat` and subagent summary before committing
- Implementation: extend `handleCommitTool()` with EA-specific validation when `a.GetActivePersona() == "executive_assistant"`

### 6. Cloud Environment Detection (Local-Only at Launch)

The EA must only be available locally at launch. Detection:

- **Simplest approach**: Add `IsLocalMode() bool` to the agent that returns true when:
  - Running from CLI (`a.ui == nil` or terminal UI)
  - Running from local WebUI (`a.ui != nil` and `os.Getenv("SPROUT_CLOUD") != "1"`)
- The `GetAvailablePersonaIDs()` method filters by `Enabled`. Add additional filter: if persona has `LocalOnly: true` and `!a.IsLocalMode()`, skip it
- **Future (6 months post-launch)**: Extend `LocalOnly` to support dev containers. When running in a dev container with user oversight (e.g., VS Code remote), the EA should be available. Detection: `SPROUT_DEV_CONTAINER=1` env var.

**Config change:**
```go
type SubagentType struct {
    // ... existing fields ...
    LocalOnly bool `json:"local_only,omitempty"` // Only available in local mode
}
```

### 7. EA Tool Set

The EA gets the **full tool set** minus a few exclusions. It is not primarily a coder, but it CAN write files, edit files, and run shell commands when needed for coordination tasks.

**Included:**
- `shell_command` — same rules as any agent, plus EA risk cascade for dangerous ops
- `read_file`, `write_file`, `edit_file` — file operations for coordination (editing config, updating task files, etc.)
- `search_files` — searching across the filesystem
- `run_subagent`, `run_parallel_subagents` — delegation to coding personas
- `commit` — git commits with strict EA rules (no force)
- `list_skills`, `activate_skill` — for loading context
- `list_memories`, `read_memory`, `add_memory`, `delete_memory` — memory system
- `analyze_image_content`, `analyze_ui_screenshot` — visual analysis
- `web_search`, `fetch_url` — research
- `mcp_tools` — MCP integration
- `todo_write`, `todo_read` — session-level task tracking
- **NEW:** `task_queue_read`, `task_queue_publish` — file-based persistent task queue (see §8)

**Excluded:**
- None by policy. The EA's system prompt steers it toward delegation, but the tool set is not artificially restricted. The persona behavior comes from the system prompt, not tool removal.

### 8. File-Based Task Queue Tools

The EA operates in a loop, reading from and publishing to a persistent task queue. This requires two new tools:

#### `task_queue_read`

Reads pending tasks from a file-based queue. The queue lives at `$HOME/.config/sprout/task_queue.json` (configurable).

```json
{
  "name": "task_queue_read",
  "description": "Read pending tasks from the persistent task queue. Returns tasks sorted by priority.",
  "parameters": {
    "status": {
      "type": "string",
      "enum": ["pending", "in_progress", "completed", "failed", "all"],
      "default": "pending",
      "description": "Filter tasks by status"
    },
    "limit": {
      "type": "integer",
      "default": 10,
      "description": "Maximum number of tasks to return"
    }
  }
}
```

**Task schema:**
```json
{
  "id": "task-uuid",
  "title": "Update changelog in sprout-foundry",
  "description": "Add entries for the v2.3.0 release",
  "status": "pending|in_progress|completed|failed",
  "priority": "high|medium|low",
  "assigned_to": "subagent-id or null",
  "working_dir": "~/projects/sprout-foundry",
  "persona": "repo_orchestrator",
  "created_at": "2026-05-17T10:00:00Z",
  "updated_at": "2026-05-17T10:05:00Z",
  "result": "summary of completed work or error",
  "parent_task_id": "optional parent for subtasks"
}
```

#### `task_queue_publish`

Updates task status and publishes progress. This is how the EA records what it's done and communicates status.

```json
{
  "name": "task_queue_publish",
  "description": "Update a task in the persistent queue. Used to claim tasks, record progress, and mark completion.",
  "parameters": {
    "task_id": {
      "type": "string",
      "required": true,
      "description": "The task ID to update"
    },
    "status": {
      "type": "string",
      "enum": ["in_progress", "completed", "failed"],
      "required": true
    },
    "result": {
      "type": "string",
      "description": "Summary of work done or error message"
    },
    "subtasks": {
      "type": "array",
      "description": "Break down into subtasks. Each gets its own ID.",
      "items": {
        "type": "object",
        "properties": {
          "title": {"type": "string"},
          "working_dir": {"type": "string"},
          "persona": {"type": "string"},
          "priority": {"type": "string"}
        }
      }
    }
  }
}
```

#### `task_queue_add`

Adds new tasks to the queue. Used by the user (via natural language) or by the EA when breaking down work.

```json
{
  "name": "task_queue_add",
  "description": "Add a new task to the persistent queue.",
  "parameters": {
    "title": {"type": "string", "required": true},
    "description": {"type": "string"},
    "priority": {"type": "string", "enum": ["high", "medium", "low"], "default": "medium"},
    "working_dir": {"type": "string"},
    "persona": {"type": "string"}
  }
}
```

**Implementation:**
- File-based storage at `~/.config/sprout/task_queue.json`
- File locking for concurrent access (EA loop + user edits)
- Task IDs are UUIDs for uniqueness
- `task_queue_read` is lightweight — reads and filters in-memory after loading
- `task_queue_publish` writes atomically (write to temp, rename)
- Task queue is independent from the session-level `TodoManager` — the queue persists across sessions

### 9. EA Startup Modes

The EA starts in one of two modes, determined by a CLI flag or config setting:

#### Mode 1: Task Queue Mode (`--ea-mode=queue` or `ea_mode: "queue"`)

The EA starts, reads all pending tasks from the task queue, and works through them sequentially. When all tasks are completed (or failed), the EA exits.

**Lifecycle:**
```
1. Start EA → load task queue
2. Read highest-priority pending task
3. Plan subtask breakdown (if needed) → publish subtasks to queue
4. Delegate to subagent(s) → wait for completion
5. Publish results to queue
6. Repeat from step 2
7. When no pending tasks remain → publish summary → exit
```

**Behavior:**
- The EA sends a single "startup" message to the LLM that includes the full task queue state and the instruction to work through it
- After each task completes, the EA re-reads the queue to pick up any new tasks that were added (by the user or by the EA itself as subtasks)
- The EA does NOT accept chat input in this mode — it is autonomous
- Progress is visible via the task queue file and any event streaming (WebUI terminal output, etc.)
- If the EA encounters a high-risk operation that requires user approval, it marks the task as `blocked` with a reason and moves to the next task
- On failure, the EA marks the task as `failed` with the error details and continues to the next task
- The EA exits with code 0 when the queue is empty, code 1 if any tasks failed

**New task schema status:** Add `"blocked"` to the status enum — indicates the task requires user intervention before it can proceed.

**Triggering:**
```bash
sprout --persona executive_assistant --ea-mode=queue
# or via config:
# "ea_mode": "queue"
# or when tasks exist in the queue and sprout is started from ~:
# auto-detects queue mode if task_queue.json has pending tasks
```

#### Mode 2: Interactive Mode (`--ea-mode=interactive` or default)

The standard chat-based interface. The user talks to the EA in natural language, and the EA delegates as needed. Task queue tools are still available — the user can ask the EA to work through queued tasks, add new tasks, or check progress — but the EA does not autonomously loop.

**Behavior:**
- The EA receives a system prompt that explains its coordination role
- The user can ask the EA to do anything: "update the changelog in sprout-foundry", "run tests across all my projects", "what's the status of my task queue?"
- The EA uses `task_queue_read` / `task_queue_publish` / `task_queue_add` tools when the user asks about tasks
- The EA uses `run_subagent` to delegate actual work
- This is the default when the EA persona is active (no `--ea-mode` flag specified)

**CLI flag:**
```bash
# These are equivalent — both start the EA in interactive mode:
sprout --persona executive_assistant
sprout --persona executive_assistant --ea-mode=interactive

# Explicit queue mode:
sprout --persona executive_assistant --ea-mode=queue
```

#### Implementation

**Config change:**
```go
// EAMode controls how the Executive Assistant persona operates.
// "interactive" = standard chat interface (default)
// "queue" = autonomous task processing, exits when done
type EAMode string

const (
    EAModeInteractive EAMode = "interactive"
    EAModeQueue       EAMode = "queue"
)
```

**Modified files:**
- `cmd/sprout/main.go` — Add `--ea-mode` CLI flag
- `pkg/configuration/config.go` — Add `EAMode string` to config
- `pkg/agent/agent_creation.go` — When EA persona is active and `EAMode == "queue"`, inject queue-processing system prompt and disable interactive input
- `pkg/agent/executive_assistant_test.go` — Tests for both modes

**Queue mode exit conditions:**
- All tasks completed or failed → exit 0 (or exit 1 if any failed)
- No pending tasks remain → exit 0
- Maximum iterations reached → exit 1 with partial progress saved
- User interrupt (Ctrl+C) → save current task as `in_progress`, exit 130

### 10. Project Discovery

The EA needs to know about the user's project landscape. Discovery happens in priority order:

1. **AGENTS.md / project config** — If a project has an `AGENTS.md` or `.sprout/config.json`, read it for project metadata (name, description, language, related projects)
2. **Git repo scan** — Walk `$HOME` (up to 2 levels deep) looking for `.git` directories. Skip `node_modules`, `.cache`, `.local`, common noise directories
3. **Memory system** — Read memories for project context learned in previous sessions
4. **Organic learning** — The EA learns about projects through conversation and saves to memory

The project index is cached in memory and refreshed periodically (or on demand via a tool call).

### 11. Default Persona Based on Workspace

- If sprout is started from `~` (home directory), the EA persona is activated by default
- If started from a project directory, the default persona remains `repo_orchestrator` (current behavior)
- User can switch to EA at any time via `/persona executive_assistant`
- User can configure this default in config: `default_persona_by_path` mapping

## Implementation Phases

### Phase A: Subagent Depth System (Backend Core)

Replace boolean `isSubagent` with integer `subagentDepth`. Enable 3-level nesting.

**Modified files:**
- `pkg/agent/agent.go` — Replace `isSubagent bool` with `subagentDepth int`; add `SubagentDepth() int` getter
- `pkg/agent/agent_getters.go` — `IsSubagent()` returns `subagentDepth > 0`; add `SubagentDepth() int`; add `CanDelegate() bool` (returns `subagentDepth < maxDepth`)
- `pkg/agent/subagent_runner.go` — In `createSubagent()`, set `subagentDepth = parentDepth + 1`; accept `InitialDepth` in options for EA-spawned orchestrators
- `pkg/agent/conversation.go` — In `getOptimizedToolDefinitions()`, check `subagentDepth >= 2` instead of `isSubagent` to filter delegation tools
- `pkg/configuration/config.go` — Add `MaxSubagentDepth int` to config (default: 2)
- `pkg/agent/tool_handlers_subagent.go` — Pass depth context through to subagent creation
- All tests that check `isSubagent` — Update to use `subagentDepth`

**Acceptance:** An EA-spawned `repo_orchestrator` subagent (depth=1) can spawn a `coder` subagent (depth=2). The coder subagent (depth=2) cannot spawn further subagents.

### Phase B: Subagent Working Directory Override

Enable spawning subagents in directories other than the parent's workspace root.

**Modified files:**
- `pkg/agent/subagent_runner.go` — Add `WorkingDir string` to `SubagentOptions` and `SubagentTask`; use in `createSubagent()` to set `agent.workspaceRoot`
- `pkg/agent/tool_handlers_subagent.go` — Parse `working_dir` parameter; validate exists and is within `$HOME`; pass to `SubagentOptions`
- `pkg/agent/tool_handlers_subagent_test.go` — Tests for `working_dir` validation, scope enforcement, and fallback

**Acceptance:** `run_subagent(persona="repo_orchestrator", working_dir="~/projects/sprout", prompt="...")` spawns a subagent with `workspaceRoot=~/projects/sprout`.

### Phase C: File-Based Task Queue Tools

New tools for persistent task management.

**New files:**
- `pkg/agent_tools/task_queue.go` — TaskQueue struct, file locking, CRUD operations, atomic writes
- `pkg/agent_tools/task_queue_test.go` — Tests for queue operations, concurrent access, atomicity

**Modified files:**
- `pkg/agent/tool_definitions.go` — Add `task_queue_read`, `task_queue_publish`, `task_queue_add` tool definitions
- `pkg/agent/tool_executor.go` — Register handlers for new tools
- `pkg/configuration/config.go` — Add `TaskQueuePath string` config field (default: `~/.config/sprout/task_queue.json`)

**Acceptance:** EA can read pending tasks, claim them, publish progress, and add new tasks. Queue persists across sessions.

### Phase D: Persona Infrastructure — Local-Only + Risk Cascade

Support for local-only personas and the EA approval system.

**Modified files:**
- `pkg/configuration/config.go` — Add `LocalOnly bool` to `SubagentType`; add `AutoApproveRules` struct
- `pkg/agent/persona.go` — Filter local-only personas in `GetAvailablePersonaIDs()`; add `IsLocalMode()` check
- `pkg/agent/agent_getters.go` — Add `IsLocalMode() bool` method
- `pkg/agent/tool_handlers_shell.go` — Extend approval checks with risk cascade for EA persona; add `-f`/`--force` detection
- `pkg/agent/tool_handlers_shell.go` — Extend `isOrchestratorGitWriteAllowed()` to recognize EA persona with auto-approve config
- `pkg/agent/persona_test.go` — Tests for local-only filtering and risk cascade
- `pkg/agent/agent.go` — Auto-detect EA as default when workspaceRoot is `$HOME`

**Risk cascade config schema:**
```json
{
  "auto_approve_rules": {
    "low_risk": ["git_add", "git_status", "git_log", "git_diff", "read_file", "search_files"],
    "medium_risk": ["git_commit", "git_push", "cross_directory", "subagent_spawn"],
    "high_risk_never": ["force_flag", "rm_recursive", "git_reset_hard", "docker_prune"]
  }
}
```

**Acceptance:** EA auto-approves low-risk ops, reasons about medium-risk ops, and auto-rejects high-risk ops. `-f`/`--force` always escalates. Local-only personas hidden in cloud mode.

### Phase E: Executive Assistant Persona Definition

Define the EA with its system prompt, project discovery, and integration.

**New files:**
- `pkg/agent/prompts/subagent_prompts/executive_assistant.md` — Full system prompt for the EA
- `pkg/agent/project_discovery.go` — Project scanning, AGENTS.md parsing, index caching
- `pkg/agent/project_discovery_test.go` — Tests for project discovery
- `pkg/agent/executive_assistant_test.go` — Integration tests for full EA workflow

**Modified files:**
- `pkg/configuration/config.go` — Add `DefaultPersonaByPath map[string]string` config field
- `pkg/agent/agent_creation.go` — Auto-activate EA when started from `$HOME`
- `pkg/agent/tool_handlers_subagent.go` — EA persona can spawn subagents at any `$HOME` subdirectory; passes `subagentDepth=1` for orchestrator spawns
- `pkg/agent/tool_handlers_shell.go` — EA commit validation (reject force, require meaningful message)

**Acceptance:**
- `/persona executive_assistant` activates the EA
- Starting sprout from `~` auto-activates EA
- EA discovers projects via AGENTS.md, git scan, and memory
- EA delegates to `repo_orchestrator` subagents that can spawn their own coding subagents
- EA reads from task queue, delegates, and publishes progress
- Commit tool rejects `-f`/`--force` for EA
- All operations respect the risk cascade

## Files Changed (Summary)

| File | Change |
|------|--------|
| `pkg/agent/agent.go` | `isSubagent bool` → `subagentDepth int` |
| `pkg/agent/agent_getters.go` | Add `IsLocalMode()`, `SubagentDepth()`, `CanDelegate()`; update `IsSubagent()` |
| `pkg/agent/agent_creation.go` | Update `isSubagent bool` param to `subagentDepth int` in agent constructor |
| `pkg/agent/subagent_runner.go` | Add `WorkingDir` to options; depth propagation; depth-aware agent creation |
| `pkg/agent/conversation.go` | Filter delegation tools at depth >= 2 instead of isSubagent |
| `pkg/agent/tool_handlers_subagent.go` | Parse `working_dir`; EA depth=1 spawning; scope validation |
| `pkg/agent/tool_handlers_shell.go` | Risk cascade for EA; force flag detection; EA commit rules |
| **`pkg/agent/seed_tool_registry.go`** | **Update `PreExecuteHook` execution gate**: change `IsSubagent()` to `subagentDepth >= 2` for delegation block; update security approval paths |
| **`pkg/agent/tool_definitions.go`** | **Update `ToolRegistry.ExecuteTool` gate**: same depth check; update security approval paths |
| `pkg/agent/tool_executor.go` | Update tool start events to include `subagentDepth` |
| `pkg/agent/persona.go` | Local-only filtering; `IsLocalMode()` integration |
| `pkg/agent/persona_test.go` | Tests for local-only and depth |
| `pkg/agent/agent_events.go` | Update event publishing to use `subagentDepth` |
| `pkg/events/events.go` | Update `ToolStartEvent` struct: `isSubagent bool` → `subagentDepth int` |
| `pkg/agent/pause.go` | Verify interrupt handling works with depth (already correct) |
| `pkg/configuration/config.go` | `LocalOnly`, `AutoApproveRules`, `MaxSubagentDepth`, `TaskQueuePath`, `DefaultPersonaByPath`, `WorkingDirDenyList` |
| `pkg/agent_tools/task_queue.go` | **New:** File-based task queue with locking + atomic writes |
| `pkg/agent_tools/task_queue_test.go` | **New:** Queue tests |
| `pkg/agent/project_discovery.go` | **New:** Project scanning and index |
| `pkg/agent/project_discovery_test.go` | **New:** Discovery tests |
| `pkg/agent/executive_assistant_test.go` | **New:** Integration tests |
| `pkg/agent/prompts/subagent_prompts/executive_assistant.md` | **New:** EA system prompt |
| All files referencing `isSubagent` | Update to `subagentDepth` (search: `grep -rn "isSubagent\|IsSubagent" pkg/`) |

## Configuration Example

```json
{
  "max_subagent_depth": 2,
  "task_queue_path": "~/.config/sprout/task_queue.json",
  "default_persona_by_path": {
    "~": "executive_assistant"
  },
  "subagent_types": {
    "executive_assistant": {
      "id": "executive_assistant",
      "name": "Executive Assistant",
      "description": "Coordinates work across projects by delegating to orchestrator subagents. Manages a persistent task queue and operates on the user's behalf with elevated approval authority.",
      "enabled": true,
      "local_only": true,
      "system_prompt": "pkg/agent/prompts/subagent_prompts/executive_assistant.md",
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514",
      "auto_approve_rules": {
        "low_risk": ["git_add", "git_status", "git_log", "git_diff", "read_file", "search_files"],
        "medium_risk": ["git_commit", "git_push", "cross_directory", "subagent_spawn"],
        "high_risk_never": ["force_flag", "rm_recursive", "git_reset_hard"]
      }
    }
  }
}
```

## Future Work (Post-Launch)

1. **Cloud/Dev Container Support (~6 months)** — Extend `LocalOnly` to allow EA in dev containers where the user has oversight. Detection: `SPROUT_DEV_CONTAINER=1` env var. May require additional approval flows for cloud contexts.

2. **WebUI Task Dashboard** — A dedicated UI panel showing the task queue, active subagents, and progress across projects. More visual than the current subagent activity events.

3. **Project Relationships** — Allow the EA to learn and persist project dependency graphs (e.g., "sprout-foundry depends on sprout packages"). Use this for intelligent task ordering.

4. **Multi-User EA** — In team environments, the EA could coordinate work across multiple contributors' repos.

## Review Findings (Pre-Implementation)

A code review of the spec against the actual codebase identified these issues. They are incorporated into the spec above where applicable and summarized here for reference.

### Resolved (incorporated into spec)

| # | Finding | Resolution |
|---|---------|-----------|
| 1 | `PreExecuteHook` in `seed_tool_registry.go:1081` blocks delegation for all subagents | Added to Files Changed table; all execution gates must use `subagentDepth >= 2` |
| 2 | `ToolRegistry.ExecuteTool` in `tool_definitions.go:485` has same block | Added to Files Changed table |
| 3 | Security approval paths skip WebUI for all subagents — depth-1 agents can't route approvals | Noted in §2 as critical warning; needs design in Phase D |
| 4 | Missed files in change table | Expanded table with `seed_tool_registry.go`, `tool_definitions.go`, `tool_executor.go`, `agent_creation.go`, `agent_events.go`, `events.go`, `pause.go` |
| 5 | `working_dir` allows spawning in sensitive dirs (`~/.ssh`, etc.) | Added `WorkingDirDenyList` config |

### Open items to resolve during implementation

| # | Issue | Recommendation |
|---|-------|---------------|
| 6 | `commonParent()` bug: two files from different projects escalates workspace to `$HOME` | Fix in Phase B: limit commonParent to never exceed the spawning agent's workspace |
| 7 | Task queue concurrent write safety | Use `gofrs/flock` + atomic rename; consider per-task files for high concurrency |
| 8 | `MaxSubagentDepth = 0` ambiguity | Default to 2; document that 0 means "no nesting" (current behavior) |
| 9 | `DefaultPersonaByPath` matching semantics | Use longest-prefix match after resolving `~` to actual home path |
| 10 | `IsLocalMode()` definition for daemon mode | Daemon + local WebUI = local mode; daemon + `SPROUT_CLOUD=1` = cloud mode |
| 11 | Project discovery performance | Add 5-second timeout, cache with 1-hour staleness, skip common noise dirs |
| 12 | Task queue retention policy | Add configurable retention window; auto-archive tasks older than N days |
| 13 | `todo_write` vs `task_queue_*` confusion in EA prompt | System prompt must clearly distinguish session todos from persistent tasks |

## Related

- SP-001: Agent Core Architecture (agent struct, persona system)
- SP-023: In-Process Subagents (subagent runner, shared state)
- SP-022: Workspace Management (workspace root, directory scoping)
- SP-015: Cloud Platform Integration (cloud vs local detection)
- SP-018: Memory System (cross-session knowledge persistence)
- SP-021: ~~Self-Review Tool~~ (removed 2026-07-01 — did not add value in practice)
