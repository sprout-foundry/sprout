import { useMemo, useState } from 'react';
import { classifyDiffLine } from '../utils/format';
import './DiffView.css';

interface DiffViewProps {
  /** Raw unified diff text (the /api/changes/diff `diff` field). */
  diff: string;
  /** Lines rendered before the "show all" expander appears.
   *  Rendering tens of thousands of diff rows locks the tab; the
   *  backend caps diffs at 4k lines, and 1500 is well inside smooth
   *  DOM territory. Expand is explicit and one-way per mount. */
  initialLines?: number;
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

const DEFAULT_INITIAL_LINES = 1500;

/**
 * DiffView — read-only renderer for a unified diff with add/remove/hunk
 * coloring. Line-based, no tokenizer, no dependencies: the ChangeTracker
 * diffs are plain difflib unified output.
 */
export function DiffView({ diff, initialLines = DEFAULT_INITIAL_LINES }: DiffViewProps): JSX.Element {
  const [expanded, setExpanded] = useState(false);

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

  const visible = expanded ? lines : lines.slice(0, initialLines);
  const hidden = lines.length - visible.length;

  return (
    <div className="diff-view" data-testid="diff-view">
      {visible.map((line, i) => (
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
      {hidden > 0 && (
        <button type="button" className="diff-view-expand" onClick={() => setExpanded(true)}>
          Show {hidden.toLocaleString()} more line{hidden === 1 ? '' : 's'} ({lines.length.toLocaleString()} total)
        </button>
      )}
    </div>
  );
}

export default DiffView;
