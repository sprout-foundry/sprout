import type { TodoItem, LogEntry } from '@sprout/ui';
import { Menu, PanelRightClose } from 'lucide-react';
import React, { useCallback, useEffect, useRef, useState, useMemo } from 'react';
import { supportsLocalTerminal } from '../config/mode';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useNotifications } from '../contexts/NotificationContext';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import { useActiveChatTab } from '../hooks/useActiveChatTab';
import { useAppContentHotkeys } from '../hooks/useAppContentHotkeys';
import { useChatSessionsSync } from '../hooks/useChatSessionsSync';
import { useCurrentTodos } from '../hooks/useCurrentTodos';
import { useFileHandler } from '../hooks/useFileHandler';
import { useGitWorkspace } from '../hooks/useGitWorkspace';
import { useHotkeysConfig } from '../hooks/useHotkeysConfig';
import { useInstances } from '../hooks/useInstances';
import { type SectionTab } from '../hooks/useSidebarState';
import { ApiService } from '../services/api';
import type { ChatSession } from '../services/chatSessions';
import type { AppState, PerChatState } from '../types/app';
import { useAppStoreSetState } from '../contexts/AppStore';
import CommandPalette, { type PaletteMode } from './CommandPalette';
import type { ContextPanelHandle } from './contextPanel/types';
import ContextSidebar from './ContextSidebar';
import EditorWorkspace from './EditorWorkspace';
import ErrorBoundary from './ErrorBoundary';
import HeaderBar from './HeaderBar';
import NotificationCenter from './NotificationCenter';
import Sidebar from './Sidebar';
import Status from './Status';
import StatusBar from './StatusBar';
import Terminal from './Terminal';

interface AppContentProps {
  state: AppState;
  inputValue: string;
  onInputChange: React.Dispatch<React.SetStateAction<string>>;
  isMobile: boolean;
  isTablet: boolean;
  isSidebarOpen: boolean;
  sidebarCollapsed: boolean;
  isTerminalExpanded: boolean;
  selectedSection: SectionTab;
  sidebarWidth: number;
  sidebarWidthRef: React.MutableRefObject<number>;
  onSectionChange: (section: SectionTab) => void;
  onSidebarWidthChange: (width: number) => void;
  onSidebarWidthPersist: () => void;
  onSidebarWidthReset: () => void;
  stats: {
    queryCount: number;
    filesModified: number;
  };
  recentFiles: Array<{ path: string; modified: boolean }>;
  recentLogs: LogEntry[];
  gitRefreshToken: number;
  onSidebarToggle: () => void;
  onToggleSidebar: () => void;
  onCloseSidebar: () => void;
  onViewChange: (view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team') => void;
  onModelChange: (model: string) => void;
  onProviderChange: (provider: string) => void;
  onSendMessage: (message: string) => void;
  onQueueMessage: (message: string) => void;
  onStopProcessing: () => void;
  queuedMessagesCount: number;
  onGitCommit: (message: string, files: string[]) => Promise<unknown>;
  onGitAICommit: () => Promise<{ commitMessage: string; warnings?: string[] }>;
  onGitStage: (files: string[]) => Promise<void>;
  onGitUnstage: (files: string[]) => Promise<void>;
  onGitDiscard: (files: string[]) => Promise<void>;
  onTerminalExpandedChange: (expanded: boolean) => void;
  isConnected: boolean;
  backendReachable?: boolean;
  onRetryConnection?: () => void;
  chatSessions?: ChatSession[];
  activeChatId: string | null;
  perChatCache?: Record<string, PerChatState>;
  onActiveChatChange?: (id: string) => void;
  onTerminalOutput?: (output: string) => void;
  onCreateChat?: () => Promise<string | null>;
  onDeleteChat?: (id: string) => void;
  onRenameChat?: (id: string, name: string) => void;
}

const AppContent: React.FC<AppContentProps> = ({
  state,
  inputValue,
  onInputChange,
  isMobile,
  isTablet,
  isSidebarOpen,
  sidebarCollapsed,
  isTerminalExpanded,
  selectedSection,
  sidebarWidth,
  sidebarWidthRef,
  onSectionChange,
  onSidebarWidthChange,
  onSidebarWidthPersist,
  onSidebarWidthReset,
  stats,
  recentFiles,
  recentLogs,
  gitRefreshToken,
  onSidebarToggle,
  onToggleSidebar,
  onCloseSidebar,
  onViewChange,
  onModelChange,
  onProviderChange,
  onSendMessage,
  onQueueMessage,
  onStopProcessing,
  queuedMessagesCount,
  onGitCommit,
  onGitAICommit,
  onGitStage,
  onGitUnstage,
  onGitDiscard,
  onTerminalExpandedChange,
  isConnected,
  backendReachable,
  onRetryConnection,
  chatSessions,
  activeChatId,
  perChatCache,
  onActiveChatChange,
  onCreateChat,
  onDeleteChat,
  onRenameChat,
}) => {
  const {
    buffers,
    activeBufferId,
    openFile,
    openWorkspaceBuffer,
    updateBufferTitle,
    updateBufferMetadata,
    closeBuffer,
  } = useEditorManager();
  const apiService = ApiService.getInstance();
  const sproutFetch = useSproutFetch();
  const { notifications } = useNotifications();
  const currentTodos = useCurrentTodos(state.currentTodos, state.toolExecutions);
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);
  const [commandPaletteMode, setCommandPaletteMode] = useState<PaletteMode>('all');
  const [isNotificationCenterOpen, setIsNotificationCenterOpen] = useState(false);
  const notificationBellRef = useRef<HTMLDivElement>(null);

