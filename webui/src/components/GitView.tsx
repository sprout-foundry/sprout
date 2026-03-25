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
  ShieldCheck,
  Check,
  Minus,
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

type FileSection = 'staged' | 'modified' | 'untracked' | 'deleted';
const MOBILE_LAYOUT_MAX_WIDTH = 768;

const selectionKey = (section: FileSection, path: string): string => `${section}:${path}`;

const parseSelectionKey = (key: string): { section: FileSection; path: string } | null => {
  const separatorIndex = key.indexOf(':');
  if (separatorIndex <= 0) {
    return null;
  }
  const section = key.slice(0, separatorIndex) as FileSection;
  const path = key.slice(separatorIndex + 1);
  if (!path) {
    return null;
  }
  if (section !== 'staged' && section !== 'modified' && section !== 'untracked' && section !== 'deleted') {
    return null;
  }
  return { section, path };
};

interface GitViewProps {
  refreshToken?: number;
  onCommit?: (message: string, files: string[]) => void | Promise<unknown>;
  onAICommit?: () => Promise<string>;
  onStage?: (files: string[]) => void | Promise<unknown>;
  onUnstage?: (files: string[]) => void | Promise<unknown>;
  onDiscard?: (files: string[]) => void | Promise<unknown>;
  selectedFilePath?: string | null;
}

