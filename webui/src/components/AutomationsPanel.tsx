import type { WsEvent } from '@sprout/events';
import { Play, Square, Zap, X, AlertCircle } from 'lucide-react';
import { useState, useEffect, useCallback, useRef } from 'react';
import { subscribeAutomate } from '../services/automateEvents';
import { clientFetch } from '../services/clientSession';
import { WebSocketService } from '../services/websocket';
import { debugLog } from '../utils/log';
import AutomationsSessionDetail from './AutomationsSessionDetail';
import './AutomationsPanel.css';

/* ── Type Interfaces ───────────────────────────────────────── */

interface WorkflowInfo {
  name: string;
  description: string;
  filename: string;
  file_path: string;
}

interface AutomationSession {
  session_id: string;
  workflow: string;
  pid: number;
  status: 'running' | 'exited' | 'stopped';
  started_at: number;
  kind: string;
  output_file_path: string;
  budget_usd: number;
}

interface WorkflowsResponse {
  workflows: WorkflowInfo[];
}

interface SessionsResponse {
  sessions: AutomationSession[];
}

interface RunResponse {
  session_id: string;
  workflow: string;
  status: string;
}

interface RunModalState {
  open: boolean;
  workflow: WorkflowInfo | null;
  budgetUsd: string;
  heartbeat: string;
}

interface AutomationsPanelProps {
  onNavigateToSession?: (sessionId: string) => void;
}

/* ── Helper Functions ──────────────────────────────────────── */

function formatDuration(startedAt: number): string {
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
}

function truncateId(id: string): string {
  if (id.length <= 12) return id;
  return id.slice(0, 12) + '…';
}

function friendlyStatus(status: number): string {
  if (status === 404) return 'Not found';
  if (status === 500) return 'Internal server error';
  if (status === 503) return 'Service unavailable';
  return `Error (${status})`;
}

/* ── BudgetBar Component ───────────────────────────────────── */

interface BudgetBarProps {
  spent: number;
  cap: number;
}

function BudgetBar({ spent, cap }: BudgetBarProps): JSX.Element {
  const pct = cap > 0 ? Math.min(100, (spent / cap) * 100) : 0;
  const ratio = cap > 0 ? spent / cap : 0;

  let color: string;
  if (ratio < 0.5) {
    color = 'var(--accent-success)';
  } else if (ratio <= 0.8) {
    color = 'var(--accent-warning)';
  } else {
    color = 'var(--accent-error)';
  }

  return (
    <div className="automations-budget-bar">
      <div className="automations-budget-fill" style={{ width: `${pct}%`, background: color }} />
      <span className="automations-budget-text">
        ${spent.toFixed(2)} / ${cap.toFixed(2)}
      </span>
    </div>
  );
}

/* ── Main Component ────────────────────────────────────────── */

type TabId = 'available' | 'running' | 'recent';

