/**
 * unsavedLineHighlight.ts — CodeMirror 6 extension for highlighting
 * lines modified since the last save.
 *
 * Compares `EditorState.doc` against the buffer's `originalContent`
 * (injected via `setOriginalContent` StateEffect) and adds a subtle
 * background decoration to modified/added lines — similar to VS Code's
 * minimap modified-region indicators, but inline.
 *
 * Architecture:
 * - A `StateEffect.define<string>` carries the `originalContent` string
 *   from React into CodeMirror.
 * - A `StateField` stores the raw originalContent text.
 * - A `ViewPlugin` compares the current doc lines against the stored
 *   originalContent lines and builds `Decoration.line` decorations for
 *   every line that differs. Only visible (viewport) lines are
 *   processed for performance on large files.
 *
 * Block-decoration delivery: `Decoration.line` decorations affect the
 * editor's vertical layout, so CodeMirror only allows them through the
 * `EditorView.decorations` facet — a ViewPlugin's `decorations` option
 * throws "Block decorations may not be specified via plugins". The
 * plugin therefore pushes its computed set into a dedicated StateField
 * (via `setUnsavedDecorations` StateEffect) whose `provide` wires it
 * into the facet.
 *
 * Theming:
 * - Uses `--diff-mod-color` CSS variable for visual consistency with
 *   the existing git diff gutter markers.
 */

import { StateEffect, StateField, Annotation, type Extension } from '@codemirror/state';
import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate } from '@codemirror/view';

// ── State effect ────────────────────────────────────────────────────

/**
 * State effect to push the buffer's `originalContent` (content at last
 * save / load) into the CodeMirror state.  Dispatched from React whenever
 * `buffer.originalContent` changes.
 */
export const setOriginalContent = StateEffect.define<string>();

/** Annotation marking the plugin's own decoration-rebuild dispatch, so
 *  the rebuild transaction (which only carries the decoration-set effect)
 *  doesn't re-trigger another debounce cycle. */
const unsavedRebuildAnnotation = Annotation.define<boolean>();

// ── State field ─────────────────────────────────────────────────────

/**
 * Stores the original content string inside CodeMirror state.  The
 * ViewPlugin reads this to compute per-line diff decorations.
 */
const originalContentField = StateField.define<string>({
  create() {
    return '';
  },
  update(value, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setOriginalContent)) {
        return effect.value;
      }
    }
    return value;
  },
});

// ── Decoration delivery ─────────────────────────────────────────────

/**
 * State effect that carries a freshly computed unsaved-line decoration
 * set from the ViewPlugin into the StateField below.
 */
const setUnsavedDecorations = StateEffect.define<DecorationSet>();

/**
 * Holds the current unsaved-line decoration set and provides it directly
 * to the EditorView.decorations facet. Block decorations (Decoration.line)
 * affect the editor's vertical layout and therefore may only be supplied
 * via this facet — providing them through a ViewPlugin's `decorations`
 * option throws "Block decorations may not be specified via plugins".
 */
const unsavedDecorationsField = StateField.define<DecorationSet>({
  create() {
    return Decoration.none;
  },
  update(value, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setUnsavedDecorations)) {
        return effect.value;
      }
    }
    // Keep decorations roughly in place between debounced rebuilds.
    return value.map(tr.changes);
  },
  provide: (f) => EditorView.decorations.from(f),
});

// ── Line-diff helpers ───────────────────────────────────────────────

/**
 * Compare current document text against original text and return a set of
 * 1-based line numbers that have been added or modified.
 *
 * Uses a simple longest-common-subsequence (LCS) approach to identify
 * which lines in the current document are new or changed relative to
 * the original.  This handles insertions, deletions, and modifications
 * correctly.
 *
 * @param currentLines — Lines of the current document text.
 * @param originalLines — Lines of the original (saved) text.
 * @returns Set of 1-based line numbers that differ from the original.
 */
