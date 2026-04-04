import React, { useCallback, useRef, useState, useEffect, useMemo } from 'react';
import { Menu, X, Columns2, Rows2, LayoutGrid, PanelRightOpen, PanelRightClose, MessageSquarePlus } from 'lucide-react';
import Sidebar from './Sidebar';
import WorkspaceBar from './WorkspaceBar';
import Terminal from './Terminal';
import EditorTabs from './EditorTabs';
import WorkspacePane from './WorkspacePane';
import ContextPanel, { type ContextPanelHandle } from './ContextPanel';
import ResizeHandle from './ResizeHandle';
import Status from './Status';
import CommandPalette from './CommandPalette';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { ApiService, LeditInstance } from '../services/api';
import { useGitWorkspace } from '../hooks/useGitWorkspace';
import type { ChatSession } from '../services/chatSessions';
import type { AppState, LogEntry, PerChatState } from '../types/app';
import { INSTANCE_PID_STORAGE_KEY, INSTANCE_SWITCH_RESET_KEY } from '../constants/app';

const toPaneFlex = (weight: number): React.CSSProperties => ({
  flexGrow: weight,
  flexShrink: 1,
  flexBasis: 0,
  minWidth: 0,
  minHeight: 0,
});

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
  onDeleteChat,
  onRenameChat,
  perChatCache,
}) => {
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

  // Compute current todos: prefer state from todo_update events, fall back to parsing from TodoWrite tool executions
  const currentTodos = useMemo(() => {
    // Prefer directly-provided todos from structured todo_update events
    if (state.currentTodos && state.currentTodos.length > 0) {
      return state.currentTodos;
    }

    // Fallback: find the most recent TodoWrite tool execution and parse its arguments
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

  // Command palette state
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);
  const [isContextPanelMobileOpen, setIsContextPanelMobileOpen] = useState(false);
  const [hotkeysConfigPath, setHotkeysConfigPath] = useState<string | null>(null);
  const [instances, setInstances] = useState<LeditInstance[]>([]);
  const [selectedInstancePID, setSelectedInstancePID] = useState<number>(0);
  const [isSwitchingInstance, setIsSwitchingInstance] = useState(false);
  const [panelWidth, setPanelWidth] = useState(() => {
    if (typeof window === 'undefined') return 360;
    const storedWidth = Number(window.localStorage.getItem('ledit.contextPanel.width'));
    if (Number.isFinite(storedWidth) && storedWidth >= 260 && storedWidth <= 600) {
      return storedWidth;
    }
    return 360;
  });

  const [nestedSplit, setNestedSplit] = useState<{ hostPaneId: string; nestedPaneId: string; direction: 'vertical' | 'horizontal' } | null>(null);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem('ledit.contextPanel.width', String(Math.round(panelWidth)));
  }, [panelWidth]);

  // Keep a stable ref to the current buffers map to avoid infinite loops in effects
  const buffersRef = useRef(buffers);
  useEffect(() => { buffersRef.current = buffers; }, [buffers]);

  // Sync chat sessions → editor buffers: update the initial chat buffer with the active
  // session's ID, and open additional buffers for other sessions.
  useEffect(() => {
    if (!chatSessions || chatSessions.length === 0) return;
    const currentBuffers = buffersRef.current;
    chatSessions.forEach(session => {
      const existing = Array.from(currentBuffers.values()).find(
        b => b.kind === 'chat' && b.metadata?.chatId === session.id
      );
      if (existing) {
        // Update tab title if the session was renamed
        if (existing.file.name !== (session.name || 'Chat')) {
          updateBufferTitle(existing.id, session.name || 'Chat');
        }
        return;
      }
      // If this is the active session and the initial chat buffer has no chatId yet, claim it
      const initialBuf = currentBuffers.get('buffer-chat');
      if (session.id === activeChatId && initialBuf && !initialBuf.metadata?.chatId) {
        updateBufferMetadata('buffer-chat', { chatId: session.id });
        updateBufferTitle('buffer-chat', session.name || 'Chat');
      } else {
        openWorkspaceBuffer({
          kind: 'chat',
          path: `__workspace/chat/${session.id}`,
          title: session.name || 'Chat',
          isPinned: session.is_default ?? false,
          isClosable: !(session.is_default ?? false),
          metadata: { chatId: session.id },
        });
      }
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chatSessions, activeChatId]);

  // Detect when the user switches to a different chat tab and notify parent
  useEffect(() => {
    if (!activeBufferId) return;
    const activeBuf = buffersRef.current.get(activeBufferId);
    if (activeBuf?.kind === 'chat' && activeBuf.metadata?.chatId) {
      const chatId = activeBuf.metadata.chatId as string;
      if (chatId !== activeChatId && onActiveChatChange) {
        onActiveChatChange(chatId);
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeBufferId]);
  const initialViewSyncRef = useRef(false);

  // Load hotkeys config path on mount
  useEffect(() => {
    if (!isConnected) return;
    apiService.getHotkeys().then(config => {
      if (config.path) setHotkeysConfigPath(config.path);
    }).catch(() => {});
  }, [isConnected, apiService]);

  useEffect(() => {
    if (!isConnected) {
      return;
    }

    let cancelled = false;
    let timer: NodeJS.Timeout | null = null;

    const loadInstances = async () => {
      try {
        const data = await apiService.getInstances();
        if (cancelled) {
          return;
        }
        setInstances(data.instances || []);
        const currentPort = Number(window.location.port || 0);
        const currentInstance =
          (data.instances || []).find((instance) => instance.port === currentPort) ||
          (data.instances || []).find((instance) => instance.is_current) ||
          (data.instances || []).find((instance) => instance.pid === data.active_host_pid);
        const nextPID = currentInstance?.pid || 0;
        if (nextPID > 0) {
          setSelectedInstancePID(nextPID);
          window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(nextPID));
        }
      } catch (error) {
        if (!cancelled) {
          console.error('Failed to fetch instances:', error);
        }
      }
      if (!cancelled) {
        timer = setTimeout(loadInstances, 2000);
      }
    };

    loadInstances();
    return () => {
      cancelled = true;
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [apiService, isConnected]);
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

  const handleInstanceChange = useCallback(async (pid: number) => {
    if (!Number.isFinite(pid) || pid <= 0 || pid === selectedInstancePID) {
      return;
    }

    setIsSwitchingInstance(true);
    try {
      const targetInstance = instances.find((instance) => instance.pid === pid);
      if (!targetInstance || !targetInstance.port) {
        throw new Error('Selected instance is unavailable');
      }

      window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(pid));
      window.sessionStorage.setItem(INSTANCE_SWITCH_RESET_KEY, '1');
      const nextURL = new URL(window.location.href);
      nextURL.port = String(targetInstance.port);
      window.location.assign(nextURL.toString());
    } catch (error) {
      console.error('Failed to switch instance:', error);
      setIsSwitchingInstance(false);
    }
  }, [instances, selectedInstancePID]);

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
    if (!activePaneId || index < 0) {
      return;
    }
    const paneBuffers = Array.from(buffers.values()).filter((buffer) => buffer.paneId === activePaneId);
    const target = paneBuffers[index];
    if (target) {
      switchPane(activePaneId);
      switchToBuffer(target.id);
    }
  }, [activePaneId, buffers, switchPane, switchToBuffer]);

  const handleSplitRequest = useCallback((direction: 'vertical' | 'horizontal' | 'grid') => {
    if (direction === 'grid') {
      // If already in grid layout, close it back to single
      if (paneLayout === 'split-grid' && panes.length === 4) {
        // Keep primary pane, preserve its buffer
        const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
        if (primaryPane) {
          const bufId = primaryPane.bufferId;
          closeSplit();
          if (bufId) {
            switchToBuffer(bufId);
          }
        }
        return;
      }
      // Otherwise, save primary buffer ID and create grid directly
      const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
      const bufId = primaryPane?.bufferId;
      splitIntoGrid();
      if (bufId) {
        switchToBuffer(bufId);
      }
      return;
    }

    if (!activePaneId) {
      return;
    }

    const previousPaneCount = panes.length;
    const newPaneId = splitPane(activePaneId, direction);
    if (!newPaneId) {
      return;
    }

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

  // Listen for hotkey custom events
  useEffect(() => {
    const handleHotkey = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (!detail?.commandId) return;
      
      switch (detail.commandId) {
        case 'command_palette':
          setIsCommandPaletteOpen(prev => !prev);
          break;
        case 'new_file':
          openWorkspaceBuffer({
            kind: 'file',
            path: `__workspace/untitled-${Date.now()}`,
            title: 'Untitled',
            ext: '',
            isClosable: true,
          });
          onViewChange('editor');
          break;
        case 'toggle_sidebar':
          onSidebarToggle();
          break;
        case 'toggle_terminal':
          onTerminalExpandedChange(!isTerminalExpanded);
          break;
        case 'toggle_explorer': {
          // Reveal the active file's path in the file tree explorer
          const activeBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
          const filePath = activeBuffer?.file?.path && !activeBuffer.file.isDir && activeBuffer.kind === 'file'
            ? activeBuffer.file.path
            : null;
          
          if (filePath) {
            window.dispatchEvent(new CustomEvent('ledit:reveal-in-explorer', { detail: { path: filePath } }));
          } else {
            // No active file — just toggle sidebar to files
            onSidebarToggle();
          }
          break;
        }
        case 'quick_open':
          setIsCommandPaletteOpen(true);
          break;
        case 'switch_to_chat':
          handlePrimaryViewChange('chat');
          break;
        case 'switch_to_editor':
          handlePrimaryViewChange('editor');
          break;
        case 'switch_to_git':
          handlePrimaryViewChange('git');
          break;
        case 'focus_tab_1':
          focusTabIndex(0);
          break;
        case 'focus_tab_2':
          focusTabIndex(1);
          break;
        case 'focus_tab_3':
          focusTabIndex(2);
          break;
        case 'focus_tab_4':
          focusTabIndex(3);
          break;
        case 'focus_tab_5':
          focusTabIndex(4);
          break;
        case 'focus_tab_6':
          focusTabIndex(5);
          break;
        case 'focus_tab_7':
          focusTabIndex(6);
          break;
        case 'focus_tab_8':
          focusTabIndex(7);
          break;
        case 'focus_tab_9':
          focusTabIndex(8);
          break;
        case 'focus_next_tab': {
          if (!activePaneId) break;
          const paneBuffers = Array.from(buffers.values()).filter((buffer) => buffer.paneId === activePaneId);
          if (paneBuffers.length <= 1) break;
          const currentIdx = activeBufferId ? paneBuffers.findIndex(b => b.id === activeBufferId) : -1;
          const nextIdx = currentIdx + 1 < paneBuffers.length ? currentIdx + 1 : 0;
          if (paneBuffers[nextIdx]) {
            switchPane(activePaneId);
            switchToBuffer(paneBuffers[nextIdx].id);
          }
          break;
        }
        case 'focus_prev_tab': {
          if (!activePaneId) break;
          const paneBuffersPrev = Array.from(buffers.values()).filter((buffer) => buffer.paneId === activePaneId);
          if (paneBuffersPrev.length <= 1) break;
          const currentIdx = activeBufferId ? paneBuffersPrev.findIndex(b => b.id === activeBufferId) : -1;
          const prevIdx = currentIdx - 1 >= 0 ? currentIdx - 1 : paneBuffersPrev.length - 1;
          if (paneBuffersPrev[prevIdx]) {
            switchPane(activePaneId);
            switchToBuffer(paneBuffersPrev[prevIdx].id);
          }
          break;
        }
        case 'close_editor':
          if (activeBufferId) {
            closeBuffer(activeBufferId);
          }
          break;
        case 'close_all_editors':
          closeAllBuffers();
          break;
        case 'close_other_editors':
          if (activeBufferId) {
            closeOtherBuffers(activeBufferId);
          }
          break;
        case 'save_all_files':
          void saveAllBuffers();
          break;
        case 'split_editor_vertical':
          handleSplitRequest('vertical');
          break;
        case 'split_editor_horizontal':
          handleSplitRequest('horizontal');
          break;
        case 'split_editor_grid':
          handleSplitRequest('grid');
          break;
        case 'split_terminal_vertical':
          window.dispatchEvent(new CustomEvent('ledit:terminal-action', { detail: { action: 'split_vertical' } }));
          break;
        case 'split_terminal_horizontal':
          window.dispatchEvent(new CustomEvent('ledit:terminal-action', { detail: { action: 'split_horizontal' } }));
          break;
        case 'editor_toggle_word_wrap':
          document.dispatchEvent(new CustomEvent('editor-toggle-word-wrap'));
          break;
        case 'toggle_linked_scroll':
          document.dispatchEvent(new CustomEvent('editor-toggle-linked-scroll'));
          break;
        case 'toggle_minimap':
          document.dispatchEvent(new CustomEvent('editor-toggle-minimap'));
          break;
      }
    };
    
    window.addEventListener('ledit:hotkey', handleHotkey);
    return () => window.removeEventListener('ledit:hotkey', handleHotkey);
  }, [activeBufferId, activePaneId, buffers, closeAllBuffers, closeBuffer, closeOtherBuffers, focusTabIndex, handlePrimaryViewChange, handleSplitRequest, onSidebarToggle, onTerminalExpandedChange, isTerminalExpanded, openWorkspaceBuffer, onViewChange, saveAllBuffers, switchPane, switchToBuffer]);

  // Handler to open hotkeys config in editor
  const handleOpenHotkeysConfig = useCallback(() => {
    if (!hotkeysConfigPath) return;
    const fileName = hotkeysConfigPath.split('/').pop() || 'hotkeys.json';
    const extensionIndex = fileName.lastIndexOf('.');
    const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
    
    openFile({
      path: hotkeysConfigPath,
      name: fileName,
      isDir: false,
      size: 0,
      modified: 0,
      ext: fileExt,
    });
    
    // Ensure we're in editor view
    onViewChange('editor');
    setIsCommandPaletteOpen(false);
  }, [hotkeysConfigPath, openFile, onViewChange]);

  // Listen for open hotkeys config event
  useEffect(() => {
    const handleOpenHotkeys = () => {
      handleOpenHotkeysConfig();
    };
    window.addEventListener('ledit:open-hotkeys-config', handleOpenHotkeys);
    return () => window.removeEventListener('ledit:open-hotkeys-config', handleOpenHotkeys);
  }, [handleOpenHotkeysConfig]);

  const currentBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
  const contextPanelRef = useRef<ContextPanelHandle>(null);
  const showContextSidebar = currentBuffer?.kind === 'chat';
  const canSplit = panes.length < 3;
  const canSplitGrid = paneLayout !== 'split-grid';
  const canCloseSplit = panes.length > 1;

  useEffect(() => {
    if (!isMobile || !showContextSidebar) {
      setIsContextPanelMobileOpen(false);
    }
  }, [isMobile, showContextSidebar]);

  useEffect(() => {
    if (panes.length < 3 && nestedSplit) {
      setNestedSplit(null);
    }
  }, [nestedSplit, panes.length]);

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

  const handleFileClick = useCallback((filePath: string, lineNumber?: number) => {
    const segments = filePath.split('/').filter(Boolean);
    const fileName = segments[segments.length - 1] || filePath;
    const extensionIndex = fileName.lastIndexOf('.');
    const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
    const openInEditor = () => {
      onViewChange('editor');
      openFile({
        path: filePath,
        name: fileName,
        isDir: false,
        size: 0,
        modified: 0,
        ext: fileExt
      });
    };

    openInEditor();
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

  const handleCloseAllSplits = useCallback(() => {
    if (paneLayout === 'split-grid' && panes.length === 4) {
      // Grid mode: close all splits back to single pane
      const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
      const bufId = primaryPane?.bufferId;
      closeSplit();
      if (bufId) {
        switchToBuffer(bufId);
      }
      return;
    }
    if (nestedSplit) {
      // When a nested split is active, close just the nested pane (3 → 2 panes)
      closePane(nestedSplit.nestedPaneId);
      setNestedSplit(null);
    } else {
      // No nested split — close all splits (2 → 1 pane)
      closeSplit();
    }
  }, [closeSplit, closePane, nestedSplit, paneLayout, panes, switchToBuffer]);

  const containerRef = useRef<HTMLDivElement>(null);
  const dragStartSizeRef = useRef<Map<string, number>>(new Map());
  const isPaneDraggingRef = useRef<Set<string>>(new Set());

  const handlePaneResize = useCallback((sizeKey: string, axis: 'horizontal' | 'vertical', invert = false) => (_deltaPixels: number, totalDeltaPixels: number) => {
    if (!containerRef.current) return;

    const containerRect = containerRef.current.getBoundingClientRect();
    const isVertical = axis === 'horizontal';
    const containerSize = isVertical ? containerRect.width : containerRect.height;
    const deltaPercent = ((invert ? -totalDeltaPixels : totalDeltaPixels) / containerSize) * 100;

    // Capture size at drag start to avoid accumulation bugs.
    // Only capture on first event of each drag; leaks from interrupted
    // drags are handled by overwriting on the next drag's first event.
    if (!isPaneDraggingRef.current.has(sizeKey)) {
      isPaneDraggingRef.current.add(sizeKey);
      dragStartSizeRef.current.set(sizeKey, paneSizes[sizeKey] || 50);
    }
    const sizeAtDragStart = dragStartSizeRef.current.get(sizeKey)!;
    const newSize = Math.max(10, Math.min(90, sizeAtDragStart + deltaPercent));
    updatePaneSize(sizeKey, newSize);
  }, [paneSizes, updatePaneSize]);

  const handlePaneResizeEnd = useCallback((sizeKey: string) => () => {
    isPaneDraggingRef.current.delete(sizeKey);
    dragStartSizeRef.current.delete(sizeKey);
  }, []);

  const showResizeHandles = panes.length > 1;

  const renderSplitControls = (paneId: string) => {
    return (
    <div className="split-controls split-controls-embedded">
      {paneId === activePaneId && onCreateChat && (
        <button
          onClick={async () => {
            const newId = await onCreateChat();
            if (newId) {
              openWorkspaceBuffer({
                kind: 'chat',
                path: `__workspace/chat/${newId}`,
                title: 'New Chat',
                isPinned: false,
                isClosable: true,
                metadata: { chatId: newId },
              });
            }
          }}
          className="pane-control-btn compact"
          title="New chat"
          aria-label="New chat"
        >
          <MessageSquarePlus size={13} />
        </button>
      )}
      {paneId === activePaneId && canCloseSplit && (
        <button
          onClick={handleCloseAllSplits}
          className="pane-control-btn compact"
          title="Close split panes"
          aria-label="Close split panes"
        >
          <X size={13} />
        </button>
      )}
      {paneId === activePaneId && canSplit && (
        <button
          onClick={() => handleSplitRequest('vertical')}
          className="pane-control-btn compact"
          title="Split vertically"
          aria-label="Split vertically"
        >
          <Columns2 size={14} />
        </button>
      )}
      {paneId === activePaneId && canSplit && (
        <button
          onClick={() => handleSplitRequest('horizontal')}
          className="pane-control-btn compact"
          title="Split horizontally"
          aria-label="Split horizontally"
        >
          <Rows2 size={14} />
        </button>
      )}
      {paneId === activePaneId && canSplitGrid && (
        <button
          onClick={() => handleSplitRequest('grid')}
          className="pane-control-btn compact"
          title="Split into 2×2 grid"
          aria-label="Split into 2×2 grid"
        >
          <LayoutGrid size={14} />
        </button>
      )}
    </div>
    );
  };

  const renderPaneById = (paneId: string, style?: React.CSSProperties) => {
    const pane = panes.find((item) => item.id === paneId);
    if (!pane) {
      return null;
    }

    return (
      <PaneWrapper key={pane.id} style={style}>
        <div className="pane-shell">
          <EditorTabs
            paneId={pane.id}
            compact
            actions={renderSplitControls(pane.id)}
          />
          <EditorPaneWrapper
            isActive={pane.id === activePaneId}
            onClick={() => switchPane(pane.id)}
          >
            <EditorPaneComponent
              paneId={pane.id}
              isActive={pane.id === activePaneId}
              onClick={() => switchPane(pane.id)}
              perChatCache={perChatCache}
              activeChatId={activeChatId}
              chatProps={{
                messages: state.messages,
                onSendMessage,
                onQueueMessage,
                queuedMessagesCount,
                queuedMessages,
                onQueueMessageRemove,
                onQueueMessageEdit,
                onQueueReorder,
                onClearQueuedMessages,
                inputValue,
                onInputChange,
                isProcessing: state.isProcessing,
                lastError: state.lastError,
                toolExecutions: state.toolExecutions,
                queryProgress: state.queryProgress,
                currentTodos,
                subagentActivities: state.subagentActivities,
                onStopProcessing,
                onToolPillClick: (toolId: string) => contextPanelRef.current?.highlightTool(toolId),
              }}
              reviewProps={{
                review: deepReview,
                reviewError,
                reviewFixResult,
                reviewFixLogs,
                reviewFixSessionID,
                isReviewLoading,
                isReviewFixing,
                onFixFromReview: handleFixFromReview,
              }}
              diffState={{
                activeDiffPath,
                activeDiff,
                diffMode,
                isDiffLoading,
                diffError,
                onDiffModeChange: handleDiffModeChange,
              }}
            />
          </EditorPaneWrapper>
        </div>
      </PaneWrapper>
    );
  };

  const renderPaneLayout = () => {
    if (panes.length === 0) {
      return null;
    }

    // ── 2×2 Grid layout ─────────────────────────────────────────
    if (paneLayout === 'split-grid' && panes.length === 4) {
      const colSplit = Math.max(10, Math.min(90, paneSizes['grid:col'] ?? 50));
      const rowSplit = Math.max(10, Math.min(90, paneSizes['grid:row'] ?? 50));

      // Order panes by position: primary=0, secondary=1, tertiary=2, quaternary=3
      const positionOrder: Record<string, number> = { 'primary': 0, 'secondary': 1, 'tertiary': 2, 'quaternary': 3 };
      const sortedPanes = [...panes].sort((a, b) => (positionOrder[a.position ?? ''] ?? 99) - (positionOrder[b.position ?? ''] ?? 99));
      const [topLeft, topRight, bottomLeft, bottomRight] = sortedPanes;

      return (
        <div className="grid-pane-layout" style={{
          display: 'grid',
          gridTemplateColumns: `${colSplit}% ${100 - colSplit}%`,
          gridTemplateRows: `${rowSplit}% ${100 - rowSplit}%`,
          flex: 1,
          minWidth: 0,
          minHeight: 0,
          position: 'relative',
        }}>
          {/* Top row */}
          {renderPaneById(topLeft.id)}
          {topRight && renderPaneById(topRight.id)}
          {/* Bottom row */}
          {bottomLeft && renderPaneById(bottomLeft.id)}
          {bottomRight && renderPaneById(bottomRight.id)}

          {/* Center cross resize handles */}
          <ResizeHandle
            direction="horizontal"
            className="grid-resize-handle-col"
            position="absolute"
            style={{ left: `${colSplit}%` }}
            onResize={handlePaneResize('grid:col', 'horizontal')}
            onResizeEnd={handlePaneResizeEnd('grid:col')}
          />
          <ResizeHandle
            direction="vertical"
            className="grid-resize-handle-row"
            position="absolute"
            style={{ top: `${rowSplit}%` }}
            onResize={handlePaneResize('grid:row', 'vertical')}
            onResizeEnd={handlePaneResizeEnd('grid:row')}
          />
        </div>
      );
    }

    if (panes.length < 3 || !nestedSplit) {
      if (panes.length === 1) {
        return renderPaneById(panes[0].id, toPaneFlex(1));
      }

      if (panes.length === 2) {
        const [firstPane, secondPane] = panes;
        const splitAxis = paneLayout === 'split-horizontal' ? 'vertical' : 'horizontal';
        const firstPaneSize = Math.max(10, Math.min(90, paneSizes[firstPane.id] || 50));
        const secondPaneSize = 100 - firstPaneSize;

        return (
          <>
            {renderPaneById(firstPane.id, toPaneFlex(firstPaneSize))}
            <ResizeHandle
              direction={splitAxis}
              onResize={handlePaneResize(firstPane.id, splitAxis)}
              onResizeEnd={handlePaneResizeEnd(firstPane.id)}
            />
            {renderPaneById(secondPane.id, toPaneFlex(secondPaneSize))}
          </>
        );
      }

      return (
        <>
          {panes.map((pane, index) => {
            const paneSize = panes.length === 1
              ? 100
              : (paneSizes[pane.id] || (100 / panes.length));
            const isLast = index === panes.length - 1;
            const splitAxis = paneLayout === 'split-horizontal' ? 'vertical' : 'horizontal';

            return (
              <React.Fragment key={pane.id}>
                {renderPaneById(pane.id, toPaneFlex(paneSize))}
                {showResizeHandles && !isLast && (
                  <ResizeHandle
                    direction={splitAxis}
                    onResize={handlePaneResize(pane.id, splitAxis)}
                    onResizeEnd={handlePaneResizeEnd(pane.id)}
                  />
                )}
              </React.Fragment>
            );
          })}
        </>
      );
    }

    const hostPane = panes.find((pane) => pane.id === nestedSplit.hostPaneId);
    const nestedPane = panes.find((pane) => pane.id === nestedSplit.nestedPaneId);
    const siblingPane = panes.find((pane) => pane.id !== nestedSplit.hostPaneId && pane.id !== nestedSplit.nestedPaneId);
    if (!hostPane || !nestedPane || !siblingPane) {
      return null;
    }

    const rootDirection = paneLayout === 'split-horizontal' ? 'column' : 'row';
    const nestedDirection = nestedSplit.direction === 'horizontal' ? 'column' : 'row';
    const hostIsFirst = panes.findIndex((pane) => pane.id === hostPane.id) < panes.findIndex((pane) => pane.id === siblingPane.id);
    const rootSizeKey = `group:${hostPane.id}`;
    const nestedSizeKey = `nested:${hostPane.id}`;
    const groupSize = paneSizes[rootSizeKey] || 50;
    const nestedSize = paneSizes[nestedSizeKey] || 50;
    const rootHandleDirection = rootDirection === 'row' ? 'horizontal' : 'vertical';
    const nestedHandleDirection = nestedDirection === 'row' ? 'horizontal' : 'vertical';

    const nestedGroup = (
      <div
        className={`nested-pane-group nested-pane-group-${nestedDirection}`}
        style={toPaneFlex(groupSize)}
      >
        {renderPaneById(hostPane.id, toPaneFlex(nestedSize))}
        <ResizeHandle
          direction={nestedHandleDirection}
          onResize={handlePaneResize(nestedSizeKey, nestedHandleDirection)}
          onResizeEnd={handlePaneResizeEnd(nestedSizeKey)}
        />
        {renderPaneById(nestedPane.id, toPaneFlex(100 - nestedSize))}
      </div>
    );

    return (
      <div className={`nested-pane-layout nested-pane-layout-${rootDirection}`}>
        {hostIsFirst ? nestedGroup : renderPaneById(siblingPane.id, toPaneFlex(100 - groupSize))}
        <ResizeHandle
          direction={rootHandleDirection}
          onResize={handlePaneResize(rootSizeKey, rootHandleDirection, !hostIsFirst)}
          onResizeEnd={handlePaneResizeEnd(rootSizeKey)}
        />
        {hostIsFirst ? renderPaneById(siblingPane.id, toPaneFlex(100 - groupSize)) : nestedGroup}
      </div>
    );
  };

  return (
    <div className="app">
      {isMobile && isSidebarOpen && (
        <div
          className="mobile-overlay"
          onClick={onCloseSidebar}
        />
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
          apiService: apiService,
          openWorkspaceBuffer: openWorkspaceBuffer,
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
              <div
                ref={containerRef}
                className={`panes-container layout-${paneLayout}`}
              >
                {renderPaneLayout()}
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
          openFile({
            path: filePath,
            name: fileName,
            isDir: false,
            size: 0,
            modified: 0,
            ext: fileExt,
          });
        }}
        onToggleSidebar={onSidebarToggle}
        onToggleTerminal={() => onTerminalExpandedChange(!isTerminalExpanded)}
        onOpenHotkeysConfig={handleOpenHotkeysConfig}
      />
    </div>
  );
};

const PaneWrapper: React.FC<{children: React.ReactNode, style?: React.CSSProperties}> = ({ children, style }) => (
  <div className="pane-wrapper" style={style}>{children}</div>
);

const EditorPaneWrapper: React.FC<{children: React.ReactNode, isActive?: boolean, onClick?: () => void}> = ({ children, isActive, onClick }) => {
  return (
    <div
      className={`editor-pane-wrapper ${isActive ? 'active' : ''}`}
      onClick={onClick}
      tabIndex={isActive ? -1 : 0}
      onFocus={() => isActive && (onClick?.())}
    >
      {children}
    </div>
  );
};

const EditorPaneComponent: React.FC<{
  paneId: string;
  isActive?: boolean;
  onClick?: () => void;
  perChatCache?: Record<string, PerChatState>;
  activeChatId?: string | null;
  chatProps: React.ComponentProps<typeof WorkspacePane>['chatProps'];
  reviewProps: React.ComponentProps<typeof WorkspacePane>['reviewProps'];
  diffState: React.ComponentProps<typeof WorkspacePane>['diffState'];
}> = ({ paneId, onClick, perChatCache, activeChatId, chatProps, reviewProps, diffState }) => {
  return (
    <div className="editor-pane-host" onClick={onClick}>
      <WorkspacePane
        paneId={paneId}
        perChatCache={perChatCache}
        activeChatId={activeChatId}
        chatProps={chatProps}
        reviewProps={reviewProps}
        diffState={diffState}
      />
    </div>
  );
};

export default AppContent;
