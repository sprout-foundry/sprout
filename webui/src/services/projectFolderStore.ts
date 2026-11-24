/**
 * projectFolderStore — the one client-side copy of GET /api/workspace.
 *
 * Every surface that shows or resolves against the project folder (status
 * bar, location switcher, editor tabs, git panel, LSP, layout persistence)
 * reads it from here instead of issuing its own one-shot request. One-shot
 * fetches went stale independently: a response that raced a project folder switch
 * could land after the switch and overwrite the newer root, and a fetch that
 * failed during a reconnect left that surface blank for the rest of the
 * session.
 *
 * Guarantees:
 *   - At most one GET in flight; concurrent callers share it.
 *   - Responses are sequenced: a response that started before a newer
 *     request or an explicit apply is dropped instead of overwriting newer
 *     state.
 *   - The last known project folder survives errors and disconnects; failures are
 *     retried with backoff.
 *   - Refreshes on `sprout:workspace-changed`, on window focus when stale, and
 *     whenever a caller asks.
 */

import { useEffect, useSyncExternalStore } from 'react';
import { debugLog } from '../utils/log';
import { ApiService } from './api';
import type { ProjectFolderResponse } from './api/types';
import { setProjectFolderCwdScope } from './projectFolderCwd';

export interface ProjectFolderStoreState {
  /** Last successfully fetched project folder; kept across errors and disconnects. */
  projectFolder: ProjectFolderResponse | null;
  loading: boolean;
  /** Message of the most recent failed fetch; cleared by the next success. */
  error: string | null;
  /** Date.now() of the last successful fetch or apply (0 = never). */
  updatedAt: number;
}

/** A project folder older than this is refetched on focus or a fresh subscriber. */
export const WORKSPACE_STALE_AFTER_MS = 30_000;
const RETRY_BASE_MS = 1_000;
const RETRY_MAX_MS = 30_000;
const RETRY_MAX_ATTEMPTS = 8;

const INITIAL_STATE: ProjectFolderStoreState = { projectFolder: null, loading: false, error: null, updatedAt: 0 };

let state: ProjectFolderStoreState = INITIAL_STATE;
let sequence = 0;
let inflight: { seq: number; promise: Promise<ProjectFolderResponse> } | null = null;
let retryTimer: ReturnType<typeof setTimeout> | null = null;
let retryAttempt = 0;
/** Sequence of the refetch started by the most recent applyProjectFolder. */
let lastApplySeq = -1;
let windowListenersInstalled = false;
const listeners = new Set<() => void>();

function emit(next: Partial<ProjectFolderStoreState>): void {
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
    refreshProjectFolder().catch(() => {
      // best-effort: the failure is recorded in state and retried again.
    });
  }, delay);
}

function accept(projectFolder: ProjectFolderResponse): void {
  clearRetry();
  retryAttempt = 0;
  emit({ projectFolder: projectFolder, loading: false, error: null, updatedAt: Date.now() });
  setProjectFolderCwdScope(projectFolder.project_folder);
}

export function getProjectFolderState(): ProjectFolderStoreState {
  return state;
}

/** The last known project folder ('' until the first successful fetch). */
export function getProjectFolder(): string {
  return state.projectFolder?.project_folder ?? '';
}