interface DeepReviewResult {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
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
  const [activeDiffSelectionKey, setActiveDiffSelectionKey] = useState<string | null>(null);
  const [activeDiff, setActiveDiff] = useState<GitDiffResponse | null>(null);
  const [diffMode, setDiffMode] = useState<'combined' | 'staged' | 'unstaged'>('combined');
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffError, setDiffError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isActing, setIsActing] = useState(false);
  const [isGeneratingCommitMessage, setIsGeneratingCommitMessage] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [filesPaneWidth, setFilesPaneWidth] = useState(440);
  const [filesPaneCollapsed, setFilesPaneCollapsed] = useState(false);
  const [isReviewLoading, setIsReviewLoading] = useState(false);
  const [isReviewFixing, setIsReviewFixing] = useState(false);
  const [reviewError, setReviewError] = useState<string | null>(null);
  const [reviewFixResult, setReviewFixResult] = useState<string | null>(null);
  const [reviewFixLogs, setReviewFixLogs] = useState<string[]>([]);
  const [reviewFixSessionID, setReviewFixSessionID] = useState<string | null>(null);
  const [deepReview, setDeepReview] = useState<DeepReviewResult | null>(null);
  const [rightPanelTab, setRightPanelTab] = useState<'diff' | 'review'>('diff');
  const [isMobileLayout, setIsMobileLayout] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false;
    return window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH;
  });
  const workspaceRef = useRef<HTMLDivElement>(null);
  const isResizingRef = useRef(false);
  const fixPollTimeoutRef = useRef<number | null>(null);
  const fixPollIndexRef = useRef(0);
  const apiService = ApiService.getInstance();

  useEffect(() => {
    const onResize = () => {
      setIsMobileLayout(window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH);
    };
    onResize();
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  useEffect(() => {
    return () => {
      if (fixPollTimeoutRef.current !== null) {
        window.clearTimeout(fixPollTimeoutRef.current);
        fixPollTimeoutRef.current = null;
      }
    };
  }, []);

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
    setActiveDiffSelectionKey(null);
    setActiveDiffPath(selectedFilePath);
    loadDiff(selectedFilePath);
  }, [selectedFilePath, loadDiff]);

  const handleFileSelect = (section: FileSection, filePath: string) => {
    const key = selectionKey(section, filePath);
    setSelectedFiles(prev => {
      const newSet = new Set(prev);
      if (newSet.has(key)) {
        newSet.delete(key);
      } else {
        newSet.add(key);
      }
      return newSet;
    });
  };

  const handleSelectAll = () => {
    if (!gitStatus) return;
    
    const allFiles = [
      ...gitStatus.staged.map(f => selectionKey('staged', f.path)),
      ...gitStatus.modified.map(f => selectionKey('modified', f.path)),
      ...gitStatus.untracked.map(f => selectionKey('untracked', f.path)),
      ...gitStatus.deleted.map(f => selectionKey('deleted', f.path)),
    ];
    
    setSelectedFiles(new Set(allFiles));
  };

  const handleDeselectAll = () => {
    setSelectedFiles(new Set());
  };

  const isSectionFullySelected = (section: FileSection, files: GitFile[]) =>
    files.length > 0 && files.every((file) => selectedFiles.has(selectionKey(section, file.path)));

  const getSectionSelectedCount = (section: FileSection, files: GitFile[]) =>
    files.reduce((count, file) => (
      selectedFiles.has(selectionKey(section, file.path)) ? count + 1 : count
    ), 0);

  const handleToggleSectionSelect = (section: FileSection, files: GitFile[]) => {
    if (files.length === 0) return;
    setSelectedFiles((prev) => {
      const next = new Set(prev);
      const sectionKeys = files.map((file) => selectionKey(section, file.path));
      const allSelected = sectionKeys.every((key) => next.has(key));
      if (allSelected) {
        sectionKeys.forEach((key) => next.delete(key));
      } else {
        sectionKeys.forEach((key) => next.add(key));
      }
      return next;
    });
  };

  const handlePreviewFile = (section: FileSection, filePath: string) => {
    if (filesPaneCollapsed) {
      setFilesPaneCollapsed(false);
    }
    setActiveDiffSelectionKey(selectionKey(section, filePath));
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

  const selectedEntries = Array.from(selectedFiles)
    .map(parseSelectionKey)
    .filter((entry): entry is { section: FileSection; path: string } => entry !== null);

  const stageablePaths = Array.from(new Set(
    selectedEntries
      .filter((entry) => entry.section !== 'staged')
      .map((entry) => entry.path),
  ));
  const unstageablePaths = Array.from(new Set(
    selectedEntries
      .filter((entry) => entry.section === 'staged')
      .map((entry) => entry.path),
  ));
  const discardablePaths = Array.from(new Set(
    selectedEntries
      .filter((entry) => entry.section === 'modified' || entry.section === 'deleted')
      .map((entry) => entry.path),
  ));

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
    setActionError(null);
    setIsGeneratingCommitMessage(true);
    onAICommit()
      .then((generatedMessage) => {
        if (!generatedMessage || !generatedMessage.trim()) {
          throw new Error('AI returned an empty commit message');
        }
        setCommitMessage(generatedMessage.trim());
      })
      .catch((err) => {
        setActionError(err instanceof Error ? err.message : 'Failed to generate commit message');
      })
      .finally(() => {
        setIsGeneratingCommitMessage(false);
      });
  };

  const handleDiscardSelected = () => {
    if (discardablePaths.length === 0 || !onDiscard) return;
    runAction(() => onDiscard(discardablePaths), 'Failed to discard selected files');
  };

  const handleDeepReview = async () => {
    setRightPanelTab('review');
    setReviewError(null);
    setReviewFixResult(null);
    setIsReviewLoading(true);
    try {
      const response = await apiService.generateDeepReview();
      setDeepReview(response);
    } catch (err) {
      setReviewError(err instanceof Error ? err.message : 'Failed to generate deep review');
      setDeepReview(null);
    } finally {
      setIsReviewLoading(false);
    }
  };

  const handleFixFromReview = async () => {
    if (!deepReview?.review_output) return;
    setReviewError(null);
    setReviewFixResult(null);
    setReviewFixLogs([]);
    setReviewFixSessionID(null);
    setIsReviewFixing(true);
    try {
      const started = await apiService.startFixFromDeepReview(deepReview.review_output);
      setReviewFixSessionID(started.session_id || null);
      fixPollIndexRef.current = 0;
      setReviewFixLogs((prev) => [...prev, `Started fix session: ${started.session_id}`]);

      const poll = async () => {
        try {
          const status = await apiService.getFixFromDeepReviewStatus(started.job_id, fixPollIndexRef.current);
          if (status.logs?.length) {
            setReviewFixLogs((prev) => [...prev, ...status.logs]);
          }
          fixPollIndexRef.current = status.next_index || fixPollIndexRef.current;

          if (status.status === 'completed') {
            setReviewFixResult(status.result || 'Fix workflow completed.');
            setIsReviewFixing(false);
            await loadGitStatus();
            return;
          }
          if (status.status === 'error') {
            throw new Error(status.error || 'Fix workflow failed');
          }

          fixPollTimeoutRef.current = window.setTimeout(poll, 1000);
        } catch (pollErr) {
          setReviewError(pollErr instanceof Error ? pollErr.message : 'Failed to fetch fix progress');
          setIsReviewFixing(false);
        }
      };

      await poll();
    } catch (err) {
      setReviewError(err instanceof Error ? err.message : 'Failed to apply fixes from deep review');
      setIsReviewFixing(false);
    }
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
    section: FileSection;
    isSelected: boolean;
    isActive: boolean;
    onSelect: (section: FileSection, path: string) => void;
    onPreview: (section: FileSection, path: string) => void;
  }> = ({ file, section, isSelected, isActive, onSelect, onPreview }) => (
    <div
      className={`file-item ${isSelected ? 'selected' : ''} ${isActive ? 'active-diff' : ''}`}
      onClick={() => onSelect(section, file.path)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect(section, file.path);
        }
      }}
    >
      <input
        type="checkbox"
        checked={isSelected}
        onClick={(e) => e.stopPropagation()}
        onChange={() => onSelect(section, file.path)}
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
          onPreview(section, file.path);
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
            onClick={handleDeepReview}
            disabled={isActing || isReviewLoading || isReviewFixing || !gitStatus.staged.length}
            className="action-btn"
          >
            <ShieldCheck size={14} style={{ marginRight: 6, verticalAlign: 'middle' }} />
            {isReviewLoading ? 'Reviewing…' : 'Review'}
          </button>
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
            Discard / Restore Selected {discardablePaths.length > 0 ? `(${discardablePaths.length})` : ''}
          </button>
        </div>
      </div>
      {actionError && <div className="git-action-error"><AlertTriangle size={14} style={{ marginRight: 4, verticalAlign: 'middle' }} />{actionError}</div>}

      <div
        className="git-workspace"
        ref={workspaceRef}
        style={isMobileLayout ? undefined : {
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
            <div className="file-section-header">
              <h3>Staged Files ({gitStatus.staged.length})</h3>
              <button
                type="button"
                className="section-select-btn"
                onClick={() => handleToggleSectionSelect('staged', gitStatus.staged)}
                title={isSectionFullySelected('staged', gitStatus.staged) ? 'Clear staged selection' : 'Select all staged files'}
                aria-label={isSectionFullySelected('staged', gitStatus.staged) ? 'Clear staged selection' : 'Select all staged files'}
              >
                {isSectionFullySelected('staged', gitStatus.staged) ? <Minus size={14} /> : <Check size={14} />}
                <span className="section-select-count">
                  {getSectionSelectedCount('staged', gitStatus.staged)}
                </span>
              </button>
            </div>
            <div className="file-list">
              {gitStatus.staged.map((file, index) => (
                <FileItem
                  key={`staged-${file.path}-${index}`}
                  section="staged"
                  file={file}
                  isSelected={selectedFiles.has(selectionKey('staged', file.path))}
                  isActive={
                    activeDiffSelectionKey
                      ? activeDiffSelectionKey === selectionKey('staged', file.path)
                      : activeDiffPath === file.path
                  }
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
            <div className="file-section-header">
              <h3>Modified Files ({gitStatus.modified.length})</h3>
              <button
                type="button"
                className="section-select-btn"
                onClick={() => handleToggleSectionSelect('modified', gitStatus.modified)}
                title={isSectionFullySelected('modified', gitStatus.modified) ? 'Clear modified selection' : 'Select all modified files'}
                aria-label={isSectionFullySelected('modified', gitStatus.modified) ? 'Clear modified selection' : 'Select all modified files'}
              >
                {isSectionFullySelected('modified', gitStatus.modified) ? <Minus size={14} /> : <Check size={14} />}
                <span className="section-select-count">
                  {getSectionSelectedCount('modified', gitStatus.modified)}
                </span>
              </button>
            </div>
            <div className="file-list">
              {gitStatus.modified.map((file, index) => (
                <FileItem
                  key={`modified-${file.path}-${index}`}
                  section="modified"
                  file={file}
                  isSelected={selectedFiles.has(selectionKey('modified', file.path))}
                  isActive={
                    activeDiffSelectionKey
                      ? activeDiffSelectionKey === selectionKey('modified', file.path)
                      : activeDiffPath === file.path
                  }
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
            <div className="file-section-header">
              <h3>Untracked Files ({gitStatus.untracked.length})</h3>
              <button
                type="button"
                className="section-select-btn"
                onClick={() => handleToggleSectionSelect('untracked', gitStatus.untracked)}
                title={isSectionFullySelected('untracked', gitStatus.untracked) ? 'Clear untracked selection' : 'Select all untracked files'}
                aria-label={isSectionFullySelected('untracked', gitStatus.untracked) ? 'Clear untracked selection' : 'Select all untracked files'}
              >
                {isSectionFullySelected('untracked', gitStatus.untracked) ? <Minus size={14} /> : <Check size={14} />}
                <span className="section-select-count">
                  {getSectionSelectedCount('untracked', gitStatus.untracked)}
                </span>
              </button>
            </div>
            <div className="file-list">
              {gitStatus.untracked.map((file, index) => (
                <FileItem
                  key={`untracked-${file.path}-${index}`}
                  section="untracked"
                  file={file}
                  isSelected={selectedFiles.has(selectionKey('untracked', file.path))}
                  isActive={
                    activeDiffSelectionKey
                      ? activeDiffSelectionKey === selectionKey('untracked', file.path)
                      : activeDiffPath === file.path
                  }
                  onSelect={handleFileSelect}
                  onPreview={handlePreviewFile}
                />
              ))}
            </div>
          </div>
        )}

        {/* Deleted Files */}
        {gitStatus.deleted.length > 0 && (
          <div className="file-section deleted">
            <div className="file-section-header">
              <h3>Deleted Files ({gitStatus.deleted.length})</h3>
              <button
                type="button"
                className="section-select-btn"
                onClick={() => handleToggleSectionSelect('deleted', gitStatus.deleted)}
                title={isSectionFullySelected('deleted', gitStatus.deleted) ? 'Clear deleted selection' : 'Select all deleted files'}
                aria-label={isSectionFullySelected('deleted', gitStatus.deleted) ? 'Clear deleted selection' : 'Select all deleted files'}
              >
                {isSectionFullySelected('deleted', gitStatus.deleted) ? <Minus size={14} /> : <Check size={14} />}
                <span className="section-select-count">
                  {getSectionSelectedCount('deleted', gitStatus.deleted)}
                </span>
              </button>
            </div>
            <div className="file-list">
              {gitStatus.deleted.map((file, index) => (
                <FileItem
                  key={`deleted-${file.path}-${index}`}
                  section="deleted"
                  file={file}
                  isSelected={selectedFiles.has(selectionKey('deleted', file.path))}
                  isActive={
                    activeDiffSelectionKey
                      ? activeDiffSelectionKey === selectionKey('deleted', file.path)
                      : activeDiffPath === file.path
                  }
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

        {/* Combined Right Panel */}
        <div className="git-combined-panel">
          {/* Tab Bar */}
          <div className="git-combined-panel-tabs">
            <button
              type="button"
              className={`git-combined-panel-tab ${rightPanelTab === 'diff' ? 'active' : ''}`}
              onClick={() => setRightPanelTab('diff')}
            >
              <SplitSquareHorizontal size={14} style={{ marginRight: 6 }} />
              Diff
            </button>
            <button
              type="button"
              className={`git-combined-panel-tab ${rightPanelTab === 'review' ? 'active' : ''}`}
              onClick={() => setRightPanelTab('review')}
            >
              <ShieldCheck size={14} style={{ marginRight: 6 }} />
              Review
            </button>
            <div className="git-combined-panel-actions">
              {rightPanelTab === 'diff' && activeDiffPath && (
                <span className="git-diff-path">{activeDiffPath}</span>
              )}
              {rightPanelTab === 'review' && (
                <button
                  type="button"
                  className="action-btn"
                  onClick={handleDeepReview}
                  disabled={isActing || isReviewLoading || isReviewFixing || !gitStatus.staged.length}
                  title={gitStatus.staged.length === 0 ? 'Stage files to run Review' : 'Run Review'}
                >
                  {isReviewLoading ? 'Reviewing…' : (deepReview ? 'Re-run Review' : 'Start Review')}
                </button>
              )}
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

          {/* Tab Content */}
          {rightPanelTab === 'diff' ? (
            <>
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
            </>
          ) : (
            <div className="git-review-pane-body">
              {isReviewLoading ? (
                <div className="git-review-loading">
                  <div className="spinner"></div>
                  <p>Running /deep-review on staged changes…</p>
                </div>
              ) : reviewError ? (
                <div className="git-review-error">
                  <p>{reviewError}</p>
                  <button
                    type="button"
                    className="action-btn"
                    onClick={handleDeepReview}
                    disabled={isReviewLoading || isReviewFixing || !gitStatus.staged.length}
                  >
                    Retry Review
                  </button>
                </div>
              ) : deepReview ? (
                <>
                  <div className="git-review-meta">
                    <span className={`git-review-status status-${(deepReview.status || '').toLowerCase()}`}>
                      {(deepReview.status || 'unknown').toUpperCase()}
                    </span>
                    {deepReview.provider && deepReview.model && (
                      <span className="git-review-model">{deepReview.provider} · {deepReview.model}</span>
                    )}
                  </div>
                  <div className="git-review-section">
                    <h4>Feedback</h4>
                    <pre>{deepReview.feedback || 'No feedback.'}</pre>
                  </div>
                  {deepReview.detailed_guidance && (
                    <div className="git-review-section">
                      <h4>Detailed Guidance</h4>
                      <pre>{deepReview.detailed_guidance}</pre>
                    </div>
                  )}
                  {deepReview.suggested_new_prompt && (
                    <div className="git-review-section">
                      <h4>Suggested New Prompt</h4>
                      <pre>{deepReview.suggested_new_prompt}</pre>
                    </div>
                  )}
                  {reviewFixResult && (
                    <div className="git-review-section">
                      <h4>Fix Result</h4>
                      <pre>{reviewFixResult}</pre>
                    </div>
                  )}
                  {(isReviewFixing || reviewFixLogs.length > 0) && (
                    <div className="git-review-section">
                      <h4>
                        Fix Progress
                        {reviewFixSessionID ? ` (${reviewFixSessionID})` : ''}
                      </h4>
                      <pre>{reviewFixLogs.length ? reviewFixLogs.join('\n') : 'Waiting for progress...'}</pre>
                    </div>
                  )}
                </>
              ) : (
                <div className="git-review-empty">
                  <p>Run Review to analyze staged changes.</p>
                  <button
                    type="button"
                    className="action-btn primary"
                    onClick={handleDeepReview}
                    disabled={isReviewLoading || isReviewFixing || !gitStatus.staged.length}
                  >
                    Start Review
                  </button>
                </div>
              )}
              <div className="git-review-pane-footer">
                <button
                  type="button"
                  className="action-btn"
                  onClick={() => setRightPanelTab('diff')}
                  disabled={isReviewFixing}
                >
                  Accept
                </button>
                <button
                  type="button"
                  className="action-btn primary"
                  onClick={handleFixFromReview}
                  disabled={!deepReview || isReviewLoading || isReviewFixing}
                >
                  {isReviewFixing ? 'Fixing…' : 'Fix'}
                </button>
              </div>
            </div>
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
              disabled={gitStatus.staged.length === 0 || isActing || isGeneratingCommitMessage}
              className="commit-btn ai"
            >
              <Sparkles size={14} style={{ marginRight: 6, verticalAlign: 'middle' }} />
              {isGeneratingCommitMessage ? 'Generating…' : 'Generate Message'}
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

      {isGeneratingCommitMessage && (
        <div className="git-generating-overlay" role="dialog" aria-modal="true" aria-label="Generating commit message">
          <div className="git-generating-dialog">
            <div className="spinner"></div>
            <h4>Generating Commit Message</h4>
            <p>Using staged changes to draft a commit message...</p>
          </div>
        </div>
      )}
    </div>
  );
};

export default GitView;
