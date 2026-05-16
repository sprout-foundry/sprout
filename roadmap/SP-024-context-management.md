# SP-024: Context Management — File Read Optimization

**Status:** ✅ Phase 1-3, Phase 4 complete (Phase 2 deferred; tree-sitter in SP-025)  
**Date:** 2026-05-15

## Problem

The `read_file` tool has a default limit of 80KB (~20,000 tokens), consuming ~15% of a 128K context window for a single file read. This is significantly higher than competing tools and leads to unnecessary context bloat, especially in multi-step agent loops where context accumulates quadratically.

**Current limits:**
- Default read: 80KB (~20K tokens) with head+tail truncation
- Line range read: 10MB (~2.5M tokens) — safety valve, not expected use
- Configurable via `READ_FILE_MAX_BYTES` env var

**Competitor comparison:**
| Tool | Per-File Limit | Strategy |
|---|---|---|
| Claude Code (Desktop) | 10K tokens | Error if exceeded; manual chunking |
| Cursor | ~500 lines | Chunked reads + semantic indexing |
| Aider | No hard limit | Repo map (1K tokens) + graph ranking |
| Sprout (current) | 20K tokens | Head+tail truncation |

## Research Findings

**Token costs are quadratic.** Every LLM API call re-bills the entire conversation history. A 10-step loop with 8K tokens per tool output generates 472,500 total input tokens vs. 9,000 for a single-pass — a 43× multiplier.

**Key strategies from research:**
1. **Lower per-file limits** — 500-line / ~10K token sweet spot
2. **Progressive disclosure** — Directory structure first, drill into files on demand
3. **Repo maps** — Lightweight AST overview (signatures only) before reading files
4. **Semantic search before reading** — Find relevant sections, then read only those lines
5. **Observation masking** — Replace old tool outputs with placeholders (52% cheaper, 2.6% more effective than summarization)

## Solution

### Phase 1: Reduce Default Limits (Low Risk)

**File:** `pkg/agent_tools/read.go`

- Lower `defaultMaxFileSize` from 80KB to **32KB** (~8K tokens)
- Lower `lineRangeMaxSize` from 10MB to **2MB** (~500K tokens)
- Keep `READ_FILE_MAX_BYTES` env var override

**Rationale:** 32KB aligns with the ~500-line community sweet spot and reduces worst-case context usage by 60%.

### Phase 2: Enforce Line Ranges for Large Files

**File:** `pkg/agent_tools/read.go`

When a file exceeds the default limit and no `view_range` is specified:
- **Current:** Auto-truncate with head+tail (60/40 split)
- **Proposed:** Return error suggesting `view_range` parameter

This forces agents to be intentional about reading large files rather than silently getting partial content.

### Phase 3: Repo Map Tool (Future)

Add a lightweight `repo_map` tool that generates an AST-based overview of the codebase:
- Shows file paths, function signatures, type declarations
- Budget: ~1,024 tokens (following Aider's approach)
- Enables agents to understand codebase structure before reading files
- Uses graph ranking to select most relevant portions

### Phase 4: Observation Masking ✅

Replace old tool outputs with placeholders after they've been processed:
- `[PREVIOUS RESULT: read_file, 12345 chars, 450 lines]`
- More effective than summarization per JetBrains research
- Prevents quadratic context accumulation

## Implementation Plan

1. [x] Lower `defaultMaxFileSize` to 32KB
2. [x] Lower `lineRangeMaxSize` to 2MB
3. [x] Update truncation warning message to reflect new limits
4. [x] Add `repo_map` tool with line numbers (see SP-025 for tree-sitter upgrade)
5. [ ] (Future) Error on large files instead of auto-truncating
6. [x] Add observation masking

## Risks

- **Breaking existing workflows:** Agents that rely on reading large files will need to use `view_range`. Mitigation: env var override available.
- **Head+tail truncation may lose context:** For files where middle content matters, the 60/40 split may not capture what's needed. Mitigation: encourage `view_range` usage.
