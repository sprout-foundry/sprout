/**
 * Storage scope for per-repository browser state (chat list, current
 * transcript, editor layout) in the hosted editor.
 *
 * Switching repositories reloads the editor onto ?repo=, so the scope is read
 * once from the URL and stays fixed for the page: everything saved during the
 * page lands under the repo it was opened for. Without ?repo= the scope is
 * empty and the unscoped keys are used, as before.
 */

function normalize(repoURL: string): string {
  let s = repoURL.trim().toLowerCase();
  const scp = /^git@([^:]+):(.+)$/.exec(s);
  if (scp) s = `${scp[1]}/${scp[2]}`;
  s = s.replace(/^[a-z][a-z0-9+.-]*:\/\//, '');
  s = s.replace(/^[^@/]+@/, '');
  return s.replace(/\/+$/, '').replace(/\.git$/, '');
}

function readScope(): string {
  if (typeof window === 'undefined') return '';
  const repo = new URLSearchParams(window.location.search).get('repo');
  return repo && repo.trim() ? normalize(repo) : '';
}

let scope = readScope();

/** The normalized repo this page was opened for ("host/owner/name"), or ''. */
export function repoScope(): string {
  return scope;
}

/** `base` suffixed with the repo scope, or `base` itself when unscoped. */
export function repoScopedKey(base: string): string {
  return scope ? `${base}@${scope}` : base;
}

export function __setRepoScopeForTests(repoURL: string | null): void {
  scope = repoURL ? normalize(repoURL) : '';
}
