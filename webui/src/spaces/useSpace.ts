/**
 * Active space state.
 *
 * Owns which mode the shell is showing and persists it **per instance**: each
 * connected filesystem root remembers which mode the user was in, matching how
 * the rest of the app scopes state by instance PID and UI context
 * (`getAppStateStorageKey` in services/appStatePersistence.ts).
 *
 * Persistence is best-effort. A read or write that throws (private mode, quota,
 * disabled storage) must not break the shell — the mode falls back to the
 * default and the user can still switch.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { EARLIER_SPACE_STORAGE_KEY, INSTANCE_PID_STORAGE_KEY, SPACE_STORAGE_KEY } from '../constants/app';
import { readMovedItem } from '../services/movedStorageKey';
import { repoScopedKey } from '../services/repoScope';
import {
  BUILTIN_DEFAULT_WORKSPACE_MODE,
  availableModes,
  defaultSpace,
  resolveSpace,
  type Space,
  type SpaceContext,
  type SpaceId,
} from './registry';

/** The UI-context scope, mirroring services/appStatePersistence's rule. */
export function uiContextScope(): string {
  if (typeof window === 'undefined') return 'local';
  const path = window.location.pathname || '/';
  return path.startsWith('/ssh/') ? 'ssh' : 'local';
}

/** Storage key for the current instance + UI context. */
export function spaceStorageKey(prefix: string = SPACE_STORAGE_KEY): string {
  if (typeof window === 'undefined' || !window.localStorage) {
    return `${prefix}:default:local`;
  }
  const pid = window.localStorage.getItem(INSTANCE_PID_STORAGE_KEY) || 'default';
  return repoScopedKey(`${prefix}:${pid}:${uiContextScope()}`);
}

/** The persisted mode id for this instance, or null when unset/unreadable. */
export function readPersistedSpace(): SpaceId | null {
  if (typeof window === 'undefined' || !window.localStorage) return null;
  try {
    const raw = readMovedItem(window.localStorage, spaceStorageKey(), spaceStorageKey(EARLIER_SPACE_STORAGE_KEY));
    return raw ? (JSON.parse(raw) as SpaceId) : null;
  } catch {
    return null;
  }
}

/** Persist the mode id for this instance. Failures are swallowed by design. */
export function persistSpace(id: SpaceId): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    window.localStorage.setItem(spaceStorageKey(), JSON.stringify(id));
  } catch {
    // Storage unavailable (private mode, quota). The mode still works for this
    // session; only the preference is lost.
  }
}

export interface UseSpaceResult {
  /** The active mode, resolved against what this project folder offers. */
  mode: Space;
  /** Modes to list in the switcher, in order. */
  modes: Space[];
  /** Switch modes. */
  select: (id: SpaceId) => void;
  /** True when the project folder offers more than one mode. */
  canSwitch: boolean;
}

/**
 * Tracks the active mode for the current project folder.
 *
 * The requested id is kept in state and always resolved through `resolveSpace`,
 * so a persisted id this build no longer registers degrades to Code instead of
 * rendering a mode with nothing to show.
 */
export function useSpace(ctx: SpaceContext): UseSpaceResult {
  const [requested, setRequested] = useState<SpaceId | null>(() => readPersistedSpace());
  const { hasMockup } = ctx;

  // Depend on the primitive, not the context object: a caller passing an inline
  // object literal would otherwise get a new identity every render and rebuild
  // the mode list each time.
  const modes = useMemo(() => availableModes({ hasMockup }), [hasMockup]);
  const mode = useMemo(() => resolveSpace(requested, { hasMockup }), [requested, hasMockup]);

  // Re-persist when availability changes the resolved mode: a project folder that
  // no longer offers the mode should come back on Code, not keep it stale.
  useEffect(() => {
    if (requested !== null && requested !== mode.id) persistSpace(mode.id);
  }, [requested, mode.id]);

  const select = useCallback((id: SpaceId) => {
    setRequested(id);
    persistSpace(id);
  }, []);

  return { mode, modes, select, canSwitch: modes.length > 1 };
}

export { BUILTIN_DEFAULT_WORKSPACE_MODE, defaultSpace };
