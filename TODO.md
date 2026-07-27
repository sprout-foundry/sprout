# TODO

Active work tracked here. Each item should be scoped for a single agent run
via the workflow automation (~2-4 hours of focused work). Items are grouped
by spec and ordered by priority within each group.

---

## SP-124b: Batch Security Analysis for Chained Commands

_Proposed, not started. Most concrete unshipped spec. ~2-3 days total._

- [ ] **SP-124b Phase 1: Backend batch analysis** — Add `ChainedClassification` struct + `ClassifyChainedCommand()` wrapper; add `Chain` struct + `ParseChain()` delegating to `SplitChainedCommand`; add `AnalyzeChain()` with chain-aware prompt when `len(Subcommands) > 1`; switch cache key to normalized chain under `sp-124b:v1:` namespace. **~1 day.** New file: `pkg/agent/security_analyzer_chain.go`.

- [ ] **SP-124b Phase 2: Per-subcommand fallback + UI** — Chain-length cap (10) → fall back to per-subcommand analyses; add `ChainSubcommands` to `SecurityAnalysisView`; render chain stepper in `SecurityApprovalDialog.tsx` with per-subcommand risk dots; CLI prompt caps noise with "(N more...)". **~1 day.**

---

## SP-126: Effective Context Cap

_Bug fix for existing user-facing feature. Scoping, not yet approved. ~4 hours._

- [ ] **SP-126: Fix MaxContextTokens clobbering** — `Config.MaxContextTokens` is silently clobbered on every iteration. Add `ResolveEffectiveContextCap()` resolver, `effectiveContextCap` field on Agent, fix `seed_provider.Info()` to return capped value, add defensive re-cap in `seed_query.OnIteration`, and unit tests. **~4 hours.**

---

## SP-121-8: Git UI Polish

_3 of 5 features shipped (markdown preview, push/pull, branch chip). 2 remain._

- [ ] **SP-121-8: File/folder creation UI** — Wire `onCreateFile` → `gitClient.writeFile()` + lightning-fs + VFS sync. Add "New file" and "New folder" buttons in file tree context menu. **~2 hours.**

- [ ] **SP-121-8: ZIP download** — Wire "Download as ZIP" via JSZip + `ReadableStream` for workspace export. **~2 hours.**

---

## SP-121-9: First-Run UX + Onboarding

_Onboarding dialog shipped. 2 of 3 remaining items unstarted._

- [ ] **SP-121-9: URL import dialog** — Add URL paste entry point to onboarding flow. Clone via isomorphic-git, auto-bridge to VFS. **~2 hours.**

- [ ] **SP-121-9: Create-repo dialog** — Add "Create new repo" entry point to onboarding. Local `git init` in lightning-fs; optional GitHub creation (depends on SP-121-11 OAuth). **~2 hours.**

---

## SP-121-7: Repo Content Flow

_Phases 1-5 shipped. 6 future-phase items remain._

- [ ] **SP-121-7: File content preview in RepoFileTree** — Click a file → view it in editor without full clone. **~2 hours.**

- [ ] **SP-121-7: Auto-bridge on clone completion** — Clone finishes → automatically sync to VFS (currently manual). **~1 hour.**

- [ ] **SP-121-7: Auto-trigger clone on `?repo=` deep link** — Currently just stores URL; should auto-clone. **~1 hour.**

- [ ] **SP-121-7: Branch switch + file tree refresh** — Switch branch → re-render tree. **~1 hour.**

- [ ] **SP-121-7: Commit history view in RepoDetailPage** — UI for `git log` output. **~2 hours.**

- [ ] **SP-121-7: Conflict resolution UI for push failures** — Handle push rejections with merge-conflict UI. (Depends on SP-121-11 OAuth.) **~2 hours.**

---

## SP-121-11: Git Provider OAuth + Multi-Repo

_Draft, not started. 20 items total. This is a large feature — each sub-item is a separate agent task._

**OAuth (11 items):**
- [ ] **SP-121-11: Provider abstraction interface** — Define `Provider` interface + GitHub/GitLab/Bitbucket implementations. **~2 hours.**
- [ ] **SP-121-11: OAuth app registration** — Register OAuth apps for all 3 providers. **~1 hour (manual).**
- [ ] **SP-121-11: GitHub OAuth flow** — End-to-end redirect: connect → authorize → callback → stored tokens. **~2 hours.**
- [ ] **SP-121-11: GitLab + Bitbucket OAuth flows** — Same redirect pattern for GitLab and Bitbucket. **~2 hours.**
- [ ] **SP-121-11: Token refresh** — Proactive timer + on-demand on 401. **~2 hours.**
- [ ] **SP-121-11: Wire OAuth into gitClient** — Auto-detect provider from URL domain, use token for auth. **~2 hours.**
- [ ] **SP-121-11: PAT fallback + UI** — PAT remains as fallback; UI shows active auth method. **~1 hour.**
- [ ] **SP-121-11: Disconnect revokes tokens** — "Disconnect" revokes tokens + clears credentials. **~1 hour.**
- [ ] **SP-121-11: Provider connection status in Settings** — Show connection status per provider. **~1 hour.**
- [ ] **SP-121-11: Provider picker on onboarding** — Add provider selection to onboarding screen. **~1 hour.**
- [ ] **SP-121-11: `user_oauth_tokens` DB table** — Persist OAuth tokens in database. **~1 hour.**

