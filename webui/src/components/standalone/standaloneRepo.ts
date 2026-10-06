/**
 * standaloneRepo — the standalone pages' slice of active-repo state.
 *
 * Mirrors the tiny part of services/activeRepo.ts the embedded pages
 * need: record the repo the host opened the page with, so escalation
 * workspace resolution has its URL. NOT the full app's module — that one
 * pulls React (useSyncExternalStore) and the recent-repos/repo-import
 * persistence chain into the standalone bundles; the pages have no repo
 * UI, so a session-scoped value plus localStorage mirror is everything.
 *
 * Kept compatible by contract: the full app's activeRepo reads the same
 * localStorage key (`sprout-last-repo`) via repoImportCache, so an
 * embedded editor and a later full-app session agree on "last repo".
 */

const LAST_REPO_KEY = 'sprout-last-repo';

let activeRepoURL: string | null = null;

/** The repo URL escalation resolves workspaces against (null when none). */
export function getStandaloneRepoURL(): string | null {
  return activeRepoURL;
}

/** Record the repo (session-scoped, mirrored to localStorage). */
export function setActiveRepoURL(url: string): void {
  const next = url.trim();
  if (!next || next === activeRepoURL) return;
  activeRepoURL = next;
  try {
    window.localStorage.setItem(LAST_REPO_KEY, next);
  } catch {
    // Storage unavailable (private mode): session-scoped only.
  }
}

/** Test hook. */
export function __resetStandaloneRepo(): void {
  activeRepoURL = null;
}
