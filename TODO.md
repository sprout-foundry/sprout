# TODO

## Done — SP-009: Component Library Maturation

- [x] SP-009-P1.1: Verify `npm run build` produces clean output in `packages/ui/`; create README.md and CHANGELOG.md
- [x] SP-009-P1.2: Publish `@sprout/ui` to npm under `@sprout` org scope; create `.github/workflows/publish-ui.yml`
- [x] SP-009-P1.3: Replace `file:../packages/ui` in `webui/package.json` with versioned npm dependency
- [x] SP-009-P1.4: Document Foundry consumption pattern (`npm install @sprout/ui`)
- [x] SP-009-P2.1: Install Storybook 8 in `packages/ui/` with Vite builder and React framework
- [x] SP-009-P2.2: Create `MockAdapter` implementing `APIAdapter`; wrap all stories in `SproutProvider` with mock
- [x] SP-009-P2.3: Write stories for StatusBar, FileTree, Terminal, GitPanel, CommandPalette (Tier 1)
- [x] SP-009-P2.3: Write stories for MessageBubble, MessageContent, MessageSegments, ChatPanel (Tier 2)
- [x] SP-009-P2.3: Write stories for ContextMenu, NotificationItem, SelectionActionBar, CommandInput (Tier 3)
- [x] SP-009-P2.4: Connect to Chromatic for visual regression testing on PRs
- [x] SP-009-P2.5: Write MDX documentation pages for complex components (FileTree, ChatPanel, GitPanel)
- [x] SP-009-migration: Replace duplicate component implementations in webui with imports from `@sprout/ui`

## Partially Done — SP-010: Editor Modernization

- [x] SP-010-P1-hooks: Extract remaining hooks: `useEditorExtensions`, `useEditorDiagnostics`, `useEditorFileIO`, `useEditorScrollSync`, `useEditorSymbols`, `useEditorCursor`
- [x] SP-010-P1-components: Create `EditorCore.tsx` (CodeMirror mount point) and `EditorToolbarActions.tsx`; reduce `EditorPane.tsx` to ~300-line composition root
- [x] SP-010-P2-errorLens: Create `webui/src/extensions/errorLens.ts` — inline diagnostic display via `Decoration.widget`
- [x] SP-010-P2-wordHighlights: Verify/fix `highlightSelectionMatches()` wiring and add custom styling
- [x] SP-010-P2-inlayHints: Create `webui/src/extensions/inlayHints.ts` — request LSP inlay hints for type/parameter annotations
- [x] SP-010-P2-signatureHelp: Create `webui/src/extensions/signatureHelp.ts` — function signature tooltip on `(`/`,`
- [x] SP-010-P2-formatOnSave: Wire existing formatter service to save action (opt-in setting)
- [x] SP-010-P2-goToReferences: Add dedicated go-to-references panel (hover tooltip has refs but no standalone panel)
- [x] SP-010-P3-memo: Add `React.memo` to EditorTabs, EditorBreadcrumb, EditorToolbar, and other child components
- [x] SP-010-P3-symbolKey: Fix symbol extraction to be keyed to content checksum, not cursor position
- [x] SP-010-P3-tabTooltips: Add `title` attribute to tab names showing full file path on hover
- [x] SP-010-P3-paneLimit: Remove 3-pane cap in `EditorManagerContext.tsx` — allow up to 6 configurable panes
- [x] SP-010-P3-tabIcons: Add file-type icons in editor tabs based on extension

## Partially Done — SP-014: Agent Terminal Sessions

- [x] SP-014-A-sentinelExec: Create `pkg/webui/terminal_agent_exec.go` — sentinel-based synchronous command execution via PTY
- [x] SP-014-A-terminalTypes: Add `Hidden`, `Owner`, `ChatID`, `Name`, `AutoClose` fields to `TerminalSession`; add `CreateHiddenSession()`, `ListHiddenSessions()`
- [x] SP-014-A-apiEndpoints: Create `pkg/webui/api_agent_sessions.go` — REST endpoints: list hidden sessions, promote to visible, retrieve output
- [x] SP-014-A-lifecycle: Exclude hidden sessions from default listing; 2-hour cleanup timeout for background sessions
- [x] SP-014-A-routes: Register agent session API routes in `server.go`
- [x] SP-014-B-shellRouting: Route agent `shell_command` through hidden PTY when `TerminalManager` is available (WebUI mode)
- [x] SP-014-B-background: Add `background` parameter to `shell_command` tool; handle `background=true` to write to hidden PTY and return session ID immediately
- [x] SP-014-B-sessionID: Add `session_id` parameter for querying accumulated output of background sessions
- [x] SP-014-B-context: Expose `TerminalManager` accessor in `client_context.go` for agent context wiring
- [x] SP-014-C-backgroundPanel: Create collapsible panel showing running background sessions with status, output preview, Attach/Kill buttons
- [x] SP-014-C-terminalWire: Wire background tasks panel into `Terminal.tsx`; add "Agent Sessions" dropdown in `TerminalTabBar.tsx`
- [x] SP-014-C-attachFlow: Implement promote-to-visible flow (clear `Hidden` flag → session appears in tab bar → scrollback replay)

