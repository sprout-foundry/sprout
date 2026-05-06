# SP-021: Self-Review Tool

**Status:** ✅ Implemented  
**Dependencies:** SP-001 (Agent Core), SP-004 (Change Tracking)  
**Location:** `pkg/agent/self_review_tool.go`, `pkg/spec/`  
**Size:** ~1,000 lines Go implementation  
**Test Files:** 4 test files (700+ lines total)

## Problem

Agents can introduce scope creep by making changes that were not explicitly requested by the user. This leads to:

1. **Unintended modifications** — Code changes or refactoring that go beyond the user's stated requirements
2. **Drift from objectives** — The agent may add features, refactor unrelated code, or change patterns without user approval
3. **No verification mechanism** — Without a systematic review, changes cannot be validated against the original specification
4. **User distrust** — Users cannot be confident the agent is staying within scope

The agent already has change tracking (SP-004) to record file modifications, but this is passive recording. An active review mechanism is needed to compare tracked changes against a canonical specification extracted from the conversation, ensuring work aligns with user requirements.

## Current State

The Self-Review Tool provides a mechanism for agents to review their own work against a canonical specification extracted from the conversation. It detects scope creep, validates changes, and provides actionable feedback on violations.

### Architecture

```
User or Agent → self_review Tool Call
                  │
                  ├──► Get revision ID (from change tracker or history)
                  │
                  ├──► Checkpoint pending changes (if any)
                  │
                  ├──► Create SpecReviewService
                  │         │
                  │         ├──► SpecExtractor (LLM-based spec extraction)
                  │         └──► ScopeValidator (LLM-based validation)
                  │
                  └──► spec.ReviewTrackedChanges(revisionID, cfg, logger)
                            │
                            ├──► ExtractSpec(conversation, userIntent)
                            │         └──► Returns CanonicalSpec + confidence
                            │
                            ├──► ValidateScope(diff, spec)
                            │         └──► Returns ScopeReviewResult (violations, suggestions)
                            │
                            └──► Returns ChangeReviewResult

Format & Display
        │
        └──► formatSelfReviewResult() → Markdown report
```

### Tool: self_review

| Attribute | Value |
|-----------|-------|
| **Name** | `self_review` |
| **Handler** | `handleSelfReview` |
| **Location** | `pkg/agent/self_review_tool.go` |
| **Size** | 151 lines |

**Parameters:**
- `revision_id` (string, optional) — Revision ID to review. Defaults to the current/most recent revision from the change tracker or history.

**Returns:** Markdown-formatted review report with specification, validation results, violations, and recommendations.

### Review Process

1. **Get Revision ID** — The handler retrieves the revision ID from the agent's change tracker. If no active revision exists, it fetches the most recent revision from history.
2. **Checkpoint Changes** — Before review, any pending in-memory changes are committed to ensure the review sees all tracked work.
3. **Create SpecReviewService** — A service instance is created with the agent's configuration and logger.
4. **Extract Specification** — The service calls `SpecExtractor.ExtractSpec()` with the conversation history and user intent to derive a canonical specification.
5. **Validate Scope** — The service calls `ScopeValidator.ValidateScope()` with the code diff and extracted spec to check for scope violations.
6. **Format Results** — The `ChangeReviewResult` is formatted as a markdown report for the agent to review.

### Spec Extraction

`SpecExtractor` (in `pkg/spec/extractor.go`) extracts a canonical specification from the conversation using LLM analysis.

**CanonicalSpec Structure:**
```go
type CanonicalSpec struct {
    ID           string    // Unique identifier
    CreatedAt    time.Time // When spec was extracted
    UserPrompt   string    // Original user request
    Objective    string    // Clear objective statement
    InScope      []string  // What's included in the task
    OutOfScope   []string  // What's explicitly excluded
    Acceptance   []string  // Acceptance criteria
    Context      string    // Additional context from conversation
    Conversation []Message // Full conversation for reference
}
```

**Extraction Process:**
1. Builds conversation text from message history
2. Constructs a prompt using `SpecExtractionPrompt()` (LLM prompt template)
3. Calls the LLM to extract structured JSON: objective, in-scope items, out-of-scope items, acceptance criteria, context, and confidence score
4. Parses JSON response and creates `CanonicalSpec` with unique ID

**Confidence Score:**
- Float between 0.0 and 1.0
- Self-assessed by the LLM during extraction
- Low confidence (< 0.7) triggers warning recommendations in the review output

