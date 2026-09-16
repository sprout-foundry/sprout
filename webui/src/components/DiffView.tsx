import { useMemo } from 'react';
import './DiffView.css';

interface DiffViewProps {
  /** Raw unified diff text (the /api/changes/diff `diff` field). */
  diff: string;
}

interface DiffLine {
  kind: 'add' | 'del' | 'hunk' | 'meta' | 'context';
  text: string;
}

/**
 * Classify one line of a unified diff. Header lines ("--- a/x",
 * "+++ b/x") and the difflib "(no textual difference)" placeholder get
 * their own muted treatment; everything else with a recognized prefix
 * maps to add/del/hunk/context.
 */
function classifyLine(line: string): DiffLine {
  // Meta headers first: "--- a/x" starts with "-" and "+++ b/x" starts
  // with "+", so the file-header check must precede the prefix checks.
  if (line.startsWith('---') || line.startsWith('+++') || line.startsWith('index ') || line.startsWith('diff ')) {
    return { kind: 'meta', text: line };
  }
  if (line.startsWith('@@')) return { kind: 'hunk', text: line };
  if (line.startsWith('+')) return { kind: 'add', text: line };
  if (line.startsWith('-')) return { kind: 'del', text: line };
  return { kind: 'context', text: line };
}

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
        .map(classifyLine),
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