## Partially Done — SP-015: Cloud Platform Integration

- [x] SP-015-R1: Add WASM interception in `CloudAdapter.fetch()` — check `isWasmLocal()` and route to WASM shell methods instead of `fetch()` (17 endpoints currently fall through)
- [x] SP-015-R3: Audit components referencing SSH, instances, local terminal, or settings; ensure they use `supports*` flags from `mode.ts`
- [x] SP-015-R5: Verify WebSocket client correctly handles all three patterns (transparent reverse proxy, JSON-over-WS tunnel, SSE + MessageChannel)
- [x] SP-015-R6: Define canonical dist bundle layout; ensure `build-webui-dist.mjs` output matches Foundry's `browser-ide/dist/sprout-webui/` expectations
- [x] SP-015-R7-edgeCases: Add edge case tests for chat translation: empty query, missing chat_id, steer, stop signals
- [x] SP-015-R7-sharedModule: Extract chat translation logic into shared module (CloudAdapter + chat-bridge.ts both do same translation)

## Partially Done — SP-039: UI Library Consolidation

- [x] SP-039-2a: Move `BillingPage`, `TasksPage`, `TeamPage` from `packages/ui` to `webui/src/components/`
- [x] SP-039-2a-imports: Update all imports across both packages after composite migration
- [x] SP-039-2b: Audit for any other domain-coupled components hiding in `packages/ui` primitives
- [x] SP-039-3: Replace duplicate component implementations in webui with imports from `@sprout/ui` (~30 components exist in both locations)

## Partially Done — SP-045: WASM Feature Parity

- [x] SP-045-Tier1-conversationTurns: Wire conversation turn persistence to `SproutWasm` JS entry point
- [x] SP-045-Tier1-config: Expose `getConfig` and `setConfigValue` on `SproutWasm`
- [x] SP-045-Tier1-workspaceAnalysis: Expose direct JS API for workspace file walk on `SproutWasm`
- [x] SP-045-Tier2a-onnxBridge: Implement `syscall/js` bridge for `onnxruntime-web` — detect `globalThis.__sproutONNX`, marshal embed/embedBatch calls
- [x] SP-045-Tier2b-agent: Plumb `agent` command through WASM (HTTP to LLM providers via Fetch API)
- [x] SP-045-Tier2b-llmCommands: Plumb `question`, `code`, `commit`, `review`, `plan` commands through WASM
- [x] SP-045-Tier2b-apiKeys: Implement WASM credential storage (localStorage / IndexedDB + Web Crypto AES-GCM / host-page injection)
- [x] SP-045-Tier2b-streaming: Verify `js/wasm` net/http handles SSE streaming end-to-end; adapt provider code for Fetch API streaming
- [x] SP-045-Tier2b-cors: Handle provider CORS restrictions; support user-supplied proxy URL
- [x] SP-045-Tier2b-toolExec: Route agent loop tool execution through `SproutWasm.executeCommand` JS bridge
- [x] SP-045-buildMatrix-pty: Tag `pkg/webui/terminal_*.go` with `!js` build constraint to avoid `creack/pty` import
- [x] SP-045-buildMatrix-sweep: Replace `//go:build !windows` patterns with `unix && !js` across `pkg/`
- [x] SP-045-dist-ldflags: Add `ldflags="-s -w"` to strip symbols (~25% size saving)
- [x] SP-045-dist-tinygo: Spike tinygo feasibility for WASM build (huge saving, compatibility risk) — **NOT FEASIBLE**. 480 deps (194 3rd-party), heavy syscall/js + reflect + net/http usage incompatible with TinyGo. Better: gzip compression (59→24MB), lazy module splitting (see SP-045-dist-splitModules).
- [x] SP-045-dist-splitModules: Investigate splitting into small shell-only WASM + larger `embedding.wasm` lazy-load — **FEASIBLE but complex**. Go WASM lacks dynamic linking; requires 2 separate compiled modules communicating via JS bridge. Shell-only (~5-10MB estimated) vs full (~60MB). Recommend: defer to post-MVP; start with gzip'd full module (~24MB); add shell-only entry point if initial load time is problematic.

