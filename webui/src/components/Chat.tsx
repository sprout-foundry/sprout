import React, { useRef, useEffect, useCallback, useState, useLayoutEffect } from 'react';
import { Zap, Bot, AlertTriangle } from 'lucide-react';
import CommandInput from './CommandInput';
import MessageSegments from './MessageSegments';
import MessageContent from './MessageContent';
import MessageBubble from './MessageBubble';
import './Chat.css';

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string;  // Chain-of-thought content from content_type: "reasoning"
  toolRefs?: Array<{ toolId: string; toolName: string; label: string }>;
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
  onStopProcessing?: () => void;
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
  onToolPillClick,
  onStopProcessing
}) => {
  const AUTO_SCROLL_THRESHOLD_PX = 96;
  const chatShellRef = useRef<HTMLDivElement>(null);
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const inputContainerRef = useRef<HTMLDivElement>(null);
  const shouldAutoScrollRef = useRef(true);
  const [inputContainerHeight, setInputContainerHeight] = useState(0);

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
  }, [messages, toolExecutions, queryProgress, isProcessing]);

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
                            <span className="reasoning-icon">💭</span>
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
          queuedCount={queuedMessagesCount}
        />
      </div>
    </div>
  );
};

export default Chat;
