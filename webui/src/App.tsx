import { EventsContextProvider, useEvents } from '@sprout/events';
import { useState, useCallback, useRef, useMemo } from 'react';
import AppContent from './components/AppContent';
import AskUserDialog from './components/AskUserDialog';
import DriftNotification from './components/DriftNotification';
import ErrorBoundary from './components/ErrorBoundary';
import ModelSelectionModal from './components/ModelSelectionModal';
import Notification from './components/Notification';
import OnboardingDialog from './components/OnboardingDialog';
import SecurityApprovalDialog from './components/SecurityApprovalDialog';
import SecurityPromptDialog from './components/SecurityPromptDialog';
import UIManager from './components/UIManager';
import UpdateNotification from './components/UpdateNotification';
import { MAX_PERSISTED_LOGS } from './constants/app';
import { AppStoreProvider, useAppStoreSetState, useAppStoreState } from './contexts/AppStore';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import { NotificationProvider } from './contexts/NotificationContext';
import { LocalEventsProvider } from './services/localEventsProvider';
import { PlatformNavProvider } from './contexts/PlatformNavContext';
import { SproutAdapterProvider } from './contexts/SproutAdapterContext';
import { ThemeProvider } from './contexts/ThemeContext';
import './App.css';
import './components/UpdateNotification.css';
import { useAppInitialization } from './hooks/useAppInitialization';
import { useAppStatePersistence } from './hooks/useAppStatePersistence';
import { useChatSessionManager } from './hooks/useChatSessionManager';
import { useGitHandlers } from './hooks/useGitHandlers';
import { useModelProviderHandlers } from './hooks/useModelProviderHandlers';
import useOnboarding from './hooks/useOnboarding';
import { useSecurityHandlers } from './hooks/useSecurityHandlers';
import { useSidebarState } from './hooks/useSidebarState';
import type { UseWebSocketEventHandlerRefs } from './hooks/useWebSocketEventHandler';
import { useWebSocketEventHandler } from './hooks/useWebSocketEventHandler';
import { ApiService } from './services/api';
import { loadPersistedAppState } from './services/appStatePersistence';
import { debugLog } from './utils/log';

// ── App Component ─────────────────────────────────────────────────────

function App() {
  const initialState = useMemo(() => {
    const persisted = loadPersistedAppState();
    return {
      provider: persisted?.provider || 'unknown',
      model: persisted?.model || 'unknown',
      sessionId: persisted?.sessionId || null,
      queryCount: persisted?.queryCount || 0,
      currentView: persisted?.currentView || 'chat',
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
      modelSelectionRequest: null,
      driftNotification: null,
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
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef<boolean>(false);
  // Shared refs for tracking provider change state across hooks
  const pendingProviderChangeRef = useRef(false);
  const pendingProviderChangeValueRef = useRef<string | null>(null);

  // ── Hooks ───────────────────────────────────────────────────────

  const apiService = ApiService.getInstance();

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
    setInputValue,
    isProcessing: state.isProcessing,
  });

  // ── Persistence ───────────────────────────────────────────────────

  useAppStatePersistence({ state });

  // ── Handlers ─────────────────────────────────────────────────────

  const {
    handleSecurityApprovalResponse,
    handleSecurityPromptResponse,
    handleAskUserResponse,
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
        console.error('Application error:', error, errorInfo);
        // You could send this to an error reporting service here
      }}
    >
      <SproutAdapterProvider>
        <PlatformNavProvider>
          <ThemeProvider>
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
                      allowOptions={state.securityApprovalRequest.allowOptions}
                      fsKind={state.securityApprovalRequest.fsKind}
                      fsFolder={state.securityApprovalRequest.fsFolder}
                      fsPath={state.securityApprovalRequest.fsPath}
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
                </UIManager>
              </EditorManagerProvider>
            </HotkeyProvider>
          </ThemeProvider>
        </PlatformNavProvider>
      </SproutAdapterProvider>
    </ErrorBoundary>
  );
}

export default App;
