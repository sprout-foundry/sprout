import type { UseGitWorkspaceOptions, UseGitWorkspaceResult } from '../../hooks/useGitWorkspace';
import type { GitCommitDetail, GitCommitSummary } from '../../types/git-types';
import type { GitSidebarPanelProps } from '../GitSidebarPanel';

/** Opens a workspace tab (a diff, a review, a compare view) and returns its buffer id. */
export type OpenWorkspaceBuffer = UseGitWorkspaceOptions['openWorkspaceBuffer'];

/**
 * Everything the git section renders from: the working-tree panel's props
 * plus the history tab's loaders and the tab opener the history panel uses
 * for commit diffs.
 */
export interface SidebarGitPanelProps extends GitSidebarPanelProps {
  openWorkspaceBuffer: OpenWorkspaceBuffer;
  onLoadCommits: (
    limit: number,
    offset: number,
    opts?: { signal?: AbortSignal },
  ) => Promise<{
    commits: GitCommitSummary[];
    total: number;
  }>;
  onLoadCommitDetail: (hash: string) => Promise<GitCommitDetail>;
  onLoadCommitFileDiff: (
    hash: string,
    filePath: string,
  ) => Promise<{
    message: string;
    hash: string;
    path: string;
    diff: string;
  }>;
  onCheckoutCommit: (commitHash: string) => Promise<{ message: string }>;
  onRevertCommit: (commitHash: string) => Promise<{ message: string }>;
}

export interface GitPanelPropsExtras {
  openWorkspaceBuffer: OpenWorkspaceBuffer;
  onOpenFile?: (path: string) => void;
}

/**
 * Map the git workspace state (`useGitWorkspace`) onto the git section's
 * props. The app's sidebar and the composed workspace's `git` view both build
 * their panel through it, so the two cannot drift apart.
 */
export function buildGitPanelProps(
  git: UseGitWorkspaceResult,
  { openWorkspaceBuffer, onOpenFile }: GitPanelPropsExtras,
): SidebarGitPanelProps {
  return {
    gitStatus: git.gitStatus,
    gitBranches: git.gitBranches,
    workspaceRoot: git.workspaceRoot,
    selectedFiles: git.selectedFiles,
    activeDiffSelectionKey: git.activeDiffSelectionKey,
    commitMessage: git.commitMessage,
    isLoading: git.isGitLoading,
    isActing: git.isGitActing,
    isGeneratingCommitMessage: git.isGeneratingCommitMessage,
    isReviewLoading: git.isReviewLoading,
    actionError: git.gitActionError,
    actionWarning: git.gitActionWarning,
    onCommitMessageChange: git.setCommitMessage,
    onGenerateCommitMessage: git.handleGenerateCommitMessage,
    onCommit: git.handleGitCommitClick,
    onRunReview: git.handleRunReview,
    onCheckoutBranch: git.handleCheckoutBranch,
    onCreateBranch: git.handleCreateBranch,
    onPull: git.handlePull,
    onPush: git.handlePush,
    onPullRequest: git.handleCreatePullRequest,
    onRefresh: git.refreshGitStatus,
    onToggleFileSelection: git.handleToggleFileSelection,
    onToggleSectionSelection: git.handleToggleSectionSelection,
    onClearSelection: git.clearSelectedFiles,
    onSelectFiles: git.handleSelectFiles,
    onPreviewFile: git.handlePreviewGitFile,
    onStageSelected: git.handleStageSelected,
    onUnstageSelected: git.handleUnstageSelected,
    onDiscardSelected: git.handleDiscardSelected,
    onStageFile: git.handleStageFile,
    onUnstageFile: git.handleUnstageFile,
    onDiscardFile: git.handleDiscardFile,
    onSectionAction: git.handleSectionAction,
    onOpenFile,
    onLoadCommits: git.handleLoadCommits,
    onLoadCommitDetail: git.handleLoadCommitDetail,
    onLoadCommitFileDiff: git.handleLoadCommitFileDiff,
    onCheckoutCommit: git.handleCheckoutCommit,
    onRevertCommit: git.handleRevertCommit,
    openWorkspaceBuffer,
  };
}
