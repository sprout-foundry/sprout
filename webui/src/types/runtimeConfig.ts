/**
 * Runtime configuration provided by the server via GET /api/bootstrap.
 * Falls back to import.meta.env.VITE_* vars, then localhost defaults.
 */
import type { PlatformNavItem } from '../services/apiAdapter';

/**
 * Git reconciliation snapshot served by the daemon (ETH-1 sync-on-resume).
 * Mirrors the pinned Go contract in pkg/git/sync.go — snake_case field names
 * are part of that contract and must not be renamed here.
 */
export interface GitSyncReport {
  /** False when the workspace root is not inside a git repository (all other fields are then zero/empty). */
  in_git_repo: boolean;

  /** Checked-out branch (empty when it cannot be determined). */
  branch: string;

  /** Repo-root-relative paths that differ from HEAD or do not exist in HEAD
   * (modified, staged, deleted, renamed, copied AND untracked). */
  dirty_files: string[];

  /** Commits on HEAD that the upstream does not have. */
  ahead: number;

  /** Commits on the upstream that HEAD does not have. */
  behind: number;

  /** Tip commit; empty object when the repository has no commits. */
  last_commit: {
    sha: string;
    subject: string;
    author: string;
    /** RFC3339 UTC, e.g. "2026-08-25T22:14:03Z". */
    timestamp: string;
  };

  /** Outcome of the optional non-destructive `git pull --ff-only`. */
  pull: {
    /** True iff a pull subprocess actually ran (false for every skipped_* outcome). */
    attempted: boolean;
    result: 'not_attempted' | 'up_to_date' | 'fast_forwarded' | 'skipped_no_upstream' | 'skipped_dirty' | 'error';
    /** git's own message when result is "error"; empty otherwise. */
    error: string;
  };
}

export interface RuntimeConfig {
  /** Base URL for API requests (e.g., "http://localhost:56000") */
  apiBaseURL: string;

  /** WebSocket URL for real-time updates */
  wsURL: string;

  /** Authentication mode: "none" (local) or "bearer" (cloud/token) */
  authMode: 'none' | 'bearer';

  /** Application mode: "local" (desktop/self-hosted) or "cloud" (managed) */
  appMode: 'local' | 'cloud';

  /** Version string embedded at build time */
  buildVersion: string;

  /** True when the server shares the CLI's agent (non-daemon interactive mode).
   * The frontend hides multi-chat UI and shows "coupled with terminal" messaging. */
  sharedMode?: boolean;

  /** Platform navigation items (tasks, billing, team, etc.) injected at runtime.
   * Falls back to CLOUD_NAV_ITEMS when the platform doesn't provide them. */
  navItems?: PlatformNavItem[];

  /** Authenticated user identity, injected by the platform in cloud mode.
   * Undefined when there is no session (local mode or unauthenticated). */
  user?: {
    id: string;
    email: string;
    tier: string;
  };

  /** URLs of external plugin script bundles (IIFE) to load at bootstrap time. */
  pluginScripts?: string[];

  /** Workspace git state at boot (ETH-1 sync-on-resume), computed by the
   * daemon with the pull disabled so bootstrap never mutates the repo.
   * null when the daemon could not determine git state; the frontend can
   * always re-fetch the live state from GET /api/sync. */
  sync?: GitSyncReport | null;
}
