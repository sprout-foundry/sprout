import { useCallback, useEffect, useRef, useState } from 'react';
import { debugLog } from '../utils/log';

export const INLAY_HINTS_SETTING_KEY = 'editor:inlay-hints-enabled';
export const CODE_LENS_SETTING_KEY = 'editor:code-lens-enabled';

const SETTING_CHANGED_EVENT = 'sprout:editor-boolean-setting-changed';

interface SettingChangedDetail {
  key: string;
  value: boolean;
}

/**
 * Persisted boolean editor setting with a debounced toggle.
 *
 * Every instance sharing a storage key stays in sync: a write broadcasts on
 * `window`, so split panes and the settings panel all see the same value
 * without going through the active-pane-gated command events.
 *
 * The ref mirror is necessary because the toggle is bound to global event
 * listeners (`useEditorEvents`) whose callbacks must read the *current*
 * value, not the value captured when the effect first registered.
 */
export function useEditorBooleanSetting(
  storageKey: string,
  defaultValue: boolean,
): {
  value: boolean;
  ref: React.MutableRefObject<boolean>;
  toggle: () => void;
  set: (v: boolean) => void;
} {
  const [value, setValue] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey);
      return stored !== null ? stored === 'true' : defaultValue;
    } catch (err) {
      debugLog(`Failed to read ${storageKey} from localStorage:`, err);
      return defaultValue;
    }
  });
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  }, [value]);

  useEffect(() => {
    const onChanged = (e: Event) => {
      const detail = (e as CustomEvent<SettingChangedDetail>).detail;
      if (detail?.key !== storageKey) return;
      ref.current = detail.value;
      setValue(detail.value);
    };
    window.addEventListener(SETTING_CHANGED_EVENT, onChanged);
    return () => window.removeEventListener(SETTING_CHANGED_EVENT, onChanged);
  }, [storageKey]);

  const set = useCallback(
    (v: boolean) => {
      ref.current = v;
      setValue(v);
      try {
        localStorage.setItem(storageKey, String(v));
      } catch (err) {
        debugLog(`[set ${storageKey}] localStorage persist failed:`, err);
      }
      window.dispatchEvent(
        new CustomEvent<SettingChangedDetail>(SETTING_CHANGED_EVENT, { detail: { key: storageKey, value: v } }),
      );
    },
    [storageKey],
  );

  const lastToggleRef = useRef(0);
  const toggle = useCallback(() => {
    const now = Date.now();
    // Coalesce double-fires within 100ms. Some hotkey schemes (omnibox →
    // event → keybinding) can trigger the same toggle twice within a tick.
    if (now - lastToggleRef.current < 100) return;
    lastToggleRef.current = now;
    set(!ref.current);
  }, [set]);

  return { value, ref, toggle, set };
}
