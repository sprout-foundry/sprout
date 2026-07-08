import { useState, useEffect, useCallback, useRef } from 'react';
import { useLog } from '../../utils/log';
import { ApiService } from '../../services/api';
import type { SessionSearchResult } from '../../services/api/types/session';
import { showThemedConfirm } from '../ThemedDialog';
import type { ChatContextPanelProps, ChatTabId, SessionEntry } from './types';

export function useSessionManager(chatProps: ChatContextPanelProps | null, chatTab: ChatTabId, isProcessing: boolean) {
  const log = useLog();
  const [sessions, setSessions] = useState<SessionEntry[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string>('');
  const [isLoadingSessions, setIsLoadingSessions] = useState(false);
  const [sessionRestoreError, setSessionRestoreError] = useState<string | null>(null);
  const [sessionsCount, setSessionsCount] = useState(0);

  // ── Session search state (relocated from Sidebar.tsx) ───────────────
  const [sessionSearchQuery, setSessionSearchQuery] = useState('');
  const [sessionSearchResults, setSessionSearchResults] = useState<SessionSearchResult[]>([]);
  const [sessionSearchLoading, setSessionSearchLoading] = useState(false);
  const [sessionSearchError, setSessionSearchError] = useState<string | null>(null);
  const [sessionSearchFocused, setSessionSearchFocused] = useState(false);

  // ── Export-all state (relocated from Sidebar.tsx) ────────────────────
  const [isExportingAll, setIsExportingAll] = useState(false);
  const [exportAllError, setExportAllError] = useState<string | null>(null);

  // Debounced search execution
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

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
        !(await showThemedConfirm(`Restore session ${sessionId}?\n\nThis will replace the current conversation.`, {
          title: 'Restore Session',
          type: 'warning',
        }))
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
              }),
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

  // ── Session search handlers (relocated from Sidebar.tsx) ────────────
  const executeSessionSearch = useCallback(async (q: string) => {
    if (!q.trim()) {
      setSessionSearchResults([]);
      setSessionSearchError(null);
      return;
    }
    setSessionSearchLoading(true);
    setSessionSearchError(null);
    try {
      const resp = await ApiService.getInstance().searchSessions(q.trim(), { limit: 20 });
      setSessionSearchResults(resp.results || []);
    } catch (err) {
      setSessionSearchError(err instanceof Error ? err.message : 'Search failed');
      setSessionSearchResults([]);
    } finally {
      setSessionSearchLoading(false);
    }
  }, []);

  const handleSessionSearchChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setSessionSearchQuery(value);

      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
      }

      if (!value.trim()) {
        setSessionSearchResults([]);
        setSessionSearchError(null);
        setSessionSearchLoading(false);
        return;
      }

      searchTimerRef.current = setTimeout(() => {
        executeSessionSearch(value);
      }, 300);
    },
    [executeSessionSearch],
  );

  const handleSessionSearchClear = useCallback(() => {
    setSessionSearchQuery('');
    setSessionSearchResults([]);
    setSessionSearchError(null);
    setSessionSearchLoading(false);
    if (searchTimerRef.current) {
      clearTimeout(searchTimerRef.current);
    }
  }, []);

  const handleSessionSearchBlur = useCallback(() => {
    // Delay so result clicks register before the dropdown closes.
    setTimeout(() => setSessionSearchFocused(false), 150);
  }, []);

  const handleSessionSearchFocus = useCallback(() => {
    setSessionSearchFocused(true);
  }, []);

  const handleSessionSearchResultClick = useCallback(
    async (sessionId: string) => {
      // Search-result click bypasses the restore confirm prompt that the
      // session list uses — picking from a search result is already an
      // explicit selection. Restore directly via chatProps (matches the
      // original Sidebar.tsx flow that called onSessionSearchRestore
      // without confirmation).
      if (!chatProps) return;
      try {
        const response = await chatProps.onRestoreSession(sessionId);
        if (response.messages?.length) {
          setTimeout(() => {
            window.dispatchEvent(
              new CustomEvent('sprout:session-restored', {
                detail: { messages: response.messages },
              }),
            );
          }, 400);
        }
        await loadSessions();
      } catch (err) {
        setSessionSearchError(err instanceof Error ? err.message : 'Failed to restore session');
      } finally {
        setSessionSearchQuery('');
        setSessionSearchResults([]);
        setSessionSearchError(null);
        setSessionSearchLoading(false);
        setSessionSearchFocused(false);
      }
    },
    [chatProps, loadSessions],
  );

  // ── Export-all handler (relocated from Sidebar.tsx) ─────────────────
  const handleExportAllSessions = useCallback(async () => {
    if (isExportingAll) return;
    setExportAllError(null);
    setIsExportingAll(true);

    try {
      const response = await ApiService.getInstance().getSessions('current');
      const sessionsToExport = Array.isArray(response?.sessions)
        ? response.sessions.filter((s) => s.message_count > 0)
        : [];

      for (const session of sessionsToExport) {
        // Bulk export defaults to safe/redacted. Users who need unredacted
        // exports should use the per-session ExportDialog.
        const url = `/api/sessions/${encodeURIComponent(session.session_id)}/export?format=markdown&include_tool_calls=false&include_cost=true`;

        // HEAD pre-check: skip silently if the session was deleted between
        // getSessions() and now.
        try {
          const headResp = await fetch(url, { method: 'HEAD' });
          if (!headResp.ok) {
            console.warn(`[export-all] Skipping session ${session.session_id}: HEAD ${headResp.status}`);
            continue;
          }
        } catch {
          console.warn(`[export-all] Skipping session ${session.session_id}: HEAD request failed`);
          continue;
        }

        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = '';
        document.body.appendChild(anchor);
        anchor.click();
        document.body.removeChild(anchor);

        // Small delay avoids the browser blocking multiple downloads.
        await new Promise((resolve) => setTimeout(resolve, 300));
      }
    } catch (err) {
      setExportAllError(`Failed to export sessions: ${err instanceof Error ? err.message : 'Unknown error'}`);
    } finally {
      setIsExportingAll(false);
    }
  }, [isExportingAll]);

  // Show search dropdown when focused + has a query, or has active results.
  const showSessionSearchDropdown =
    sessionSearchFocused &&
    (sessionSearchQuery.trim().length > 0 ||
      sessionSearchLoading ||
      sessionSearchResults.length > 0 ||
      sessionSearchError !== null);

  // Clean up the debounce timer on unmount.
  useEffect(() => {
    return () => {
      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
      }
    };
  }, []);

  // Auto-load on tab switch
  useEffect(() => {
    if (chatTab === 'sessions' && sessionsCount === 0 && !isLoadingSessions) {
      loadSessions();
    }
  }, [chatTab, sessionsCount, isLoadingSessions, loadSessions]);

  return {
    // Session list (existing)
    sessions,
    currentSessionId,
    isLoadingSessions,
    sessionRestoreError,
    sessionsCount,
    loadSessions,
    handleRestoreSession,
    // Session search (relocated from Sidebar.tsx)
    sessionSearchQuery,
    sessionSearchResults,
    sessionSearchLoading,
    sessionSearchError,
    showSessionSearchDropdown,
    handleSessionSearchChange,
    handleSessionSearchClear,
    handleSessionSearchBlur,
    handleSessionSearchFocus,
    handleSessionSearchResultClick,
    // Export all (relocated from Sidebar.tsx)
    isExportingAll,
    exportAllError,
    handleExportAllSessions,
  };
}
