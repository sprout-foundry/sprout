import React, {
  useState,
  useEffect,
  useCallback,
  useRef,
  useMemo,
  type ReactNode,
  useImperativeHandle,
  forwardRef,
} from 'react';
import {
  Wrench,
  History,
  ListTodo,
  Clock,
  Activity,
  Terminal,
  BookOpen,
  Pencil,
  Search,
  Eye,
  FlaskConical,
  Globe,
  ArrowDown,
  ClipboardList,
  ScrollText,
  RotateCcw,
  Rocket,
  Zap,
  CheckCircle2,
  XCircle,
  Hourglass,
  Bot,
  BarChart3,
  FileText,
  PanelRightOpen,
  PanelRightClose,
  ChevronDown,
  ChevronRight,
  FileCode,
  FileEdit,
} from 'lucide-react';
import './ContextPanel.css';
import { showThemedConfirm } from './ThemedDialog';
import TodoPanel from './TodoPanel';
import { ApiService } from '../services/api';
import { stripAnsiCodes } from '../utils/ansi';
import { getSubagentResultPreview, formatToolDetail } from '../utils/resultSummary';
import RevisionListPanel from './RevisionListPanel';

interface ToolExecution {
  id: string;
  tool: string;
  status: 'started' | 'running' | 'completed' | 'error';
  message?: string;
  startTime: Date;
  endTime?: Date;
  details?: unknown;
  arguments?: string;
  result?: string;
  persona?: string;
  subagentType?: 'single' | 'parallel';
}

interface LogEntry {
  id: string;
  type: string;
  timestamp: Date;
  data: unknown;
  level: 'info' | 'warning' | 'error' | 'success';
  category: 'query' | 'tool' | 'file' | 'system' | 'stream';
}

interface SubagentActivity {
  id: string;
  toolCallId: string;
  toolName: string;
  phase: 'spawn' | 'output' | 'complete' | 'step';
  message: string;
  timestamp: Date;
  taskId?: string;
  persona?: string;
  isParallel?: boolean;
  provider?: string;
  model?: string;
  taskCount?: number;
  failures?: number;
  tool?: string;
}

interface RevisionFile {
  file_revision_hash?: string;
  path: string;
  operation: string;
  lines_added: number;
  lines_deleted: number;
}

interface Revision {
  revision_id: string;
  timestamp: string;
  files: RevisionFile[];
  description: string;
}

interface RevisionDetailFile extends RevisionFile {
  original_code: string;
  new_code: string;
  diff: string;
}

interface SessionEntry {
  session_id: string;
  name: string;
  working_directory: string;
  last_updated: string;
  message_count: number;
  total_tokens: number;
}

interface StatusMetrics {
  userMsgs: number;
  assistantMsgs: number;
  totalMsgs: number;
  completedTools: number;
  failedTools: number;
  activeTools: number;
  totalTools: number;
  totalAdditions: number;
  totalDeletions: number;
  filesTouched: number;
  topTools: Array<[string, number]>;
  maxToolCount: number;
  duration: number;
}

// ── Props ──────────────────────────────────────────────────────────

interface ContextPanelBaseProps {
  className?: string;
  style?: React.CSSProperties;
  isMobileLayout?: boolean;
  panelWidth?: number;
  onPanelWidthChange?: (width: number) => void;
  onMobileOpenChange?: (open: boolean) => void;
}

// Chat-context specific props
interface ChatContextPanelProps extends ContextPanelBaseProps {
  context: 'chat';
  toolExecutions: ToolExecution[];
  fileEdits: Array<{
    path: string;
    action: string;
    timestamp: Date;
    linesAdded?: number;
    linesDeleted?: number;
  }>;
  logs: LogEntry[];
  subagentActivities: SubagentActivity[];
  currentTodos: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: unknown;
  stats?: {
    provider?: string;
    model?: string;
    total_tokens?: number;
    prompt_tokens?: number;
    completion_tokens?: number;
    cached_tokens?: number;
    current_context_tokens?: number;
    max_context_tokens?: number;
    context_usage_percent?: number;
    cache_efficiency?: number;
    total_cost?: number;
    cached_cost_savings?: number;
    last_tps?: number;
    current_iteration?: number;
    max_iterations?: number;
    streaming_enabled?: boolean;
    debug_mode?: boolean;
    context_warning_issued?: boolean;
    uptime?: string;
    query_count?: number;
  };
  onHandleToolPillClick?: (toolId: string) => void;
  onOpenRevisionDiff?: (options: { path: string; diff: string; title: string }) => void;
}

export type ContextPanelProps = ChatContextPanelProps;

// ── Public API via ref ─────────────────────────────────────────────

export interface ContextPanelHandle {
  openTab: (tab: string) => void;
  highlightTool: (toolId: string) => void;
  closePanel: () => void;
}

// ── Constants ──────────────────────────────────────────────────────

const PANEL_COLLAPSED_KEY = 'ledit.contextPanel.collapsed';
const PANEL_TAB_KEY = 'ledit.contextPanel.tab';
const PANEL_MIN = 280;
const PANEL_MAX = 760;
const MOBILE_LAYOUT_MAX_WIDTH = 768;

const normalizeRevision = (raw: unknown): Revision => {
  const r = raw as Record<string, unknown> | null | undefined;
  if (!r) {
    return {
      revision_id: 'unknown',
      timestamp: new Date().toISOString(),
      files: [],
      description: '',
    };
  }
  const files = Array.isArray(r.files)
    ? (r.files as Array<Record<string, unknown>>).map((file: Record<string, unknown>) => ({
        file_revision_hash: typeof file?.file_revision_hash === 'string' ? file.file_revision_hash : undefined,
        path: typeof file?.path === 'string' ? file.path : 'Unknown',
        operation: typeof file?.operation === 'string' ? file.operation : 'edited',
        lines_added: Number(file?.lines_added || 0),
        lines_deleted: Number(file?.lines_deleted || 0),
      }))
    : [];

  return {
    revision_id: typeof r?.revision_id === 'string' ? r.revision_id : 'unknown',
    timestamp: typeof r?.timestamp === 'string' ? r.timestamp : new Date().toISOString(),
    files,
    description: typeof r?.description === 'string' ? r.description : '',
  };
};

// ── Component ──────────────────────────────────────────────────────

