# Agent Step-Machine and Progress Guardrails TODO

- [x] Enforce strict PLAN → EXECUTE → EVALUATE loop
  - [x] Block write/exec tools (`micro_edit`, `edit_file_section`, `validate_file`, `run_shell_command`) unless a valid `plan_step` has been accepted
  - [x] Add corrective system message when invalid tool usage is attempted
  - [x] Scope tool menu per phase via guidance message

- [x] Require an artifact every turn
  - [x] After tool_calls, next turn must produce a JSON plan or concrete patch; inject “stop searching; read_file top candidate; return plan JSON now” after 1 no-progress turn
  - [x] Synthesize minimal plan on repeated misses

- [x] Tighten budgets and dedupe
  - [x] Cap `workspace_context` to 1 per interactive session
  - [x] Cap `read_file` to ~12 and dedupe identical reads/queries
  - [x] Deny repeated identical `workspace_context` calls; require read_file next

- [x] Evidence-first edits (docs)
  - [x] Require claim→citation (file:line) hunks; hard-reject plans lacking citations

- [x] Deterministic patching for docs/code
  - [x] Keep find/replace JSON path
  - [x] Add hunk-based patch format + deterministic apply for docs and code

- [x] Auto-read after search
  - [x] After `workspace_context.search_keywords`, auto `read_file` the `top_file` (and 1 more) to seed evidence

- [x] Cache-and-complete fallback
  - [x] Cache `{"edits":[...]}` plans found in the same turn as tool_calls and return on exhaustion

- [x] Provider-native function calling everywhere
  - [x] Use native tools for interactive controller

- [x] Focus files bias
  - [x] Provide a small “focus set” to the planner; gate doc edits outside focus

- [x] Post-edit validation gate
  - [x] After edits, run validator/build; on failure, one-shot repair turn

- [ ] Logs/observability (next)
  - [ ] Add per-turn artifact logging (plan JSON, applied patches, validation output)

- [ ] Prompt and guidance (next)
  - [ ] Stronger doc/plan template showing exact JSON schema with citations and examples

- [ ] Future: Re-enable playbooks
  - [ ] Once core planning/execution is reliable, move focus back to playbooks and incrementally restore them with the new guardrails (phase-scoped tools, evidence-first plans, deterministic apply)
