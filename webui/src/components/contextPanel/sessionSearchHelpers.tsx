import React from 'react';

/** Format a relative time string for session search results */
export function formatSessionDate(isoString: string): string {
  const date = new Date(isoString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);
  const diffMonth = Math.floor(diffDay / 30);
  const diffYear = Math.floor(diffDay / 365);

  if (diffSec < 60) return 'just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  if (diffDay < 30) return `${diffDay}d ago`;
  if (diffMonth < 12) return `${diffMonth}mo ago`;
  return `${diffYear}y ago`;
}

/** Render an excerpt string, converting [matched] terms to highlighted spans */
export function renderSessionExcerpt(excerpt: string): React.ReactNode {
  const parts = excerpt.split(/(\[.+?\])/g);
  return parts.map((part, i) => {
    if (part.startsWith('[') && part.endsWith(']')) {
      const text = part.slice(1, -1);
      return (
        <span key={i} className="sidebar-session-search-match">
          {text}
        </span>
      );
    }
    return <span key={i}>{part}</span>;
  });
}
