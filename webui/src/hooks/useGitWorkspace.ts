import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useEvents } from '../contexts/EventsContext';
import * as gitApi from '../services/api/gitApi';
import * as miscApi from '../services/api/miscApi';
import * as workspaceApi from '../services/api/workspaceApi';
import { notificationBus } from '../services/notificationBus';
import type { SproutEvent } from '../types/events';
import type {
  GitStatusData,
  FileSection,
  GitCommitSummary,
  GitCommitDetail,
  GitBranchesState,
} from '../types/git-types';
import { selectionKey, parseSelectionKey } from '../types/git-types';
import { useLog, debugLog, warn } from '../utils/log';
import { showThemedConfirm } from '../components/ThemedDialog';

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

interface UseGitWorkspaceOptions {
  fetchFn: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  gitRefreshToken: number;
  selectedGitFilePath?: string | null;
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onGitCommit: (message: string, files: string[]) => Promise<unknown>;
  onGitAICommit: () => Promise<{ commitMessage: string; warnings?: string[] }>;
  onGitStage: (files: string[]) => Promise<void>;
  onGitUnstage: (files: string[]) => Promise<void>;
  onGitDiscard: (files: string[]) => Promise<void>;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'compare';
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
  fetchFn,
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
  const events = useEvents();

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
  const [workspaceRoot, setWorkspaceRoot] = useState<string>('');
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
        gitApi.getGitStatus(fetchFn),
        gitApi.getGitBranches(fetchFn).catch((err) => {
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

      // Not in a git repository — show the "No git repository found" message.
      if (!data.in_git_repo) {
        setGitStatus(null);
        setGitActionWarning(null);
        setGitBranches({ current: '', branches: [] });
        return;
      }

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
  }, [fetchFn, log]);

  useEffect(() => {
    loadGitStatus();
  }, [loadGitStatus, gitRefreshToken]);

  // Fetch workspace root once on mount (workspace rarely changes during a session).
  useEffect(() => {
    workspaceApi
      .getWorkspace(fetchFn)
      .then((ws) => {
        if (ws?.workspace_root) {
          setWorkspaceRoot(String(ws.workspace_root));
        }
      })
      .catch((err) => {
        debugLog('[useGitWorkspace] failed to fetch workspace root:', err);
      });
  }, [fetchFn]);

  // Debounced git status refresh on file change and tool completion events.
  // When files are written (editor save, agent edits, search replace, shell
  // commands), the git panel should reflect the new status.
  // Uses a 2s debounce to coalesce rapid agent edits into a single refresh.
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    const scheduleRefresh = () => {
      if (debounceTimerRef.current !== null) {
        clearTimeout(debounceTimerRef.current);
      }
      debounceTimerRef.current = setTimeout(() => {
        debounceTimerRef.current = null;
        loadGitStatus();
      }, 2000);
    };

    const handleGitRefreshEvent = (event: SproutEvent) => {
      // 1) file_changed events from the agent's write/edit file tools
      if (event?.type === 'file_changed') {
        const eventData = event.data as Record<string, unknown> | undefined;
        const action = String(eventData?.action || '');
        // Skip git-level actions — those already trigger explicit refreshes
        // via runGitAction → loadGitStatus.
        if (action.startsWith('git_')) return;

        // Only refresh on actual file content changes
        if (!['write', 'edit', 'created', 'deleted'].includes(action)) return;

        scheduleRefresh();
        return;
      }

      // 2) file_content_changed events from the server-side fsnotify file
      //    watcher (e.g., when a file changes on disk from a shell command
      //    or external process, and the file is open in the editor).
      if (event?.type === 'file_content_changed') {
        scheduleRefresh();
        return;
      }

      // 3) tool_end events from file-modifying tools — covers shell_command
      //    (which can run sed/cp/mv/git checkout/etc.) and the structured
      //    file tools that go through writeFileContent on the server.
      if (event?.type === 'tool_end') {
        const eventData = event.data as Record<string, unknown> | undefined;
        if (eventData?.status === 'failed') return; // Don't refresh on failure

        const toolName = String(eventData?.tool_name || '');
        const fileModifyingTools = [
          'shell_command',
          'write_file',
          'edit_file',
          'write_structured_file',
          'patch_structured_file',
        ];
        if (!fileModifyingTools.includes(toolName)) return;

        scheduleRefresh();
        return;
      }
    };