  const setAppState = useAppStoreSetState();
  // Opens the ModelSelectionModal for the currently active provider when
  // the user clicks the model name in the status bar. The modal handles
  // the actual swap via the existing handleModelSelectionResponse path.
  //
  // Editor-only mode ("editor" is the sentinel used by
  // pkg/webui/provider_check.go for "no LLM, editor only") gets routed to
  // the provider setup flow instead — opening ModelSelectionModal for
  // "editor" would fail at the /api/providers/models?provider=editor
  // fetch since there's no such provider on the backend.
  const handleStatusBarModelClick = useCallback(
    (provider: string) => {
      const p = provider || state.provider || '';
      if (!p || p === 'editor') {
        window.dispatchEvent(
          new CustomEvent('sprout:open-settings-focus', { detail: { focus: 'provider' } }),
        );
        return;
      }
      // useAppStoreSetState takes an updater that returns a *partial*
      // AppState (the store merges it in) — not the full spread we'd use
      // with React's setState.
      setAppState(() => ({ modelSelectionRequest: { provider: p } }));
    },
    [setAppState, state.provider],
  );
  const hotkeysConfigPath = useHotkeysConfig(apiService, isConnected);
  const {
    instances,
    selectedInstancePID,
    isSwitchingInstance,
    onInstanceChange: handleInstanceChange,
  } = useInstances({ apiService, isConnected });
  const buffersRef = useRef(buffers);
  buffersRef.current = buffers;

  const initialViewSyncRef = useRef(false);

  useChatSessionsSync({
    chatSessions,
    activeChatId,
    buffersRef,
    updateBufferTitle,
    updateBufferMetadata,
    openWorkspaceBuffer,
  });
  useActiveChatTab({ activeBufferId, buffersRef, activeChatId, onActiveChatChange });

  const handlePrimaryViewChange = useCallback(
    (view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team') => {
      if (view === 'chat') {
        openWorkspaceBuffer({
          kind: 'chat',
          path: '__workspace/chat',
          title: 'Chat',
          ext: '.chat',
          isPinned: true,
          isClosable: false,
        });
      }
      onViewChange(view);
    },
    [onViewChange, openWorkspaceBuffer],
  );

  const { handleFileClick } = useFileHandler({ onViewChange, openFile });

  const handleOutlineNavigateToSymbol = useCallback((line: number) => {
    document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
  }, []);

  const currentBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
  const contextPanelRef = useRef<ContextPanelHandle>(null);
  const showContextSidebar = currentBuffer?.kind === 'chat';

  useEffect(() => {
    if (initialViewSyncRef.current) {
      return;
    }
    if (currentBuffer?.kind === 'chat' && state.currentView !== 'chat') {
      initialViewSyncRef.current = true;
      onViewChange('chat');
      return;
    }
    if (currentBuffer) {
      initialViewSyncRef.current = true;
    }
  }, [currentBuffer, onViewChange, state.currentView]);

