import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { type ApiService } from '../services/api';
import { WebSocketService } from '../services/websocket';
import { notificationBus } from '../services/notificationBus';
import type { WsEvent } from '../services/websocket';
import type { GitStatusData } from '../types/git-types';
import type { FileSection } from '../types/git-types';
import { selectionKey, parseSelectionKey } from '../types/git-types';
import { useLog, debugLog, warn } from '../utils/log';

export interface GitDiffResponse {
  message: string;
  path: string;
  has_staged: boolean;
  has_unstaged: boolean;
  staged_diff: string;
  unstaged_diff: string;
  diff: string;
}

export interface DeepReviewResult {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
  warnings?: string[];
}

export interface GitBranchesState {
  current: string;
  branches: string[];
}

interface UseGitWorkspaceOptions {
  apiService: ApiService;
  gitRefreshToken: number;
  selectedGitFilePath?: string | null;
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onGitCommit: (message: string, files: string[]) => Promise<unknown>;
  onGitAICommit: () => Promise<{ commitMessage: string; warnings?: string[] }>;
  onGitStage: (files: string[]) => Promise<void>;
  onGitUnstage: (files: string[]) => Promise<void>;
  onGitDiscard: (files: string[]) => Promise<void>;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, unknown>;
  }) => string;
}

