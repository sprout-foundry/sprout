import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useHostCapabilities } from '../../host';
import { clientFetch } from '../../services/clientSession';
import { getBrowseTarget, normalizePath } from './pathUtils';
import type { ProjectFolderDirectory, SwitchingState, SSHFailureState, RemoteProjectFolderContext } from './types';
import { MAX_RECENT_PROJECT_FOLDERS, MAX_SUGGESTIONS } from './types';

export interface UseProjectFolderSuggestionsProps {
  isConnected: boolean;
  projectFolder: string;
  daemonRoot: string;
  remoteContext: RemoteProjectFolderContext | null;
  switchingState: SwitchingState;
  sshFailure: SSHFailureState | null;
  isLoading: boolean;
  recentProjectFolders: string[];
  remoteRecentProjectFolders: Record<string, string[]>;
  sshFavoriteProjectFolders: Record<string, string[]>;
  submitProjectFolderChange: (targetPath: string) => Promise<void>;
  setSwitchingState: React.Dispatch<React.SetStateAction<SwitchingState>>;
  setSshFailure: (s: SSHFailureState | null) => void;
  sidebarCollapsed: boolean;
  isOpen: boolean;
  isSshPanelOpen: boolean;
  setIsOpen: (v: boolean | ((p: boolean) => boolean)) => void;
  setIsSshPanelOpen: (v: boolean | ((p: boolean) => boolean)) => void;
}

export interface UseProjectFolderSuggestionsResult {
  inputValue: string;
  selectedIndex: number;
  suggestions: ProjectFolderDirectory[];
  suggestionsLoading: boolean;
  suggestionsError: string | null;
  recentProjectFolderItems: string[];
  remoteHostFavorites: string[];
  totalProjectFolderRows: number;
  showText: boolean;
  setInputValue: (v: string) => void;
  setSelectedIndex: (i: number | ((p: number) => number)) => void;
  togglePopover: () => void;
  toggleSshPanel: () => void;
  openPopover: () => void;
  closePopover: () => void;
  handleInputSubmit: () => Promise<void>;
  popoverRef: React.RefObject<HTMLDivElement>;
  triggerRef: React.RefObject<HTMLButtonElement>;
  sshBtnRef: React.RefObject<HTMLButtonElement>;
  sshPanelRef: React.RefObject<HTMLDivElement>;
  pathInputRef: React.RefObject<HTMLInputElement>;
}