  const handleToggleContextPanel = () => {
    if (!contextPanelRef.current) return;
    window.dispatchEvent(new CustomEvent('toggle-context-panel'));
  };

  const handleOpenRevisionDiff = useCallback(
    (options: { path: string; diff: string; title: string }) => {
      onViewChange('editor');
      openWorkspaceBuffer({
        kind: 'diff',
        path: `__workspace/revision/${options.path}-${Date.now()}`,
        title: `${options.title}: ${options.path.split('/').pop() || options.path}`,
        ext: '.diff',
        metadata: {
          sourcePath: options.path,
          diff: {
            message: 'success',
            path: options.path,
            has_staged: false,
            has_unstaged: false,
            staged_diff: '',
            unstaged_diff: '',
            diff: options.diff,
          },
          diffMode: 'combined',
          modeOptions: ['combined'],
          title: options.title,
        },
      });
    },
    [onViewChange, openWorkspaceBuffer],
  );

  const { handleOpenHotkeysConfig } = useAppContentHotkeys({
    activeBufferId,
    buffersRef,
    onSidebarToggle,
    onTerminalExpandedChange,
    isTerminalExpanded,
    openWorkspaceBuffer,
    onViewChange,
    handlePrimaryViewChange,
    closeBuffer,
    setCommandPaletteMode,
    setIsCommandPaletteOpen,
    hotkeysConfigPath,
    openFile,
  });

  const {
    gitStatus,
    gitBranches,
    workspaceRoot,
    selectedFiles,
    activeDiffSelectionKey,
    activeDiffPath,
    activeDiff,
    diffMode,
    isDiffLoading,
    diffError,
    isGitLoading,
    isGitActing,
    isGeneratingCommitMessage,
    gitActionError,
    gitActionWarning,
    isReviewLoading,
    isReviewFixing,
    reviewError,
    reviewFixResult,
    reviewFixLogs,
    reviewFixSessionID,
    deepReview,
    handleToggleFileSelection,
    handleToggleSectionSelection,
    clearSelectedFiles,
    handleSelectFiles,
    handlePreviewGitFile,
    handleStageSelected,
    handleUnstageSelected,
    handleDiscardSelected,
    handleStageFile,
    handleUnstageFile,
    handleDiscardFile,
    handleSectionAction,
    handleGitCommitClick,
    handleGenerateCommitMessage,
    handleRunReview,
    handleFixFromReview,
    handleDiffModeChange,
    handleCheckoutBranch,
    handleCreateBranch,
    handlePull,
    handlePush,
    handleLoadCommits,
    handleLoadCommitDetail,
    handleLoadCommitFileDiff,
    handleCheckoutCommit,
    handleRevertCommit,
    refreshGitStatus,
    commitMessage,
    setCommitMessage,
  } = useGitWorkspace({
    fetchFn: sproutFetch,
    gitRefreshToken,
    selectedGitFilePath: null,
    onViewChange,
    onGitCommit,
    onGitAICommit,
    onGitStage,
    onGitUnstage,
    onGitDiscard,
    openWorkspaceBuffer,
  });

  const handleToolPillClick = useCallback((toolId: string) => contextPanelRef.current?.highlightTool(toolId), []);

