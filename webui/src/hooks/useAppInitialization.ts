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
import { fetchRuntimeConfig, getBootstrapUser } from '../bootstrapAdapter';
import { useHost, useHostCapabilities } from '../host';
import { getActiveHost } from '../host/accessor';
import type { AppStoreSetState } from '../contexts/AppStore';
import { ApiService } from '../services/api';
import type { StatsResponse } from '../services/api';
import { polledStatsPatch } from '../utils/polledStats';
import type { SessionEntry } from '../services/api/types';
import { getAdapter } from '../services/apiAdapter';
import { listChatSessions } from '../services/chatSessions';
import { getTabWorkspacePath } from '../services/clientSession';
import type { CloudAdapter } from '../services/cloudAdapter';
import { NATIVE_FS_ENABLED } from '../services/nativeFsStubs/nativeFsFlag';
import { NATIVE_GIT_ENABLED } from '../services/nativeGitStubs/nativeGitFlag';
import { registerServiceWorker } from '../services/serviceWorkerRegistration';
import type { AppState } from '../types/app';
import type { WsEvent } from '@sprout/events';
import type { SproutEvent } from '../types/events';
import { WebSocketService } from '../services/websocket';
import { debugLog, useLog } from '../utils/log';
import { canAutoRestoreLatestSession, clearedByUser } from './bootSessionRestore';

/**
 * The origin of a model endpoint, for the context-window probe (which appends
 * its own `/proxy/chat/models` path). An endpoint that is not an absolute URL
 * (a host may give a path) yields '' so the caller falls back to the page
 * origin.
 */
function modelEndpointOrigin(endpoint: string | undefined): string {
  if (!endpoint) return '';
  try {
    return new URL(endpoint, window.location.origin).origin;
  } catch {
    return '';
  }
}

export interface UseAppInitializationOptions {
  eventsProvider: EventsProvider;
  handleEvent: (event: SproutEvent) => void;
  connectionTimeoutRef: MutableRefObject<ReturnType<typeof setTimeout> | null>;
  /** Loads the chat list and the backend's active chat. Awaited before the boot-time
   * active-chat decision so a design-mode restore/fresh start is not clobbered. */
  loadChatSessions: () => Promise<void>;
  setIsMobile: Dispatch<SetStateAction<boolean>>;
  setIsTablet: Dispatch<SetStateAction<boolean>>;
  setState: AppStoreSetState;
  /** Reconnect handler that recovers stuck processing state after WebSocket reconnection. */
  handleReconnect: () => void;
  /**
   * Whether this hook registers the event/reconnect callbacks with the
   * transport. Defaults to true (the app's historical behavior). When the
   * chat unit owns the subscription (`WorkspaceChatProvider`), the app passes
   * false so events are not delivered twice; the hook still connects.
   */
  manageEventSubscription?: boolean;
}

