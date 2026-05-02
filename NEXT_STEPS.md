# NEXT_STEPS.md — Issues, Gaps & Improvement Opportunities

This document captures findings from a deep audit of the codebase that are **not already tracked** in `TODO.md` or the roadmap specs. The 18 open TODO items (SP-11 terminal features + SP-012 UX polish) are not duplicated here.

---

## Completed ✅

| Item | Description | Commit |
|------|-------------|--------|
| §1.1 | Renumber SP-008 terminal sessions → SP-014, update README | `97a9952` |
| §2.1 | Cap `logs[]` at 500 entries in App.tsx | `97a9952` |
| §2.3 | Per-panel ErrorBoundaries (Sidebar, Editor, ContextPanel, Terminal) | `97a9952` |
| §2.4 | Add sync.RWMutex to ConversationOptimizer | `97a9952` |
| §2.5 | Derive tool timeout from agent interrupt context (Ctrl+C propagation) | `97a9952` |
| §3.3 | Redact secrets from shell output via OutputRedactor **[BEHAVIOR]** | `97a9952` |
| §3.4 | write_file preserves original file permissions **[BEHAVIOR]** | `97a9952` |
| §3.2 | WebSocket ReadLimit already set at 512KB (was already done) | — |
| §5.3 | write_file returns summary with preview instead of full echo **[BEHAVIOR]** | `97a9952` |
| §6.3 | Cloud adapter spec → SP-015 roadmap spec | `97a9952` |
| §6.5 | Universal tool result truncation at 50K chars **[BEHAVIOR]** | `97a9952` |

---

## 1. Roadmap Hygiene

### 1.2 Roadmap Missing Existing Features
These implemented features have **no roadmap spec coverage**:

| Feature | Location | Lines |
|---------|----------|-------|
| Multi-chat sessions (tabbed conversations) | `pkg/webui/client_context.go` | 23 references |
| Context compaction / conversation optimizer | `pkg/agent/conversation_optimizer.go` | 1,204 |
| Trace/dataset mode (JSONL export) | `pkg/trace/` | ~1,300 non-test |
| Scripted client (deterministic E2E) | `pkg/agent/scripted_client.go` | 1,093 |
| Memory system (persist across sessions) | `pkg/agent_tools/memory.go` + agent tools | ~400 |
| 27 registered tools | `pkg/agent/tool_definitions.go` | 27 tools |
| Background shell execution | `pkg/webui/terminal_background.go` | ~180 |
| Agent session attach (hidden → visible) | `pkg/webui/terminal_agent_exec.go` | ~290 |

**Action:** Either create new specs or extend existing ones to document these.

### 1.3 TODO.md Has ~25 Duplicates
The file has back-to-back duplicate entries across Editor Tier 3/4, SP-010, Credentials, Cloud, and AGENT-TERM sections.

**Action:** Deduplicate TODO.md in the next cleanup pass.

---

## 2. Reliability

### 2.2 `messages[]` Has No Size Bound Within a Chat Session
**File:** `webui/src/App.tsx`
**Impact:** While `Chat.tsx` uses `react-virtuoso` for rendering, the underlying `messages[]` array still holds every message. Very long sessions (100+ tool calls) accumulate significant memory.

**Fix:** Implement message windowing — keep the last N messages in state, with a separate persistence layer for history.

---

## 3. Security & Safety

### 3.1 [RISK] WebSocket CheckOrigin — Already Mitigated
The `upgrader.CheckOrigin` already validates against localhost/loopback and `SPROUT_ALLOWED_ORIGINS`. The original concern was unfounded — the code is correct.

---

## 4. Frontend Architecture

### 4.1 [TECH] App.tsx Is a 2,376-Line God Component
**File:** `webui/src/App.tsx`
**Impact:** All state, event handling, and rendering logic in one file.

