## Agent v2 Improvement TODO (ordered easiest → hardest)

Actionable tasks to bring v2 in line with high-performing agent patterns. Track progress using the checkboxes; each item includes intent and acceptance criteria.

### 1) Prompt/policy optimization
- [x] Keep control prompts < 300 tokens; “state plan JSON, then tool_calls JSON”
  - Accept: Control messages adhere to a compact template; token usage drops per turn
- [x] Include STOP WHEN in plans; require every plan to specify explicit completion criteria
  - Accept: Plans always include `stop_when` and Evaluator honors it

### 2) Operational caps and anti-loop guardrails
- [x] Reduce max turns to 10; hard-cap workspace_context (≤2) and shell (≤5), dedupe exact shell commands
  - Accept: Exceeding caps injects a forced route to read_file → edit → validate
- [x] Tighten no-progress detector: if no edit/validate for 2 turns, force deterministic step
  - Accept: Log “stuck-detector fired” and see an edit attempt on next turn

### 3) Tool schema hardening (strict validation already started)
- [x] Enforce required args per tool pre-execution; block and instruct re-emit on missing fields
  - Accept: read_file.file_path, run_shell.command, workspace_context.action, edit_file_section.{file_path,instructions}, micro_edit.{optional but if present must be valid}
- [x] Reject unknown tools and malformed tool_calls (missing function name)
  - Accept: Invalid calls do not execute; logs show “blocked: invalid,” plus a schema reminder

### 4) Observability and reporting
- [x] Expand final debug summary: per-tool counts, blocks, cache hits, and time per turn
  - Accept: End-of-run summary includes: turns, tools, blocked invalids/dups, cache hits, per-turn timings
- [x] Add “served from cache” and “blocked (reason)” markers to tool results
  - Accept: Logs clearly differentiate executed vs short-circuited actions

### 5) Model strategy
- [x] Use small/fast model for planner/executor/evaluator control turns; editing model only for codegen
  - Accept: Config defaults to a cheap control model; editing model invoked solely for code generation

### 6) Reproducibility and config snapshot
- [x] Capture a per-run config snapshot (models, temps, caps) and deterministic IDs
  - Accept: Each run logs a correlation ID and saved config snapshot for reproductions

### 7) LLM prompt pinning
- [x] Hash/print system prompts; detect format drift and inject auto-correction
  - Accept: Logs show prompt hash; drift triggers a reminder and schema re-send

### 8) Output size and streaming controls
- [x] Disable streaming for control turns; cap output size; consistent truncation markers
  - Accept: Control turns never stream; large outputs are truncated deterministically

### 9) Secret hygiene
- [x] Redact secrets in logs; avoid echoing env vars; block tool uploads of sensitive files
  - Accept: Logs scrubbed; sensitive paths and envs are not printed; upload attempts blocked

### 10) Deterministic file targeting
- [x] Extract concrete file paths from user intent; restrict to existing `.go` files
  - Accept: For intents with filenames, v2 skips workspace_context and targets that file directly
- [x] Fallback discovery cap (≤2 calls): embeddings or keywords, then choose top-1 file deterministically
  - Accept: If no filename in intent, at most two discovery calls precede a required read_file of a specific target

### 11) Fast-path micro edits
- [x] If intent is tiny (comments/imports/literals), skip planning and attempt micro_edit immediately after read_file
  - Accept: For small tasks, v2 performs micro_edit within ≤2 turns
- [x] Escalate to edit_file_section if micro_edit limits exceeded (hunks/lines)
  - Accept: Diff size guard triggers escalation with a single follow-up plan

### 12) Outcome-driven evaluation
- [x] Define success criteria per common task types (e.g., comment added at top of file)
  - Accept: Evaluator uses simple regex/AST checks to confirm success without another LLM call when possible
- [x] For doc-only changes, allow “success” without build/test
  - Accept: Validation step is skipped for docs; run completes

### 13) Preflight checks
- [x] Add preflight tool: verify file exist/writable, clean git state, required CLIs available
  - Accept: Preflight runs before edits/validation; blocks on unmet prerequisites with clear guidance

### 14) Tool result normalization
- [x] Normalize/cap tool outputs, redact secrets, compute short hashes for cache keys
  - Accept: Long outputs are truncated with markers; cache keys stable; secrets stripped from logs

### 15) Standard error taxonomy
- [x] Classify tool errors (invalid_args, transient, permission, not_found) and route handling accordingly
  - Accept: Logs show error class; planner guidance tailored to class

### 16) Tool-call interceptors
- [x] Auto-fix common arg mistakes (path normalization/quoting); reject unsafe patterns
  - Accept: Interceptor rewrites benign cases; blocks dangerous shell usage with rationale

### 17) Evidence verification
- [x] Add post-condition checks (regex/grep) for claimed edits before declaring success
  - Accept: If claim fails, Evaluator requests a repair plan; if passes, stop

### 18) Pre-apply diff review
- [x] Automatic code review LLM pass for generated diffs before apply
  - Accept: Risky diffs flagged; user can require approval flag to proceed

### 19) Model/task routing
- [x] Route by task type (docs/code/test) and file size; swap models with suitable context limits
  - Accept: Router selects control/editing models based on task; logs routing decision

### 20) Evidence cache and dedupe
- [x] Cache outputs of read_file, run_shell_command, and workspace_context; key = tool+args
  - Accept: Duplicate calls return cached result and are logged as “served from cache”; no external calls
- [x] Inject “You already have this evidence” on duplicates to guide model away from repeats
  - Accept: Duplicate attempts are reduced in subsequent turns
  - Notes: read_file cache is persisted with file-hash validation; shell and workspace_context cached persistently by args

