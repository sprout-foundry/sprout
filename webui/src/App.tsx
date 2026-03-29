import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import ErrorBoundary from './components/ErrorBoundary';
import AppContent from './components/AppContent';
import UIManager from './components/UIManager';
import { EditorManagerProvider } from './contexts/EditorManagerContext';
import { ThemeProvider } from './contexts/ThemeContext';
import { HotkeyProvider } from './contexts/HotkeyContext';
import './App.css';
import { WebSocketService } from './services/websocket';
import { ApiService, OnboardingEnvironment, OnboardingProviderOption } from './services/api';
import { clientFetch, getWebUIClientId } from './services/clientSession';
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
  sessionId: string | null;
  queryCount: number;
  messages: Message[];
  logs: LogEntry[];
  isProcessing: boolean;
  lastError: string | null;
  currentView: 'chat' | 'editor' | 'git';
  toolExecutions: ToolExecution[];
  queryProgress: any;
  stats: any; // Enhanced stats from API
  currentTodos: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
  fileEdits: Array<{
    path: string;
    action: string;
    timestamp: Date;
    linesAdded?: number;
    linesDeleted?: number;
  }>;
  subagentActivities: SubagentActivity[];
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
  reasoning?: string;  // Chain-of-thought content from content_type: "reasoning"
  toolRefs?: Array<{ toolId: string; toolName: string; label: string }>;
}

interface LogEntry {
  id: string;
  type: string;
  timestamp: Date;
  data: any;
  level: 'info' | 'warning' | 'error' | 'success';
  category: 'query' | 'tool' | 'file' | 'system' | 'stream';
}

interface SubagentActivity {
  id: string;
  toolCallId: string;
  toolName: string;
  phase: 'spawn' | 'output' | 'complete';
  message: string;
  timestamp: Date;
  taskId?: string;
  persona?: string;
  isParallel?: boolean;
  provider?: string;
  model?: string;
  taskCount?: number;
  failures?: number;
}

interface OnboardingState {
  checking: boolean;
  open: boolean;
  reason: string;
  providers: OnboardingProviderOption[];
  environment: OnboardingEnvironment | null;
  provider: string;
  model: string;
  apiKey: string;
  showAllProviders: boolean;
  submitting: boolean;
  platformActionMessage: string | null;
  error: string | null;
}

const APP_STATE_STORAGE_KEY = 'ledit:webui:state:v1';
const INSTANCE_PID_STORAGE_KEY = 'ledit:webui:instancePid';
const INSTANCE_SWITCH_RESET_KEY = 'ledit:webui:instanceSwitchReset';
const MAX_PERSISTED_LOGS = 1000;

const getAppStateStorageKey = (): string => {
  if (typeof window === 'undefined' || !window.localStorage) {
    return `${APP_STATE_STORAGE_KEY}:default`;
  }
  const instancePid = window.localStorage.getItem(INSTANCE_PID_STORAGE_KEY) || 'default';
  return `${APP_STATE_STORAGE_KEY}:${instancePid}`;
};

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

const AGENT_CHAT_LEAK_PATTERNS: RegExp[] = [
  /^\[\d+\s*-\s*\d+%\]\s*executing tool/i,
  /executing tool\s*\[[^\]]+\]/i,
  /\bTodoWrite\b/i,
  /\btodos=\d+/i,
  /\[\s*\]=\d+\s*\[~\]=\d+\s*\[x\]=\d+\s*\[-\]=\d+/i,
  /^Subagent:\s*\[\d+\s*-\s*\d+%\]/i,
];

const shouldSuppressAgentMessageInChat = (message: string): boolean => {
  const line = message.trim();
  if (!line) {
    return true;
  }
  return AGENT_CHAT_LEAK_PATTERNS.some((pattern) => pattern.test(line));
};

const extractToolNameFromToolLogTarget = (target: string): string | null => {
  if (!target) return null;
  const trimmed = target.trim();
  if (!trimmed.startsWith('[') || !trimmed.endsWith(']')) return null;
  const inner = trimmed.slice(1, -1).trim();
  if (!inner) return null;
  const firstToken = inner.split(/\s+/, 1)[0] || '';
  return firstToken || null;
};

const TODO_STATUSES = new Set(['pending', 'in_progress', 'completed', 'cancelled']);

const normalizeTodoList = (
  rawTodos: unknown
): Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }> => {
  if (!Array.isArray(rawTodos)) {
    return [];
  }

  const normalized: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }> = [];
  const seen = new Set<string>();

  rawTodos.forEach((item, idx) => {
    if (!item || typeof item !== 'object') {
      return;
    }

    const t = item as Record<string, unknown>;
    const rawContent = typeof t.content === 'string' ? t.content.trim() : '';
    const rawStatus = typeof t.status === 'string' ? t.status.trim() : '';
    const rawID = typeof t.id === 'string' ? t.id.trim() : '';

    // Strict validation: reject entries that don't look like real todos.
    if (!rawContent || !TODO_STATUSES.has(rawStatus)) {
      return;
    }

    const status = rawStatus as 'pending' | 'in_progress' | 'completed' | 'cancelled';
    const id = rawID || `todo-${idx}-${rawStatus}-${rawContent.slice(0, 48)}`;
    const dedupeKey = `${id}::${status}::${rawContent}`;
    if (seen.has(dedupeKey)) {
      return;
    }
    seen.add(dedupeKey);

    normalized.push({ id, content: rawContent, status });
  });

  return normalized;
};