## Partially Done — SP-046: Workspace Sync Model

- [x] SP-046-sync-protocol-ws: Implement WebSocket patch stream (Container → Browser) — each tool-call write emits one patch event
- [x] SP-046-sync-protocol-http: Implement HTTP POST per op (Browser → Container) — browser queues outbound ops in OPFS, flushes when WS is up
- [x] SP-046-sync-heartbeat: Implement 15s heartbeat ping; container terminates job after 60s missed heartbeat
- [x] SP-046-conflict-browserWriteOnContainerPatch: On receiving container patch with unsynced browser edits, write as `<path>.theirs` and show git-style conflict marker UI
- [x] SP-046-conflict-agentWriteRefusal: Agent's `write_file` tool wrapper refuses write if `browser_seq > last_synced_browser`
- [x] SP-046-opfs-replica: Implement OPFS browser-side replica with file-level metadata
- [x] SP-046-multiDevice: Implement single-active-session enforcement — second device gets "Take over?" prompt
- [x] SP-046-firstLoad: Implement cold-hydrate progress bar for new device first-load (container → browser via WS)
- [x] SP-046-stalenessRule: Implement 30s re-read invariant in `write_file` tool wrapper
- [x] SP-046-freeTierDegradation: Ensure sync protocol degenerates cleanly for free-tier (browser is sole authority, WASM-side tool handlers write directly to OPFS)
- [x] SP-046-failureModes: Implement recovery paths: container death (reconnect + seq reconciliation), browser crash (OPFS persist + seq replay), volume corruption (git clone + replay)

## Partially Done — SP-048: CLI Delight

- [x] SP-048-4a: Honor `NO_COLOR` / `FORCE_COLOR` in `NewMarkdownFormatter`; stop unsetting `NO_COLOR` in `agent_exec_utils.go`
- [x] SP-048-4b: Bold the capitalized default letter in `[y/N]` prompts and the safe default option in 4-choice secret prompt
- [x] SP-048-4c: When bracketed paste delivers >100 lines or >5KB, show confirmation: `[Use] [Save as file & reference] [Cancel]`
- [x] SP-048-4e: Implement Ctrl-R reverse history search (incremental substring search over history) — requires state machine in raw-mode read loop
- [x] SP-048-4f: Implement `$EDITOR` escape via Ctrl-X Ctrl-E or `/edit` — open `$EDITOR` with current buffer pre-filled
- [x] SP-048-5a: After each assistant turn, print dim line: `⎯ this turn: 1.2k in / 4.8k out · $0.04 · 6.1s ⎯`
- [x] SP-048-5b: Implement `/help <command>` per-command usage text
- [x] SP-048-5c: Add short aliases: `/m` → `/models`, `/p` → `/providers`, `/x` → `/exit`, `/?` → `/help`
- [x] SP-048-5d: Strip ANSI from non-TTY stdout when piped

## Not Started — SP-006: Delegate Tool

- [x] SP-006-A-types: Create `pkg/agent/delegate_types.go` — `DelegateResult`, `DelegateConfig`, `ToolCallRecord` types
- [x] SP-006-A-factory: Create `pkg/agent/delegate_factory.go` — `CreateDelegateAgent(parent, cfg)` via `NewAgentWithLayers` + role overlay
- [x] SP-006-A-streamBridge: Create `pkg/agent/delegate_stream.go` — `DelegateStreamBridge` event bus bridge
- [x] SP-006-A-handler: Create `pkg/agent/tool_handlers_delegate.go` — tool handler + registration
- [x] SP-006-A-toolDef: Register `delegate` tool in `tool_definitions.go` with params: prompt, role, provider, model, tools, context, max_iterations, files
- [x] SP-006-A-events: Add delegate event types to `pkg/events/events.go`
- [x] SP-006-A-nestingLimit: Implement max nesting depth via `SPROUT_MAX_DELEGATE_DEPTH=3` env var
- [x] SP-006-B-render: Render `delegate_activity` events in WebUI (expandable tool call tree)
- [x] SP-006-B-costDisplay: Show delegate cost/token accumulation in real-time
- [x] SP-006-C-followUp: Allow parent to inject follow-up messages into running delegate (future)
- [x] SP-006-C-interactive: Support interactive delegation — not just blocking (future)
- [x] SP-006-C-clarification: Allow delegate to request clarification from parent via event bus (future)

