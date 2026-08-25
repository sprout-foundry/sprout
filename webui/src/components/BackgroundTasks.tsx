import { Play, Square, Layers, RefreshCw } from 'lucide-react';
import { useState, useEffect, useCallback, useRef } from 'react';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';
import './BackgroundTasks.css';

const POLL_INTERVAL_OPEN_MS = 3000;
const POLL_INTERVAL_IDLE_MS = 15000;

interface BackgroundSession {
  id: string;
  name: string;
  status: 'active' | 'inactive';
  chat_id: string;
  output_preview: string;
  started_at: number;
}

interface SessionsResponse {
  sessions: BackgroundSession[];
  count: number;
}

export interface BackgroundTasksProps {
  onAttachSession?: (sessionId: string) => void;
}

function formatDuration(startTime: number): string {
  if (!startTime || startTime === 0) return '';
  const now = Date.now() / 1000;
  const elapsed = Math.max(0, Math.floor(now - startTime));
  if (elapsed < 60) return `${elapsed}s`;
  if (elapsed < 3600) {
    const mins = Math.floor(elapsed / 60);
    const secs = elapsed % 60;
    return `${mins}m ${secs}s`;
  }
  const hours = Math.floor(elapsed / 3600);
  const mins = Math.floor((elapsed % 3600) / 60);
  return `${hours}h ${mins}m`;
}

function friendlyStatus(status: number): string {
  if (status === 404) return 'Not found';
  if (status === 500) return 'Internal server error';
  if (status === 503) return 'Service unavailable';
  return `Error (${status})`;
}

