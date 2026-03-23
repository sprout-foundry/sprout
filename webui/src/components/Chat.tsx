import React, { useState, useRef, useEffect, useCallback, type ReactNode } from 'react';
import {
  Terminal, BookOpen, FileEdit, Pencil, Search, Eye, FlaskConical,
  Globe, ArrowDown, ClipboardList, ScrollText, RotateCcw,
  Wrench, Rocket, Zap, CheckCircle2, XCircle, Hourglass,
  Bot, Copy, AlertTriangle, ChevronDown, ChevronRight,
  BarChart3, FileText
} from 'lucide-react';
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
  arguments?: string;
  result?: string;
  persona?: string;
  subagentType?: 'single' | 'parallel';
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

  const formatTime = (date: Date) => {
    return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const copyToClipboard = useCallback((text: string) => {
    navigator.clipboard.writeText(text);
  }, []);

  const activeToolCount = toolExecutions.filter(
    (tool) => tool.status === 'started' || tool.status === 'running'
  ).length;

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
    <div className="chat-shell">
      <div className="chat-header">
        <h2><span className="header-icon"><Bot size={16} /></span>AI Assistant</h2>
        {isProcessing && (
          <div className="header-status">
            <span className="status-dot processing"></span>
            Processing
          </div>
        )}
      </div>

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
                    {renderContent(message.content)}
                  </div>
                  <div className="message-timestamp">
                    {formatTime(message.timestamp)}
                  </div>
                </div>
              </div>
            ))
          )}

          {/* Query Progress */}
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

          {/* Processing Indicator */}
          {isProcessing && toolExecutions.length === 0 && !queryProgress && (
            <div className="processing-indicator">
              <div className="processing-content">
                <div className="processing-spinner"><Zap size={14} /></div>
                <div className="processing-text">Processing your request...</div>
              </div>
            </div>
          )}

          {/* Error Display */}
          {lastError && (
            <div className="error-indicator">
              <div className="error-content">
                <div className="error-icon"><AlertTriangle size={14} /></div>
                <div className="error-text">{lastError}</div>
              </div>
            </div>
          )}
        </div>

        <aside className="chat-tools-panel" aria-label="Tool executions panel">
          <div className="tool-executions-header">
            <h4><Wrench size={14} className="inline-icon" /> Tool Executions</h4>
            <span className="tool-count">
              {activeToolCount > 0 ? `${activeToolCount} active` : `${toolExecutions.length} total`}
            </span>
          </div>
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
                    className={`tool-execution tool-${tool.status} ${isSub ? 'tool-subagent' : ''}`}
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
                        {!tool.result && tool.arguments && isSub && (
                          <div className="tool-detail-section">
                            <div className="tool-detail-label"><ClipboardList size={12} className="inline-icon" /> Call</div>
                            <pre>{formatToolDetail(tool.arguments)}</pre>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                );
              })
            )}
          </div>
        </aside>
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
    </div>
  );
};

export default Chat;
