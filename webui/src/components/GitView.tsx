import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
  FileEdit,
  Plus,
  Trash2,
  RefreshCw,
  HelpCircle,
  File,
  XCircle,
  CheckCircle2,
  Sparkles,
  GitBranch,
  ArrowUp,
  ArrowDown,
  AlertTriangle,
  PanelLeftClose,
  PanelLeftOpen,
  SplitSquareHorizontal,
} from 'lucide-react';
import './GitView.css';
import { ApiService } from '../services/api';

interface GitStatus {
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

interface GitFile {
  path: string;
  status: string;
  changes?: {
    additions: number;
    deletions: number;
  };
}

interface GitDiffResponse {
  message: string;
  path: string;
  has_staged: boolean;
  has_unstaged: boolean;
  staged_diff: string;
  unstaged_diff: string;
  diff: string;
}

interface GitViewProps {
  refreshToken?: number;
  onCommit?: (message: string, files: string[]) => void | Promise<unknown>;
  onAICommit?: () => void | Promise<unknown>;
  onStage?: (files: string[]) => void | Promise<unknown>;
  onUnstage?: (files: string[]) => void | Promise<unknown>;
  onDiscard?: (files: string[]) => void | Promise<unknown>;
  selectedFilePath?: string | null;
}

const GitView: React.FC<GitViewProps> = ({
  refreshToken = 0,
  onCommit,
  onAICommit,
  onStage,
  onUnstage,
  onDiscard,
  selectedFilePath = null
}) => {
  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [commitMessage, setCommitMessage] = useState('');
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
  const [activeDiffPath, setActiveDiffPath] = useState<string | null>(selectedFilePath);
  const [activeDiff, setActiveDiff] = useState<GitDiffResponse | null>(null);
  const [diffMode, setDiffMode] = useState<'combined' | 'staged' | 'unstaged'>('combined');
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffError, setDiffError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isActing, setIsActing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [filesPaneWidth, setFilesPaneWidth] = useState(440);
  const [filesPaneCollapsed, setFilesPaneCollapsed] = useState(false);
  const workspaceRef = useRef<HTMLDivElement>(null);
  const isResizingRef = useRef(false);
  const apiService = ApiService.getInstance();

  const loadGitStatus = useCallback(async () => {
    setIsLoading(true);
    setError(null);

    try {
      const data = await apiService.getGitStatus();
      if (data.message === 'success') {
        const status = data.status || {
          branch: '',
          ahead: 0,
          behind: 0,
          staged: [],
          modified: [],
          untracked: [],
          deleted: [],
          renamed: []
        };
        setGitStatus({
          branch: status.branch || '',
          ahead: status.ahead || 0,
          behind: status.behind || 0,
          staged: status.staged || [],
          modified: status.modified || [],
          untracked: status.untracked || [],
          deleted: status.deleted || [],
          renamed: status.renamed || [],
          clean: !(status.staged?.length || status.modified?.length || status.untracked?.length || status.deleted?.length || status.renamed?.length)
        });
      } else {
        throw new Error(data.message || 'Unknown error');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load git status');
    } finally {
      setIsLoading(false);
    }
  }, [apiService]);

  useEffect(() => {
    loadGitStatus();
  }, [loadGitStatus, refreshToken]);

  const loadDiff = useCallback(async (filePath: string) => {
    setIsDiffLoading(true);
    setDiffError(null);
    try {
      const response = await apiService.getGitDiff(filePath);
      setActiveDiff(response);
      if (response.has_staged && !response.has_unstaged) {
        setDiffMode('staged');
      } else if (!response.has_staged && response.has_unstaged) {
        setDiffMode('unstaged');
      } else {
        setDiffMode('combined');
      }
    } catch (err) {
      setDiffError(err instanceof Error ? err.message : 'Failed to load diff');
      setActiveDiff(null);
    } finally {
      setIsDiffLoading(false);
    }
  }, [apiService]);

  useEffect(() => {
    if (!selectedFilePath) {
      return;
    }
    setActiveDiffPath(selectedFilePath);
    loadDiff(selectedFilePath);
  }, [selectedFilePath, loadDiff]);

  const handleFileSelect = (filePath: string) => {
    setSelectedFiles(prev => {
      const newSet = new Set(prev);
      if (newSet.has(filePath)) {
        newSet.delete(filePath);
      } else {
        newSet.add(filePath);
      }
      return newSet;
    });
  };

  const handleSelectAll = () => {
    if (!gitStatus) return;
    
    const allFiles = [
      ...gitStatus.staged.map(f => f.path),
      ...gitStatus.modified.map(f => f.path),
      ...gitStatus.untracked.map(f => f.path),
      ...gitStatus.deleted.map(f => f.path),
      ...gitStatus.renamed.map(f => f.path)
    ];
    
    setSelectedFiles(new Set(allFiles));
  };

  const handleDeselectAll = () => {
    setSelectedFiles(new Set());
  };

  const handlePreviewFile = (filePath: string) => {
    if (filesPaneCollapsed) {
      setFilesPaneCollapsed(false);
    }
    setActiveDiffPath(filePath);
    loadDiff(filePath);
  };

  const handleStartResize = (e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    if (filesPaneCollapsed) {
      setFilesPaneCollapsed(false);
    }
    isResizingRef.current = true;

    const onMouseMove = (moveEvent: MouseEvent) => {
      if (!isResizingRef.current || !workspaceRef.current) return;
      const rect = workspaceRef.current.getBoundingClientRect();
      const raw = moveEvent.clientX - rect.left;
      const maxWidth = Math.max(280, rect.width - 360);
      const next = Math.max(280, Math.min(maxWidth, raw));
      setFilesPaneWidth(next);
    };

    const onMouseUp = () => {
      isResizingRef.current = false;
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
    };

    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'col-resize';
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  };

  const getVisibleDiffText = () => {
    if (!activeDiff) {
      return '';
    }
    if (diffMode === 'staged') {
      return activeDiff.staged_diff || 'No staged diff output.';
    }
    if (diffMode === 'unstaged') {
      return activeDiff.unstaged_diff || 'No unstaged diff output.';
    }
    return activeDiff.diff || 'No diff output.';
  };

  const visibleDiff = getVisibleDiffText();
  const diffLines = visibleDiff.split('\n');

  const selectedPaths = Array.from(selectedFiles);
  const stagedSet = new Set(gitStatus?.staged.map(f => f.path) || []);
  const modifiedSet = new Set(gitStatus?.modified.map(f => f.path) || []);
  const deletedSet = new Set(gitStatus?.deleted.map(f => f.path) || []);
  const renamedSet = new Set(gitStatus?.renamed.map(f => f.path) || []);
  const untrackedSet = new Set(gitStatus?.untracked.map(f => f.path) || []);

  const stageablePaths = selectedPaths.filter(
    p => modifiedSet.has(p) || deletedSet.has(p) || renamedSet.has(p) || untrackedSet.has(p),
  );
  const unstageablePaths = selectedPaths.filter(p => stagedSet.has(p));
  const discardablePaths = selectedPaths.filter(
    p => modifiedSet.has(p) || deletedSet.has(p) || renamedSet.has(p),
  );

  const runAction = async (action: () => void | Promise<unknown>, fallbackMessage: string) => {
    setActionError(null);
    setIsActing(true);
    try {
      await action();
      await loadGitStatus();
      setSelectedFiles(new Set());
    } catch (err) {
      setActionError(err instanceof Error ? err.message : fallbackMessage);
    } finally {
      setIsActing(false);
    }
  };

  const handleStageSelected = () => {
    if (stageablePaths.length === 0 || !onStage) return;
    runAction(() => onStage(stageablePaths), 'Failed to stage selected files');
  };

  const handleUnstageSelected = () => {
    if (unstageablePaths.length === 0 || !onUnstage) return;
    runAction(() => onUnstage(unstageablePaths), 'Failed to unstage selected files');
  };

  const handleCommit = () => {
    if (!commitMessage.trim() || !gitStatus?.staged.length || !onCommit) return;
    
    const stagedFiles = gitStatus.staged.map(f => f.path);
    runAction(async () => {
      await onCommit(commitMessage, stagedFiles);
      setCommitMessage('');
    }, 'Failed to create commit');
  };

  const handleAICommit = () => {
    if (!gitStatus?.staged.length || !onAICommit) return;
    runAction(async () => {
      await onAICommit();
    }, 'Failed to run /commit workflow');
  };

  const handleDiscardSelected = () => {
    if (discardablePaths.length === 0 || !onDiscard) return;
    runAction(() => onDiscard(discardablePaths), 'Failed to discard selected files');
  };

  const getStatusIcon = (status: string): React.ReactNode => {
    switch (status) {
      case 'M': return <FileEdit size={14} />;
      case 'A': return <Plus size={14} />;
      case 'D': return <Trash2 size={14} />;
      case 'R': return <RefreshCw size={14} />;
      case '??': return <HelpCircle size={14} />;
      default: return <File size={14} />;
    }
  };

  const getStatusText = (status: string) => {
    switch (status) {
      case 'M': return 'Modified';
      case 'A': return 'Added';
      case 'D': return 'Deleted';
      case 'R': return 'Renamed';
      case '??': return 'Untracked';
      default: return 'Unknown';
    }
  };

  const FileItem: React.FC<{
    file: GitFile;
    keyPrefix: string;
    index: number;
    isSelected: boolean;
    isActive: boolean;
    onSelect: (path: string) => void;
    onPreview: (path: string) => void;
  }> = ({ file, keyPrefix, index, isSelected, isActive, onSelect, onPreview }) => (
    <div
      key={`${keyPrefix}-${index}`}
      className={`file-item ${isSelected ? 'selected' : ''} ${isActive ? 'active-diff' : ''}`}
      onClick={() => onSelect(file.path)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect(file.path);
        }
      }}
    >
      <input
        type="checkbox"
        checked={isSelected}
        onClick={(e) => e.stopPropagation()}
        onChange={() => onSelect(file.path)}
      />
      <span className="file-icon">{getStatusIcon(file.status)}</span>
      <span className="file-path">{file.path}</span>
      <span className="file-status">{getStatusText(file.status)}</span>
      {file.changes && (
        <span className="file-changes">
          <span className="additions">+{file.changes.additions}</span>
          <span className="deletions">-{file.changes.deletions}</span>
        </span>
      )}
      <button
        className="file-diff-btn"
        onClick={(e) => {
          e.stopPropagation();
          onPreview(file.path);
        }}
        title="Show diff"
        type="button"
      >
        <SplitSquareHorizontal size={14} />
      </button>
    </div>
  );

