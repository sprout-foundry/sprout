/**
 * README manifest listings (SP-140-1 §1e) — the parse shared by the screen
 * brief and any other manifest reader.
 *
 * Extracted from screenBrief.ts so each file stays under the repo's
 * 500-line rule; the behavior is unchanged.
 */

import { SCREEN_STATUSES } from './screenBrief';

/**
 * The manifest listing's tail split on its dash separators (em dash or spaced
 * ASCII hyphen), mirroring `designApiParse.splitDashSegments` so the status
 * and the purpose read from the same segments.
 */
export function splitListingDashSegments(tail: string): string[] {
  let text = tail.trim();
  if (!text) return [];
  for (const dash of ['\u2014', '-']) {
    if (text.startsWith(dash + ' ')) text = text.slice(dash.length + 1);
    if (text.endsWith(' ' + dash)) text = text.slice(0, text.length - dash.length - 1);
  }
  const normalized = text.split(' \u2014 ').join('\u0000').split(' - ').join('\u0000');
  return normalized
    .split('\u0000')
    .map((segment) => segment.trim())
    .filter((segment) => segment !== '');
}

export interface ManifestListings {
  /** Listing name (lowercased) → status marker ('' when the listing has none). */
  statuses: Record<string, string>;
  /** Listing name (lowercased) → purpose summary (the listing's tail text). */
  summaries: Record<string, string>;
  /** Every lowercased listing name, including ones with no tail text. */
  listed: string[];
}

/**
 * Parse the README manifest's screen listings, mirroring
 * `pkg/design/inventory.go`'s `parseManifestListings`: a bullet of the form
 * ``- `login` — ready — sign-in entry point`` yields status `ready` and
 * purpose `sign-in entry point`. A single-segment tail that is not a known
 * status is the purpose; a multi-segment tail whose first segment is a status
 * splits into status + the remaining segments joined. Names key lowercased.
 */
export function parseManifestListings(text: string): ManifestListings {
  const result: ManifestListings = { statuses: {}, summaries: {}, listed: [] };
  if (!text) return result;
  for (const raw of text.split('\n')) {
    const trimmed = raw.trim();
    if (!trimmed.startsWith('- ')) continue;
    const body = trimmed.slice(2).trim();
    const start = body.indexOf('`');
    if (start < 0) continue;
    const rest = body.slice(start + 1);
    const end = rest.indexOf('`');
    if (end < 0) continue;
    const name = rest.slice(0, end).trim().toLowerCase();
    if (!name) continue;
    result.listed.push(name);
    const segments = splitListingDashSegments(rest.slice(end + 1));
    if (segments.length === 0) continue;
    const isStatus = (segment: string): boolean =>
      (SCREEN_STATUSES as readonly string[]).includes(segment.toLowerCase());
    if (segments.length === 1) {
      if (isStatus(segments[0])) result.statuses[name] = segments[0].toLowerCase();
      else result.summaries[name] = segments[0];
      continue;
    }
    if (isStatus(segments[0])) {
      result.statuses[name] = segments[0].toLowerCase();
      const tail = segments.slice(1).join(' \u2014 ').trim();
      if (tail) result.summaries[name] = tail;
    } else {
      result.summaries[name] = segments.join(' \u2014 ');
    }
  }
  return result;
}
