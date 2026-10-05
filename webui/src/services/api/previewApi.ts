/**
 * Preview domain API — SP-155 §155a (TODO 155.6).
 *
 * Adapter-aware access to the dev-server lifecycle surface
 * (pkg/webui/api_preview.go):
 *   - getPreviewStatus: GET  /api/preview/status
 *   - startPreview:     POST /api/preview/start
 *   - restartPreview:   POST /api/preview/restart
 *   - stopPreview:      POST /api/preview/stop
 *
 * Each function takes a fetch function as its first parameter (the
 * adapter-aware transport convention) and returns a typed PreviewState. The
 * backend is trusted to return JSON, but every function parses defensively:
 * a non-JSON or empty body degrades to a well-formed (or a clear-error)
 * state rather than throwing a parser exception.
 */

import type { PreviewState, PreviewStatus } from './types/preview';

const STATUSES: PreviewStatus[] = ['starting', 'running', 'stopped', 'failed'];

/** Defensively parse a preview-state body into a well-formed PreviewState. */
function parseState(data: Record<string, unknown>): PreviewState {
  const rawStatus = String(data.status ?? '');
  const state: PreviewState = {
    // A missing/unknown status is treated as stopped (a safe, inert state).
    status: STATUSES.includes(rawStatus as PreviewStatus) ? (rawStatus as PreviewStatus) : 'stopped',
  };
  if (typeof data.url === 'string' && data.url !== '') state.url = data.url;
  if (typeof data.error === 'string' && data.error !== '') state.error = data.error;
  if (data.detected === true) state.detected = true;
  if (data.hosted === true) state.hosted = true;
  return state;
}

/** Read a response body, throwing the server's error message on !ok. */
async function readState(response: Response): Promise<PreviewState> {
  const data = (await response.json().catch(() => ({}))) as Record<string, unknown>;
  if (!response.ok) {
    const message = String(data.error ?? data.message ?? 'preview request failed');
    throw new Error(String(data.code ? `${data.code}: ${message}` : message));
  }
  return parseState(data);
}

/** GET /api/preview/status — the dev server's current lifecycle state. */
export async function getPreviewStatus(fetchFn: typeof fetch, signal?: AbortSignal): Promise<PreviewState> {
  // `signal` (optional) aborts an in-flight status poll: the pane's polling
  // loop passes one so a hung request never outlives the loop (when the
  // surface closes or the cadence switches). User actions never pass a
  // signal — they run to completion.
  const response = await fetchFn('/api/preview/status', { signal });
  return readState(response);
}

/**
 * POST /api/preview/start — detect an already-running dev server or start the
 * manifest's dev command. Resolves to the state the start settles into
 * (usually `starting`; readiness settles in the background and is tracked by
 * polling status). A project with no usable dev declaration is refused
 * (404 no_dev_command / 400 invalid_manifest), thrown as an Error.
 */
export async function startPreview(fetchFn: typeof fetch): Promise<PreviewState> {
  const response = await fetchFn('/api/preview/start', { method: 'POST' });
  return readState(response);
}

/**
 * POST /api/preview/restart — stop the manager's dev server and start it
 * again (a detected server is re-detected, not restarted).
 */
export async function restartPreview(fetchFn: typeof fetch): Promise<PreviewState> {
  const response = await fetchFn('/api/preview/restart', { method: 'POST' });
  return readState(response);
}

/** POST /api/preview/stop — stop the dev server the manager started. */
export async function stopPreview(fetchFn: typeof fetch): Promise<PreviewState> {
  const response = await fetchFn('/api/preview/stop', { method: 'POST' });
  return readState(response);
}
