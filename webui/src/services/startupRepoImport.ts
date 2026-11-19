/**
 * The startup repository import for the in-browser agent's workspace.
 *
 * The cloud adapter can be installed from two places: the bootstrap (the
 * hosted build's entry, whose repo is the bootstrap's or the `?repo=` deep
 * link) and a mounted `SproutWorkspace` with a wasm backend (whose repo is the
 * host project's `repoUrl`). Both import through here, so:
 *
 * - a repo is imported once per page, not on every mount, host change or
 *   reconnect (a failed import may be retried by a later caller);
 * - a host project repo, once claimed by a mounted workspace, wins: the
 *   bootstrap's own startup repo is not imported over it.
 */

import type { CloudAdapter } from './cloudAdapter';

type RestoreResult = Awaited<ReturnType<CloudAdapter['restoreRepo']>>;

/** Repos whose import has started (or finished) on this page. */
const started = new Map<string, Promise<RestoreResult>>();

/** The repo a mounted workspace's host named, which outranks the bootstrap's. */
let hostProjectRepo: string | null = null;

/** The adapter the host project repo was last imported through, for a retry. */
let hostAdapter: Pick<CloudAdapter, 'restoreRepo'> | null = null;

/**
 * Where the host's project repo stands: opening, open, or failed with the
 * reason. Null when no mounted workspace names a project repo.
 */
export type HostProjectRepoStatus = {
  repo: string;
  state: 'opening' | 'open' | 'failed';
  error?: string;
} | null;

let hostStatus: HostProjectRepoStatus = null;
const hostStatusListeners = new Set<() => void>();

function setHostStatus(next: HostProjectRepoStatus): void {
  hostStatus = next;
  hostStatusListeners.forEach((listener) => listener());
}

/** The host project repo's status now. */
export function getHostProjectRepoStatus(): HostProjectRepoStatus {
  return hostStatus;
}

/** Follow the host project repo's status; returns the unsubscribe. */
export function subscribeHostProjectRepoStatus(listener: () => void): () => void {
  hostStatusListeners.add(listener);
  return () => {
    hostStatusListeners.delete(listener);
  };
}

/**
 * Record that a mounted workspace's host names this project repo. Called
 * synchronously before either adapter install finishes, so a bootstrap import
 * that lands afterwards sees the claim.
 */
export function claimHostProjectRepo(repoUrl: string): void {
  hostProjectRepo = repoUrl;
  if (hostStatus?.repo !== repoUrl) setHostStatus({ repo: repoUrl, state: 'opening' });
}

/** Release the host project repo claim (the workspace that made it unmounted). */
export function releaseHostProjectRepo(): void {
  hostProjectRepo = null;
  hostAdapter = null;
  setHostStatus(null);
}

/** Try opening the host project repo again after it failed. */
export function retryHostProjectRepo(): void {
  if (hostProjectRepo && hostAdapter) void importStartupRepo(hostAdapter, hostProjectRepo, 'host');
}

/**
 * Import a repo into the in-browser workspace through `adapter`, unless it is
 * already importing or imported. With `source: 'bootstrap'`, a different repo
 * claimed by a host project is left alone.
 *
 * Progress is published the way the app's file views expect: the
 * `__repoImporting` / `__repoImported` / `__repoImportFailed` window markers
 * and the `sprout:repo-imported` / `sprout:repo-import-failed` events. The
 * imported event's `history` says how git was set up (`clone`, `snapshot` or
 * `none`); clone progress arrives as `sprout:repo-import-progress` events.
 */
export function importStartupRepo(
  adapter: Pick<CloudAdapter, 'restoreRepo'>,
  repoUrl: string,
  source: 'host' | 'bootstrap',
): Promise<RestoreResult> | null {
  if (source === 'bootstrap' && hostProjectRepo && hostProjectRepo !== repoUrl) return null;
  if (source === 'host') hostAdapter = adapter;
  const forHost = () => hostStatus?.repo === repoUrl;
  const existing = started.get(repoUrl);
  if (existing) {
    // A remount claims the repo again: its status follows the import that
    // already ran rather than staying "opening".
    void existing.then((result) => {
      if (forHost())
        setHostStatus(
          result.success ? { repo: repoUrl, state: 'open' } : { repo: repoUrl, state: 'failed', error: result.error },
        );
    });
    return existing;
  }
  if (forHost()) setHostStatus({ repo: repoUrl, state: 'opening' });

  const markers = window as unknown as Record<string, unknown>;
  // Signal that an import is in progress (before the WASM shell is ready).
  markers.__repoImporting = repoUrl;
  const run = adapter.restoreRepo(repoUrl).then(
    (result) => {
      if (result.success) {
        if (forHost()) setHostStatus({ repo: repoUrl, state: 'open' });
        console.warn(
          `startup repo ${result.fromCache ? 'restored from cache' : 'imported'}: ${result.repo ?? repoUrl}`,
        );
        markers.__repoImported = result.repo ?? repoUrl;
        delete markers.__repoImporting;
        window.dispatchEvent(
          new CustomEvent('sprout:repo-imported', {
            detail: { repo: result.repo ?? repoUrl, history: result.history },
          }),
        );
      } else {
        importFailed(repoUrl, result.error ?? 'Unknown error');
        if (forHost()) setHostStatus({ repo: repoUrl, state: 'failed', error: result.error ?? 'Unknown error' });
      }
      return result;
    },
    (err: unknown) => {
      const error = err instanceof Error ? err.message : String(err);
      importFailed(repoUrl, error);
      if (forHost()) setHostStatus({ repo: repoUrl, state: 'failed', error });
      return { success: false, error };
    },
  );
  started.set(repoUrl, run);
  return run;
}

function importFailed(repoUrl: string, error: string): void {
  console.warn(`startup repo import failed: ${error}`);
  // A later caller may try again.
  started.delete(repoUrl);
  const markers = window as unknown as Record<string, unknown>;
  delete markers.__repoImporting;
  markers.__repoImportFailed = error;
  window.dispatchEvent(new CustomEvent('sprout:repo-import-failed', { detail: { error } }));
}

/** Forget every import and claim (tests only). */
export function __resetStartupRepoImportForTests(): void {
  started.clear();
  hostProjectRepo = null;
  hostAdapter = null;
  hostStatus = null;
  hostStatusListeners.clear();
}
