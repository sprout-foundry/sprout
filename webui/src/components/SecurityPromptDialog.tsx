import { useEffect, useCallback, useRef } from 'react';
import './ThemedDialog.css';

export interface SecurityPromptDialogProps {
  requestId: string;
  prompt: string;
  filePath?: string;
  concern?: string;
  onRespond: (requestId: string, response: boolean) => void;
}

function SecurityPromptDialog({ requestId, prompt, filePath, concern, onRespond }: SecurityPromptDialogProps): JSX.Element {
  const issueBtnRef = useRef<HTMLButtonElement>(null);
  const ignoreBtnRef = useRef<HTMLButtonElement>(null);

  const handleIssue = useCallback(() => {
    onRespond(requestId, true);
  }, [requestId, onRespond]);

  const handleIgnore = useCallback(() => {
    onRespond(requestId, false);
  }, [requestId, onRespond]);

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      // Cannot dismiss via Escape — user MUST choose
      e.preventDefault();
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
    }
  }, []);

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    // Lock scroll while dialog is open
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
    };
  }, [handleKeyDown]);

  // Auto-focus the "Mark as Issue" button (the safer default)
  useEffect(() => {
    const timer = setTimeout(() => {
      issueBtnRef.current?.focus();
    }, 60);
    return () => clearTimeout(timer);
  }, []);

  return (
    <div className="security-prompt-overlay" role="dialog" aria-modal="true" aria-label="Security concern detected">
      <div className="security-prompt-card" onClick={(e) => e.stopPropagation()}>
        {/* Accent bar - warning color */}
        <div className="security-prompt-accent-bar" />

        {/* Header */}
        <div className="security-prompt-header">
          <span className="security-prompt-shield">⚠</span>
          <h2 className="security-prompt-title">Security Concern Detected</h2>
        </div>

        {/* Body */}
        <div className="security-prompt-body">
          {/* File path */}
          {filePath && (
            <div>
              <span className="security-prompt-file-name-label">File</span>
              <span className="security-prompt-file-name">{filePath}</span>
            </div>
          )}

          {/* Concern type */}
          {concern && (
            <div>
              <span className="security-prompt-concern-label">Concern</span>
              <span className="security-prompt-concern">{concern}</span>
            </div>
          )}

          {/* Full prompt text */}
          <div>
            <span className="security-prompt-prompt-label">Security Analysis</span>
            <div className="security-prompt-prompt-text">{prompt}</div>
          </div>
        </div>

        {/* Footer - Cannot be dismissed, must choose */}
        <div className="security-prompt-footer">
          <button
            ref={ignoreBtnRef}
            type="button"
            className="security-prompt-btn security-prompt-btn--ignore"
            onClick={handleIgnore}
          >
            Ignore
          </button>
          <button
            ref={issueBtnRef}
            type="button"
            className="security-prompt-btn security-prompt-btn--issue"
            onClick={handleIssue}
          >
            Mark as Issue
          </button>
        </div>
      </div>
    </div>
  );
};

export default SecurityPromptDialog;
