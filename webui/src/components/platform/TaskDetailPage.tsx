import { ArrowLeft, Check, ChevronDown, ChevronRight, Copy, ExternalLink } from 'lucide-react';
import React, { useCallback, useEffect, useState } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import { useLog } from '../../utils/log';
import './PlatformPages.css';

// ---------------------------------------------------------------------------
// Types — mirror the platform's /task/{id} response shape.
// ---------------------------------------------------------------------------

type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'timeout' | 'cancelled' | 'canceled';

interface TaskDetail {
  task_id: string;
  status: TaskStatus;
  repo_url: string;
  prompt: string;
  provider?: string;
  model?: string;
  task_size?: 'standard' | 'power';
  result?: string; // JSON string with metrics, files_modified, etc.
  diff?: string;
  pr_url?: string;
  estimated_cost_cents?: number;
  created_at: string;
  updated_at: string;
}

interface TaskMetrics {
  elapsed_seconds?: number;
  tokens_in?: number;
  tokens_out?: number;
  llm_calls?: number;
  provider?: string;
  model?: string;
}

interface TaskResult {
  status?: string;
  stdout?: string;
  files_modified?: string[];
  metrics?: TaskMetrics;
  duration_ms?: number;
  error?: string;
}

export interface TaskDetailPageProps {
  /** The task ID to load. */
  taskId: string;
  /** Called when the user clicks the Back button / breadcrumb. */
  onBack: () => void;
}

// Statuses that are terminal — polling stops once a task reaches one.
const TERMINAL_STATUSES = new Set(['completed', 'failed', 'cancelled', 'canceled', 'timeout']);

/** Polling interval for refreshing an in-flight task's status. */
const POLL_INTERVAL_MS = 5000;

