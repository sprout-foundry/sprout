import { EventsContextProvider, useEvents } from '@sprout/events';
import { useState, useCallback, useRef, useMemo, useEffect } from 'react';
import AppContent from './components/AppContent';
import AskUserDialog from './components/AskUserDialog';
import { DisconnectedOverlay } from './components/DisconnectedOverlay';
import DriftNotification from './components/DriftNotification';
import EditApprovalPanel from './components/EditApprovalPanel';
import ErrorBoundary from './components/ErrorBoundary';
import { EscalationListener } from './components/EscalationListener';
import KeyboardShortcutsModal from './components/KeyboardShortcutsModal';
import ModelSelectionModal from './components/ModelSelectionModal';
import NotificationCenter from './components/NotificationCenter';
import OnboardingDialog from './components/OnboardingDialog';
import SecurityApprovalDialog from './components/SecurityApprovalDialog';
import ShellApprovalPanel from './components/ShellApprovalPanel';
import PasswordPromptDialog from './components/PasswordPromptDialog';
import SecurityPromptDialog from './components/SecurityPromptDialog';
import UIManager from './components/UIManager';
import UpdateNotification from './components/UpdateNotification';
import { WasmLoadingOverlay } from './components/WasmLoadingOverlay';
import { MAX_PERSISTED_LOGS } from './constants/app';
import { AppStoreProvider, useAppStoreSetState, useAppStoreState } from './contexts/AppStore';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import { NotificationProvider } from './contexts/NotificationContext';
import { PlatformNavProvider } from './contexts/PlatformNavContext';
import { ProviderCatalogProvider } from './contexts/ProviderCatalogContext';
import { SproutAdapterProvider } from './contexts/SproutAdapterContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { useAppInitialization } from './hooks/useAppInitialization';
import { useAppStatePersistence } from './hooks/useAppStatePersistence';
import { useChatSessionManager } from './hooks/useChatSessionManager';
import { useCloudSessionPersistence } from './hooks/useCloudSessionPersistence';
import { useEscalationTriggers } from './hooks/useEscalationTriggers';
import { useGitHandlers } from './hooks/useGitHandlers';
import { useModelProviderHandlers } from './hooks/useModelProviderHandlers';
import useOnboarding from './hooks/useOnboarding';
import { usePageVisibility } from './hooks/usePageVisibility';
import { useSecurityHandlers } from './hooks/useSecurityHandlers';
import { useSidebarState } from './hooks/useSidebarState';
import type { UseWebSocketEventHandlerRefs } from './hooks/useWebSocketEventHandler';
import { useWebSocketEventHandler } from './hooks/useWebSocketEventHandler';
import { ApiService } from './services/api';
import { loadPersistedAppState } from './services/appStatePersistence';
import { LocalEventsProvider } from './services/localEventsProvider';
import { notificationBus } from './services/notificationBus';
import { debugLog } from './utils/log';
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
      driftNotification: null,
      outputVerbosity: 'default' as const,
      inputValue: '',
    };
  }, []);

  const eventsProvider = useMemo(() => new LocalEventsProvider(), []);

  return (
    <AppStoreProvider initialState={initialState}>
      <NotificationProvider>
        <EventsContextProvider provider={eventsProvider}>
          <AppInner />
        </EventsContextProvider>
      </NotificationProvider>
    </AppStoreProvider>
  );
}

