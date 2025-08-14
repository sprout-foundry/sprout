# Roadmap: Post-Agent v2 Enhancements

Prioritized best-practice improvements for the Agent → Code Editing workflow.

## Editing fidelity and safety
- AST-backed edits via tree-sitter across Go/TS/JS/Python/Ruby/PHP/Rust/Java: select precise spans, emit minimal diffs.
  - Status: Go implemented using stdlib AST; others pending.
- Three-way, context-aware patch apply: generate unified diffs with generous context; detect/rebase conflicts.
  - Status: basic three-way merge integrated into full/partial edit flows.
- Idempotent micro-edits: avoid duplicating insertions; check AST presence/markers before applying.
  - Status: duplicate-insertion guard added for partial edits.
- Stronger postconditions: regex + AST assertions (symbol added, imports resolved, signatures preserved).

## Planning and risk management
- Risk-aware gates: score by change surface, criticality (auth/CI/build), recent blame, and test coverage; add human review gates for high-risk.
- Enhanced multi-file ordering: SCC detection and topological ordering across ecosystems; batch within cycles.
- Better file discovery: hybrid retrieval (embeddings + keyword + symbol index), symbol/usage graph, rerank by intent.

## Validation and tests
- Language-aware validation runners: Node (tsc/eslint/jest), Rust (cargo check/test), Java (mvn/gradle), PHP (php -l/phpunit), Ruby (rubocop/rspec), Python (pytest/ruff/mypy).
- Selective test generation: unit tests for touched symbols; fast smoke paths; run only affected tests.

## Runtime robustness
- Provider resilience: circuit breakers/health scoring, pre-warmed fallbacks, deterministic failover.
- JSON/tool schema hardening: JSON Schema validation; auto-correct drift; refuse execution on failure.
- Sandbox hardening: memory/vmem/file count limits; tmpdir isolation; prefer firejail/bwrap when present.

## UX and ops
- Human-in-the-loop thresholds: preview/gate when touching many files, CI, secrets, or dependencies.
- Telemetry with outcomes: track success/failure, diff sizes, repair loops, validation types; A/B hooks for planner prompts/variants.
- Encoding robustness: detect/preserve BOM/UTF-16 using x/text/encoding; opt-in if unknown encoding.
- Cost/latency heuristics: predict cost/turns by task; cap exploration; pre-batch read_file of top-K.

## Migration
- Remove Agent v1 and default to Agent v2 (done). Ensure CLI help/docs reflect v2 by default. Clean up v1 code paths where safe.


## E2E test coverage and stability – follow-ups

- Process command tests:
  - The `process` CLI expects a path to a JSON process file, not a freeform prompt. Update `e2e_test_scripts/test_orchestration.sh` and new `test_process_*.sh` scenarios to:
    - Generate a minimal `process.json` in the test workspace (or call `ledit process --create-example process.json` and trim it).
    - Run `ledit process --skip-prompt process.json`.
    - Verify `.ledit/orchestration_state.json` exists and all steps have `"status": "completed"` (instead of checking `.ledit/requirements.json`).
  - Docs alignment:
    - README currently shows `ledit process "..."` with a freeform prompt. Replace with file-based examples or add a supported flag (e.g., `--prompt`) to generate a transient process file from a prompt.

- Agent v2 revision loop robustness (test failures: discover file/edit, multi-file edit, cached workspace):
  - Strengthen review/revision application when no code blocks are parsed or when the LLM produces incomplete diffs.
    - If parser finds 0 blocks, retry once with stricter instructions; then fall back to deterministic micro-edits or AST-based fix where feasible.
    - Preserve previously applied changes when revisions fail; do not regress files to prior state.
  - Auto-fix trivial syntactic regressions detected by review (e.g., extra closing brace in Go) via a language-aware micro-edit pass before re-invoking the LLM.
  - Reduce interactive retries that stall the loop; prefer a bounded, deterministic correction path after N failed revision attempts.

- Self-contained workspace tests:
  - `test_file_modification.sh` assumes `file1.txt` exists; create it explicitly in the test setup to avoid cross-test coupling. Ensure each test seeds its own workspace artifacts.
  - Before issuing `#WORKSPACE` prompts, trigger an initial index if needed (e.g., a no-op `ledit code` run) so `.ledit/workspace.json` exists deterministically.

- New examples to support tests and users:
  - Add minimal `examples/process/*.json` templates for Python CLI, Node Express API, Static site, Go CLI, and Rust lib (single-agent, 1–2 small steps). Point e2e tests to these templates to reduce flakiness and LLM variance.

- Orchestrator validation and UX:
  - Consider writing a concise `.ledit/requirements.json`-like summary from the orchestrator with step statuses to simplify test assertions.
  - Expose `--prompt` to accept inline goals and synthesize a temporary `process.json` (opt-in), matching README convenience while keeping `process_loader` JSON as the canonical path.

