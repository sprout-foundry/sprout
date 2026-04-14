import { useRef, useEffect, useCallback, useState, useMemo, useLayoutEffect } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import {
  Zap,
  Bot,
  AlertTriangle,
  BrainCircuit,
  CheckCircle2,
  XCircle,
  ChevronDown,
  ChevronRight,
  Loader2,
  GitBranch,
  Settings,
  Terminal,
  BookOpen,
  Pencil,
  FileEdit,
  Search,
  Eye,
  FlaskConical,
  Globe,
  ArrowDown,
  ClipboardList,
  ScrollText,
  RotateCcw,
  Wrench,
} from 'lucide-react';
import CommandInput from './CommandInput';
import MessageSegments from './MessageSegments';
import MessageContent from './MessageContent';
import MessageBubble from './MessageBubble';
import ChatMessageContextMenu from './ChatMessageContextMenu';
import LiveLog from './LiveLog';
import Status from './Status';
import './Chat.css';

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string; // Chain-of-thought content from content_type: "reasoning"
  toolRefs?: Array<{ toolId: string; toolName: string; label: string; parallel?: boolean; toolIndex?: number }>;
}

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
  queryId?: number;
  toolIndex?: number;
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
}

interface ChatProps {
  messages: Message[];
  onSendMessage: (message: string) => void;
  onQueueMessage: (message: string) => void;
  queuedMessagesCount: number;
  queuedMessages?: string[];
  onQueueMessageRemove?: (index: number) => void;
  onQueueMessageEdit?: (index: number, newText: string) => void;
  onQueueReorder?: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages?: () => void;
  inputValue: string;
  onInputChange: (value: string) => void;
  isProcessing?: boolean;
  lastError?: string | null;
  toolExecutions?: ToolExecution[];
  queryProgress?: unknown;
  currentTodos?: Array<{
    id: string;
    content: string;
    status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
  }>;
  subagentActivities?: SubagentActivity[];
  onToolPillClick?: (toolId: string) => void;
  onStopProcessing?: () => void;
  // Worktree support
  chatId?: string;
  worktreePath?: string;
  workspaceRoot?: string;
  onWorktreeChange?: (worktreePath: string) => void;
  // Provider availability
  providerAvailable?: boolean;
  onRequestProviderSetup?: () => void;
  // Status bar
  stats?: Record<string, unknown>;
  isConnected?: boolean;
}

// ── Subagent Activity Feed ─────────────────────────────────────────

const MAX_ACTIVE_LINES = 50;
const MAX_COMPLETED_SUMMARIES = 3;

interface SubagentRun {
  toolCallId: string;
  persona: string;
  isParallel: boolean;
  isComplete: boolean;
  completionMessage: string;
  completionTimestamp: Date | null;
  activities: SubagentActivity[];
  spawnActivity: SubagentActivity | null;
  completeActivity: SubagentActivity | null;
  outputLines: Array<{ id: string; text: string; timestamp: Date; taskId?: string }>;
}

const groupSubagentRuns = (activities: SubagentActivity[]): SubagentRun[] => {
  const runMap = new Map<string, SubagentRun>();

  for (const activity of activities) {
    const key = activity.toolCallId || activity.id;
    let run = runMap.get(key);
    if (!run) {
      run = {
        toolCallId: activity.toolCallId,
        persona: activity.persona || 'subagent',
        isParallel: activity.isParallel || false,
        isComplete: false,
        completionMessage: '',
        completionTimestamp: null,
        activities: [],
        spawnActivity: null,
        completeActivity: null,
        outputLines: [],
      };
      runMap.set(key, run);
    }

    run.activities.push(activity);
    if (activity.persona && (!run.spawnActivity || activity.phase === 'spawn')) {
      run.persona = activity.persona;
    }
    if (activity.isParallel) {
      run.isParallel = true;
    }
    if (activity.phase === 'spawn') {
      run.spawnActivity = activity;
    }
    if (activity.phase === 'complete') {
      run.isComplete = true;
      run.completeActivity = activity;
      run.completionMessage = activity.message;
      run.completionTimestamp = activity.timestamp;
    }
    if (activity.phase === 'output' || activity.phase === 'step') {
      // Split multi-line batched messages into individual lines
      const lines = activity.message.split('\n').filter((l) => l.trim());
      for (const line of lines) {
        run.outputLines.push({
          id: `${activity.id}-${run.outputLines.length}`,
          text: line.trim(),
          timestamp: activity.timestamp,
          taskId: activity.taskId,
        });
      }
    }
  }

  return Array.from(runMap.values());
};

