import React, { useState, useRef, useEffect, useCallback } from 'react';
import CommandInput from './CommandInput';
import { stripAnsiCodes } from '../utils/ansi';
import './Chat.css';

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
}

interface ToolExecution {
  id: string;
  tool: string;
  status: 'started' | 'running' | 'completed' | 'error';
  message?: string;
  startTime: Date;
  endTime?: Date;
  details?: any;
}

interface ChatProps {
  messages: Message[];
  onSendMessage: (message: string) => void;
  inputValue: string;
  onInputChange: (value: string) => void;
  isProcessing?: boolean;
  lastError?: string | null;
  toolExecutions?: ToolExecution[];
  queryProgress?: any;
}

const Chat: React.FC<ChatProps> = ({
  messages,
  onSendMessage,
  inputValue,
  onInputChange,
  isProcessing = false,
  lastError = null,
  toolExecutions = [],
  queryProgress = null
}) => {
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set());
  const chatContainerRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when messages, tool executions, or progress updates
  useEffect(() => {
    if (chatContainerRef.current) {
      chatContainerRef.current.scrollTop = chatContainerRef.current.scrollHeight;
    }
  }, [messages, toolExecutions, queryProgress, isProcessing]);

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

  const getToolIcon = (toolName: string) => {
    const iconMap: { [key: string]: string } = {
      'shell_command': '🖥️',
      'read_file': '📖',
      'write_file': '✏️',
      'edit_file': '📝',
      'search_files': '🔍',
      'analyze_ui_screenshot': '🖼️',
      'analyze_image_content': '🔬',
      'web_search': '🌐',
      'fetch_url': '📥',
      'TodoWrite': '📋',
      'TodoRead': '📝',
      'view_history': '📚',
      'rollback_changes': '⏪',
      'mcp_tools': '🔧'
    };
    return iconMap[toolName] || '🔧';
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'started': return '🚀';
      case 'running': return '⚡';
      case 'completed': return '✅';
      case 'error': return '❌';
      default: return '⏳';
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

  const formatTime = (date: Date) => {
    return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const copyToClipboard = useCallback((text: string) => {
    navigator.clipboard.writeText(text);
  }, []);

  const renderContent = (content: string) => {
    const parts = content.split(/(```[\s\S]*?```)/g);
    return parts.map((part, i) => {
      if (part.startsWith('```') && part.endsWith('```')) {
        const code = part.slice(3, -3).trim();
        const firstNewline = code.indexOf('\n');
        const language = firstNewline > 0 ? code.slice(0, firstNewline) : '';
        const codeContent = firstNewline > 0 ? code.slice(firstNewline + 1) : code;
        return (
          <pre key={i} className="code-block">
            {language && <span className="code-language">{language}</span>}
            <code>{codeContent}</code>
          </pre>
        );
      }
      return stripAnsiCodes(part).split('\n').map((line, j) => (
        <div key={`${i}-${j}`} className="message-line">{line || '\u00A0'}</div>
      ));
    });
  };

  return (
    <>
      <div className="chat-header">
        <h2><span className="header-icon">🤖</span>AI Assistant</h2>
        {isProcessing && (
          <div className="header-status">
            <span className="status-dot processing"></span>
            Processing
          </div>
        )}
      </div>

      <div className="chat-container" ref={chatContainerRef}>
        {messages.length === 0 ? (
          <div className="welcome-message">
            <div className="welcome-icon">🤖</div>
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
                  📋
                </button>
                <div className="message-content">
                  {renderContent(message.content)}
                </div>
                <div className="message-timestamp">
                  {formatTime(message.timestamp)}
                </div>
              </div>
            </div>
          ))
        )}

        {/* Tool Execution Progress */}
        {toolExecutions.length > 0 && (
          <div className="tool-executions">
            <div className="tool-executions-header">
              <h4>🔧 Tool Executions</h4>
              <span className="tool-count">{toolExecutions.length} active</span>
            </div>
            {toolExecutions.map((tool) => (
              <div
                key={tool.id}
                className={`tool-execution tool-${tool.status}`}
                onClick={() => toggleToolExpansion(tool.id)}
              >
                <div className="tool-summary">
                  <span className="tool-icon">{getToolIcon(tool.tool)}</span>
                  <span className="tool-name">{tool.tool}</span>
                  <span className="tool-status">{getStatusIcon(tool.status)}</span>
                  <span className="tool-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
                  <span className="tool-expand">
                    {expandedTools.has(tool.id) ? '▼' : '▶'}
                  </span>
                </div>
                
                {tool.message && (
                  <div className="tool-message">{stripAnsiCodes(tool.message)}</div>
                )}
                
                {expandedTools.has(tool.id) && tool.details && (
                  <div className="tool-details">
                    <pre>{JSON.stringify(tool.details, null, 2)}</pre>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Query Progress */}
        {queryProgress && (
          <div className="query-progress">
            <div className="progress-header">
              <span className="progress-icon">⚡</span>
              <span className="progress-text">{queryProgress.message || 'Processing...'}</span>
            </div>
            {queryProgress.details && (
              <div className="progress-details">
                {queryProgress.details}
              </div>
            )}
          </div>
        )}

        {/* Processing Indicator */}
        {isProcessing && toolExecutions.length === 0 && !queryProgress && (
          <div className="processing-indicator">
            <div className="processing-content">
              <div className="processing-spinner">⚡</div>
              <div className="processing-text">Processing your request...</div>
            </div>
          </div>
        )}

        {/* Error Display */}
        {lastError && (
          <div className="error-indicator">
            <div className="error-content">
              <div className="error-icon">⚠️</div>
              <div className="error-text">{lastError}</div>
            </div>
          </div>
        )}
      </div>

      <div className="input-container">
        <CommandInput
          value={inputValue}
          onChange={onInputChange}
          onSend={onSendMessage}
          placeholder="Ask me anything about your code..."
          multiline={true}
          autoFocus={true}
        />
      </div>
    </>
  );
};

export default Chat;