    events.onEvent(handleGitRefreshEvent);
    return () => {
      events.removeEvent(handleGitRefreshEvent);
      if (debounceTimerRef.current !== null) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [events, loadGitStatus]);

  const loadDiff = useCallback(
    async (filePath: string) => {
      setIsDiffLoading(true);
      setDiffError(null);
      try {
        const response = await gitApi.getGitDiff(fetchFn, filePath);
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
    [fetchFn, openWorkspaceBuffer, log],
  );

  useEffect(() => {
    if (!selectedGitFilePath) {
      return;
    }
    setActiveDiffSelectionKey(null);
    setActiveDiffPath(selectedGitFilePath);
    loadDiff(selectedGitFilePath);
  }, [loadDiff, selectedGitFilePath]);

  // When git status refreshes, if the currently previewed file moved to a
  // different section (e.g. user staged it), re-key activeDiffSelectionKey so
  // the row stays highlighted in its new section instead of going dark.
  useEffect(() => {
    if (!gitStatus || !activeDiffSelectionKey) return;
    const parsed = parseSelectionKey(activeDiffSelectionKey);
    if (!parsed) return;
    const stillInOriginalSection = gitStatus[parsed.section]?.some((file) => file.path === parsed.path);
    if (stillInOriginalSection) return;
    const sections: FileSection[] = ['staged', 'modified', 'untracked', 'deleted'];
    for (const section of sections) {
      if (gitStatus[section]?.some((file) => file.path === parsed.path)) {
        setActiveDiffSelectionKey(selectionKey(section, parsed.path));
        return;
      }
    }
    // File no longer present in any section — clear preview key.
    setActiveDiffSelectionKey(null);
  }, [gitStatus, activeDiffSelectionKey]);

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

  const handleSelectFiles = useCallback((keys: string[]) => {
    setSelectedFiles(new Set(keys));
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
    const count = discardablePaths.length;
    const preview = discardablePaths.slice(0, 5).join('\n');
    const more = count > 5 ? `\n…and ${count - 5} more` : '';
    runGitAction(async () => {
      const confirmed = await showThemedConfirm(
        `Discard local changes to ${count} file${count === 1 ? '' : 's'}? This cannot be undone.\n\n${preview}${more}`,
        { title: 'Discard changes', type: 'danger', confirmLabel: 'Discard' },
      );
      if (!confirmed) return;
      await onGitDiscard(discardablePaths);
    }, 'Failed to discard selected files');
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
      runGitAction(async () => {
        const confirmed = await showThemedConfirm(`Discard local changes to ${filePath}? This cannot be undone.`, {
          title: 'Discard changes',
          type: 'danger',
          confirmLabel: 'Discard',
        });
        if (!confirmed) return;
        await onGitDiscard([filePath]);
      }, `Failed to discard ${filePath}`);
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
      const response = await miscApi.generateDeepReview(fetchFn);
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
  }, [fetchFn, onViewChange, openWorkspaceBuffer]);

  const handleFixFromReview = useCallback(
    async (options?: { fixPrompt?: string; selectedItems?: string[] }) => {
      if (!deepReview?.review_output) return;
      setReviewError(null);
      setReviewFixResult(null);
      setReviewFixLogs([]);
      setReviewFixSessionID(null);
      setIsReviewFixing(true);
      try {
        const started = await miscApi.startFixFromDeepReview(fetchFn, deepReview.review_output, options);
        setReviewFixSessionID(started.session_id || null);
        fixPollIndexRef.current = 0;
        setReviewFixLogs((prev) => [...prev, `Started fix session: ${started.session_id}`]);

        const poll = async () => {
          try {
            const status = await miscApi.getFixFromDeepReviewStatus(fetchFn, started.job_id, fixPollIndexRef.current);
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
    [fetchFn, deepReview, loadGitStatus],
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
        await gitApi.checkoutGitBranch(fetchFn, branch);
      }, `Failed to checkout ${branch}`);
    },
    [fetchFn, currentBranch, runGitAction],
  );

  const handleCreateBranch = useCallback(
    (name: string) => {
      const trimmed = name.trim();
      if (!trimmed) return;
      runGitAction(async () => {
        await gitApi.createGitBranch(fetchFn, trimmed);
      }, `Failed to create branch ${trimmed}`);
    },
    [fetchFn, runGitAction],
  );

  const handlePull = useCallback(() => {
    runGitAction(async () => {
      await gitApi.pullGit(fetchFn);
    }, 'Failed to pull changes');
  }, [fetchFn, runGitAction]);

  const handlePush = useCallback(() => {
    runGitAction(async () => {
      await gitApi.pushGit(fetchFn);
    }, 'Failed to push changes');
  }, [fetchFn, runGitAction]);

  const handleCreatePullRequest = useCallback(
    async (params: {
      title: string;
      body?: string;
      base?: string;
      head?: string;
      draft?: boolean;
    }): Promise<{ url: string; number: number; state: string }> => {
      setGitActionError(null);
      setGitActionWarning(null);
      setIsGitActing(true);
      try {
        const result = await gitApi.createPullRequest(fetchFn, params);
        return result;
      } catch (error) {
        warn(`[handleCreatePullRequest] failed: ${error instanceof Error ? error.message : String(error)}`);
        setGitActionError(error instanceof Error ? error.message : 'Failed to create pull request');
        throw error;
      } finally {
        setIsGitActing(false);
      }
    },
    [fetchFn],
  );

  // Git history callbacks
  const handleLoadCommits = useCallback(
    async (limit: number, offset: number, opts?: { signal?: AbortSignal }) => {
      const res = await gitApi.getGitLog(fetchFn, limit, offset, opts);
      return { commits: res.commits, total: res.total };
    },
    [fetchFn],
  );

  const handleLoadCommitDetail = useCallback((hash: string) => gitApi.getGitCommitDetail(fetchFn, hash), [fetchFn]);

  const handleLoadCommitFileDiff = useCallback(
    (hash: string, path: string) => gitApi.getGitCommitFileDiff(fetchFn, hash, path),
    [fetchFn],
  );

  const handleCheckoutCommit = useCallback(
    async (hash: string): Promise<{ message: string }> => {
      setGitActionError(null);
      setGitActionWarning(null);
      setIsGitActing(true);
      try {
        const result = await gitApi.checkoutGitCommit(fetchFn, hash);
        await loadGitStatus();
        setSelectedFiles(new Set());
        return result;
      } catch (error) {
        warn(`[handleCheckoutCommit] failed: ${error instanceof Error ? error.message : String(error)}`);
        setGitActionError(error instanceof Error ? error.message : `Failed to checkout commit ${hash}`);
        throw error;
      } finally {
        setIsGitActing(false);
      }
    },
    [fetchFn, loadGitStatus],
  );

  const handleRevertCommit = useCallback(
    async (hash: string): Promise<{ message: string }> => {
      setGitActionError(null);
      setGitActionWarning(null);
      setIsGitActing(true);
      try {
        const result = await gitApi.revertGitCommit(fetchFn, hash);
        await loadGitStatus();
        setSelectedFiles(new Set());
        return result;
      } catch (error) {
        warn(`[handleRevertCommit] failed: ${error instanceof Error ? error.message : String(error)}`);
        setGitActionError(error instanceof Error ? error.message : 'Failed to revert commit');
        throw error;
      } finally {
        setIsGitActing(false);
      }
    },
    [fetchFn, loadGitStatus],
  );

  return {
    gitStatus,
    gitBranches,
    workspaceRoot,
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
    handleSelectFiles,
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
    handleCreatePullRequest,
    handleLoadCommits,
    handleLoadCommitDetail,
    handleLoadCommitFileDiff,
    handleCheckoutCommit,
    handleRevertCommit,
    refreshGitStatus: loadGitStatus,
  };
};
