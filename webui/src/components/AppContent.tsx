import type { TodoItem, LogEntry } from '@sprout/ui';
import React, { useCallback, useEffect, useRef, useState, useMemo } from 'react';
import { useHost, useHostCapabilities } from '../host';
import { useAppStoreSetState } from '../contexts/AppStore';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import { useActiveChatTab } from '../hooks/useActiveChatTab';
import { useAppContentHotkeys } from '../hooks/useAppContentHotkeys';
import { useHotkeyCommandHandler } from '../hooks/useHotkeyCommandHandler';
import { useChatSessionsSync } from '../hooks/useChatSessionsSync';
import { getPluginViewIds } from '../services/pluginRegistry';
import { useFileHandler } from '../hooks/useFileHandler';
import { useGitWorkspace } from '../hooks/useGitWorkspace';
import { buildGitPanelProps } from './git/gitPanelProps';
import { useHotkeysConfig } from '../hooks/useHotkeysConfig';
import { useShallowStable } from '../hooks/useShallowStable';
import { useInstances } from '../hooks/useInstances';
import { type SectionTab } from '../hooks/useSidebarState';
import { useSwipeGesture } from '../hooks/useSwipeGesture';
import { useWorkspace } from '../hooks/useWorkspace';
import { requestComposerFocus } from '../hooks/useComposerFocusRequest';
import { ApiService } from '../services/api';
import type { ChatSession } from '../services/chatSessions';
import { forkChatSession } from '../services/chatSessions';
import { executeCommand } from '../services/api/chatApi';
import { clientFetch } from '../services/clientSession';
import { notificationBus } from '../services/notificationBus';
import type { AppState, PerChatState, ViewType } from '../types/app';
import { useLog } from '../utils/log';
import type { WorkspaceModeId, WorkspaceShellProps } from '../views';
import { SproutWorkspace, useWorkspaceMode, useWorkspaceChatProps, useWorkspaceEditor } from '../views';
import { closeBuffersMatching } from './closeBuffers';
import CommandPalette, { type PaletteMode } from './CommandPalette';
import { useCommandPaletteSources } from './CommandPalette/useCommandPaletteSources';
import type { ContextPanelHandle } from './contextPanel/types';
import ErrorBoundary from './ErrorBoundary';
import Sidebar from './Sidebar';
import { useMockupPresence } from './useMockupPresence';
import Terminal from './Terminal';
import WorkspaceGateModal from './WorkspaceGateModal';
import { WorktreeChatDialog } from './WorktreeChatDialog';
import { isLayeredLayout, OPEN_COMMAND_PALETTE_EVENT } from '../config/layout';
import { ChatHistorySwitcher } from './chat/ChatHistorySwitcher';
import PhoneTabBar from './layered/PhoneTabBar';
import NotificationCenterHost from './NotificationCenterHost';
import PlatformHome from './layered/PlatformHome';
import { closeHome } from '../services/homeView';
import { startPlatformNotifications } from '../services/platformNotifications';

interface AppContentProps {
  state: AppState;
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
  recentLogs: LogEntry[];
  gitRefreshToken: number;
  onSidebarToggle: () => void;
  onToggleSidebar: () => void;
  onCloseSidebar: () => void;
  onViewChange: (view: ViewType) => void;
  onModelChange: (model: string) => void;
  onProviderChange: (provider: string) => void;
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
  /** Switch the active chat session. May resolve `true` (landed) / `false` (failed); per-mode pinning uses the result. The lane param (SP-142) lets the server reject cross-mode switches. */
  onActiveChatChange?: (id: string, mode?: 'code' | 'design') => void | Promise<boolean>;
  onTerminalOutput?: (output: string) => void;
  onCreateChat?: (mode?: 'code' | 'design') => Promise<string | null>;
  onCreateChatInWorktree?: (
    branch: string,
    baseRef?: string,
    name?: string,
    autoSwitch?: boolean,
  ) => Promise<string | null>;
  onDeleteChat?: (id: string, options?: { removeWorktree?: boolean }) => Promise<void>;
  onDeleteAllChats?: () => void;
  onRenameChat?: (id: string, name: string) => void;
}

