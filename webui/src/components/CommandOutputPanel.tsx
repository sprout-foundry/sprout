/**
 * CommandOutputPanel — SP-114 Phase 2d client-side wire-up.
 *
 * Modal-style terminal output viewer. Subscribes (via the useCommandOutput
 * hook from the consumer) to the WebSocket command_output stream so users
 * see streaming output from a safe slash command as it arrives.
 *
 * Visibility rules:
 *   - Hidden when there's no command, no running state, and no output.
 *   - Visible while the command is running or while output lingers.
 *   - Auto-hides 2 seconds after is_final if the user hasn't interacted
 *     (hover pauses the auto-hide timer so users can read the output).
 *
 * Keyboard / a11y:
 *   - role="status" wraps the panel so screen readers announce it.
 *   - Dismiss button has :focus-visible styling (CSS).
 *   - aria-live="polite" inside the output region so chunked updates
 *     don't shout; final state is announced via the banner state.
 */

import { AlertCircle, Loader2, X } from 'lucide-react';
import { useCallback, useEffect, useRef } from 'react';
import type { CommandOutputState } from '../hooks/useCommandOutput';
import './CommandOutputPanel.css';

export interface CommandOutputPanelProps {
  state: CommandOutputState;
  onDismiss?: () => void;
}

// 2 seconds feels right: short enough to not block the chat, long
// enough to read a short result. Hover pauses; the timer is recreated
// any time input/output arrives or final arrives, so users with a
// streaming command get plenty of time to read.
const AUTO_HIDE_MS = 2000;
const DISTANCE_FROM_BOTTOM_PX = 48;

export function CommandOutputPanel({ state, onDismiss }: CommandOutputPanelProps): JSX.Element | null {
  const scrollRef = useRef<HTMLDivElement>(null);
  const userScrolledRef = useRef(false);
  const hoverRef = useRef(false);
  const dismissTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Combined auto-scroll + user-scroll-reset (mirrors packages/ui
  // LiveLog.tsx). When the user scrolls up beyond a threshold we treat
  // it as "they want to read what was there" and lock autoscroll until
  // they scroll back to the bottom.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom <= DISTANCE_FROM_BOTTOM_PX) {
      userScrolledRef.current = false;
    }
    if (!userScrolledRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [state.output]);

  // Auto-hide: clear any prior timer and reschedule when the command
  // is finished (not running) and there's content/error to show. Hover
  // pauses the timer; unpausing restarts it (so the user gets a full
  // 2s after they stop hovering).
  //
  // The schedule fires regardless of whether output is non-empty — a
  // finished command with output is exactly the case where the user
  // wants to read briefly then have the panel disappear. We only skip
  // scheduling when the command is still running OR there's nothing
  // to show (no command, no output, no error).
  useEffect(() => {
    if (dismissTimerRef.current) {
      clearTimeout(dismissTimerRef.current);
      dismissTimerRef.current = null;
    }
    const isEmpty = state.command === null && state.output === '' && state.error === null;
    const shouldSchedule = !state.isRunning && !isEmpty && onDismiss;
    if (shouldSchedule && onDismiss) {
      dismissTimerRef.current = setTimeout(() => {
        if (!hoverRef.current) {
          onDismiss();
        }
      }, AUTO_HIDE_MS);
    }
    return () => {
      if (dismissTimerRef.current) {
        clearTimeout(dismissTimerRef.current);
        dismissTimerRef.current = null;
      }
    };
  }, [state.isRunning, state.output, state.error, state.command, onDismiss]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledRef.current = distanceFromBottom > DISTANCE_FROM_BOTTOM_PX;
  }, []);

  // Visibility predicates — keep this as a memo-free computation; it
  // changes whenever the parent re-renders (cheap).
  const hasOutput = state.output !== '';
  const error = state.error;
  const hasError = error !== null;
  const isVisible = state.command !== null || state.isRunning || hasOutput || hasError;
  if (!isVisible) return null;

  const headerLabel = state.command ? `/${state.command}` : 'Command';
  // Visibility is computed above; React renders the panel either way
  // (because we already returned null above). Visual styling diverges
  // based on running vs done-with-content.
  return (
    <div
      className={`command-output-panel${state.isRunning ? ' is-running' : ''}${hasError ? ' has-error' : ''}`}
      role="status"
      aria-live="polite"
      onMouseEnter={() => {
        hoverRef.current = true;
      }}
      onMouseLeave={() => {
        hoverRef.current = false;
      }}
      data-testid="command-output-panel"
    >
      <div className="command-output-panel-header">
        <div className="command-output-panel-title">
          {state.isRunning ? (
            <Loader2 size={14} className="command-output-panel-spinner" aria-hidden="true" />
          ) : (
            <span className="command-output-panel-dot" aria-hidden="true" />
          )}
          <span className="command-output-panel-cmd">{headerLabel}</span>
          {state.isRunning ? <span className="command-output-panel-running">running…</span> : null}
        </div>
        <button
          type="button"
          className="command-output-panel-dismiss"
          onClick={onDismiss}
          aria-label="Dismiss command output"
          title="Dismiss"
        >
          <X size={14} aria-hidden="true" />
        </button>
      </div>
      {state.droppedBytes > 0 ? (
        <div className="command-output-panel-warning" role="alert">
          <AlertCircle size={14} aria-hidden="true" />
          <span>
            Some output was dropped by the server ({state.droppedBytes.toLocaleString()} bytes).
            The HTTP response contains the complete transcript.
          </span>
        </div>
      ) : null}
      {hasError && error ? (
        <div className="command-output-panel-error" role="alert">
          <AlertCircle size={14} aria-hidden="true" />
          <span>{error.message}</span>
        </div>
      ) : null}
      {hasOutput ? (
        <div
          className="command-output-panel-body"
          ref={scrollRef}
          onScroll={handleScroll}
          data-testid="command-output-panel-body"
          // Tab order: -1 so the panel doesn't steal focus from the
          // chat input. The dismiss button is the only interactive.
          tabIndex={-1}
          // Mark scrolled bottom so screen readers know the user can
          // scroll; aria-live ensures new chunks are read.
          aria-label="Command output stream"
        >
          <pre className="command-output-panel-text">{state.output}</pre>
        </div>
      ) : !state.isRunning ? (
        <div className="command-output-panel-empty">No output captured.</div>
      ) : null}
    </div>
  );
}

export default CommandOutputPanel;
