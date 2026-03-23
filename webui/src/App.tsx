import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import ErrorBoundary from './components/ErrorBoundary';
import AppContent from './components/AppContent';
import UIManager from './components/UIManager';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import './App.css';
import { WebSocketService } from './services/websocket';
import { ApiService } from './services/api';
import { viewRegistry, ChatViewProvider, EditorViewProvider, GitViewProvider, LogsViewProvider } from './providers';
import { debugLog } from './utils/log';

// Service Worker Registration
const registerServiceWorker = async () => {
  if (!('serviceWorker' in navigator)) {
    return null;
  }

  if (process.env.NODE_ENV !== 'production') {
    const registrations = await navigator.serviceWorker.getRegistrations();
    await Promise.all(registrations.map((registration) => registration.unregister()));
    return null;
  }

  try {
    const swUrl = `${process.env.PUBLIC_URL || ''}/sw.js`;
    const registration = await navigator.serviceWorker.register(swUrl);
    await registration.update();
    debugLog('SW registered:', registration);

    // If an update is already waiting, activate it immediately.
    if (registration.waiting) {
      registration.waiting.postMessage({ type: 'SKIP_WAITING' });
    }

    // Ensure we pick up new SW/controller as soon as it activates.
    let hasReloadedForController = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (hasReloadedForController) {
        return;
      }
      hasReloadedForController = true;
      window.location.reload();
    });

    registration.addEventListener('updatefound', () => {
      const newWorker = registration.installing;
      if (newWorker) {
        newWorker.addEventListener('statechange', () => {
          if (newWorker.state === 'installed') {
            newWorker.postMessage({ type: 'SKIP_WAITING' });
          }
          if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
            debugLog('New service worker available');
          }
        });
      }
    });

    return registration;
  } catch (error) {
    debugLog('SW registration failed:', error);
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
  fileEdits: Array<{
    path: string;
    action: string;
    timestamp: Date;
    linesAdded?: number;
    linesDeleted?: number;
  }>;
}

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

const APP_STATE_STORAGE_KEY = 'ledit:webui:state:v1';

const parseDate = (value: unknown): Date => {
  if (value instanceof Date) {
    return value;
  }
  if (typeof value === 'string' || typeof value === 'number') {
    const parsed = new Date(value);
    if (!Number.isNaN(parsed.getTime())) {
      return parsed;
    }
  }
  return new Date();
};

const loadPersistedAppState = (): Partial<AppState> | null => {
  if (typeof window === 'undefined' || !window.localStorage) {
    return null;
  }

  try {
    const raw = window.localStorage.getItem(APP_STATE_STORAGE_KEY);
    if (!raw) {
      return null;
    }

    const parsed = JSON.parse(raw);
    return {
      provider: typeof parsed.provider === 'string' ? parsed.provider : 'unknown',
      model: typeof parsed.model === 'string' ? parsed.model : 'unknown',
      queryCount: typeof parsed.queryCount === 'number' ? parsed.queryCount : 0,
      currentView: ['chat', 'editor', 'git', 'logs'].includes(parsed.currentView) ? parsed.currentView : 'chat',
      messages: Array.isArray(parsed.messages)
        ? parsed.messages.map((message: any) => ({
            ...message,
            timestamp: parseDate(message?.timestamp)
          }))
        : [],
      logs: Array.isArray(parsed.logs)
        ? parsed.logs.map((log: any) => ({
            ...log,
            timestamp: parseDate(log?.timestamp)
          }))
        : [],
      toolExecutions: Array.isArray(parsed.toolExecutions)
        ? parsed.toolExecutions.map((tool: any) => ({
            ...tool,
            startTime: parseDate(tool?.startTime),
            endTime: tool?.endTime ? parseDate(tool.endTime) : undefined
          }))
        : [],
      fileEdits: Array.isArray(parsed.fileEdits)
        ? parsed.fileEdits.map((edit: any) => ({
            ...edit,
            timestamp: parseDate(edit?.timestamp)
          }))
        : []
    };
  } catch (error) {
    console.warn('Failed to load persisted app state:', error);
    return null;
  }
};