## Not Started — SP-007: Extend Configuration

- [x] SP-007-1-roleSchema: Create `pkg/configuration/role.go` — `RoleConfig`, `RoleMeta`, `RoleToolsConfig`, `RoleSkillsConfig`, `RoleConstraints` types + `MergeRoleConfig()`
- [x] SP-007-1-roleManager: Create `pkg/configuration/role_manager.go` — `RoleManager` with resolution chain, `Resolve()`, `List()`, `Save()`, `Delete()`
- [x] SP-007-1-roleTests: Create `pkg/configuration/role_test.go` — unit tests for resolution, merge, save
- [x] SP-007-1-configModify: Modify `pkg/configuration/config.go` — add `~/.sprout/roles/` and `{workspace}/.sprout/roles/` support
- [x] SP-007-1-personaModify: Extend `GetSubagentType()` to check RoleManager before falling back to existing `subagent_types`
- [x] SP-007-2-extendHandler: Create `pkg/agent/extend_handler.go` — guided collaborative configuration session (7-question flow)
- [x] SP-007-2-extendTests: Create `pkg/agent/extend_handler_test.go`
- [x] SP-007-2-wireCommand: Wire `/extend` into command routing in `conversation_handler.go`
- [x] SP-007-3-webuiSettings: Settings panel for role CRUD; visual role editor (future)
- [x] SP-007-3-apiEndpoints: Add REST endpoints for role CRUD: `GET/PUT/DELETE /api/settings/roles/{name}` (future)
- [x] SP-007-3-roleSelector: Role selector in agent persona picker (future)

## Not Started — SP-008: Reliability Engineering

- [x] SP-008-A1: Replace direct method-call-from-goroutine patterns with channel-based communication for `ProcessQuery` → tool executor feedback loop
- [x] SP-008-A2: Systematic audit of every field access in concurrent code paths — verify correct mutex, document invariant
- [x] SP-008-A3-raceDefault: Add `-race` to default `make test` target
- [x] SP-008-A3-raceTests: Create `pkg/agent/concurrency_test.go` — focused race detection tests
- [x] SP-008-A3-ciRace: Remove `-short` from CI race detector step
- [x] SP-008-B1-errorTypes: Create `pkg/errors/types.go` — `TransientError`, `RateLimitError`, `SecurityViolationError`, `InvalidInputError`, `ContextOverflowError`, `AuthError`
- [x] SP-008-B2-structuredLog: Create `pkg/logging/structured.go` — `StructuredLogger` interface with `WithContext()`, `WithProvider()`, `WithTool()` methods
- [x] SP-008-B3-migrateLifecycle: Migrate agent lifecycle event logging to structured logger
- [x] SP-008-B3-migrateToolExec: Migrate tool execution lifecycle logging to structured logger
- [x] SP-008-B3-migrateConversation: Migrate conversation flow logging to structured logger
- [x] SP-008-B3-migrateRemaining: Migrate all remaining `fmt.Printf` calls in `pkg/agent/` to structured logger
- [x] SP-008-B4-retryLogic: Create `pkg/agent/retry.go` — `handleToolError()` with typed error classification
- [x] SP-008-B4-apiTypedErrors: Replace string matching in `ErrorHandler.HandleAPIFailure()` with typed errors

## Not Started — SP-011: Terminal Parity