function AutomationsPanel({ onNavigateToSession }: AutomationsPanelProps): JSX.Element {
  const [activeTab, setActiveTab] = useState<TabId>('available');

  // Workflows
  const [workflows, setWorkflows] = useState<WorkflowInfo[]>([]);
  const [workflowsLoading, setWorkflowsLoading] = useState(false);
  const [workflowsError, setWorkflowsError] = useState<string | null>(null);

  // Sessions
  const [sessions, setSessions] = useState<AutomationSession[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsError, setSessionsError] = useState<string | null>(null);

  // Run modal
  const [runModal, setRunModal] = useState<RunModalState>({
    open: false,
    workflow: null,
    budgetUsd: '',
    heartbeat: '',
  });
  const [isRunningWorkflow, setIsRunningWorkflow] = useState(false);

  // Stop loading tracking
  const [stoppingIds, setStoppingIds] = useState<Set<string>>(new Set());

  // Pagination for running/recent session lists
  const AUTOMATIONS_SESSIONS_INITIAL = 10;
  const AUTOMATIONS_SESSIONS_INCREMENT = 10;
  const [visibleRunning, setVisibleRunning] = useState(AUTOMATIONS_SESSIONS_INITIAL);
  const [visibleRecent, setVisibleRecent] = useState(AUTOMATIONS_SESSIONS_INITIAL);

  // Tick for live elapsed time display
  const [tick, setTick] = useState(0);

  // Session detail panel
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);

  // Fetch guard refs
  const isFetchingWorkflowsRef = useRef(false);
  const isFetchingSessionsRef = useRef(false);
  // Tracks whether we've already attempted a workflows fetch for the
  // current Available-tab visit. Without this, the "available" effect
  // re-fires forever when the workflow list is empty (workflows.length
  // stays 0; workflowsLoading flips back to false; effect deps change
  // → another fetch). Reset to false when activeTab leaves Available.
  const hasFetchedWorkflowsRef = useRef(false);

  /* ── Data Fetching ─────────────────────────────────────── */

  const fetchWorkflows = useCallback(async () => {
    if (isFetchingWorkflowsRef.current) return;
    isFetchingWorkflowsRef.current = true;
    setWorkflowsLoading(true);
    setWorkflowsError(null);
    try {
      const response = await clientFetch('/api/automate/workflows');
      if (!response.ok) {
        throw new Error(`Failed to fetch workflows: ${friendlyStatus(response.status)}`);
      }
      const data: WorkflowsResponse = await response.json();
      setWorkflows(data?.workflows || []);
    } catch (err) {
      debugLog('[AutomationsPanel] Failed to fetch workflows:', err);
      setWorkflowsError(err instanceof Error ? err.message : String(err));
      setWorkflows([]);
    } finally {
      setWorkflowsLoading(false);
      isFetchingWorkflowsRef.current = false;
    }
  }, []);

  const fetchSessions = useCallback(async () => {
    if (isFetchingSessionsRef.current) return;
    isFetchingSessionsRef.current = true;
    setSessionsLoading(true);
    setSessionsError(null);
    try {
      const response = await clientFetch('/api/automate/sessions');
      if (!response.ok) {
        throw new Error(`Failed to fetch sessions: ${friendlyStatus(response.status)}`);
      }
      const data: SessionsResponse = await response.json();
      setSessions(data?.sessions || []);
      setVisibleRunning(AUTOMATIONS_SESSIONS_INITIAL);
      setVisibleRecent(AUTOMATIONS_SESSIONS_INITIAL);
    } catch (err) {
      debugLog('[AutomationsPanel] Failed to fetch sessions:', err);
      setSessionsError(err instanceof Error ? err.message : String(err));
      setSessions([]);
    } finally {
      setSessionsLoading(false);
      isFetchingSessionsRef.current = false;
    }
  }, []);

  /* ── Actions ───────────────────────────────────────────── */

  const openRunModal = useCallback((wf: WorkflowInfo) => {
    setRunModal({
      open: true,
      workflow: wf,
      budgetUsd: '',
      heartbeat: '',
    });
  }, []);

  const closeRunModal = useCallback(() => {
    setRunModal((prev) => ({ ...prev, open: false, workflow: null }));
    setIsRunningWorkflow(false);
  }, []);

  const handleRunWorkflow = useCallback(async () => {
    if (!runModal.workflow) return;

    const confirmed = window.confirm(`Run workflow "${runModal.workflow.name}"?`);
    if (!confirmed) return;

    setIsRunningWorkflow(true);

    const body: Record<string, unknown> = {
      workflow: runModal.workflow.name,
    };
    if (runModal.budgetUsd.trim() !== '') {
      body.budget_usd = parseFloat(runModal.budgetUsd);
    }
    if (runModal.heartbeat.trim() !== '') {
      body.heartbeat = parseInt(runModal.heartbeat, 10);
    }

    try {
      const response = await clientFetch('/api/automate/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!response.ok) {
        throw new Error(`Failed to run workflow: ${friendlyStatus(response.status)}`);
      }
      const result: RunResponse = await response.json();
      debugLog('[AutomationsPanel] Workflow started:', result);
      closeRunModal();
      setActiveTab('running');
      await fetchSessions();
    } catch (err) {
      debugLog('[AutomationsPanel] Failed to run workflow:', err);
      setSessionsError(err instanceof Error ? err.message : String(err));
    } finally {
      setIsRunningWorkflow(false);
    }
  }, [runModal, closeRunModal, fetchSessions]);

  const closeDetail = useCallback(() => {
    setSelectedSessionId(null);
  }, []);

  const handleStopSession = useCallback(
    async (sessionId: string) => {
      const shortId = truncateId(sessionId);
      const confirmed = window.confirm(`Stop session ${shortId}?`);
      if (!confirmed) return;

      setStoppingIds((prev) => new Set(prev).add(sessionId));

      try {
        const response = await clientFetch(`/api/automate/sessions/${encodeURIComponent(sessionId)}/stop`, {
          method: 'POST',
        });
        if (!response.ok) {
          throw new Error(`Failed to stop session: ${friendlyStatus(response.status)}`);
        }
        await fetchSessions();
      } catch (err) {
        debugLog('[AutomationsPanel] Failed to stop session:', err);
        setSessionsError(err instanceof Error ? err.message : String(err));
      } finally {
        setStoppingIds((prev) => {
          const next = new Set(prev);
          next.delete(sessionId);
          return next;
        });
      }
    },
    [fetchSessions],
  );

  /* ── Event-driven session refetch (replaces polling) ──────── */

  // Fetch workflows once per visit to the Available tab. The empty-list
  // case must not trigger a refetch on every render — see the
  // `hasFetchedWorkflowsRef` comment above.
  useEffect(() => {
    if (activeTab !== 'available') {
      hasFetchedWorkflowsRef.current = false;
      return;
    }
    if (hasFetchedWorkflowsRef.current) return;
    hasFetchedWorkflowsRef.current = true;
    fetchWorkflows();
  }, [activeTab, fetchWorkflows]);

  // Subscribe to the automate WebSocket channel. Send once on mount AND
  // every time the WS reconnects — if the user opens this panel before
  // the initial WebSocket handshake completes, the cold-start subscribe
  // would otherwise sit in the message queue (which only flushes on
  // reconnect) and be silently dropped. We listen for connection_status
  // with `connected: true` to handle initial connect and reconnect
  // uniformly.
  useEffect(() => {
    const sendSubscribe = () => {
      WebSocketService.getInstance().sendEvent({
        type: 'subscribe',
        data: { channel: 'automate' },
      });
    };

    sendSubscribe();

    // The WebSocketService already emits a synthetic connection_status
    // event with connected: true both for initial connect and reconnect,
    // so listening here covers both cases uniformly. Treat any event that
    // carries `connected === true` as "send subscribe now".
    const handleEvent = (event: WsEvent) => {
      if (event.type !== 'connection_status') return;
      const data = event.data as { connected?: boolean } | undefined;
      if (data?.connected === true) {
        sendSubscribe();
      }
    };
    WebSocketService.getInstance().onEvent(handleEvent);
    return () => {
      WebSocketService.getInstance().removeEvent(handleEvent);
    };
  }, []);

  // Refetch sessions when automate lifecycle events arrive.
  useEffect(() => {
    return subscribeAutomate((eventType, payload) => {
      if (eventType === 'automate.session_started' || eventType === 'automate.session_ended') {
        // Only refetch when the panel is viewing a sessions tab.
        // The event fires globally so we let the panel decide whether to act.
        if (activeTab === 'running' || activeTab === 'recent') {
          fetchSessions();
        }
      }
      // budget_update and output_chunk are handled by AutomationsSessionDetail
    });
  }, [activeTab, fetchSessions]);

  // Initial fetch when switching to running or recent tab (no polling —
  // events drive subsequent updates).
  useEffect(() => {
    if (activeTab === 'running' || activeTab === 'recent') {
      fetchSessions();
    }
  }, [activeTab, fetchSessions]);

  // Tick for live elapsed time display on running tab
  useEffect(() => {
    if (activeTab !== 'running') return;
    const hasRunning = sessions.some((s) => s.status === 'running');
    if (!hasRunning) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [activeTab, sessions]);

  /* ── Focus Session (from sprout://automations/session/ links) ── */

  useEffect(() => {
    const handler = (event: CustomEvent) => {
      const sessionId = event.detail?.sessionId;
      if (sessionId) {
        debugLog('[AutomationsPanel] Focusing on session:', sessionId);
        setActiveTab('running');
        setSelectedSessionId(sessionId);
        fetchSessions();
      }
    };
    window.addEventListener('sprout-navigate-automation', handler as EventListener);
    return () => window.removeEventListener('sprout-navigate-automation', handler as EventListener);
  }, [fetchSessions]);

  /* ── Derived data ──────────────────────────────────────── */

  const runningSessions = sessions.filter((s) => s.status === 'running');
  const recentSessions = sessions.filter((s) => s.status !== 'running');

  const visibleRunningSessions = runningSessions.slice(0, visibleRunning);
  const visibleRecentSessions = recentSessions.slice(0, visibleRecent);
  const runningOverflow = Math.max(0, runningSessions.length - visibleRunning);
  const recentOverflow = Math.max(0, recentSessions.length - visibleRecent);

  /* ── Tab labels ────────────────────────────────────────── */

  const tabLabels: { id: TabId; label: string; count?: number }[] = [
    { id: 'available', label: 'Available' },
    { id: 'running', label: 'Running', count: runningSessions.length },
    { id: 'recent', label: 'Recent', count: recentSessions.length },
  ];

  /* ── Render ────────────────────────────────────────────── */

  return (
    <div className="automations-panel">
      {/* Tab Bar */}
      <div className="automations-tab-bar" role="tablist" aria-label="Automation tabs">
        {tabLabels.map((tab) => (
          <button
            key={tab.id}
            role="tab"
            aria-selected={activeTab === tab.id}
            className={`automations-tab ${activeTab === tab.id ? 'active' : ''}`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
            {tab.count !== undefined && tab.count > 0 ? (
              <span className="automations-tab-count">{tab.count}</span>
            ) : null}
          </button>
        ))}
      </div>

      {/* Tab Content */}
      <div className="automations-tab-content">
        {/* ── Available Tab ─────────────────────────────── */}
        {activeTab === 'available' && (
          <div className="automations-available">
            {workflowsError && (
              <div className="automations-error" aria-live="polite">
                <AlertCircle size={14} />
                <span>{workflowsError}</span>
              </div>
            )}

            {workflowsLoading && workflows.length === 0 && !workflowsError && (
              <div className="automations-empty">Loading workflows...</div>
            )}

            {!workflowsError && workflows.length === 0 && !workflowsLoading && (
              <div className="automations-empty">No automation workflows available</div>
            )}

            {workflows.length > 0 && (
              <div className="automations-workflow-list">
                {workflows.map((wf) => (
                  <div key={wf.filename} className="automations-workflow-card">
                    <div className="automations-workflow-info">
                      <div className="automations-workflow-name">{wf.name}</div>
                      {wf.description && <div className="automations-workflow-desc">{wf.description}</div>}
                    </div>
                    <button
                      className="automations-run-btn"
                      onClick={() => openRunModal(wf)}
                      title={`Run ${wf.name}`}
                      aria-label={`Run ${wf.name}`}
                    >
                      <Play size={14} />
                      <span>Run</span>
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* ── Running Tab ──────────────────────────────── */}
        {activeTab === 'running' && (
          <div className="automations-running">
            {sessionsError && (
              <div className="automations-error" aria-live="polite">
                <AlertCircle size={14} />
                <span>{sessionsError}</span>
              </div>
            )}

            {sessionsLoading && sessions.length === 0 && !sessionsError && (
              <div className="automations-empty">Loading sessions...</div>
            )}

            {!sessionsError && runningSessions.length === 0 && !sessionsLoading && (
              <div className="automations-empty">No sessions running</div>
            )}

            {runningSessions.length > 0 && (
              <div className="automations-session-table">
                <div className="automations-session-header">
                  <span className="automations-col-id">Session</span>
                  <span className="automations-col-workflow">Workflow</span>
                  <span className="automations-col-status">Status</span>
                  <span className="automations-col-elapsed">Elapsed</span>
                  <span className="automations-col-budget">Budget</span>
                  <span className="automations-col-actions" />
                </div>
                {visibleRunningSessions.map((session) => (
                  <div
                    key={session.session_id}
                    className="automations-session-row clickable"
                    onClick={() => {
                      setSelectedSessionId(session.session_id);
                      onNavigateToSession?.(session.session_id);
                    }}
                  >
                    <span className="automations-session-id" title={session.session_id}>
                      {truncateId(session.session_id)}
                    </span>
                    <span className="automations-session-workflow">{session.workflow}</span>
                    <span className="automations-status-badge running">
                      <span className="automations-status-dot" />
                      <span>Running</span>
                    </span>
                    <span className="automations-session-elapsed">{formatDuration(session.started_at)}</span>
                    <span className="automations-session-budget">
                      {session.budget_usd > 0 ? (
                        <BudgetBar spent={0} cap={session.budget_usd} />
                      ) : (
                        <span className="automations-no-budget">No limit</span>
                      )}
                    </span>
                    <span className="automations-col-actions">
                      <button
                        className="automations-stop-btn"
                        onClick={(e) => {
                          e.stopPropagation();
                          handleStopSession(session.session_id);
                        }}
                        disabled={stoppingIds.has(session.session_id)}
                        title="Stop session"
                        aria-label={`Stop session ${truncateId(session.session_id)}`}
                      >
                        {stoppingIds.has(session.session_id) ? (
                          'Stopping...'
                        ) : (
                          <>
                            <Square size={12} />
                            <span>Stop</span>
                          </>
                        )}
                      </button>
                    </span>
                  </div>
                ))}
                {/* Pagination: show more if there are running sessions past the visible window */}
                {runningOverflow > 0 && (
                  <button
                    type="button"
                    className="automations-load-more"
                    onClick={() => setVisibleRunning((n) => n + AUTOMATIONS_SESSIONS_INCREMENT)}
                  >
                    Show more ({runningOverflow} more)
                  </button>
                )}
                {/* Force re-render for tick-based elapsed updates */}
                <span className="sr-only" aria-live="polite">
                  {tick}
                </span>
              </div>
            )}
          </div>
        )}

        {/* ── Recent Tab ───────────────────────────────── */}
        {activeTab === 'recent' && (
          <div className="automations-recent">
            {sessionsError && (
              <div className="automations-error" aria-live="polite">
                <AlertCircle size={14} />
                <span>{sessionsError}</span>
              </div>
            )}

            {sessionsLoading && sessions.length === 0 && !sessionsError && (
              <div className="automations-empty">Loading sessions...</div>
            )}

            {!sessionsError && recentSessions.length === 0 && !sessionsLoading && (
              <div className="automations-empty">No recent sessions</div>
            )}

            {recentSessions.length > 0 && (
              <div className="automations-session-table">
                <div className="automations-session-header">
                  <span className="automations-col-id">Session</span>
                  <span className="automations-col-workflow">Workflow</span>
                  <span className="automations-col-status">Status</span>
                  <span className="automations-col-elapsed">Duration</span>
                </div>
                {visibleRecentSessions.map((session) => (
                  <div
                    key={session.session_id}
                    className={`automations-session-row ${onNavigateToSession ? 'clickable' : ''}`}
                    onClick={() => {
                      setSelectedSessionId(session.session_id);
                      onNavigateToSession?.(session.session_id);
                    }}
                    title={`View session ${truncateId(session.session_id)}`}
                  >
                    <span className="automations-session-id" title={session.session_id}>
                      {truncateId(session.session_id)}
                    </span>
                    <span className="automations-session-workflow">{session.workflow}</span>
                    <span className={`automations-status-badge ${session.status}`}>
                      <span className="automations-status-dot" />
                      <span>{session.status === 'exited' ? 'Exited' : 'Stopped'}</span>
                    </span>
                    <span className="automations-session-elapsed">{formatDuration(session.started_at)}</span>
                  </div>
                ))}
                {/* Pagination: show more if there are recent sessions past the visible window */}
                {recentOverflow > 0 && (
                  <button
                    type="button"
                    className="automations-load-more"
                    onClick={() => setVisibleRecent((n) => n + AUTOMATIONS_SESSIONS_INCREMENT)}
                  >
                    Show more ({recentOverflow} more)
                  </button>
                )}
              </div>
            )}
          </div>
        )}
      </div>

      {/* ── Session Detail Panel ──────────────────────────── */}
      {selectedSessionId && (
        <div className="automations-detail-overlay">
          <AutomationsSessionDetail sessionId={selectedSessionId} onClose={closeDetail} />
        </div>
      )}

      {/* ── Run Modal ─────────────────────────────────── */}
      {runModal.open && runModal.workflow && (
        <div className="automations-run-modal" role="dialog" aria-modal="true" aria-labelledby="run-modal-title">
          <div className="automations-modal-overlay" onClick={closeRunModal} />
          <div className="automations-modal-content">
            <div className="automations-modal-header">
              <h3 id="run-modal-title">Run Workflow</h3>
              <button className="automations-modal-close" onClick={closeRunModal} title="Close" aria-label="Close">
                <X size={16} />
              </button>
            </div>

            <div className="automations-modal-body">
              <div className="automations-modal-workflow-info">
                <div className="automations-modal-workflow-name">{runModal.workflow.name}</div>
                {runModal.workflow.description && (
                  <div className="automations-modal-workflow-desc">{runModal.workflow.description}</div>
                )}
              </div>

              <div className="automations-modal-fields">
                <label className="automations-field" htmlFor="automation-budget">
                  <span className="automations-field-label">Budget ($)</span>
                  <input
                    id="automation-budget"
                    type="number"
                    step="0.01"
                    min="0"
                    placeholder="No limit"
                    className="automations-input"
                    value={runModal.budgetUsd}
                    onChange={(e) => setRunModal((prev) => ({ ...prev, budgetUsd: e.target.value }))}
                  />
                  <span className="automations-field-hint">Optional — max spend cap in USD</span>
                </label>

                <label className="automations-field" htmlFor="automation-heartbeat">
                  <span className="automations-field-label">Heartbeat (seconds)</span>
                  <input
                    id="automation-heartbeat"
                    type="number"
                    step="1"
                    min="1"
                    placeholder="30"
                    className="automations-input"
                    value={runModal.heartbeat}
                    onChange={(e) => setRunModal((prev) => ({ ...prev, heartbeat: e.target.value }))}
                  />
                  <span className="automations-field-hint">Optional — heartbeat interval in seconds</span>
                </label>
              </div>
            </div>

            <div className="automations-modal-footer">
              <button className="automations-modal-cancel-btn" onClick={closeRunModal} disabled={isRunningWorkflow}>
                Cancel
              </button>
              <button className="automations-modal-run-btn" onClick={handleRunWorkflow} disabled={isRunningWorkflow}>
                {isRunningWorkflow ? (
                  'Starting...'
                ) : (
                  <>
                    <Zap size={14} />
                    <span>Run</span>
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default AutomationsPanel;
