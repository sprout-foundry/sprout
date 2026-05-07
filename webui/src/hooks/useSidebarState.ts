/**
 * Sidebar UI state with localStorage persistence.
 *
 * Manages: sidebar collapsed state, mobile detection, sidebar open/close
 * (mobile overlay), terminal expanded state, and active sidebar tab.
 */

import { useState, useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { debugLog } from '../utils/log';

export type SectionTab = 'git' | 'logs' | 'files' | 'settings' | 'search';

export interface UseSidebarStateReturn {
  isMobile: boolean;
  isSidebarOpen: boolean;
  sidebarCollapsed: boolean;
  isTerminalExpanded: boolean;
  selectedSection: SectionTab;
  setIsMobile: Dispatch<SetStateAction<boolean>>;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebar: () => void;
  closeSidebar: () => void;
  handleSidebarToggle: () => void;
  setIsTerminalExpanded: (expanded: boolean) => void;
  setSelectedSection: (tab: SectionTab) => void;
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

const VALID_SECTION_TABS: readonly SectionTab[] = ['git', 'logs', 'files', 'settings', 'search'] as const;

export function useSidebarState(): UseSidebarStateReturn {
  const [isMobile, setIsMobile] = useState(false);
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

  const toggleSidebar = useCallback(() => {
    setIsSidebarOpen((prev) => !prev);
  }, []);

  const closeSidebar = useCallback(() => {
    setIsSidebarOpen(false);
  }, []);

  const handleSidebarToggle = useCallback(() => {
    setSidebarCollapsedRaw((prev) => {
      const next = !prev;
      try { window.localStorage.setItem('sprout-sidebar-collapsed', String(next)); } catch {}
      return next;
    });
  }, []);

  return {
    isMobile,
    isSidebarOpen,
    sidebarCollapsed,
    isTerminalExpanded,
    selectedSection,
    setIsMobile,
    setSidebarCollapsed,
    toggleSidebar,
    closeSidebar,
    handleSidebarToggle,
    setIsTerminalExpanded,
    setSelectedSection,
  };
}
