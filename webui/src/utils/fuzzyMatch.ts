/**
 * Fuzzy matcher for command palette.
 *
 * Provides both substring matching (with scoring bonuses) and true fuzzy
 * character‑sequence matching so users can type partial abbreviations like
 * "gts" to match "Go to File...".
 */

/** Score ≥ 0 means a match.  Higher is better. */
export interface FuzzyResult<T> {
  item: T;
  score: number;
  /** Array of [start, end) ranges inside `label` that matched. */
  matches: Array<[number, number]>;
}

/**
 * Fuzzy‑match `query` against `label`.
 *
 * Two strategies are tried and the better score is kept:
 *   1. **Substring** – does the lower‑cased query appear anywhere?  Bonus
 *      points for prefix / word‑boundary matches.
 *   2. **Character sequence** – can each query character be found in order
 *      inside the label?  Bonus points for consecutive / boundary hits.
 *
 * Returns `{ score, matches }` where `score < 0` means no match.
 */
export function fuzzyScore(query: string, label: string): {
  score: number;
  matches: Array<[number, number]>;
} {
  if (!query) return { score: 0, matches: [] };

  const q = query.toLowerCase();
  const l = label.toLowerCase();

  // --- Substring match ---------------------------------------------------
  const subIdx = l.indexOf(q);
  if (subIdx !== -1) {
    let score = 100;
    const matches: Array<[number, number]> = [[subIdx, subIdx + q.length]];

    // Prefix bonus
    if (subIdx === 0) score += 200;
    // Word-boundary bonus (character before is non-alnum or slash)
    else if (subIdx > 0 && /[^a-z0-9/]/.test(l[subIdx - 1])) score += 120;
    // Contiguous word bonus
    if (q.includes(' ')) score += 40;

    return { score, matches };
  }

  // --- Fuzzy character sequence match ------------------------------------
  const matches: Array<[number, number]> = [];
  let qi = 0; // query index
  let consecutive = 0;
  let score = 0;

  for (let li = 0; li < l.length && qi < q.length; li++) {
    if (l[li] === q[qi]) {
      const start = li;
      // check if it's a word boundary
      const isBoundary = li === 0 || /[^a-z0-9/]/.test(l[li - 1]);

      if (isBoundary) {
        score += 30;
        consecutive = 1;
      } else if (consecutive > 0) {
        score += 15;
        consecutive++;
      } else {
        score += 5;
        consecutive = 1;
      }

      matches.push([start, start + 1]);
      qi++;
    } else {
      consecutive = 0;
    }
  }

  if (qi < q.length) return { score: -1, matches: [] }; // not all chars matched

  return { score, matches };
}

/**
 * Fuzzy‑search a list of items, returning the best matches sorted by score.
 *
 * @param query - The user's search string.
 * @param items - Array of items to search.
 * @param getLabel - Extract the searchable string from an item.
 * @param limit - Max results to return.
 */
export function fuzzyFilter<T>(
  query: string,
  items: T[],
  getLabel: (item: T) => string,
  limit = 50,
): FuzzyResult<T>[] {
  if (!query) return [];

  const results: FuzzyResult<T>[] = [];

  for (const item of items) {
    const label = getLabel(item);
    const { score, matches } = fuzzyScore(query, label);
    if (score >= 0) {
      results.push({ item, score, matches });
    }
  }

  results.sort((a, b) => {
    if (b.score !== a.score) return b.score - a.score;
    const la = getLabel(a.item).toLowerCase();
    const lb = getLabel(b.item).toLowerCase();
    return la.localeCompare(lb);
  });

  return results.slice(0, limit);
}

/**
 * Highlight the matched portions of a label string by wrapping them in
 * `<mark>` tags (for use with `dangerouslySetInnerHTML`).
 */
export function highlightMatches(label: string, matches: Array<[number, number]>): string {
  if (!matches.length) return escapeHtml(label);

  // Merge overlapping / adjacent ranges
  const sorted = [...matches].sort((a, b) => a[0] - b[0]);
  const merged: Array<[number, number]> = [sorted[0]];
  for (let i = 1; i < sorted.length; i++) {
    const last = merged[merged.length - 1];
    if (sorted[i][0] <= last[1]) {
      merged[merged.length - 1] = [last[0], Math.max(last[1], sorted[i][1])];
    } else {
      merged.push(sorted[i]);
    }
  }

  let html = '';
  let cursor = 0;
  for (const [start, end] of merged) {
    if (cursor < start) html += escapeHtml(label.slice(cursor, start));
    html += `<mark>${escapeHtml(label.slice(start, end))}</mark>`;
    cursor = end;
  }
  if (cursor < label.length) html += escapeHtml(label.slice(cursor));
  return html;
}

function escapeHtml(str: string): string {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
