import { useState, useMemo, type Dispatch, type SetStateAction } from 'react';
import ErrorBoundary from './components/ErrorBoundary';
import AppContent from './components/AppContent';
import UIManager from './components/UIManager';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import { NotificationProvider } from './contexts/NotificationContext';
import './App.css';
import useOnboarding from './hooks/useOnboarding';
import OnboardingDialog from './components/OnboardingDialog';
import Notification from './components/Notification';
import { usePageVisibility } from './hooks/usePageVisibility';
import { MAX_PERSISTED_LOGS } from './constants/app';

// Hooks that need NotificationProvider (used inside AppWithProviders)
import useWebSocketEvents from './hooks/useWebSocketEvents';
import { useChatSessions } from './hooks/useChatSessions';
import { useMessageSending } from './hooks/useMessageSending';
import { useSecurityApproval } from './hooks/useSecurityApproval';
import { useSecurityPrompt } from './hooks/useSecurityPrompt';
import { useAppInitialization } from './hooks/useAppInitialization';
import { useSessionRestored } from './hooks/useSessionRestored';

// Hooks that do NOT need providers (used in outer App)
import { useAppState } from './hooks/useAppState';
import { useSidebarState } from './hooks/useSidebarState';
import { useGitActions } from './hooks/useGitActions';
import { useQueuedMessages, useQueuedMessagesAutoSend } from './hooks/useQueuedMessages';
import { useChatPersistence } from './hooks/useChatPersistence';
import { useModelProviderHandlers } from './hooks/useModelProviderHandlers';

import SecurityApprovalDialog from './components/SecurityApprovalDialog';
import SecurityPromptDialog from './components/SecurityPromptDialog';
import { notificationBus } from './services/notificationBus';

/**
 * Outer App — initialises state that does NOT require any providers.
 * Avoids calling hooks like useNotifications before NotificationProvider
 * is mounted in the tree.
 */
function App() {
  // ── 1. Core state (no providers needed) ──────────────────────────
  const { state, setState } = useAppState();
  const [inputValue, setInputValue] = useState('');

  // ── 2. Queued messages (no providers needed) ────────────────────
  const {
    queuedMessages,
    queuedMessagesRef,
    setQueuedMessages,
    handleQueueMessage,
    handleRemoveQueuedMessage,
    handleEditQueuedMessage,
    handleReorderQueuedMessages,
    handleClearQueuedMessages,
  } = useQueuedMessages();

  // ── 3. Sidebar & git state (no providers needed) ───────────────
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

  const { gitRefreshToken, handleGitCommit, handleGitAICommit, handleGitStage, handleGitUnstage, handleGitDiscard } =
    useGitActions();

  // ── 4. Persistence & derived values (no providers needed) ───────
  useChatPersistence(state);
  usePageVisibility();

  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);
  const recentLogs = useMemo(() => state.logs.slice(-MAX_PERSISTED_LOGS), [state.logs]);

  const stats = useMemo(
    () => ({
      queryCount: state.queryCount,
      filesModified: 0, // TODO: track modified files from buffers
    }),
    [state.queryCount],
  );

  const onboardingHook = useOnboarding();
  const onboarding = {
    ...onboardingHook,
    onComplete: () => onboardingHook.onComplete((values) => setState((prev) => ({ ...prev, ...values }))),
  };

  const { handleModelChange, handleProviderChange, handleViewChange, handlePersonaChange } = useModelProviderHandlers({
    state,
    setState,
  });

  // ── Render ──────────────────────────────────────────────────────
  return (
    <ErrorBoundary
      onError={(_error, _errorInfo) => {
        notificationBus.notify(
          'error',
          'Application Error',
          'An unexpected error occurred. Check the console for details.',
          8000,
        );
      }}
    >
      <ThemeProvider>
        <NotificationProvider>
          <HotkeyProvider>
            <EditorManagerProvider>
              <AppWithProviders
                state={state}
                setState={setState}
                inputValue={inputValue}
                setInputValue={setInputValue}
                queuedMessages={queuedMessages}
                queuedMessagesRef={queuedMessagesRef}
                setQueuedMessages={setQueuedMessages}
                handleQueueMessage={handleQueueMessage}
                handleRemoveQueuedMessage={handleRemoveQueuedMessage}
                handleEditQueuedMessage={handleEditQueuedMessage}
                handleReorderQueuedMessages={handleReorderQueuedMessages}
                handleClearQueuedMessages={handleClearQueuedMessages}
                isMobile={isMobile}
                isSidebarOpen={isSidebarOpen}
                sidebarCollapsed={sidebarCollapsed}
                isTerminalExpanded={isTerminalExpanded}
                setIsMobile={setIsMobile}
                toggleSidebar={toggleSidebar}
                closeSidebar={closeSidebar}
                handleSidebarToggle={handleSidebarToggle}
                setIsTerminalExpanded={setIsTerminalExpanded}
                gitRefreshToken={gitRefreshToken}
                handleGitCommit={handleGitCommit}
                handleGitAICommit={handleGitAICommit}
                handleGitStage={handleGitStage}
                handleGitUnstage={handleGitUnstage}
                handleGitDiscard={handleGitDiscard}
                recentFiles={recentFiles}
                setRecentFiles={setRecentFiles}
                recentLogs={recentLogs}
                stats={stats}
                onboardingHook={onboardingHook}
                onboarding={onboarding}
                handleModelChange={handleModelChange}
                handleProviderChange={handleProviderChange}
                handleViewChange={handleViewChange}
                handlePersonaChange={handlePersonaChange}
              />
            </EditorManagerProvider>
            <Notification />
          </HotkeyProvider>
        </NotificationProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}

