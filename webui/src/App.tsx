import React, { useState, useEffect, useCallback } from 'react';
import Sidebar from './components/Sidebar';
import Chat from './components/Chat';
import GitView from './components/GitView';
import Status from './components/Status';
import UIManager from './components/UIManager';
import CodeEditor from './components/CodeEditor';
import LogsView from './components/LogsView';
import Terminal from './components/Terminal';
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
  files: FileItem[];
  isProcessing: boolean;
  lastError: string | null;
  currentView: 'chat' | 'editor' | 'git' | 'logs';
  selectedFile: FileInfo | null;
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

interface FileItem {
  path: string;
  modified: boolean;
  content?: string;
}

interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
}

function App() {
  const [state, setState] = useState<AppState>({
    isConnected: false,
    provider: 'unknown',
    model: 'unknown',
    queryCount: 0,
    messages: [],
    logs: [],
    files: [],
    isProcessing: false,
    lastError: null,
    currentView: 'chat',
    selectedFile: null,
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
          const updatedFiles = prev.files.map(file => 
            file.path === event.data.path 
              ? { ...file, modified: true }
              : file
          );
          
          // If file not in list, add it
          if (!updatedFiles.some(file => file.path === event.data.path)) {
            updatedFiles.push({
              path: event.data.path,
              modified: true
            });
          }
          
          return {
            ...prev,
            files: updatedFiles,
            logs: [...prev.logs, logEntry]
          };
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

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git' | 'logs') => {
    setState(prev => ({
      ...prev,
      currentView: view
    }));
  }, []);

  const handleFileSelect = useCallback((file: FileInfo) => {
    setState(prev => ({
      ...prev,
      selectedFile: file
    }));
  }, []);

  const handleGitCommit = useCallback((message: string, files: string[]) => {
    console.log('Git commit:', message, files);
    // TODO: Implement actual git commit API call
    // This would call the backend to perform git operations
  }, []);

  const handleGitStage = useCallback((files: string[]) => {
    console.log('Git stage:', files);
    // TODO: Implement actual git stage API call
  }, []);

  const handleGitUnstage = useCallback((files: string[]) => {
    console.log('Git unstage:', files);
    // TODO: Implement actual git unstage API call
  }, []);

  const handleGitDiscard = useCallback((files: string[]) => {
    console.log('Git discard:', files);
    // TODO: Implement actual git discard API call
  }, []);

  const handleFileSave = useCallback((content: string) => {
    console.log('File saved:', state.selectedFile?.path);
    // You could add additional logic here like refreshing the file tree
  }, [state.selectedFile]);

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

  return (
    <UIManager>
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
          currentView={state.currentView}
          onViewChange={handleViewChange}
          stats={{
            queryCount: state.queryCount,
            filesModified: state.files.filter(f => f.modified).length
          }}
          recentFiles={state.files.slice(-5)} // Show last 5 files
          recentLogs={state.logs.slice(-10)} // Show last 10 log entries
          isMobileMenuOpen={isSidebarOpen}
          onMobileMenuToggle={toggleSidebar}
          isMobile={isMobile}
          sidebarCollapsed={sidebarCollapsed}
          onSidebarToggle={() => setSidebarCollapsed(!sidebarCollapsed)}
          // Props for FileTree when in editor view
          onFileSelect={handleFileSelect}
          selectedFile={state.selectedFile?.path}
        />
        <div className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''}`}>
          <Status isConnected={state.isConnected} stats={state.stats} />

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
          ) : (
            <div className="editor-view">
              <CodeEditor
                file={state.selectedFile}
                onSave={handleFileSave}
              />
            </div>
          )}
        </div>

        {/* Terminal Component */}
        <Terminal
          onCommand={handleTerminalCommand}
          onOutput={handleTerminalOutput}
          isConnected={state.isConnected}
        />
      </div>
    </UIManager>
  );
}

export default App;