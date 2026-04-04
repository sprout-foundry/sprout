import React, { useRef, useState, useEffect } from 'react';
import { Menu, PanelRightOpen, PanelRightClose } from 'lucide-react';
import Sidebar from './Sidebar';
import WorkspaceBar from './WorkspaceBar';
import Terminal from './Terminal';
import ContextPanel, { type ContextPanelHandle } from './ContextPanel';
import Status from './Status';
import CommandPalette from './CommandPalette';
import PaneLayoutManager from './PaneLayoutManager';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { ApiService } from '../services/api';
import { useGitWorkspace } from '../hooks/useGitWorkspace';
import { useInstanceManager } from '../hooks/useInstanceManager';
import { useChatSessionSync } from '../hooks/useChatSessionSync';
import { useHotkeyIntegration } from '../hooks/useHotkeyIntegration';
import { useCurrentTodos } from '../hooks/useCurrentTodos';
import { useSplitManager } from '../hooks/useSplitManager';
import { useHotkeysConfig } from '../hooks/useHotkeysConfig';
import { useFileHandlers } from '../hooks/useFileHandlers';
import { usePanelWidth } from '../hooks/usePanelWidth';
import type { ChatSession } from '../services/chatSessions';
import type { AppState, LogEntry, PerChatState } from '../types/app';

interface AppContentProps {
  state: AppState;
  inputValue: string;
  onInputChange: React.Dispatch<React.SetStateAction<string>>;
  isMobile: boolean;
  isSidebarOpen: boolean;
  sidebarCollapsed: boolean;
  isTerminalExpanded: boolean;
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
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onModelChange: (model: string) => void;
  onProviderChange: (provider: string) => void;
  onSendMessage: (message: string) => void;
  onQueueMessage: (message: string) => void;
  onStopProcessing: () => void;
  queuedMessagesCount: number;
  queuedMessages: string[];
  onQueueMessageRemove: (index: number) => void;
  onQueueMessageEdit: (index: number, newText: string) => void;
  onQueueReorder: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages: () => void;
  onGitCommit: (message: string, files: string[]) => Promise<unknown>;
  onGitAICommit: () => Promise<{ commitMessage: string; warnings?: string[] }>;
  onGitStage: (files: string[]) => Promise<void>;
  onGitUnstage: (files: string[]) => Promise<void>;
  onGitDiscard: (files: string[]) => Promise<void>;
  onTerminalExpandedChange: (expanded: boolean) => void;
  isConnected: boolean;
  chatSessions?: ChatSession[];
  activeChatId?: string | null;
  onActiveChatChange?: (id: string) => void;
  onCreateChat?: () => Promise<string | null>;
  onDeleteChat?: (id: string) => void;
  onRenameChat?: (id: string, name: string) => void;
  perChatCache?: Record<string, PerChatState>;
}