**Fix:** Extract state management into a reducer or context. Extract event handling into a dedicated `useEventHandler` hook. Target: App.tsx under 500 lines.

### 4.2 [TECH] 10+ Components Over 500 Lines
| Component | Lines |
|-----------|-------|
| SettingsPanel.tsx | 2,019 |
| LocationSwitcher.tsx | 1,885 |
| ContextPanel.tsx | 1,829 |
| Chat.tsx | 760 |

**Fix:** Priority targets are SettingsPanel, LocationSwitcher, and ContextPanel.

### 4.3 [TECH] 30+ `any` Types in Critical Paths
**Fix:** Define proper TypeScript interfaces for all event types. Enable `no-explicit-any` as `error` in ESLint.

### 4.4 [TECH] Duplicate Type Definitions Between webui and packages/ui
**Fix:** Define shared types in `packages/ui/src/types/` and import from webui.

### 4.5 [TECH] ESLint Rules All Set to `warn`
**Fix:** Promote critical rules to `error`: `no-explicit-any`, `react-hooks/exhaustive-deps`.

---

## 5. Go Backend Architecture

### 5.1 [TECH] Top Oversized Files Need Splitting

| File | Lines |
|------|-------|
| `pkg/configuration/config.go` | 1,421 |
| `pkg/webui/websocket.go` | 1,266 |
| `pkg/agent/tool_handlers_subagent.go` | 1,232 |
| `pkg/webui/api_files.go` | 1,235 |
| `pkg/agent/conversation_optimizer.go` | 1,204 |
| `pkg/agent/scripted_client.go` | 1,093 |

**Target:** Under 500 lines per file.

### 5.2 [TECH] `AskUser` Tool Blocks on stdin — Broken in WebUI Mode
**File:** `pkg/agent_tools/ask_user.go`
**Impact:** In WebUI mode, reads from stdin (which is `/dev/null` in daemon mode). The tool hangs until timeout.

**Fix:** Route through the event bus + approval manager pattern.

### 5.4 [TECH] No Proactive Rate Limiting
**Fix:** Add a simple token-bucket rate limiter per provider.

### 5.5 [TECH] MCP Client Has No Reconnection
**Fix:** Add exponential backoff reconnection with health check pings.

### 5.6 [TECH] Global Env Mutation in Configuration Manager
**Fix:** Pass paths explicitly through function arguments instead of mutating the process environment.

---

## 6. Missing Functionality (Not in Roadmap or TODO)

### 6.1 [GAP] Agent Memory System Has No Roadmap Spec
### 6.2 [GAP] Multi-Chat Sessions Have No Roadmap Spec
### 6.4 [GAP] Trace/Dataset Mode Has No Roadmap Spec
### 6.6 [GAP] No `self_review` Tool in Roadmap

**Action:** Create specs or extend existing ones for each of these.

---

## 7. Test Coverage Gaps

### 7.1 ~170 Go Source Files Have Zero Test Coverage
### 7.2 `packages/ui` Has Zero Test Coverage
### 7.3 `webui/src/` Has 262 Source Files, 11 Test Files (~4% coverage)

---

## 8. Proposed Priority Order

**Short-term (next sprint):**
1. §4.1 — Begin App.tsx decomposition
2. §5.2 — Fix AskUser tool for WebUI mode
3. §1.2 — Create specs for undocumented features (cloud adapter, memory, multi-chat, trace)

**Medium-term (next cycle):**
4. §4.2 — Decompose oversized frontend components
5. §5.1 — Split oversized Go files
6. §5.4 — Add proactive rate limiting
7. §5.5 — Add MCP reconnection
8. §4.3 — Eliminate `any` types

**Longer-term (technical health):**
9. §7 — Improve test coverage across all packages
10. §5.6 — Remove global env mutation
11. §4.4 — Deduplicate types between webui and packages/ui
12. §4.5 — Promote ESLint rules to `error`
13. §1.3 — Deduplicate TODO.md
