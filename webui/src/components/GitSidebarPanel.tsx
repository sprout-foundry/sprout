import React, { useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  CheckSquare,
  GitBranch,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Square,
  Undo2,
  Trash2,
  PlusSquare,
} from 'lucide-react';

export interface GitFile {
  path: string;
  status: string;
  changes?: {
    additions: number;
    deletions: number;
  };
}

export interface GitStatusData {
  branch: string;
  ahead: number;
  behind: number;
  staged: GitFile[];
  modified: GitFile[];
  untracked: GitFile[];
  deleted: GitFile[];
  renamed: GitFile[];
  clean: boolean;
}

type FileSection = 'staged' | 'modified' | 'untracked' | 'deleted';

interface GitSidebarPanelProps {
  gitStatus: GitStatusData | null;
  selectedFiles: Set<string>;
  activeDiffSelectionKey: string | null;
  commitMessage: string;
  isLoading: boolean;
  isActing: boolean;
  isGeneratingCommitMessage: boolean;
  isReviewLoading: boolean;
  actionError: string | null;
  onCommitMessageChange: (value: string) => void;
  onGenerateCommitMessage: () => void;
  onCommit: () => void;
  onRunReview: () => void;
  onToggleFileSelection: (section: FileSection, path: string) => void;
  onToggleSectionSelection: (section: FileSection) => void;
  onPreviewFile: (section: FileSection, path: string) => void;
  onStageFile: (path: string) => void;
  onUnstageFile: (path: string) => void;
  onDiscardFile: (path: string) => void;
  onSectionAction: (section: FileSection) => void;
}

const selectionKey = (section: FileSection, path: string): string => `${section}:${path}`;

const FILE_SECTIONS: Array<{ id: FileSection; title: string }> = [
  { id: 'staged', title: 'Staged' },
  { id: 'modified', title: 'Modified' },
  { id: 'untracked', title: 'Untracked' },
  { id: 'deleted', title: 'Deleted' },
];

const GitSidebarPanel: React.FC<GitSidebarPanelProps> = ({
  gitStatus,
  selectedFiles,
  activeDiffSelectionKey,
  commitMessage,
  isLoading,
  isActing,
  isGeneratingCommitMessage,
  isReviewLoading,
  actionError,
  onCommitMessageChange,
  onGenerateCommitMessage,
  onCommit,
  onRunReview,
  onToggleFileSelection,
  onToggleSectionSelection,
  onPreviewFile,
  onStageFile,
  onUnstageFile,
  onDiscardFile,
  onSectionAction,
}) => {
  const panelRef = useRef<HTMLDivElement>(null);
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

    const close = () => setContextMenu(null);
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu(null);
      }
    };

    window.addEventListener('click', close);
    window.addEventListener('contextmenu', close);
    window.addEventListener('keydown', handleEscape);
    return () => {
      window.removeEventListener('click', close);
      window.removeEventListener('contextmenu', close);
      window.removeEventListener('keydown', handleEscape);
    };
  }, [contextMenu]);

  if (isLoading) {
    return <div className="empty">Loading git status…</div>;
  }

  if (!gitStatus) {
    return <div className="empty">No git repository found</div>;
  }

  const hasStagedFiles = gitStatus.staged.length > 0;

  return (
    <div className="git-sidebar-panel" ref={panelRef}>
      <div className="git-sidebar-header">
        <div className="git-sidebar-branch">
          <span className="branch-icon"><GitBranch size={14} /></span>
          <span className="branch-name">{gitStatus.branch}</span>
          {gitStatus.ahead > 0 ? <span className="ahead"><ArrowUp size={12} />{gitStatus.ahead}</span> : null}
          {gitStatus.behind > 0 ? <span className="behind"><ArrowDown size={12} />{gitStatus.behind}</span> : null}
        </div>
        <div className="git-sidebar-cleanliness">
          {gitStatus.clean ? (
            <span className="clean"><CheckCircle2 size={14} />Clean</span>
          ) : (
            <span className="dirty"><RefreshCw size={14} />Changes</span>
          )}
        </div>
      </div>

      <div className="git-sidebar-commit-box">
        <div className="git-sidebar-commit-header">
          <h4>Commit</h4>
          <button
            className="git-generate-icon-btn"
            onClick={onGenerateCommitMessage}
            disabled={!hasStagedFiles || isGeneratingCommitMessage || isActing}
            title="Generate"
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
            Commit
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

      <div className="git-sidebar-file-sections">
        {FILE_SECTIONS.map((section) => {
          const files = gitStatus[section.id];
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
                      {section.id === 'staged' ? <Undo2 size={14} /> : <PlusSquare size={14} />}
                    </button>
                  )}
                </div>
              </div>
              {files.length === 0 ? (
                <div className="git-sidebar-empty-group">No files</div>
              ) : (
                <div className="git-sidebar-file-list">
                  {files.map((file) => {
                    const key = selectionKey(section.id, file.path);
                    const isSelected = selectedFiles.has(key);
                    const isPreviewing = activeDiffSelectionKey === key;
                    return (
                      <div
                        key={key}
                        className={`git-sidebar-file-row ${isPreviewing ? 'previewing' : ''}`}
                        onContextMenu={(event) => {
                          event.preventDefault();
                          const rect = panelRef.current?.getBoundingClientRect();
                          setContextMenu({
                            x: event.clientX - (rect?.left || 0),
                            y: event.clientY - (rect?.top || 0),
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
                              <Undo2 size={14} />
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
              )}
            </div>
          );
        })}
      </div>
      {contextMenu ? (
        <div
          className="file-tree-context-menu git-context-menu"
          style={{ left: `${contextMenu.x}px`, top: `${contextMenu.y}px` }}
          onClick={(event) => event.stopPropagation()}
        >
          <button className="file-tree-context-item" onClick={() => onPreviewFile(contextMenu.section, contextMenu.file.path)}>
            Preview diff
          </button>
          {contextMenu.section === 'staged' ? (
            <button className="file-tree-context-item" onClick={() => onUnstageFile(contextMenu.file.path)}>
              Unstage
            </button>
          ) : (
            <button className="file-tree-context-item" onClick={() => onStageFile(contextMenu.file.path)}>
              Stage
            </button>
          )}
          <button className="file-tree-context-item danger" onClick={() => onDiscardFile(contextMenu.file.path)}>
            {contextMenu.section === 'deleted' ? 'Restore' : 'Delete'}
          </button>
        </div>
      ) : null}
    </div>
  );
};

export default GitSidebarPanel;
