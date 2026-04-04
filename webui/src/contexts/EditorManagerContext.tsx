import React, { createContext, useContext, useState, useEffect, useRef, type ReactNode } from 'react';
import { type EditorBuffer, type EditorPane, type PaneLayout, type PaneSize } from '../types/editor';
import { readStorageItem, PANE_LAYOUT_STORAGE_KEY, PANE_SIZES_STORAGE_KEY } from '../services/layoutPersistence';
import { useBufferMutations } from '../hooks/useBufferMutations';
import { useBufferPersistence } from '../hooks/useBufferPersistence';
import { useTabManagement } from '../hooks/useTabManagement';
import { useTabOpen } from '../hooks/useTabOpen';
import { usePaneManagement } from '../hooks/usePaneManagement';
import { useLayoutPersistence } from '../hooks/useLayoutPersistence';
import { useExternalFileWatcher } from '../hooks/useExternalFileWatcher';
import { useAutoReloadCleanBuffers } from '../hooks/useAutoReloadCleanBuffers';

// ---------------------------------------------------------------------------
// Context interface & hook — unchanged public API
// ---------------------------------------------------------------------------

interface EditorManagerContextValue {
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
  activeBufferId: string | null;
  isAutoSaveEnabled: boolean;
  autoSaveInterval: number;
  isLinkedScrollEnabled: boolean;
  paneSizes: PaneSize;

  openFile: (file: any) => string;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, any>;
  }) => string;
  closeBuffer: (bufferId: string) => void;
  closeAllBuffers: () => void;
  closeOtherBuffers: (bufferId: string) => void;
  reorderBuffers: (sourceBufferId: string, targetBufferId: string) => void;
  moveBufferToPane: (bufferId: string, paneId: string) => void;
  closePane: (paneId: string) => void;
  switchPane: (paneId: string) => void;
  switchToBuffer: (bufferId: string) => void;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => string | null;
  splitIntoGrid: () => string[];
  closeSplit: () => void;
  setPaneLayout: (layout: PaneLayout) => void;
  updateBufferContent: (bufferId: string, content: string) => void;
  updateBufferCursor: (bufferId: string, position: { line: number; column: number }) => void;
  updateBufferScroll: (bufferId: string, position: { top: number; left: number }) => void;
  updateBufferMetadata: (bufferId: string, updates: Record<string, any>) => void;
  updateBufferTitle: (bufferId: string, title: string) => void;
  saveBuffer: (bufferId: string) => Promise<void>;
  setBufferModified: (bufferId: string, isModified: boolean) => void;
  setBufferOriginalContent: (bufferId: string, originalContent: string) => void;
  revertBufferToOriginal: (bufferId: string) => void;
  setBufferExternallyModified: (bufferId: string, diskContent: string, mtime?: number) => void;
  clearBufferExternallyModified: (bufferId: string) => void;
  reloadBufferFromDisk: (bufferId: string, diskContent: string, mtime?: number) => void;
  saveAllBuffers: () => Promise<void>;
  updatePaneSize: (paneId: string, size: number) => void;
  setBufferLanguageOverride: (bufferId: string, languageId: string | null) => void;
  toggleLinkedScroll: () => void;
  restoreLayout: () => void;
}

const EditorManagerContext = createContext<EditorManagerContextValue | null>(null);

export const useEditorManager = () => {
  const context = useContext(EditorManagerContext);
  if (!context) {
    throw new Error('useEditorManager must be used within EditorManagerProvider');
  }
  return context;
};

// ---------------------------------------------------------------------------
// Provider — thin orchestrator
// ---------------------------------------------------------------------------

interface EditorManagerProviderProps {
  children: ReactNode;
}

