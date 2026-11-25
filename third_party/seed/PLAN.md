# seed — Implementation Plan

## Conformance Test Suite (SP-016)

**Spec:** `roadmap/SP-016-conformance-test-suite.md`

### Phase 1: CLI Binary

- [x] SP-016-1a: Create `cmd/seed-cli/main.go` with JSON-RPC protocol (newline-delimited JSON over stdin/stdout). Request/response framing with `id`, `method`, `params`, `result`, `error` fields. Async events use `event` field. Handle stdin-close as context cancellation. Ref: SP-016 §Architecture.
- [x] SP-016-1b: Implement agent lifecycle methods in CLI: `agent.new`, `agent.run`, `agent.runStream`, `agent.interrupt`, `agent.resetInterrupt`, `agent.pause`, `agent.resume`, `agent.isPaused`. Ref: SP-016 §Agent Lifecycle table.
- [x] SP-016-1c: Implement state management methods: `state.export`, `state.import`, `state.messages`, `state.sessionId`, `state.setSessionId`, `state.ensureSessionId`, `state.tokens`, `state.cost`, `state.addMessage`, `state.len`, `state.clearCheckpoints`. Ref: SP-016 §State Management table.
- [x] SP-016-1d: Implement configuration methods: `agent.setSystemPrompt`, `agent.setProvider` (only runtime-swappable component — document that Executor/UI are construction-time), `agent.setFlushCallback`. Ref: SP-016 §Configuration table.
- [x] SP-016-1e: Implement steering & injection methods: `agent.steer`, `agent.steerSystem`, `agent.injectInput`. Ref: SP-016 §Steering & Injection table.
- [x] SP-016-1f: Implement checkpoint, provider access, streaming buffer methods: `agent.checkpoints`, `agent.providerInfo`, `agent.estimateTokens`, `agent.streamingBuffer`, `agent.reasoningBuffer`. Ref: SP-016 §Checkpoints/Provider/Streaming Buffers tables.
- [x] SP-016-1g: Implement output manager methods: `output.setMetadata`, `output.getMetadata`, `output.flush`, `output.reset`. Ref: SP-016 §Output Manager table.
- [x] SP-016-1h: Implement scriptable mock provider in CLI: `mock.addTextResponse`, `mock.addTextResponseWithFinish`, `mock.addToolCallResponse`, `mock.addMalformedResponse`, `mock.addError`, `mock.addStreamChunks`, `mock.withInfo`, `mock.withTokenEstimate`, `mock.blockUntil`, `mock.blockOnCallN`, `mock.release` (unblocks blocked calls). Ref: SP-016 §Mock Configuration table.
- [x] SP-016-1i: Implement scriptable mock executor in CLI: `mock.addTool`, `mock.addToolResult`, `mock.reset`, `mock.callCount`, `mock.lastRequest`, `mock.executorCallCount`, `mock.lastExecutorCalls`. Ref: SP-016 §Mock Configuration table.
- [x] SP-016-1j: Add async event forwarding — CLI captures all EventBus events and writes them as JSON lines to stdout. Auto-wire `OnIteration` to emit `on_iteration` events and `OnCheckpoint` to emit `on_checkpoint` events. Ref: SP-016 §Architecture.
- [x] SP-016-1k: Wire `agent.new` to accept ALL `Options` fields via params: `systemPrompt`, `maxIterations`, `maxTokens`, `disableFallbackParser`, `disableValidator`, `disableNormalizer`, `initialMessages`, `initialCheckpoints`, `compactionTriggerFraction`, `retryConfig` (object with `maxAttempts`, `initialDelay`, `maxDelay`, `multiplier`, `jitter`), `optimizer` (object with `toolCategories` map), `debug`. Auto-wire `UI` to `NoopUI`, `EventPublisher` to internal EventBus. Defer `LLMSummarizer` and `Pruner`. Ref: SP-016 §agent.new Options table.

### Phase 2: Test Runner

- [x] SP-016-2a: Create `conformance/runner/main.go` — reads spec directory, spawns CLI process per test (fresh process for isolation), sends JSON-RPC actions over stdin, collects responses and async events from stdout, evaluates assertions. Ref: SP-016 §Test Runner.
- [x] SP-016-2b: Implement assertion engine in runner: `response`, `event`, `events`, `state`, `mock`, `noError`, `error` (with `errorType` field supporting: `no_provider`, `no_executor`, `interrupted`, `max_iterations`, `paused`, `zero_choices`, `transient`, `rate_limit`, `context_overflow`, `auth`, `client`, `content_filtered`, `blank_response`). Ref: SP-016 §Assertion Types table.
- [x] SP-016-2c: Add TAP output format to runner. Add `--verbose` flag for debugging (show all events/responses). Add `--filter` flag for running specific test files. Add `--cli` flag for specifying CLI binary path. Ref: SP-016 §Test Runner.
- [x] SP-016-2d: Add `make conformance` target to Makefile. Builds CLI, builds runner, runs runner against CLI, reports pass/fail count.