**Multi-repo (9 items):**
- [ ] **SP-121-11: Multi-repo in lightning-fs** — Workspace holds multiple repos simultaneously. **~2 hours.**
- [ ] **SP-121-11: Repo tab bar** — Click to switch active repo. **~2 hours.**
- [ ] **SP-121-11: Agent context carries currentRepo** — Tools operate on active repo's VFS root. **~2 hours.**
- [ ] **SP-121-11: Workspace state persistence** — Persist across page reloads. **~1 hour.**
- [ ] **SP-121-11: "+" button to attach new repo** — Wire to onboarding flow. **~1 hour.**
- [ ] **SP-121-11: Detach repo** — Remove from workspace without deleting local files. **~1 hour.**
- [ ] **SP-121-11: Sidebar repo list with status** — Show all repos with status indicators. **~1 hour.**
- [ ] **SP-121-11: `/repo owner/name` slash command** — Switch agent context to named repo. **~1 hour.**
- [ ] **SP-121-11: Cross-repo ambiguity prompt** — Disambiguate when tools could target multiple repos. **~1 hour.**

---

## SP-103: Vision Pipeline Reliability

_3 of 6 sub-items remain open._

- [ ] **SP-103 D1: Inline-image cost into budget tracker** — When `processImagesAsMultimodal` embeds images, per-image `image_tokens` / `cache_read_input_tokens` from provider response are dropped. Bridge them into cost tracking so users see actual vision cost. **~2 hours.** Touches `conversation.go` and provider response structs.

- [ ] **SP-103 D2: Batch splitting with fallback** — When N images exceed provider's vision context window, inline path fails with 400. Add batch splitting: try inline; on overflow, keep first K inline, call `analyze_image_content` for the rest. **~2 hours.** New `vision_batch_split.go` helper.

- [ ] **SP-103 D3: Per-provider VisionCapabilities values** — Struct exists with defaults. Ollama and OpenAI-compatible populate values. Populate per-provider values for Anthropic, Gemini, and other non-OpenAI providers. **~1 hour.**

---

## SP-094: Typed Error Hierarchy

_Foundation shipped. Migration broken into per-wave tasks._

- [ ] **SP-094 Wave 1: Migrate tool handler errors** — Convert `pkg/agent_tools/*_handler.go` from `fmt.Errorf` to `TypedError`. ~80 sites, partial progress (vision handlers done). **~2 hours.**

- [ ] **SP-094 Wave 2: Migrate provider client errors** — Convert `pkg/agent/api_client*.go` from `fmt.Errorf` to `TypedError`. ~40 sites. **~2 hours.**

- [ ] **SP-094 Wave 3: Migrate subagent + delegator errors** — Convert `pkg/agent/subagent_*.go` from `fmt.Errorf` to `TypedError`. ~60 sites. **~2 hours.**

- [ ] **SP-094 Wave 4: Migrate remaining pkg/agent errors** — Convert remaining `pkg/agent/*.go` from `fmt.Errorf` to `TypedError`. ~330 sites across multiple files. **~4 hours.**

- [ ] **SP-094: Wire broker exponential backoff** — Add exponential backoff on `ProviderError+RateLimitError` in `pkg/agent/approval_broker.go`. Emit per-category labels in `pkg/agent/metrics.go`. **~2 hours.**

- [ ] **SP-094: `sprout explain` typed errors** — Integrate typed error hierarchy into `sprout explain <hash>` instead of raw stack traces. **~1 hour.**

---

## SP-098: Large-File Decomposition — Second Pass

_13 of 25 files resolved. 12 remain ≥800 lines. Each file is a separate agent task._

**Highest priority (grew since audit):**
- [ ] **SP-098: Split `pkg/agent/tool_security.go` (1142 lines)** — Extract `tool_security_policy.go` + `tool_security_paths.go` + `tool_security_audit.go`. **~2 hours.**
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

## Deferred (Low Priority)

_Deferred from shipped specs. Not blocking, tracked for completeness._

- [ ] **SP-112 Tier 4: WASM limitation docs** — Show tooltip when user tries WASM-unavailable feature. **~1 hour.**
- [ ] **SP-112 Tier 4: Non-Darwin/Linux limitation docs** — User-facing explanation for foreground app detection and panic key chord. **~1 hour.**
- [ ] **SP-114: Long-output WebSocket streaming** — Commands like `/changes` with many files need WebSocket streaming instead of single JSON response. **~2 hours.**
- [ ] **SP-114: CommandRegistry allocation storm fix** — `ClassifyPromptIntent` creates `NewCommandRegistry()` on every call. Fix with `DefaultRegistry()` + `sync.Once`. **~1 hour.**
- [ ] **SP-125 R3: Lite capability probe for 32K** — Measure probe's own token usage at 32K; may need lite variant. **~1 hour.**
- [ ] **SP-125 R4: Subagent LCM inheritance** — Hook in `subagent_creation.go` so 32K subagent auto-activates LCM. **~1 hour.**
- [ ] **SP-127: Performance benchmark for Gate-1** — Show ≤10% regression for common case; add path caching if needed. **~1 hour.**

---

## Future (Scope TBD)

_Requested but not scoped. Needs design before they become actionable tasks._

- **SP-113: Subscription quota tracking** — Track remaining quota/rate-limit usage for subscription providers.
- **SP-113: Cost alerts by billing type** — Distinct alerts for API spend vs subscription limits.
- **SP-113: Ollama Cloud credits** — Support credit-based billing type.
- **SP-124: Learn from user decisions** — Suggest auto-approving patterns the user consistently approves.
- **SP-124: Policy-based auto-approval** — LLM analysis feeds into workspace security policy.
- **SP-124: Multi-language security analysis** — Commands in non-English locales analyzed in user's language.
- **WASM stub-tools** — CGO-only handler-stub coverage for large logic files (`security_classifier.go` at 900 lines has no WASM guard).
- **Multi-workspace daemon** — `daemonRoot` concept exists; needs proper multi-workspace daemon design.

---
