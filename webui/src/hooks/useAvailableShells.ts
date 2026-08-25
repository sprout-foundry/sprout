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
