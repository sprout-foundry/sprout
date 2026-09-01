import { GitCompareArrows, ChevronUp, ChevronDown } from 'lucide-react';
import React, { useCallback, useEffect, useState, useMemo } from 'react';
import { writeFileWithConsent } from '../services/fileAccess';
import { parseUnifiedDiffToDocuments } from '../utils/diffParser';
import { useLog } from '../utils/log';
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
  // User-made merge-state edits (chunk reverts / typing in pane B). Lifted
  // from the CodeMirror instance so they survive view-mode toggles and
  // re-renders; null means "no edits yet, use the parsed diff content".
  const [editedModified, setEditedModified] = useState<string | null>(null);
  const log = useLog();

  const availableModes =
    modeOptions ||
    (['combined', 'staged', 'unstaged'] as const).filter((mode) => {
      if (mode === 'combined') return true;
      if (mode === 'staged') return !!diff?.has_staged;
      return !!diff?.has_unstaged;
    });

  const diffText = getDiffText(diff, diffMode);

  const docs = useMemo(() => parseUnifiedDiffToDocuments(diffText), [diffText]);

  // When the underlying diff changes (mode switch, git refresh), drop any
  // in-progress merge edits so the view reflects the fresh content.
  useEffect(() => {
    setEditedModified(null);
  }, [diffText]);

  // Report pane-B changes (reverts / typing) up into local state.
  const handleModifiedChange = useCallback((content: string) => {
    setEditedModified(content);
  }, []);

  // Cmd+S in the merge view writes the reverted pane-B content back to disk.
  const handleSave = useCallback(
    async (content: string) => {
      if (!path) return;
      try {
        // Set the save cooldown before the HTTP write so the server-side
        // fsnotify echo is suppressed (same pattern as the editor save).
        document.dispatchEvent(
          new CustomEvent('file:editor-saved', {
            detail: { path, mtime: Math.floor(Date.now() / 1000) },
          }),
        );
        const response = await writeFileWithConsent(path, content);
        if (!response.ok) {
          const errorText = await response.text().catch(() => response.statusText);
          throw new Error(errorText || `Failed to save file: ${response.statusText}`);
        }
        log.success(`${path} saved successfully`, { title: 'File Saved', duration: 3000 });
      } catch (error) {
        const msg = error instanceof Error ? error.message : 'Failed to save file';
        log.error(msg, { title: 'Save Error' });
      }
    },
    [path, log],
  );

  // Stable reference to avoid recreating MergeView on every render
  const collapseConfig = useMemo(
    () => (collapseUnchanged ? { margin: 4, minSize: 3 } : undefined),
    [collapseUnchanged],
  );

  const modifiedContent = editedModified ?? docs.modified;

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
              modifiedContent={modifiedContent}
              mode="side-by-side"
              fileName={path}
              aLabel="Before"
              bLabel="After"
              collapseUnchanged={collapseConfig}
              onModifiedChange={handleModifiedChange}
              onSave={handleSave}
            />
          </div>
        ) : (
          <DiffSurface diffText={diffText} title={title} path={diff?.path || path} />
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
