import React, { useState, useEffect, useCallback, useRef, useMemo, type ReactNode, useImperativeHandle, forwardRef } from 'react';
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
  Copy,
  AlertTriangle,
  ShieldCheck,
  FileEdit,
  Plus,
  File,
  Check,
  Minus,
  Circle,
  Loader2,
} from 'lucide-react';
import './ContextPanel.css';
import TodoPanel from './TodoPanel';
import { ApiService } from '../services/api';
import { stripAnsiCodes } from '../utils/ansi';
import RevisionListPanel from './RevisionListPanel';

// ── Types ──────────────────────────────────────────────────────────

type FileSection = 'staged' | 'modified' | 'untracked' | 'deleted';

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

interface GitDiffResponse {
  message: string;
  path: string;
  has_staged: boolean;
  has_unstaged: boolean;
  staged_diff: string;
  unstaged_diff: string;
  diff: string;
}

interface DeepReviewResult {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
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
}

// Chat-context specific props
interface ChatContextPanelProps extends ContextPanelBaseProps {
  context: 'chat';
  toolExecutions: ToolExecution[];
  currentTodos: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: any;
  onHandleToolPillClick?: (toolId: string) => void;
  onOpenRevisionDiff?: (options: { path: string; diff: string; title: string }) => void;
}

// Git-context specific props
interface GitContextPanelProps extends ContextPanelBaseProps {
  context: 'git';
  stagedFiles: Array<{ path: string; status?: string; changes?: { additions: number; deletions: number } }>;
  modifiedFiles: Array<{ path: string; status?: string; changes?: { additions: number; deletions: number } }>;
  untrackedFiles: Array<{ path: string; status?: string; changes?: { additions: number; deletions: number } }>;
  deletedFiles: Array<{ path: string; status?: string; changes?: { additions: number; deletions: number } }>;
  selectedFiles: Set<string>;
  onFileSelect: (section: FileSection, path: string) => void;
  onPreviewFile: (section: FileSection, path: string) => void;
  activeDiffSelectionKey: string | null;
  activeDiffPath: string | null;
  activeDiff: GitDiffResponse | null;
  diffMode: 'combined' | 'staged' | 'unstaged';
  onDiffModeChange: (mode: 'combined' | 'staged' | 'unstaged') => void;
  isDiffLoading: boolean;
  diffError: string | null;
  onStage: (files: string[]) => void;
  onUnstage?: (files: string[]) => void;
  onDiscard: (files: string[]) => void;
  deepReview: DeepReviewResult | null;
  reviewError: string | null;
  reviewFixResult: string | null;
  reviewFixLogs: string[];
  reviewFixSessionID: string | null;
  isReviewLoading: boolean;
  isReviewFixing: boolean;
  onRunReview: () => void;
  onFixFromReview: () => void;
}

export type ContextPanelProps = ChatContextPanelProps | GitContextPanelProps;

// ── Public API via ref ─────────────────────────────────────────────

export interface ContextPanelHandle {
  openTab: (tab: string) => void;
  highlightTool: (toolId: string) => void;
}

// ── Constants ──────────────────────────────────────────────────────

const PANEL_WIDTH_KEY = 'ledit.contextPanel.width';
const PANEL_COLLAPSED_KEY = 'ledit.contextPanel.collapsed';
const PANEL_TAB_KEY = 'ledit.contextPanel.tab';
const PANEL_MIN = 280;
const PANEL_MAX = 760;
const MOBILE_LAYOUT_MAX_WIDTH = 900;

const normalizeRevision = (raw: any): Revision => {
  const files = Array.isArray(raw?.files)
    ? raw.files.map((file: any) => ({
        file_revision_hash: typeof file?.file_revision_hash === 'string' ? file.file_revision_hash : undefined,
        path: typeof file?.path === 'string' ? file.path : 'Unknown',
        operation: typeof file?.operation === 'string' ? file.operation : 'edited',
        lines_added: Number(file?.lines_added || 0),
        lines_deleted: Number(file?.lines_deleted || 0),
      }))
    : [];

  return {
    revision_id: typeof raw?.revision_id === 'string' ? raw.revision_id : 'unknown',
    timestamp: typeof raw?.timestamp === 'string' ? raw.timestamp : new Date().toISOString(),
    files,
    description: typeof raw?.description === 'string' ? raw.description : '',
  };
};

// ── Component ──────────────────────────────────────────────────────

