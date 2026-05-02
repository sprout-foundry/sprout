import React, { useMemo, useRef, useCallback, useState } from 'react';
import ErrorBoundary from './components/ErrorBoundary';
import AppContent from './components/AppContent';
import UIManager from './components/UIManager';
import Notification from './components/Notification';
import UpdateNotification from './components/UpdateNotification';
import OnboardingDialog from './components/OnboardingDialog';
import SecurityApprovalDialog from './components/SecurityApprovalDialog';
import SecurityPromptDialog from './components/SecurityPromptDialog';
import { AppStateProvider } from './contexts/AppStateContext';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import { NotificationProvider } from './contexts/NotificationContext';
import { PlatformNavProvider } from './contexts/PlatformNavContext';
import { SproutAdapterProvider } from './contexts/SproutAdapterContext';
import { EventsContextProvider } from './contexts/EventsContext';
import { LocalEventsProvider } from './services/localEventsProvider';
import type { AppState } from './types/app';
import { useAppState } from './hooks/useAppState';
import { useAppStatePersistence } from './hooks/useAppStatePersistence';
import useWebSocketEvents from './hooks/useWebSocketEvents';
import { useChatSessions } from './hooks/useChatSessions';
import { useMessageSending } from './hooks/useMessageSending';
import { useQueuedMessages, useQueuedMessagesAutoSend } from './hooks/useQueuedMessages';
import useOnboarding from './hooks/useOnboarding';
import { useModelProviderHandlers } from './hooks/useModelProviderHandlers';
import { useSidebarState } from './hooks/useSidebarState';
import { useAppInitialization } from './hooks/useAppInitialization';
import { useSessionRestored } from './hooks/useSessionRestored';
import { useBackendReachable } from './hooks/useBackendReachable';
import { useGitHandlers } from './hooks/useGitHandlers';
import { useSecurityHandlers } from './hooks/useSecurityHandlers';
import BackendConnectionBanner from './components/BackendConnectionBanner';
import { triggerHealthCheck } from './services/backendHealth';
import { MAX_PERSISTED_LOGS } from './constants/app';
import './App.css';
import './components/UpdateNotification.css';

interface AppInnerProps {
  eventsProvider: LocalEventsProvider;
}

/**
 * Inner component that lives inside all context providers.
 * This allows us to use hooks like useEvents(), useNotifications(), etc.
 */
