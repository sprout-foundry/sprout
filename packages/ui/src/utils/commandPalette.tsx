import type { ReactNode } from 'react';

export interface FuzzyMatchResult {
  score: number;
  indices: number[];
}

/**
 * Simple fuzzy match utility for command search.
 * Returns a score (0-1) and match indices array.
 */
export function fuzzyMatch(query: string, text: string): FuzzyMatchResult {
  if (!query) return { score: 1, indices: [] };

  const q = query.toLowerCase();
  const t = text.toLowerCase();
  const indices: number[] = [];
  let qIdx = 0;
  let score = 0;

  for (let i = 0; i < t.length && qIdx < q.length; i++) {
    if (t[i] === q[qIdx]) {
      indices.push(i);
      score += 1;
      qIdx++;
    }
  }

  // Calculate score based on match ratio and consecutive matches
  if (qIdx < q.length) {
    return { score: 0, indices: [] }; // Not all query chars matched
  }

  // Bonus for consecutive matches
  let consecutive = 0;
  for (let i = 1; i < indices.length; i++) {
    if (indices[i] === indices[i - 1] + 1) {
      consecutive++;
    }
  }
  score += consecutive * 0.5;

  // Normalize by text length
  score = score / (t.length + indices.length);

  return { score, indices };
}

/**
 * Highlight matched text in the search query.
 */
export function highlightMatch(text: string, indices: number[]): ReactNode {
  if (indices.length === 0) return text;

  const result: ReactNode[] = [];
  let lastIndex = 0;

  indices.forEach((idx, i) => {
    if (idx > lastIndex) {
      result.push(text.substring(lastIndex, idx));
    }
    result.push(<strong key={i}>{text[idx]}</strong>);
    lastIndex = idx + 1;
  });

  if (lastIndex < text.length) {
    result.push(text.substring(lastIndex));
  }

  return <>{result}</>;
}
