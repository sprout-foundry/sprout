import { useCallback, useState, useRef } from 'react';
import { GitBranch, RefreshCw } from 'lucide-react';
import { GitFileSection } from './GitFileSection';
import './GitPanel.css';

export interface GitStatus {
  modified: string[];
  untracked: string[];
  staged: string[];
  deleted: string[];
  renamed: Array<{ from: string; to: string }>;
}

export interface GitPanelProps {
  branch?: string;
  status?: GitStatus;
  stagedFiles?: string[];
  onStageFile?: (path: string) => void;
  onUnstageFile?: (path: string) => void;
  onStageAll?: () => void;
  onUnstageAll?: () => void;
  onCommit?: (message: string) => void;
  onRefresh?: () => void;
  commitMessage?: string;
  onCommitMessageChange?: (msg: string) => void;
  isLoading?: boolean;
  className?: string;
}

/**
 * A git status and staging panel.
 *
 * Displays branch info, file lists grouped by status (staged, modified,
 * untracked, deleted, renamed), stage/unstage toggles, and commit message input.
 */
function GitPanel({
  branch,
  status = { modified: [], untracked: [], staged: [], deleted: [], renamed: [] },
  stagedFiles,
  onStageFile,
  onUnstageFile,
  onStageAll,
  onUnstageAll,
  onCommit,
  onRefresh,
  commitMessage = '',
  onCommitMessageChange,
  isLoading = false,
  className,
}: GitPanelProps): JSX.Element {
  const [showCommitBox, setShowCommitBox] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const isStaged = useCallback(
    (path: string) => {
      return stagedFiles?.includes(path) || status.staged.includes(path);
    },
    [stagedFiles, status.staged],
  );

  const handleFileClick = useCallback(
    (path: string) => {
      if (isStaged(path)) {
        onUnstageFile?.(path);
      } else {
        onStageFile?.(path);
      }
    },
    [isStaged, onStageFile, onUnstageFile],
  );

  const handleCommit = useCallback(() => {
    if (commitMessage.trim()) {
      onCommit?.(commitMessage);
      setShowCommitBox(false);
    }
  }, [commitMessage, onCommit]);

  const handleCommitKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
        handleCommit();
      }
    },
    [handleCommit],
  );

  const toggleCommitBox = useCallback(() => {
    setShowCommitBox((prev) => {
      const newState = !prev;
      if (newState) {
        requestAnimationFrame(() => textareaRef.current?.focus());
      }
      return newState;
    });
  }, []);

  const totalChanges = status.modified.length + status.untracked.length + status.deleted.length + status.renamed.length;
  const stagedCount = status.staged.length + (stagedFiles?.length || 0);

  return (
    <div className={`gitpanel ${className || ''}`}>
      {/* Header with branch and actions */}
      <div className="gitpanel-header">
        <div className="gitpanel-branch">
          <GitBranch size={14} className="gitpanel-branch-icon" />
          <span className="gitpanel-branch-name">{branch || 'No Git'}</span>
        </div>
        <button
          type="button"
          className={`gitpanel-refresh ${isLoading ? 'gitpanel-refresh-spinning' : ''}`}
          onClick={onRefresh}
          disabled={isLoading}
          aria-label="Refresh git status"
          title="Refresh"
        >
          <RefreshCw size={14} />
        </button>
      </div>

      {/* Changes summary */}
      {totalChanges > 0 && (
        <div className="gitpanel-summary">
          <span className="gitpanel-summary-text">
            {stagedCount > 0 && `${stagedCount} staged`} {totalChanges > 0 && stagedCount > 0 && '·'} {totalChanges} change{totalChanges !== 1 ? 's' : ''}
          </span>
        </div>
      )}

      {/* Stage/Unstage All buttons */}
      {(status.modified.length > 0 || status.untracked.length > 0 || status.deleted.length > 0) && (
        <div className="gitpanel-actions">
          <button type="button" className="gitpanel-action-button" onClick={onStageAll} disabled={isLoading}>
            Stage All
          </button>
          {stagedCount > 0 && (
            <button type="button" className="gitpanel-action-button" onClick={onUnstageAll} disabled={isLoading}>
              Unstage All
            </button>
          )}
        </div>
      )}

      {/* File lists grouped by status */}
      <div className="gitpanel-files">
        <GitFileSection type="staged" title={`Staged Changes (${status.staged.length})`} files={status.staged} isStaged={isStaged} onFileClick={handleFileClick} />
        <GitFileSection type="modified" title={`Modified (${status.modified.length})`} files={status.modified} isStaged={isStaged} onFileClick={handleFileClick} />
        <GitFileSection type="untracked" title={`Untracked (${status.untracked.length})`} files={status.untracked} isStaged={isStaged} onFileClick={handleFileClick} />
        <GitFileSection type="deleted" title={`Deleted (${status.deleted.length})`} files={status.deleted} isStaged={isStaged} onFileClick={handleFileClick} />
        <GitFileSection type="renamed" title={`Renamed (${status.renamed.length})`} files={status.renamed.map((item) => item.to)} renamedFiles={status.renamed} isStaged={isStaged} onFileClick={handleFileClick} />

        {/* No changes */}
        {totalChanges === 0 && stagedCount === 0 && (
          <div className="gitpanel-empty">
            <p className="gitpanel-empty-text">No changes detected</p>
          </div>
        )}
      </div>

      {/* Commit message area */}
      {(stagedCount > 0 || showCommitBox) && (
        <div className="gitpanel-commit">
          {!showCommitBox ? (
            <button type="button" className="gitpanel-commit-toggle" onClick={toggleCommitBox}>
              Write a commit message...
            </button>
          ) : (
            <>
              <textarea
                ref={textareaRef}
                className="gitpanel-commit-input"
                value={commitMessage}
                onChange={(e) => onCommitMessageChange?.(e.target.value)}
                onKeyDown={handleCommitKeyDown}
                placeholder="Commit message"
                rows={3}
              />
              <div className="gitpanel-commit-actions">
                <button type="button" className="gitpanel-commit-cancel" onClick={() => setShowCommitBox(false)}>
                  Cancel
                </button>
                <button type="button" className="gitpanel-commit-button" onClick={handleCommit} disabled={!commitMessage.trim() || isLoading}>
                  Commit
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}

export default GitPanel;