### Scope Validation

`ScopeValidator` (in `pkg/spec/validator.go`) validates code changes against the extracted specification using LLM analysis.

**ScopeReviewResult Structure:**
```go
type ScopeReviewResult struct {
    InScope     bool             // Overall pass/fail
    Violations  []ScopeViolation // Specific violations
    Summary     string           // Human-readable summary
    Suggestions []string         // How to fix violations
}
```

**ScopeViolation Structure:**
```go
type ScopeViolation struct {
    File        string // File where violation found
    Line        int    // Line number
    Type        string // "addition", "modification", "removal"
    Severity    string // "critical", "high", "medium", "low"
    Description string // What was added/changed
    Why         string // Why it's out of scope
}
```

**Validation Process:**
1. Marshals the spec to JSON
2. Constructs a prompt using `ScopeValidationPrompt()` with the spec and code diff
3. Calls the LLM to analyze changes against spec boundaries
4. Parses JSON response: in_scope flag, violations list, summary, suggestions
5. Post-processes violations to infer line numbers from diff if missing

**Rate Limiting Handling:**
- If the provider returns 429 (rate limited), the validator fails closed with a "validation unavailable" result
- This prevents silently approving out-of-scope changes when validation cannot run

### Change Review Result

The final `ChangeReviewResult` integrates both spec extraction and scope validation:

```go
type ChangeReviewResult struct {
    RevisionID   string                 // Revision being reviewed
    FilesChanged int                    // Number of files modified
    TotalChanges int                    // Total change count
    SpecResult   *SpecExtractionResult  // Extracted spec + confidence
    ScopeResult  *ScopeReviewResult     // Validation results
    Summary      string                 // Overall summary
}
```

### Self-Review Gate

The self-review gate (`runSelfReviewGate` in `conversation_handler_review.go`) automatically triggers the review at the end of `ProcessQuery` based on configuration and conditions.

**Gate Conditions:**
- Gate must not be skipped by `LEDIT_SKIP_SELF_REVIEW_GATE` environment variable
- Config `self_review_gate_mode` must not be `"off"`
- Active persona must be gate-enabled (`orchestrator`, `coder`, `repo_orchestrator`)
- Tracked changes must exist (changeCount > 0)
- For `"code"` mode: at least one code-like file must be tracked

**Gate Modes:**
- **off** — Gate never runs (default)
- **code** — Gate runs only when code-like files are tracked (`.go`, `.py`, `.rs`, `.java`, etc.)
- **always** — Gate runs for any tracked changes

**Gate Flow:**
1. Check skip conditions (env var, persona, tracked changes)
2. Check code-mode filter (if mode is `"code"`)
3. Verify revision ID exists (error if missing)
4. Call `handleSelfReview` tool
5. Publish result to event bus (for WebUI)
6. Log violations to the agent's output

**Persona Access:**
- `orchestrator` — Gate-enabled, has `self_review` for scope validation
- `coder` — Gate-enabled, has `self_review` for scope validation
- `repo_orchestrator` — Gate-enabled, has `self_review` added for repo-level validation
- `code_reviewer` — Not gate-enabled, but has `self_review` for manual review passes
- Other personas (`general`, `tester`, `debugger`, etc.) — Not gate-enabled, tool access restricted by persona tool allowlists

### Review Output Format

The `formatSelfReviewResult()` function produces a markdown report:

```
## Self-Review Results

**Revision ID**: rev-123
**Files Changed**: 3
**Total Changes**: 5

### Specification

**Objective**: Implement user authentication with JWT
**Confidence**: 85%

**In Scope**:
  - JWT token generation
  - Token validation middleware
  - Login endpoint

**Out of Scope**:
  - OAuth integration
  - Password reset flow

### Scope Validation

[WARN] **Status**: OUT_OF_SCOPE

Changes include OAuth integration which was not in the specification.

**Violations**:

- **[high]** [auth/oauth.go:42]
  - **What**: Added OAuth client configuration
  - **Why**: Not in specification — explicit out-of-scope exclusion

**Suggestions**:

  - Remove OAuth integration to align with scope
  - Or update specification to include OAuth

### Summary

The implementation includes 3 files with 5 changes. One violation was detected: OAuth integration was added despite being explicitly out of scope.

### [WARN] Recommendation

Scope violations were detected. Consider:
1. Removing out-of-scope changes
2. Updating the specification if these changes are intentional
3. Re-running the review after addressing violations
```