const ContextPanel = forwardRef<ContextPanelHandle, ContextPanelProps>((props, ref) => {
  const { context, onPanelWidthChange, onMobileOpenChange, panelWidth: requestedPanelWidth } = props;

  // ── Panel infrastructure state ───────────────────────────────────
  const [panelCollapsed, setPanelCollapsed] = useState(() => {
    // On mobile, default to collapsed
    if (typeof window !== 'undefined' && window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH) {
      return true;
    }
    return false;
  });
  const panelWidth = typeof requestedPanelWidth === 'number' ? requestedPanelWidth : 360;
  const panelContainerRef = useRef<HTMLDivElement>(null);

  // ── Chat-specific state ──────────────────────────────────────────
  type ChatTabId = 'subagents' | 'tools' | 'changes' | 'tasks' | 'status' | 'sessions';
  const [chatTab, setChatTab] = useState<ChatTabId>('subagents');
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set());
  const [expandedSubagents, setExpandedSubagents] = useState<Set<string>>(new Set());
  const [activeToolId, setActiveToolId] = useState<string | null>(null);
  const toolRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const liveActivityListRef = useRef<HTMLDivElement | null>(null);
  const liveActivityScrollTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [expandedRevisionIds, setExpandedRevisionIds] = useState<Set<string>>(new Set());
  const [revisionDetailsById, setRevisionDetailsById] = useState<Record<string, Record<string, string>>>({});
  const [revisionDetailsLoading, setRevisionDetailsLoading] = useState<Record<string, boolean>>({});
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const historyLoadRequestRef = useRef(0);

  const [sessions, setSessions] = useState<SessionEntry[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string>('');
  const [isLoadingSessions, setIsLoadingSessions] = useState(false);
  const [sessionRestoreError, setSessionRestoreError] = useState<string | null>(null);
  const [sessionsCount, setSessionsCount] = useState(0);

  // ── API service ──────────────────────────────────────────────────
  const apiService = ApiService.getInstance();

  // ── Chat data loading ────────────────────────────────────────────

  const loadRevisionHistory = useCallback(async () => {
    const requestId = ++historyLoadRequestRef.current;
    setIsLoadingHistory(true);
    setRevisionDetailsById({});
    setRevisionDetailsLoading({});
    try {
      const response = await apiService.getChangelog();
      if (requestId !== historyLoadRequestRef.current) return;
      const normalized = (response.revisions || []).map(normalizeRevision).sort((a, b) => {
        return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
      });
      setRevisions(normalized);
      setExpandedRevisionIds(normalized.length > 0 ? new Set([normalized[0].revision_id]) : new Set());
    } catch (error) {
      if (requestId !== historyLoadRequestRef.current) return;
      console.error('Failed to load revision history:', error);
    } finally {
      if (requestId === historyLoadRequestRef.current) {
        setIsLoadingHistory(false);
      }
    }
  }, [apiService]);

  const loadSessions = useCallback(async () => {
    setIsLoadingSessions(true);
    try {
      const response = await apiService.getSessions();
      setSessions(response.sessions || []);
      setCurrentSessionId(response.current_session_id || '');
      setSessionsCount(response.sessions?.length || 0);
    } catch (error) {
      console.error('Failed to load sessions:', error);
    } finally {
      setIsLoadingSessions(false);
    }
  }, [apiService]);

  // ── Public API via ref ───────────────────────────────────────────
  useImperativeHandle(
    ref,
    () => ({
      openTab: (tab: string) => {
        setPanelCollapsed(false);
        if (context === 'chat' && ['subagents', 'tools', 'changes', 'tasks', 'status', 'sessions'].includes(tab)) {
          setChatTab(tab as ChatTabId);
          if (tab === 'changes' && revisions.length === 0) {
            loadRevisionHistory();
          }
          if (tab === 'sessions' && sessionsCount === 0) {
            loadSessions();
          }
        }
      },
      highlightTool: (toolId: string) => {
        if (context !== 'chat') return;
        setPanelCollapsed(false);
        setChatTab('tools');
        setActiveToolId(toolId);
        setTimeout(() => {
          const el = toolRefs.current[toolId];
          if (el != null) {
            el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
          }
        }, 100);
      },
      closePanel: () => {
        setPanelCollapsed(true);
      },
    }),
    [context, revisions.length, sessionsCount, loadRevisionHistory, loadSessions],
  );

  // ── Persistence ──────────────────────────────────────────────────
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const storedCollapsed = window.localStorage.getItem(PANEL_COLLAPSED_KEY);
    const storedTab = window.localStorage.getItem(`${PANEL_TAB_KEY}.${context}`);

    if (storedCollapsed === '1') {
      setPanelCollapsed(true);
    }
    if (storedTab) {
      if (['subagents', 'tools', 'changes', 'tasks', 'status', 'sessions'].includes(storedTab)) {
        setChatTab(storedTab as ChatTabId);
      }
    }
  }, [context]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(PANEL_COLLAPSED_KEY, panelCollapsed ? '1' : '0');
  }, [panelCollapsed]);

  useEffect(() => {
    if (!props.isMobileLayout) {
      return;
    }
    onMobileOpenChange?.(!panelCollapsed);
  }, [panelCollapsed, props.isMobileLayout, onMobileOpenChange]);

  // Persist active tab
  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(`${PANEL_TAB_KEY}.${context}`, chatTab);
  }, [context, chatTab]);

  // ── Clear highlight after 3 seconds ──────────────────────────────
  useEffect(() => {
    if (activeToolId) {
      const timer = setTimeout(() => setActiveToolId(null), 3000);
      return () => clearTimeout(timer);
    }
  }, [activeToolId]);

  // ── Resize handler ───────────────────────────────────────────────
  const startResize = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      e.preventDefault();
      setPanelCollapsed(false);
      const startX = e.clientX;
      const startWidth = panelWidth;

      const onMouseMove = (moveEvent: MouseEvent) => {
        const parentEl = panelContainerRef.current?.parentElement;
        const parentWidth = parentEl ? parentEl.getBoundingClientRect().width : window.innerWidth;
        const rawWidth = startWidth + (startX - moveEvent.clientX);
        const maxByLayout = parentWidth - 260;
        const clamped = Math.max(PANEL_MIN, Math.min(Math.min(PANEL_MAX, maxByLayout), rawWidth));
        onPanelWidthChange?.(clamped);
      };

      const onMouseUp = () => {
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('mouseup', onMouseUp);
      };

      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'col-resize';
      document.addEventListener('mousemove', onMouseMove);
      document.addEventListener('mouseup', onMouseUp);
    },
    [onPanelWidthChange, panelWidth],
  );

  const isProcessing = context === 'chat' ? props.isProcessing : false;

  // Live duration: updates every 1s while the agent is processing
  // so the status tab shows a ticking clock instead of a frozen time.
  const [liveDurationMs, setLiveDurationMs] = useState<number | null>(null);

  // Stable primitives for the effect dependency array (avoids interval teardown on every render).
  // We only care about whether there's at least one message and its timestamp.
  const msgArr: Array<{ timestamp: Date }> = 'messages' in props ? props.messages : [];
  const messageCount = msgArr.length;
  const firstMessageTs =
    messageCount > 0
      ? msgArr[0].timestamp instanceof Date
        ? msgArr[0].timestamp.getTime()
        : new Date(msgArr[0].timestamp).getTime()
      : 0;

  useEffect(() => {
    if (!isProcessing || messageCount === 0) {
      return undefined;
    }

    const tick = () => setLiveDurationMs(Date.now() - firstMessageTs);
    tick();
    const id = setInterval(tick, 1000);
    return () => {
      clearInterval(id);
      setLiveDurationMs(null);
    };
  }, [isProcessing, messageCount, firstMessageTs]);

  const handleRestoreSession = useCallback(
    async (sessionId: string) => {
      if (isProcessing) {
        setSessionRestoreError('Wait for current request to finish.');
        return;
      }
      if (sessionId === currentSessionId) {
        setSessionRestoreError('This is the current session.');
        return;
      }
      if (
        !(await showThemedConfirm(`Restore session ${sessionId}?\n\nThis will replace the current conversation.`, {
          title: 'Restore Session',
          type: 'warning',
        }))
      ) {
        return;
      }
      setIsLoadingSessions(true);
      setSessionRestoreError(null);
      try {
        const response = await apiService.restoreSession(sessionId);
        if (response.messages?.length) {
          setTimeout(() => {
            window.dispatchEvent(
              new CustomEvent('ledit:session-restored', {
                detail: { messages: response.messages },
              }),
            );
          }, 400);
        }
        await loadSessions();
      } catch (error) {
        setSessionRestoreError(error instanceof Error ? error.message : 'Failed to restore session');
      } finally {
        setIsLoadingSessions(false);
      }
    },
    [apiService, currentSessionId, isProcessing, loadSessions],
  );

  const buildRevisionFileKey = useCallback((file: RevisionFile | RevisionDetailFile, index: number) => {
    return `${file.file_revision_hash || file.path}::${index}`;
  }, []);

  const loadRevisionDetails = useCallback(
    async (revisionId: string) => {
      if (!revisionId || revisionDetailsById[revisionId] || revisionDetailsLoading[revisionId]) return;

      setRevisionDetailsLoading((prev) => ({ ...prev, [revisionId]: true }));

      try {
        const response = await apiService.getRevisionDetails(revisionId);
        const detailsMap: Record<string, string> = {};
        (response.revision?.files || []).forEach((file: RevisionDetailFile, index: number) => {
          detailsMap[buildRevisionFileKey(file, index)] = file.diff || '';
        });
        setRevisionDetailsById((prev) => ({ ...prev, [revisionId]: detailsMap }));
      } catch (error) {
        console.error('Failed to load revision details:', error);
      } finally {
        setRevisionDetailsLoading((prev) => ({ ...prev, [revisionId]: false }));
      }
    },
    [apiService, buildRevisionFileKey, revisionDetailsById, revisionDetailsLoading],
  );

  // ── Data loading triggers ────────────────────────────────────────

  useEffect(() => {
    if (context !== 'chat') return;
    if (chatTab === 'changes' && revisions.length === 0 && !isLoadingHistory) {
      loadRevisionHistory();
    }
  }, [context, chatTab, revisions.length, isLoadingHistory, loadRevisionHistory]);

  useEffect(() => {
    if (context !== 'chat') return;
    if (chatTab === 'sessions' && sessionsCount === 0 && !isLoadingSessions) {
      loadSessions();
    }
  }, [context, chatTab, sessionsCount, isLoadingSessions, loadSessions]);

  useEffect(() => {
    if (expandedRevisionIds.size === 0) return;
    expandedRevisionIds.forEach((revisionId) => {
      loadRevisionDetails(revisionId);
    });
  }, [expandedRevisionIds, loadRevisionDetails]);

  // ── Listen for global events ─────────────────────────────────────
  useEffect(() => {
    if (context !== 'chat') return;
    if (typeof window === 'undefined') return;

    const openHistoryPanel = () => {
      setPanelCollapsed(false);
      setChatTab('changes');
      loadRevisionHistory();
    };

    window.addEventListener('ledit:open-revision-history', openHistoryPanel);
    return () => window.removeEventListener('ledit:open-revision-history', openHistoryPanel);
  }, [context, loadRevisionHistory]);

  // ── Toggle handlers ──────────────────────────────────────────────

  const toggleToolExpansion = (toolId: string) => {
    setExpandedTools((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(toolId)) newSet.delete(toolId);
      else newSet.add(toolId);
      return newSet;
    });
  };

  const toggleSubagentExpansion = (toolId: string) => {
    setExpandedSubagents((prev) => {
      const next = new Set(prev);
      if (next.has(toolId)) {
        next.delete(toolId);
      } else {
        next.add(toolId);
      }
      return next;
    });
  };

  // ── Chat helpers ─────────────────────────────────────────────────

  const isSubagentTool = (tool: ToolExecution) =>
    tool.tool === 'run_subagent' || tool.tool === 'run_parallel_subagents';

  const getSubagentPrompt = (tool: ToolExecution): string | undefined => {
    if (!tool.arguments) return undefined;
    try {
      const args = JSON.parse(tool.arguments);
      return typeof args.prompt === 'string' ? args.prompt : undefined;
    } catch {
      return undefined;
    }
  };

  const getSubagentLogMessage = useCallback((log: LogEntry): string | null => {
    if (log.type !== 'agent_message' || !log.data || typeof log.data !== 'object') {
      return null;
    }
    const d = log.data as Record<string, unknown>;
    const raw = typeof d.message === 'string' ? d.message : '';
    if (!raw || (!raw.includes('Subagent:') && !/Spawning subagent/i.test(raw))) {
      return null;
    }
    return stripAnsiCodes(raw).trim() || null;
  }, []);

  const summarizeExecutionTarget = useCallback((message: string): string => {
    const match = message.match(/executing tool \[([^\]]+)\]/i);
    if (!match) {
      return message;
    }
    const rawTarget = match[1].trim();
    if (!rawTarget) {
      return message;
    }
    const parts = rawTarget.split(/\s+/);
    const toolName = parts[0] || 'tool';
    const argPreview = parts.slice(1).join(' ').trim();
    const suffix = argPreview ? ` ${argPreview.slice(0, 56)}${argPreview.length > 56 ? '...' : ''}` : '';
    return message.replace(/executing tool \[[^\]]+\]/i, `Running ${toolName}${suffix}`);
  }, []);

  const normalizeSubagentActivity = useCallback(
    (rawMessage: string) => {
      const cleaned = stripAnsiCodes(rawMessage).trim();
      const taskMatch = cleaned.match(/^→\s+\[([^\]]+)\]\s+Subagent:\s+(.*)$/);
      if (taskMatch) {
        const body = summarizeExecutionTarget(taskMatch[2].trim())
          .replace(/^\[\d+\s*-\s*\d+%\]\s*/i, '')
          .trim();
        return {
          taskId: taskMatch[1],
          label: body,
          isSpawn: false,
        };
      }

      const spawnMatch = cleaned.match(/Spawning subagent \[([^\]]+)\]:\s*(.*)$/i);
      if (spawnMatch) {
        const spawnDetails = spawnMatch[2].trim();
        return {
          taskId: undefined,
          label: spawnDetails ? `Starting ${spawnMatch[1]} (${spawnDetails})` : `Starting ${spawnMatch[1]}`,
          isSpawn: true,
        };
      }

      const inlineMatch = cleaned.match(/^→\s+Subagent:\s+(.*)$/);
      if (inlineMatch) {
        const body = summarizeExecutionTarget(inlineMatch[1].trim())
          .replace(/^\[\d+\s*-\s*\d+%\]\s*/i, '')
          .trim();
        return {
          taskId: undefined,
          label: body,
          isSpawn: false,
        };
      }

      return {
        taskId: undefined,
        label: summarizeExecutionTarget(cleaned),
        isSpawn: false,
      };
    },
    [summarizeExecutionTarget],
  );

  const getToolIcon = (toolName: string): ReactNode => {
    const iconMap: { [key: string]: ReactNode } = {
      shell_command: <Terminal size={14} />,
      read_file: <BookOpen size={14} />,
      write_file: <Pencil size={14} />,
      edit_file: <FileEdit size={14} />,
      search_files: <Search size={14} />,
      analyze_ui_screenshot: <Eye size={14} />,
      analyze_image_content: <FlaskConical size={14} />,
      web_search: <Globe size={14} />,
      fetch_url: <ArrowDown size={14} />,
      TodoWrite: <ClipboardList size={14} />,
      TodoRead: <ClipboardList size={14} />,
      view_history: <ScrollText size={14} />,
      rollback_changes: <RotateCcw size={14} />,
      mcp_tools: <Wrench size={14} />,
      run_subagent: <Bot size={14} />,
      run_parallel_subagents: <Bot size={14} />,
    };
    return iconMap[toolName] || <Wrench size={14} />;
  };

  const getPersonaColor = (persona?: string) => {
    const colorMap: Record<string, string> = {
      coder: '#58a6ff',
      reviewer: '#d2a8ff',
      code_reviewer: '#d2a8ff',
      tester: '#7ee787',
      debugger: '#f0883e',
      refactor: '#79c0ff',
      researcher: '#ff7b72',
      general: '#8b949e',
    };
    return colorMap[persona || ''] || '#8b949e';
  };

  const getStatusIcon = (status: string): ReactNode => {
    switch (status) {
      case 'started':
        return <Rocket size={14} />;
      case 'running':
        return <Zap size={14} />;
      case 'completed':
        return <CheckCircle2 size={14} />;
      case 'error':
        return <XCircle size={14} />;
      default:
        return <Hourglass size={14} />;
    }
  };

  const formatDuration = (startTime: Date, endTime?: Date) => {
    const end = endTime || new Date();
    const duration = end.getTime() - startTime.getTime();
    if (duration < 1000) return `${duration}ms`;
    if (duration < 60000) return `${(duration / 1000).toFixed(1)}s`;
    return `${(duration / 60000).toFixed(1)}m`;
  };

  const formatRelativeTime = (value: string) => {
    const date = new Date(value);
    const diffMs = Date.now() - date.getTime();
    const diffSecs = Math.max(0, Math.floor(diffMs / 1000));
    const diffMins = Math.floor(diffSecs / 60);
    const diffHours = Math.floor(diffMins / 60);
    if (diffSecs < 60) return `${diffSecs}s ago`;
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    return date.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  };

  const formatTime = (value: Date) => {
    return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const formatDurationMs = (ms: number): string => {
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(0)}s`;
    const mins = Math.floor(ms / 60000);
    const secs = Math.floor((ms % 60000) / 1000);
    return `${mins}m ${secs}s`;
  };

  const formatTokens = (tokens: number): string => {
    if (!Number.isFinite(tokens) || tokens < 0) return '—';
    if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`;
    if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`;
    return tokens.toString();
  };

  const formatCost = (cost: number): string => {
    if (!Number.isFinite(cost)) return '—';
    return `$${cost.toFixed(4)}`;
  };

  // ── Chat computed values ─────────────────────────────────────────

  const chatProps = context === 'chat' ? (props as ChatContextPanelProps) : null;
  const toolExecutions = useMemo(() => chatProps?.toolExecutions ?? [], [chatProps]);
  const currentTodos = chatProps?.currentTodos ?? [];
  const chatMessages = useMemo(() => chatProps?.messages ?? [], [chatProps]);
  const chatFileEdits = useMemo(() => chatProps?.fileEdits ?? [], [chatProps]);
  const subagentToolExecutions = useMemo(() => chatProps?.toolExecutions ?? [], [chatProps]);
  const subagentLogs = useMemo(() => chatProps?.logs ?? [], [chatProps]);
  const subagentActivities = useMemo(() => chatProps?.subagentActivities ?? [], [chatProps]);

  // Auto-scroll live subagent activity lists to the bottom (debounced)
  useEffect(() => {
    const el = liveActivityListRef.current;
    if (!el) return;
    if (liveActivityScrollTimeoutRef.current) {
      clearTimeout(liveActivityScrollTimeoutRef.current);
    }
    liveActivityScrollTimeoutRef.current = setTimeout(() => {
      el.scrollTop = el.scrollHeight;
    }, 150);
    return () => {
      if (liveActivityScrollTimeoutRef.current) {
        clearTimeout(liveActivityScrollTimeoutRef.current);
      }
    };
  }, [subagentActivities.length]);

  const chatStats = chatProps?.stats ?? null;

  const subagentRuns = useMemo(() => {
    return subagentToolExecutions.filter(isSubagentTool).map((tool) => {
      const structuredActivities = subagentActivities
        .filter((activity) => {
          if (activity.toolCallId) {
            return activity.toolCallId === tool.id;
          }
          const ts =
            activity.timestamp instanceof Date ? activity.timestamp.getTime() : new Date(activity.timestamp).getTime();
          const startMs = tool.startTime.getTime() - 500;
          const endMs = (tool.endTime || new Date()).getTime() + 500;
          return ts >= startMs && ts <= endMs;
        })
        .map((activity) => ({
          id: activity.id,
          timestamp: activity.timestamp,
          taskId: activity.taskId,
          label: activity.message,
          isSpawn: activity.phase === 'spawn',
        }));

      const startMs = tool.startTime.getTime() - 500;
      const endMs = (tool.endTime || new Date()).getTime() + 500;
      const fallbackActivities = subagentLogs
        .filter((log) => {
          const message = getSubagentLogMessage(log);
          if (!message) {
            return false;
          }
          const ts = log.timestamp instanceof Date ? log.timestamp.getTime() : new Date(log.timestamp).getTime();
          return ts >= startMs && ts <= endMs;
        })
        .map((log) => {
          const message = getSubagentLogMessage(log) || '';
          const normalized = normalizeSubagentActivity(message);
          return {
            id: log.id,
            timestamp: log.timestamp,
            taskId: normalized.taskId,
            label: normalized.label,
            isSpawn: normalized.isSpawn,
          };
        })
        .filter((item, index, items) => {
          if (!item.label) {
            return false;
          }
          const previous = items[index - 1];
          return !previous || previous.label !== item.label;
        });
      const activities = structuredActivities.length > 0 ? structuredActivities : fallbackActivities;

      const taskGroups = activities.reduce<Record<string, typeof activities>>((acc, item) => {
        const key = item.taskId || '__main__';
        if (!acc[key]) {
          acc[key] = [];
        }
        acc[key].push(item);
        return acc;
      }, {});

      const orderedTaskGroups = Object.entries(taskGroups).map(([taskId, items]) => ({
        taskId: taskId === '__main__' ? null : taskId,
        items,
        latest: items[items.length - 1],
      }));

      return {
        tool,
        prompt: getSubagentPrompt(tool),
        latestActivity: activities[activities.length - 1],
        activities,
        orderedTaskGroups,
      };
    });
  }, [getSubagentLogMessage, normalizeSubagentActivity, subagentActivities, subagentLogs, subagentToolExecutions]);

  const activeToolCount = toolExecutions.filter(
    (tool) => tool.status === 'started' || tool.status === 'running',
  ).length;

  const activeSubagentCount = subagentRuns.filter(
    ({ tool }) => tool.status === 'started' || tool.status === 'running',
  ).length;

  const historyCounts = revisions.length;

  const statusMetrics: StatusMetrics = useMemo(() => {
    if (context !== 'chat') {
      return {
        userMsgs: 0,
        assistantMsgs: 0,
        totalMsgs: 0,
        completedTools: 0,
        failedTools: 0,
        activeTools: 0,
        totalTools: 0,
        totalAdditions: 0,
        totalDeletions: 0,
        filesTouched: 0,
        topTools: [],
        maxToolCount: 1,
        duration: 0,
      };
    }

    const msgs = chatMessages;
    const userMsgs = msgs.filter((m) => m.type === 'user').length;
    const assistantMsgs = msgs.filter((m) => m.type === 'assistant').length;
    const completedTools = toolExecutions.filter((t) => t.status === 'completed').length;
    const failedTools = toolExecutions.filter((t) => t.status === 'error').length;
    const activeTools = toolExecutions.filter((t) => t.status === 'running' || t.status === 'started').length;

    const totalAdditions = chatFileEdits.reduce((sum, edit) => sum + (edit.linesAdded || 0), 0);
    const totalDeletions = chatFileEdits.reduce((sum, edit) => sum + (edit.linesDeleted || 0), 0);

    const touchedFiles = new Set<string>();
    toolExecutions.forEach((t) => {
      if (t.tool === 'write_file' || t.tool === 'edit_file') {
        try {
          const args = t.arguments ? JSON.parse(t.arguments) : {};
          if (args.path) touchedFiles.add(args.path);
        } catch {
          /* ignore */
        }
      }
    });
    chatFileEdits.forEach((edit) => {
      if (edit.path) {
        touchedFiles.add(edit.path);
      }
    });

    const toolCounts: Record<string, number> = {};
    toolExecutions.forEach((t) => {
      const name = isSubagentTool(t) ? 'subagent' : t.tool;
      toolCounts[name] = (toolCounts[name] || 0) + 1;
    });
    const sortedTools = Object.entries(toolCounts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 6);
    const maxToolCount = sortedTools.length > 0 ? sortedTools[0][1] : 1;

    let duration = 0;
    if (msgs.length >= 2) {
      duration = msgs[msgs.length - 1].timestamp.getTime() - msgs[0].timestamp.getTime();
    }

    return {
      userMsgs,
      assistantMsgs,
      totalMsgs: userMsgs + assistantMsgs,
      completedTools,
      failedTools,
      activeTools,
      totalTools: toolExecutions.length,
      totalAdditions,
      totalDeletions,
      filesTouched: touchedFiles.size,
      topTools: sortedTools,
      maxToolCount,
      duration,
    };
  }, [chatFileEdits, chatMessages, context, toolExecutions]);

  // ── Tab definitions ──────────────────────────────────────────────

  const chatPanelTabs: Array<{
    id: 'subagents' | 'tools' | 'changes' | 'tasks' | 'status' | 'sessions';
    label: string;
    icon: ReactNode;
    count: string;
  }> = [
    {
      id: 'subagents',
      label: 'Subagents',
      icon: <Bot size={14} />,
      count: activeSubagentCount > 0 ? `${activeSubagentCount} active` : `${subagentRuns.length} total`,
    },
    {
      id: 'tools',
      label: 'Tool Executions',
      icon: <Wrench size={14} />,
      count: activeToolCount > 0 ? `${activeToolCount} active` : `${toolExecutions.length} total`,
    },
    { id: 'changes', label: 'Session Changes', icon: <History size={14} />, count: `${historyCounts} revisions` },
    {
      id: 'tasks',
      label: 'Tasks',
      icon: <ListTodo size={14} />,
      count: `${currentTodos.filter((t) => t.status === 'in_progress').length || 0} active`,
    },
    { id: 'sessions', label: 'Sessions', icon: <Clock size={14} />, count: `${sessionsCount}` },
    { id: 'status', label: 'Status', icon: <Activity size={14} />, count: `${statusMetrics.totalMsgs} msgs` },
  ];

  const activeTab = chatPanelTabs.find((t) => t.id === chatTab) || chatPanelTabs[0];

  // ── Render: Tools Tab (Chat) ─────────────────────────────────────

  const renderToolsTab = () => (
    <div className="context-panel-tools-list">
      <>
        {toolExecutions.length === 0 ? (
          <div className="context-panel-empty">Tool calls will appear here.</div>
        ) : (
          toolExecutions.map((tool) => {
            const isSub = isSubagentTool(tool);
            const subagentPrompt = isSub ? getSubagentPrompt(tool) : undefined;

            return (
              <div
                key={tool.id}
                ref={(el) => {
                  toolRefs.current[tool.id] = el;
                }}
                className={`tool-execution tool-${tool.status} ${isSub ? 'tool-subagent' : ''} ${activeToolId === tool.id ? 'tool-highlighted' : ''}`}
                onClick={() => toggleToolExpansion(tool.id)}
              >
                <>
                  <div className="tool-summary">
                    <span className="tool-icon">
                      {isSub ? (
                        <span className="subagent-icon" style={{ color: getPersonaColor(tool.persona) }}>
                          <Bot size={14} />
                        </span>
                      ) : (
                        getToolIcon(tool.tool)
                      )}
                    </span>
                    <span className={`tool-name ${isSub ? 'tool-name-subagent' : ''}`}>
                      {isSub
                        ? tool.persona
                          ? `${tool.persona}`
                          : tool.subagentType === 'parallel'
                            ? 'parallel subagents'
                            : 'subagent'
                        : tool.tool}
                      {isSub && tool.subagentType === 'parallel' && ' (parallel)'}
                    </span>
                    <span className="tool-status">{getStatusIcon(tool.status)}</span>
                    <span className="tool-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
                    <span className="tool-expand">
                      {expandedTools.has(tool.id) ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                    </span>
                  </div>

                  {isSub && subagentPrompt && !expandedTools.has(tool.id) && (
                    <div className="tool-message tool-subagent-prompt">{stripAnsiCodes(subagentPrompt)}</div>
                  )}

                  {tool.message && !(isSub && subagentPrompt) && (
                    <div className="tool-message">{stripAnsiCodes(tool.message)}</div>
                  )}

                  {expandedTools.has(tool.id) && (tool.arguments || tool.result || tool.details) && (
                    <div className="tool-details">
                      {isSub && subagentPrompt && (
                        <div className="tool-detail-section">
                          <div className="tool-detail-label">
                            <FileEdit size={12} className="inline-icon" /> Task
                          </div>
                          <pre className="subagent-prompt-detail">{stripAnsiCodes(subagentPrompt)}</pre>
                        </div>
                      )}
                      {tool.arguments && !isSub && (
                        <div className="tool-detail-section">
                          <div className="tool-detail-label">
                            <ClipboardList size={12} className="inline-icon" /> Call
                          </div>
                          <pre>{formatToolDetail(tool.arguments)}</pre>
                        </div>
                      )}
                      {tool.result && (
                        <div className="tool-detail-section">
                          <div className="tool-detail-label">
                            {isSub ? (
                              <>
                                <BarChart3 size={12} className="inline-icon" /> Summary
                              </>
                            ) : (
                              <>
                                <FileText size={12} className="inline-icon" /> Response
                              </>
                            )}
                          </div>
                          <pre>{formatToolDetail(tool.result)}</pre>
                        </div>
                      )}
                    </div>
                  )}
                </>
              </div>
            );
          })
        )}
      </>
    </div>
  );

  const renderSubagentsTab = () => (
    <div className="context-panel-tools-list">
      {subagentRuns.length === 0 ? (
        <div className="context-panel-empty">
          Delegated work will appear here when the orchestrator runs `run_subagent` or `run_parallel_subagents`.
        </div>
      ) : (
        subagentRuns.map(({ tool, prompt, latestActivity, activities, orderedTaskGroups }) => {
          const isActive = tool.status === 'started' || tool.status === 'running';
          // Auto-expand active subagents so users can see real-time output
          const expanded = expandedSubagents.has(tool.id) || isActive;
          const isParallel = tool.subagentType === 'parallel';
          // Show more activities when active or expanded
          const collapsedActivities = activities.slice(isActive ? -10 : -3);
          const visibleActivities = expanded ? activities : collapsedActivities;
          const taskCount = orderedTaskGroups.filter((group) => group.taskId).length;
          const hiddenActivityCount = Math.max(0, activities.length - visibleActivities.length);
          const resultPreview = getSubagentResultPreview(tool.result);
          const lastUpdatedAt = latestActivity?.timestamp || tool.endTime || tool.startTime;

          return (
            <section key={tool.id} className={`subagent-card tool-${tool.status}`}>
              <button
                className="subagent-card-header"
                onClick={() => toggleSubagentExpansion(tool.id)}
                aria-expanded={expanded}
              >
                <span className="subagent-card-title-row">
                  <span className="subagent-card-icon" style={{ color: getPersonaColor(tool.persona) }}>
                    <Bot size={14} />
                  </span>
                  <span className="subagent-card-title">
                    {tool.persona || (isParallel ? 'parallel subagents' : 'subagent')}
                  </span>
                  {isParallel && <span className="subagent-kind-badge">parallel</span>}
                </span>
                <span className="subagent-card-meta">
                  <span className="subagent-card-status">{getStatusIcon(tool.status)}</span>
                  <span className="tool-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
                  <span className="tool-expand">
                    {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                  </span>
                </span>
              </button>

              {prompt && <div className="subagent-prompt-preview">{stripAnsiCodes(prompt)}</div>}

              <div className="subagent-card-stats">
                <span className="subagent-stat-chip">
                  {activities.length} {activities.length === 1 ? 'update' : 'updates'}
                </span>
                {isParallel && taskCount > 0 && (
                  <span className="subagent-stat-chip">
                    {taskCount} {taskCount === 1 ? 'task' : 'tasks'}
                  </span>
                )}
                <span className="subagent-stat-chip">Updated {formatTime(lastUpdatedAt)}</span>
              </div>

              {latestActivity && (
                <div className="subagent-current-step">
                  <span className="subagent-current-label">Now</span>
                  <span className="subagent-current-text">{latestActivity.label}</span>
                </div>
              )}

              {isParallel && orderedTaskGroups.filter((group) => group.taskId).length > 0 && (
                <div className="subagent-task-groups">
                  {orderedTaskGroups
                    .filter((group) => group.taskId)
                    .map((group) => (
                      <div key={group.taskId || 'main'} className="subagent-task-card">
                        <div className="subagent-task-name">{group.taskId}</div>
                        <div className="subagent-task-summary">{group.latest?.label || 'Waiting...'}</div>
                      </div>
                    ))}
                </div>
              )}

              {visibleActivities.length > 0 && (
                <div
                  ref={isActive ? liveActivityListRef : undefined}
                  className={`subagent-activity-list ${isActive ? 'subagent-activity-live' : ''}`}
                >
                  {visibleActivities.map((activity) => (
                    <div key={activity.id} className="subagent-activity-item">
                      <span className={`subagent-activity-dot ${activity.isSpawn ? 'spawn' : ''}`} />
                      <div className="subagent-activity-body">
                        <div className="subagent-activity-text">
                          {activity.taskId && <span className="subagent-task-pill">{activity.taskId}</span>}
                          <span>{activity.label}</span>
                        </div>
                        <div className="subagent-activity-time">{formatTime(activity.timestamp)}</div>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {hiddenActivityCount > 0 && !expanded && (
                <div className="subagent-collapsed-note">
                  Showing the latest {visibleActivities.length} of {activities.length} updates
                </div>
              )}

              {resultPreview && (
                <div className="subagent-result-preview">
                  <div className="tool-detail-label">
                    <BarChart3 size={12} className="inline-icon" /> Result preview
                  </div>
                  <div className="subagent-result-preview-text">{resultPreview}</div>
                </div>
              )}

              {tool.result && expanded && (
                <div className="subagent-result-snippet">
                  <div className="tool-detail-label">
                    <BarChart3 size={12} className="inline-icon" /> Result
                  </div>
                  <pre>{formatToolDetail(tool.result)}</pre>
                </div>
              )}

              <div className="subagent-card-actions">
                {activities.length > 3 && (
                  <button className="subagent-link-btn" onClick={() => toggleSubagentExpansion(tool.id)}>
                    {expanded ? 'Show fewer updates' : 'Show all updates'}
                  </button>
                )}
                <button
                  className="subagent-link-btn"
                  onClick={() => {
                    setChatTab('tools');
                    setActiveToolId(tool.id);
                    setExpandedTools((prev) => new Set(prev).add(tool.id));
                    setTimeout(() => {
                      const el = toolRefs.current[tool.id];
                      if (el != null) {
                        el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
                      }
                    }, 100);
                  }}
                >
                  View raw tool details
                </button>
              </div>
            </section>
          );
        })
      )}
    </div>
  );

  // ── Render: Sessions Tab (Chat) ──────────────────────────────────

  const renderSessionsTab = () => (
    <div className="context-panel-tools-list">
      <div className="history-toolbar">
        <button className="history-refresh-btn" onClick={loadSessions} disabled={isLoadingSessions}>
          <RotateCcw size={12} /> Refresh
        </button>
      </div>

      {sessionRestoreError && <div className="history-error-inline">{sessionRestoreError}</div>}

      {isLoadingSessions ? (
        <div className="context-panel-empty">Loading sessions...</div>
      ) : sessions.length === 0 ? (
        <div className="context-panel-empty">No saved sessions found.</div>
      ) : (
        sessions.map((session) => {
          const isCurrent = session.session_id === currentSessionId;
          const dirName = session.working_directory.split('/').filter(Boolean).slice(-2).join('/');
          return (
            <div key={session.session_id} className={`history-item ${isCurrent ? 'session-current' : ''}`}>
              <div className="session-summary">
                <span className="history-main">
                  <span className="history-id" title={session.session_id}>
                    {session.name || session.session_id}
                  </span>
                  <span className="history-time">{formatRelativeTime(session.last_updated)}</span>
                </span>
                <span className="history-stats">
                  <span>{session.message_count} msgs</span>
                  {session.total_tokens > 0 && <span>{session.total_tokens.toLocaleString()} tok</span>}
                </span>
              </div>
              <div className="session-meta">
                <span className="session-dir" title={session.working_directory}>
                  {dirName}
                </span>
                {isCurrent ? (
                  <span className="session-current-badge">Current</span>
                ) : (
                  <button
                    className="history-rollback-btn"
                    onClick={() => handleRestoreSession(session.session_id)}
                    disabled={isLoadingSessions}
                    style={{ marginTop: '4px' }}
                  >
                    Restore
                  </button>
                )}
              </div>
            </div>
          );
        })
      )}
    </div>
  );

  // ── Render: Status Tab (Chat) ────────────────────────────────────

  const chatLastError = chatProps?.lastError ?? null;
  const chatQueryProgress = chatProps?.queryProgress ?? null;
  const chatIsProcessing = chatProps?.isProcessing ?? false;

  const renderStatusTab = () => (
    <div className="context-panel-status">
      <div className="status-section">
        <div className="status-section-title">
          <Activity size={12} /> Processing
        </div>
        <div className="status-row">
          {chatIsProcessing ? (
            <>
              <span className="status-dot-indicator active" />
              <span className="status-label">
                {chatQueryProgress ? ((chatQueryProgress as Record<string, unknown>).message as string) : 'Working...'}
              </span>
            </>
          ) : chatLastError ? (
            <>
              <span className="status-dot-indicator error" />
              <span className="status-label">{chatLastError}</span>
            </>
          ) : chatMessages.length === 0 ? (
            <>
              <span className="status-dot-indicator" />
              <span className="status-label">Idle — waiting for input</span>
            </>
          ) : (
            <>
              <span className="status-dot-indicator idle" />
              <span className="status-label">Ready</span>
            </>
          )}
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Bot size={12} /> Conversation
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.userMsgs}</span>
            <span className="status-metric-label">User</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.assistantMsgs}</span>
            <span className="status-metric-label">Assistant</span>
          </div>
          {(() => {
            const displayMs = liveDurationMs ?? statusMetrics.duration;
            if (!displayMs || isNaN(displayMs) || displayMs <= 0) return null;
            return (
              <div className="status-metric status-metric-wide">
                <span className="status-metric-value">{formatDurationMs(displayMs)}</span>
                <span className="status-metric-label">Duration</span>
              </div>
            );
          })()}
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Wrench size={12} /> Tool Usage
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.completedTools}</span>
            <span className="status-metric-label">Completed</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.failedTools}</span>
            <span className="status-metric-label">Failed</span>
          </div>
          {statusMetrics.activeTools > 0 && (
            <div className="status-metric">
              <span className="status-metric-value status-metric-active">{statusMetrics.activeTools}</span>
              <span className="status-metric-label">Active</span>
            </div>
          )}
        </div>
        {statusMetrics.topTools.length > 0 && (
          <div className="status-tool-bars">
            {statusMetrics.topTools.map(([name, count]) => (
              <div key={name} className="status-tool-bar-row">
                <span className="status-tool-bar-name">{name}</span>
                <div className="status-tool-bar-track">
                  <div
                    className="status-tool-bar-fill"
                    style={{ width: `${(count / statusMetrics.maxToolCount) * 100}%` }}
                  />
                </div>
                <span className="status-tool-bar-count">{count}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <FileCode size={12} /> Changes
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.filesTouched}</span>
            <span className="status-metric-label">Files</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value status-metric-add">+{statusMetrics.totalAdditions}</span>
            <span className="status-metric-label">Added</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value status-metric-del">-{statusMetrics.totalDeletions}</span>
            <span className="status-metric-label">Removed</span>
          </div>
          {revisions.length > 0 && (
            <div className="status-metric">
              <span className="status-metric-value">{revisions.length}</span>
              <span className="status-metric-label">Revisions</span>
            </div>
          )}
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <BarChart3 size={12} /> Token Usage
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{chatStats ? formatTokens(chatStats.total_tokens || 0) : '—'}</span>
            <span className="status-metric-label">Total</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">{chatStats ? formatTokens(chatStats.prompt_tokens || 0) : '—'}</span>
            <span className="status-metric-label">Prompt</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats ? formatTokens(chatStats.completion_tokens || 0) : '—'}
            </span>
            <span className="status-metric-label">Completion</span>
          </div>
          {(chatStats?.cached_tokens || 0) > 0 && (
            <div className="status-metric">
              <span className="status-metric-value">{formatTokens(chatStats?.cached_tokens || 0)}</span>
              <span className="status-metric-label">Cached</span>
            </div>
          )}
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Activity size={12} /> Context Window
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats?.context_usage_percent != null ? `${chatStats.context_usage_percent.toFixed(1)}%` : '—'}
            </span>
            <span className="status-metric-label">Used</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats ? formatTokens(chatStats.current_context_tokens || 0) : '—'}
            </span>
            <span className="status-metric-label">Current</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats ? formatTokens(chatStats.max_context_tokens || 0) : '—'}
            </span>
            <span className="status-metric-label">Max</span>
          </div>
        </div>
        {chatStats && chatStats.context_usage_percent != null && (
          <div className="status-context-bar">
            <div
              className={`status-context-bar-fill ${chatStats.context_usage_percent > 90 ? 'critical' : chatStats.context_usage_percent > 75 ? 'high' : ''}`}
              style={{ width: `${Math.max(0, Math.min(100, chatStats.context_usage_percent))}%` }}
            />
          </div>
        )}
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Activity size={12} /> Costs
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{chatStats ? formatCost(chatStats.total_cost || 0) : '—'}</span>
            <span className="status-metric-label">Total Cost</span>
          </div>
          {(chatStats?.cached_cost_savings || 0) > 0 && (
            <div className="status-metric">
              <span className="status-metric-value">{formatCost(chatStats?.cached_cost_savings || 0)}</span>
              <span className="status-metric-label">Cache Savings</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );

  // ── Main Render ──────────────────────────────────────────────────

  const isMobileLayout = props.isMobileLayout ?? false;

  const tabs = chatPanelTabs;
  const activeTabId = chatTab;

  const handleTabClick = (tabId: string) => {
    setPanelCollapsed(false);
    const id = tabId as 'subagents' | 'tools' | 'changes' | 'tasks' | 'status' | 'sessions';
    setChatTab(id);
    if (id === 'changes' && revisions.length === 0) loadRevisionHistory();
    if (id === 'sessions' && sessionsCount === 0) loadSessions();
  };

  const renderTabContent = () => {
    switch (chatTab) {
      case 'subagents':
        return renderSubagentsTab();
      case 'tools':
        return renderToolsTab();
      case 'changes':
        return (
          <RevisionListPanel
            mode="session"
            onOpenDiff={(props as ChatContextPanelProps).onOpenRevisionDiff || (() => {})}
          />
        );
      case 'tasks':
        return (
          <div className="side-panel-tasks">
            <TodoPanel todos={currentTodos || []} isLoading={isProcessing && currentTodos.length === 0} />
          </div>
        );
      case 'sessions':
        return renderSessionsTab();
      case 'status':
        return renderStatusTab();
      default:
        return renderSubagentsTab();
    }
  };

  return (
    <>
      {!panelCollapsed && !isMobileLayout && (
        <div
          className="context-panel-resizer"
          onMouseDown={startResize}
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize context panel"
        />
      )}
      {isMobileLayout && panelCollapsed ? null : (
        <aside
          className={`context-panel ${panelCollapsed ? 'collapsed' : ''}${isMobileLayout ? ' context-panel-mobile' : ''}`}
          aria-label="Context panel"
          style={!panelCollapsed && !isMobileLayout ? { width: `${panelWidth}px` } : undefined}
          ref={panelContainerRef}
        >
          <div className="side-panel-rail">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                className={`side-rail-btn ${activeTabId === tab.id ? 'active' : ''}`}
                onClick={() => handleTabClick(tab.id)}
                title={tab.label}
                aria-label={tab.label}
                aria-pressed={activeTabId === tab.id}
              >
                {tab.icon}
              </button>
            ))}
            <button
              className="side-collapse-btn"
              onClick={() => setPanelCollapsed((prev) => !prev)}
              title={panelCollapsed ? 'Expand panel' : 'Collapse panel'}
            >
              {panelCollapsed ? <PanelRightOpen size={14} /> : <PanelRightClose size={14} />}
            </button>
          </div>

          {!panelCollapsed && (
            <div className="side-panel-content">
              <div className="side-panel-header">
                <div className="side-panel-title">
                  {activeTab.icon}
                  <h4>{activeTab.label}</h4>
                </div>
                <div className="side-panel-header-actions">
                  <span className="tool-count">{activeTab.count}</span>
                </div>
              </div>
              <div className="side-panel-body">{renderTabContent()}</div>
            </div>
          )}
        </aside>
      )}
    </>
  );
});

ContextPanel.displayName = 'ContextPanel';

export default ContextPanel;