- [x] SP-011-P1.1-ptyExit: In `TerminalPane.tsx`, handle `pty_exit`: write `[Process exited]`, set pane disconnected, close WebSocket cleanly
- [x] SP-011-P1.1-parentHandle: In `Terminal.tsx`, implement `handlePaneExit`: auto-close secondary split pane, auto-restart last session, close tab in multi-session pane
- [x] SP-011-P1.2-perPaneModel: Replace flat session model with `PaneState` (id, sessions[], activeSessionId) — each split pane has its own tab bar
- [x] SP-011-P1.2-rendering: Render per-pane tab bars and `TerminalPane` components mapped from `PaneState[]`
- [x] SP-011-P1.2-splitBehavior: Implement `toggleSplit()`, `addSessionToPane()`, `closeSessionInPane()`, `switchSessionInPane()`
- [x] SP-011-P1.3-removeButtons: Remove global (+) shell picker from tab bar when split is active; each pane's tab bar gets its own (+) button
- [x] SP-011-P2.1-exitedCSS: Add `.terminal-tab.exited` CSS (opacity 0.5, italic); show "Session ended." before auto-restart
- [x] SP-011-P2.2-zoomVerify: Verify ZoomIn/ZoomOut buttons are correctly visible and persist to localStorage
- [x] SP-011-P2.3-cleanupPackagesUI: Evaluate `packages/ui/src/components/Terminal.tsx` — update to match new per-pane model or remove if unused
- [x] SP-011-P3.1-terminalSearch: Install `@xterm/addon-search`; add search bar above terminal pane with match counter
- [x] SP-011-P3.2-clickablePaths: Detect `./foo.go:12:34` patterns in terminal output; dispatch event to open file in editor
- [x] SP-011-P3.3-copyOnSelect: Auto-copy selected text to clipboard
- [x] SP-011-P3.4-scrollbackPersistence: Save terminal buffer to IndexedDB on unmount; restore on reconnect

## Not Started — SP-012: UX Polish

- [x] SP-012-U0-inlineBadges: Implement inline tool call badges in assistant messages — `display: inline-flex`, 11px font, click opens tool in context sidebar
- [x] SP-012-U1.1-notificationCenter: Create `NotificationCenter.tsx` — history panel with bell icon in StatusBar, dismiss individual/all, copy message
- [x] SP-012-U1.1-markAllRead: Add `markAllRead()` method to notification bus
- [x] SP-012-U1.1-statusBarBell: Add bell icon with unread count badge to `StatusBar.tsx`
- [x] SP-012-U1.2-reducedMotion: Add `@media (prefers-reduced-motion: reduce)` CSS block — force zero animation/transition duration
- [x] SP-012-U1.3-fileTreeARIA: Add `role="treeitem"` and `aria-expanded` to FileTree items
- [x] SP-012-U1.3-terminalARIA: Add `aria-label` to TerminalPane container
- [x] SP-012-U1.3-commandPaletteARIA: Add `aria-live` region to CommandPalette results list
- [x] SP-012-U1.3-chatPanelARIA: Add `role="log"` and `aria-label` to ChatPanel messages
- [x] SP-012-U1.3-editorTabsARIA: Add `aria-label="Close {filename}"` to tab close buttons
- [x] SP-012-U1.3-sidebarARIA: Add `role="navigation"` to Sidebar navigation sections
- [x] SP-012-U1.4-focusIndicators: Add global `:focus-visible` outline styles and `:focus:not(:focus-visible)` outline removal
- [x] SP-012-U2.1-remove3Pane: Change 3-pane cap to configurable `MAX_PANES` (default 6); add minimum pane width enforcement
- [x] SP-012-U2.2-sidebarPersist: Ensure `isCollapsed`, `activeTab`, and `width` in `useSidebarState.ts` all persist to localStorage
- [x] SP-012-U2.3-responsiveCSS: Add tablet (768-1024px) and mobile (<768px) responsive breakpoints — icons-only sidebar, vertical stacking
- [x] SP-012-U2.3-touchGestures: Add swipe left/right to toggle sidebar on mobile
- [x] SP-012-U2.4-skeletons: Create skeleton placeholder components for FileTree, Chat history, Editor, Settings panel loading states
- [x] SP-012-U2.5-panelAnimation: Add `transition: width 200ms, opacity 150ms` to sidebar collapse, context panel resize, terminal toggle

## Not Started — SP-013: Agent Settings Management Tool

