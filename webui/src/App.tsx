import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import ErrorBoundary from './components/ErrorBoundary';
import AppContent from './components/AppContent';
import UIManager from './components/UIManager';
import Notification from './components/Notification';
import UpdateNotification from './components/UpdateNotification';
import OnboardingDialog from './components/OnboardingDialog';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import { NotificationProvider } from './contexts/NotificationContext';
import { SproutAdapterProvider } from './contexts/SproutAdapterContext';
import { EventsContextProvider } from '@sprout/events';
import { LocalEventsProvider } from './services/localEventsProvider';
import { PlatformNavProvider } from './contexts/PlatformNavContext';
import './App.css';
import './components/UpdateNotification.css';
import SecurityApprovalDialog from './components/SecurityApprovalDialog';
import SecurityPromptDialog from './components/SecurityPromptDialog';
import AskUserDialog from './components/AskUserDialog';
import ModelSelectionModal from './components/ModelSelectionModal';
import { WebSocketService } from './services/websocket';
import { ApiService } from './services/api';
import { clientFetch, getTabWorkspacePath } from './services/clientSession';
import type { AppState } from './types/app';
import { debugLog } from './utils/log';
import { useSidebarState } from './hooks/useSidebarState';
import { useWebSocketEventHandler } from './hooks/useWebSocketEventHandler';
import { useChatSessionManager } from './hooks/useChatSessionManager';
import useOnboarding from './hooks/useOnboarding';

// ── Service Worker Registration ────────────────────────────────────────

const registerServiceWorker = async () => {
  if (!('serviceWorker' in navigator)) {
    return null;
  }

  if (process.env.NODE_ENV !== 'production') {
    const registrations = await navigator.serviceWorker.getRegistrations();
    await Promise.all(registrations.map((registration) => registration.unregister()));
    return null;
  }

  try {
    const swUrl = `${process.env.PUBLIC_URL || ''}/sw.js`;
    const registration = await navigator.serviceWorker.register(swUrl);
    await registration.update();
    debugLog('SW registered:', registration);

    // If an update is already waiting, activate it immediately.
    if (registration.waiting) {
      registration.waiting.postMessage({ type: 'SKIP_WAITING' });
    }

    // Ensure we pick up new SW/controller as soon as it activates.
    let hasReloadedForController = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (hasReloadedForController) {
        return;
      }
      hasReloadedForController = true;
      window.location.reload();
    });

    registration.addEventListener('updatefound', () => {
      const newWorker = registration.installing;
      if (newWorker) {
        newWorker.addEventListener('statechange', () => {
          if (newWorker.state === 'installed') {
            newWorker.postMessage({ type: 'SKIP_WAITING' });
          }
          if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
            debugLog('New service worker available');
          }
        });
      }
    });

    return registration;
  } catch (error) {
    debugLog('SW registration failed:', error);
  }

  return null;
};

// ── Constants ───────────────────────────────────────────────────────────

const APP_STATE_STORAGE_KEY = 'ledit:webui:state:v2';
const INSTANCE_PID_STORAGE_KEY = 'ledit:webui:instancePid';
const INSTANCE_SWITCH_RESET_KEY = 'ledit:webui:instanceSwitchReset';
const MAX_PERSISTED_LOGS = 1000;

// ── Helpers ────────────────────────────────────────────────────────────

const getUIContextScope = (): string => {
  if (typeof window === 'undefined') {
    return 'local';
  }

  const path = window.location.pathname || '/';
  if (!path.startsWith('/ssh/')) {
    return 'local';
  }

  // Path shape: /ssh/{encodedSessionKey}/...
  const parts = path.split('/').filter(Boolean);
  const encodedSessionKey = parts.length >= 2 ? parts[1] : '';
  if (!encodedSessionKey) {
    return 'ssh:unknown';
  }

  return `ssh:${encodedSessionKey}`;
};