function App() {
  const [state, setState] = useState<AppState>(() => {
    const persisted = loadPersistedAppState();
    return {
      provider: 'unknown',
      model: 'unknown',
      queryCount: 0,
      messages: [],
      logs: [],
      currentView: 'chat',
      toolExecutions: [],
      stats: {},
      fileEdits: [],
      ...persisted,
      isConnected: false,
      isProcessing: false,
      lastError: null,
      queryProgress: null,
    };
  });

  const [inputValue, setInputValue] = useState('');
  const [isMobile, setIsMobile] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [isTerminalExpanded, setIsTerminalExpanded] = useState(false);
  const lastChunkRef = useRef<string>('');
  const isProcessingRef = useRef(false);
  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);
  const [gitRefreshToken, setGitRefreshToken] = useState(0);
  const [selectedGitFilePath, setSelectedGitFilePath] = useState<string | null>(null);

  useEffect(() => {
    if (typeof window === 'undefined' || !window.localStorage) {
      return;
    }

    try {
      window.localStorage.setItem(
        APP_STATE_STORAGE_KEY,
        JSON.stringify({
          provider: state.provider,
          model: state.model,
          queryCount: state.queryCount,
          currentView: state.currentView,
          messages: state.messages.slice(-200),
          logs: state.logs.slice(-300),
          toolExecutions: state.toolExecutions.slice(-200),
          fileEdits: state.fileEdits.slice(-100)
        })
      );
    } catch (error) {
      console.warn('Failed to persist app state:', error);
    }
  }, [
    state.provider,
    state.model,
    state.queryCount,
    state.currentView,
    state.messages,
    state.logs,
    state.toolExecutions,
    state.fileEdits
  ]);

  // Memoize recent logs to prevent unnecessary Sidebar remounts
  const recentLogs = useMemo(() => state.logs.slice(-10), [state.logs]);

  // Memoize stats to prevent unnecessary Sidebar remounts
  const stats = useMemo(() => ({
    queryCount: state.queryCount,
    filesModified: 0 // TODO: track modified files from buffers
  }), [state.queryCount]);

  // Memoize sidebar toggle handler
  const handleSidebarToggle = useCallback(() => {
    setSidebarCollapsed(prev => !prev);
  }, []);

  const wsService = WebSocketService.getInstance();
  const apiService = ApiService.getInstance();

  // Debounce connection status updates to prevent flashing
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef<boolean>(false);

  const handleEvent = useCallback((event: any) => {
    // Filter out ping events and webpack dev server events early to prevent console spam
    const filteredEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'ping'];
    if (filteredEvents.includes(event.type)) {
      return; // Don't process these events
    }

    debugLog('[msg] Received event:', event.type, event.data);

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

        // Debounce connection status updates to prevent rapid re-renders
        const newConnectionState = event.data.connected;

        // Only update if state actually changed
        if (newConnectionState !== lastConnectionStateRef.current) {
          // Clear any pending timeout
          if (connectionTimeoutRef.current) {
            clearTimeout(connectionTimeoutRef.current);
          }

          // Debounce the state update
          connectionTimeoutRef.current = setTimeout(() => {
            lastConnectionStateRef.current = newConnectionState;
            setState(prev => ({
              ...prev,
              isConnected: newConnectionState,
              logs: [...prev.logs, logEntry]
            }));
          }, 300); // Wait 300ms to confirm the connection state is stable
        }
        debugLog('[link] Connection status updated:', newConnectionState);
        break;

      case 'query_started':
        logEntry.category = 'query';
        logEntry.level = 'info';
        const startedQuery = event.data?.query || '';
        lastChunkRef.current = ''; // Reset chunk tracking for new query
        setState(prev => ({
          ...prev,
          isProcessing: true,
          lastError: null,
          queryCount: prev.queryCount + 1,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'user',
            content: startedQuery,
            timestamp: new Date()
          }],
          toolExecutions: [], // Clear previous tool executions
          queryProgress: null, // Clear previous progress
          logs: [...prev.logs, logEntry]
        }));
        debugLog('[>>] Query started:', startedQuery);
        break;

      case 'query_progress':
        setState(prev => ({
          ...prev,
          queryProgress: event.data
        }));
        debugLog('[>>] Query progress:', event.data);
        break;

      case 'stream_chunk':
        logEntry.category = 'stream';
        logEntry.level = 'info';
        
        // Only skip duplicates for non-empty chunks to prevent message duplication
        // Empty chunks (heartbeats) should always be passed through
        if (event.data.chunk && event.data.chunk === lastChunkRef.current) {
          break;
        }
        if (event.data.chunk) {
          lastChunkRef.current = event.data.chunk;
        }
        
        setState(prev => {
          const newMessages = [...prev.messages];
          const lastMessage = newMessages[newMessages.length - 1];
          if (lastMessage && lastMessage.type === 'assistant') {
            // Create new message object instead of mutating
            newMessages[newMessages.length - 1] = {
              ...lastMessage,
              content: lastMessage.content + (event.data.chunk || '')
            };
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
            messages: newMessages
          };
        });
        break;

      case 'query_completed':
        logEntry.category = 'query';
        logEntry.level = 'success';
        isProcessingRef.current = false;
        setState(prev => ({
          ...prev,
          isProcessing: false,
          lastError: null,
          queryProgress: null,
          toolExecutions: prev.toolExecutions.map((tool) => {
            if (tool.status === 'started' || tool.status === 'running') {
              return {
                ...tool,
                status: 'completed',
                endTime: tool.endTime || new Date()
              };
            }
            return tool;
          }),
          logs: [...prev.logs, logEntry]
        }));
        debugLog('[OK] Query completed');
        break;

      case 'tool_execution':
        logEntry.category = 'tool';
        const rawStatus = (event.data?.status || event.data?.action || '').toString().toLowerCase();
        const normalizedStatus: ToolExecution['status'] =
          rawStatus === 'error' || rawStatus === 'failed'
            ? 'error'
            : rawStatus === 'completed' || rawStatus === 'complete' || rawStatus === 'done' || rawStatus === 'success'
              ? 'completed'
              : rawStatus === 'started' || rawStatus === 'start'
                ? 'started'
                : 'running';
        const normalizedToolName = event.data?.tool || event.data?.tool_name || event.data?.name || 'unknown_tool';
        const normalizedMessage = event.data?.message || event.data?.error || undefined;
        const toolCallID = event.data?.tool_call_id || event.data?.id || undefined;
        logEntry.level = normalizedStatus === 'error' ? 'error' : 'info';
        setState(prev => {
          const existingExecution = prev.toolExecutions.find((t) => {
            const existingCallID = t.details?.tool_call_id || t.details?.id;
            if (toolCallID && existingCallID) {
              return existingCallID === toolCallID;
            }
            return t.tool === normalizedToolName && !t.endTime;
          });
          const evArgs = event.data?.arguments != null ? String(event.data.arguments) : undefined;
          const evResult = event.data?.result != null ? String(event.data.result) : undefined;
          const evError = event.data?.error != null ? String(event.data.error) : undefined;
          const isSubagent = normalizedToolName === 'run_subagent' || normalizedToolName === 'run_parallel_subagents';
          const subagentType: ToolExecution['subagentType'] = normalizedToolName === 'run_parallel_subagents' ? 'parallel' : normalizedToolName === 'run_subagent' ? 'single' : undefined;

          // Extract persona from subagent arguments
          let parsedPersona: string | undefined;
          if (isSubagent && evArgs) {
            try {
              const argsObj = JSON.parse(evArgs);
              parsedPersona = typeof argsObj.persona === 'string' ? argsObj.persona : undefined;
            } catch { /* args not parseable */ }
          }

          // Extract summary from subagent result JSON into a readable string
          let subagentSummary: string | undefined;
          if (isSubagent && evResult) {
            try {
              const resultObj = JSON.parse(evResult);
              if (normalizedToolName === 'run_parallel_subagents') {
                // Parallel: collect task IDs and statuses
                const taskSummaries: string[] = [];
                for (const [taskId, taskResult] of Object.entries(resultObj)) {
                  if (typeof taskResult === 'object' && taskResult !== null) {
                    const tr = taskResult as Record<string, unknown>;
                    const exitCode = String(tr.exit_code ?? '?');
                    taskSummaries.push(`[${taskId}] exit ${exitCode}`);
                  }
                }
                subagentSummary = taskSummaries.join('\n');
              } else {
                // Single: use the summary field if present
                if (resultObj.summary) {
                  subagentSummary = typeof resultObj.summary === 'string'
                    ? resultObj.summary
                    : JSON.stringify(resultObj.summary, null, 2);
                }
              }
            } catch { /* result not parseable */ }
          }

          let updatedExecutions: ToolExecution[];
          
          if (existingExecution) {
            // Update existing execution, preserving arguments from start and adding result/error from completion
            updatedExecutions = prev.toolExecutions.map(t => 
              t.id === existingExecution.id
                ? {
                    ...t,
                    status: normalizedStatus,
                    message: normalizedMessage || (evError ? evError : t.message),
                    endTime: normalizedStatus === 'completed' || normalizedStatus === 'error' ? new Date() : undefined,
                    details: event.data || t.details,
                    arguments: t.arguments || evArgs,
                    result: t.result || subagentSummary || evResult || evError,
                    persona: t.persona || parsedPersona,
                    subagentType: t.subagentType || subagentType
                  }
                : t
            );
          } else {
            // Add new execution
            const newExecution: ToolExecution = {
              id: toolCallID ? String(toolCallID) : `${normalizedToolName}-${Date.now()}`,
              tool: normalizedToolName,
              status: normalizedStatus,
              message: normalizedMessage,
              startTime: new Date(),
              endTime: normalizedStatus === 'completed' || normalizedStatus === 'error' ? new Date() : undefined,
              details: event.data,
              arguments: evArgs,
              result: subagentSummary || evResult || evError,
              persona: parsedPersona,
              subagentType
            };
            updatedExecutions = [...prev.toolExecutions, newExecution];
          }
          
          return {
            ...prev,
            toolExecutions: updatedExecutions,
            logs: [...prev.logs, logEntry]
          };
        });
        debugLog('[tool] Tool execution:', normalizedToolName, normalizedStatus);
        break;

      case 'file_changed':
        logEntry.category = 'file';
        logEntry.level = 'info';
        setState(prev => {
          const newLogs = [...prev.logs, logEntry];

          // Track file edits
          const newFileEdit = {
            path: event.data.path || event.data.file_path || 'Unknown',
            action: event.data.action || event.data.operation || 'edited',
            timestamp: new Date(),
            linesAdded: event.data.lines_added,
            linesDeleted: event.data.lines_deleted
          };

          // Add to file edits (keep last 50)
          const updatedFileEdits = [...prev.fileEdits, newFileEdit].slice(-50);

          return { ...prev, logs: newLogs, fileEdits: updatedFileEdits };
        });
        debugLog('[edit] File changed:', event.data.path);
        break;

      case 'terminal_output':
        logEntry.category = 'system';
        logEntry.level = 'info';
        // Handle terminal output - this will be processed by the Terminal component
        setState(prev => ({
          ...prev,
          logs: [...prev.logs, logEntry]
        }));
        debugLog('[term] Terminal output received:', event.data);
        break;

      case 'error':
        logEntry.category = 'system';
        logEntry.level = 'error';
        isProcessingRef.current = false;
        const errorMessage = event.data?.message || 'Unknown error';
        setState(prev => ({
          ...prev,
          isProcessing: false,
          queryProgress: null,
          lastError: errorMessage,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'assistant',
            content: `[FAIL] Error: ${errorMessage}`,
            timestamp: new Date()
          }],
          logs: [...prev.logs, logEntry]
        }));
        console.error('[FAIL] Error event:', event.data);
        break;

      case 'metrics_update':
        logEntry.category = 'system';
        logEntry.level = 'info';
        setState(prev => ({
          ...prev,
          provider: event.data?.provider || prev.provider,
          model: event.data?.model || prev.model,
          stats: {
            ...prev.stats,
            ...event.data
          },
          logs: [...prev.logs, logEntry]
        }));
        break;

      default:
        // Handle any unknown event types
        logEntry.level = 'warning';
        setState(prev => ({
          ...prev,
          logs: [...prev.logs, logEntry]
        }));
        debugLog('[?] Unknown event type:', event.type, event.data);
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
          stats: JSON.stringify(prev.stats) === JSON.stringify(stats) ? prev.stats : stats
        }));
      }).catch(console.error);
    };

    // Load recent files
    const loadFiles = () => {
      apiService.getFiles().then((response: any) => {
        if (response && response.files) {
          // Convert files array to expected format
          const files = response.files.map((file: any) => ({
            path: file.path || file.name,
            modified: false
          }));
          setRecentFiles(files);
        }
      }).catch(console.error);
    };

    // Load initial stats
    loadStats();

    // Load initial files
    loadFiles();

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
      // Clear any pending connection timeout
      if (connectionTimeoutRef.current) {
        clearTimeout(connectionTimeoutRef.current);
      }
      wsService.removeEvent(handleEvent);
      wsService.disconnect();
      window.removeEventListener('resize', checkMobile);
      clearInterval(statsInterval);
    };
  }, [handleEvent, wsService, apiService]);

  // Register content providers
  useEffect(() => {
    viewRegistry.register(new ChatViewProvider());
    viewRegistry.register(new EditorViewProvider());
    viewRegistry.register(new GitViewProvider());
    viewRegistry.register(new LogsViewProvider());

    debugLog('[OK] Content providers registered');
  }, []);

  const handleSendMessage = useCallback(async (message: string) => {
    if (!message.trim() || isProcessingRef.current) return;
    isProcessingRef.current = true;

    // Clear any previous errors and set processing state
    setState(prev => ({
      ...prev,
      isProcessing: true,
      lastError: null
    }));

    try {
      debugLog('[>>] Sending message:', message);
      await apiService.sendQuery(message);
      setInputValue('');
      debugLog('[OK] Message sent successfully');
    } catch (error) {
      console.error('[FAIL] Failed to send message:', error);
      isProcessingRef.current = false;
      const errorMsg = error instanceof Error ? error.message : 'Failed to send message';
      setState(prev => ({
        ...prev,
        isProcessing: false,
        lastError: `Failed to send message: ${errorMsg}`,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMsg}`,
          timestamp: new Date()
        }]
      }));
    }
  }, [apiService]);

  
  const handleModelChange = useCallback((model: string) => {
    debugLog('Model changed to:', model);
    wsService.sendEvent({
      type: 'model_change',
      data: { model }
    });
  }, [wsService]);

  const handleProviderChange = useCallback((provider: string) => {
    debugLog('Provider changed to:', provider);
    wsService.sendEvent({
      type: 'provider_change',
      data: { provider }
    });
  }, [wsService]);

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git' | 'logs') => {
    setState(prev => ({
      ...prev,
      currentView: view
    }));
  }, []);

  const handleGitCommit = useCallback(async (message: string, files: string[]) => {
    debugLog('Git commit:', message, files);
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
      debugLog('Commit successful:', data);
      setGitRefreshToken(k => k + 1);
      return data;
    } catch (err) {
      console.error('Failed to commit:', err);
      throw err;
    }
  }, []);

  const handleGitStage = useCallback(async (files: string[]) => {
    debugLog('Git stage:', files);
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
      setGitRefreshToken(k => k + 1);
    } catch (err) {
      console.error('Failed to stage files:', err);
      throw err;
    }
  }, []);

  const handleGitUnstage = useCallback(async (files: string[]) => {
    debugLog('Git unstage:', files);
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
      setGitRefreshToken(k => k + 1);
    } catch (err) {
      console.error('Failed to unstage files:', err);
      throw err;
    }
  }, []);

  const handleGitDiscard = useCallback(async (files: string[]) => {
    debugLog('Git discard:', files);
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
      setGitRefreshToken(k => k + 1);
    } catch (err) {
      console.error('Failed to discard files:', err);
      throw err;
    }
  }, []);

  const handleTerminalOutput = useCallback((output: string) => {
    // You could handle terminal output here if needed
    debugLog('[term] Terminal output:', output);
  }, []);

  const toggleSidebar = useCallback(() => {
    setIsSidebarOpen(prev => !prev);
  }, []);

  const closeSidebar = useCallback(() => {
    setIsSidebarOpen(false);
  }, []);

  return (
    <ErrorBoundary
      onError={(error, errorInfo) => {
        console.error('Application error:', error, errorInfo);
        // You could send this to an error reporting service here
      }}
    >
      <ThemeProvider>
        <HotkeyProvider>
          <EditorManagerProvider>
            <UIManager>
              <AppContent
                state={state}
                inputValue={inputValue}
                onInputChange={setInputValue}
                isMobile={isMobile}
                isSidebarOpen={isSidebarOpen}
                sidebarCollapsed={sidebarCollapsed}
                isTerminalExpanded={isTerminalExpanded}
                stats={stats}
                recentFiles={recentFiles}
                recentLogs={recentLogs}
                gitRefreshToken={gitRefreshToken}
                onSidebarToggle={handleSidebarToggle}
                onToggleSidebar={toggleSidebar}
                onCloseSidebar={closeSidebar}
                onViewChange={handleViewChange}
                onModelChange={handleModelChange}
                onProviderChange={handleProviderChange}
                onSendMessage={handleSendMessage}
                onGitCommit={handleGitCommit}
                onGitStage={handleGitStage}
                onGitUnstage={handleGitUnstage}
                onGitDiscard={handleGitDiscard}
                selectedGitFilePath={selectedGitFilePath}
                onGitFileSelect={setSelectedGitFilePath}
                onClearLogs={() => setState(prev => ({ ...prev, logs: [] }))}
                onTerminalOutput={handleTerminalOutput}
                onTerminalExpandedChange={setIsTerminalExpanded}
              />
            </UIManager>
          </EditorManagerProvider>
        </HotkeyProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}

export default App;