const loadPersistedAppState = (): Partial<AppState> | null => {
  if (typeof window === 'undefined' || !window.localStorage) {
    return null;
  }

  try {
    if (window.sessionStorage?.getItem(INSTANCE_SWITCH_RESET_KEY) === '1') {
      window.sessionStorage.removeItem(INSTANCE_SWITCH_RESET_KEY);
      window.localStorage.removeItem(getAppStateStorageKey());
      return null;
    }

    const storageKey = getAppStateStorageKey();
    const raw = window.localStorage.getItem(storageKey);
    if (!raw) {
      return null;
    }

    const parsed = JSON.parse(raw);
    return {
      provider: typeof parsed.provider === 'string' ? parsed.provider : 'unknown',
      model: typeof parsed.model === 'string' ? parsed.model : 'unknown',
      sessionId: typeof parsed.sessionId === 'string' ? parsed.sessionId : null,
      queryCount: typeof parsed.queryCount === 'number' ? parsed.queryCount : 0,
      currentView: ['chat', 'editor', 'git'].includes(parsed.currentView) ? parsed.currentView : 'chat',
      messages: Array.isArray(parsed.messages)
        ? parsed.messages.map((message: any) => ({
            ...message,
            timestamp: parseDate(message?.timestamp),
            toolRefs: Array.isArray(message?.toolRefs) ? message.toolRefs : undefined
          }))
        : [],
      fileEdits: Array.isArray(parsed.fileEdits)
        ? parsed.fileEdits.map((edit: any) => ({
            ...edit,
            timestamp: parseDate(edit?.timestamp)
          }))
        : [],
      subagentActivities: []
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
      sessionId: null,
      queryCount: 0,
      messages: [],
      logs: [],
      currentView: 'chat',
      toolExecutions: [],
      stats: {},
      currentTodos: [],
      fileEdits: [],
      subagentActivities: [],
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
  const activeRequestsRef = useRef(0);
  const queuedMessagesRef = useRef<string[]>([]);
  const [queuedMessagesCount, setQueuedMessagesCount] = useState(0);
  const [recentFiles, setRecentFiles] = useState<Array<{ path: string; modified: boolean }>>([]);
  const [gitRefreshToken, setGitRefreshToken] = useState(0);
  const [onboarding, setOnboarding] = useState<OnboardingState>({
    checking: true,
    open: false,
    reason: '',
    providers: [],
    environment: null,
    provider: '',
    model: '',
    apiKey: '',
    showAllProviders: false,
    submitting: false,
    platformActionMessage: null,
    error: null,
  });

  useEffect(() => {
    if (typeof window === 'undefined' || !window.localStorage) {
      return;
    }

    const storageKey = getAppStateStorageKey();
    // Only persist what is needed to restore the chat view. Logs and
    // toolExecutions are ephemeral — they are large and re-populated by
    // the WebSocket stream, so storing them wastes quota unnecessarily.
    const persistPayload = (messageCount: number) => JSON.stringify({
      provider: state.provider,
      model: state.model,
      sessionId: state.sessionId,
      queryCount: state.queryCount,
      currentView: state.currentView,
      messages: state.messages.slice(-messageCount),
      fileEdits: state.fileEdits.slice(-50),
    });
    try {
      window.localStorage.setItem(storageKey, persistPayload(100));
    } catch {
      // QuotaExceededError: retry with fewer messages, then give up gracefully.
      try {
        window.localStorage.setItem(storageKey, persistPayload(20));
      } catch {
        try {
          window.localStorage.removeItem(storageKey);
        } catch { /* nothing more we can do */ }
      }
    }
  }, [
    state.provider,
    state.model,
    state.sessionId,
    state.queryCount,
    state.currentView,
    state.messages,
    state.fileEdits,
  ]);

  // Keep a larger client-side log buffer available to the sidebar logs view.
  const recentLogs = useMemo(() => state.logs.slice(-MAX_PERSISTED_LOGS), [state.logs]);

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

  const selectedOnboardingProvider = useMemo(() => {
    return onboarding.providers.find((p) => p.id === onboarding.provider) || null;
  }, [onboarding.provider, onboarding.providers]);

  const recommendedOnboardingProviders = useMemo(() => {
    return onboarding.providers.filter((p) => p.recommended);
  }, [onboarding.providers]);

  const advancedOnboardingProviders = useMemo(() => {
    return onboarding.providers.filter((p) => !p.recommended);
  }, [onboarding.providers]);

  const windowsOnboardingGuidance = useMemo(() => {
    const env = onboarding.environment;
    if (!env) {
      return null;
    }

    const isWindowsHost = env.host_platform === 'windows' || env.runtime_platform === 'windows';
    if (!isWindowsHost) {
      return null;
    }

    if (env.backend_mode === 'wsl') {
      return {
        tone: 'success',
        title: 'WSL mode is already active',
        body: 'This window is already using a WSL backend, which is the recommended setup for terminals, shell tools, and repo workflows on Windows.',
        checklist: [
          'Keep repos inside the WSL filesystem when practical.',
          'Use native Windows mode only when you specifically need Windows-only tools.',
          env.has_git_bash ? 'Git Bash is also available as a native Windows fallback.' : 'Git Bash is optional and only needed if you plan to use the native Windows backend.',
        ],
        canInstallWsl: false,
        canInstallGitBash: !env.has_git_bash,
      };
    }

    return {
      tone: env.has_wsl ? 'warning' : 'info',
      title: env.has_wsl ? 'Recommended: use WSL for the best Windows experience' : 'Recommended: install WSL before relying on shell-heavy workflows',
      body: env.has_wsl
        ? 'Native Windows mode can handle some tasks, but this app is built around Unix-style terminal behavior. WSL is the intended path.'
        : 'This app expects Unix-style shell and terminal behavior. WSL gives the best compatibility for chat tools, shell commands, and git workflows.',
      checklist: [
        env.has_wsl ? 'Reopen the project through the WSL-backed desktop mode when possible.' : 'Install WSL with an Ubuntu distro, then reopen the project through the WSL-backed desktop mode.',
        env.has_git_bash ? 'Git Bash is installed and can help with native Windows shell commands.' : 'Install Git for Windows if you want Git Bash as a native-Windows fallback for shell commands.',
        'Expect the native Windows backend to be less complete than the WSL path for terminal behavior.',
      ],
      canInstallWsl: !env.has_wsl,
      canInstallGitBash: !env.has_git_bash,
    };
  }, [onboarding.environment]);

  // Debounce connection status updates to prevent flashing
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef<boolean>(false);

  const refreshOnboardingStatus = useCallback(async () => {
    setOnboarding((prev) => ({ ...prev, checking: true, error: null }));
    try {
      const status = await apiService.getOnboardingStatus();
      const providers = Array.isArray(status.providers) ? status.providers : [];
      const preferredProvider = status.current_provider
        || providers.find((p) => p.recommended)?.id
        || providers[0]?.id
        || '';
      const providerInfo = providers.find((p) => p.id === preferredProvider) || providers[0];
      const preferredModel = status.current_model || providerInfo?.recommended_model || providerInfo?.models?.[0] || '';
      setOnboarding({
        checking: false,
        open: !!status.setup_required,
        reason: status.reason || '',
        providers,
        environment: status.environment || null,
        provider: preferredProvider,
        model: preferredModel,
        apiKey: '',
        showAllProviders: false,
        submitting: false,
        platformActionMessage: null,
        error: null,
      });
    } catch (error) {
      setOnboarding((prev) => ({
        ...prev,
        checking: false,
        open: true,
        showAllProviders: false,
        platformActionMessage: null,
        error: error instanceof Error ? error.message : 'Failed to check setup status',
      }));
    }
  }, [apiService]);

  const pendingProviderRef = useRef<string>(state.provider);

  useEffect(() => {
    pendingProviderRef.current = state.provider;
  }, [state.provider]);

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
        if (event.data?.client_id && event.data.client_id !== getWebUIClientId()) {
          break;
        }
        logEntry.category = 'system';
        logEntry.level = event.data.connected ? 'success' : 'warning';
        const incomingSessionId = typeof event.data?.session_id === 'string' ? event.data.session_id : null;

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
              // NOTE:
              // WebSocket `session_id` is a transport connection id (ws_<timestamp>),
              // not a chat session id. It changes on reconnect and must never clear chat state.
              sessionId: prev.sessionId || incomingSessionId,
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
          fileEdits: [],      // Clear previous file edits for current-run status metrics
          subagentActivities: [],
          queryProgress: null, // Clear previous progress
          currentTodos: [],    // Clear previous todos
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
        
        const chunkContent = event.data.chunk || '';
        const chunkType = event.data.content_type || 'assistant_text';
        
        setState(prev => {
          const newMessages = [...prev.messages];
          const lastMessage = newMessages[newMessages.length - 1];
          if (lastMessage && lastMessage.type === 'assistant') {
            if (chunkType === 'reasoning') {
              // Append to reasoning field
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                reasoning: (lastMessage.reasoning || '') + chunkContent
              };
            } else {
              // Append to content field (default behavior)
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                content: lastMessage.content + chunkContent
              };
            }
          } else {
            // Create new assistant message
            const newMsg: Message = {
              id: Date.now().toString(),
              type: 'assistant',
              content: chunkType === 'reasoning' ? '' : chunkContent,
              timestamp: new Date(),
            };
            if (chunkType === 'reasoning') {
              newMsg.reasoning = chunkContent;
            }
            newMessages.push(newMsg);
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
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const completedQuery = String(event.data?.query || '').trim().toLowerCase();
        const wasClearCommand = completedQuery === '/clear';
        if (wasClearCommand) {
          queuedMessagesRef.current = [];
          setQueuedMessagesCount(0);
        }
        setState(prev => ({
          ...prev,
          messages: wasClearCommand ? [] : prev.messages,
          currentTodos: wasClearCommand ? [] : prev.currentTodos,
          isProcessing: activeRequestsRef.current > 0,
          lastError: null,
          queryProgress: null,
          toolExecutions: wasClearCommand
            ? []
            : prev.toolExecutions.map((tool) => {
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

      case 'tool_start':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        setState(prev => {
          const toolCallID = String(event.data?.tool_call_id || '');
          const toolName = String(event.data?.tool_name || 'unknown_tool');
          const rawArgs = event.data?.arguments != null ? String(event.data.arguments) : undefined;
          const displayName = String(event.data?.display_name || toolName);
          const persona = typeof event.data?.persona === 'string' ? event.data.persona : undefined;
          const isSubagent = !!event.data?.is_subagent;
          const subagentType: ToolExecution['subagentType'] = event.data?.subagent_type === 'parallel'
            ? 'parallel'
            : isSubagent ? 'single' : undefined;

          // Check if we already have this tool from a legacy tool_execution event
          const existingIdx = prev.toolExecutions.findIndex(t => {
            const existingID = t.details?.tool_call_id || t.details?.id || t.id;
            return toolCallID && existingID === toolCallID;
          });

          if (existingIdx >= 0) {
            // Update existing with richer start data
            const updated = [...prev.toolExecutions];
            updated[existingIdx] = {
              ...updated[existingIdx],
              tool: toolName,
              status: 'started',
              startTime: updated[existingIdx].startTime, // keep existing start time
              message: displayName,
              arguments: updated[existingIdx].arguments || rawArgs,
              details: event.data,
              persona: updated[existingIdx].persona || persona,
              subagentType: updated[existingIdx].subagentType || subagentType,
            };
            const messages = [...prev.messages];
            for (let i = messages.length - 1; i >= 0; i -= 1) {
              const msg = messages[i];
              if (msg.type !== 'assistant') continue;
              const toolRefs = Array.isArray(msg.toolRefs) ? [...msg.toolRefs] : [];
              if (!toolRefs.some((ref) => ref.toolId === updated[existingIdx].id)) {
                toolRefs.push({
                  toolId: updated[existingIdx].id,
                  toolName,
                  label: displayName,
                });
                messages[i] = { ...msg, toolRefs };
              }
              break;
            }
            return { ...prev, messages, toolExecutions: updated, logs: [...prev.logs, logEntry] };
          }

          // Add new tool execution from rich start event
          const newTool: ToolExecution = {
            id: toolCallID || `${toolName}-${Date.now()}`,
            tool: toolName,
            status: 'started',
            message: displayName,
            startTime: new Date(),
            details: event.data,
            arguments: rawArgs,
            persona,
            subagentType,
          };
          const messages = [...prev.messages];
          for (let i = messages.length - 1; i >= 0; i -= 1) {
            const msg = messages[i];
            if (msg.type !== 'assistant') continue;
            const toolRefs = Array.isArray(msg.toolRefs) ? [...msg.toolRefs] : [];
            if (!toolRefs.some((ref) => ref.toolId === newTool.id)) {
              toolRefs.push({
                toolId: newTool.id,
                toolName,
                label: displayName,
              });
              messages[i] = { ...msg, toolRefs };
            }
            break;
          }

          return {
            ...prev,
            messages,
            toolExecutions: [...prev.toolExecutions, newTool],
            logs: [...prev.logs, logEntry]
          };
        });
        debugLog('[tool] Tool start:', event.data?.tool_name);
        break;

      case 'tool_end':
        logEntry.category = 'tool';
        logEntry.level = event.data?.status === 'failed' ? 'error' : 'info';
        setState(prev => {
          const toolCallID = String(event.data?.tool_call_id || '');
          const status: ToolExecution['status'] = event.data?.status === 'failed' ? 'error' : 'completed';
          const result = event.data?.result != null ? String(event.data.result) : undefined;
          const error = event.data?.error != null ? String(event.data.error) : undefined;

          let matched = false;
          const updatedExecutions = prev.toolExecutions.map(t => {
            const existingID = t.details?.tool_call_id || t.id;
            const match = toolCallID && existingID === toolCallID;
            if (!match) {
              // Also try matching by tool name + no end time (for backward compat)
              const nameMatch = !toolCallID && t.tool === event.data?.tool_name && !t.endTime;
              if (!nameMatch) return t;
            }
            matched = true;

            return {
              ...t,
              status,
              endTime: new Date(),
              result: t.result || result || error,
              details: event.data,
              arguments: t.arguments,  // preserve arguments from tool_start
            };
          });

          if (!matched) {
            const fallbackExecution: ToolExecution = {
              id: toolCallID || `${event.data?.tool_name || 'tool'}-${Date.now()}`,
              tool: String(event.data?.tool_name || 'unknown_tool'),
              status,
              message: String(event.data?.display_name || event.data?.tool_name || 'Tool'),
              startTime: new Date(),
              endTime: new Date(),
              details: event.data,
              arguments: event.data?.arguments != null ? String(event.data.arguments) : undefined,
              result: result || error,
            };
            return {
              ...prev,
              toolExecutions: [...prev.toolExecutions, fallbackExecution],
              logs: [...prev.logs, logEntry]
            };
          }

          return { ...prev, toolExecutions: updatedExecutions, logs: [...prev.logs, logEntry] };
        });
        debugLog('[tool] Tool end:', event.data?.tool_name, event.data?.status);
        break;

      case 'subagent_activity':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        setState(prev => {
          const activity: SubagentActivity = {
            id: String(event.id || `${Date.now()}-${Math.random()}`),
            toolCallId: String(event.data?.tool_call_id || ''),
            toolName: String(event.data?.tool_name || 'run_subagent'),
            phase: event.data?.phase === 'spawn' || event.data?.phase === 'complete' ? event.data.phase : 'output',
            message: String(event.data?.message || '').trim(),
            timestamp: new Date(),
            taskId: typeof event.data?.task_id === 'string' ? event.data.task_id : undefined,
            persona: typeof event.data?.persona === 'string' ? event.data.persona : undefined,
            isParallel: event.data?.is_parallel === true,
            provider: typeof event.data?.provider === 'string' ? event.data.provider : undefined,
            model: typeof event.data?.model === 'string' ? event.data.model : undefined,
            taskCount: typeof event.data?.task_count === 'number' ? event.data.task_count : undefined,
            failures: typeof event.data?.failures === 'number' ? event.data.failures : undefined,
          };

          if (!activity.message) {
            return { ...prev, logs: [...prev.logs, logEntry] };
          }

          return {
            ...prev,
            subagentActivities: [...prev.subagentActivities, activity].slice(-500),
            logs: [...prev.logs, logEntry]
          };
        });
        break;

      case 'agent_message':
        {
          // Handle agent system messages from the backend
          let category = String(event.data?.category || 'info');
          const message = String(event.data?.message || '');

          // Clean ANSI codes from the message
          const cleanedMsg = message.replace(new RegExp(String.fromCharCode(27) + '\\[[0-9;]*[mGKHJABCD]', 'g'), '').trim();
          const suppressInChat = shouldSuppressAgentMessageInChat(cleanedMsg);

          // Auto-classify info messages by content pattern so important ones render in chat
          if (category === 'info') {
            if (/^\[FAIL\]|\[!!\]/.test(cleanedMsg)) {
              category = 'error';
            } else if (/^\[WARN\]|\[~\]|\[!\]/.test(cleanedMsg)) {
              category = 'warning';
            } else if (/^\[OK\]|\[edit\]|\[chart\]/.test(cleanedMsg)) {
              category = 'info_rendered'; // meaningful info that should render
            }
          }

          if (category === 'tool_log' && cleanedMsg) {
            // Tool logs are operational breadcrumbs from the router.
            // Do not create synthetic tool execution rows from these logs; rich
            // tool_start/tool_end events are the source of truth for tool state.
            logEntry.category = 'tool';
            logEntry.level = 'info';

            const toolAction = String(event.data?.action || 'tool');
            const toolTarget = String(event.data?.target || '');
            const parsedToolName = extractToolNameFromToolLogTarget(toolTarget);

            setState(prev => {
              // Best effort: if this log says a tool is executing, mark its
              // most recent started row as running (without adding a duplicate row).
              if (/^executing tool$/i.test(toolAction) && parsedToolName) {
                let touched = false;
                const updated = [...prev.toolExecutions];
                for (let i = updated.length - 1; i >= 0; i--) {
                  const row = updated[i];
                  if (row.tool !== parsedToolName || row.endTime) continue;
                  if (row.status !== 'running') {
                    updated[i] = { ...row, status: 'running' };
                  }
                  touched = true;
                  break;
                }
                if (touched) {
                  return { ...prev, toolExecutions: updated, logs: [...prev.logs, logEntry] };
                }
              }

              return { ...prev, logs: [...prev.logs, logEntry] };
            });
          } else if ((category === 'warning' || category === 'error') && !suppressInChat) {
            // Warning/error messages are operational notices, not model reasoning.
            logEntry.category = 'system';
            logEntry.level = category === 'error' ? 'error' : 'warning';

            setState(prev => {
              const newMessages = [...prev.messages];
              const lastMessage = newMessages[newMessages.length - 1];
              if (lastMessage && lastMessage.type === 'assistant') {
                const prefixedMsg = category === 'error'
                  ? `\n\nWarning: ${cleanedMsg}`
                  : `\n\nNote: ${cleanedMsg}`;
                newMessages[newMessages.length - 1] = {
                  ...lastMessage,
                  content: (lastMessage.content || '') + prefixedMsg
                };
              }
              return { ...prev, messages: newMessages, logs: [...prev.logs, logEntry] };
            });
          } else if (category === 'info_rendered' && cleanedMsg && !suppressInChat) {
            // Meaningful info messages should render in chat, but not inside reasoning.
            logEntry.category = 'system';
            logEntry.level = 'info';

            setState(prev => {
              const newMessages = [...prev.messages];
              const lastMessage = newMessages[newMessages.length - 1];
              if (lastMessage && lastMessage.type === 'assistant') {
                newMessages[newMessages.length - 1] = {
                  ...lastMessage,
                  content: (lastMessage.content || '') + `\n\nInfo: ${cleanedMsg}`
                };
              }
              return { ...prev, messages: newMessages, logs: [...prev.logs, logEntry] };
            });
          }
          // For plain 'info' (unclassified): silently skip rendering in WebUI.
          // These include blank lines, iteration markers, context pruning messages, etc.
          // The meaningful assistant content comes through stream_chunk events.
          break;
        }

      case 'todo_update':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        const normalizedTodos = normalizeTodoList(event.data?.todos);
        setState(prev => ({
          ...prev,
          currentTodos: normalizedTodos,
          logs: [...prev.logs, logEntry]
        }));
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
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const errorMessage = event.data?.message || 'Unknown error';
        setState(prev => ({
          ...prev,
          isProcessing: activeRequestsRef.current > 0,
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

      case 'workspace_changed':
        logEntry.category = 'system';
        logEntry.level = 'info';
        debugLog('[workspace] Workspace changed:', event.data);
        if (!event.data?.client_id || event.data.client_id === getWebUIClientId()) {
          window.location.reload();
        }
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
    refreshOnboardingStatus().catch(() => {});
  }, [refreshOnboardingStatus]);

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

  // Listen for session-restored events from Chat.tsx to populate messages
  useEffect(() => {
    const handleSessionRestored = (event: Event) => {
      const customEvent = event as CustomEvent<{ messages: Array<{ role: string; content: string }> }>;
      const rawMessages = customEvent.detail?.messages;
      if (!Array.isArray(rawMessages)) return;

      // Map backend Message format { role, content } to frontend Message format { id, type, content, timestamp }
      // Only include user and assistant messages (skip system/tool)
      const restoredMessages: Message[] = rawMessages
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => ({
          id: `restored-${i}`,
          type: m.role as 'user' | 'assistant',
          content: typeof m.content === 'string' ? m.content : '',
          timestamp: new Date()
        }));

      if (restoredMessages.length > 0) {
        setState(prev => ({
          ...prev,
          messages: restoredMessages,
          toolExecutions: [],
          fileEdits: [],
          subagentActivities: [],
          currentTodos: [],
          queryProgress: null,
          lastError: null,
          isProcessing: false,
        }));
      }
    };

    window.addEventListener('ledit:session-restored', handleSessionRestored);
    return () => window.removeEventListener('ledit:session-restored', handleSessionRestored);
  }, []);

  const handleSendMessage = useCallback(async (message: string, options?: { allowConcurrent?: boolean }) => {
    if (!message.trim()) return;
    const trimmedMessage = message.trim();
    const allowConcurrent = options?.allowConcurrent === true;
    if (!allowConcurrent && activeRequestsRef.current > 0) {
      setState(prev => ({
        ...prev,
        lastError: null,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'user',
          content: trimmedMessage,
          timestamp: new Date()
        }]
      }));
      await apiService.steerQuery(trimmedMessage);
      setInputValue('');
      return;
    }
    activeRequestsRef.current += 1;

    // Clear any previous errors and set processing state
    setState(prev => ({
      ...prev,
      isProcessing: true,
      lastError: null
    }));

    try {
      debugLog('[>>] Sending message:', trimmedMessage);
      await apiService.sendQuery(trimmedMessage);
      setInputValue('');
      debugLog('[OK] Message sent successfully');
    } catch (error) {
      console.error('[FAIL] Failed to send message:', error);
      if (activeRequestsRef.current > 0) {
        activeRequestsRef.current -= 1;
      }
      const errorMsg = error instanceof Error ? error.message : 'Failed to send message';
      setState(prev => ({
        ...prev,
        isProcessing: activeRequestsRef.current > 0,
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

  const handleQueueMessage = useCallback((message: string) => {
    const trimmed = message.trim();
    if (!trimmed) return;
    queuedMessagesRef.current.push(trimmed);
    setQueuedMessagesCount(queuedMessagesRef.current.length);
  }, []);

  const handleStopProcessing = useCallback(async () => {
    try {
      await apiService.stopQuery();
      setState(prev => ({
        ...prev,
        lastError: null,
      }));
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : 'Failed to stop query';
      setState(prev => ({
        ...prev,
        lastError: errorMsg,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMsg}`,
          timestamp: new Date()
        }]
      }));
    }
  }, [apiService]);

  useEffect(() => {
    if (state.isProcessing || activeRequestsRef.current > 0) {
      return;
    }
    if (queuedMessagesRef.current.length === 0) {
      return;
    }

    const next = queuedMessagesRef.current.shift();
    setQueuedMessagesCount(queuedMessagesRef.current.length);
    if (!next) return;

    handleSendMessage(next).catch((error) => {
      const errorMsg = error instanceof Error ? error.message : 'Failed to send queued message';
      setState(prev => ({
        ...prev,
        lastError: `Failed to send queued message: ${errorMsg}`,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMsg}`,
          timestamp: new Date()
        }]
      }));
    });
  }, [state.isProcessing, handleSendMessage]);

  const handleOnboardingProviderChange = useCallback((providerID: string) => {
    setOnboarding((prev) => {
      const provider = prev.providers.find((p) => p.id === providerID);
      return {
        ...prev,
        provider: providerID,
        model: provider?.recommended_model || provider?.models?.[0] || '',
        apiKey: '',
        error: null,
      };
    });
  }, []);

  const handleCompleteOnboarding = useCallback(async () => {
    if (!onboarding.provider) {
      setOnboarding((prev) => ({ ...prev, error: 'Select a provider first.' }));
      return;
    }
    if (selectedOnboardingProvider?.requires_api_key && !selectedOnboardingProvider.has_credential && !onboarding.apiKey.trim()) {
      setOnboarding((prev) => ({ ...prev, error: 'API key is required for this provider.' }));
      return;
    }

    setOnboarding((prev) => ({ ...prev, submitting: true, error: null }));
    try {
      const response = await apiService.completeOnboarding({
        provider: onboarding.provider,
        model: onboarding.model || undefined,
        api_key: onboarding.apiKey.trim() || undefined,
      });
      setState((prev) => ({
        ...prev,
        provider: response.provider || prev.provider,
        model: response.model || prev.model,
      }));
      setOnboarding((prev) => ({
        ...prev,
        open: false,
        submitting: false,
        apiKey: '',
      }));
    } catch (error) {
      setOnboarding((prev) => ({
        ...prev,
        submitting: false,
        error: error instanceof Error ? error.message : 'Failed to complete setup',
      }));
    }
  }, [apiService, onboarding.apiKey, onboarding.model, onboarding.provider, selectedOnboardingProvider]);

  const handleInstallWsl = useCallback(async () => {
    const desktopBridge = (window as any).leditDesktop;
    if (!desktopBridge?.installWsl) {
      setOnboarding((prev) => ({ ...prev, platformActionMessage: 'WSL installation is only available from the desktop app.' }));
      return;
    }
    const result = await desktopBridge.installWsl();
    setOnboarding((prev) => ({ ...prev, platformActionMessage: result?.message || 'Started WSL setup.' }));
  }, []);

  const handleInstallGitBash = useCallback(async () => {
    const desktopBridge = (window as any).leditDesktop;
    if (!desktopBridge?.installGitForWindows) {
      setOnboarding((prev) => ({ ...prev, platformActionMessage: 'Git Bash installation is only available from the desktop app.' }));
      return;
    }
    const result = await desktopBridge.installGitForWindows();
    setOnboarding((prev) => ({ ...prev, platformActionMessage: result?.message || 'Started Git for Windows setup.' }));
  }, []);

  
  const handleModelChange = useCallback((model: string) => {
    debugLog('Model changed to:', model);
    const provider = pendingProviderRef.current || state.provider;
    setState(prev => ({
      ...prev,
      model
    }));
    wsService.sendEvent({
      type: 'model_change',
      data: { provider, model }
    });
  }, [state.provider, wsService]);

  const handleProviderChange = useCallback((provider: string) => {
    debugLog('Provider changed to:', provider);
    pendingProviderRef.current = provider;
    setState(prev => ({
      ...prev,
      provider
    }));
    wsService.sendEvent({
      type: 'provider_change',
      data: { provider }
    });
  }, [wsService]);

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git') => {
    setState(prev => ({
      ...prev,
      currentView: view
    }));
  }, []);

  const handleGitCommit = useCallback(async (message: string, files: string[]) => {
    debugLog('Git commit:', message, files);
    try {
      const response = await clientFetch('/api/git/commit', {
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

  const handleGitAICommit = useCallback(async (): Promise<{ commitMessage: string; warnings?: string[] }> => {
    const response = await apiService.generateCommitMessage();
    return {
      commitMessage: response.commit_message || '',
      warnings: response.warnings || [],
    };
  }, [apiService]);

  const handleGitStage = useCallback(async (files: string[]) => {
    debugLog('Git stage:', files);
    try {
      for (const file of files) {
        const response = await clientFetch('/api/git/stage', {
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
        const response = await clientFetch('/api/git/unstage', {
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
        const response = await clientFetch('/api/git/discard', {
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
                onQueueMessage={handleQueueMessage}
                onStopProcessing={handleStopProcessing}
                queuedMessagesCount={queuedMessagesCount}
                onGitCommit={handleGitCommit}
                onGitAICommit={handleGitAICommit}
                onGitStage={handleGitStage}
                onGitUnstage={handleGitUnstage}
                onGitDiscard={handleGitDiscard}
                onTerminalOutput={handleTerminalOutput}
                onTerminalExpandedChange={setIsTerminalExpanded}
                isConnected={state.isConnected}
              />
              {onboarding.open && (
                <div className="onboarding-overlay" role="dialog" aria-modal="true" aria-label="Set up ledit">
                  <div className="onboarding-card">
                    <h2>Set Up Ledit</h2>
                    <p>
                      {onboarding.reason === 'missing_provider_credential'
                        ? 'The selected provider is missing credentials.'
                        : 'Choose a provider and model to get started.'}
                    </p>

                    {windowsOnboardingGuidance && (
                      <div className={`onboarding-platform-panel ${windowsOnboardingGuidance.tone}`}>
                        <div className="onboarding-platform-title">{windowsOnboardingGuidance.title}</div>
                        <div className="onboarding-platform-body">{windowsOnboardingGuidance.body}</div>
                        <ul className="onboarding-platform-list">
                          {windowsOnboardingGuidance.checklist.map((item) => (
                            <li key={item}>{item}</li>
                          ))}
                        </ul>
                        <div className="onboarding-platform-actions">
                          {windowsOnboardingGuidance.canInstallWsl && (
                            <button
                              type="button"
                              className="onboarding-platform-btn"
                              onClick={handleInstallWsl}
                              disabled={onboarding.submitting || onboarding.checking}
                            >
                              Install WSL
                            </button>
                          )}
                          {windowsOnboardingGuidance.canInstallGitBash && (
                            <button
                              type="button"
                              className="onboarding-platform-btn"
                              onClick={handleInstallGitBash}
                              disabled={onboarding.submitting || onboarding.checking}
                            >
                              Install Git Bash
                            </button>
                          )}
                        </div>
                        <div className="onboarding-provider-links onboarding-platform-links">
                          <a href="https://learn.microsoft.com/windows/wsl/install" target="_blank" rel="noreferrer">Install WSL</a>
                          <a href="https://gitforwindows.org/" target="_blank" rel="noreferrer">Install Git Bash</a>
                        </div>
                      </div>
                    )}

                    <div className="onboarding-step-title">1. Choose an inference provider</div>
                    <div className="onboarding-provider-grid">
                      {recommendedOnboardingProviders.map((providerOption) => (
                        <button
                          key={providerOption.id}
                          type="button"
                          className={`onboarding-provider-card ${onboarding.provider === providerOption.id ? 'selected' : ''}`}
                          onClick={() => handleOnboardingProviderChange(providerOption.id)}
                          disabled={onboarding.submitting || onboarding.checking}
                        >
                          <span className="onboarding-provider-name">{providerOption.name}</span>
                        </button>
                      ))}
                    </div>

                    {advancedOnboardingProviders.length > 0 && (
                      <>
                        <button
                          type="button"
                          className="onboarding-toggle-btn"
                          onClick={() => setOnboarding((prev) => ({ ...prev, showAllProviders: !prev.showAllProviders }))}
                          disabled={onboarding.submitting || onboarding.checking}
                        >
                          {onboarding.showAllProviders ? 'Hide other providers' : 'Show other providers'}
                        </button>

                        {onboarding.showAllProviders && (
                          <>
                            <label htmlFor="onboarding-provider">Other Providers</label>
                            <select
                              id="onboarding-provider"
                              value={onboarding.provider}
                              onChange={(e) => handleOnboardingProviderChange(e.target.value)}
                              disabled={onboarding.submitting || onboarding.checking}
                            >
                              {onboarding.providers.map((p) => (
                                <option key={p.id} value={p.id}>
                                  {p.name}
                                  {p.requires_api_key && !p.has_credential ? ' (API key required)' : ''}
                                </option>
                              ))}
                            </select>
                          </>
                        )}
                      </>
                    )}

                    {selectedOnboardingProvider && (
                      <div className="onboarding-provider-summary">
                        <div className="onboarding-provider-summary-title">{selectedOnboardingProvider.name}</div>
                        <div className="onboarding-provider-summary-body">{selectedOnboardingProvider.setup_hint || selectedOnboardingProvider.description}</div>
                        <div className="onboarding-provider-links">
                          {selectedOnboardingProvider.docs_url && (
                            <a href={selectedOnboardingProvider.docs_url} target="_blank" rel="noreferrer">Docs</a>
                          )}
                          {selectedOnboardingProvider.signup_url && (
                            <a href={selectedOnboardingProvider.signup_url} target="_blank" rel="noreferrer">Get API access</a>
                          )}
                        </div>
                      </div>
                    )}

                    <div className="onboarding-step-title">2. Choose a model</div>
                    <label htmlFor="onboarding-model">Model</label>
                    <input
                      id="onboarding-model"
                      value={onboarding.model}
                      onChange={(e) => setOnboarding((prev) => ({ ...prev, model: e.target.value }))}
                      placeholder="Enter model name"
                      list="onboarding-models"
                      disabled={onboarding.submitting || onboarding.checking}
                    />
                    <datalist id="onboarding-models">
                      {(selectedOnboardingProvider?.models || []).map((modelName) => (
                        <option key={modelName} value={modelName} />
                      ))}
                    </datalist>

                    {selectedOnboardingProvider?.recommended_model && (
                      <div className="onboarding-note">
                        Recommended model: <strong>{selectedOnboardingProvider.recommended_model}</strong>
                        {selectedOnboardingProvider.recommended_model_why ? ` — ${selectedOnboardingProvider.recommended_model_why}` : ''}
                        {onboarding.model !== selectedOnboardingProvider.recommended_model && (
                          <>
                            {' '}
                            <button
                              type="button"
                              className="onboarding-inline-action"
                              onClick={() => setOnboarding((prev) => ({ ...prev, model: selectedOnboardingProvider.recommended_model, error: null }))}
                              disabled={onboarding.submitting || onboarding.checking}
                            >
                              Use recommended model
                            </button>
                          </>
                        )}
                      </div>
                    )}

                    {selectedOnboardingProvider?.requires_api_key && !selectedOnboardingProvider?.has_credential && (
                      <>
                        <div className="onboarding-step-title">3. Add your API key</div>
                        <label htmlFor="onboarding-api-key">{selectedOnboardingProvider.api_key_label || 'API Key'}</label>
                        <input
                          id="onboarding-api-key"
                          type="password"
                          value={onboarding.apiKey}
                          onChange={(e) => setOnboarding((prev) => ({ ...prev, apiKey: e.target.value }))}
                          placeholder="Paste API key"
                          disabled={onboarding.submitting || onboarding.checking}
                        />
                        {selectedOnboardingProvider.api_key_help && (
                          <div className="onboarding-help">{selectedOnboardingProvider.api_key_help}</div>
                        )}
                      </>
                    )}

                    {selectedOnboardingProvider?.requires_api_key && selectedOnboardingProvider?.has_credential && (
                      <div className="onboarding-note">Credential already configured for this provider.</div>
                    )}

                    {onboarding.error && <div className="onboarding-error">{onboarding.error}</div>}
                    {onboarding.platformActionMessage && <div className="onboarding-help">{onboarding.platformActionMessage}</div>}

                    <div className="onboarding-actions">
                      <button
                        type="button"
                        onClick={refreshOnboardingStatus}
                        disabled={onboarding.submitting}
                      >
                        Refresh
                      </button>
                      <button
                        type="button"
                        className="primary"
                        onClick={handleCompleteOnboarding}
                        disabled={onboarding.submitting || onboarding.checking}
                      >
                        {onboarding.submitting ? 'Saving...' : 'Complete Setup'}
                      </button>
                    </div>
                  </div>
                </div>
              )}
            </UIManager>
          </EditorManagerProvider>
        </HotkeyProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}

export default App;
