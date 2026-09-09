/**
 * workspaceCwd — session-level working directory for the studio webui.
 *
 * Sprout Studio mobile apps embed this webui with cloned GitHub repos living
 * at `repos/<owner>/<name>/` in the workspace (see services/workspaceFs/
 * workspaceGit.ts). Every surface — Files tree, native terminal, git panel,
 * agent — is rooted at the workspace top today. This module is the single
 * source of truth for a session-level working directory so selecting a repo
 * scopes all of them to `repos/<owner>/<name>` at once.
 *
 * Shape: a tiny external store (subscribe/get/set, like the project's other
 * services — see automateEvents.ts) holding `{ cwd }` where `cwd` is a
 * WORKSPACE-RELATIVE directory path and `''` means the workspace root.
 * Consumers:
 *   - SidebarFilesSection  → roots the file tree at cwd, repo selector chip.
 *   - NativeTerminalConsole → passes cwd to each `terminalSpawn` (one-shot
 *     semantics: every spawn uses the cwd current at submit time) and shows
 *     the last path segment in the prompt.
 *   - gitApi / useGitWorkspace / useGitHandlers → target the repo at cwd.
 *   - Chat send path → appends a one-line working-directory context note.
 *
 * Persistence: localStorage key `sprout-workspace-cwd` (plain string, not
 * JSON — the value is already an unambiguous path or ''). Storage failures
 * (private mode, quota) degrade to an in-memory store, never throw.
 *
 * Default build: cwd starts `''` (workspace root), so every consumer's
 * "cwd active" branch is a no-op and behavior is identical to before.
 */

import { useSyncExternalStore } from 'react';
import { debugLog } from '../utils/log';

/** localStorage key holding the workspace-relative cwd ('' = workspace root). */
export const WORKSPACE_CWD_STORAGE_KEY = 'sprout-workspace-cwd';

/** The store's state. Kept as an object so future fields can be added. */
export interface WorkspaceCwdState {
  cwd: string;
}

// ── Validation / normalization (pure) ───────────────────────────────────────

/**
 * Normalize a candidate working directory to the canonical workspace-relative
 * form: leading/trailing slashes stripped, `.`/empty segments dropped,
 * duplicate slashes collapsed.
 *
 * Returns `''` for the workspace root (including for inputs like `'/'`,
 * `'.'`, and `''`). Returns `null` when the input escapes the workspace —
 * any `..` segment (before OR after normalization) is a hard reject: callers
 * must never be able to point Files/Terminal/Git/Agent above the workspace.
 * Non-string input is also `null`.
 *
 * Pure and synchronous; safe to call anywhere (no window access).
 */
export function normalizeWorkspaceCwd(input: string): string | null {
  if (typeof input !== 'string') return null;
  const segments = input.split('/').filter((s) => s.length > 0 && s !== '.');
  if (segments.includes('..')) return null;
  return segments.join('/');
}

// ── Store (module-level singleton) ──────────────────────────────────────────

type CwdListener = (cwd: string) => void;
const listeners = new Set<CwdListener>();

/** Safely read the persisted cwd; '' on any failure (also outside browsers). */
function readPersistedCwd(): string {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return '';
    const raw = window.localStorage.getItem(WORKSPACE_CWD_STORAGE_KEY);
    if (!raw) return '';
    const normalized = normalizeWorkspaceCwd(raw);
    return normalized ?? '';
  } catch {
    // best-effort: unreadable storage reads as the workspace root.
    return '';
  }
}

/** Safely persist the cwd; storage failures are ignored (in-memory only). */
function persistCwd(cwd: string): void {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return;
    window.localStorage.setItem(WORKSPACE_CWD_STORAGE_KEY, cwd);
  } catch (err) {
    // Private mode / quota: the session still works, just not across reloads.
    debugLog('[workspaceCwd] failed to persist cwd:', err);
  }
}

/**
 * Lazily-initialized state. Reading localStorage at module scope would run in
 * every test/SSR import; deferring to first access keeps imports side-effect
 * free and lets tests reset cleanly via `__resetWorkspaceCwdForTests()`.
 */
let state: WorkspaceCwdState | null = null;

function getState(): WorkspaceCwdState {
  if (state === null) {
    state = { cwd: readPersistedCwd() };
  }
  return state;
}

/**
 * The current workspace-relative working directory; `''` = workspace root.
 * Never throws. Reads the persisted value on first call.
 */
