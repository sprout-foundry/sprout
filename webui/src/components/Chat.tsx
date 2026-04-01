import React, { useRef, useEffect, useCallback, useState, useLayoutEffect, useMemo } from 'react';
import { Zap, Bot, AlertTriangle, BrainCircuit, Wrench, Cpu } from 'lucide-react';
import CommandInput from './CommandInput';
import MessageSegments from './MessageSegments';
import MessageContent from './MessageContent';
import MessageBubble from './MessageBubble';
import { stripAnsiCodes } from '../utils/ansi';
import './Chat.css';

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string;  // Chain-of-thought content from content_type: "reasoning"
  toolRefs?: Array<{ toolId: string; toolName: string; label: string; parallel?: boolean }>;
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

interface ChatProps {
  messages: Message[];
  onSendMessage: (message: string) => void;
  onQueueMessage: (message: string) => void;
  queuedMessages: string[];
  onRemoveQueuedMessage?: (index: number) => void;
  onEditQueuedMessage?: (index: number, newText: string) => void;
  onReorderQueuedMessage?: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages?: () => void;
  inputValue: string;
  onInputChange: (value: string) => void;
  isProcessing?: boolean;
  lastError?: string | null;
  toolExecutions?: ToolExecution[];
  queryProgress?: any;
  currentTodos?: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
  onToolPillClick?: (toolId: string) => void;
  onStopProcessing?: () => void;
  subagentActivities?: Array<{
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
  }>;
}

