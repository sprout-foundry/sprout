/**
 * bracketColorization.ts — CodeMirror 6 extension for rainbow bracket coloring.
 *
 * Assigns distinct colors to nested bracket pairs: `()`, `[]`, `{}`.
 * Each nesting level gets a color from the set `--cm-bracket-0` through
 * `--cm-bracket-5`, wrapping at depth 6 (modulo).
 *
 * Angle brackets `<>` are intentionally NOT colored (they appear too
 * frequently in generics, JSX, and HTML templates).
 *
 * Algorithm:
 * - Scans the entire document text in O(n) to build a flat array mapping
 *   each bracket position to its nesting depth.
 * - Uses a simple stack: opening brackets push, matching closing brackets
 *   pop.  Unmatched closing brackets (wrong type or empty stack) are ignored.
 * - Only brackets within the current viewport are turned into
 *   `Decoration.mark` instances.
 *
 * Theming:
 * - Colors are drawn from CSS custom properties `--cm-bracket-0` …
 *   `--cm-bracket-5`, so each theme pack can provide its own palette.
 * - The `> span` selectors force inner CodeMirror syntax-highlighting spans
 *   to inherit the bracket colour instead of their syntax colour.
 */

import {
  Decoration,
  DecorationSet,
  EditorView,
  ViewPlugin,
  ViewUpdate,
} from '@codemirror/view';
import { type Extension, type Text } from '@codemirror/state';

// ── Types ──────────────────────────────────────────────────────────

/** A single bracket character's position in the document and its nesting depth. */
export interface BracketDecoration {
  /** Document position of the bracket's first character. */
  from: number;
  /** Document position one past the bracket's last character. */
  to: number;
  /** Nesting depth modulo 6 (0–5). */
  depth: number;
}

// ── Constants ──────────────────────────────────────────────────────

const OPEN_BRACKETS = new Set(['(', '[', '{']);

/** Maps each closing bracket to its matching opening bracket. */
const MATCHING_CLOSE: ReadonlyMap<string, string> = new Map([
  [')', '('],
  [']', '['],
  ['}', '{'],
]);

export const MAX_DEPTH = 6;

/** Matches any of the six bracket characters our colorizer cares about. */
const BRACKET_PATTERN = /[\[()\]{}]/;

// ── Pure helpers (exported for testing) ────────────────────────────

/**
 * Scan `text` and return every bracket's position and nesting depth.
 *
 * Opening brackets are always included.  A closing bracket is included
 * only when it matches the bracket on top of the depth stack; otherwise
 * it is silently ignored (treated as a stray character).
 *
 * Depth wraps at 6 (modulo), giving six distinct colour buckets.
 *
 * @param text — The full document text to scan.
 * @returns An array of `{ from, to, depth }` entries, one per coloured bracket.
 */
export function computeBracketDecorations(text: string): BracketDecoration[] {
  const result: BracketDecoration[] = [];

  // Stack entries track position, computed depth, and the bracket character
  // so we can validate pair types on close.
  const stack: { pos: number; depth: number; bracket: string }[] = [];

  for (let i = 0; i < text.length; i++) {
    const ch = text[i];

    if (OPEN_BRACKETS.has(ch)) {
      // Opening bracket: record at current stack size, then push.
      const depth = stack.length % MAX_DEPTH;
      stack.push({ pos: i, depth, bracket: ch });
      result.push({ from: i, to: i + 1, depth });
    } else if (MATCHING_CLOSE.has(ch)) {
      const expected = MATCHING_CLOSE.get(ch)!;
      // Only pop when the top of the stack matches.  Stray or mismatched
      // closing brackets are ignored to keep the depth tracking simple
      // and well-defined.
      if (stack.length > 0 && stack[stack.length - 1].bracket === expected) {
        const match = stack.pop()!;
        result.push({ from: i, to: i + 1, depth: match.depth });
      }
    }
    // All other characters (including '<' and '>') are skipped.
  }

  return result;
}

/**
 * Same as `computeBracketDecorations` but accepts a CodeMirror `Text`
 * object.  Delegates to the string-based implementation to avoid
 * maintaining a parallel, untested code path.
 */
export function computeBracketDecorationsFromDoc(doc: Text): BracketDecoration[] {
  return computeBracketDecorations(doc.toString());
}

