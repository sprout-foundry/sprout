import { ArrowDown, ArrowUp, Check, GitBranch, GitPullRequest, Plus, RefreshCw } from 'lucide-react';
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
  /** Disable the Pull button (e.g. browser mode has no pull). */
  pullDisabled?: boolean;
  /** Disable the Pull Request button (e.g. browser mode). */
  pullRequestDisabled?: boolean;
  /** Tooltip shown on disabled-for-browser buttons. */
  unsupportedTooltip?: string;
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
  pullDisabled = false,
  pullRequestDisabled = false,
  unsupportedTooltip,
}: GitHeaderProps) {
  const behind = gitStatus?.behind ?? 0;
  const ahead = gitStatus?.ahead ?? 0;

  return (
    <div className="git-sidebar-header">
      {/* Row 1: Branch selector + create branch — tight, visually connected */}
      <div className="git-toolbar-branch-row">
        <label className="git-branch-select-wrap" htmlFor="git-branch-select" data-testid="git-remote-url">
          <span className="branch-icon">
            <GitBranch size={13} />
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
          className="git-toolbar-icon-btn"
          onClick={onCreateBranch}
          disabled={isActing || isLoading}
          title="Create branch"
          aria-label="Create branch"
        >
          <Plus size={14} />
        </button>
      </div>
      {/* Row 2: Sync status dot + toolbar actions — compact, IDE-like */}
      <div className="git-toolbar-action-row">
        <div className="git-sync-indicator">
          <span className={`git-sync-dot ${gitStatus?.clean ? 'clean' : 'dirty'}`} />
          <span className="git-sync-label">{gitStatus?.clean ? 'Clean' : 'Changes'}</span>
          {(ahead > 0 || behind > 0) && (
            <span className="git-sync-ahead-behind">
              {ahead > 0 && <span className="ahead">↑{ahead}</span>}
              {behind > 0 && <span className="behind">↓{behind}</span>}
            </span>
          )}
        </div>
        <div className="git-toolbar-group">
          <button
            type="button"
            className="git-toolbar-icon-btn"
            onClick={onRefresh}
            disabled={isActing || isLoading}
            title="Refresh git status"
            aria-label="Refresh git status"
          >
            <RefreshCw size={14} className={isLoading ? 'git-spin' : undefined} />
          </button>
          <button
            type="button"
            className="git-toolbar-icon-btn"
            data-testid="git-push-button"
            onClick={onPush}
            disabled={isActing || isLoading}
            title={ahead > 0 ? `Push ${ahead} commit${ahead === 1 ? '' : 's'} to upstream` : 'Push to upstream'}
            aria-label="Push"
          >
            <ArrowUp size={14} />
            {ahead > 0 && <span className="git-icon-badge">{ahead}</span>}
          </button>
          <button
            type="button"
            className="git-toolbar-icon-btn"
            onClick={onPull}
            disabled={isActing || isLoading || pullDisabled}
            title={
              pullDisabled
                ? unsupportedTooltip
                : behind > 0
                  ? `Pull ${behind} commit${behind === 1 ? '' : 's'} from upstream`
                  : 'Pull from upstream'
            }
            aria-label="Pull"
          >
            <ArrowDown size={14} />
            {behind > 0 && <span className="git-icon-badge">{behind}</span>}
          </button>
          <button
            type="button"
            className="git-toolbar-icon-btn"
            onClick={onOpenPrDialog}
            disabled={isActing || isLoading || pullRequestDisabled}
            title={pullRequestDisabled ? unsupportedTooltip : 'Create pull request on GitHub'}
            aria-label="Create pull request"
          >
            <GitPullRequest size={14} />
          </button>
        </div>
      </div>
    </div>
  );
}

export default GitHeader;
