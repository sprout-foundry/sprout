import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { clientFetch } from '../../services/clientSession';
import { supportsWorkspaceSwitching } from '../../config/mode';
import { normalizePath } from './pathUtils';
import type { WorkspaceDirectory, SwitchingState, SSHFailureState, RemoteWorkspaceContext } from './types';
import { MAX_RECENT_WORKSPACES, MAX_SUGGESTIONS } from './types';

export interface UseWorkspaceSuggestionsProps {
  isConnected: boolean;
  workspaceRoot: string;
  daemonRoot: string;
  remoteContext: RemoteWorkspaceContext | null;
  switchingState: SwitchingState;
  sshFailure: SSHFailureState | null;
  isLoading: boolean;
  recentWorkspaces: string[];
  remoteRecentWorkspaces: Record<string, string[]>;
  sshFavoriteWorkspaces: Record<string, string[]>;
  submitWorkspaceChange: (targetPath: string) => Promise<void>;
  setSwitchingState: React.Dispatch<React.SetStateAction<SwitchingState>>;
  setSshFailure: (s: SSHFailureState | null) => void;
  sidebarCollapsed: boolean;
  isOpen: boolean;
  isSshPanelOpen: boolean;
  setIsOpen: (v: boolean | ((p: boolean) => boolean)) => void;
  setIsSshPanelOpen: (v: boolean | ((p: boolean) => boolean)) => void;
}

