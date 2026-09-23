/**
 * Manifest status curation model (SP-140-7 §7d).
 *
 * Pure functions for the screen status menu: a structured README rewrite that
 * touches ONLY the chosen listing's status segment, leaving every other byte
 * of the manifest alone. The manifest is human-edited documentation — the
 * rule is "never clobber": an unparsable listing is reported, never rewritten
 * through a lossy regeneration.
 */

import { parseManifestStatuses } from '../services/api/designApiParse';

export const SCREEN_STATUSES = ['draft', 'review', 'ready'] as const;
export type ScreenStatus = (typeof SCREEN_STATUSES)[number];

/** True when the value is one of the §1e status markers. */
export function isScreenStatus(value: string): value is ScreenStatus {
  return (SCREEN_STATUSES as readonly string[]).includes(value);
}

/**
 * The listing's current status from the manifest text (lowercased marker or
 * '' when the listing carries none).
 */
export function currentStatusFor(manifestText: string, stem: string): string {
  return parseManifestStatuses(manifestText)[stem.toLowerCase()] ?? '';
}

/**
 * Set the status segment of ONE listing bullet for `stem` inside the manifest
 * text. Returns the new text plus whether the rewrite was structural (true)
 * or refused (false) because no parsable `- \`stem\` …` bullet exists —
 * the caller then falls back to opening the README instead of writing.
 *
 * The rewrite preserves the line's other segments (summary, links) verbatim:
 * it only replaces/inserts the first dash segment after the name when needed.
 */
export function setStatusInManifest(
  manifestText: string,
  stem: string,
  status: ScreenStatus | '',
): { text: string; changed: boolean } {
  const lines = manifestText.split('\n');
  const target = stem.toLowerCase();
  let found = false;

  const next = lines.map((line) => {
    if (found || !parsesAsListingFor(line, target)) return line;
    found = true;
    return rewriteLineStatus(line, status);
  });

  if (!found) return { text: manifestText, changed: false };
  return { text: next.join('\n'), changed: true };
}

/** Does this line look like a parsable `- \`stem\` …` listing bullet? */
function parsesAsListingFor(line: string, targetLower: string): boolean {
  const trimmed = line.trim();
  if (!trimmed.startsWith('- ')) return false;
  const body = trimmed.slice(2).trim();
  const start = body.indexOf('`');
  if (start < 0) return false;
  const rest = body.slice(start + 1);
  const end = rest.indexOf('`');
  if (end < 0) return false;
  return rest.slice(0, end).trim().toLowerCase() === targetLower;
}

/** Rewrite one listing line's status segment (the parser's vocabulary). */
function rewriteLineStatus(line: string, status: ScreenStatus | ''): string {
  const trimmedStart = line.length - line.trimStart().length;
  const indent = line.slice(0, trimmedStart);
  const trimmed = line.trim();
  const bullet = trimmed.slice(0, 2);
  const body = trimmed.slice(2).trim();

  // The name spans from its opening to its closing backtick (both kept).
  const nameStart = body.indexOf('`');
  const nameEnd = body.indexOf('`', nameStart + 1);
  const name = body.slice(nameStart, nameEnd + 1);
  const tail = body.slice(nameEnd + 1);

  // Split the tail on the dash separators the parser accepts, keeping the
  // segments so summary text is preserved verbatim.
  const segments = splitSegments(tail);
  if (segments.length > 0 && isScreenStatus(segments[0].toLowerCase())) {
    segments[0] = status;
  } else if (status) {
    segments.unshift(status);
  }
  const joined = segments.filter((segment) => segment !== '').join(' — ');
  return `${indent}${bullet.trimEnd()} ${name}${joined ? ` — ${joined}` : ''}`.trimEnd();
}

/** Split a listing tail ("— draft — summary") into its dash segments. */
function splitSegments(text: string): string[] {
  // The tail always begins with the first separator; drop it, then split on
  // the spaced dash forms (em dash and ASCII hyphen) the parser accepts.
  const withoutLeading = text.trim().replace(/^[—-]\s*/, '');
  if (!withoutLeading) return [];
  return withoutLeading
    .split(/\s+[—-]\s+/)
    .map((segment) => segment.trim())
    .filter(Boolean);
}
