# SP-029: Monolith Decomposition — File Size Reduction

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** High (technical debt; blocks parallel work on hot files)
**Depends on:** SP-028 (test stabilization — need green baseline before refactoring)

## Problem

The project's own `CLAUDE.md` sets a 500-LOC target and an 800-LOC review threshold. Nine Go files exceed 1000 LOC, three exceed 1300, and `pkg/configuration/config.go` sits at **1895 LOC**. These files are merge-conflict hotspots, are hard to unit-test in isolation, and they signal to new contributors that the rule doesn't apply.

| File | LOC | Primary cause |
|------|-----|---------------|
| `pkg/configuration/config.go` | 1895 | Types + risk scoring + subagents + skills + paths + load/save + accessors + validation all in one |
| `pkg/wasmshell/commands.go` | 1633 | ~25 shell builtins in one file |
| `pkg/webcontent/browser_rod.go` | 1335 | Session lifecycle + actions + capture + GPU probing |
| `pkg/agent/conversation_optimizer.go` | 1319 | Optimizer + summary builders + file tracking + shell tracking |
| `pkg/agent/tool_handlers_subagent.go` | 1318 | `handleRunSubagent` + `handleRunParallelSubagents` + batching + utilities |
| `pkg/agent_providers/generic_provider.go` | 1276 | HTTP client + error formatting + request building + model discovery + retry logic |
| `pkg/agent/seed_tool_registry.go` | 1223 | Tool registry seeding |
| `pkg/lsp/semantic/go_adapter.go` | 1188 | Go LSP adapter |
| `pkg/agent/scripted_client.go` | 1068 | Test-double client |

**Total LOC across these 9 files: 12,255.** Target after decomposition: ~30 files averaging 300–400 LOC each. **No behaviour change** — this is a pure split.

## Goals / Non-Goals

**Goals**
- Every file in the decomposed set ≤ 500 LOC after the split.
- Zero behaviour changes; full test suite passes before and after each split (relies on SP-028 being done first).
- Preserve git blame where possible (use `git mv` style splits + targeted moves; avoid mass rewrites).

**Non-Goals**
- Renaming exported symbols.
- Changing the public API of any package.
- Introducing new abstractions or interfaces "while we're in there." This is a split, not a rewrite.
- The full agent-package decomposition (238 files into clearer subdomains) — that's a separate, larger conversation.

## Decomposition Plan

### A. `pkg/configuration/config.go` (1895 → ~8 files × ~240 LOC)

| New file | Contents | Source lines |
|----------|----------|--------------|
| `config.go` | `Config` struct, `NewConfig`, package doc | 34–139, 532–591 |
| `config_types.go` | `APITimeoutConfig`, `APIKeys`, `CustomProviderConfig`, `EmbeddingIndexConfig`, `PersistentContextConfig`, `Skill`, `APIKeys` methods | 140–178, 457–531 |
| `config_risk.go` | `RiskLevel`, `AutoApproveRules`, `DefaultAutoApproveRules`, `EvaluateOperationRisk`, `containsForceFlag`, `categorizeCommand`, `categorizeGitCommand`, `matchesRiskPattern`, `firstFieldAfter` | 179–456 |
| `config_subagents.go` | `SubagentType`, `GetAutoApproveRules`, `defaultSubagentTypes`, `mergeMissingDefaultSubagentTypes`, `mergeLegacyStructuredToolsIntoPersonaAllowlists`, `hasAnyTool`, `hasTool`, `normalizePersonaID`, `GetSubagentType`, `GetSubagentTypeProvider`, `GetSubagentTypeModel` | 216–456, 1266–1530, 1760–1778 |
| `config_skills.go` | `defaultSkills`, `mergeMissingDefaultSkills`, `discoverProjectSkills`, `parseSkillFrontMatter`, `GetSkill`, `GetSkillPath`, `GetAllEnabledSkills` | 1531–1814 |
| `config_paths.go` | `GetConfigDir`, `getDefaultConfigDir`, `GetConfigPath`, `GetWorkspaceConfigPath`, `IsWorkspaceConfigPresent` | 592–636 |
| `config_persistence.go` | `MergeConfig`, `cloneConfig`, `Load`, `Save`, `SaveToDir` | 637–1078 |
| `config_accessors.go` | All `Get*ForProvider`/`Set*ForProvider`/`Get*Provider`/`Get*Model` methods, `GetMCPTimeout`, `NormalizeSelfReviewGateMode`, role-based accessors | 1079–1265, 1835–end |
| `config_validate.go` | `Validate` | 1815–1834 |

