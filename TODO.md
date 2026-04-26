# TODO

[x] - WEBUI: Add `go to file in editor` and `copy relative path` and `copy absolute path` to the file system browser and the files listed in the git view.
[x] - WEBUI: Look for additional context menu improvements throughout the experience of the Webui and then add them in a way consistent with the established patterns used by this codebase
[x] - WEBUI: Audit the hotkeys and add in any missing keys.
[x] - WEBUI: [BUG] when a review has been completed and "fixed", the next time the review process runs you see the fixed output from the previous run.
[x] - WEBUI: When running in daemon mode, the review fix process is using the cwd from the daemon, not the correct one for the workspace.
[x] - WEBUI: The fix review items process after a review needs to accept a prompt to steer the agent toward fixing the items the should be fixed, or instead have the user select a check by each item they want fixed, then have the fix process run to fix them. That will help the user steer away from "fixing" changes that were intentional, but may have been flagged in the review.
[x] - WEBUI: We need more real-time output from the subagents in the UI so that it is clear that something is happening. We can start with just piping in the log messages that would be printing out in the terminal, but ideally it would something a bit cleaner and clearer.
[x] - WEBUI: The command palette could be improved by as soon as a user starts typing is starts fuzzy finding to commands first, then filenames etc and keeps filtering as typing happens.
[x] - WEBUI: Update the git viewer to have 2 different sections. 1 for the current commit flow, and a second for viewing past commits and viewing individual file diffs or a full listview of all the file diffs for that commit. Currently there is some duplication here. Clean it up.
[x] - The maximum iterations of 1000 appears to be a hard stop, but should reset after every entered prompt. Maybe the right move is to just remove it entirely unless a user passes in a specific max iterations value
[x] - WEBUI: When a website, or a pwd gets "paused" by chrome, it changes state in ways that we are not handling correctly in the WEBUI. We need to make sure that it still works when it gets restored and that we don't lose the terminal session that was attached, or the chat session. (backend has extended timeouts; frontend has freeze()/resume() methods and visibilitychange listener wired up)
[x] - WEBUI: We need to add support for multiple independent chats that can be managed and concurrently run.
[x] - WEBUI: Missing costs and token counting in the status tab of the webui chat.
[x] - WEBUI: In the status tab of the webui chat, the duration is not accurate. It appears to anchor to the first time the tab is opened and never progresses
[x] - WEBUI: Selecting subagents providers and models doesn't work in the ui.
[x] - Provider and model selection should be scoped to the session so if it changes it doesn't affect all agents and just sets the last used provider / model in the config if possible, but fail cleanly if it can't update the config. → Implemented: Session-scoped config overrides via `chatSession.ConfigOverrides` + `ConversationState.ConfigOverrides`. Settings API writes provider/model to session (not config file). `getOrCreateAgent` restores overrides from session on agent creation. Workspace config layer (`{workspace}/.ledit/config.json`) merges on top of global. See commits implementing `MergeConfig`, `NewManagerWithLayers`, `NewAgentWithLayers`.
[x] - WEBUI: the git view is not auto-updating as expected when files are edited, deleted, or renamed.
[x] - WEBUI: Queued prompts need to be able to be modified.
[x] - WEBUI: prompt history is not preserved past a refresh of the browser (pressing up arrow to scroll through history). It should be using the same history mechanism as the terminal cli so not sure why the behavior is different
[x] - WEBUI: When we exectute: ctrl+n, it creates a new tab with an empty file, but it shows: Failed to load file: Bad Request and there is no way to save the new file.
[x] - WEBUI: Content in editor can change by another process and the editor doesn't reflect the changes, or allow handling differences elegantly
[x] - WEBUI: if a security prompt would have shown in the cli, it doesn't get handled in the webui if a user is using ledit through that

### E2E Conversation Test Coverage

~Was originally marked complete as "full end to end conversation mock" but 19 existing tests only cover basic-loop and unit-level compaction. 12 of 22 conversational patterns and 8 of 20 compaction paths remain untested at the e2e level. The first item (scriptedClient expansion) is a prerequisite for most others.~ Before any major refactoring, we need more robust testing of the full solution to catch regressions as refactoring is executed.

[x] - E2E-TESTING: [FOUNDATION] Expand `scriptedClient` (from `termination_reason_test.go`) to support: sequential scripted responses with tool calls, streaming simulation, error injection, vision support, and rate limit simulation. (stub_client.go did not exist)
[x] - E2E-TESTING: Add e2e test for tool call execution through `ProcessQuery`: model returns `tool_call` → tool executes → result appended → model sees result and continues → stops.
[x] - E2E-TESTING: Add e2e test for fallback parser through `ProcessQuery`: model returns unstructured tool text → fallback parser extracts tool → tool executes → continues.
[x] - E2E-TESTING: Add e2e test for malformed JSON tool arguments rejection through `ProcessQuery`: model returns invalid args → rejected → transient reminder → model re-emits valid args.
[x] - E2E-TESTING: Add e2e test for streaming responses through `ProcessQuery`: validate streaming callbacks fire, content accumulates in `streamingBuffer`, and buffer content is preferred over choice content.
[x] - E2E-TESTING: Add e2e test for API retry/error recovery: transient error → retry with backoff → success.
[x] - E2E-TESTING: Add e2e test for rate limit handling: model returns rate limit error → `RateLimitExceededError` path exercised. (Unit tests exist in scripted_client_test.go but no e2e_rate_limit_test.go — no E2E test through ProcessQuery)
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

---

## Identified Gaps — Editor Features, Hotkeys, Context Menus & Code Quality
 
### Hotkey Gaps

