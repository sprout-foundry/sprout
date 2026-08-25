import { CheckCircle2, CheckSquare, MinusSquare, PlusSquare, Search, Square, Trash2, X } from 'lucide-react';
import { useRef } from 'react';
import { MAX_FILES_PER_SECTION } from '../../constants/git-constants';
import type { FileSection, GitFile, GitStatusData } from '../../types/git-types';
import { FILE_SECTIONS, selectionKey } from '../../types/git-types';
import { splitPath } from '../../utils/format';
import { getStatusInfo } from '../../utils/git';

export interface GitFileSectionsProps {
  gitStatus: GitStatusData;
  filteredBySection: Record<FileSection, GitFile[]>;
  visibleCounts: Record<FileSection, number>;
  selectedFiles: Set<string>;
  activeDiffSelectionKey: string | null;
  isActing: boolean;
  fileFilter: string;
  normalizedFilter: string;
  totalFiles: number;
  matchedFileCount: number;
  hiddenSectionCount: number;
  onSelectFiles?: (keys: string[]) => void;
  onToggleFileSelection: (section: FileSection, path: string) => void;
  onToggleSectionSelection: (section: FileSection) => void;
  onPreviewFile: (section: FileSection, path: string) => void;
  onStageFile: (path: string) => void;
  onUnstageFile: (path: string) => void;
  onDiscardFile: (path: string) => void;
  onSectionAction: (section: FileSection) => void;
  onLoadMore: (section: FileSection) => void;
  onSetFileFilter: (value: string) => void;
  onOpenContextMenu: (section: FileSection, file: GitFile, x: number, y: number) => void;
  /** Disable the per-row/section Unstage buttons (browser mode). */
  unstageDisabled?: boolean;
  /** Disable the per-row Discard buttons (browser mode). */
  discardDisabled?: boolean;
  /** Tooltip for disabled-for-browser buttons. */
  unsupportedTooltip?: string;
}

function GitFileSections({
  gitStatus,
  filteredBySection,
  visibleCounts,
  selectedFiles,
  activeDiffSelectionKey,
  isActing,
  fileFilter,
  normalizedFilter,
  totalFiles,
  matchedFileCount,
  hiddenSectionCount,
  onSelectFiles,
  onToggleFileSelection,
  onToggleSectionSelection,
  onPreviewFile,
  onStageFile,
  onUnstageFile,
  onDiscardFile,
  onSectionAction,
  onLoadMore,
  onSetFileFilter,
  onOpenContextMenu,
  unstageDisabled = false,
  discardDisabled = false,
  unsupportedTooltip,
}: GitFileSectionsProps) {
  // Tracks the last-clicked index per section for shift+click range selection
  const anchorRef = useRef<Map<string, number>>(new Map());

  const visibleSections = FILE_SECTIONS.filter((section) => filteredBySection[section.id].length > 0);

  return (
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
            onChange={(e) => onSetFileFilter(e.target.value)}
            placeholder={`Filter ${totalFiles} file${totalFiles === 1 ? '' : 's'}…`}
            aria-label="Filter changed files"
          />
          {fileFilter && (
            <button
              type="button"
              className="git-sidebar-filter-clear"
              onClick={() => onSetFileFilter('')}
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
            <div className="git-sidebar-clean-state-title">No files match &ldquo;{fileFilter}&rdquo;</div>
            <button type="button" className="git-sidebar-clean-state-action" onClick={() => onSetFileFilter('')}>
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
                    disabled={section.id === 'staged' ? unstageDisabled : false}
                    title={
                      section.id === 'staged' && unstageDisabled
                        ? unsupportedTooltip
                        : section.id === 'staged'
                          ? 'Unstage all in section'
                          : 'Stage all in section'
                    }
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
                      onOpenContextMenu(section.id, file, event.clientX, event.clientY);
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
                          title={unstageDisabled ? unsupportedTooltip : 'Unstage file'}
                          disabled={isActing || unstageDisabled}
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
                          title={
                            discardDisabled
                              ? unsupportedTooltip
                              : section.id === 'deleted'
                                ? 'Restore file'
                                : 'Discard file changes'
                          }
                          disabled={isActing || discardDisabled}
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
              <button className="git-sidebar-load-more" onClick={() => onLoadMore(section.id)}>
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
  );
}

export default GitFileSections;
