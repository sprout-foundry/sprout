# Conversation Flow

Describes the conversation loop implemented in `core/conversation.go` and its supporting modules.

---

## ProcessQuery Main Loop

Entry: `Agent.Run(ctx, query)` → `ConversationHandler.ProcessQuery()` → `runLoop()`.

1. **Reset** — Clear streaming buffers, reset per-query counters (continuation, blank, content-filter retry).
2. **Add user message** — Append the query to state.
3. **Publish `query_started`** — Event with query text and model name.
4. **Loop** (bounded by `MaxIterations`):
   a. **Context check** — Return `ErrInterrupted` if cancelled.
   b. **Prepare messages** — Build message list from state + transient messages (steer, continuation prompts).
   c. **Token estimate** — Call `provider.EstimateTokens()`.
   d. **OnIteration callback** — Fire if configured (panic-safe).
   e. **Compaction** — If token estimate exceeds context window, compact messages and re-estimate. Publish `compaction` event.
   f. **Compute max_tokens** — From `Options.MaxTokens` or derived from context window minus prompt tokens (with 256-token safety buffer).
   g. **Chat call** — Call LLM via `chatFn` (with retry/backoff for transient/rate-limit errors; fail fast on auth/context-overflow).
   h. **Zero-choice guard** — Return `ErrZeroChoices` if provider returns no choices.
   i. **Add assistant message to state** — Before finish-reason dispatch, so continuation paths have it in history.
   j. **Finish reason dispatch** — See section below.
   k. **Fallback parser** — If no structured tool calls and content has tool-call patterns, extract and inject calls.
   l. **No tool calls?** — Check for injected input, truncation, then break loop (set `turnCompleted = true`).
   m. **Normalize tool calls** — Run through `ToolCallNormalizer`. If all calls dropped as malformed, ask model to re-emit.
   n. **Sync to state** — Write normalized assistant message to state.
   o. **Reset counters** — Valid tool calls represent progress; reset continuation, blank, and tentative counters.
   p. **Execute tools** — Publish `tool_start` events, call `executor.Execute()`, publish `tool_end` events, add result messages to state.
   q. **Loop again**.
5. **Finalize** — Extract final content, record checkpoint, publish `query_completed`.

### Chat Retry (ProcessQuery only)

`ProcessQuery` wraps each LLM call in a retry loop with exponential backoff (`RetryConfig`). Transient and rate-limit errors are retried; auth and context-overflow errors fail immediately. Each retry attempt publishes an `error` event.

---

## Streaming Path

Entry: `Agent.RunStream(ctx, query)` → `ProcessQueryStream()` → `runLoop()`.

Identical to `ProcessQuery` except the `chatFn` uses `provider.ChatStream()` with an `AgentStreamHandler`.

**AgentStreamHandler** (`core/streaming.go`):
- **`OnContent(content)`** — Writes to content buffer, publishes `stream_chunk` (text) + `agent_message` via `OutputManager.PublishOutput()`, calls flush callback.
- **`OnReasoning(reasoning)`** — Writes to reasoning buffer, publishes `stream_chunk` (reasoning), calls flush callback.
- **`OnDone(resp)`** — Fills in what the `ChatResponse` left out from what this one stream delivered: `Content` when the response has none, and `ReasoningContent` from the streamed reasoning. Each model call gets its own handler, so a later call in the same query never inherits an earlier call's text, and reasoning never lands in `Content`. Records token usage, publishes `metrics_update`.
- **`OnError(err)`** — Publishes `error` event.

After `OnDone`, `runLoop` continues the same dispatch/normalize/execute cycle as the non-streaming path.

---

## Fallback Parser

Defined in `core/fallback_parser.go`. Extracts tool calls from malformed LLM responses that lack structured `tool_calls`.

### Trigger (three-tier confidence)

