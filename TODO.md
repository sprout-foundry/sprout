# TODO

[x] - WEBUI: Add `go to file in editor` and `copy relative path` and `copy absolute path` to the file system browser and the files listed in the git view.
[x] - WEBUI: Look for additional context menu improvements throughout the experience of the Webui and then add them in a way consistent with the established patterns used by this codebase
[X] - WEBUI: Audit the hotkeys and add in any missing keys.
[x] - WEBUI: [BUG] when a review has been completed and "fixed", the next time the review process runs you see the fixed output from the previous run.
[x] - WEBUI: When running in daemon mode, the review fix process is using the cwd from the daemon, not the correct one for the workspace.
[x] - WEBUI: The fix review items process after a review needs to accept a prompt to steer the agent toward fixing the items the should be fixed, or instead have the user select a check by each item they want fixed, then have the fix process run to fix them. That will help the user steer away from "fixing" changes that were intentional, but may have been flagged in the review.
[x] - WEBUI: We need more real-time output from the subagents in the UI so that it is clear that something is happening. We can start with just piping in the log messages that would be printing out in the terminal, but ideally it would something a bit cleaner and clearer.
[x] - WEBUI: The command palette could be improved by as soon as a user starts typing is starts fuzzy finding to commands first, then filenames etc and keeps filtering as typing happens.
[x] - WEBUI: Update the git viewer to have 2 different sections. 1 for the current commit flow, and a second for viewing past commits and viewing individual file diffs or a full listview of all the file diffs for that commit. Currently there is some duplication here. Clean it up.
[x] - The maximum iterations of 1000 appears to be a hard stop, but should reset after every entered prompt. Maybe the right move is to just remove it entirely unless a user passes in a specific max iterations value
[x] - WEBUI: When a website, or a pwd gets "paused" by chrome, it changes state in ways that we are not handling correctly in the WEBUI. We need to make sure that it still works when it gets restored and that we don't lose the terminal session that was attached, or the chat session.
[x] - WEBUI: We need to add support for multiple independent chats that can be managed and concurrently run.
[x] - WEBUI: Missing costs and token counting in the status tab of the webui chat.
[x] - WEBUI: In the status tab of the webui chat, the duration is not accurate. It appears to anchor to the first time the tab is opened and never progresses
[X] - WEBUI: Selecting subagents providers and models doesn't work in the ui.
[X] - Provider and model selection should be scoped to the session so if it changes it doesn't affect all agents and just sets the last used provider / model in the config if possible, but fail cleanly if it can't update the config.
[x] - WEBUI: the git view is not auto-updating as expected when files are edited, deleted, or renamed.
[x] - WEBUI: Queued prompts need to be able to be modified.
[x] - WEBUI: prompt history is not preserved past a refresh of the browser (pressing up arrow to scroll through history). It should be using the same history mechanism as the terminal cli so not sure why the behavior is different
[x] - WEBUI: When we exectute: ctrl+n, it creates a new tab with an empty file, but it shows: Failed to load file: Bad Request and there is no way to save the new file.


### E2E Conversation Test Coverage

~Was originally marked complete as "full end to end conversation mock" but 19 existing tests only cover basic-loop and unit-level compaction. 12 of 22 conversational patterns and 8 of 20 compaction paths remain untested at the e2e level. The first item (scriptedClient expansion) is a prerequisite for most others.~ Before any major refactoring, we need more robust testing of the full solution to catch regressions as refactoring is executed.