export function useProjectFolderSuggestions({
  isConnected,
  projectFolder,
  daemonRoot,
  remoteContext,
  switchingState: _switchingState,
  sshFailure: _sshFailure,
  isLoading: _isLoading,
  recentProjectFolders,
  remoteRecentProjectFolders,
  sshFavoriteProjectFolders,
  submitProjectFolderChange,
  setSwitchingState,
  setSshFailure,
  sidebarCollapsed: _sidebarCollapsed,
  isOpen,
  isSshPanelOpen,
  setIsOpen,
  setIsSshPanelOpen,
}: UseProjectFolderSuggestionsProps): UseProjectFolderSuggestionsResult {
  const { projectFolderSwitching: supportsProjectFolderSwitching } = useHostCapabilities();
  const [inputValue, setInputValue] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [suggestions, setSuggestions] = useState<ProjectFolderDirectory[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(false);
  const [suggestionsError, setSuggestionsError] = useState<string | null>(null);

  const popoverRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const sshBtnRef = useRef<HTMLButtonElement>(null);
  const sshPanelRef = useRef<HTMLDivElement>(null);
  const pathInputRef = useRef<HTMLInputElement>(null);

  // Reset input when project folder changes and popover is closed
  useEffect(() => {
    if (!isOpen) setInputValue(projectFolder);
  }, [projectFolder, isOpen]);

  // Derived: recent project folder items
  const recentProjectFolderItems = useMemo(() => {
    const source = remoteContext?.hostAlias
      ? remoteRecentProjectFolders[remoteContext.hostAlias] || []
      : recentProjectFolders;
    return source.filter((p) => p !== projectFolder).slice(0, MAX_RECENT_PROJECT_FOLDERS);
  }, [recentProjectFolders, remoteContext?.hostAlias, remoteRecentProjectFolders, projectFolder]);

  // Derived: remote host favorites
  const remoteHostFavorites = useMemo(() => {
    if (!remoteContext?.hostAlias) return [];
    return (sshFavoriteProjectFolders[remoteContext.hostAlias] || []).filter((p) => p !== projectFolder);
  }, [remoteContext?.hostAlias, sshFavoriteProjectFolders, projectFolder]);

  const showText = !_sidebarCollapsed;
  const totalProjectFolderRows = suggestions.length + recentProjectFolderItems.length;

  // Suggestions loading
  useEffect(() => {
    if (!isOpen || !isConnected) return;
    const normalizedInput = normalizePath(inputValue);
    if (!normalizedInput) {
      setSuggestions([]);
      setSuggestionsError(null);
      setSuggestionsLoading(false);
      return;
    }
    const target = getBrowseTarget(inputValue, daemonRoot);
    if (!target) {
      setSuggestions([]);
      setSuggestionsError(null);
      setSuggestionsLoading(false);
      return;
    }
    let cancelled = false;
    (async () => {
      setSuggestionsLoading(true);
      setSuggestionsError(null);
      try {
        const response = await clientFetch(`/api/project-folder/browse?path=${encodeURIComponent(target.browsePath)}`);
        if (!response.ok) {
          const body = await response.json().catch(() => null);
          const message =
            body && typeof body === 'object'
              ? String((body as Record<string, unknown>).error ?? (body as Record<string, unknown>).message ?? '')
              : '';
          throw new Error(message || 'Failed to fetch matching folders');
        }
        const ct = response.headers.get('Content-Type') || '';
        if (!ct.includes('application/json')) {
          setSuggestions([]);
          setSuggestionsLoading(false);
          return;
        }
        const data = await response.json();
        if (cancelled) return;
        const next = (data.files || [])
          .filter(
            (f: { type: string; name: string; path: string }) =>
              f.type === 'directory' && !String(f.name || '').startsWith('.'),
          )
          .map((f: { type: string; name: string; path: string }) => ({
            name: String(f.name),
            path: normalizePath(String(f.path)),
          }))
          .filter(
            (e: ProjectFolderDirectory) =>
              !target.prefix || e.name.toLowerCase().startsWith(target.prefix.toLowerCase()),
          )
          .sort((a: ProjectFolderDirectory, b: ProjectFolderDirectory) => a.name.localeCompare(b.name))
          .slice(0, MAX_SUGGESTIONS);
        setSuggestions(next);
        setSuggestionsLoading(false);
      } catch (error) {
        if (cancelled) return;
        setSuggestions([]);
        setSuggestionsLoading(false);
        setSuggestionsError(error instanceof Error ? error.message : 'Failed to fetch matching folders');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [inputValue, daemonRoot, isConnected, isOpen]);

  // Clamp selected index
  useEffect(() => {
    const max = suggestions.length + recentProjectFolderItems.length - 1;
    if (selectedIndex > max) setSelectedIndex(-1);
  }, [recentProjectFolderItems.length, selectedIndex, suggestions.length]);

  // Focus input on open
  useEffect(() => {
    if (!isOpen || !pathInputRef.current) return;
    const timer = window.setTimeout(() => {
      pathInputRef.current?.focus();
      pathInputRef.current?.select();
    }, 50);
    return () => window.clearTimeout(timer);
  }, [isOpen]);

  // closePopover uses external setter
  const closePopover = useCallback(() => {
    setIsOpen(false);
    setSelectedIndex(-1);
  }, [setIsOpen]);

  const openPopover = useCallback(() => {
    setIsSshPanelOpen(false);
    setIsOpen(true);
    setSelectedIndex(-1);
    setSwitchingState({ isSwitching: false, error: null, status: null });
    setSuggestionsError(null);
    setSshFailure(null);
    if (projectFolder) setInputValue(projectFolder);
  }, [projectFolder, setSwitchingState, setSshFailure, setIsSshPanelOpen, setIsOpen]);

  const togglePopover = useCallback(() => {
    setIsOpen((prev) => {
      const next = !prev;
      if (next) {
        setSelectedIndex(-1);
        setInputValue(projectFolder);
        setSwitchingState({ isSwitching: false, error: null, status: null });
        setSuggestionsError(null);
        setSshFailure(null);
      }
      return next;
    });
    setIsSshPanelOpen(false);
  }, [projectFolder, setSwitchingState, setSshFailure, setIsOpen, setIsSshPanelOpen]);

  const toggleSshPanel = useCallback(() => {
    setIsSshPanelOpen((prev) => !prev);
    setIsOpen(false);
    setSwitchingState({ isSwitching: false, error: null, status: null });
    setSshFailure(null);
  }, [setSwitchingState, setSshFailure, setIsOpen, setIsSshPanelOpen]);

  const handleInputSubmit = useCallback(async () => {
    if (selectedIndex >= 0 && selectedIndex < suggestions.length) {
      await submitProjectFolderChange(suggestions[selectedIndex].path);
      return;
    }
    const ri = selectedIndex - suggestions.length;
    if (ri >= 0 && ri < recentProjectFolderItems.length) {
      await submitProjectFolderChange(recentProjectFolderItems[ri]);
      return;
    }
    await submitProjectFolderChange(inputValue);
  }, [inputValue, recentProjectFolderItems, selectedIndex, submitProjectFolderChange, suggestions]);

  // Store handleInputSubmit in a ref so keyboard handler doesn't re-bind constantly
  const handleInputSubmitRef = useRef(handleInputSubmit);
  handleInputSubmitRef.current = handleInputSubmit;

  // Keyboard handling
  useEffect(() => {
    if (!isOpen) return undefined;
    const handler = (event: KeyboardEvent) => {
      if (document.activeElement !== pathInputRef.current) return;
      switch (event.key) {
        case 'Escape':
          event.preventDefault();
          closePopover();
          break;
        case 'ArrowDown':
          event.preventDefault();
          if (totalProjectFolderRows === 0) return;
          setSelectedIndex((p) => (p < totalProjectFolderRows - 1 ? p + 1 : 0));
          break;
        case 'ArrowUp':
          event.preventDefault();
          if (totalProjectFolderRows === 0) return;
          setSelectedIndex((p) => (p <= 0 ? totalProjectFolderRows - 1 : p - 1));
          break;
        case 'Enter':
          event.preventDefault();
          handleInputSubmitRef.current();
          break;
      }
    };
    document.addEventListener('keydown', handler);
    return () => {
      document.removeEventListener('keydown', handler);
    };
  }, [totalProjectFolderRows, isOpen, closePopover]);

  // sprout:open-project-folder-switcher event — disabled in cloud mode
  useEffect(() => {
    if (!supportsProjectFolderSwitching) return;
    const handler = () => {
      openPopover();
    };
    window.addEventListener('sprout:open-project-folder-switcher', handler);
    return () => window.removeEventListener('sprout:open-project-folder-switcher', handler);
  }, [openPopover, supportsProjectFolderSwitching]);

  // Click outside
  useEffect(() => {
    if (!isOpen && !isSshPanelOpen) return undefined;
    const handler = (event: MouseEvent) => {
      const target = event.target as Node;
      const inWP = popoverRef.current?.contains(target);
      const inTrigger = triggerRef.current?.contains(target);
      const inSP = sshPanelRef.current?.contains(target);
      const inSBtn = sshBtnRef.current?.contains(target);
      if (!inWP && !inTrigger) {
        if (isOpen) {
          setIsOpen(false);
          setSelectedIndex(-1);
        }
        if (!inSP && !inSBtn) {
          setSwitchingState({ isSwitching: false, error: null, status: null });
          setSshFailure(null);
        }
      }
      if (!inSP && !inSBtn && isSshPanelOpen) setIsSshPanelOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => {
      document.removeEventListener('mousedown', handler);
    };
  }, [isOpen, isSshPanelOpen, setSwitchingState, setSshFailure, setIsOpen, setIsSshPanelOpen]);

  return {
    inputValue,
    selectedIndex,
    suggestions,
    suggestionsLoading,
    suggestionsError,
    recentProjectFolderItems,
    remoteHostFavorites,
    totalProjectFolderRows,
    showText,
    setInputValue,
    setSelectedIndex,
    togglePopover,
    toggleSshPanel,
    openPopover,
    closePopover,
    handleInputSubmit,
    popoverRef,
    triggerRef,
    sshBtnRef,
    sshPanelRef,
    pathInputRef,
  };
}
