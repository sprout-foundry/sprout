import { useRef } from 'react';

/**
 * Keep an object's identity stable while its values are shallow-equal.
 *
 * The object is rebuilt on every render; the previous reference is returned
 * when every key compares `Object.is`-equal. That lets a `React.memo` child
 * downstream bail out of renders driven by state it does not read — the app
 * root re-renders on every chat stream flush, and without this the sidebar's
 * memo never matched.
 *
 * Caller contract: every value in the object must be a primitive, a stable
 * reference, or a memoized callback. The returned object is the earlier
 * render's object, so an inline closure that happens to compare equal would
 * be returned as the old closure.
 */
export function useShallowStable<T extends object>(value: T): T {
  const ref = useRef<T>(value);
  const prev = ref.current;
  if (prev !== value) {
    const nextRecord = value as Record<string, unknown>;
    const prevRecord = prev as Record<string, unknown>;
    const keys = Object.keys(nextRecord);
    const prevKeys = Object.keys(prevRecord);
    const same =
      keys.length === prevKeys.length &&
      keys.every(
        (key) => Object.prototype.hasOwnProperty.call(prevRecord, key) && Object.is(prevRecord[key], nextRecord[key]),
      );
    if (!same) ref.current = value;
  }
  return ref.current;
}
