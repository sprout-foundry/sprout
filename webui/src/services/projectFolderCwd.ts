/**
 * projectFolderCwd — session-level working directory for the studio webui.
 *
 * Sprout Studio mobile apps embed this webui with cloned GitHub repos living
 * at `repos/<owner>/<name>/` in the project folder (see services/projectFolderFs/
 * projectFolderGit.ts). Every surface — Files tree, native terminal, git panel,
 * agent — is rooted at the project folder top today. This module is the single
 * source of truth for a session-level working directory so selecting a repo
 * scopes all of them to `repos/<owner>/<name>` at once.
 *
 * Shape: a tiny external store (subscribe/get/set, like the project's other
 * services — see automateEvents.ts) holding `{ cwd }` where `cwd` is a
 * WORKSPACE-RELATIVE directory path and `''` means the project folder.
 * Consumers:
 *   - SidebarFilesSection  → roots the file tree at cwd, repo selector chip.
 *   - NativeTerminalConsole → passes cwd to each `terminalSpawn` (one-shot
 *     semantics: every spawn uses the cwd current at submit time) and shows
 *     the last path segment in the prompt.
 *   - gitApi / useGitProjectFolder / useGitHandlers → target the repo at cwd.
 *   - Chat send path → appends a one-line working-directory context note.
 *
 * Persistence: localStorage key `sprout-workspace-cwd:<project folder>`
 * (plain string, not JSON — the value is already an unambiguous path or '').
 * The cwd is relative to one project folder, so it is stored per project folder:
 * a single shared key carried a repo subdirectory chosen in one project into
 * every other project (and every other window). Until the root is known
 * (setProjectFolderCwdScope) the unscoped key is used. Storage failures (private
 * mode, quota) degrade to an in-memory store, never throw.
 *
 * Default build: cwd starts `''` (project folder), so every consumer's
 * "cwd active" branch is a no-op and behavior is identical to before.
 */

import { useSyncExternalStore } from 'react';
import { debugLog } from '../utils/log';

/** localStorage key prefix holding the workspace-relative cwd ('' = project folder). */
export const WORKSPACE_CWD_STORAGE_KEY = 'sprout-workspace-cwd';

/** The storage key for the cwd within `projectFolder` ('' = root not known yet). */
export function projectFolderCwdStorageKey(projectFolder: string): string {
  return projectFolder ? `${WORKSPACE_CWD_STORAGE_KEY}:${projectFolder}` : WORKSPACE_CWD_STORAGE_KEY;
}

/** The store's state. Kept as an object so future fields can be added. */
export interface ProjectFolderCwdState {
  cwd: string;
}

// ── Validation / normalization (pure) ───────────────────────────────────────

/**
 * Normalize a candidate working directory to the canonical workspace-relative
 * form: leading/trailing slashes stripped, `.`/empty segments dropped,
 * duplicate slashes collapsed.
 *
 * Returns `''` for the project folder (including for inputs like `'/'`,
 * `'.'`, and `''`). Returns `null` when the input escapes the project folder —
 * any `..` segment (before OR after normalization) is a hard reject: callers
 * must never be able to point Files/Terminal/Git/Agent above the project folder.
 * Non-string input is also `null`.
 *
 * Pure and synchronous; safe to call anywhere (no window access).
 */
export function normalizeProjectFolderCwd(input: string): string | null {
  if (typeof input !== 'string') return null;
  const segments = input.split('/').filter((s) => s.length > 0 && s !== '.');
  if (segments.includes('..')) return null;
  return segments.join('/');
}

// ── Store (module-level singleton) ──────────────────────────────────────────

type CwdListener = (cwd: string) => void;
const listeners = new Set<CwdListener>();

/** The project folder the cwd is relative to ('' until known). */
let scope = '';

/** Safely read the persisted cwd; '' on any failure (also outside browsers). */
function readPersistedCwd(): string {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return '';
    const raw = window.localStorage.getItem(projectFolderCwdStorageKey(scope));
    if (!raw) return '';
    const normalized = normalizeProjectFolderCwd(raw);
    return normalized ?? '';
  } catch {
    // best-effort: unreadable storage reads as the project folder.
    return '';
  }
}

/** Safely persist the cwd; storage failures are ignored (in-memory only). */
function persistCwd(cwd: string): void {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return;
    window.localStorage.setItem(projectFolderCwdStorageKey(scope), cwd);
  } catch (err) {
    // Private mode / quota: the session still works, just not across reloads.
    debugLog('[projectFolderCwd] failed to persist cwd:', err);
  }
}

/**
 * Lazily-initialized state. Reading localStorage at module scope would run in
 * every test/SSR import; deferring to first access keeps imports side-effect
 * free and lets tests reset cleanly via `__resetProjectFolderCwdForTests()`.
 */
let state: ProjectFolderCwdState | null = null;

function getState(): ProjectFolderCwdState {
  if (state === null) {
    state = { cwd: readPersistedCwd() };
  }
  return state;
}

/**
 * The current workspace-relative working directory; `''` = project folder.
 * Never throws. Reads the persisted value on first call.
 */
export function getProjectFolderCwd(): string {
  return getState().cwd;
}

/**
 * Set the working directory. The input is normalized (slashes stripped,
 * `.` segments dropped); a `..` traversal (or non-string) is rejected and the
 * current cwd is left untouched — the caller gets `false` back.
 *
 * Setting the already-current value is a no-op (no persist, no notify). On a
 * real change the new value is persisted and every subscriber is notified
 * (listener errors are swallowed so one broken subscriber cannot block the
 * rest, mirroring automateEvents.ts).
 *
 * @returns `true` when the cwd changed, `false` when rejected or unchanged.
 */