  if (isLoading) {
    return (
      <div className="git-view">
        <div className="git-loading">
          <div className="spinner"></div>
          <p>Loading git status...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="git-view">
        <div className="git-error">
          <span className="error-icon"><XCircle size={16} /></span>
          <p>{error}</p>
          <button onClick={() => loadGitStatus()}>Retry</button>
        </div>
      </div>
    );
  }

  if (!gitStatus) {
    return (
      <div className="git-view">
        <div className="git-empty">
          <span className="empty-icon"><GitBranch size={16} /></span>
          <p>No git repository found</p>
        </div>
      </div>
    );
  }

  return (
    <div className="git-view">
      {/* Header */}
      <div className="git-header">
        <div className="git-branch">
          <span className="branch-icon"><GitBranch size={14} /></span>
          <span className="branch-name">{gitStatus.branch}</span>
          {gitStatus.ahead > 0 && <span className="ahead"><ArrowUp size={12} />{gitStatus.ahead}</span>}
          {gitStatus.behind > 0 && <span className="behind"><ArrowDown size={12} />{gitStatus.behind}</span>}
        </div>
        <div className="git-status">
          {gitStatus.clean ? (
            <span className="clean"><CheckCircle2 size={14} style={{ marginRight: 4, verticalAlign: 'middle' }} /> Clean</span>
          ) : (
            <span className="dirty"><RefreshCw size={14} style={{ marginRight: 4, verticalAlign: 'middle' }} /> Changes</span>
          )}
        </div>
      </div>

      {/* Actions Bar */}
      <div className="git-actions">
        <div className="selection-actions">
          <button onClick={handleSelectAll} className="action-btn">
            Select All
          </button>
          <button onClick={handleDeselectAll} className="action-btn">
            Deselect All
          </button>
          <span className="selected-count">
            {selectedFiles.size} selected
          </span>
        </div>
        <div className="file-actions">
          <button 
            onClick={handleStageSelected}
            disabled={stageablePaths.length === 0 || isActing}
            className="action-btn primary"
          >
            Stage Selected {stageablePaths.length > 0 ? `(${stageablePaths.length})` : ''}
          </button>
          <button 
            onClick={handleUnstageSelected}
            disabled={unstageablePaths.length === 0 || isActing}
            className="action-btn"
          >
            Unstage Selected {unstageablePaths.length > 0 ? `(${unstageablePaths.length})` : ''}
          </button>
          <button 
            onClick={handleDiscardSelected}
            disabled={discardablePaths.length === 0 || isActing}
            className="action-btn danger"
          >
            Discard Selected {discardablePaths.length > 0 ? `(${discardablePaths.length})` : ''}
          </button>
        </div>
      </div>
      {actionError && <div className="git-action-error"><AlertTriangle size={14} style={{ marginRight: 4, verticalAlign: 'middle' }} />{actionError}</div>}

      <div
        className="git-workspace"
        ref={workspaceRef}
        style={{
          gridTemplateColumns: filesPaneCollapsed
            ? '0 8px minmax(0, 1fr)'
            : `${filesPaneWidth}px 8px minmax(0, 1fr)`
        }}
      >
        {/* File Sections */}
        <div className={`git-files ${filesPaneCollapsed ? 'collapsed' : ''}`}>
        {/* Staged Files */}
        {gitStatus.staged.length > 0 && (
          <div className="file-section staged">
            <h3>Staged Files ({gitStatus.staged.length})</h3>
            <div className="file-list">
              {gitStatus.staged.map((file, index) => (
                <FileItem
                  keyPrefix="staged"
                  index={index}
                  file={file}
                  isSelected={selectedFiles.has(file.path)}
                  isActive={activeDiffPath === file.path}
                  onSelect={handleFileSelect}
                  onPreview={handlePreviewFile}
                />
              ))}
            </div>
          </div>
        )}

        {/* Modified Files */}
        {gitStatus.modified.length > 0 && (
          <div className="file-section modified">
            <h3>Modified Files ({gitStatus.modified.length})</h3>
            <div className="file-list">
              {gitStatus.modified.map((file, index) => (
                <FileItem
                  keyPrefix="modified"
                  index={index}
                  file={file}
                  isSelected={selectedFiles.has(file.path)}
                  isActive={activeDiffPath === file.path}
                  onSelect={handleFileSelect}
                  onPreview={handlePreviewFile}
                />
              ))}
            </div>
          </div>
        )}

        {/* Untracked Files */}
        {gitStatus.untracked.length > 0 && (
          <div className="file-section untracked">
            <h3>Untracked Files ({gitStatus.untracked.length})</h3>
            <div className="file-list">
              {gitStatus.untracked.map((file, index) => (
                <FileItem
                  keyPrefix="untracked"
                  index={index}
                  file={file}
                  isSelected={selectedFiles.has(file.path)}
                  isActive={activeDiffPath === file.path}
                  onSelect={handleFileSelect}
                  onPreview={handlePreviewFile}
                />
              ))}
            </div>
          </div>
        )}

        {/* No Changes */}
        {gitStatus.clean && (
          <div className="no-changes">
            <span className="no-changes-icon"><Sparkles size={16} /></span>
            <p>Working directory clean</p>
          </div>
        )}
        </div>

        <div
          className="git-workspace-resizer"
          onMouseDown={handleStartResize}
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize git file list"
        />

        {/* Diff Preview */}
        <div className="git-diff-preview">
          <div className="git-diff-preview-header">
            <h3>Diff Preview</h3>
            <div className="git-diff-preview-actions">
              {activeDiffPath && <span className="git-diff-path">{activeDiffPath}</span>}
              <button
                type="button"
                className="git-pane-toggle-btn"
                onClick={() => setFilesPaneCollapsed(prev => !prev)}
                title={filesPaneCollapsed ? 'Show file list' : 'Hide file list'}
              >
                {filesPaneCollapsed ? <PanelLeftOpen size={14} /> : <PanelLeftClose size={14} />}
              </button>
            </div>
          </div>
          {activeDiff && activeDiff.has_staged && activeDiff.has_unstaged && (
            <div className="git-diff-mode-tabs">
              <button
                type="button"
                className={`git-diff-mode-tab ${diffMode === 'combined' ? 'active' : ''}`}
                onClick={() => setDiffMode('combined')}
              >
                Combined
              </button>
              <button
                type="button"
                className={`git-diff-mode-tab ${diffMode === 'staged' ? 'active' : ''}`}
                onClick={() => setDiffMode('staged')}
              >
                Staged
              </button>
              <button
                type="button"
                className={`git-diff-mode-tab ${diffMode === 'unstaged' ? 'active' : ''}`}
                onClick={() => setDiffMode('unstaged')}
              >
                Unstaged
              </button>
            </div>
          )}
          {isDiffLoading ? (
            <div className="git-diff-loading">Loading diff...</div>
          ) : diffError ? (
            <div className="git-diff-error">{diffError}</div>
          ) : activeDiffPath ? (
            <pre className="git-diff-content">
              {diffLines.map((line, index) => {
                const className =
                  line.startsWith('+++') || line.startsWith('---')
                    ? 'diff-file'
                    : line.startsWith('@@')
                      ? 'diff-hunk'
                      : line.startsWith('+')
                        ? 'diff-add'
                        : line.startsWith('-')
                          ? 'diff-del'
                          : 'diff-context';
                return (
                  <div key={index} className={`diff-line ${className}`}>
                    {line || ' '}
                  </div>
                );
              })}
            </pre>
          ) : (
            <div className="git-diff-empty">Select a file to preview its diff.</div>
          )}
        </div>
      </div>

      {/* Commit Section */}
      <div className="git-commit">
        <h3>Commit Changes</h3>
        <div className="commit-form">
          <textarea
            value={commitMessage}
            onChange={(e) => setCommitMessage(e.target.value)}
            placeholder="Enter commit message..."
            className="commit-input"
            rows={3}
          />
          <div className="commit-actions">
            <button
              onClick={handleAICommit}
              disabled={gitStatus.staged.length === 0 || isActing}
              className="commit-btn ai"
            >
              <Sparkles size={14} style={{ marginRight: 6, verticalAlign: 'middle' }} />
              AI Commit (/commit)
            </button>
            <button 
              onClick={handleCommit}
              disabled={!commitMessage.trim() || gitStatus.staged.length === 0 || isActing}
              className="commit-btn primary"
            >
              Commit {gitStatus.staged.length} file{gitStatus.staged.length !== 1 ? 's' : ''}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default GitView;
