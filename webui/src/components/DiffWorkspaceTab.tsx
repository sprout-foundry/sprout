import { GitCompareArrows } from 'lucide-react';
import DiffSurface from './DiffSurface';

interface GitDiffResponse {
  message: string;
  path: string;
  has_staged: boolean;
  has_unstaged: boolean;
  staged_diff: string;
  unstaged_diff: string;
  diff: string;
}

interface DiffWorkspaceTabProps {
  path: string;
  diff: GitDiffResponse | null;
  diffMode: 'combined' | 'staged' | 'unstaged';
  isLoading: boolean;
  error: string | null;
  onDiffModeChange: (mode: 'combined' | 'staged' | 'unstaged') => void;
  title?: string;
  modeOptions?: Array<'combined' | 'staged' | 'unstaged'>;
}

const getDiffText = (diff: GitDiffResponse | null, diffMode: 'combined' | 'staged' | 'unstaged'): string => {
  if (!diff) return '';
  switch (diffMode) {
    case 'staged':
      return diff.staged_diff || '(no staged changes)';
    case 'unstaged':
      return diff.unstaged_diff || '(no unstaged changes)';
    default:
      return diff.diff || '(no diff available)';
  }
};

function DiffWorkspaceTab({
  path,
  diff,
  diffMode,
  isLoading,
  error,
  onDiffModeChange,
  title = 'Git Diff',
  modeOptions,
}: DiffWorkspaceTabProps): JSX.Element {
  const availableModes =
    modeOptions ||
    (['combined', 'staged', 'unstaged'] as const).filter((mode) => {
      if (mode === 'combined') return true;
      if (mode === 'staged') return !!diff?.has_staged;
      return !!diff?.has_unstaged;
    });

  const diffText = getDiffText(diff, diffMode);

  return (
    <div className="workspace-tab workspace-diff-tab">
      <div className="workspace-tab-header">
        <div>
          <div className="workspace-tab-eyebrow">{title}</div>
          <h2>{path}</h2>
        </div>
        {availableModes.length > 1 ? (
          <div className="workspace-diff-mode-tabs">
            {availableModes.map((mode) => (
              <button
                key={mode}
                className={`workspace-diff-mode-tab ${diffMode === mode ? 'active' : ''}`}
                onClick={() => onDiffModeChange(mode)}
              >
                {mode}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      {isLoading ? (
        <div className="workspace-tab-empty">
          <GitCompareArrows size={28} />
          <p>Loading diff…</p>
        </div>
      ) : error ? (
        <div className="workspace-tab-empty workspace-tab-error">
          <GitCompareArrows size={28} />
          <p>{error}</p>
        </div>
      ) : diffText ? (
        <DiffSurface diffText={diffText} />
      ) : (
        <div className="workspace-tab-empty">
          <p>(no diff available)</p>
        </div>
      )}
    </div>
  );
}

export default DiffWorkspaceTab;