const getAppStateStorageKey = (): string => {
  if (typeof window === 'undefined' || !window.localStorage) {
    return `${APP_STATE_STORAGE_KEY}:default:local`;
  }
  const instancePid = window.localStorage.getItem(INSTANCE_PID_STORAGE_KEY) || 'default';
  const scope = getUIContextScope();
  return `${APP_STATE_STORAGE_KEY}:${instancePid}:${scope}`;
};

const parseDate = (value: unknown): Date => {
  if (value instanceof Date) {
    return value;
  }
  if (typeof value === 'string' || typeof value === 'number') {
    const parsed = new Date(value);
    if (!Number.isNaN(parsed.getTime())) {
      return parsed;
    }
  }
  return new Date();
};

const loadPersistedAppState = (): Partial<AppState> | null => {
  if (typeof window === 'undefined' || !window.localStorage) {
    return null;
  }

  try {
    if (window.sessionStorage?.getItem(INSTANCE_SWITCH_RESET_KEY) === '1') {
      window.sessionStorage.removeItem(INSTANCE_SWITCH_RESET_KEY);
      window.localStorage.removeItem(getAppStateStorageKey());
      return null;
    }

    const storageKey = getAppStateStorageKey();
    const raw = window.localStorage.getItem(storageKey);
    if (!raw) {
      return null;
    }

    const parsed = JSON.parse(raw);
    const parsedMessages = Array.isArray(parsed.messages)
        ? parsed.messages.map((message: any) => ({
            ...message,
            timestamp: parseDate(message?.timestamp),
            toolRefs: Array.isArray(message?.toolRefs) ? message.toolRefs : undefined
          }))
        : [];
      return {
      provider: typeof parsed.provider === 'string' ? parsed.provider : 'unknown',
      model: typeof parsed.model === 'string' ? parsed.model : 'unknown',
      sessionId: typeof parsed.sessionId === 'string' ? parsed.sessionId : null,
      queryCount: typeof parsed.queryCount === 'number' ? parsed.queryCount : 0,
      currentView: ['chat', 'editor', 'git'].includes(parsed.currentView) ? parsed.currentView : 'chat',
      messages: [],
      fileEdits: Array.isArray(parsed.fileEdits)
        ? parsed.fileEdits.map((edit: any) => ({
            ...edit,
            timestamp: parseDate(edit?.timestamp)
          }))
        : [],
      subagentActivities: []
    };
  } catch (error) {
    console.warn('Failed to load persisted app state:', error);
    return null;
  }
};

// ── App Component ─────────────────────────────────────────────────────

