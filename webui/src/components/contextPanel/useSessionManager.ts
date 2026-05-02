import { useState, useEffect, useCallback } from 'react';
import { useLog } from '../../utils/log';
import { showThemedConfirm } from '../ThemedDialog';
import type {
  ChatContextPanelProps,
  ChatTabId,
  SessionEntry,
} from './types';

export function useSessionManager(
  chatProps: ChatContextPanelProps | null,
  chatTab: ChatTabId,
  isProcessing: boolean,
) {
  const log = useLog();
  const [sessions, setSessions] = useState<SessionEntry[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string>('');
  const [isLoadingSessions, setIsLoadingSessions] = useState(false);
  const [sessionRestoreError, setSessionRestoreError] = useState<string | null>(null);
  const [sessionsCount, setSessionsCount] = useState(0);

  const loadSessions = useCallback(async () => {
    if (!chatProps) return;
    setIsLoadingSessions(true);
    try {
      const response = await chatProps.onLoadSessions();
      setSessions(response.sessions || []);
      setCurrentSessionId(response.current_session_id || '');
      setSessionsCount(response.sessions?.length || 0);
    } catch (error) {
      log.error(`Failed to load sessions: ${error instanceof Error ? error.message : String(error)}`, {
        title: 'Session Load Error',
      });
    } finally {
      setIsLoadingSessions(false);
    }
  }, [chatProps, log]);

  const handleRestoreSession = useCallback(
    async (sessionId: string) => {
      if (!chatProps) return;
      if (isProcessing) {
        setSessionRestoreError('Wait for current request to finish.');
        return;
      }
      if (sessionId === currentSessionId) {
        setSessionRestoreError('This is the current session.');
        return;
      }
      if (
        !(await showThemedConfirm(
          `Restore session ${sessionId}?\n\nThis will replace the current conversation.`,
          {
            title: 'Restore Session',
            type: 'warning',
          }
        ))
      ) {
        return;
      }
      setIsLoadingSessions(true);
      setSessionRestoreError(null);
      try {
        const response = await chatProps.onRestoreSession(sessionId);
        if (response.messages?.length) {
          setTimeout(() => {
            window.dispatchEvent(
              new CustomEvent('sprout:session-restored', {
                detail: { messages: response.messages },
              })
            );
          }, 400);
        }
        await loadSessions();
      } catch (error) {
        setSessionRestoreError(error instanceof Error ? error.message : 'Failed to restore session');
      } finally {
        setIsLoadingSessions(false);
      }
    },
    [chatProps, currentSessionId, isProcessing, loadSessions],
  );

  // Auto-load on tab switch
  useEffect(() => {
    if (chatTab === 'sessions' && sessionsCount === 0 && !isLoadingSessions) {
      loadSessions();
    }
  }, [chatTab, sessionsCount, isLoadingSessions, loadSessions]);

  return {
    sessions,
    currentSessionId,
    isLoadingSessions,
    sessionRestoreError,
    sessionsCount,
    loadSessions,
    handleRestoreSession,
  };
}
