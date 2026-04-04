import { useState, useMemo } from 'react';
import ErrorBoundary from './components/ErrorBoundary';
import AppContent from './components/AppContent';
import UIManager from './components/UIManager';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import { NotificationProvider } from './contexts/NotificationContext';
import './App.css';
import useOnboarding from './hooks/useOnboarding';
import useWebSocketEvents from './hooks/useWebSocketEvents';
import OnboardingDialog from './components/OnboardingDialog';
import Notification from './components/Notification';
import { usePageVisibility } from './hooks/usePageVisibility';
import { MAX_PERSISTED_LOGS } from './constants/app';

// Extracted custom hooks
import { useAppState } from './hooks/useAppState';
import { useSidebarState } from './hooks/useSidebarState';
import { useGitActions } from './hooks/useGitActions';
import { useChatSessions } from './hooks/useChatSessions';
import { useQueuedMessages, useQueuedMessagesAutoSend } from './hooks/useQueuedMessages';
import { useChatPersistence } from './hooks/useChatPersistence';
import { useMessageSending } from './hooks/useMessageSending';
import { useModelProviderHandlers } from './hooks/useModelProviderHandlers';
import { useAppInitialization } from './hooks/useAppInitialization';
import { useSessionRestored } from './hooks/useSessionRestored';
import { useSecurityApproval } from './hooks/useSecurityApproval';
import { useSecurityPrompt } from './hooks/useSecurityPrompt';
import SecurityApprovalDialog from './components/SecurityApprovalDialog';
import SecurityPromptDialog from './components/SecurityPromptDialog';
import { notificationBus } from './services/notificationBus';

function App() {
  // ── 1. Core state ──────────────────────────────────────────────────
  const { state, setState } = useAppState();
  const [inputValue, setInputValue] = useState('');

  // ── 2. Queued messages (must be initialised BEFORE useWebSocketEvents
  //    because the WS handler references setQueuedMessages/queuedMessagesRef) ──
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

  // ── 3. WebSocket event processing ─────────────────────────────────
  const { handleEvent, activeChatIdRef, activeRequestsRef, connectionTimeoutRef } = useWebSocketEvents({
    state,
    setState,
    setInputValue,
    setQueuedMessages,
    queuedMessagesRef,
  });

  // ── 4. Chat session management (depends on WS refs) ───────────────
  const { loadChatSessions, handleActiveChatChange, handleCreateChat, handleDeleteChat, handleRenameChat } =
    useChatSessions({ setState, activeChatIdRef, activeRequestsRef });

  // ── 5. Message sending (depends on WS refs) ───────────────────────
  const { handleSendMessage, handleStopProcessing } = useMessageSending({
    setState,
    setInputValue,
    activeChatIdRef,
    activeRequestsRef,
  });

  // ── 6. Auto-send queued messages (depends on handleSendMessage) ──
  useQueuedMessagesAutoSend(
    state,
    activeRequestsRef,
    queuedMessagesRef,
    setQueuedMessages,
    handleSendMessage,
    setState,
  );

  // ── 7. Security approval handler ───────────────────────────────────
  const { handleSecurityApprovalResponse } = useSecurityApproval(setState);

  // ── 7b. Security prompt handler ─────────────────────────────────
  const { handleSecurityPromptResponse } = useSecurityPrompt(setState);

  // ── 8. Sidebar state (independent) ────────────────────────────────
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

  // ── 8. Git actions (independent) ───────────────────────────────────
  const { gitRefreshToken, handleGitCommit, handleGitAICommit, handleGitStage, handleGitUnstage, handleGitDiscard } =
    useGitActions();

  // ── 9. Persistence (depends on state) ──────────────────────────────
  useChatPersistence(state);

  // ── 10. Derived values ─────────────────────────────────────────────
  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);

  // Keep a larger client-side log buffer available to the sidebar logs view.
  const recentLogs = useMemo(() => state.logs.slice(-MAX_PERSISTED_LOGS), [state.logs]);

  // Memoize stats to prevent unnecessary Sidebar remounts
  const stats = useMemo(
    () => ({
      queryCount: state.queryCount,
      filesModified: 0, // TODO: track modified files from buffers
    }),
    [state.queryCount],
  );

  // ── 11. Onboarding ─────────────────────────────────────────────────
  const onboardingHook = useOnboarding();

  // Adapter wrapping the hook's onComplete so that parent AppState is updated
  const onboarding = {
    ...onboardingHook,
    onComplete: () => onboardingHook.onComplete((values) => setState((prev) => ({ ...prev, ...values }))),
  };

  // Wire up browser tab freeze/resume for WebSocket connections.
  usePageVisibility();

  // ── 12. Model / provider / view change handlers ───────────────────
  const { handleModelChange, handleProviderChange, handleViewChange, handlePersonaChange } = useModelProviderHandlers({
    state,
    setState,
  });

  // ── 13. Initialisation effect (WS, stats, files, mobile) ──────────
  useAppInitialization({
    handleEvent,
    connectionTimeoutRef,
    loadChatSessions,
    setRecentFiles,
    setIsMobile,
    setState,
  });

  // ── 14. Session-restored event listener ───────────────────────────
  useSessionRestored({ setState });

  // ── 15. Render ────────────────────────────────────────────────────
  return (
    <ErrorBoundary
      onError={(error, errorInfo) => {
        console.error('Application error:', error, errorInfo);
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
            </EditorManagerProvider>
            <Notification />
          </HotkeyProvider>
        </NotificationProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}

export default App;