- [x] SP-013-1-toolDef: Register `manage_settings` tool with 4 params: operation (get/set/list_providers/test_credential), key, value, provider
- [x] SP-013-1-get: Implement `get` operation — return full config summary or specific key with type + valid values
- [x] SP-013-1-listProviders: Implement `list_providers` — one line per provider with credential status and current model
- [x] SP-013-1-testCredential: Implement `test_credential` — lightweight API validation with 60s cooldown per provider
- [x] SP-013-1-handlers: Create `pkg/agent/tool_handlers_settings.go` with all handler implementations
- [x] SP-013-1-validation: Implement key whitelist, type coercion, provider lookup, unknown-key guidance message
- [x] SP-013-2-set: Implement `set` — type coercion, whitelist validation, invalid-value guidance with valid options
- [x] SP-013-2-providerModel: Special path for `provider`/`model`: session override via `SetProvider()`/`SetModel()`
- [x] SP-013-2-persistOther: All other keys persist via `cm.UpdateConfig()`
- [x] SP-013-2-getter: Add `GetConfigManager()` accessor on Agent if not already public
- [x] SP-013-3-verifyEnums: Verify all enum fields return valid-value lists on bad input
- [x] SP-013-3-verifyConfirm: Verify `set` confirms change and mentions related keys
- [x] SP-013-3-verifySecurity: Verify API keys never appear in output

## Not Started — SP-017: Settings Panel Rework

- [x] SP-017-S1-dataModel: Replace `SUB_TABS` in `settings/types.ts` with `SettingsSection` and `SettingsSubsection` interface structure
- [x] SP-017-S2-navigation: Rewrite `SettingsPanel.tsx` navigation — collapsible section headers with chevron toggle, replace tab bar
- [x] SP-017-S3-mergeTabs: Merge Security → Agent > Behavior, Performance → Environment > Performance, OCR → Environment > OCR, CommitReview → Environment > Commit & Review
- [x] SP-017-S3-splitGeneral: Split `GeneralSettingsTab` — editor prefs → Editor section, behavior → Agent section
- [x] SP-017-S4-providerModel: Create `ProviderModelSubsection.tsx` — reusable provider/model picker with inherited values and "Override" button
- [x] SP-017-S5-removeLayerPicker: Delete session/workspace/global toggle buttons; scope determined by which section user edits
- [x] SP-017-S6-css: Add collapsible section styles: section headers with chevron + scope badge, expand/collapse animation, subsection indentation
- [x] SP-017-S6-scopeBadges: Implement colored scope badges: Session=blue, Workspace=green, Global=orange, Runtime=gray

## Not Started — SP-022: Workspace Management

- [x] SP-022-W1.1-detect: Create `pkg/webui/project_detect.go` — `IsProjectDirectory()`, `FindNearestProjectRoot()`, `FindProjectsInDirectory()`
- [x] SP-022-W1.2-validate: At server startup, validate `workspaceRoot` is a project directory; auto-correct to nearest project root if not
- [x] SP-022-W1.3-restore: On first connection for new client, restore workspace from most recent session's `WorkingDirectory`
- [x] SP-022-W1.4-recent: Create `pkg/webui/recent_workspaces.go` — `GetRecentWorkspaces()` (max 10, LRU eviction), storage in `~/.sprout/recent_workspaces.json`
- [x] SP-022-W1.5-apiResponse: Enhance `GET /api/workspace` with `is_project`, `project_markers`, `needs_workspace_selection`, `suggested_projects` fields
- [x] SP-022-W1.5-projectsEndpoint: Add `GET /api/workspace/projects` endpoint
- [x] SP-022-W2.1-picker: Create `WorkspacePicker.tsx` — shows current workspace, recent projects, nearby projects, "Browse..." button
- [x] SP-022-W2.2-autoSelect: On frontend load, auto-select if exactly 1 recent workspace; show picker if multiple or none
- [x] SP-022-W2.3-statusBar: Add workspace indicator to status bar; clicking opens workspace picker
- [x] SP-022-W2.4-locationSwitcher: Wire workspace picker into `LocationSwitcher.tsx` for sidebar-based workspace switching
- [x] SP-022-W2.5-welcomeTab: Show workspace picker in `WelcomeTab.tsx` when no workspace detected
- [x] SP-022-W2.6-workspaceApi: Add new API calls in `workspaceApi.ts` and types in `types.ts`

## Not Started — SP-025: Tree-Sitter Integration

