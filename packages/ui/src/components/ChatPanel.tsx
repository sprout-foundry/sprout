import { useRef, useEffect, useCallback, useState, useMemo, useLayoutEffect, memo } from 'react';
import type { Message, ToolExecution, SubagentActivity, SubagentRun, TodoStatus, TodoItem, FileEdit, LogEntry, ChatProps } from '../types/chat';
import { MAX_ACTIVE_LINES, MAX_COMPLETED_SUMMARIES } from '../types/chat';
import { groupSubagentRuns } from '../utils/subagentGrouping';
import type { CSSProperties, ReactNode } from 'react';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
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
  CloudOff,
} from 'lucide-react';
import CommandInput from './CommandInput';
import MessageSegments from './MessageSegments';
import MessageContent from './MessageContent';
import MessageBubble from './MessageBubble';
import ChatMessageContextMenu from './ChatMessageContextMenu';
import LiveLog from './LiveLog';
import { getPersonaColor } from '../utils/personaColors';
import './ChatPanel.css';

// ── Subagent Activity Feed ─────────────────────────────────────────

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

// ── Memoized Message Item ─────────────────────────────────────────
// Extracted from the Virtuoso itemContent callback so that unchanged
// messages keep their DOM nodes across renders, preserving text selection.

interface MessageItemProps {
  message: Message;
  onToolPillClick?: (toolId: string) => void;
  findMatchingToolExecution: (toolName: string) => ToolExecution | undefined;
  filteredToolExecutions: ToolExecution[];
  formatTime: (date: Date) => string;
}

const MessageItem = memo(function MessageItem({
  message,
  onToolPillClick,
  findMatchingToolExecution,
  filteredToolExecutions,
  formatTime,
}: MessageItemProps) {
  return (
    <MessageBubble
      type={message.type}
      ariaLabel={`${message.type} message`}
      copyText={message.content}
      timestamp={formatTime(message.timestamp)}
      persona={message.persona}
      depth={message.subagentDepth}
      tokensUsed={message.tokensUsed}
      cost={message.cost}
      model={message.model}
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
            onToolRefClick={onToolPillClick}
            onToolClick={(toolName) => {
              const matchingTool = findMatchingToolExecution(toolName);
              if (matchingTool) {
                onToolPillClick?.(matchingTool.id);
              }
            }}
            getToolStatus={(toolId) => {
              const te = filteredToolExecutions.find((t) => t.id === toolId);
              return te?.status;
            }}
          />
        </>
      ) : (
        <MessageContent content={message.content} />
      )}
    </MessageBubble>
  );
});

// ── Helper function to detect compaction summary messages ──────────