### Spec Package Structure

| File | Purpose | Lines |
|------|---------|-------|
| `pkg/spec/service.go` | `SpecReviewService` integrating extraction and validation | 67 |
| `pkg/spec/extractor.go` | `SpecExtractor` — LLM-based spec extraction from conversation | 145 |
| `pkg/spec/validator.go` | `ScopeValidator` — LLM-based scope validation with diff analysis | 209 |
| `pkg/spec/entities.go` | Data entities: `CanonicalSpec`, `ScopeViolation`, etc. | 49 |
| `pkg/spec/prompts.go` | LLM prompt templates for extraction and validation | 173 |
| `pkg/spec/integration.go` | Integration helpers and utilities | 121 |
| `pkg/spec/change_integration.go` | Change tracking integration | 257 |
| `pkg/spec/provider_resolution.go` | LLM provider resolution for review operations | 35 |

### Self-Review Tool Integration

| File | Purpose | Lines |
|------|---------|-------|
| `pkg/agent/self_review_tool.go` | `handleSelfReview` handler and `formatSelfReviewResult` | 151 |
| `pkg/agent/tool_definitions.go` | Tool registration for `self_review` | ~15 (registration block) |
| `pkg/agent/conversation_handler_review.go` | Self-review gate logic (`runSelfReviewGate`) | 117 |

### Test Coverage

| File | Purpose | Lines |
|------|---------|-------|
| `pkg/agent/self_review_tool_test.go` | Unit tests for `formatSelfReviewResult` with all scenarios | 359 |
| `pkg/agent/e2e_self_review_gate_test.go` | E2E tests for gate conditions (persona, mode, env var, etc.) | 325 |
| `pkg/agent/conversation_handler_review_test.go` | Tests for `hasCodeLikeTrackedFiles()` and `isSelfReviewGatePersonaEnabled()` | ~100 |
| `pkg/agent/conversation_handler_gate_test.go` | Additional gate behavior tests | ~100 |

**Test Coverage:**
- Format output with spec results (objective, confidence, in-scope, out-of-scope)
- Format output with scope validation (in-scope vs. out-of-scope)
- Violation formatting (severity, file, line, description, why)
- Suggestion formatting
- Recommendation logic (OK vs. WARN based on violations and confidence)
- Empty lists (no in-scope/out-of-scope items)
- Nil spec/scope results
- Confidence formatting (percentage display)
- Gate skip conditions (persona, tracked changes, env var, mode)
- Code-like file detection (extensions: `.go`, `.py`, `.rs`, `.java`, `.js`, `.ts`, etc.)
- Revision ID validation (error when empty)

## Design Decisions

### LLM-Based Spec Extraction

The spec extraction uses LLM analysis rather than heuristic parsing.

**Rationale:**
- Natural language objectives are ambiguous — LLM can infer intent from conversation context
- Out-of-scope items are often implicit (e.g., "don't use external libraries")
- LLM can capture acceptance criteria and context that heuristic rules would miss
- Confidence score provides a signal for low-quality extractions

**Trade-offs:**
- Slower than heuristic extraction (requires LLM call)
- Dependent on LLM quality — poor models may produce inaccurate specs
- Rate limiting can block extraction (graceful degradation handled)

### LLM-Based Scope Validation

Scope validation uses LLM analysis rather than static analysis tools.

**Rationale:**
- Scope is semantic, not syntactic — a change may be syntactically valid but semantically out of scope
- LLM can understand why a change violates the spec and provide reasoning
- Violation severity can be assessed contextually (critical vs. low priority)
- Suggestions can be generated based on the specific violation

**Trade-offs:**
- Slower than static analysis (requires LLM call)
- May miss subtle violations that static analysis would catch
- Rate limiting can block validation (fails closed to avoid silent approval)

### Fail-Closed Rate Limiting

When rate limiting occurs during validation, the system returns a "validation unavailable" result with `InScope: false`.

**Rationale:**
- Prevents silently approving potentially out-of-scope changes
- Explicit warning informs the user that validation could not run
- Users can retry after rate limits reset
- Maintains security by assuming changes may be out of scope

### Gate Modes (off/code/always)

Three configuration modes provide flexibility in gate enforcement.

