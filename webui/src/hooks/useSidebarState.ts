/**
 * Sidebar UI state with localStorage persistence.
 *
 * Manages: sidebar collapsed state, mobile detection, sidebar open/close
 * (mobile overlay), and terminal expanded state.
 */

import { useState, useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { debugLog } from '../utils/log';

export interface UseSidebarStateReturn {
  isMobile: boolean;
  isSidebarOpen: boolean;
  sidebarCollapsed: boolean;
  isTerminalExpanded: boolean;
  setIsMobile: Dispatch<SetStateAction<boolean>>;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebar: () => void;
  closeSidebar: () => void;
  handleSidebarToggle: () => void;
  setIsTerminalExpanded: (expanded: boolean) => void;
}

function loadPersistedBoolean(key: string, fallback: boolean): boolean {
  try {
    return window.localStorage.getItem(key) === 'true';
  } catch (err) {
    debugLog('[loadPersistedBoolean] failed to read localStorage key:', key, err);
    return fallback;
  }
}

export function useSidebarState(): UseSidebarStateReturn {
  const [isMobile, setIsMobile] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);

  const [sidebarCollapsed, setSidebarCollapsedRaw] = useState(() =>
    loadPersistedBoolean('ledit-sidebar-collapsed', false),
  );

  const [isTerminalExpanded, setIsTerminalExpandedRaw] = useState(() =>
    loadPersistedBoolean('ledit-terminal-expanded', false),
  );

  const setSidebarCollapsed = useCallback((collapsed: boolean) => {
    try {
      window.localStorage.setItem('ledit-sidebar-collapsed', String(collapsed));
    } catch (err) {
      debugLog('[useSidebarState] failed to persist sidebar collapsed state:', err);
    }
    setSidebarCollapsedRaw(collapsed);
  }, []);

  const setIsTerminalExpanded = useCallback((expanded: boolean) => {
    try {
      window.localStorage.setItem('ledit-terminal-expanded', String(expanded));
    } catch (err) {
      debugLog('[useSidebarState] failed to persist terminal expanded state:', err);
    }
    setIsTerminalExpandedRaw(expanded);
  }, []);

  const toggleSidebar = useCallback(() => {
    setIsSidebarOpen((prev) => !prev);
  }, []);

  const closeSidebar = useCallback(() => {
    setIsSidebarOpen(false);
  }, []);

  const handleSidebarToggle = useCallback(() => {
    setSidebarCollapsed(!sidebarCollapsed);
  }, [sidebarCollapsed, setSidebarCollapsed]);

  return {
    isMobile,
    isSidebarOpen,
    sidebarCollapsed,
    isTerminalExpanded,
    setIsMobile,
    setSidebarCollapsed,
    toggleSidebar,
    closeSidebar,
    handleSidebarToggle,
    setIsTerminalExpanded,
  };
}