[x] - HOTKEYS: [BUG] `Ctrl+D` is mapped to "delete line" but in VS Code it means "add selection to next find match". The actual VS Code delete line key is `Ctrl+Shift+K`. This conflict prevents multi-cursor find-match selection.
[x] - HOTKEYS: [BUG] `save_all_files` is defined in backend and fallback but has no case handler in AppContent's hotkey switch — pressing `Ctrl+Shift+S` does nothing.
[x] - HOTKEYS: [BUG] `split_editor_vertical` (`Ctrl+\`) is defined in backend but has no case handler in AppContent — pressing it does nothing.
[x] - HOTKEYS: [BUG] `focus_tab_4` through `focus_tab_9` are defined in the backend but have no case handlers in AppContent — `Ctrl+4` through `Ctrl+9` do nothing.
[x] - HOTKEYS: [BUG] `focus_next_tab` (`Ctrl+Tab`) and `focus_prev_tab` (`Ctrl+Shift+Tab`) are defined in fallback only (not backend) and have no case handlers — tab cycling does not work.
[x] - HOTKEYS: Add missing toggle line comment (`Ctrl+/`) and toggle block comment (`Ctrl+Shift+/`) keybindings. (No @codemirror/comment package installed, no command IDs, no implementation found)
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
[x] - CONTEXT MENU: Add context menu to **Chat messages** — should support "Copy message", "Copy code block" (right-clicking code sections), "Insert at cursor" (re-inject message into input).
[x] - CONTEXT MENU: Add context menu to **GitHistoryPanel** — should support "Copy commit SHA", "Copy commit message", "Checkout this commit", "Revert commit".
[x] - CONTEXT MENU: Add context menu to **FileTree empty/background area** — right-clicking blank space should offer "New File" and "New Folder" at the workspace root.
[x] - CONTEXT MENU: Add context menu to **EditorTabs empty bar area** — right-clicking the empty tab strip should offer "Close All Tabs".
[x] - CONTEXT MENU: Extract the hand-rolled context menu pattern (current `createPortal`-based approach repeated in FileTree, EditorTabs, ChatTabBar, GitSidebarPanel) into a shared/reusable `ContextMenu` component to reduce duplication and ensure consistent styling/behavior.

### Editor Feature Gaps

[x] - EDITOR: Enable multi-cursor editing — CodeMirror 6 supports it natively but Alt+Click and rectangular selection are not wired up.
[x] - EDITOR: Enable the in-file find & replace panel from `@codemirror/search` — only `search()` is loaded, not `replace`/`replaceKeymap`. The global `SearchView` replace exists but the standard `Ctrl+H` in-editor replace panel is not functional.
[x] - EDITOR: Use `@codemirror/lint` (installed but zero imports) — the package is available at v6.9.2 but never used. Should be wired up to enable diagnostics/error squiggles from linters or the LSP.
[x] - EDITOR: Wire `@codemirror/lang-wast` (installed but unused) into the `getLanguageSupport()` extension-to-language switch.
[x] - EDITOR: Add missing language support extensions — no syntax highlighting for Rust, C/C++, Java, Ruby, Shell/Bash, YAML, TOML, XML, SQL, Dockerfile, and many other common file types. Need to add corresponding `@codemirror/lang-*` packages and switch-case entries.
[x] - EDITOR: Add language mode switcher UI — currently language is detected by file extension only; there is no way for the user to manually override the language mode.
[x] - EDITOR: Make word wrap toggleable — currently `EditorView.lineWrapping` is hardcoded on. Add an `Alt+Z` toggle and a toolbar/menu option.
[x] - EDITOR: Add indentation guides — no visible indent markers. Would benefit from a `indent-guides` extension or custom decoration.
[x] - EDITOR: Add breadcrumb navigation bar — no breadcrumb row showing file path or symbol context above the editor.
[x] - EDITOR: Add linked scrolling for split panes — when the same file is open in multiple panes, there is no option to sync scroll positions.
[x] - EDITOR: Add minimap — no minimap extension. Requires `@codemirror/minimap` or a custom implementation.
[x] - EDITOR: Add snippet support (expand `for`, `ifn`, etc. with tab-stop navigation through placeholders).
[x] - EDITOR: Add bracket colorization — no distinct colors for nested bracket pairs (only matching-bracket highlight exists).
[x] - EDITOR: Implement `'split-grid'` layout type — defined in `PaneLayout` type but not rendered in the layout logic in AppContent.
[x] - UX: Add a proper notification/toast system — errors from saves, API failures, and background operations often only appear in `console.error`. Users need visible, dismissible notifications for important events.

### Editor Quality-of-Life & Best-in-Class Feature Gaps

These gaps were identified by cross-referencing the current editor implementation (CodeMirror 6 with 7 custom extensions) against the full CM6 ecosystem and the feature sets of best-in-class editors (VS Code, JetBrains). Items are ordered by implementation complexity (easiest first).

#### Tier 1 — Single-Line Additions (CM6 already provides the extension)

[x] - EDITOR: Add `highlightActiveLineGutter()` to the editor extensions array in `EditorPane.tsx` — the CSS class `.cm-activeLineGutter` already exists and is styled via `--cm-active-line-gutter`, but the extension `highlightActiveLineGutter` from `@codemirror/view` is not imported or configured. Without it, the gutter does not visually highlight the active line number. Import from `@codemirror/view` and add to the extensions array.

[x] - EDITOR: Add `highlightSelectionMatches()` to the editor extensions array — currently when you select text, matching occurrences are not highlighted throughout the document. This is a standard feature in every modern editor (VS Code, Sublime, JetBrains). The extension `highlightSelectionMatches` is already available from `@codemirror/search` (which is installed). Import it and add to the extensions array.

[x] - EDITOR: Add `scrollPastEnd()` to the editor extensions array — when viewing a file, the last line is pinned to the bottom of the viewport with no dead space below it. `scrollPastEnd` from `@codemirror/view` (already installed) allows the user to scroll the last line up to the middle of the viewport, which is standard in VS Code and most editors. Import from `@codemirror/view` and add to the extensions array.

[x] - EDITOR: Add `dropCursor()` to the editor extensions array — when dragging text within the editor, there is no visual indicator of where the text will be dropped. The `.cm-dropCursor` CSS class exists and is themed, and the CodeMirror import `dropCursor` is available from `@codemirror/view` (not currently imported). Add it to the extensions array so a vertical line appears at the drop target during text drag operations.

[x] - EDITOR: Add `drawSelection()` to the editor extensions array — explicitly enable CodeMirror's selection drawing extension from `@codemirror/view`. While CM6 does render selections by default, importing `drawSelection` explicitly ensures consistent cross-browser selection rendering (especially for single-cursor caret-style selections) and makes the dependency visible in the code.

[x] - EDITOR: Add editor font size zoom (Cmd/Ctrl+= to zoom in, Cmd/Ctrl+- to zoom out) — the editor font is hardcoded to `13px` in `EditorPane.tsx`. Add keybindings that adjust the `--editor-font-size` CSS variable (or the inline style) and persist the preference to `localStorage`. This is an accessibility and comfort feature that every IDE supports. Implementation: add a Compartment or reactive CSS variable, register `Mod-=` and `Mod--` keybindings, and read/write from localStorage.

#### Tier 2 — Small Package Additions (npm install + a few lines of code)

[x] - EDITOR: Add relative line numbers via `@uiw/codemirror-extensions-line-numbers-relative` — currently line numbers are absolute-only. Relative line numbers (showing distance from current line) are expected by vim users and are useful for `j15`-style jump commands. Install the package, wrap in a Compartment for toggling, and add a settings entry or keybinding to enable/disable.

[x] - EDITOR: Add Emmet HTML/CSS abbreviation expansion via `@emmetio/codemirror6-plugin` — when editing HTML or CSS, Emmet allows typing abbreviations like `div.container>ul>li*3` and expanding them to full HTML. This is a massive productivity feature for web development and standard in VS Code. Install the package, configure it, and wire it into the editor extensions (only active for HTML/CSS/JSX language modes).

[x] - EDITOR: Add clickable URL support via `@uiw/codemirror-extensions-hyper-link` — URLs in comments and strings are not clickable. Install this package so Cmd/Ctrl+click on a URL opens it in a browser. Add to the editor extensions array.

[x] - EDITOR: Add color value widget via `@uiw/codemirror-extensions-color` — CSS color values like `#ff0000`, `rgb(255,0,0)`, `hsl()` are displayed as plain text with no preview. This package renders an inline color swatch and opens a color picker when clicked. Install the package and add to the editor extensions (relevant for CSS, HTML, JS, TS, and any language with color literals).

[x] - EDITOR: Add configurable tab size — currently the editor uses CodeMirror's default 4-space indent unit. Add a `EditorState.tabSize` Compartment in `EditorPane.tsx` that reads from a setting (stored in localStorage or the settings API) and allows the user to choose 2, 4, or 8 spaces. Also add a tab size indicator to the editor footer/status bar.

[x] - EDITOR: Add auto-detect indentation from opened files — when opening a file, scan the first ~100 lines to detect the most common indent style (tabs vs spaces) and indent width (2, 4, 8). Apply the detected settings to the editor's tabSize/indentUnit compartments. This ensures consistency when opening files from different projects without requiring manual configuration.

#### Tier 3 — Small Custom Extensions (ViewPlugin/StateField, ~2-4 hours each)

[x] - EDITOR: Add trailing whitespace highlighting — create a `trailingWhitespacePlugin` ViewPlugin in `webui/src/extensions/` that decorates trailing spaces and tabs on each line with a subtle background color (configurable via CSS variable). Lines with no trailing whitespace are unaffected. This is a standard feature in VS Code and Atom and helps catch whitespace-only diffs. Use viewport-decoration filtering for performance on large files.
[x] - EDITOR: Add trailing whitespace highlighting — create a `trailingWhitespacePlugin` ViewPlugin in `webui/src/extensions/` that decorates trailing spaces and tabs on each line with a subtle background color (configurable via CSS variable). Lines with no trailing whitespace are unaffected. This is a standard feature in VS Code and Atom and helps catch whitespace-only diffs. Use viewport-decoration filtering for performance on large files.

[x] - EDITOR: Add whitespace rendering mode (render tabs/spaces as visible characters) — create a `whitespaceRenderingPlugin` ViewPlugin that replaces tab characters with visible `→` symbols and trailing spaces with `·` dots. Add a setting entry to toggle between: "none" (default), "boundary" (only trailing whitespace), and "all" (all whitespace). VS Code exposes this as "Editor: Render Whitespace". Use decorations to overlay the special characters.
[x] - EDITOR: Add whitespace rendering mode (render tabs/spaces as visible characters) — create a `whitespaceRenderingPlugin` ViewPlugin that replaces tab characters with visible `→` symbols and trailing spaces with `·` dots. Add a setting entry to toggle between: "none" (default), "boundary" (only trailing whitespace), and "all" (all whitespace). VS Code exposes this as "Editor: Render Whitespace". Use decorations to overlay the special characters.

[x] - EDITOR: Add highlight for unsaved/modified lines — the diff gutter shows git changes, but there is no indicator for lines changed since the last save. Create a ViewPlugin that compares `EditorState.doc` against the buffer's `originalContent` (already tracked in `EditorManagerContext`) and adds a subtle background decoration to modified lines (similar to VS Code's minimap modified-region indicators, but inline). Use the existing `--diff-mod-color` CSS variable for visual consistency.

[x] - EDITOR: Add selection length/count to the editor footer — currently the footer shows `Ln X, Col Y` for cursor position but nothing when text is selected. Display `Ln X, Col Y (Z selected)` where Z is the character count of the selection. When multiple selections exist, show `(N selections)`. This is a trivial computation from `view.state.selection` in the existing editor update listener.
[x] - EDITOR: Add selection length/count to the editor footer — currently the footer shows `Ln X, Col Y` for cursor position but nothing when text is selected. Display `Ln X, Col Y (Z selected)` where Z is the character count of the selection. When multiple selections exist, show `(N selections)`. This is a trivial computation from `view.state.selection` in the existing editor update listener.

[x] - EDITOR: Add file encoding and line ending indicator to the editor footer — display `UTF-8 · LF` or `UTF-8 · CRLF` in the pane footer. The line ending can be detected by scanning the document for `\r\n`. The encoding is typically UTF-8 but could be detected from the file read response if the API provides it. This is informational and low-effort.
[x] - EDITOR: Add file encoding and line ending indicator to the editor footer — display `UTF-8 · LF` or `UTF-8 · CRLF` in the pane footer. The line ending can be detected by scanning the document for `\r\n`. The encoding is typically UTF-8 but could be detected from the file read response if the API provides it. This is informational and low-effort.

[x] - EDITOR: Enhance the search panel with match-case, match-whole-word, and regex toggle buttons — currently the search panel (opened via Cmd/Ctrl+F) provides basic find/replace inputs but no toggles for case sensitivity, whole-word matching, or regex mode. These are standard in every search panel. The `@codemirror/search` package does not ship visible toggle UI, so build a small panel extension that sets `SearchConfig` via the search extension's reconfiguration.
[x] - EDITOR: Enhance the search panel with match-case, match-whole-word, and regex toggle buttons — currently the search panel (opened via Cmd/Ctrl+F) provides basic find/replace inputs but no toggles for case sensitivity, whole-word matching, or regex mode. These are standard in every search panel. The `@codemirror/search` package does not ship visible toggle UI, so build a small panel extension that sets `SearchConfig` via the search extension's reconfiguration. (DUPLICATE — marked done; see line above)

