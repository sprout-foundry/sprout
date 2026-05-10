import type { ReactNode } from 'react';

/**
 * Highlight a match region inside a line of text by wrapping it
 * in a <span className="search-match-highlight">.
 */
export function highlightMatch(line: string, colStart: number, colEnd: number): ReactNode {
  if (colStart <= 0 || colEnd <= colStart || colEnd > line.length) {
    return line;
  }

  const before = line.substring(0, colStart - 1);
  const match = line.substring(colStart - 1, colEnd);
  const after = line.substring(colEnd);

  return (
    <>
      {before}
      <span className="search-match-highlight">{match}</span>
      {after}
    </>
  );
}