/**
 * Inner component — rendered inside all providers so hooks that require
 * NotificationProvider, EditorManagerProvider, etc. can safely be used.
 */
function AppWithProviders({
  state,
  setState,
  inputValue,
  setInputValue,
  queuedMessages,
  queuedMessagesRef,
  setQueuedMessages,
  handleQueueMessage,
  handleRemoveQueuedMessage,
  handleEditQueuedMessage,
  handleReorderQueuedMessages,
  handleClearQueuedMessages,
  isMobile,
  isSidebarOpen,
  sidebarCollapsed,
  isTerminalExpanded,
  setIsMobile,
  toggleSidebar,
  closeSidebar,
  handleSidebarToggle,
  setIsTerminalExpanded,
  gitRefreshToken,
  handleGitCommit,
  handleGitAICommit,
  handleGitStage,
  handleGitUnstage,
  handleGitDiscard,
  recentFiles,
  setRecentFiles,
  recentLogs,
  stats,
  onboardingHook,
  onboarding,
  handleModelChange,
  handleProviderChange,
  handleViewChange,
  handlePersonaChange,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
}: {
  state: any;
  setState: any;
  inputValue: string;
  setInputValue: Dispatch<SetStateAction<string>>;
  queuedMessages: any[];
  queuedMessagesRef: any;
  setQueuedMessages: any;
  handleQueueMessage: any;
  handleRemoveQueuedMessage: any;
  handleEditQueuedMessage: any;
  handleReorderQueuedMessages: any;
  handleClearQueuedMessages: any;
  isMobile: boolean;
  isSidebarOpen: boolean;
  sidebarCollapsed: boolean;
  isTerminalExpanded: boolean;
  setIsMobile: any;
  toggleSidebar: any;
  closeSidebar: any;
  handleSidebarToggle: any;
  setIsTerminalExpanded: any;
  gitRefreshToken: number;
  handleGitCommit: any;
  handleGitAICommit: any;
  handleGitStage: any;
  handleGitUnstage: any;
  handleGitDiscard: any;
  recentFiles: any;
  setRecentFiles: any;
  recentLogs: any;
  stats: any;
  onboardingHook: any;
  onboarding: any;
  handleModelChange: any;
  handleProviderChange: any;
  handleViewChange: any;
  handlePersonaChange: any;
}) {
  // ── WebSocket event processing (needs NotificationProvider) ────
  const { handleEvent, activeChatIdRef, activeRequestsRef, connectionTimeoutRef } = useWebSocketEvents({
    state,
    setState,
    setInputValue,
    setQueuedMessages,
    queuedMessagesRef,
  });

  // ── Chat session management (depends on WS refs) ──────────────
  const { loadChatSessions, handleActiveChatChange, handleCreateChat, handleDeleteChat, handleRenameChat } =
    useChatSessions({ setState, activeChatIdRef, activeRequestsRef });

  // ── Message sending (depends on WS refs) ──────────────────────
  const { handleSendMessage, handleStopProcessing } = useMessageSending({
    setState,
    setInputValue,
    activeChatIdRef,
    activeRequestsRef,
  });

  // ── Auto-send queued messages (depends on handleSendMessage) ──
  useQueuedMessagesAutoSend(
    state,
    activeRequestsRef,
    queuedMessagesRef,
    setQueuedMessages,
    handleSendMessage,
    setState,
  );

  // ── Security approval / prompt handlers ──────────────────────
  const { handleSecurityApprovalResponse } = useSecurityApproval(setState);
  const { handleSecurityPromptResponse } = useSecurityPrompt(setState);

  // ── Initialisation effect (WS, stats, files, mobile) ─────────
  useAppInitialization({
    handleEvent,
    connectionTimeoutRef,
    loadChatSessions,
    setRecentFiles,
    setIsMobile,
    setState,
  });

  // ── Session-restored event listener ──────────────────────────
  useSessionRestored({ setState });

  // ── Render ────────────────────────────────────────────────────
  return (
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
        onPersonaChange={handlePersonaChange}
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
        onTerminalExpandedChange={setIsTerminalExpanded}
        isConnected={state.isConnected}
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
  );
}

export default App;
