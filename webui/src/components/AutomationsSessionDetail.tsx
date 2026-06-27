import { X } from 'lucide-react';
import { useState, useEffect, useRef, useCallback } from 'react';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';
import './AutomationsSessionDetail.css';

/* ── Type Interfaces ───────────────────────────────────────── */

interface SessionDetail {
  session_id: string;
  workflow: string;
  pid: number;
  status: 'running' | 'exited' | 'stopped';
  started_at: number | string;
  kind: string;
  output_file_path: string;
  budget_usd: number | null;
}

interface OutputResponse {
  output: string;
  offset: number;
  total: number;
}

interface AutomationsSessionDetailProps {
  sessionId: string;
  onClose: () => void;
}

/* ── Helper Functions ──────────────────────────────────────── */

function parseStartedAt(val: number | string): number {
  if (typeof val === 'number') return val;
  return Math.floor(new Date(val).getTime() / 1000);
}

/* ── Component ─────────────────────────────────────────────── */

function AutomationsSessionDetail({ sessionId, onClose }: AutomationsSessionDetailProps): JSX.Element {
  const [session, setSession] = useState<SessionDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [output, setOutput] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(true);

  const outputContainerRef = useRef<HTMLPreElement>(null);
  const isAutoScrolling = useRef<boolean>(true);
  const pollIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const outputOffsetRef = useRef<number>(0);

  /* ── Format Duration ─────────────────────────────────── */

  const formatDuration = useCallback((startedAt: number): string => {
    if (!startedAt || startedAt === 0) return '';
    const now = Date.now() / 1000;
    const elapsed = Math.max(0, Math.floor(now - startedAt));
    if (elapsed < 60) return `${elapsed}s`;
    if (elapsed < 3600) {
      const mins = Math.floor(elapsed / 60);
      const secs = elapsed % 60;
      return `${mins}m ${secs}s`;
    }
    const hours = Math.floor(elapsed / 3600);
    const mins = Math.floor((elapsed % 3600) / 60);
    return `${hours}h ${mins}m`;
  }, []);

  /* ── Data Fetching ───────────────────────────────────── */

  const fetchSession = useCallback(async () => {
    try {
      const response = await clientFetch(`/api/automate/sessions/${encodeURIComponent(sessionId)}`);
      if (!response.ok) {
        if (response.status === 404) {
          setSession(null);
        }
        throw new Error(`Failed to fetch session: ${response.status}`);
      }
      const data: SessionDetail = await response.json();
      setSession(data);
    } catch (err) {
      debugLog('[AutomationsSessionDetail] Failed to fetch session:', err);
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [sessionId]);

  const fetchOutput = useCallback(async () => {
    try {
      const response = await clientFetch(
        `/api/automate/sessions/${encodeURIComponent(sessionId)}/output?since=${outputOffsetRef.current}`,
      );
      if (!response.ok) {
        throw new Error(`Failed to fetch output: ${response.status}`);
      }
      const data: OutputResponse = await response.json();
      if (data.output) {
        setOutput((prev) => prev + data.output);
      }
      outputOffsetRef.current = data.offset;
    } catch (err) {
      debugLog('[AutomationsSessionDetail] Failed to fetch output:', err);
    }
  }, [sessionId]);

  /* ── Auto-scroll ─────────────────────────────────────── */

  const scrollToBottom = useCallback(() => {
    if (isAutoScrolling.current && outputContainerRef.current) {
      outputContainerRef.current.scrollTop = outputContainerRef.current.scrollHeight;
    }
  }, []);

  const handleScroll = useCallback((e: React.UIEvent<HTMLPreElement>) => {
    const el = e.currentTarget;
    const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 10;
    isAutoScrolling.current = atBottom;
  }, []);

  /* ── Initial Fetch ───────────────────────────────────── */

  useEffect(() => {
    setLoading(true);
    setError(null);
    setOutput('');
    outputOffsetRef.current = 0;
    isAutoScrolling.current = true;

    fetchSession();
    fetchOutput();
    setLoading(false);
  }, [sessionId, fetchSession, fetchOutput]);

  /* ── Polling ─────────────────────────────────────────── */

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  useEffect(() => {
    if (session?.status !== 'running') {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
      return;
    }

    pollIntervalRef.current = setInterval(() => {
      fetchSession();
      fetchOutput();
    }, 2000);

    return () => {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
    };
  }, [session?.status, fetchSession, fetchOutput]);

  /* ── After output changes, scroll if auto-scrolling ──── */

  useEffect(() => {
    scrollToBottom();
  }, [output, scrollToBottom]);

  /* ── Derived ─────────────────────────────────────────── */

  const startedAt = session ? parseStartedAt(session.started_at) : 0;
  const elapsed = session ? formatDuration(startedAt) : '';

  /* ── Render ──────────────────────────────────────────── */

  return (
    <div
      className="automations-session-detail"
      role="dialog"
      aria-modal="true"
      aria-label="Session details"
      tabIndex={-1}
    >
      {/* Close Button */}
      <button type="button" className="automations-detail-close" onClick={onClose} title="Close" aria-label="Close">
        <X size={16} />
      </button>

      {/* Header */}
      <div className="automations-detail-header">
        <div className="automations-detail-header-info">
          <div className="automations-detail-session-id" title={session?.session_id}>
            {session?.session_id || sessionId}
          </div>
          <div className="automations-detail-workflow-name">{session?.workflow || 'Unknown'}</div>
        </div>
        <div className="automations-detail-header-meta">
          <span className={`automations-status-badge ${session?.status || 'running'}`}>
            <span className="automations-status-dot" />
            <span>
              {session?.status === 'exited' ? 'Exited' : session?.status === 'stopped' ? 'Stopped' : 'Running'}
            </span>
          </span>
          {session && <span className="automations-detail-elapsed">{elapsed}</span>}
        </div>
      </div>

      {/* Budget Section */}
      <div className="automations-detail-section">
        <div className="automations-detail-section-title">Budget</div>
        {session?.budget_usd && session.budget_usd > 0 ? (
          <div className="automations-budget-bar">
            <div className="automations-budget-fill" style={{ width: '0%', background: 'var(--accent-success)' }} />
            <span className="automations-budget-text">${(session.budget_usd || 0).toFixed(2)} cap</span>
          </div>
        ) : (
          <div className="automations-no-budget">No limit</div>
        )}
      </div>

      {/* Output Section */}
      <div className="automations-detail-section automations-detail-section-output">
        <div className="automations-detail-section-title">Output</div>
        {loading && !session ? (
          <div className="automations-output-empty">Loading...</div>
        ) : error && !session ? (
          <div className="automations-output-empty">{error}</div>
        ) : session?.output_file_path ? (
          <div className="automations-output-container">
            <pre className="automations-output-pre" ref={outputContainerRef} onScroll={handleScroll}>
              <code className="automations-output-code">{output || '(empty)'}</code>
            </pre>
          </div>
        ) : (
          <div className="automations-output-empty">No output captured</div>
        )}
      </div>

      {/* Step Timeline Section */}
      <div className="automations-detail-section">
        <div className="automations-detail-section-title">Step Progress</div>
        {/* SP-065-4c: step events are not yet exposed by the automate backend.
            The session response only includes workflow-level status; per-step
            state (when each step ran, cost, output) requires backend changes
            tracked under SP-065. */}
        <div className="automations-detail-placeholder">
          Step-level events aren't tracked yet. The workflow &quot;{session?.workflow ?? ''}&quot; is currently{' '}
          {session?.status ?? 'unknown'}.
        </div>
      </div>

      {/* Budget Section */}
      <div className="automations-detail-section">
        <div className="automations-detail-section-title">Budget</div>
        {/* SP-065-4d: per-event budget log isn't tracked yet; show the session-level
            cap the workflow was started with. Per-step cost requires backend changes. */}
        <div className="automations-detail-placeholder">
          {session?.budget_usd && session.budget_usd > 0
            ? `Session budget cap: $${session.budget_usd.toFixed(2)}. Per-event cost tracking is not available yet.`
            : 'No budget cap was set for this session.'}
        </div>
      </div>
    </div>
  );
}

export default AutomationsSessionDetail;
