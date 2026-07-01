# SP-010: Editor Modernization

**Status:** ✅ Implemented (EditorPane 2604→513 lines; EditorCore extracted; React.memo + 18 bug fixes)
**Depends on:** SP-003 (Webui & Frontend Architecture)  
**Priority:** High  
**Effort Estimate:** ~4-5 weeks (3 phases)

## Problem

The editor (`EditorPane.tsx`, 2604 lines) is the largest component in the webui and violates the project's 500-line file size target by 5x. It bundles file I/O, CodeMirror extension management, cursor tracking, scroll synchronization, diagnostics, symbol extraction, and rendering into a single component with 25 `useState` variables and 73+ hook calls. This causes:

1. **Performance**: 25 state variables in one component means any state change triggers a re-evaluation of all hooks and potentially re-renders child components. Symbol extraction runs on every cursor move.
2. **Maintainability**: Adding any editor feature requires understanding the entire 2604-line file.
3. **Missing IDE features**: Error Lens (inline diagnostics), inlay hints, word occurrence highlighting, signature help are not implemented despite the LSP infrastructure being in place.

## Current State

### Component Size

| File | Lines | Target | Status |
|------|-------|--------|--------|
| `EditorPane.tsx` | 2604 | 500 | ❌ 5x over |
| `EditorManagerContext.tsx` | 486 | 500 | ✅ |
| `EditorTabs.tsx` | varies | 500 | ✅ |
| `EditorBreadcrumb.tsx` | varies | 500 | ✅ |

### CodeMirror 6 Extensions (already implemented)

| Extension | File | Status |
|-----------|------|--------|
| Syntax highlighting (25+ langs) | EditorPane.tsx | ✅ |
| Search/Replace (regex, case, whole word) | searchPanel.ts | ✅ |
| Bracket matching + auto-close | EditorPane.tsx | ✅ |
| Code folding + fold gutter | EditorPane.tsx | ✅ |
| Sticky scroll | stickyScroll.ts | ✅ |
| Minimap | minimap.ts | ✅ |
| Code lens (reference counts) | codeLens.ts | ✅ |
| Rainbow bracket colorization | bracketColorization.ts | ✅ |
| Hover tooltips (semantic API) | hoverTooltip.ts | ✅ |
| Rename overlay (F2) | renameOverlay.ts (437 lines) | ✅ |
| Document outline panel | DocumentOutlinePanel.tsx | ✅ |
| Snippets (20+ languages) | snippets.ts | ✅ |
| Emmet (HTML/CSS/JSX) | emmet.ts | ✅ |
| LSP integration | lspExtensions.ts | ✅ |
| Lint diagnostics (gutter) | lintDiagnostics.ts | ✅ |
| Cursor history | cursorHistory.ts | ✅ |
| Drag-drop move | dragDropMove.ts | ✅ |
| Indent guides | indentGuides.ts | ✅ |
| Linked scroll (split panes) | linkedScroll.ts | ✅ |
| Trailing whitespace | trailingWhitespace.ts | ✅ |
| Relative line numbers | EditorPane.tsx | ✅ |
| Color highlighting | EditorPane.tsx | ✅ |

### What's Missing

| Feature | Priority | Notes |
|---------|----------|-------|
| Error Lens (inline diagnostics) | High | Show error/warning text inline at end of line |
| Word occurrence highlighting | High | Highlight all instances of selected word |
| Inlay hints (type/parameter) | Medium | LSP capability exists, not wired to UI |
| Signature help | Medium | LSP capability exists, not wired to UI |
| Format on save | Medium | Formatter service exists, not auto-triggered |
| Go-to-references panel | Medium | Hover tooltip has refs, no dedicated panel |
| Tab tooltips | Low | Truncated filenames need hover-to-reveal full path |
| Editor tab icons | Low | File type icons in tabs |

### Performance Issues

1. **Symbol extraction on cursor move** (EditorPane.tsx ~line 2018): `getEnclosingSymbols()` runs every 100ms on cursor movement. Should be keyed to content changes, not cursor position.
2. **25 useState variables**: Any single state change triggers all hooks to re-evaluate.
3. **No React.memo on child components**: EditorTabs, EditorBreadcrumb, etc. re-render on parent state changes.
4. **Diagnostics update on every content change** (500ms debounce): Could be smarter about when to re-request.

## Proposed Solution

### Phase 1: Decompose EditorPane (Week 1-2)

Break `EditorPane.tsx` into focused sub-components and hooks:

**New hooks (extracted from EditorPane):**

| Hook | Responsibility | Target Lines |
|------|----------------|--------------|
| `useEditorExtensions.ts` | Build CodeMirror extension set from buffer config | ~150 |
| `useEditorDiagnostics.ts` | Diagnostic fetching, lint gutter updates | ~120 |
| `useEditorFileIO.ts` | File load/save, external change handling | ~200 |
| `useEditorScrollSync.ts` | Scroll position persistence, cross-pane sync | ~100 |
| `useEditorSymbols.ts` | Symbol extraction, breadcrumb data | ~100 |
| `useEditorCursor.ts` | Cursor position tracking, selection state | ~80 |