### Phase 3: Test Specs

- [x] SP-016-3a: Write `conformance/specs/00_smoke/` (1 spec): protocol_plumbing. Ref: SP-016 §00 — Smoke table.
- [x] SP-016-4a: Write `conformance/specs/01_lifecycle/` (8 specs): basic_text_query, agent_new_requires_provider, agent_new_requires_executor, agent_pause_resume, agent_interrupt, agent_run_stream, agent_reset_interrupt, agent_multiple_queries. Ref: SP-016 §01 — Agent Lifecycle table.
- [x] SP-016-5a: Write `conformance/specs/02_conversation/` (12 specs): single_tool_call, multi_tool_call, tool_call_then_text, max_iterations_exceeded, context_cancellation (via stdin close), finish_reason_length, finish_reason_content_filter, finish_reason_stop, finish_reason_unknown, zero_choices, blank_response_handling, repetitive_response. Ref: SP-016 §02 — Conversation Loop table.
- [x] SP-016-6a: Write `conformance/specs/03_tools/` (11 specs): tool_register, tool_unregister, tool_alias, tool_result_ordering, tool_timeout, tool_result_truncation, circuit_breaker_opens, circuit_breaker_half_open, pre_execute_hook, post_execute_hook, parallel_execution. Ref: SP-016 §03 — Tool Execution table.
- [x] SP-016-7a: Write `conformance/specs/04_state/` (8 specs): export_import (assert excluding Meta — `json:"-"` tag means Meta not preserved), session_id_auto, token_accumulation, cost_accumulation, messages_immutable, add_checkpoint, clear_checkpoints, initial_messages. Ref: SP-016 §04 — State Management table + §Serialization Notes.
- [x] SP-016-8a: Write `conformance/specs/05_streaming/` (6 specs): stream_content, stream_reasoning, stream_on_done, stream_on_error, stream_buffer, stream_events. Ref: SP-016 §05 — Streaming table.
- [x] SP-016-9a: Write `conformance/specs/06_events/` (7 specs): query_started, query_completed, tool_start_end, metrics_update, compaction_event, error_event, critical_delivery (fire many events rapidly, assert critical event arrives). Ref: SP-016 §06 — Events table.
- [x] SP-016-10a: Write `conformance/specs/07_compaction/` (10 specs): phase0_checkpoint_sub, phase0_masking, phase1_drop_summaries, phase15_drop_turns, phase2_emergency, trigger_fraction, max_tokens_derivation, context_overflow_recovery, turn_checkpoint, on_checkpoint_callback. Ref: SP-016 §07 — Compaction table.
- [x] SP-016-11a: Write `conformance/specs/08_fallback/` (8 specs): json_code_fence, bare_json, xml_blocks, named_blocks, tool_use_blocks, deduplication, validates_known_tools, disabled. Ref: SP-016 §08 — Fallback Parser table.
- [x] SP-016-12a: Write `conformance/specs/09_normalize/` (6 specs): strip_channel, repair_json, generate_id, normalizer_dedup, validator_truncated, tentative_post_tool. Ref: SP-016 §09 — Normalizer & Validator table.
- [x] SP-016-13a: Write `conformance/specs/10_errors/` (12 specs): retry_transient, retry_rate_limit, auth_fails_fast, overflow_fails_fast, max_attempts, classify_transient, classify_auth, classify_rate_limit, backoff_exponential, backoff_max_cap, backoff_jitter, backoff_reset. Backoff tests are tested through the retry loop via event timestamps, not as standalone unit. Ref: SP-016 §10 — Error Handling & Retry table.
- [x] SP-016-14a: Write `conformance/specs/11_steering/` (5 specs): steer_message, steer_system, steer_queue, inject_input, inject_full. Ref: SP-016 §11 — Steering & Injection table.
- [x] SP-016-15a: Write `conformance/specs/12_output/` (4 specs): publish_content, publish_reasoning, flush_callback, event_metadata. Ref: SP-016 §12 — Output Manager table.
- [x] SP-016-16a: Write `conformance/specs/13_optimizer/` (4 specs): file_read_dedup, shell_command_dedup, masking_gated, preserves_latest. Ref: SP-016 §13 — Conversation Optimizer table.
- [x] SP-016-17a: Write `conformance/specs/14_config/` (1 spec): set_provider_swap. Ref: SP-016 §14 — Configuration table.

### Phase 4: Validate & Polish

- [x] SP-016-18a: Run full conformance suite against `seed-cli` (Go). Fix any CLI/spec mismatches until all 103 tests pass. Ref: SP-016 §Design Decisions.
- [x] SP-016-18b: Write `conformance/PROTOCOL.md` — CLI JSON-RPC protocol reference for language implementors. Ref: SP-016 §Architecture.
- [x] SP-016-18c: Write `conformance/SPEC_FORMAT.md` — test spec JSON schema and assertion type reference. Ref: SP-016 §Test Specs.
- [x] SP-016-18d: Update `roadmap/README.md` to list SP-016 as in-progress.
