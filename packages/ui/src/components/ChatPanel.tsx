import { useRef, useEffect, useCallback, useState } from 'react';
import { Send, Bot, User, AlertCircle, Clock } from 'lucide-react';
import { parseMarkdown, formatTimestamp } from '../utils/chatPanel';
import './ChatPanel.css';

export type ChatMessageType = 'user' | 'assistant' | 'system';

export interface ChatMessage {
  id: string;
  type: ChatMessageType;
  content: string;
  timestamp?: Date | number;
  status?: 'sending' | 'sent' | 'error';
}

export interface ChatPanelProps {
  messages: ChatMessage[];
  onSendMessage?: (message: string) => void;
  inputValue?: string;
  onInputChange?: (value: string) => void;
  isLoading?: boolean;
  placeholder?: string;
  className?: string;
}

/**
 * Individual message component.
 */
function MessageBubble({ message }: { message: ChatMessage }): JSX.Element {
  const isUser = message.type === 'user';
  const isSystem = message.type === 'system';

  if (isSystem) {
    return (
      <div className="chatpanel-message chatpanel-message-system">
        <span className="chatpanel-message-icon">
          <AlertCircle size={14} />
        </span>
        <div className="chatpanel-message-content">
          <p className="chatpanel-message-text">{message.content}</p>
          {message.timestamp && (
            <span className="chatpanel-message-time">{formatTimestamp(message.timestamp)}</span>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className={`chatpanel-message chatpanel-message-${message.type}`}>
      <span className={`chatpanel-message-icon ${isUser ? 'chatpanel-icon-user' : 'chatpanel-icon-assistant'}`}>
        {isUser ? <User size={14} /> : <Bot size={14} />}
      </span>
      <div className="chatpanel-message-content">
        <div className="chatpanel-message-text">{parseMarkdown(message.content)}</div>
        {(message.timestamp || message.status) && (
          <div className="chatpanel-message-meta">
            {message.timestamp && <span className="chatpanel-message-time">{formatTimestamp(message.timestamp)}</span>}
            {message.status === 'sending' && (
              <span className="chatpanel-message-status chatpanel-status-sending">
                <Clock size={12} />
                Sending...
              </span>
            )}
            {message.status === 'error' && (
              <span className="chatpanel-message-status chatpanel-status-error">Failed to send</span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * A chat message list with input.
 *
 * Displays messages as bubbles with user/assistant styling, auto-scrolls
 * to bottom, includes input area with send button, loading indicator,
 * timestamps, and basic markdown-like content rendering.
 */
function ChatPanel({
  messages,
  onSendMessage,
  inputValue: controlledInputValue,
  onInputChange,
  isLoading = false,
  placeholder = 'Type a message...',
  className,
}: ChatPanelProps): JSX.Element {
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [uncontrolledInputValue, setUncontrolledInputValue] = useState('');

  // Auto-scroll to bottom when messages change
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // Get current input value (controlled or uncontrolled)
  const inputValue = controlledInputValue !== undefined ? controlledInputValue : uncontrolledInputValue;

  // Handle input change
  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      const value = e.target.value;
      if (onInputChange) {
        onInputChange(value);
      } else {
        setUncontrolledInputValue(value);
      }
    },
    [onInputChange],
  );

  // Handle send
  const handleSend = useCallback(() => {
    const trimmed = inputValue.trim();
    if (trimmed && onSendMessage) {
      onSendMessage(trimmed);
      if (onInputChange) {
        onInputChange('');
      } else {
        setUncontrolledInputValue('');
      }
    }
  }, [inputValue, onSendMessage, onInputChange]);

  // Handle keyboard shortcuts
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  return (
    <div className={`chatpanel ${className || ''}`}>
      {/* Messages List */}
      <div className="chatpanel-messages">
        {messages.length === 0 ? (
          <div className="chatpanel-empty">
            <Bot size={32} className="chatpanel-empty-icon" />
            <p className="chatpanel-empty-text">Start a conversation</p>
          </div>
        ) : (
          <>
            {messages.map((message) => (
              <MessageBubble key={message.id} message={message} />
            ))}
            {isLoading && (
              <div className="chatpanel-message chatpanel-message-assistant chatpanel-message-loading">
                <span className="chatpanel-message-icon chatpanel-icon-assistant">
                  <Bot size={14} />
                </span>
                <div className="chatpanel-message-content">
                  <div className="chatpanel-typing-indicator">
                    <span></span>
                    <span></span>
                    <span></span>
                  </div>
                </div>
              </div>
            )}
            <div ref={messagesEndRef} />
          </>
        )}
      </div>

      {/* Input Area */}
      <div className="chatpanel-input-wrapper">
        <div className="chatpanel-input-container">
          <textarea
            ref={textareaRef}
            className="chatpanel-input"
            value={inputValue}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            rows={1}
            disabled={isLoading}
          />
          <button
            type="button"
            className={`chatpanel-send ${!inputValue.trim() || isLoading ? 'chatpanel-send-disabled' : ''}`}
            onClick={handleSend}
            disabled={!inputValue.trim() || isLoading}
            aria-label="Send message"
          >
            <Send size={16} />
          </button>
        </div>
      </div>
    </div>
  );
}

export default ChatPanel;