const TaskDetailPage: React.FC<TaskDetailPageProps> = ({ taskId, onBack }) => {
  const log = useLog();

  const [task, setTask] = useState<TaskDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [creatingPR, setCreatingPR] = useState(false);

  const result = task?.result ? parseTaskResult(task.result) : null;

  // load does the full initial fetch (task). On a fresh load we flip the
  // loading spinner; on subsequent polling refreshes we update task state
  // in place without the spinner (skipLoadingState).
  const load = useCallback(
    async (skipLoadingState = false) => {
      const adapter = getAdapter();
      if (!adapter) {
        setError('Not available - running in local mode');
        setLoading(false);
        return;
      }

      if (!skipLoadingState) setLoading(true);
      setError(null);

      try {
        const response = await adapter.fetch(`/task/${taskId}`);
        if (!response.ok) {
          throw new Error(`Failed to load task: ${response.status} ${response.statusText}`);
        }
        const data = (await response.json()) as TaskDetail;
        setTask(data);
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Failed to load task';
        setError(message);
        log.error(message, { title: 'Task Detail Error' });
      } finally {
        if (!skipLoadingState) setLoading(false);
      }
    },
    [taskId, log],
  );

  useEffect(() => {
    void load();
  }, [load]);

  // Poll the task while it is still in-flight (running/pending), and stop once
  // it reaches a terminal state. Depends on `task?.status` (not `task`) so the
  // interval isn't torn down and recreated on every polling tick.
  const taskStatus = task?.status;
  useEffect(() => {
    if (!taskStatus || !taskId) return;
    if (TERMINAL_STATUSES.has(taskStatus)) return;
    const interval = setInterval(() => {
      void load(true);
    }, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [taskStatus, taskId, load]);

  const handleCreatePR = async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available - running in local mode');
      return;
    }
    setCreatingPR(true);
    try {
      const response = await adapter.fetch(`/task/${taskId}/pr`, { method: 'POST' });
      if (!response.ok) {
        const errData = await response.json().catch(() => ({}));
        throw new Error(errData?.error || `Failed to create PR: ${response.status} ${response.statusText}`);
      }
      const data = (await response.json()) as { pr_url?: string };
      // Refresh the task so the "View PR" button appears immediately.
      await load(true);
      if (data?.pr_url) {
        log.success(`PR created: ${data.pr_url}`, { title: 'Pull Request' });
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create PR';
      log.error(message, { title: 'Create PR Error' });
    } finally {
      setCreatingPR(false);
    }
  };

  if (loading) {
    return <div className="platform-page-loading">Loading task…</div>;
  }

  if (error) {
    return (
      <div className="platform-page">
        <button
          className="platform-button platform-button-secondary platform-button-sm"
          onClick={onBack}
          style={{ marginBottom: '16px', display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <ArrowLeft size={14} /> Back to tasks
        </button>
        <div className="platform-page-error">
          <h3>Couldn't load task</h3>
          <p>{friendlyError(error)}</p>
          <button
            className="platform-button platform-button-secondary platform-button-sm"
            onClick={() => void load()}
            style={{ marginTop: '16px' }}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!task) return null;

  const slug = repoSlug(task.repo_url);

  return (
    <div className="platform-page">
      {/* Breadcrumb / back */}
      <div className="platform-page-header" style={{ marginBottom: '16px', paddingBottom: '12px' }}>
        <button
          onClick={onBack}
          className="platform-button platform-button-secondary platform-button-sm"
          style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <ArrowLeft size={14} /> Back to tasks
        </button>
      </div>

      {/* Task header */}
      <div className="platform-card" style={{ marginBottom: '16px' }}>
        <div className="platform-card-header">
          <div style={{ minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '6px' }}>
              <span className={`platform-status-badge ${normalizeStatusClass(task.status)}`}>{task.status}</span>
              {task.task_size === 'power' && <span className="platform-status-badge running">power</span>}
            </div>
            <h3 className="platform-card-title" style={{ wordBreak: 'break-word' }}>
              {task.prompt}
            </h3>
          </div>
        </div>
        <div className="platform-card-body" style={{ display: 'flex', flexWrap: 'wrap', gap: '8px 14px' }}>
          <span>{slug}</span>
          {task.model && <span>· {task.model}</span>}
          {task.updated_at && <span>· {timeAgo(task.updated_at)}</span>}
        </div>
      </div>

      {/* Action bar */}
      {task.pr_url && (
        <div style={{ marginBottom: '16px' }}>
          <a
            href={task.pr_url}
            target="_blank"
            rel="noopener noreferrer"
            className="platform-button platform-button-primary platform-button-sm"
            style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', textDecoration: 'none' }}
          >
            View PR <ExternalLink size={14} />
          </a>
        </div>
      )}
      {task.status === 'completed' && !task.pr_url && task.diff && (
        <div style={{ marginBottom: '16px' }}>
          <button
            className="platform-button platform-button-primary platform-button-sm"
            onClick={() => void handleCreatePR()}
            disabled={creatingPR}
            style={{ opacity: creatingPR ? 0.6 : 1, display: 'inline-flex', alignItems: 'center', gap: '6px' }}
          >
            {creatingPR ? 'Creating PR…' : 'Create PR'}
          </button>
        </div>
      )}

      {/* Metrics */}
      {result?.metrics && (
        <div className="platform-metric-grid" style={{ marginBottom: '16px' }}>
          {result.metrics.elapsed_seconds != null && (
            <Metric label="Duration" value={`${result.metrics.elapsed_seconds.toFixed(1)}s`} />
          )}
          {result.metrics.tokens_in != null && result.metrics.tokens_in > 0 && (
            <Metric label="Tokens In" value={result.metrics.tokens_in.toLocaleString()} />
          )}
          {result.metrics.tokens_out != null && result.metrics.tokens_out > 0 && (
            <Metric label="Tokens Out" value={result.metrics.tokens_out.toLocaleString()} />
          )}
          {result.metrics.llm_calls != null && <Metric label="LLM Calls" value={String(result.metrics.llm_calls)} />}
        </div>
      )}

      {/* Error display */}
      {task.status === 'failed' && result?.error && (
        <div
          style={{
            padding: '12px 16px',
            background: 'var(--bg-error, rgba(224, 108, 117, 0.12))',
            border: '1px solid var(--accent-error)',
            borderRadius: '8px',
            color: 'var(--accent-error)',
            fontSize: '14px',
            marginBottom: '16px',
          }}
        >
          <strong>Task failed:</strong> {friendlyError(result.error)}
        </div>
      )}

      {/* Files modified summary */}
      {result?.files_modified && result.files_modified.length > 0 && (
        <div className="platform-card" style={{ marginBottom: '16px' }}>
          <div className="platform-card-header">
            <h3 className="platform-card-title">
              {result.files_modified.length} file{result.files_modified.length !== 1 ? 's' : ''} modified
            </h3>
          </div>
          <div className="platform-card-body">
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
              {result.files_modified.map((f) => (
                <code
                  key={f}
                  style={{
                    padding: '3px 8px',
                    background: 'var(--bg-tertiary)',
                    border: '1px solid var(--border-color)',
                    borderRadius: '4px',
                    fontSize: '12px',
                    color: 'var(--text-secondary)',
                  }}
                >
                  {f}
                </code>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Diff viewer */}
      {task.diff ? (
        <DiffViewer raw={task.diff} />
      ) : (
        <div className="platform-page-empty" style={{ padding: '32px' }}>
          <h3>No diff for this task.</h3>
        </div>
      )}
    </div>
  );
};

// --- Sub-components ---

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="platform-metric-card">
      <div className="platform-metric-label">{label}</div>
      <div className="platform-metric-value">{value}</div>
    </div>
  );
}

function DiffViewer({ raw }: { raw: string }) {
  const parsed = React.useMemo(() => parseDiff(raw), [raw]);
  const [copied, setCopied] = useState(false);

  const copyDiff = async () => {
    try {
      await navigator.clipboard.writeText(raw);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard unavailable — no-op */
    }
  };

  if (parsed.files.length === 0) {
    return (
      <div className="platform-card">
        <div className="platform-card-header">
          <h3 className="platform-card-title">Raw diff</h3>
        </div>
        <pre
          style={{
            margin: 0,
            padding: '12px',
            background: 'var(--bg-tertiary)',
            borderRadius: '6px',
            fontFamily: 'monospace',
            fontSize: '12px',
            color: 'var(--text-secondary)',
            overflowX: 'auto',
            whiteSpace: 'pre-wrap',
          }}
        >
          {raw}
        </pre>
      </div>
    );
  }

  return (
    <div className="platform-card">
      <div className="platform-card-header">
        <h3 className="platform-card-title">Changes</h3>
        <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
          <span style={{ color: 'var(--accent-success)', fontSize: '13px', fontWeight: 600 }}>
            +{parsed.totalAdditions}
          </span>
          <span style={{ color: 'var(--accent-error)', fontSize: '13px', fontWeight: 600 }}>
            −{parsed.totalDeletions}
          </span>
          <button
            onClick={copyDiff}
            className="platform-button platform-button-secondary platform-button-sm"
            style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
          >
            {copied ? <Check size={14} /> : <Copy size={14} />}
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
      </div>
      <div className="platform-card-body" style={{ paddingTop: 0 }}>
        {parsed.files.map((file, i) => (
          <DiffFileBlock key={i} file={file} />
        ))}
      </div>
    </div>
  );
}

function DiffFileBlock({ file }: { file: DiffFile }) {
  const [collapsed, setCollapsed] = useState(file.collapsed ?? false);
  const isRenamed = file.oldPath !== file.newPath;
  const displayName = isRenamed ? `${shortPath(file.oldPath)} → ${shortPath(file.newPath)}` : shortPath(file.newPath);

  return (
    <div
      style={{
        marginBottom: '12px',
        border: '1px solid var(--border-color)',
        borderRadius: '6px',
        overflow: 'hidden',
        background: 'var(--bg-primary)',
      }}
    >
      <div
        onClick={() => setCollapsed(!collapsed)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          padding: '8px 12px',
          background: 'var(--bg-tertiary)',
          cursor: 'pointer',
          fontSize: '13px',
        }}
      >
        {collapsed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
        <span
          style={{
            fontFamily: 'monospace',
            color: 'var(--text-primary)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
          title={file.newPath}
        >
          {displayName}
        </span>
        <span style={{ marginLeft: 'auto', display: 'flex', gap: '10px', flexShrink: 0 }}>
          <span style={{ color: 'var(--accent-success)' }}>+{file.additions}</span>
          <span style={{ color: 'var(--accent-error)' }}>−{file.deletions}</span>
        </span>
      </div>
      {!collapsed && (
        <div style={{ fontFamily: 'monospace', fontSize: '12px', overflowX: 'auto' }}>
          {file.lines.map((line, i) => (
            <DiffLineRow key={i} line={line} />
          ))}
        </div>
      )}
    </div>
  );
}

function DiffLineRow({ line }: { line: DiffLine }) {
  let background = 'transparent';
  let color = 'var(--text-secondary)';
  let prefix = ' ';
  switch (line.type) {
    case 'add':
      background = 'var(--bg-success, rgba(152, 195, 121, 0.12))';
      color = 'var(--text-success, var(--accent-success))';
      prefix = '+';
      break;
    case 'del':
      background = 'var(--bg-error, rgba(224, 108, 117, 0.12))';
      color = 'var(--text-error, var(--accent-error))';
      prefix = '−';
      break;
    case 'hunk':
      color = 'var(--accent-info, #61afef)';
      prefix = '@';
      break;
    case 'meta':
      color = 'var(--text-muted)';
      break;
    default:
      break;
  }

  return (
    <div style={{ display: 'flex', background, padding: '0 12px' }}>
      <span style={{ color, width: '16px', flexShrink: 0, userSelect: 'none' }}>{prefix}</span>
      <span style={{ color, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{line.content}</span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

/** Map raw status strings to the CSS badge class names defined in PlatformPages.css. */
function normalizeStatusClass(status: TaskStatus): string {
  if (status === 'timeout') return 'failed';
  if (status === 'canceled') return 'cancelled';
  return status;
}

/** Extract "owner/name" from a GitHub URL for display. */
function repoSlug(url: string): string {
  try {
    const u = new URL(url);
    let path = u.pathname.replace(/\.git$/, '').replace(/^\//, '');
    if (path.endsWith('/')) path = path.slice(0, -1);
    return path;
  } catch {
    return url.split('/').slice(-2).join('/');
  }
}

/** Human-readable relative time: "just now", "5m ago", "3h ago", "2d ago". */
function timeAgo(iso: string): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  const seconds = Math.max(0, (Date.now() - t) / 1000);
  if (seconds < 60) return 'just now';
  const minutes = seconds / 60;
  if (minutes < 60) return `${Math.round(minutes)}m ago`;
  const hours = minutes / 60;
  if (hours < 24) return `${Math.round(hours)}h ago`;
  const days = hours / 24;
  if (days < 30) return `${Math.round(days)}d ago`;
  const months = days / 30;
  if (months < 12) return `${Math.round(months)}mo ago`;
  return `${Math.round(months / 12)}y ago`;
}

/** Parse the result JSON field into typed metrics. */
function parseTaskResult(resultJson?: string): TaskResult | null {
  if (!resultJson) return null;
  try {
    return JSON.parse(resultJson) as TaskResult;
  } catch {
    return null;
  }
}

/** Shorten a file path for display (trim long directories). */
function shortPath(path: string): string {
  const parts = path.split('/');
  if (parts.length <= 3) return path;
  return parts.slice(0, 2).join('/') + '/…/' + parts[parts.length - 1];
}

/** Translate common raw error strings into friendlier messages. */
function friendlyError(message: string): string {
  const lower = message.toLowerCase();
  if (lower.includes('github_reauth') || lower.includes('re-authenticate')) {
    return 'GitHub re-authentication is required to complete this action.';
  }
  if (lower.includes('unauthorized') || lower === 'unauthenticated') {
    return 'Your session has expired. Please reload to sign in again.';
  }
  if (lower.includes('context deadline exceeded') || lower.includes('timeout')) {
    return 'The operation timed out. Please try again.';
  }
  return message;
}

// ---------------------------------------------------------------------------
// Inline diff parser — structured per-file rendering for the diff viewer.
// (The editor's utils/diffParser produces whole-document pairs for the merge
// view, so we keep a compact structured parser here that matches this UI.)
// ---------------------------------------------------------------------------

type DiffLineType = 'add' | 'del' | 'context' | 'hunk' | 'meta';

interface DiffLine {
  type: DiffLineType;
  content: string;
}

interface DiffFile {
  oldPath: string;
  newPath: string;
  additions: number;
  deletions: number;
  lines: DiffLine[];
  collapsed: boolean;
}

interface ParsedDiff {
  files: DiffFile[];
  totalAdditions: number;
  totalDeletions: number;
}

const FILE_HEADER = /^diff --git a\/(.+?) b\/(.+)$/;
const HUNK_HEADER = /^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@/;

function parseDiff(diffText: string): ParsedDiff {
  const files: DiffFile[] = [];
  let currentFile: DiffFile | null = null;
  let totalAdditions = 0;
  let totalDeletions = 0;

  const lines = diffText.split('\n');

  for (const line of lines) {
    const fileMatch = line.match(FILE_HEADER);
    if (fileMatch) {
      if (currentFile) files.push(currentFile);
      currentFile = {
        oldPath: fileMatch[1],
        newPath: fileMatch[2],
        additions: 0,
        deletions: 0,
        lines: [],
        collapsed: false,
      };
      continue;
    }

    if (!currentFile) continue;

    const hunkMatch = line.match(HUNK_HEADER);
    if (hunkMatch) {
      currentFile.lines.push({ type: 'hunk', content: line });
      continue;
    }

    if (line.startsWith('+++') || line.startsWith('---')) continue;

    if (line.startsWith('+')) {
      currentFile.additions++;
      totalAdditions++;
      currentFile.lines.push({ type: 'add', content: line.slice(1) });
    } else if (line.startsWith('-')) {
      currentFile.deletions++;
      totalDeletions++;
      currentFile.lines.push({ type: 'del', content: line.slice(1) });
    } else if (line.startsWith(' ')) {
      currentFile.lines.push({ type: 'context', content: line.slice(1) });
    } else if (line.startsWith('\\ No newline')) {
      currentFile.lines.push({ type: 'meta', content: line });
    }
  }

  if (currentFile) files.push(currentFile);

  return { files, totalAdditions, totalDeletions };
}

export default TaskDetailPage;
