import { useState, useEffect, useCallback, useRef } from 'react';
import { Play, Square, Terminal, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';
import './BackgroundTasks.css';

const POLL_INTERVAL_MS = 3000;

interface BackgroundSession {
  id: string;
  name: string;
  status: 'active' | 'inactive';
  chat_id: string;
  output_preview: string;
  started_at: number; // Unix timestamp (seconds)
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
  const [isExpanded, setIsExpanded] = useState(false);
  const [sessions, setSessions] = useState<BackgroundSession[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const isFetchingRef = useRef(false);

  // Fetch sessions from the API (guarded against concurrent calls)
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

  // Attach a session (make it visible in the terminal)
  const attachSession = useCallback(
    async (sessionId: string, sessionName?: string) => {
      try {
        const response = await clientFetch(`/api/terminal/agent-sessions/${sessionId}/attach`, {
          method: 'POST',
        });
        if (!response.ok) {
          throw new Error(`Failed to attach session: ${friendlyStatus(response.status)}`);
        }

        // Fire a custom event to notify Terminal.tsx
        window.dispatchEvent(
          new CustomEvent('sprout:terminal-attach-session', {
            detail: { sessionId, name: sessionName },
          }),
        );

        // Call the optional prop callback
        onAttachSession?.(sessionId);

        // Refresh the list (session should be removed from background tasks)
        await fetchSessions();
      } catch (err) {
        debugLog('[BackgroundTasks] Failed to attach session:', err);
        setError(err instanceof Error ? err.message : String(err));
      }
    },
    [fetchSessions, onAttachSession],
  );

  // Kill a session
  const killSession = useCallback(
    async (sessionId: string) => {
      try {
        const response = await clientFetch(`/api/terminal/agent-sessions/${sessionId}/kill`, {
          method: 'POST',
        });
        if (!response.ok) {
          throw new Error(`Failed to kill session: ${friendlyStatus(response.status)}`);
        }

        // Refresh the list (session should be removed)
        await fetchSessions();
      } catch (err) {
        debugLog('[BackgroundTasks] Failed to kill session:', err);
        setError(err instanceof Error ? err.message : String(err));
      }
    },
    [fetchSessions],
  );

  // Poll for sessions when expanded
  useEffect(() => {
    if (!isExpanded) return;

    // Clear stale errors on expand
    setError(null);

    // Initial fetch
    fetchSessions();

    // Set up polling
    const intervalId = setInterval(() => {
      fetchSessions();
    }, POLL_INTERVAL_MS);

    return () => {
      clearInterval(intervalId);
    };
  }, [isExpanded, fetchSessions]);

  // Tick every second when expanded and has active sessions
  useEffect(() => {
    if (!isExpanded) return;
    const hasActive = sessions.some((s) => s.status === 'active');
    if (!hasActive) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [isExpanded, sessions]); // depends on isExpanded and sessions (hasActive derived in effect body)

  // Listen for WebSocket events to auto-refresh on terminal updates
  useEffect(() => {
    if (!isExpanded) return;
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      // Refresh if it's a terminal-related event
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
  }, [isExpanded, fetchSessions]);

  // Also fetch when the component first mounts (to update the badge when collapsed)
  useEffect(() => {
    // Skip mount fetch if already expanded (the polling effect handles it)
    if (isExpanded) return;
    fetchSessions();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- intentional mount-only; isExpanded read at mount time

  const count = sessions.length;

  // Tick is used to trigger re-renders for duration updates
  void tick;

  return (
    <div className="background-tasks-container">
      <div className="background-tasks-header" onClick={() => setIsExpanded((prev) => !prev)}>
        <div className="background-tasks-title">
          <Terminal size={16} />
          <span>Background Tasks</span>
          {count > 0 && <span className="background-tasks-badge">{count}</span>}
        </div>
        <div className="background-tasks-toggle">
          {isExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
        </div>
      </div>

      {isExpanded && (
        <div className="background-tasks-body">
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
                <div key={session.id} className="background-task-item">
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
                    {session.output_preview && <pre className="background-task-preview">{session.output_preview}</pre>}
                  </div>
                  <div className="background-task-actions">
                    <button
                      className="background-task-btn background-task-btn-attach"
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
      )}
    </div>
  );
}

export default BackgroundTasks;