function AppInner({ eventsProvider }: AppInnerProps): JSX.Element {
  // Core state
  const { state, setState } = useAppState();
  const [inputValue, setInputValue] = useState('');
  const [gitRefreshToken, setGitRefreshToken] = useState(0);
  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);

  // Connected refs for hooks that need them
  const isConnectedRef = useRef(state.isConnected);
  isConnectedRef.current = state.isConnected;

  // Queued messages (must be before useWebSocketEvents so refs are available)
  const { queuedMessages, queuedMessagesRef, setQueuedMessages, handleQueueMessage } = useQueuedMessages();

  // Websocket events and session management
  const { handleEvent, activeChatIdRef, activeRequestsRef, connectionTimeoutRef, handleReconnect } =
    useWebSocketEvents({
      state,
      setState,
      setQueuedMessages,
      queuedMessagesRef,
    });

  const {
    loadChatSessions,
    handleActiveChatChange,
    handleCreateChat,
    handleDeleteChat,
    handleRenameChat,
  } = useChatSessions({
    setState,
    activeChatIdRef,
    activeRequestsRef,
  });

  // Onboarding (before useMessageSending — provides refreshOnboardingStatus)
  const {
    onboarding,
    selectedProvider,
    recommendedProviders,
    advancedProviders,
    windowsGuidance,
    refreshStatus: refreshOnboardingStatus,
    onProviderChange: handleOnboardingProviderChange,
    onComplete: handleCompleteOnboarding,
    onSkip: handleSkipOnboarding,
    onInstallWsl,
    onInstallGitBash,
    updateOnboarding,
  } = useOnboarding();

  // Ref to hold the latest refreshOnboardingStatus so useMessageSending can use it
  // without creating a circular dependency in hook ordering.
  const refreshOnboardingStatusRef = useRef(refreshOnboardingStatus);
  refreshOnboardingStatusRef.current = refreshOnboardingStatus;

  // Message sending
  const { handleSendMessage: handleMessageSend, handleStopProcessing } = useMessageSending({
    setState,
    setInputValue,
    activeChatIdRef,
    activeRequestsRef,
    isConnectedRef,
    onRequestProviderSetup: useCallback(() => {
      refreshOnboardingStatusRef.current();
    }, []),
  });

  // Security handlers (use eventsProvider directly, not useEvents)
  const { handleSecurityApprovalResponse, handleSecurityPromptResponse } = useSecurityHandlers({
    eventsProvider,
    setState,
  });

  // Handler that applies the onboarding provider/model to app state
  const handleOnboardingComplete = useCallback(async () => {
    handleCompleteOnboarding(async (values) => {
      setState((prev: AppState) => ({
        ...prev,
        provider: values.provider,
        model: values.model,
      }));
    });
  }, [handleCompleteOnboarding, setState]);

  // Model/provider/view handlers (uses useEvents, must be inside EventsContextProvider)
  const { handleModelChange, handleProviderChange, handleViewChange: handleViewChangeBase } = useModelProviderHandlers({
    state,
    setState,
  });

  // Wrapper for view change to match AppContent's expected type
  const handleViewChange = useCallback(
    (view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team') => {
      if (view === 'tasks' || view === 'billing' || view === 'team') return;
      handleViewChangeBase(view);
    },
    [handleViewChangeBase],
  );

  // Sidebar state
  const {
    isMobile,
    isSidebarOpen,
    sidebarCollapsed,
    isTerminalExpanded,
    setIsMobile,
    toggleSidebar,
    closeSidebar,
    handleSidebarToggle,
    setIsTerminalExpanded,
  } = useSidebarState();

  // Git handlers
  const {
    handleGitCommit,
    handleGitAICommit,
    handleGitStage,
    handleGitUnstage,
    handleGitDiscard,
  } = useGitHandlers({
    setGitRefreshToken,
  });

  // Sidebar state
  const stats = useMemo(
    () => ({
      queryCount: state.queryCount,
      filesModified: 0,
    }),
    [state.queryCount],
  );

  // Keep a larger client-side log buffer available to the sidebar logs view.
  const recentLogs = useMemo(
    () => state.logs.slice(-MAX_PERSISTED_LOGS),
    [state.logs],
  );

  // Backend reachability
  const { isReachable: backendReachable } = useBackendReachable();

  // Retry connection handler
  const handleRetryConnection = useCallback(async () => {
    try {
      await triggerHealthCheck();
    } catch {
      /* Health check failed — hook will keep showing offline state */
    }
  }, []);

  // Initial setup effects (only run once via dependencies array)
  useAppInitialization({
    handleEvent,
    connectionTimeoutRef,
    loadChatSessions,
    setRecentFiles,
    setIsMobile,
    setState,
    handleReconnect,
  });

  useSessionRestored({ setState });

  // Auto-send queued messages when processing completes
  useQueuedMessagesAutoSend(
    state,
    activeRequestsRef,
    queuedMessagesRef,
    setQueuedMessages,
    handleMessageSend,
    setState,
  );

  // Persist state to localStorage
  useAppStatePersistence({ state });

  return (
    <UIManager>
      <BackendConnectionBanner isReachable={backendReachable} />
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
        onSendMessage={handleMessageSend}
        onQueueMessage={handleQueueMessage}
        onStopProcessing={handleStopProcessing}
        queuedMessagesCount={queuedMessages.length}
        onGitCommit={handleGitCommit}
        onGitAICommit={handleGitAICommit}
        onGitStage={handleGitStage}
        onGitUnstage={handleGitUnstage}
        onGitDiscard={handleGitDiscard}
        onTerminalExpandedChange={setIsTerminalExpanded}
        isConnected={state.isConnected}
        backendReachable={backendReachable}
        onRetryConnection={handleRetryConnection}
        chatSessions={state.chatSessions}
        activeChatId={state.activeChatId}
        onActiveChatChange={handleActiveChatChange}
        onCreateChat={handleCreateChat}
        onDeleteChat={handleDeleteChat}
        onRenameChat={handleRenameChat}
        perChatCache={state.perChatCache}
      />
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
      <OnboardingDialog
        onboarding={onboarding}
        selectedProvider={selectedProvider}
        recommendedProviders={recommendedProviders}
        advancedProviders={advancedProviders}
        windowsGuidance={windowsGuidance}
        onProviderChange={handleOnboardingProviderChange}
        onComplete={handleOnboardingComplete}
        onSkip={handleSkipOnboarding}
        onRefresh={refreshOnboardingStatus}
        onInstallWsl={onInstallWsl}
        onInstallGitBash={onInstallGitBash}
        updateOnboarding={updateOnboarding}
      />
      <Notification />
      <UpdateNotification />
    </UIManager>
  );
}

/**
 * Main App component.
 * Sets up provider tree and instantiates LocalEventsProvider.
 */
function App(): JSX.Element {
  const eventsProvider = useMemo(() => new LocalEventsProvider(), []);

  return (
    <ErrorBoundary
      onError={(error, errorInfo) => {
        console.error('Application error:', error, errorInfo);
      }}
    >
      <AppStateProvider>
        <SproutAdapterProvider>
          <EventsContextProvider provider={eventsProvider}>
            <ThemeProvider>
              <NotificationProvider>
                <PlatformNavProvider>
                  <HotkeyProvider>
                    <EditorManagerProvider>
                      <AppInner eventsProvider={eventsProvider} />
                    </EditorManagerProvider>
                  </HotkeyProvider>
                </PlatformNavProvider>
              </NotificationProvider>
            </ThemeProvider>
          </EventsContextProvider>
        </SproutAdapterProvider>
      </AppStateProvider>
    </ErrorBoundary>
  );
}

export default App;