export function computeModifiedLines(currentLines: string[], originalLines: string[]): Set<number> {
  const result = new Set<number>();

  // Fast path: identical content
  if (currentLines.length === originalLines.length) {
    let identical = true;
    for (let i = 0; i < currentLines.length; i++) {
      if (currentLines[i] !== originalLines[i]) {
        identical = false;
        break;
      }
    }
    if (identical) return result;
  }

  // Fast path: empty original means every line is new
  if (originalLines.length === 0) {
    for (let i = 1; i <= currentLines.length; i++) {
      result.add(i);
    }
    return result;
  }

  // For very large files, fall back to a simpler heuristic to avoid
  // O(n*m) LCS memory.  Compare line-by-line from the top and bottom,
  // marking the differing middle region as modified.
  const MAX_LCS_SIZE = 3000;
  if (currentLines.length > MAX_LCS_SIZE || originalLines.length > MAX_LCS_SIZE) {
    return computeModifiedLinesFastPath(currentLines, originalLines);
  }

  // LCS-based diff
  const m = currentLines.length;
  const n = originalLines.length;

  // Build LCS table
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (currentLines[i - 1] === originalLines[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  // Backtrack to identify added lines in the current document.
  // Lines in `currentLines` that are NOT part of the LCS are "added/modified".
  let i = m;
  let j = n;
  const inLCS = new Set<number>(); // 1-based indices into currentLines

  while (i > 0 && j > 0) {
    if (currentLines[i - 1] === originalLines[j - 1]) {
      inLCS.add(i); // line i (1-based) is in the LCS
      i--;
      j--;
    } else if (dp[i][j - 1] >= dp[i - 1][j]) {
      j--;
    } else {
      i--;
    }
  }

  // Any current line not in the LCS is modified/added
  for (let k = 1; k <= m; k++) {
    if (!inLCS.has(k)) {
      result.add(k);
    }
  }

  return result;
}

/**
 * Fast-path diff for large files.  Finds the common prefix and suffix,
 * then marks everything in between as modified.  This is O(n) but less
 * precise than LCS (over-marks in some cases).
 */
function computeModifiedLinesFastPath(currentLines: string[], originalLines: string[]): Set<number> {
  const result = new Set<number>();

  // Find common prefix length
  let prefixLen = 0;
  const minLen = Math.min(currentLines.length, originalLines.length);
  while (prefixLen < minLen && currentLines[prefixLen] === originalLines[prefixLen]) {
    prefixLen++;
  }

  // Find common suffix length (not overlapping prefix)
  let suffixLen = 0;
  while (
    suffixLen < currentLines.length - prefixLen &&
    suffixLen < originalLines.length - prefixLen &&
    currentLines[currentLines.length - 1 - suffixLen] === originalLines[originalLines.length - 1 - suffixLen]
  ) {
    suffixLen++;
  }

  // Mark everything between prefix and suffix as modified
  // If prefix + suffix cover or exceed the current lines, all lines are
  // identical (or overlap case) — no modifications detected.
  if (prefixLen + suffixLen >= currentLines.length) {
    return result;
  }

  for (let i = prefixLen + 1; i <= currentLines.length - suffixLen; i++) {
    result.add(i);
  }

  return result;
}

// ── Decoration ──────────────────────────────────────────────────────

/** Line decoration applied to unsaved/modified lines. */
const unsavedLineDeco = Decoration.line({ class: 'cm-unsavedLine' });

// ── ViewPlugin ──────────────────────────────────────────────────────

const unsavedHighlightPlugin = ViewPlugin.fromClass(
  class UnsavedHighlightPlugin {
    decorations: DecorationSet;
    private debounceTimer: ReturnType<typeof setTimeout> | null = null;

    constructor(view: EditorView) {
      this.decorations = this.buildDecorations(view);
      // Dispatching inside a plugin constructor is not allowed, and the
      // plugin's update() is not called on creation — only on the first
      // real transaction. Defer the initial render to a microtask so the
      // field can serve the decorations through the view facet.
      this.schedulePush(view, this.decorations);
    }

    update(update: ViewUpdate): void {
      // Ignore our own rebuild dispatches (they only carry the
      // decoration-set effect + marker annotation) so a rebuild that
      // changes layout can't re-trigger another debounce cycle.
      if (update.transactions.some((tr) => tr.annotation(unsavedRebuildAnnotation))) {
        return;
      }

      // Rebuild on document changes, viewport changes, or when
      // originalContent is updated via StateEffect.
      const origChanged = update.transactions.some((tr) => tr.effects.some((e) => e.is(setOriginalContent)));

      if (origChanged) {
        // originalContent changes (file load/save) are not user edits —
        // rebuild immediately so highlights are correct right away.
        if (this.debounceTimer) {
          clearTimeout(this.debounceTimer);
          this.debounceTimer = null;
        }
        this.decorations = this.buildDecorations(update.view);
        this.schedulePush(update.view, this.decorations);
      } else if (update.docChanged || update.viewportChanged) {
        // Debounce user edits and viewport scrolling — the diff is O(n)
        // and doesn't need to update on every keystroke.
        if (this.debounceTimer) clearTimeout(this.debounceTimer);
        const view = update.view;
        this.debounceTimer = setTimeout(() => {
          this.debounceTimer = null;
          if (!view.dom.isConnected) return;
          this.decorations = this.buildDecorations(view);
          this.schedulePush(view, this.decorations);
        }, 300);
      }
    }

    /**
     * Push a computed decoration set into unsavedDecorationsField. Must
     * not run synchronously from a plugin constructor or update() — both
     * execute inside the view's update cycle, where dispatch is forbidden.
     */
    private schedulePush(view: EditorView, decos: DecorationSet): void {
      queueMicrotask(() => {
        if (!view.dom.isConnected) return;
        view.dispatch({
          effects: setUnsavedDecorations.of(decos),
          annotations: [unsavedRebuildAnnotation.of(true)],
        });
      });
    }

    private buildDecorations(view: EditorView): DecorationSet {
      const originalContent = view.state.field(originalContentField);

      // Fast path: no original content means buffer hasn't been loaded
      // yet or is empty — no highlights needed.
      if (!originalContent && originalContent !== '') {
        return Decoration.none;
      }

      const doc = view.state.doc;

      // Fast path: document matches original — no modifications.
      const currentText = doc.toString();
      if (currentText === originalContent) {
        return Decoration.none;
      }

      const currentLines: string[] = [];
      for (let i = 1; i <= doc.lines; i++) {
        currentLines.push(doc.line(i).text);
      }
      const originalLines = originalContent.split('\n');

      const modifiedLines = computeModifiedLines(currentLines, originalLines);

      if (modifiedLines.size === 0) {
        return Decoration.none;
      }

      // Only decorate visible lines for performance
      const { from: viewFrom, to: viewTo } = view.viewport;
      const pieces: { from: number; deco: Decoration }[] = [];

      // Walk visible lines
      let pos = viewFrom > 0 ? doc.lineAt(viewFrom).from : 0;

      while (pos < viewTo && pos < doc.length) {
        const line = doc.lineAt(pos);
        if (modifiedLines.has(line.number)) {
          pieces.push({ from: line.from, deco: unsavedLineDeco });
        }
        pos = line.to + 1;
      }

      if (pieces.length === 0) {
        return Decoration.none;
      }

      // Already sorted because we walk top-down
      return Decoration.set(
        pieces.map(({ from, deco }) => deco.range(from)),
        true,
      );
    }

    destroy() {
      if (this.debounceTimer) {
        clearTimeout(this.debounceTimer);
        this.debounceTimer = null;
      }
    }
  },
);

// ── Theme (base) ────────────────────────────────────────────────────

/**
 * Base theme styles for unsaved line highlighting.
 *
 * Uses `--diff-mod-color` for visual consistency with the git diff
 * gutter markers, applied as a subtle left border + background tint.
 */
const unsavedHighlightBaseTheme = EditorView.baseTheme({
  '.cm-unsavedLine': {
    background: 'var(--diff-mod-bg, rgba(66, 133, 244, 0.08)) !important',
    borderLeft: '2px solid var(--diff-mod-color, rgba(66, 133, 244, 0.7))',
  },
  '&light .cm-unsavedLine': {
    background: 'var(--diff-mod-bg, rgba(66, 133, 244, 0.06)) !important',
    borderLeft: '2px solid var(--diff-mod-color, rgba(66, 133, 244, 0.6))',
  },
});

// ── Public API ──────────────────────────────────────────────────────

/**
 * Returns a CodeMirror extension bundle that highlights lines modified
 * since the last save.
 *
 * Usage in EditorPane:
 * ```ts
 * import {
 *   unsavedLineHighlight,
 *   setOriginalContent,
 * } from '../extensions/unsavedLineHighlight';
 *
 * // In extensions array:
 * unsavedLineHighlightCompartment.current.of(unsavedLineHighlight()),
 *
 * // When buffer.originalContent changes:
 * view.dispatch({ effects: setOriginalContent.of(buffer.originalContent) })
 * ```
 */
export function unsavedLineHighlight(): Extension {
  return [originalContentField, unsavedDecorationsField, unsavedHighlightPlugin, unsavedHighlightBaseTheme];
}
