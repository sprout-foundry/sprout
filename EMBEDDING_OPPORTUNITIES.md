# Embedding Leverage Opportunities — Audited

## What Exists Today

The embedding system currently powers 5 surfaces:

1. **`semantic_search` tool** — Agent searches code index by natural language
2. **Duplicate detection** — Checks new file content against indexed code on every write
3. **`search_memories` tool** — Searches persisted memory entries by meaning
4. **Proactive context injection** — Injects semantically similar past turns into system prompt
5. **Conversation turn persistence** — Every agent turn is embedded and stored in conversation store

### Also already implemented (discovered during audit):
6. **`read_file` semantic augmentation** (`injectSemanticContext` in `tool_handlers_file.go`) — After every file read, queries the code index with the first ~500 chars of the file content and appends "Related code" results (filtered to external files, threshold 0.85, top 5). This runs for `.go`, `.ts`, `.tsx`, `.py` files.

## Audit Results

### Proposal A: `search_files` empty-result → suggest `semantic_search`
**Verdict: DOWNSIZE — conditional hint only**

The LLM already has `semantic_search` in its tool list and can discover it on its own. A flat "try semantic_search" hint is risky because embedding may not be available (model not downloaded, CGO disabled). The hint should only appear when `env.EmbeddingMgr != nil && env.EmbeddingMgr.IsInitialized()`.

If implemented, the change is: in `search_files_handler.go`, when `len(results) == 0` and embedding is available, append "No text matches found. Try `semantic_search` to find code with similar meaning." Otherwise keep the current message.

**Value: Low. Effort: Trivial. Risk: Low with the guard.**

---

### Proposal B: `repo_map` with semantic query filter
**Verdict: DROP — chicken-and-egg problem**

`repo_map` is used at the START of a task to discover files. The embedding index doesn't exist yet at that point (it's built in the background by `AutoBuildWhenReady` after a 3-second delay). Even if the index exists, adding embedding queries to `repo_map` adds seconds of latency to what should be a fast filesystem walk. The agent already has the correct workflow: `repo_map` for structure → `semantic_search` for depth.

---

### Proposal C: Proactive context searches the CODE index too
**Verdict: DROP — code is already discoverable; would crowd out conversation turns**

The whole point of proactive context is surfacing things the agent CAN'T discover on its own — past conversation history from previous sessions. Code is already discoverable via:
- `semantic_search` tool (always available)
- `injectSemanticContext` (runs on every file read)

Adding code to the system prompt would consume the 4000-char context budget, crowding out conversation turns. It would also risk staleness (code changes during a session). The current design is correct: conversation turns in the system prompt, code discovered via tools.

---

### Proposal D: `read_file` augmented with related code
**Verdict: ALREADY IMPLEMENTED**

`injectSemanticContext` in `pkg/agent/tool_handlers_file.go` already does exactly this. After every file read of `.go`/`.ts`/`.tsx`/`.py` files, it queries the code index with the first 500 chars, filters to external files, and appends "Related code" results. The threshold (0.85) and result cap (5) are well-calibrated.

Minor gaps: doesn't cover `.js`, `.jsx`, `.mjs`, `.rs`, `.java`, `.c`. Could be extended but low priority since the primary languages are covered.

---

### Proposal E: `self_review` checks past similar work
**Verdict: DROP — actively harmful**

Surfacing "you tried this before and it failed" without the subsequent resolution context could cause the LLM to reject its own correct work. Past relevant turns are already injected at session start via `InjectProactiveContext`. Adding them again in `self_review` is redundant and adds latency to a tool that should be fast.

---

### Proposal F: `edit_file`/`write_file` suggests related files
**Verdict: DROP — false positives actively harmful**

Code index similarity doesn't distinguish "this file calls the function you just edited" from "this file has a similarly-named function." Both score high. Suggesting unrelated files could trigger cascade editing — the agent "fixes" files that don't need it, introducing bugs. The agent's own reasoning (supported by `semantic_search`, `repo_map`, and `injectSemanticContext`) is the right mechanism.

---

## Bottom Line

**5 of 6 proposals were either already implemented, redundant, or actively harmful.** The embedding system is already well-leveraged — it augments file reads, checks writes for duplicates, powers semantic search, and injects past context. The only actionable item is:

- **Proposal A (downsized)**: Add a conditional hint to `search_files` when embedding is available. Trivial effort, low but real value.

The codebase already has the correct architecture: embeddings serve as a **passive augmentation layer** (injected into tool output, system prompt) rather than an active suggestion engine. This is the right design — the LLM's own reasoning decides what to act on, while embeddings quietly enrich the information available to it.
