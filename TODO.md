# Roadmap: Post-Agent v2 Enhancements

Prioritized best-practice improvements for the Agent → Code Editing workflow.

## Editing fidelity and safety
- AST-backed edits via tree-sitter across Go/TS/JS/Python/Ruby/PHP/Rust/Java: select precise spans, emit minimal diffs.
- Three-way, context-aware patch apply: generate unified diffs with generous context; detect/rebase conflicts.
- Idempotent micro-edits: avoid duplicating insertions; check AST presence/markers before applying.
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


