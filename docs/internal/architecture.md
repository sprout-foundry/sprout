# Internal Architecture Notes

## Context Architecture (SP-066)

Three distinct operations manage conversation context:

1. **Substitute** (free, every prompt build) — replaces checkpoint ranges with summaries. No LLM call.
2. **Rollup** (LLM call, amortized) — folds oldest checkpoints into higher-level summaries.
3. **Compact** (LLM call, explicit) — `/compact` wipes active checkpoint list, replaces history head with recap.

Key invariants:
- `/compact` must not skip when checkpoints exist.
- Don't gate substitution on headroom.
- Don't let rollups consume the recency window.

## Security Risk Classification

Shell commands classified by behavior-based heuristic (`pkg/agent_tools/security_classifier.go` + `shell_patterns.go`).

**Do NOT** reintroduce name-based whitelist or default-CAUTION fallback. When adding coverage for new risky behavior, add patterns to `isCautionPattern` / `isDangerousPattern` in `shell_patterns.go`.

## Change Tracking

`ChangeTracker` (`pkg/agent/change_tracking*.go`) records every file mutation. Trust `files_modified` in subagent returns. Git is authoritative for committed content (SP-077).