const AppContent: React.FC<AppContentProps> = ({
  state,
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
  recentLogs,
  gitRefreshToken,
  onSidebarToggle,
  onToggleSidebar,
  onCloseSidebar,
  onViewChange,
  onModelChange,
  onProviderChange,
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
  onCreateChatInWorktree,
  onDeleteChat,
  onDeleteAllChats,
  onRenameChat,
}) => {
  const { localTerminal: supportsLocalTerminal } = useHostCapabilities();
  // The host provides a Home surface (its work places) when it has platform
  // pages to embed; the frame is only reachable through that surface.
  const host = useHost();
  // host.8: the hosted build (authMode 'bearer') polls the platform's own
  // notifications and shows the phone tab bar; the local build does neither.
  // This replaces the former isCloud branch.
  const hosted = host.transport.authMode === 'bearer';
  const hasHomeSurface = (host.navigation.workItems?.length ?? 0) > 0;
  const {
    buffers,
    buffersRef,
    activeBufferId,
    activePaneId,
    openFile,
    openWorkspaceBuffer,
    updateBufferTitle,
    updateBufferMetadata,
    closeBuffer,
    setBufferPinned,
    setBufferClosable,
    splitPane,
    switchPane,
    switchToBuffer,
    saveAllBuffers,
    toggleBufferPin,
  } = useEditorManager();
  const { openFile: openWorkspaceFile } = useWorkspaceEditor();
  const apiService = ApiService.getInstance();
  const sproutFetch = useSproutFetch();
  // SP-130: home-directory gate. `useWorkspace` fetches workspace metadata
  // (is_project, needs_workspace_selection, workspace_is_home, …). When the
  // resolved workspace is the user's home directory without consent, a
  // blocking overlay renders on top of the normal UI until the user either
  // selects a project folder or explicitly consents to running in home.
  const { workspaceInfo, setWorkspace: setWorkspaceViaHook, consentHome } = useWorkspace();
  const handleConsentHome = useCallback(async () => {
    // Re-reads the workspace first; a successful consent reloads the page,
    // and a project restored meanwhile closes the modal through the store.
    await consentHome();
  }, [consentHome]);
  // Swipe-left/right gesture to toggle the sidebar on mobile viewports.
  useSwipeGesture({
    onSwipeLeft: onCloseSidebar,
    onSwipeRight: onToggleSidebar,
    enabled: isMobile,
  });
  useEffect(() => (hosted ? startPlatformNotifications() : undefined), [hosted]);
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);
  const [commandPaletteMode, setCommandPaletteMode] = useState<PaletteMode>('all');
  // The layered layout's header search opens the palette from outside this tree.
  useEffect(() => {
    const open = () => {
      setCommandPaletteMode('all');
      setIsCommandPaletteOpen(true);
    };
    window.addEventListener(OPEN_COMMAND_PALETTE_EVENT, open);
    return () => window.removeEventListener(OPEN_COMMAND_PALETTE_EVENT, open);
  }, []);
  const [isForking, setIsForking] = useState(false);
  // The tool id whose inline detail is currently open in the chat.
  // Toggled by a tool pill; reset when the active chat session changes.
  const [activeToolDetailId, setActiveToolDetailId] = useState<string | null>(null);

  // ── New Chat in Worktree dialog ────────────────────────────────
  const [worktreeDialogOpen, setWorktreeDialogOpen] = useState(false);
  const [worktreeCreating, setWorktreeCreating] = useState(false);
  const [worktreeError, setWorktreeError] = useState<string | null>(null);

  const handleWorktreeSubmit = useCallback(
    async (params: { branch: string; baseRef: string; name: string; autoSwitch: boolean }) => {
      if (!onCreateChatInWorktree) return;
      setWorktreeCreating(true);
      setWorktreeError(null);
      try {
        const chatId = await onCreateChatInWorktree(
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
          setWorktreeDialogOpen(false);
        } else {
          setWorktreeError('Failed to create chat session in worktree');
        }
      } catch (err) {
        setWorktreeError(err instanceof Error ? err.message : 'Failed to create chat in worktree');
      } finally {
        setWorktreeCreating(false);
      }
    },
    [onCreateChatInWorktree, openWorkspaceBuffer],
  );

  const setAppState = useAppStoreSetState();

  // Optimistic clear for the New Session (/clear) button: empty the
  // transcript the instant the user clicks, mirroring the state reset the
  // typed /clear path does in useChatSessionManager. The backend confirms
  // with session_changed("clear") — if the command fails there, the
  // post-command state sync restores the transcript.
  const handleChatCleared = useCallback(() => {
    setAppState((prev) => ({
      messages: [],
      isProcessing: false,
      toolExecutions: [],
      fileEdits: [],
      subagentActivities: [],
      currentTodos: [],
      queryProgress: null,
      lastError: null,
    }));
  }, []);

  const clearConversation = useCallback(
    async (id: string) => {
      if (id === activeChatId) handleChatCleared();
      try {
        const result = await executeCommand(clientFetch, '/clear', id);
        if (result.error) notificationBus.notify('error', 'Clear conversation', result.error);
      } catch (err) {
        notificationBus.notify('error', 'Clear conversation', err instanceof Error ? err.message : String(err));
      }
    },
    [activeChatId, handleChatCleared],
  );

  // SP-139 Phase 2: the turn change strip fetched an agent-session diff;
  // open it as a review buffer. Shaped as a GitDiffResponse so the
  // DiffWorkspaceTab text view renders it directly (no staged/unstaged
  // modes; defaultView text — the fragment diff has no full-file pair for
  // the merge view).
  const handleReviewChange = useCallback(
    (path: string, diff: { stats?: string; diff?: string }) => {
      openWorkspaceBuffer({
        kind: 'diff',
        path: `__workspace/changes/${path}`,
        title: `Agent change: ${path.split('/').pop() || path}`,
        ext: '.diff',
        metadata: {
          sourcePath: path,
          defaultView: 'text',
          modeOptions: ['combined'],
          title: 'Agent Change',
          diff: {
            message: 'Agent session change',
            path,
            has_staged: false,
            has_unstaged: true,
            staged_diff: '',
            unstaged_diff: '',
            diff: (diff.stats ? `${diff.stats}\n\n` : '') + (diff.diff ?? ''),
          },
        },
      });
    },
    [openWorkspaceBuffer],
  );

  // Opens the ModelSelectionModal for the currently active provider when
  // the user clicks the model name in the status bar. The modal handles
  // the actual swap via the existing handleModelSelectionResponse path.
  //
  // Editor-only mode ("editor" is the sentinel used by
  // pkg/webui/provider_check.go for "no LLM, editor only") gets routed to
  // the provider setup flow instead — opening ModelSelectionModal for
  // "editor" would fail at the /api/providers/models?provider=editor
  // fetch since there's no such provider on the backend.
  const handleChatModelClick = useCallback(
    (provider: string) => {
      const p = provider || state.provider || '';
      if (!p || p === 'editor') {
        window.dispatchEvent(new CustomEvent('sprout:open-settings-focus', { detail: { focus: 'provider' } }));
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

  // Session search restore: call API then dispatch the custom event
  const log = useLog();
  const handleSessionSearchRestore = useCallback(
    async (sessionId: string, chatId?: string) => {
      try {
        // chatId scopes the restore to that chat (empty = active chat).
        const response = await apiService.restoreSession(sessionId, chatId);
        if (response.messages?.length) {
          window.dispatchEvent(
            new CustomEvent('sprout:session-restored', {
              detail: { messages: response.messages },
            }),
          );
        }
      } catch (err) {
        log.error(`Failed to restore session: ${err instanceof Error ? err.message : String(err)}`, {
          title: 'Session Restore Error',
        });
      }
    },
    [apiService, log],
  );

  const initialViewSyncRef = useRef(false);

  // The workspace's mockup, probed once here and handed to the Code shell's
  // preview panel. A mockup is a view of the work, not a mode: `draw_mockup`
  // writes `design/mockup.html` and the panel shows it beside the code.
  const { present: hasMockup, loading: mockupLoading } = useMockupPresence();
  const { mode: workspaceMode, modes: workspaceModes, select: selectWorkspaceMode } = useWorkspaceMode({ hasMockup });

  useChatSessionsSync({
    chatSessions,
    activeChatId,
    buffersRef,
    updateBufferTitle,
    setBufferPinned,
    setBufferClosable,
    closeBuffer,
    openWorkspaceBuffer,
  });
  // Tab-driven switches (clicking a chat tab) must record the
  // mode pin too — the raw handler would switch without recording, leaving
  // the pin pointing at the previous session.
  useActiveChatTab({ activeBufferId, buffersRef, activeChatId, onActiveChatChange });

  const handlePrimaryViewChange = useCallback(
    (view: ViewType) => {
      if (view === 'chat') {
        // Land on the active chat's own tab. Reopening the generic chat path
        // refocused whichever chat first claimed it, and the tab-driven
        // switch then moved the conversation back to that chat.
        const ownTab = Array.from(buffersRef.current?.values() ?? []).find(
          (b) => b.kind === 'chat' && activeChatId && b.metadata?.chatId === activeChatId,
        );
        openWorkspaceBuffer({
          kind: 'chat',
          path: ownTab?.file.path ?? '__workspace/chat',
          title: ownTab?.file.name ?? 'Chat',
          ext: '.chat',
          isClosable: false,
        });
      }
      onViewChange(view);
    },
    [onViewChange, openWorkspaceBuffer, buffersRef, activeChatId],
  );

  const { handleFileClick } = useFileHandler({ onViewChange, openFile: openWorkspaceFile });

  // Open a conversation from outside the tabs (the layered sidebar): focus its
  // chat tab — or open one bound to it — exactly as clicking the tab would,
  // then switch chat state. The focused chat tab drives the active chat
  // (useActiveChatTab), so opening a generic chat tab would switch it back.
  // The chat's own mode rides the switch (SP-147: one conversation per
  // project — the list is not lane-filtered, and the server's lane backstop
  // keys on the chat's mode, so a design chat must not be opened as code).
  const openConversation = useCallback(
    (id: string, title?: string) => {
      const existing = Array.from(buffersRef.current?.values() ?? []).find(
        (b) => b.kind === 'chat' && b.metadata?.chatId === id,
      );
      if (existing) {
        switchToBuffer(existing.id);
      } else {
        openWorkspaceBuffer({
          kind: 'chat',
          path: `__workspace/chat/${id}`,
          title: title ?? chatSessions?.find((c) => c.id === id)?.name ?? 'Chat',
          isPinned: false,
          isClosable: true,
          metadata: { chatId: id },
        });
      }
      const switched = Promise.resolve(onActiveChatChange?.(id));
      onViewChange('chat');
      // A phone keyboard would cover the conversation being opened.
      if (!isMobile) void switched.finally(() => requestComposerFocus(id));
    },
    [chatSessions, isMobile, onActiveChatChange, onViewChange, openWorkspaceBuffer, switchToBuffer],
  );

  /**
   * Selecting a mode moves the view. The workspace-mode state is the source of
   * truth for "which surface am I in"; `currentView` remains the intra-mode
   * route (chat/editor/git inside Code), so the two are kept in step here
   * rather than each mode re-deriving it.
   */
  const handleSelectMode = useCallback(
    (id: WorkspaceModeId) => {
      selectWorkspaceMode(id);
      // Reset the view only when it belongs to a surface the target mode does
      // not own (a mode that has since been unregistered). chat/editor/git and
      // plugin views are Code's own routes and must not be yanked.
      if (!['chat', 'editor', 'git', ...getPluginViewIds()].includes(state.currentView)) {
        onViewChange('chat');
      }
    },
    [selectWorkspaceMode, onViewChange, state.currentView],
  );

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
    // Direct ref call (the old custom-event hop depended on a listener that
    // lived in ContextSidebar and silently toggled hidden state when the
    // panel was unmounted in file view).
    contextPanelRef.current?.togglePanel();
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

  // The editor, tab and terminal commands (menu items, shortcuts, palette).
  // useAppContentHotkeys above owns the app-level ones and EditorWorkspace the
  // pane splits, so every command has exactly one handler.
  const handleCloseAllBuffers = useCallback(
    () =>
      void closeBuffersMatching(
        () => buffersRef.current,
        closeBuffer,
        () => true,
      ),
    [buffersRef, closeBuffer],
  );
  const handleCloseOtherBuffers = useCallback(() => {
    if (!activeBufferId) return;
    void closeBuffersMatching(
      () => buffersRef.current,
      closeBuffer,
      (buffer) => buffer.id !== activeBufferId,
    );
  }, [activeBufferId, buffersRef, closeBuffer]);
  const handleSaveAllBuffers = useCallback(() => void saveAllBuffers(), [saveAllBuffers]);
  const handleTogglePinTab = useCallback(() => {
    if (activeBufferId) toggleBufferPin(activeBufferId);
  }, [activeBufferId, toggleBufferPin]);
  useHotkeyCommandHandler({
    onCloseAllBuffers: handleCloseAllBuffers,
    onCloseOtherBuffers: handleCloseOtherBuffers,
    onSaveAllBuffers: handleSaveAllBuffers,
    onTogglePinTab: handleTogglePinTab,
    onSwitchToBuffer: switchToBuffer,
    onSwitchPane: switchPane,
    activeBufferId,
    activePaneId,
    buffers,
  });

  // ── Command palette plumbing ─────────────────────────────────────────
  // The palette's data sources (commands, the file index, recents, symbol
  // search) are shared with the composed workspace's palette unit.
  const {
    commands: paletteCommands,
    isLoading: paletteIsLoading,
    recentFiles: paletteRecentFiles,
    workspaceRoot: paletteWorkspaceRoot,
    onSearchFiles: handlePaletteSearchFiles,
    onSearchSymbols: handlePaletteSearchSymbols,
    onSearchWorkspaceSymbols: handlePaletteSearchWorkspaceSymbols,
    onOpenFileAtLine: handlePaletteOpenFileAtLine,
  } = useCommandPaletteSources({ isOpen: isCommandPaletteOpen, openFile: openWorkspaceFile });
  // Open a file in a new editor pane (split). Bound to ⌘/Ctrl+↵ in the
  // palette. Splits the currently-active pane to the right, then opens the
  // file in the newly-focused pane.
  const handlePaletteOpenFileInNewPane = useCallback(
    (filePath: string) => {
      const sourcePaneId = activePaneId;
      const newPaneId = sourcePaneId ? splitPane(sourcePaneId, 'vertical') : null;
      if (newPaneId) {
        switchPane(newPaneId);
      }
      openWorkspaceFile(filePath);
    },
    [activePaneId, splitPane, switchPane, openWorkspaceFile],
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
      if (commandId === 'toggle_terminal') {
        onTerminalExpandedChange(!isTerminalExpanded);
        return;
      }
      if (commandId === 'open_hotkeys_config') {
        handleOpenHotkeysConfig();
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
      // Everything else routes through the hotkey handlers (useAppContentHotkeys,
      // useHotkeyCommandHandler, EditorWorkspace), the path menus and shortcuts take.
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId } }));
    },
    [onSidebarToggle, onTerminalExpandedChange, isTerminalExpanded, handleOpenHotkeysConfig],
  );

  const gitWorkspace = useGitWorkspace({
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

  const {
    gitStatus,
    gitBranches,
    workspaceRoot,
    activeDiffPath,
    activeDiff,
    diffMode,
    isDiffLoading,
    diffError,
    isReviewLoading,
    isReviewFixing,
    reviewError,
    reviewFixResult,
    reviewFixLogs,
    reviewFixSessionID,
    deepReview,
    handleFixFromReview,
    handleDiffModeChange,
  } = gitWorkspace;

  // Open/close the inline tool detail from a tool pill. The chat
  // renders the detail inline (below the message) instead of routing it to
  // the context sidebar, so no mode has to open the sidebar to inspect a
  // tool call.
  const handleToolDetailToggle = useCallback((toolId: string | null) => {
    // null closes the open detail; a matching id toggles it off, a new id opens it.
    // At most one open at a time — this inline detail replaces the old sidebar.
    setActiveToolDetailId((prev) => (prev === toolId ? null : toolId));
  }, []);

  // A session switch replaces the tool list, so the open detail must close.
  useEffect(() => {
    setActiveToolDetailId(null);
  }, [activeChatId]);

  // The ToolExecution backing the open detail (null if the id no longer
  // resolves against the current session's tool list).
  const activeToolDetail = useMemo(
    () => (activeToolDetailId ? (state.toolExecutions.find((t) => t.id === activeToolDetailId) ?? null) : null),
    [activeToolDetailId, state.toolExecutions],
  );

  const handleForkAtBreakpoint = useCallback(
    async (breakpointIndex: number) => {
      if (!activeChatId || isForking) return;
      setIsForking(true);
      try {
        const result = await forkChatSession(activeChatId, breakpointIndex);
        // Reload the chat from the backend to pick up the truncated
        // (forked) history. The fork API mutates the agent in-memory and
        // syncs the state snapshot, so a switchChatSession call returns
        // the correct messages.
        window.dispatchEvent(new CustomEvent('sprout:chat-gap-reload', { detail: { chatId: activeChatId } }));
        // Refresh the session list sidebar so the user sees the fork
        // reflected (new session ID, updated message counts).
        window.dispatchEvent(new CustomEvent('sprout:refresh-sessions'));
        notificationBus.notify('success', 'Session forked', `New session: ${result.session_id.slice(0, 8)}…`);
      } catch (e) {
        console.error('[fork] Failed to fork:', e);
        setAppState((prev) => ({
          lastError: `Failed to fork session: ${e instanceof Error ? e.message : String(e)}`,
        }));
      } finally {
        setIsForking(false);
      }
    },
    [activeChatId, isForking, setAppState],
  );

  // The chat unit assembles the view props from its own chat state and the
  // app-specific callbacks below; the queue mapping and the input value live
  // there too, so the app no longer threads them through props.
  const { chatProps, reviewProps, diffState } = useWorkspaceChatProps({
    activeToolDetail,
    onToolDetailToggle: handleToolDetailToggle,
    onChatCleared: handleChatCleared,
    onReviewChange: handleReviewChange,
    onRestoreSession: handleSessionSearchRestore,
    onModelClick: handleChatModelClick,
    onForkAtBreakpoint: handleForkAtBreakpoint,
    isForking,
    backendReachable,
    onRetryConnection,
    review: {
      review: deepReview,
      reviewError,
      reviewFixResult,
      reviewFixLogs,
      reviewFixSessionID,
      isReviewLoading,
      isReviewFixing,
      onFixFromReview: handleFixFromReview,
    },
    diff: {
      activeDiffPath,
      activeDiff,
      diffMode,
      isDiffLoading,
      diffError,
      onDiffModeChange: handleDiffModeChange,
    },
  });

  // The active mode's shell (workspaceMode.Shell) owns the <main> column and
  // the chrome that belongs to that mode — see workspaces/shell.ts. Each
  // shell reads only the slice of this object its mode renders.
  const shellProps: WorkspaceShellProps = {
    isMobile,
    isTablet,
    isSidebarOpen,
    isConnected: state.isConnected,
    currentView: state.currentView,
    onViewChange,
    onToggleSidebar,
    onToggleContextPanel: handleToggleContextPanel,
    supportsLocalTerminal,
    isTerminalExpanded,
    onTerminalExpandedChange,
    showContextSidebar,
    contextPanelRef,
    toolExecutions: state.toolExecutions,
    logs: state.logs,
    subagentActivities: state.subagentActivities,
    messages: state.messages,
    isProcessing: state.isProcessing,
    lastError: state.lastError,
    queryProgress: state.queryProgress,
    currentBuffer: currentBuffer ?? null,
    handleOutlineNavigateToSymbol,
    chat: {
      perChatCache,
      activeChatId,
      chatSessions,
      onActiveChatChange,
      onCreateChat,
      onCreateChatInWorktree: onCreateChatInWorktree ? () => setWorktreeDialogOpen(true) : undefined,
      onDeleteChat,
      onDeleteAllChats,
      onRenameChat,
      chatProps,
      reviewProps,
      diffState,
    },
    mockup: {
      loading: mockupLoading,
      present: hasMockup,
    },
    git: {
      gitBranches,
      gitStatus,
      workspaceRoot,
    },
    // Ship payload. The deploy data source is the backend deploy API via the
    // app's services; until it is wired, Ship renders its not-yet-deployed
    // state. The shape is final, so wiring the source is a supply-side change
    // only — the shell and surface read this prop and nothing else.
    ship: {
      live: {},
      availability: { canDeploy: false, blockedReason: 'Deploy target not configured' },
      history: [],
    },
  };

  // The workspace composition (SP-160 §160a): the app renders the active space
  // through `SproutWorkspace` — the same component a host mounts — rather than
  // assembling the registry shell itself. The space is the app's resolved
  // mode; the app owns "which space" (its switcher lives in the Sidebar) and
  // feeds the resolved id down, so the switcher and the mounted space agree.
  //
  // `providers="ambient"`: the app root already mounted `HostProvider`
  // (`localHost` in `index.tsx`) and `SproutProviders` (in `App.tsx`), and the
  // app-level chrome (Sidebar, dialogs, terminal) shares those contexts with
  // the space. A second stack here would fork them, so the composition reuses
  // the ambient one and renders only the space.
  //
  // `project` mirrors how the web UI already identifies a project: the local
  // workspace root (falling back to the workspace metadata's root), which is
  // the stable identifier a local/daemon host uses.
  const project = useMemo(
    () => ({ id: workspaceRoot || workspaceInfo.workspace_root || '' }),
    [workspaceRoot, workspaceInfo.workspace_root],
  );

  // ── Sidebar props ────────────────────────────────────────────────
  // The Sidebar (and the file tree inside it) is memoized, but it was being
  // handed freshly-built object literals and closures on every render, so the
  // memo never matched. The app root re-renders on every chat stream flush
  // (~20/s), which dragged the whole sidebar — and the file tree — along with
  // it. Stable callbacks + a shallow-stable object keep that subtree out of
  // the streaming path.
  const chatSessionList = useMemo(() => chatSessions ?? [], [chatSessions]);

  const isChatWorking = useCallback(
    (id: string) => (id === activeChatId ? state.isProcessing : !!perChatCache?.[id]?.isProcessing),
    [activeChatId, state.isProcessing, perChatCache],
  );
  const handleConversationSelect = useCallback((id: string) => openConversation(id), [openConversation]);
  const handleConversationDelete = useMemo(
    () => (onDeleteChat ? (id: string) => void onDeleteChat(id) : undefined),
    [onDeleteChat],
  );
  const canDeleteConversation = useCallback((id: string) => id !== 'default', []);
  const handleConversationClear = useCallback((id: string) => void clearConversation(id), [clearConversation]);
  const handleConversationOpen = useCallback(
    () => (activeChatId ? openConversation(activeChatId) : handlePrimaryViewChange('chat')),
    [activeChatId, openConversation, handlePrimaryViewChange],
  );
  const handleConversationCreate = useMemo(
    () =>
      onCreateChat
        ? () => {
            void (async () => {
              const id = await onCreateChat('code');
              if (id) openConversation(id, 'New Chat');
            })();
          }
        : undefined,
    [onCreateChat, openConversation],
  );
  const conversationHistory = useMemo(
    () =>
      isLayeredLayout ? (
        <ChatHistorySwitcher
          chatId={activeChatId ?? undefined}
          onRestoreSession={handleSessionSearchRestore}
          iconOnly
        />
      ) : undefined,
    [activeChatId, handleSessionSearchRestore],
  );
  const conversations = useShallowStable({
    // The mode's own lane, like its chat tabs (SP-142).
    sessions: chatSessionList,
    activeId: activeChatId,
    // The chat list is only refetched on switch, so its
    // active_query goes stale; the per-chat UI state is live.
    isWorking: isChatWorking,
    inMain: showContextSidebar,
    onSelect: handleConversationSelect,
    onRename: onRenameChat,
    onDelete: handleConversationDelete,
    // The daemon keeps its built-in chat; hosted chats can all go.
    canDelete: canDeleteConversation,
    // The kept chat can still be emptied.
    onClear: handleConversationClear,
    history: conversationHistory,
    onOpen: handleConversationOpen,
    onCreate: handleConversationCreate,
  });

  // `useGitWorkspace` returns a fresh object each render, so mapping it inline
  // rebuilt the panel props every time. The values inside are what matter —
  // rebuild them each render (no stale closures) and keep the identity only
  // when they are unchanged.
  const gitPanel = useShallowStable(
    buildGitPanelProps(gitWorkspace, { openWorkspaceBuffer, onOpenFile: handleFileClick }),
  );

  return (
    <div className="app">
      {/* SP-130: blocking home-workspace gate. Renders as a full-screen
       * overlay on top of the normal UI when the workspace resolves to the
       * user's home directory without consent. The component itself also
       * short-circuits in cloud mode (supportsWorkspaceSwitching = false). */}
      {workspaceInfo.needs_workspace_selection && workspaceInfo.workspace_is_home && (
        <WorkspaceGateModal
          workspaceInfo={workspaceInfo}
          onSelectWorkspace={(path) => setWorkspaceViaHook(path)}
          onConsentHome={handleConsentHome}
        />
      )}
      {isMobile && isSidebarOpen && <div className="mobile-overlay" onClick={onCloseSidebar} />}
      <>
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
            recentLogs={recentLogs}
            isMobileMenuOpen={isSidebarOpen}
            onMobileMenuToggle={onToggleSidebar}
            isMobile={isMobile}
            sidebarCollapsed={sidebarCollapsed}
            onSidebarToggle={onSidebarToggle}
            conversations={conversations}
            modes={workspaceModes}
            activeModeId={workspaceMode.id}
            onSelectMode={handleSelectMode}
            selectedSection={selectedSection}
            onSectionChange={onSectionChange}
            sidebarWidth={sidebarWidth}
            sidebarWidthRef={sidebarWidthRef}
            onSidebarWidthChange={onSidebarWidthChange}
            onSidebarWidthPersist={onSidebarWidthPersist}
            onSidebarWidthReset={onSidebarWidthReset}
            onProviderChange={onProviderChange}
            gitPanel={gitPanel}
          />
        </ErrorBoundary>
        <SproutWorkspace
          providers="ambient"
          project={project}
          space={workspaceMode.id}
          shellProps={shellProps}
          hasMockup={hasMockup}
          onSpaceChange={selectWorkspaceMode}
          className="app-workspace"
        />
      </>
      {supportsLocalTerminal ? (
        <ErrorBoundary panelName="Terminal">
          <Terminal isExpanded={isTerminalExpanded} onToggleExpand={onTerminalExpandedChange} />
        </ErrorBoundary>
      ) : null}
      {isLayeredLayout && hasHomeSurface && <PlatformHome isMobile={isMobile} onOpenMenu={onToggleSidebar} />}
      <NotificationCenterHost />
      {isLayeredLayout && hosted && isMobile && (
        <PhoneTabBar
          drawerOpen={isSidebarOpen}
          onToggleDrawer={onToggleSidebar}
          onCloseDrawer={onCloseSidebar}
          terminalOpen={isTerminalExpanded}
          onLeaveTerminal={() => onTerminalExpandedChange(false)}
        />
      )}
      <CommandPalette
        isOpen={isCommandPaletteOpen}
        onClose={() => setIsCommandPaletteOpen(false)}
        onOpenFile={(filePath) => {
          // Search stays usable above Home; what it opens shows in the editor.
          closeHome();
          openWorkspaceFile(filePath);
          // Recents are populated by the buffer-watcher effect — no explicit
          // recordRecentFile call needed here.
        }}
        onToggleSidebar={onSidebarToggle}
        onToggleTerminal={() => onTerminalExpandedChange(!isTerminalExpanded)}
        onOpenHotkeysConfig={handleOpenHotkeysConfig}
        initialMode={commandPaletteMode}
        onNavigateToLine={(line) => {
          closeHome();
          document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
        }}
        commands={paletteCommands}
        isLoading={paletteIsLoading}
        recentFiles={paletteRecentFiles}
        workspaceRoot={paletteWorkspaceRoot}
        onSearchFiles={handlePaletteSearchFiles}
        onSearchSymbols={handlePaletteSearchSymbols}
        onSearchWorkspaceSymbols={handlePaletteSearchWorkspaceSymbols}
        onOpenFileAtLine={handlePaletteOpenFileAtLine}
        onOpenFileInNewPane={handlePaletteOpenFileInNewPane}
        onExecuteCommand={handlePaletteExecuteCommand}
      />
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
    </div>
  );
};

export default React.memo(AppContent);
