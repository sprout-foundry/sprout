import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
  RefreshCw,
  XCircle,
  CheckCircle2,
  Sparkles,
  GitBranch,
  ArrowUp,
  ArrowDown,
  AlertTriangle,
  ShieldCheck,
} from 'lucide-react';
import './GitView.css';
import { ApiService } from '../services/api';
import ContextPanel from './ContextPanel';

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
  const [isReviewLoading, setIsReviewLoading] = useState(false);
  const [isReviewFixing, setIsReviewFixing] = useState(false);
  const [reviewError, setReviewError] = useState<string | null>(null);
  const [reviewFixResult, setReviewFixResult] = useState<string | null>(null);
  const [reviewFixLogs, setReviewFixLogs] = useState<string[]>([]);
  const [reviewFixSessionID, setReviewFixSessionID] = useState<string | null>(null);
  const [deepReview, setDeepReview] = useState<DeepReviewResult | null>(null);
  const [isMobileLayout, setIsMobileLayout] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false;
    return window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH;
  });
  const workspaceRef = useRef<HTMLDivElement>(null);
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

  const handlePreviewFile = (section: FileSection, filePath: string) => {
    setActiveDiffSelectionKey(selectionKey(section, filePath));
    setActiveDiffPath(filePath);
    loadDiff(filePath);
  };

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
          gridTemplateColumns: 'minmax(200px, auto) 8px minmax(0, 1fr)'
        }}
      >
        {/* ContextPanel - Shared Side Panel Component */}
        <ContextPanel
          context="git"
          deepReview={deepReview}
          reviewError={reviewError}
          reviewFixResult={reviewFixResult}
          reviewFixLogs={reviewFixLogs}
          reviewFixSessionID={reviewFixSessionID}
          isReviewLoading={isReviewLoading}
          isReviewFixing={isReviewFixing}
          isDiffLoading={isDiffLoading}
          diffError={diffError}
          isMobileLayout={isMobileLayout}
          diffMode={diffMode}
          onRunReview={handleDeepReview}
          onFixFromReview={handleFixFromReview}
          onDiscard={handleDiscardSelected}
          onStage={handleStageSelected}
          stagedFiles={gitStatus.staged}
          modifiedFiles={gitStatus.modified}
          untrackedFiles={gitStatus.untracked}
          deletedFiles={gitStatus.deleted}
          selectedFiles={selectedFiles}
          onFileSelect={handleFileSelect}
          onPreviewFile={handlePreviewFile}
          activeDiffSelectionKey={activeDiffSelectionKey}
          activeDiffPath={activeDiffPath}
          activeDiff={activeDiff}
          onDiffModeChange={setDiffMode}
        />

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
    </div>
  );
};

export default GitView;