export interface UseWorkspaceSuggestionsResult {
  inputValue: string;
  selectedIndex: number;
  suggestions: WorkspaceDirectory[];
  suggestionsLoading: boolean;
  suggestionsError: string | null;
  recentWorkspaceItems: string[];
  remoteHostFavorites: string[];
  totalWorkspaceRows: number;
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

export function useWorkspaceSuggestions({
  isConnected,
  workspaceRoot,
  daemonRoot: _daemonRoot,
  remoteContext,
  switchingState: _switchingState,
  sshFailure: _sshFailure,
  isLoading: _isLoading,
  recentWorkspaces,
  remoteRecentWorkspaces,
  sshFavoriteWorkspaces,
  submitWorkspaceChange,
  setSwitchingState,
  setSshFailure,
  sidebarCollapsed: _sidebarCollapsed,
  isOpen,
  isSshPanelOpen,
  setIsOpen,
  setIsSshPanelOpen,
}: UseWorkspaceSuggestionsProps): UseWorkspaceSuggestionsResult {
  const [inputValue, setInputValue] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [suggestions, setSuggestions] = useState<WorkspaceDirectory[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(false);
  const [suggestionsError, setSuggestionsError] = useState<string | null>(null);

  const popoverRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const sshBtnRef = useRef<HTMLButtonElement>(null);
  const sshPanelRef = useRef<HTMLDivElement>(null);
  const pathInputRef = useRef<HTMLInputElement>(null);

  // Reset input when workspace changes and popover is closed
  useEffect(() => {
    if (!isOpen) setInputValue(workspaceRoot);
  }, [workspaceRoot, isOpen]);

  // Derived: recent workspace items
  const recentWorkspaceItems = useMemo(() => {
    const source = remoteContext?.hostAlias ? remoteRecentWorkspaces[remoteContext.hostAlias] || [] : recentWorkspaces;
    return source.filter((p) => p !== workspaceRoot).slice(0, MAX_RECENT_WORKSPACES);
  }, [recentWorkspaces, remoteContext?.hostAlias, remoteRecentWorkspaces, workspaceRoot]);

  // Derived: remote host favorites
  const remoteHostFavorites = useMemo(() => {
    if (!remoteContext?.hostAlias) return [];
    return (sshFavoriteWorkspaces[remoteContext.hostAlias] || []).filter((p) => p !== workspaceRoot);
  }, [remoteContext?.hostAlias, sshFavoriteWorkspaces, workspaceRoot]);

  const showText = !_sidebarCollapsed;
  const totalWorkspaceRows = suggestions.length + recentWorkspaceItems.length;

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
    const endsWithSlash = inputValue.trim().endsWith('/');
    const parentPath = endsWithSlash
      ? normalizedInput
      : normalizePath(normalizedInput.split('/').slice(0, -1).join('/')) || '/';
    const prefix = endsWithSlash ? '' : normalizedInput.split('/').filter(Boolean).pop() || '';
    let cancelled = false;
    (async () => {
      setSuggestionsLoading(true);
      setSuggestionsError(null);
      try {
        const response = await clientFetch(`/api/workspace/browse?path=${encodeURIComponent(parentPath)}`);
        if (!response.ok) {
          const text = await response.text();
          throw new Error(text || 'Failed to fetch matching folders');
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
          .filter((e: WorkspaceDirectory) => !prefix || e.name.toLowerCase().startsWith(prefix.toLowerCase()))
          .sort((a: WorkspaceDirectory, b: WorkspaceDirectory) => a.name.localeCompare(b.name))
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
  }, [inputValue, isConnected, isOpen]);

  // Clamp selected index
  useEffect(() => {
    const max = suggestions.length + recentWorkspaceItems.length - 1;
    if (selectedIndex > max) setSelectedIndex(-1);
  }, [recentWorkspaceItems.length, selectedIndex, suggestions.length]);

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
    if (workspaceRoot) setInputValue(workspaceRoot);
  }, [workspaceRoot, setSwitchingState, setSshFailure, setIsSshPanelOpen, setIsOpen]);

  const togglePopover = useCallback(() => {
    setIsOpen((prev) => {
      const next = !prev;
      if (next) {
        setSelectedIndex(-1);
        setInputValue(workspaceRoot);
        setSwitchingState({ isSwitching: false, error: null, status: null });
        setSuggestionsError(null);
        setSshFailure(null);
      }
      return next;
    });
    setIsSshPanelOpen(false);
  }, [workspaceRoot, setSwitchingState, setSshFailure, setIsOpen, setIsSshPanelOpen]);

  const toggleSshPanel = useCallback(() => {
    setIsSshPanelOpen((prev) => !prev);
    setIsOpen(false);
    setSwitchingState({ isSwitching: false, error: null, status: null });
    setSshFailure(null);
  }, [setSwitchingState, setSshFailure, setIsOpen, setIsSshPanelOpen]);

  const handleInputSubmit = useCallback(async () => {
    if (selectedIndex >= 0 && selectedIndex < suggestions.length) {
      await submitWorkspaceChange(suggestions[selectedIndex].path);
      return;
    }
    const ri = selectedIndex - suggestions.length;
    if (ri >= 0 && ri < recentWorkspaceItems.length) {
      await submitWorkspaceChange(recentWorkspaceItems[ri]);
      return;
    }
    await submitWorkspaceChange(inputValue);
  }, [inputValue, recentWorkspaceItems, selectedIndex, submitWorkspaceChange, suggestions]);

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
          if (totalWorkspaceRows === 0) return;
          setSelectedIndex((p) => (p < totalWorkspaceRows - 1 ? p + 1 : 0));
          break;
        case 'ArrowUp':
          event.preventDefault();
          if (totalWorkspaceRows === 0) return;
          setSelectedIndex((p) => (p <= 0 ? totalWorkspaceRows - 1 : p - 1));
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
  }, [totalWorkspaceRows, isOpen, closePopover]);

  // sprout:open-workspace-switcher event — disabled in cloud mode
  useEffect(() => {
    if (!supportsWorkspaceSwitching) return;
    const handler = () => {
      openPopover();
    };
    window.addEventListener('sprout:open-workspace-switcher', handler);
    return () => window.removeEventListener('sprout:open-workspace-switcher', handler);
  }, [openPopover, supportsWorkspaceSwitching]);

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
    recentWorkspaceItems,
    remoteHostFavorites,
    totalWorkspaceRows,
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
