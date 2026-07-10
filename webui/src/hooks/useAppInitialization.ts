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
import { isCloud, supportsWorkspaceSwitching } from '../config/mode';
import type { AppStoreSetState } from '../contexts/AppStore';
import { ApiService } from '../services/api';
import type { StatsResponse, FilesResponse } from '../services/api';
import type { SessionEntry } from '../services/api/types';
import { getAdapter } from '../services/apiAdapter';
import { getTabWorkspacePath } from '../services/clientSession';
import type { CloudAdapter } from '../services/cloudAdapter';
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
    // ── Cloud mode: check auth BEFORE anything else ─────────────
    // In cloud mode, redirect to login if not authenticated.
    // All other initialization (WebSocket, data loading, WASM) is
    // gated behind this check to avoid 401 error spam.
    if (isCloud) {
      fetch('/user/me', { credentials: 'include' })
        .then((authRes) => {
          if (!authRes.ok) {
            window.location.href = '/webui/auth/login';
            return;
          }
          initApp();
        })
        .catch(() => initApp());
    } else {
      initApp();
    }

    function initApp() {
      // Register Service Worker for PWA functionality
      registerServiceWorker();

      // Initialize WebSocket connection
      eventsProvider.connect();
      eventsProvider.onEvent(handleEvent);
      eventsProvider.onReconnect(handleReconnect);

      // ── Cloud mode: eagerly preload the WASM shell ──────────────
      // In cloud mode the WASM shell (44 MB) must be compiled and
      // instantiated before any wasm-local endpoint (files, terminal,
      // search) is reachable.  Starting the load here, before stats
      // and file requests fire, eliminates the init-race window where
      // the first /api/files call falls through to the backend (which
      // may return 401 or empty data).
      const wasmPreloadPromise: Promise<boolean> = isCloud
        ? ((getAdapter() as CloudAdapter | null)?.preloadWasmShell() ?? Promise.resolve(false))
        : Promise.resolve(false);

      wasmPreloadPromise.then((ready) => {
        if (ready) {
          debugLog('[startup] WASM shell preloaded successfully');
          setState((prev) => ({ ...prev, wasmReady: true, wasmLoading: false }));

          // Configure browser-native git with VFS access callbacks.
          // isomorphic-git needs to read/write files from the same
          // virtual filesystem the agent uses.
          if (isCloud) {
            import('../services/cloudWasmHandlers').then(({ listAllVfsFiles }) => {
              import('../services/browserGit').then(({ configureBrowserGit }) => {
                const shell = (getAdapter() as CloudAdapter | null)?.getWasmShell?.();
                if (shell) {
                  configureBrowserGit({
                    name: 'Browser IDE',
                    email: 'browser-ide@sprout.dev',
                    readVfsFiles: async () => {
                      return listAllVfsFiles(shell);
                    },
                    writeVfsFiles: async (files) => {
                      for (const f of files) {
                        shell.writeFile(f.path, f.content);
                      }
                    },
                  });
                }
              });
            });
          }
        } else if (isCloud) {
          console.warn('[startup] WASM shell preload failed — falling through to server safety-net');
          setState((prev) => ({ ...prev, wasmLoading: false, wasmError: 'Failed to load browser runtime' }));
        }
      });
      if (isCloud) {
        setState((prev) => ({ ...prev, wasmLoading: true }));
      }

      // ── Cloud mode: wire agent events to the webui ──────────────
      // In cloud mode, the agent loop runs in the WASM binary. Events
      // from the agent are dispatched via the agentEventDispatcher,
      // which feeds them into the same handleEvent that WebSocket
      // events use. This makes agent responses render in the chat UI.
      if (isCloud) {
        import('../services/cloudWasmHandlers').then(({ setAgentEventDispatcher }) => {
          setAgentEventDispatcher((event) => {
            handleEvent(event as SproutEvent);
          });
        });
      }

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

      // In cloud mode, listen for repo import completion to refresh files.
      // The import runs async in bootstrapAdapter.ts and may complete after
      // the initial loadFiles() call returned empty.
      const handleRepoImported = () => {
        debugLog('[startup] repo import completed — refreshing file list');
        loadFiles();
      };
      window.addEventListener('sprout:repo-imported', handleRepoImported);

      // Check if import already completed before we mounted (race condition).
      const importedRepo = (window as unknown as Record<string, unknown>).__repoImported;
      if (importedRepo) {
        debugLog('[startup] repo was already imported before mount — loading files');
        // Small delay to ensure WASM shell writes have settled.
        setTimeout(() => loadFiles(), 500);
      }

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
            // In cloud mode, workspace switching is disabled.
            if (!savedWorkspace && supportsWorkspaceSwitching) {
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
    } // end startDataLoading

    // eslint-disable-next-line react-hooks/exhaustive-deps -- setState, setRecentFiles, setIsMobile, setIsTablet are stable useState setters; connectionTimeoutRef is a stable ref; eventsProvider/apiService are stable from hooks/singletons; loadChatSessions is stable (empty useCallback deps); handleReconnect is stable (useCallback with empty deps)
  }, [handleEvent, loadChatSessions, eventsProvider]);
}
