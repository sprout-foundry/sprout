# SP-027: Persistent Context & Conversational Memory

**Status:** ✅ Shipped (all 4 phases complete 2026-06)

Sprout previously relied on the active conversation's message list as the
sole source of context — past sessions were effectively unreachable, and
long-running chats eventually hit context limits. SP-027 added a
persistent embedding-backed conversation store (`pkg/embedding/conversation_store.go`)
that indexes every turn and proactively surfaces relevant past context
into new prompts (`pkg/agent/proactive_context.go`). Phase 3 added drift
detection to flag when the agent's working context has drifted from the
user's actual intent. Phase 4 wired the existing memory system into the
conversation store so memories persist across sessions.

## Key decisions

- **Conversation store is orthogonal to the active message list.** A
  turn is embedded and stored independently of whether it remains in
  the live prompt. `/compact` (and rollups) can drop messages from the
  active list without losing them from the store.
- **Time-decay scoring** for retrieval — older turns fade unless
  semantically relevant. Half-life configurable per workspace.
- **Drift detection is a signal, not an action.** A `DriftNotification`
  surfaces to the user; they decide whether to reset, redirect, or
  proceed. The agent doesn't auto-correct.
- **Memory system integration** uses the conversation store as the
  persistence layer — memories are searchable alongside turns.
- **Hand-off via `CreateSessionWithHandoff()`** lets a new session
  inherit context from a previous one explicitly, not implicitly.

## Artifacts

- code: `pkg/embedding/conversation_store.go` — per-session vector store
- code: `pkg/agent/turn_embedding.go` — `EmbedAndStoreTurn`
- code: `pkg/agent/proactive_context.go` — time-decay retrieval + injection
- code: `pkg/agent/drift_detection.go` — `DriftDetector`
- code: `pkg/agent/memory_embedding.go` — memory persistence integration
- code: `pkg/agent/chat_sessions.go::CreateSessionWithHandoff` — explicit handoff
- UI: `webui/src/components/DriftNotification.tsx`
- config: `PersistentContextConfig` in `config_domain.go`

Full specification archived — see git history for original content.