### 21) Rate limiting and backoff
- [x] Add provider backoff on 429/5xx with jitter; cap parallel tool calls where applicable
  - Accept: Retries follow exponential backoff; metrics show fewer hard failures

### 22) Context budgeting
- [x] Enforce per-turn token budgets; compress evidence when over budget; chunk long files
  - Accept: Turns log budget use and any compression; no over-budget turns

### 23) Dry-run mode
- [x] Add a --dry-run flag to execute tools in simulation (no writes/shell side-effects)
  - Accept: Executor prints planned actions and would-be results; no file changes or shell effects occur

### 24) Budget/timeout policies
- [x] Configurable per-run cost/time/token budgets with early stop and summary
  - Accept: Run stops when any budget is hit; summary includes which budget triggered

### 25) Structured logging
- [x] Optional JSON logs with correlation IDs per turn/tool
  - Accept: --json-logs emits structured entries for easy analysis

### 26) Provider health checks
- [x] Preflight health checks for selected provider endpoints
  - Accept: Detects outages early and switches model or aborts fast

### 27) Provider failover
- [x] Failover to alternate provider/local model on outage/errors
  - Accept: Router retries with fallback model; logs failover

### 28) Security scanning pre-commit
- [x] Scan for secrets in modified files; block committing leaks
  - Accept: Fails with guidance if secret patterns detected (override via --allow-secrets)

### 29) Minimal diff application
- [x] Ensure smallest possible edits are generated/applied; avoid full-file rewrites unless necessary
  - Accept: Diff sizer reports reductions; full rewrites only when unavoidable

### 30) File encoding/EOL safety
- [x] Normalize EOLs and preserve original style
  - Accept: Edits retain file EOL style; lints pass formatting checks

### 31) Concurrency for independent steps
- [x] Parallelize independent read_file calls; keep dependent steps sequential
  - Accept: Measurable latency reduction on multi-file inspections; no dependency hazards

### 32) Test harness integration
- [x] Generate/run minimal tests for changed code where missing; prioritize fast path
  - Accept: For functions touched, a smoke test can be auto-generated/executed with a flag
  - Implemented: When `AutoGenerateTests` is enabled, minimal Go/Python smoke tests are generated for changed files. After edits, smoke tests are executed non-fatally (Go: `go test -run TestSmoke`; Python: `pytest -k smoke` fallback). Logs record pass/fail.

### 33) State serialization and resume
- [x] Persist planner/executor/evaluator state to disk and support resume on next run
  - Accept: Interrupted runs can resume with prior plan and evidence cache
  - Implemented: `pkg/agent/run_state.go` saves/loads/clears `.ledit/run_state.json`; `pkg/agent/orchestrator.go` loads on start (auto with `--skip-prompt`, prompt otherwise), saves after each iteration, clears on completion.

### 34) Policy versioning and canaries
- [x] Version control prompts/policies and add a quick canary task suite
  - Accept: Before rollout, canaries pass; policy version printed in logs
  - Implemented: PolicyVersion logged at start; run snapshot includes policy_version. Canary hook ready (use existing e2e minimal tasks as canaries).

### 35) AB testing and telemetry
- [x] Add lightweight metrics and AB hooks for planner/policy variants
  - Accept: Metrics include success rate, cost per goal, edits/turn, duplicate blocks; opt-in
  - Implemented: Telemetry events logged to configurable file with policy version/variant per iteration; hook present for AB variant (`PolicyVariant`).

### 36) Shell sandboxing
- [x] Run shell commands in a restricted sandbox (working dir jail, resource limits)
  - Accept: Risky commands are constrained by ulimits/working dir; logs note sandbox mode
  - Implemented: Commands run from workspace root with configurable timeout, conservative ulimit caps, environment sanitization, denylist + small allowlist, process-group kill on timeout, and verbose sandbox logging.

### 37) Persistent evidence cache with invalidation
- [x] Persist evidence across runs; invalidate on file/hash change
  - Accept: Cache hits survive process restarts; invalidates on diffs (implemented in evidence_cache with file-hash guard)

### 38) Staged workspace edits
- [x] Apply edits in a temp workspace and merge when validated
  - Accept: Validation runs against staged area; merge only on success

### 39) Multi-file dependency ordering
- [x] Order multi-file edits by import/use graph
  - Accept: Planner emits an order; executor respects it

### 40) Git context awareness
- [x] Use blame and recent changes to reduce risky modifications
  - Accept: Planner/evaluator consider blame info in guidance

### 41) AST-aware edits (optional)
- [x] Add function/struct span detection to anchor micro/section edits
  - Accept: Edits target AST spans; smaller diffs; higher success rate

### 42) Planner → Executor → Evaluator split
- [x] Implement Planner tool that returns a tiny JSON plan (1 step max)
  - Intent: Replace freeform tool selection with explicit 1-step plans including success criteria and stop condition
  - Accept: Given an intent, Planner returns `{action, target_file, instructions, stop_when}`; no prose
- [x] Modify Executor to run only the plan’s single step (no freestyle)
  - Accept: Executor rejects any tool not matching the plan; logs reason; requires next plan for further steps
- [x] Add Evaluator that checks success criteria and either stops or requests a revised micro-plan
  - Accept: After each step, Evaluator emits `{status: completed|continue, reason}` and either completes or re-invokes Planner

---

Notes
- Avoid plan horizons > 1 step to reduce drift
- Prefer partial edits and keep change surface minimal
- Default to concrete actions over exploration; explore only on hard blocks


