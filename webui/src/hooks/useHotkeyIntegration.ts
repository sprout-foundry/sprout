import { useCallback, useRef } from 'react';
import type { EditorBuffer } from '../types/editor';
import { useHotkeyCommandHandler } from './useHotkeyCommandHandler';

interface UseHotkeyIntegrationOptions {
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onToggleSidebar: () => void;
  onTerminalExpandedChange: (expanded: boolean) => void;
  isTerminalExpanded: boolean;
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
  activePaneId: string | null;
  activeBufferId: string | null;
  buffers: Map<string, EditorBuffer>;
  panes: Array<{ id: string }>;
  handleSplitRequest: (direction: 'vertical' | 'horizontal' | 'grid') => void;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => string | null;
  switchPane: (paneId: string) => void;
  updatePaneSize: (sizeKey: string, size: number) => void;
  closeBuffer: (bufferId: string) => void;
  closeAllBuffers: () => void;
  closeOtherBuffers: (bufferId: string) => void;
  saveAllBuffers: () => Promise<void>;
  switchToBuffer: (bufferId: string) => void;
  toggleBufferPin: (bufferId: string) => void;
  onToggleCommandPalette: () => void;
  onOpenCommandPalette: () => void;
  maxPanes?: number; // Optional: configurable max panes limit (default: 6)
}

export function useHotkeyIntegration({
  onViewChange,
  onToggleSidebar,
  onTerminalExpandedChange,
  isTerminalExpanded,
  openWorkspaceBuffer,
  activePaneId,
  activeBufferId,
  buffers,
  panes,
  handleSplitRequest,
  splitPane,
  switchPane,
  updatePaneSize,
  closeBuffer,
  closeAllBuffers,
  closeOtherBuffers,
  saveAllBuffers,
  switchToBuffer,
  toggleBufferPin,
  onToggleCommandPalette,
  onOpenCommandPalette,
  maxPanes = 6,
}: UseHotkeyIntegrationOptions) {
  // Keep refs to avoid unstable deps in useCallback (buffers Map changes on every keystroke)
  const buffersRef = useRef(buffers);
  buffersRef.current = buffers;
  const panesRef = useRef(panes);
  panesRef.current = panes;

  const handlePrimaryViewChange = useCallback(
    (view: 'chat' | 'editor' | 'git') => {
      if (view === 'chat') {
        openWorkspaceBuffer({
          kind: 'chat',
          path: '__workspace/chat',
          title: 'Chat',
          ext: '.chat',
          isPinned: true,
          isClosable: false,
        });
      }
      onViewChange(view);
    },
    [onViewChange, openWorkspaceBuffer],
  );

  const focusTabIndex = useCallback(
    (index: number) => {
      if (!activePaneId || index < 0) return;
      const paneBuffers = Array.from(buffersRef.current.values()).filter((buffer) => buffer.paneId === activePaneId);
      const target = paneBuffers[index];
      if (target) {
        switchPane(activePaneId);
        switchToBuffer(target.id);
      }
    },
    [activePaneId, switchPane, switchToBuffer],
  );

  const handleFocusPaneIndex = useCallback(
    (index: number) => {
      const currentPanes = panesRef.current;
      if (index < currentPanes.length) {
        // Focus existing pane
        switchPane(currentPanes[index].id);
        return;
      }
      // index >= panes.length — need to split to create more panes
      if (currentPanes.length < maxPanes) {
        // Split from the active pane (or last pane)
        const sourcePaneId = activePaneId || currentPanes[currentPanes.length - 1]?.id;
        if (!sourcePaneId) return;
        const direction = currentPanes.length === 1 ? 'vertical' : 'horizontal';
        const newPaneId = splitPane(sourcePaneId, direction);
        if (newPaneId) {
          // Update pane sizes to 50/50
          updatePaneSize(`group:${sourcePaneId}`, 50);
          updatePaneSize(`nested:${sourcePaneId}`, 50);
          // Focus the new pane
          switchPane(newPaneId);
        }
      }
    },
    [activePaneId, splitPane, switchPane, updatePaneSize],
  );

  const handleNewFile = useCallback(() => {
    openWorkspaceBuffer({
      kind: 'file',
      path: `__workspace/untitled-${Date.now()}`,
      title: 'Untitled',
      ext: '',
      isClosable: true,
    });
    onViewChange('editor');
  }, [openWorkspaceBuffer, onViewChange]);

  const handleCloseBuffer = useCallback(() => {
    if (activeBufferId) closeBuffer(activeBufferId);
  }, [activeBufferId, closeBuffer]);

  const handleCloseOtherBuffers = useCallback(() => {
    if (activeBufferId) closeOtherBuffers(activeBufferId);
  }, [activeBufferId, closeOtherBuffers]);

  const handleSaveAllBuffers = useCallback(() => {
    void saveAllBuffers();
  }, [saveAllBuffers]);

  const handleTogglePinTab = useCallback(() => {
    if (activeBufferId) {
      toggleBufferPin(activeBufferId);
    }
  }, [activeBufferId, toggleBufferPin]);

  useHotkeyCommandHandler({
    onToggleCommandPalette,
    onOpenCommandPalette,
    onNewFile: handleNewFile,
    onToggleSidebar,
    onToggleTerminal: useCallback(
      () => onTerminalExpandedChange(!isTerminalExpanded),
      [isTerminalExpanded, onTerminalExpandedChange],
    ),
    onPrimaryViewChange: handlePrimaryViewChange,
    onFocusTabIndex: focusTabIndex,
    onFocusPaneIndex: handleFocusPaneIndex,
    onSplitRequest: handleSplitRequest,
    onCloseBuffer: handleCloseBuffer,
    onCloseAllBuffers: closeAllBuffers,
    onCloseOtherBuffers: handleCloseOtherBuffers,
    onSaveAllBuffers: handleSaveAllBuffers,
    onTogglePinTab: handleTogglePinTab,
    onSwitchToBuffer: switchToBuffer,
    onSwitchPane: switchPane,
    activeBufferId,
    activePaneId,
    buffers,
  });

  return { handlePrimaryViewChange };
}
