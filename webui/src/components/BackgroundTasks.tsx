import { useState, useEffect, useCallback } from 'react';
import { Play, Square, Terminal, ChevronDown, ChevronRight } from 'lucide-react';
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
}

interface SessionsResponse {
  sessions: BackgroundSession[];
  count: number;
}

interface BackgroundTasksProps {
  onAttachSession?: (sessionId: string) => void;
}

function BackgroundTasks({ onAttachSession }: BackgroundTasksProps): JSX.Element {
  const [isExpanded, setIsExpanded] = useState(false);
  const [sessions, setSessions] = useState<BackgroundSession[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Fetch sessions from the API
  const fetchSessions = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await clientFetch('/api/terminal/agent-sessions');
      if (!response.ok) {
        throw new Error(`Failed to fetch sessions: ${response.status}`);
      }
      const data: SessionsResponse = await response.json();
      setSessions(data.sessions || []);
    } catch (err) {
      debugLog('[BackgroundTasks] Failed to fetch sessions:', err);
      setError(err instanceof Error ? err.message : String(err));
      setSessions([]);
    } finally {
      setIsLoading(false);
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
          throw new Error(`Failed to attach session: ${response.status}`);
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
          throw new Error(`Failed to kill session: ${response.status}`);
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

  // Also fetch when the component first mounts (to update the badge)
  useEffect(() => {
    // Fetch once on mount, but don't set up polling until expanded
    let cancelled = false;
    clientFetch('/api/terminal/agent-sessions')
      .then((response) => {
        if (cancelled) return;
        if (response.ok) {
          return response.json() as Promise<SessionsResponse>;
        }
        throw new Error(`Failed to fetch sessions: ${response.status}`);
      })
      .then((data) => {
        if (cancelled) return;
        setSessions(data?.sessions || []);
      })
      .catch((err) => {
        if (cancelled) return;
        debugLog('[BackgroundTasks] Failed to fetch sessions on mount:', err);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const count = sessions.length;

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
          {error && <div className="background-tasks-error">{error}</div>}

          {!error && count === 0 && !isLoading && (
            <div className="background-tasks-empty">No background tasks running</div>
          )}

          {!error && count > 0 && (
            <div className="background-tasks-list">
              {sessions.map((session) => (
                <div key={session.id} className="background-task-item">
                  <div className="background-task-info">
                    <div className="background-task-header-row">
                      <span className={`background-task-status ${session.status}`} aria-label={`Status: ${session.status}`}>
                        <span className="background-task-status-dot" />
                      </span>
                      <span className="background-task-name">{session.name || session.id}</span>
                    </div>
                    {session.output_preview && (
                      <pre className="background-task-preview">
                        {session.output_preview}
                      </pre>
                    )}
                  </div>
                  <div className="background-task-actions">
                    <button
                      className="background-task-btn background-task-btn-attach"
                      onClick={() => attachSession(session.id, session.name)}
                      title="Attach to terminal"
                      type="button"
                    >
                      <Play size={14} />
                    </button>
                    <button
                      className="background-task-btn background-task-btn-kill"
                      onClick={() => killSession(session.id)}
                      title="Kill task"
                      type="button"
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
