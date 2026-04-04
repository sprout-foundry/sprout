/**
 * Git operations hook.
 *
 * Provides handlers for git commit (manual & AI-generated), staging,
 * unstaging, and discarding changes. Includes a refresh token to
 * trigger re-fetches of the git status view.
 */

import { useState, useCallback } from 'react';
import { clientFetch } from '../services/clientSession';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';

interface GitAICommitResult {
  commitMessage: string;
  warnings?: string[];
}

export interface UseGitActionsReturn {
  gitRefreshToken: number;
  handleGitCommit: (message: string, files: string[]) => Promise<Record<string, unknown>>;
  handleGitAICommit: () => Promise<GitAICommitResult>;
  handleGitStage: (files: string[]) => Promise<void>;
  handleGitUnstage: (files: string[]) => Promise<void>;
  handleGitDiscard: (files: string[]) => Promise<void>;
}

export function useGitActions(): UseGitActionsReturn {
  const [gitRefreshToken, setGitRefreshToken] = useState(0);
  const apiService = ApiService.getInstance();

  const handleGitCommit = useCallback(async (message: string, files: string[]) => {
    debugLog('Git commit:', message, files);
    try {
      const response = await clientFetch('/api/git/commit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, files }),
      });

      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(errorData.message || 'Failed to create commit');
      }

      const data = await response.json();
      debugLog('Commit successful:', data);
      setGitRefreshToken((k) => k + 1);
      return data;
    } catch (err) {
      console.error('Failed to commit:', err);
      throw err;
    }
  }, []);

  const handleGitAICommit = useCallback(async (): Promise<GitAICommitResult> => {
    const response = await apiService.generateCommitMessage();
    return {
      commitMessage: response.commit_message || '',
      warnings: response.warnings || [],
    };
  }, [apiService]);

  const handleGitStage = useCallback(async (files: string[]) => {
    debugLog('Git stage:', files);
    try {
      for (const file of files) {
        const response = await clientFetch('/api/git/stage', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: file }),
        });
        if (!response.ok) {
          throw new Error(`Failed to stage ${file}`);
        }
      }
      setGitRefreshToken((k) => k + 1);
    } catch (err) {
      console.error('Failed to stage files:', err);
      throw err;
    }
  }, []);

  const handleGitUnstage = useCallback(async (files: string[]) => {
    debugLog('Git unstage:', files);
    try {
      for (const file of files) {
        const response = await clientFetch('/api/git/unstage', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: file }),
        });
        if (!response.ok) {
          throw new Error(`Failed to unstage ${file}`);
        }
      }
      setGitRefreshToken((k) => k + 1);
    } catch (err) {
      console.error('Failed to unstage files:', err);
      throw err;
    }
  }, []);

  const handleGitDiscard = useCallback(async (files: string[]) => {
    debugLog('Git discard:', files);
    try {
      for (const file of files) {
        const response = await clientFetch('/api/git/discard', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: file }),
        });
        if (!response.ok) {
          throw new Error(`Failed to discard ${file}`);
        }
      }
      setGitRefreshToken((k) => k + 1);
    } catch (err) {
      console.error('Failed to discard files:', err);
      throw err;
    }
  }, []);

  return {
    gitRefreshToken,
    handleGitCommit,
    handleGitAICommit,
    handleGitStage,
    handleGitUnstage,
    handleGitDiscard,
  };
}
