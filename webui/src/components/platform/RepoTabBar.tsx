/**
 * RepoTabBar — Tab bar for multi-repo workspace.
 *
 * Shows all attached repos as clickable tabs. Active tab is highlighted.
 * "+" button opens the onboarding screen to attach a new repo.
 */

import React from 'react';
import { GitBranch, Plus, X, Loader2 } from 'lucide-react';
import './RepoFileTree.css';

interface AttachedRepo {
  owner: string;
  name: string;
  id: string;
}

interface RepoTabBarProps {
  repos: AttachedRepo[];
  activeRepoId: string | null;
  onSelectRepo: (id: string) => void;
  onAddRepo: () => void;
  onDetachRepo: (id: string) => void;
}

export const RepoTabBar: React.FC<RepoTabBarProps> = ({
  repos,
  activeRepoId,
  onSelectRepo,
  onAddRepo,
  onDetachRepo,
}) => {
  if (repos.length === 0) {
    return (
      <div className="repo-tab-bar-empty">
        <button className="repo-tab-add" onClick={onAddRepo}>
          <Plus size={14} /> Add Repository
        </button>
      </div>
    );
  }

  return (
    <div className="repo-tab-bar-container">
      <div className="repo-tab-bar-tabs">
        {repos.map((repo) => {
          const isActive = repo.id === activeRepoId;
          const label = repo.owner === 'local' ? repo.name : `${repo.owner}/${repo.name}`;
          return (
            <div
              key={repo.id}
              className={`repo-tab-item ${isActive ? 'active' : ''}`}
              onClick={() => onSelectRepo(repo.id)}
              role="tab"
              tabIndex={0}
              aria-selected={isActive}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelectRepo(repo.id);
                }
              }}
            >
              <GitBranch size={12} />
              <span className="repo-tab-label">{label}</span>
              {repos.length > 1 && (
                <button
                  className="repo-tab-close"
                  onClick={(e) => {
                    e.stopPropagation();
                    onDetachRepo(repo.id);
                  }}
                  title={`Detach ${label}`}
                  aria-label={`Detach ${label}`}
                >
                  <X size={10} />
                </button>
              )}
            </div>
          );
        })}
      </div>
      <button className="repo-tab-add" onClick={onAddRepo} title="Add another repository">
        <Plus size={14} />
      </button>
    </div>
  );
};

export default RepoTabBar;
