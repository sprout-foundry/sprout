import React, { useState, useEffect, useRef } from 'react';
import { GitBranch, X, Loader2, FolderTree } from 'lucide-react';
import { listWorktrees } from '../services/chatSessions';
import type { WorktreeInfo } from '../services/chatSessions';
import './WorktreePickerDialog.css';

export interface WorktreePickerDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onSelect: (worktreePath: string, branch: string) => void;
  disabledPaths?: string[];
}

export const WorktreePickerDialog: React.FC<WorktreePickerDialogProps> = ({
  isOpen,
  onClose,
  onSelect,
  disabledPaths = [],
}) => {
  const [worktrees, setWorktrees] = useState<WorktreeInfo[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const listRef = useRef<HTMLUListElement>(null);

  // Fetch worktrees when the dialog opens
  useEffect(() => {
    if (isOpen) {
      setError(null);
      setWorktrees([]);
      setIsLoading(true);
      listWorktrees()
        .then((resp) => setWorktrees(resp.worktrees))
        .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load worktrees'))
        .finally(() => setIsLoading(false));
    }
  }, [isOpen]);

  // Overlay click to close
  const handleOverlayClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };

  // Escape key to close
  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  const handleSelect = (wt: WorktreeInfo) => {
    if (disabledPaths.includes(wt.path)) return;
    onSelect(wt.path, wt.branch);
  };

  // Only show non-main worktrees
  const selectableWorktrees = worktrees.filter((wt) => !wt.is_main);

  if (!isOpen) return null;

  return (
    <div
      className="wt-picker-overlay"
      onClick={handleOverlayClick}
      role="dialog"
      aria-modal="true"
      aria-labelledby="wt-picker-title"
    >
      <div className="wt-picker-card">
        {/* Header */}
        <div className="wt-picker-header">
          <h2 id="wt-picker-title">
            <GitBranch className="wt-picker-icon" size={18} />
            Select Worktree
          </h2>
          <button
            className="wt-picker-close"
            onClick={onClose}
            aria-label="Close dialog"
          >
            <X size={18} />
          </button>
        </div>

        {/* Content */}
        <div className="wt-picker-content">
          {error && (
            <div className="wt-picker-error">{error}</div>
          )}

          {isLoading ? (
            <div className="wt-picker-loading">
              <Loader2 size={16} className="wt-picker-spinner" />
              <span>Loading worktrees…</span>
            </div>
          ) : selectableWorktrees.length === 0 ? (
            <div className="wt-picker-empty">
              <FolderTree size={20} />
              <span>No worktrees available. Create one from the worktree button.</span>
            </div>
          ) : (
            <ul className="wt-picker-list" ref={listRef}>
              {selectableWorktrees.map((wt) => {
                const isDisabled = disabledPaths.includes(wt.path);
                return (
                  <li key={wt.path}>
                    <button
                      type="button"
                      className={`wt-picker-item ${wt.is_current ? 'current' : ''} ${isDisabled ? 'disabled' : ''}`}
                      onClick={() => handleSelect(wt)}
                      disabled={isDisabled}
                      title={wt.path}
                    >
                      <GitBranch size={14} />
                      <span className="wt-picker-item-branch">{wt.branch}</span>
                      <span className="wt-picker-item-path">{wt.path}</span>
                      {wt.is_current && (
                        <span className="wt-picker-badge">active</span>
                      )}
                      {isDisabled && (
                        <span className="wt-picker-badge assigned">assigned</span>
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        {/* Footer */}
        <div className="wt-picker-footer">
          <button type="button" className="wt-picker-btn-cancel" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
};

export default WorktreePickerDialog;