export const EditorManagerProvider: React.FC<EditorManagerProviderProps> = ({ children }) => {
  // ---------------------------------------------------------------------------
  // Base state
  // ---------------------------------------------------------------------------

  const [buffers, setBuffers] = useState<Map<string, EditorBuffer>>(() => {
    const chatBuffer: EditorBuffer = {
      id: 'buffer-chat',
      kind: 'chat',
      file: { name: 'Chat', path: '__workspace/chat', isDir: false, size: 0, modified: 0, ext: '.chat' },
      content: '',
      originalContent: '',
      cursorPosition: { line: 0, column: 0 },
      scrollPosition: { top: 0, left: 0 },
      isModified: false,
      isActive: true,
      paneId: 'pane-1',
      isPinned: true,
      isClosable: false,
      metadata: { chatId: null as string | null },
    };
    return new Map([[chatBuffer.id, chatBuffer]]);
  });

  const initialLayout: PaneLayout = (() => {
    const stored = readStorageItem(PANE_LAYOUT_STORAGE_KEY);
    if (stored === 'split-vertical' || stored === 'split-horizontal' || stored === 'split-grid') return stored;
    return 'single';
  })();

  const [paneLayout, setPaneLayoutState] = useState<PaneLayout>(initialLayout);

  const [panes, setPanes] = useState<EditorPane[]>(() => {
    const primary: EditorPane = { id: 'pane-1', bufferId: 'buffer-chat', isActive: true, position: 'primary' };
    if (initialLayout === 'split-vertical' || initialLayout === 'split-horizontal') {
      return [primary, { id: 'pane-2', bufferId: null, isActive: false, position: 'secondary' as const }];
    }
    if (initialLayout === 'split-grid') {
      return [
        primary,
        { id: 'pane-2', bufferId: null, isActive: false, position: 'secondary' as const },
        { id: 'pane-3', bufferId: null, isActive: false, position: 'tertiary' as const },
        { id: 'pane-4', bufferId: null, isActive: false, position: 'quaternary' as const },
      ];
    }
    return [primary];
  });

  const [activePaneId, setActivePaneId] = useState<string | null>('pane-1');
  const [activeBufferId, setActiveBufferId] = useState<string | null>('buffer-chat');
  const [isAutoSaveEnabled] = useState(true);
  const [autoSaveInterval] = useState(30000);
  const [isLinkedScrollEnabled, setIsLinkedScrollEnabled] = useState(false);

  const [paneSizes, setPaneSizes] = useState<PaneSize>(() => {
    const STABLE = ['pane-1', 'pane-2', 'pane-3', 'pane-4'];
    const isSplit = initialLayout === 'split-vertical' || initialLayout === 'split-horizontal';
    const isGrid = initialLayout === 'split-grid';
    const defaults: PaneSize = isGrid
      ? { 'grid:col': 50, 'grid:row': 50 }
      : isSplit
        ? { 'pane-1': 50, 'pane-2': 50 }
        : { 'pane-1': 100 };

    const stored = readStorageItem(PANE_SIZES_STORAGE_KEY);
    if (!stored) return defaults;
    try {
      const parsed: PaneSize = JSON.parse(stored);
      const filtered: PaneSize = {};
      for (const key of Object.keys(parsed)) {
        if (
          (STABLE.includes(key) || !key.startsWith('pane-')) &&
          typeof parsed[key] === 'number' &&
          isFinite(parsed[key])
        ) {
          filtered[key] = Math.max(10, Math.min(90, parsed[key]));
        }
      }
      if (isGrid) {
        filtered['grid:col'] = filtered['grid:col'] ?? 50;
        filtered['grid:row'] = filtered['grid:row'] ?? 50;
        for (const k of STABLE) delete filtered[k];
      } else if (isSplit) {
        filtered['pane-1'] = filtered['pane-1'] ?? 50;
        filtered['pane-2'] = filtered['pane-2'] ?? 50;
        delete filtered['grid:col'];
        delete filtered['grid:row'];
      } else {
        filtered['pane-1'] = 100;
        delete filtered['grid:col'];
        delete filtered['grid:row'];
      }
      return filtered;
    } catch {
      /* JSON parse error */
    }
    return defaults;
  });

  // ---------------------------------------------------------------------------
  // Refs (avoid stale closures in callbacks)
  // ---------------------------------------------------------------------------

  const buffersRef = useRef(buffers);
  buffersRef.current = buffers; // Synchronous update — event handlers always see latest state

  const activePaneIdRef = useRef(activePaneId);
  useEffect(() => {
    activePaneIdRef.current = activePaneId;
  }, [activePaneId]);

  const panesRef = useRef(panes);
  useEffect(() => {
    panesRef.current = panes;
  }, [panes]);

  // ---------------------------------------------------------------------------
  // Effects that stay in the orchestrator
  // ---------------------------------------------------------------------------

  // Auto-sync layout to 'single' when panes are reduced to 1
  useEffect(() => {
    if (panes.length === 1 && paneLayout !== 'single') setPaneLayoutState('single');
  }, [panes.length, paneLayout]);

  // ---------------------------------------------------------------------------
  // Extracted hooks
  // ---------------------------------------------------------------------------

  // 1. Buffer mutations (pure setBuffers callbacks)
  const mutations = useBufferMutations(setBuffers);

  // 2. Buffer persistence (save / saveAll)
  const { saveBuffer, saveAllBuffers } = useBufferPersistence({ buffersRef, setBuffers });

  // 3. Tab management (activate, switch, close, reorder, move)
  const tab = useTabManagement({
    buffersRef,
    activePaneIdRef,
    setBuffers,
    setPanes,
    setActiveBufferId,
    setActivePaneId,
    activePaneId,
    activeBufferId,
    isAutoSaveEnabled,
    saveBuffer,
  });

  // 4. Tab open (openFile, openWorkspaceBuffer — depends on activateBuffer & switchToBuffer)
  const tabOpen = useTabOpen({
    buffersRef,
    activePaneIdRef,
    panesRef,
    setBuffers,
    setPanes,
    setActiveBufferId,
    setActivePaneId,
    activePaneId,
    activateBuffer: tab.activateBuffer,
    switchToBuffer: tab.switchToBuffer,
  });

  // 5. Pane management (split, close, switch, resize)
  const paneMgmt = usePaneManagement({
    panes,
    activePaneId,
    activeBufferId,
    closeBuffer: tab.closeBuffer,
    setBuffers,
    setPanes,
    setPaneLayoutState,
    setActivePaneId,
    setActiveBufferId,
    setPaneSizes,
    setIsLinkedScrollEnabled,
  });

  // 6. Layout persistence (restore / save snapshot / beforeunload / cleanup)
  const layoutPersist = useLayoutPersistence({
    buffersRef,
    panesRef,
    buffers,
    panes,
    setBuffers,
    setPanes,
    activePaneId,
    activeBufferId,
    setActivePaneId,
    setActiveBufferId,
    paneLayout,
    paneSizes,
  });

  // 7. External file change watcher (polls backend for file changes on disk)
  useExternalFileWatcher({ buffers });

  // 8. Auto-reload clean buffers when they change on disk
  useAutoReloadCleanBuffers({ buffersRef, reloadBufferFromDisk: mutations.reloadBufferFromDisk });

  // 9. Auto-save interval (stays in orchestrator — ties persistence + state together)
  useEffect(() => {
    if (!isAutoSaveEnabled) return;
    const id = setInterval(() => {
      saveAllBuffers();
    }, autoSaveInterval);
    return () => clearInterval(id);
  }, [isAutoSaveEnabled, autoSaveInterval, saveAllBuffers]);

  // ---------------------------------------------------------------------------
  // Context value — identical public API
  // ---------------------------------------------------------------------------

  const value: EditorManagerContextValue = {
    buffers,
    panes,
    paneLayout,
    activePaneId,
    activeBufferId,
    isAutoSaveEnabled,
    autoSaveInterval,
    isLinkedScrollEnabled,
    paneSizes,
    openFile: tabOpen.openFile,
    openWorkspaceBuffer: tabOpen.openWorkspaceBuffer,
    closeBuffer: tab.closeBuffer,
    closeAllBuffers: tab.closeAllBuffers,
    closeOtherBuffers: tab.closeOtherBuffers,
    reorderBuffers: tab.reorderBuffers,
    moveBufferToPane: tab.moveBufferToPane,
    switchToBuffer: tab.switchToBuffer,
    closePane: paneMgmt.closePane,
    switchPane: paneMgmt.switchPane,
    splitPane: paneMgmt.splitPane,
    splitIntoGrid: paneMgmt.splitIntoGrid,
    closeSplit: paneMgmt.closeSplit,
    setPaneLayout: paneMgmt.setPaneLayout,
    updatePaneSize: paneMgmt.updatePaneSize,
    toggleLinkedScroll: paneMgmt.toggleLinkedScroll,
    updateBufferContent: mutations.updateBufferContent,
    updateBufferCursor: mutations.updateBufferCursor,
    updateBufferScroll: mutations.updateBufferScroll,
    updateBufferMetadata: mutations.updateBufferMetadata,
    updateBufferTitle: mutations.updateBufferTitle,
    setBufferModified: mutations.setBufferModified,
    setBufferOriginalContent: mutations.setBufferOriginalContent,
    setBufferLanguageOverride: mutations.setBufferLanguageOverride,
    revertBufferToOriginal: mutations.revertBufferToOriginal,
    setBufferExternallyModified: mutations.setBufferExternallyModified,
    clearBufferExternallyModified: mutations.clearBufferExternallyModified,
    reloadBufferFromDisk: mutations.reloadBufferFromDisk,
    saveBuffer,
    saveAllBuffers,
    restoreLayout: layoutPersist.restoreLayout,
  };

  return <EditorManagerContext.Provider value={value}>{children}</EditorManagerContext.Provider>;
};
