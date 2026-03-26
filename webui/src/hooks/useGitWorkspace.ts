import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiService } from '../services/api';
import { GitStatusData } from '../components/GitSidebarPanel';

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
}

export type FileSection = 'staged' | 'modified' | 'untracked' | 'deleted';

const selectionKey = (section: FileSection, path: string): string => `${section}:${path}`;

const parseSelectionKey = (key: string): { section: FileSection; path: string } | null => {
  const separatorIndex = key.indexOf(':');
  if (separatorIndex <= 0) {
    return null;
  }

  const section = key.slice(0, separatorIndex) as FileSection;
  const path = key.slice(separatorIndex + 1);
  if (!path || !['staged', 'modified', 'untracked', 'deleted'].includes(section)) {
    return null;
  }

  return { section, path };
};

interface UseGitWorkspaceOptions {
  apiService: ApiService;
  gitRefreshToken: number;
  selectedGitFilePath?: string | null;
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onGitCommit: (message: string, files: string[]) => Promise<unknown>;
  onGitAICommit: () => Promise<string>;
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
    metadata?: Record<string, any>;
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
      const data = await apiService.getGitStatus();
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
    } catch (error) {
      console.error('Failed to load git status:', error);
      setGitStatus(null);
      setGitActionError(error instanceof Error ? error.message : 'Failed to load git status');
    } finally {
      setIsGitLoading(false);
    }
  }, [apiService]);

  useEffect(() => {
    loadGitStatus();
  }, [loadGitStatus, gitRefreshToken]);

  const loadDiff = useCallback(async (filePath: string) => {
    setIsDiffLoading(true);
    setDiffError(null);
    try {
      const response = await apiService.getGitDiff(filePath);
      setActiveDiff(response);
      const nextMode = response.has_staged && !response.has_unstaged
        ? 'staged'
        : (!response.has_staged && response.has_unstaged ? 'unstaged' : 'combined');
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
        }
      });
    } catch (error) {
      console.error('Failed to load diff:', error);
      setDiffError(error instanceof Error ? error.message : 'Failed to load diff');
      setActiveDiff(null);
    } finally {
      setIsDiffLoading(false);
    }
  }, [apiService, openWorkspaceBuffer]);

  useEffect(() => {
    if (!selectedGitFilePath) {
      return;
    }
    setActiveDiffSelectionKey(null);
    setActiveDiffPath(selectedGitFilePath);
    loadDiff(selectedGitFilePath);
  }, [loadDiff, selectedGitFilePath]);

  const selectedEntries = useMemo(() => (
    Array.from(selectedFiles)
      .map(parseSelectionKey)
      .filter((entry): entry is { section: FileSection; path: string } => entry !== null)
  ), [selectedFiles]);

  const stageablePaths = useMemo(() => Array.from(new Set(
    selectedEntries
      .filter((entry) => entry.section !== 'staged')
      .map((entry) => entry.path)
  )), [selectedEntries]);

  const unstageablePaths = useMemo(() => Array.from(new Set(
    selectedEntries
      .filter((entry) => entry.section === 'staged')
      .map((entry) => entry.path)
  )), [selectedEntries]);

  const discardablePaths = useMemo(() => Array.from(new Set(
    selectedEntries
      .filter((entry) => entry.section === 'modified' || entry.section === 'deleted')
      .map((entry) => entry.path)
  )), [selectedEntries]);

  const runGitAction = useCallback(async (action: () => Promise<unknown>, fallbackMessage: string) => {
    setGitActionError(null);
    setIsGitActing(true);
    try {
      await action();
      await loadGitStatus();
      setSelectedFiles(new Set());
    } catch (error) {
      setGitActionError(error instanceof Error ? error.message : fallbackMessage);
    } finally {
      setIsGitActing(false);
    }
  }, [loadGitStatus]);

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

  const handleToggleSectionSelection = useCallback((section: FileSection) => {
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
  }, [gitStatus]);

  const handlePreviewGitFile = useCallback((section: FileSection, filePath: string) => {
    setActiveDiffSelectionKey(selectionKey(section, filePath));
    setActiveDiffPath(filePath);
    loadDiff(filePath);
    onViewChange('editor');
  }, [loadDiff, onViewChange]);

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

  const handleStageFile = useCallback((filePath: string) => {
    runGitAction(() => onGitStage([filePath]), `Failed to stage ${filePath}`);
  }, [onGitStage, runGitAction]);

  const handleUnstageFile = useCallback((filePath: string) => {
    runGitAction(() => onGitUnstage([filePath]), `Failed to unstage ${filePath}`);
  }, [onGitUnstage, runGitAction]);

  const handleDiscardFile = useCallback((filePath: string) => {
    runGitAction(() => onGitDiscard([filePath]), `Failed to discard ${filePath}`);
  }, [onGitDiscard, runGitAction]);

  const handleSectionAction = useCallback((section: FileSection) => {
    const files = gitStatus?.[section] || [];
    if (files.length === 0) return;

    if (section === 'staged') {
      runGitAction(() => onGitUnstage(files.map((file) => file.path)), 'Failed to unstage section');
      return;
    }

    if (section === 'modified' || section === 'untracked' || section === 'deleted') {
      runGitAction(() => onGitStage(files.map((file) => file.path)), 'Failed to stage section');
    }
  }, [gitStatus, onGitStage, onGitUnstage, runGitAction]);

  const handleGitCommitClick = useCallback(() => {
    if (!commitMessage.trim() || !gitStatus?.staged.length) return;
    runGitAction(async () => {
      await onGitCommit(commitMessage, gitStatus.staged.map((file) => file.path));
      setCommitMessage('');
      setDeepReview(null);
    }, 'Failed to create commit');
  }, [commitMessage, gitStatus, onGitCommit, runGitAction]);

  const handleGenerateCommitMessage = useCallback(() => {
    if (!gitStatus?.staged.length) return;
    setGitActionError(null);
    setIsGeneratingCommitMessage(true);
    onGitAICommit()
      .then((generatedMessage) => {
        if (!generatedMessage || !generatedMessage.trim()) {
          throw new Error('AI returned an empty commit message');
        }
        setCommitMessage(generatedMessage.trim());
      })
      .catch((error) => {
        setGitActionError(error instanceof Error ? error.message : 'Failed to generate commit message');
      })
      .finally(() => {
        setIsGeneratingCommitMessage(false);
      });
  }, [gitStatus, onGitAICommit]);

  const handleRunReview = useCallback(async () => {
    setReviewError(null);
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
        metadata: { reviewGeneratedAt: Date.now() }
      });
      onViewChange('editor');
    } catch (error) {
      setReviewError(error instanceof Error ? error.message : 'Failed to generate deep review');
      setDeepReview(null);
    } finally {
      setIsReviewLoading(false);
    }
  }, [apiService, onViewChange, openWorkspaceBuffer]);

  const handleFixFromReview = useCallback(async () => {
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
        } catch (error) {
          setReviewError(error instanceof Error ? error.message : 'Failed to fetch fix progress');
          setIsReviewFixing(false);
        }
      };

      await poll();
    } catch (error) {
      setReviewError(error instanceof Error ? error.message : 'Failed to apply fixes from deep review');
      setIsReviewFixing(false);
    }
  }, [apiService, deepReview, loadGitStatus]);

  const handleDiffModeChange = useCallback((mode: 'combined' | 'staged' | 'unstaged') => {
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
      }
    });
  }, [activeDiff, activeDiffPath, openWorkspaceBuffer]);

  return {
    gitStatus,
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
  };
};