export function useAppInitialization({
  eventsProvider,
  handleEvent,
  connectionTimeoutRef,
  loadChatSessions,
  setIsMobile,
  setIsTablet,
  setState,
  handleReconnect,
  manageEventSubscription = true,
}: UseAppInitializationOptions): void {
  const { workspaceSwitching: supportsWorkspaceSwitching } = useHostCapabilities();
  // host.8: the hosted build's agent runs in the browser (WASM shell + browser
  // git) and its transport authenticates against a platform (authMode
  // 'bearer'). This single flag replaces every former isCloud read in this
  // hook. A host that explicitly selects the in-browser agent (its transport's
  // agent backend is 'wasm') also runs the agent in the browser even when its
  // transport is not the platform's bearer transport, so it takes the same
  // init path (WASM preload, the agent-event dispatcher, the model-endpoint
  // probe).
  const hostTransport = useHost().transport;
  const hosted = hostTransport.authMode === 'bearer' || hostTransport.agent?.kind === 'wasm';
  const log = useLog();
  const apiService = ApiService.getInstance();

  useEffect(() => {
    // ── Cloud mode: check auth BEFORE anything else ─────────────
    // In cloud mode, redirect to login if not authenticated. The session
    // check reuses the bootstrap response's `user` field instead of a
    // separate /user/me round-trip, so the redirect fires only AFTER
    // bootstrap has resolved and the app is about to mount.
    // All other initialization (WebSocket, data loading, WASM) is
    // gated behind this check to avoid 401 error spam.
    if (hosted) {
      fetchRuntimeConfig()
        .then((config) => {
          if (!config.user) {
            // No session — redirect to platform login with return_to so the
            // user comes back to the browser IDE (on the same project and
            // page) after authenticating, not stranded on the dashboard.
            // The query is re-encoded: the platform refuses a return path
            // containing "://", which a hand-typed ?repo=https://… has.
            const params = new URLSearchParams(window.location.search).toString();
            window.location.href =
              '/login?return_to=' + encodeURIComponent(window.location.pathname + (params ? `?${params}` : ''));
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
      if (manageEventSubscription) {
        eventsProvider.onEvent(handleEvent);
        eventsProvider.onReconnect(handleReconnect);
      }

      // ── Cloud mode: eagerly preload the WASM shell ──────────────
      // In cloud mode the WASM shell (44 MB) must be compiled and
      // instantiated before any wasm-local endpoint (files, terminal,
      // search) is reachable.  Starting the load here, before stats
      // and file requests fire, eliminates the init-race window where
      // the first /api/files call falls through to the backend (which
      // may return 401 or empty data).
      // Compile-time short-circuit (R-2f): a --native-fs dist hard-excludes
      // the wasmShell module (the shell provides the POSIX shell / VFS
      // natively), so the boot path must NOT touch the adapter WASM
      // preload at all — no fetch/instantiate,
      // no wasmLoading/wasmError state. NATIVE_FS_ENABLED is a
      // compile-time constant, so in the default build this guard
      // short-circuits into a dead branch and the block below runs
      // exactly as before (byte-identical behavior).
      if (!NATIVE_FS_ENABLED) {
        const wasmPreloadPromise: Promise<boolean> = hosted
          ? ((getAdapter() as CloudAdapter | null)?.preloadWasmShell() ?? Promise.resolve(false))
          : Promise.resolve(false);

        wasmPreloadPromise.then((ready) => {
          if (ready) {
            debugLog('[startup] WASM shell preloaded successfully');
            setState((prev) => ({ ...prev, wasmReady: true, wasmLoading: false }));

            // Configure browser-native git with VFS access callbacks.
            // isomorphic-git needs to read/write files from the same
            // virtual filesystem the agent uses.
            //
            // Compile-time short-circuit (Track R --native-git): in a
            // --native-git dist the shell provides git natively (the git
            // client API + boot wiring are hard-excluded), so the webui
            // must NOT wire browser git at all — no configureBrowserGit,
            // no agent git tool bridge, no shell git adapter.
            // NATIVE_GIT_ENABLED is a compile-time constant, so in the
            // default build this guard short-circuits into a dead branch
            // and the three git boot blocks below run exactly as before
            // (byte-identical behavior).
            if (!NATIVE_GIT_ENABLED) {
              if (hosted) {
                import('../services/cloudWasmHandlers').then(({ listAllVfsFiles }) => {
                  import('../services/browserGit').then(({ configureBrowserGit }) => {
                    const shell = (getAdapter() as CloudAdapter | null)?.getWasmShell?.();
                    if (shell) {
                      // Commits are authored as the signed-in account, so
                      // pushed history is attributed to the user on GitHub.
                      const user = getBootstrapUser();
                      configureBrowserGit({
                        name: user?.email ? user.email.split('@')[0] : 'Browser IDE',
                        email: user?.email || 'browser-ide@sprout.dev',
                        readVfsFiles: async () => {
                          return listAllVfsFiles(shell);
                        },
                        writeVfsFiles: async (files) => {
                          for (const f of files) {
                            shell.writeFile(f.path, f.content);
                          }
                        },
                        deleteVfsFiles: async (paths) => {
                          for (const p of paths) {
                            shell.deleteFile(p);
                          }
                        },
                      });
                    }
                  });
                });
              }
            }
            // Register the agent git tool bridge so the WASM agent can call
            // browser-side git tools via the setToolExecutionHook + globalThis.
            // (Compile-time short-circuit for --native-git: skipped when
            // NATIVE_GIT_ENABLED — see the guard above; default build runs
            // this exactly as before.)
            if (!NATIVE_GIT_ENABLED) {
              if (hosted) {
                import('../services/agentGitToolBridge')
                  .then(({ registerGitToolGlobal, installGitToolBridge }) => {
                    const shell = (getAdapter() as CloudAdapter | null)?.getWasmShell?.();
                    if (shell) {
                      registerGitToolGlobal();
                      // The WASM binary exposes setToolExecutionHook on SproutWasm.
                      const wasmApi = shell.wasm?.SproutWasm as
                        | { setToolExecutionHook?: (fn: (cmd: string) => unknown) => void }
                        | undefined;
                      if (wasmApi?.setToolExecutionHook) {
                        installGitToolBridge(wasmApi);
                        debugLog('[startup] Agent git tool bridge installed');
                      } else {
                        debugLog(
                          '[startup] setToolExecutionHook not found on SproutWasm — bridge sync hook not installed',
                        );
                      }
                    }
                  })
                  .catch((err) => {
                    debugLog('[startup] agentGitToolBridge import failed:', err);
                  });
                // Back the WASM shell's `git` command with browser git (read-only
                // subcommands) so `git status`/`diff`/`log` run in-browser instead
                // of exiting 127 into a container txn.
                import('../services/shellGitAdapter')
                  .then(({ registerShellGitGlobal }) => {
                    registerShellGitGlobal();
                    debugLog('[startup] Shell git adapter installed (__sproutShellGit)');
                  })
                  .catch((err) => {
                    debugLog('[startup] shellGitAdapter import failed:', err);
                  });
                // Back the WASM shell's `gh` command with browser GitHub support
                // (clone/checkout via isomorphic-git + PR ops via the REST API)
                // so `gh pr checkout`/`gh repo clone` run in-browser instead of
                // exiting 127 into a container txn.
                import('../services/shellGhAdapter')
                  .then(({ registerShellGhGlobal }) => {
                    registerShellGhGlobal();
                    debugLog('[startup] Shell gh adapter installed (__sproutShellGh)');
                  })
                  .catch((err) => {
                    debugLog('[startup] shellGhAdapter import failed:', err);
                  });
              }
            }
            // The design tools' screenshots render in this page (SP-158).
            if (hosted) {
              const shell = (getAdapter() as CloudAdapter | null)?.getWasmShell?.();
              if (shell) {
                import('../services/pageRenderer')
                  .then(({ registerPageRenderer }) => {
                    registerPageRenderer(shell);
                    debugLog('[startup] Page renderer installed (__sproutRender)');
                  })
                  .catch((err) => {
                    debugLog('[startup] pageRenderer import failed:', err);
                  });
              }
            }
          } else if (hosted) {
            console.warn('[startup] WASM shell preload failed — falling through to server safety-net');
            setState((prev) => ({ ...prev, wasmLoading: false, wasmError: 'Failed to load browser runtime' }));
          }
        });
        if (hosted) {
          setState((prev) => ({ ...prev, wasmLoading: true }));
        }
      }

      // ── Cloud mode: wire agent events to the webui ──────────────
      // In cloud mode, the agent loop runs in the WASM binary. Events
      // from the agent are dispatched via the agentEventDispatcher,
      // which feeds them into the same handleEvent that WebSocket
      // events use. This makes agent responses render in the chat UI.
      if (hosted) {
        import('../services/cloudWasmHandlers').then(({ setAgentEventDispatcher }) => {
          // Through the event bus, not straight to handleEvent: other
          // listeners (the git panel's refresh after an agent edit) missed
          // every hosted event.
          setAgentEventDispatcher((event) => {
            WebSocketService.getInstance().deliverLocal(event as WsEvent);
          });
        });
        // The managed model's context window decides the agent's context mode.
        // The window probe goes to the host's model endpoint when the host
        // named one (its agent backend's model endpoint, or the transport's),
        // else the page origin (the platform proxy path).
        void import('../services/platformProvider').then(({ loadManagedContextWindow }) => {
          const transport = getActiveHost()?.transport;
          const endpoint =
            (transport?.agent?.kind === 'wasm' ? transport.agent.modelEndpoint : undefined) ||
            transport?.modelEndpoint ||
            '';
          loadManagedContextWindow(modelEndpointOrigin(endpoint) || window.location.origin);
        });
      }

      // Load initial stats
      const loadStats = () => {
        apiService
          .getStats()
          .then((stats: StatsResponse) => {
            const patch = polledStatsPatch(stats, hosted);
            setState((prev) => {
              const merged = { ...prev.stats, ...patch };
              return {
                // Only update provider/model from stats when the backend
                // has a real value.  An empty string means the agent hasn't
                // been lazily created yet — we should keep whatever the
                // frontend already knows (persisted state, WS event…).
                provider: stats.provider || prev.provider,
                model: stats.model || prev.model,
                // Merge, not replace: a poll response without cost/token
                // fields (nil-agent window during lazy recreation) must not
                // erase the last-known values — that was the status bar's
                // "flashes to $0.00 then back" flicker. Absent keys keep the
                // previous value; present keys are authoritative.
                stats: JSON.stringify(prev.stats) === JSON.stringify(merged) ? prev.stats : merged,
              };
            });
          })
          .catch((err) =>
            log.error(`Failed to initialize connection: ${err instanceof Error ? err.message : String(err)}`, {
              title: 'Connection Error',
            }),
          );
      };

      // Load initial stats. (The former loadFiles() fed the dead
      // `recentFiles` prop — a root-directory listing mislabeled as recents;
      // the palette's real recents live in paletteRecents.ts, and the file
      // tree fetches its own listing.)
      loadStats();

      // In cloud mode, a repo import (?repo= param) completes asynchronously
      // after boot; components that need the new files listen for the same
      // event themselves (e.g. SidebarFilesSection refreshes its tree).
      const handleRepoImported = () => {
        debugLog('[startup] repo import completed');
      };
      window.addEventListener('sprout:repo-imported', handleRepoImported);

      // Check if import already completed before we mounted (race condition).
      const importedRepo = (window as unknown as Record<string, unknown>).__repoImported;
      if (importedRepo) {
        debugLog('[startup] repo was already imported before mount');
      }

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

        // Settle the chat list + backend active chat BEFORE the
        // boot-time active-chat decision below. The design-mode restore /
        // fresh-start switches the active chat; if the list load is still in
        // flight it can race that switch (the fresh chat would be missing
        // from the list the load adopts, and the switch's result is only
        // meaningful once the list has settled). Best-effort: a failure here
        // must not break the shell — later activity refreshes the list.
        try {
          await loadChatSessions();
        } catch (error) {
          debugLog('[startup] chat session load failed:', error);
        }

        // One conversation per project (SP-147): boot restores the chat the
        // server says is active (or the most recent non-empty one), whatever
        // the mode. There are no per-mode pins and no fresh design chat at
        // boot — the mode is a lens on one conversation history, not a
        // second one.
        try {
          const sessionsResponse = await apiService.getSessions('current');
          const sessions = Array.isArray(sessionsResponse?.sessions) ? sessionsResponse.sessions : [];
          const currentSessionId = String(sessionsResponse?.current_session_id || '');
          const currentSession = sessions.find(
            (item: SessionEntry) => String(item?.session_id || '') === currentSessionId,
          );
          const currentHasMessages = Number(currentSession?.message_count || 0) > 0;

          if (hosted && currentHasMessages && currentSessionId) {
            // Cloud mode: the "current" session id points at the most recently
            // active localStorage-backed conversation, but its transcript is
            // not loaded into React state on a fresh page load. When that
            // session has messages, restore it directly so the conversation
            // reappears after a refresh. (In local mode the backend pre-loads
            // the current session, so this branch is a no-op.)
            const restored = await apiService.restoreSession(currentSessionId);
            if (Array.isArray(restored?.messages) && restored.messages.length > 0) {
              window.dispatchEvent(
                new CustomEvent('sprout:session-restored', {
                  detail: { messages: restored.messages },
                }),
              );
            }
          } else if (!currentHasMessages) {
            // Auto-restore the most recent non-empty session, but only when
            // there is no explicit current session pointer. In cloud mode a
            // `/clear` persists a fresh empty session id as current (see
            // startNewCloudSession) — that is an intentional "start fresh"
            // signal, so we must NOT fall back to history and resurrect the
            // just-cleared conversation. In local mode the backend supplies
            // the current id, so this only fires when there genuinely is none.
            const hasExplicitCurrent = !!currentSessionId && !!currentSession;
            const chats = await listChatSessions()
              .then((resp) => resp.chat_sessions ?? [])
              .catch(() => []);
            const allowFallback = (!hosted || !hasExplicitCurrent) && canAutoRestoreLatestSession(chats);
            if (allowFallback) {
              // Never a conversation the user cleared: that was "start fresh".
              const restorable = sessions.find(
                (item: SessionEntry) =>
                  String(item?.session_id || '') !== currentSessionId &&
                  Number(item?.message_count || 0) > 0 &&
                  !clearedByUser(item?.last_updated),
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
          }
        } catch (error) {
          debugLog('[startup] session restore check failed:', error);
        }

        // Deep-link: ?chat=<session_id> restores a specific conversation.
        // Used by dashboard task links, PWA shortcuts, and share links.
        try {
          if (typeof window !== 'undefined') {
            const chatParam = new URLSearchParams(window.location.search).get('chat');
            if (chatParam) {
              const restored = await apiService.restoreSession(chatParam);
              if (Array.isArray(restored?.messages) && restored.messages.length > 0) {
                window.dispatchEvent(
                  new CustomEvent('sprout:session-restored', {
                    detail: { messages: restored.messages },
                  }),
                );
              }
              // Drop ?chat= so a refresh doesn't re-trigger; the rest of the
              // query (?repo=, ?home=) still describes where the editor is.
              const cleanUrl = new URL(window.location.href);
              cleanUrl.searchParams.delete('chat');
              window.history.replaceState(window.history.state, '', cleanUrl);
            }
          }
        } catch (error) {
          debugLog('[startup] ?chat= deep-link restore failed:', error);
        }

        // Deep-link: ?file=<path> opens a specific file in the editor.
        // Dispatches the same event that markdown file links use
        // (sprout:open-in-editor), so the existing file handler picks it up.
        try {
          if (typeof window !== 'undefined') {
            const params = new URLSearchParams(window.location.search);
            const fileParam = params.get('file');
            if (fileParam) {
              // The file handler expects to switch to editor view first,
              // so we dispatch the event with a small delay to let the
              // editor pane mount.
              setTimeout(() => {
                window.dispatchEvent(
                  new CustomEvent('sprout:open-in-editor', {
                    detail: {
                      path: fileParam,
                      lineNumber: params.get('line') ? parseInt(params.get('line')!, 10) : undefined,
                    },
                  }),
                );
              }, 300);
              const cleanUrl = new URL(window.location.href);
              cleanUrl.searchParams.delete('file');
              cleanUrl.searchParams.delete('line');
              window.history.replaceState(window.history.state, '', cleanUrl);
            }
          }
        } catch (error) {
          debugLog('[startup] ?file= deep-link failed:', error);
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
        if (manageEventSubscription) {
          eventsProvider.removeEvent(handleEvent);
          eventsProvider.onReconnect(null);
        }
        window.removeEventListener('resize', checkBreakpoints);
        clearInterval(statsInterval);
      };
    } // end startDataLoading

    // eslint-disable-next-line react-hooks/exhaustive-deps -- setState, setIsMobile, setIsTablet are stable useState setters; connectionTimeoutRef is a stable ref; eventsProvider/apiService are stable from hooks/singletons; loadChatSessions is stable (empty useCallback deps); handleReconnect is stable (useCallback with empty deps)
  }, [handleEvent, loadChatSessions, eventsProvider]);
}
