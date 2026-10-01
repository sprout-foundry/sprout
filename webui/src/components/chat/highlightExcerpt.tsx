import type React from 'react';

/**
 * Session search marks each matched term as "[term]" (the CLI's rendering).
 * Show those as highlights; brackets around anything that isn't a searched
 * term are the conversation's own text and stay as written.
 */
export function highlightExcerpt(excerpt: string, query: string): React.ReactNode {
  const terms = new Set(query.toLowerCase().split(/\s+/).filter(Boolean));
  if (terms.size === 0) return excerpt;
  const parts = excerpt.split(/(\[[^\]]+\])/);
  return parts.map((part, i) => {
    const inner = part.startsWith('[') && part.endsWith(']') ? part.slice(1, -1) : null;
    return inner !== null && terms.has(inner.toLowerCase()) ? (
      <mark key={i} className="chs-row-match">
        {inner}
      </mark>
    ) : (
      part
    );
  });
}
