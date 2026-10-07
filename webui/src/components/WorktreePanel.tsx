import { Plus, X, GitBranch, AlertCircle, RefreshCw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useWorktrees } from '../hooks/useWorktrees';
import { showThemedConfirm } from './ThemedDialog';
import './WorktreePanel.css';

interface WorktreePanelProps {
  onClose?: () => void;
}

interface CreateWorktreeDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onCreate: (path: string, branch: string, baseRef?: string) => Promise<void>;
}

function CreateWorktreeDialog({ isOpen, onClose, onCreate }: CreateWorktreeDialogProps) {
  const [path, setPath] = useState('');
  const [branch, setBranch] = useState('');
  const [baseRef, setBaseRef] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!path || !branch) return;

    setIsSubmitting(true);
    setError(null);
    try {
      await onCreate(path, branch, baseRef || undefined);
      setPath('');
      setBranch('');
      setBaseRef('');
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create worktree');
    } finally {
      setIsSubmitting(false);
    }
  };

  // Keyboard-driven dismiss while the modal is open: Escape mirrors the
  // click-outside affordance. Window-level listener (rather than div
  // onKeyDown) because divs need focus to receive keydown.
  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return (
    <div className="worktree-modal-overlay" onClick={onClose} role="presentation">
      <div
        className="worktree-modal"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Create Git Worktree"
      >
        <div className="worktree-modal-header">
          <h2>Create Git Worktree</h2>
          <button className="worktree-modal-close" onClick={onClose} aria-label="Close">
            <X size={18} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="worktree-form">
          {error && (
            <div className="worktree-error">
              <AlertCircle size={16} />
              <span>{error}</span>
            </div>
          )}

          <div className="worktree-form-group">
            <label htmlFor="wt-path">Worktree Path</label>
            <input
              id="wt-path"
              type="text"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="/path/to/feature-branch-worktree"
              required
            />
            <small>Relative to workspace root, e.g., ../feature-auth-worktree</small>
          </div>

          <div className="worktree-form-group">
            <label htmlFor="wt-branch">Branch Name</label>
            <input
              id="wt-branch"
              type="text"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder="feature-auth"
              required
            />
          </div>

          <div className="worktree-form-group">
            <label htmlFor="wt-base">Base Reference (optional)</label>
            <input
              id="wt-base"
              type="text"
              value={baseRef}
              onChange={(e) => setBaseRef(e.target.value)}
              placeholder="main (leave empty to use current branch)"
            />
            <small>Start from this branch/commit. If empty, uses current HEAD.</small>
          </div>

          <div className="worktree-form-actions">
            <button type="button" className="worktree-btn-secondary" onClick={onClose} disabled={isSubmitting}>
              Cancel
            </button>
            <button type="submit" className="worktree-btn-primary" disabled={isSubmitting}>
              {isSubmitting ? 'Creating...' : 'Create Worktree'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default function WorktreePanel({ onClose: _onClose }: WorktreePanelProps) {
  const { worktrees, isLoading, error, refresh, createWorktree, removeWorktree, checkoutWorktree } = useWorktrees();

  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);

  const handleCreate = async (path: string, branch: string, baseRef?: string) => {
    await createWorktree(path, branch, baseRef);
  };

  const handleRemove = async (path: string) => {
    const confirmed = await showThemedConfirm(`Are you sure you want to remove this worktree?\n\nPath: ${path}`, {
      type: 'danger',
    });
    if (!confirmed) {
      return;
    }
    await removeWorktree(path);
  };

  const handleCheckout = async (path: string) => {
    const confirmed = await showThemedConfirm(
      `Switch the workspace to this worktree?\n\nPath: ${path}\n\nThis changes the working directory, closes terminals, and rebinds chats to the new workspace.`,
      { title: 'Switch worktree?', confirmLabel: 'Switch', cancelLabel: 'Cancel' },
    );
    if (!confirmed) {
      return;
    }
    await checkoutWorktree(path);
    // Panel will close automatically after workspace switch
  };

  if (isLoading) {
    return (
      <div className="worktree-panel embedded" data-testid="worktree-panel">
        <div className="worktree-panel-content worktree-loading">
          <p>Loading worktrees...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="worktree-panel embedded" data-testid="worktree-panel">
      <div className="worktree-panel-toolbar">
        <button
          className="worktree-btn-secondary"
          onClick={() => setIsCreateDialogOpen(true)}
          data-testid="worktree-create-button"
        >
          <Plus size={16} />
          New Worktree
        </button>
        <button className="worktree-btn-icon" onClick={refresh} aria-label="Refresh worktrees">
          <RefreshCw size={16} />
        </button>
      </div>

      {error && (
        <div className="worktree-error-panel">
          <AlertCircle size={16} />
          <span>{error}</span>
        </div>
      )}

      <div className="worktree-panel-content">
        {worktrees.length === 0 ? (
          <div className="worktree-empty">
            <p>No git worktrees found.</p>
            <p className="worktree-empty-hint">Create a worktree to run isolated chats for scoped feature work.</p>
            <button className="worktree-btn-primary" onClick={() => setIsCreateDialogOpen(true)}>
              <Plus size={16} />
              Create Worktree
            </button>
          </div>
        ) : (
          <div className="worktree-list" data-testid="worktree-list">
            {worktrees.map((wt) => (
              <div
                key={wt.path}
                className={`worktree-item${wt.is_current ? ' is-current' : ''}`}
                data-testid="worktree-item"
              >
                <div className="worktree-item-header">
                  <GitBranch size={16} className="worktree-branch-icon" />
                  <div className="worktree-item-info">
                    <span className="worktree-branch-line">
                      <span className="worktree-branch">{wt.branch || 'HEAD'}</span>
                      {wt.is_current && <span className="worktree-badge worktree-badge-current">current</span>}
                      {wt.is_main && <span className="worktree-badge worktree-badge-main">main</span>}
                    </span>
                    <span className="worktree-path">{wt.path}</span>
                    {wt.parent_branch && <span className="worktree-parent"> from {wt.parent_branch}</span>}
                  </div>
                  {!wt.is_current && (
                    <>
                      <button
                        className="worktree-switch-btn"
                        onClick={() => handleCheckout(wt.path)}
                        aria-label="Switch to worktree"
                        data-testid="worktree-switch"
                      >
                        Switch
                      </button>
                      <button
                        className="worktree-remove-btn"
                        onClick={() => handleRemove(wt.path)}
                        aria-label="Remove worktree"
                        data-testid="worktree-delete"
                      >
                        <X size={14} />
                      </button>
                    </>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <CreateWorktreeDialog
        isOpen={isCreateDialogOpen}
        onClose={() => setIsCreateDialogOpen(false)}
        onCreate={handleCreate}
      />
    </div>
  );
}
