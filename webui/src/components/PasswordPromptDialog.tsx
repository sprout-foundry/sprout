/**
 * PasswordPromptDialog — modal that asks the user for a password when a
 * shell command triggers one (sudo, passwd, ssh, gpg, etc.).
 *
 * Wire format (see pkg/events/events.go::PasswordRequestEvent):
 *   payload = { request_id, command, prompt, timestamp }
 *
 * Response (see pkg/webui/websocket_password_test.go::PasswordResponseData):
 *   { type: "password_response", data: { request_id, password } }
 *
 * Empty password signals "cancel" — the shell sees EOF on stdin and
 * surfaces a clean error rather than hanging indefinitely. The Go-side
 * handler treats unknown / already-responded request IDs as 404s and
 * returns an error to the client; the dialog is keyed on request_id so
 * a stale modal is harmless.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { Lock, X } from 'lucide-react';
import './ThemedDialog.css';

export interface PasswordPromptDialogProps {
  requestId: string;
  command: string;
  prompt: string;
  onRespond: (requestId: string, password: string) => void;
}

function PasswordPromptDialog({
  requestId,
  command,
  prompt,
  onRespond,
}: PasswordPromptDialogProps): JSX.Element {
  const [password, setPassword] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  const handleSubmit = useCallback(
    (e?: React.FormEvent) => {
      e?.preventDefault();
      // Empty = cancel. Trimmed submission is fine; spaces in passwords
      // are uncommon but legal (sudo tolerates them).
      onRespond(requestId, password);
    },
    [requestId, password, onRespond],
  );

  const handleCancel = useCallback(() => {
    onRespond(requestId, '');
  }, [requestId, onRespond]);

  // Enter submits, Escape cancels — but only when the input is focused.
  // Outside the input (e.g. clicking backdrop), Escape still cancels
  // since there's nothing else to lose focus from.
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        handleCancel();
      }
    },
    [handleCancel],
  );

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    document.body.style.overflow = 'hidden';
    // Auto-focus the input so the user can type immediately.
    // Slight delay lets the dialog mount before stealing focus.
    const focusTimer = window.setTimeout(() => inputRef.current?.focus(), 50);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
      window.clearTimeout(focusTimer);
    };
  }, [handleKeyDown]);

  // Reset state when the request changes (e.g. another sudo prompt arrives
  // before the user finishes the first one). The dialog itself is keyed
  // by requestId in App.tsx so a new request remounts this component.
  useEffect(() => {
    setPassword('');
  }, [requestId]);

  return (
    <div className="password-prompt-overlay" role="dialog" aria-modal="true" aria-labelledby="password-prompt-title">
      <form className="password-prompt-card" onSubmit={handleSubmit}>
        <div className="password-prompt-accent-bar" />
        <div className="password-prompt-header">
          <div className="password-prompt-shield">
            <Lock size={18} />
          </div>
          <h2 id="password-prompt-title" className="password-prompt-title">
            Password required
          </h2>
        </div>

        <div className="password-prompt-body">
          <p className="password-prompt-description">
            {prompt || 'The agent needs a password to continue.'}
          </p>
          <code className="password-prompt-command">{command}</code>

          <label htmlFor="password-prompt-input" className="password-prompt-label">
            Password
          </label>
          <input
            id="password-prompt-input"
            ref={inputRef}
            type="password"
            className="password-prompt-input"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            spellCheck={false}
            placeholder="Enter password"
          />

          <p className="password-prompt-hint">
            The password is sent only to this shell process and is not stored.
          </p>
        </div>

        <div className="password-prompt-actions">
          <button
            type="button"
            className="password-prompt-btn password-prompt-btn--cancel"
            onClick={handleCancel}
          >
            <X size={14} />
            <span>Cancel</span>
          </button>
          <button
            type="submit"
            className="password-prompt-btn password-prompt-btn--submit"
            disabled={password.length === 0}
          >
            <Lock size={14} />
            <span>Submit</span>
          </button>
        </div>
      </form>
    </div>
  );
}

export default PasswordPromptDialog;