function BackgroundTasks({ onAttachSession }: BackgroundTasksProps): JSX.Element {
  const [isOpen, setIsOpen] = useState(false);
  const [sessions, setSessions] = useState<BackgroundSession[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const isFetchingRef = useRef(false);
  const popoverRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const fetchSessions = useCallback(async () => {
    if (isFetchingRef.current) return;
    isFetchingRef.current = true;
    setIsLoading(true);
    setError(null);
    try {
      const response = await clientFetch('/api/terminal/agent-sessions');
      if (!response.ok) {
        throw new Error(`Failed to fetch sessions: ${friendlyStatus(response.status)}`);
      }
      const data: SessionsResponse = await response.json();
      setSessions(data?.sessions || []);
    } catch (err) {
      debugLog('[BackgroundTasks] Failed to fetch sessions:', err);
      setError(err instanceof Error ? err.message : String(err));
      setSessions([]);
    } finally {
      setIsLoading(false);
      isFetchingRef.current = false;
    }
  }, []);

  const attachSession = useCallback(
    async (sessionId: string, sessionName?: string) => {
      try {
        const response = await clientFetch(`/api/terminal/agent-sessions/${sessionId}/attach`, {
          method: 'POST',
        });
        if (!response.ok) {
          throw new Error(`Failed to attach session: ${friendlyStatus(response.status)}`);
        }

        window.dispatchEvent(
          new CustomEvent('sprout:terminal-attach-session', {
            detail: { sessionId, name: sessionName },
          }),
        );

        onAttachSession?.(sessionId);
        setIsOpen(false);
        await fetchSessions();
      } catch (err) {
        debugLog('[BackgroundTasks] Failed to attach session:', err);
        setError(err instanceof Error ? err.message : String(err));
      }
    },
    [fetchSessions, onAttachSession],
  );

  const killSession = useCallback(
    async (sessionId: string) => {
      try {
        const response = await clientFetch(`/api/terminal/agent-sessions/${sessionId}/kill`, {
          method: 'POST',
        });
        if (!response.ok) {
          throw new Error(`Failed to kill session: ${friendlyStatus(response.status)}`);
        }
        await fetchSessions();
      } catch (err) {
        debugLog('[BackgroundTasks] Failed to kill session:', err);
        setError(err instanceof Error ? err.message : String(err));
      }
    },
    [fetchSessions],
  );

  // Initial fetch + adaptive polling.
  // When the popover is open we poll quickly so the list stays fresh;
  // when closed we still poll (slowly) so the badge count is accurate.
  useEffect(() => {
    fetchSessions();
    const intervalMs = isOpen ? POLL_INTERVAL_OPEN_MS : POLL_INTERVAL_IDLE_MS;
    const intervalId = setInterval(fetchSessions, intervalMs);
    return () => clearInterval(intervalId);
  }, [isOpen, fetchSessions]);

  // Tick for live duration display, only while the popover is open and
  // there's at least one active task.
  useEffect(() => {
    if (!isOpen) return;
    const hasActive = sessions.some((s) => s.status === 'active');
    if (!hasActive) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [isOpen, sessions]);

  // Refresh badge on terminal WS events.
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (
        detail?.type === 'terminal_output' ||
        detail?.type === 'pty_exit' ||
        detail?.type === 'agent_session_update'
      ) {
        fetchSessions();
      }
    };
    window.addEventListener('sprout:wsevent', handler as EventListener);
    return () => window.removeEventListener('sprout:wsevent', handler as EventListener);
  }, [fetchSessions]);

  // Close popover on outside click / Escape.
  useEffect(() => {
    if (!isOpen) return;
    const handleClick = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        popoverRef.current &&
        !popoverRef.current.contains(target) &&
        triggerRef.current &&
        !triggerRef.current.contains(target)
      ) {
        setIsOpen(false);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsOpen(false);
    };
    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen]);

  void tick;

  const count = sessions.length;
  const triggerTitle = count > 0 ? `${count} background ${count === 1 ? 'task' : 'tasks'}` : 'Background tasks';

  return (
    <div className="background-tasks-dropdown">
      <button
        ref={triggerRef}
        className={`background-tasks-trigger${isOpen ? ' open' : ''}${count > 0 ? ' has-tasks' : ''}`}
        data-testid="background-tasks-trigger"
        onClick={() => setIsOpen((prev) => !prev)}
        title={triggerTitle}
        aria-label={triggerTitle}
        aria-haspopup="menu"
        aria-expanded={isOpen}
        type="button"
      >
        <Layers size={14} />
        {count > 0 && <span className="background-tasks-trigger-badge">{count}</span>}
      </button>

      {isOpen && (
        <div ref={popoverRef} className="background-tasks-popover" role="menu" data-testid="background-tasks-popover">
          <div className="background-tasks-popover-header">
            <div className="background-tasks-popover-title">
              <span>Background Tasks</span>
              {count > 0 && <span className="background-tasks-badge">{count}</span>}
            </div>
            <button
              className="background-task-btn background-task-btn-refresh"
              onClick={(e) => {
                e.stopPropagation();
                fetchSessions();
              }}
              title="Refresh"
              aria-label="Refresh background tasks"
              type="button"
            >
              <RefreshCw size={12} className={isLoading ? 'background-tasks-spin' : ''} />
            </button>
          </div>

          <div className="background-tasks-popover-body">
            {error && (
              <div className="background-tasks-error" aria-live="polite">
                <span>{error}</span>
                <button
                  className="background-task-btn background-task-btn-retry"
                  onClick={(e) => {
                    e.stopPropagation();
                    fetchSessions();
                  }}
                  title="Retry"
                  type="button"
                >
                  <RefreshCw size={12} />
                </button>
              </div>
            )}

            {isLoading && count === 0 && !error && <div className="background-tasks-empty">Loading...</div>}

            {!error && count === 0 && !isLoading && (
              <div className="background-tasks-empty">No background tasks running</div>
            )}

            {!error && count > 0 && (
              <div className="background-tasks-list">
                {sessions.map((session) => (
                  <div key={session.id} className="background-task-item" data-testid="background-task-item">
                    <div className="background-task-info">
                      <div className="background-task-header-row">
                        <span
                          className={`background-task-status ${session.status}`}
                          aria-label={`Status: ${session.status}`}
                        >
                          <span className="background-task-status-dot" />
                        </span>
                        <span className="background-task-name">{session.name || session.id}</span>
                      </div>
                      <div className="background-task-meta">
                        <span className={`background-task-status-text ${session.status}`}>
                          {session.status === 'active' ? 'Running' : 'Exited'}
                        </span>
                        {session.started_at > 0 && (
                          <span className="background-task-duration">{formatDuration(session.started_at)}</span>
                        )}
                      </div>
                      {session.output_preview && (
                        <pre className="background-task-preview">{session.output_preview}</pre>
                      )}
                    </div>
                    <div className="background-task-actions">
                      <button
                        className="background-task-btn background-task-btn-attach"
                        data-testid="background-task-attach"
                        onClick={() => attachSession(session.id, session.name)}
                        title="Attach to terminal"
                        type="button"
                        disabled={session.status === 'inactive'}
                        aria-label={`Attach ${session.name || session.id} to terminal`}
                      >
                        <Play size={14} />
                      </button>
                      <button
                        className="background-task-btn background-task-btn-kill"
                        data-testid="background-task-kill"
                        onClick={() => killSession(session.id)}
                        title="Kill task"
                        type="button"
                        aria-label={`Kill ${session.name || session.id}`}
                      >
                        <Square size={14} />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

export default BackgroundTasks;