const isCompactionSummary = (message: Message): boolean => {
  return message.type === 'assistant' && message.content.startsWith('[Context compaction — layered summary]');
};

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
  // Backend reachability (cloud mode)
  backendReachable,
  onRetryConnection,
}: ChatProps): JSX.Element {
  const chatShellRef = useRef<HTMLDivElement>(null);
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const inputContainerRef = useRef<HTMLDivElement>(null);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [inputContainerHeight, setInputContainerHeight] = useState(0);

  const inputValueRef = useRef(inputValue);
  inputValueRef.current = inputValue;

  const hasSubagentActivity = subagentActivities.length > 0;
  const needsHealthCheck = false;

  // Filter out compaction summary messages from the chat view
  const visibleMessages = useMemo(() => {
    return messages.filter(m => !isCompactionSummary(m));
  }, [messages]);

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
    return () => {
      observer.disconnect();
    };
  }, []);

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
    false && !!lastError && (lastError as string).toLowerCase().includes('ssh session not found or expired');

  const handleInsertAtCursor = useCallback(
    (text: string) => {
      const separator = inputValueRef.current ? '\n' : '';
      onInputChange(inputValueRef.current + separator + text);
    },
    [onInputChange],
  );

  // Footer component for Virtuoso - renders trailing content
  const ChatFooter = useCallback((): JSX.Element => {
    const elements: JSX.Element[] = [];

    if (hasSubagentActivity) {
      elements.push(<SubagentActivityFeed key="subagent" activities={subagentActivities} />);
    }

    if (queryProgress) {
      elements.push(
        <div key="progress" className="query-progress">
          <div className="progress-header">
            <span className="progress-icon">
              <Zap size={14} />
            </span>
            <span className="progress-text">
              {((queryProgress as Record<string, unknown>).message as string) || 'Processing...'}
            </span>
          </div>
          {(queryProgress as Record<string, unknown>).details != null && (
            <div className="progress-details">
              {(queryProgress as Record<string, unknown>).details as ReactNode}
            </div>
          )}
        </div>,
      );
    }

    if (isProcessing && filteredToolExecutions.length === 0 && !queryProgress && !hasSubagentActivity) {
      elements.push(
        <div key="processing" className="processing-indicator">
          <div className="processing-content">
            <div className="processing-spinner">
              <Zap size={14} />
            </div>
            <div className="processing-text">Processing your request...</div>
          </div>
        </div>,
      );
    }

    if (lastError) {
      elements.push(
        <div key="error" className="error-indicator">
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
        </div>,
      );
    }

    return elements.length === 1 ? elements[0] : <>{elements}</>;
  }, [
    hasSubagentActivity,
    subagentActivities,
    queryProgress,
    isProcessing,
    filteredToolExecutions,
    lastError,
    showExpiredSessionRecovery,
    handleReloadWithoutSSHPath,
  ]);

  // Header component for Virtuoso - renders worktree indicator
  const ChatHeader = useCallback((): JSX.Element | null => {
    if (!worktreePath) return null;
    return (
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
    );
  }, [worktreePath]);

  return (
    <div
      className="chat-shell"
      ref={chatShellRef}
      style={{ '--chat-input-height': `${inputContainerHeight}px` } as CSSProperties}
    >
      <div className="chat-main">
        {/* When backend requires health checks and is unreachable, show offline panel */}
        {needsHealthCheck && backendReachable === false && !isProcessing && messages.length === 0 ? (
          <div className="chat-container chat-container--empty" ref={chatContainerRef}>
            <div className="chat-offline-panel" role="status">
              <CloudOff size={48} className="chat-offline-icon" aria-hidden="true" />
              <h3 className="chat-offline-title">No Server Connection</h3>
              <p className="chat-offline-description">
                Chat requires a connection to your Sprout server.
                Your editor and terminal remain available while offline.
              </p>
              <button
                className="chat-offline-retry-btn"
                onClick={onRetryConnection}
                type="button"
                aria-label="Retry connection"
              >
                Retry Connection
              </button>
            </div>
          </div>
        ) : messages.length === 0 ? (
          <div className="chat-container chat-container--empty" ref={chatContainerRef}>
            <>
              {providerAvailable === false ? (
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
                    Welcome to sprout! I&apos;m ready to help you with code analysis, editing, and more.
                  </div>
                  <div className="welcome-hint">
                    Try asking: &quot;Show me the project structure&quot; or &quot;Find the main function&quot;
                  </div>
                </div>
              )}
            </>
          </div>
        ) : (
          <div role="log" aria-live="polite" aria-label="Chat messages" ref={chatContainerRef} style={{ flex: 1, minHeight: 0, position: 'relative' }}>
            <Virtuoso
              ref={virtuosoRef}
              data={visibleMessages}
              followOutput={(isAtBottom) => (isAtBottom ? 'smooth' : false)}
              initialTopMostItemIndex={visibleMessages.length - 1}
              increaseViewportBy={{ top: 400, bottom: 400 }}
              atBottomStateChange={(atBottom) => setIsAtBottom(atBottom)}
              itemContent={(index, message) => (
                <MessageItem
                  message={message}
                  onToolPillClick={onToolPillClick}
                  findMatchingToolExecution={findMatchingToolExecution}
                  filteredToolExecutions={filteredToolExecutions}
                  formatTime={formatTime}
                />
              )}
              components={{
                Header: ChatHeader,
                Footer: ChatFooter,
              }}
              className="chat-virtuoso"
              style={{ height: '100%' }}
            />
            {!isAtBottom && (
              <button
                className="scroll-to-bottom-btn"
                onClick={() => virtuosoRef.current?.scrollToIndex({ index: 'LAST', behavior: 'smooth', align: 'end' })}
                type="button"
                aria-label="Scroll to bottom"
              >
                <ChevronDown size={18} />
              </button>
            )}
          </div>
        )}
      </div>

      <div className="input-container" ref={inputContainerRef}>
        <CommandInput
          value={inputValue}
          onChange={onInputChange}
          onSend={onSendMessage}
          onQueue={onQueueMessage}
          onStop={onStopProcessing}
          placeholder={
            providerAvailable === false
              ? 'Configure a provider to start chatting...'
              : needsHealthCheck && backendReachable === false
                ? 'Waiting for server connection...'
                : 'Ask me anything about your code...'
          }
          multiline={true}
          autoFocus={providerAvailable !== false && !(needsHealthCheck && backendReachable === false)}
          isProcessing={isProcessing}
          isConnected={isConnected}
          disabled={providerAvailable === false || (needsHealthCheck && backendReachable === false)}
          queuedCount={queuedMessagesCount}
          queuedMessages={queuedMessages}
          onQueueMessageRemove={onQueueMessageRemove}
          onQueueMessageEdit={onQueueMessageEdit}
          onQueueReorder={onQueueReorder}
          onClearQueuedMessages={onClearQueuedMessages}
        />
      </div>

      <ChatMessageContextMenu containerRef={chatContainerRef} onInsertAtCursor={handleInsertAtCursor} />
    </div>
  );
}

export default Chat;
