import { useState, useEffect } from 'react';

const STORAGE_KEY = 'ledit.contextPanel.width';
const DEFAULT_WIDTH = 360;
const MIN_WIDTH = 260;
const MAX_WIDTH = 600;

/**
 * Manages the context-panel width with localStorage persistence.
 *
 * Reads from `ledit.contextPanel.width` on mount (clamped to 260–600),
 * falling back to 360.  Every change is written back to localStorage so
 * the user's preference survives page reloads.
 */
export function usePanelWidth(): {
  panelWidth: number;
  setPanelWidth: (width: number) => void;
} {
  const [panelWidth, setPanelWidth] = useState(() => {
    if (typeof window === 'undefined') return DEFAULT_WIDTH;
    const storedWidth = Number(window.localStorage.getItem(STORAGE_KEY));
    if (Number.isFinite(storedWidth) && storedWidth >= MIN_WIDTH && storedWidth <= MAX_WIDTH) {
      return storedWidth;
    }
    return DEFAULT_WIDTH;
  });

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(STORAGE_KEY, String(Math.round(panelWidth)));
  }, [panelWidth]);

  return { panelWidth, setPanelWidth };
}