  const chatProps = useMemo(
    () => ({
      messages: state.messages,
      onSendMessage,
      onQueueMessage,
      queuedMessagesCount,
      inputValue,
      onInputChange,
      isProcessing: state.isProcessing,
      lastError: state.lastError,
      toolExecutions: state.toolExecutions,
      queryProgress: state.queryProgress,
      currentTodos,
      onStopProcessing,
      onToolPillClick: handleToolPillClick,
      stats: state.stats,
      isConnected: state.isConnected,
      backendReachable,
      onRetryConnection,
    }),
    [
      state.messages,
      onSendMessage,
      onQueueMessage,
      queuedMessagesCount,
      inputValue,
      onInputChange,
      state.isProcessing,
      state.lastError,
      state.toolExecutions,
      state.queryProgress,
      currentTodos,
      onStopProcessing,
      handleToolPillClick,
      state.stats,
      state.isConnected,
      backendReachable,
      onRetryConnection,
    ],
  );
  const reviewProps = useMemo(
    () => ({
      review: deepReview,
      reviewError,
      reviewFixResult,
      reviewFixLogs,
      reviewFixSessionID,
      isReviewLoading,
      isReviewFixing,
      onFixFromReview: handleFixFromReview,
    }),
    [
      deepReview,
      reviewError,
      reviewFixResult,
      reviewFixLogs,
      reviewFixSessionID,
      isReviewLoading,
      isReviewFixing,
      handleFixFromReview,
    ],
  );
  const diffState = useMemo(
    () => ({
      activeDiffPath,
      activeDiff,
      diffMode,
      isDiffLoading,
      diffError,
      onDiffModeChange: handleDiffModeChange,
    }),
    [activeDiffPath, activeDiff, diffMode, isDiffLoading, diffError, handleDiffModeChange],
  );