export function getWorkspaceCwd(): string {
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
export function setWorkspaceCwd(dir: string): boolean {
  const normalized = normalizeWorkspaceCwd(dir);
  if (normalized === null) return false;
  const current = getState();
  if (current.cwd === normalized) return false;
  state = { cwd: normalized };
  persistCwd(normalized);
  for (const listener of listeners) {
    try {
      listener(normalized);
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error('[workspaceCwd] Listener error (swallowed):', err);
    }
  }
  return true;
}

/**
 * Subscribe to cwd changes. Returns an unsubscribe function.
 * The listener receives the new workspace-relative cwd ('' = workspace root).
 */
export function subscribeWorkspaceCwd(listener: CwdListener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

// ── Derived helpers ─────────────────────────────────────────────────────────

/**
 * The short cwd label for compact UI (terminal prompt, breadcrumb):
 * the last path segment, or `~` for the workspace root.
 */
export function workspaceCwdLabel(cwd: string): string {
  const normalized = normalizeWorkspaceCwd(cwd) ?? '';
  if (normalized === '') return '~';
  return normalized.split('/').pop() || '~';
}

/**
 * The one-line agent context note for the current cwd, or `''` when the cwd
 * is the workspace root (nothing to say). Rendered into the outgoing query
 * context so the agent knows all project files live under this prefix —
 * the agent's file-tool paths are NOT rewritten.
 */
export function workspaceCwdContextLine(cwd: string = getWorkspaceCwd()): string {
  const normalized = normalizeWorkspaceCwd(cwd) ?? '';
  if (normalized === '') return '';
  return `Working directory: ${normalized} (all project files live under this prefix)`;
}

/**
 * Resolve a path seen by the UI (workspace-relative, possibly cwd-relative in
 * a rooted tree) against the cwd, for handing to workspace file ops which
 * always speak workspace-relative paths.
 */
export function resolveWorkspacePath(path: string, cwd: string = getWorkspaceCwd()): string {
  const normalizedCwd = normalizeWorkspaceCwd(cwd) ?? '';
  const normalizedPath = normalizeWorkspaceCwd(path) ?? '';
  if (normalizedPath === '') return normalizedCwd;
  if (normalizedCwd === '') return normalizedPath;
  if (normalizedPath.startsWith(`${normalizedCwd}/`)) return normalizedPath;
  return `${normalizedCwd}/${normalizedPath}`;
}

/**
 * Resolve a `cd` argument against the session directory (pure; the native
 * terminal console uses this to track its own session directory).
 *
 * Chroot semantics: the workspace root is the hard ceiling — `..` past it
 * stays at the root, and the result can never escape the workspace.
 * Supported inputs: relative names, `.`/`./x`, `..`/`../x`, `a/../b`,
 * trailing slashes, `~` and `~/x` (root-relative home). Returns `null`
 * only for unsupported input: absolute paths (`/…`) and non-strings —
 * callers should surface that as "unsupported path", not silently ignore.
 *
 * Returns the new workspace-relative directory ('' = workspace root).
 */
export function resolveCdTarget(arg: string, sessionCwd: string): string | null {
  if (typeof arg !== 'string') return null;
  const trimmed = arg.trim();
  // Absolute paths point outside the workspace grant — unsupported.
  if (trimmed.startsWith('/')) return null;
  // `~` / `~/…` resolve against the workspace root (the chroot home).
  const fromRoot = trimmed === '~' || trimmed.startsWith('~/');
  const base = fromRoot ? '' : (normalizeWorkspaceCwd(sessionCwd) ?? '');
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
 * pattern): `getWorkspaceCwd` is a stable snapshot getter and the subscribe
 * closure is bound once so the store never resubscribes.
 */
export function useWorkspaceCwd(): string {
  return useSyncExternalStore(subscribeWorkspaceCwdForReact, getWorkspaceCwd);
}

/** Stable-bound subscribe for useSyncExternalStore (bound once, like AppStore). */
const subscribeWorkspaceCwdForReact = (listener: () => void): (() => void) => subscribeWorkspaceCwd(listener);

/** Test hook: drop the in-memory state (and the persisted copy) so the next
 *  read re-reads localStorage — mirrors workspaceFs's `__reset…ForTests`. */
export function __resetWorkspaceCwdForTests(): void {
  state = null;
  listeners.clear();
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      window.localStorage.removeItem(WORKSPACE_CWD_STORAGE_KEY);
    }
  } catch {
    // best-effort: reset() already cleared in-memory state; a leftover
    // storage key only restores a stale cwd on the next reload.
  }
}