const AppContent: React.FC<AppContentProps> = ({
  state,
  inputValue,
  onInputChange,
  isMobile,
  isSidebarOpen,
  sidebarCollapsed,
  isTerminalExpanded,
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
  queuedMessages,
  onQueueMessageRemove,
  onQueueMessageEdit,
  onQueueReorder,
  onClearQueuedMessages,
  onGitCommit,
  onGitAICommit,
  onGitStage,
  onGitUnstage,
  onGitDiscard,
  onTerminalExpandedChange,
  isConnected,
  chatSessions,
  activeChatId,
  onActiveChatChange,
  onCreateChat,
  onDeleteChat: _onDeleteChat,
  onRenameChat: _onRenameChat,
  perChatCache,
}) => {
  // ── Editor manager ─────────────────────────────────────────────
  const {
    panes,
    paneLayout,
    activePaneId,
    activeBufferId,
    buffers,
    switchPane,
    switchToBuffer,
    splitPane,
    splitIntoGrid,
    closeSplit,
    closePane,
    closeBuffer,
    closeAllBuffers,
    closeOtherBuffers,
    openFile,
    openWorkspaceBuffer,
    paneSizes,
    updatePaneSize,
    updateBufferMetadata,
    updateBufferTitle,
    saveAllBuffers,
  } = useEditorManager();

  const apiService = ApiService.getInstance();

  // ── Hooks & local UI state ─────────────────────────────────────
  const currentTodos = useCurrentTodos(state.currentTodos, state.toolExecutions);

  const {
    handleSplitRequest,
    handleCloseAllSplits,
    nestedSplit,
    onNestedSplitChange,
    canSplit,
    canSplitGrid,
    canCloseSplit,
  } = useSplitManager({
    activePaneId,
    panes,
    paneLayout,
    splitPane,
    splitIntoGrid,
    closeSplit,
    closePane,
    updatePaneSize,
    switchToBuffer,
  });

  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);
  const [isContextPanelMobileOpen, setIsContextPanelMobileOpen] = useState(false);
  const { panelWidth, setPanelWidth } = usePanelWidth();

  const { instances, selectedInstancePID, isSwitchingInstance, handleInstanceChange } = useInstanceManager({
    isConnected,
    apiService,
  });

  const {
    gitStatus,
    gitBranches,
    commitMessage,
    setCommitMessage,
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
    refreshGitStatus,
  } = useGitWorkspace({
    apiService,
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

  useChatSessionSync({
    chatSessions,
    activeChatId,
    activeBufferId,
    buffers,
    onActiveChatChange,
    openWorkspaceBuffer,
    updateBufferMetadata,
    updateBufferTitle,
  });

  useHotkeysConfig({
    isConnected,
    apiService,
    openFile,
    onViewChange,
    onCloseCommandPalette: () => setIsCommandPaletteOpen(false),
  });

  // ── Hotkey integration ─────────────────────────────────────────
  useHotkeyIntegration({
    onViewChange,
    onToggleSidebar,
    onTerminalExpandedChange,
    isTerminalExpanded,
    openWorkspaceBuffer,
    activePaneId,
    activeBufferId,
    buffers,
    handleSplitRequest,
    closeBuffer,
    closeAllBuffers,
    closeOtherBuffers,
    saveAllBuffers,
    switchToBuffer,
    switchPane,
    onToggleCommandPalette: () => setIsCommandPaletteOpen((prev) => !prev),
    onOpenCommandPalette: () => setIsCommandPaletteOpen(true),
  });
  const currentBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
  const contextPanelRef = useRef<ContextPanelHandle>(null);
  const showContextSidebar = currentBuffer?.kind === 'chat';
  const initialViewSyncRef = useRef(false);

  useEffect(() => {
    if (!isMobile || !showContextSidebar) setIsContextPanelMobileOpen(false);
  }, [isMobile, showContextSidebar]);

  useEffect(() => {
    if (initialViewSyncRef.current) return;
    if (currentBuffer?.kind === 'chat' && state.currentView !== 'chat') {
      initialViewSyncRef.current = true;
      onViewChange('chat');
      return;
    }
    if (currentBuffer) {
      initialViewSyncRef.current = true;
    }
  }, [currentBuffer, onViewChange, state.currentView]);

  const { handleFileClick, handleOpenRevisionDiff } = useFileHandlers({
    onViewChange,
    openFile,
    openWorkspaceBuffer,
  });

  const containerRef = useRef<HTMLDivElement>(null);

  // ── Render ──────────────────────────────────────────────────────

  return (
    <div className="app">
      {isMobile && isSidebarOpen && <div className="mobile-overlay" onClick={onCloseSidebar} />}

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
        onProviderChange={onProviderChange}
        gitPanel={{
          gitStatus,
          gitBranches,
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
          onPreviewFile: handlePreviewGitFile,
          onStageSelected: handleStageSelected,
          onUnstageSelected: handleUnstageSelected,
          onDiscardSelected: handleDiscardSelected,
          onStageFile: handleStageFile,
          onUnstageFile: handleUnstageFile,
          onDiscardFile: handleDiscardFile,
          onSectionAction: handleSectionAction,
          apiService,
          openWorkspaceBuffer,
        }}
      />

      <div
        className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${isTerminalExpanded ? 'terminal-expanded' : ''}`}
      >
        <WorkspaceBar isConnected={state.isConnected} isMobile={isMobile} isMobileMenuOpen={isSidebarOpen} />
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
                    onClick={() => {
                      if (isContextPanelMobileOpen) {
                        contextPanelRef.current?.closePanel();
                        return;
                      }
                      contextPanelRef.current?.openTab('subagents');
                    }}
                    aria-label={isContextPanelMobileOpen ? 'Close context panel' : 'Open context panel'}
                    title={isContextPanelMobileOpen ? 'Close context panel' : 'Open context panel'}
                  >
                    {isContextPanelMobileOpen ? <PanelRightClose size={16} /> : <PanelRightOpen size={16} />}
                  </button>
                )}
              </div>
            )}

            <div className={`editor-workspace ${paneLayout}`}>
              <div ref={containerRef} className={`panes-container layout-${paneLayout}`}>
                <PaneLayoutManager
                  panes={panes}
                  paneLayout={paneLayout}
                  activePaneId={activePaneId}
                  activeBufferId={activeBufferId}
                  buffers={buffers}
                  paneSizes={paneSizes}
                  contextPanelRef={contextPanelRef}
                  perChatCache={perChatCache}
                  activeChatId={activeChatId}
                  messages={state.messages}
                  onSendMessage={onSendMessage}
                  onQueueMessage={onQueueMessage}
                  onStopProcessing={onStopProcessing}
                  queuedMessagesCount={queuedMessagesCount}
                  queuedMessages={queuedMessages}
                  onQueueMessageRemove={onQueueMessageRemove}
                  onQueueMessageEdit={onQueueMessageEdit}
                  onQueueReorder={onQueueReorder}
                  onClearQueuedMessages={onClearQueuedMessages}
                  inputValue={inputValue}
                  onInputChange={onInputChange}
                  isProcessing={state.isProcessing}
                  lastError={state.lastError}
                  toolExecutions={state.toolExecutions}
                  queryProgress={state.queryProgress}
                  currentTodos={currentTodos}
                  subagentActivities={state.subagentActivities}
                  deepReview={deepReview}
                  reviewError={reviewError}
                  reviewFixResult={reviewFixResult}
                  reviewFixLogs={reviewFixLogs}
                  reviewFixSessionID={reviewFixSessionID}
                  isReviewLoading={isReviewLoading}
                  isReviewFixing={isReviewFixing}
                  onFixFromReview={handleFixFromReview}
                  activeDiffPath={activeDiffPath}
                  activeDiff={activeDiff}
                  diffMode={diffMode}
                  isDiffLoading={isDiffLoading}
                  diffError={diffError}
                  onDiffModeChange={handleDiffModeChange}
                  switchPane={switchPane}
                  switchToBuffer={switchToBuffer}
                  updatePaneSize={updatePaneSize}
                  openWorkspaceBuffer={openWorkspaceBuffer}
                  canSplit={canSplit}
                  canSplitGrid={canSplitGrid}
                  canCloseSplit={canCloseSplit}
                  onSplitRequest={handleSplitRequest}
                  onCloseAllSplits={handleCloseAllSplits}
                  onCreateChat={onCreateChat}
                  nestedSplit={nestedSplit}
                  onNestedSplitChange={onNestedSplitChange}
                  containerRef={containerRef}
                />
              </div>
            </div>
          </div>

          {showContextSidebar && (
            <ContextPanel
              ref={contextPanelRef}
              context="chat"
              toolExecutions={state.toolExecutions}
              fileEdits={state.fileEdits}
              logs={state.logs}
              subagentActivities={state.subagentActivities}
              currentTodos={currentTodos}
              messages={state.messages}
              isProcessing={state.isProcessing}
              lastError={state.lastError}
              queryProgress={state.queryProgress}
              isMobileLayout={isMobile}
              onMobileOpenChange={setIsContextPanelMobileOpen}
              panelWidth={panelWidth}
              onPanelWidthChange={setPanelWidth}
              onOpenRevisionDiff={handleOpenRevisionDiff}
            />
          )}
        </div>
        <Status isConnected={state.isConnected} position="bottom" stats={state.stats} />
      </div>

      <Terminal isExpanded={isTerminalExpanded} onToggleExpand={onTerminalExpandedChange} />

      <CommandPalette
        isOpen={isCommandPaletteOpen}
        onClose={() => setIsCommandPaletteOpen(false)}
        onOpenFile={handleFileClick}
        onToggleSidebar={onSidebarToggle}
        onToggleTerminal={() => onTerminalExpandedChange(!isTerminalExpanded)}
        onOpenHotkeysConfig={() => {
          window.dispatchEvent(new CustomEvent('ledit:open-hotkeys-config'));
        }}
      />
    </div>
  );
};

export default AppContent;