[x] - EDITOR: Fix Cmd/Ctrl+S priority when search panel is open — when the CodeMirror search panel is focused, pressing Cmd/Ctrl+S does not trigger save because the search panel's keybindings consume the event. Ensure the save key binding has higher priority (use `Prec.highest` or register the save keymap after the search keymap in the extensions array so it takes precedence). This was previously attempted (see `editor替换PanelKeymap`) but may still have edge cases.
[x] - EDITOR: Fix Cmd/Ctrl+S priority when search panel is open — when the CodeMirror search panel is focused, pressing Cmd/Ctrl+S does not trigger save because the search panel's keybindings consume the event. Ensure the save key binding has higher priority (use `Prec.highest` or register the save keymap after the search keymap in the extensions array so it takes precedence). This was previously attempted (see `editor替换PanelKeymap`) but may still have edge cases.

[x] - EDITOR: Ensure multi-cursor operations work in all editor states — while `EditorState.allowMultipleSelections.of(true)` is set and `Cmd+D` / `Cmd+click` work, verify that: (1) multi-cursor undo works correctly (each cursor's edits undo together as a single transaction), (2) paste into multi-cursor inserts at all cursors, (3) find-and-replace works with multiple selections, (4) line manipulation commands (move, duplicate, delete) handle multiple cursors gracefully. Add test coverage for multi-cursor edge cases.

#### Tier 4 — Medium Features (new files/panels, ~8-16 hours each)

[x] - EDITOR: Add hover tooltips for type/signature documentation — when hovering over a token (variable, function, type), show a tooltip with the type signature and documentation. For TypeScript/JavaScript/Go, this can use the existing `apiService.getSemanticDefinition` or a new hover-API endpoint. Implement using `hoverTooltip()` from `@codemirror/tooltip` (already a transitive dependency via `@codemirror/autocomplete`). For non-LSP languages, fall back to showing basic token info or nothing. This is a core IDE feature and one of the most impactful improvements.
[x] - EDITOR: Add hover tooltips for type/signature documentation — when hovering over a token (variable, function, type), show a tooltip with the type signature and documentation. For TypeScript/JavaScript/Go, this can use the existing `apiService.getSemanticDefinition` or a new hover-API endpoint. Implement using `hoverTooltip()` from `@codemirror/tooltip` (already a transitive dependency via `@codemirror/autocomplete`). For non-LSP languages, fall back to showing basic token info or nothing. This is a core IDE feature and one of the most impactful improvements.

[x] - EDITOR: Add markdown live preview — when editing `.md` files, add a toggle button in the toolbar that opens a side-by-side preview pane rendering the markdown as HTML. Use `react-markdown` or the existing `marked` library. The preview should update live as the user types. Consider adding a split-view toggle (side-by-side vs. inline-rendered). This is essential for README editing and documentation work.

[x] - EDITOR: Add document formatting (format-on-save) — integrate a formatting backend (Prettier via the Go server, or LSP `textDocument/formatting`) and add a "Format on Save" toggle in settings. When enabled, format the document before saving. Add a "Format Document" command to the command palette and a keybinding (Opt+Shift+F / Alt+Shift+F). This is one of the most commonly expected IDE features.
[x] - EDITOR: Add document formatting (format-on-save) — integrate a formatting backend (Prettier via the Go server, or LSP `textDocument/formatting`) and add a "Format on Save" toggle in settings. When enabled, format the document before saving. Add a "Format Document" command to the command palette and a keybinding (Opt+Shift+F / Alt+Shift+F). This is one of the most commonly expected IDE features.

[x] - EDITOR: Add LSP-aware rename (F2) — the current rename workflow is manual (find and replace). Implement F2 rename that uses the backend's semantic capabilities (for TS/JS/Go) or falls back to a find-and-replace dialog with preview for other languages. Add an input dialog at cursor position, show a rename preview with highlighting, and apply the change atomically.

[x] - EDITOR: Add LSP-aware "Find All References" (Shift+F12) — for TypeScript/JavaScript/Go files, add a "Find All References" command that queries the backend semantic API and displays results in a panel or popover. Show file path and line number for each reference. Make it available via keybinding, command palette, and context menu.

[x] - EDITOR: Add quick actions / refactor menu (Ctrl/Cmd+.) — when the cursor is on a line that has available code actions (from LSP or static analysis), show a lightbulb icon in the gutter and a menu (triggered by Ctrl+.) with actions like "Add import", "Extract function", "Fix all", etc. This is a defining IDE feature. Start with static analysis actions (missing imports, unused variables) and expand to LSP code actions.

[x] - EDITOR: Add sticky scroll (pinned function/class headers) — when scrolling through a large file, the current function or class header should pin at the top of the viewport so the user always sees context. This is a feature in VS Code 2023+ and is very valuable for navigating large files. Implement as a ViewPlugin that: (1) uses the syntax tree to find the enclosing function/class at the viewport top, (2) renders a pinned/sticky decoration above the editor content, (3) updates on scroll and cursor movement.
[x] - EDITOR: Add sticky scroll (pinned function/class headers) — when scrolling through a large file, the current function or class header should pin at the top of the viewport so the user always sees context. This is a feature in VS Code 2023+ and is very valuable for navigating large files. Implement as a ViewPlugin that: (1) uses the syntax tree to find the enclosing function/class at the viewport top, (2) renders a pinned/sticky decoration above the editor content, (3) updates on scroll and cursor movement.

[x] - EDITOR: Add drag-and-drop text movement — implement proper drag-and-drop for text within the editor. Currently `dropCursor()` shows a visual indicator but there is no actual drag handler for text movement. Use `EditorView.domEventHandlers({ dragstart, dragover, drop })` to: (1) on dragstart, store the selected text and cursor position, (2) on drop, delete the dragged text from the source position and insert at the drop position. Hold Alt during drop to copy instead of move.
[x] - EDITOR: Add drag-and-drop text movement — implement proper drag-and-drop for text within the editor. Currently `dropCursor()` shows a visual indicator but there is no actual drag handler for text movement. Use `EditorView.domEventHandlers({ dragstart, dragover, drop })` to: (1) on dragstart, store the selected text and cursor position, (2) on drop, delete the dragged text from the source position and insert at the drop position. Hold Alt during drop to copy instead of move.

[x] - EDITOR: Add workspace-wide symbol search — the current "Go to Symbol" (`Cmd+Shift+O`) only searches within the current file. Add a "Go to Symbol in Workspace" command that queries the backend for symbols across all files in the project (leverages the existing semantic API). Show results grouped by file in the overlay.
[x] - EDITOR: Add workspace-wide symbol search — the current "Go to Symbol" (`Cmd+Shift+O`) only searches within the current file. Add a "Go to Symbol in Workspace" command that queries the backend for symbols across all files in the project (leverages the existing semantic API). Show results grouped by file in the overlay.

[x] - EDITOR: Improve the Go to Symbol overlay — add keyboard navigation (arrow keys to move between results), fuzzy matching for symbol names, display of symbol kind (function, class, variable, type) with icons, and show the enclosing scope path. Currently it does substring matching and basic rendering.

[x] - EDITOR: Add minimap click-to-scroll — the minimap (already implemented via `@replit/codemirror-minimap`) shows the viewport position but may not support clicking/dragging on the minimap to jump to that position in the document. If the package doesn't support it natively, add click and drag event handlers on the minimap container that map the click position to a document line and scroll the editor there.

#### Tier 5 — Large Features (new architecture/significant integration, ~16-40 hours each)

[x] - EDITOR: Full LSP client integration via `@codemirror/lsp-client` — currently the editor has basic semantic features for TS/JS/Go only (go-to-definition and diagnostics, both via custom API calls to the Go backend). `@codemirror/lsp-client` provides real WebSocket-based LSP integration that would unlock: (1) real-time completions with type info and documentation, (2) hover tooltips with rich content, (3) signature help (inline parameter hints), (4) full rename with preview, (5) find references and go-to-implementation, (6) code actions and code lens, (7) workspace symbols. This is the single highest-impact improvement but requires a WebSocket LSP proxy in the Go backend and significant extension wiring. Consider incremental rollout: start with completions and hover, then add rename/references/code actions.

[x] - EDITOR: Add inline parameter hints (signature help) — when typing a function call, show the function signature with the current argument highlighted (like VS Code's parameter hints). Implement using `signatureHelp` from `@codemirror/autocomplete` or through LSP. Show the hint after `(` and `,` and dismiss after `)`. This is very useful for API calls with many parameters. → Addressed by `@codemirror/lsp-client`'s `signatureHelp()` extension bundled in `languageServerExtensions()`. Provides signature help via `textDocument/signatureHelp` with keymap `Ctrl-Shift-Space` (show), `Ctrl-Shift-Up/Down` (navigate signatures). Connected via LSP WebSocket proxy.

[x] - EDITOR: Add document outline panel — a collapsible sidebar panel showing all symbols in the current file (functions, classes, interfaces, variables) as a tree. Allow click-to-navigate, search/filter within the outline, and sync with the current cursor position (highlight the enclosing symbol). This is a more persistent and detailed version of the existing breadcrumbs and Go to Symbol overlay. → Implemented: `DocumentOutlinePanel.tsx` component with hierarchical symbol tree, fuzzy search/filter, cursor sync highlighting, click-to-navigate, expand/collapse all, resizable/collapsible panel with localStorage persistence. Integrated via `EditorWithOutline.tsx` wrapper adjacent to the editor.

[x] - EDITOR: Unify the command palette — the current command palette handles commands but not files or symbols. Add modes so Cmd+P searches files, Cmd+Shift+O searches symbols, Cmd+Shift+P searches commands — all within the same palette UI (with mode tabs or auto-detection based on prefix like `>` for commands, `#` for symbols). This matches the VS Code paradigm where one palette is the entry point for all navigation.

[x] - EDITOR: Add inline diff/merge view using `@codemirror/merge` — the current diff gutter shows colored markers on individual lines but does not render a proper diff view with added/removed/changed content, hunk navigation (next/previous change), or accept/reject individual changes. Install `@codemirror/merge` and use it for: (1) viewing git diffs in the sidebar (instead of the current text-only diff), (2) the file-change dialog when external modifications are detected (show an inline merge view instead of just "Reload / Keep mine"), (3) a dedicated "Compare" tab for side-by-side file comparison.

[x] - EDITOR: Add code lens decorations — render inline text above lines (e.g., "12 references", "Run test" button) using LSP code lens data or custom rules. This requires a ViewPlugin that creates inline widgets at specific line positions. Start with basic static code lens (function reference counts) before adding interactive elements.

[x] - EDITOR: Add auto-close HTML/XML/JSX tags — the current `closeBrackets()` from `@codemirror/autocomplete` auto-closes `()`, `[]`, `{}`, `"`, `'` but does NOT auto-close HTML/JSX tags (`<div>` → `</div>`, `<span>` → `</span>`, etc.). Implement a custom extension that: (1) on typing `>`, checks if the current context is inside a tag (needs syntax tree consultation), (2) inserts the matching closing tag after auto-indent, (3) places the cursor between the opening and closing tags. Consider the Emmet plugin as an alternative if it covers this use case.

### Terminal & File Pane Gaps

[x] - TERMINAL: Add terminal tabs to support 3+ terminal sessions — currently the model is binary (0 or 2 side-by-side panes). Need a tab bar with named sessions and add/remove cycle.
[x] - TERMINAL: Add vertical terminal split option — implemented with Columns2/Rows2 buttons, hotkeys (Ctrl+Shift+5 / Ctrl+Alt+5), command palette entries, and full CSS layout support.
[x] - TERMINAL: Persist terminal height to `localStorage` — always resets to 400px on mount. Sidebar and context panel widths are already persisted; terminal height should be too.
[x] - TERMINAL: Allow user to choose shell profile for new terminal instances (e.g., bash, zsh, fish).
[x] - FILE TREE: Add search/filter input to the file tree — currently there is no way to filter or fuzzy-find within the file tree (the command palette does project-wide file search, but not the tree itself).
[x] - FILE TREE: Add `.gitignore`-aware toggle — currently ignored files are sorted to the bottom but always visible. Add a toggle to hide them.
[x] - FILE TREE: Add drag-and-drop support — no ability to move files between folders via drag-and-drop. Currently files can only be moved via the rename operation.

### Layout & Persistence Gaps

[x] - LAYOUT: Persist editor split pane sizes and layout type to `localStorage` — sidebar and context panel widths are persisted, but editor `paneSizes` and `PaneLayout` are ephemeral React state that resets on page reload.
[x] - LAYOUT: Implement layout save/restore — all layout state (pane arrangement, sizes, open files/tabs with their positions, cursor/scroll positions, terminal height) is ephemeral and lost on reload. This is the single biggest UX gap for returning users.
[x] - LAYOUT: Optionally restore the set of open files and their tab/pane arrangement on page load.

### Code Quality & Structural Improvements

[x] - REFACTOR: Break up `App.tsx` (1,987 lines) — this monolithic file likely contains types, state, callbacks, and rendering that should be extracted into separate modules, custom hooks, and smaller components. It is the largest file in the project. (Duplicate removed)
[x] - REFACTOR: Break up `AppContent.tsx` (1,140 lines → 486 lines) — extracted useCurrentTodos, useSplitManager, useHotkeysConfig, useFileHandlers, usePanelWidth, useHotkeyIntegration hooks and parseFilePath utility. Now under the 500-line target.
[x] - REFACTOR: Break up `git_api.go` (1,861 lines) — this is the largest Go file in `pkg/webui/` and likely combines multiple API endpoints that could be split by domain (status, staging, commit, history).
[x] - REFACTOR: Break up `tool_executor.go` (1,353 lines) — the agent tool executor has grown large and could benefit from splitting by tool category or lifecycle stage.
[x] - REFACTOR: Break up `EditorManagerContext.tsx` (817 lines) — consider extracting buffer persistence (save/load) and buffer mutation operations into separate hooks or modules.
[x] - CODE QUALITY: Adopt a frontend linting setup — currently there is no ESLint config file, no Prettier config, and only a minimal `eslintConfig` in package.json. For a React/TypeScript project of this size, a proper linting and formatting setup is essential for consistency.
[x] - CODE QUALITY: Reduce excessive `console.error/warn` logging — there are 80+ `console.error` and `console.warn` calls scattered across frontend components. Many of these should be replaced with a proper logging service (the `utils/log.ts` file exists but is not widely used) to allow configurable log levels, filtering, and error reporting.
[x] - CODE QUALITY: Reduce silent error swallowing — many catch blocks use `catch {}`, `catch { /* ignore */ }`, or `.catch(() => {})` which silently discard errors. At minimum, these should log at debug/warn level so issues are not invisible during development.
[x] - CODE QUALITY: Reduce silent error swallowing — many catch blocks use `catch {}`, `catch { /* ignore */ }`, or `.catch(() => {})` which silently discard errors. At minimum, these should log at debug/warn level so issues are not invisible during development. (Go: 12 `_ =` sites across 8 files now log via log.Printf; TS already clean)
[x] - CODE QUALITY: Improve test coverage across low-coverage packages — `pkg/credentials` (20.0%), `pkg/interfaces/types` (34.8%), `pkg/trace` (48.2%), `pkg/validation` (0%), `pkg/git` (65.9%) have notably low coverage. Several files in `cmd/` have 0% function coverage (copilot.go, plan.go, log.go, diag.go, review_staged.go, github_setup_prompt.go).
[x] - CODE QUALITY: Use standardized error handling in Go — inconsistent patterns of `fmt.Errorf` vs `errors.New` vs returning bare errors across packages. Adopt a project-wide convention (e.g., always use `fmt.Errorf("context: %w", err)` for wrapped errors).
[x] - CODE QUALITY: Add proper TypeScript strict mode auditing — `tsconfig.json` has `strict: true` but there is no CI step that fails on type errors. Ensure `tsc --noEmit` runs as part of CI/build checks.
[x] - CODE QUALITY: Consider migrating from `React.FC` typed components to regular function components with explicit return types — `React.FC` is considered an anti-pattern in modern React (doesn't support generics well, inconsistent with plain functions).

### General UX Gaps

[x] - UX: Add keyboard-accessible menu bar (File, Edit, View, Terminal, Help) — VS Code users expect a menu bar for discoverability of features that don't have hotkey assignments.
[x] - UX: Add a welcome/Getting Started tab for new users — when the editor opens with no files, show helpful content instead of a blank pane.
[x] - UX: Add file drag-and-drop from OS into the editor (open dropped files).
[x] - UX: Add "Unsaved changes" indicator on close — when closing a tab or the browser window, warn if there are unsaved editor buffers.
[x] - UX: Add "Unsaved changes" indicator on close — when closing a tab or the browser window, warn if there are unsaved editor buffers. (useUnsavedChangesWarning hook with beforeunload + tab close confirm dialog + document title indicator + tab dot indicator)
[x] - UX: Add notifications for file changes detected on disk (when a file is modified externally, prompt the user to reload).
[x] - UX: Add the ability to pin tabs to prevent accidental closure (type partially supported in `EditorBuffer` but no UI toggle for it).
[x] - UX: Add a status bar at the bottom showing current branch, file type, encoding, line endings, indentation settings — currently cursor position is in the editor footer but there is no global status bar.
[x] - UX: Add "zoom into/zoom out of terminal" controls or a font size setting for the integrated terminal.

### General

[x] - WEBUI: Add support for leveraging worktrees for running secondary chats for scoped feature work.

---

## API Key / Credential Handling Improvements

### Security

[x] - CREDENTIALS: Encrypt API keys at rest — `api_keys.json` stores keys in plaintext. Keys should be encrypted with a key derived from a user passphrase or machine-specific key (e.g., via `age`, `nacl/secretbox`, or OS keyring) so that a compromised `~/.ledit/` directory does not expose all provider secrets.
[x] - CREDENTIALS: Support OS-native secret storage (keyring) — Integrate with `keychain` (macOS), `secret-service` (Linux/DBus), or `wincred` (Windows) via a library like `zalando/go-keyring` so keys are never written to disk in any file under `~/.ledit/`. Fall back to encrypted file if keyring is unavailable.
[x] - CREDENTIALS: Mask API keys in logs — Ensure resolved credential values are never printed or logged (not even in debug/trace logs). Audit all `log.Printf`/`fmt.Printf` calls that handle `Resolved.Value` or `configCopy.Auth.Key` to confirm no leakage.

### Architecture & Consolidation

[x] - CREDENTIALS: Consolidate the three parallel credential paths into one — Currently there are three independent ways credentials are resolved: (1) `credentials.Resolve()` in `pkg/credentials/store.go` (env → stored file), (2) `configuration.ResolveProviderCredential()` in `pkg/configuration/provider_auth.go` (env → stored keys → env metadata), and (3) hardcoded `credentials.Resolve(provider, "PROVIDER_API_KEY")` calls scattered in `pkg/agent_api/interface.go` and `pkg/agent_api/models.go`. These should be unified into a single resolution function with a clear precedence chain, eliminating duplication and reducing the risk of inconsistent behavior.
[x] - CREDENTIALS: Consolidate the three parallel credential paths into one — (duplicate; see above)
[x] - CREDENTIALS: Remove hardcoded env var names from `pkg/agent_api/interface.go` — `IsProviderAvailable()` now delegates to `credentials.HasProviderCredential()`, which uses the unified resolution path.
[x] - CREDENTIALS: Remove hardcoded env var names from `pkg/agent_api/models.go` — All model listing wrappers now use `credentials.ResolveProviderAPIKey()`. No `resolveCredentialValue()` exists.
[x] - CREDENTIALS: `api_keys.go` `ReachableAPIKey` struct duplicates `ProviderAuthMetadata` — File and types removed; provider info driven by `ProviderAuthMetadata` + embedded provider configs.

### Custom Providers

[x] - CREDENTIALS: Custom providers should resolve keys through the same unified path — Currently `GetAuthToken()` in `pkg/agent_providers/provider_config.go` only checks the env var and the hardcoded `Auth.Key` field. These two paths should be consistent. The factory should inject the resolved key into `configCopy.Auth.Key` for custom providers (it already does this for generic providers) and document that `Auth.Key` is runtime-only, never persisted.

### MCP Service Credentials

[x] - CREDENTIALS: MCP server env vars store secrets in plaintext in `config.json` — MCP server configs allow setting `env` vars (e.g., `GITHUB_PERSONAL_ACCESS_TOKEN`) which are stored alongside the config in `config.json`. These secrets should be migrated to the credential store (or OS keyring) and referenced by a placeholder/pointer in the MCP config, so the main config file never contains raw token values.
[x] - CREDENTIALS: Add a dedicated credential management API for MCP servers — Currently the only way to set MCP service credentials is via the env block in the MCP server config or by setting shell environment variables. Add explicit `credentials` fields to `MCPServerConfig` (or a separate `mcp_credentials.json` store) so the webui can present per-service credential input fields and securely store/reference them without users having to know the correct environment variable name. (Implemented: MCPServerConfig.Credentials map, GET/PUT/DELETE /api/settings/mcp/servers/{name}/credentials, WebUI credential panel, HTTP client generic auth headers via buildAuthHeaders)

### WebUI Credential UX

[x] - CREDENTIALS: Add a credential management page in the webui settings — Currently the webui only exposes credential input during onboarding. There is no settings page to view, add, update, or delete stored API keys for providers (built-in or custom) or MCP services. Users must edit files manually or re-run the CLI onboarding flow.
[x] - CREDENTIALS: Show credential status (has key / missing key / env-only) for all providers in the settings UI — The onboarding status endpoint returns `has_credential` per provider, but the general settings pages do not. Users cannot see at a glance which providers are properly configured without starting onboarding.
[x] - CREDENTIALS: Allow testing/validating stored credentials from the webui — Add a "Test Connection" button per provider that makes a lightweight API call (e.g., `GET /models`) to verify the stored credential is valid and not expired. Show clear success/failure feedback.

### Per-Provider Key Rotation & Multi-Key Support

[x] - CREDENTIALS: Support key rotation without service interruption — When a user updates an API key, the new key should be validated before replacing the old one. If validation fails, keep the old key and show an error. Currently `SetAPIKey` → `SaveAPIKeys` is a blind write with no validation that the new key works.
[x] - CREDENTIALS: Support key rotation without service interruption — When a user updates an API key, the new key should be validated before replacing the old one. If validation fails, keep the old key and show an error. Currently `SetAPIKey` → `SaveAPIKeys` is a blind write with no validation that the new key works. (ValidateAndSaveAPIKey validates before persisting and restores old key on failure; Manager.RefreshAPIKeys keeps in-memory cache in sync; deprecated blind-write SaveAPIKeys)
[x] - CREDENTIALS: Support multiple keys per provider — Some users may want to use different keys for different projects or to distribute load. Supporting a list of keys per provider with automatic rotation/fallback would help (low priority — env var per-project covers some of this).
[x] - CREDENTIALS: Support multiple keys per provider — Some users may want to use different keys for different projects or to distribute load. Supporting a list of keys per provider with automatic rotation/fallback would help (low priority — env var per-project covers some of this). (KeyPool, KeyRotator, round-robin resolution, auto-rotation on 429 rate limits, pool CRUD REST API, backward-compat single-key format, TOCTOU mutex protection)

### Cleanup & Hardening

[x] - CREDENTIALS: File permissions audit — `api_keys.json` is written with `0600` which is correct, but `config.json` (which may contain MCP env vars with secrets and the `CustomProviderConfig.APIKey` field) uses whatever `os.WriteFile` default is in `config.go`. Ensure all files containing secrets are created with `0600` and the config directory has `0700`. Add a startup permission check that warns if permissions are too open.
[x] - CREDENTIALS: Add credential redaction to config export/debug commands — Any command that dumps config (e.g., `ledit diag`, `ledit log`, config export) should redact all credential values before output. Audit `cmd/diag.go` and any other config-dumping paths.
[x] - CREDENTIALS: Add credential redaction to config export/debug commands — Any command that dumps config (e.g., `ledit diag`, `ledit log`, config export) should redact all credential values before output. Audit `cmd/diag.go` and any other config-dumping paths. → Addressed: Created `RedactConfig()` in `pkg/configuration/redact.go`, added `ledit config show` command, fixed unredacted Args in `cmd/mcp.go` and `pkg/agent_commands/mcp.go`, deep-copied Args in `RedactServerConfig`. Fixed: WebUI `sanitizedConfig()` leaking raw MCP config to browser, `runMCPList`/`listServers` now use `RedactMCPConfig` upfront (`cmd/mcp.go`, `pkg/agent_commands/mcp.go`), completed `RedactConfig()` deep copies for `APITimeouts`/`SubagentTypes`/`Skills`, expanded `knownSecretVars` with 13 additional providers and narrowed `LEDIT_` prefix exclusion (`pkg/credentials/redact.go`, `pkg/mcp/secrets.go`).

---

## Desktop Productization

### Crash & Diagnostics

[x] - DESKTOP: Add a frontend error boundary in the React renderer — currently there is no `ErrorBoundary` component wrapping the app tree, so an unhandled render error produces a blank white screen with no user guidance. Show a fallback UI with a "Reload" button and a link to the diagnostics log location.

[x] - DESKTOP: Add a diagnostic bundle export — users hitting persistent failures need a single action to gather logs. Implement a "Export Diagnostics" option (in the Help menu or failure screen) that zips `userData/logs/`, a redacted config snapshot, and the last N lines of backend stdout/stderr into a timestamped archive the user can share.

[x] - DESKTOP: Improve the backend-launch failure screen — `renderErrorPage()` currently shows a raw exit code. Replace it with a structured page that shows: the likely cause, the relevant log lines from `userData/logs/`, and a "Copy diagnostics" button.

### First-Run Onboarding

[x] - DESKTOP: Verify and wire first-run onboarding for the desktop app — `pkg/webui/onboarding_api.go` and `DesktopOnboardingHandler` exist on the backend but it is unclear whether the desktop launcher triggers the onboarding flow when no config is present. Audit the startup path in `desktop/main.js` and ensure a new install navigates to the onboarding UI before opening a workspace.

[x] - DESKTOP: Add WSL distro selection to the onboarding flow on Windows — when `LEDIT_DESKTOP_BACKEND_MODE=wsl` is detected, the onboarding UI should enumerate available distros (via `listWslDistros`) and let the user pick one before proceeding.

### Architecture

[x] - DESKTOP: Split `desktop/main.js` into focused modules — at ~1780 lines the file mixes protocol handling, state management, WSL logic, SSH logic, window management, backend spawning, and error rendering. Extract into at least: `windows.js`, `backend.js`, `wsl.js`, `protocol.js`, `errorPages.js`. Build must pass after each extraction step.

### Auto-Update

[x] - DESKTOP: Implement auto-update — there is currently no mechanism to notify users of or apply new releases. Integrate `electron-updater` (already a peer dep in the electron ecosystem) pointing at the GitHub Releases feed. Show a non-intrusive "Update available" notification and allow deferred install-on-quit.

### Testing

[x] - DESKTOP: Add desktop E2E smoke tests to CI — write a minimal Playwright or Spectron test suite that launches the packaged app in headless mode, opens a temp workspace, and asserts the UI loads and the backend health endpoint responds. Run on Linux in CI at minimum.
[x] - DESKTOP: Add desktop E2E smoke tests to CI — write a minimal Playwright or Spectron test suite that launches the packaged app in headless mode, opens a temp workspace, and asserts the UI loads and the backend health endpoint responds. Run on Linux in CI at minimum. (duplicate of line above)

## Onboarding Flow Improvements

### Editor-Only Mode (No Provider Required)

[x] - ONBOARDING: Allow the webui to be used as a pure editor/terminal without configuring any AI provider — When a fresh user opens the webui, they are blocked by a mandatory onboarding dialog that requires selecting a provider and (for most providers) entering an API key. The webui is a full code editor with file browsing, terminals, and git integration — features that work entirely without an AI provider. The onboarding dialog should have a clear "Skip setup — use as editor" option (or equivalently, not block at all) so users can explore the editor first and set up AI later via Settings. Currently `handleAPIOnboardingStatus` in `pkg/webui/onboarding_api.go` returns `setup_required: true` when `currentProvider == "" || currentProvider == "test"`, which guarantees the modal blocks entry. The `test` provider is already excluded from the "configured" check even though it works fine for non-AI workflows.
[x] - ONBOARDING: Allow the webui chat/agent to gracefully degrade when no provider is configured — Once a user dismisses onboarding without a provider, the chat panel should show a friendly prompt explaining that AI features require a provider, with a button to open provider setup (rather than showing an error or a broken chat). The editor, terminal, file tree, and git panels should all remain fully functional. Currently `getClientAgent()` in `pkg/webui/client_context.go` calls `agent.NewAgentWithModel("")` which goes through `EnsureAPIKey` → `SelectNewProvider` and would fail without an interactive terminal. The webui agent creation path should tolerate a missing provider and produce a "no-agent" state that the chat UI can present gracefully.
[x] - ONBOARDING: Add "Set up later" / "Use as editor only" to the onboarding dialog — The webui `OnboardingDialog` component (`webui/src/components/OnboardingDialog.tsx`) has only "Refresh" and "Complete Setup" buttons. There is no way to dismiss the dialog without completing setup. Add a prominent "Skip — use as editor" button that dismisses the dialog and stores a `provider: "none"` or `provider: ""` preference so subsequent page loads do not re-trigger onboarding. The skip should be easily reversible (e.g., a banner or settings link saying "Configure AI provider to enable chat features").
[x] - ONBOARDING: The CLI `agent` command should not block on provider setup — `NewAgentWithModel` (`pkg/agent/agent.go`) calls `configManager.EnsureAPIKey()` then `client.CheckConnection()` in a retry loop, and falls through to `recoverProviderStartup` which calls `SelectNewProvider()` — a terminal prompt that blocks. In the webui daemon path (non-interactive), this will hang or return an opaque error. The CLI should detect non-interactive environments and either default to `test` or fail fast with a clear message ("No provider configured. Run `ledit agent` interactively or set LEDIT_PROVIDER / configure ~/.ledit/config.json") instead of blocking.

### CLI Onboarding UX

[x] - ONBOARDING: CLI first-run should offer a clear "skip / use local only" option — `selectInitialProvider()` in `pkg/configuration/init.go` presents all providers and forces the user to pick one, even for users who just want to try the editor or use a local model like Ollama. Add a "Skip provider setup" or "Local only (no cloud API)" option at the top of the list that sets `LastUsedProvider` to a sentinel (e.g., `"none"`) so the CLI can detect it and either skip AI features or prompt again only when AI is actually needed (e.g., the user sends a chat message).
[x] - ONBOARDING: CLI onboarding does not mention the webui editor — `ShowWelcomeMessage()` and `ShowNextSteps()` in `pkg/configuration/init.go` only describe CLI agent usage. New users are not told that `ledit agent -d` (daemon mode) opens a full web-based code editor. The welcome message should mention the webui as a first-class interface, especially since it provides a much friendlier setup experience for provider/model configuration.

### Webui Onboarding UX

[x] - ONBOARDING: Webui onboarding should show which providers are already configured from env vars or existing key files — `handleAPIOnboardingStatus` fetches providers and checks credentials, but the onboarding dialog (`OnboardingDialog.tsx`) does not visually distinguish "already configured" providers from ones needing setup. Badge or mark providers that already have credentials (e.g., from `OPENROUTER_API_KEY` in the environment) so users know they can just click through without entering anything.
[x] - ONBOARDING: Webui onboarding model list defaults to first model, not recommended — When a user selects a provider, the model input defaults to `provider.models[0]` (potentially an obscure or expensive model) rather than the `recommended_model`. The `onboarding-models` datalist includes all models but doesn't pre-select the recommended one. Auto-fill the model input with `selectedProvider.recommended_model` and make the recommended model visually distinct in the datalist.
[x] - ONBOARDING: Webui onboarding should validate the API key before marking setup complete — `handleAPIOnboardingComplete` saves the API key via `cm.SaveAPIKeys()` but never verifies it works. If the user pastes a bad key, setup succeeds but the first chat message will fail with an unhelpful auth error. After saving, make a lightweight `GET /models` call to the provider endpoint and return validation feedback in the response so the dialog can show success or ask the user to re-enter the key.
[x] - ONBOARDING: Webui onboarding should persist the chosen model — The onboarding complete endpoint calls both `clientAgent.SetModel(model)` (in-memory) and `persistProviderModelToConfig`. However, if the agent creation failed (no provider) or the config persist failed (logged but swallowed), the model choice is lost on next launch. Ensure the model is always persisted to `config.ProviderModels` before returning success.
[x] - ONBOARDING: Re-onboarding should be accessible from the webui (not just on first run) — Once onboarding is completed, there is no way to re-trigger it from the webui without manually deleting config. Add a "Provider setup" entry in the Settings panel (or a gear icon in the chat panel) that opens the same onboarding flow for changing provider/model/key. This is especially important since users may want to switch providers over time.

---

## Testing & CI Gaps

[x] - TESTING: Add tests for `pkg/mcp` (zero test files) — The MCP package has no tests at all. It handles server lifecycle, HTTP/stdio transport, tool registration, and config parsing — all critical paths for any user with MCP enabled. At minimum add tests for config loading, server name validation, env-var passthrough, and the registry/template system. → Addressed: 17 test files with ~13,500 lines of tests covering config loading, server validation, env-var passthrough, registry/template system, secrets, redaction, manager lifecycle, client lifecycle/messaging/resources/prompts, HTTP client, and tool wrapper. 78.9% statement coverage.
[x] - TESTING: Add tests for `pkg/filediscovery` (zero test files) — The file discovery module has no tests. It powers the file tree, search, and `.ledit/.ignore` rule loading. Broken discovery would silently corrupt the UI. Add tests for ignore-rule parsing, file-to-directory mapping, and the glob-based walker. → Addressed: Comprehensive test suite with 2,956 lines, 80+ test cases, 95.8% statement coverage. Tests cover ignore-rule parsing (.gitignore/.ledit/.ignore), file-to-directory mapping, glob-based walker, shell command discovery, workspace structure, filtering, and all error paths.
[x] - TESTING: CI has no coverage threshold or race detection — `.github/workflows/build.yml` runs `make test-unit` and `make test-integration` but never checks coverage percentages and does not pass `-race` to `go test`. Add a minimum coverage gate (e.g., `go test -race -coverprofile=...` with a `go tool cover -func` check) so regressions are caught.
[x] - TESTING: CI has no frontend type-checking step — The TypeScript build runs via `make deploy-ui` (which bundles), but `tsc --noEmit` is never called in isolation. A type error that happens to be bundled away would go unnoticed. Add `npx tsc --noEmit` to the CI pipeline.
[x] - TESTING: E2E tests have no onboarding coverage — There are zero tests for the onboarding API (`handleAPIOnboardingStatus`, `handleAPIOnboardingComplete`) and no frontend tests for `OnboardingDialog.tsx`. The onboarding flow is the very first thing every new user sees — a regression here is high-impact. → Addressed: Comprehensive test coverage with 12 Go E2E tests in `pkg/webui/onboarding_e2e_test.go`, 27 API unit tests in `pkg/webui/onboarding_api_test.go`, and 43 frontend tests in `webui/src/components/OnboardingDialog.test.tsx`. Tests cover status API, skip API, complete API, full E2E flows (fresh user skip, provider selection, credential handling, model persistence, re-onboarding, error handling), and component-level tests for visibility, provider selection, model selection, API key input, validation, platform guidance, and edge cases.

---

## Provider Catalog & Registration Gaps

[x] - PROVIDERS: Adding a new provider requires touching 6+ files — To add a fully-wired provider you must edit: (1) `configs/<name>.json`, (2) `getSupportedProviders()` in `api_keys.go`, (3) `IsProviderAvailable()` in `interface.go`, (4) `mapClientTypeToString()` in `manager.go`, (5) `GetProviderAuthMetadata()` in `provider_auth.go`, and (6) potentially the provider catalog. A new provider auto-discovery system should read the configs directory and generate the registration dynamically so that dropping a JSON file is sufficient.
[x] - PROVIDERS: Cerebras has a provider config but is not wired into the product — `pkg/agent_providers/configs/cerebras.json` exists but there is no `CerebrasClientType` in `pkg/agent_api/interface.go`, no entry in `getSupportedProviders()` in `api_keys.go`, no `IsProviderAvailable` case, no factory mapping, and no onboarding entry. The config is dead code. Either finish wiring it in or remove it to avoid confusion. -- All of this should be resolved if the above item gets completed

[x] - PROVIDERS: Build a lightweight static model registry server to keep provider model catalogs fresh — The existing `cmd/refresh_provider_catalog/` is a one-shot CLI tool that fetches models from each provider's API and writes a JSON catalog to `pkg/providercatalog/providers.json`. This is a manual step that quickly goes stale. Replace it with a small, low-latency static file server (e.g., Cloudflare Workers, a simple Go HTTP server, or an S3-hosted JSON file updated via CI cron) that serves per-provider model lists over HTTP. The client (`pkg/agent_api/models.go`) should check this endpoint first (with a short cache TTL) and fall back to the current per-provider API calls if the registry is unreachable. Key design points: (1) Each provider gets its own JSON blob (e.g., `/models/openrouter.json`, `/models/openai.json`) generated by calling the provider's `/v1/models` endpoint on a schedule, so the catalog stays current without requiring a new binary release. (2) The server should be static JSON files behind a CDN for sub-10ms latency — no dynamic processing. (3) Custom/user-defined providers are explicitly out of scope; they remain resolved locally as they do today. (4) The existing `refresh_provider_catalog` tool becomes the CI job that publishes the updated JSON to the server. (5) The embedded `pkg/providercatalog/providers.json` remains the offline fallback bundled into the binary. -- Maybe we can use github docs to serve the file?

---

## Remaining Large File Refactors (Go)

All files above the 500-line target that still need splitting: `tool_handlers_subagent.go` (1230), `webcontent/browser_rod.go` (1196), `agent_tools/vision_types.go` (1140), `scripted_client.go` (1089), `agent_tools/security.go` (994), `agent_providers/generic_provider.go` (982), `websocket.go` (968), `configuration/config.go` (968), `api_files.go` (917), `agent_api/tools.go` (856), `conversation_optimizer.go` (855), `fallback_parser.go` (823), `git_api_status.go` (813), `console/input_core.go` (807), `conversation_pruner.go` (789), `conversation_handler.go` (741), and 20 more in the 500–730 range. Previous TODO entries already addressed `App.tsx`, `AppContent.tsx`, `git_api.go`, `tool_executor.go`, and `EditorManagerContext.tsx`.

---

## Layered Config — Complete

All layered config work (backend + frontend) is complete:

[x] - WEBUI SETTINGS: Add config scope tabs (Global / Workspace / Session) to the Settings panel — Currently the Settings panel shows a flat list with no indication of which layer a setting comes from. Add tabs at the top: "Global" (reads/writes `~/.ledit/config.json`), "Workspace" (reads/writes `{workspace}/.ledit/config.json`, disabled when no workspace), "Session" (reads/writes `chatSession.ConfigOverrides`, ephemeral per-chat). The "General" settings tab provider/model selector should default to the Session scope. Add API endpoints: `GET /api/settings?layer=global|workspace|session` and `PUT /api/settings?layer=global|workspace|session`. → Implemented: Backend GET `/api/settings?layer=...` already existed. Added backend PUT `/api/settings?layer=global|workspace|session` with three scoped handlers. Frontend: SettingsPanel already had Session/Workspace/Global tab buttons. Wired `updateSetting()` to pass `configViewLayer` to `api.updateSettings()`, and `ApiService.updateSettings()` now accepts optional `layer` param. Display settings use `displaySettingsRef` to render the correct layer's values.
[x] - WEBUI SETTINGS: Show config layer provenance in the UI — When viewing settings, indicate which layer each value comes from (e.g., a badge showing "workspace" next to a reasoning_effort value that was set at the workspace level). This requires the GET endpoint to return provenance metadata alongside values. → Implemented: Backend `GET /api/settings?layer=provenance` returns `{ config, sources }`. Frontend `renderProvenanceBadge()` shows color-coded labels (session=blue, workspace=amber, global=gray) on all setting labels when viewing effective config.
[x] - WEBUI SETTINGS: Add workspace config creation UX — Provide a "Create workspace config" button in the Workspace tab that copies current global config values to `{workspace}/.ledit/config.json` so users can then customize per-project settings without starting from scratch. → Implemented: "Create Workspace Config" button appears when workspace layer is selected and no workspace config exists. Copies global settings via `api.getSettingsLayer('global')` → `api.updateSettings(data, 'workspace')`.

---

## Remaining Large File Refactors (TypeScript)

18 components still over 500 lines: `LocationSwitcher.tsx` (1850), `ContextPanel.tsx` (1662), `EditorPane.tsx` (1262), `FileTree.tsx` (1243), `SettingsPanel.tsx` (1936), `Sidebar.tsx` (1025), `CommandInput.tsx` (945), `SearchView.tsx` (735), `CommandPalette.tsx` (653), `Chat.tsx` (622), `Terminal.tsx` (619), `ReviewWorkspaceTab.tsx` (598), `GoToSymbolOverlay.tsx` (571), `PaneLayoutManager.tsx` (533), `EditorTabs.tsx` (530), `GitSidebarPanel.tsx` (524), `AppContent.tsx` (504), `FileEditsPanel.tsx` (503).

---

## Reliability & Resilience

[x] - RELIABILITY: Main chat WebSocket has no reconnection logic — `clientSession.ts` uses `clientFetch` for API calls but the real-time event stream depends on a WebSocket managed elsewhere. The terminal WebSocket (`terminalWebSocket.ts`) has proper reconnect with exponential backoff, but if the main agent-event WebSocket drops mid-conversation, there is no documented reconnect path and the user may silently stop receiving updates. Audit and add reconnect with in-flight message replay.
[x] - RELIABILITY: Config version has no migration pipeline — `ConfigVersion` is `"2.0"` and `Load()` applies ad-hoc field-default heuristics (e.g., checking if a key exists in raw JSON to decide whether to apply defaults). There is no `migrate("1.0" → "2.0")` function. As more fields are added, these inline checks will become fragile and hard to test. Add a proper migration registry keyed by version pairs so each upgrade step is isolated and testable.
[x] - RELIABILITY: Config version has no migration pipeline — `ConfigVersion` is `"2.0"` and `Load()` applies ad-hoc field-default heuristics (e.g., checking if a key exists in raw JSON to decide whether to apply defaults). There is no `migrate("1.0" → "2.0")` function. As more fields are added, these inline checks will become fragile and hard to test. Add a proper migration registry keyed by version pairs so each upgrade step is isolated and testable. (Migration registry extended with map初始化, default subagent types, default skills, legacy tool allowlist migration; merge functions removed from Load())
[x] - RELIABILITY: WebSocket panic recovery logs but may leave state inconsistent — `websocket.go` has `recover()` in multiple goroutines that logs the panic and returns, but does not notify the client or clean up agent state. A panicked goroutine could leave the agent in a half-initialized state. After recovery, send an error event to the client and potentially re-initialize or terminate the session cleanly.
---

## Developer Experience

[x] - DX: Add a `CONTRIBUTING.md` or similar developer setup guide — There is no documentation on how to set up a development environment. The `Makefile` has `help` but there is no guide covering: required Go version (1.25 per CI), Node.js version (22), how to run the webui in dev mode (`npm run dev`), how the embed system works, or the test strategy.
[x] - DX: `make lint` and `make lint-fix` exist but are not wired into CI — Replaced the two separate frontend lint steps (`npm run lint:ci` and `npm run type-check`) in CI with a single consolidated `make lint` step (ESLint + Prettier format check + TypeScript type-check). Added as non-blocking (`continue-on-error: true`) with a GitHub Actions warning annotation. Once the codebase is clean (run `make lint-fix`), the `continue-on-error` can be removed to make lint blocking.

---

## Housekeeping — Files Safe to Delete

The following files are candidates for removal. They are either build artifacts that belong in .gitignore, duplicate/overlapping documentation, scratch/debug files, or compiled binaries that should not be in the working tree.

### Build Artifacts & Compiled Binaries (~290 MB)

These are already covered by .gitignore patterns (`*.test`, `ledit`, `*.exe`, `*.out`, `coverage.out`, `coverage.html`) but exist in the working tree as leftover build/test output:

| File | Reason |
|------|--------|
| `cmd.test` (30 MB) | Go test binary, regenerated by `go test ./cmd/...` |
| `webui.test` (29 MB) | Go test binary, regenerated by `go test ./webui/...` |
| `configuration.test` (8 MB) | Go test binary, regenerated by `go test` |
| `ledit` (30 MB) | Compiled Go binary, regenerated by `go build` |
| `ledit.exe` (27 MB) | Windows binary artifact |
| `coverage.out` | Go coverage output |
| `coverage2.out` | Go coverage output (empty, 10 bytes) |
| `coverage3.out` | Go coverage output (empty, 10 bytes) |
| `coverage4.out` | Go coverage output |
| `final_coverage.out` | Go coverage output (2 MB) |
| `coverage.html` | HTML coverage report |

**Cleanup command:** `rm -f cmd.test webui.test configuration.test ledit ledit.exe coverage.out coverage2.out coverage3.out coverage4.out final_coverage.out coverage.html`

### Scratch / Debug / Temporary Files

| File | Reason |
|------|--------|
| `hello.py` | Scratch file — literally just `print("Hello, World!")`. Already listed in .gitignore. |
| `openai_models_response.json` (11 KB) | Snapshot of OpenAI API models response — one-time reference/debug artifact, not referenced by any code. Already in .gitignore indirectly (not referenced). |
| `e2e_results.csv` (41 bytes) | Empty test results file (header row only). Already in .gitignore. |
| `.ledit_pasted_images/paste_20260324_140648_6317ed.png` | Debug screenshot pasted from a UI session (187 KB). Not currently in .gitignore. |

**Cleanup command:** `rm -f hello.py openai_models_response.json e2e_results.csv .ledit_pasted_images/paste_20260324_140648_6317ed.png`
**Also add to .gitignore:** `.ledit_pasted_images/`

### Compiled Python Cache

| Path | Reason |
|------|--------|
| `examples/__pycache__/` (27 KB) | Python bytecode cache from running examples. Already covered by `**/__pycache__//` in .gitignore. |

**Cleanup command:** `rm -rf examples/__pycache__/`

### Duplicate/Overlapping Error Handling Documentation (4 files → 1)

There are four error handling convention docs at the project root that all cover essentially the same content (wrap errors with `fmt.Errorf`, use `%w`, etc.):

| File | Lines |
|------|-------|
| `ERROR_HANDLING_CONVENTION.md` | 215 |
| `ERROR_HANDLING_GUIDE.md` | 198 |
| `ERROR_HANDLING_GUIDELINES.md` | 168 |
| `GO_ERROR_HANDLING_CONVENTION.md` | 429 |

Additionally, `docs/error-handling-convention.md` and `docs/error_handling_convention.md` (different filenames, same topic) overlap with each other and with the root files.

**Recommendation:** Consolidate into a single file (e.g., `docs/error-handling-convention.md` using the most comprehensive version — likely `GO_ERROR_HANDLING_CONVENTION.md` at 429 lines), then delete the other 5 files.

### Potentially Redundant Test Runners

Three Python test runner scripts at the project root with overlapping purposes:

| File | Lines |
|------|-------|
| `test_runner.py` | 640 |
| `integration_test_runner.py` | 199 |
| `e2e_test_runner.py` | 172 |

**Recommendation:** Review whether `integration_test_runner.py` and `e2e_test_runner.py` are superseded by `test_runner.py`. If so, remove the older ones.

### Other Candidates to Review

| File | Reason |
|------|--------|
| `update_and_test.sh` | One-off development script for testing alternate screen support — interactive, project-specific debug workflow that is unlikely to be useful long-term. |
| `replay_last_request.sh` | Debug utility for replaying LLM API requests — potentially useful but not referenced by any docs or code. Consider keeping or moving to `scripts/`. |
| `test-results/.last-run.json` | Test artifact — directory is in .gitignore. |

**Cleanup command:** `rm -f test-results/.last-run.json`
**Update_and_test.sh:** Consider deleting or moving to a `scripts/dev/` directory.

---

## Cloud Integration — Sprout Foundry

### 1. Complete ledit → sprout Rename (partially done)

[x] - CLOUD: Rename Go module to `github.com/sprout-foundry/sprout` and update import paths across all `.go` files (already done)
[x] - CLOUD: Update WASM global to `SproutWasm` and IndexedDB to `sprout-wasm-fs` (already done)
[x] - CLOUD: Update logos to sprout design (already done)
[x] - CLOUD: Create `GetEnv(sproutKey, legacyKey)` helper in `pkg/configuration/env.go` that checks `SPROUT_*` first with `LEDIT_*` fallback + deprecation warning → Implemented as `pkg/envutil` (zero-dependency) + `pkg/configuration/env.go` wrapper
[x] - CLOUD: Replace all ~221 `os.Getenv("LEDIT_...")` call sites with `envutil.GetEnvSimple()` / `configuration.GetEnvSimple()` across non-test Go files
[x] - CLOUD: Update `cmd/service_darwin.go` and `cmd/service_linux.go` to use `SPROUT_SERVICE=1` in launchd/systemd config (keep `LEDIT_SERVICE` as backward-compat alias)
[x] - CLOUD: Rename WebUI types: `LeditInstance` → `SproutInstance`, `LeditSettings` → `SproutSettings`, `LeditConfigDir` → `SproutConfigDir`, `LeditLogo` → `SproutLogo`, `LeditLogoProps` → `SproutLogoProps` (in `webui/src/services/api.ts` and refs in Sidebar.tsx, AppContent.tsx, LocationSwitcher.tsx)
[x] - CLOUD: Update `webui/package.json` name from `ledit-webui` to `sprout-webui`
[x] - CLOUD: Update `webui/package.json` name from `ledit-webui` to `sprout-webui` (duplicate — already done on line above)
[] - CLOUD: Update all CLI help strings and comments referencing `ledit agent`, `ledit custom add`, `ledit service install` etc. to use `sprout` prefix (in `cmd/*.go` and `pkg/`)
[] - CLOUD: Update desktop/Electron branding: window title, app ID `dev.alantheprice.sprout`, `desktop/package.json` name/productName/build.appId
[] - CLOUD: Update install scripts `scripts/install.sh` and `scripts/install.ps1` with sprout binary name and GitHub URL paths
[] - CLOUD: Clean install script — Ensure no remaining `ledit`/`Ledit`/`LEDIT` references in source (excluding `.ledit` config dir paths and backward-compat test files)

### 2. Add Token Metrics to Structured JSON Output

[] - CLOUD: Extend `AgentResultMetrics` in `cmd/agent_result.go` with `TokensIn`, `TokensOut`, `LLMCalls`, `Provider`, `Model` fields
[] - CLOUD: Create `pkg/agent/metrics.go` with thread-safe `ExecutionMetrics` accumulator (RecordCall with mutex-protected totals)
[] - CLOUD: Hook token count accumulation into LLM completion/chat response handler (from API `usage` fields) — ensure subagent LLM calls are excluded from provider/model (record primary agent only)
[] - CLOUD: Pass `ExecutionMetrics` to `emitJSONResult` and populate the new fields in the JSON output
[] - CLOUD: Add tests for token metrics — verify accumulation across multiple LLM calls, subagent exclusion, backward-compatible output structure
[] - CLOUD: Verify backward compatibility — existing tests pass, new fields are additive only

### 3. Fix `--port` vs `--web-port` Flag Inconsistency

[] - CLOUD: Add `--port` as a hidden alias for `--web-port` in `cmd/agent.go` so that `sprout agent -d --port 54000` works for the Docker entrypoint

### 4. Service Mode: Bind Address, Origin Allowlist, and Auth Header Trust

[] - CLOUD: Add `--bind` flag and `SPROUT_BIND_ADDR` env var to control web UI listen address (default: `127.0.0.1`) — update `pkg/webui/server.go` to use configurable bind address
[] - CLOUD: Add `SPROUT_ALLOWED_ORIGINS` env var (comma-separated) to origin-check middleware — accept listed origins in addition to localhost
[] - CLOUD: Add `SPROUT_TRUSTED_USER_HEADER` env var for auth header extraction in service mode — read user ID from a configurable header when `SPROUT_SERVICE=1`
[] - CLOUD: Add `GET /health` endpoint that is always accessible regardless of origin (for ALB health checks)
[] - CLOUD: Verify that in local mode (no `SPROUT_SERVICE`), the trusted user header is ignored (no spoofing)

### 5. Git Diff Robustness — Handle Missing HEAD

[] - CLOUD: Update `emitJSONResult` in `cmd/agent_result.go` to handle missing HEAD (fall back to `git diff` without HEAD ref)
[] - CLOUD: Include untracked new files in `files_modified` via `git ls-files --others --exclude-standard`
[] - CLOUD: Verify no duplicate entries in `files_modified` list

### 6. WASM Shell — Merge and Rebrand

[x] - CLOUD: Merge `browser-wasm-fileserver` branch into main and verify all files present, no `ledit`/`Ledit`/`LEDIT` references remain in Go/JS/WASM source (commit `f7efa5b2`)
[x] - CLOUD: Verify `pkg/wasmshell/` tests pass, `SproutWasm` global and `sprout-wasm-fs` IndexedDB name are correct
[x] - CLOUD: Add `build-wasm` target to `Makefile` and integrate into `build-all` so the WASM binary is rebuilt when `pkg/wasmshell/` or `cmd/wasm/` changes

### 7. Make WebUI Servable by Sprout Foundry via Service Worker Shim

[] - CLOUD: Add `--dist` flag to `scripts/build-wasm.sh` that produces a self-contained distributable directory (webui build + WASM binary + version.json)
[] - CLOUD: Create `webui/src/config/mode.ts` feature flag module — read `REACT_APP_SPROUT_MODE` and export `isCloud`, `supportsSSH`, `supportsInstances`, `supportsLocalTerminal`, `supportsSettings` flags
[] - CLOUD: Conditionally render SSH panels, instance management panels, local terminal PTY, and local settings in WebUI components based on cloud mode feature flags
[] - CLOUD: Ensure the webui renders gracefully when no Go backend is reachable (shows a connection error message, but editor/file tree/terminal still load via WASM)
[] - CLOUD: Make `wasmShell.ts` paths configurable — accept optional `wasmUrl` and `wasmExecUrl` in `initWasmShell()` config parameter
[] - CLOUD: Add `build-webui-dist` and `build-webui-dist-local` makefile targets for cloud-mode and local-mode distributable bundles
[] - CLOUD: Verify the dist bundle serves correctly from a plain static HTTP server (all assets load, no 404s, app renders without backend)

<!-- Dependency order: [1] rename first → [2]–[5] can be done in parallel → [7] depends on [1] + [6]. Section [6] is already complete. -->

---

## Core Architecture & Engineering Improvements

These are high-impact structural improvements identified through code evaluation. They address fundamental architectural concerns that will improve maintainability, testability, and reliability as the project scales.

[] - ARCHITECTURE: Refactor `pkg/agent/agent.go` to decompose the "God Object" Agent struct — The current `Agent` struct holds ~100 fields spanning LLM client logic, security, MCP management, WebUI event routing, terminal output, and tool execution. This creates tight coupling and makes unit testing difficult. Break into specialized sub-managers: `OutputManager` (interface for CLI/WebUI output), `SecurityManager` (approvals, redaction, elevation), `MCPManager` (server lifecycle), and `StateManager` (conversation history, checkpoints). The core `Agent` should only coordinate these via interfaces, not hold direct implementation references. Target: reduce `Agent` struct to < 30 fields and isolate presentation logic from core orchestration.

[] - CONCURRENCY: Improve synchronization patterns in `pkg/agent/agent.go` and related packages — The code uses ~15 different mutexes (`sync.Mutex`, `sync.RWMutex`, `sync.Once`) protecting individual fields. This creates risk of race conditions and deadlocks as complexity grows. Implement: (1) Encapsulated state objects that update atomically (e.g., `ConversationState` with its own mutex, `SecurityState` with its own mutex), (2) Use channels for cross-component communication instead of direct method calls from goroutines, (3) Audit all field accesses in `CheckFileContentSecurity`, `ProcessQuery`, and tool handlers to ensure consistent locking. Target: reduce mutex count by 40% through state encapsulation and add race detector tests (`go test -race`) to CI.

[] - OBSERVABILITY: Implement structured error taxonomy and diagnostic logging — Currently errors use ad-hoc `fmt.Errorf` wrapping without classification, making it hard for the Agent to implement intelligent retry/recovery logic. Create: (1) Error types package (`pkg/errors/types.go`) with categorized errors (`ErrTransientProvider`, `ErrSecurityViolation`, `ErrInvalidInput`, `ErrRateLimited`), (2) Structured logging interface that automatically attaches context (`sessionID`, `iteration`, `provider`, `model`) to all log entries, (3) Replace `fmt.Printf` debug statements with the structured logger. Target: 100% of errors in `pkg/agent/` use typed errors; debug logs include session context; Agent implements retry logic based on error type (transient = retry with backoff, security = stop and prompt).
