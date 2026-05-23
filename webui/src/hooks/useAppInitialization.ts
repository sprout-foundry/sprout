/**
 * Application initialization side-effect.
 *
 * Runs a single useEffect on mount that registers the service worker,
 * opens the WebSocket connection, loads initial stats/files/chat
 * sessions, restores the workspace/session startup state, and sets up
 * the periodic stats polling and mobile resize listener.
 * Returns nothing — this is a fire-and-forget initialisation hook.
 */

import type { EventsProvider } from '@sprout/events';
import { useEffect } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { ApiService } from '../services/api';
import type { StatsResponse, FilesResponse } from '../services/api';
import type { SessionEntry } from '../services/api/types';
import { getTabWorkspacePath } from '../services/clientSession';
import { registerServiceWorker } from '../services/serviceWorkerRegistration';
import type { AppState } from '../types/app';
import type { SproutEvent } from '../types/events';
import { debugLog, useLog } from '../utils/log';

interface RecentFile {
  path: string;
  modified: boolean;
}

export interface UseAppInitializationOptions {
  eventsProvider: EventsProvider;
  handleEvent: (event: SproutEvent) => void;
  connectionTimeoutRef: MutableRefObject<ReturnType<typeof setTimeout> | null>;
  loadChatSessions: () => void;
  setRecentFiles: Dispatch<SetStateAction<RecentFile[]>>;
  setIsMobile: Dispatch<SetStateAction<boolean>>;
  setIsTablet: Dispatch<SetStateAction<boolean>>;
  setState: AppStoreSetState;
  /** Reconnect handler that recovers stuck processing state after WebSocket reconnection. */
  handleReconnect: () => void;
}

