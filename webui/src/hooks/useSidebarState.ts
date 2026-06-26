/**
 * Sidebar UI state with localStorage persistence.
 *
 * Manages: sidebar collapsed state, mobile detection, sidebar open/close
 * (mobile overlay), terminal expanded state, active sidebar tab, and sidebar width.
 */

import { useState, useCallback, useRef, useEffect } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { debugLog } from '../utils/log';

export type SectionTab = 'git' | 'logs' | 'files' | 'settings' | 'search' | 'automations';

export const SIDEBAR_MIN_WIDTH = 200;
export const SIDEBAR_MAX_WIDTH = 600;
export const SIDEBAR_DEFAULT_WIDTH = 288;
/** Width of the icon-rail-only collapsed sidebar (px). Must match .sidebar.collapsed width in Sidebar.css. */
export const SIDEBAR_COLLAPSED_WIDTH = 48;

export const clampSidebarWidth = (value: number): number => {
  if (!Number.isFinite(value)) return SIDEBAR_DEFAULT_WIDTH;
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, value));
};

export interface UseSidebarStateReturn {
  isMobile: boolean;
  isTablet: boolean;
  isSidebarOpen: boolean;
  sidebarCollapsed: boolean;
  isTerminalExpanded: boolean;
  selectedSection: SectionTab;
  sidebarWidth: number;
  sidebarWidthRef: React.MutableRefObject<number>;
  setIsMobile: Dispatch<SetStateAction<boolean>>;
  setIsTablet: Dispatch<SetStateAction<boolean>>;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebar: () => void;
  closeSidebar: () => void;
  handleSidebarToggle: () => void;
  setIsTerminalExpanded: (expanded: boolean) => void;
  setSelectedSection: (tab: SectionTab) => void;
  setSidebarWidth: (width: number) => void;
  persistSidebarWidth: () => void;
  resetSidebarWidth: () => void;
}

function loadPersistedBoolean(key: string, fallback: boolean): boolean {
  try {
    return window.localStorage.getItem(key) === 'true';
  } catch (err) {
    debugLog('[loadPersistedBoolean] failed to read localStorage key:', key, err);
    return fallback;
  }
}

function loadPersistedString<T extends string>(key: string, fallback: T, validValues: readonly T[]): T {
  try {
    const value = window.localStorage.getItem(key);
    if (value && validValues.includes(value as T)) {
      return value as T;
    }
  } catch (err) {
    debugLog('[loadPersistedString] failed to read localStorage key:', key, err);
  }
  return fallback;
}

const VALID_SECTION_TABS: readonly SectionTab[] = [
  'git',
  'logs',
  'files',
  'settings',
  'search',
  'automations',
] as const;

export function useSidebarState(): UseSidebarStateReturn {
  const [isMobile, setIsMobile] = useState(false);
  const [isTablet, setIsTablet] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);

  const [sidebarCollapsed, setSidebarCollapsedRaw] = useState(() =>
    loadPersistedBoolean('sprout-sidebar-collapsed', false),
  );

  const [isTerminalExpanded, setIsTerminalExpandedRaw] = useState(() =>
    loadPersistedBoolean('sprout-terminal-expanded', false),
  );

  const [selectedSection, setSelectedSectionRaw] = useState<SectionTab>(() =>
    loadPersistedString('sprout-sidebar-active-tab', 'git' as SectionTab, VALID_SECTION_TABS),
  );

  const [sidebarWidth, setSidebarWidthRaw] = useState(() => {
    try {
      const stored = window.localStorage.getItem('sprout-sidebar-width');
      const parsed = stored ? Number(stored) : SIDEBAR_DEFAULT_WIDTH;
      return clampSidebarWidth(parsed);
    } catch (err) {
      debugLog('[useSidebarState] failed to load sidebar width from localStorage:', err);
      return SIDEBAR_DEFAULT_WIDTH;
    }
  });

  const sidebarWidthRef = useRef(sidebarWidth);
  sidebarWidthRef.current = sidebarWidth;

  const setSidebarCollapsed = useCallback((collapsed: boolean) => {
    try {
      window.localStorage.setItem('sprout-sidebar-collapsed', String(collapsed));
    } catch (err) {
      debugLog('[useSidebarState] failed to persist sidebar collapsed state:', err);
    }
    setSidebarCollapsedRaw(collapsed);
  }, []);

  const setIsTerminalExpanded = useCallback((expanded: boolean) => {
    try {
      window.localStorage.setItem('sprout-terminal-expanded', String(expanded));
    } catch (err) {
      debugLog('[useSidebarState] failed to persist terminal expanded state:', err);
    }
    setIsTerminalExpandedRaw(expanded);
  }, []);

  const setSelectedSection = useCallback((tab: SectionTab) => {
    try {
      window.localStorage.setItem('sprout-sidebar-active-tab', String(tab));
    } catch (err) {
      debugLog('[useSidebarState] failed to persist selected section:', err);
    }
    setSelectedSectionRaw(tab);
  }, []);

  const setSidebarWidth = useCallback((width: number) => {
    const clamped = clampSidebarWidth(width);
    setSidebarWidthRaw(clamped);
  }, []);

  const persistSidebarWidth = useCallback(() => {
    try {
      window.localStorage.setItem('sprout-sidebar-width', String(sidebarWidthRef.current));
    } catch (err) {
      debugLog('[useSidebarState] failed to persist sidebar width:', err);
    }
  }, []);

  const resetSidebarWidth = useCallback(() => {
    try {
      window.localStorage.setItem('sprout-sidebar-width', String(SIDEBAR_DEFAULT_WIDTH));
    } catch (err) {
      debugLog('[useSidebarState] failed to persist sidebar width reset:', err);
    }
    setSidebarWidthRaw(SIDEBAR_DEFAULT_WIDTH);
  }, []);

  // Keep the ref in sync with the state value for real-time drag operations
  useEffect(() => {
    sidebarWidthRef.current = sidebarWidth;
  }, [sidebarWidth]);

  const toggleSidebar = useCallback(() => {
    setIsSidebarOpen((prev) => !prev);
  }, []);

  const closeSidebar = useCallback(() => {
    setIsSidebarOpen(false);
  }, []);

  const handleSidebarToggle = useCallback(() => {
    setSidebarCollapsedRaw((prev) => {
      const next = !prev;
      try {
        window.localStorage.setItem('sprout-sidebar-collapsed', String(next));
      } catch (err) {
        debugLog('[useSidebarState] failed to persist sidebar collapsed state:', err);
      }
      return next;
    });
  }, []);

  return {
    isMobile,
    isTablet,
    isSidebarOpen,
    sidebarCollapsed,
    isTerminalExpanded,
    selectedSection,
    sidebarWidth,
    sidebarWidthRef,
    setIsMobile,
    setIsTablet,
    setSidebarCollapsed,
    toggleSidebar,
    closeSidebar,
    handleSidebarToggle,
    setIsTerminalExpanded,
    setSelectedSection,
    setSidebarWidth,
    persistSidebarWidth,
    resetSidebarWidth,
  };
}