const PERSONA_COLORS: Record<string, string> = {
  coder: '#58a6ff',
  reviewer: '#d2a8ff',
  code_reviewer: '#d2a8ff',
  tester: '#7ee787',
  debugger: '#f0883e',
  refactor: '#79c0ff',
  researcher: '#ff7b72',
  general: '#8b949e',
};

const getPersonaColor = (persona?: string): string => {
  return PERSONA_COLORS[persona || ''] || '#8b949e';
};

const formatDuration = (start: Date, end?: Date): string => {
  const ms = (end || new Date()).getTime() - start.getTime();
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
};

// ── Active Subagent Card ───────────────────────────────────────────

interface ActiveSubagentCardProps {
  run: SubagentRun;
}

function ActiveSubagentCard({ run }: ActiveSubagentCardProps): JSX.Element {
  const [expanded, setExpanded] = useState(true);
  const color = getPersonaColor(run.persona);
  const startTime = run.spawnActivity?.timestamp || run.activities[0]?.timestamp;
  const hasOutput = run.outputLines.length > 0;

  return (
    <div
      className="subagent-feed-card subagent-feed-card--active"
      style={{ '--feed-persona-color': color } as CSSProperties}
    >
      <button
        className="subagent-feed-card-header"
        onClick={() => hasOutput && setExpanded((prev) => !prev)}
        type="button"
        aria-expanded={expanded}
      >
        <span className="subagent-feed-card-left">
          <span className="subagent-feed-status-dot subagent-feed-status-dot--active">
            <Loader2 size={8} />
          </span>
          <Bot size={13} className="subagent-feed-card-icon" />
          <span className="subagent-feed-persona">{run.persona}</span>
          {run.isParallel && <span className="subagent-feed-badge">parallel</span>}
        </span>
        <span className="subagent-feed-card-right">
          {run.outputLines.length > 0 && (
            <span className="subagent-feed-line-count">{run.outputLines.length} lines</span>
          )}
          {startTime && <span className="subagent-feed-duration">{formatDuration(startTime)}</span>}
          {hasOutput && (
            <span className="subagent-feed-toggle">
              {expanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
            </span>
          )}
        </span>
      </button>

      {expanded && hasOutput && <LiveLog lines={run.outputLines} maxLines={MAX_ACTIVE_LINES} />}
    </div>
  );
}

// ── Completed Subagent Card ────────────────────────────────────────

interface CompletedSubagentCardProps {
  run: SubagentRun;
}

function CompletedSubagentCard({ run }: CompletedSubagentCardProps): JSX.Element {
  const hasFailures =
    run.completionMessage?.toLowerCase().includes('fail') || run.completionMessage?.toLowerCase().includes('error');

  return (
    <div className="subagent-feed-card subagent-feed-card--completed">
      <span className="subagent-feed-status-dot subagent-feed-status-dot--completed">
        {hasFailures ? <XCircle size={9} /> : <CheckCircle2 size={9} />}
      </span>
      <Bot size={13} className="subagent-feed-card-icon" style={{ color: getPersonaColor(run.persona) }} />
      <span className="subagent-feed-persona">{run.persona}</span>
      {run.isParallel && <span className="subagent-feed-badge">parallel</span>}
      <span className="subagent-feed-sep">·</span>
      <span className={`subagent-feed-result ${hasFailures ? 'subagent-feed-result--fail' : ''}`}>
        {run.completionMessage || 'Completed'}
      </span>
      {run.spawnActivity?.timestamp && (
        <>
          <span className="subagent-feed-sep">·</span>
          <span className="subagent-feed-duration">
            {formatDuration(run.spawnActivity.timestamp, run.completionTimestamp || undefined)}
          </span>
        </>
      )}
    </div>
  );
}

// ── Subagent Activity Feed ─────────────────────────────────────────

interface SubagentActivityFeedProps {
  activities: SubagentActivity[];
}

function SubagentActivityFeed({ activities }: SubagentActivityFeedProps): JSX.Element | null {
  const [visible, setVisible] = useState(true);

  const runs = useMemo(() => groupSubagentRuns(activities), [activities]);

  // Separate active and completed runs. Only show the most recent completed runs.
  const activeRuns = useMemo(() => runs.filter((r) => !r.isComplete), [runs]);
  const completedRuns = useMemo(() => runs.filter((r) => r.isComplete).slice(-MAX_COMPLETED_SUMMARIES), [runs]);

  // Show feed only when there are any runs to display
  const hasContent = activeRuns.length > 0 || completedRuns.length > 0;
  if (!hasContent) return null;

  return (
    <div className={`subagent-feed ${visible ? '' : 'subagent-feed--collapsed'}`}>
      <button
        className="subagent-feed-toggle-bar"
        onClick={() => setVisible((prev) => !prev)}
        type="button"
        aria-expanded={visible}
      >
        <span className="subagent-feed-toggle-left">
          <Bot size={14} />
          <span className="subagent-feed-toggle-label">Subagent Activity</span>
          {activeRuns.length > 0 && (
            <span className="subagent-feed-active-badge">
              {activeRuns.length === 1 ? '1 active' : `${activeRuns.length} active`}
            </span>
          )}
        </span>
        <span className="subagent-feed-toggle-right">
          {visible ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </span>
      </button>

      {visible && (
        <div className="subagent-feed-body">
          {activeRuns.map((run) => (
            <ActiveSubagentCard key={run.toolCallId} run={run} />
          ))}
          {completedRuns.map((run) => (
            <CompletedSubagentCard key={run.toolCallId} run={run} />
          ))}
        </div>
      )}
    </div>
  );
}

// ── Tool Icon Helper (reused from MessageSegments) ────────────────

const getToolIcon = (toolName: string): ReactNode => {
  const iconMap: { [key: string]: ReactNode } = {
    shell_command: <Terminal size={12} />,
    read_file: <BookOpen size={12} />,
    write_file: <Pencil size={12} />,
    edit_file: <FileEdit size={12} />,
    search_files: <Search size={12} />,
    analyze_ui_screenshot: <Eye size={12} />,
    analyze_image_content: <FlaskConical size={12} />,
    web_search: <Globe size={12} />,
    fetch_url: <ArrowDown size={12} />,
    TodoWrite: <ClipboardList size={12} />,
    TodoRead: <ClipboardList size={12} />,
    view_history: <ScrollText size={12} />,
    rollback_changes: <RotateCcw size={12} />,
    mcp_tools: <Wrench size={12} />,
    run_subagent: <Bot size={12} />,
    run_parallel_subagents: <Bot size={12} />,
  };
  return iconMap[toolName] || <Wrench size={12} />;
};

const SHORT_TOOL_NAMES: { [key: string]: string } = {
  read_file: 'read',
  write_file: 'write',
  edit_file: 'edit',
  shell_command: 'shell',
  search_files: 'search',
  analyze_ui_screenshot: 'screenshot',
  analyze_image_content: 'image',
  web_search: 'web',
  fetch_url: 'fetch',
  TodoWrite: 'todo',
  TodoRead: 'todo',
  view_history: 'history',
  rollback_changes: 'rollback',
  mcp_tools: 'mcp',
  run_subagent: 'subagent',
  run_parallel_subagents: 'subagents',
};

const getShortToolName = (toolName: string): string => SHORT_TOOL_NAMES[toolName] ?? toolName;

// ── Tool Activity Feed ─────────────────────────────────────────────

interface ToolActivityFeedProps {
  toolExecutions: ToolExecution[];
}

function ToolActivityFeed({ toolExecutions }: ToolActivityFeedProps): JSX.Element | null {
  const [visible, setVisible] = useState(true);
  const now = useMemo(() => new Date(), []);

  // Separate active and completed tools
  const activeTools = useMemo(() => {
    return toolExecutions.filter((t) => t.status === 'started' || t.status === 'running');
  }, [toolExecutions]);

  // Show completed tools from the last 60 seconds (or most recent 5)
  const recentCompletedTools = useMemo(() => {
    const completed = toolExecutions.filter((t) => t.status === 'completed' || t.status === 'error');
    const recent = completed.filter((t) => {
      if (!t.endTime) return false;
      return now.getTime() - t.endTime.getTime() < 60000;
    });
    // Return most recent 5
    return recent.sort((a, b) => (b.endTime?.getTime() || 0) - (a.endTime?.getTime() || 0)).slice(0, 5);
  }, [toolExecutions, now]);

  // Show feed only when there are any tools to display
  const hasContent = activeTools.length > 0 || recentCompletedTools.length > 0;
  if (!hasContent) return null;

  return (
    <div className={`subagent-feed tool-feed ${visible ? '' : 'subagent-feed--collapsed'}`}>
      <button
        className="subagent-feed-toggle-bar"
        onClick={() => setVisible((prev) => !prev)}
        type="button"
        aria-expanded={visible}
      >
        <span className="subagent-feed-toggle-left">
          <Terminal size={14} />
          <span className="subagent-feed-toggle-label">Tool Activity</span>
          {activeTools.length > 0 && (
            <span className="subagent-feed-active-badge">
              {activeTools.length === 1 ? '1 active' : `${activeTools.length} active`}
            </span>
          )}
        </span>
        <span className="subagent-feed-toggle-right">
          {visible ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </span>
      </button>

      {visible && (
        <div className="subagent-feed-body">
          {activeTools.map((tool) => {
            const baseName = tool.tool.split('(')[0];
            const displayName = getShortToolName(baseName);
            const argsPreview = tool.arguments ? tool.arguments.slice(0, 80) + (tool.arguments.length > 80 ? '...' : '') : '';

            return (
              <div key={tool.id} className="subagent-feed-card subagent-feed-card--active">
                <span className="subagent-feed-status-dot subagent-feed-status-dot--active">
                  <Loader2 size={8} />
                </span>
                <span className="subagent-feed-card-icon">{getToolIcon(baseName)}</span>
                <span className="subagent-feed-persona">{displayName}</span>
                {argsPreview && <span className="subagent-feed-result">{argsPreview}</span>}
                {tool.startTime && <span className="subagent-feed-duration">{formatDuration(tool.startTime)}</span>}
              </div>
            );
          })}
          {recentCompletedTools.map((tool) => {
            const baseName = tool.tool.split('(')[0];
            const displayName = getShortToolName(baseName);
            const hasError = tool.status === 'error';
            const argsPreview = tool.arguments ? tool.arguments.slice(0, 60) + (tool.arguments.length > 60 ? '...' : '') : '';

            return (
              <div key={tool.id} className="subagent-feed-card subagent-feed-card--completed">
                <span className="subagent-feed-status-dot subagent-feed-status-dot--completed">
                  {hasError ? <XCircle size={9} /> : <CheckCircle2 size={9} />}
                </span>
                <span className="subagent-feed-card-icon" style={{ color: hasError ? '#f85149' : '#7ee787' }}>
                  {getToolIcon(baseName)}
                </span>
                <span className="subagent-feed-persona">{displayName}</span>
                <span className="subagent-feed-sep">·</span>
                <span className={`subagent-feed-result ${hasError ? 'subagent-feed-result--fail' : ''}`}>
                  {hasError ? (tool.message || 'Error') : 'Completed'}
                </span>
                {argsPreview && (
                  <>
                    <span className="subagent-feed-sep">·</span>
                    <span className="subagent-feed-result" style={{ color: '#8b949e' }}>{argsPreview}</span>
                  </>
                )}
                {tool.startTime && tool.endTime && (
                  <>
                    <span className="subagent-feed-sep">·</span>
                    <span className="subagent-feed-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
                  </>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ── Main Chat Component ───────────────────────────────────────────

function Chat({
  messages,
  onSendMessage,
  onQueueMessage,
  queuedMessagesCount,
  queuedMessages = [],
  onQueueMessageRemove,
  onQueueMessageEdit,
  onQueueReorder,
  onClearQueuedMessages,
  inputValue,
  onInputChange,
  isProcessing = false,
  lastError = null,
  toolExecutions = [],
  queryProgress = null,
  currentTodos: _currentTodos = [],
  subagentActivities = [],
  onToolPillClick,
  onStopProcessing,
  // Worktree support — chatId, workspaceRoot, onWorktreeChange available for future worktree switching
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  chatId: _chatId,
  worktreePath,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  workspaceRoot: _workspaceRoot,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  onWorktreeChange: _onWorktreeChange,
  // Provider availability
  providerAvailable,
  onRequestProviderSetup,
  // Status bar
  stats,
  isConnected,
}: ChatProps): JSX.Element {
  const AUTO_SCROLL_THRESHOLD_PX = 96;
  const chatShellRef = useRef<HTMLDivElement>(null);
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const inputContainerRef = useRef<HTMLDivElement>(null);
  const shouldAutoScrollRef = useRef(true);
  const [inputContainerHeight, setInputContainerHeight] = useState(0);

  const inputValueRef = useRef(inputValue);
  inputValueRef.current = inputValue;

  const hasSubagentActivity = subagentActivities.length > 0;

  // Filter tool executions to show only those for the current chat session
  // Tools with queryId matching the current stats.queryCount are from the current query
  // Tools without queryId are legacy data (show them for backward compatibility)
  const currentQueryCount = stats?.queryCount as number | undefined;
  const filteredToolExecutions = useMemo(() => {
    if (!currentQueryCount) {
      // If no queryCount is available, return all toolExecutions (backward compatibility)
      return toolExecutions;
    }
    // Filter to show only tools from the current query (matching queryId)
    // or tools without queryId (legacy)
    return toolExecutions.filter(
      (tool: ToolExecution) => tool.queryId === undefined || tool.queryId === currentQueryCount,
    );
  }, [toolExecutions, currentQueryCount]);

  const hasToolActivity = filteredToolExecutions.length > 0;

  const isNearBottom = useCallback(
    (node: HTMLDivElement) => {
      const distanceFromBottom = node.scrollHeight - node.scrollTop - node.clientHeight;
      return distanceFromBottom <= AUTO_SCROLL_THRESHOLD_PX;
    },
    [AUTO_SCROLL_THRESHOLD_PX],
  );

  useLayoutEffect(() => {
    const node = inputContainerRef.current;
    if (!node) {
      return;
    }

    const updateHeight = () => {
      setInputContainerHeight(node.getBoundingClientRect().height);
    };

    updateHeight();

    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateHeight);
      return () => window.removeEventListener('resize', updateHeight);
    }

    const observer = new ResizeObserver(updateHeight);
    observer.observe(node);
    window.addEventListener('resize', updateHeight);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', updateHeight);
    };
  }, []);

  useEffect(() => {
    const node = chatContainerRef.current;
    if (!node || !shouldAutoScrollRef.current) {
      return;
    }

    node.scrollTop = node.scrollHeight;
  }, [messages, filteredToolExecutions, queryProgress, isProcessing, subagentActivities.length]);

  const handleChatScroll = useCallback(() => {
    const node = chatContainerRef.current;
    if (!node) {
      return;
    }
    shouldAutoScrollRef.current = isNearBottom(node);
  }, [isNearBottom]);

  const findMatchingToolExecution = useCallback(
    (toolName: string) => {
      const normalized = toolName.split('(')[0];
      for (let i = filteredToolExecutions.length - 1; i >= 0; i -= 1) {
        if (filteredToolExecutions[i].tool === normalized) {
          return filteredToolExecutions[i];
        }
      }
      return undefined;
    },
    [filteredToolExecutions],
  );

  const formatTime = (date: Date) => {
    return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const handleReloadWithoutSSHPath = useCallback(() => {
    const { origin, pathname } = window.location;
    if (pathname.startsWith('/ssh/')) {
      window.location.assign(`${origin}/`);
      return;
    }
    window.location.reload();
  }, []);

  const showExpiredSessionRecovery =
    !!lastError && lastError.toLowerCase().includes('ssh session not found or expired');

  const toolStatusMap = useMemo(() => {
  const m = new Map<string, ToolExecution['status']>();
  for (const t of filteredToolExecutions) m.set(t.id, t.status);
  return m;
}, [filteredToolExecutions]);const handleInsertAtCursor = useCallback(
    (text: string) => {
      const separator = inputValueRef.current ? '\n' : '';
      onInputChange(inputValueRef.current + separator + text);
    },
    [onInputChange],
  );

  return (
    <div
      className="chat-shell"
      ref={chatShellRef}
      style={{ '--chat-input-height': `${inputContainerHeight}px` } as CSSProperties}
    >
      <div className="chat-main">
        <div className="chat-container" ref={chatContainerRef} onScroll={handleChatScroll}>
          <>
            {worktreePath && (
              <div className="worktree-indicator">
                <div className="worktree-indicator-content">
                  <div className="worktree-indicator-icon">
                    <GitBranch size={14} />
                  </div>
                  <span className="worktree-indicator-text" title={worktreePath}>
                    Worktree: {worktreePath.split('/').pop()}
                  </span>
                </div>
              </div>
            )}
            {messages.length === 0 ? (
              providerAvailable === false ? (
                <div className="welcome-message no-provider-state">
                  <div className="welcome-icon">
                    <Bot size={32} />
                  </div>
                  <div className="welcome-text">
                    No AI provider configured
                  </div>
                  <div className="welcome-hint">
                    AI features require a provider to be set up. The editor, terminal, file tree, and git panels are fully functional without one.
                  </div>
                  {onRequestProviderSetup && (
                    <button type="button" className="provider-setup-btn" onClick={onRequestProviderSetup} aria-label="Open provider setup">
                      <Settings size={14} />
                      Configure Provider
                    </button>
                  )}
                </div>
              ) : (
                <div className="welcome-message">
                  <div className="welcome-icon">
                    <Bot size={32} />
                  </div>
                  <div className="welcome-text">
                    Welcome to ledit! I&apos;m ready to help you with code analysis, editing, and more.
                  </div>
                  <div className="welcome-hint">
                    Try asking: &quot;Show me the project structure&quot; or &quot;Find the main function&quot;
                  </div>
                </div>
              )
            ) : (
              messages.map((message) => (
                <MessageBubble
                  key={message.id}
                  type={message.type}
                  ariaLabel={`${message.type} message`}
                  copyText={message.content}
                  timestamp={formatTime(message.timestamp)}
                >
                  {message.type === 'assistant' ? (
                    <>
                      {message.reasoning && message.reasoning.trim() && (
                        <details className="reasoning-block" open={false}>
                          <summary className="reasoning-summary">
                            <BrainCircuit size={13} className="reasoning-icon" />
                            <span>Reasoning</span>
                            <span className="reasoning-toggle">▶</span>
                          </summary>
                          <div className="reasoning-content">
                            <MessageContent content={message.reasoning} />
                          </div>
                        </details>
                      )}
                      <MessageSegments
                        content={message.content}
                        toolRefs={message.toolRefs}
                        onToolRefClick={(toolId) => onToolPillClick?.(toolId)}
                        onToolClick={(toolName) => {
                          const matchingTool = findMatchingToolExecution(toolName);
                          if (matchingTool) {
                            onToolPillClick?.(matchingTool.id);
                          }
                        }}
                      />
                    </>
                  ) : (
                    <MessageContent content={message.content} />
                  )}
                </MessageBubble>
              ))
            )}

            {/* Inline subagent activity feed – shows between messages and processing indicators */}
            {hasSubagentActivity && <SubagentActivityFeed activities={subagentActivities} />}

            {/* Inline tool activity feed – shows active/recent tool executions */}
            {hasToolActivity && <ToolActivityFeed toolExecutions={filteredToolExecutions} />}

            {queryProgress && (
              <div className="query-progress">
                <>
                  <div className="progress-header">
                    <span className="progress-icon">
                      <Zap size={14} />
                    </span>
                    <span className="progress-text">
                      {((queryProgress as Record<string, unknown>).message as string) || 'Processing...'}
                    </span>
                  </div>
                  {(queryProgress as Record<string, unknown>).details && (
                    <div className="progress-details">
                      {(queryProgress as Record<string, unknown>).details as ReactNode}
                    </div>
                  )}
                </>
              </div>
            )}

            {isProcessing && filteredToolExecutions.length === 0 && !queryProgress && !hasSubagentActivity && (
              <div className="processing-indicator">
                <div className="processing-content">
                  <div className="processing-spinner">
                    <Zap size={14} />
                  </div>
                  <div className="processing-text">Processing your request...</div>
                </div>
              </div>
            )}

            {lastError && (
              <div className="error-indicator">
                <div className="error-content">
                  <div className="error-icon">
                    <AlertTriangle size={14} />
                  </div>
                  <div className="error-text">{lastError}</div>
                  {showExpiredSessionRecovery ? (
                    <div className="error-actions">
                      <button type="button" className="error-recovery-btn" onClick={handleReloadWithoutSSHPath}>
                        Reload Without SSH Path
                      </button>
                    </div>
                  ) : null}
                </div>
              </div>
            )}
          </>
        </div>
      </div>

      <div className="input-container" ref={inputContainerRef}>
        <CommandInput
          value={inputValue}
          onChange={onInputChange}
          onSend={onSendMessage}
          onQueue={onQueueMessage}
          onStop={onStopProcessing}
          placeholder={providerAvailable === false ? 'Configure a provider to start chatting...' : 'Ask me anything about your code...'}
          multiline={true}
          autoFocus={providerAvailable !== false}
          isProcessing={isProcessing}
          isConnected={isConnected}
          disabled={providerAvailable === false}
          queuedCount={queuedMessagesCount}
          queuedMessages={queuedMessages}
          onQueueMessageRemove={onQueueMessageRemove}
          onQueueMessageEdit={onQueueMessageEdit}
          onQueueReorder={onQueueReorder}
          onClearQueuedMessages={onClearQueuedMessages}
        />
      </div>

      <Status isConnected={!!isConnected} stats={stats} />
      <ChatMessageContextMenu containerRef={chatContainerRef} onInsertAtCursor={handleInsertAtCursor} />
    </div>
  );
}

export default Chat;