**Rationale:**
- `off` — Allows users to disable the gate entirely if not needed
- `code` — Focuses on code changes, ignores documentation-only work
- `always` — Maximum enforcement for strict compliance workflows

**Persona Filtering:**
- Only gate-enabled personas (`orchestrator`, `coder`, `repo_orchestrator`) trigger the gate
- Prevents noise from personas like `general`, `tester`, `researcher`
- Maintains manual tool access for `code_reviewer` without automatic gating

### Confidence Score Warnings

Spec extractions with confidence < 0.7 trigger warning recommendations even when scope validation passes.

**Rationale:**
- Low confidence suggests the LLM is uncertain about the spec
- Users should clarify requirements before proceeding
- Prevents acting on potentially inaccurate specifications
- Provides a signal without blocking completion (unlike violations)

### Change Checkpointing

Before review, pending in-memory changes are committed to ensure all work is visible to the review process.

**Rationale:**
- Change tracker may have uncommitted changes in memory
- Review should see the complete picture of work done
- Prevents missing violations from uncommitted changes
- Commit uses "Self-review checkpoint" message for tracking

## Key Files

| File | Purpose |
|------|---------|
| `pkg/agent/self_review_tool.go` | `handleSelfReview` handler, `formatSelfReviewResult` output formatter |
| `pkg/agent/tool_definitions.go` | Tool registration for `self_review` |
| `pkg/agent/conversation_handler_review.go` | Self-review gate logic (`runSelfReviewGate`, `hasCodeLikeTrackedFiles`, `isSelfReviewGatePersonaEnabled`) |
| `pkg/spec/service.go` | `SpecReviewService` integrating extraction and validation |
| `pkg/spec/extractor.go` | `SpecExtractor` — LLM-based spec extraction from conversation |
| `pkg/spec/validator.go` | `ScopeValidator` — LLM-based scope validation with diff analysis |
| `pkg/spec/entities.go` | Data entities: `CanonicalSpec`, `SpecExtractionResult`, `ScopeReviewResult`, `ScopeViolation` |
| `pkg/spec/prompts.go` | LLM prompt templates for spec extraction and scope validation |
| `pkg/spec/integration.go` | Integration helpers and utilities |
| `pkg/spec/change_integration.go` | Change tracking integration |
| `pkg/spec/provider_resolution.go` | LLM provider resolution for review operations |
| `pkg/agent/self_review_tool_test.go` | Unit tests for output formatting |
| `pkg/agent/e2e_self_review_gate_test.go` | E2E tests for gate conditions |
| `pkg/agent/conversation_handler_review_test.go` | Helper function tests (code detection, persona check) |
| `pkg/agent/conversation_handler_gate_test.go` | Gate behavior tests |

## Success Criteria

| Metric | Target | Actual |
|--------|--------|--------|
| Spec extraction from conversation | ✅ LLM-based extraction | ✅ Implemented |
| Scope validation against spec | ✅ LLM-based validation | ✅ Implemented |
| Violation detection with severity | ✅ Critical/high/medium/low | ✅ Implemented |
| Self-review gate integration | ✅ Automatic triggering at ProcessQuery end | ✅ Implemented |
| Gate modes (off/code/always) | ✅ Configurable behavior | ✅ Implemented |
| Persona filtering | ✅ Only gate-enabled personas trigger gate | ✅ Implemented |
| Confidence scoring | ✅ 0-1 score with low-confidence warnings | ✅ Implemented |
| Rate limiting handling | ✅ Fail-closed with "validation unavailable" | ✅ Implemented |
| Code-like file detection | ✅ Extensions: .go, .py, .rs, .java, .js, .ts, etc. | ✅ Implemented |
| Change checkpointing | ✅ Pending changes committed before review | ✅ Implemented |
| Test coverage | >80% | ✅ 700+ lines of tests |

## Open Questions

None — the feature is fully implemented and tested.

## Future Enhancements

**Potential improvements (not currently planned):**

1. **Suggest spec updates** — When violations are intentional, offer to update the specification automatically
2. **Incremental validation** — Validate changes incrementally during development rather than at the end
3. **Rule-based validation** — Add static analysis rules for common scope violations (e.g., "don't add new dependencies")
4. **Violation history** — Track scope violations across sessions to detect chronic scope creep patterns
5. **Auto-fix suggestions** — Provide code patches for removing out-of-scope changes
6. **Multi-revision diffing** — Compare multiple revisions to detect gradual scope drift over time
