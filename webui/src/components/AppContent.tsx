import type { TodoItem, LogEntry } from '@sprout/ui';
import { Menu, PanelRightClose } from 'lucide-react';
import React, { useCallback, useEffect, useRef, useState, useMemo } from 'react';
import { supportsLocalTerminal } from '../config/mode';
import { useEditorManager } from '../contexts/EditorManagerContext';
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
import { useSwipeGesture } from '../hooks/useSwipeGesture';
import { ApiService } from '../services/api';
import { getWorkspaceSymbols } from '../services/api/editorApi';
import type { ChatSession } from '../services/chatSessions';
import type { AppState, PerChatState } from '../types/app';
import { useAppStoreSetState } from '../contexts/AppStore';
import { useHotkeys } from '../contexts/HotkeyContext';
import { fuzzyFilter } from '../utils/fuzzyMatch';
import { useLog } from '../utils/log';
import { extractSymbols } from '../utils/symbolUtils';
import CommandPalette, { type PaletteMode } from './CommandPalette';
import { VISIBLE_COMMANDS } from './CommandPalette/constants';
import useFileIndex from './CommandPalette/useFileIndex';
import type { ContextPanelHandle } from './contextPanel/types';
import ContextSidebar from './ContextSidebar';
import EditorWorkspace from './EditorWorkspace';
import ErrorBoundary from './ErrorBoundary';
import HeaderBar from './HeaderBar';
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
    activePaneId,
    openFile,
    openWorkspaceBuffer,
    updateBufferTitle,
    updateBufferMetadata,
    closeBuffer,
    splitPane,
    switchPane,
  } = useEditorManager();
  const apiService = ApiService.getInstance();
  const sproutFetch = useSproutFetch();
  const currentTodos = useCurrentTodos(state.currentTodos, state.toolExecutions);
  // Swipe-left/right gesture to toggle the sidebar on mobile viewports.
  useSwipeGesture({
    onSwipeLeft: onCloseSidebar,
    onSwipeRight: onToggleSidebar,
    enabled: isMobile,
  });
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);
  const [commandPaletteMode, setCommandPaletteMode] = useState<PaletteMode>('all');

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
      // with React's setState. reason='switch' so the modal renders with
      // neutral "Choose a model" copy instead of the warning treatment
      // used for unavailable-model recovery.
      setAppState(() => ({ modelSelectionRequest: { provider: p, reason: 'switch' } }));
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

  // ── Command palette plumbing ─────────────────────────────────────────
  // Pre-index workspace files only while the palette is open. The fuzzy
  // filter then runs against this in-memory list for instant results.
  const paletteLog = useLog();
  // Bake the user's current keybinding next to each command so the palette
  // doubles as a shortcut cheatsheet. `hotkeyForCommand` already handles
  // Mac vs non-Mac modifier substitution (Cmd ↔ Ctrl).
  const { hotkeyForCommand } = useHotkeys();
  const paletteCommands = useMemo(
    () => VISIBLE_COMMANDS.map((cmd) => ({ ...cmd, shortcut: hotkeyForCommand(cmd.id) ?? undefined })),
    [hotkeyForCommand],
  );
  const { allFiles: paletteAllFiles, isLoadingFiles: paletteIsLoading } = useFileIndex({
    apiService,
    isOpen: isCommandPaletteOpen,
    log: paletteLog,
  });
  const handlePaletteSearchFiles = useCallback(
    async (query: string) => {
      if (!query) return [];
      const matches = fuzzyFilter(query, paletteAllFiles, (f) => f.path, 50);
      return matches.map((m) => ({ name: m.item.name, path: m.item.path, type: m.item.type }));
    },
    [paletteAllFiles],
  );
  // Most-recently-opened files. Persisted across sessions so the palette
  // has something to show on idle, even right after page load. Limited to
  // 15 entries — beyond that the list becomes noise.
  const RECENT_FILES_STORAGE_KEY = 'sprout.commandPalette.recentFiles.v1';
  const RECENT_FILES_LIMIT = 15;
  type RecentFile = { name: string; path: string; type: string };
  const [paletteRecentFiles, setPaletteRecentFiles] = useState<RecentFile[]>(() => {
    try {
      const raw = typeof window !== 'undefined' ? window.localStorage.getItem(RECENT_FILES_STORAGE_KEY) : null;
      if (!raw) return [];
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed)) return [];
      return parsed.slice(0, RECENT_FILES_LIMIT);
    } catch {
      return [];
    }
  });
  const recordRecentFile = useCallback((file: RecentFile) => {
    setPaletteRecentFiles((prev) => {
      const next = [file, ...prev.filter((f) => f.path !== file.path)].slice(0, RECENT_FILES_LIMIT);
      try {
        window.localStorage.setItem(RECENT_FILES_STORAGE_KEY, JSON.stringify(next));
      } catch {
        // ignore quota / privacy-mode errors
      }
      return next;
    });
  }, []);
  // Watch the editor manager's buffer set so *any* path that opens a file
  // (palette, file tree, hotkey, drag-drop, layout restore…) contributes to
  // the recents MRU. Skip non-file kinds (welcome/chat/diff/etc.) and the
  // synthetic __workspace/ paths used by virtual buffers.
  const seenFileBufferIdsRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const seen = seenFileBufferIdsRef.current;
    for (const [id, buffer] of buffers.entries()) {
      if (seen.has(id)) continue;
      seen.add(id);
      if (buffer.kind !== 'file') continue;
      const path = buffer.file?.path;
      if (!path || path.startsWith('__workspace/')) continue;
      recordRecentFile({
        name: buffer.file.name,
        path,
        type: 'file',
      });
    }
  }, [buffers, recordRecentFile]);
  const handlePaletteSearchSymbols = useCallback(
    (query: string) => {
      const content = currentBuffer?.content;
      if (!content) return [];
      const ext = currentBuffer?.file?.ext || '';
      const langId = ext.startsWith('.') ? ext.slice(1) : ext;
      // extractSymbols is internally cached by content hash, so repeated
      // calls for the same buffer are cheap.
      const symbols = extractSymbols(content, langId);
      const top = query.trim()
        ? fuzzyFilter(query, symbols, (s) => s.name, 100).map((m) => m.item)
        : symbols.slice(0, 100);
      return top.map((s) => ({ name: s.name, kind: s.kind, line: s.line }));
    },
    [currentBuffer],
  );
  // Workspace-wide LSP symbol search. Surfaces symbols from any indexed
  // file so the user can jump to a definition without first opening the
  // file. The shared palette handles debouncing + race-safety.
  const handlePaletteSearchWorkspaceSymbols = useCallback(
    async (query: string) => {
      const q = query.trim();
      if (!q) return [];
      try {
        const response = await getWorkspaceSymbols(sproutFetch, q);
        const out: Array<{ name: string; kind: string; line: number; filePath: string }> = [];
        for (const file of response.files ?? []) {
          for (const sym of file.symbols ?? []) {
            if (sym.line == null) continue;
            out.push({
              name: sym.name,
              kind: sym.kind || 'symbol',
              line: sym.line,
              filePath: file.file,
            });
            if (out.length >= 100) return out;
          }
        }
        return out;
      } catch {
        return [];
      }
    },
    [sproutFetch],
  );
  // Open a file at a specific line. Used by workspace symbol clicks: the
  // user picks a symbol that lives elsewhere; we load that file then fire
  // the goto-line event after the editor has mounted the buffer.
  const handlePaletteOpenFileAtLine = useCallback(
    (filePath: string, line: number) => {
      const fileName = filePath.split('/').filter(Boolean).pop() || filePath;
      const extensionIndex = fileName.lastIndexOf('.');
      const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
      openFile({ path: filePath, name: fileName, isDir: false, size: 0, modified: 0, ext: fileExt });
      // Defer the goto-line dispatch one tick so the editor has time to
      // mount the new buffer's CodeMirror instance; otherwise the event
      // fires against a buffer that doesn't exist yet and is dropped.
      setTimeout(() => {
        document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
      }, 100);
    },
    [openFile],
  );
  // Open a file in a new editor pane (split). Bound to ⌘/Ctrl+↵ in the
  // palette. Splits the currently-active pane to the right, then opens the
  // file in the newly-focused pane.
  const handlePaletteOpenFileInNewPane = useCallback(
    (filePath: string) => {
      const fileName = filePath.split('/').filter(Boolean).pop() || filePath;
      const extensionIndex = fileName.lastIndexOf('.');
      const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
      const sourcePaneId = activePaneId;
      const newPaneId = sourcePaneId ? splitPane(sourcePaneId, 'vertical') : null;
      if (newPaneId) {
        switchPane(newPaneId);
      }
      openFile({ path: filePath, name: fileName, isDir: false, size: 0, modified: 0, ext: fileExt });
    },
    [activePaneId, splitPane, switchPane, openFile],
  );
  // Returning `false` from onExecuteCommand keeps the palette open — used by
  // commands that change the palette's own mode (quick_open, goto_symbol).
  const handlePaletteExecuteCommand = useCallback(
    (commandId: string): void | boolean => {
      if (commandId === 'quick_open') {
        setCommandPaletteMode('files');
        return false;
      }
      if (commandId === 'editor_goto_symbol') {
        setCommandPaletteMode('symbols');
        return false;
      }
      if (commandId === 'toggle_sidebar' || commandId === 'toggle_explorer') {
        onSidebarToggle();
        return;
      }
      if (commandId === 'toggle_terminal' && supportsLocalTerminal) {
        onTerminalExpandedChange(!isTerminalExpanded);
        return;
      }
      if (commandId === 'open_hotkeys_config') {
        handleOpenHotkeysConfig();
        return;
      }
      if (commandId === 'editor_cycle_whitespace_rendering') {
        window.dispatchEvent(new CustomEvent('editor-cycle-whitespace-rendering'));
        return;
      }
      if (commandId === 'format_document') {
        document.dispatchEvent(new CustomEvent('editor-format-document'));
        return;
      }
      if (commandId === 'editor_find_all_references') {
        document.dispatchEvent(new CustomEvent('editor-find-all-references'));
        return;
      }
      if (commandId === 'editor_workspace_symbol') {
        document.dispatchEvent(new CustomEvent('editor-go-to-workspace-symbol'));
        return;
      }
      // Everything else routes through the existing hotkey infrastructure
      // (useAppContentHotkeys already listens for sprout:hotkey events).
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId } }));
    },
    [
      onSidebarToggle,
      onTerminalExpandedChange,
      isTerminalExpanded,
      handleOpenHotkeysConfig,
    ],
  );

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
      subagentActivities: state.subagentActivities,
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
      state.subagentActivities,
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
      <main
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
          />
        </div>
        <Status isConnected={state.isConnected} stats={state.stats} />
        <StatusBar
          branch={gitBranches.current || gitStatus?.branch}
          workspacePath={workspaceRoot}
          onWorkspaceClick={() => onToggleSidebar()}
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
          chatStats={state.stats}
          isConnected={state.isConnected}
          onModelClick={handleStatusBarModelClick}
        />
      </main>
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
          // Recents are populated by the buffer-watcher effect — no explicit
          // recordRecentFile call needed here.
        }}
        onToggleSidebar={onSidebarToggle}
        onToggleTerminal={supportsLocalTerminal ? () => onTerminalExpandedChange(!isTerminalExpanded) : () => {}}
        onOpenHotkeysConfig={handleOpenHotkeysConfig}
        initialMode={commandPaletteMode}
        onNavigateToLine={(line) => {
          document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
        }}
        commands={paletteCommands}
        isLoading={paletteIsLoading}
        recentFiles={paletteRecentFiles}
        onSearchFiles={handlePaletteSearchFiles}
        onSearchSymbols={handlePaletteSearchSymbols}
        onSearchWorkspaceSymbols={handlePaletteSearchWorkspaceSymbols}
        onOpenFileAtLine={handlePaletteOpenFileAtLine}
        onOpenFileInNewPane={handlePaletteOpenFileInNewPane}
        onExecuteCommand={handlePaletteExecuteCommand}
      />
    </div>
  );
};

export default React.memo(AppContent);
