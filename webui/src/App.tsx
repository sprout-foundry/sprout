import { useEvents } from '@sprout/events';
import { useState, useCallback, useMemo, useEffect } from 'react';
import { AgentEscalationBridge } from './components/AgentEscalationBridge';
import AppContent from './components/AppContent';
import { DisconnectedOverlay } from './components/DisconnectedOverlay';
import ErrorBoundary from './components/ErrorBoundary';
import { EscalationListener } from './components/EscalationListener';
import InstallPromptBanner from './components/InstallPromptBanner';
import KeyboardShortcutsModal from './components/KeyboardShortcutsModal';
import NotificationCenter from './components/NotificationCenter';
import OnboardingDialog from './components/OnboardingDialog';
import SyncStatusBanner from './components/SyncStatusBanner';
import UIManager from './components/UIManager';
import UpdateAvailableBanner from './components/UpdateAvailableBanner';
import UpdateNotification from './components/UpdateNotification';
import { WasmLoadingOverlay } from './components/WasmLoadingOverlay';
import { MAX_PERSISTED_LOGS } from './constants/app';
import { useAppStoreState } from './contexts/AppStore';
import { useAppInitialization } from './hooks/useAppInitialization';
import { useAppStatePersistence } from './hooks/useAppStatePersistence';
import { useCloudSessionPersistence } from './hooks/useCloudSessionPersistence';
import { useEscalationTriggers } from './hooks/useEscalationTriggers';
import { useGitHandlers } from './hooks/useGitHandlers';
import { useModelProviderHandlers } from './hooks/useModelProviderHandlers';
import useOnboarding from './hooks/useOnboarding';
import { usePageVisibility } from './hooks/usePageVisibility';
import { useSidebarState } from './hooks/useSidebarState';
import { SproutProviders } from './providers';
import { useActiveRepoURL } from './services/activeRepo';
import { ApiService } from './services/api';
import { loadPersistedAppState } from './services/appStatePersistence';
import { clientFetch } from './services/clientSession';
import { LocalEventsProvider } from './services/localEventsProvider';
import { notificationBus } from './services/notificationBus';
import { debugLog } from './utils/log';
import { WorkspaceAgentPrompts, WorkspaceChatProvider, WorkspaceEditorProvider, useWorkspaceChat } from './views';
import './App.css';
import './components/UpdateNotification.css';

// ── App Component ─────────────────────────────────────────────────────

function App() {
  const initialState = useMemo(() => {
    const persisted = loadPersistedAppState();
    return {
      provider: persisted?.provider || 'unknown',
      model: persisted?.model || 'unknown',
      sessionId: persisted?.sessionId || null,
      queryCount: persisted?.queryCount || 0,
      currentView: (() => {
        // Deep-link: ?view=chat|editor overrides persisted state
        if (typeof window !== 'undefined') {
          const params = new URLSearchParams(window.location.search);
          const viewParam = params.get('view');
          if (viewParam === 'chat' || viewParam === 'editor') {
            return viewParam;
          }
        }
        return persisted?.currentView || 'chat';
      })(),
      messages: [],
      logs: [],
      toolExecutions: [],
      stats: {},
      currentTodos: [],
      fileEdits: [],
      subagentActivities: [],
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
      passwordRequest: null,
      editApprovalRequest: null,
      shellApprovalRequest: null,
      modelSelectionRequest: null,
      outputVerbosity: 'default' as const,
      inputValue: '',
      workspaceBusy: null,
    };
  }, []);

  const eventsProvider = useMemo(() => new LocalEventsProvider(), []);

  // The app's single provider stack. Everything below, AppInner and its
  // shell included, reads these instances (one editor manager, one hotkey
  // provider); the stack's connection state follows the transport.
  return (
    <SproutProviders eventsProvider={eventsProvider}>
      <WorkspaceChatProvider initialState={initialState} eventsProvider={eventsProvider} fetchFn={clientFetch}>
        <AppInner />
      </WorkspaceChatProvider>
    </SproutProviders>
  );
}

