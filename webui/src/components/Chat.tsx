import React, { useState, useRef, useEffect, useCallback, useMemo, type ReactNode } from 'react';
import {
  Terminal, BookOpen, FileEdit, Pencil, Search, Eye, FlaskConical,
  Globe, ArrowDown, ClipboardList, ScrollText, RotateCcw,
  Wrench, Rocket, Zap, CheckCircle2, XCircle, Hourglass,
  Bot, Copy, AlertTriangle, ChevronDown, ChevronRight,
  BarChart3, FileText, PanelRightOpen, PanelRightClose, History, Clock, Activity, FileCode,
  ListTodo, ExternalLink, CheckCircle, Circle, Loader2, Minus
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import CommandInput from './CommandInput';
import { stripAnsiCodes } from '../utils/ansi';
import { parseMessageSegments } from '../utils/messageSegments';
import TodoPanel from './TodoPanel';
import { ApiService } from '../services/api';
import './Chat.css';

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string;  // Chain-of-thought content from content_type: "reasoning"
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

interface RevisionDetailFile {
  file_revision_hash?: string;
  path: string;
  operation: string;
  lines_added: number;
  lines_deleted: number;
  original_code: string;
  new_code: string;
  diff: string;
}

interface ChatProps {
  messages: Message[];
  onSendMessage: (message: string) => void;
  onQueueMessage: (message: string) => void;
  queuedMessagesCount: number;
  inputValue: string;
  onInputChange: (value: string) => void;
  isProcessing?: boolean;
  lastError?: string | null;
  toolExecutions?: ToolExecution[];
  queryProgress?: any;
  currentTodos?: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
}

const SIDE_PANEL_WIDTH_KEY = 'ledit.chat.sidePanelWidth';
const SIDE_PANEL_COLLAPSED_KEY = 'ledit.chat.sidePanelCollapsed';
const SIDE_PANEL_MIN = 280;
const SIDE_PANEL_MAX = 760;
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

const Chat: React.FC<ChatProps> = ({
  messages,
  onSendMessage,
  onQueueMessage,
  queuedMessagesCount,
  inputValue,
  onInputChange,
  isProcessing = false,
  lastError = null,
  toolExecutions = [],
  queryProgress = null,
  currentTodos = []
}) => {
  const apiService = ApiService.getInstance();
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set());
  const [sidePanelCollapsed, setSidePanelCollapsed] = useState(false);
  const [sidePanelWidth, setSidePanelWidth] = useState(360);
  const [sidePanelTab, setSidePanelTab] = useState<'tools' | 'history' | 'tasks' | 'status' | 'sessions'>('tools');
  const [isMobileLayout, setIsMobileLayout] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false;
    return window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH;
  });
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [expandedRevisionIds, setExpandedRevisionIds] = useState<Set<string>>(new Set());
  const [expandedRevisionFileDiffs, setExpandedRevisionFileDiffs] = useState<Set<string>>(new Set());
  const [revisionDetailsById, setRevisionDetailsById] = useState<Record<string, Record<string, string>>>({});
  const [revisionDetailsLoading, setRevisionDetailsLoading] = useState<Record<string, boolean>>({});
  const [revisionDetailsError, setRevisionDetailsError] = useState<Record<string, string>>({});
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [sessions, setSessions] = useState<Array<{
    session_id: string;
    name: string;
    working_directory: string;
    last_updated: string;
    message_count: number;
    total_tokens: number;
  }>>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string>('');
  const [isLoadingSessions, setIsLoadingSessions] = useState(false);
  const [sessionRestoreError, setSessionRestoreError] = useState<string | null>(null);
  const [sessionsCount, setSessionsCount] = useState(0);
  const [activeToolId, setActiveToolId] = useState<string | null>(null);
  const toolRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const chatMainRef = useRef<HTMLDivElement>(null);
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const resizingSidePanelRef = useRef(false);
  const historyLoadRequestRef = useRef(0);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const storedWidth = Number(window.localStorage.getItem(SIDE_PANEL_WIDTH_KEY));
    const storedCollapsed = window.localStorage.getItem(SIDE_PANEL_COLLAPSED_KEY);

    if (Number.isFinite(storedWidth) && storedWidth >= SIDE_PANEL_MIN && storedWidth <= SIDE_PANEL_MAX) {
      setSidePanelWidth(storedWidth);
    }
    if (storedCollapsed === '1') {
      setSidePanelCollapsed(true);
    }
  }, []);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const onResize = () => {
      setIsMobileLayout(window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH);
    };
    onResize();
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(SIDE_PANEL_WIDTH_KEY, String(Math.round(sidePanelWidth)));
  }, [sidePanelWidth]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(SIDE_PANEL_COLLAPSED_KEY, sidePanelCollapsed ? '1' : '0');
  }, [sidePanelCollapsed]);

  useEffect(() => {
    if (chatContainerRef.current) {
      chatContainerRef.current.scrollTop = chatContainerRef.current.scrollHeight;
    }
  }, [messages, toolExecutions, queryProgress, isProcessing]);

  const loadRevisionHistory = useCallback(async () => {
    const requestId = ++historyLoadRequestRef.current;
    setIsLoadingHistory(true);
    setHistoryError(null);
    // Refresh should invalidate detail caches to avoid stale/partial diff states.
    setExpandedRevisionFileDiffs(new Set());
    setRevisionDetailsById({});
    setRevisionDetailsLoading({});
    setRevisionDetailsError({});
    try {
      const response = await apiService.getChangelog();
      if (requestId !== historyLoadRequestRef.current) {
        return;
      }
      const normalized = (response.revisions || []).map(normalizeRevision).sort((a, b) => {
        return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
      });
      setRevisions(normalized);
      if (normalized.length > 0) {
        setExpandedRevisionIds(new Set([normalized[0].revision_id]));
      } else {
        setExpandedRevisionIds(new Set());
      }
    } catch (error) {
      if (requestId !== historyLoadRequestRef.current) {
        return;
      }
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
      // Dispatch event so App.tsx can populate the chat with restored messages.
      // Keep a short delay so websocket/provider-state updates settle before hydration.
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
    if (!revisionId) return;
    if (revisionDetailsById[revisionId] || revisionDetailsLoading[revisionId]) return;

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

  useEffect(() => {
    if (sidePanelTab === 'history' && revisions.length === 0 && !isLoadingHistory) {
      loadRevisionHistory();
    }
  }, [sidePanelTab, revisions.length, isLoadingHistory, loadRevisionHistory]);

  useEffect(() => {
    if (sidePanelTab === 'sessions' && sessionsCount === 0 && !isLoadingSessions) {
      loadSessions();
    }
  }, [sidePanelTab, sessionsCount, isLoadingSessions, loadSessions]);

  useEffect(() => {
    if (expandedRevisionIds.size === 0) {
      return;
    }

    expandedRevisionIds.forEach((revisionId) => {
      loadRevisionDetails(revisionId);
    });
  }, [expandedRevisionIds, loadRevisionDetails]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    const openHistoryPanel = () => {
      setSidePanelCollapsed(false);
      setSidePanelTab('history');
      loadRevisionHistory();
    };

    window.addEventListener('ledit:open-revision-history', openHistoryPanel);
    return () => window.removeEventListener('ledit:open-revision-history', openHistoryPanel);
  }, [loadRevisionHistory]);

  useEffect(() => {
    // Clear highlight after 3 seconds
    if (activeToolId) {
      const timer = setTimeout(() => setActiveToolId(null), 3000);
      return () => clearTimeout(timer);
    }
  }, [activeToolId]);

  const toggleToolExpansion = (toolId: string) => {
    setExpandedTools(prev => {
      const newSet = new Set(prev);
      if (newSet.has(toolId)) {
        newSet.delete(toolId);
      } else {
        newSet.add(toolId);
      }
      return newSet;
    });
  };

  const toggleRevisionExpanded = (revisionId: string) => {
    setExpandedRevisionIds((prev) => {
      const next = new Set(prev);
      if (next.has(revisionId)) {
        next.delete(revisionId);
      } else {
        next.add(revisionId);
        loadRevisionDetails(revisionId);
      }
      return next;
    });
  };

  const toggleRevisionFileDiff = (diffKey: string) => {
    setExpandedRevisionFileDiffs((prev) => {
      const next = new Set(prev);
      if (next.has(diffKey)) {
        next.delete(diffKey);
      } else {
        next.add(diffKey);
      }
      return next;
    });
  };

  const startSidePanelResize = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    setSidePanelCollapsed(false);
    resizingSidePanelRef.current = true;

    const onMouseMove = (moveEvent: MouseEvent) => {
      if (!resizingSidePanelRef.current || !chatMainRef.current) return;
      const rect = chatMainRef.current.getBoundingClientRect();
      const rawWidth = rect.right - moveEvent.clientX;
      const maxByLayout = Math.max(SIDE_PANEL_MIN, rect.width - 260);
      const clamped = Math.max(SIDE_PANEL_MIN, Math.min(Math.min(SIDE_PANEL_MAX, maxByLayout), rawWidth));
      setSidePanelWidth(clamped);
    };

    const onMouseUp = () => {
      resizingSidePanelRef.current = false;
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

  const handleRollback = useCallback(async (revisionId: string) => {
    if (!window.confirm(`Rollback to revision ${revisionId}?\n\nThis will undo all changes after this revision.`)) {
      return;
    }

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

  const handleToolPillClick = useCallback((toolId: string) => {
    setSidePanelCollapsed(false);
    setSidePanelTab('tools');
    setActiveToolId(toolId);
    setTimeout(() => {
      const el = toolRefs.current[toolId];
      if (el != null) {
        el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
      }
    }, 100);
  }, []);

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
      'mcp_tools': <Wrench size={14} />
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
    if (duration < 1000) {
      return `${duration}ms`;
    } else if (duration < 60000) {
      return `${(duration / 1000).toFixed(1)}s`;
    } else {
      return `${(duration / 60000).toFixed(1)}m`;
    }
  };

  const formatToolDetail = (content: string) => {
    try {
      const parsed = JSON.parse(content);
      return JSON.stringify(parsed, null, 2);
    } catch {
      return content;
    }
  };

  const formatTime = (date: Date) => {
    return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
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

  const copyToClipboard = useCallback((text: string) => {
    navigator.clipboard.writeText(text);
  }, []);

  const activeToolCount = toolExecutions.filter(
    (tool) => tool.status === 'started' || tool.status === 'running'
  ).length;

  const renderContent = (content: string) => {
    const cleaned = stripAnsiCodes(content);

    return (
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          code({ inline, className, children, ...props }: any) {
            const languageMatch = /language-(\w+)/.exec(className || '');
            const language = languageMatch ? languageMatch[1] : '';

            if (inline) {
              return (
                <code className="inline-code" {...props}>
                  {children}
                </code>
              );
            }

            return (
              <pre className="code-block">
                <span className="code-language">{language || 'text'}</span>
                <code className={className} {...props}>
                  {children}
                </code>
              </pre>
            );
          },
          a({ href, children, ...props }: any) {
            return (
              <a href={href} target="_blank" rel="noreferrer" {...props}>
                {children}
              </a>
            );
          },
        }}
      >
        {cleaned}
      </ReactMarkdown>
    );
  };

  const renderMessageSegments = (content: string): ReactNode => {
    try {
      const cleaned = stripAnsiCodes(content);
      const segments = parseMessageSegments(cleaned);
    
    return (
      <div className="message-segments">
        {segments.map((segment, idx) => {
          switch (segment.type) {
            case 'text':
              // Render text as markdown (same as current renderContent for non-traces)
              return (
                <div key={`seg-${idx}`} className="segment-text">
                  <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    components={{
                      code({ inline, className, children, ...props }: any) {
                        const languageMatch = /language-(\w+)/.exec(className || '');
                        const language = languageMatch ? languageMatch[1] : '';
                        if (inline) {
                          return <code className="inline-code" {...props}>{children}</code>;
                        }
                        return (
                          <pre className="code-block">
                            <span className="code-language">{language || 'text'}</span>
                            <code className={className} {...props}>{children}</code>
                          </pre>
                        );
                      },
                      a({ href, children, ...props }: any) {
                        return <a href={href} target="_blank" rel="noreferrer" {...props}>{children}</a>;
                      },
                    }}
                  >
                    {segment.content}
                  </ReactMarkdown>
                </div>
              );
            
            case 'tool_call':
              return (
                <div 
                  key={`seg-${idx}`} 
                  className="segment-tool-call"
                  role="button"
                  tabIndex={0}
                  aria-label={`View ${segment.toolName} execution details`}
                  onClick={() => {
                    const matchingTool = toolExecutions.find(t => 
                      t.tool === segment.toolName.split('(')[0]
                    );
                    if (matchingTool) {
                      handleToolPillClick(matchingTool.id);
                    }
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      const matchingTool = toolExecutions.find(t => 
                        t.tool === segment.toolName.split('(')[0]
                      );
                      if (matchingTool) {
                        handleToolPillClick(matchingTool.id);
                      }
                    }
                  }}
                >
                  <span className="tool-pill-icon">{getToolIcon(segment.toolName.split('(')[0])}</span>
                  <span className="tool-pill-name">{segment.summary || segment.toolName}</span>
                  <ExternalLink size={10} className="tool-pill-link-icon" />
                </div>
              );
            
            case 'todo_update':
              // Render as a compact inline todo summary
              return (
                <div key={`seg-${idx}`} className="segment-todo-summary">
                  {segment.todos.map((todo, todoIdx) => (
                    <span key={`todo-${todoIdx}`} className={`inline-todo inline-todo-${todo.status}`}>
                      <span className="inline-todo-icon">
                        {todo.status === 'completed' ? <CheckCircle size={10} /> :
                         todo.status === 'in_progress' ? <Loader2 size={10} /> :
                         todo.status === 'cancelled' ? <Minus size={10} /> :
                         <Circle size={10} />}
                      </span>
                      {todo.content}
                    </span>
                  ))}
                </div>
              );
            
            case 'progress':
              // Skip progress segments in the main chat (they're ephemeral)
              return null;
            
            case 'result':
              // Skip result segments in the main chat 
              return null;
            
            default:
              return null;
          }
        })}
      </div>
    );
    } catch {
      return renderContent(content);
    }
  };

  const renderDiff = (diff: string) => {
    const lines = stripAnsiCodes(diff).split('\n');
    return (
      <div className="history-diff-view" role="region" aria-label="Revision file diff">
        {lines.map((line, index) => {
          let lineClass = 'context';
          if (line.startsWith('@@')) lineClass = 'hunk';
          else if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('FILE:')) lineClass = 'header';
          else if (line.startsWith('+') && !line.startsWith('+++')) lineClass = 'add';
          else if (line.startsWith('-') && !line.startsWith('---')) lineClass = 'del';
          return (
            <div key={`diff-line-${index}`} className={`history-diff-line ${lineClass}`}>
              {line || ' '}
            </div>
          );
        })}
      </div>
    );
  };

  const historyCounts = useMemo(() => {
    return revisions.length;
  }, [revisions.length]);

  const statusMetrics = useMemo(() => {
    const userMsgs = messages.filter(m => m.type === 'user').length;
    const assistantMsgs = messages.filter(m => m.type === 'assistant').length;
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
    const sortedTools = Object.entries(toolCounts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 6);
    const maxToolCount = sortedTools.length > 0 ? sortedTools[0][1] : 1;

    let duration = 0;
    if (messages.length >= 2) {
      duration = messages[messages.length - 1].timestamp.getTime() - messages[0].timestamp.getTime();
    }

    return {
      userMsgs, assistantMsgs, totalMsgs: userMsgs + assistantMsgs,
      completedTools, failedTools, activeTools, totalTools: toolExecutions.length,
      totalAdditions, totalDeletions,
      filesTouched: touchedFiles.size,
      topTools: sortedTools, maxToolCount,
      duration,
    };
  }, [messages, toolExecutions, revisions]);

  const formatDurationMs = (ms: number): string => {
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(0)}s`;
    const mins = Math.floor(ms / 60000);
    const secs = Math.floor((ms % 60000) / 1000);
    return `${mins}m ${secs}s`;
  };

  const panelTabs: Array<{ id: 'tools' | 'history' | 'tasks' | 'status' | 'sessions'; label: string; icon: ReactNode; count: string }> = [
    {
      id: 'tools',
      label: 'Tool Executions',
      icon: <Wrench size={14} />,
      count: activeToolCount > 0 ? `${activeToolCount} active` : `${toolExecutions.length} total`,
    },
    {
      id: 'history',
      label: 'Revision History',
      icon: <History size={14} />,
      count: `${historyCounts} revisions`,
    },
    {
      id: 'tasks',
      label: 'Tasks',
      icon: <ListTodo size={14} />,
      count: `${currentTodos?.filter(t => t.status === 'in_progress').length || 0} active`,
    },
    {
      id: 'sessions',
      label: 'Sessions',
      icon: <Clock size={14} />,
      count: `${sessionsCount}`,
    },
    {
      id: 'status',
      label: 'Status',
      icon: <Activity size={14} />,
      count: `${statusMetrics.totalMsgs} msgs`,
    },
  ];

  const activeTab = panelTabs.find((tab) => tab.id === sidePanelTab) || panelTabs[0];

  return (
    <div className="chat-shell">
      <div
        className={`chat-main ${sidePanelCollapsed ? 'panel-collapsed' : ''}`}
        ref={chatMainRef}
        style={isMobileLayout ? undefined : {
          gridTemplateColumns: sidePanelCollapsed
            ? 'minmax(0, 1fr) 0 52px'
            : `minmax(0, 1fr) 6px ${sidePanelWidth}px`,
        }}
      >
        <div className="chat-container" ref={chatContainerRef}>
          {messages.length === 0 ? (
            <div className="welcome-message">
              <div className="welcome-icon"><Bot size={32} /></div>
              <div className="welcome-text">
                Welcome to ledit! I'm ready to help you with code analysis, editing, and more.
              </div>
              <div className="welcome-hint">
                Try asking: "Show me the project structure" or "Find the main function"
              </div>
            </div>
          ) : (
            messages.map((message) => (
              <div
                key={message.id}
                className={`message ${message.type}`}
                role={message.type === 'user' ? 'user-message' : 'assistant-message'}
                aria-label={`${message.type} message`}
              >
                <div className="message-bubble">
                  <button
                    className="copy-button"
                    onClick={() => copyToClipboard(message.content)}
                    title="Copy message"
                    aria-label="Copy message"
                  >
                    <Copy size={14} />
                  </button>
                  <div className="message-content">
                    {message.type === 'assistant' 
                      ? (
                        <>
                          {message.reasoning && message.reasoning.trim() && (
                            <details className="reasoning-block" open={false}>
                              <summary className="reasoning-summary">
                                <span className="reasoning-icon">💭</span>
                                <span>Reasoning</span>
                                <span className="reasoning-toggle">▶</span>
                              </summary>
                              <div className="reasoning-content">
                                {renderContent(message.reasoning)}
                              </div>
                            </details>
                          )}
                          {renderMessageSegments(message.content)}
                        </>
                      )
                      : renderContent(message.content)
                    }
                  </div>
                  <div className="message-timestamp">
                    {formatTime(message.timestamp)}
                  </div>
                </div>
              </div>
            ))
          )}

          {queryProgress && (
            <div className="query-progress">
              <div className="progress-header">
                <span className="progress-icon"><Zap size={14} /></span>
                <span className="progress-text">{queryProgress.message || 'Processing...'}</span>
              </div>
              {queryProgress.details && (
                <div className="progress-details">
                  {queryProgress.details}
                </div>
              )}
            </div>
          )}

          {isProcessing && toolExecutions.length === 0 && !queryProgress && (
            <div className="processing-indicator">
              <div className="processing-content">
                <div className="processing-spinner"><Zap size={14} /></div>
                <div className="processing-text">Processing your request...</div>
              </div>
            </div>
          )}

          {lastError && (
            <div className="error-indicator">
              <div className="error-content">
                <div className="error-icon"><AlertTriangle size={14} /></div>
                <div className="error-text">{lastError}</div>
              </div>
            </div>
          )}
        </div>

        {!sidePanelCollapsed && !isMobileLayout && (
          <div
          className={`chat-side-resizer ${sidePanelCollapsed ? 'hidden' : ''}`}
            onMouseDown={startSidePanelResize}
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize context panel"
          />
        )}
        <aside
          className={`chat-side-panel ${sidePanelCollapsed ? 'collapsed' : ''}`}
          aria-label="Context side panel"
          style={sidePanelCollapsed || isMobileLayout ? undefined : { width: `${sidePanelWidth}px` }}
        >
          <div className="side-panel-rail">
            {panelTabs.map((tab) => (
              <button
                key={tab.id}
                className={`side-rail-btn ${sidePanelTab === tab.id ? 'active' : ''}`}
                onClick={() => {
                  setSidePanelTab(tab.id);
                  setSidePanelCollapsed(false);
                  if (tab.id === 'history' && revisions.length === 0) {
                    loadRevisionHistory();
                  }
                  if (tab.id === 'sessions' && sessionsCount === 0) {
                    loadSessions();
                  }
                }}
                title={tab.label}
                aria-label={tab.label}
                aria-pressed={sidePanelTab === tab.id}
              >
                {tab.icon}
              </button>
            ))}
            <button
              className="side-collapse-btn"
              onClick={() => setSidePanelCollapsed((prev) => !prev)}
              title={sidePanelCollapsed ? 'Expand side panel' : 'Collapse side panel'}
            >
              {sidePanelCollapsed ? <PanelRightOpen size={14} /> : <PanelRightClose size={14} />}
            </button>
          </div>

          {!sidePanelCollapsed && (
            <div className="side-panel-content">
              <div className="side-panel-header">
                <div className="side-panel-title">
                  {activeTab.icon}
                  <h4>{activeTab.label}</h4>
                </div>
                <span className="tool-count">{activeTab.count}</span>
              </div>
              <div className="side-panel-body">
                {sidePanelTab === 'tools' ? (
                <>
                  <div className="chat-tools-list">
                    {toolExecutions.length === 0 ? (
                      <div className="chat-tools-empty">Tool calls will appear here.</div>
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
                </>
              ) : sidePanelTab === 'history' ? (
                <>
                  <div className="chat-tools-list">
                    <div className="history-toolbar">
                      <button className="history-refresh-btn" onClick={loadRevisionHistory} disabled={isLoadingHistory}>
                        <RotateCcw size={12} /> Refresh
                      </button>
                    </div>

                    {historyError && <div className="history-error-inline">{historyError}</div>}

                    {isLoadingHistory ? (
                      <div className="chat-tools-empty">Loading revision history...</div>
                    ) : revisions.length === 0 ? (
                      <div className="chat-tools-empty">No revisions found yet.</div>
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
                                            <div className="chat-tools-empty">Loading file diff…</div>
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
                </>
              ) : sidePanelTab === 'sessions' ? (
                <>
                  <div className="chat-tools-list">
                    <div className="history-toolbar">
                      <button className="history-refresh-btn" onClick={loadSessions} disabled={isLoadingSessions}>
                        <RotateCcw size={12} /> Refresh
                      </button>
                    </div>

                    {sessionRestoreError && <div className="history-error-inline">{sessionRestoreError}</div>}

                    {isLoadingSessions ? (
                      <div className="chat-tools-empty">Loading sessions...</div>
                    ) : sessions.length === 0 ? (
                      <div className="chat-tools-empty">No saved sessions found.</div>
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
                </>
              ) : sidePanelTab === 'tasks' ? (
                <div className="side-panel-tasks">
                  <TodoPanel todos={currentTodos || []} isLoading={isProcessing && (currentTodos || []).length === 0} />
                </div>
              ) : (
                <div className="chat-status-panel">
                  <div className="status-section">
                    <div className="status-section-title">
                      <Activity size={12} /> Processing
                    </div>
                    <div className="status-row">
                      {isProcessing ? (
                        <>
                          <span className="status-dot-indicator active" />
                          <span className="status-label">{queryProgress?.message || 'Working...'}</span>
                        </>
                      ) : lastError ? (
                        <>
                          <span className="status-dot-indicator error" />
                          <span className="status-label">{lastError}</span>
                        </>
                      ) : messages.length === 0 ? (
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
              )}
              </div>
            </div>
          )}
        </aside>
      </div>

      <div className="input-container">
        <CommandInput
          value={inputValue}
          onChange={onInputChange}
          onSend={onSendMessage}
          onQueue={onQueueMessage}
          placeholder="Ask me anything about your code..."
          multiline={true}
          autoFocus={true}
          isProcessing={isProcessing}
          queuedCount={queuedMessagesCount}
        />
      </div>
    </div>
  );
};

export default Chat;
