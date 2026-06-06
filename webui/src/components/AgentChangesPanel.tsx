/**
 * AgentChangesPanel — visualizes the ChangeTracker's session buffer.
 *
 * Shows what THIS agent has done this session, grouped into activity
 * blocks (≈ one block per turn). Each entry has actions:
 *   - View diff      — calls /api/changes/diff, shows unified diff
 *   - Revert file    — calls /api/changes/revert with file scope
 *   - Ask agent      — opens chat preloaded with a "what did you do
 *                      in this file?" prompt (parent's responsibility)
 *
 * Also exposes:
 *   - "Revert all" / "Revert since" bulk actions
 *   - Optional Timeline tab spanning previous sessions
 *   - Live updates via file_changed events (already published by the
 *     ChangeTracker; webSocket service delivers them).
 */

import {
  ChevronDown,
  ChevronRight,
  Eye,
  Undo2,
  MessageSquare,
  FileText,
  RotateCcw,
  Inbox,
  CircleAlert,
  Clock,
  FilePlus,
  FilePen,
  FileMinus,
  FolderCog,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { ApiService } from '../services/api';
import type {
  SessionChangeEntry,
  SessionSummaryBlock,
  SessionSummaryResponse,
  TimelineItem,
} from '../services/api/changesApi';
import { useLog } from '../utils/log';
import { showThemedConfirm } from './ThemedDialog';
import './AgentChangesPanel.css';

interface AgentChangesPanelProps {
  /**
   * Optional callback for "Ask agent about this change". When provided,
   * clicking the chat icon next to a file opens the chat with a
   * pre-filled prompt referencing that file. If omitted, the action
   * is hidden.
   */
  onAskAgent?: (filePath: string) => void;
  /** Optional callback for "View diff in editor" — opens the file
   *  using the host's normal file-click handler. */
  onFileClick?: (filePath: string) => void;
}

type Tab = 'session' | 'timeline';

const opIcon = (op: string) => {
  switch (op) {
    case 'create':
      return <FilePlus size={14} className="op-icon op-create" />;
    case 'delete':
      return <FileMinus size={14} className="op-icon op-delete" />;
    case 'bulk':
      return <FolderCog size={14} className="op-icon op-bulk" />;
    default:
      return <FilePen size={14} className="op-icon op-edit" />;
  }
};

const opLabel = (op: string) => {
  switch (op) {
    case 'create':
      return 'Created';
    case 'delete':
      return 'Deleted';
    case 'bulk':
      return 'Build output';
    case 'edit':
    default:
      return 'Modified';
  }
};

// formatBulkCount renders a thousands-separated file count for the
// build-output rollup row ("1,247 files"). Empty / undefined leaves the
// row label terse so the path remains the focal point.
function formatBulkCount(n?: number): string {
  if (!n || n <= 0) return '';
  return new Intl.NumberFormat(undefined).format(n) + ' file' + (n === 1 ? '' : 's');
}

function formatRelativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  const now = Date.now();
  const delta = Math.floor((now - then) / 1000);
  if (delta < 5) return 'just now';
  if (delta < 60) return `${delta}s ago`;
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
  return `${Math.floor(delta / 86400)}d ago`;
}

