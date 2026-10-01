import { useEffect, useRef } from 'react';
import type { SproutEvent } from '../types/events';
import { changesWorkspaceFiles } from './workspaceFileEvents';

const DEBOUNCE_MS = 600;

/**
 * Calls `refresh` after workspace files change (the agent's file tools, shell
 * commands, saves), debounced so a burst of edits refreshes once. Listens on
 * the window event bridge, so it needs no events provider.
 */
export function useFileTreeAutoRefresh(refresh: () => void): void {
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const onEvent = (e: Event) => {
      if (!changesWorkspaceFiles((e as CustomEvent<SproutEvent>).detail)) return;
      if (timer !== null) clearTimeout(timer);
      timer = setTimeout(() => {
        timer = null;
        refreshRef.current();
      }, DEBOUNCE_MS);
    };
    window.addEventListener('sprout:wsevent', onEvent);
    return () => {
      window.removeEventListener('sprout:wsevent', onEvent);
      if (timer !== null) clearTimeout(timer);
    };
  }, []);
}
