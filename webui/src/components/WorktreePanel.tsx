import { useState } from 'react';
import {
  Plus,
  X,
  ChevronRight,
  ChevronDown,
  GitBranch,
  AlertCircle,
  RefreshCw,
} from 'lucide-react';
import { useWorktrees } from '../hooks/useWorktrees';
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

  if (!isOpen) return null;

  return (
    <div className="worktree-modal-overlay" onClick={onClose}>
      <div className="worktree-modal" onClick={(e) => e.stopPropagation()}>
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
  const { worktrees, currentBranch, isLoading, error, refresh, createWorktree, removeWorktree, checkoutWorktree } =
    useWorktrees();

  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [expandedWorktrees, setExpandedWorktrees] = useState<boolean>(true);

  const handleCreate = async (path: string, branch: string, baseRef?: string) => {
    await createWorktree(path, branch, baseRef);
  };

  const handleRemove = async (path: string) => {
    if (!window.confirm(`Are you sure you want to remove this worktree?\n\nPath: ${path}`)) {
      return;
    }
    await removeWorktree(path);
  };

  const handleCheckout = async (path: string) => {
    await checkoutWorktree(path);
    // Panel will close automatically after workspace switch
  };

  const toggleExpand = () => {
    setExpandedWorktrees(!expandedWorktrees);
  };

  if (isLoading) {
    return (
      <div className="worktree-panel embedded">
        <div className="worktree-panel-content worktree-loading">
          <p>Loading worktrees...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="worktree-panel embedded">
      <div className="worktree-panel-toolbar">
        <button className="worktree-btn-secondary" onClick={() => setIsCreateDialogOpen(true)}>
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
            <p className="worktree-empty-hint">
              Create a worktree to run isolated chats for scoped feature work.
            </p>
            <button className="worktree-btn-primary" onClick={() => setIsCreateDialogOpen(true)}>
              <Plus size={16} />
              Create Worktree
            </button>
          </div>
        ) : (
          <div className="worktree-list">
            <div className="worktree-item is-main">
              <div className="worktree-item-header">
                <button className="worktree-expand-btn" onClick={toggleExpand} aria-label={expandedWorktrees ? 'Collapse' : 'Expand'}>
                  {expandedWorktrees ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                </button>
                <div className="worktree-item-info">
                  <GitBranch size={16} className="worktree-branch-icon" />
                  <span className="worktree-branch">{currentBranch || 'HEAD'}</span>
                  {expandedWorktrees && worktrees[0] && (
                    <span className="worktree-path">{worktrees[0].path}</span>
                  )}
                </div>
              </div>
            </div>

            {worktrees
              .filter((wt) => !wt.is_current)
              .map((wt) => (
                <div key={wt.path} className="worktree-item">
                  <div className="worktree-item-header">
                    <button
                      className="worktree-expand-btn"
                      onClick={() => handleCheckout(wt.path)}
                      aria-label="Switch to worktree"
                    >
                      <ChevronRight size={16} />
                    </button>
                    <div className="worktree-item-info">
                      <GitBranch size={16} className="worktree-branch-icon" />
                      <span className="worktree-branch">{wt.branch || 'HEAD'}</span>
                      <span className="worktree-path">{wt.path}</span>
                      {wt.parent_branch && (
                        <span className="worktree-parent">
                          {' '}
                          from {wt.parent_branch}
                        </span>
                      )}
                    </div>
                    <button
                      className="worktree-remove-btn"
                      onClick={() => handleRemove(wt.path)}
                      aria-label="Remove worktree"
                    >
                      <X size={14} />
                    </button>
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