### B. `pkg/wasmshell/commands.go` (1633 → ~5 files)

| New file | Contents |
|----------|----------|
| `commands.go` | `Env`, `CmdResult`, `DirEntry`, `CommandFunc`, command registry `init()` |
| `commands_fs.go` | `ls`, `cd`, `pwd`, `cat`, `mkdir`, `rm`, `rmdir`, `cp`, `mv`, `touch`, `find`, `tree` |
| `commands_text.go` | `head`, `tail`, `wc`, `grep`, `sort`, `echo` |
| `commands_env.go` | `env`, `export`, `whoami`, `which`, `date`, `clear`, `help` |
| `commands_util.go` | `copyPath`, `copyFile`, `humanizeSize`, `sortedKeys` |

### C. `pkg/webcontent/browser_rod.go` (1335 → ~5 files)

| New file | Contents |
|----------|----------|
| `browser_rod.go` | `rodRenderer`, `NewBrowserRenderer`, `connect`, `launchAndProbe`, `openIncognitoPage`, `Close` |
| `browser_rod_session.go` | `browserSession`, `acquireSession`, `closeSessionByID`, `applyViewportAndUA`, `newBrowserSessionID` |
| `browser_rod_actions.go` | `RenderPage`, `Screenshot`, `CaptureDOM`, `Run`, `executeBrowseStep`, `waitForSelectorIfNeeded`, `requireElement`, `pressPageKey`, `lookupInputKey` |
| `browser_rod_capture.go` | `captureSelectors`, `captureStorageMap`, `captureBrowserDiagnostics`, `detectCORSIssues`, `markCORSBlockedRequests`, `evalToJSONString`, truncation helpers, `captureCurrentPageScreenshot` |
| `browser_rod_gpu.go` | `gpuProbeDone`, `gpuProbeFailed`, `markGPUProbe`, `resetGPUProbe`, `probeGPUSupport`, `systemBrowserPaths`, `getNavigationTimeout` |

### D. `pkg/agent/conversation_optimizer.go` (1319 → ~4 files)

| New file | Contents |
|----------|----------|
| `conversation_optimizer.go` | `ConversationOptimizer` struct, `New…`, `OptimizeConversation`, `CompactConversation`, `compactConversationLayered`, `compactionAnchorEnd`, `adjustCompactionBoundary`, configuration methods |
| `conversation_optimizer_summary.go` | `buildActionableSummary`, `buildLLMCompactionSummary*`, `buildGoCompactionSummary`, `mergeLayeredSummaries`, `normalizeSummaryEntry`, `wrapCompactionSummary*`, `extractCompactionContext`, `isCheckpointSummary`, `looksLikeDurableAssistantState` |
| `conversation_optimizer_files.go` | `FileReadRecord`, `isRedundantFileRead`, `trackFileRead`, `extractFilePath`, `extractFileContent`, `hashContent`, `createFileReadSummary`, `InvalidateFile`, `getTrackedFilePaths` |
| `conversation_optimizer_shell.go` | `ShellCommandRecord`, `isRedundantShellCommand`, `trackShellCommand`, `extractShellCommand`, `extractShellOutput`, `isTransientCommand`, `createShellCommandSummary`, `getTrackedCommands` |

### E. `pkg/agent/tool_handlers_subagent.go` (1318 → ~4 files)

| New file | Contents |
|----------|----------|
| `tool_handlers_subagent.go` | `handleRunSubagent` (the main one) |
| `tool_handlers_subagent_parallel.go` | `handleRunParallelSubagents` |
| `tool_handlers_subagent_batch.go` | `subagentBatchBuffer`, `flushSubagentBatch`, `cleanupSubagentBatch`, `flushAllSubagentBuffers`, `publishSubagentActivity` |
| `tool_handlers_subagent_utils.go` | `extractSubagentSummary`, `warnSubagentFallback`, `truncateString`, `stripAnsiCodes`, `isPathInWorkspace`, `isPathInTmp`, `commonParent` |

### F. `pkg/agent_providers/generic_provider.go` (1276 → ~5 files)

