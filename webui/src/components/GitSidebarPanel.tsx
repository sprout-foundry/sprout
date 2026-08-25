import { AlertTriangle, CheckSquare, MinusSquare, PlusSquare, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { isCloud } from '../config/mode';
import { MAX_FILES_INITIAL, LOAD_MORE_INCREMENT } from '../constants/git-constants';
import { BROWSER_GIT_UNSUPPORTED_OPS } from '../services/browserGit';
import type { FileSection, GitBranchesState, GitFile, GitStatusData } from '../types/git-types';
import { parseSelectionKey } from '../types/git-types';
import GitCommitBox from './git/GitCommitBox';
import GitContextMenu from './git/GitContextMenu';
import GitFileSections from './git/GitFileSections';
import GitHeader from './git/GitHeader';
import GitPRDialog from './git/GitPRDialog';
import { showThemedPrompt } from './ThemedDialog';

// Tooltip shown on disabled buttons that browser git can't perform.
const BROWSER_UNSUPPORTED_TOOLTIP = 'Not available in browser mode';

/**
 * Whether a given git op (by the names used in browserGit's
 * executeGitOp switch) is unavailable in cloud/browser mode. Returns
 * false everywhere except browser mode so local-mode callers always
 * report full support.
 */
export function isGitOpUnsupported(op: string): boolean {
  return isCloud && BROWSER_GIT_UNSUPPORTED_OPS.has(op);
}

// Re-export types so existing consumers (tests, Sidebar, etc.) don't break.
export type { GitStatusData, GitFile } from '../types/git-types';

export interface GitSidebarPanelProps {
  gitStatus: GitStatusData | null;
  gitBranches: GitBranchesState;
  selectedFiles: Set<string>;
  activeDiffSelectionKey: string | null;
  commitMessage: string;
  isLoading: boolean;
  isActing: boolean;
  isGeneratingCommitMessage: boolean;
  isReviewLoading: boolean;
  actionError: string | null;
  actionWarning: string | null;
  workspaceRoot?: string;
  onCommitMessageChange: (value: string) => void;
  onGenerateCommitMessage: () => void;
  onCommit: () => void;
  onRunReview: () => void;
  onCheckoutBranch: (branch: string) => void;
  onCreateBranch: (name: string) => void;
  onPull: () => void;
  onPush: () => void;
  onRefresh: () => void;
  onToggleFileSelection: (section: FileSection, path: string) => void;
  onToggleSectionSelection: (section: FileSection) => void;
  onClearSelection: () => void;
  onSelectFiles?: (keys: string[]) => void;
  onPreviewFile: (section: FileSection, path: string) => void;
  onStageSelected: () => void;
  onUnstageSelected: () => void;
  onDiscardSelected: () => void;
  onStageFile: (path: string) => void;
  onUnstageFile: (path: string) => void;
  onDiscardFile: (path: string) => void;
  onSectionAction: (section: FileSection) => void;
  onOpenFile?: (path: string) => void;
  onPullRequest: (params: {
    title: string;
    body?: string;
    base?: string;
    head?: string;
    draft?: boolean;
  }) => Promise<{ url: string; number: number; state: string }>;
}

function GitSidebarPanel({
  gitStatus,
  gitBranches,
  selectedFiles,
  activeDiffSelectionKey,
  commitMessage,
  isLoading,
  isActing,
  isGeneratingCommitMessage,
  isReviewLoading,
  actionError,
  actionWarning,
  workspaceRoot,
  onCommitMessageChange,
  onGenerateCommitMessage,
  onCommit,
  onRunReview,
  onCheckoutBranch,
  onCreateBranch,
  onPull,
  onPush,
  onRefresh,
  onToggleFileSelection,
  onToggleSectionSelection,
  onClearSelection,
  onSelectFiles,
  onPreviewFile,
  onStageSelected,
  onUnstageSelected,
  onDiscardSelected,
  onStageFile,
  onUnstageFile,
  onDiscardFile,
  onSectionAction,
  onOpenFile,
  onPullRequest,
}: GitSidebarPanelProps): JSX.Element {
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    section: FileSection;
    file: GitFile;
  } | null>(null);

  // Free-text filter over file paths across all sections
  const [fileFilter, setFileFilter] = useState('');
  const normalizedFilter = fileFilter.trim().toLowerCase();
  const filterFiles = useCallback(
    (files: GitFile[]): GitFile[] => {
      if (!normalizedFilter) return files;
      return files.filter((file) => file.path.toLowerCase().includes(normalizedFilter));
    },
    [normalizedFilter],
  );

  // Track visible file count per section for pagination
  const [visibleCounts, setVisibleCounts] = useState<Record<FileSection, number>>({
    staged: MAX_FILES_INITIAL,
    modified: MAX_FILES_INITIAL,
    untracked: MAX_FILES_INITIAL,
    deleted: MAX_FILES_INITIAL,
  });

  // PR dialog state
  const [showPrDialog, setShowPrDialog] = useState(false);

  const handleResetVisibleCounts = useCallback(() => {
    setVisibleCounts({
      staged: MAX_FILES_INITIAL,
      modified: MAX_FILES_INITIAL,
      untracked: MAX_FILES_INITIAL,
      deleted: MAX_FILES_INITIAL,
    });
  }, []);

  // Reset visible counts when git status changes
  useEffect(() => {
    handleResetVisibleCounts();
  }, [handleResetVisibleCounts]);

  const handleLoadMore = useCallback(
    (section: FileSection) => {
      setVisibleCounts((prev) => {
        const current = prev[section];
        const files = gitStatus?.[section] || [];
        const newCount = Math.min(current + LOAD_MORE_INCREMENT, files.length);
        return { ...prev, [section]: newCount };
      });
    },
    [gitStatus],
  );

  // Filtered file lists per section; recomputed only when status or filter changes.
  // Must run before any early returns — React requires a stable hook count across renders.
  const filteredBySection = useMemo(() => {
    const out: Record<FileSection, GitFile[]> = {
      staged: filterFiles(gitStatus?.staged ?? []),
      modified: filterFiles(gitStatus?.modified ?? []),
      untracked: filterFiles(gitStatus?.untracked ?? []),
      deleted: filterFiles(gitStatus?.deleted ?? []),
    };
    return out;
  }, [filterFiles, gitStatus]);

  if (isLoading) {
    return (
      <div className="git-sidebar-panel">
        <div className="empty">Loading git status…</div>
      </div>
    );
  }

  if (!gitStatus) {
    return (
      <div className="git-sidebar-panel">
        <div className="empty">No git repository found</div>
      </div>
    );
  }

  const hasStagedFiles = (gitStatus?.staged.length ?? 0) > 0;
  const branchName = gitBranches.current || gitStatus?.branch || 'detached';

  // In cloud/browser mode several git ops are unimplemented (unstage,
  // discard, pull, revert, PR). Disable the corresponding UI so users
  // never trigger a "not yet supported in browser mode" 500 error.
  const unsupported = {
    unstage: isGitOpUnsupported('unstage'),
    discard: isGitOpUnsupported('discard'),
    pull: isGitOpUnsupported('pull'),
    revert: isGitOpUnsupported('revert'),
    pullRequest: isGitOpUnsupported('pull-request'),
  } as const;
  const unsupportedTooltip = isCloud ? BROWSER_UNSUPPORTED_TOOLTIP : undefined;

  const totalFiles =
    (gitStatus?.staged.length ?? 0) +
    (gitStatus?.modified.length ?? 0) +
    (gitStatus?.untracked.length ?? 0) +
    (gitStatus?.deleted.length ?? 0);
  const matchedFileCount =
    filteredBySection.staged.length +
    filteredBySection.modified.length +
    filteredBySection.untracked.length +
    filteredBySection.deleted.length;
  const selectedEntries = Array.from(selectedFiles)
    .map(parseSelectionKey)
    .filter((entry): entry is { section: FileSection; path: string } => entry !== null);
  const stageSelectedCount = new Set(
    selectedEntries.filter((entry) => entry.section !== 'staged').map((entry) => entry.path),
  ).size;
  const unstageSelectedCount = new Set(
    selectedEntries.filter((entry) => entry.section === 'staged').map((entry) => entry.path),
  ).size;
  const discardSelectedCount = new Set(
    selectedEntries
      .filter((entry) => entry.section === 'modified' || entry.section === 'deleted')
      .map((entry) => entry.path),
  ).size;

  // Compute hidden section count for the file sections component
  const visibleSectionCount = Object.values(filteredBySection).filter((files) => files.length > 0).length;
  const hiddenSectionCount = 4 - visibleSectionCount;

  const handleCreateBranch = async () => {
    const value = await showThemedPrompt('Enter a name for the new branch:', {
      title: 'Create Branch',
      defaultValue: '',
      placeholder: 'branch-name',
    });
    if (!value) {
      return;
    }
    onCreateBranch(value);
  };

  const handleOpenContextMenu = (section: FileSection, file: GitFile, x: number, y: number) => {
    setContextMenu({ x, y, section, file });
  };

  return (
    <div className="git-sidebar-panel">
      <GitHeader
        gitStatus={gitStatus}
        gitBranches={gitBranches}
        branchName={branchName}
        isActing={isActing}
        isLoading={isLoading}
        onCheckoutBranch={onCheckoutBranch}
        onCreateBranch={handleCreateBranch}
        onPull={onPull}
        onPush={onPush}
        onOpenPrDialog={() => setShowPrDialog(true)}
        onRefresh={onRefresh}
        pullDisabled={unsupported.pull}
        pullRequestDisabled={unsupported.pullRequest}
        unsupportedTooltip={unsupportedTooltip}
      />

      <GitCommitBox
        commitMessage={commitMessage}
        hasStagedFiles={hasStagedFiles}
        isActing={isActing}
        isGeneratingCommitMessage={isGeneratingCommitMessage}
        isReviewLoading={isReviewLoading}
        onCommitMessageChange={onCommitMessageChange}
        onGenerateCommitMessage={onGenerateCommitMessage}
        onCommit={onCommit}
        onRunReview={onRunReview}
      />

      {actionError ? (
        <div className="git-sidebar-error">
          <AlertTriangle size={14} />
          <span>{actionError}</span>
        </div>
      ) : null}

      {actionWarning ? (
        <div className="git-sidebar-warning">
          <AlertTriangle size={14} />
          <span>{actionWarning}</span>
        </div>
      ) : null}

      {selectedEntries.length > 0 ? (
        <div className="git-sidebar-selection-bar">
          <div className="git-sidebar-selection-summary">
            <CheckSquare size={14} />
            <span>{selectedEntries.length} selected</span>
          </div>
          <div className="git-sidebar-selection-actions">
            {stageSelectedCount > 0 && (
              <button className="sidebar-action-btn compact" onClick={onStageSelected} disabled={isActing}>
                <PlusSquare size={13} />
                Stage {stageSelectedCount}
              </button>
            )}
            {unstageSelectedCount > 0 && (
              <button
                className="sidebar-action-btn compact"
                onClick={onUnstageSelected}
                disabled={isActing || unsupported.unstage}
                title={unsupported.unstage ? unsupportedTooltip : undefined}
              >
                <MinusSquare size={13} />
                Unstage {unstageSelectedCount}
              </button>
            )}
            {discardSelectedCount > 0 && (
              <button
                className="sidebar-action-btn compact danger"
                onClick={onDiscardSelected}
                disabled={isActing || unsupported.discard}
                title={unsupported.discard ? unsupportedTooltip : undefined}
              >
                <Trash2 size={13} />
                {selectedEntries.some((entry) => entry.section === 'deleted') ? 'Restore/Discard' : 'Discard'}{' '}
                {discardSelectedCount}
              </button>
            )}
            <button className="sidebar-action-btn compact ghost" onClick={onClearSelection} disabled={isActing}>
              Clear
            </button>
          </div>
        </div>
      ) : null}

      <GitFileSections
        gitStatus={gitStatus}
        filteredBySection={filteredBySection}
        visibleCounts={visibleCounts}
        selectedFiles={selectedFiles}
        activeDiffSelectionKey={activeDiffSelectionKey}
        isActing={isActing}
        fileFilter={fileFilter}
        normalizedFilter={normalizedFilter}
        totalFiles={totalFiles}
        matchedFileCount={matchedFileCount}
        hiddenSectionCount={hiddenSectionCount}
        onSelectFiles={onSelectFiles}
        onToggleFileSelection={onToggleFileSelection}
        onToggleSectionSelection={onToggleSectionSelection}
        onPreviewFile={onPreviewFile}
        onStageFile={onStageFile}
        onUnstageFile={onUnstageFile}
        onDiscardFile={onDiscardFile}
        onSectionAction={onSectionAction}
        onLoadMore={handleLoadMore}
        onSetFileFilter={setFileFilter}
        onOpenContextMenu={handleOpenContextMenu}
        unstageDisabled={unsupported.unstage}
        discardDisabled={unsupported.discard}
        unsupportedTooltip={unsupportedTooltip}
      />

      <GitContextMenu
        contextMenu={contextMenu}
        workspaceRoot={workspaceRoot}
        onPreviewFile={onPreviewFile}
        onOpenFile={onOpenFile}
        onStageFile={onStageFile}
        onUnstageFile={onUnstageFile}
        onDiscardFile={onDiscardFile}
        onClose={() => setContextMenu(null)}
        unstageDisabled={unsupported.unstage}
        discardDisabled={unsupported.discard}
        unsupportedTooltip={unsupportedTooltip}
      />

      <GitPRDialog isOpen={showPrDialog} onClose={() => setShowPrDialog(false)} onPullRequest={onPullRequest} />
    </div>
  );
}

export default GitSidebarPanel;