function AppInner() {
  const state = useAppStoreState();
  const events = useEvents();
  const { setState, handleEvent, handleReconnect, chat: chatManager, refs: chatRefs } = useWorkspaceChat();

  // Freeze the WebSocket when the tab is hidden (clean close so the backend
  // detaches) and resume + reattach when it returns to the foreground. Without
  // this, a backgrounded tab's socket is killed by the server's read deadline
  // and only recovers slowly via the pong watchdog. (Accidentally dropped in a
  // hooks-consolidation refactor; restored here.)
  usePageVisibility();

  const [gitRefreshToken, setGitRefreshToken] = useState(0);
  const [showKeyboardShortcuts, setShowKeyboardShortcuts] = useState(false);

  // Cloud mode: refresh the git panel after a ?repo= import completes.
  // The import writes files into the WASM VFS (via the platform's
  // /api/repo/import) and the in-browser git syncs VFS files into its
  // working tree on the next status call. The panel's initial status
  // load happens before the import finishes (falling back to the
  // platform's /api/git/status, which has no per-user git), so without
  // this refresh the panel stays stuck on "No git repository found".
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const handleImported = () => {
      // Let the WASM shell's VFS writes settle before the browser git's
      // status call syncs them into the working tree (matches the
      // pre-mount race handling in useAppInitialization).
      timer = setTimeout(() => setGitRefreshToken((n) => n + 1), 500);
    };
    window.addEventListener('sprout:repo-imported', handleImported);
    // The import may have completed before this mounted (a cached repo
    // imports near-instantly on the second visit).
    if ((window as unknown as Record<string, unknown>).__repoImported) handleImported();
    return () => {
      window.removeEventListener('sprout:repo-imported', handleImported);
      if (timer) clearTimeout(timer);
    };
  }, []);

  // Keyboard shortcuts modal — listen for menu/welcome-tab event and `?` shortcut.
  // The menu dispatches `sprout:open-hotkeys-config`; the dedicated JSON-editing
  // path (SidebarSettingsSection's "Edit Keyboard Shortcuts (JSON)" button) uses
  // `sprout:open-hotkeys-json` so it doesn't trigger the modal.
  useEffect(() => {
    const handler = () => setShowKeyboardShortcuts(true);
    window.addEventListener('sprout:open-hotkeys-config', handler);
    return () => window.removeEventListener('sprout:open-hotkeys-config', handler);
  }, []);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key !== '?' || e.ctrlKey || e.metaKey || e.altKey) return;
      const el = document.activeElement as HTMLElement | null;
      if (el?.tagName === 'INPUT' || el?.tagName === 'TEXTAREA' || el?.isContentEditable) return;
      e.preventDefault();
      setShowKeyboardShortcuts(true);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

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
    cycleSidebarSnap,
  } = useSidebarState();

  // ── Hooks ───────────────────────────────────────────────────────

  const apiService = ApiService.getInstance();

  // SP-076: sync output_verbosity from backend settings into AppState.
  // The settings panel saves to the backend config; the frontend reads
  // it back here so MessageItem can filter narration in compact mode.
  useEffect(() => {
    if (!state.isConnected) return;
    let cancelled = false;
    apiService
      .getSettings()
      .then((settings) => {
        if (cancelled) return;
        const verbosity = settings.output_verbosity;
        if (verbosity === 'compact' || verbosity === 'default' || verbosity === 'verbose') {
          setState((prev) => (prev.outputVerbosity === verbosity ? prev : { outputVerbosity: verbosity }));
        }
      })
      .catch(() => {
        // Settings load failure is non-fatal — default verbosity applies.
      });
    return () => {
      cancelled = true;
    };
  }, [state.isConnected, apiService, setState]);

  // The chat unit owns the refs the event reducer and the send path share;
  // the model/provider handlers write the same provider-change refs and the
  // pending-provider ref directly, so a provider switch and the events it
  // drives stay in step.
  const { handleModelChange, handleProviderChange, handleViewChange } = useModelProviderHandlers({
    state,
    setState,
    pendingProviderChangeRef: chatRefs.pendingProviderChangeRef,
    pendingProviderChangeValueRef: chatRefs.pendingProviderChangeValueRef,
    pendingProviderRef: chatRefs.pendingProviderRef,
  });

  // ── Persistence ───────────────────────────────────────────────────

  useAppStatePersistence({ state });

  // Cloud mode: mirror the conversation to localStorage so it survives
  // page reloads and appears in the session picker. No-op in local mode.
  useCloudSessionPersistence({ state });

  // ── Handlers ─────────────────────────────────────────────────────

  const { handleGitCommit, handleGitAICommit, handleGitStage, handleGitUnstage, handleGitDiscard } = useGitHandlers({
    setGitRefreshToken,
  });

  // ── Initialization ───────────────────────────────────────────────

  useAppInitialization({
    eventsProvider: events,
    handleEvent,
    connectionTimeoutRef: chatRefs.connectionTimeoutRef,
    loadChatSessions: chatManager.loadChatSessions,
    setIsMobile,
    setIsTablet,
    setState,
    handleReconnect,
    // The chat unit owns the event/reconnect subscription; this hook only
    // connects the transport and runs the boot sequence.
    manageEventSubscription: false,
    // SP-147: one conversation per project — boot has no per-mode chat
    // restore, so no lane-aware create/switch hooks are passed. The server's
    // active chat (or the most recent non-empty one) comes back whatever the
    // mode.
  });

  // ── Escalation Triggers (cloud mode) ────────────────────────────

  const repoURL = useActiveRepoURL();

  useEscalationTriggers({
    repoURL,
    onBlockingTrigger: (evt) => {
      notificationBus.notify('warning', 'Browser limitation', evt.message);
    },
    onInfoTrigger: (evt) => {
      notificationBus.notify('info', 'Heads up', evt.message);
    },
  });

  // ── Onboarding ───────────────────────────────────────────────────

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

  const handleCompleteOnboarding = useCallback(async () => {
    await onComplete((vals) => {
      setState((_prev) => ({
        provider: vals.provider,
        model: vals.model,
      }));
    });
  }, [onComplete, setState]);

  // ── Terminal Handler ─────────────────────────────────────────────

  const handleTerminalOutput = useCallback((output: string) => {
    debugLog('[term] Terminal output:', output);
  }, []);

  // ── Memos ───────────────────────────────────────────────────────

  const recentLogs = useMemo(() => state.logs.slice(-MAX_PERSISTED_LOGS), [state.logs]);

  const stats = useMemo(
    () => ({
      queryCount: state.queryCount,
      // Not currently tracked — no consumer reads this field today. The
      // type is preserved across the Sidebar/AppContent prop boundary
      // because @sprout/ui's ChatProps expects it; a future buffer-dirty
      // signal can populate it without touching that interface.
      filesModified: 0,
    }),
    [state.queryCount],
  );

  // ── Render ───────────────────────────────────────────────────────

  return (
    <ErrorBoundary
      onError={(error, errorInfo) => {
        console.error('=== ERROR BOUNDARY CAUGHT ===');
        console.error('Error:', error.message);
        console.error('Stack:', error.stack);
        console.error('Component Stack:', errorInfo.componentStack);
      }}
    >
      <WasmLoadingOverlay isLoading={!!state.wasmLoading} error={state.wasmError} />
      <UIManager>
        <NotificationCenter />
        <WorkspaceEditorProvider>
          <AppContent
            state={state}
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
            // Double-tap/double-click on the grip cycles snap
            // presets (narrow → standard → wide, wraps) instead
            // of only resetting to default.
            onSidebarWidthReset={cycleSidebarSnap}
            stats={stats}
            recentLogs={recentLogs}
            gitRefreshToken={gitRefreshToken}
            onSidebarToggle={handleSidebarToggle}
            onToggleSidebar={toggleSidebar}
            onCloseSidebar={closeSidebar}
            onViewChange={handleViewChange}
            onModelChange={handleModelChange}
            onProviderChange={handleProviderChange}
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
            onCreateChatInWorktree={chatManager.handleCreateChatInWorktree}
            onDeleteChat={chatManager.handleDeleteChat}
            onDeleteAllChats={chatManager.handleDeleteAllChats}
            onRenameChat={chatManager.handleRenameChat}
            perChatCache={state.perChatCache}
          />
        </WorkspaceEditorProvider>
        <UpdateNotification />
        <UpdateAvailableBanner />
        <EscalationListener />
        <AgentEscalationBridge repoURL={repoURL} />
        <InstallPromptBanner />
        <SyncStatusBanner />
        <DisconnectedOverlay isConnected={state.isConnected} />
        {/* The agent's blocking prompts (approvals, ask_user, passwords, edit and
            shell approvals) and the model picker, from the chat unit's state. */}
        <WorkspaceAgentPrompts />
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
        {showKeyboardShortcuts && <KeyboardShortcutsModal onClose={() => setShowKeyboardShortcuts(false)} />}
      </UIManager>
    </ErrorBoundary>
  );
}

export default App;