| New file | Contents |
|----------|----------|
| `generic_provider.go` | `GenericProvider`, `NewGenericProvider`, `SendChatRequest`, `SendChatRequestStream`, lifecycle accessors (`SetDebug`, `SetModel`, `RefreshAPIKey`, `GetModel`, `GetProvider`, `GetModelContextLimit`, `CheckConnection`, TPS getters) |
| `generic_provider_errors.go` | `formatProviderHTTPError`, `summarizeProviderHTTPError`, `extractProviderJSONErrorMessage`, `extractProviderJSONErrorField`, `looksLikeProviderHTMLErrorPage`, `summarizeProviderHTMLErrorPage`, `extractProviderHTMLTitle`, `limitProviderErrorText` |
| `generic_provider_request.go` | `buildChatRequest`, `applyReasoningEffort`, `applyDisableThinking`, `applyModelSpecificSettings`, `buildMultiModalContent`, `buildImageURL`, `getModelCompletionLimit` |
| `generic_provider_models.go` | `ListModels`, `fallbackToConfigOrCurrent`, `ensureModel`, `SupportsVision`, `GetVisionModel`, `SendVisionRequest`, `modelInfoHasVisionTag` |
| `generic_provider_retry.go` | `shouldRetryWithMaxCompletionTokens`, `rewriteMaxTokensToMaxCompletionTokens`, `tryMaxCompletionTokensRetry` |

### G. Remaining three files

These need a focused investigation pass before final partition. Decomposition sketch:

- **`pkg/agent/seed_tool_registry.go` (1223)** — split per registry section (tool definitions vs. dispatcher wiring vs. handler bindings). Likely a 3-way split.
- **`pkg/lsp/semantic/go_adapter.go` (1188)** — split by LSP capability area (definitions, references, completions, diagnostics, document symbols). Probably 4 files.
- **`pkg/agent/scripted_client.go` (1068)** — split scripting DSL from playback engine and the response builders. Likely 3 files.

For each, the split is **decided in a small PR** after rereading the file in full, not in this spec.

## Mechanics — How to split safely

For every file split:

1. **Confirm green baseline:** `make test-all` passes before starting (requires SP-028).
2. **Create the new files as empty (with package decl + license header).** Commit. This makes the next commit a pure move.
3. **Move declarations using cut/paste in IDE or `git mv` for whole files.** Do not edit signatures, types, or bodies during the move.
4. **Run `go build ./...` and `gofmt -l .` after each move.** Fix imports.
5. **Run the package's test suite** (`go test ./pkg/configuration/...` etc.) after each move.
6. **Open one PR per top-level entry (A through G).** Reviewers can validate "no semantic change" in isolation.

## Implementation Phases

### Phase 1: Smallest blast radius first (Day 1-3)
- [ ] E. `tool_handlers_subagent.go` — already has clean handler-per-function boundaries
- [ ] F. `generic_provider.go` — well-isolated helper groupings

### Phase 2: Configuration and optimizer (Day 4-7)
- [ ] A. `config.go` — highest impact, most touched file
- [ ] D. `conversation_optimizer.go`

### Phase 3: Surface area packages (Day 8-10)
- [ ] B. `wasmshell/commands.go`
- [ ] C. `webcontent/browser_rod.go`

### Phase 4: Investigate-then-split (Day 11-14)
- [ ] G1. `seed_tool_registry.go`
- [ ] G2. `lsp/semantic/go_adapter.go`
- [ ] G3. `scripted_client.go`

## Success Criteria

| Metric | Target |
|--------|--------|
| Files in `pkg/` over 800 LOC | 0 (down from 9) |
| Files in `pkg/` over 1000 LOC | 0 (down from 9) |
| Median Go file size in touched packages | ≤ 400 LOC |
| Behaviour-changing diffs per split PR | 0 |
| Test suite pass | Same outcome before/after each PR |

## Risks

- **Cross-file dependencies surface during the split** (private helpers used by both halves). Mitigation: when discovered, move the helper to whichever new file has more callers; if even, keep it in the "root" file (e.g. `config.go`).
- **Blame-history fragmentation.** Reviewers lose `git blame` continuity. Mitigation: use `git log --follow` and keep splits to pure moves (no edits) so `-M`/`-C` heuristics work.
- **Merge conflicts with in-flight work** (esp. on `config.go`). Mitigation: do Phase 2A in a single, fast PR; coordinate with anyone editing config that week.

## Files Reference

See decomposition tables above. New files are created; old files are slimmed down to their "primary" residual contents. No file is deleted in this spec.