// ── Decoration factories ───────────────────────────────────────────

/** Pre-built mark decorations keyed by depth (0–5). Reused across updates. */
const markDecorations: readonly Decoration[] = Array.from(
  { length: MAX_DEPTH },
  (_, depth) =>
    Decoration.mark({ class: `cm-bracket-depth-${depth}` }),
);

// ── ViewPlugin ─────────────────────────────────────────────────────

const bracketPlugin = ViewPlugin.fromClass(
  class BracketColorizationPlugin {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = this.buildDecorations(view);
    }

    update(update: ViewUpdate): void {
      if (update.viewportChanged || update.transactions.some((t) => t.reconfigured)) {
        this.decorations = this.buildDecorations(update.view);
      } else if (update.docChanged) {
        // Only rescan if the changed regions contain any bracket characters.
        // Typing non-bracket text should not trigger a full-document rescan.
        let bracketsChanged = false;
        update.changes.iterChanges((fromA, toA, _fromB, _toB, inserted) => {
          if (bracketsChanged) return; // short-circuit once found
          const deleted = update.startState.doc.sliceString(fromA, toA);
          if (BRACKET_PATTERN.test(deleted) || BRACKET_PATTERN.test(inserted.toString())) {
            bracketsChanged = true;
          }
        });
        if (bracketsChanged) {
          this.decorations = this.buildDecorations(update.view);
        }
      }
    }

    /**
     * Recompute bracket decorations for the current viewport.
     *
     * 1. Scan the entire document to build the global depth map.
     * 2. Filter to only brackets inside `view.viewport`.
     * 3. Build a `DecorationSet` from the filtered brackets.
     *
     * The O(n) full-document scan is acceptable because the decorations
     * object is only rebuilt when the document or viewport actually
     * changes (not on every selection move).
     */
    private buildDecorations(view: EditorView): DecorationSet {
      const doc = view.state.doc;
      if (doc.length === 0) return Decoration.none;

      const allBrackets = computeBracketDecorationsFromDoc(doc);
      if (allBrackets.length === 0) return Decoration.none;

      const { from: viewFrom, to: viewTo } = view.viewport;

      // Collect decorations inside the viewport.  Because allBrackets is
      // generated in document order the resulting array is already sorted.
      const ranges: ReturnType<Decoration['range']>[] = [];
      for (const bracket of allBrackets) {
        // A single-char bracket at position `from` overlaps the viewport
        // when `from < viewTo && (from + 1) > viewFrom`.
        if (bracket.from < viewTo && bracket.to > viewFrom) {
          ranges.push(
            markDecorations[bracket.depth].range(bracket.from, bracket.to),
          );
        }
      }

      return Decoration.set(ranges, true); // already sorted
    }
  },
  {
    // CM calls this getter after every update to obtain the current
    // decoration set, so it stays in sync with the view.
    decorations: (v) => v.decorations,
  },
);

// ── Base theme ─────────────────────────────────────────────────────

/**
 * Generate the CSS rules for the six depth levels.
 *
 * Each depth level gets two selectors:
 * - `.cm-bracket-depth-N` — colours the bracket character itself.
 * - `.cm-bracket-depth-N > span` — overrides inner syntax-highlighting
 *   spans that CodeMirror nests inside our mark `<span>`.
 */
function depthStyles(): Record<string, { color: string }> {
  const styles: Record<string, { color: string }> = {};
  for (let i = 0; i < MAX_DEPTH; i++) {
    const cssVar = `--cm-bracket-${i}`;
    styles[`.cm-bracket-depth-${i}`] = { color: `var(${cssVar})` };
    styles[`.cm-bracket-depth-${i} > span`] = { color: `var(${cssVar})` };
  }
  return styles;
}

const bracketBaseTheme = EditorView.baseTheme(depthStyles());

// ── Public API ──────────────────────────────────────────────────────

/**
 * Returns a CodeMirror extension bundle that colors brackets by nesting depth.
 *
 * Include in the editor's extensions array:
 * ```ts
 * import { bracketColorizationPlugin } from '../extensions/bracketColorization';
 * // …
 * extensions: [..., bracketColorizationPlugin(), ...]
 * ```
 */
export function bracketColorizationPlugin(): Extension {
  return [bracketBaseTheme, bracketPlugin];
}
