import React from 'react';
import { Copy } from 'lucide-react';

interface MessageBubbleProps {
  type?: 'user' | 'assistant';
  ariaLabel: string;
  copyText?: string;
  timestamp?: string;
  children: React.ReactNode;
}

const MessageBubble: React.FC<MessageBubbleProps> = ({
  type = 'assistant',
  ariaLabel,
  copyText,
  timestamp,
  children,
}) => {
  const handleCopy = () => {
    if (copyText) {
      navigator.clipboard.writeText(copyText);
    }
  };

  return (
    <div
      className={`message ${type}`}
      role={type === 'user' ? 'user-message' : 'assistant-message'}
      aria-label={ariaLabel}
    >
      <div className="message-bubble">
        {copyText ? (
          <button
            className="copy-button"
            onClick={handleCopy}
            title="Copy message"
            aria-label="Copy message"
          >
            <Copy size={14} />
          </button>
        ) : null}
        <div className="message-content">
          {children}
        </div>
        {timestamp ? (
          <div className="message-timestamp">
            {timestamp}
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default MessageBubble;
