import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  CheckSquare,
  GitBranch,
  MinusSquare,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Square,
  Trash2,
  PlusSquare,
  Plus,
} from 'lucide-react';
import { showThemedPrompt } from './ThemedDialog';
import type { GitBranchesState } from '../hooks/useGitWorkspace';
import { copyToClipboard } from '../utils/clipboard';
import type { FileSection } from '../types/git-types';
import { FILE_SECTIONS, selectionKey, parseSelectionKey } from '../types/git-types';
import type { GitFile, GitStatusData } from '../types/git-types';

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
  onPreviewFile: (section: FileSection, path: string) => void;
  onStageSelected: () => void;
  onUnstageSelected: () => void;
  onDiscardSelected: () => void;
  onStageFile: (path: string) => void;
  onUnstageFile: (path: string) => void;
  onDiscardFile: (path: string) => void;
  onSectionAction: (section: FileSection) => void;
  onOpenFile?: (path: string) => void;
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
  onPreviewFile,
  onStageSelected,
  onUnstageSelected,
  onDiscardSelected,
  onStageFile,
  onUnstageFile,
  onDiscardFile,
  onSectionAction,
  onOpenFile,
}: GitSidebarPanelProps): JSX.Element {
  const contextMenuRef = useRef<HTMLDivElement>(null);
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    section: FileSection;
    file: GitFile;
  } | null>(null);

  useEffect(() => {
    if (!contextMenu) {
      return;
    }

    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (target && contextMenuRef.current?.contains(target)) {
        return;
      }
      setContextMenu(null);
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu(null);
      }
    };

    window.addEventListener('mousedown', handlePointerDown);
    window.addEventListener('keydown', handleEscape);
    return () => {
      window.removeEventListener('mousedown', handlePointerDown);
      window.removeEventListener('keydown', handleEscape);
    };
  }, [contextMenu]);

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
  const visibleSections = FILE_SECTIONS.filter((section) => (gitStatus?.[section.id].length ?? 0) > 0);
  const hiddenSectionCount = FILE_SECTIONS.length - visibleSections.length;
  const selectedEntries = Array.from(selectedFiles)
    .map(parseSelectionKey)
    .filter((entry): entry is { section: FileSection; path: string } => entry !== null);
  const stageSelectedCount = new Set(
    selectedEntries
      .filter((entry) => entry.section !== 'staged')
      .map((entry) => entry.path)
  ).size;
  const unstageSelectedCount = new Set(
    selectedEntries
      .filter((entry) => entry.section === 'staged')
      .map((entry) => entry.path)
  ).size;
  const discardSelectedCount = new Set(
    selectedEntries
      .filter((entry) => entry.section === 'modified' || entry.section === 'deleted')
      .map((entry) => entry.path)
  ).size;

  const handleCreateBranch = async () => {
    const value = await showThemedPrompt('Enter a name for the new branch:', { title: 'Create Branch', defaultValue: '', placeholder: 'branch-name' });
    if (!value) {
      return;
    }
    onCreateBranch(value);
  };

  return (
    <div className="git-sidebar-panel">
      <div className="git-sidebar-header">
            <div className="git-sidebar-toolbar-row">
              <label className="git-branch-select-wrap" htmlFor="git-branch-select">
                <span className="branch-icon"><GitBranch size={14} /></span>
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
                  <span className="clean"><CheckCircle2 size={14} />Clean</span>
                ) : (
                  <span className="dirty"><RefreshCw size={14} />Changes</span>
                )}
                {gitStatus?.ahead && gitStatus.ahead > 0 ? <span className="ahead"><ArrowUp size={12} />{gitStatus.ahead}</span> : null}
                {gitStatus?.behind && gitStatus.behind > 0 ? <span className="behind"><ArrowDown size={12} />{gitStatus.behind}</span> : null}
              </div>
              <div className="git-sidebar-toolbar-actions">
                <button
                  type="button"
                  className="git-header-action-btn"
                  onClick={onPull}
                  disabled={isActing || isLoading}
                >
                  Pull
                </button>
                <button
                  type="button"
                  className="git-header-action-btn"
                  onClick={onPush}
                  disabled={isActing || isLoading}
                >
                  Push
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
              disabled={!hasStagedFiles || isActing}
              placeholder={hasStagedFiles ? 'Write commit message…' : 'Stage files to write a commit message'}
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
                  <button
                    className="sidebar-action-btn compact"
                    onClick={onStageSelected}
                    disabled={isActing}
                  >
                    <PlusSquare size={13} />
                    Stage {stageSelectedCount}
                  </button>
                )}
                {unstageSelectedCount > 0 && (
                  <button
                    className="sidebar-action-btn compact"
                    onClick={onUnstageSelected}
                    disabled={isActing}
                  >
                    <MinusSquare size={13} />
                    Unstage {unstageSelectedCount}
                  </button>
                )}
                {discardSelectedCount > 0 && (
                  <button
                    className="sidebar-action-btn compact danger"
                    onClick={onDiscardSelected}
                    disabled={isActing}
                  >
                    <Trash2 size={13} />
                    {selectedEntries.some((entry) => entry.section === 'deleted') ? 'Restore/Discard' : 'Discard'} {discardSelectedCount}
                  </button>
                )}
                <button
                  className="sidebar-action-btn compact ghost"
                  onClick={onClearSelection}
                  disabled={isActing}
                >
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
            {hiddenSectionCount > 0 ? (
              <div className="git-sidebar-hidden-sections-note">
                Hiding {hiddenSectionCount} empty {hiddenSectionCount === 1 ? 'section' : 'sections'}
              </div>
            ) : null}
            {visibleSections.map((section) => {
              const files = gitStatus?.[section.id] ?? [];
              const allSelected = files.length > 0 && files.every((file) => selectedFiles.has(selectionKey(section.id, file.path)));
              return (
                <div key={section.id} className="git-sidebar-file-section">
                  <div className="git-sidebar-file-section-header">
                    <div className="git-sidebar-file-section-title">
                      <h4>{section.title}</h4>
                      <span>{files.length}</span>
                    </div>
                    <div className="git-sidebar-file-section-actions">
                      <button
                        className="git-section-icon-btn"
                        onClick={() => onToggleSectionSelection(section.id)}
                        title={allSelected ? `Deselect all ${section.title.toLowerCase()}` : `Select all ${section.title.toLowerCase()}`}
                      >
                        {allSelected ? <CheckSquare size={14} /> : <Square size={14} />}
                      </button>
                      {files.length > 0 && (
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
                    {files.map((file) => {
                      const key = selectionKey(section.id, file.path);
                      const isSelected = selectedFiles.has(key);
                      const isPreviewing = activeDiffSelectionKey === key;
                      return (
                        <div
                          key={key}
                          className={`git-sidebar-file-row ${isPreviewing ? 'previewing' : ''} ${isSelected ? 'selected' : ''}`}
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
                          <label className="git-sidebar-file-select">
                            <input
                              type="checkbox"
                              checked={isSelected}
                              onChange={() => onToggleFileSelection(section.id, file.path)}
                            />
                          </label>
                          <button
                            className="git-sidebar-file-open"
                            onClick={() => onPreviewFile(section.id, file.path)}
                          >
                            <span className="git-sidebar-file-path">{file.path}</span>
                            <span className="git-sidebar-file-status">{file.status}</span>
                          </button>
                          <div className="git-sidebar-row-actions">
                            {section.id === 'staged' ? (
                              <button
                                className="git-row-icon-btn"
                                onClick={() => onUnstageFile(file.path)}
                                title="Unstage file"
                                disabled={isActing}
                              >
                                <MinusSquare size={14} />
                              </button>
                            ) : (
                              <button
                                className="git-row-icon-btn"
                                onClick={() => onStageFile(file.path)}
                                title="Stage file"
                                disabled={isActing}
                              >
                                <PlusSquare size={14} />
                              </button>
                            )}
                            {(section.id === 'modified' || section.id === 'untracked' || section.id === 'deleted') && (
                              <button
                                className="git-row-icon-btn danger"
                                onClick={() => onDiscardFile(file.path)}
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
                </div>
              );
            })}
          </div>

      {contextMenu && typeof document !== 'undefined'
        ? createPortal(
            <div
              ref={contextMenuRef}
              className="file-tree-context-menu git-context-menu"
              style={{ left: `${contextMenu.x}px`, top: `${contextMenu.y}px` }}
              onClick={(event) => event.stopPropagation()}
            >
              <button className="file-tree-context-item" onClick={() => { setContextMenu(null); onPreviewFile(contextMenu.section, contextMenu.file.path); }}>
                Preview diff
              </button>
              {onOpenFile && contextMenu.section !== 'deleted' && (
                <button className="file-tree-context-item" onClick={() => { setContextMenu(null); onOpenFile(contextMenu.file.path); }}>Open in editor</button>
              )}
              {contextMenu.section !== 'deleted' && (
                <>
                  <div className="file-tree-context-separator" />
                  <button className="file-tree-context-item" onClick={() => { copyToClipboard(contextMenu.file.path); setContextMenu(null); }}>Copy relative path</button>
                  {workspaceRoot && (
                    <button className="file-tree-context-item" onClick={() => { copyToClipboard(`${workspaceRoot.replace(/\/+$/, '')}/${contextMenu.file.path}`); setContextMenu(null); }}>Copy absolute path</button>
                  )}
                </>
              )}
              <div className="file-tree-context-separator" />
              {contextMenu.section === 'staged' ? (
                <button className="file-tree-context-item" onClick={() => { setContextMenu(null); onUnstageFile(contextMenu.file.path); }}>
                  Unstage
                </button>
              ) : (
                <button className="file-tree-context-item" onClick={() => { setContextMenu(null); onStageFile(contextMenu.file.path); }}>
                  Stage
                </button>
              )}
              <button className="file-tree-context-item danger" onClick={() => { setContextMenu(null); onDiscardFile(contextMenu.file.path); }}>
                {contextMenu.section === 'deleted' ? 'Restore' : 'Delete'}
              </button>
            </div>,
            document.body
          )
        : null}
    </div>
  );
}

export default GitSidebarPanel;
