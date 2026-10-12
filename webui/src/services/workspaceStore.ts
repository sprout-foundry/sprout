/**
 * workspaceStore — the one client-side copy of GET /api/workspace.
 *
 * Every surface that shows or resolves against the workspace root (status
 * bar, location switcher, editor tabs, git panel, LSP, layout persistence)
 * reads it from here instead of issuing its own one-shot request. One-shot
 * fetches went stale independently: a response that raced a workspace switch
 * could land after the switch and overwrite the newer root, and a fetch that
 * failed during a reconnect left that surface blank for the rest of the
 * session.
 *
 * Guarantees:
 *   - At most one GET in flight; concurrent callers share it.
 *   - Responses are sequenced: a response that started before a newer
 *     request or an explicit apply is dropped instead of overwriting newer
 *     state.
 *   - The last known workspace survives errors and disconnects; failures are
 *     retried with backoff.
 *   - Refreshes on `sprout:workspace-changed`, on window focus when stale, and
 *     whenever a caller asks.
 */

import { useEffect, useSyncExternalStore } from 'react';
import { debugLog } from '../utils/log';
import { ApiService } from './api';
import type { WorkspaceResponse } from './api/types';
import { setWorkspaceCwdScope } from './workspaceCwd';

export interface WorkspaceStoreState {
  /** Last successfully fetched workspace; kept across errors and disconnects. */
  workspace: WorkspaceResponse | null;
  loading: boolean;
  /** Message of the most recent failed fetch; cleared by the next success. */
  error: string | null;
  /** Date.now() of the last successful fetch or apply (0 = never). */
  updatedAt: number;
}

/** A workspace older than this is refetched on focus or a fresh subscriber. */
export const WORKSPACE_STALE_AFTER_MS = 30_000;
const RETRY_BASE_MS = 1_000;
const RETRY_MAX_MS = 30_000;
const RETRY_MAX_ATTEMPTS = 8;

const INITIAL_STATE: WorkspaceStoreState = { workspace: null, loading: false, error: null, updatedAt: 0 };

let state: WorkspaceStoreState = INITIAL_STATE;
let sequence = 0;
let inflight: { seq: number; promise: Promise<WorkspaceResponse> } | null = null;
let retryTimer: ReturnType<typeof setTimeout> | null = null;
let retryAttempt = 0;
/** Sequence of the refetch started by the most recent applyWorkspaceRoot. */
let lastApplySeq = -1;
let windowListenersInstalled = false;
const listeners = new Set<() => void>();

function emit(next: Partial<WorkspaceStoreState>): void {
  state = { ...state, ...next };
  for (const listener of listeners) {
    try {
      listener();
    } catch (err) {
      debugLog('[workspaceStore] listener error (swallowed):', err);
    }
  }
}

function clearRetry(): void {
  if (retryTimer !== null) {
    clearTimeout(retryTimer);
    retryTimer = null;
  }
}

function scheduleRetry(): void {
  if (retryTimer !== null || retryAttempt >= RETRY_MAX_ATTEMPTS) return;
  const delay = Math.min(RETRY_BASE_MS * 2 ** retryAttempt, RETRY_MAX_MS);
  retryAttempt += 1;
  retryTimer = setTimeout(() => {
    retryTimer = null;
    refreshWorkspace().catch(() => {
      // best-effort: the failure is recorded in state and retried again.
    });
  }, delay);
}

function accept(workspace: WorkspaceResponse): void {
  clearRetry();
  retryAttempt = 0;
  emit({ workspace, loading: false, error: null, updatedAt: Date.now() });
  setWorkspaceCwdScope(workspace.workspace_root);
}

export function getWorkspaceState(): WorkspaceStoreState {
  return state;
}

/** The last known workspace root ('' until the first successful fetch). */
export function getWorkspaceRoot(): string {
  return state.workspace?.workspace_root ?? '';
}

