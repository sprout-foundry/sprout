import type { ReactNode } from 'react';
import { Copy } from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';

interface MessageBubbleProps {
  type?: 'user' | 'assistant';
  ariaLabel: string;
  copyText?: string;
  timestamp?: string;
  children: ReactNode;
}

function MessageBubble({ type = 'assistant', ariaLabel, copyText, timestamp, children }: MessageBubbleProps): JSX.Element {
  const handleCopy = async () => {
    if (copyText) {
      await copyToClipboard(copyText);
    }
  };

  return (
    <div
      className={`message ${type}`}
      role={type === 'user' ? 'user-message' : 'assistant-message'}
      aria-label={ariaLabel}
    >
      <div className="message-bubble" data-message-content={copyText || ''}>
        {copyText ? (
          <button className="copy-button" onClick={handleCopy} title="Copy message" aria-label="Copy message">
            <Copy size={14} />
          </button>
        ) : null}
        <div className="message-content">{children}</div>
        {timestamp ? <div className="message-timestamp">{timestamp}</div> : null}
      </div>
    </div>
  );
};

export default MessageBubble;
