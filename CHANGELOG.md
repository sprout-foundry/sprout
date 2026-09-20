# Changelog

All notable changes to Sprout will be documented in this file.

## [v0.18.7] - 2026-09-19

- docs: Update changelog for v0.18.7 (220b05e94)
- fix(ci): pin golangci-lint v2.13.2 for Go 1.26 toolchain (02d475507)
- test(agent_api): force distinct mtime in vision TTL expiry fixture (126bccc5a)
- Merge remote-tracking branch 'origin/main' (edb747e2d)
- Merge origin/main into main (213806d80)
- Merge branch 'main' of github.com:sprout-foundry/sprout (7364ab7a6)
- SP-140-5: workspace modes — Code and Design as peers (switcher, rails, shells) (#81) (5c480d870)
- fix(ci): move errcheck exclusion to linters.exclusions.rules (36fc48854)
- Merge branch 'main' of github.com:sprout-foundry/sprout (facb1311d)
- SP-140: design workspace backend — formats, tools, visual loop, sync (#82) (20a89b2bd)
- fix(providers): monotone budget curve via capped bias reserve (b0c783d0c)
- docs(roadmap): add SP-141 pkg/agent package decomposition (plan-only) (d961aff98)
- chore: archive CHANGELOG releases prior to v0.17.21 (446297acc)
- fix(configuration): drop panic-prone deprecated RegisterMigration wrapper (6a55c3239)
- refactor: unify review important-comment keyword list in pkg/codereview (33f372e06)
- refactor(webui): split useWebSocketEventHandler into domain modules (0c78aeb63)
- feat(ci): add golangci-lint with new-code-only gate (099d78f22)
- chore: remove suspended desktop CI workflows, fix ELECTRON.md reference (5552d28c1)
- test(providers): ai-worker big-prompt budget regression coverage (9a419c71c)
- fix(providers): proportional output taper and 16K reasoning-aware floor (0b43b97d6)
- fix(providers): stop 512-token decapitation of big-context agent turns (2394e499f)
- docs: Update changelog for v0.18.7 (220b05e94)
- Merge branch 'main' of github.com:sprout-foundry/sprout (7364ab7a6)
- Merge branch 'main' of github.com:sprout-foundry/sprout (facb1311d)

## [v0.18.5] - 2026-09-18

- Merge branch 'main' of github.com:sprout-foundry/sprout (c1f430f68)
- feat(vision): delegation tier, URL sniffing, assumed-vision prompts (2b840c41c)
- feat(vision): runtime capability learning with optimistic delivery (a4b6d8722)
- refactor(providers): treat ollama as a plain provider, no special-casing (6accd42c7)
- refactor(vision): single capability resolver, delete conversational-vision split (b7bfa2231)
- roadmap: add SP-140 vision-first-class spec (6da67f061)
- fix(ci): resolve the PR head ref for issue_comment-triggered reviews (23b11c11b)
- chore: refresh provider catalog (#75) (16d000e4c)
- fix(pricing): pricing and model audit (#76) (c310e95ac)
- style(webui): prettier join currentView check after costs removal (dc1b0a5ba)
- Merge remote-tracking branch 'origin/main' (01485d3d1)
- refactor(webui): remove the Costs page and /api/costs endpoints (8d6c7809e)
- docs: Update changelog for v0.18.4 (b906d4508)

## [v0.18.4] - 2026-09-17

- fix(test): wait for agent teardown before temp-dir cleanup in concurrent delete test (daefd65a9)
- feat(webui): clickable reload badge and context-menu reload for conflicted tabs (802313017)
- fix(webui): stop save/auto-save from clobbering external file changes (4d18473f7)
- docs: document hot-reload WebUI development workflow (3059c96e6)
- feat(webui): move LLM metrics out of the app footer into the chat (27ca0778c)
- perf(history): stop reading LLM responses on manifest loads (863baf522)
- fix(changes): bound diff computation and rendering that locked up the WebUI (9e93356b6)
- refactor(webui): deduplicate change-tracking utilities (eac3f93be)
- feat(webui): improve the agent change-tracking panel (cb9f07554)
- docs: refocus README on setup/development, tighten AGENTS.md (0464628a9)
- refactor(webui): remove the legacy settings subtab shim (f081bff40)
- feat(webui): gate semantic search behind the experimental embedding opt-in (0b650bbb0)
- fix(webui): layered settings writes reload the live config manager (b9083d4b2)
- fix(config): Manager.Reload re-reads the workspace layer (f0f58a32c)
- docs: Update changelog for v0.18.3 (f06d00e28)

## [v0.18.3] - 2026-09-16

- feat(webui): rewire external file change detection into buffer manager (978b1d9d3)
- fix(webui): protect unsaved editor work on workspace switch and reload (68c983e6e)
- perf(webui): throttle stream-chunk re-renders and prune DOM-bridge traffic (9b997e06a)
- fix(webui): pad git sub-tab panels so content clears the tab bar (de73f4136)
- feat(webui): unify select styling and modernize dropdown visuals (caedb6e33)
- fix(localmodel): skip model dirs with corrupt config.json instead of panicking (ca1aad5aa)
- Merge remote-tracking branch 'origin/main' (1b4e41aec)
- fix(tools): background session expiry probes liveness instead of polling (c1bf81b58)
- docs: Update changelog for v0.18.2 (b9f3eb016)

## [v0.18.2] - 2026-09-15

- fix(catalog): current deepinfra recommendation and staleness policy (ebe5e5f51)
- chore(deps): bump github.com/sprout-foundry/seed to v1.4.2 (78e971782)
- ci: run the Windows suite in a parallel lane, off the release critical path (cb462f466)
- fix(localmodel): model picker serves the full catalog in every build (90239da16)
- fix(webui): no render in lifecycle — move hook mount into each test (f6a70c10f)
- fix(release): ship real UI to macos-build; verify badge lifecycle end-to-end (a0dcdc659)
- fix(ui): render toolRefs with no text marker as inline chat badges (a5e76c09c)
- fix(release): build darwin/arm64 natively on a self-hosted Mac runner (d7e674a89)
- docs: Update changelog for v0.18.1 (45390d0f8)

## [v0.18.1] - 2026-09-15

- docs: update changelog for v0.18.1 (2aade74d0)
- fix(webui): stop chat tab pinning/activation storm that blanks multi-chat windows (eb3d8a599)
- docs: Update changelog for v0.18.0 (41713863a)

## [v0.18.1] - 2026-09-15

- fix(webui): stop the chat tab pinning/activation storm that blanked multi-chat windows (eb3d8a599)

## [v0.18.0] - 2026-09-14

- test: drop flaky BPM password-scanner e2e-ish test (5fbf5eac5)
- fix(e2e): accept the restore confirm in tier2-multi-chat (9414952c3)
- fix(localmodel): download tests were env-blind and fixture-blind (e1b73b0cf)
- fix(e2e): retarget specs to the history switcher after Sessions-tab removal (11ca1d2ae)
- fix: address subagent review findings across the release set (63cd3914a)
- webui: download not-downloaded local models from the model picker (92d000dde)
- local: select and download specific models — in-process engine, WebUI progress (7effef422)
- feat(webui): chat-header history switcher, chat-scoped restore (SP-139 Phase 3) (e68b0c63a)
- docs(roadmap): SP-139 Phase 2 shipped — per-turn change strip (7e12a4da3)
- feat(webui): per-turn change strip with Review/Revert (SP-139 Phase 2) (af8cdb8f5)
- local: qwen3.8-27b support — reasoning traces through the local provider (d706a95fe)
- docs(roadmap): SP-139 context panel de-necessitation + drop dead import (37aeafe6b)
- docs: update AGENTS.md with frontend testing and gotchas (cc4ab0361)
- fix(webui): make /clear instant and fix the New Session button (fbbeb8191)
- fix(webui): six WebUI fixes — diff controls, live split chats, optimistic new chat, pane resize/close (c5bbba855)
- fix(config): recognize newer-than-build configs, dedupe migration logging (2db9733a5)
- merge: origin/main (release changelog) (90ff18a34)
- feat(webui): show running build version in settings General tab (736caba76)
- docs: Update changelog for v0.17.27 (d90daf8ac)

## [v0.17.27] - 2026-09-13

- fix(config): default auto-resume to enabled (514cf9ade)
- fix(providers): replay reasoning history on DeepInfra (8091a9813)
- deps: bump sinter v0.4.0 -> v0.4.1 (46e412020)
- fix(agent): log panic stacks in query recover; guard embedding AST span slices (89bc58beb)
- fix(agent): estimate tokens against the wire view for non-reasoning-replay providers (de258a82f)
- deps: bump sinter v0.2.3 -> v0.4.0 — minicpm5-2b becomes the suggested local default (6a57374c2)
- fix(test): suppress state-leak detector when a live sprout session is autosaving (f98ce112f)
- deps: bump sinter v0.2.2 -> v0.2.3; fix state-leak detector false positive on live sessions (82177284b)
- deps: bump sinter v0.1.2 -> v0.2.2 (bfb99c48c)
- fix(agent): bootstrap the /tmp/sprout scratch dir and document the sandbox fallback (1005ee4a6)
- fix(tracking): resolve four review follow-ups (4db94531c)
- fix(tracking): classify background shell completions with the original command (2f3d26cdc)
- fix(tracking): second-pass review fixes for ChangeTracker (edc12437f)
- perf(tracking): key shell snapshot cache to workspace root, not shell cwd (3bf4c2aef)
- refactor(events): drop tool_execution and validation browser forwarding (1d9bcd683)
- feat(webui): restore the New Chat in Worktree flow (a34e45549)
- feat(settings): expose unexposed config fields; drop dead pre-write toggle (72addd5b4)
- fix(ui): align package license with repo (Apache 2.0) and fix stale messageSegments test (2e4429597)
- refactor(webui): prune stale synthetic endpoints and pin clear as steer-safe (bbf8754aa)
- feat(webui): handle provider_no_credential, rate_limited, compact, session_changed events (108d22cb3)
- style(webui): prettier UpdateAvailableBanner (869831520)
- chore: gitignore local webui audit notes (52fda36d6)
- feat(webui): SyncStatusBanner for the ETH-1 sync-on-resume report (ff7fb69c4)
- fix(webui): rewire chat session rename/delete/delete-all into EditorTabs (e2971997f)
- fix(webui): surface password_request events via PasswordPromptDialog (f6b43e878)
- Merge branch 'feat/update-reminder' (93009832b)
- fix(update): address review findings on the passive release check (8d3f0fb04)
- feat(update): passive release check with CLI notice and WebUI banner (fd7626064)
- fix(webui): wire live changes refresh; repair diff endpoint; gate timeline actions (9524e39e7)
- fix(tracking): capture true pre-write state; track background shells; restore from persisted history (9d784f385)
- fix(providers): keep tool message content as string when tool result carries images (061a6a1d3)
- Merge branch 'main' of github.com:sprout-foundry/sprout (a4c3d23f1)
- feat(webui): default diff buffers to text view (a5c13131d)
- chore: refresh provider catalog (#73) (2c092bf72)
- fix(pricing): pricing and model audit (#74) (27c7c6f94)
- docs: Update changelog for v0.17.26 (422d89cee)

## [v0.17.26] - 2026-09-11

- fix(webui): restore workspace on client-context recreation; terminal WS anchors its client (278ce71d6)
- Merge remote-tracking branch 'origin/main' (9aee5a762)
- chore(deps): bump vitest chain to 4.1.11 + js-yaml patches — clear 4 dependabot alerts (8e0bbca04)
- fix(webui): prettier-format context panel files failing CI format:check (69b89df07)
- Merge remote-tracking branch 'origin/main' (94f5b3857)
- feat: exact subagent correlation (seed v1.4.0) + restore ChangeTracker hooks (053caeb5d)
- fix(webui): live capability flags + review fixes for the shell-identity gate (4117ff488)
- feat(webui): shell-identity gate for mobile-only UI — no studio chrome on the plain webui (f6ac8d567)
- refactor(webui): drop dead panel code surfaced by review pass (fb0306490)
- Merge remote-tracking branch 'origin/main' (1dc2f8f51)
- feat(cli): render shell tool calls as literal commands with glyph grammar (27907e7b9)
- fix(webui): consult live active chat before dropping security-scoped events (b2402ac9f)
- fix(webui): stage missed ToolCard ref-type widening (bd3a3d8d9)
- refactor(webui): consolidate context panel to Activity/Changes/Sessions tabs (0b333b5d3)
- fix(webui): stabilize context panel frame across chat/file buffer switches (339cd1100)
- Merge remote-tracking branch 'origin/main' (70da1dde2)
- webui: single scrollbar in the workspace gate + switcher popover (desktop) (7e9cd19f3)
- Merge remote-tracking branch 'origin/main' (1488dee30)
- fix(console): erase resize/stop stale-footer window at exact block top (974df516e)
- test(persona): web-scraper alias resolves to researcher — fix smoke test (d3c287649)
- style(webui): prettier-format 7 files failing CI's format:check (0416edd8a)
- Merge remote-tracking branch 'origin/main' (0758e1945)
- webui: P4.2 mobile peer-buffer topology — keep-alive, not sheets (88433e405)
- webui: logical reserved-height (kill the editor/terminal dead band) + touch drag handle (6f748f813)
- refactor(personas): consolidate specialists, inject shared subagent preamble, opt-in coordinator (b3056c515)
- webui: real CSS for the native console host + app-top breathing room (P4.5) (ab55dc22b)
- webui: black through the home-indicator inset — kill the terminal's color-seam margin (76d5a62cc)
- webui: viewport-fit=cover — terminal flush to the PHYSICAL screen bottom (380fc2892)
- webui: remove the terminal portal's safe-area padding — the console's phantom bottom margin (45b1c1b46)
- webui: compensate terminal portal width under the paint scale (0610e7a07)
- webui: terminal portal scale² fix + hide dead webui terminal under native console (12cb15b3f)
- webui: terminal portal explicit height — fix 'whole UI is a terminal' (1a7bf6eac)
- webui: portal the terminal out of the UI-Size transform — fix ghost margins (830758a56)
- refactor(prompts): size-based delegation, self-review lane, diff-focused reviewer (ac6b988cc)
- webui: replace zoom with transform scale for UI Size — verified by screenshot (a69b27916)
- webui: move UI Size zoom from <html> to #root — keep the layout band (5c869e2b7)
- webui: UI Size scales EVERYTHING (zoom) + Extra Large tier (c22bf8c2f)
- webui: widen UI Size spread — Large ~1.21x, Compact ~0.86x (3367fc0a6)
- webui: fix P4.5 not applying on iPad — cascade order + tablet rail lock (8d418582f)
- webui: UI Size setting + touch-large handles + panel/snap defaults (P4.5) (ba2c364ab)
- wasm: re-stamp the cached agent's workspace root from the live cwd each turn (0966b08cc)
- docs: Update changelog for v0.17.25 (91ffe01c8)

## [v0.17.25] - 2026-09-09

- perf(webui): cap WS event payloads, add ANSI fast path and linkify gate (977279b4d)
- TODO: track bg-session inactivity expiry killing quiet watchers (adf8afb9b)
- style(webui): prettier-format useWorkspaceSuggestions — CI lint gate (7a2a86f84)
- fix(agent): drain in-flight auto-resume goroutines — wakeup test state-leak CI failure (62704406d)
- fix(webui): clamp workspace switcher browse to daemon root, stop showing raw JSON errors (2a617976e)
- test(computer_use): skip TCC permission checks on non-darwin — linux CI (99fef4347)
- test(e2e): Environment section now has 4 subsection tabs (GitHub added) (a1409f33f)
- webui: hide split-pane resize chrome on mobile (P4.1) (7527bdbe7)
- style(webui): prettier-format 27 files that failed CI's format:check (b4ed361eb)
- test(computer_use): skip screenshot assertion when Screen Recording TCC is unavailable — merge polish (ad1166a56)
- Merge branch 'feat-computer-use' (214d0bd5b)
- feat(computer_use): sprout diag computer-use preflight — tools, TCC permissions, config gates (9ac893db0)
- fix(webui): wire editor-save-current — the global save hotkey dispatched into the void (656470314)
- fix(computer_use): detect denied Screen Recording permission instead of returning blank screenshots (9aa6f1565)
- test(computer_use): live backend smoke test — screenshot, type, press, scroll, click (4f484ac0c)
- fix(webui): status-bar cost flicker — stale snapshot clobber, no per-turn update (4af337b20)
- Merge branch 'main' of github.com:sprout-foundry/sprout (a4e8ba3f2)
- webui: kill the every-file-load 'Failed to fetch git diff' toast on native-fs dists (dc3b48242)
- fix(webui): diff-view Cmd+S save never fired — CM keymaps don't run inside @codemirror/merge panes (5814c35e2)
- fix(agent): install SetTestStateDirHook in TestMain — stop session-state leaks into real state dir (b48c14ca7)
- fix(webui): git worktree create/remove/checkout resolved relative paths against the daemon CWD (e85bfbf26)
- Merge branch 'main' of github.com:sprout-foundry/sprout (71cc40fd1)
- fix(webui): cloned popups share the opener's server context — ownership oracle (580719b6d)
- fix(webui): editor breadcrumb — workspace-relative display, middle collapse, no bar chrome (2b0be5dd7)
- webui: workspace selector row — single cwd surface, no duplication (9a64d604f)
- Merge branch 'main' of github.com:sprout-foundry/sprout (2fb94e1e5)
- Merge branch 'feat-lsp-fixes' (1e7dd7059)
- feat(vision): SP-137 — first-class image paths, provider neutrality, native OCR (69021ab5f)
- webui: Files header redesign — overflow menu, workspace-row Add repo (0a92ba370)
- fix(webui): resolve relative buffer paths to absolute for LSP URIs (db2c44333)
- webui: touch-draggable resize handles + keyboard-safe terminal fit (27e165acc)
- webui: terminal cd + pwd — session directory for the native console (520751c11)
- webui: service-layer catch audit — persist failures logged, best-effort marked (a5f5fe867)
- webui: hardening + UX pass — safe parsing, surfaced errors, guarded destructive actions (46a43fb53)
- Merge branch 'main' of github.com:sprout-foundry/sprout (804384483)
- docs(roadmap): add SP-137 — vision first-class paths, provider neutrality, native OCR (e02282bb4)
- chore: refresh provider catalog (#71) (437aa20a1)
- fix(pricing): verify new model pricing and enrich metadata (#72) (f03736c8b)
- webui: core.symlinks=false fallback — clones of repos with symlinks no longer abort (99e560d2b)
- Deduplicate the three identical Date-based formatDuration implementations across the webui package (chat feed, chat tree, and contextPanel helpers) into a single canonical implementation in webui/src/utils/format.ts. helpers.tsx re-exports it so existing imports of './helpers' keep working. Behavior unchanged — logic is byte-identical, only variable names differed. (#58) (71f95084c)
- SP-CLOUD-7: Responsive layout for browser (5e76b0130)
- SP-CLOUD-6: Repo import deployment (69c5edd23)
- fix(console): repair resize chrome stranding and select-list frame stacking (f98aee359)
- chore: strip dated incident-narrative references from comments (beb830db7)
- test: hermetic git config for all git-spawning test binaries (c8f854c7b)
- chore: replace personal identifiers in comments and test fixtures with synthetic ones (405b3aff4)
- fix(secretdetect): boundary-safe redaction — egress backstop corrupted JSON request bodies (4559ce771)
- workspace gate: studio variant with native folder picker + new-project flow (4a6443ac1)
- session working directory: repo-scoped Files, Terminal, Git, Agent (049971acc)
- useAvailableShells: add missing path field on synthetic native shell (ba1b66954)
- useAvailableShells: gate the fetch on native-terminal gate, not mount order (91166756c)
- Fix GitHub clone on native-fs: subtree listings, loud batch errors (92406153f)
- Add GitHub OAuth device flow sign-in to webui GitHub panel (b5157cf66)
- webui: GitHub sign-in section in cloud-mode settings (studio surfaces) (62170a3b3)
- Open cloned repo after seam clone: reveal README in file tree (a1bddc61f)
- Add workspaceFs seam + gitFs adapter; route GitHub picker clones through it (373b18dbb)
- Add GitHub login, repo browser, and authenticated clone to webui (d3975b940)
- docs(docs): remove webui bundle and protocol specs (b40c65a3a)
- Adds mobile control row, studio-providers make target (3460536cc)
- Fix streaming requests answered with plain JSON (zai coding-plan) (fdd54ac39)
- chore: refresh provider catalog (#69) (fc22dd48d)
- Adds git_clone/git_list_repos agent tools + model-ID canonicalization (b231eaa00)

## [v0.17.24] - 2026-09-04

### Background processing, attach, and wakeup — comprehensive wiring repair

- fix(webui): repair background processing, attach, and wakeup wiring — WebUI background commands now emit a `__SPROUT_DONE__` sentinel so completion is observable while the shell lives (real exit codes, `bgDone` channel, honest `finish_reason`); the daemon wakeup poller reaches every live per-chat agent instead of only `ws.agent`; pending notifications survive agent eviction via AgentState; shell sessions scope per chat (chatID flows Agent → ToolEnv → context); the server emits an allowlisted `agent_session_update` event that the frontend actually receives (`sprout:wsevent` bridge); 2-minute-deadline promotions attach wakeup watchers; `TryAutoResume` drains before consuming budget; cleanup never reaps sessions with live subscribers (7dab3eee6)
- feat(wakeup): user-facing display for auto-resume turns — the raw `[wakeup]` batch no longer surfaces as a chat bubble: query_started carries `display` + `source`, wakeup turns render "Looking into 'make build'…", labels ride notifications via `NotifyCompletionLabeled`; real user queries reset the wakeup budget (exhaustion pauses instead of silencing all session); post-turn `TryAutoResume` acts on mid-turn completions immediately; system prompt documents the [wakeup] contract (54d338549)

### WebUI fixes

- fix(webui): repair queued-message panel and cross-chat drain routing — the 3b516bbb5 refactor left the queue panel fed by default props (empty list, no-op remove/edit/reorder/clear); restored the full CRUD surface and wired the five missing props through App → AppContent → ChatView → CommandInput. Queue entries are tagged with their originating chat and the drain only dispatches to the matching idle chat, so a message queued for chat A can't fire into chat B after a switch (bc3e2e366)

### Local model fixes

- fix(localmodel): stop silent 512-token truncation of local model turns — both LocalProvider send paths inherited sinter's 512-token MaxTokens default and never overrode it, cutting long turns mid-thought while reporting a clean "stop"; output now budgets against the real context window (exact local-tokenizer count, 16384 runaway cap) and reports `finish_reason: "length"` on truncation; the standalone llm_server subprocess no longer clamps client budgets. Verified live on qwen3.6-35b-a3b (1545-token completion, natural ending) (9c1a46dcb)

### Other

- wasm: seed credential placeholders for proxy-attached auth (9d7630079)

## [v0.17.23] - 2026-09-04

### Dependencies

- chore: bump seed to v1.3.20 — pulls in the FallbackParser fix for Qwen-family checkpoints that narrate tool calls as text (qwen3.8-27b via NInfer): tag-suffix parameters (<parameter=name>), bare closers (</function>), and <tool_call> wrapper variants all recover into structured calls; spawned subagents inherit the parent's seed version pin (cffb50f97)

## [v0.17.22] - 2026-08-31

### CLI fixes

- fix(console): repair approval flow, resize handling, and footer overwrite bugs — per-part shell picker's resume hook now balances its suspend (activity spinner restarts after approvals); the per-line readLineCtx goroutine that raced the steer reader for stdin after cancel is replaced by a single persistent reader; SelectList non-TTY fallback honors ctx instead of hanging on idle piped stdin; the filesystem approval prompt uses the approvalPicker test seam; the WebUI approval path suspends streaming like the CLI path; SelectList subscribes to resize events with height-clamped walk-backs so approval prompts no longer garble on SIGWINCH; beginTurn closes the prior resize subscriber (leak); drawFullLocked re-asserts the DECSTBM scroll region every draw so child processes (editors, pagers) that drop it no longer let output scroll over the pinned footer (5d6214eb7, c877d2a24)
- fix(console): keep steer readLoop exitable under cooked-mode termios flips — "[steer] Stop() timed out waiting for readLoop" traced to the readLoop parked in a blocking Read when something restored cooked mode (VMIN=1) underneath (racing PauseSteer/prompt exitSteerMode); reads are now poll-gated (10ms bound regardless of termios) and PauseSteer/ResumeSteer are refcounted like SuspendStreaming so overlapping prompts can't trample each other's termios (76bf56e22)

### Background-session fixes

- fix(tools): stop orphan cleanup killing live background sessions; richer completion notices — orphan cleanup (run at every agent creation) killed any PID in a .pid file, so test binaries or CLI invocations sharing the config dir reaped the interactive process's still-running sessions (wakeup "exit code -1" + vanished output files); pidfiles are now owner-aware ("<child-pid> <owner-pid>", live-owner sessions skipped, 24h age gate), signal deaths report 128+N, and completion notifications embed a ≤2KB output tail (c3ee32b56)
- fix(tools): make permission-preservation test umask-independent (3fd0a3685)

### CI / test hygiene

- fix(tests): deflake agent-socket TempDir teardown and daemon health recovery — the two intermittent main-branch CI failures (ubuntu "unlinkat directory not empty" from the ephemeral agent's async Shutdown racing TempDir cleanup; macos health-recovery polling a ~10ms observability window) (dd1f43b67)
- fix(webui): apply Prettier to R-2w native-FS seam files (6af55f968); lint cleanups for upstream R-3 merge — drop manual testing-library cleanup + import order (c9cb38dc9)

### Dependencies

- chore(deps): remove vestigial workspace lockfiles — all 17 Dependabot alerts pointed at a pre-workspace packages/ui/package-lock.json no build path consults; removed it and the matching events lockfile, taking open alerts 17 → 0 (1519a0481)

### Track R (webui decoupling, upstream)

- R-0 decoupling audit + build-time feature-flag seams (e24c49dc8); R-2w manifest-driven native-FS deferral (f03d9c6ec); R-2f conditional WASM boot for ratified --native-fs dists — fixes the iOS shell boot error (92e7a76fe); R-3 native terminal swap, sprout-side seam — 43 new tests, dist builds verified both ways (d245c5595); dashboard fallbacks honor ?from=editor, dead crossTabSync dropped (a048f8c74); coordinator retargeted to zai-coding glm-5.3 (c43cea4ee); provider catalog refresh (#64), pricing and model audit (#65)

Releases prior to v0.17.21: see [CHANGELOG-ARCHIVE.md](CHANGELOG-ARCHIVE.md).