export function subscribeWorkspace(listener: () => void): () => void {
  installWindowListeners();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Fetch the workspace. Joins the request already in flight unless `force`
 * is set, in which case a new request supersedes it (the older response is
 * then discarded). Resolves with the newest accepted workspace.
 */
export function refreshWorkspace(options: { force?: boolean } = {}): Promise<WorkspaceResponse> {
  installWindowListeners();
  if (inflight && !options.force) return inflight.promise;

  const seq = ++sequence;
  if (!state.loading) emit({ loading: true });

  const promise = ApiService.getInstance()
    .getWorkspace()
    .then(
      (workspace) => {
        if (seq !== sequence) return latestOr(workspace);
        inflight = null;
        accept(workspace);
        return workspace;
      },
      (err: unknown) => {
        if (seq !== sequence) {
          if (state.workspace) return latestOr(state.workspace);
          throw err;
        }
        inflight = null;
        emit({ loading: false, error: err instanceof Error ? err.message : String(err) });
        scheduleRetry();
        throw err;
      },
    );
  inflight = { seq, promise };
  return promise;
}

/** A superseded request hands its caller the newer result instead. */
function latestOr(fallback: WorkspaceResponse): Promise<WorkspaceResponse> | WorkspaceResponse {
  if (inflight) return inflight.promise;
  return state.workspace ?? fallback;
}

/**
 * The cached workspace when it is fresher than `maxAgeMs`, otherwise a
 * (shared) refresh. For callers that need the root once, e.g. before a
 * request.
 */
export function ensureWorkspace(maxAgeMs = WORKSPACE_STALE_AFTER_MS): Promise<WorkspaceResponse> {
  if (state.workspace && !inflight && Date.now() - state.updatedAt < maxAgeMs) {
    return Promise.resolve(state.workspace);
  }
  return refreshWorkspace();
}

/**
 * Record a workspace root the server just confirmed (a successful POST
 * /api/workspace or a workspace_changed event) and refetch the full
 * workspace metadata. Any GET started before this point is discarded: it
 * may describe the previous root.
 */
export function applyWorkspaceRoot(workspaceRoot: string, daemonRoot?: string): Promise<WorkspaceResponse | null> {
  installWindowListeners();
  const root = workspaceRoot.trim();
  if (!root) return refreshWorkspace({ force: true }).catch(() => null);
  // The same root applied twice in a row (the caller, then the DOM event it
  // announced) shares the refetch the first apply started.
  if (inflight && state.workspace?.workspace_root === root && inflight.seq === lastApplySeq) {
    return inflight.promise.catch(() => null);
  }
  sequence += 1;
  inflight = null;
  const base: WorkspaceResponse = state.workspace ?? {
    daemon_root: '',
    workspace_root: '',
    is_project: false,
    project_markers: [],
    needs_workspace_selection: false,
    workspace_is_home: false,
    home_dir: '',
    suggested_projects: [],
    recent_workspaces: [],
  };
  if (base.workspace_root !== root || (daemonRoot && base.daemon_root !== daemonRoot)) {
    accept({ ...base, workspace_root: root, daemon_root: daemonRoot || base.daemon_root });
  }
  const refreshed = refreshWorkspace({ force: true });
  lastApplySeq = sequence;
  return refreshed.catch(() => null);
}

/**
 * Tell every workspace-dependent surface that the root changed. The same DOM
 * event the server's workspace_changed event produces, for changes the client
 * made itself before the event stream is up (boot restore, reconnect).
 */
export function announceWorkspaceChanged(workspaceRoot: string, daemonRoot = ''): void {
  if (typeof window === 'undefined') return;
  window.dispatchEvent(new CustomEvent('sprout:workspace-changed', { detail: { workspaceRoot, daemonRoot } }));
}

function installWindowListeners(): void {
  if (windowListenersInstalled || typeof window === 'undefined') return;
  windowListenersInstalled = true;
  window.addEventListener('sprout:workspace-changed', onWorkspaceChangedEvent);
  window.addEventListener('focus', refreshIfStale);
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', onVisibilityChange);
  }
}

function onWorkspaceChangedEvent(event: Event): void {
  const detail = (event as CustomEvent<{ workspaceRoot?: string; daemonRoot?: string }>).detail;
  const root = typeof detail?.workspaceRoot === 'string' ? detail.workspaceRoot : '';
  const daemonRoot = typeof detail?.daemonRoot === 'string' ? detail.daemonRoot : '';
  void applyWorkspaceRoot(root, daemonRoot);
}

function onVisibilityChange(): void {
  if (document.visibilityState === 'visible') refreshIfStale();
}

function refreshIfStale(): void {
  if (inflight) return;
  if (state.workspace && Date.now() - state.updatedAt < WORKSPACE_STALE_AFTER_MS) return;
  refreshWorkspace().catch(() => {
    // best-effort: recorded in state and retried.
  });
}

/**
 * Reactive workspace state. Fetches on mount when nothing is known yet or the
 * snapshot is stale; otherwise reads the shared copy.
 */
export function useWorkspaceState(): WorkspaceStoreState {
  const snapshot = useSyncExternalStore(subscribeWorkspace, getWorkspaceState, getWorkspaceState);
  useEffect(() => {
    refreshIfStale();
  }, []);
  return snapshot;
}

/** Reactive workspace root ('' until known; the last known root while disconnected). */
export function useWorkspaceRoot(): string {
  return useWorkspaceState().workspace?.workspace_root ?? '';
}

export function __resetWorkspaceStoreForTests(): void {
  clearRetry();
  state = INITIAL_STATE;
  sequence = 0;
  inflight = null;
  retryAttempt = 0;
  lastApplySeq = -1;
  listeners.clear();
  if (windowListenersInstalled && typeof window !== 'undefined') {
    window.removeEventListener('sprout:workspace-changed', onWorkspaceChangedEvent);
    window.removeEventListener('focus', refreshIfStale);
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisibilityChange);
  }
  windowListenersInstalled = false;
}

// The shared vitest setup resets the store between tests through this hook
// rather than importing the module, which would bind it before a test file's
// vi.mock of the API service could apply.
if (typeof globalThis !== 'undefined') {
  (globalThis as { __resetWorkspaceStoreForTests?: () => void }).__resetWorkspaceStoreForTests =
    __resetWorkspaceStoreForTests;
}
