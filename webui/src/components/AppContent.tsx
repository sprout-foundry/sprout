import React, { useCallback, useRef, useState, useEffect, useMemo } from 'react';
import { Menu, X, Columns2, Rows2 } from 'lucide-react';
import Sidebar from './Sidebar';
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

const INSTANCE_PID_STORAGE_KEY = 'ledit:webui:instancePid';
const INSTANCE_SWITCH_RESET_KEY = 'ledit:webui:instanceSwitchReset';

const toPaneFlex = (weight: number): React.CSSProperties => ({
  flexGrow: weight,
  flexShrink: 1,
  flexBasis: 0,
  minWidth: 0,
  minHeight: 0,
});

interface ToolExecution {
  id: string;
  tool: string;
  status: 'started' | 'running' | 'completed' | 'error';
  message?: string;
  startTime: Date;
  endTime?: Date;
  details?: any;
  arguments?: string;
  result?: string;
  persona?: string;
  subagentType?: 'single' | 'parallel';
}

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
}

interface LogEntry {
  id: string;
  type: string;
  timestamp: Date;
  data: any;
  level: 'info' | 'warning' | 'error' | 'success';
  category: 'query' | 'tool' | 'file' | 'system' | 'stream';
}

interface AppState {
  isConnected: boolean;
  provider: string;
  model: string;
  queryCount: number;
  messages: Message[];
  logs: LogEntry[];
  isProcessing: boolean;
  lastError: string | null;
  currentView: 'chat' | 'editor' | 'git';
  toolExecutions: ToolExecution[];
  queryProgress: any;
  stats: any;
  currentTodos: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
  fileEdits: Array<{
    path: string;
    action: string;
    timestamp: Date;
    linesAdded?: number;
    linesDeleted?: number;
  }>;
}

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
  queuedMessagesCount: number;
  onGitCommit: (message: string, files: string[]) => Promise<unknown>;
  onGitAICommit: () => Promise<string>;
  onGitStage: (files: string[]) => Promise<void>;
  onGitUnstage: (files: string[]) => Promise<void>;
  onGitDiscard: (files: string[]) => Promise<void>;
  onTerminalOutput: (output: string) => void;
  onTerminalExpandedChange: (expanded: boolean) => void;
  isConnected: boolean;
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
  queuedMessagesCount,
  onGitCommit,
  onGitAICommit,
  onGitStage,
  onGitUnstage,
  onGitDiscard,
  onTerminalOutput,
  onTerminalExpandedChange,
  isConnected
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
    closeSplit,
    closeBuffer,
    openFile,
    openWorkspaceBuffer,
    paneSizes,
    updatePaneSize
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
        if (data.desired_host_pid && data.desired_host_pid > 0) {
          setSelectedInstancePID(data.desired_host_pid);
          window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(data.desired_host_pid));
        } else if (data.active_host_pid && data.active_host_pid > 0) {
          setSelectedInstancePID(data.active_host_pid);
          window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(data.active_host_pid));
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
    isReviewLoading,
    isReviewFixing,
    reviewError,
    reviewFixResult,
    reviewFixLogs,
    reviewFixSessionID,
    deepReview,
    handleToggleFileSelection,
    handleToggleSectionSelection,
    handlePreviewGitFile,
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

  const handleInstanceChange = useCallback(async (e: React.ChangeEvent<HTMLSelectElement>) => {
    const pid = Number(e.target.value);
    if (!Number.isFinite(pid) || pid <= 0 || pid === selectedInstancePID) {
      return;
    }

    setIsSwitchingInstance(true);
    try {
      window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(pid));
      window.sessionStorage.setItem(INSTANCE_SWITCH_RESET_KEY, '1');
      await apiService.selectInstance(pid);
      // Full page reload to clear all client-side state (editor buffers,
      // CodeMirror instances, WebSocket connections, chat history, etc.)
      window.location.reload();
    } catch (error) {
      console.error('Failed to switch instance:', error);
      setIsSwitchingInstance(false);
    }
  }, [apiService, selectedInstancePID]);

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

  // Listen for hotkey custom events
  useEffect(() => {
    const handleHotkey = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (!detail?.commandId) return;
      
      switch (detail.commandId) {
        case 'command_palette':
          setIsCommandPaletteOpen(prev => !prev);
          break;
        case 'toggle_sidebar':
          onSidebarToggle();
          break;
        case 'toggle_terminal':
          onTerminalExpandedChange(!isTerminalExpanded);
          break;
        case 'toggle_explorer':
          onSidebarToggle();
          break;
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
        case 'close_editor':
          if (activeBufferId) {
            closeBuffer(activeBufferId);
          }
          break;
      }
    };
    
    window.addEventListener('ledit:hotkey', handleHotkey);
    return () => window.removeEventListener('ledit:hotkey', handleHotkey);
  }, [activeBufferId, closeBuffer, focusTabIndex, handlePrimaryViewChange, onSidebarToggle, onTerminalExpandedChange, isTerminalExpanded]);

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
  const canCloseSplit = panes.length > 1;

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

  const handleSplitRequest = useCallback((direction: 'vertical' | 'horizontal') => {
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
  }, [activePaneId, panes.length, splitPane, updatePaneSize]);

  const handleCloseAllSplits = useCallback(() => {
    setNestedSplit(null);
    closeSplit();
  }, [closeSplit]);

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

  const renderSplitControls = (paneId: string) => (
    <div className="split-controls split-controls-embedded">
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
    </div>
  );

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
              chatProps={{
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
        onOpenRevisionDiff={handleOpenRevisionDiff}
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
          onPreviewFile: handlePreviewGitFile,
          onStageFile: handleStageFile,
          onUnstageFile: handleUnstageFile,
          onDiscardFile: handleDiscardFile,
          onSectionAction: handleSectionAction,
        }}
      />
      <div className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${isTerminalExpanded ? 'terminal-expanded' : ''}`}>
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
              currentTodos={currentTodos}
              messages={state.messages}
              isProcessing={state.isProcessing}
              lastError={state.lastError}
              queryProgress={state.queryProgress}
              isMobileLayout={isMobile}
              panelWidth={panelWidth}
              onPanelWidthChange={setPanelWidth}
              onOpenRevisionDiff={handleOpenRevisionDiff}
            />
          )}
        </div>
        <Status isConnected={state.isConnected} position="bottom" stats={state.stats} />
      </div>

      <Terminal
        onOutput={onTerminalOutput}
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
        onViewChange={onViewChange}
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
  chatProps: React.ComponentProps<typeof WorkspacePane>['chatProps'];
  reviewProps: React.ComponentProps<typeof WorkspacePane>['reviewProps'];
  diffState: React.ComponentProps<typeof WorkspacePane>['diffState'];
}> = ({ paneId, onClick, chatProps, reviewProps, diffState }) => {
  return (
    <div className="editor-pane-host" onClick={onClick}>
      <WorkspacePane
        paneId={paneId}
        chatProps={chatProps}
        reviewProps={reviewProps}
        diffState={diffState}
      />
    </div>
  );
};

export default AppContent;
