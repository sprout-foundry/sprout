import React, { useState } from 'react';
import CommandInput from './CommandInput';
import './Chat.css';

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
}

interface ChatProps {
  messages: Message[];
  onSendMessage: (message: string) => void;
  inputValue: string;
  onInputChange: (value: string) => void;
  isProcessing?: boolean;
  lastError?: string | null;
}

const Chat: React.FC<ChatProps> = ({
  messages,
  onSendMessage,
  inputValue,
  onInputChange,
  isProcessing = false,
  lastError = null
}) => {

  return (
    <>
      <div className="chat-header">
        <h2>💬 AI Assistant</h2>
      </div>

      <div className="chat-container">
        {messages.length === 0 ? (
          <div className="welcome-message">
            Welcome to ledit! I'm ready to help you with code analysis, editing, and more.
          </div>
        ) : (
          messages.map((message) => (
            <div
              key={message.id}
              className={`message ${message.type}`}
            >
              <div className="message-bubble">
                <div className="message-content">
                  {message.content.split('\n').map((line, index) => (
                    <div key={index}>{line || '\u00A0'}</div>
                  ))}
                </div>
              </div>
            </div>
          ))
        )}

        {/* Processing Indicator */}
        {isProcessing && (
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