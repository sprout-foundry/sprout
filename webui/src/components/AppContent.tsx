import { useRef, useState, useEffect, useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { Menu, PanelRightOpen, PanelRightClose } from 'lucide-react';
import Sidebar from './Sidebar';
import WorkspaceBar from './WorkspaceBar';
import MenuBar from './MenuBar';
import Terminal from './Terminal';
import ContextPanel, { type ContextPanelHandle } from './ContextPanel';
import Status from './Status';
import StatusBar from './StatusBar';
import CommandPalette from './CommandPalette';
import PaneLayoutManager from './PaneLayoutManager';
import { WorktreeChatDialog } from './WorktreeChatDialog';
import WorktreePickerDialog from './WorktreePickerDialog';
import ChatTabBar from './ChatTabBar';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { ApiService } from '../services/api';
import { deleteChatSession } from '../services/chatSessions';
import { useGitWorkspace } from '../hooks/useGitWorkspace';
import { useInstanceManager } from '../hooks/useInstanceManager';
import { useChatSessionSync } from '../hooks/useChatSessionSync';
import { useHotkeyIntegration } from '../hooks/useHotkeyIntegration';
import { useCurrentTodos } from '../hooks/useCurrentTodos';
import { useSplitManager } from '../hooks/useSplitManager';
import { useHotkeysConfig } from '../hooks/useHotkeysConfig';
import { useFileHandlers } from '../hooks/useFileHandlers';
import { usePanelWidth } from '../hooks/usePanelWidth';
import { useFileDropZone } from '../hooks/useFileDropZone';
import FileDropOverlay from './FileDropOverlay';
import { parseFilePath } from '../utils/filePath';
import { isBinaryFile } from '../utils/binaryPatterns';
import { notificationBus } from '../services/notificationBus';
import type { ChatSession } from '../services/chatSessions';
import { setChatSessionWorktree } from '../services/chatSessions';
import type { AppState, LogEntry, PerChatState } from '../types/app';

interface AppContentProps {
  state: AppState;
  inputValue: string;
  onInputChange: Dispatch<SetStateAction<string>>;
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
  onPersonaChange?: (persona: string) => void;
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
  onCreateChatInWorktree?: (branch: string, baseRef?: string, name?: string, autoSwitch?: boolean) => Promise<string | null>;
}

function AppContent({
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
  onPersonaChange,
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
  onCreateChatInWorktree,
}: AppContentProps): JSX.Element {
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
    toggleBufferPin,
    setBufferPinned,
    setBufferClosable,
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
    setBufferPinned,
    setBufferClosable,
  });

  const [worktreeDialogOpen, setWorktreeDialogOpen] = useState(false);
  const [worktreeCreating, setWorktreeCreating] = useState(false);
  const [worktreeError, setWorktreeError] = useState<string | null>(null);

  const [worktreePickerOpen, setWorktreePickerOpen] = useState(false);
  const [worktreePickerSessionId, setWorktreePickerSessionId] = useState<string | null>(null);

  const handleWorktreeSubmit = useCallback(async (params: {
    branch: string;
    baseRef: string;
    name: string;
    autoSwitch: boolean;
  }) => {
    setWorktreeCreating(true);
    setWorktreeError(null);
    try {
      const chatId = await onCreateChatInWorktree?.(
        params.branch,
        params.baseRef || undefined,
        params.name || undefined,
        params.autoSwitch,
      );
      if (chatId) {
        openWorkspaceBuffer({
          kind: 'chat',
          path: `__workspace/chat/${chatId}`,
          title: 'Worktree Chat',
          isPinned: false,
          isClosable: true,
          metadata: { chatId },
        });
      }
      setWorktreeDialogOpen(false);
    } catch (err) {
      setWorktreeError(err instanceof Error ? err.message : 'Failed to create chat in worktree');
    } finally {
      setWorktreeCreating(false);
    }
  }, [onCreateChatInWorktree, openWorkspaceBuffer]);

  const handleSetWorktree = useCallback((sessionId: string) => {
    setWorktreePickerSessionId(sessionId);
    setWorktreePickerOpen(true);
  }, []);

  const handleClearWorktree = useCallback(async (sessionId: string) => {
    try {
      await setChatSessionWorktree(sessionId, '');
    } catch (err) {
      console.warn('[AppContent] Failed to clear worktree:', err);
    }
  }, []);

  const handleDeleteChatWithWorktree = useCallback(async (id: string) => {
    try {
      await deleteChatSession(id, true);
      await _onDeleteChat?.(id);
    } catch (err) {
      console.warn('[AppContent] Failed to delete chat with worktree:', err);
    }
  }, [_onDeleteChat]);

  const handleWorktreePickerSelect = useCallback(async (worktreePath: string, _branch: string) => {
    if (!worktreePickerSessionId) return;
    try {
      await setChatSessionWorktree(worktreePickerSessionId, worktreePath);
    } catch (err) {
      console.warn('[AppContent] Failed to set worktree for chat:', err);
    } finally {
      setWorktreePickerOpen(false);
      setWorktreePickerSessionId(null);
    }
  }, [worktreePickerSessionId]);

  const handleWorktreePickerClose = useCallback(() => {
    setWorktreePickerOpen(false);
    setWorktreePickerSessionId(null);
  }, []);

  // Compute worktree paths already assigned to other chat sessions
  const assignedWorktreePaths = (chatSessions ?? [])
    .filter((s) => s.worktree_path)
    .filter((s) => s.id !== worktreePickerSessionId)
    .map((s) => s.worktree_path!);

  useHotkeysConfig({
    isConnected,
    apiService,
    openFile,
    onViewChange,
    onCloseCommandPalette: () => setIsCommandPaletteOpen(false),
  });

  // ── Hotkey integration ─────────────────────────────────────────
  const onOpenCommandPalette = () => setIsCommandPaletteOpen(true);

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
    toggleBufferPin,
    onToggleCommandPalette: () => setIsCommandPaletteOpen((prev) => !prev),
    onOpenCommandPalette,
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

  // ── File drag-and-drop from OS ──────────────────────────────────
  const handleFilesDropped = useCallback(
    async (files: File[]) => {
      onViewChange('editor');

      let skippedCount = 0;
      let failedCount = 0;

      for (const file of files) {
        try {
          if (isBinaryFile(file.name)) {
            skippedCount++;
            continue;
          }
          const content = await file.text();
          const { fileName, fileExt } = parseFilePath(file.name);
          // Sanitize filename: strip path separators and leading dots to prevent
        // path traversal or unexpected behavior in the workspace buffer path.
        const safeName = file.name.replace(/[\\/]/g, '_').replace(/^\.+/, '_');
        openWorkspaceBuffer({
            kind: 'file',
            path: `__workspace/dropped/${safeName}-${Date.now()}`,
            title: `${fileName} (dropped)`,
            content,
            ext: fileExt || undefined,
            isPinned: false,
            isClosable: true,
            metadata: { sourceKind: 'dropped', originalName: file.name },
          });
        } catch (err) {
          failedCount++;
          console.warn('[AppContent] Failed to read dropped file:', file.name, err);
        }
      }

      // Show user notification if any files were skipped or failed
      if (skippedCount > 0 || failedCount > 0) {
        const skippedMsg = skippedCount > 0 ? `${skippedCount} binary file${skippedCount > 1 ? 's' : ''} skipped` : '';
        const failedMsg = failedCount > 0 ? `${failedCount} file${failedCount > 1 ? 's' : ''} failed to open` : '';
        const combinedMsg = [skippedMsg, failedMsg].filter(Boolean).join(', ');
        if (combinedMsg) {
          notificationBus.notify(
            'warning',
            'File Drop',
            combinedMsg,
            5000,
          );
        }
      }
    },
    [onViewChange, openWorkspaceBuffer],
  );

  const { isDragging } = useFileDropZone({ containerRef, onFilesDropped: handleFilesDropped });

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
        onPersonaChange={onPersonaChange}
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
          onOpenFile: handleFileClick,
          apiService,
          openWorkspaceBuffer,
        }}
      />

      <div
        className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${isTerminalExpanded ? 'terminal-expanded' : ''}`}
      >
        {!isMobile && <MenuBar />}
        <WorkspaceBar isConnected={state.isConnected} isMobile={isMobile} isMobileMenuOpen={isSidebarOpen} />
        {chatSessions && chatSessions.length > 0 && (
          <ChatTabBar
            sessions={chatSessions}
            activeChatId={activeChatId || ''}
            onSwitch={(id) => onActiveChatChange?.(id)}
            onCreate={() => onCreateChat?.()}
            onDelete={(id) => _onDeleteChat?.(id)}
            onRename={(id, name) => _onRenameChat?.(id, name)}
            onCreateChatInWorktree={() => setWorktreeDialogOpen(true)}
            onSetWorktree={handleSetWorktree}
            onClearWorktree={handleClearWorktree}
            onDeleteWithWorktree={handleDeleteChatWithWorktree}
          />
        )}
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
              <div ref={containerRef} className={`panes-container layout-${paneLayout}`} style={{ position: 'relative' }}>
                <FileDropOverlay visible={isDragging} />
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
                  chatSessions={chatSessions}
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
                  onOpenCommandPalette={onOpenCommandPalette}
                  onOpenTerminal={() => {
                    onTerminalExpandedChange(true);
                    onViewChange('editor');
                  }}
                  onViewGit={() => onViewChange('git')}
                  onStartChat={() => {
                    switchToBuffer('buffer-chat');
                    onViewChange('chat');
                  }}
                  canSplit={canSplit}
                  canSplitGrid={canSplitGrid}
                  canCloseSplit={canCloseSplit}
                  onSplitRequest={handleSplitRequest}
                  onCloseAllSplits={handleCloseAllSplits}
                  onCreateChat={onCreateChat}
                  onCreateChatInWorktree={() => setWorktreeDialogOpen(true)}
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
        <StatusBar 
          branch={gitBranches.current || gitStatus?.branch}
          buffer={currentBuffer ? {
            kind: currentBuffer.kind,
            file: currentBuffer.file,
            content: currentBuffer.content,
            cursorPosition: currentBuffer.cursorPosition,
            languageOverride: currentBuffer.languageOverride,
          } : null}
        />
        <Status isConnected={state.isConnected} position="bottom" stats={state.stats} />
      </div>

      <Terminal isExpanded={isTerminalExpanded} onToggleExpand={onTerminalExpandedChange} />

      <WorktreeChatDialog
        isOpen={worktreeDialogOpen}
        onClose={() => {
          setWorktreeDialogOpen(false);
          setWorktreeError(null);
        }}
        onSubmit={handleWorktreeSubmit}
        isCreating={worktreeCreating}
        error={worktreeError}
      />

      <WorktreePickerDialog
        isOpen={worktreePickerOpen}
        onClose={handleWorktreePickerClose}
        onSelect={handleWorktreePickerSelect}
        disabledPaths={assignedWorktreePaths}
      />

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
}

export default AppContent;