const ContextPanel = forwardRef<ContextPanelHandle, ContextPanelProps>((props, ref) => {
  const { context } = props;

  // ── Panel infrastructure state ───────────────────────────────────
  const [panelCollapsed, setPanelCollapsed] = useState(false);
  const [panelWidth, setPanelWidth] = useState(360);
  const panelContainerRef = useRef<HTMLDivElement>(null);
  const resizingRef = useRef(false);

  // ── Chat-specific state ──────────────────────────────────────────
  const [chatTab, setChatTab] = useState<'tools' | 'changes' | 'tasks' | 'status' | 'sessions'>('tools');
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set());
  const [activeToolId, setActiveToolId] = useState<string | null>(null);
  const toolRefs = useRef<Record<string, HTMLDivElement | null>>({});

  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [expandedRevisionIds, setExpandedRevisionIds] = useState<Set<string>>(new Set());
  const [expandedRevisionFileDiffs, setExpandedRevisionFileDiffs] = useState<Set<string>>(new Set());
  const [revisionDetailsById, setRevisionDetailsById] = useState<Record<string, Record<string, string>>>({});
  const [revisionDetailsLoading, setRevisionDetailsLoading] = useState<Record<string, boolean>>({});
  const [revisionDetailsError, setRevisionDetailsError] = useState<Record<string, string>>({});
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const historyLoadRequestRef = useRef(0);

  const [sessions, setSessions] = useState<SessionEntry[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string>('');
  const [isLoadingSessions, setIsLoadingSessions] = useState(false);
  const [sessionRestoreError, setSessionRestoreError] = useState<string | null>(null);
  const [sessionsCount, setSessionsCount] = useState(0);

  // ── Git-specific state ───────────────────────────────────────────
  const [gitTab, setGitTab] = useState<'files' | 'diff' | 'review'>('files');

  // ── API service ──────────────────────────────────────────────────
  const apiService = ApiService.getInstance();

  // ── Chat data loading ────────────────────────────────────────────

  const loadRevisionHistory = useCallback(async () => {
    const requestId = ++historyLoadRequestRef.current;
    setIsLoadingHistory(true);
    setHistoryError(null);
    setExpandedRevisionFileDiffs(new Set());
    setRevisionDetailsById({});
    setRevisionDetailsLoading({});
    setRevisionDetailsError({});
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
      setHistoryError('Failed to load revision history');
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
  useImperativeHandle(ref, () => ({
    openTab: (tab: string) => {
      setPanelCollapsed(false);
      if (context === 'chat' && ['tools', 'changes', 'tasks', 'status', 'sessions'].includes(tab)) {
        setChatTab(tab as any);
        if (tab === 'changes' && revisions.length === 0) {
          loadRevisionHistory();
        }
        if (tab === 'sessions' && sessionsCount === 0) {
          loadSessions();
        }
      } else if (context === 'git' && ['files', 'diff', 'review'].includes(tab)) {
        setGitTab(tab as any);
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
  }), [context, revisions.length, sessionsCount, loadRevisionHistory, loadSessions]);

  // ── Persistence ──────────────────────────────────────────────────
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const storedWidth = Number(window.localStorage.getItem(PANEL_WIDTH_KEY));
    const storedCollapsed = window.localStorage.getItem(PANEL_COLLAPSED_KEY);
    const storedTab = window.localStorage.getItem(`${PANEL_TAB_KEY}.${context}`);

    if (Number.isFinite(storedWidth) && storedWidth >= PANEL_MIN && storedWidth <= PANEL_MAX) {
      setPanelWidth(storedWidth);
    }
    if (storedCollapsed === '1') {
      setPanelCollapsed(true);
    }
    if (storedTab) {
      if (context === 'chat' && ['tools', 'changes', 'tasks', 'status', 'sessions'].includes(storedTab)) {
        setChatTab(storedTab as any);
      } else if (context === 'git' && ['files', 'diff', 'review'].includes(storedTab)) {
        setGitTab(storedTab as any);
      }
    }
  }, [context]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(PANEL_WIDTH_KEY, String(Math.round(panelWidth)));
  }, [panelWidth]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(PANEL_COLLAPSED_KEY, panelCollapsed ? '1' : '0');
  }, [panelCollapsed]);

  // Persist active tab
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const tab = context === 'chat' ? chatTab : gitTab;
    window.localStorage.setItem(`${PANEL_TAB_KEY}.${context}`, tab);
  }, [context, chatTab, gitTab]);

  // ── Clear highlight after 3 seconds ──────────────────────────────
  useEffect(() => {
    if (activeToolId) {
      const timer = setTimeout(() => setActiveToolId(null), 3000);
      return () => clearTimeout(timer);
    }
  }, [activeToolId]);

  // ── Resize handler ───────────────────────────────────────────────
  const startResize = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    setPanelCollapsed(false);
    resizingRef.current = true;

    const onMouseMove = (moveEvent: MouseEvent) => {
      if (!resizingRef.current || !panelContainerRef.current) return;
      const rect = panelContainerRef.current.getBoundingClientRect();
      const rawWidth = rect.right - moveEvent.clientX;
      const maxByLayout = Math.max(PANEL_MIN, rect.width - 260);
      const clamped = Math.max(PANEL_MIN, Math.min(Math.min(PANEL_MAX, maxByLayout), rawWidth));
      setPanelWidth(clamped);
    };

    const onMouseUp = () => {
      resizingRef.current = false;
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
    };

    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'col-resize';
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  }, []);

  const isProcessing = context === 'chat' ? props.isProcessing : false;

  const handleRestoreSession = useCallback(async (sessionId: string) => {
    if (isProcessing) {
      setSessionRestoreError('Wait for current request to finish.');
      return;
    }
    if (sessionId === currentSessionId) {
      setSessionRestoreError('This is the current session.');
      return;
    }
    if (!window.confirm(`Restore session ${sessionId}?\n\nThis will replace the current conversation.`)) {
      return;
    }
    setIsLoadingSessions(true);
    setSessionRestoreError(null);
    try {
      const response = await apiService.restoreSession(sessionId);
      if (response.messages?.length) {
        setTimeout(() => {
          window.dispatchEvent(new CustomEvent('ledit:session-restored', {
            detail: { messages: response.messages }
          }));
        }, 400);
      }
      await loadSessions();
    } catch (error) {
      setSessionRestoreError(error instanceof Error ? error.message : 'Failed to restore session');
    } finally {
      setIsLoadingSessions(false);
    }
  }, [apiService, currentSessionId, isProcessing, loadSessions]);

  const buildRevisionFileKey = useCallback((file: RevisionFile | RevisionDetailFile, index: number) => {
    return `${file.file_revision_hash || file.path}::${index}`;
  }, []);

  const loadRevisionDetails = useCallback(async (revisionId: string) => {
    if (!revisionId || revisionDetailsById[revisionId] || revisionDetailsLoading[revisionId]) return;

    setRevisionDetailsLoading((prev) => ({ ...prev, [revisionId]: true }));
    setRevisionDetailsError((prev) => {
      const next = { ...prev };
      delete next[revisionId];
      return next;
    });

    try {
      const response = await apiService.getRevisionDetails(revisionId);
      const detailsMap: Record<string, string> = {};
      (response.revision?.files || []).forEach((file: RevisionDetailFile, index: number) => {
        detailsMap[buildRevisionFileKey(file, index)] = file.diff || '';
      });
      setRevisionDetailsById((prev) => ({ ...prev, [revisionId]: detailsMap }));
    } catch (error) {
      setRevisionDetailsError((prev) => ({
        ...prev,
        [revisionId]: error instanceof Error ? error.message : 'Failed to load revision details',
      }));
    } finally {
      setRevisionDetailsLoading((prev) => ({ ...prev, [revisionId]: false }));
    }
  }, [apiService, buildRevisionFileKey, revisionDetailsById, revisionDetailsLoading]);

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
    setExpandedTools(prev => {
      const newSet = new Set(prev);
      if (newSet.has(toolId)) newSet.delete(toolId);
      else newSet.add(toolId);
      return newSet;
    });
  };

  const toggleRevisionExpanded = (revisionId: string) => {
    setExpandedRevisionIds((prev) => {
      const next = new Set(prev);
      if (next.has(revisionId)) next.delete(revisionId);
      else {
        next.add(revisionId);
        loadRevisionDetails(revisionId);
      }
      return next;
    });
  };

  const toggleRevisionFileDiff = (diffKey: string) => {
    setExpandedRevisionFileDiffs((prev) => {
      const next = new Set(prev);
      if (next.has(diffKey)) next.delete(diffKey);
      else next.add(diffKey);
      return next;
    });
  };

  const handleRollback = useCallback(async (revisionId: string) => {
    if (!window.confirm(`Rollback to revision ${revisionId}?\n\nThis will undo all changes after this revision.`)) return;
    setIsLoadingHistory(true);
    setHistoryError(null);
    try {
      await apiService.rollbackToRevision(revisionId);
      window.location.reload();
    } catch (error) {
      console.error('Rollback failed:', error);
      setHistoryError(error instanceof Error ? error.message : 'Rollback failed');
    } finally {
      setIsLoadingHistory(false);
    }
  }, [apiService]);

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

  const getToolIcon = (toolName: string): ReactNode => {
    const iconMap: { [key: string]: ReactNode } = {
      'shell_command': <Terminal size={14} />,
      'read_file': <BookOpen size={14} />,
      'write_file': <Pencil size={14} />,
      'edit_file': <FileEdit size={14} />,
      'search_files': <Search size={14} />,
      'analyze_ui_screenshot': <Eye size={14} />,
      'analyze_image_content': <FlaskConical size={14} />,
      'web_search': <Globe size={14} />,
      'fetch_url': <ArrowDown size={14} />,
      'TodoWrite': <ClipboardList size={14} />,
      'TodoRead': <ClipboardList size={14} />,
      'view_history': <ScrollText size={14} />,
      'rollback_changes': <RotateCcw size={14} />,
      'mcp_tools': <Wrench size={14} />,
      'run_subagent': <Bot size={14} />,
      'run_parallel_subagents': <Bot size={14} />,
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
      case 'started': return <Rocket size={14} />;
      case 'running': return <Zap size={14} />;
      case 'completed': return <CheckCircle2 size={14} />;
      case 'error': return <XCircle size={14} />;
      default: return <Hourglass size={14} />;
    }
  };

  const formatDuration = (startTime: Date, endTime?: Date) => {
    const end = endTime || new Date();
    const duration = end.getTime() - startTime.getTime();
    if (duration < 1000) return `${duration}ms`;
    if (duration < 60000) return `${(duration / 1000).toFixed(1)}s`;
    return `${(duration / 60000).toFixed(1)}m`;
  };

  const formatToolDetail = (content: string) => {
    try {
      const parsed = JSON.parse(content);
      return JSON.stringify(parsed, null, 2);
    } catch {
      return content;
    }
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

  const formatDurationMs = (ms: number): string => {
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(0)}s`;
    const mins = Math.floor(ms / 60000);
    const secs = Math.floor((ms % 60000) / 1000);
    return `${mins}m ${secs}s`;
  };

  const summarizeRevision = (revision: Revision) => {
    let additions = 0;
    let deletions = 0;
    for (const file of revision.files) {
      additions += Number(file.lines_added || 0);
      deletions += Number(file.lines_deleted || 0);
    }
    return { fileCount: revision.files.length, additions, deletions };
  };

  const getOperationText = (operation: string) => {
    switch (operation) {
      case 'edited': return 'Modified';
      case 'created': return 'Created';
      case 'deleted': return 'Deleted';
      case 'renamed': return 'Renamed';
      default: return operation;
    }
  };

  // ── Diff renderer ────────────────────────────────────────────────

  const renderDiff = (diff: string) => {
    const lines = stripAnsiCodes(diff).split('\n');
    return (
      <div className="context-panel-diff-view" role="region" aria-label="Diff view">
        {lines.map((line, index) => {
          let lineClass = 'context-diff';
          if (line.startsWith('@@')) lineClass = 'context-diff context-diff-hunk';
          else if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('FILE:')) lineClass = 'context-diff context-diff-header';
          else if (line.startsWith('+') && !line.startsWith('+++')) lineClass = 'context-diff context-diff-add';
          else if (line.startsWith('-') && !line.startsWith('---')) lineClass = 'context-diff context-diff-del';
          return (
            <div key={`diff-line-${index}`} className={lineClass}>
              {line || ' '}
            </div>
          );
        })}
      </div>
    );
  };

  const renderGitDiffContent = (diff: string) => {
    const lines = stripAnsiCodes(diff).split('\n');
    return (
      <div className="context-panel-diff-view" role="region" aria-label="Git diff view">
        {lines.map((line, index) => {
          let lineClass = 'context-diff';
          if (line.startsWith('diff --git') || line.startsWith('index ') || line.startsWith('old mode') || line.startsWith('new mode'))
            lineClass = 'context-diff context-diff-file';
          else if (line.startsWith('@@')) lineClass = 'context-diff context-diff-hunk';
          else if (line.startsWith('+') && !line.startsWith('+++')) lineClass = 'context-diff context-diff-add';
          else if (line.startsWith('-') && !line.startsWith('---')) lineClass = 'context-diff context-diff-del';
          else if (line.startsWith('+++') || line.startsWith('---')) lineClass = 'context-diff context-diff-header';
          return (
            <div key={`git-diff-${index}`} className={lineClass}>
              {line || ' '}
            </div>
          );
        })}
      </div>
    );
  };

  // ── Chat computed values ─────────────────────────────────────────

  const toolExecutions = context === 'chat' ? props.toolExecutions : [];
  const currentTodos = context === 'chat' ? props.currentTodos : [];

  const activeToolCount = toolExecutions.filter(
    (tool) => tool.status === 'started' || tool.status === 'running'
  ).length;

  const historyCounts = revisions.length;

  const statusMetrics: StatusMetrics = useMemo(() => {
    if (context !== 'chat') {
      return { userMsgs: 0, assistantMsgs: 0, totalMsgs: 0, completedTools: 0, failedTools: 0, activeTools: 0, totalTools: 0, totalAdditions: 0, totalDeletions: 0, filesTouched: 0, topTools: [], maxToolCount: 1, duration: 0 };
    }

    const msgs = props.messages;
    const userMsgs = msgs.filter(m => m.type === 'user').length;
    const assistantMsgs = msgs.filter(m => m.type === 'assistant').length;
    const completedTools = toolExecutions.filter(t => t.status === 'completed').length;
    const failedTools = toolExecutions.filter(t => t.status === 'error').length;
    const activeTools = toolExecutions.filter(t => t.status === 'running' || t.status === 'started').length;

    const totalAdditions = revisions.reduce((sum, r) =>
      sum + r.files.reduce((fSum, f) => fSum + (f.lines_added || 0), 0), 0);
    const totalDeletions = revisions.reduce((sum, r) =>
      sum + r.files.reduce((fSum, f) => fSum + (f.lines_deleted || 0), 0), 0);

    const touchedFiles = new Set<string>();
    toolExecutions.forEach(t => {
      if (t.tool === 'write_file' || t.tool === 'edit_file') {
        try {
          const args = t.arguments ? JSON.parse(t.arguments) : {};
          if (args.path) touchedFiles.add(args.path);
        } catch { /* ignore */ }
      }
    });
    revisions.forEach(r => {
      r.files.forEach(f => touchedFiles.add(f.path));
    });

    const toolCounts: Record<string, number> = {};
    toolExecutions.forEach(t => {
      const name = isSubagentTool(t) ? 'subagent' : t.tool;
      toolCounts[name] = (toolCounts[name] || 0) + 1;
    });
    const sortedTools = Object.entries(toolCounts).sort((a, b) => b[1] - a[1]).slice(0, 6);
    const maxToolCount = sortedTools.length > 0 ? sortedTools[0][1] : 1;

    let duration = 0;
    if (msgs.length >= 2) {
      duration = msgs[msgs.length - 1].timestamp.getTime() - msgs[0].timestamp.getTime();
    }

    return { userMsgs, assistantMsgs, totalMsgs: userMsgs + assistantMsgs, completedTools, failedTools, activeTools, totalTools: toolExecutions.length, totalAdditions, totalDeletions, filesTouched: touchedFiles.size, topTools: sortedTools, maxToolCount, duration };
  }, [context, context === 'chat' ? props.messages : null, toolExecutions, revisions]);

  // ── Tab definitions ──────────────────────────────────────────────

  const chatPanelTabs: Array<{ id: 'tools' | 'changes' | 'tasks' | 'status' | 'sessions'; label: string; icon: ReactNode; count: string }> = [
    { id: 'tools', label: 'Tool Executions', icon: <Wrench size={14} />, count: activeToolCount > 0 ? `${activeToolCount} active` : `${toolExecutions.length} total` },
    { id: 'changes', label: 'Session Changes', icon: <History size={14} />, count: `${historyCounts} revisions` },
    { id: 'tasks', label: 'Tasks', icon: <ListTodo size={14} />, count: `${currentTodos.filter(t => t.status === 'in_progress').length || 0} active` },
    { id: 'sessions', label: 'Sessions', icon: <Clock size={14} />, count: `${sessionsCount}` },
    { id: 'status', label: 'Status', icon: <Activity size={14} />, count: `${statusMetrics.totalMsgs} msgs` },
  ];

  const gitPanelTabs: Array<{ id: 'files' | 'diff' | 'review'; label: string; icon: ReactNode; count: string }> = useMemo(() => {
    if (context !== 'git') return [];
    const gitProps = props as GitContextPanelProps;
    const totalFiles = gitProps.stagedFiles.length + gitProps.modifiedFiles.length + gitProps.untrackedFiles.length + gitProps.deletedFiles.length;
    return [
      { id: 'files', label: 'Files', icon: <FileCode size={14} />, count: `${totalFiles} files` },
      { id: 'diff', label: 'Diff', icon: <FileText size={14} />, count: gitProps.activeDiffPath || 'No selection' },
      { id: 'review', label: 'Review', icon: <ShieldCheck size={14} />, count: gitProps.deepReview ? gitProps.deepReview.status : 'No review' },
    ];
  }, [context, context === 'git' ? (props as GitContextPanelProps).stagedFiles : [], context === 'git' ? (props as GitContextPanelProps).modifiedFiles : [], context === 'git' ? (props as GitContextPanelProps).untrackedFiles : [], context === 'git' ? (props as GitContextPanelProps).deletedFiles : [], context === 'git' ? (props as GitContextPanelProps).activeDiffPath : null, context === 'git' ? (props as GitContextPanelProps).deepReview : null]);

  const activeTab = context === 'chat' ? (chatPanelTabs.find(t => t.id === chatTab) || chatPanelTabs[0]) : (gitPanelTabs.find(t => t.id === gitTab) || gitPanelTabs[0]);

  // ── Git helpers ──────────────────────────────────────────────────

  const getGitSectionFiles = (section: FileSection) => {
    if (context !== 'git') return [];
    const gitProps = props as GitContextPanelProps;
    switch (section) {
      case 'staged': return gitProps.stagedFiles;
      case 'modified': return gitProps.modifiedFiles;
      case 'untracked': return gitProps.untrackedFiles;
      case 'deleted': return gitProps.deletedFiles;
    }
  };

  const getGitSectionCount = (section: FileSection) => {
    return getGitSectionFiles(section).length;
  };

  const handleGitSelectAll = (section: FileSection) => {
    if (context !== 'git') return;
    const gitProps = props as GitContextPanelProps;
    const files = getGitSectionFiles(section);
    const newSelected = new Set(gitProps.selectedFiles);
    files.forEach(f => newSelected.add(`${section}:${f.path}`));
    // We cannot directly set selectedFiles since it's a prop
    // Instead, call onFileSelect for each
    files.forEach(f => {
      if (!gitProps.selectedFiles.has(`${section}:${f.path}`)) {
        gitProps.onFileSelect(section, f.path);
      }
    });
  };

  // ── Render: Tools Tab (Chat) ─────────────────────────────────────

  const renderToolsTab = () => (
    <div className="context-panel-tools-list">
      {toolExecutions.length === 0 ? (
        <div className="context-panel-empty">Tool calls will appear here.</div>
      ) : (
        toolExecutions.map((tool) => {
          const isSub = isSubagentTool(tool);
          const subagentPrompt = isSub ? getSubagentPrompt(tool) : undefined;

          return (
            <div
              key={tool.id}
              ref={(el) => { toolRefs.current[tool.id] = el; }}
              className={`tool-execution tool-${tool.status} ${isSub ? 'tool-subagent' : ''} ${activeToolId === tool.id ? 'tool-highlighted' : ''}`}
              onClick={() => toggleToolExpansion(tool.id)}
            >
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
                    ? (tool.persona ? `${tool.persona}` : (tool.subagentType === 'parallel' ? 'parallel subagents' : 'subagent'))
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
                      <div className="tool-detail-label"><FileEdit size={12} className="inline-icon" /> Task</div>
                      <pre className="subagent-prompt-detail">{stripAnsiCodes(subagentPrompt)}</pre>
                    </div>
                  )}
                  {tool.arguments && !isSub && (
                    <div className="tool-detail-section">
                      <div className="tool-detail-label"><ClipboardList size={12} className="inline-icon" /> Call</div>
                      <pre>{formatToolDetail(tool.arguments)}</pre>
                    </div>
                  )}
                  {tool.result && (
                    <div className="tool-detail-section">
                      <div className="tool-detail-label">{isSub ? <><BarChart3 size={12} className="inline-icon" /> Summary</> : <><FileText size={12} className="inline-icon" /> Response</>}</div>
                      <pre>{formatToolDetail(tool.result)}</pre>
                    </div>
                  )}
                </div>
              )}
            </div>
          );
        })
      )}
    </div>
  );

  // ── Render: History Tab (Chat) ───────────────────────────────────

  const renderHistoryTab = () => (
    <div className="context-panel-tools-list">
      <div className="history-toolbar">
        <button className="history-refresh-btn" onClick={loadRevisionHistory} disabled={isLoadingHistory}>
          <RotateCcw size={12} /> Refresh
        </button>
      </div>

      {historyError && <div className="history-error-inline">{historyError}</div>}

      {isLoadingHistory ? (
        <div className="context-panel-empty">Loading revision history...</div>
      ) : revisions.length === 0 ? (
        <div className="context-panel-empty">No revisions found yet.</div>
      ) : (
        revisions.map((revision) => {
          const summary = summarizeRevision(revision);
          const isExpanded = expandedRevisionIds.has(revision.revision_id);
          return (
            <div key={revision.revision_id} className="history-item">
              <button
                className="history-summary"
                onClick={() => toggleRevisionExpanded(revision.revision_id)}
                aria-expanded={isExpanded}
              >
                <span className="history-expand">{isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}</span>
                <span className="history-main">
                  <span className="history-id">{revision.revision_id}</span>
                  <span className="history-time">{formatRelativeTime(revision.timestamp)}</span>
                </span>
                <span className="history-stats">
                  <span>{summary.fileCount} files</span>
                  {summary.additions > 0 && <span className="additions">+{summary.additions}</span>}
                  {summary.deletions > 0 && <span className="deletions">-{summary.deletions}</span>}
                </span>
              </button>

              {isExpanded && (
                <div className="history-details">
                  {revision.files.map((file, i) => {
                    const fileKey = buildRevisionFileKey(file, i);
                    const expandedDiffKey = `${revision.revision_id}::${fileKey}`;
                    const isDiffExpanded = expandedRevisionFileDiffs.has(expandedDiffKey);
                    const fileDiff = revisionDetailsById[revision.revision_id]?.[fileKey];

                    return (
                      <div key={`${revision.revision_id}-${file.path}-${i}`} className="history-file-entry">
                        <button
                          className="history-file-row history-file-row-interactive"
                          onClick={() => toggleRevisionFileDiff(expandedDiffKey)}
                          aria-expanded={isDiffExpanded}
                        >
                          <span className="history-expand">
                            {isDiffExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                          </span>
                          <span className="history-file-op">{getOperationText(file.operation)}</span>
                          <span className="history-file-path" title={file.path}>{file.path}</span>
                          <span className="history-file-diff">
                            {file.lines_added > 0 && <span className="additions">+{file.lines_added}</span>}
                            {file.lines_deleted > 0 && <span className="deletions">-{file.lines_deleted}</span>}
                          </span>
                        </button>

                        {isDiffExpanded && (
                          <div className="history-file-diff-panel">
                            {revisionDetailsLoading[revision.revision_id] && !fileDiff && (
                              <div className="context-panel-empty">Loading file diff…</div>
                            )}
                            {revisionDetailsError[revision.revision_id] && (
                              <div className="history-error-inline">{revisionDetailsError[revision.revision_id]}</div>
                            )}
                            {!!fileDiff && (
                              <div className="history-diff-pre">{renderDiff(fileDiff)}</div>
                            )}
                          </div>
                        )}
                      </div>
                    );
                  })}
                  <button
                    className="history-rollback-btn"
                    onClick={() => handleRollback(revision.revision_id)}
                    disabled={isLoadingHistory}
                  >
                    <RotateCcw size={12} /> Rollback to this revision
                  </button>
                </div>
              )}
            </div>
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
                <span className="session-dir" title={session.working_directory}>{dirName}</span>
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

  const chatMessages = context === 'chat' ? props.messages : [];
  const chatLastError = context === 'chat' ? props.lastError : null;
  const chatQueryProgress = context === 'chat' ? props.queryProgress : null;
  const chatIsProcessing = context === 'chat' ? props.isProcessing : false;

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
              <span className="status-label">{chatQueryProgress?.message || 'Working...'}</span>
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
          {statusMetrics.duration > 0 && (
            <div className="status-metric status-metric-wide">
              <span className="status-metric-value">{formatDurationMs(statusMetrics.duration)}</span>
              <span className="status-metric-label">Duration</span>
            </div>
          )}
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
    </div>
  );

  // ── Render: Files Tab (Git) ──────────────────────────────────────

  const renderGitFilesTab = () => {
    if (context !== 'git') return null;
    const gitProps = props as GitContextPanelProps;
    const sections: Array<{ section: FileSection; title: string; cssClass: string }> = [
      { section: 'staged', title: 'Staged', cssClass: 'staged' },
      { section: 'modified', title: 'Modified', cssClass: 'modified' },
      { section: 'untracked', title: 'Untracked', cssClass: 'untracked' },
      { section: 'deleted', title: 'Deleted', cssClass: 'deleted' },
    ];

    const allEmpty = sections.every(s => getGitSectionCount(s.section) === 0);

    if (allEmpty) {
      return (
        <div className="no-changes">
          <span className="no-changes-icon"><FileCode size={32} /></span>
          <p>No changes detected</p>
        </div>
      );
    }

    return (
      <div className="context-panel-file-list">
        {sections.map(({ section, title, cssClass }) => {
          const files = getGitSectionFiles(section);
          if (files.length === 0) return null;

          return (
            <div key={section} className={`file-section ${cssClass}`}>
              <div className="file-section-header">
                <h3>{title}</h3>
                <button
                  className="section-select-btn"
                  onClick={() => handleGitSelectAll(section)}
                >
                  <Check size={12} />
                  <span className="section-select-count">{files.length}</span>
                </button>
              </div>
              <div className="file-list">
                {files.map((file) => {
                  const key = `${section}:${file.path}`;
                  const isSelected = gitProps.selectedFiles.has(key);
                  const isActiveDiff = gitProps.activeDiffSelectionKey === key;

                  return (
                    <div
                      key={key}
                      className={`file-item ${isSelected ? 'selected' : ''} ${isActiveDiff ? 'active-diff' : ''}`}
                    >
                      <input
                        type="checkbox"
                        checked={isSelected}
                        onChange={() => gitProps.onFileSelect(section, file.path)}
                        onClick={(e) => e.stopPropagation()}
                      />
                      <span className="file-icon"><File size={14} /></span>
                      <span className="file-path">{file.path}</span>
                      {file.changes && (
                        <span className="file-changes">
                          {file.changes.additions > 0 && <span className="additions">+{file.changes.additions}</span>}
                          {file.changes.deletions > 0 && <span className="deletions">-{file.changes.deletions}</span>}
                        </span>
                      )}
                      <button
                        className="file-diff-btn"
                        onClick={() => gitProps.onPreviewFile(section, file.path)}
                        title="View diff"
                      >
                        <FileText size={14} />
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    );
  };

  // ── Render: Diff Tab (Git) ───────────────────────────────────────

  const renderGitDiffTab = () => {
    if (context !== 'git') return null;
    const gitProps = props as GitContextPanelProps;

    if (!gitProps.activeDiffPath) {
      return (
        <div className="no-changes">
          <span className="no-changes-icon"><FileText size={32} /></span>
          <p>Select a file to view its diff</p>
        </div>
      );
    }

    const diffContent = (() => {
      if (gitProps.isDiffLoading) return null;
      if (gitProps.diffError) return null;
      if (!gitProps.activeDiff) return null;

      switch (gitProps.diffMode) {
        case 'staged': return gitProps.activeDiff.staged_diff || '(no staged changes)';
        case 'unstaged': return gitProps.activeDiff.unstaged_diff || '(no unstaged changes)';
        default: return gitProps.activeDiff.diff || '(no diff)';
      }
    })();

    return (
      <div className="context-panel-diff-container">
        <div className="context-panel-diff-header">
          <h3>{gitProps.activeDiffPath}</h3>
        </div>
        <div className="context-panel-diff-mode-tabs">
          {(['combined', 'staged', 'unstaged'] as const).map((mode) => (
            <button
              key={mode}
              className={`context-panel-diff-mode-tab ${gitProps.diffMode === mode ? 'active' : ''}`}
              onClick={() => gitProps.onDiffModeChange(mode)}
            >
              {mode}
            </button>
          ))}
        </div>
        <div className="context-panel-diff-content">
          {gitProps.isDiffLoading && (
            <div className="context-panel-diff-loading">
              <Loader2 size={16} className="context-panel-spinner" /> Loading diff…
            </div>
          )}
          {gitProps.diffError && (
            <div className="context-panel-diff-error">{gitProps.diffError}</div>
          )}
          {!gitProps.isDiffLoading && !gitProps.diffError && diffContent && (
            renderGitDiffContent(diffContent)
          )}
        </div>
      </div>
    );
  };

  // ── Render: Review Tab (Git) ─────────────────────────────────────

  const renderGitReviewTab = () => {
    if (context !== 'git') return null;
    const gitProps = props as GitContextPanelProps;

    if (!gitProps.deepReview && !gitProps.isReviewLoading && !gitProps.reviewError) {
      return (
        <div className="no-changes">
          <span className="no-changes-icon"><ShieldCheck size={32} /></span>
          <p>No review generated yet</p>
          <p style={{ fontSize: '12px', opacity: 0.7 }}>Click "Review" in the git actions bar to generate an AI code review</p>
        </div>
      );
    }

    return (
      <div className="context-panel-review">
        <div className="review-meta">
          {gitProps.deepReview?.status && (
            <span className={`review-status status-${gitProps.deepReview.status}`}>
              {gitProps.deepReview.status}
            </span>
          )}
          {gitProps.deepReview?.model && (
            <span className="review-model">{gitProps.deepReview.model}</span>
          )}
        </div>

        <div className="review-scroll-area">
          {gitProps.isReviewLoading ? (
            <div className="review-loading">
              <Loader2 size={24} className="context-panel-spinner" />
              <p>Running deep review…</p>
            </div>
          ) : gitProps.reviewError ? (
            <div className="review-error">
              <AlertTriangle size={16} />
              <p>{gitProps.reviewError}</p>
            </div>
          ) : gitProps.deepReview ? (
            <>
              {gitProps.deepReview.feedback && (
                <div className="review-section">
                  <h4>Feedback</h4>
                  <pre>{gitProps.deepReview.feedback}</pre>
                </div>
              )}
              {gitProps.deepReview.detailed_guidance && (
                <div className="review-section">
                  <h4>Detailed Guidance</h4>
                  <pre>{gitProps.deepReview.detailed_guidance}</pre>
                </div>
              )}
              {gitProps.reviewFixLogs.length > 0 && (
                <div className="review-section">
                  <h4>Fix Progress</h4>
                  <div className="review-fix-logs">
                    {gitProps.reviewFixLogs.map((log, i) => (
                      <div key={i} className="review-fix-log-entry">{log}</div>
                    ))}
                    {gitProps.isReviewFixing && (
                      <div className="review-fix-log-entry review-fix-active">
                        <Loader2 size={12} className="context-panel-spinner" /> Working…
                      </div>
                    )}
                  </div>
                </div>
              )}
              {gitProps.reviewFixResult && (
                <div className="review-section">
                  <h4>Fix Result</h4>
                  <pre>{gitProps.reviewFixResult}</pre>
                </div>
              )}
            </>
          ) : null}
        </div>

        {gitProps.deepReview && !gitProps.isReviewLoading && (
          <div className="review-footer">
            <button
              onClick={gitProps.onRunReview}
              disabled={gitProps.isReviewLoading || gitProps.isReviewFixing}
              className="review-action-btn"
            >
              <RotateCcw size={14} /> Re-run Review
            </button>
            {gitProps.deepReview.review_output && (
              <button
                onClick={gitProps.onFixFromReview}
                disabled={gitProps.isReviewFixing || gitProps.isReviewLoading}
                className="review-action-btn review-action-primary"
              >
                {gitProps.isReviewFixing ? 'Fixing…' : 'Apply Fixes'}
              </button>
            )}
          </div>
        )}
      </div>
    );
  };

  // ── Main Render ──────────────────────────────────────────────────

  const isMobileLayout = props.isMobileLayout ?? false;

  const tabs = context === 'chat' ? chatPanelTabs : gitPanelTabs;
  const activeTabId = context === 'chat' ? chatTab : gitTab;

  const handleTabClick = (tabId: string) => {
    setPanelCollapsed(false);
    if (context === 'chat') {
      const id = tabId as 'tools' | 'changes' | 'tasks' | 'status' | 'sessions';
      setChatTab(id);
      if (id === 'changes' && revisions.length === 0) loadRevisionHistory();
      if (id === 'sessions' && sessionsCount === 0) loadSessions();
    } else {
      setGitTab(tabId as 'files' | 'diff' | 'review');
    }
  };

  const renderTabContent = () => {
    if (context === 'chat') {
      switch (chatTab) {
        case 'tools': return renderToolsTab();
        case 'changes': return (
          <RevisionListPanel
            mode="session"
            onOpenDiff={(props as ChatContextPanelProps).onOpenRevisionDiff || (() => {})}
          />
        );
        case 'tasks': return <div className="side-panel-tasks"><TodoPanel todos={currentTodos || []} isLoading={isProcessing && currentTodos.length === 0} /></div>;
        case 'sessions': return renderSessionsTab();
        case 'status': return renderStatusTab();
        default: return renderToolsTab();
      }
    } else {
      switch (gitTab) {
        case 'files': return renderGitFilesTab();
        case 'diff': return renderGitDiffTab();
        case 'review': return renderGitReviewTab();
        default: return renderGitFilesTab();
      }
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
      <aside
        className={`context-panel ${panelCollapsed ? 'collapsed' : ''}`}
        aria-label="Context panel"
        style={panelCollapsed || isMobileLayout ? undefined : { width: `${panelWidth}px` }}
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
              <span className="tool-count">{activeTab.count}</span>
            </div>
            <div className="side-panel-body">
              {renderTabContent()}
            </div>
          </div>
        )}
      </aside>
    </>
  );
});

ContextPanel.displayName = 'ContextPanel';

export default ContextPanel;
