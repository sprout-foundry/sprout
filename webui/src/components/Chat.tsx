import React, { useRef, useEffect, useCallback, type ReactNode } from 'react';
import {
  Terminal, BookOpen, FileEdit, Pencil, Search, Eye, FlaskConical,
  Globe, ArrowDown, ClipboardList, ScrollText, RotateCcw,
  Wrench, Zap, Bot, AlertTriangle,
  ExternalLink, CheckCircle, Circle, Loader2, Minus
} from 'lucide-react';
import CommandInput from './CommandInput';
import { stripAnsiCodes } from '../utils/ansi';
import { parseMessageSegments } from '../utils/messageSegments';
import MessageContent from './MessageContent';
import MessageBubble from './MessageBubble';
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
  onToolPillClick?: (toolId: string) => void;
}

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
  currentTodos = [],
  onToolPillClick
}) => {
  const chatContainerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (chatContainerRef.current) {
      chatContainerRef.current.scrollTop = chatContainerRef.current.scrollHeight;
    }
  }, [messages, toolExecutions, queryProgress, isProcessing]);

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

  const renderMessageSegments = (content: string): ReactNode => {
    try {
      const cleaned = stripAnsiCodes(content);
      const segments = parseMessageSegments(cleaned);

      return (
        <div className="message-segments">
          {segments.map((segment, idx) => {
            switch (segment.type) {
              case 'text':
                return (
                  <div key={`seg-${idx}`} className="segment-text">
                    <MessageContent content={segment.content} />
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
                      const matchingTool = findMatchingToolExecution(segment.toolName);
                      if (matchingTool) {
                        onToolPillClick?.(matchingTool.id);
                      }
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        const matchingTool = findMatchingToolExecution(segment.toolName);
                        if (matchingTool) {
                          onToolPillClick?.(matchingTool.id);
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
                return null;

              case 'result':
                return null;

              default:
                return null;
            }
          })}
        </div>
      );
    } catch {
      return <MessageContent content={content} />;
    }
  };

  return (
    <div className="chat-shell">
      <div className="chat-main">
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
                            <span className="reasoning-icon">💭</span>
                            <span>Reasoning</span>
                            <span className="reasoning-toggle">▶</span>
                          </summary>
                          <div className="reasoning-content">
                            <MessageContent content={message.reasoning} />
                          </div>
                        </details>
                      )}
                      {renderMessageSegments(message.content)}
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