- [ ] SP-025-P1-deps: Add `odvcencio/gotreesitter` to go.mod
- [ ] SP-025-P1-parser: Create `pkg/ast/parser.go` — `ParseFile(path, content) (*ASTResult, error)`
- [ ] SP-025-P1-grammarBlobs: Pre-compile grammar blobs for Go, TypeScript, JavaScript, Python
- [ ] SP-025-P1-symbols: Create `pkg/ast/symbols.go` — walk AST, extract top-level symbols with line numbers, scopes, kinds
- [ ] SP-025-P1-cache: Create `pkg/ast/cache.go` — grammar blob caching
- [ ] SP-025-P1-tests: Test parsing Go, TS, JS, Python; verify symbol names, line numbers, scopes
- [ ] SP-025-P1-wasmBuild: Verify `GOOS=js GOARCH=wasm go build ./cmd/wasm/` still works
- [ ] SP-025-P2-repoMap: Update `repo_map.go` to use `pkg/ast` for Go, TS, JS, Python
- [ ] SP-025-P2-removeRegex: Remove regex patterns from repo_map.go
- [ ] SP-025-P2-tests: Update repo map tests with AST-derived expected line numbers
- [ ] SP-025-P3-symbolIndex: Update `pkg/index/symbols.go` to use `pkg/ast` for all supported languages
- [ ] SP-025-P4-wasmVerify: Ensure `pkg/ast` compiles for GOOS=js GOARCH=wasm
- [ ] SP-025-P4-grammarCacheWasm: Implement grammar blob caching for WASM browser storage
- [ ] SP-025-P4-wasmShell: Add `pkg/ast` to WASM shell for code intelligence features
- [ ] SP-025-P4-sizeCheck: Verify WASM binary size impact; set acceptable threshold
- [ ] SP-025-P5-embeddingExtract: Wire `pkg/ast` into embedding extractor for accurate code unit extraction (deferred)
- [ ] SP-025-P5-functionBodies: Extract function bodies using AST scope information (deferred)

## Not Started — SP-049: Shell Permission Overhaul

- [ ] SP-049-riskCategories: Define granular risk categories beyond Safe/Caution/Dangerous: read-only, file-write, network, process-management, destructive, privileged
- [ ] SP-049-allowlist: Implement user-configurable tool allow-list (`allowed_tools` per persona/workspace)
- [ ] SP-049-approvals: Implement tiered approval: Safe=auto, Caution=auto-with-log, Danger=user-prompt, Custom=user-configurable threshold
- [ ] SP-049-ui-visual: Visual risk indicators in tool call rendering (green/yellow/red)
- [ ] SP-049-workspacePolicy: Per-workspace security policies (`.sprout/security-policy.json`)
- [ ] SP-049-auditLog: Audit log of all security decisions (`~/.sprout/audit.log`)

## Not Started — SP-050: Orchestrator Persona Collapse

- [x] SP-050-legacyAliases: Fully collapse `repo_orchestrator` alias to `orchestrator` — remove all code paths that treat them as distinct
- [x] SP-050-unifiedPrompt: Write unified `orchestrator.md` system prompt that handles both repo-scoped and project-scoped coordination
- [x] SP-050-gitWrite: Merge `allow_orchestrator_git_write` and `allow_repo_orchestrator_git_write` into single config
- [x] SP-050-personaRemoval: Remove `repo_orchestrator` from persona registry; update all references
- [x] SP-050-uiUpdate: Update all WebUI references (status bar, session labels, persona selector) to use only `orchestrator`
- [x] SP-050-configMigration: Auto-migrate existing configs that reference `repo_orchestrator` to `orchestrator`
- [x] SP-050-defaultPersona: Set `orchestrator` as default persona in all contexts where `repo_orchestrator` was default

## Not Started — SP-051: Depth-Aware Subagent UI

- [x] SP-051-depthIndicator: Add visual depth indicator to `SubagentActivityFeed` — show nesting depth next to each entry
- [x] SP-051-treeView: Implement tree/nested view for subagent activity — parent-child grouping, collapsible nesting levels
- [ ] SP-051-resourceUsage: Display per-depth-level resource usage: tokens consumed, time elapsed, cost
- [ ] SP-051-contextSidebar: Add depth information to tool execution entries in context sidebar
- [ ] SP-051-subagentTreeComponent: Create `SubagentTree` component showing hierarchical relationship
- [ ] SP-051-statusIcons: Different status icons/colors for different depth levels

## Not Started — SP-053: WebUI CLI Parity

- [ ] SP-053-1d: Manual verification — install + start the daemon, open a web terminal, kick off an agent query, run `systemctl --user stop sprout`. Verify `pgrep` returns empty within 15s
- [ ] SP-053-perTurnCost: Show per-turn cost line in WebUI after each assistant turn
- [ ] SP-053-modelInPrompt: Show active model name in WebUI chat input area or status bar
- [ ] SP-053-NO_COLOR: Add `NO_COLOR` support for programmatic WebUI output consumption

