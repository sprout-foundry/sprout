import { ArrowDown, ArrowUp, CheckCircle2, ExternalLink, GitBranch, Plus, RefreshCw } from 'lucide-react';
import type { GitBranchesState, GitStatusData } from '../../types/git-types';

export interface GitHeaderProps {
  gitStatus: GitStatusData;
  gitBranches: GitBranchesState;
  branchName: string;
  isActing: boolean;
  isLoading: boolean;
  onCheckoutBranch: (branch: string) => void;
  onCreateBranch: () => void;
  onPull: () => void;
  onPush: () => void;
  onOpenPrDialog: () => void;
  onRefresh: () => void;
}

function GitHeader({
  gitStatus,
  gitBranches,
  branchName,
  isActing,
  isLoading,
  onCheckoutBranch,
  onCreateBranch,
  onPull,
  onPush,
  onOpenPrDialog,
  onRefresh,
}: GitHeaderProps) {
  return (
    <div className="git-sidebar-header">
      <div className="git-sidebar-toolbar-row">
        <label className="git-branch-select-wrap" htmlFor="git-branch-select" data-testid="git-remote-url">
          <span className="branch-icon">
            <GitBranch size={14} />
          </span>
          <select
            id="git-branch-select"
            className="git-branch-select"
            value={branchName}
            onChange={(event) => onCheckoutBranch(event.target.value)}
            disabled={isActing || isLoading || gitBranches.branches.length === 0}
          >
            {gitBranches.branches.length === 0 ? (
              <option value={branchName}>{branchName}</option>
            ) : (
              gitBranches.branches.map((branch) => (
                <option key={branch} value={branch}>
                  {branch}
                </option>
              ))
            )}
          </select>
        </label>
        <button
          type="button"
          className="git-header-icon-btn"
          onClick={onCreateBranch}
          disabled={isActing || isLoading}
          title="Create branch"
          aria-label="Create branch"
        >
          <Plus size={14} />
        </button>
      </div>
      <div className="git-sidebar-toolbar-row git-sidebar-toolbar-row-secondary">
        <div className="git-sidebar-sync-status">
          {gitStatus?.clean ? (
            <span className="clean">
              <CheckCircle2 size={14} />
              Clean
            </span>
          ) : (
            <span className="dirty">
              <RefreshCw size={14} />
              Changes
            </span>
          )}
        </div>
        <div className="git-sidebar-toolbar-actions">
          <button
            type="button"
            className="git-header-action-btn"
            onClick={onPull}
            disabled={isActing || isLoading}
            title={
              gitStatus?.behind && gitStatus.behind > 0
                ? `Pull ${gitStatus.behind} commit${gitStatus.behind === 1 ? '' : 's'} from upstream`
                : 'Pull from upstream'
            }
          >
            <ArrowDown size={12} />
            <span>Pull</span>
            {gitStatus?.behind && gitStatus.behind > 0 ? (
              <span className="git-header-action-badge">{gitStatus.behind}</span>
            ) : null}
          </button>
          <button
            type="button"
            className="git-header-action-btn"
            data-testid="git-push-button"
            onClick={onPush}
            disabled={isActing || isLoading}
            title={
              gitStatus?.ahead && gitStatus.ahead > 0
                ? `Push ${gitStatus.ahead} commit${gitStatus.ahead === 1 ? '' : 's'} to upstream`
                : 'Push to upstream'
            }
          >
            <ArrowUp size={12} />
            <span>Push</span>
            {gitStatus?.ahead && gitStatus.ahead > 0 ? (
              <span className="git-header-action-badge">{gitStatus.ahead}</span>
            ) : null}
          </button>
          <button
            type="button"
            className="git-header-action-btn"
            onClick={onOpenPrDialog}
            disabled={isActing || isLoading}
            title="Create pull request on GitHub"
          >
            <ExternalLink size={12} />
            <span>PR</span>
          </button>
          <button
            type="button"
            className="git-header-icon-btn"
            onClick={onRefresh}
            disabled={isActing || isLoading}
            title="Refresh git status"
            aria-label="Refresh git status"
          >
            <RefreshCw size={14} />
          </button>
        </div>
      </div>
    </div>
  );
}

export default GitHeader;
