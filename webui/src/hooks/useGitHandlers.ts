/**
 * Git operation handlers.
 *
 * Manages git commit, stage, unstage, discard, and AI-generated commit
 * message handlers. Uses clientFetch for direct API calls.
 */

import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';

export interface UseGitHandlersOptions {
  setGitRefreshToken: Dispatch<SetStateAction<number>>;
}

export interface UseGitHandlersReturn {
  handleGitCommit: (message: string, files: string[]) => Promise<unknown>;
  handleGitAICommit: () => Promise<{ commitMessage: string; warnings?: string[] }>;
  handleGitStage: (files: string[]) => Promise<void>;
  handleGitUnstage: (files: string[]) => Promise<void>;
  handleGitDiscard: (files: string[]) => Promise<void>;
}

export function useGitHandlers({ setGitRefreshToken }: UseGitHandlersOptions): UseGitHandlersReturn {
  const handleGitCommit = useCallback(
    async (message: string, files: string[]) => {
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
    },
    [setGitRefreshToken],
  );

  const handleGitAICommit = useCallback(async (): Promise<{
    commitMessage: string;
    warnings?: string[];
  }> => {
    const response = await clientFetch('/api/git/commit-message', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });

    if (!response.ok) {
      const text = await response.text();
      let errorData: { message?: string } | null = null;
      try {
        errorData = JSON.parse(text);
      } catch {
        // Backend returned plain text error (http.Error), not JSON.
      }
      throw new Error(errorData?.message || text || 'Failed to generate commit message');
    }

    const data = await response.json();
    return {
      commitMessage: data.commit_message || '',
      warnings: data.warnings || [],
    };
  }, []);

  const handleGitStage = useCallback(
    async (files: string[]) => {
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
    },
    [setGitRefreshToken],
  );

  const handleGitUnstage = useCallback(
    async (files: string[]) => {
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
    },
    [setGitRefreshToken],
  );

  const handleGitDiscard = useCallback(
    async (files: string[]) => {
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
    },
    [setGitRefreshToken],
  );

  return {
    handleGitCommit,
    handleGitAICommit,
    handleGitStage,
    handleGitUnstage,
    handleGitDiscard,
  };
}