export const useGitWorkspace = ({
  apiService,
  gitRefreshToken,
  selectedGitFilePath,
  onViewChange,
  onGitCommit,
  onGitAICommit,
  onGitStage,
  onGitUnstage,
  onGitDiscard,
  openWorkspaceBuffer,
}: UseGitWorkspaceOptions) => {
  const log = useLog();

  const [gitStatus, setGitStatus] = useState<GitStatusData | null>(null);
  const [commitMessage, setCommitMessage] = useState('');
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
  const [activeDiffPath, setActiveDiffPath] = useState<string | null>(selectedGitFilePath || null);
  const [activeDiffSelectionKey, setActiveDiffSelectionKey] = useState<string | null>(null);
  const [activeDiff, setActiveDiff] = useState<GitDiffResponse | null>(null);
  const [diffMode, setDiffMode] = useState<'combined' | 'staged' | 'unstaged'>('combined');
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffError, setDiffError] = useState<string | null>(null);
  const [isGitLoading, setIsGitLoading] = useState(false);
  const [isGitActing, setIsGitActing] = useState(false);
  const [isGeneratingCommitMessage, setIsGeneratingCommitMessage] = useState(false);
  const [gitActionError, setGitActionError] = useState<string | null>(null);
  const [gitActionWarning, setGitActionWarning] = useState<string | null>(null);
  const [gitBranches, setGitBranches] = useState<GitBranchesState>({ current: '', branches: [] });
  const [isReviewLoading, setIsReviewLoading] = useState(false);
  const [isReviewFixing, setIsReviewFixing] = useState(false);
  const [reviewError, setReviewError] = useState<string | null>(null);
  const [reviewFixResult, setReviewFixResult] = useState<string | null>(null);
  const [reviewFixLogs, setReviewFixLogs] = useState<string[]>([]);
  const [reviewFixSessionID, setReviewFixSessionID] = useState<string | null>(null);
  const [deepReview, setDeepReview] = useState<DeepReviewResult | null>(null);
  const fixPollTimeoutRef = useRef<number | null>(null);
  const fixPollIndexRef = useRef(0);

  useEffect(() => {
    return () => {
      if (fixPollTimeoutRef.current !== null) {
        window.clearTimeout(fixPollTimeoutRef.current);
      }
    };
  }, []);

  const loadGitStatus = useCallback(async () => {
    setIsGitLoading(true);
    try {
      const [data, branchData] = await Promise.all([
        apiService.getGitStatus(),
        apiService.getGitBranches().catch((err) => {
          debugLog('[loadGitStatus] failed to fetch git branches:', err);
          return { current: '', branches: [] };
        }),
      ]);
      if (data.message !== 'success') {
        throw new Error(data.message || 'Failed to load git status');
      }

      const status = data.status || {
        branch: '',
        ahead: 0,
        behind: 0,
        staged: [],
        modified: [],
        untracked: [],
        deleted: [],
        renamed: [],
      };

      // Show warning if file list was truncated due to limits
      if (data.status?.truncated) {
        setGitActionWarning(
          'Too many files changed. Showing only the first 500 files per section. Use git status or command line to see all files.',
        );
      } else {
        setGitActionWarning(null);
      }

      setGitStatus({
        branch: status.branch || '',
        ahead: status.ahead || 0,
        behind: status.behind || 0,
        staged: status.staged || [],
        modified: status.modified || [],
        untracked: status.untracked || [],
        deleted: status.deleted || [],
        renamed: status.renamed || [],
        clean: !(
          status.staged?.length ||
          status.modified?.length ||
          status.untracked?.length ||
          status.deleted?.length ||
          status.renamed?.length
        ),
        truncated: status.truncated || false,
      });
      setGitBranches({
        current: branchData.current || status.branch || '',
        branches: branchData.branches || [],
      });
    } catch (error) {
      log.error('Failed to load git status', { title: 'Git Error' });
      setGitStatus(null);
      setGitActionError(error instanceof Error ? error.message : 'Failed to load git status');
    } finally {
      setIsGitLoading(false);
    }
  }, [apiService, log]);

  useEffect(() => {
    loadGitStatus();
  }, [loadGitStatus, gitRefreshToken]);

  // Debounced git status refresh on file change WebSocket events.
  // When files are written (editor save, agent edits, search replace), the git
  // panel should reflect the new status. Uses a 2s debounce to coalesce rapid
  // agent edits into a single refresh.
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    const wsService = WebSocketService.getInstance();

    const handleFileChanged = (event: WsEvent) => {
      if (event?.type !== 'file_changed') return;

      const eventData = event.data as Record<string, unknown> | undefined;
      const action = String(eventData?.action || '');
      // Skip git-level actions — those already trigger explicit refreshes
      // via runGitAction → loadGitStatus.
      if (action.startsWith('git_')) return;

      // Only refresh on actual file content changes
      if (!['write', 'edit', 'created', 'deleted'].includes(action)) return;

      if (debounceTimerRef.current !== null) {
        clearTimeout(debounceTimerRef.current);
      }
      debounceTimerRef.current = setTimeout(() => {
        debounceTimerRef.current = null;
        loadGitStatus();
      }, 2000);
    };

    wsService.onEvent(handleFileChanged);
    return () => {
      wsService.removeEvent(handleFileChanged);
      if (debounceTimerRef.current !== null) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [loadGitStatus]);

  const loadDiff = useCallback(
    async (filePath: string) => {
      setIsDiffLoading(true);
      setDiffError(null);
      try {
        const response = await apiService.getGitDiff(filePath);
        setActiveDiff(response);
        const nextMode =
          response.has_staged && !response.has_unstaged
            ? 'staged'
            : !response.has_staged && response.has_unstaged
              ? 'unstaged'
              : 'combined';
        setDiffMode(nextMode);
        openWorkspaceBuffer({
          kind: 'diff',
          path: `__workspace/diff/${filePath}`,
          title: `Diff: ${filePath.split('/').pop() || filePath}`,
          ext: '.diff',
          metadata: {
            sourcePath: filePath,
            diff: response,
            diffMode: nextMode,
          },
        });
      } catch (error) {
        log.error('Failed to load diff', { title: 'Git Error' });
        setDiffError(error instanceof Error ? error.message : 'Failed to load diff');
        setActiveDiff(null);
      } finally {
        setIsDiffLoading(false);
      }
    },
    [apiService, openWorkspaceBuffer, log],
  );

  useEffect(() => {
    if (!selectedGitFilePath) {
      return;
    }
    setActiveDiffSelectionKey(null);
    setActiveDiffPath(selectedGitFilePath);
    loadDiff(selectedGitFilePath);
  }, [loadDiff, selectedGitFilePath]);

  const selectedEntries = useMemo(
    () =>
      Array.from(selectedFiles)
        .map(parseSelectionKey)
        .filter((entry): entry is { section: FileSection; path: string } => entry !== null),
    [selectedFiles],
  );

  const stageablePaths = useMemo(
    () => Array.from(new Set(selectedEntries.filter((entry) => entry.section !== 'staged').map((entry) => entry.path))),
    [selectedEntries],
  );

  const unstageablePaths = useMemo(
    () => Array.from(new Set(selectedEntries.filter((entry) => entry.section === 'staged').map((entry) => entry.path))),
    [selectedEntries],
  );

  const discardablePaths = useMemo(
    () =>
      Array.from(
        new Set(
          selectedEntries
            .filter((entry) => entry.section === 'modified' || entry.section === 'deleted')
            .map((entry) => entry.path),
        ),
      ),
    [selectedEntries],
  );

  const runGitAction = useCallback(
    async (action: () => Promise<unknown>, fallbackMessage: string) => {
      setGitActionError(null);
      setGitActionWarning(null);
      setIsGitActing(true);
      try {
        await action();
        await loadGitStatus();
        setSelectedFiles(new Set());
      } catch (error) {
        warn(`[runGitAction] failed: ${error instanceof Error ? error.message : String(error)}`);
        setGitActionError(error instanceof Error ? error.message : fallbackMessage);
      } finally {
        setIsGitActing(false);
      }
    },
    [loadGitStatus],
  );

  const handleToggleFileSelection = useCallback((section: FileSection, filePath: string) => {
    const key = selectionKey(section, filePath);
    setSelectedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  const handleToggleSectionSelection = useCallback(
    (section: FileSection) => {
      const files = gitStatus?.[section] || [];
      if (files.length === 0) return;

      setSelectedFiles((prev) => {
        const next = new Set(prev);
        const keys = files.map((file) => selectionKey(section, file.path));
        const allSelected = keys.every((key) => next.has(key));
        keys.forEach((key) => {
          if (allSelected) {
            next.delete(key);
          } else {
            next.add(key);
          }
        });
        return next;
      });
    },
    [gitStatus],
  );

  const clearSelectedFiles = useCallback(() => {
    setSelectedFiles(new Set());
  }, []);

  const handlePreviewGitFile = useCallback(
    (section: FileSection, filePath: string) => {
      setActiveDiffSelectionKey(selectionKey(section, filePath));
      setActiveDiffPath(filePath);
      loadDiff(filePath);
      onViewChange('editor');
    },
    [loadDiff, onViewChange],
  );

  const handleStageSelected = useCallback(() => {
    if (stageablePaths.length === 0) return;
    runGitAction(() => onGitStage(stageablePaths), 'Failed to stage selected files');
  }, [onGitStage, runGitAction, stageablePaths]);

  const handleUnstageSelected = useCallback(() => {
    if (unstageablePaths.length === 0) return;
    runGitAction(() => onGitUnstage(unstageablePaths), 'Failed to unstage selected files');
  }, [onGitUnstage, runGitAction, unstageablePaths]);

  const handleDiscardSelected = useCallback(() => {
    if (discardablePaths.length === 0) return;
    runGitAction(() => onGitDiscard(discardablePaths), 'Failed to discard selected files');
  }, [discardablePaths, onGitDiscard, runGitAction]);

  const handleStageFile = useCallback(
    (filePath: string) => {
      runGitAction(() => onGitStage([filePath]), `Failed to stage ${filePath}`);
    },
    [onGitStage, runGitAction],
  );

  const handleUnstageFile = useCallback(
    (filePath: string) => {
      runGitAction(() => onGitUnstage([filePath]), `Failed to unstage ${filePath}`);
    },
    [onGitUnstage, runGitAction],
  );

  const handleDiscardFile = useCallback(
    (filePath: string) => {
      runGitAction(() => onGitDiscard([filePath]), `Failed to discard ${filePath}`);
    },
    [onGitDiscard, runGitAction],
  );

  const handleSectionAction = useCallback(
    (section: FileSection) => {
      const files = gitStatus?.[section] || [];
      if (files.length === 0) return;

      if (section === 'staged') {
        runGitAction(() => onGitUnstage(files.map((file) => file.path)), 'Failed to unstage section');
        return;
      }

      if (section === 'modified' || section === 'untracked' || section === 'deleted') {
        runGitAction(() => onGitStage(files.map((file) => file.path)), 'Failed to stage section');
      }
    },
    [gitStatus, onGitStage, onGitUnstage, runGitAction],
  );

  const handleGitCommitClick = useCallback(() => {
    if (!commitMessage.trim() || !gitStatus?.staged.length) return;
    runGitAction(async () => {
      await onGitCommit(
        commitMessage,
        gitStatus.staged.map((file) => file.path),
      );
      setCommitMessage('');
      setDeepReview(null);
    }, 'Failed to create commit');
  }, [commitMessage, gitStatus, onGitCommit, runGitAction]);

  const handleGenerateCommitMessage = useCallback(() => {
    if (!gitStatus?.staged.length) return;
    setGitActionError(null);
    setGitActionWarning(null);
    setIsGeneratingCommitMessage(true);
    onGitAICommit()
      .then(({ commitMessage: generatedMessage, warnings }) => {
        if (!generatedMessage || !generatedMessage.trim()) {
          throw new Error('AI returned an empty commit message');
        }
        setCommitMessage(generatedMessage.trim());
        if (warnings && warnings.length > 0) {
          setGitActionWarning(warnings.join(' '));
        }
      })
      .catch((error) => {
        debugLog('[useGitWorkspace] Failed to generate commit message:', error);
        const msg = error instanceof Error ? error.message : 'Failed to generate commit message';
        setGitActionError(msg);
        notificationBus.notify('warning', 'AI Commit', msg, 5000);
      })
      .finally(() => {
        setIsGeneratingCommitMessage(false);
      });
  }, [gitStatus, onGitAICommit]);

  const handleRunReview = useCallback(async () => {
    setReviewError(null);
    setGitActionWarning(null);
    setReviewFixResult(null);
    setIsReviewLoading(true);
    try {
      const response = await apiService.generateDeepReview();
      setDeepReview(response);
      openWorkspaceBuffer({
        kind: 'review',
        path: '__workspace/review',
        title: 'Review',
        ext: '.review',
        metadata: { reviewGeneratedAt: Date.now() },
      });
      onViewChange('editor');
    } catch (error) {
      warn(`[handleRunReview] failed: ${error instanceof Error ? error.message : String(error)}`);
      setReviewError(error instanceof Error ? error.message : 'Failed to generate deep review');
      setDeepReview(null);
    } finally {
      setIsReviewLoading(false);
    }
  }, [apiService, onViewChange, openWorkspaceBuffer]);

  const handleFixFromReview = useCallback(
    async (options?: { fixPrompt?: string; selectedItems?: string[] }) => {
      if (!deepReview?.review_output) return;
      setReviewError(null);
      setReviewFixResult(null);
      setReviewFixLogs([]);
      setReviewFixSessionID(null);
      setIsReviewFixing(true);
      try {
        const started = await apiService.startFixFromDeepReview(deepReview.review_output, options);
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
          } catch (error) {
            warn(`[handleFixFromReview] poll error: ${error instanceof Error ? error.message : String(error)}`);
            setReviewError(error instanceof Error ? error.message : 'Failed to fetch fix progress');
            setIsReviewFixing(false);
          }
        };

        await poll();
      } catch (error) {
        warn(`[handleFixFromReview] outer error: ${error instanceof Error ? error.message : String(error)}`);
        setReviewError(error instanceof Error ? error.message : 'Failed to apply fixes from deep review');
        setIsReviewFixing(false);
      }
    },
    [apiService, deepReview, loadGitStatus],
  );

  const handleDiffModeChange = useCallback(
    (mode: 'combined' | 'staged' | 'unstaged') => {
      setDiffMode(mode);
      if (!activeDiffPath) {
        return;
      }

      openWorkspaceBuffer({
        kind: 'diff',
        path: `__workspace/diff/${activeDiffPath}`,
        title: `Diff: ${activeDiffPath.split('/').pop() || activeDiffPath}`,
        ext: '.diff',
        metadata: {
          sourcePath: activeDiffPath,
          diff: activeDiff,
          diffMode: mode,
        },
      });
    },
    [activeDiff, activeDiffPath, openWorkspaceBuffer],
  );

  const currentBranch = gitBranches.current;

  const handleCheckoutBranch = useCallback(
    (branch: string) => {
      if (!branch.trim() || branch === currentBranch) return;
      runGitAction(async () => {
        await apiService.checkoutGitBranch(branch);
      }, `Failed to checkout ${branch}`);
    },
    [apiService, currentBranch, runGitAction],
  );

  const handleCreateBranch = useCallback(
    (name: string) => {
      const trimmed = name.trim();
      if (!trimmed) return;
      runGitAction(async () => {
        await apiService.createGitBranch(trimmed);
      }, `Failed to create branch ${trimmed}`);
    },
    [apiService, runGitAction],
  );

  const handlePull = useCallback(() => {
    runGitAction(async () => {
      await apiService.pullGit();
    }, 'Failed to pull changes');
  }, [apiService, runGitAction]);

  const handlePush = useCallback(() => {
    runGitAction(async () => {
      await apiService.pushGit();
    }, 'Failed to push changes');
  }, [apiService, runGitAction]);

  return {
    gitStatus,
    gitBranches,
    commitMessage,
    setCommitMessage,
    selectedFiles,
    activeDiffSelectionKey,
    activeDiffPath,
    activeDiff,
    diffMode,
    isDiffLoading,
    diffError,
    isGitLoading,
    isGitActing,
    isGeneratingCommitMessage,
    gitActionError,
    gitActionWarning,
    isReviewLoading,
    isReviewFixing,
    reviewError,
    reviewFixResult,
    reviewFixLogs,
    reviewFixSessionID,
    deepReview,
    stageablePaths,
    unstageablePaths,
    discardablePaths,
    handleToggleFileSelection,
    handleToggleSectionSelection,
    clearSelectedFiles,
    handlePreviewGitFile,
    handleStageSelected,
    handleUnstageSelected,
    handleDiscardSelected,
    handleStageFile,
    handleUnstageFile,
    handleDiscardFile,
    handleSectionAction,
    handleGitCommitClick,
    handleGenerateCommitMessage,
    handleRunReview,
    handleFixFromReview,
    handleDiffModeChange,
    handleCheckoutBranch,
    handleCreateBranch,
    handlePull,
    handlePush,
    refreshGitStatus: loadGitStatus,
  };
};
