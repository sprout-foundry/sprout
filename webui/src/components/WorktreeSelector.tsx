import { useState, useCallback } from 'react';
import { GitBranch, Plus, X, Check, Loader2 } from 'lucide-react';
import type { WorktreeInfo } from '../services/chatSessions';
import { listWorktrees, createWorktree } from '../services/chatSessions';
import { debugLog } from '../utils/log';

interface WorktreeSelectorProps {
  chatId: string;
  currentWorktreePath?: string;
  onWorktreeChange?: (worktreePath: string) => void;
  onClose?: () => void;
}

interface CreateWorktreeModalProps {
  onClose: () => void;
  onCreate: (path: string, branch: string, baseRef?: string) => Promise<void>;
  existingPaths: string[];
}

export function WorktreeSelector({
  chatId,
  currentWorktreePath,
  onWorktreeChange,
  onClose,
}: WorktreeSelectorProps): JSX.Element | null {
  const [worktrees, setWorktrees] = useState<WorktreeInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);

  const loadWorktrees = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await listWorktrees();
      setWorktrees(result.worktrees || []);
    } catch (err) {
      debugLog('[WorktreeSelector] Failed to load worktrees:', err);
      setError(err instanceof Error ? err.message : 'Failed to load worktrees');
    } finally {
      setLoading(false);
    }
  }, []);

  useState(() => {
    loadWorktrees();
  });

  const handleSelectWorktree = useCallback(
    async (worktreePath: string) => {
      if (onWorktreeChange) {
        await onWorktreeChange(worktreePath);
      }
      if (onClose) {
        onClose();
      }
    },
    [onWorktreeChange, onClose],
  );

  if (showCreateModal) {
    return (
      <CreateWorktreeModal
        onClose={() => setShowCreateModal(false)}
        onCreate={async (path, branch, baseRef) => {
          try {
            await createWorktree(path, branch, baseRef);
            await loadWorktrees();
            setShowCreateModal(false);
          } catch (err) {
            debugLog('[WorktreeSelector] Failed to create worktree:', err);
            throw err;
          }
        }}
        existingPaths={worktrees.map((wt) => wt.path)}
      />
    );
  }

  const mainWorktree = worktrees.find((wt) => wt.is_main);
  const currentWorktree = worktrees.find((wt) => wt.is_current);

  return (
    <div className="worktree-selector">
      <div className="worktree-selector-header">
        <GitBranch size={16} />
        <span>Git Worktree</span>
        {onClose && (
          <button className="worktree-selector-close" onClick={onClose} type="button">
            <X size={14} />
          </button>
        )}
      </div>

      {error && <div className="worktree-selector-error">{error}</div>}

      {loading ? (
        <div className="worktree-selector-loading">
          <Loader2 className="spinner" size={16} />
          <span>Loading worktrees...</span>
        </div>
      ) : worktrees.length === 0 ? (
        <div className="worktree-selector-empty">
          <p>No git worktrees found</p>
          <button className="worktree-selector-create-btn" onClick={() => setShowCreateModal(true)} type="button">
            <Plus size={14} />
            Create Worktree
          </button>
        </div>
      ) : (
        <div className="worktree-selector-list">
          {/* Main workspace */}
          {mainWorktree && (
            <button
              className={`worktree-selector-item ${currentWorktreePath === mainWorktree.path ? 'active' : ''}`}
              onClick={() => handleSelectWorktree(mainWorktree.path)}
              type="button"
            >
              <div className="worktree-selector-item-main">
                <span className="worktree-selector-item-name">Main Workspace</span>
                <span className="worktree-selector-item-branch">{mainWorktree.branch}</span>
              </div>
              {currentWorktreePath === mainWorktree.path && <Check size={14} />}
            </button>
          )}

          {/* Other worktrees */}
          {worktrees
            .filter((wt) => wt.path !== mainWorktree?.path)
            .map((wt) => (
              <button
                key={wt.path}
                className={`worktree-selector-item ${currentWorktreePath === wt.path ? 'active' : ''}`}
                onClick={() => handleSelectWorktree(wt.path)}
                type="button"
              >
                <div className="worktree-selector-item-main">
                  <span className="worktree-selector-item-name">{wt.path}</span>
                  <span className="worktree-selector-item-branch">{wt.branch}</span>
                </div>
                {currentWorktreePath === wt.path && <Check size={14} />}
              </button>
            ))}

          <button
            className="worktree-selector-create-btn"
            onClick={() => setShowCreateModal(true)}
            type="button"
          >
            <Plus size={14} />
            Create New Worktree
          </button>
        </div>
      )}
    </div>
  );
}

function CreateWorktreeModal({ onClose, onCreate, existingPaths }: CreateWorktreeModalProps): JSX.Element {
  const [path, setPath] = useState('');
  const [branch, setBranch] = useState('');
  const [baseRef, setBaseRef] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);

    try {
      await onCreate(path, branch, baseRef || undefined);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create worktree');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content worktree-create-modal" onClick={(e) => e.stopPropagation()}>
        <h3>Create New Worktree</h3>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label htmlFor="wt-path">Worktree Path</label>
            <input
              id="wt-path"
              type="text"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="/path/to/worktree"
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="wt-branch">Branch Name</label>
            <input
              id="wt-branch"
              type="text"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder="feature/my-feature"
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="wt-base">Base Branch (optional)</label>
            <input
              id="wt-base"
              type="text"
              value={baseRef}
              onChange={(e) => setBaseRef(e.target.value)}
              placeholder="main (leave empty to use current branch)"
            />
          </div>

          {error && <div className="form-error">{error}</div>}

          <div className="form-actions">
            <button className="btn-secondary" onClick={onClose} type="button">
              Cancel
            </button>
            <button className="btn-primary" type="submit" disabled={loading}>
              {loading ? (
                <>
                  <Loader2 className="spinner" size={14} />
                  Creating...
                </>
              ) : (
                'Create Worktree'
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