- **Tier 1 (strong):** Single match of code fences (` ``` `), XML tags (`<function=`, `<tool=`, `<tool_use>`), or quoted JSON keys (`"tool_calls"`, `"arguments"`, `"function":`).
- **Tier 2a (weak):** Two or more weak markers (`name:`, `function:`, `tool:`, `arguments`, `args:`, `input:`, `parameters:`, `params:`).
- **Tier 2b (mixed):** One weak marker plus a JSON structure marker (`{"` or `["`).

### Extraction Strategies (7 total)

1. **JSON code fences** — Extracts ` ```json ` blocks.
2. **Bare JSON** — Finds top-level JSON objects/arrays in content.
3. **XML blocks** — Extracts `<function>`, `<tool>`, `<tool_use>` tags.
4. **Function-name patterns** — Matches `function_name:` / `name:` followed by arguments.
5. **Named tool blocks** — Matches `tool_name:` / `tool:` patterns.
6. **Tool blocks** — Extracts `<tool>` blocks with nested content.
7. **Tool_use blocks** — Extracts `<tool_use>` blocks (Anthropic format).

### Normalization

- Validates tool name against known tools (if configured).
- Validates arguments are valid JSON.
- Canonicalizes arguments to compact form.
- Deduplicates by name + canonicalized arguments.
- Generates synthetic ID if missing (`fallback_{name}_{nanotime}`).
- Forces `Type` to `"function"`.

### Content Cleaning

Extracted blocks are removed from the original content; remaining text is whitespace-normalized.

---

## Response Validation

Defined in `core/response_validator.go`. Two public methods serve different purposes.

### LooksTruncated

Used in the continuation loop. Returns true if:
- Trailing `...` (ellipsis)
- Abrupt ending (ends with `,` or `-`, or no punctuation on non-code/URL text)
- Unclosed code blocks (odd number of ` ``` ` markers)

Does **not** include the shortness heuristic — short but complete answers (e.g., "Done.") should not trigger retry.

### IsIncomplete

Superset of `LooksTruncated` plus the shortness check (<10 words, excluding known complete answers like "done", "yes", "error:").

### LooksLikeTentativePostToolResponse

Detects planning-language responses after tool execution. Returns true if:
- Under 40 words, AND
- Starts with a planning prefix (e.g., "let me check", "i'll start by", "i need to look", "first, let me", etc.)

Used to reject responses where the model plans instead of acting on tool results.

---

## Finish Reason Dispatch

After the LLM returns, the first choice's `finish_reason` determines the path:

| Reason | Behavior |
|--------|----------|
| **`length`** | Truncated response. Continue up to `maxContinuations` (3) with "Please continue..." prompt. Force-finalize if budget exhausted. |
| **`content_filter`** | Provider safety filter. Retry once with "Please rephrase..." prompt. On second occurrence, return `ContentFilteredError`. |
| **`stop`** | Normal completion. Check: (1) blank/repetitive content → send reminder or return `BlankResponseError` after 2 consecutive; (2) `LooksTruncated` → ask for final answer; (3) `LooksLikeTentativePostToolResponse` after tool results → reject up to 2 times, then accept. Fall through to tool-call/completion logic. |
| **`tool_calls`** or **`""`** | Fall through to tool execution. |
| **unknown** | Treat as incomplete. Continue up to `maxContinuations` with "Please continue." prompt. Force-finalize if budget exhausted. |

---

## Tool Call Normalizer

Defined in `core/tool_call_normalizer.go`. Cleans structured tool calls before execution.

### Steps (per call)

1. **Strip channel suffix** — Removes `<|channel|>N` from tool names.
2. **Generate missing ID** — Creates `call_{name}_{nanotime}_{seq}` if ID is empty.
3. **Repair JSON arguments** — If valid, canonicalize to compact form. If invalid, try: remove trailing commas, wrap bare key-value in braces. If unrepairable, return as-is (caller drops the call).
4. **Normalize type** — Force `Type` to `"function"`.

### Deduplication

After individual normalization, deduplicates by `ID + arguments` (first occurrence wins).

---

## Blank / Repetitive Iteration Detection

- **Blank:** Content is empty or whitespace-only.
- **Repetitive:** Content is highly similar to the previous assistant message of the same run (exact match after normalization, or ≥80% word overlap with ≥10 overlapping words). Earlier turns do not count — a turn may rightly repeat an earlier answer.
- **Counter:** `consecutiveBlank` tracks consecutive blank/repetitive responses. Threshold is 2.
- **First occurrence:** Send reminder message, loop again.
- **Second occurrence:** Return `BlankResponseError`.

---

## ANSI Sanitization

Defined in `core/conversation.go`. Strips ANSI escape sequences (CSI, OSC, set-charset, device control) from content before it enters the conversation. Prevents terminal formatting codes from polluting LLM context when they leak through tool results.

---

## Finalize

Defined in `core/finalize.go`. Called when the loop exits.

1. **Extract final content** — Walk messages backward to find the last assistant message.
2. **Record turn checkpoint** — If `turnCompleted` is true, call `RecordTurnCheckpointAsync()` to build a `TurnCheckpoint` summary asynchronously (non-blocking). Fires `OnCheckpoint` callback in its own goroutine with panic recovery.
3. **Publish `query_completed`** — Event with query, response, tokens, cost, duration.
4. **Publish `agent_message`** — With final response content.
5. **Return** final content string.
