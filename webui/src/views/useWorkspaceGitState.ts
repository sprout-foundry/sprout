/**
 * The git state a composed space shell runs on: one `useGitWorkspace`
 * instance — the hook the standalone app's root runs — feeding the sidebar's
 * Source control section, the status bar's branch, and the chat's diff and
 * review tabs, so the three agree as they do in the app.
 *
 * Diffs, commit diffs and reviews open as editor tabs (the app's behavior):
 * the composed shell renders the full editor, which shows them.
 *
 * Mount only when the host declares `capabilities.git` — the hook loads the
 * git status as soon as it mounts.
 */

import { useEffect, useMemo, useState } from 'react';
import { transportUsesBrowserGit } from '../components/git/browserGitHost';
import { buildGitPanelProps } from '../components/git/gitPanelProps';
import type { SidebarGitPanelProps } from '../components/git/gitPanelProps';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import { useGitHandlers } from '../hooks/useGitHandlers';
import { useGitWorkspace } from '../hooks/useGitWorkspace';
import type { UseGitWorkspaceResult } from '../hooks/useGitWorkspace';
import { useHost } from '../host';
import { ADAPTER_INSTALLED_EVENT } from '../services/apiAdapter';
import type { WorkspaceChatDiffState, WorkspaceChatReviewProps } from './WorkspaceChatContext';

/** Fired by the startup repo import once the repository is in the workspace. */
const REPO_IMPORTED_EVENT = 'sprout:repo-imported';

export interface WorkspaceGitState {
  /** The git workspace the app's root runs (status, branches, diffs, review). */
  workspace: UseGitWorkspaceResult;
  /** The Source control section's props. */
  panel: SidebarGitPanelProps;
  /** The review tab's state, for the chat unit's `review` override. */
  review: WorkspaceChatReviewProps;
  /** The diff tab's state, for the chat unit's `diff` override. */
  diff: WorkspaceChatDiffState;
}

/** The editor shows a diff or review tab as soon as it opens; there is no view to switch. */
const noViewChange = (): void => undefined;

export function useWorkspaceGitState({
  onOpenFile,
}: {
  /** Open a working-tree file in the editor. */
  onOpenFile: (path: string) => void;
}): WorkspaceGitState {
  const fetchFn = useSproutFetch();
  const host = useHost();
  const { openWorkspaceBuffer } = useEditorManager();
  const [refreshToken, setRefreshToken] = useState(0);
  const handlers = useGitHandlers({ setGitRefreshToken: setRefreshToken });

  // An in-browser host's first status load can run before its agent's adapter
  // is installed or the project's repository is imported; reload when either
  // lands so the panel reports the repository rather than its absence.
  useEffect(() => {
    const reload = () => setRefreshToken((token) => token + 1);
    window.addEventListener(ADAPTER_INSTALLED_EVENT, reload);
    window.addEventListener(REPO_IMPORTED_EVENT, reload);
    return () => {
      window.removeEventListener(ADAPTER_INSTALLED_EVENT, reload);
      window.removeEventListener(REPO_IMPORTED_EVENT, reload);
    };
  }, []);

  const workspace = useGitWorkspace({
    fetchFn,
    gitRefreshToken: refreshToken,
    onViewChange: noViewChange,
    onGitCommit: handlers.handleGitCommit,
    onGitAICommit: handlers.handleGitAICommit,
    onGitStage: handlers.handleGitStage,
    onGitUnstage: handlers.handleGitUnstage,
    onGitDiscard: handlers.handleGitDiscard,
    openWorkspaceBuffer,
  });

  // The review runs the agent over the working tree on the backend; browser
  // git has no such backend, so the action is not offered there.
  const reviewAvailable = !transportUsesBrowserGit(host.transport);
  const panel: SidebarGitPanelProps = {
    ...buildGitPanelProps(workspace, { openWorkspaceBuffer, onOpenFile }),
    ...(reviewAvailable ? {} : { onRunReview: undefined }),
  };

  const {
    deepReview,
    reviewError,
    reviewFixResult,
    reviewFixLogs,
    reviewFixSessionID,
    isReviewLoading,
    isReviewFixing,
    handleFixFromReview,
    activeDiffPath,
    activeDiff,
    diffMode,
    isDiffLoading,
    diffError,
    handleDiffModeChange,
  } = workspace;
  const review = useMemo<WorkspaceChatReviewProps>(
    () => ({
      review: deepReview,
      reviewError,
      reviewFixResult,
      reviewFixLogs,
      reviewFixSessionID,
      isReviewLoading,
      isReviewFixing,
      onFixFromReview: handleFixFromReview,
    }),
    [
      deepReview,
      reviewError,
      reviewFixResult,
      reviewFixLogs,
      reviewFixSessionID,
      isReviewLoading,
      isReviewFixing,
      handleFixFromReview,
    ],
  );
  const diff = useMemo<WorkspaceChatDiffState>(
    () => ({ activeDiffPath, activeDiff, diffMode, isDiffLoading, diffError, onDiffModeChange: handleDiffModeChange }),
    [activeDiffPath, activeDiff, diffMode, isDiffLoading, diffError, handleDiffModeChange],
  );

  return { workspace, panel, review, diff };
}