  return (
    <div className="app">
      {isMobile && isSidebarOpen && <div className="mobile-overlay" onClick={onCloseSidebar} />}
      <ErrorBoundary panelName="Sidebar">
        <Sidebar
          isConnected={state.isConnected}
          instances={instances}
          selectedInstancePID={selectedInstancePID}
          isSwitchingInstance={isSwitchingInstance}
          onInstanceChange={handleInstanceChange}
          provider={state.provider}
          model={state.model}
          selectedModel={state.model}
          onModelChange={onModelChange}
          currentView={state.currentView}
          onViewChange={onViewChange}
          onFileClick={handleFileClick}
          stats={stats}
          recentFiles={recentFiles}
          recentLogs={recentLogs}
          isMobileMenuOpen={isSidebarOpen}
          onMobileMenuToggle={onToggleSidebar}
          isMobile={isMobile}
          sidebarCollapsed={sidebarCollapsed}
          onSidebarToggle={onSidebarToggle}
          selectedSection={selectedSection}
          onSectionChange={onSectionChange}
          sidebarWidth={sidebarWidth}
          sidebarWidthRef={sidebarWidthRef}
          onSidebarWidthChange={onSidebarWidthChange}
          onSidebarWidthPersist={onSidebarWidthPersist}
          onSidebarWidthReset={onSidebarWidthReset}
          onProviderChange={onProviderChange}
          gitPanel={{
            gitStatus,
            gitBranches,
            workspaceRoot,
            selectedFiles,
            activeDiffSelectionKey,
            commitMessage,
            isLoading: isGitLoading,
            isActing: isGitActing,
            isGeneratingCommitMessage,
            isReviewLoading,
            actionError: gitActionError,
            actionWarning: gitActionWarning,
            onCommitMessageChange: setCommitMessage,
            onGenerateCommitMessage: handleGenerateCommitMessage,
            onCommit: handleGitCommitClick,
            onRunReview: handleRunReview,
            onCheckoutBranch: handleCheckoutBranch,
            onCreateBranch: handleCreateBranch,
            onPull: handlePull,
            onPush: handlePush,
            onRefresh: refreshGitStatus,
            onToggleFileSelection: handleToggleFileSelection,
            onToggleSectionSelection: handleToggleSectionSelection,
            onClearSelection: clearSelectedFiles,
            onSelectFiles: handleSelectFiles,
            onPreviewFile: handlePreviewGitFile,
            onStageSelected: handleStageSelected,
            onUnstageSelected: handleUnstageSelected,
            onDiscardSelected: handleDiscardSelected,
            onStageFile: handleStageFile,
            onUnstageFile: handleUnstageFile,
            onDiscardFile: handleDiscardFile,
            onSectionAction: handleSectionAction,
            onOpenFile: handleFileClick,
            onLoadCommits: handleLoadCommits,
            onLoadCommitDetail: handleLoadCommitDetail,
            onLoadCommitFileDiff: handleLoadCommitFileDiff,
            onCheckoutCommit: handleCheckoutCommit,
            onRevertCommit: handleRevertCommit,
            openWorkspaceBuffer,
          }}
        />
      </ErrorBoundary>
      <div
        className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${supportsLocalTerminal && isTerminalExpanded ? 'terminal-expanded' : ''}`}
      >
        <HeaderBar
          isMobile={isMobile}
          isSidebarOpen={isSidebarOpen}
          showContextSidebar={showContextSidebar}
          isConnected={state.isConnected}
          onToggleSidebar={onToggleSidebar}
          onToggleContextPanel={handleToggleContextPanel}
        />
        <div className="main-view-content">
          <div className="editor-view">
            {isMobile && (
              <div className="pane-controls pane-controls-mobile">
                <button
                  className="top-mobile-menu-btn"
                  onClick={onToggleSidebar}
                  aria-label={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
                  title={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
                >
                  <Menu size={16} />
                </button>
                {showContextSidebar && (
                  <button
                    className="top-mobile-context-btn"
                    onClick={handleToggleContextPanel}
                    aria-label="Toggle context panel"
                    title="Toggle context panel"
                  >
                    <PanelRightClose size={16} />
                  </button>
                )}
              </div>
            )}
            <ErrorBoundary panelName="Editor">
              <EditorWorkspace
                currentView={state.currentView}
                perChatCache={perChatCache}
                activeChatId={activeChatId}
                onCreateChat={onCreateChat}
                chatProps={chatProps}
                reviewProps={reviewProps}
                diffState={diffState}
                handleOutlineNavigateToSymbol={handleOutlineNavigateToSymbol}
              />
            </ErrorBoundary>
          </div>
          <ContextSidebar
            isMobile={isMobile}
            isTablet={isTablet}
            showContextSidebar={showContextSidebar}
            contextPanelRef={contextPanelRef}
            currentView={state.currentView}
            toolExecutions={state.toolExecutions}
            fileEdits={state.fileEdits}
            logs={state.logs}
            subagentActivities={state.subagentActivities}
            currentTodos={currentTodos}
            messages={state.messages}
            isProcessing={state.isProcessing}
            lastError={state.lastError}
            queryProgress={state.queryProgress}
            onOpenRevisionDiff={handleOpenRevisionDiff}
          />
        </div>
        <Status isConnected={state.isConnected} stats={state.stats} />
        <StatusBar
          branch={gitBranches.current || gitStatus?.branch}
          buffer={
            currentBuffer
              ? {
                  kind: currentBuffer.kind,
                  file: currentBuffer.file,
                  content: currentBuffer.content,
                  cursorPosition: currentBuffer.cursorPosition,
                  languageOverride: currentBuffer.languageOverride,
                }
              : null
          }
          notificationCount={notifications.length}
          onToggleNotificationCenter={() => setIsNotificationCenterOpen((prev) => !prev)}
          notificationCenterRef={notificationBellRef}
          chatStats={state.stats}
          onModelClick={handleStatusBarModelClick}
        />
        <NotificationCenter
          isOpen={isNotificationCenterOpen}
          onClose={() => setIsNotificationCenterOpen(false)}
          positionRef={notificationBellRef}
        />
      </div>
      {supportsLocalTerminal && (
        <ErrorBoundary panelName="Terminal">
          <Terminal isExpanded={isTerminalExpanded} onToggleExpand={onTerminalExpandedChange} />
        </ErrorBoundary>
      )}
      <CommandPalette
        isOpen={isCommandPaletteOpen}
        onClose={() => setIsCommandPaletteOpen(false)}
        onOpenFile={(filePath) => {
          const fileName = filePath.split('/').filter(Boolean).pop() || filePath;
          const extensionIndex = fileName.lastIndexOf('.');
          const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
          openFile({ path: filePath, name: fileName, isDir: false, size: 0, modified: 0, ext: fileExt });
        }}
        onToggleSidebar={onSidebarToggle}
        onToggleTerminal={supportsLocalTerminal ? () => onTerminalExpandedChange(!isTerminalExpanded) : () => {}}
        onOpenHotkeysConfig={handleOpenHotkeysConfig}
        initialMode={commandPaletteMode}
        onNavigateToLine={(line) => {
          document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
        }}
        activeBufferContent={currentBuffer?.content}
        activeBufferFileExtension={currentBuffer?.file?.ext}
      />
    </div>
  );
};

export default React.memo(AppContent);
