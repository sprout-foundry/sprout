# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that
are approved and ready to assign are listed.

---

## SP-103: Vision Pipeline Reliability

- [x] **SP-103 D1: Inline-image cost into budget tracker** — When `processImagesAsMultimodal` embeds images, per-image `image_tokens` / `cache_read_input_tokens` from provider response are dropped. Bridge them into cost tracking so users see actual vision cost. **~2 hours.** Touches `conversation.go` and provider response structs.

- [x] **SP-103 D2: Batch splitting with fallback** — When N images exceed provider's vision context window, inline path fails with 400. Add batch splitting: try inline; on overflow, keep first K inline, call `analyze_image_content` for the rest. **~2 hours.** New `vision_batch_split.go` helper.

- [x] **SP-103 D3: Per-provider VisionCapabilities values** — Struct exists with defaults. Ollama and OpenAI-compatible populate values. Populate per-provider values for Anthropic, Gemini, and other non-OpenAI providers. **~1 hour.**

---

## SP-094: Typed Error Hierarchy

- [x] **SP-094 Wave 1: Migrate tool handler errors** — Convert `pkg/agent_tools/*_handler.go` from `fmt.Errorf` to `TypedError`. ~80 sites, partial progress (vision handlers done). **~2 hours.**

- [x] **SP-094 Wave 2: Migrate provider client errors** — Convert `pkg/agent/api_client*.go` from `fmt.Errorf` to `TypedError`. ~40 sites. **~2 hours.** — Already done: `api_client.go` was deleted in seed-integration refactor, remaining files have zero `fmt.Errorf`.

- [x] **SP-094 Wave 3: Migrate subagent + delegator errors** — Convert `pkg/agent/subagent_*.go` from `fmt.Errorf` to `TypedError`. ~60 sites. **~2 hours.** — Migration was already largely done; only 1 remaining `fmt.Errorf` in `subagent_runner_test.go` converted to `agenterrors.NewInvalidInputError`.

- [ ] **SP-094 Wave 4: Migrate remaining pkg/agent errors** — Convert remaining `pkg/agent/*.go` from `fmt.Errorf` to `TypedError`. ~330 sites across multiple files. **~4 hours.** — Partial: many done across 20+ files, ~250 remaining.

- [x] **SP-094: Wire broker exponential backoff** — Add exponential backoff on `ProviderError+RateLimitError` in provider retry path. Emit per-category labels. **~2 hours.**

- [x] **SP-094: `sprout explain` typed errors** — Integrate typed error hierarchy into `sprout explain <hash>` instead of raw stack traces. **~1 hour.**

---

## SP-098: Large-File Decomposition

**Highest priority (grew since audit):**
- [x] **SP-098: Split `pkg/agent/tool_security.go` (1142 lines)** — Extract `tool_security_policy.go` + `tool_security_paths.go` + `tool_security_audit.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/events/events.go` (1218 lines)** — Extract `events_types.go` + `events_bus.go` + `events_filter.go`. **~2 hours.**

**Remaining (stable, ≥800 lines):**
- [ ] **SP-098: Split `pkg/embedding/manager.go` (902 lines)** — Extract `embedding_models.go` + `embedding_batch.go` + `embedding_cache.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/agent_tools/security_classifier.go` (900 lines)** — Extract `security_classifier_path.go` + `security_classifier_shell_patterns.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/agent_tools/background_process.go` (885 lines)** — Extract `background_process_log.go` + `background_process_pty.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/providerregistry/registry.go` (871 lines)** — Extract `registry_models.go` + `registry_providers.go` + `registry_aliases.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/history/changetracker.go` (868 lines)** — Extract `changetracker_record.go` + `changetracker_revert.go` + `changetracker_persist.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/console/select_list.go` (868 lines)** — Extract `select_list_filter.go` + `select_list_keymap.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/credentials/encrypt.go` (861 lines)** — Extract `encrypt_aes.go` + `encrypt_keyring.go` + `encrypt_migrate.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/agent/persistence.go` (857 lines)** — Extract `persistence_session.go` + `persistence_message.go` + `persistence_index.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/webui/settings_api_mcp.go` (847 lines)** — Extract `settings_api_mcp_oauth.go`. **~2 hours.**
- [ ] **SP-098: Split `pkg/agent/scripted_playback.go` (835 lines)** — Extract `scripted_record.go` + `scripted_assert.go`. **~2 hours.**

---
