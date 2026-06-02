import React, { createContext, useContext, type ReactNode } from 'react';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import type { EditorBuffer, EditorPane, PaneLayout, PaneSize } from '../types/editor';

// ---------------------------------------------------------------------------
// Import sub-contexts
// ---------------------------------------------------------------------------

import { BufferManagerProvider, useBufferManager as useBufferContext } from './BufferManagerContext';
import type { PaneBridge } from './BufferManagerContext';
import { EditorSettingsProvider, useEditorSettings as useSettings } from './EditorSettingsContext';
import { PaneManagerProvider, usePaneManager as usePaneContext } from './PaneManagerContext';

// ---------------------------------------------------------------------------
// Re-export constants and hooks for backward compatibility
// ---------------------------------------------------------------------------

export { MAX_PANES, DEFAULT_MAX_PANES, useEditorSettings, EditorSettingsProvider } from './EditorSettingsContext';
export { MIN_PANE_WIDTH_PERCENT, normalizePaneSize, usePaneManager, PaneManagerProvider } from './PaneManagerContext';
export { useBufferManager, type PaneBridge, BufferManagerProvider } from './BufferManagerContext';

// ---------------------------------------------------------------------------
// Combined Interface (matches original EditorManagerContextValue)
// ---------------------------------------------------------------------------

interface EditorManagerContextValue {
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
  activeBufferId: string | null;
  isAutoSaveEnabled: boolean;
  setAutoSaveEnabled: (enabled: boolean) => void;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  setWhitespaceRenderingMode: (mode: WhitespaceRenderingMode) => void;
  autoSaveInterval: number;
  paneSizes: PaneSize;
  isFormatOnSaveEnabled: boolean;
  setFormatOnSaveEnabled: (enabled: boolean) => void;
  maxPanes: number;
  setMaxPanes: (n: number) => void;
  openFile: (file: {
    name: string;
    path: string;
    isDir: boolean;
    size: number;
    modified: number;
    ext?: string;
  }) => string;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file' | 'compare';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, unknown>;
  }) => string;
  openCompareBuffer: (options: {
    originalContent: string;
    modifiedContent: string;
    fileName: string;
    aLabel?: string;
    bLabel?: string;
    title?: string;
  }) => string;
  closeBuffer: (bufferId: string) => void | Promise<void>;
  reorderBuffers: (sourceBufferId: string, targetBufferId: string) => void;
  moveBufferToPane: (bufferId: string, paneId: string) => void;
  closePane: (paneId: string) => void;
  switchPane: (paneId: string) => void;
  switchToBuffer: (bufferId: string) => void;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => string | null;
  closeSplit: () => void;
  setPaneLayout: (layout: PaneLayout) => void;
  updateBufferContent: (bufferId: string, content: string) => void;
  updateBufferCursor: (bufferId: string, position: { line: number; column: number }) => void;
  updateBufferScroll: (bufferId: string, position: { top: number; left: number }) => void;
  updateBufferMetadata: (bufferId: string, updates: Record<string, unknown>) => void;
  updateBufferTitle: (bufferId: string, title: string) => void;
  saveBuffer: (bufferId: string) => Promise<{ mod_time?: number; formattedContent?: string } | void>;
  setBufferModified: (bufferId: string, isModified: boolean) => void;
  setBufferOriginalContent: (bufferId: string, originalContent: string) => void;
  setBufferExternallyModified: (bufferId: string, diskContent: string, mtime?: number) => void;
  clearBufferExternallyModified: (bufferId: string) => void;
  setBufferLanguageOverride: (bufferId: string, languageId: string | null) => void;
  saveAllBuffers: () => Promise<void>;
  updatePaneSize: (paneId: string, size: number) => void;
  isLinkedScrollEnabled: boolean;
  toggleLinkedScroll: () => void;
  toggleBufferPin: (bufferId: string) => void;
  setBufferPinned: (bufferId: string, isPinned: boolean) => void;
  setBufferClosable: (bufferId: string, isClosable: boolean) => void;
  reloadBufferFromDisk: (bufferId: string, diskContent: string, mtime?: number) => void;
}

const EditorManagerContext = createContext<EditorManagerContextValue | null>(null);

export const useEditorManager = () => {
  const context = useContext(EditorManagerContext);
  if (!context) {
    throw new Error('useEditorManager must be used within EditorManagerProvider');
  }
  return context;
};

interface EditorManagerProviderProps {
  children: ReactNode;
}

