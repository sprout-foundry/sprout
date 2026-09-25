/**
 * Active Design-mode section state (SP-140-8 item 8.5).
 *
 * Owns which section (rail entry) the Design surface shows — the shared
 * `designSection` AppContent owns today — and persists it **per instance**,
 * exactly the pattern `useWorkspaceMode` uses for the mode itself: keyed by
 * the connected filesystem root's PID + UI context
 * (`getAppStateStorageKey` in services/appStatePersistence.ts), so each
 * instance and each remote context remembers the section independently and a
 * reload returns to the section the user left.
 *
 * Persistence is best-effort. A read or write that throws (private mode,
 * quota, disabled storage) must not break the shell — the section falls back
 * to the default and the user can still switch. A persisted value that is no
 * longer a real section id falls back to the default too (a section rename
 * must never wedge the surface).
 */

import { useCallback, useState } from 'react';
import { DESIGN_SECTION_STORAGE_KEY, INSTANCE_PID_STORAGE_KEY } from '../constants/app';

/** The Design mode's sections — DesignTab's union, restated to keep this
 * module import-free of the component tree (the hook sits beside the
 * workspace-mode state, not inside the design components). */
export type DesignSectionId = 'flows' | 'screens' | 'tokens';

/** The section a Design-mode workspace opens on when nothing is persisted. */
export const DEFAULT_DESIGN_SECTION: DesignSectionId = 'flows';

/** The UI-context scope, mirroring services/appStatePersistence's rule. */
export function uiContextScope(): string {
  if (typeof window === 'undefined') return 'local';
  const path = window.location.pathname || '/';
  return path.startsWith('/ssh/') ? 'ssh' : 'local';
}

/** Storage key for the current instance + UI context. */
export function designSectionStorageKey(): string {
  if (typeof window === 'undefined' || !window.localStorage) {
    return `${DESIGN_SECTION_STORAGE_KEY}:default:local`;
  }
  const pid = window.localStorage.getItem(INSTANCE_PID_STORAGE_KEY) || 'default';
  return `${DESIGN_SECTION_STORAGE_KEY}:${pid}:${uiContextScope()}`;
}

function isDesignSectionId(value: unknown): value is DesignSectionId {
  return value === 'flows' || value === 'screens' || value === 'tokens';
}

/** The persisted section id for this instance, or null when unset/unreadable. */
export function readPersistedDesignSection(): DesignSectionId | null {
  if (typeof window === 'undefined' || !window.localStorage) return null;
  try {
    const raw = window.localStorage.getItem(designSectionStorageKey());
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    return isDesignSectionId(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

/** Persist the section id for this instance. Failures are swallowed by design. */
export function persistDesignSection(id: DesignSectionId): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    window.localStorage.setItem(designSectionStorageKey(), JSON.stringify(id));
  } catch {
    // Storage unavailable (private mode, quota). The section still works for
    // this session; only the preference is lost.
  }
}

/**
 * The Design mode's section state with per-instance persistence.
 *
 * The setter persists alongside the state update, so the store can never lag
 * the surface: a reload re-reads the section the last interaction left.
 */
export function useDesignSectionPersistence(): {
  section: DesignSectionId;
  setSection: (id: DesignSectionId) => void;
} {
  const [section, setSectionState] = useState<DesignSectionId>(
    () => readPersistedDesignSection() ?? DEFAULT_DESIGN_SECTION,
  );

  const setSection = useCallback((id: DesignSectionId) => {
    setSectionState(id);
    persistDesignSection(id);
  }, []);

  return { section, setSection };
}
