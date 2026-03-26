import React, { useState, useRef, useEffect, useCallback, type ReactNode } from 'react';
import {
  Terminal, BookOpen, FileEdit, Pencil, Search, Eye, FlaskConical,
  Globe, ArrowDown, ClipboardList, ScrollText, RotateCcw,
  Wrench, Zap, Bot, Copy, AlertTriangle,
  ExternalLink, CheckCircle, Circle, Loader2, Minus
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import CommandInput from './CommandInput';
import { stripAnsiCodes } from '../utils/ansi';
import { parseMessageSegments } from '../utils/messageSegments';
import ContextPanel, { type ContextPanelHandle } from './ContextPanel';
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
}

const MOBILE_LAYOUT_MAX_WIDTH = 900;

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
  const [isMobileLayout, setIsMobileLayout] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false;
    return window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH;
  });
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const contextPanelRef = useRef<ContextPanelHandle>(null);

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
    };
    return iconMap[toolName] || <Wrench size={14} />;
  };

  const copyToClipboard = useCallback((text: string) => {
    navigator.clipboard.writeText(text);
  }, []);

  const formatTime = (date: Date) => {
    return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

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
                        contextPanelRef.current?.highlightTool(matchingTool.id);
                      }
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        const matchingTool = toolExecutions.find(t =>
                          t.tool === segment.toolName.split('(')[0]
                        );
                        if (matchingTool) {
                          contextPanelRef.current?.highlightTool(matchingTool.id);
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
      return renderContent(content);
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

        <ContextPanel
          ref={contextPanelRef}
          context="chat"
          toolExecutions={toolExecutions}
          currentTodos={currentTodos || []}
          messages={messages}
          isProcessing={isProcessing}
          lastError={lastError}
          queryProgress={queryProgress}
          isMobileLayout={isMobileLayout}
        />
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
