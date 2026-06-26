import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  CheckSquare,
  ExternalLink,
  GitBranch,
  MinusSquare,
  RefreshCw,
  Search,
  ShieldCheck,
  Sparkles,
  Square,
  Trash2,
  PlusSquare,
  Plus,
  X,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { MAX_FILES_PER_SECTION, MAX_FILES_INITIAL, LOAD_MORE_INCREMENT } from '../constants/git-constants';
import type { FileSection, GitBranchesState, GitFile, GitStatusData } from '../types/git-types';
import { FILE_SECTIONS, selectionKey, parseSelectionKey } from '../types/git-types';
import { copyToClipboard } from '../utils/clipboard';
import { getStatusInfo } from '../utils/git';
import { splitPath } from '../utils/format';
import { ContextMenu } from '@sprout/ui';
import { showThemedPrompt } from './ThemedDialog';

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

  // Tracks the last-clicked index per section for shift+click range selection
  const anchorRef = useRef<Map<string, number>>(new Map());

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
  const [prTitle, setPrTitle] = useState('');
  const [prBody, setPrBody] = useState('');
  const [prBase, setPrBase] = useState('');
  const [prDraft, setPrDraft] = useState(false);
  const [isCreatingPr, setIsCreatingPr] = useState(false);
  const [prSuccessUrl, setPrSuccessUrl] = useState<string | null>(null);
  const [prError, setPrError] = useState<string | null>(null);

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
        const newCount = Math.min(current + LOAD_MORE_INCREMENT, MAX_FILES_PER_SECTION, files.length);
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
  const totalFiles =
    (gitStatus?.staged.length ?? 0) +
    (gitStatus?.modified.length ?? 0) +
    (gitStatus?.untracked.length ?? 0) +
    (gitStatus?.deleted.length ?? 0);
  const visibleSections = FILE_SECTIONS.filter((section) => filteredBySection[section.id].length > 0);
  const hiddenSectionCount = FILE_SECTIONS.length - visibleSections.length;
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

  const handleOpenPrDialog = () => {
    setPrTitle('');
    setPrBody('');
    // Leave base empty so the backend infers the repo's default branch.
    // Seeding with gitBranches.current would send base==head for feature
    // branches, causing PR creation to fail or target the wrong base.
    setPrBase('');
    setPrDraft(false);
    setIsCreatingPr(false);
    setPrSuccessUrl(null);
    setPrError(null);
    setShowPrDialog(true);
  };

  const handleCreatePr = async () => {
    if (!prTitle.trim()) return;
    setIsCreatingPr(true);
    setPrError(null);
    setPrSuccessUrl(null);
    try {
      const result = await onPullRequest({
        title: prTitle.trim(),
        body: prBody || undefined,
        base: prBase || undefined,
        draft: prDraft,
      });
      setPrSuccessUrl(result.url);
    } catch (err) {
      setPrError(err instanceof Error ? err.message : String(err));
    } finally {
      setIsCreatingPr(false);
    }
  };

  return (
    <div className="git-sidebar-panel">
      <div className="git-sidebar-header">
        <div className="git-sidebar-toolbar-row">
          <label className="git-branch-select-wrap" htmlFor="git-branch-select">
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
            onClick={handleCreateBranch}
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
              onClick={handleOpenPrDialog}
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

      <div className="git-sidebar-commit-box">
        <div className="git-sidebar-commit-header">
          <h4>Commit Message</h4>
          <button
            className="git-generate-icon-btn"
            onClick={onGenerateCommitMessage}
            disabled={!hasStagedFiles || isGeneratingCommitMessage || isActing}
            title="Generate commit message with AI"
            aria-label="Generate commit message"
          >
            <Sparkles size={14} />
          </button>
        </div>
        <textarea
          value={commitMessage}
          onChange={(e) => onCommitMessageChange(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && hasStagedFiles && commitMessage.trim() && !isActing) {
              e.preventDefault();
              onCommit();
            }
          }}
          disabled={!hasStagedFiles || isActing}
          placeholder={
            hasStagedFiles ? 'Write commit message… (⌘/Ctrl+Enter to commit)' : 'Stage files to write a commit message'
          }
          aria-label="Commit message"
          className="git-sidebar-commit-input"
          rows={3}
        />
        <div className="git-sidebar-primary-actions">
          <button
            className="sidebar-action-btn primary"
            onClick={onCommit}
            disabled={!hasStagedFiles || !commitMessage.trim() || isActing}
          >
            Commit Changes
          </button>
          <button
            className="sidebar-action-btn"
            onClick={onRunReview}
            disabled={!hasStagedFiles || isReviewLoading || isActing}
          >
            <ShieldCheck size={14} />
            {isReviewLoading ? 'Reviewing…' : 'Review'}
          </button>
        </div>
      </div>

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
              <button className="sidebar-action-btn compact" onClick={onUnstageSelected} disabled={isActing}>
                <MinusSquare size={13} />
                Unstage {unstageSelectedCount}
              </button>
            )}
            {discardSelectedCount > 0 && (
              <button className="sidebar-action-btn compact danger" onClick={onDiscardSelected} disabled={isActing}>
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

      <div className="git-sidebar-file-sections">
        <div className="git-sidebar-section-header">
          <h4>Working Tree Files</h4>
          <span className="git-sidebar-section-subtitle">Files to stage for commit</span>
        </div>
        {totalFiles > 0 ? (
          <div className="git-sidebar-filter">
            <Search size={12} className="git-sidebar-filter-icon" aria-hidden="true" />
            <input
              type="text"
              className="git-sidebar-filter-input"
              value={fileFilter}
              onChange={(e) => setFileFilter(e.target.value)}
              placeholder={`Filter ${totalFiles} file${totalFiles === 1 ? '' : 's'}…`}
              aria-label="Filter changed files"
            />
            {fileFilter && (
              <button
                type="button"
                className="git-sidebar-filter-clear"
                onClick={() => setFileFilter('')}
                title="Clear filter"
                aria-label="Clear filter"
              >
                <X size={12} />
              </button>
            )}
          </div>
        ) : null}
        {totalFiles === 0 ? (
          <div className="git-sidebar-clean-state">
            <CheckCircle2 size={18} />
            <div>
              <div className="git-sidebar-clean-state-title">Working tree clean</div>
              <div className="git-sidebar-clean-state-hint">Nothing to commit. Edit a file to get started.</div>
            </div>
          </div>
        ) : normalizedFilter && matchedFileCount === 0 ? (
          <div className="git-sidebar-clean-state">
            <Search size={18} />
            <div>
              <div className="git-sidebar-clean-state-title">No files match “{fileFilter}”</div>
              <button type="button" className="git-sidebar-clean-state-action" onClick={() => setFileFilter('')}>
                Clear filter
              </button>
            </div>
          </div>
        ) : hiddenSectionCount > 0 && !normalizedFilter ? (
          <div className="git-sidebar-hidden-sections-note">
            Hiding {hiddenSectionCount} empty {hiddenSectionCount === 1 ? 'section' : 'sections'}
          </div>
        ) : null}
        {visibleSections.map((section) => {
          const files = filteredBySection[section.id];
          const allFilesInSection = gitStatus?.[section.id] ?? [];
          const isFiltered = normalizedFilter.length > 0;
          const allSelected =
            files.length > 0 && files.every((file) => selectedFiles.has(selectionKey(section.id, file.path)));
          return (
            <div key={section.id} className="git-sidebar-file-section">
              <div className="git-sidebar-file-section-header">
                <div className="git-sidebar-file-section-title">
                  <h4>{section.title}</h4>
                  <span className="git-sidebar-section-count">{files.length}</span>
                </div>
                <div className="git-sidebar-file-section-actions">
                  <button
                    className="git-section-icon-btn"
                    onClick={() => {
                      if (isFiltered && onSelectFiles) {
                        // When filtered, toggle selection over the visible subset only.
                        const visibleKeys = files.map((f) => selectionKey(section.id, f.path));
                        if (allSelected) {
                          // Remove visible keys from current selection.
                          const next = Array.from(selectedFiles).filter((k) => !visibleKeys.includes(k));
                          onSelectFiles(next);
                        } else {
                          const next = Array.from(new Set([...Array.from(selectedFiles), ...visibleKeys]));
                          onSelectFiles(next);
                        }
                        return;
                      }
                      onToggleSectionSelection(section.id);
                    }}
                    title={
                      allSelected
                        ? `Deselect all ${isFiltered ? 'visible ' : ''}${section.title.toLowerCase()}`
                        : `Select all ${isFiltered ? 'visible ' : ''}${section.title.toLowerCase()}`
                    }
                  >
                    {allSelected ? <CheckSquare size={14} /> : <Square size={14} />}
                  </button>
                  {allFilesInSection.length > 0 && !isFiltered && (
                    <button
                      className="git-section-icon-btn"
                      onClick={() => onSectionAction(section.id)}
                      title={section.id === 'staged' ? 'Unstage all in section' : 'Stage all in section'}
                    >
                      {section.id === 'staged' ? <MinusSquare size={14} /> : <PlusSquare size={14} />}
                    </button>
                  )}
                </div>
              </div>
              <div className="git-sidebar-file-list">
                {files.slice(0, visibleCounts[section.id]).map((file, index) => {
                  const key = selectionKey(section.id, file.path);
                  const isSelected = selectedFiles.has(key);
                  const isPreviewing = activeDiffSelectionKey === key;
                  const { dir, name } = splitPath(file.path);
                  const statusInfo = getStatusInfo(file.status);
                  return (
                    <div
                      key={key}
                      className={`git-sidebar-file-row ${isPreviewing ? 'previewing' : ''} ${isSelected ? 'selected' : ''}`}
                      role="button"
                      tabIndex={0}
                      title={file.path}
                      onClick={(event) => {
                        if (isActing) return;
                        if (event.shiftKey) {
                          const anchor = anchorRef.current.get(section.id) ?? 0;
                          const from = Math.min(anchor, index);
                          const to = Math.max(anchor, index);
                          const rangeKeys = files.slice(from, to + 1).map((f) => selectionKey(section.id, f.path));
                          if (onSelectFiles) {
                            onSelectFiles(rangeKeys);
                          }
                        } else if (event.ctrlKey || event.metaKey) {
                          anchorRef.current.set(section.id, index);
                          onToggleFileSelection(section.id, file.path);
                        } else {
                          anchorRef.current.set(section.id, index);
                          if (onSelectFiles) {
                            onSelectFiles([key]);
                          }
                        }
                        onPreviewFile(section.id, file.path);
                      }}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault();
                          if (!isActing) {
                            anchorRef.current.set(section.id, index);
                            onToggleFileSelection(section.id, file.path);
                            onPreviewFile(section.id, file.path);
                          }
                        }
                      }}
                      onContextMenu={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        setContextMenu({
                          x: event.clientX,
                          y: event.clientY,
                          section: section.id,
                          file,
                        });
                      }}
                    >
                      <span className="git-sidebar-file-path">
                        {dir && <span className="git-sidebar-file-dir">{dir}</span>}
                        <span className="git-sidebar-file-name">{name}</span>
                      </span>
                      <span
                        className={`git-sidebar-file-status ${statusInfo.className}`}
                        aria-label={`status ${file.status}`}
                      >
                        {statusInfo.label}
                      </span>
                      <div className="git-sidebar-row-actions">
                        {section.id === 'staged' ? (
                          <button
                            className="git-row-icon-btn"
                            onClick={(e) => {
                              e.stopPropagation();
                              onUnstageFile(file.path);
                            }}
                            title="Unstage file"
                            disabled={isActing}
                          >
                            <MinusSquare size={14} />
                          </button>
                        ) : (
                          <button
                            className="git-row-icon-btn"
                            onClick={(e) => {
                              e.stopPropagation();
                              onStageFile(file.path);
                            }}
                            title="Stage file"
                            disabled={isActing}
                          >
                            <PlusSquare size={14} />
                          </button>
                        )}
                        {(section.id === 'modified' || section.id === 'untracked' || section.id === 'deleted') && (
                          <button
                            className="git-row-icon-btn danger"
                            onClick={(e) => {
                              e.stopPropagation();
                              onDiscardFile(file.path);
                            }}
                            title={section.id === 'deleted' ? 'Restore file' : 'Discard file changes'}
                            disabled={isActing}
                          >
                            <Trash2 size={14} />
                          </button>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
              {files.length > visibleCounts[section.id] && visibleCounts[section.id] < MAX_FILES_PER_SECTION && (
                <button className="git-sidebar-load-more" onClick={() => handleLoadMore(section.id)}>
                  Show more ({files.length - visibleCounts[section.id]} more files)
                </button>
              )}
              {files.length > MAX_FILES_PER_SECTION && (
                <div className="git-sidebar-file-limit-note">
                  Showing up to {MAX_FILES_PER_SECTION} files. Use git status or command line to see all files.
                </div>
              )}
            </div>
          );
        })}
      </div>

      {contextMenu && (
        <ContextMenu isOpen x={contextMenu.x} y={contextMenu.y} onClose={() => setContextMenu(null)}>
          <button
            className="context-menu-item"
            onClick={() => {
              setContextMenu(null);
              onPreviewFile(contextMenu.section, contextMenu.file.path);
            }}
          >
            Preview diff
          </button>
          {onOpenFile && contextMenu.section !== 'deleted' && (
            <button
              className="context-menu-item"
              onClick={() => {
                setContextMenu(null);
                onOpenFile(contextMenu.file.path);
              }}
            >
              Open in editor
            </button>
          )}
          {contextMenu.section !== 'deleted' && (
            <>
              <div className="context-menu-divider" />
              <button
                className="context-menu-item"
                onClick={() => {
                  copyToClipboard(contextMenu.file.path);
                  setContextMenu(null);
                }}
              >
                Copy relative path
              </button>
              {workspaceRoot && (
                <button
                  className="context-menu-item"
                  onClick={() => {
                    copyToClipboard(`${workspaceRoot.replace(/\/+$/, '')}/${contextMenu.file.path}`);
                    setContextMenu(null);
                  }}
                >
                  Copy absolute path
                </button>
              )}
            </>
          )}
          <div className="context-menu-divider" />
          {contextMenu.section === 'staged' ? (
            <button
              className="context-menu-item"
              onClick={() => {
                setContextMenu(null);
                onUnstageFile(contextMenu.file.path);
              }}
            >
              Unstage
            </button>
          ) : (
            <button
              className="context-menu-item"
              onClick={() => {
                setContextMenu(null);
                onStageFile(contextMenu.file.path);
              }}
            >
              Stage
            </button>
          )}
          <button
            className="context-menu-item danger"
            onClick={() => {
              setContextMenu(null);
              onDiscardFile(contextMenu.file.path);
            }}
          >
            {contextMenu.section === 'deleted' ? 'Restore' : 'Delete'}
          </button>
        </ContextMenu>
      )}

      {showPrDialog && (
        <div
          className="themed-dialog-overlay"
          onClick={() => {
            if (!isCreatingPr) setShowPrDialog(false);
          }}
        >
          <div
            className="themed-dialog-card"
            style={{ width: 'min(500px, 100%)' }}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="themed-dialog-accent-bar themed-dialog-accent-bar--info" />
            <div className="themed-dialog-header">
              <span className="themed-dialog-icon themed-dialog-icon--info">
                <ExternalLink size={16} />
              </span>
              <h2 className="themed-dialog-title">Create Pull Request</h2>
            </div>

            {prSuccessUrl ? (
              <>
                <div className="themed-dialog-body" style={{ textAlign: 'center' }}>
                  <div style={{ marginBottom: 8, color: 'var(--accent-success)', fontWeight: 600 }}>
                    Pull request created!
                  </div>
                  <a
                    href={prSuccessUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ color: 'var(--accent-primary)', wordBreak: 'break-all' }}
                  >
                    {prSuccessUrl}
                  </a>
                </div>
                <div className="themed-dialog-footer">
                  <button
                    type="button"
                    className="themed-dialog-btn themed-dialog-btn--primary"
                    onClick={() => setShowPrDialog(false)}
                  >
                    Done
                  </button>
                </div>
              </>
            ) : (
              <>
                <div className="themed-dialog-body">
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                    <div>
                      <label
                        style={{
                          display: 'block',
                          fontSize: 11,
                          fontWeight: 600,
                          color: 'var(--text-tertiary)',
                          textTransform: 'uppercase',
                          letterSpacing: '0.06em',
                          marginBottom: 4,
                        }}
                      >
                        Title *
                      </label>
                      <input
                        type="text"
                        className="themed-dialog-input"
                        value={prTitle}
                        onChange={(e) => setPrTitle(e.target.value)}
                        placeholder="PR title"
                        autoFocus
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' && prTitle.trim() && !isCreatingPr) {
                            e.preventDefault();
                            handleCreatePr();
                          }
                        }}
                      />
                    </div>
                    <div>
                      <label
                        style={{
                          display: 'block',
                          fontSize: 11,
                          fontWeight: 600,
                          color: 'var(--text-tertiary)',
                          textTransform: 'uppercase',
                          letterSpacing: '0.06em',
                          marginBottom: 4,
                        }}
                      >
                        Description
                      </label>
                      <textarea
                        className="themed-dialog-input"
                        value={prBody}
                        onChange={(e) => setPrBody(e.target.value)}
                        placeholder="Optional description…"
                        rows={4}
                        style={{ resize: 'vertical', minHeight: 80, lineHeight: 1.55 }}
                      />
                    </div>
                    <div>
                      <label
                        style={{
                          display: 'block',
                          fontSize: 11,
                          fontWeight: 600,
                          color: 'var(--text-tertiary)',
                          textTransform: 'uppercase',
                          letterSpacing: '0.06em',
                          marginBottom: 4,
                        }}
                      >
                        Base branch
                      </label>
                      <input
                        type="text"
                        className="themed-dialog-input"
                        value={prBase}
                        onChange={(e) => setPrBase(e.target.value)}
                        placeholder="main"
                      />
                    </div>
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                      <input
                        type="checkbox"
                        checked={prDraft}
                        onChange={(e) => setPrDraft(e.target.checked)}
                        style={{ accentColor: 'var(--accent-primary)', width: 16, height: 16 }}
                      />
                      <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>Draft PR</span>
                    </label>
                  </div>
                </div>

                {prError && (
                  <div
                    style={{
                      padding: 'var(--space-4) var(--space-4)',
                      color: 'var(--accent-error)',
                      fontSize: 13,
                      background: 'var(--color-error-bg)',
                      borderRadius: 'var(--radius-md)',
                    }}
                  >
                    {prError}
                  </div>
                )}

                <div className="themed-dialog-footer">
                  <button
                    type="button"
                    className="themed-dialog-btn"
                    onClick={() => setShowPrDialog(false)}
                    disabled={isCreatingPr}
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    className="themed-dialog-btn themed-dialog-btn--primary"
                    onClick={handleCreatePr}
                    disabled={!prTitle.trim() || isCreatingPr}
                  >
                    {isCreatingPr ? 'Creating…' : 'Create'}
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

export default GitSidebarPanel;