export const EditorManagerProvider: React.FC<EditorManagerProviderProps> = ({ children }) => {
  return (
    <EditorSettingsProvider>
      <SettingsToPaneProvider>{children}</SettingsToPaneProvider>
    </EditorSettingsProvider>
  );
};

// Internal component to bridge Settings -> Pane -> Buffer
const SettingsToPaneProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const settings = useSettings();

  return <PaneToBufferProvider settings={settings}>{children}</PaneToBufferProvider>;
};

const PaneToBufferProvider: React.FC<{
  children: ReactNode;
  settings: {
    maxPanes: number;
    isAutoSaveEnabled: boolean;
    isFormatOnSaveEnabled: boolean;
    autoSaveInterval: number;
  };
}> = ({ children, settings }) => {
  // Need to get closeBuffer from BufferManager first, but BufferManager needs PaneBridge
  // Use ref pattern to break circular dependency
  const closeBufferRef = React.useRef<((bufferId: string) => void | Promise<void>) | null>(null);

  const stableCloseBuffer = React.useCallback((id: string) => closeBufferRef.current?.(id), []);

  return (
    <PaneManagerProvider maxPanes={settings.maxPanes} closeBuffer={stableCloseBuffer}>
      <PaneToBufferBridge settings={settings} closeBufferRef={closeBufferRef}>
        {children}
      </PaneToBufferBridge>
    </PaneManagerProvider>
  );
};

const PaneToBufferBridge: React.FC<{
  children: ReactNode;
  settings: {
    isAutoSaveEnabled: boolean;
    isFormatOnSaveEnabled: boolean;
    autoSaveInterval: number;
  };
  closeBufferRef: React.MutableRefObject<((bufferId: string) => void | Promise<void>) | null>;
}> = ({ children, settings, closeBufferRef }) => {
  const pane = usePaneContext();

  // Use refs for data values so PaneBridge is stable across pane state changes.
  // These refs are read synchronously by `openFile` / `switchToBuffer` inside
  // the same React event tick that updated `pane.activePaneId`, so the writes
  // must land at render time (not post-commit) — otherwise the bridge sees
  // stale data on the call that immediately follows a pane switch.
  const activePaneIdRef = React.useRef(pane.activePaneId);
  const activeBufferIdRef = React.useRef(pane.activeBufferId);
  const panesRef = React.useRef(pane.panes);

  activePaneIdRef.current = pane.activePaneId;
  activeBufferIdRef.current = pane.activeBufferId;
  panesRef.current = pane.panes;

  const paneBridge: PaneBridge = React.useMemo(
    () => ({
      get activePaneId() {
        return activePaneIdRef.current;
      },
      get activeBufferId() {
        return activeBufferIdRef.current;
      },
      get panes() {
        return panesRef.current;
      },
      setActiveBufferId: pane.setActiveBufferId,
      setActivePaneId: pane.setActivePaneId,
      setPanes: pane.setPanes,
      switchPane: pane.switchPane,
      closeBuffer: (id: string) => closeBufferRef.current?.(id),
      moveBufferToPane: pane.moveBufferToPane,
    }),
    [pane.setActiveBufferId, pane.setActivePaneId, pane.setPanes, pane.switchPane, pane.moveBufferToPane],
  );

  return (
    <BufferManagerProvider
      paneBridge={paneBridge}
      isAutoSaveEnabled={settings.isAutoSaveEnabled}
      isFormatOnSaveEnabled={settings.isFormatOnSaveEnabled}
      autoSaveInterval={settings.autoSaveInterval}
    >
      <CombinedContextProvider closeBufferRef={closeBufferRef}>{children}</CombinedContextProvider>
    </BufferManagerProvider>
  );
};