[x] - E2E-TESTING: [FOUNDATION] Expand `scriptedClient` (from `termination_reason_test.go`) to support: sequential scripted responses with tool calls, streaming simulation, error injection, vision support, and rate limit simulation. (stub_client.go did not exist)
[x] - E2E-TESTING: [FOUNDATION] Expand `scriptedClient` (from `termination_reason_test.go`) to support: sequential scripted responses with tool calls, streaming simulation, error injection, vision support, and rate limit simulation. (stub_client.go did not exist)
[x] - E2E-TESTING: Add e2e test for tool call execution through `ProcessQuery`: model returns `tool_call` → tool executes → result appended → model sees result and continues → stops.
[x] - E2E-TESTING: Add e2e test for fallback parser through `ProcessQuery`: model returns unstructured tool text → fallback parser extracts tool → tool executes → continues.
[x] - E2E-TESTING: Add e2e test for malformed JSON tool arguments rejection through `ProcessQuery`: model returns invalid args → rejected → transient reminder → model re-emits valid args.
[x] - E2E-TESTING: Add e2e test for streaming responses through `ProcessQuery`: validate streaming callbacks fire, content accumulates in `streamingBuffer`, and buffer content is preferred over choice content.
[x] - E2E-TESTING: Add e2e test for streaming responses through `ProcessQuery`: validate streaming callbacks fire, content accumulates in `streamingBuffer`, and buffer content is preferred over choice content.
[x] - E2E-TESTING: Add e2e test for API retry/error recovery: transient error → retry with backoff → success.
[x] - E2E-TESTING: Add e2e test for API retry/error recovery: transient error → retry with backoff → success.
[x] - E2E-TESTING: Add e2e test for rate limit handling: model returns rate limit error → `RateLimitExceededError` path exercised.
[x] - E2E-TESTING: Add e2e test for input injection/interrupt mid-conversation: conversation running → user injects input via channel → input becomes new user message → conversation continues.
[x] - E2E-TESTING: Add e2e test for input injection/interrupt mid-conversation: conversation running → user injects input via channel → input becomes new user message → conversation continues.
[x] - E2E-TESTING: Add e2e test for tentative post-tool rejection through full `ProcessQuery`: model stops with tentative text after tool results → rejected → continues up to 2x.
[x] - E2E-TESTING: Add e2e test for `content_filter` finish reason: model returns `content_filter` → conversation continues instead of stopping.
[x] - E2E-TESTING: Add e2e test for OCR completion gate through `ProcessQuery`: query with OCR policy → model tries to stop → gate reminds → model calls `analyze_image_content` → allowed to stop.
[x] - E2E-TESTING: Add e2e test for self-review gate through `ProcessQuery`: completion → `runSelfReviewGate` runs → passes/blocks.
[x] - E2E-TESTING: Add e2e test for multimodal/image queries through `ProcessQuery`: query with images → `processImagesInQuery` → `stripImagesForNonVisionModels` → `prepareMessages` pipeline.
[x] - E2E-TESTING: Add e2e test for LLM-based compaction summary: wire `optimizer.SetLLMClient()` in test setup → trigger compaction → verify LLM summary path (not Go fallback) produces correct summary.
[x] - E2E-TESTING: Add e2e test for second-pass structural compaction in `prepareMessages`: checkpoint compaction insufficient → LLM structural compaction triggered (L60-76 of `conversation_messaging.go`).
[x] - E2E-TESTING: Add e2e test for redundant shell command optimization through the full `prepareMessages` pipeline.

[x] - E2E-TESTING: Add e2e test for orphaned tool result removal after compaction: checkpoint compaction runs → verify orphaned tool results from compacted ranges are removed.
[x] - E2E-TESTING: Add e2e test for file invalidation after edits: read file → optimizer caches → edit file → `InvalidateFile` called → old read not treated as redundant with new content.
[x] - E2E-TESTING: Add e2e test for checkpoint compaction actionable summary round-trip: `ProcessQuery` completes → async checkpoint records → next `ProcessQuery` triggers compaction → actionable summary injected → model sees useful context.
[x] - The codebase needs a lot of refactoring to follow SRP and to reduce file size to something more manageable.
[x] - The codebase needs a lot of refactoring to follow SRP and to reduce file size to something more manageable.

---

## Identified Gaps — Editor Features, Hotkeys, Context Menus & Code Quality

### Hotkey Gaps

