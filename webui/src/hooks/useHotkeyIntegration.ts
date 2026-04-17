import { useCallback } from 'react';
import type { EditorBuffer } from '../types/editor';
import { useHotkeyCommandHandler } from './useHotkeyCommandHandler';

interface UseHotkeyIntegrationOptions {
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onToggleSidebar: () => void;
  onTerminalExpandedChange: (expanded: boolean) => void;
  isTerminalExpanded: boolean;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file';
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
}: UseHotkeyIntegrationOptions) {
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
      const paneBuffers = Array.from(buffers.values()).filter((buffer) => buffer.paneId === activePaneId);
      const target = paneBuffers[index];
      if (target) {
        switchPane(activePaneId);
        switchToBuffer(target.id);
      }
    },
    [activePaneId, buffers, switchPane, switchToBuffer],
  );

  const handleFocusPaneIndex = useCallback(
    (index: number) => {
      if (index < panes.length) {
        // Focus existing pane
        switchPane(panes[index].id);
        return;
      }
      // index >= panes.length — need to split to create more panes
      if (panes.length < 3) {
        // Split from the active pane (or last pane)
        const sourcePaneId = activePaneId || panes[panes.length - 1]?.id;
        if (!sourcePaneId) return;
        const direction = panes.length === 1 ? 'vertical' : 'horizontal';
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
    [panes, activePaneId, splitPane, switchPane, updatePaneSize],
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
