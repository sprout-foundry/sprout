/**
 * useEditorSymbols — optimized hook for computing enclosing symbols for breadcrumb display.
 *
 * Decouples expensive symbol extraction (regex-based parsing of the entire document)
 * from cheap cursor-based filtering. This prevents the expensive extraction from
 * running on every cursor move — it only re-runs when document content changes.
 *
 * @example
 * ```tsx
 * const { enclosingSymbols } = useEditorSymbols(localContent, buffer);
 * ```
 */
import { useMemo } from 'react';
import type { EditorBuffer } from '../types/editor';
import { extractSymbols, findSymbolScopeEnd, CONTAINER_KINDS, type SymbolInfo } from '../utils/symbolUtils';

/** BreadcrumbSymbol is an alias for SymbolInfo (re-exported for convenience) */
export type BreadcrumbSymbol = SymbolInfo;

export interface UseEditorSymbolsReturn {
  /** Symbols (up to 3) that enclose the current cursor position, sorted outermost → innermost */
  enclosingSymbols: BreadcrumbSymbol[];
}

/**
 * Hook that computes enclosing symbols for breadcrumb display.
 *
 * Uses a two-stage memoization strategy:
 * - Stage 1 (content-keyed): Extract all symbols using `extractSymbols()` — only runs
 *   when `localContent` or `file extension` changes. This is the expensive regex-based
 *   parsing step that should NOT run on every cursor move.
 * - Stage 2 (cursor-keyed): Filter extracted symbols to find those enclosing the cursor —
 *   runs on cursor position changes but is cheap (just iteration and scope checking).
 *
 * The cursor line in `buffer.cursorPosition` is 0-based (CodeMirror convention), but
 * `findSymbolScopeEnd` expects 0-based indices while symbol line numbers are 1-based.
 * The conversion is handled internally.
 *
 * @param localContent - The current editor content string
 * @param buffer - The EditorBuffer containing cursor position and file extension
 * @returns Object with `enclosingSymbols` array (empty if no content or no symbols found)
 */
export function useEditorSymbols(
  localContent: string | undefined,
  buffer: EditorBuffer | null | undefined,
): UseEditorSymbolsReturn {
  // Stage 1: Extract ALL symbols from the content.
  // Memoized on content and extension only — does NOT depend on cursor position.
  // This is the expensive regex-based parsing step that must not run on every cursor move.
  const allSymbols = useMemo(() => {
    if (!localContent || !buffer?.file?.ext) {
      return [];
    }
    return extractSymbols(localContent, buffer.file.ext);
  }, [localContent, buffer?.file?.ext]);

  // Stage 2: Filter extracted symbols to find those enclosing the cursor.
  // Cheap O(n) iteration over pre-computed symbols — only re-runs when cursor moves.
  const enclosingSymbols = useMemo(() => {
    if (!localContent || !buffer?.cursorPosition) {
      return [];
    }

    // Cursor line is 0-based in buffer, convert to 1-based for comparison with symbol lines
    const cursorLine = buffer.cursorPosition.line + 1;
    const ext = buffer.file?.ext;

    // Split content once for scope checking
    const lines = localContent.split('\n');

    // Filter to container kinds only; extractSymbols returns in line-ascending order (outermost first)
    const containers = allSymbols.filter((s) => CONTAINER_KINDS.has(s.kind) && s.line <= cursorLine);

    // Find containers whose scopes enclose the cursor (up to 3 levels deep)
    const result: SymbolInfo[] = [];
    for (const sym of containers) {
      const endLine = findSymbolScopeEnd(lines, sym.line - 1, ext);
      if (cursorLine <= endLine) {
        result.push(sym);
        if (result.length >= 3) {
          break; // Cap at 3 levels deep (typical breadcrumb limit)
        }
      }
    }

    return result;
  }, [localContent, buffer?.cursorPosition?.line, buffer?.file?.ext, allSymbols]);

  return { enclosingSymbols };
}
