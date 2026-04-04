import React, { useCallback, useRef, useState, useEffect, useMemo } from 'react';
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
import { useHotkeyCommandHandler } from '../hooks/useHotkeyCommandHandler';
import type { ChatSession } from '../services/chatSessions';
import type { AppState, LogEntry, PerChatState } from '../types/app';

// ── Props interface ────────────────────────────────────────────────

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

// ── Component ──────────────────────────────────────────────────────

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
  onDeleteChat,
  onRenameChat,
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

  // ── Current todos memo ─────────────────────────────────────────
  const currentTodos = useMemo(() => {
    if (state.currentTodos && state.currentTodos.length > 0) {
      return state.currentTodos;
    }

    const todoWrites = state.toolExecutions
      .filter(t => t.tool === 'TodoWrite')
      .sort((a, b) => b.startTime.getTime() - a.startTime.getTime());

    if (todoWrites.length === 0) return [];

    const latest = todoWrites[0];
    try {
      if (latest.arguments) {
        const args = JSON.parse(latest.arguments);
        if (Array.isArray(args.todos)) {
          return args.todos.map((todo: any) => ({
            id: todo.id || `${todo.content}-${todo.status}`,
            content: todo.content || '',
            status: (['pending', 'in_progress', 'completed', 'cancelled'].includes(todo.status) ? todo.status : 'pending') as 'pending' | 'in_progress' | 'completed' | 'cancelled'
          }));
        }
      }
    } catch { /* ignore */ }

    return [];
  }, [state.currentTodos, state.toolExecutions]);

  // ── Local UI state ─────────────────────────────────────────────
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);
  const [isContextPanelMobileOpen, setIsContextPanelMobileOpen] = useState(false);
  const [hotkeysConfigPath, setHotkeysConfigPath] = useState<string | null>(null);
  const [nestedSplit, setNestedSplit] = useState<{ hostPaneId: string; nestedPaneId: string; direction: 'vertical' | 'horizontal' } | null>(null);
  const [panelWidth, setPanelWidth] = useState(() => {
    if (typeof window === 'undefined') return 360;
    const storedWidth = Number(window.localStorage.getItem('ledit.contextPanel.width'));
    if (Number.isFinite(storedWidth) && storedWidth >= 260 && storedWidth <= 600) {
      return storedWidth;
    }
    return 360;
  });

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem('ledit.contextPanel.width', String(Math.round(panelWidth)));
  }, [panelWidth]);

  // ── Extracted hooks ────────────────────────────────────────────

  const { instances, selectedInstancePID, isSwitchingInstance, handleInstanceChange } =
    useInstanceManager({ isConnected, apiService });

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

  // ── View / tab / split callbacks ───────────────────────────────

  const initialViewSyncRef = useRef(false);

  const handlePrimaryViewChange = useCallback((view: 'chat' | 'editor' | 'git') => {
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
  }, [onViewChange, openWorkspaceBuffer]);

  const focusTabIndex = useCallback((index: number) => {
    if (!activePaneId || index < 0) return;
    const paneBuffers = Array.from(buffers.values()).filter((buffer) => buffer.paneId === activePaneId);
    const target = paneBuffers[index];
    if (target) {
      switchPane(activePaneId);
      switchToBuffer(target.id);
    }
  }, [activePaneId, buffers, switchPane, switchToBuffer]);

  const handleSplitRequest = useCallback((direction: 'vertical' | 'horizontal' | 'grid') => {
    if (direction === 'grid') {
      if (paneLayout === 'split-grid' && panes.length === 4) {
        const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
        if (primaryPane) {
          const bufId = primaryPane.bufferId;
          closeSplit();
          if (bufId) switchToBuffer(bufId);
        }
        return;
      }
      const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
      const bufId = primaryPane?.bufferId;
      splitIntoGrid();
      if (bufId) switchToBuffer(bufId);
      return;
    }

    if (!activePaneId) return;

    const previousPaneCount = panes.length;
    const newPaneId = splitPane(activePaneId, direction);
    if (!newPaneId) return;

    if (previousPaneCount === 2) {
      setNestedSplit({
        hostPaneId: activePaneId,
        nestedPaneId: newPaneId,
        direction,
      });
      updatePaneSize(`group:${activePaneId}`, 50);
      updatePaneSize(`nested:${activePaneId}`, 50);
    }
  }, [activePaneId, panes, paneLayout, splitPane, splitIntoGrid, closeSplit, updatePaneSize, switchToBuffer]);

  const handleCloseAllSplits = useCallback(() => {
    if (paneLayout === 'split-grid' && panes.length === 4) {
      const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
      const bufId = primaryPane?.bufferId;
      closeSplit();
      if (bufId) switchToBuffer(bufId);
      return;
    }
    if (nestedSplit) {
      closePane(nestedSplit.nestedPaneId);
      setNestedSplit(null);
    } else {
      closeSplit();
    }
  }, [closeSplit, closePane, nestedSplit, paneLayout, panes, switchToBuffer]);

  const canSplit = panes.length < 3;
  const canSplitGrid = paneLayout !== 'split-grid';
  const canCloseSplit = panes.length > 1;

  useEffect(() => {
    if (panes.length < 3 && nestedSplit) {
      setNestedSplit(null);
    }
  }, [nestedSplit, panes.length]);

  // ── Hotkey handler ─────────────────────────────────────────────

  useHotkeyCommandHandler({
    onToggleCommandPalette: () => setIsCommandPaletteOpen(prev => !prev),
    onOpenCommandPalette: () => setIsCommandPaletteOpen(true),
    onNewFile: () => {
      openWorkspaceBuffer({ kind: 'file', path: `__workspace/untitled-${Date.now()}`, title: 'Untitled', ext: '', isClosable: true });
      onViewChange('editor');
    },
    onToggleSidebar,
    onToggleTerminal: () => onTerminalExpandedChange(!isTerminalExpanded),
    onPrimaryViewChange: handlePrimaryViewChange,
    onFocusTabIndex: focusTabIndex,
    onSplitRequest: handleSplitRequest,
    onCloseBuffer: () => { if (activeBufferId) closeBuffer(activeBufferId); },
    onCloseAllBuffers: closeAllBuffers,
    onCloseOtherBuffers: () => { if (activeBufferId) closeOtherBuffers(activeBufferId); },
    onSaveAllBuffers: () => { void saveAllBuffers(); },
    onSwitchToBuffer: switchToBuffer,
    onSwitchPane: switchPane,
    activeBufferId,
    activePaneId,
    buffers,
  });

  // ── Hotkeys config loading ─────────────────────────────────────

  useEffect(() => {
    if (!isConnected) return;
    apiService.getHotkeys().then(config => {
      if (config.path) setHotkeysConfigPath(config.path);
    }).catch(() => {});
  }, [isConnected, apiService]);

  const handleOpenHotkeysConfig = useCallback(() => {
    if (!hotkeysConfigPath) return;
    const fileName = hotkeysConfigPath.split('/').pop() || 'hotkeys.json';
    const extensionIndex = fileName.lastIndexOf('.');
    const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
    openFile({ path: hotkeysConfigPath, name: fileName, isDir: false, size: 0, modified: 0, ext: fileExt });
    onViewChange('editor');
    setIsCommandPaletteOpen(false);
  }, [hotkeysConfigPath, openFile, onViewChange]);

  useEffect(() => {
    const handler = () => { handleOpenHotkeysConfig(); };
    window.addEventListener('ledit:open-hotkeys-config', handler);
    return () => window.removeEventListener('ledit:open-hotkeys-config', handler);
  }, [handleOpenHotkeysConfig]);

  // ── Derived state & effects ────────────────────────────────────

  const currentBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
  const contextPanelRef = useRef<ContextPanelHandle>(null);
  const showContextSidebar = currentBuffer?.kind === 'chat';

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

  // ── File click / revision diff handlers ────────────────────────

  const handleFileClick = useCallback((filePath: string, lineNumber?: number) => {
    const segments = filePath.split('/').filter(Boolean);
    const fileName = segments[segments.length - 1] || filePath;
    const extensionIndex = fileName.lastIndexOf('.');
    const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
    onViewChange('editor');
    openFile({ path: filePath, name: fileName, isDir: false, size: 0, modified: 0, ext: fileExt });
    if (typeof lineNumber === 'number') {
      setTimeout(() => {
        document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line: lineNumber } }));
      }, 100);
    }
  }, [onViewChange, openFile]);

  const handleOpenRevisionDiff = useCallback((options: { path: string; diff: string; title: string }) => {
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
      }
    });
  }, [onViewChange, openWorkspaceBuffer]);

  // ── Pane layout container ref ──────────────────────────────────

  const containerRef = useRef<HTMLDivElement>(null);

  // ── Render ──────────────────────────────────────────────────────

  return (
    <div className="app">
      {isMobile && isSidebarOpen && (
        <div className="mobile-overlay" onClick={onCloseSidebar} />
      )}

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

      <div className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${isTerminalExpanded ? 'terminal-expanded' : ''}`}>
        <WorkspaceBar
          isConnected={state.isConnected}
          isMobile={isMobile}
          isMobileMenuOpen={isSidebarOpen}
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
                  onNestedSplitChange={setNestedSplit}
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

      <Terminal
        isExpanded={isTerminalExpanded}
        onToggleExpand={onTerminalExpandedChange}
      />

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
        onToggleTerminal={() => onTerminalExpandedChange(!isTerminalExpanded)}
        onOpenHotkeysConfig={handleOpenHotkeysConfig}
      />
    </div>
  );
};

export default AppContent;
