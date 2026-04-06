import React, { useState, useEffect, useRef } from 'react';
import { GitBranch, X, Loader2, AlertCircle, Plus, FolderTree, TriangleAlert } from 'lucide-react';
import { listWorktrees } from '../services/chatSessions';
import type { WorktreeInfo } from '../services/chatSessions';
import './WorktreeChatDialog.css';

export interface WorktreeChatDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (params: {
    branch: string;
    baseRef: string;
    name: string;
    autoSwitch: boolean;
  }) => void;
  isCreating?: boolean;
  error?: string | null;
}

export const WorktreeChatDialog: React.FC<WorktreeChatDialogProps> = ({
  isOpen,
  onClose,
  onSubmit,
  isCreating = false,
  error = null,
}) => {
  const [branch, setBranch] = useState('');
  const [baseRef, setBaseRef] = useState('');
  const [name, setName] = useState('');
  const [autoSwitch, setAutoSwitch] = useState(true);
  const [existingWorktrees, setExistingWorktrees] = useState<WorktreeInfo[]>([]);
  const [isLoadingWorktrees, setIsLoadingWorktrees] = useState(false);
  
  const branchInputRef = useRef<HTMLInputElement>(null);

  // Reset form when dialog opens
  useEffect(() => {
    if (isOpen) {
      setBranch('');
      setBaseRef('');
      setName('');
      setAutoSwitch(true);
      setExistingWorktrees([]);
      
      // Focus the branch input when dialog opens
      setTimeout(() => {
        branchInputRef.current?.focus();
      }, 50);

      // Fetch existing worktrees
      setIsLoadingWorktrees(true);
      listWorktrees()
        .then((resp) => setExistingWorktrees(resp.worktrees))
        .catch((err) => {
          console.warn('[WorktreeChatDialog] Failed to load existing worktrees:', err);
        })
        .finally(() => setIsLoadingWorktrees(false));
    }
  }, [isOpen]);

  // Handle overlay click (close on overlay click only)
  const handleOverlayClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };

  // Handle keyboard escape to close
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen && !isCreating) {
        onClose();
      }
    };

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, onClose, isCreating]);

  // Handle form submission
  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    // Validate branch is non-empty
    if (!branch.trim()) {
      return;
    }

    onSubmit({
      branch: branch.trim(),
      baseRef: baseRef.trim(),
      name: name.trim(),
      autoSwitch,
    });
  };

  // Handle cancel
  const handleCancel = () => {
    onClose();
  };

  // Check if the typed branch already has a worktree
  const branchHasWorktree =
    branch.trim() && existingWorktrees.some((wt) => wt.branch === branch.trim());

  // Handle clicking an existing worktree row to pre-fill the branch
  const handleWorktreeClick = (wt: WorktreeInfo) => {
    if (wt.is_main) return;
    setBranch(wt.branch);
  };

  // Non-main worktrees that are suitable for selection
  const selectableWorktrees = existingWorktrees.filter((wt) => !wt.is_main);

  if (!isOpen) {
    return null;
  }

  return (
    <div
      className="wt-chat-dialog-overlay"
      onClick={handleOverlayClick}
      role="dialog"
      aria-modal="true"
      aria-labelledby="wt-chat-dialog-title"
    >
      <div className="wt-chat-dialog-card">
        {/* Header */}
        <div className="wt-chat-dialog-header">
          <h2 id="wt-chat-dialog-title">
            <GitBranch className="wt-chat-icon" size={18} />
            Create Chat in Worktree
          </h2>
          <button
            className="wt-chat-dialog-close"
            onClick={handleCancel}
            aria-label="Close dialog"
            disabled={isCreating}
          >
            <X size={18} />
          </button>
        </div>

        {/* Content */}
        <form onSubmit={handleSubmit} className="wt-chat-dialog-content">
          {/* Error Message */}
          {error && (
            <div className="wt-chat-dialog-error" role="alert">
              <AlertCircle className="wt-chat-error-icon" size={16} />
              <span>{error}</span>
            </div>
          )}

          {/* Branch Name Input */}
          <div className="wt-chat-dialog-form-group">
            <label htmlFor="wt-branch">Branch Name</label>
            <input
              ref={branchInputRef}
              id="wt-branch"
              type="text"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder="feature/my-feature"
              disabled={isCreating}
              required
              autoComplete="off"
            />
            {branchHasWorktree && (
              <small className="wt-chat-dialog-warning">
                <TriangleAlert size={12} />
                A worktree already exists for branch <strong>{branch.trim()}</strong>
              </small>
            )}
            {!branchHasWorktree && (
              <small>Create a new branch for this worktree</small>
            )}
          </div>

          {/* Base Reference Input */}
          <div className="wt-chat-dialog-form-group">
            <label htmlFor="wt-base-ref">Base Reference</label>
            <input
              id="wt-base-ref"
              type="text"
              value={baseRef}
              onChange={(e) => setBaseRef(e.target.value)}
              placeholder="main"
              disabled={isCreating}
              autoComplete="off"
            />
            <small>The branch to create from (e.g., main, develop)</small>
          </div>

          {/* Chat Name Input */}
          <div className="wt-chat-dialog-form-group">
            <label htmlFor="wt-name">Chat Name</label>
            <input
              id="wt-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Auto-generated"
              disabled={isCreating}
              autoComplete="off"
            />
            <small>Leave empty for auto-generated name</small>
          </div>

          {/* Auto-switch Checkbox */}
          <div className="wt-chat-dialog-checkbox-wrapper">
            <input
              type="checkbox"
              id="wt-auto-switch"
              className="wt-chat-dialog-checkbox"
              checked={autoSwitch}
              onChange={(e) => setAutoSwitch(e.target.checked)}
              disabled={isCreating}
            />
            <label htmlFor="wt-auto-switch" className="wt-chat-dialog-checkbox-label">
              Switch workspace to this worktree
            </label>
          </div>

          {/* Existing Worktrees Section */}
          <div className="wt-chat-dialog-worktrees-section">
            <div className="wt-chat-dialog-worktrees-header">
              <FolderTree size={14} />
              <span>Existing Worktrees</span>
            </div>
            {isLoadingWorktrees ? (
              <div className="wt-chat-dialog-worktrees-loading">
                <Loader2 size={14} className="wt-chat-dialog-spinner" />
                <span>Loading worktrees…</span>
              </div>
            ) : selectableWorktrees.length === 0 ? (
              <div className="wt-chat-dialog-worktrees-empty">
                No existing worktrees. The branch above will create a new one.
              </div>
            ) : (
              <ul className="wt-chat-dialog-worktrees-list">
                {selectableWorktrees.map((wt) => (
                  <li key={wt.path}>
                    <button
                      type="button"
                      className={`wt-chat-dialog-worktree-item ${wt.is_current ? 'current' : ''}`}
                      onClick={() => handleWorktreeClick(wt)}
                      title={wt.path}
                      disabled={isCreating}
                    >
                      <GitBranch size={13} />
                      <span className="wt-chat-dialog-worktree-branch">{wt.branch}</span>
                      {wt.is_current && (
                        <span className="wt-chat-dialog-worktree-badge">active</span>
                      )}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {/* Actions */}
          <div className="wt-chat-dialog-actions">
            <button
              type="button"
              className="wt-chat-dialog-btn-cancel"
              onClick={handleCancel}
              disabled={isCreating}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="wt-chat-dialog-btn-create"
              disabled={isCreating || !branch.trim()}
            >
              {isCreating ? (
                <>
                  <Loader2 className="wt-chat-dialog-spinner" size={16} />
                  Creating...
                </>
              ) : (
                <>
                  <Plus size={16} />
                  Create
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