const CombinedContextProvider: React.FC<{
  children: ReactNode;
  closeBufferRef: React.MutableRefObject<((bufferId: string) => void | Promise<void>) | null>;
}> = ({ children, closeBufferRef }) => {
  const settings = useSettings();
  const pane = usePaneContext();
  const buffer = useBufferContext();

  // Store closeBuffer for PaneManager to use. Synchronous render-time
  // assignment is required because PaneManager reads through this ref in
  // the same tick as it's mounted (closeBuffer would otherwise be null on
  // first call).
  closeBufferRef.current = buffer.closeBuffer;

  const value = React.useMemo<EditorManagerContextValue>(
    () => ({
      // From BufferManager
      buffers: buffer.buffers,
      openFile: buffer.openFile,
      openWorkspaceBuffer: buffer.openWorkspaceBuffer,
      openCompareBuffer: buffer.openCompareBuffer,
      closeBuffer: buffer.closeBuffer,
      reorderBuffers: buffer.reorderBuffers,
      moveBufferToPane: buffer.moveBufferToPane,
      switchToBuffer: buffer.switchToBuffer,
      updateBufferContent: buffer.updateBufferContent,
      updateBufferCursor: buffer.updateBufferCursor,
      updateBufferScroll: buffer.updateBufferScroll,
      updateBufferMetadata: buffer.updateBufferMetadata,
      updateBufferTitle: buffer.updateBufferTitle,
      saveBuffer: buffer.saveBuffer,
      setBufferModified: buffer.setBufferModified,
      setBufferOriginalContent: buffer.setBufferOriginalContent,
      setBufferExternallyModified: buffer.setBufferExternallyModified,
      clearBufferExternallyModified: buffer.clearBufferExternallyModified,
      setBufferLanguageOverride: buffer.setBufferLanguageOverride,
      saveAllBuffers: buffer.saveAllBuffers,
      toggleBufferPin: buffer.toggleBufferPin,
      setBufferPinned: buffer.setBufferPinned,
      setBufferClosable: buffer.setBufferClosable,
      reloadBufferFromDisk: buffer.reloadBufferFromDisk,

      // From PaneManager
      panes: pane.panes,
      paneLayout: pane.paneLayout,
      activePaneId: pane.activePaneId,
      activeBufferId: pane.activeBufferId,
      paneSizes: pane.paneSizes,
      isLinkedScrollEnabled: pane.isLinkedScrollEnabled,
      closePane: pane.closePane,
      switchPane: pane.switchPane,
      splitPane: pane.splitPane,
      closeSplit: pane.closeSplit,
      setPaneLayout: pane.setPaneLayout,
      updatePaneSize: pane.updatePaneSize,
      toggleLinkedScroll: pane.toggleLinkedScroll,

      // From Settings
      isAutoSaveEnabled: settings.isAutoSaveEnabled,
      setAutoSaveEnabled: settings.setAutoSaveEnabled,
      whitespaceRenderingMode: settings.whitespaceRenderingMode,
      setWhitespaceRenderingMode: settings.setWhitespaceRenderingMode,
      autoSaveInterval: settings.autoSaveInterval,
      isFormatOnSaveEnabled: settings.isFormatOnSaveEnabled,
      setFormatOnSaveEnabled: settings.setFormatOnSaveEnabled,
      maxPanes: settings.maxPanes,
      setMaxPanes: settings.setMaxPanes,
    }),
    [
      buffer.buffers,
      buffer.openFile,
      buffer.openWorkspaceBuffer,
      buffer.openCompareBuffer,
      buffer.closeBuffer,
      buffer.reorderBuffers,
      buffer.moveBufferToPane,
      buffer.switchToBuffer,
      buffer.updateBufferContent,
      buffer.updateBufferCursor,
      buffer.updateBufferScroll,
      buffer.updateBufferMetadata,
      buffer.updateBufferTitle,
      buffer.saveBuffer,
      buffer.setBufferModified,
      buffer.setBufferOriginalContent,
      buffer.setBufferExternallyModified,
      buffer.clearBufferExternallyModified,
      buffer.setBufferLanguageOverride,
      buffer.saveAllBuffers,
      buffer.toggleBufferPin,
      buffer.setBufferPinned,
      buffer.setBufferClosable,
      buffer.reloadBufferFromDisk,
      pane.panes,
      pane.paneLayout,
      pane.activePaneId,
      pane.activeBufferId,
      pane.paneSizes,
      pane.isLinkedScrollEnabled,
      pane.closePane,
      pane.switchPane,
      pane.splitPane,
      pane.closeSplit,
      pane.setPaneLayout,
      pane.updatePaneSize,
      pane.toggleLinkedScroll,
      settings.isAutoSaveEnabled,
      settings.setAutoSaveEnabled,
      settings.whitespaceRenderingMode,
      settings.setWhitespaceRenderingMode,
      settings.autoSaveInterval,
      settings.isFormatOnSaveEnabled,
      settings.setFormatOnSaveEnabled,
      settings.maxPanes,
      settings.setMaxPanes,
    ],
  );

  return <EditorManagerContext.Provider value={value}>{children}</EditorManagerContext.Provider>;
};
