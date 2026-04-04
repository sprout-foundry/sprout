import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import ErrorBoundary from './components/ErrorBoundary';
import AppContent from './components/AppContent';
import UIManager from './components/UIManager';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import './App.css';
import { WebSocketService } from './services/websocket';
import { ApiService } from './services/api';
import useOnboarding from './hooks/useOnboarding';
import useWebSocketEvents from './hooks/useWebSocketEvents';
import OnboardingDialog from './components/OnboardingDialog';
import { clientFetch } from './services/clientSession';
import {
  listChatSessions,
  createChatSession,
  deleteChatSession,
  renameChatSession,
  switchChatSession,
} from './services/chatSessions';
import { debugLog } from './utils/log';
import { usePageVisibility } from './hooks/usePageVisibility';
import type { AppState, Message } from './types/app';
import { MAX_PERSISTED_LOGS } from './constants/app';
import { getAppStateStorageKey, loadPersistedAppState } from './services/appStatePersistence';
import { registerServiceWorker } from './services/serviceWorkerRegistration';

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
    };
  });

  const [inputValue, setInputValue] = useState('');
  const [isMobile, setIsMobile] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem('ledit-sidebar-collapsed') === 'true';
    } catch {
      return false;
    }
  });
  const setSidebarCollapsedPersisted = useCallback((collapsed: boolean) => {
    try {
      window.localStorage.setItem('ledit-sidebar-collapsed', String(collapsed));
    } catch { /* ignore */ }
    setSidebarCollapsed(collapsed);
  }, []);
  const [isTerminalExpanded, setIsTerminalExpanded] = useState(() => {
    try {
      return window.localStorage.getItem('ledit-terminal-expanded') === 'true';
    } catch {
      return false;
    }
  });
  const setIsTerminalExpandedPersisted = useCallback((expanded: boolean) => {
    try {
      window.localStorage.setItem('ledit-terminal-expanded', String(expanded));
    } catch { /* ignore */ }
    setIsTerminalExpanded(expanded);
  }, []);
  const [queuedMessages, setQueuedMessages] = useState<string[]>([]);
  const queuedMessagesRef = useRef<string[]>([]);
  const { handleEvent, activeChatIdRef, activeRequestsRef, connectionTimeoutRef } = useWebSocketEvents({
    state,
    setState,
    setInputValue,
    setQueuedMessages,
    queuedMessagesRef,
  });
  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);
  const [gitRefreshToken, setGitRefreshToken] = useState(0);

    // Onboarding hook — encapsulates all onboarding state, memos, callbacks, and side-effects
  const onboardingHook = useOnboarding();

  // Adapter wrapping the hook's onComplete so that parent AppState is updated
  const onboarding = {
    ...onboardingHook,
    onComplete: () =>
      onboardingHook.onComplete((values) =>
        setState((prev) => ({ ...prev, ...values })),
      ),
  };

  // Wire up browser tab freeze/resume for WebSocket connections.
  // When Chrome throttles a background tab, WebSocket connections become stale.
  // This hook calls freeze()/resume() on all WS services when visibility changes.
  usePageVisibility();

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
        window.localStorage.setItem(storageKey, persistPayload(5));
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

  // Keep a larger client-side log buffer available to the sidebar logs view.
  const recentLogs = useMemo(() => state.logs.slice(-MAX_PERSISTED_LOGS), [state.logs]);

  // Memoize stats to prevent unnecessary Sidebar remounts
  const stats = useMemo(() => ({
    queryCount: state.queryCount,
    filesModified: 0 // TODO: track modified files from buffers
  }), [state.queryCount]);

  // Memoize sidebar toggle handler
  const handleSidebarToggle = useCallback(() => {
    const next = !sidebarCollapsed;
    setSidebarCollapsedPersisted(next);
  }, [sidebarCollapsed]);

  const wsService = WebSocketService.getInstance();
  const apiService = ApiService.getInstance();

  const pendingProviderRef = useRef<string>(state.provider);

  useEffect(() => {
    pendingProviderRef.current = state.provider;
  }, [state.provider]);

  const loadChatSessions = useCallback(async () => {
    try {
      const response = await listChatSessions();
      const activeChatId = response.active_chat_id || null;
      // Load message history for the currently active chat so history shows on first load
      let initialMessages: Message[] = [];
      if (activeChatId) {
        try {
          const switchResp = await switchChatSession(activeChatId);
          initialMessages = switchResp.chat_session.messages
            .filter((m) => m.role === 'user' || m.role === 'assistant')
            .map((m, i) => ({
              id: `chat-${activeChatId}-${i}`,
              type: m.role as 'user' | 'assistant',
              content: typeof m.content === 'string' ? m.content : '',
              timestamp: new Date(),
              ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
            }));
          // Eagerly update the ref in case this is the initial load
          if (!activeChatIdRef.current) {
            activeChatIdRef.current = activeChatId;
          }
        } catch (e) {
          debugLog('[chat] Failed to load initial messages:', e);
        }
      }
      setState(prev => ({
        ...prev,
        chatSessions: response.chat_sessions,
        activeChatId: prev.activeChatId || activeChatId,
        // Only set initial messages if we have none yet (don't clobber live messages)
        messages: prev.messages.length === 0 && initialMessages.length > 0
          ? initialMessages
          : prev.messages,
      }));
    } catch (error) {
      debugLog('[chat] Failed to load chat sessions:', error);
    }
  }, []);

  const handleActiveChatChange = useCallback(async (id: string) => {
    const currentId = activeChatIdRef.current;
    if (currentId === id) return; // Already active

    // Update the ref immediately so WebSocket events for the new chat are
    // accepted and events for the old chat are filtered during the switch.
    activeChatIdRef.current = id;

    // Phase 1: instant UI update from cache — no blank flash while API loads.
    // Saves the outgoing chat's full state (including messages) and restores
    // the incoming chat's previously-cached state atomically.
    setState(prev => {
      const cached = prev.perChatCache[id];
      const newCache = currentId ? {
        ...prev.perChatCache,
        [currentId]: {
          messages: prev.messages,
          toolExecutions: prev.toolExecutions,
          fileEdits: prev.fileEdits,
          subagentActivities: prev.subagentActivities,
          currentTodos: prev.currentTodos,
          queryProgress: prev.queryProgress,
          lastError: prev.lastError,
          isProcessing: prev.isProcessing,
        },
      } : prev.perChatCache;
      const restoredIsProcessing = cached?.isProcessing ?? false;
      // Sync the requests counter with cached processing state so the Chat
      // input/stop-button reflects the correct state without waiting for the
      // backend response.
      activeRequestsRef.current = restoredIsProcessing ? 1 : 0;
      return {
        ...prev,
        // Use id optimistically — overwritten by response.active_chat_id below
        activeChatId: id,
        messages: cached?.messages ?? [],
        isProcessing: restoredIsProcessing,
        toolExecutions: cached?.toolExecutions ?? [],
        fileEdits: cached?.fileEdits ?? [],
        subagentActivities: cached?.subagentActivities ?? [],
        currentTodos: cached?.currentTodos ?? [],
        queryProgress: cached?.queryProgress ?? null,
        lastError: cached?.lastError ?? null,
        perChatCache: newCache,
      };
    });

    try {
      const response = await switchChatSession(id);
      const backendMessages: Message[] = (response.chat_session.messages ?? [])
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => ({
          id: `chat-${id}-${i}`,
          type: m.role as 'user' | 'assistant',
          content: typeof m.content === 'string' ? m.content : '',
          timestamp: new Date(),
          ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
        }));

      // Use backend active_query as the authoritative source of truth for
      // isProcessing — it knows whether the query actually completed, even
      // if we missed the query_completed WebSocket event while on another chat.
      const backendIsActive = !!(response.chat_session as any).active_query;

      setState(prev => {
        // Only update messages if backend has equal or more messages than what's
        // already shown from cache. If backend has fewer (query still in flight
        // so AgentState not yet synced), keep the cache messages which include
        // the optimistically-added user message and any streamed chunks.
        const useBackendMessages = backendMessages.length >= prev.messages.length;
        const finalIsProcessing = backendIsActive;
        activeRequestsRef.current = finalIsProcessing ? 1 : 0;
        return {
          ...prev,
          activeChatId: response.active_chat_id,
          messages: useBackendMessages ? backendMessages : prev.messages,
          isProcessing: finalIsProcessing,
        };
      });

      // Refresh session list to reflect updated active state
      const sessionsResp = await listChatSessions();
      setState(prev => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
    } catch (error) {
      // Rollback the eagerly-updated ref so subsequent switches aren't confused
      activeChatIdRef.current = currentId;
      debugLog('[chat] Failed to switch chat session:', error);
    }
  }, []);

  // Keep the old name as an alias for internal use
  const handleSwitchChat = handleActiveChatChange;

  const handleCreateChat = useCallback(async (): Promise<string | null> => {
    try {
      const response = await createChatSession();
      const newId = response.chat_session.id;
      const sessionsResp = await listChatSessions();
      setState(prev => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
      return newId;
    } catch (error) {
      debugLog('[chat] Failed to create chat session:', error);
      const message = error instanceof Error ? error.message : 'Failed to create new chat';
      setState(prev => ({ ...prev, lastError: message }));
      return null;
    }
  }, []);

  const handleDeleteChat = useCallback(async (id: string) => {
    try {
      await deleteChatSession(id);
      if (id === activeChatIdRef.current) {
        const sessionsResp = await listChatSessions();
        if (sessionsResp.chat_sessions.length > 0) {
          await handleSwitchChat(sessionsResp.active_chat_id);
        } else {
          setState(prev => ({ ...prev, chatSessions: [], activeChatId: null, messages: [] }));
        }
      } else {
        const sessionsResp = await listChatSessions();
        setState(prev => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
      }
    } catch (error) {
      debugLog('[chat] Failed to delete chat session:', error);
    }
  }, [handleSwitchChat]);

  const handleRenameChat = useCallback(async (id: string, name: string) => {
    try {
      await renameChatSession(id, name);
      const sessionsResp = await listChatSessions();
      setState(prev => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
    } catch (error) {
      debugLog('[chat] Failed to rename chat session:', error);
    }
  }, []);

  useEffect(() => {
    // Register Service Worker for PWA functionality
    registerServiceWorker();

    // Initialize WebSocket connection
    wsService.connect();
    wsService.onEvent(handleEvent);

    // Load initial stats
    const loadStats = () => {
      apiService.getStats().then((stats: any) => {
        setState(prev => ({
          ...prev,
          provider: stats.provider,
          model: stats.model,
          stats: JSON.stringify(prev.stats) === JSON.stringify(stats) ? prev.stats : stats
        }));
      }).catch(console.error);
    };

    // Load recent files
    const loadFiles = () => {
      apiService.getFiles().then((response: any) => {
        if (response && response.files) {
          // Convert files array to expected format
          const files = response.files.map((file: any) => ({
            path: file.path || file.name,
            modified: false
          }));
          setRecentFiles(files);
        }
      }).catch(console.error);
    };

    // Load initial stats
    loadStats();

    // Load initial files
    loadFiles();

    // Load initial chat sessions
    loadChatSessions();

    // Set up periodic stats updates
    const statsInterval = setInterval(loadStats, 5000); // Update every 5 seconds

    // Check for mobile screen size
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    
    checkMobile();
    window.addEventListener('resize', checkMobile);

    // Cleanup
    return () => {
      // Clear any pending connection timeout
      if (connectionTimeoutRef.current) {
        clearTimeout(connectionTimeoutRef.current);
      }
      wsService.removeEvent(handleEvent);
      wsService.disconnect();
      window.removeEventListener('resize', checkMobile);
      clearInterval(statsInterval);
    };
  }, [handleEvent, wsService, apiService, loadChatSessions]);

  // Listen for session-restored events from Chat.tsx to populate messages
  useEffect(() => {
    const handleSessionRestored = (event: Event) => {
      const customEvent = event as CustomEvent<{ messages: Array<{ role: string; content: string }> }>;
      const rawMessages = customEvent.detail?.messages;
      if (!Array.isArray(rawMessages)) return;

      // Map backend Message format { role, content } to frontend Message format { id, type, content, timestamp }
      // Only include user and assistant messages (skip system/tool)
      const restoredMessages: Message[] = rawMessages
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => ({
          id: `restored-${i}`,
          type: m.role as 'user' | 'assistant',
          content: typeof m.content === 'string' ? m.content : '',
          timestamp: new Date()
        }));

      if (restoredMessages.length > 0) {
        setState(prev => ({
          ...prev,
          messages: restoredMessages,
          toolExecutions: [],
          fileEdits: [],
          subagentActivities: [],
          currentTodos: [],
          queryProgress: null,
          lastError: null,
          isProcessing: false,
        }));
      }
    };

    window.addEventListener('ledit:session-restored', handleSessionRestored);
    return () => window.removeEventListener('ledit:session-restored', handleSessionRestored);
  }, []);

  const handleSendMessage = useCallback(async (message: string, options?: { allowConcurrent?: boolean }) => {
    if (!message.trim()) return;
    const trimmedMessage = message.trim();
    const allowConcurrent = options?.allowConcurrent === true;
    if (!allowConcurrent && activeRequestsRef.current > 0) {
      setState(prev => ({
        ...prev,
        lastError: null,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'user',
          content: trimmedMessage,
          timestamp: new Date()
        }]
      }));
      await apiService.steerQuery(trimmedMessage, activeChatIdRef.current ?? undefined);
      setInputValue('');
      return;
    }
    activeRequestsRef.current += 1;

    // Clear any previous errors and set processing state
    setState(prev => ({
      ...prev,
      isProcessing: true,
      lastError: null
    }));

    try {
      debugLog('[>>] Sending message:', trimmedMessage);
      await apiService.sendQuery(trimmedMessage, activeChatIdRef.current ?? undefined);
      setInputValue('');
      debugLog('[OK] Message sent successfully');
    } catch (error) {
      console.error('[FAIL] Failed to send message:', error);
      if (activeRequestsRef.current > 0) {
        activeRequestsRef.current -= 1;
      }
      const errorMsg = error instanceof Error ? error.message : 'Failed to send message';
      setState(prev => ({
        ...prev,
        isProcessing: activeRequestsRef.current > 0,
        lastError: `Failed to send message: ${errorMsg}`,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMsg}`,
          timestamp: new Date()
        }]
      }));
    }
  }, [apiService]);

  const handleQueueMessage = useCallback((message: string) => {
    const trimmed = message.trim();
    if (!trimmed) return;
    queuedMessagesRef.current.push(trimmed);
    setQueuedMessages([...queuedMessagesRef.current]);
  }, []);

  const handleRemoveQueuedMessage = useCallback((index: number) => {
    setQueuedMessages(prev => {
      const next = [...prev];
      next.splice(index, 1);
      queuedMessagesRef.current = next;
      return next;
    });
  }, []);

  const handleEditQueuedMessage = useCallback((index: number, newText: string) => {
    setQueuedMessages(prev => {
      const next = [...prev];
      next[index] = newText;
      queuedMessagesRef.current = next;
      return next;
    });
  }, []);

  const handleReorderQueuedMessages = useCallback((fromIndex: number, toIndex: number) => {
    setQueuedMessages(prev => {
      const next = [...prev];
      const [moved] = next.splice(fromIndex, 1);
      next.splice(toIndex, 0, moved);
      queuedMessagesRef.current = next;
      return next;
    });
  }, []);

  const handleClearQueuedMessages = useCallback(() => {
    setQueuedMessages([]);
    queuedMessagesRef.current = [];
  }, []);

  const handleStopProcessing = useCallback(async () => {
    try {
      await apiService.stopQuery();
      setState(prev => ({
        ...prev,
        lastError: null,
      }));
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : 'Failed to stop query';
      setState(prev => ({
        ...prev,
        lastError: errorMsg,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMsg}`,
          timestamp: new Date()
        }]
      }));
    }
  }, [apiService]);

  useEffect(() => {
    if (state.isProcessing || activeRequestsRef.current > 0) {
      return;
    }
    if (queuedMessagesRef.current.length === 0) {
      return;
    }

    const next = queuedMessagesRef.current.shift();
    setQueuedMessages([...queuedMessagesRef.current]);
    if (!next) return;

    handleSendMessage(next).catch((error) => {
      const errorMsg = error instanceof Error ? error.message : 'Failed to send queued message';
      setState(prev => ({
        ...prev,
        lastError: `Failed to send queued message: ${errorMsg}`,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMsg}`,
          timestamp: new Date()
        }]
      }));
    });
  }, [state.isProcessing, handleSendMessage]);

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
    setState(prev => ({
      ...prev,
      provider
    }));
    wsService.sendEvent({
      type: 'provider_change',
      data: { provider }
    });
  }, [wsService]);

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git') => {
    setState(prev => ({
      ...prev,
      currentView: view
    }));
  }, []);

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

  const toggleSidebar = useCallback(() => {
    setIsSidebarOpen(prev => !prev);
  }, []);

  const closeSidebar = useCallback(() => {
    setIsSidebarOpen(false);
  }, []);

  return (
    <ErrorBoundary
      onError={(error, errorInfo) => {
        console.error('Application error:', error, errorInfo);
        // You could send this to an error reporting service here
      }}
    >
      <ThemeProvider>
        <HotkeyProvider>
          <EditorManagerProvider>
            <UIManager>
              <AppContent
                state={state}
                inputValue={inputValue}
                onInputChange={setInputValue}
                isMobile={isMobile}
                isSidebarOpen={isSidebarOpen}
                sidebarCollapsed={sidebarCollapsed}
                isTerminalExpanded={isTerminalExpanded}
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
                onSendMessage={handleSendMessage}
                onQueueMessage={handleQueueMessage}
                onStopProcessing={handleStopProcessing}
                queuedMessagesCount={queuedMessages.length}
                queuedMessages={queuedMessages}
                onQueueMessageRemove={handleRemoveQueuedMessage}
                onQueueMessageEdit={handleEditQueuedMessage}
                onQueueReorder={handleReorderQueuedMessages}
                onClearQueuedMessages={handleClearQueuedMessages}
                onGitCommit={handleGitCommit}
                onGitAICommit={handleGitAICommit}
                onGitStage={handleGitStage}
                onGitUnstage={handleGitUnstage}
                onGitDiscard={handleGitDiscard}
                onTerminalExpandedChange={setIsTerminalExpandedPersisted}
                isConnected={state.isConnected}
                chatSessions={state.chatSessions}
                activeChatId={state.activeChatId}
                onActiveChatChange={handleActiveChatChange}
                onCreateChat={handleCreateChat}
                onDeleteChat={handleDeleteChat}
                onRenameChat={handleRenameChat}
                perChatCache={state.perChatCache}
              />
              <OnboardingDialog
                onboarding={onboardingHook.onboarding}
                selectedProvider={onboardingHook.selectedProvider}
                recommendedProviders={onboardingHook.recommendedProviders}
                advancedProviders={onboardingHook.advancedProviders}
                windowsGuidance={onboardingHook.windowsGuidance}
                onProviderChange={onboardingHook.onProviderChange}
                onComplete={onboarding.onComplete}
                onRefresh={onboardingHook.refreshStatus}
                onInstallWsl={onboardingHook.onInstallWsl}
                onInstallGitBash={onboardingHook.onInstallGitBash}
                updateOnboarding={onboardingHook.updateOnboarding}
              />
            </UIManager>
          </EditorManagerProvider>
        </HotkeyProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}

export default App;
