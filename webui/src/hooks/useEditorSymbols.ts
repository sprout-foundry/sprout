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
 * Fast content checksum using a dual-hash approach (djb2 + FNV-1a).
 * Combining two independent 32-bit hash functions dramatically reduces
 * collision probability compared to a single hash, while remaining
 * cheap enough to run on every content change.
 * Returns a composite key like `"djb2:12345|fnv:67890"`.
 */
function contentChecksum(doc: string): string {
  // djb2 hash
  let djb2 = 5381;
  for (let i = 0; i < doc.length; i++) {
    djb2 = ((djb2 << 5) + djb2 + doc.charCodeAt(i)) | 0;
  }

  // FNV-1a hash (32-bit)
  let fnv = 2166136261 >>> 0; // offset basis, force unsigned
  for (let i = 0; i < doc.length; i++) {
    fnv = (fnv ^ doc.charCodeAt(i)) >>> 0;
    fnv = (fnv * 16777619) >>> 0; // FNV prime, force unsigned
  }

  return `djb2:${djb2}|fnv:${fnv}`;
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
  // Compute a stable content fingerprint. This ensures that symbol extraction
  // is keyed to actual content changes, not string reference identity. If the
  // parent passes a new string object with identical content, the hash stays
  // the same and useMemo skips re-extraction. Wrapped in useMemo so the
  // dual-hash is only recomputed when the string reference actually changes.
  const contentKey = useMemo(
    () => (localContent ? contentChecksum(localContent) : ''),
    [localContent],
  );

  // Stage 1: Extract ALL symbols from the content.
  // Memoized on content fingerprint and extension only — does NOT depend on cursor position.
  // This is the expensive regex-based parsing step that must not run on every cursor move.
  const allSymbols = useMemo(() => {
    if (!localContent || !buffer?.file?.ext) {
      return [];
    }
    return extractSymbols(localContent, buffer.file.ext);
  }, [contentKey, buffer?.file?.ext]);

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
  }, [contentKey, buffer?.cursorPosition?.line, buffer?.file?.ext, allSymbols]);

  return { enclosingSymbols };
}