[x] - HOTKEYS: [BUG] `Ctrl+D` is mapped to "delete line" but in VS Code it means "add selection to next find match". The actual VS Code delete line key is `Ctrl+Shift+K`. This conflict prevents multi-cursor find-match selection.
[x] - HOTKEYS: [BUG] `save_all_files` is defined in backend and fallback but has no case handler in AppContent's hotkey switch — pressing `Ctrl+Shift+S` does nothing.
[x] - HOTKEYS: [BUG] `split_editor_vertical` (`Ctrl+\`) is defined in backend but has no case handler in AppContent — pressing it does nothing.
[x] - HOTKEYS: [BUG] `focus_tab_4` through `focus_tab_9` are defined in the backend but have no case handlers in AppContent — `Ctrl+4` through `Ctrl+9` do nothing.
[x] - HOTKEYS: [BUG] `focus_tab_4` through `focus_tab_9` are defined in the backend but have no case handlers in AppContent — `Ctrl+4` through `Ctrl+9` do nothing.
[x] - HOTKEYS: [BUG] `focus_next_tab` (`Ctrl+Tab`) and `focus_prev_tab` (`Ctrl+Shift+Tab`) are defined in fallback only (not backend) and have no case handlers — tab cycling does not work.
[x] - HOTKEYS: [BUG] `focus_next_tab` (`Ctrl+Tab`) and `focus_prev_tab` (`Ctrl+Shift+Tab`) are defined in fallback only (not backend) and have no case handlers — tab cycling does not work.
[x] - HOTKEYS: Add missing toggle line comment (`Ctrl+/`) and toggle block comment (`Ctrl+Shift+/`) keybindings.
[x] - HOTKEYS: Add insert line below (`Ctrl+Enter`) and insert line above (`Ctrl+Shift+Enter`) keybindings.
[x] - HOTKEYS: Add select all occurrences of find match (`Ctrl+Shift+L`) keybinding.
[x] - HOTKEYS: Add go to symbol in file (`Ctrl+Shift+O`) keybinding.
[x] - HOTKEYS: Add add selection to next find match (`Ctrl+D` — correct VS Code behavior) keybinding.
[x] - HOTKEYS: Add toggle word wrap (`Alt+Z`) keybinding.
[x] - HOTKEYS: Add navigate back (`Alt+Left`) and navigate forward (`Alt+Right`) keybindings.
[x] - HOTKEYS: Add keybindings for `split_editor_horizontal`, `close_all_editors`, `close_other_editors` — currently command-palette-only with no keyboard shortcuts.
[x] - HOTKEYS: Add keybindings for panel switching (`switch_to_chat`, `switch_to_editor`, `switch_to_git`) — currently command-palette-only.
[x] - HOTKEYS: Add insert cursor above (`Ctrl+Alt+Up`) and insert cursor below (`Ctrl+Alt+Down`) for multi-cursor editing.

### Context Menu Gaps

[x] - CONTEXT MENU: Add context menu to **SearchView** results — should support "Copy match text", "Open in editor at match line", "Copy file path", and "Exclude file/folder from search".
[x] - CONTEXT MENU: Add context menu to **EditorPane** (CodeMirror area) — at minimum: "Reveal in File Explorer", "Copy relative path", "Copy absolute path". Later: "Go to Definition", "Rename Symbol", "Find All References" when LSP is available.
[x] - CONTEXT MENU: Add context menu to **Terminal** — should support "Copy" (selection), "Paste", "Clear Terminal", "Select All", "Copy Link" (for terminal URLs).
[] - CONTEXT MENU: Add context menu to **Chat messages** — should support "Copy message", "Copy code block" (right-clicking code sections), "Insert at cursor" (re-inject message into input).
[] - CONTEXT MENU: Add context menu to **GitHistoryPanel** — should support "Copy commit SHA", "Copy commit message", "Checkout this commit", "Revert commit".
[] - CONTEXT MENU: Add context menu to **FileTree empty/background area** — right-clicking blank space should offer "New File" and "New Folder" at the workspace root.
[] - CONTEXT MENU: Add context menu to **EditorTabs empty bar area** — right-clicking the empty tab strip should offer "Close All Tabs".
[] - CONTEXT MENU: Extract the hand-rolled context menu pattern (current `createPortal`-based approach repeated in FileTree, EditorTabs, ChatTabBar, GitSidebarPanel) into a shared/reusable `ContextMenu` component to reduce duplication and ensure consistent styling/behavior.

### Editor Feature Gaps

[] - EDITOR: Enable multi-cursor editing — CodeMirror 6 supports it natively but Alt+Click and rectangular selection are not wired up.
[] - EDITOR: Enable the in-file find & replace panel from `@codemirror/search` — only `search()` is loaded, not `replace`/`replaceKeymap`. The global `SearchView` replace exists but the standard `Ctrl+H` in-editor replace panel is not functional.
[] - EDITOR: Use `@codemirror/lint` (installed but zero imports) — the package is available at v6.9.2 but never used. Should be wired up to enable diagnostics/error squiggles from linters or the LSP.
[] - EDITOR: Wire `@codemirror/lang-wast` (installed but unused) into the `getLanguageSupport()` extension-to-language switch.
[] - EDITOR: Add missing language support extensions — no syntax highlighting for Rust, C/C++, Java, Ruby, Shell/Bash, YAML, TOML, XML, SQL, Dockerfile, and many other common file types. Need to add corresponding `@codemirror/lang-*` packages and switch-case entries.
[] - EDITOR: Add language mode switcher UI — currently language is detected by file extension only; there is no way for the user to manually override the language mode.
[] - EDITOR: Make word wrap toggleable — currently `EditorView.lineWrapping` is hardcoded on. Add an `Alt+Z` toggle and a toolbar/menu option.
[] - EDITOR: Add indentation guides — no visible indent markers. Would benefit from a `indent-guides` extension or custom decoration.
[] - EDITOR: Add breadcrumb navigation bar — no breadcrumb row showing file path or symbol context above the editor.
[] - EDITOR: Add linked scrolling for split panes — when the same file is open in multiple panes, there is no option to sync scroll positions.
[] - EDITOR: Add minimap — no minimap extension. Requires `@codemirror/minimap` or a custom implementation.
[] - EDITOR: Add snippet support (expand `for`, `ifn`, etc. with tab-stop navigation through placeholders).
[] - EDITOR: Add bracket colorization — no distinct colors for nested bracket pairs (only matching-bracket highlight exists).
[] - EDITOR: Implement `'split-grid'` layout type — defined in `PaneLayout` type but not rendered in the layout logic in AppContent.

### Terminal & File Pane Gaps

[] - TERMINAL: Add terminal tabs to support 3+ terminal sessions — currently the model is binary (0 or 2 side-by-side panes). Need a tab bar with named sessions and add/remove cycle.
[] - TERMINAL: Add vertical terminal split option — currently only horizontal split is supported.
[] - TERMINAL: Persist terminal height to `localStorage` — always resets to 400px on mount. Sidebar and context panel widths are already persisted; terminal height should be too.
[] - TERMINAL: Allow user to choose shell profile for new terminal instances (e.g., bash, zsh, fish).
[] - FILE TREE: Add search/filter input to the file tree — currently there is no way to filter or fuzzy-find within the file tree (the command palette does project-wide file search, but not the tree itself).
[] - FILE TREE: Add `.gitignore`-aware toggle — currently ignored files are sorted to the bottom but always visible. Add a toggle to hide them.
[] - FILE TREE: Add drag-and-drop support — no ability to move files between folders via drag-and-drop. Currently files can only be moved via the rename operation.

### Layout & Persistence Gaps

[] - LAYOUT: Persist editor split pane sizes and layout type to `localStorage` — sidebar and context panel widths are persisted, but editor `paneSizes` and `PaneLayout` are ephemeral React state that resets on page reload.
[] - LAYOUT: Implement layout save/restore — all layout state (pane arrangement, sizes, open files/tabs with their positions, cursor/scroll positions, terminal height) is ephemeral and lost on reload. This is the single biggest UX gap for returning users.
[] - LAYOUT: Optionally restore the set of open files and their tab/pane arrangement on page load.

### Code Quality & Structural Improvements

[] - REFACTOR: Break up `App.tsx` (1,987 lines) — this monolithic file likely contains types, state, callbacks, and rendering that should be extracted into separate modules, custom hooks, and smaller components. It is the largest file in the project.
[] - REFACTOR: Break up `AppContent.tsx` (1,140 lines) — the layout rendering, pane management, and hotkey handling are heavily intertwined and should be decomposed.
[] - REFACTOR: Break up `git_api.go` (1,861 lines) — this is the largest Go file in `pkg/webui/` and likely combines multiple API endpoints that could be split by domain (status, staging, commit, history).
[] - REFACTOR: Break up `tool_executor.go` (1,353 lines) — the agent tool executor has grown large and could benefit from splitting by tool category or lifecycle stage.
[] - REFACTOR: Break up `EditorManagerContext.tsx` (817 lines) — consider extracting buffer persistence (save/load) and buffer mutation operations into separate hooks or modules.
[] - CODE QUALITY: Adopt a frontend linting setup — currently there is no ESLint config file, no Prettier config, and only a minimal `eslintConfig` in package.json. For a React/TypeScript project of this size, a proper linting and formatting setup is essential for consistency.
[] - CODE QUALITY: Reduce excessive `console.error/warn` logging — there are 80+ `console.error` and `console.warn` calls scattered across frontend components. Many of these should be replaced with a proper logging service (the `utils/log.ts` file exists but is not widely used) to allow configurable log levels, filtering, and error reporting.
[] - CODE QUALITY: Reduce silent error swallowing — many catch blocks use `catch {}`, `catch { /* ignore */ }`, or `.catch(() => {})` which silently discard errors. At minimum, these should log at debug/warn level so issues are not invisible during development.
[] - CODE QUALITY: Improve test coverage across low-coverage packages — `pkg/credentials` (20.0%), `pkg/interfaces/types` (34.8%), `pkg/trace` (48.2%), `pkg/validation` (0%), `pkg/git` (65.9%) have notably low coverage. Several files in `cmd/` have 0% function coverage (copilot.go, plan.go, log.go, diag.go, review_staged.go, github_setup_prompt.go).
[] - CODE QUALITY: Use standardized error handling in Go — inconsistent patterns of `fmt.Errorf` vs `errors.New` vs returning bare errors across packages. Adopt a project-wide convention (e.g., always use `fmt.Errorf("context: %w", err)` for wrapped errors).
[] - CODE QUALITY: Clean up duplicate TODO.md entries — there are 5+ duplicate entries in the existing TODO.md (e.g., daemon cwd fix, review item steering, chrome pause handling, git viewer sections) that should be deduplicated.
[] - CODE QUALITY: Add proper TypeScript strict mode auditing — `tsconfig.json` has `strict: true` but there is no CI step that fails on type errors. Ensure `tsc --noEmit` runs as part of CI/build checks.
[] - CODE QUALITY: Consider migrating from `React.FC` typed components to regular function components with explicit return types — `React.FC` is considered an anti-pattern in modern React (doesn't support generics well, inconsistent with plain functions).

### General UX Gaps

[] - UX: Add a proper notification/toast system — errors from saves, API failures, and background operations often only appear in `console.error`. Users need visible, dismissible notifications for important events.
[] - UX: Add keyboard-accessible menu bar (File, Edit, View, Terminal, Help) — VS Code users expect a menu bar for discoverability of features that don't have hotkey assignments.
[] - UX: Add a welcome/Getting Started tab for new users — when the editor opens with no files, show helpful content instead of a blank pane.
[] - UX: Add file drag-and-drop from OS into the editor (open dropped files).
[] - UX: Add "Unsaved changes" indicator on close — when closing a tab or the browser window, warn if there are unsaved editor buffers.
[] - UX: Add notifications for file changes detected on disk (when a file is modified externally, prompt the user to reload).
[] - UX: Add the ability to pin tabs to prevent accidental closure (type partially supported in `EditorBuffer` but no UI toggle for it).
[] - UX: Add a status bar at the bottom showing current branch, file type, encoding, line endings, indentation settings — currently cursor position is in the editor footer but there is no global status bar.
[] - UX: Add "zoom into/zoom out of terminal" controls or a font size setting for the integrated terminal.

### General

[] - WEBUI: Add support for leveraging worktrees for runnning secondary chats for scoped feature work.
[] - WEBUI: terminal randomly resetting.