## Not Started — SP-054: LSP Language Coverage Expansion

- [ ] SP-054-1.1: Add `LanguageServerConfig` entries for Python, Rust, C/C++, C#, Java, Ruby, PHP, Swift, Kotlin, Dart, Lua, Shell to `DefaultLanguageServers()`
- [ ] SP-054-1.2: Add all new language IDs to `LSP_SUPPORTED_LANGUAGES` in `lspClientService.ts`
- [ ] SP-054-1.3: Add `GET /api/lsp/status` endpoint returning which language servers are available vs not installed
- [ ] SP-054-1.4: Graceful missing-server UX — clear log messages with install instructions when binary not found
- [ ] SP-054-2.1: Add `sprout lsp install <language>` and `sprout lsp list` CLI commands
- [ ] SP-054-2.2: User-configurable servers via configuration file with `languageServers` section
- [ ] SP-054-2.3: Workspace activation hints — detect `Cargo.toml`, `requirements.txt`, `*.sln`, etc. and suggest servers
- [ ] SP-054-3.1: Python semantic adapter — diagnostics via `ruff check`, hover/def/refs via LSP proxy
- [ ] SP-054-3.2: Rust semantic adapter — diagnostics via `cargo check`, hover/def/refs via LSP proxy
- [ ] SP-054-3.3: C/C++ semantic adapter — diagnostics via `clang-tidy`, hover/def/refs via LSP proxy
- [ ] SP-054-3.4: Shared `lsp_query.go` helper in `pkg/lsp/semantic/` for routing adapter queries through the LSP proxy

## Not Started — SP-056: Remove Static Embedding Provider
_Spec: roadmap/SP-056-remove-static-embeddings.md_
- [ ] SP-056-P1-deleteStatic: Delete 9 static provider files (~1,532 lines + 55 MB model blob): `static_provider.go`, `static_tokenizer.go`, `static_loader.go`, `static_model_embed.go`, `static_model_nostub.go`, `static_model_js_testmain_test.go`, `static_test.go`, `compare_embed.go`, `static_model.bin`
- [ ] SP-056-P2a-managerFields: Simplify `EmbeddingManager` struct — replace `provider *StaticProvider` + `onnxProvider` with single `provider EmbeddingProvider`; remove `onnxStore`/`onnxConvoStore`/`onnxBuilding`/`onnxBuildCancel`/`onnxBuildWG`/`onnxReady`/`onnxError`/`onnxInitWG` fields
- [ ] SP-056-P2b-initLocked: Rewrite `initLocked()` to create ONNX provider synchronously as the sole provider; remove background ONNX init goroutine; fail fast with clear error if ONNX unavailable
- [ ] SP-056-P2c-removeRRF: Remove `RRFMergeResults` function and `GetONNXConversationStore` method from `manager.go`; simplify `SearchSemantic` to query single store directly
- [ ] SP-056-P2d-closeProviderInfo: Simplify `Close()` to close single provider + store; update `ProviderInfo` to remove primary/secondary distinction
- [ ] SP-056-P3-wasmExports: Remove `setStaticModel` JS export and `setStaticModelFunc` from `cmd/wasm/embedding_funcs.go`; keep ONNX bridge exports (`buildSemanticIndex`, `searchSemantic`, etc.)
- [ ] SP-056-P4-memoryEmbed: Simplify `pkg/agent/memory_embedding.go` — remove dual-write in `EmbedMemory`/`DeleteMemoryEmbedding`, delete `BackfillMemoryONNX`, simplify `MigrateMemories` to single store
- [ ] SP-056-P5-memorySearch: Simplify `queryMemoriesAcrossStores` in `pkg/agent/memory_search_handler.go` to single-store query (no RRF merge)
- [ ] SP-056-P6-tests: Remove static provider tests (`static_test.go` already deleted in P1); remove RRF merge tests from `manager_test.go`; update memory embedding tests for single-store behavior; verify all ONNX tests still pass
- [ ] SP-056-P7-buildDocs: Remove `staticmodel` build tag from Makefile/build scripts; update `docs/WASM_API.md` to remove `setStaticModel` section and document ONNX-only path; update error messages to be provider-agnostic
