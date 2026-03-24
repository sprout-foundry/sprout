import React, { useCallback, useRef, useState, useEffect, useMemo } from 'react';
import { Menu, X, Columns2, Rows2, MessageSquare, FileCode2, GitBranch } from 'lucide-react';
import Sidebar from './Sidebar';
import Chat from './Chat';
import GitView from './GitView';
import Terminal from './Terminal';
import EditorTabs from './EditorTabs';
import EditorPane from './EditorPane';
import ResizeHandle from './ResizeHandle';
import Status from './Status';
import CommandPalette from './CommandPalette';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { ApiService, LeditInstance } from '../services/api';

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
  selectedGitFilePath?: string | null;
  onGitFileSelect?: (filePath: string) => void;
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
  selectedGitFilePath,
  onGitFileSelect,
  onTerminalOutput,
  onTerminalExpandedChange,
  isConnected
}) => {
  const { panes, paneLayout, activePaneId, switchPane, splitPane, closeSplit, openFile, paneSizes, updatePaneSize } = useEditorManager();
  const apiService = ApiService.getInstance();

  // Compute current todos from TodoWrite tool executions
  const currentTodos = useMemo(() => {
    // Find the most recent TodoWrite tool execution that has todos in its result or arguments
    const todoWrites = state.toolExecutions
      .filter(t => t.tool === 'TodoWrite')
      .sort((a, b) => b.startTime.getTime() - a.startTime.getTime());
    
    if (todoWrites.length === 0) return [];
    
    // Try to parse todos from the latest TodoWrite
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
  }, [state.toolExecutions]);

  // Command palette state
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);
  const [hotkeysConfigPath, setHotkeysConfigPath] = useState<string | null>(null);
  const [instances, setInstances] = useState<LeditInstance[]>([]);
  const [selectedInstancePID, setSelectedInstancePID] = useState<number>(0);
  const [isSwitchingInstance, setIsSwitchingInstance] = useState(false);
  const [instanceSwitchError, setInstanceSwitchError] = useState<string | null>(null);

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
        } else if (data.active_host_pid && data.active_host_pid > 0) {
          setSelectedInstancePID(data.active_host_pid);
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

  const handleInstanceChange = useCallback(async (e: React.ChangeEvent<HTMLSelectElement>) => {
    const pid = Number(e.target.value);
    if (!Number.isFinite(pid) || pid <= 0 || pid === selectedInstancePID) {
      return;
    }

    setInstanceSwitchError(null);
    setIsSwitchingInstance(true);
    try {
      await apiService.selectInstance(pid);
      // Full page reload to clear all client-side state (editor buffers,
      // CodeMirror instances, WebSocket connections, chat history, etc.)
      window.location.reload();
    } catch (error) {
      console.error('Failed to switch instance:', error);
      setInstanceSwitchError('Failed to switch instance');
      setIsSwitchingInstance(false);
    }
  }, [apiService, selectedInstancePID]);

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
          onViewChange('chat');
          break;
        case 'switch_to_editor':
          onViewChange('editor');
          break;
        case 'switch_to_git':
          onViewChange('git');
          break;
      }
    };
    
    window.addEventListener('ledit:hotkey', handleHotkey);
    return () => window.removeEventListener('ledit:hotkey', handleHotkey);
  }, [onSidebarToggle, onTerminalExpandedChange, isTerminalExpanded, onViewChange]);

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

  const canSplit = panes.length < 3;
  const canCloseSplit = panes.length > 1;

  const handleFileClick = useCallback((filePath: string) => {
    const segments = filePath.split('/').filter(Boolean);
    const fileName = segments[segments.length - 1] || filePath;
    const extensionIndex = fileName.lastIndexOf('.');
    const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';

    switch (state.currentView) {
      case 'chat':
        onInputChange(prev => prev + ` @${filePath}`);
        setTimeout(() => {
          const textarea = document.querySelector('textarea[placeholder*="Ask me"]');
          if (textarea instanceof HTMLTextAreaElement) {
            textarea.focus();
          }
        }, 100);
        break;
      case 'editor':
        openFile({
          path: filePath,
          name: fileName,
          isDir: false,
          size: 0,
          modified: 0,
          ext: fileExt
        });
        break;
      case 'git':
        onGitFileSelect?.(filePath);
        break;
      default:
        console.log('File clicked in unknown view:', state.currentView, filePath);
    }
  }, [state.currentView, onInputChange, openFile, onGitFileSelect]);

  const containerRef = useRef<HTMLDivElement>(null);
  const handlePaneResize = useCallback((paneId: string) => (deltaPixels: number) => {
    if (!containerRef.current) return;

    const containerRect = containerRef.current.getBoundingClientRect();
    const isVertical = paneLayout === 'split-vertical';
    const containerSize = isVertical ? containerRect.width : containerRect.height;
    const deltaPercent = (deltaPixels / containerSize) * 100;
    const currentSize = paneSizes[paneId] || 50;
    const newSize = Math.max(10, Math.min(90, currentSize + deltaPercent));
    updatePaneSize(paneId, newSize);
  }, [paneLayout, paneSizes, updatePaneSize]);

  const showResizeHandles = panes.length > 1;

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
      />
      <div className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${isTerminalExpanded ? 'terminal-expanded' : ''}`}>
        <div className="top-view-toolbar">
          <div className="top-toolbar-left">
            {isMobile && (
              <button
                className="top-mobile-menu-btn"
                onClick={onToggleSidebar}
                aria-label={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
                title={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
              >
                <Menu size={16} />
              </button>
            )}
          <div className="top-view-group" role="tablist" aria-label="Primary views">
            <button
              className={`top-view-btn ${state.currentView === 'chat' ? 'active' : ''}`}
              onClick={() => onViewChange('chat')}
              aria-label="Chat view"
              title="Chat"
              role="tab"
              aria-selected={state.currentView === 'chat'}
            >
              <MessageSquare size={16} />
            </button>
            <button
              className={`top-view-btn ${state.currentView === 'editor' ? 'active' : ''}`}
              onClick={() => onViewChange('editor')}
              aria-label="Editor view"
              title="Editor"
              role="tab"
              aria-selected={state.currentView === 'editor'}
            >
              <FileCode2 size={16} />
            </button>
            <button
              className={`top-view-btn ${state.currentView === 'git' ? 'active' : ''}`}
              onClick={() => onViewChange('git')}
              aria-label="Git view"
              title="Git"
              role="tab"
              aria-selected={state.currentView === 'git'}
            >
              <GitBranch size={16} />
            </button>
          </div>
          </div>
          <div className="top-toolbar-right">
            <div className="top-instance-switcher">
              <select
                id="top-instance-select"
                value={selectedInstancePID || ''}
                onChange={handleInstanceChange}
                disabled={!isConnected || instances.length === 0 || isSwitchingInstance}
                className="top-instance-select"
                title={instances.find(i => i.pid === selectedInstancePID)?.working_dir || ''}
              >
                {instances.length === 0 && (
                  <option value="">No instances</option>
                )}
                {instances.map((instance) => {
                  const suffix = [
                    instance.is_host ? 'host' : '',
                    instance.is_current ? 'this' : '',
                  ].filter(Boolean).join(', ');
                  const name = instance.working_dir.split('/').filter(Boolean).slice(-2).join('/');
                  const fullLabel = isMobile
                    ? `pid:${instance.pid}${suffix ? ` (${suffix})` : ''}`
                    : `${name} · pid:${instance.pid}${suffix ? ` (${suffix})` : ''}`;
                  return (
                    <option key={instance.id} value={instance.pid}>
                      {fullLabel}
                    </option>
                  );
                })}
              </select>
            </div>
          <span className="top-view-current" aria-live="polite">
            {isSwitchingInstance ? 'switching…' : (instanceSwitchError || state.currentView)}
          </span>
          </div>
        </div>

        <div className="main-view-content">
        {state.currentView === 'chat' ? (
          <>
            <Chat
              messages={state.messages}
              onSendMessage={onSendMessage}
              onQueueMessage={onQueueMessage}
              queuedMessagesCount={queuedMessagesCount}
              inputValue={inputValue}
              onInputChange={onInputChange}
              isProcessing={state.isProcessing}
              lastError={state.lastError}
              toolExecutions={state.toolExecutions}
              queryProgress={state.queryProgress}
              currentTodos={currentTodos}
            />
          </>
        ) : state.currentView === 'git' ? (
          <GitView
            refreshToken={gitRefreshToken}
            onCommit={onGitCommit}
            onAICommit={onGitAICommit}
            onStage={onGitStage}
            onUnstage={onGitUnstage}
            onDiscard={onGitDiscard}
            selectedFilePath={selectedGitFilePath}
          />
        ) : state.currentView === 'editor' ? (
          <div className="editor-view">
            <div className="pane-controls">
              {canCloseSplit && (
                <button
                  onClick={closeSplit}
                  className="pane-control-btn"
                  title="Close split pane"
                >
                  <X size={14} /> Close Split
                </button>
              )}
              {canSplit && (
                <button
                  onClick={() => activePaneId && splitPane(activePaneId, 'vertical')}
                  className="pane-control-btn"
                  title="Split vertically"
                >
                  <Columns2 size={16} /> Split
                </button>
              )}
              {canSplit && (
                <button
                  onClick={() => activePaneId && splitPane(activePaneId, 'horizontal')}
                  className="pane-control-btn"
                  title="Split horizontally"
                >
                  <Rows2 size={16} /> Split
                </button>
              )}
            </div>

            <EditorTabs />

            <div className={`editor-workspace ${paneLayout}`}>
              <div
                ref={containerRef}
                className={`panes-container layout-${paneLayout}`}
              >
                {panes.map((pane, index) => {
                  const paneSize = panes.length === 1
                    ? 100
                    : (paneSizes[pane.id] || (100 / panes.length));
                  const isLast = index === panes.length - 1;

                  return (
                    <React.Fragment key={pane.id}>
                      <PaneWrapper style={{ flex: `0 0 ${paneSize}%` }}>
                        <EditorPaneWrapper
                          isActive={pane.id === activePaneId}
                          onClick={() => switchPane(pane.id)}
                        >
                          <EditorPaneComponent
                            paneId={pane.id}
                            isActive={pane.id === activePaneId}
                            onClick={() => switchPane(pane.id)}
                          />
                        </EditorPaneWrapper>
                      </PaneWrapper>

                      {showResizeHandles && !isLast && (
                        <ResizeHandle
                          direction={paneLayout === 'split-horizontal' ? 'vertical' : 'horizontal'}
                          onResize={handlePaneResize(pane.id)}
                        />
                      )}
                    </React.Fragment>
                  );
                })}
              </div>
            </div>
          </div>
        ) : null}
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

const EditorPaneComponent: React.FC<{paneId: string, isActive?: boolean, onClick?: () => void}> = ({ paneId, onClick }) => {
  return (
    <div className="editor-pane-host" onClick={onClick}>
      <EditorPane paneId={paneId} />
    </div>
  );
};

export default AppContent;
