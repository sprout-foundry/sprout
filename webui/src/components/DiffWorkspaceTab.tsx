import { Check, GitCompareArrows, ChevronUp, ChevronDown, RotateCcw, Save } from 'lucide-react';
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
  /** Full pre-image of the file (undefined when unavailable). Empty string is valid (new file). */
  fullOriginal?: string;
  /** Full post-image of the file (undefined when unavailable). Empty string is valid (deleted file). */
  fullModified?: string;
  /**
   * Whether pane-B edits may be saved back to disk. Only true when the
   * caller has verified full-file contents AND the buffer's path is a real
   * filesystem path (not a commit:/revision: virtual path). Saving a
   * fragment reconstruction would destroy the rest of the file.
   */
  canSave?: boolean;
  /** Initial view mode ('text' unless the opener opts into the merge view). */
  defaultView?: 'merge' | 'text';
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
  fullOriginal,
  fullModified,
  canSave = false,
  defaultView = 'text',
}: DiffWorkspaceTabProps): JSX.Element {
  const [viewMode, setViewMode] = useState<'merge' | 'text'>(defaultView);
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

  // Merge-view documents. Full contents are authoritative when present —
  // the fragment reconstruction (context lines glued together) is only a
  // fallback and is never editable (canSave requires full contents).
  // diffText only matters in the fragment path, so it's excluded from the
  // deps when full contents are present (avoids re-parsing on mode switch).
  const hasFullContents = fullOriginal !== undefined && fullModified !== undefined;
  const docs = useMemo(
    () =>
      hasFullContents ? { original: fullOriginal, modified: fullModified } : parseUnifiedDiffToDocuments(diffText),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    hasFullContents ? [fullOriginal, fullModified] : [diffText],
  );

  // When the underlying documents change (mode switch, git refresh, contents
  // arriving), drop any in-progress merge edits so the view reflects the
  // fresh content.
  useEffect(() => {
    setEditedModified(null);
  }, [docs.original, docs.modified]);

  // Report pane-B changes (reverts / typing) up into local state.
  const handleModifiedChange = useCallback((content: string) => {
    setEditedModified(content);
  }, []);

  // Cmd+S in the merge view writes pane-B content back to disk. Only wired
  // when canSave — i.e. the view holds the full file and the path is real.
  const handleSave = useCallback(
    async (content: string) => {
      if (!path) return;
      setIsSaving(true);
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
        // Edits are now on disk; the next git refresh will produce a diff
        // matching the saved content, so drop the local edit state.
        setEditedModified(null);
      } catch (error) {
        const msg = error instanceof Error ? error.message : 'Failed to save file';
        log.error(msg, { title: 'Save Error' });
      } finally {
        setIsSaving(false);
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
  const mergeEditable = canSave && hasFullContents;
  // True when pane B holds edits the user made (reverted chunks or typed
  // text) that haven't been saved — drives the in-flow confirm bar.
  const hasUnsavedEdits = editedModified !== null && editedModified !== docs.modified;
  const [isSaving, setIsSaving] = useState(false);

  const handleDiscardEdits = useCallback(() => {
    setEditedModified(null);
  }, []);

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
            {/* In-flow confirm bar: appears only when pane B holds unsaved
                edits (reverted chunks or typing). Accept/Reject belong on the
                LLM edit-approval surface, not here — this bar is the only
                decision a git-diff review needs: keep the edited result on
                disk, or throw the edits away. */}
            {hasUnsavedEdits && mergeEditable && (
              <div className="workspace-diff-edit-bar" role="status">
                <span className="workspace-diff-edit-bar-label">
                  <Check size={13} /> Unsaved edits in this diff
                </span>
                <div className="workspace-diff-edit-bar-actions">
                  <button
                    type="button"
                    className="workspace-diff-edit-bar-btn discard"
                    onClick={handleDiscardEdits}
                    disabled={isSaving}
                    title="Discard your edits and restore the diff view"
                  >
                    <RotateCcw size={13} /> Discard
                  </button>
                  <button
                    type="button"
                    className="workspace-diff-edit-bar-btn save"
                    onClick={() => void handleSave(modifiedContent)}
                    disabled={isSaving}
                    title="Write the edited file to disk (Cmd+S)"
                  >
                    <Save size={13} /> {isSaving ? 'Saving…' : 'Save to disk'}
                  </button>
                </div>
              </div>
            )}
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
              onSave={mergeEditable ? handleSave : undefined}
              readOnly={!mergeEditable}
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
