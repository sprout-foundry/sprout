import { useCallback, useState } from 'react';

// useLocalStorage persists a piece of state to localStorage under key. The
// value is read once, lazily, so a render never touches storage unless it
// needs the initial value; writes go through localStorage so the value
// survives a reload. A malformed stored value falls back to initial rather
// than throwing, so a stale key never breaks the app.
export function useLocalStorage<T>(key: string, initial: T) {
  const [value, setValue] = useState<T>(() => readStored(key, initial));

  const set = useCallback(
    (next: T) => {
      setValue(next);
      try {
        localStorage.setItem(key, JSON.stringify(next));
      } catch {
        // Storage can be unavailable (private mode, quota). The in-memory
        // state still updates; persistence is best-effort.
      }
    },
    [key],
  );

  return [value, set] as const;
}

function readStored<T>(key: string, initial: T): T {
  try {
    const raw = localStorage.getItem(key);
    if (raw === null) {
      return initial;
    }
    return JSON.parse(raw) as T;
  } catch {
    return initial;
  }
}