function AgentChangesPanel({ onAskAgent, onFileClick }: AgentChangesPanelProps): JSX.Element {
  const apiService = ApiService.getInstance();
  const log = useLog();

  const [tab, setTab] = useState<Tab>('session');

  // Session-tab state
  const [summary, setSummary] = useState<SessionSummaryResponse | null>(null);
  const [changes, setChanges] = useState<SessionChangeEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expandedBlocks, setExpandedBlocks] = useState<Set<number>>(new Set([0])); // newest expanded

  // Diff modal state
  const [diffOpen, setDiffOpen] = useState(false);
  const [diffPath, setDiffPath] = useState<string>('');
  const [diffText, setDiffText] = useState<string>('');
  const [diffLoading, setDiffLoading] = useState(false);

  // Timeline-tab state
  const [timelineSince, setTimelineSince] = useState<string>('7d');
  const [timelineItems, setTimelineItems] = useState<TimelineItem[]>([]);
  const [timelineLoading, setTimelineLoading] = useState(false);
  const [flashedPaths, setFlashedPaths] = useState<Set<string>>(new Set());

  // ── Load session ────────────────────────────────────────────────

  const loadSession = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [summaryRes, changesRes] = await Promise.all([
        apiService.getAgentSessionSummary(),
        apiService.getAgentSessionChanges(),
      ]);
      setSummary(summaryRes);
      setChanges(changesRes.files);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      log.error(`Failed to load agent changes: ${msg}`, { title: 'Changes Panel' });
    } finally {
      setLoading(false);
    }
  }, [apiService, log]);

  useEffect(() => {
    void loadSession();
  }, [loadSession]);

  // Live updates: when a file_changed event arrives, refresh + flash
  // Escape closes the diff viewer overlay while it's open. Window-level
  // listener because the overlay div doesn't carry focus.
  useEffect(() => {
    if (!diffOpen) return;
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDiffOpen(false);
    };
    window.addEventListener('keydown', handleEsc);
    return () => window.removeEventListener('keydown', handleEsc);
  }, [diffOpen]);

  // the changed file row. The websocket service surfaces events via
  // window-level CustomEvents (see useEventHandler) — we listen on
  // that channel rather than threading a prop through every parent.
  useEffect(() => {
    const handler = (ev: Event) => {
      const detail = (ev as CustomEvent).detail as { path?: string } | undefined;
      if (detail?.path) {
        setFlashedPaths((prev) => new Set(prev).add(detail.path!));
        // Clear the flash after the animation duration.
        setTimeout(() => {
          setFlashedPaths((prev) => {
            const next = new Set(prev);
            next.delete(detail.path!);
            return next;
          });
        }, 1200);
      }
      // Debounced reload — fire-and-forget; if multiple events arrive
      // in quick succession the new state still converges.
      void loadSession();
    };
    window.addEventListener('agent-file-changed', handler);
    return () => window.removeEventListener('agent-file-changed', handler);
  }, [loadSession]);

  // ── Diff modal ─────────────────────────────────────────────────

  const openDiff = useCallback(
    async (path: string) => {
      setDiffPath(path);
      setDiffOpen(true);
      setDiffLoading(true);
      setDiffText('');
      try {
        const res = await apiService.getAgentChangeDiff(path);
        if (!res.found) {
          setDiffText(`(no tracked change for ${path})`);
        } else {
          setDiffText(res.diff || '(empty diff)');
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setDiffText(`Error fetching diff: ${msg}`);
      } finally {
        setDiffLoading(false);
      }
    },
    [apiService],
  );

  // ── Revert actions ─────────────────────────────────────────────

  const revertOne = useCallback(
    async (path: string) => {
      const ok = await showThemedConfirm(
        `Restore ${path} to the state before the agent's first edit this session?`,
        {
          title: 'Revert this file?',
          confirmLabel: 'Revert',
          cancelLabel: 'Keep changes',
          type: 'warning',
        },
      );
      if (!ok) return;
      try {
        const res = await apiService.revertAgentChanges({ file: path });
        log.info(`Revert: ${res.summary}`, { title: 'Agent Changes' });
        await loadSession();
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        log.error(`Revert failed: ${msg}`, { title: 'Agent Changes' });
      }
    },
    [apiService, loadSession, log],
  );

  const revertAll = useCallback(async () => {
    const ok = await showThemedConfirm(
      `This will restore every file the agent touched in this session (${summary?.totals.files ?? 0} files) to its state before the session started. The user's own working-tree changes are NOT affected.`,
      {
        title: 'Revert all session changes?',
        confirmLabel: 'Revert all',
        cancelLabel: 'Cancel',
        type: 'danger',
      },
    );
    if (!ok) return;
    try {
      const res = await apiService.revertAgentChanges({ scope: 'all' });
      log.info(`Revert all: ${res.summary}`, { title: 'Agent Changes' });
      await loadSession();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log.error(`Revert failed: ${msg}`, { title: 'Agent Changes' });
    }
  }, [apiService, loadSession, log, summary]);

  // ── Timeline tab ───────────────────────────────────────────────

  const loadTimeline = useCallback(async () => {
    setTimelineLoading(true);
    try {
      const res = await apiService.getAgentChangesTimeline(timelineSince || undefined);
      setTimelineItems(res.files ?? res.items ?? []);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log.error(`Timeline load failed: ${msg}`, { title: 'Agent Changes' });
    } finally {
      setTimelineLoading(false);
    }
  }, [apiService, log, timelineSince]);

  useEffect(() => {
    if (tab === 'timeline') {
      void loadTimeline();
    }
  }, [tab, loadTimeline]);

  const toggleBlock = (idx: number) => {
    setExpandedBlocks((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  };

  // Renderer for activity blocks (session tab)
  const renderBlock = (block: SessionSummaryBlock, idx: number) => {
    const expanded = expandedBlocks.has(idx);
    const blockFiles = block.files;
    const toolList = Object.entries(block.tools)
      .map(([t, n]) => `${t} ×${n}`)
      .join(', ');
    return (
      <div key={idx} className="changes-block">
        <button
          type="button"
          className="changes-block-header"
          onClick={() => toggleBlock(idx)}
          aria-expanded={expanded}
        >
          {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          <span className="changes-block-title">
            Block {idx + 1} · {blockFiles.length} file{blockFiles.length === 1 ? '' : 's'}
          </span>
          <span className="changes-block-meta">
            <Clock size={12} /> {formatRelativeTime(block.started_at)}
          </span>
          <span className="changes-block-tools">{toolList}</span>
        </button>
        {expanded && (
          <div className="changes-block-body">
            {blockFiles.map((f) => {
              const meta = changes.find((c) => c.path === f.path);
              const flashed = flashedPaths.has(f.path);
              // Bulk rollups (SP-061-1): one row per build-output
              // directory, no view/revert/ask actions. The path field
              // already carries a trailing slash on the backend so the
              // user can see at a glance that this is a directory entry.
              if (f.op === 'bulk') {
                const count = meta?.bulk_count;
                return (
                  <div
                    key={f.path + f.op}
                    className={`changes-file-row changes-file-row--bulk ${flashed ? 'changes-file-flash' : ''}`}
                    title={`Build output rolled up — ${f.path} (${count ?? '?'} files). Not shown individually to keep the panel readable.`}
                  >
                    {opIcon(f.op)}
                    <span className="changes-file-path changes-file-path--bulk">{f.path}</span>
                    <span className="changes-file-op">
                      {opLabel(f.op)}
                      {count ? ` · ${formatBulkCount(count)}` : ''}
                    </span>
                  </div>
                );
              }
              return (
                <div key={f.path + f.op} className={`changes-file-row ${flashed ? 'changes-file-flash' : ''}`}>
                  {opIcon(f.op)}
                  <button
                    type="button"
                    className="changes-file-path"
                    onClick={() => onFileClick?.(f.path)}
                    title={f.path}
                  >
                    {f.path}
                  </button>
                  <span className="changes-file-op">{opLabel(f.op)}</span>
                  <div className="changes-file-actions">
                    <button
                      type="button"
                      className="changes-action-btn"
                      onClick={() => openDiff(f.path)}
                      title="View diff"
                      aria-label="View diff"
                    >
                      <Eye size={14} />
                    </button>
                    <button
                      type="button"
                      className="changes-action-btn"
                      onClick={() => revertOne(f.path)}
                      disabled={meta && !meta.recoverable}
                      title={meta && !meta.recoverable ? 'Original content not captured' : 'Revert this file'}
                      aria-label="Revert this file"
                    >
                      <Undo2 size={14} />
                    </button>
                    {onAskAgent && (
                      <button
                        type="button"
                        className="changes-action-btn"
                        onClick={() => onAskAgent(f.path)}
                        title="Ask the agent about this change"
                        aria-label="Ask the agent about this change"
                      >
                        <MessageSquare size={14} />
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    );
  };

  const isEmpty = !loading && (summary?.totals.changes ?? 0) === 0;

  // ── Render ─────────────────────────────────────────────────────

  return (
    <div className="agent-changes-panel">
      <div className="changes-tabs">
        <button
          type="button"
          className={`changes-tab ${tab === 'session' ? 'active' : ''}`}
          onClick={() => setTab('session')}
        >
          This session
          {summary && summary.totals.changes > 0 && (
            <span className="changes-tab-badge">{summary.totals.files}</span>
          )}
        </button>
        <button
          type="button"
          className={`changes-tab ${tab === 'timeline' ? 'active' : ''}`}
          onClick={() => setTab('timeline')}
        >
          Recent history
        </button>
        <div style={{ flex: 1 }} />
        <button
          type="button"
          className="changes-action-btn"
          onClick={() => void loadSession()}
          title="Refresh"
          aria-label="Refresh"
        >
          <RotateCcw size={14} />
        </button>
      </div>

      {error && (
        <div className="changes-error">
          <CircleAlert size={14} />
          {error}
        </div>
      )}

      {tab === 'session' && (
        <div className="changes-session">
          {loading && <div className="changes-loading">Loading…</div>}
          {isEmpty && (
            <div className="changes-empty">
              <Inbox size={32} />
              <p>The agent hasn't changed anything this session yet.</p>
              <p className="changes-empty-hint">
                When the agent edits, creates, or deletes files, each entry will
                have a <Eye size={12} aria-hidden="true" /> view-diff and{' '}
                <Undo2 size={12} aria-hidden="true" /> revert button.
              </p>
            </div>
          )}
          {!isEmpty && summary && (
            <>
              <div className="changes-totals">
                <FileText size={14} />
                <span>
                  {summary.totals.changes} change{summary.totals.changes === 1 ? '' : 's'} across{' '}
                  {summary.totals.files} file{summary.totals.files === 1 ? '' : 's'}, in{' '}
                  {summary.blocks.length} activity block{summary.blocks.length === 1 ? '' : 's'}
                </span>
                <div style={{ flex: 1 }} />
                <button
                  type="button"
                  className="changes-revert-all-btn"
                  onClick={() => void revertAll()}
                  title="Revert all changes the agent made this session"
                >
                  <Undo2 size={14} /> Revert all
                </button>
              </div>
              <div className="changes-blocks">
                {/* Newest blocks first */}
                {[...summary.blocks].reverse().map((block, idx) => renderBlock(block, idx))}
              </div>
            </>
          )}
        </div>
      )}

      {tab === 'timeline' && (
        <div className="changes-timeline">
          <div className="changes-timeline-controls">
            <label htmlFor="timeline-since">Since:</label>
            <select
              id="timeline-since"
              value={timelineSince}
              onChange={(e) => setTimelineSince(e.target.value)}
            >
              <option value="1d">Last 1 day</option>
              <option value="7d">Last 7 days</option>
              <option value="30d">Last 30 days</option>
              <option value="">All time</option>
            </select>
          </div>
          {timelineLoading && <div className="changes-loading">Loading…</div>}
          {!timelineLoading && timelineItems.length === 0 && (
            <div className="changes-empty">
              <Inbox size={32} />
              <p>No tracked changes in this time window.</p>
            </div>
          )}
          {timelineItems.length > 0 && (
            <div className="changes-timeline-list">
              {timelineItems.map((item, i) => (
                <div key={`${item.revision_id || 'session'}-${item.path}-${i}`} className="changes-timeline-row">
                  {opIcon(item.op)}
                  <button
                    type="button"
                    className="changes-file-path"
                    onClick={() => onFileClick?.(item.path)}
                    title={item.path}
                  >
                    {item.path}
                  </button>
                  <span className={`changes-timeline-source source-${item.source}`}>{item.source}</span>
                  {item.tier && <span className="changes-timeline-tier">{item.tier}</span>}
                  <span className="changes-timeline-time">{formatRelativeTime(item.timestamp)}</span>
                  <div className="changes-file-actions">
                    <button
                      type="button"
                      className="changes-action-btn"
                      onClick={() => openDiff(item.path)}
                      title="View diff"
                      aria-label="View diff"
                    >
                      <Eye size={14} />
                    </button>
                    <button
                      type="button"
                      className="changes-action-btn"
                      onClick={() => revertOne(item.path)}
                      title="Revert this file"
                      aria-label="Revert this file"
                    >
                      <Undo2 size={14} />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {diffOpen && (
        <div className="changes-diff-overlay" onClick={() => setDiffOpen(false)} role="presentation">
          <div
            className="changes-diff-modal"
            onClick={(e) => e.stopPropagation()}
            role="dialog"
            aria-modal="true"
            aria-label={`Diff for ${diffPath}`}
          >
            <div className="changes-diff-header">
              <FileText size={14} /> <span title={diffPath}>{diffPath}</span>
              <div style={{ flex: 1 }} />
              <button type="button" className="changes-action-btn" onClick={() => setDiffOpen(false)}>
                Close
              </button>
            </div>
            <pre className="changes-diff-body">
              {diffLoading ? 'Loading…' : diffText}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}

export default AgentChangesPanel;
