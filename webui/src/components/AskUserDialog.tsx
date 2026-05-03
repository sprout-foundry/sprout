import { useEffect, useCallback, useRef, useState } from 'react';
import './ThemedDialog.css';

export interface AskUserDialogProps {
  requestId: string;
  question: string;
  onRespond: (requestId: string, response: string) => void;
}

function AskUserDialog({
  requestId,
  question,
  onRespond,
}: AskUserDialogProps): JSX.Element {
  const [response, setResponse] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSubmit = useCallback(() => {
    const trimmedResponse = response.trim();
    if (trimmedResponse.length > 0) {
      onRespond(requestId, trimmedResponse);
    }
  }, [requestId, response, onRespond]);

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      // Cannot dismiss via Escape — user MUST respond
      e.preventDefault();
      return;
    }
    if (e.key === 'Enter') {
      // Submit on Enter (without Ctrl/Cmd)
      if (e.metaKey || e.ctrlKey || e.shiftKey) {
        // Allow Ctrl+Enter, Cmd+Enter, Shift+Enter for newlines
        return;
      }
      e.preventDefault();
      handleSubmit();
    }
  }, [handleSubmit]);

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    // Lock scroll while dialog is open
    document.body.style.overflow = 'hidden';
    // Auto-focus textarea
    const timer = setTimeout(() => {
      textareaRef.current?.focus();
    }, 60);

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
      clearTimeout(timer);
    };
  }, [handleKeyDown]);

  return (
    <div className="ask-user-overlay" role="dialog" aria-modal="true" aria-label="User input required">
      <div className="ask-user-card" onClick={(e) => e.stopPropagation()}>
        {/* Accent bar - info color */}
        <div className="ask-user-accent-bar" />

        {/* Header */}
        <div className="ask-user-header">
          <span className="ask-user-icon">?</span>
          <h2 className="ask-user-title">Question</h2>
        </div>

        {/* Body */}
        <div className="ask-user-body">
          {/* Question text */}
          <div>
            <span className="ask-user-question-label">Question</span>
            <div className="ask-user-question-text">{question}</div>
          </div>

          {/* Text input */}
          <div>
            <label htmlFor="ask-user-response">Your Response</label>
            <textarea
              id="ask-user-response"
              ref={textareaRef}
              value={response}
              onChange={(e) => setResponse(e.target.value)}
              placeholder="Type your response here..."
              rows={4}
            />
          </div>
        </div>

        {/* Footer - Cannot be dismissed, must respond */}
        <div className="ask-user-footer">
          <button
            type="button"
            className="ask-user-btn ask-user-btn--submit"
            onClick={handleSubmit}
            disabled={response.trim().length === 0}
          >
            Submit
          </button>
        </div>
      </div>
    </div>
  );
}

export default AskUserDialog;