const Chat: React.FC<ChatProps> = ({
  messages,
  onSendMessage,
  onQueueMessage,
  queuedMessages,
  onRemoveQueuedMessage,
  onEditQueuedMessage,
  onReorderQueuedMessage,
  onClearQueuedMessages,
  inputValue,
  onInputChange,
  isProcessing = false,
  lastError = null,
  toolExecutions = [],
  queryProgress = null,
  currentTodos = [],
  onToolPillClick,
  onStopProcessing,
  subagentActivities = [],
}) => {
  const AUTO_SCROLL_THRESHOLD_PX = 96;
  const chatShellRef = useRef<HTMLDivElement>(null);
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const inputContainerRef = useRef<HTMLDivElement>(null);
  const shouldAutoScrollRef = useRef(true);
  const outputLogRef = useRef<HTMLDivElement>(null);
  const lastCompletedIdRef = useRef<string>('');
  const [inputContainerHeight, setInputContainerHeight] = useState(0);
  const [showCompletionBadge, setShowCompletionBadge] = useState(false);

  const isNearBottom = useCallback((node: HTMLDivElement) => {
    const distanceFromBottom = node.scrollHeight - node.scrollTop - node.clientHeight;
    return distanceFromBottom <= AUTO_SCROLL_THRESHOLD_PX;
  }, [AUTO_SCROLL_THRESHOLD_PX]);

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
  }, [messages, toolExecutions, queryProgress, isProcessing, subagentActivities]);

  // Auto-scroll the output log when new lines arrive
  useEffect(() => {
    if (outputLogRef.current && shouldAutoScrollRef.current) {
      outputLogRef.current.scrollTop = outputLogRef.current.scrollHeight;
    }
  }, [subagentActivities]);

  // Check for newly completed subagents to show a brief completion badge
  useEffect(() => {
    const completedActivities = subagentActivities.filter(a => a.phase === 'complete');
    if (completedActivities.length > 0) {
      const latestComplete = completedActivities[completedActivities.length - 1];
      if (latestComplete.id !== lastCompletedIdRef.current) {
        lastCompletedIdRef.current = latestComplete.id;
        setShowCompletionBadge(true);
        const timer = setTimeout(() => setShowCompletionBadge(false), 3000);
        return () => clearTimeout(timer);
      }
    }
  }, [subagentActivities]);

  const handleChatScroll = useCallback(() => {
    const node = chatContainerRef.current;
    if (!node) {
      return;
    }
    shouldAutoScrollRef.current = isNearBottom(node);
  }, [isNearBottom]);

  const findMatchingToolExecution = useCallback((toolName: string) => {
    const normalized = toolName.split('(')[0];
    for (let i = toolExecutions.length - 1; i >= 0; i -= 1) {
      if (toolExecutions[i].tool === normalized) {
        return toolExecutions[i];
      }
    }
    return undefined;
  }, [toolExecutions]);

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

  // Live activity feed data – memoized to avoid unnecessary re-renders
  const latestActiveTool = useMemo(() => {
    const active = toolExecutions.filter(t => t.status === 'started' || t.status === 'running');
    return active.length > 0 ? active[active.length - 1] : null;
  }, [toolExecutions]);

  const latestSubagentActivity = useMemo(() => {
    // Only show spawn/output/step phases (not 'complete' — those are terminal)
    const liveActivities = subagentActivities.filter(a => a.phase !== 'complete');
    return liveActivities.length > 0 ? liveActivities[liveActivities.length - 1] : null;
  }, [subagentActivities]);

  // The overall most recent activity (regardless of phase) — needed for
  // completion badge detection.
  const latestActivity = useMemo(() => {
    return subagentActivities.length > 0
      ? subagentActivities[subagentActivities.length - 1]
      : null;
  }, [subagentActivities]);

  // Group recent output lines per tool call for the live feed
  const activeSubagentOutputGroups = useMemo(() => {
    // Include 'output' and 'step' phases — step events are meaningful milestones
    const outputActivities = subagentActivities.filter(a => (a.phase === 'output' || a.phase === 'step') && a.message);
    if (outputActivities.length === 0) return [];

    // Group by toolCallId, keep the most recent group(s)
    const groups: Record<string, { toolCallId: string; persona?: string; isParallel?: boolean; lines: Array<{ text: string; isStep: boolean }> }> = {};
    for (const activity of outputActivities) {
      const key = activity.toolCallId || '_default';
      const isStep = activity.phase === 'step';
      if (!groups[key]) {
        groups[key] = {
          toolCallId: key,
          persona: activity.persona,
          isParallel: activity.isParallel,
          lines: [],
        };
      }
      groups[key].lines.push({ text: activity.message, isStep });
    }

    // Return groups sorted by most recent activity, with at most 20 lines each
    return Object.values(groups)
      .sort((a, b) => b.lines.length - a.lines.length)
      .map(g => ({ ...g, lines: g.lines.slice(-20) }));
  }, [subagentActivities]);

  const getToolLabel = useCallback((tool: ToolExecution) => {
    if (tool.tool === 'run_subagent') {
      try {
        const args = tool.arguments ? JSON.parse(tool.arguments) : {};
        return args.persona ? `Running ${args.persona} subagent…` : 'Running subagent…';
      } catch { return 'Running subagent…'; }
    }
    if (tool.tool === 'run_parallel_subagents') return 'Running parallel subagents…';
    return `${tool.tool.replace(/_/g, ' ')}…`;
  }, []);

  const hasOutputYet = activeSubagentOutputGroups.some(g => g.lines.length > 0);

  const activeOutputLineCount = useMemo(() => {
    return activeSubagentOutputGroups.reduce((sum, g) => sum + g.lines.length, 0);
  }, [activeSubagentOutputGroups]);

  return (
    <div
      className="chat-shell"
      ref={chatShellRef}
      style={{ '--chat-input-height': `${inputContainerHeight}px` } as React.CSSProperties}
    >
      <div className="chat-main">
        <div className="chat-container" ref={chatContainerRef} onScroll={handleChatScroll}>
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
              <MessageBubble
                key={message.id}
                type={message.type}
                ariaLabel={`${message.type} message`}
                copyText={message.content}
                timestamp={formatTime(message.timestamp)}
              >
                  {message.type === 'assistant'
                  ? (
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
                  )
                  : <MessageContent content={message.content} />
                }
              </MessageBubble>
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

          {isProcessing && !queryProgress && (
            (latestActiveTool || latestSubagentActivity) ? (
              <div className="live-activity-feed">
                {latestActiveTool && (
                  <div className="live-activity-row">
                    <Wrench size={13} className="live-activity-icon" />
                    <span className="live-activity-label">Tool</span>
                    <span className="live-activity-text">{getToolLabel(latestActiveTool)}</span>
                  </div>
                )}
                {latestSubagentActivity && latestSubagentActivity.phase !== 'complete' && (
                  <>
                    <div className="live-activity-subagent-header">
                      <Cpu size={13} className="live-activity-subagent-icon" />
                      <span className="live-activity-subagent-badge">
                        {latestSubagentActivity.isParallel ? 'Parallel ' : ''}{latestSubagentActivity.persona || 'subagent'}
                      </span>
                      {latestSubagentActivity.provider && (
                        <span className="live-activity-subagent-model">
                          {latestSubagentActivity.provider}/{latestSubagentActivity.model}
                        </span>
                      )}
                      <span className="live-activity-subagent-status">running</span>
                      <span className="live-activity-subagent-spinner" />
                    </div>
                    {activeSubagentOutputGroups.length > 0 && (
                      <>
                        {activeSubagentOutputGroups.slice(0, 1).map((group) => (
                          <div key={group.toolCallId} className="live-activity-output-log" ref={outputLogRef}>
                            {group.lines.map((item, i) => (
                              <div
                                key={`${group.toolCallId}-${i}`}
                                className={`live-activity-output-line ${item.isStep ? 'live-activity-line-step' : ''}`}
                                title={stripAnsiCodes(item.text)}
                              >
                                <span className={`live-activity-line-chevron ${item.isStep ? 'live-activity-chevron-step' : ''}`}>
                                  {item.isStep ? '◆' : '›'}
                                </span>
                                <span className="live-activity-line-text">{stripAnsiCodes(item.text)}</span>
                              </div>
                            ))}
                          </div>
                        ))}
                        {activeOutputLineCount > 0 && (
                          <div className="live-activity-line-count">
                            {activeOutputLineCount} lines output
                          </div>
                        )}
                      </>
                    )}
                    {!hasOutputYet && (
                      <div className="live-activity-waiting">
                        <span className="live-activity-waiting-dots" />
                        <span>Waiting for output...</span>
                      </div>
                    )}
                  </>
                )}
              </div>
            ) : (
              <div className="processing-indicator">
                <div className="processing-content">
                  <div className="processing-spinner"><Zap size={14} /></div>
                  <div className="processing-text">Processing your request...</div>
                </div>
              </div>
            )
          )}

          {showCompletionBadge && latestActivity?.phase === 'complete' && (
            <div className="live-activity-completion-badge">
              <span className="completion-checkmark">✓</span>
              <span className="completion-text">Subagent completed</span>
            </div>
          )}

          {lastError && (
            <div className="error-indicator">
              <div className="error-content">
                <div className="error-icon"><AlertTriangle size={14} /></div>
                <div className="error-text">{lastError}</div>
                {showExpiredSessionRecovery ? (
                  <div className="error-actions">
                    <button
                      type="button"
                      className="error-recovery-btn"
                      onClick={handleReloadWithoutSSHPath}
                    >
                      Reload Without SSH Path
                    </button>
                  </div>
                ) : null}
              </div>
            </div>
          )}
        </div>
      </div>

      <div className="input-container" ref={inputContainerRef}>
        <CommandInput
          value={inputValue}
          onChange={onInputChange}
          onSend={onSendMessage}
          onQueue={onQueueMessage}
          onStop={onStopProcessing}
          placeholder="Ask me anything about your code..."
          multiline={true}
          autoFocus={true}
          isProcessing={isProcessing}
          queuedMessages={queuedMessages}
          onRemoveQueuedMessage={onRemoveQueuedMessage}
          onEditQueuedMessage={onEditQueuedMessage}
          onReorderQueuedMessage={onReorderQueuedMessage}
          onClearQueuedMessages={onClearQueuedMessages}
        />
      </div>
    </div>
  );
};

export default Chat;
