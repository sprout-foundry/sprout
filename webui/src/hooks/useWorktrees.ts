/**
 * Hook for managing git worktrees in the workspace.
 *
 * Provides functionality to:
 * - List available worktrees
 * - Create new worktrees
 * - Remove existing worktrees
 * - Switch between worktrees
 */

import { useCallback, useState, useEffect } from 'react';
import {
  listWorktrees,
  createWorktree,
  removeWorktree,
  checkoutWorktree,
  type WorktreeInfo,
} from '../services/chatSessions';
import { debugLog } from '../utils/log';

export interface UseWorktreesReturn {
  worktrees: WorktreeInfo[];
  currentBranch: string;
  isLoading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  createWorktree: (path: string, branch: string, baseRef?: string) => Promise<string | null>;
  removeWorktree: (path: string) => Promise<void>;
  checkoutWorktree: (path: string) => Promise<void>;
}

export function useWorktrees(): UseWorktreesReturn {
  const [worktrees, setWorktrees] = useState<WorktreeInfo[]>([]);
  const [currentBranch, setCurrentBranch] = useState<string>('');
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await listWorktrees();
      setWorktrees(response.worktrees);
      setCurrentBranch(response.current);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load worktrees';
      setError(message);
      debugLog('[worktrees] Failed to load worktrees:', err);
      setWorktrees([]);
      setCurrentBranch('');
    } finally {
      setIsLoading(false);
    }
  }, []);

  // Load worktrees on mount
  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const createWorktreeHandler = useCallback(async (path: string, branch: string, baseRef?: string): Promise<string | null> => {
    try {
      await createWorktree(path, branch, baseRef);
      // Refresh the list after creating
      await refresh();
      return path;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create worktree';
      setError(message);
      debugLog('[worktrees] Failed to create worktree:', err);
      return null;
    }
  }, [refresh]);

  const removeWorktreeHandler = useCallback(async (path: string): Promise<void> => {
    try {
      await removeWorktree(path);
      // Refresh the list after removing
      await refresh();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to remove worktree';
      setError(message);
      debugLog('[worktrees] Failed to remove worktree:', err);
    }
  }, [refresh]);

  const checkoutWorktreeHandler = useCallback(async (path: string): Promise<void> => {
    try {
      await checkoutWorktree(path);
      // Refresh the list after switching
      await refresh();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to checkout worktree';
      setError(message);
      debugLog('[worktrees] Failed to checkout worktree:', err);
    }
  }, [refresh]);

  return {
    worktrees,
    currentBranch,
    isLoading,
    error,
    refresh,
    createWorktree: createWorktreeHandler,
    removeWorktree: removeWorktreeHandler,
    checkoutWorktree: checkoutWorktreeHandler,
  };
}
