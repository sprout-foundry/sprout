/**
 * Grid reorder model (SP-140-7 §7e, TODO item 7.5).
 *
 * Dragging a screen card to a new position persists the README manifest's
 * `Screens:` listing order (manifest-level, human-authored curation). Pure
 * array/line manipulation — no React. The write itself goes through the §7a
 * safe-write seam in the host component.
 */

import { parseManifestStatuses } from '../services/api/designApiParse';

/**
 * Reorder an array by moving one item to a new index (stable for the others).
 * Indices out of range or from===to return the same array (no change).
 */
export function moveItem<T>(items: readonly T[], from: number, to: number): T[] {
  if (from === to) return items as T[];
  if (from < 0 || to < 0 || from >= items.length || to >= items.length) return items as T[];
  const next = [...items];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/**
 * Reorder the manifest's `Screens:` section to match `orderedStems`. Stems
 * not mentioned in the manifest keep their relative position after the listed
 * ones; listed stems the caller omitted stay in place (the rewrite is
 * order-only). Returns the new text and whether anything changed.
 */
export function reorderManifestScreens(
  manifestText: string,
  orderedStems: readonly string[],
): { text: string; changed: boolean } {
  if (orderedStems.length === 0) return { text: manifestText, changed: false };
  const lines = manifestText.split('\n');

  // Locate the Screens section: a heading line containing "Screens", up to
  // the next heading or blank-line-delimited section end.
  const sectionStart = lines.findIndex((line) => /^#{1,6}\s+.*Screens/i.test(line));
  if (sectionStart < 0) return { text: manifestText, changed: false };

  let sectionEnd = sectionStart + 1;
  while (sectionEnd < lines.length && !/^#{1,6}\s/.test(lines[sectionEnd])) sectionEnd += 1;

  // Collect listing-line indices within the section, keyed by stem.
  const listingIndices: Array<{ index: number; stem: string }> = [];
  for (let i = sectionStart + 1; i < sectionEnd; i++) {
    const stem = listingStemOf(lines[i]);
    if (stem) listingIndices.push({ index: i, stem });
  }
  if (listingIndices.length < 2) return { text: manifestText, changed: false };

  // Desired order: the caller's stems first (those present), then any listed
  // stems the caller omitted, in their manifest order.
  const byStem = new Map(listingIndices.map((entry) => [entry.stem, entry]));
  const lower = orderedStems.map((stem) => stem.toLowerCase());
  const desired: Array<{ index: number; stem: string }> = [];
  for (const stem of lower) {
    const entry = byStem.get(stem);
    if (entry && !desired.includes(entry)) desired.push(entry);
  }
  for (const entry of listingIndices) {
    if (!desired.includes(entry)) desired.push(entry);
  }

  const sameOrder = desired.every((entry, position) => listingIndices[position] === entry);
  if (sameOrder) return { text: manifestText, changed: false };

  // Write the lines back: the p-th listing slot receives the p-th desired
  // line (desired is the new order; listingIndices are the original slots).
  const lineByStem = new Map(listingIndices.map((entry) => [entry.stem, lines[entry.index]]));

  const next = [...lines];
  listingIndices.forEach((slot, position) => {
    const source = desired[position];
    next[slot.index] = lineByStem.get(source.stem) as string;
  });
  return { text: next.join('\n'), changed: true };
}

/** The stem a manifest listing line names (`- \`login\` — …`), or null. */
function listingStemOf(line: string): string | null {
  const trimmed = line.trim();
  if (!trimmed.startsWith('- ')) return null;
  const body = trimmed.slice(2).trim();
  const start = body.indexOf('`');
  if (start < 0) return null;
  const rest = body.slice(start + 1);
  const end = rest.indexOf('`');
  if (end < 0) return null;
  const stem = rest.slice(0, end).trim().toLowerCase();
  return stem || null;
}

/** Re-export for hosts that validate a listing before dragging it. */
export { parseManifestStatuses };
