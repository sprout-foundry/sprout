import { GitCompareArrows, ChevronUp, ChevronDown } from 'lucide-react';
import React, { useState, useMemo } from 'react';
import { parseUnifiedDiffToDocuments } from '../utils/diffParser';
import DiffSurface from './DiffSurface';
import { MergeViewWrapper } from './MergeViewWrapper';

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

const DiffWorkspaceTab = React.memo(function DiffWorkspaceTab({
  path,
  diff,
  diffMode,
  isLoading,
  error,
  onDiffModeChange,
  title = 'Git Diff',
  modeOptions,
}: DiffWorkspaceTabProps): JSX.Element {
  const [viewMode, setViewMode] = useState<'merge' | 'text'>('merge');
  const [collapseUnchanged, setCollapseUnchanged] = useState(true);

  const availableModes =
    modeOptions ||
    (['combined', 'staged', 'unstaged'] as const).filter((mode) => {
      if (mode === 'combined') return true;
      if (mode === 'staged') return !!diff?.has_staged;
      return !!diff?.has_unstaged;
    });

  const diffText = getDiffText(diff, diffMode);

  const docs = useMemo(() => parseUnifiedDiffToDocuments(diffText), [diffText]);

  // Stable reference to avoid recreating MergeView on every render
  const collapseConfig = useMemo(
    () => (collapseUnchanged ? { margin: 4, minSize: 3 } : undefined),
    [collapseUnchanged],
  );

  return (
    <div className="workspace-tab workspace-diff-tab">
      <div className="workspace-tab-header">
        <div>
          <div className="workspace-tab-eyebrow">{title}</div>
          <h2>{path}</h2>
        </div>
        <div className="workspace-diff-controls">
          {availableModes.length > 1 && (
            <div className="workspace-diff-mode-tabs">
              {availableModes.map((mode) => (
                <button
                  key={mode}
                  className={`workspace-diff-mode-tab ${diffMode === mode ? 'active' : ''}`}
                  onClick={() => onDiffModeChange(mode)}
                >
                  {mode.charAt(0).toUpperCase() + mode.slice(1)}
                </button>
              ))}
            </div>
          )}
          <div className="workspace-diff-view-toggle">
            <button
              className={`workspace-diff-view-btn ${viewMode === 'merge' ? 'active' : ''}`}
              onClick={() => setViewMode('merge')}
            >
              Merge
            </button>
            <button
              className={`workspace-diff-view-btn ${viewMode === 'text' ? 'active' : ''}`}
              onClick={() => setViewMode('text')}
            >
              Text
            </button>
          </div>
        </div>
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
        viewMode === 'merge' && (docs.original !== '' || docs.modified !== '') ? (
          <div className="workspace-diff-merge-wrapper">
            {/* Collapse unchanged toggle */}
            <div className="workspace-diff-collapse-toggle">
              <button
                className={`workspace-diff-collapse-btn ${collapseUnchanged ? 'active' : ''}`}
                onClick={() => setCollapseUnchanged(!collapseUnchanged)}
                title={collapseUnchanged ? 'Expand unchanged regions' : 'Collapse unchanged regions'}
                aria-pressed={collapseUnchanged}
              >
                {collapseUnchanged ? <ChevronDown size={14} /> : <ChevronUp size={14} />}
                <span>{collapseUnchanged ? 'Collapse Unchanged' : 'Show All'}</span>
              </button>
            </div>
            <MergeViewWrapper
              originalContent={docs.original}
              modifiedContent={docs.modified}
              mode="side-by-side"
              fileName={path}
              aLabel="Before"
              bLabel="After"
              collapseUnchanged={collapseConfig}
            />
          </div>
        ) : (
          <DiffSurface diffText={diffText} />
        )
      ) : (
        <div className="workspace-tab-empty">
          <p>(no diff available)</p>
        </div>
      )}
    </div>
  );
});

export default DiffWorkspaceTab;