function AppInner() {
  const state = useAppStoreState();
  const setState = useAppStoreSetState();
  const events = useEvents();

  // Freeze the WebSocket when the tab is hidden (clean close so the backend
  // detaches) and resume + reattach when it returns to the foreground. Without
  // this, a backgrounded tab's socket is killed by the server's read deadline
  // and only recovers slowly via the pong watchdog. (Accidentally dropped in a
  // hooks-consolidation refactor; restored here.)
  usePageVisibility();

  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);
  const [gitRefreshToken, setGitRefreshToken] = useState(0);
  const [showKeyboardShortcuts, setShowKeyboardShortcuts] = useState(false);

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
  } = useSidebarState();

  // ── Refs ───────────────────────────────────────────────────────

  const activeRequestsRef = useRef(0);
  const queuedMessagesRef = useRef<string[]>([]);
  const activeChatIdRef = useRef<string | null>(null);
  activeChatIdRef.current = state.activeChatId;
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef<boolean>(false);
  // Shared refs for tracking provider change state across hooks
  const pendingProviderChangeRef = useRef(false);
  const pendingProviderChangeValueRef = useRef<string | null>(null);

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

  const { handleModelChange, handleProviderChange, handleViewChange, pendingProviderRef } = useModelProviderHandlers({
    state,
    setState,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
  });

  const wsEventHandlerRefs: UseWebSocketEventHandlerRefs = {
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
    isProcessing: state.isProcessing,
  });

  // ── Persistence ───────────────────────────────────────────────────

  useAppStatePersistence({ state });

  // Cloud mode: mirror the conversation to localStorage so it survives
  // page reloads and appears in the session picker. No-op in local mode.
  useCloudSessionPersistence({ state });

  // ── Handlers ─────────────────────────────────────────────────────

  const {
    handleSecurityApprovalResponse,
    handleSecurityPromptResponse,
    handleAskUserResponse,
    handlePasswordResponse,
    handleModelSelectionResponse,
    handleModelSelectionClose,
  } = useSecurityHandlers({
    eventsProvider: events,
    provider: state.provider,
    setState,
  });

  const { handleGitCommit, handleGitAICommit, handleGitStage, handleGitUnstage, handleGitDiscard } = useGitHandlers({
    setGitRefreshToken,
  });

  // Clear the edit approval request state after the panel's REST POST completes.
  const handleEditApprovalResolved = useCallback(() => {
    setState((_prev) => ({ editApprovalRequest: null }));
  }, [setState]);

  // ── Initialization ───────────────────────────────────────────────

  useAppInitialization({
    eventsProvider: events,
    handleEvent,
    connectionTimeoutRef,
    loadChatSessions: chatManager.loadChatSessions,
    setRecentFiles,
    setIsMobile,
    setIsTablet,
    setState,
    handleReconnect,
  });

  // ── Escalation Triggers (cloud mode) ────────────────────────────

  const repoURL = useMemo(() => {
    if (typeof window === 'undefined') return undefined;
    const params = new URLSearchParams(window.location.search);
    return params.get('repo') ?? undefined;
  }, []);

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
      <SproutAdapterProvider>
        <PlatformNavProvider>
          <ThemeProvider>
            <HotkeyProvider>
              <EditorManagerProvider>
                <ProviderCatalogProvider isConnected={state.isConnected}>
                  <UIManager>
                    <NotificationCenter />
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
                    <UpdateNotification />
                    <EscalationListener />
                    <DisconnectedOverlay isConnected={state.isConnected} />
                    {state.securityApprovalRequest && (
                      <SecurityApprovalDialog
                        requestId={state.securityApprovalRequest.requestId}
                        toolName={state.securityApprovalRequest.toolName}
                        riskLevel={state.securityApprovalRequest.riskLevel as 'SAFE' | 'CAUTION' | 'DANGEROUS'}
                        reasoning={state.securityApprovalRequest.reasoning}
                        command={state.securityApprovalRequest.command}
                        riskType={state.securityApprovalRequest.riskType}
                        target={state.securityApprovalRequest.target}
                        allowOptions={state.securityApprovalRequest.allowOptions}
                        fsKind={state.securityApprovalRequest.fsKind}
                        fsFolder={state.securityApprovalRequest.fsFolder}
                        fsPath={state.securityApprovalRequest.fsPath}
                        securityAnalysis={state.securityApprovalRequest.securityAnalysis}
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
                        header={state.askUserRequest.header}
                        options={state.askUserRequest.options}
                        multiSelect={state.askUserRequest.multiSelect}
                        defaultValue={state.askUserRequest.default}
                        onRespond={handleAskUserResponse}
                      />
                    )}
                    {state.passwordRequest && (
                      <PasswordPromptDialog
                        key={state.passwordRequest.requestId}
                        requestId={state.passwordRequest.requestId}
                        command={state.passwordRequest.command}
                        prompt={state.passwordRequest.prompt}
                        onRespond={handlePasswordResponse}
                      />
                    )}
                    {state.editApprovalRequest && (
                      <EditApprovalPanel
                        requestId={state.editApprovalRequest.requestId}
                        filePath={state.editApprovalRequest.filePath}
                        unifiedDiff={state.editApprovalRequest.unifiedDiff}
                        hunks={state.editApprovalRequest.hunks}
                        onRespond={handleEditApprovalResolved}
                      />
                    )}
                    {state.shellApprovalRequest && (
                      <ShellApprovalPanel
                        request={{
                          request_id: state.shellApprovalRequest.requestId,
                          command: state.shellApprovalRequest.command,
                          parts: state.shellApprovalRequest.parts,
                          unified_view: state.shellApprovalRequest.unifiedView,
                          risk_level: state.shellApprovalRequest.riskLevel,
                          security_analysis: state.shellApprovalRequest.securityAnalysis
                            ? {
                                summary: state.shellApprovalRequest.securityAnalysis.summary,
                                modifies: state.shellApprovalRequest.securityAnalysis.modifies,
                                risk_assessment: state.shellApprovalRequest.securityAnalysis.riskAssessment,
                                recommendation: state.shellApprovalRequest.securityAnalysis.recommendation,
                              }
                            : undefined,
                        }}
                        onSubmit={async () => {
                          // Clear the request on submit; the handler POSTs the decision
                          setState(() => ({ shellApprovalRequest: null }));
                        }}
                      />
                    )}
                    {state.driftNotification && (
                      <DriftNotification
                        similarity={state.driftNotification.similarity}
                        threshold={state.driftNotification.threshold}
                        sessionId={state.driftNotification.sessionId}
                        options={state.driftNotification.options}
                        onContinue={() => setState(() => ({ driftNotification: null }))}
                        onNewChat={() => {
                          setState(() => ({ driftNotification: null }));
                          chatManager.handleCreateChat();
                        }}
                      />
                    )}
                    {state.modelSelectionRequest && (
                      <ModelSelectionModal
                        provider={state.modelSelectionRequest.provider}
                        reason={state.modelSelectionRequest.reason}
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
                    {showKeyboardShortcuts && (
                      <KeyboardShortcutsModal onClose={() => setShowKeyboardShortcuts(false)} />
                    )}
                  </UIManager>
                </ProviderCatalogProvider>
              </EditorManagerProvider>
            </HotkeyProvider>
          </ThemeProvider>
        </PlatformNavProvider>
      </SproutAdapterProvider>
    </ErrorBoundary>
  );
}

export default App;
