import { useEffect } from 'react';
import type { RefObject } from 'react';
import type { FileTreeHandle } from '../components/SidebarFilesSection';
import { supportsSettings } from '../config/mode';
import type { SectionTab } from './useSidebarState';

interface OpenSettingsFocusEventDetail {
  focus?: 'persona' | 'provider';
}

interface UseSidebarEventHandlersParams {
  effectiveSidebarCollapsed: boolean;
  isMobile: boolean;
  onSidebarToggle?: () => void;
  onSectionChange?: (section: SectionTab) => void;
  finalOnMobileMenuToggle?: () => void;
  fileTreeRef: RefObject<FileTreeHandle | null>;
  settingsFocusTarget: 'persona' | 'provider' | null;
  setSettingsFocusTarget: (target: 'persona' | 'provider' | null) => void;
}

export function useSidebarEventHandlers({
  effectiveSidebarCollapsed,
  isMobile,
  onSidebarToggle,
  onSectionChange,
  finalOnMobileMenuToggle,
  fileTreeRef,
  settingsFocusTarget,
  setSettingsFocusTarget,
}: UseSidebarEventHandlersParams): void {
  // Ctrl+\ or Cmd+\ to toggle sidebar width (collapsed/expanded)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === '\\' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        onSidebarToggle?.();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onSidebarToggle]);

  // Open search tab on hotkey command
  useEffect(() => {
    const handleHotkey = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail?.commandId === 'open_search') {
        if (effectiveSidebarCollapsed) {
          onSectionChange?.('search');
          onSidebarToggle?.();
        } else {
          onSectionChange?.('search');
        }
      }
    };
    window.addEventListener('sprout:hotkey', handleHotkey);
    return () => window.removeEventListener('sprout:hotkey', handleHotkey);
  }, [effectiveSidebarCollapsed, onSidebarToggle, onSectionChange]);

  // Handle reveal-in-explorer event
  useEffect(() => {
    const handleReveal = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      const filePath = detail?.path;
      if (!filePath) return;

      // Switch to files tab — uncollapse if needed
      if (effectiveSidebarCollapsed) {
        onSectionChange?.('files');
        onSidebarToggle?.();
      } else {
        onSectionChange?.('files');
      }

      // If we have a file path, reveal it in the tree
      if (filePath) {
        // Give the section switch time to render, then reveal
        setTimeout(() => {
          fileTreeRef.current?.revealFile(filePath);
        }, 100);
      }
    };

    window.addEventListener('sprout:reveal-in-explorer', handleReveal);
    return () => window.removeEventListener('sprout:reveal-in-explorer', handleReveal);
  }, [effectiveSidebarCollapsed, onSidebarToggle, onSectionChange, fileTreeRef]);

  // Handle open-settings-focus event (from Status bar clicks)
  useEffect(() => {
    const handleOpenSettingsFocus = (e: Event) => {
      if (!supportsSettings) return;
      const detail = (e as CustomEvent<OpenSettingsFocusEventDetail>).detail;
      const focusTarget = detail?.focus;
      if (focusTarget !== 'persona' && focusTarget !== 'provider') return;

      // On mobile, open the sidebar first
      if (isMobile) {
        finalOnMobileMenuToggle?.();
      }

      // If collapsed, expand the sidebar
      if (effectiveSidebarCollapsed) {
        onSidebarToggle?.();
      }

      // Switch to settings tab
      onSectionChange?.('settings');
      setSettingsFocusTarget(focusTarget);
    };

    window.addEventListener('sprout:open-settings-focus', handleOpenSettingsFocus);
    return () => window.removeEventListener('sprout:open-settings-focus', handleOpenSettingsFocus);
  }, [
    effectiveSidebarCollapsed,
    isMobile,
    onSidebarToggle,
    finalOnMobileMenuToggle,
    onSectionChange,
    setSettingsFocusTarget,
  ]);

  // Focus the targeted settings control once it renders
  useEffect(() => {
    if (!settingsFocusTarget || !supportsSettings) return;

    // Brief delay to allow the settings section to mount
    const timerId = setTimeout(() => {
      if (settingsFocusTarget === 'persona') {
        document.getElementById('persona-select')?.focus();
      } else if (settingsFocusTarget === 'provider') {
        document.getElementById('provider-select')?.focus();
      }
      setSettingsFocusTarget(null);
    }, 80);

    return () => clearTimeout(timerId);
  }, [settingsFocusTarget, setSettingsFocusTarget]);
}
