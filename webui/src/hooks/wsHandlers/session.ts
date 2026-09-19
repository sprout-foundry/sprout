import type {
  CompactCompletedData,
  CompactStartedData,
  ContextManagementDiagnosticData,
  DriftDetectedData,
  MetricsUpdateData,
  ProviderNoCredentialData,
  RateLimitedData,
  WorkspaceChangedData,
} from '@sprout/events';
import { getWebUIClientId } from '../../services/clientSession';
import { notifyIfHidden } from '../../services/desktopNotify';
import { LSPClientService } from '../../services/lspClientService';
import { notificationBus } from '../../services/notificationBus';
import { debugLog } from '../../utils/log';
import { appendCappedLog } from '../../utils/logCap';
import { createLogEntry, type EventHandlerContext } from '../webSocketEventHelpers';

// Handle metrics_update event
export const handleMetricsUpdate = (ctx: EventHandlerContext): void => {
  const { event, setState, pendingProviderChangeRef, pendingProviderChangeValueRef } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as MetricsUpdateData;

  if (pendingProviderChangeRef.current && data.provider === pendingProviderChangeValueRef.current) {
    pendingProviderChangeRef.current = false;
    pendingProviderChangeValueRef.current = null;
  }

  setState((prev) => ({
    provider: String(data.provider || prev.provider),
    model: String(data.model || prev.model),
    stats: { ...prev.stats, ...data },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
};

// Handle workspace_changed event
//
// When the workspace root changes (e.g. switching to a git worktree via the
// Worktrees panel), we refresh workspace-dependent UI in place instead of
// doing a hard page reload.  A reload destroys all in-memory React state
// (chat messages, open files, terminal sessions) and in service mode the
// per-client server context is re-initialised from ws.workspaceRoot — which,
// combined with the old unconditional reload, caused the "lands in home
// directory" bug.
//
// The in-place refresh does three things:
//   1. Tears down cached LSP clients (their WebSocket URLs are keyed by the
//      old workspace root).
//   2. Clears recentFiles / recentLogs caches in React state so stale data
//      from the previous workspace doesn't linger.
//   3. Dispatches a `sprout:workspace-changed` DOM event so other components
//      (WorkspaceBar, FileBrowser, editor tabs, etc.) can re-fetch fresh data
//      from the server's new workspace root.
export const handleWorkspaceChanged = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const data = (event.data ?? {}) as WorkspaceChangedData;
  debugLog('[workspace] Workspace changed:', data);

  // Only react to events targeting this client (or broadcasts without a
  // client_id).
  if (data.client_id && String(data.client_id) !== getWebUIClientId()) {
    return;
  }

  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';

  // Tear down cached LSP clients whose connections reference the old workspace.
  try {
    LSPClientService.getInstance().cleanup();
  } catch (err) {
    debugLog('[workspace] LSP cleanup failed:', err);
  }

  // Clear workspace-derived caches so they re-fetch from the new root.
  setState((prev) => ({
    recentFiles: [],
    recentLogs: [],
    logs: appendCappedLog(prev.logs, logEntry),
  }));

  // Notify other components to refresh their workspace-dependent data.
  const workspaceRoot = (data as Record<string, unknown>).workspace_root;
  const daemonRoot = (data as Record<string, unknown>).daemon_root;
  window.dispatchEvent(
    new CustomEvent('sprout:workspace-changed', {
      detail: {
        workspaceRoot: typeof workspaceRoot === 'string' ? workspaceRoot : '',
        daemonRoot: typeof daemonRoot === 'string' ? daemonRoot : '',
      },
    }),
  );
};

/**
 * Handles drift_detected events: sets drift notification state so the
 * DriftNotification component can render a banner with action buttons.
 */
export const handleDriftDetected = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const data = (event.data ?? {}) as DriftDetectedData;
  debugLog('[drift] Drift detected:', data);

  const similarity = data.similarity ?? 0;
  const threshold = data.threshold ?? 0;
  const sessionId = data.sessionId ?? '';
  const options = data.options ?? [];

  setState((prev) => ({
    driftNotification: { similarity, threshold, sessionId, options },
  }));
};

/**
 * Handles context_management_diagnostic events: per-iteration telemetry
 * from the agent's context-management layer (cache hit rate, reserved
 * budget slices, iteration counter). The backend emits one of these per
 * OnIteration callback so SP-066's substitution/rollup behavior can be
 * observed live. We keep it out of the chat transcript and out of the
 * Logs pane — the structured Stats card is the right home — but still
 * log it at debug level so engineers can inspect from the console when
 * investigating context pressure.
 */
export const handleContextManagementDiagnostic = (ctx: EventHandlerContext): void => {
  const { event } = ctx;
  const data = (event.data ?? {}) as ContextManagementDiagnosticData;
  debugLog('[context_diag]', {
    iteration: data.iteration,
    currentTokens: data.current_tokens,
    maxTokens: data.max_tokens,
    cacheHitRate: data.cache_hit_rate,
    cachedTokens: data.cached_tokens,
    promptTokens: data.prompt_tokens,
    cacheWriteTokens: data.cache_write_tokens,
  });
};

// Handle provider_no_credential. The backend switched to a provider whose
// API key is missing; without this toast the failure is silent until the
// model errors out. The action jumps to Settings → Providers.
export const handleProviderNoCredential = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'error';
  const data = (event.data ?? {}) as ProviderNoCredentialData;
  setState((prev) => ({
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  notificationBus.notify(
    'error',
    'Provider credential missing',
    data.message || `Provider "${data.provider}" has no API key configured.`,
    8000,
    {
      label: 'Open settings',
      onClick: () => {
        window.dispatchEvent(new CustomEvent('sprout:open-settings-focus', { detail: { focus: 'provider' } }));
      },
    },
  );
};

// Handle rate_limited (approval broker backoff). Informs the user why the
// stream stalled and that a retry is scheduled — without it a mid-turn
// backoff looks like a hang.
export const handleRateLimited = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as RateLimitedData;
  setState((prev) => ({
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  const retrySecs = Math.max(1, Math.round((data.retry_after_ms ?? 0) / 1000));
  notificationBus.notify(
    'warning',
    'Rate limited',
    `${data.provider || 'Provider'} rate limit hit (attempt ${data.attempt ?? '?'}/${data.max_attempts ?? '?'}) — retrying in ~${retrySecs}s.`,
    Math.min(10000, retrySecs * 1000 + 2000),
  );
  notifyIfHidden('Sprout', 'Rate limited — retrying');
};

// Handle compact lifecycle events. Compaction rewrites the middle of the
// conversation; surfacing start/complete keeps the transcript gap from
// looking like lost messages. Failures are toasts, successes are log-only
// (the compaction summary itself arrives as a chat message).
export const handleCompactStarted = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as CompactStartedData;
  setState((prev) => ({
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[compact] started:', data.source, data.message_count, 'messages');
};

export const handleCompactCompleted = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  const data = (event.data ?? {}) as CompactCompletedData;
  if (data.success) {
    logEntry.level = 'info';
    setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
    return;
  }
  logEntry.level = 'error';
  setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  notificationBus.notify(
    'error',
    'Compaction failed',
    data.error || 'Context compaction did not complete; the conversation is unchanged.',
    8000,
  );
};

// Handle workspace_patch event (log-only; full VFS integration is a follow-up).
// Redact file contents from the log entry — workspace_patch carries the full
// written `content`, which may include secrets/PII and should not persist in
// React state. Only path/action/seq are safe to log.
export const handleWorkspacePatch = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const raw = (event.data ?? {}) as { path?: unknown; action?: unknown; seq?: unknown; content?: unknown };
  const logEntry = createLogEntry({
    ...event,
    data: { path: String(raw.path ?? ''), action: String(raw.action ?? ''), seq: raw.seq },
  });
  logEntry.category = 'file';
  logEntry.level = 'info';
  setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  debugLog('[workspace] Patch:', String(raw.path ?? ''));
};

// Handle recall_diagnostic event (log-only; structured diagnostics UI is a follow-up).
export const handleRecallDiagnostic = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as Record<string, unknown>;
  setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  debugLog('[recall] Diagnostic:', data);
};
