import { useEffect, useState } from 'react';
import { ApiService, type ShellInfo } from '../services/api';
import { notificationBus } from '../services/notificationBus';
import { debugLog } from '../utils/log';

export interface UseAvailableShellsResult {
  availableShells: ShellInfo[];
  shellsLoaded: boolean;
  selectedShell: string | null;
  setSelectedShell: (shell: string | null) => void;
}

/**
 * Owns the available-shells state and async-load effect previously
 * inlined in Terminal.tsx. Selects the default shell on first load if
 * no shell has been chosen yet; notifies via notificationBus on failure
 * (warning level).
 *
 * SP-075-extension: extracted from Terminal.tsx to reduce
 * single-file complexity. No behavior change.
 */
export function useAvailableShells(): UseAvailableShellsResult {
  const [availableShells, setAvailableShells] = useState<ShellInfo[]>([]);
  const [shellsLoaded, setShellsLoaded] = useState(false);
  const [selectedShell, setSelectedShell] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    // Track R (terminal): in native mode (ratified dist + shell-provided
    // terminal) the daemon shells endpoint never exists — the native
    // console marks sessionStorage before this effect runs. Skip the
    // fetch (and its warning toast) instead of failing every mount.
    let nativeMode = false;
    try {
      nativeMode = sessionStorage.getItem('sprout-native-terminal') === '1';
    } catch {
      /* ignore */
    }
    if (nativeMode) {
      setShellsLoaded(true);
      return;
    }
    ApiService.getInstance()
      .getAvailableShells()
      .then((res) => {
        if (cancelled) return;
        const shells = res.shells || [];
        setAvailableShells(shells);
        const defaultShell = shells.find((s) => s.default) || shells[0];
        if (defaultShell) {
          setSelectedShell(defaultShell.name);
        }
        setShellsLoaded(true);
      })
      .catch((err) => {
        debugLog('[Terminal] Failed to load available shells:', err);
        notificationBus.notify('warning', 'Terminal', 'Failed to load available shells: ' + String(err));
        setShellsLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return { availableShells, shellsLoaded, selectedShell, setSelectedShell };
}
