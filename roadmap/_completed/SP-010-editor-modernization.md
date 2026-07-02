# SP-010: Editor Modernization

**Status:** ✅ Implemented (EditorPane 2604→513 lines; EditorCore extracted; Error Lens, inlay hints, signature help)

The editor (`EditorPane.tsx`) was the largest component in the webui at 2,604 lines — 5x over the 500-line target — bundling file I/O, CodeMirror extensions, cursor tracking, scroll sync, diagnostics, symbol extraction, and rendering into one component with 25 `useState` variables. This spec decomposed it into focused hooks and sub-components, shipped missing IDE features (Error Lens, word occurrence highlighting, inlay hints, signature help), and added performance fixes (React.memo on children, symbol extraction keyed to content not cursor).

## Key decisions

- Extracted 6 hooks from EditorPane: `useEditorExtensions`, `useEditorDiagnostics`, `useEditorFileIO`, `useEditorScrollSync`, `useEditorSymbols`, `useEditorCursor` — each under 200 lines.
- Created `EditorCore.tsx` as the CodeMirror `EditorView` mount point + extension context — EditorPane became a ~513-line composition root.
- Error Lens uses a `StateField` reading from the existing lint diagnostics compartment, rendering `Decoration.widget` at end of each diagnostic line (debounced 300ms).
- Word occurrence highlighting reuses `highlightSelectionMatches()` from `@codemirror/search` (already imported but not wired).
- Removed the 3-pane split limit — panes are now configurable up to 6.
- `React.memo` applied to `EditorTabs`, `EditorBreadcrumb`, `EditorToolbar` to prevent unnecessary re-renders on parent state changes.

## Artifacts

- code: `webui/src/components/EditorPane.tsx` — composition root (513 lines, down from 2,604)
- code: `webui/src/components/EditorCore.tsx` — CodeMirror EditorView mount point + extension context
- code: `webui/src/extensions/errorLens.ts` — inline diagnostics at end of diagnostic lines
- code: `webui/src/extensions/inlayHints.ts` — LSP inlay hints for type/parameter annotations
- code: `webui/src/extensions/signatureHelp.ts` — function signature popup on `(` and `,`

Full specification archived — see git history for original content.
