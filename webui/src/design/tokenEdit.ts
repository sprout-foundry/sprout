/**
 * Token editing model (SP-140-7 §7c).
 *
 * Pure functions for the structured token editor: surgical DTCG document
 * edits (parse → set ONE leaf's $value → canonical stringify), light client
 * validation (the authoritative check is design_validate via the health
 * strip), and value coercion per $type.
 *
 * The file stays the only artifact — no sidecars, no shadow state. Edits are
 * written through the §7a safe-write seam.
 */

/** The subset of the DTCG document the editor touches. */
export interface TokenLeaf {
  $type?: string;
  $value: unknown;
  $description?: string;
  [key: string]: unknown;
}

export type TokenDocument = Record<string, unknown>;

/** The editor's notion of a token: dotted path + leaf, plus its source file. */
export interface TokenRef {
  /** Dotted alias path, e.g. "color.brand.primary". */
  path: string;
  /** The file the leaf lives in (workspace-relative design/…). */
  file: string;
}

/**
 * Set ONE leaf's $value inside a parsed DTCG document. Creates intermediate
 * groups only when every level of the path already exists as an object OR the
 * path is brand-new at the document root — it never overwrites a non-object
 * mid-path (that would be data destruction, not editing). Returns a NEW
 * document; the input is not mutated.
 */
export function setTokenValue(doc: TokenDocument, dottedPath: string, value: unknown): TokenDocument {
  const parts = dottedPath.split('.').filter(Boolean);
  if (parts.length === 0) throw new Error('empty token path');
  const leafKey = parts[parts.length - 1];

  const clone: TokenDocument = { ...doc };
  let cursor: Record<string, unknown> = clone;
  for (let i = 0; i < parts.length - 1; i++) {
    const key = parts[i];
    const next = cursor[key];
    if (next === undefined) {
      const group: Record<string, unknown> = {};
      cursor[key] = group;
      cursor = group;
    } else if (typeof next === 'object' && next !== null && !Array.isArray(next)) {
      const group: Record<string, unknown> = { ...(next as Record<string, unknown>) };
      cursor[key] = group;
      cursor = group;
    } else {
      throw new Error(`token path "${dottedPath}" crosses a non-object at "${parts.slice(0, i + 1).join('.')}"`);
    }
  }
  cursor[leafKey] = {
    ...(typeof cursor[leafKey] === 'object' && cursor[leafKey] !== null ? (cursor[leafKey] as TokenLeaf) : {}),
    $value: value,
  };
  return clone;
}

/**
 * Coerce a form input string into a $value for the leaf's $type. Light and
 * type-aware: numbers stay numbers for dimension/number/fontWeight/duration,
 * everything else is a string. Returns the parsed value plus whether it is
 * plausibly valid for its type (alias refs and empty inputs are the user's
 * call — the validator reports real problems).
 */
export function coerceTokenValue(type: string | undefined, input: string): { value: unknown; plausible: boolean } {
  const trimmed = input.trim();
  if (trimmed === '') return { value: '', plausible: false };
  // Alias refs are always plausible as-is.
  if (trimmed.startsWith('{') && trimmed.endsWith('}')) return { value: trimmed, plausible: true };
  switch (type) {
    case 'number':
    case 'dimension':
    case 'fontWeight':
    case 'duration': {
      const numericPart = Number.parseFloat(trimmed);
      const plausible = Number.isFinite(numericPart);
      // Preserve the author's unit spelling for dimension/duration ("16px",
      // "120ms") rather than collapsing to a bare number.
      return { value: plausible && trimmed === String(numericPart) ? numericPart : trimmed, plausible };
    }
    case 'color': {
      const plausible =
        /^#([0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i.test(trimmed) ||
        /^rgba?\(/i.test(trimmed) ||
        /^hsla?\(/i.test(trimmed);
      return { value: trimmed, plausible };
    }
    default:
      return { value: trimmed, plausible: trimmed.length > 0 };
  }
}

/**
 * Parse + edit + serialize in one step: the surgical document edit. Throws
 * (never clobbers) when the file is not valid JSON or the path crosses a
 * non-object. The output is canonical two-space JSON with a trailing newline
 * — matching how the export tools serialize DTCG documents.
 */
export function applyTokenEdit(fileText: string, dottedPath: string, value: unknown): string {
  const parsed = JSON.parse(fileText) as TokenDocument;
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error('token file is not a DTCG document');
  }
  const next = setTokenValue(parsed, dottedPath, value);
  return `${JSON.stringify(next, null, 2)}\n`;
}

/**
 * The alias-warning input (§7c): how many references point at this token,
 * from the status endpoint's tokenRefs map. Zero for unreferenced tokens.
 */
export function referenceCount(tokenRefs: Record<string, number> | undefined, dottedPath: string): number {
  return tokenRefs?.[dottedPath] ?? 0;
}
