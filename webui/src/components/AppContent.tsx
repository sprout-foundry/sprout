import React, { useCallback, useRef } from 'react';
import Sidebar from './Sidebar';
import Chat from './Chat';
import GitView from './GitView';
import LogsView from './LogsView';
import FileEditsPanel from './FileEditsPanel';
import Terminal from './Terminal';
import NavigationBar from './NavigationBar';
import EditorTabs from './EditorTabs';
import EditorPane from './EditorPane';
import ResizeHandle from './ResizeHandle';
import Status from './Status';
import { useEditorManager } from '../contexts/EditorManagerContext';

interface ToolExecution {
  id: string;
  tool: string;
  status: 'started' | 'running' | 'completed' | 'error';
  message?: string;
  startTime: Date;
  endTime?: Date;
  details?: any;
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
  currentView: 'chat' | 'editor' | 'git' | 'logs';
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
  onViewChange: (view: 'chat' | 'editor' | 'git' | 'logs') => void;
  onModelChange: (model: string) => void;
  onProviderChange: (provider: string) => void;
  onSendMessage: (message: string) => void;
  onGitCommit: (message: string, files: string[]) => Promise<unknown>;
  onGitStage: (files: string[]) => Promise<void>;
  onGitUnstage: (files: string[]) => Promise<void>;
  onGitDiscard: (files: string[]) => Promise<void>;
  onClearLogs: () => void;
  onTerminalOutput: (output: string) => void;
  onTerminalExpandedChange: (expanded: boolean) => void;
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
  onGitCommit,
  onGitStage,
  onGitUnstage,
  onGitDiscard,
  onClearLogs,
  onTerminalOutput,
  onTerminalExpandedChange
}) => {
  const { panes, paneLayout, switchPane, splitPane, closeSplit, openFile, paneSizes, updatePaneSize } = useEditorManager();

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
        console.log('Git status for file:', filePath);
        break;
      case 'logs':
        console.log('Filter logs by file:', filePath);
        break;
      default:
        console.log('File clicked in unknown view:', state.currentView, filePath);
    }
  }, [state.currentView, onInputChange, openFile]);

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
      {isMobile && (
        <button
          className="mobile-menu-btn"
          onClick={onToggleSidebar}
          aria-label="Toggle sidebar"
        >
          ☰
        </button>
      )}

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
        <NavigationBar
          currentView={state.currentView}
          onViewChange={onViewChange}
        />

        <Status isConnected={state.isConnected} stats={state.stats} />

        {state.currentView === 'chat' ? (
          <>
            <FileEditsPanel
              edits={state.fileEdits}
              onFileClick={handleFileClick}
            />
            <Chat
              messages={state.messages}
              onSendMessage={onSendMessage}
              inputValue={inputValue}
              onInputChange={onInputChange}
              isProcessing={state.isProcessing}
              lastError={state.lastError}
              toolExecutions={state.toolExecutions}
              queryProgress={state.queryProgress}
            />
          </>
        ) : state.currentView === 'git' ? (
          <GitView
            refreshToken={gitRefreshToken}
            onCommit={onGitCommit}
            onStage={onGitStage}
            onUnstage={onGitUnstage}
            onDiscard={onGitDiscard}
          />
        ) : state.currentView === 'logs' ? (
          <LogsView
            logs={state.logs}
            onClearLogs={onClearLogs}
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
                  ❌ Close Split
                </button>
              )}
              {canSplit && (
                <button
                  onClick={() => panes.find(p => p.isActive) && splitPane(panes.find(p => p.isActive)!.id, 'vertical')}
                  className="pane-control-btn"
                  title="Split vertically"
                >
                  ⬇️ Split ⟂
                </button>
              )}
              {canSplit && (
                <button
                  onClick={() => panes.find(p => p.isActive) && splitPane(panes.find(p => p.isActive)!.id, 'horizontal')}
                  className="pane-control-btn"
                  title="Split horizontally"
                >
                  ➡️ Split ↔
                </button>
              )}
            </div>

            <EditorTabs />

            <div className={`editor-content ${paneLayout}`}>
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
                        <EditorPaneWrapper>
                          <EditorPaneComponent
                            paneId={pane.id}
                            isActive={pane.isActive}
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

      <Terminal
        onOutput={onTerminalOutput}
        isExpanded={isTerminalExpanded}
        onToggleExpand={onTerminalExpandedChange}
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
    <div onClick={onClick}>
      <EditorPane paneId={paneId} />
    </div>
  );
};

export default AppContent;