**New components:**

| Component | Responsibility | Target Lines |
|-----------|----------------|--------------|
| `EditorCore.tsx` | CodeMirror EditorView mount point + extension context | ~200 |
| `EditorToolbarActions.tsx` | Toolbar buttons (word wrap, format, etc.) | ~150 |

**Resulting EditorPane.tsx**: ~300 lines — composition root orchestrating hooks and sub-components.

### Phase 2: Missing IDE Features (Week 3-4)

#### Error Lens

```typescript
// webui/src/extensions/errorLens.ts
// Shows diagnostic message inline, at the end of the diagnostic line
// Style: faded text color, clickable to focus the diagnostic
// Debounced 300ms to batch rapid diagnostic updates
```

Pattern: Use a `StateField` that reads diagnostics from the existing lint diagnostics compartment and renders `Decoration.widget` at end of each diagnostic line.

#### Word Occurrence Highlighting

```typescript
// webui/src/extensions/wordHighlights.ts
// On double-click or Ctrl+D: highlight all occurrences of selected word
// Uses existing highlightSelectionMatches from @codemirror/search
// (already imported but may need custom styling)
```

Note: `highlightSelectionMatches()` is already imported in EditorPane.tsx (line 1394) — verify it works correctly and add custom styling.

#### Inlay Hints

```typescript
// webui/src/extensions/inlayHints.ts
// Request LSP inlay hints via semantic API for TypeScript/Go
// Show type annotations and parameter names inline
// Toggle via editor settings or menu bar
```

#### Signature Help

```typescript
// webui/src/extensions/signatureHelp.ts
// When typing `(` or `,` inside a function call, show a tooltip
// with the current parameter signature and documentation
// Uses LSP signatureHelp capability
```

### Phase 3: Performance & Polish (Week 4-5)

1. **Memoize child components**: Wrap `EditorTabs`, `EditorBreadcrumb`, `EditorToolbar` with `React.memo`
2. **Fix symbol extraction**: Key to content checksum, not cursor position
3. **Tab tooltips**: Add `title` attribute to tab name showing full file path
4. **Remove 3-pane limit**: Allow up to 6 panes (configurable)
5. **Editor tab icons**: Add file-type icons based on extension
6. **Format on save**: Wire existing formatter to save action (opt-in setting)

## Implementation Phases

### Phase 1: Decompose (Week 1-2)

**New files:**
- `webui/src/hooks/useEditorExtensions.ts`
- `webui/src/hooks/useEditorDiagnostics.ts`
- `webui/src/hooks/useEditorFileIO.ts`
- `webui/src/hooks/useEditorScrollSync.ts`
- `webui/src/hooks/useEditorSymbols.ts`
- `webui/src/hooks/useEditorCursor.ts`
- `webui/src/components/EditorCore.tsx`
- `webui/src/components/EditorToolbarActions.tsx`

**Modified files:**
- `webui/src/components/EditorPane.tsx` — reduced to composition root (~300 lines)

### Phase 2: Features (Week 3-4)

**New files:**
- `webui/src/extensions/errorLens.ts`
- `webui/src/extensions/inlayHints.ts`
- `webui/src/extensions/signatureHelp.ts`
- `webui/src/extensions/errorLens.css`

**Modified files:**
- `webui/src/components/EditorPane.tsx` — wire new extensions
- `webui/src/extensions/wordHighlights.ts` — verify or fix highlightSelectionMatches

### Phase 3: Polish (Week 4-5)

**Modified files:**
- `webui/src/components/EditorTabs.tsx` — add tooltips, icons
- `webui/src/contexts/EditorManagerContext.tsx` — remove 3-pane limit
- `webui/src/components/EditorToolbar.tsx` — format-on-save toggle
- Performance: add React.memo to child components

## Success Criteria

| Metric | Target |
|--------|--------|
| `EditorPane.tsx` size | Under 400 lines |
| All extracted hooks | Under 200 lines each |
| Error Lens | Shows diagnostics inline in editor |
| Word highlights | Visible on double-click/selection |
| Tab tooltips | Full file path visible on hover |
| React.memo | Applied to all editor child components |
| Build | `make build-all` passes |

## Files Reference

| File | Action |
|------|--------|
| `webui/src/components/EditorPane.tsx` | Major: decompose into hooks + sub-components |
| `webui/src/contexts/EditorManagerContext.tsx` | Modify: remove 3-pane limit |
| `webui/src/components/EditorTabs.tsx` | Modify: add tooltips, icons |
| `webui/src/extensions/errorLens.ts` | Create: inline diagnostics |
| `webui/src/extensions/inlayHints.ts` | Create: type/parameter hints |
| `webui/src/extensions/signatureHelp.ts` | Create: function signature popup |
| `webui/src/hooks/useEditor*.ts` | Create: 6 extracted hooks |
| `webui/src/components/EditorCore.tsx` | Create: CodeMirror mount point |