function App() {
  const [state, setState] = useState<AppState>(() => {
    const persisted = loadPersistedAppState();
    return {
      provider: 'unknown',
      model: 'unknown',
      sessionId: null,
      queryCount: 0,
      messages: [],
      logs: [],
      currentView: 'chat',
      toolExecutions: [],
      stats: {},
      currentTodos: [],
      fileEdits: [],
      subagentActivities: [],
      ...persisted,
      isConnected: false,
      isProcessing: false,
      lastError: null,
      queryProgress: null,
      activeChatId: null,
      chatSessions: [],
      perChatCache: {},
      securityApprovalRequest: null,
      securityPromptRequest: null,
      askUserRequest: null,
      modelSelectionRequest: null,
    };
  });

  const [inputValue, setInputValue] = useState('');
  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);
  const [gitRefreshToken, setGitRefreshToken] = useState(0);

  const {
    isMobile,
    isTablet,
    setIsMobile,
    setIsTablet,
    isSidebarOpen,
    sidebarCollapsed,
    isTerminalExpanded,
    selectedSection,
    sidebarWidth,
    sidebarWidthRef,
    toggleSidebar,
    closeSidebar,
    handleSidebarToggle,
    setIsTerminalExpanded,
    setSelectedSection,
    setSidebarWidth,
    persistSidebarWidth,
    resetSidebarWidth,
  } = useSidebarState();

  // ── Refs ───────────────────────────────────────────────────────

  const activeRequestsRef = useRef(0);
  const queuedMessagesRef = useRef<string[]>([]);
  const activeChatIdRef = useRef<string | null>(null);
  activeChatIdRef.current = state.activeChatId;
  const pendingProviderRef = useRef<string>(state.provider);
  const pendingProviderChangeRef = useRef<boolean>(false);
  const pendingProviderChangeValueRef = useRef<string | null>(null);
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef<boolean>(false);

  useEffect(() => {
    pendingProviderRef.current = state.provider;
  }, [state.provider]);

  // ── Hooks ───────────────────────────────────────────────────────

  const wsService = WebSocketService.getInstance();
  const apiService = ApiService.getInstance();

  const wsEventHandlerRefs: import('./hooks/useWebSocketEventHandler').UseWebSocketEventHandlerRefs = {
    activeRequestsRef,
    activeChatIdRef,
    pendingProviderRef,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
  };

  const { handleEvent, handleReconnect } = useWebSocketEventHandler({
    setState,
    refs: wsEventHandlerRefs,
    apiService,
  });

  const chatManager = useChatSessionManager({
    setState,
    activeRequestsRef,
    activeChatIdRef,
    queuedMessagesRef,
    setInputValue,
    isProcessing: state.isProcessing,
  });

  const {
    onboarding,
    selectedProvider,
    recommendedProviders,
    advancedProviders,
    windowsGuidance,
    refreshProviderList,
    onProviderChange,
    onComplete,
    onSkip,
    onInstallWsl,
    onInstallGitBash,
    updateOnboarding,
  } = useOnboarding();

  // ── Persistence Effect ────────────────────────────────────────────

  useEffect(() => {
    if (typeof window === 'undefined' || !window.localStorage) {
      return;
    }

    const storageKey = getAppStateStorageKey();
    // Only persist what is needed to restore the chat view. Logs and
    // toolExecutions are ephemeral — they are large and re-populated by
    // the WebSocket stream, so storing them wastes quota unnecessarily.
    const persistPayload = (messageCount: number) => JSON.stringify({
      provider: state.provider,
      model: state.model,
      sessionId: state.sessionId,
      queryCount: state.queryCount,
      currentView: state.currentView,
      messages: state.messages.slice(-messageCount),
      fileEdits: state.fileEdits.slice(-20),
    });
    try {
      window.localStorage.setItem(storageKey, persistPayload(20));
    } catch {
      // QuotaExceededError: retry with fewer messages, then give up gracefully.
      try {
        window.localStorage.setItem(storageKey, persistPayload(20));
      } catch {
        try {
          window.localStorage.removeItem(storageKey);
        } catch { /* nothing more we can do */ }
      }
    }
  }, [
    state.provider,
    state.model,
    state.sessionId,
    state.queryCount,
    state.currentView,
    state.messages,
    state.fileEdits,
  ]);

  // ── Memos ───────────────────────────────────────────────────────

  // Keep a larger client-side log buffer available to the sidebar logs view.
  const recentLogs = useMemo(() => state.logs.slice(-MAX_PERSISTED_LOGS), [state.logs]);

  // Memoize stats to prevent unnecessary Sidebar remounts
  const stats = useMemo(() => ({
    queryCount: state.queryCount,
    filesModified: 0 // TODO: track modified files from buffers
  }), [state.queryCount]);

  // ── Security Dialog Handlers ─────────────────────────────────────

  const handleSecurityApprovalResponse = useCallback((requestId: string, approved: boolean) => {
    if (!wsService.isConnected()) return;
    wsService.sendEvent({
      type: 'security_approval_response',
      data: { request_id: requestId, approved },
    });
    setState((prev) => ({ ...prev, securityApprovalRequest: null }));
  }, [wsService]);

  const handleSecurityPromptResponse = useCallback((requestId: string, response: boolean) => {
    if (!wsService.isConnected()) return;
    wsService.sendEvent({
      type: 'security_prompt_response',
      data: { request_id: requestId, response },
    });
    setState((prev) => ({ ...prev, securityPromptRequest: null }));
  }, [wsService]);

  const handleAskUserResponse = useCallback((requestId: string, response: string) => {
    if (!wsService.isConnected()) return;
    wsService.sendEvent({
      type: 'ask_user_response',
      data: { request_id: requestId, response },
    });
    setState((prev) => ({ ...prev, askUserRequest: null }));
  }, [wsService]);

  const handleModelSelectionResponse = useCallback((model: string) => {
    // Send model_change event via WebSocket
    wsService.sendEvent({
      type: 'model_change',
      data: { provider: state.provider, model },
    });
    // Update local state
    setState((prev) => ({ ...prev, modelSelectionRequest: null }));
  }, [wsService, state.provider]);

  const handleModelSelectionClose = useCallback(() => {
    setState((prev) => ({ ...prev, modelSelectionRequest: null }));
  }, []);

  // ── Model/Provider/View Change Handlers ──────────────────────────

  const handleModelChange = useCallback((model: string) => {
    debugLog('Model changed to:', model);
    const provider = pendingProviderRef.current || state.provider;
    setState(prev => ({
      ...prev,
      model
    }));
    wsService.sendEvent({
      type: 'model_change',
      data: { provider, model }
    });
  }, [state.provider, wsService]);

  const handleProviderChange = useCallback((provider: string) => {
    debugLog('Provider changed to:', provider);
    pendingProviderRef.current = provider;
    pendingProviderChangeRef.current = true;
    pendingProviderChangeValueRef.current = provider;
    setState(prev => ({
      ...prev,
      provider
    }));
    wsService.sendEvent({
      type: 'provider_change',
      data: { provider }
    });
  }, [wsService]);

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team') => {
    setState(prev => ({
      ...prev,
      currentView: view
    }));
  }, []);

  // ── Git Handlers ─────────────────────────────────────────────────

  const handleGitCommit = useCallback(async (message: string, files: string[]) => {
    debugLog('Git commit:', message, files);
    try {
      const response = await clientFetch('/api/git/commit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, files })
      });

      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(errorData.message || 'Failed to create commit');
      }

      const data = await response.json();
      debugLog('Commit successful:', data);
      setGitRefreshToken(k => k + 1);
      return data;
    } catch (err) {
      console.error('Failed to commit:', err);
      throw err;
    }
  }, []);

  const handleGitAICommit = useCallback(async (): Promise<{ commitMessage: string; warnings?: string[] }> => {
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
          body: JSON.stringify({ path: file })
        });
        if (!response.ok) {
          throw new Error(`Failed to stage ${file}`);
        }
      }
      setGitRefreshToken(k => k + 1);
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
          body: JSON.stringify({ path: file })
        });
        if (!response.ok) {
          throw new Error(`Failed to unstage ${file}`);
        }
      }
      setGitRefreshToken(k => k + 1);
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
          body: JSON.stringify({ path: file })
        });
        if (!response.ok) {
          throw new Error(`Failed to discard ${file}`);
        }
      }
      setGitRefreshToken(k => k + 1);
    } catch (err) {
      console.error('Failed to discard files:', err);
      throw err;
    }
  }, []);

  const handleTerminalOutput = useCallback((output: string) => {
    debugLog('[term] Terminal output:', output);
  }, []);

  // ── Effects ─────────────────────────────────────────────────────

  useEffect(() => {
    registerServiceWorker().catch(console.error);

    const loadStats = async () => {
      try {
        const stats = await apiService.getStats();
        setState(prev => ({
          ...prev,
          provider: stats.provider || prev.provider,
          model: stats.model || prev.model,
        }));
      } catch (error) {
        console.error('Failed to load stats:', error);
      }
    };

    const loadFiles = async () => {
      try {
        const files = await apiService.getFiles();
        const filesMap = (files?.files as unknown as Record<string, { modified?: boolean }>) || {};
        const fileList = Object.keys(filesMap).map((path) => ({
          path,
          modified: filesMap[path]?.modified || false
        }));
        setRecentFiles(fileList);
      } catch (error) {
        console.error('Failed to load files:', error);
      }
    };

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
            window.dispatchEvent(new CustomEvent('ledit:open-workspace-switcher'));
          }
        }
      } catch (error) {
        debugLog('[startup] workspace check failed:', error);
      }

      try {
        const sessionsResponse = await apiService.getSessions('current');
        const sessions = Array.isArray(sessionsResponse?.sessions) ? sessionsResponse.sessions : [];
        const currentSessionId = String(sessionsResponse?.current_session_id || '');
        const currentSession = sessions.find((item: any) => String(item?.session_id || '') === currentSessionId);
        const currentHasMessages = Number(currentSession?.message_count || 0) > 0;
        if (!currentHasMessages) {
          const restorable = sessions.find((item: any) =>
            String(item?.session_id || '') !== currentSessionId && Number(item?.message_count || 0) > 0,
          );
          if (restorable?.session_id) {
            const restored = await apiService.restoreSession(String(restorable.session_id));
            if (Array.isArray(restored?.messages) && restored.messages.length > 0) {
              window.dispatchEvent(
                new CustomEvent('ledit:session-restored', {
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

    // Load initial stats/files/sessions and then reconcile workspace/session startup.
    loadStats();
    loadFiles();

    // Initialize WebSocket connection
    wsService.connect();
    wsService.onEvent(handleEvent);
    wsService.onReconnect(handleReconnect);

    chatManager.loadChatSessions();
    restoreStartupState().catch(() => {});

    // Set up periodic stats updates
    const statsInterval = setInterval(loadStats, 5000); // Update every 5 seconds

    // Check viewport breakpoints (mobile < 768px, tablet 769-1024px)
    const checkBreakpoints = () => {
      const w = window.innerWidth;
      setIsMobile(w <= 768);
      setIsTablet(w >= 769 && w <= 1024);
    };

    checkBreakpoints();
    window.addEventListener('resize', checkBreakpoints);

    // Cleanup
    return () => {
      // Clear any pending connection timeout
      if (connectionTimeoutRef.current) {
        clearTimeout(connectionTimeoutRef.current);
      }
      // Reset refs to their default values
      connectionTimeoutRef.current = null;
      pendingProviderChangeRef.current = false;
      pendingProviderChangeValueRef.current = null;
      wsService.removeEvent(handleEvent);
      wsService.onReconnect(null);
      wsService.disconnect();
      window.removeEventListener('resize', checkBreakpoints);
      clearInterval(statsInterval);
    };
  }, [handleEvent, handleReconnect, wsService, apiService, chatManager.loadChatSessions, setIsMobile, setIsTablet]);

  // ── Onboarding Completion Handler ────────────────────────────────

  const handleCompleteOnboarding = useCallback(async () => {
    await onComplete((vals) => {
      setState(prev => ({
        ...prev,
        provider: vals.provider,
        model: vals.model,
      }));
    });
  }, [onComplete]);

  // ── Render ───────────────────────────────────────────────────────

  return (
    <ErrorBoundary
      onError={(error, errorInfo) => {
        console.error('Application error:', error, errorInfo);
        // You could send this to an error reporting service here
      }}
    >
      <SproutAdapterProvider>
        <PlatformNavProvider>
        <EventsContextProvider provider={new LocalEventsProvider()}>
        <ThemeProvider>
        <NotificationProvider>
        <HotkeyProvider>
          <EditorManagerProvider>
            <UIManager>
              <AppContent
                state={state}
                inputValue={inputValue}
                onInputChange={setInputValue}
                isMobile={isMobile}
                isTablet={isTablet}
                isSidebarOpen={isSidebarOpen}
                sidebarCollapsed={sidebarCollapsed}
                isTerminalExpanded={isTerminalExpanded}
                selectedSection={selectedSection}
                sidebarWidth={sidebarWidth}
                sidebarWidthRef={sidebarWidthRef}
                onSectionChange={setSelectedSection}
                onSidebarWidthChange={setSidebarWidth}
                onSidebarWidthPersist={persistSidebarWidth}
                onSidebarWidthReset={resetSidebarWidth}
                stats={stats}
                recentFiles={recentFiles}
                recentLogs={recentLogs}
                gitRefreshToken={gitRefreshToken}
                onSidebarToggle={handleSidebarToggle}
                onToggleSidebar={toggleSidebar}
                onCloseSidebar={closeSidebar}
                onViewChange={handleViewChange}
                onModelChange={handleModelChange}
                onProviderChange={handleProviderChange}
                onSendMessage={chatManager.handleSendMessage}
                onQueueMessage={chatManager.handleQueueMessage}
                onStopProcessing={chatManager.handleStopProcessing}
                queuedMessagesCount={chatManager.queuedMessagesCount}
                onGitCommit={handleGitCommit}
                onGitAICommit={handleGitAICommit}
                onGitStage={handleGitStage}
                onGitUnstage={handleGitUnstage}
                onGitDiscard={handleGitDiscard}
                onTerminalOutput={handleTerminalOutput}
                onTerminalExpandedChange={setIsTerminalExpanded}
                isConnected={state.isConnected}
                chatSessions={state.chatSessions}
                activeChatId={state.activeChatId}
                onActiveChatChange={chatManager.handleActiveChatChange}
                onCreateChat={chatManager.handleCreateChat}
                onDeleteChat={chatManager.handleDeleteChat}
                onRenameChat={chatManager.handleRenameChat}
                perChatCache={state.perChatCache}
              />
              <Notification />
              <UpdateNotification />
              {state.securityApprovalRequest && (
                <SecurityApprovalDialog
                  requestId={state.securityApprovalRequest.requestId}
                  toolName={state.securityApprovalRequest.toolName}
                  riskLevel={state.securityApprovalRequest.riskLevel as 'SAFE' | 'CAUTION' | 'DANGEROUS'}
                  reasoning={state.securityApprovalRequest.reasoning}
                  command={state.securityApprovalRequest.command}
                  riskType={state.securityApprovalRequest.riskType}
                  target={state.securityApprovalRequest.target}
                  onRespond={handleSecurityApprovalResponse}
                />
              )}
              {state.securityPromptRequest && (
                <SecurityPromptDialog
                  requestId={state.securityPromptRequest.requestId}
                  prompt={state.securityPromptRequest.prompt}
                  filePath={state.securityPromptRequest.filePath}
                  concern={state.securityPromptRequest.concern}
                  onRespond={handleSecurityPromptResponse}
                />
              )}
              {state.askUserRequest && (
                <AskUserDialog
                  requestId={state.askUserRequest.requestId}
                  question={state.askUserRequest.question}
                  onRespond={handleAskUserResponse}
                />
              )}
              {state.modelSelectionRequest && (
                <ModelSelectionModal
                  provider={state.modelSelectionRequest.provider}
                  onClose={handleModelSelectionClose}
                  onSelectModel={handleModelSelectionResponse}
                />
              )}
              <OnboardingDialog
                onboarding={onboarding}
                selectedProvider={selectedProvider}
                recommendedProviders={recommendedProviders}
                advancedProviders={advancedProviders}
                windowsGuidance={windowsGuidance}
                onProviderChange={onProviderChange}
                onComplete={handleCompleteOnboarding}
                onSkip={onSkip}
                onRefresh={refreshProviderList}
                onInstallWsl={onInstallWsl}
                onInstallGitBash={onInstallGitBash}
                updateOnboarding={updateOnboarding}
              />
            </UIManager>
          </EditorManagerProvider>
        </HotkeyProvider>
        </NotificationProvider>
      </ThemeProvider>
        </EventsContextProvider>
        </PlatformNavProvider>
      </SproutAdapterProvider>
    </ErrorBoundary>
  );
}

export default App;
