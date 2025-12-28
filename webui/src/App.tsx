import React, { useState, useEffect, useCallback } from 'react';
import Sidebar from './components/Sidebar';
import Chat from './components/Chat';
import GitView from './components/GitView';
import Status from './components/Status';
import LogsView from './components/LogsView';
import Terminal from './components/Terminal';
import NavigationBar from './components/NavigationBar';
import FileTree from './components/FileTree';
import EditorTabs from './components/EditorTabs';
import EditorPane from './components/EditorPane';
import { EditorManagerProvider, useEditorManager } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import './App.css';
import { WebSocketService } from './services/websocket';
import { ApiService } from './services/api';

// Service Worker Registration
const registerServiceWorker = async () => {
  if ('serviceWorker' in navigator) {
    try {
      const registration = await navigator.serviceWorker.register('/sw.js');
      console.log('SW registered:', registration);

      // Check for updates periodically
      registration.addEventListener('updatefound', () => {
        const newWorker = registration.installing;
        if (newWorker) {
          newWorker.addEventListener('statechange', () => {
            if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
              // New version available, show update notification
              console.log('New service worker available');
              // You could show a toast notification here
            }
          });
        }
      });

      return registration;
    } catch (error) {
      console.log('SW registration failed:', error);
    }
  }
  return null;
};

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
  stats: any; // Enhanced stats from API
}

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

