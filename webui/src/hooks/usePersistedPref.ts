/**
 * usePersistedPref — small hooks for `useState`-backed preferences that
 * persist to localStorage with a validation/clamp step.
 *
 * These were extracted from `Terminal.tsx` during the SP-075-4h file
 * decomposition. They are general-purpose and have no Terminal-specific
 * behavior, so they live here in `hooks/`.
 *
 * Each hook:
 *  - Initializes lazily from localStorage on the client, with a sensible
 *    default when running on the server / in tests (where `window` is
 *    undefined) or when the stored value is missing/malformed.
 *  - Returns `[value, setValue]` where `setValue` either accepts a new
 *    value directly or an updater function. Setting the value persists it
 *    to localStorage as a side effect.
 *  - Catches localStorage errors (quota, disabled storage in private mode)
 *    via `debugLog` and never throws.
 */
import { useCallback, useEffect, useState } from 'react';
import { debugLog } from '../utils/log';

function safeWindow(): Window | undefined {
  return typeof window === 'undefined' ? undefined : window;
}

function safeRead(storageKey: string): string | null {
  const w = safeWindow();
  if (!w) return null;
  try {
    return w.localStorage.getItem(storageKey);
  } catch (err) {
    debugLog(`[usePersistedPref] failed to read ${storageKey} from localStorage:`, err);
    return null;
  }
}

function safeWrite(storageKey: string, value: string): void {
  const w = safeWindow();
  if (!w) return;
  try {
    w.localStorage.setItem(storageKey, value);
  } catch (err) {
    debugLog(`[usePersistedPref] failed to persist ${storageKey} to localStorage:`, err);
  }
}

/**
 * `usePersistedNumber` — a number-typed preference that round-trips
 * through localStorage. `parse` is responsible for converting the stored
 * string into a number (returning the fallback on parse failure);
 * `clamp` is responsible for validating the parsed value (and may
 * return the fallback too). The same `parse` and `clamp` functions are
 * used for both the initial read and subsequent writes, so a value that
 * becomes invalid after the schema changes is normalized on the next
 * render.
 */
export function usePersistedNumber(
  storageKey: string,
  fallback: number,
  parse: (raw: string | null) => number,
  clamp: (value: number) => number,
): [number, (next: number | ((prev: number) => number)) => void] {
  const [value, setValueState] = useState<number>(() => {
    const stored = safeRead(storageKey);
    return clamp(parse(stored));
  });

  const setValue = useCallback(
    (next: number | ((prev: number) => number)) => {
      setValueState((prev) => {
        const resolved = typeof next === 'function' ? (next as (p: number) => number)(prev) : next;
        const clamped = clamp(resolved);
        safeWrite(storageKey, String(clamped));
        return clamped;
      });
    },
    [storageKey, clamp],
  );

  return [value, setValue];
}

/**
 * `usePersistedBoolean` — a boolean-typed preference that round-trips
 * through localStorage as the strings "true" / "false". `parse` may
 * translate the stored string into a boolean (returning the fallback
 * for `null` or unexpected values); `format` serializes back to a
 * string for storage.
 */
export function usePersistedBoolean(
  storageKey: string,
  fallback: boolean,
  parse: (raw: string | null, fallback: boolean) => boolean,
  format: (value: boolean) => string = (v) => String(v),
): [boolean, (next: boolean | ((prev: boolean) => boolean)) => void] {
  const [value, setValueState] = useState<boolean>(() => {
    const stored = safeRead(storageKey);
    return parse(stored, fallback);
  });

  const setValue = useCallback(
    (next: boolean | ((prev: boolean) => boolean)) => {
      setValueState((prev) => {
        const resolved = typeof next === 'function' ? (next as (p: boolean) => boolean)(prev) : next;
        safeWrite(storageKey, format(resolved));
        return resolved;
      });
    },
    [storageKey, format],
  );

  return [value, setValue];
}

/**
 * `useOutsideClickDismiss` — when `enabled` is true, attaches a
 * `mousedown` listener on `document` and a `keydown` listener for the
 * `Escape` key. Both call `onDismiss` when fired and the click target
 * is not contained in `containerRef`. The listeners are cleaned up
 * automatically when the hook unmounts or when `enabled` flips to
 * false.
 *
 * Originally extracted from Terminal.tsx where the shell-picker menu and
 * the overflow menu each had their own copy of the same effect.
 */
export function useOutsideClickDismiss(
  enabled: boolean,
  containerRef: React.RefObject<HTMLElement | null>,
  onDismiss: () => void,
): void {
  useEffect(() => {
    if (!enabled) return undefined;

    const handleClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onDismiss();
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onDismiss();
      }
    };

    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [enabled, containerRef, onDismiss]);
}