export function setProjectFolderCwd(dir: string): boolean {
  const normalized = normalizeProjectFolderCwd(dir);
  if (normalized === null) return false;
  const current = getState();
  if (current.cwd === normalized) return false;
  state = { cwd: normalized };
  persistCwd(normalized);
  notify(normalized);
  return true;
}

function notify(cwd: string): void {
  for (const listener of listeners) {
    try {
      listener(cwd);
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error('[projectFolderCwd] Listener error (swallowed):', err);
    }
  }
}

/**
 * Point the store at the project folder the cwd is relative to. A different root
 * loads that project folder's own persisted cwd (the root when it has none) and
 * notifies subscribers if the effective cwd changed.
 */
export function setProjectFolderCwdScope(projectFolder: string): void {
  const next = (projectFolder || '').trim();
  if (next === scope) return;
  const previous = getState().cwd;
  scope = next;
  state = { cwd: readPersistedCwd() };
  if (state.cwd !== previous) notify(state.cwd);
}

/**
 * Subscribe to cwd changes. Returns an unsubscribe function.
 * The listener receives the new workspace-relative cwd ('' = project folder).
 */
export function subscribeProjectFolderCwd(listener: CwdListener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

// ── Derived helpers ─────────────────────────────────────────────────────────

/**
 * The short cwd label for compact UI (terminal prompt, breadcrumb):
 * the last path segment, or `~` for the project folder.
 */
export function projectFolderCwdLabel(cwd: string): string {
  const normalized = normalizeProjectFolderCwd(cwd) ?? '';
  if (normalized === '') return '~';
  return normalized.split('/').pop() || '~';
}

/**
 * The one-line agent context note for the current cwd, or `''` when the cwd
 * is the project folder (nothing to say). Rendered into the outgoing query
 * context so the agent knows all project files live under this prefix —
 * the agent's file-tool paths are NOT rewritten.
 */
export function projectFolderCwdContextLine(cwd: string = getProjectFolderCwd()): string {
  const normalized = normalizeProjectFolderCwd(cwd) ?? '';
  if (normalized === '') return '';
  return `Working directory: ${normalized} (all project files live under this prefix)`;
}

/**
 * Resolve a path seen by the UI (workspace-relative, possibly cwd-relative in
 * a rooted tree) against the cwd, for handing to project folder file ops which
 * always speak workspace-relative paths.
 */
export function resolveProjectFolderPath(path: string, cwd: string = getProjectFolderCwd()): string {
  const normalizedCwd = normalizeProjectFolderCwd(cwd) ?? '';
  const normalizedPath = normalizeProjectFolderCwd(path) ?? '';
  if (normalizedPath === '') return normalizedCwd;
  if (normalizedCwd === '') return normalizedPath;
  if (normalizedPath.startsWith(`${normalizedCwd}/`)) return normalizedPath;
  return `${normalizedCwd}/${normalizedPath}`;
}

/**
 * Resolve a `cd` argument against the session directory (pure; the native
 * terminal console uses this to track its own session directory).
 *
 * Chroot semantics: the project folder is the hard ceiling — `..` past it
 * stays at the root, and the result can never escape the project folder.
 * Supported inputs: relative names, `.`/`./x`, `..`/`../x`, `a/../b`,
 * trailing slashes, `~` and `~/x` (root-relative home). Returns `null`
 * only for unsupported input: absolute paths (`/…`) and non-strings —
 * callers should surface that as "unsupported path", not silently ignore.
 *
 * Returns the new workspace-relative directory ('' = project folder).
 */
export function resolveCdTarget(arg: string, sessionCwd: string): string | null {
  if (typeof arg !== 'string') return null;
  const trimmed = arg.trim();
  // Absolute paths point outside the project folder grant — unsupported.
  if (trimmed.startsWith('/')) return null;
  // `~` / `~/…` resolve against the project folder (the chroot home).
  const fromRoot = trimmed === '~' || trimmed.startsWith('~/');
  const base = fromRoot ? '' : (normalizeProjectFolderCwd(sessionCwd) ?? '');
  const body = fromRoot ? trimmed.replace(/^~\/?/, '') : trimmed;
  const segments: string[] = base === '' ? [] : base.split('/');
  for (const seg of body.split('/')) {
    if (seg === '' || seg === '.') continue;
    if (seg === '..') {
      if (segments.length > 0) segments.pop(); // chroot: '..' at root stays
      continue;
    }
    segments.push(seg);
  }
  return segments.join('/');
}

// ── React binding ───────────────────────────────────────────────────────────

/**
 * Reactive cwd for components, via `useSyncExternalStore` (the AppStore
 * pattern): `getProjectFolderCwd` is a stable snapshot getter and the subscribe
 * closure is bound once so the store never resubscribes.
 */
export function useProjectFolderCwd(): string {
  return useSyncExternalStore(subscribeProjectFolderCwdForReact, getProjectFolderCwd);
}

/** Stable-bound subscribe for useSyncExternalStore (bound once, like AppStore). */
const subscribeProjectFolderCwdForReact = (listener: () => void): (() => void) => subscribeProjectFolderCwd(listener);

/** Test hook: drop the in-memory state (and the persisted copy) so the next
 *  read re-reads localStorage — mirrors projectFolderFs's `__reset…ForTests`. */
export function __resetProjectFolderCwdForTests(): void {
  const persistedScope = scope;
  state = null;
  scope = '';
  listeners.clear();
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      window.localStorage.removeItem(WORKSPACE_CWD_STORAGE_KEY);
      if (persistedScope) window.localStorage.removeItem(projectFolderCwdStorageKey(persistedScope));
    }
  } catch {
    // best-effort: reset() already cleared in-memory state; a leftover
    // storage key only restores a stale cwd on the next reload.
  }
}
