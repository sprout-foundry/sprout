/**
 * useSidebarState.test.ts — Unit tests for the useSidebarState hook.
 *
 * Covers:
 * - Initial default state when localStorage is empty
 * - isCollapsed persistence to localStorage via setSidebarCollapsed
 * - handleSidebarToggle toggling + persistence
 * - activeTab persistence to localStorage via setSelectedSection
 * - Invalid activeTab values fall back to default
 * - sidebarWidth persistence via persistSidebarWidth
 * - sidebarWidth clamping (min/max)
 * - resetSidebarWidth resets to default and persists
 * - isTerminalExpanded persistence
 * - Mobile/overlay state (isMobile, isSidebarOpen, toggleSidebar, closeSidebar)
 * - Error handling when localStorage throws on read/write
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// Mock log before importing the module under test
vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

import {
  useSidebarState,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_DEFAULT_WIDTH,
  clampSidebarWidth,
  type UseSidebarStateReturn,
} from './useSidebarState';

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

let result: UseSidebarStateReturn;
let container: HTMLDivElement;
let root: Root;

function TestComponent() {
  result = useSidebarState();
  return createElement('div');
}

function renderHook() {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root.render(createElement(TestComponent));
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useSidebarState', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    if (container.parentNode) {
      container.parentNode.removeChild(container);
    }
    vi.restoreAllMocks();
  });

  // -----------------------------------------------------------------------
  // Initial state
  // -----------------------------------------------------------------------

  describe('initial state (empty localStorage)', () => {
    it('returns correct defaults', () => {
      renderHook();
      expect(result.isMobile).toBe(false);
      expect(result.isSidebarOpen).toBe(false);
      expect(result.sidebarCollapsed).toBe(false);
      expect(result.isTerminalExpanded).toBe(false);
      expect(result.selectedSection).toBe('git');
      expect(result.sidebarWidth).toBe(SIDEBAR_DEFAULT_WIDTH);
    });
  });

  // -----------------------------------------------------------------------
  // isCollapsed persistence
  // -----------------------------------------------------------------------

  describe('isCollapsed persistence', () => {
    it('reads initial value from localStorage', () => {
      localStorage.setItem('sprout-sidebar-collapsed', 'true');
      renderHook();
      expect(result.sidebarCollapsed).toBe(true);
    });

    it('persists collapsed=true via setSidebarCollapsed', () => {
      renderHook();
      act(() => {
        result.setSidebarCollapsed(true);
      });
      expect(result.sidebarCollapsed).toBe(true);
      expect(localStorage.getItem('sprout-sidebar-collapsed')).toBe('true');
    });

    it('persists collapsed=false via setSidebarCollapsed', () => {
      localStorage.setItem('sprout-sidebar-collapsed', 'true');
      renderHook();
      act(() => {
        result.setSidebarCollapsed(false);
      });
      expect(result.sidebarCollapsed).toBe(false);
      expect(localStorage.getItem('sprout-sidebar-collapsed')).toBe('false');
    });

    it('survives page reload (re-render reads persisted value)', () => {
      renderHook();
      act(() => {
        result.setSidebarCollapsed(true);
      });
      act(() => {
        root.unmount();
      });
      // Re-render — localStorage still has 'true'
      renderHook();
      expect(result.sidebarCollapsed).toBe(true);
    });
  });

  // -----------------------------------------------------------------------
  // handleSidebarToggle
  // -----------------------------------------------------------------------

  describe('handleSidebarToggle', () => {
    it('toggles from false to true and persists', () => {
      renderHook();
      expect(result.sidebarCollapsed).toBe(false);
      act(() => {
        result.handleSidebarToggle();
      });
      expect(result.sidebarCollapsed).toBe(true);
      expect(localStorage.getItem('sprout-sidebar-collapsed')).toBe('true');
    });

    it('toggles back from true to false and persists', () => {
      localStorage.setItem('sprout-sidebar-collapsed', 'true');
      renderHook();
      act(() => {
        result.handleSidebarToggle();
      });
      expect(result.sidebarCollapsed).toBe(false);
      expect(localStorage.getItem('sprout-sidebar-collapsed')).toBe('false');
    });

    it('double toggle returns to original state', () => {
      renderHook();
      act(() => {
        result.handleSidebarToggle();
      });
      expect(result.sidebarCollapsed).toBe(true);
      act(() => {
        result.handleSidebarToggle();
      });
      expect(result.sidebarCollapsed).toBe(false);
      expect(localStorage.getItem('sprout-sidebar-collapsed')).toBe('false');
    });
  });

  // -----------------------------------------------------------------------
  // activeTab persistence
  // -----------------------------------------------------------------------

  describe('activeTab persistence', () => {
    it('reads initial value from localStorage', () => {
      localStorage.setItem('sprout-sidebar-active-tab', 'files');
      renderHook();
      expect(result.selectedSection).toBe('files');
    });

    it('persists tab via setSelectedSection', () => {
      renderHook();
      act(() => {
        result.setSelectedSection('logs');
      });
      expect(result.selectedSection).toBe('logs');
      expect(localStorage.getItem('sprout-sidebar-active-tab')).toBe('logs');
    });

    it('persists all valid tab values', () => {
      const tabs = ['git', 'logs', 'files', 'settings', 'search'] as const;
      for (const tab of tabs) {
        localStorage.clear();
        renderHook();
        act(() => {
          result.setSelectedSection(tab);
        });
        expect(result.selectedSection).toBe(tab);
        expect(localStorage.getItem('sprout-sidebar-active-tab')).toBe(tab);
        act(() => {
          root.unmount();
        });
      }
    });

    it('falls back to git for invalid localStorage value', () => {
      localStorage.setItem('sprout-sidebar-active-tab', 'invalid-tab');
      renderHook();
      expect(result.selectedSection).toBe('git');
    });

    it('falls back to git for empty string in localStorage', () => {
      localStorage.setItem('sprout-sidebar-active-tab', '');
      renderHook();
      expect(result.selectedSection).toBe('git');
    });

    it('survives page reload', () => {
      renderHook();
      act(() => {
        result.setSelectedSection('search');
      });
      act(() => {
        root.unmount();
      });
      renderHook();
      expect(result.selectedSection).toBe('search');
    });
  });

  // -----------------------------------------------------------------------
  // sidebarWidth persistence
  // -----------------------------------------------------------------------

  describe('sidebarWidth persistence', () => {
    it('reads initial value from localStorage', () => {
      localStorage.setItem('sprout-sidebar-width', '350');
      renderHook();
      expect(result.sidebarWidth).toBe(350);
    });

    it('falls back to default for NaN', () => {
      localStorage.setItem('sprout-sidebar-width', 'not-a-number');
      renderHook();
      expect(result.sidebarWidth).toBe(SIDEBAR_DEFAULT_WIDTH);
    });

    it('falls back to default when key is absent', () => {
      renderHook();
      expect(result.sidebarWidth).toBe(SIDEBAR_DEFAULT_WIDTH);
    });

    it('clamps values below minimum', () => {
      localStorage.setItem('sprout-sidebar-width', '50');
      renderHook();
      expect(result.sidebarWidth).toBe(SIDEBAR_MIN_WIDTH);
    });

    it('clamps values above maximum', () => {
      localStorage.setItem('sprout-sidebar-width', '9999');
      renderHook();
      expect(result.sidebarWidth).toBe(SIDEBAR_MAX_WIDTH);
    });

    it('setSidebarWidth clamps and updates state but does not persist immediately', () => {
      renderHook();
      act(() => {
        result.setSidebarWidth(400);
      });
      expect(result.sidebarWidth).toBe(400);
      // setSidebarWidth does NOT persist — that's for drag-in-progress
      expect(localStorage.getItem('sprout-sidebar-width')).toBeNull();
    });

    it('setSidebarWidth clamps below minimum', () => {
      renderHook();
      act(() => {
        result.setSidebarWidth(10);
      });
      expect(result.sidebarWidth).toBe(SIDEBAR_MIN_WIDTH);
    });

    it('setSidebarWidth clamps above maximum', () => {
      renderHook();
      act(() => {
        result.setSidebarWidth(9999);
      });
      expect(result.sidebarWidth).toBe(SIDEBAR_MAX_WIDTH);
    });

    it('persistSidebarWidth writes current width to localStorage', () => {
      renderHook();
      act(() => {
        result.setSidebarWidth(400);
      });
      act(() => {
        result.persistSidebarWidth();
      });
      expect(localStorage.getItem('sprout-sidebar-width')).toBe('400');
    });

    it('resetSidebarWidth sets to default and persists', () => {
      localStorage.setItem('sprout-sidebar-width', '400');
      renderHook();
      act(() => {
        result.resetSidebarWidth();
      });
      expect(result.sidebarWidth).toBe(SIDEBAR_DEFAULT_WIDTH);
      expect(localStorage.getItem('sprout-sidebar-width')).toBe(String(SIDEBAR_DEFAULT_WIDTH));
    });

    it('width survives page reload after persist', () => {
      renderHook();
      act(() => {
        result.setSidebarWidth(350);
      });
      act(() => {
        result.persistSidebarWidth();
      });
      act(() => {
        root.unmount();
      });
      renderHook();
      expect(result.sidebarWidth).toBe(350);
    });
  });

  // -----------------------------------------------------------------------
  // clampSidebarWidth utility
  // -----------------------------------------------------------------------

  describe('clampSidebarWidth', () => {
    it('returns value within range', () => {
      expect(clampSidebarWidth(300)).toBe(300);
    });

    it('clamps below minimum', () => {
      expect(clampSidebarWidth(100)).toBe(SIDEBAR_MIN_WIDTH);
    });

    it('clamps above maximum', () => {
      expect(clampSidebarWidth(800)).toBe(SIDEBAR_MAX_WIDTH);
    });

    it('exact minimum passes through', () => {
      expect(clampSidebarWidth(SIDEBAR_MIN_WIDTH)).toBe(SIDEBAR_MIN_WIDTH);
    });

    it('exact maximum passes through', () => {
      expect(clampSidebarWidth(SIDEBAR_MAX_WIDTH)).toBe(SIDEBAR_MAX_WIDTH);
    });

    it('returns default for NaN', () => {
      expect(clampSidebarWidth(NaN)).toBe(SIDEBAR_DEFAULT_WIDTH);
    });

    it('returns default for Infinity', () => {
      expect(clampSidebarWidth(Infinity)).toBe(SIDEBAR_DEFAULT_WIDTH);
    });

    it('returns default for -Infinity', () => {
      expect(clampSidebarWidth(-Infinity)).toBe(SIDEBAR_DEFAULT_WIDTH);
    });
  });

  // -----------------------------------------------------------------------
  // isTerminalExpanded persistence
  // -----------------------------------------------------------------------

  describe('isTerminalExpanded persistence', () => {
    it('reads initial value from localStorage', () => {
      localStorage.setItem('sprout-terminal-expanded', 'true');
      renderHook();
      expect(result.isTerminalExpanded).toBe(true);
    });

    it('persists via setIsTerminalExpanded', () => {
      renderHook();
      act(() => {
        result.setIsTerminalExpanded(true);
      });
      expect(result.isTerminalExpanded).toBe(true);
      expect(localStorage.getItem('sprout-terminal-expanded')).toBe('true');
    });

    it('persists false', () => {
      localStorage.setItem('sprout-terminal-expanded', 'true');
      renderHook();
      act(() => {
        result.setIsTerminalExpanded(false);
      });
      expect(result.isTerminalExpanded).toBe(false);
      expect(localStorage.getItem('sprout-terminal-expanded')).toBe('false');
    });

    it('survives page reload', () => {
      renderHook();
      act(() => {
        result.setIsTerminalExpanded(true);
      });
      act(() => {
        root.unmount();
      });
      renderHook();
      expect(result.isTerminalExpanded).toBe(true);
    });
  });

  // -----------------------------------------------------------------------
  // Mobile / overlay state (NOT persisted)
  // -----------------------------------------------------------------------

  describe('mobile and overlay state', () => {
    it('isMobile defaults to false', () => {
      renderHook();
      expect(result.isMobile).toBe(false);
    });

    it('setIsMobile changes state', () => {
      renderHook();
      act(() => {
        result.setIsMobile(true);
      });
      expect(result.isMobile).toBe(true);
    });

    it('isSidebarOpen defaults to false', () => {
      renderHook();
      expect(result.isSidebarOpen).toBe(false);
    });

    it('toggleSidebar toggles overlay open state', () => {
      renderHook();
      act(() => {
        result.toggleSidebar();
      });
      expect(result.isSidebarOpen).toBe(true);
      act(() => {
        result.toggleSidebar();
      });
      expect(result.isSidebarOpen).toBe(false);
    });

    it('closeSidebar closes overlay', () => {
      renderHook();
      act(() => {
        result.toggleSidebar();
      });
      expect(result.isSidebarOpen).toBe(true);
      act(() => {
        result.closeSidebar();
      });
      expect(result.isSidebarOpen).toBe(false);
    });
  });

  // -----------------------------------------------------------------------
  // Error handling
  // -----------------------------------------------------------------------

  describe('error handling', () => {
    it('state defaults when localStorage.getItem throws on init', () => {
      vi.spyOn(Storage.prototype, 'getItem').mockImplementation((_key: string) => {
        throw new Error('storage error');
      });
      renderHook();
      expect(result.sidebarCollapsed).toBe(false);
      expect(result.selectedSection).toBe('git');
      expect(result.sidebarWidth).toBe(SIDEBAR_DEFAULT_WIDTH);
      expect(result.isTerminalExpanded).toBe(false);
    });

    it('state still updates when localStorage.setItem throws on setSidebarCollapsed', () => {
      renderHook();
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('storage error');
      });
      act(() => {
        result.setSidebarCollapsed(true);
      });
      // State should still update even though persist failed
      expect(result.sidebarCollapsed).toBe(true);
    });

    it('state still updates when localStorage.setItem throws on setSelectedSection', () => {
      renderHook();
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('storage error');
      });
      act(() => {
        result.setSelectedSection('settings');
      });
      expect(result.selectedSection).toBe('settings');
    });

    it('state still updates when localStorage.setItem throws on setSidebarWidth + persist', () => {
      renderHook();
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('storage error');
      });
      act(() => {
        result.setSidebarWidth(400);
      });
      expect(result.sidebarWidth).toBe(400);
      act(() => {
        result.persistSidebarWidth();
      });
      // No crash — width still 400
      expect(result.sidebarWidth).toBe(400);
    });

    it('handleSidebarToggle works when localStorage.setItem throws', () => {
      renderHook();
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('storage error');
      });
      act(() => {
        result.handleSidebarToggle();
      });
      expect(result.sidebarCollapsed).toBe(true);
    });
  });
});
