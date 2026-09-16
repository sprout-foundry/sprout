import { useMemo } from 'react';
import { classifyDiffLine } from '../utils/format';
import './DiffView.css';

interface DiffViewProps {
  /** Raw unified diff text (the /api/changes/diff `diff` field). */
  diff: string;
}

interface DiffLine {
  kind: 'add' | 'del' | 'hunk' | 'meta' | 'context';
  text: string;
}

// classifyDiffLine (utils/format) names the header class 'file'; the
// view's CSS calls it 'meta'. One-line bridge.
const KIND_MAP: Record<string, DiffLine['kind']> = {
  file: 'meta',
  hunk: 'hunk',
  add: 'add',
  del: 'del',
  context: 'context',
};

/**
 * DiffView — read-only renderer for a unified diff with add/remove/hunk
 * coloring. Line-based, no tokenizer, no dependencies: the ChangeTracker
 * diffs are plain difflib unified output.
 */
export function DiffView({ diff }: DiffViewProps): JSX.Element {
  const lines = useMemo(
    () =>
      diff
        .split('\n')
        // difflib output ends with a trailing newline → a final empty
        // split artifact; drop it so the view doesn't end on a gap.
        .filter((l, i, arr) => !(i === arr.length - 1 && l === ''))
        .map((text) => ({ kind: KIND_MAP[classifyDiffLine(text)] ?? 'context', text })),
    [diff],
  );

  return (
    <div className="diff-view" data-testid="diff-view">
      {lines.map((line, i) => (
        // Lines are positional content of an immutable diff string —
        // index is the correct key here.
        // eslint-disable-next-line react/no-array-index-key
        <div key={i} className={`diff-line diff-line--${line.kind}`}>
          <span className="diff-line-gutter" aria-hidden="true">
            {line.kind === 'add' ? '+' : line.kind === 'del' ? '−' : line.kind === 'hunk' ? '@' : ''}
          </span>
          <span className="diff-line-text">{line.text}</span>
        </div>
      ))}
    </div>
  );
}

export default DiffView;