export function subscribeProjectFolder(listener: () => void): () => void {
  installWindowListeners();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Fetch the project folder. Joins the request already in flight unless `force`
 * is set, in which case a new request supersedes it (the older response is
 * then discarded). Resolves with the newest accepted project folder.
 */
export function refreshProjectFolder(options: { force?: boolean } = {}): Promise<ProjectFolderResponse> {
  installWindowListeners();
  if (inflight && !options.force) return inflight.promise;

  const seq = ++sequence;
  if (!state.loading) emit({ loading: true });

  const promise = ApiService.getInstance()
    .getProjectFolder()
    .then(
      (projectFolder) => {
        if (seq !== sequence) return latestOr(projectFolder);
        inflight = null;
        accept(projectFolder);
        return projectFolder;
      },
      (err: unknown) => {
        if (seq !== sequence) {
          if (state.projectFolder) return latestOr(state.projectFolder);
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
function latestOr(fallback: ProjectFolderResponse): Promise<ProjectFolderResponse> | ProjectFolderResponse {
  if (inflight) return inflight.promise;
  return state.projectFolder ?? fallback;
}

/**
 * The cached project folder when it is fresher than `maxAgeMs`, otherwise a
 * (shared) refresh. For callers that need the root once, e.g. before a
 * request.
 */
export function ensureProjectFolder(maxAgeMs = WORKSPACE_STALE_AFTER_MS): Promise<ProjectFolderResponse> {
  if (state.projectFolder && !inflight && Date.now() - state.updatedAt < maxAgeMs) {
    return Promise.resolve(state.projectFolder);
  }
  return refreshProjectFolder();
}

/**
 * Record a project folder the server just confirmed (a successful POST
 * /api/project-folder or a project_folder_changed event) and refetch the full
 * project folder metadata. Any GET started before this point is discarded: it
 * may describe the previous root.
 */
export function applyProjectFolder(projectFolder: string, daemonRoot?: string): Promise<ProjectFolderResponse | null> {
  installWindowListeners();
  const root = projectFolder.trim();
  if (!root) return refreshProjectFolder({ force: true }).catch(() => null);
  // The same root applied twice in a row (the caller, then the DOM event it
  // announced) shares the refetch the first apply started.
  if (inflight && state.projectFolder?.project_folder === root && inflight.seq === lastApplySeq) {
    return inflight.promise.catch(() => null);
  }
  sequence += 1;
  inflight = null;
  const base: ProjectFolderResponse = state.projectFolder ?? {
    daemon_root: '',
    project_folder: '',
    is_project: false,
    project_markers: [],
    needs_project_folder_selection: false,
    project_folder_is_home: false,
    home_dir: '',
    suggested_projects: [],
    recent_project_folders: [],
  };
  if (base.project_folder !== root || (daemonRoot && base.daemon_root !== daemonRoot)) {
    accept({ ...base, project_folder: root, daemon_root: daemonRoot || base.daemon_root });
  }
  const refreshed = refreshProjectFolder({ force: true });
  lastApplySeq = sequence;
  return refreshed.catch(() => null);
}

/**
 * Tell every workspace-dependent surface that the root changed. The same DOM
 * event the server's project_folder_changed event produces, for changes the client
 * made itself before the event stream is up (boot restore, reconnect).
 */
export function announceProjectFolderChanged(projectFolder: string, daemonRoot = ''): void {
  if (typeof window === 'undefined') return;
  window.dispatchEvent(
    new CustomEvent('sprout:workspace-changed', { detail: { projectFolder: projectFolder, daemonRoot } }),
  );
}

function installWindowListeners(): void {
  if (windowListenersInstalled || typeof window === 'undefined') return;
  windowListenersInstalled = true;
  window.addEventListener('sprout:workspace-changed', onProjectFolderChangedEvent);
  window.addEventListener('focus', refreshIfStale);
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', onVisibilityChange);
  }
}

function onProjectFolderChangedEvent(event: Event): void {
  const detail = (event as CustomEvent<{ projectFolder?: string; daemonRoot?: string }>).detail;
  const root = typeof detail?.projectFolder === 'string' ? detail.projectFolder : '';
  const daemonRoot = typeof detail?.daemonRoot === 'string' ? detail.daemonRoot : '';
  void applyProjectFolder(root, daemonRoot);
}

function onVisibilityChange(): void {
  if (document.visibilityState === 'visible') refreshIfStale();
}

function refreshIfStale(): void {
  if (inflight) return;
  if (state.projectFolder && Date.now() - state.updatedAt < WORKSPACE_STALE_AFTER_MS) return;
  refreshProjectFolder().catch(() => {
    // best-effort: recorded in state and retried.
  });
}

/**
 * Reactive project folder state. Fetches on mount when nothing is known yet or the
 * snapshot is stale; otherwise reads the shared copy.
 */
export function useProjectFolderState(): ProjectFolderStoreState {
  const snapshot = useSyncExternalStore(subscribeProjectFolder, getProjectFolderState, getProjectFolderState);
  useEffect(() => {
    refreshIfStale();
  }, []);
  return snapshot;
}

/** Reactive project folder ('' until known; the last known root while disconnected). */
export function useProjectFolder(): string {
  return useProjectFolderState().projectFolder?.project_folder ?? '';
}

export function __resetProjectFolderStoreForTests(): void {
  clearRetry();
  state = INITIAL_STATE;
  sequence = 0;
  inflight = null;
  retryAttempt = 0;
  lastApplySeq = -1;
  listeners.clear();
  if (windowListenersInstalled && typeof window !== 'undefined') {
    window.removeEventListener('sprout:workspace-changed', onProjectFolderChangedEvent);
    window.removeEventListener('focus', refreshIfStale);
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisibilityChange);
  }
  windowListenersInstalled = false;
}

// The shared vitest setup resets the store between tests through this hook
// rather than importing the module, which would bind it before a test file's
// vi.mock of the API service could apply.
if (typeof globalThis !== 'undefined') {
  (globalThis as { __resetProjectFolderStoreForTests?: () => void }).__resetProjectFolderStoreForTests =
    __resetProjectFolderStoreForTests;
}
