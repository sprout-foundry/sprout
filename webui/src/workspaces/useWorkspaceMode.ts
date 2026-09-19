/**
 * Active workspace-mode state.
 *
 * Owns which mode the shell is showing and persists it **per instance**: each
 * connected filesystem root remembers whether the user was in Code or Design,
 * matching how the rest of the app scopes state by instance PID and UI context
 * (`getAppStateStorageKey` in services/appStatePersistence.ts).
 *
 * Persistence is best-effort. A read or write that throws (private mode, quota,
 * disabled storage) must not break the shell — the mode falls back to the
 * default and the user can still switch.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { INSTANCE_PID_STORAGE_KEY, WORKSPACE_MODE_STORAGE_KEY } from '../constants/app';
import {
  DEFAULT_WORKSPACE_MODE,
  availableModes,
  resolveWorkspaceMode,
  type WorkspaceMode,
  type WorkspaceModeContext,
  type WorkspaceModeId,
} from './registry';

/** The UI-context scope, mirroring services/appStatePersistence's rule. */
function uiContextScope(): string {
  if (typeof window === 'undefined') return 'local';
  const path = window.location.pathname || '/';
  return path.startsWith('/ssh/') ? 'ssh' : 'local';
}

/** Storage key for the current instance + UI context. */
export function workspaceModeStorageKey(): string {
  if (typeof window === 'undefined' || !window.localStorage) {
    return `${WORKSPACE_MODE_STORAGE_KEY}:default:local`;
  }
  const pid = window.localStorage.getItem(INSTANCE_PID_STORAGE_KEY) || 'default';
  return `${WORKSPACE_MODE_STORAGE_KEY}:${pid}:${uiContextScope()}`;
}

/** The persisted mode id for this instance, or null when unset/unreadable. */
export function readPersistedWorkspaceMode(): WorkspaceModeId | null {
  if (typeof window === 'undefined' || !window.localStorage) return null;
  try {
    const raw = window.localStorage.getItem(workspaceModeStorageKey());
    return raw ? (JSON.parse(raw) as WorkspaceModeId) : null;
  } catch {
    return null;
  }
}

/** Persist the mode id for this instance. Failures are swallowed by design. */
export function persistWorkspaceMode(id: WorkspaceModeId): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    window.localStorage.setItem(workspaceModeStorageKey(), JSON.stringify(id));
  } catch {
    // Storage unavailable (private mode, quota). The mode still works for this
    // session; only the preference is lost.
  }
}

export interface UseWorkspaceModeResult {
  /** The active mode, resolved against what this workspace offers. */
  mode: WorkspaceMode;
  /** Modes to list in the switcher, in order. */
  modes: WorkspaceMode[];
  /** Switch modes. */
  select: (id: WorkspaceModeId) => void;
  /** True when the workspace offers more than one mode. */
  canSwitch: boolean;
}

/**
 * Tracks the active mode for the current workspace.
 *
 * The requested id is kept in state and always resolved through `resolveWorkspaceMode`,
 * so a persisted `design` in a workspace that has since lost its design tree
 * degrades to Code instead of rendering a mode with nothing to show.
 */
export function useWorkspaceMode(ctx: WorkspaceModeContext): UseWorkspaceModeResult {
  const [requested, setRequested] = useState<WorkspaceModeId | null>(() => readPersistedWorkspaceMode());
  const { hasDesignTree } = ctx;

  // Depend on the primitive, not the context object: a caller passing an inline
  // object literal would otherwise get a new identity every render and rebuild
  // the mode list each time.
  const modes = useMemo(() => availableModes({ hasDesignTree }), [hasDesignTree]);
  const mode = useMemo(() => resolveWorkspaceMode(requested, { hasDesignTree }), [requested, hasDesignTree]);

  // Re-persist when availability changes the resolved mode: a workspace that
  // lost its design tree should come back on Code, not keep a stale `design`.
  useEffect(() => {
    if (requested !== null && requested !== mode.id) persistWorkspaceMode(mode.id);
  }, [requested, mode.id]);

  const select = useCallback((id: WorkspaceModeId) => {
    setRequested(id);
    persistWorkspaceMode(id);
  }, []);

  return { mode, modes, select, canSwitch: modes.length > 1 };
}

export { DEFAULT_WORKSPACE_MODE };