export function useAppInitialization({
  eventsProvider,
  handleEvent,
  connectionTimeoutRef,
  loadChatSessions,
  setRecentFiles,
  setIsMobile,
  setIsTablet,
  setState,
  handleReconnect,
}: UseAppInitializationOptions): void {
  const log = useLog();
  const apiService = ApiService.getInstance();

  useEffect(() => {
    // Register Service Worker for PWA functionality
    registerServiceWorker();

    // Initialize WebSocket connection
    eventsProvider.connect();
    eventsProvider.onEvent(handleEvent);
    eventsProvider.onReconnect(handleReconnect);

    // Load initial stats
    const loadStats = () => {
      apiService
        .getStats()
        .then((stats: StatsResponse) => {
          setState((prev) => ({
            // Only update provider/model from stats when the backend
            // has a real value.  An empty string means the agent hasn't
            // been lazily created yet — we should keep whatever the
            // frontend already knows (persisted state, WS event…).
            provider: stats.provider || prev.provider,
            model: stats.model || prev.model,
            stats: JSON.stringify(prev.stats) === JSON.stringify(stats) ? prev.stats : { ...stats },
          }));
        })
        .catch((err) =>
          log.error(`Failed to initialize connection: ${err instanceof Error ? err.message : String(err)}`, {
            title: 'Connection Error',
          }),
        );
    };

    const loadFiles = () => {
      apiService
        .getFiles()
        .then((response: FilesResponse) => {
          if (response && Array.isArray(response.files)) {
            const files = response.files.map((file) => ({
              path: file.path,
              modified: false,
            }));
            setRecentFiles(files);
          }
        })
        .catch((err) =>
          log.error(`Failed to load initial data: ${err instanceof Error ? err.message : String(err)}`, {
            title: 'Initialization Error',
          }),
        );
    };

    // Load initial stats & files
    loadStats();
    loadFiles();

    // Load initial chat sessions
    loadChatSessions();

    // Restore workspace and session startup state
    const restoreStartupState = async () => {
      try {
        const workspace = await apiService.getWorkspace();
        const workspaceRoot = String(workspace?.workspace_root || '').trim();
        const daemonRoot = String(workspace?.daemon_root || '').trim();
        if (workspaceRoot && daemonRoot && workspaceRoot === daemonRoot) {
          const savedWorkspace = getTabWorkspacePath().trim();
          if (savedWorkspace && savedWorkspace !== workspaceRoot) {
            // A previous workspace was explicitly chosen — restore it silently.
            try {
              await apiService.setWorkspace(savedWorkspace);
              return;
            } catch (restoreError) {
              debugLog('[startup] failed to auto-restore saved workspace:', restoreError);
            }
          }
          // Only prompt when there is genuinely no prior choice. If savedWorkspace
          // equals workspaceRoot the user intentionally set their workspace to the
          // daemon root (e.g. home dir) — don't interrupt them with the picker.
          if (!savedWorkspace) {
            window.dispatchEvent(new CustomEvent('sprout:open-workspace-switcher'));
          }
        }
      } catch (error) {
        debugLog('[startup] workspace check failed:', error);
      }

      try {
        const sessionsResponse = await apiService.getSessions('current');
        const sessions = Array.isArray(sessionsResponse?.sessions) ? sessionsResponse.sessions : [];
        const currentSessionId = String(sessionsResponse?.current_session_id || '');
        const currentSession = sessions.find(
          (item: SessionEntry) => String(item?.session_id || '') === currentSessionId,
        );
        const currentHasMessages = Number(currentSession?.message_count || 0) > 0;
        if (!currentHasMessages) {
          const restorable = sessions.find(
            (item: SessionEntry) =>
              String(item?.session_id || '') !== currentSessionId && Number(item?.message_count || 0) > 0,
          );
          if (restorable?.session_id) {
            const restored = await apiService.restoreSession(String(restorable.session_id));
            if (Array.isArray(restored?.messages) && restored.messages.length > 0) {
              window.dispatchEvent(
                new CustomEvent('sprout:session-restored', {
                  detail: { messages: restored.messages },
                }),
              );
            }
          }
        }
      } catch (error) {
        debugLog('[startup] session restore check failed:', error);
      }
    };
    restoreStartupState().catch((err) => {
      debugLog('[startup] Restore startup state failed:', err);
    });

    // Set up periodic stats updates
    const statsInterval = setInterval(loadStats, 5000);

    // Check for mobile screen size
    // Check viewport breakpoints (mobile < 768px, tablet 769-1024px)
    const checkBreakpoints = () => {
      const w = window.innerWidth;
      setIsMobile(w <= 768);
      setIsTablet(w >= 769 && w <= 1024);
    };
    checkBreakpoints();
    window.addEventListener('resize', checkBreakpoints);

    // Snapshot ref value for cleanup (ref.current in cleanup triggers exhaustive-deps)
    const timeoutId = connectionTimeoutRef.current;

    // Cleanup — detach listeners and timers, but DON'T disconnect the WS.
    // WebSocketService is a process-lifetime singleton; calling disconnect()
    // here sets `intentionalClose=true` and exhausts reconnectAttempts,
    // which permanently kills the connection if this effect ever runs
    // twice (React 18 StrictMode dev double-invoke, error-boundary retry,
    // or a dep change). The backend then sees a single connect/close
    // pair and the app sits in a disconnected state with no recovery.
    // Leaving the WS alive across remounts is safe: the next effect run
    // re-registers the handler and reuses the existing connection.
    return () => {
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
      eventsProvider.removeEvent(handleEvent);
      eventsProvider.onReconnect(null);
      window.removeEventListener('resize', checkBreakpoints);
      clearInterval(statsInterval);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- setState, setRecentFiles, setIsMobile, setIsTablet are stable useState setters; connectionTimeoutRef is a stable ref; eventsProvider/apiService are stable from hooks/singletons; loadChatSessions is stable (empty useCallback deps); handleReconnect is stable (useCallback with empty deps)
  }, [handleEvent, loadChatSessions, eventsProvider]);
}