function App() {
  const [state, setState] = useState<AppState>({
    isConnected: false,
    provider: 'unknown',
    model: 'unknown',
    queryCount: 0,
    messages: [],
    logs: [],
    isProcessing: false,
    lastError: null,
    currentView: 'chat',
    toolExecutions: [],
    queryProgress: null,
    stats: {}
  });

  const [inputValue, setInputValue] = useState('');
  const [isMobile, setIsMobile] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const wsService = WebSocketService.getInstance();
  const apiService = ApiService.getInstance();

  const handleEvent = useCallback((event: any) => {
    console.log('📨 Received event:', event.type, event.data);

    // Create log entry for all events
    const logEntry: LogEntry = {
      id: `${Date.now()}-${Math.random()}`,
      type: event.type,
      timestamp: new Date(),
      data: event.data,
      level: 'info',
      category: 'system'
    };

    // Determine log level and category based on event type
    switch(event.type) {
      case 'connection_status':
        logEntry.category = 'system';
        logEntry.level = event.data.connected ? 'success' : 'warning';
        setState(prev => ({
          ...prev,
          isConnected: event.data.connected,
          logs: [...prev.logs, logEntry]
        }));
        console.log('🔗 Connection status updated:', event.data.connected);
        break;

      case 'query_started':
        logEntry.category = 'query';
        logEntry.level = 'info';
        setState(prev => ({
          ...prev,
          queryCount: prev.queryCount + 1,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'user',
            content: event.data.query,
            timestamp: new Date()
          }],
          toolExecutions: [], // Clear previous tool executions
          queryProgress: null, // Clear previous progress
          logs: [...prev.logs, logEntry]
        }));
        console.log('🚀 Query started:', event.data.query);
        break;

      case 'query_progress':
        logEntry.category = 'query';
        logEntry.level = 'info';
        setState(prev => ({
          ...prev,
          queryProgress: event.data,
          logs: [...prev.logs, logEntry]
        }));
        console.log('⏳ Query progress:', event.data);
        break;

      case 'stream_chunk':
        logEntry.category = 'stream';
        logEntry.level = 'info';
        setState(prev => {
          const newMessages = [...prev.messages];
          const lastMessage = newMessages[newMessages.length - 1];
          if (lastMessage && lastMessage.type === 'assistant') {
            lastMessage.content += event.data.chunk;
          } else {
            newMessages.push({
              id: Date.now().toString(),
              type: 'assistant',
              content: event.data.chunk,
              timestamp: new Date()
            });
          }
          return { 
            ...prev, 
            messages: newMessages,
            logs: [...prev.logs, logEntry]
          };
        });
        break;

      case 'query_completed':
        logEntry.category = 'query';
        logEntry.level = 'success';
        setState(prev => ({
          ...prev,
          isProcessing: false,
          lastError: null,
          logs: [...prev.logs, logEntry]
        }));
        console.log('✅ Query completed');
        break;

      case 'tool_execution':
        logEntry.category = 'tool';
        logEntry.level = event.data.status === 'error' ? 'error' : 'info';
        setState(prev => {
          const existingExecution = prev.toolExecutions.find(t => t.tool === event.data.tool);
          let updatedExecutions: ToolExecution[];
          
          if (existingExecution) {
            // Update existing execution
            updatedExecutions = prev.toolExecutions.map(t => 
              t.tool === event.data.tool 
                ? {
                    ...t,
                    status: event.data.status,
                    message: event.data.message,
                    endTime: event.data.status === 'completed' || event.data.status === 'error' ? new Date() : undefined,
                    details: event.data
                  }
                : t
            );
          } else {
            // Add new execution
            const newExecution: ToolExecution = {
              id: `${event.data.tool}-${Date.now()}`,
              tool: event.data.tool,
              status: event.data.status,
              message: event.data.message,
              startTime: new Date(),
              details: event.data
            };
            updatedExecutions = [...prev.toolExecutions, newExecution];
          }
          
          return {
            ...prev,
            toolExecutions: updatedExecutions,
            logs: [...prev.logs, logEntry]
          };
        });
        console.log('🔧 Tool execution:', event.data.tool, event.data.status);
        break;

      case 'file_changed':
        logEntry.category = 'file';
        logEntry.level = 'info';
        setState(prev => {
          const newLogs = [...prev.logs, logEntry];
          return { ...prev, logs: newLogs };
        });
        console.log('📝 File changed:', event.data.path);
        break;

      case 'terminal_output':
        logEntry.category = 'system';
        logEntry.level = 'info';
        // Handle terminal output - this will be processed by the Terminal component
        setState(prev => ({
          ...prev,
          logs: [...prev.logs, logEntry]
        }));
        console.log('🖥️ Terminal output received:', event.data);
        break;

      case 'error':
        logEntry.category = 'system';
        logEntry.level = 'error';
        setState(prev => ({
          ...prev,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'assistant',
            content: `❌ Error: ${event.data.message}`,
            timestamp: new Date()
          }],
          logs: [...prev.logs, logEntry]
        }));
        console.error('❌ Error event:', event.data);
        break;

      case 'metrics_update':
        logEntry.category = 'system';
        logEntry.level = 'info';
        // Update provider/model info if available
        if (event.data.provider && event.data.model) {
          setState(prev => ({
            ...prev,
            provider: event.data.provider,
            model: event.data.model,
            logs: [...prev.logs, logEntry]
          }));
        } else {
          setState(prev => ({
            ...prev,
            logs: [...prev.logs, logEntry]
          }));
        }
        break;

      default:
        // Handle any unknown event types
        logEntry.level = 'warning';
        setState(prev => ({
          ...prev,
          logs: [...prev.logs, logEntry]
        }));
        console.log('❓ Unknown event type:', event.type, event.data);
    }
  }, []);

  useEffect(() => {
    // Register Service Worker for PWA functionality
    registerServiceWorker();

    // Initialize WebSocket connection
    wsService.connect();
    wsService.onEvent(handleEvent);

    // Load initial stats
    const loadStats = () => {
      apiService.getStats().then((stats: any) => {
        setState(prev => ({
          ...prev,
          provider: stats.provider,
          model: stats.model,
          stats: stats
        }));
      }).catch(console.error);
    };

    // Load initial stats
    loadStats();

    // Set up periodic stats updates
    const statsInterval = setInterval(loadStats, 5000); // Update every 5 seconds

    // Check for mobile screen size
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    
    checkMobile();
    window.addEventListener('resize', checkMobile);

    // Cleanup
    return () => {
      wsService.disconnect();
      window.removeEventListener('resize', checkMobile);
      clearInterval(statsInterval);
    };
  }, [handleEvent, wsService, apiService]);

  const handleSendMessage = useCallback(async (message: string) => {
    if (!message.trim() || state.isProcessing) return;

    // Clear any previous errors and set processing state
    setState(prev => ({
      ...prev,
      isProcessing: true,
      lastError: null
    }));

    try {
      console.log('🚀 Sending message:', message);
      await apiService.sendQuery(message);
      setInputValue('');
      console.log('✅ Message sent successfully');
    } catch (error) {
      console.error('❌ Failed to send message:', error);
      setState(prev => ({
        ...prev,
        isProcessing: false,
        lastError: `Failed to send message: ${error instanceof Error ? error.message : 'Unknown error'}`
      }));

      // Add error message to chat
      setState(prev => ({
        ...prev,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `❌ Error: ${error instanceof Error ? error.message : 'Failed to send message'}`,
          timestamp: new Date()
        }]
      }));
    }
  }, [apiService, state.isProcessing]);

  
  const handleModelChange = useCallback((model: string) => {
    console.log('Model changed to:', model);
    // Send model change to backend
    wsService.sendEvent({
      type: 'model_change',
      data: { model }
    });

    setState(prev => ({
      ...prev,
      model
    }));
  }, [wsService]);

  const handleProviderChange = useCallback((provider: string) => {
    console.log('Provider changed to:', provider);
    // Send provider change to backend
    wsService.sendEvent({
      type: 'provider_change',
      data: { provider }
    });

    setState(prev => ({
      ...prev,
      provider
    }));
  }, [wsService]);

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git' | 'logs') => {
    setState(prev => ({
      ...prev,
      currentView: view
    }));
  }, []);

  const handleGitCommit = useCallback(async (message: string, files: string[]) => {
    console.log('Git commit:', message, files);
    try {
      const response = await fetch('/api/git/commit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, files })
      });

      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(errorData.message || 'Failed to create commit');
      }

      const data = await response.json();
      console.log('Commit successful:', data);
      return data;
    } catch (err) {
      console.error('Failed to commit:', err);
      throw err;
    }
  }, []);

  const handleGitStage = useCallback(async (files: string[]) => {
    console.log('Git stage:', files);
    try {
      for (const file of files) {
        const response = await fetch('/api/git/stage', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: file })
        });
        if (!response.ok) {
          throw new Error(`Failed to stage ${file}`);
        }
      }
    } catch (err) {
      console.error('Failed to stage files:', err);
      throw err;
    }
  }, []);

  const handleGitUnstage = useCallback(async (files: string[]) => {
    console.log('Git unstage:', files);
    try {
      for (const file of files) {
        const response = await fetch('/api/git/unstage', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: file })
        });
        if (!response.ok) {
          throw new Error(`Failed to unstage ${file}`);
        }
      }
    } catch (err) {
      console.error('Failed to unstage files:', err);
      throw err;
    }
  }, []);

  const handleGitDiscard = useCallback(async (files: string[]) => {
    console.log('Git discard:', files);
    try {
      for (const file of files) {
        const response = await fetch('/api/git/discard', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: file })
        });
        if (!response.ok) {
          throw new Error(`Failed to discard ${file}`);
        }
      }
    } catch (err) {
      console.error('Failed to discard files:', err);
      throw err;
    }
  }, []);

  const handleTerminalCommand = useCallback(async (command: string) => {
    try {
      console.log('🖥️ Terminal command:', command);
      // Send terminal command to backend via WebSocket
      wsService.sendEvent({
        type: 'terminal_command',
        data: { command }
      });
    } catch (error) {
      console.error('❌ Failed to send terminal command:', error);
    }
  }, [wsService]);

  const handleTerminalOutput = useCallback((output: string) => {
    // You could handle terminal output here if needed
    console.log('🖥️ Terminal output:', output);
  }, []);

  const toggleSidebar = useCallback(() => {
    setIsSidebarOpen(prev => !prev);
  }, []);

  const closeSidebar = useCallback(() => {
    setIsSidebarOpen(false);
  }, []);

  // Child component with access to editor manager
  const AppContent: React.FC = () => {
    const { panes, paneLayout, switchPane, splitPane, closeSplit } = useEditorManager();

    const canSplit = panes.length < 3;
    const canCloseSplit = panes.length > 1;

    return (
      <div className="app">
        {/* Mobile menu button */}
        {isMobile && (
          <button 
            className="mobile-menu-btn"
            onClick={toggleSidebar}
            aria-label="Toggle sidebar"
          >
            ☰
          </button>
        )}

        {/* Mobile overlay */}
        {isMobile && isSidebarOpen && (
          <div 
            className="mobile-overlay"
            onClick={closeSidebar}
          />
        )}

        <Sidebar
          isConnected={state.isConnected}
          selectedModel={state.model}
          onModelChange={handleModelChange}
          availableModels={[state.model]} // You might want to fetch available models from API
          // Note: onViewChange is no longer used in Sidebar - view switching is now in NavigationBar
          stats={{
            queryCount: state.queryCount,
            filesModified: 0 // TODO: track modified files from buffers
          }}
          recentFiles={[]}
          recentLogs={state.logs.slice(-10)}
          isMobileMenuOpen={isSidebarOpen}
          onMobileMenuToggle={toggleSidebar}
          isMobile={isMobile}
          sidebarCollapsed={sidebarCollapsed}
          onSidebarToggle={() => setSidebarCollapsed(!sidebarCollapsed)}
          onProviderChange={handleProviderChange}
        />
        <div className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''}`}>
          {/* Top Navigation Bar */}
          <NavigationBar
            currentView={state.currentView}
            onViewChange={handleViewChange}
          />

          <Status isConnected={state.isConnected} stats={state.stats} />

          {/* View Content */}
          {state.currentView === 'chat' ? (
            <Chat
              messages={state.messages}
              onSendMessage={handleSendMessage}
              inputValue={inputValue}
              onInputChange={setInputValue}
              isProcessing={state.isProcessing}
              lastError={state.lastError}
              toolExecutions={state.toolExecutions}
              queryProgress={state.queryProgress}
            />
          ) : state.currentView === 'git' ? (
            <GitView
              onCommit={handleGitCommit}
              onStage={handleGitStage}
              onUnstage={handleGitUnstage}
              onDiscard={handleGitDiscard}
            />
          ) : state.currentView === 'logs' ? (
            <LogsView
              logs={state.logs}
              onClearLogs={() => setState(prev => ({ ...prev, logs: [] }))}
            />
          ) : state.currentView === 'editor' ? (
            <div className="editor-view">
              {/* Pane Controls */}
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

              {/* Editor Tabs */}
              <EditorTabs />

              <div className={`editor-content ${paneLayout}`}>
                {/* Editor Panes Container */}
                <div className={`panes-container layout-${paneLayout}`}>
                  <PaneWrapper>
                    <FileTreeWrapper>
                      <FileTreeWrapperInner>
                        <FileTreeConnected />
                      </FileTreeWrapperInner>
                    </FileTreeWrapper>
                  </PaneWrapper>
                  {panes.map((pane) => (
                    <PaneWrapper key={pane.id}>
                      <EditorPaneWrapper>
                        <EditorPaneComponent
                          paneId={pane.id}
                          isActive={pane.isActive}
                          onClick={() => switchPane(pane.id)}
                        />
                      </EditorPaneWrapper>
                    </PaneWrapper>
                  ))}
                </div>
              </div>
            </div>
          ) : null}
        </div>

        {/* Terminal Component */}
        <Terminal
          onCommand={handleTerminalCommand}
          onOutput={handleTerminalOutput}
          isConnected={state.isConnected}
        />
      </div>
    );
  };

  return (
    <ThemeProvider>
      <EditorManagerProvider>
        <AppContent />
      </EditorManagerProvider>
    </ThemeProvider>
  );
}

// Wrapper components to avoid React hooks issues
const PaneWrapper: React.FC<{children: React.ReactNode}> = ({ children }) => (
  <div className="pane-wrapper">{children}</div>
);

const FileTreeWrapper: React.FC<{children: React.ReactNode}> = ({ children }) => (
  <div className="filetree-wrapper">{children}</div>
);

const FileTreeWrapperInner: React.FC<{children: React.ReactNode}> = ({ children }) => (
  <div>{children}</div>
);

const FileTreeConnected: React.FC = () => {
  const { openFile } = useEditorManager();
  return (
    <FileTree
      onFileSelect={openFile}
      selectedFile={undefined}
    />
  );
};

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

const EditorPaneComponent: React.FC<{paneId: string, isActive?: boolean, onClick?: () => void}> = ({ paneId, isActive, onClick }) => {
  return (
    <div onClick={onClick}>
      <EditorPane paneId={paneId} />
    </div>
  );
};

export default App;
