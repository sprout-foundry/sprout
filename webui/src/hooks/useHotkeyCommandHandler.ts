import { useEffect } from 'react';
import type { EditorBuffer } from '../types/editor';

type ViewMode = 'chat' | 'editor' | 'git';

export interface UseHotkeyCommandHandlerOptions {
  /** Toggle command palette open state */
  onToggleCommandPalette: () => void;
  /** Open command palette */
  onOpenCommandPalette: () => void;
  /** Create a new untitled workspace buffer */
  onNewFile: () => void;
  /** Toggle sidebar */
  onToggleSidebar: () => void;
  /** Toggle terminal expanded */
  onToggleTerminal: () => void;
  /** Change the primary view mode */
  onPrimaryViewChange: (view: ViewMode) => void;
  /** Focus a specific tab index in the active pane */
  onFocusTabIndex: (index: number) => void;
  /** Split editor panes */
  onSplitRequest: (direction: 'vertical' | 'horizontal' | 'grid') => void;
  /** Close the active buffer */
  onCloseBuffer: () => void;
  /** Close all buffers */
  onCloseAllBuffers: () => void;
  /** Close other buffers (keeping active) */
  onCloseOtherBuffers: () => void;
  /** Save all buffers */
  onSaveAllBuffers: () => void;
  /** Switch to a specific pane and buffer */
  onSwitchToBuffer: (bufferId: string) => void;
  /** Switch to a specific pane */
  onSwitchPane: (paneId: string) => void;
  /** Get active buffer ID */
  activeBufferId: string | null;
  /** Get active pane ID */
  activePaneId: string | null;
  /** All buffers map */
  buffers: Map<string, EditorBuffer>;
}

/**
 * Installs a global listener for `ledit:hotkey` custom events and dispatches
 * them to the appropriate handler callbacks.
 */
export function useHotkeyCommandHandler(options: UseHotkeyCommandHandlerOptions): void {
  const {
    onToggleCommandPalette,
    onOpenCommandPalette,
    onNewFile,
    onToggleSidebar,
    onToggleTerminal,
    onPrimaryViewChange,
    onFocusTabIndex,
    onSplitRequest,
    onCloseBuffer,
    onCloseAllBuffers,
    onCloseOtherBuffers,
    onSaveAllBuffers,
    onSwitchToBuffer,
    onSwitchPane,
    activeBufferId,
    activePaneId,
    buffers,
  } = options;

  useEffect(() => {
    const handleHotkey = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (!detail?.commandId) return;

      switch (detail.commandId) {
        case 'command_palette':
          onToggleCommandPalette();
          break;
        case 'new_file':
          onNewFile();
          break;
        case 'toggle_sidebar':
          onToggleSidebar();
          break;
        case 'toggle_terminal':
          onToggleTerminal();
          break;
        case 'toggle_explorer': {
          // Reveal the active file's path in the file tree explorer
          const activeBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
          const filePath = activeBuffer?.file?.path && !activeBuffer.file.isDir && activeBuffer.kind === 'file'
            ? activeBuffer.file.path
            : null;

          if (filePath) {
            window.dispatchEvent(new CustomEvent('ledit:reveal-in-explorer', { detail: { path: filePath } }));
          } else {
            // No active file — just toggle sidebar to files
            onToggleSidebar();
          }
          break;
        }
        case 'quick_open':
          onOpenCommandPalette();
          break;
        case 'switch_to_chat':
          onPrimaryViewChange('chat');
          break;
        case 'switch_to_editor':
          onPrimaryViewChange('editor');
          break;
        case 'switch_to_git':
          onPrimaryViewChange('git');
          break;
        case 'focus_tab_1':
          onFocusTabIndex(0);
          break;
        case 'focus_tab_2':
          onFocusTabIndex(1);
          break;
        case 'focus_tab_3':
          onFocusTabIndex(2);
          break;
        case 'focus_tab_4':
          onFocusTabIndex(3);
          break;
        case 'focus_tab_5':
          onFocusTabIndex(4);
          break;
        case 'focus_tab_6':
          onFocusTabIndex(5);
          break;
        case 'focus_tab_7':
          onFocusTabIndex(6);
          break;
        case 'focus_tab_8':
          onFocusTabIndex(7);
          break;
        case 'focus_tab_9':
          onFocusTabIndex(8);
          break;
        case 'focus_next_tab': {
          if (!activePaneId) break;
          const paneBuffers = Array.from(buffers.values()).filter((buffer) => buffer.paneId === activePaneId);
          if (paneBuffers.length <= 1) break;
          const currentIdx = activeBufferId ? paneBuffers.findIndex(b => b.id === activeBufferId) : -1;
          const nextIdx = currentIdx + 1 < paneBuffers.length ? currentIdx + 1 : 0;
          if (paneBuffers[nextIdx]) {
            onSwitchPane(activePaneId);
            onSwitchToBuffer(paneBuffers[nextIdx].id);
          }
          break;
        }
        case 'focus_prev_tab': {
          if (!activePaneId) break;
          const paneBuffersPrev = Array.from(buffers.values()).filter((buffer) => buffer.paneId === activePaneId);
          if (paneBuffersPrev.length <= 1) break;
          const currentIdx = activeBufferId ? paneBuffersPrev.findIndex(b => b.id === activeBufferId) : -1;
          const prevIdx = currentIdx - 1 >= 0 ? currentIdx - 1 : paneBuffersPrev.length - 1;
          if (paneBuffersPrev[prevIdx]) {
            onSwitchPane(activePaneId);
            onSwitchToBuffer(paneBuffersPrev[prevIdx].id);
          }
          break;
        }
        case 'close_editor':
          onCloseBuffer();
          break;
        case 'close_all_editors':
          onCloseAllBuffers();
          break;
        case 'close_other_editors':
          onCloseOtherBuffers();
          break;
        case 'save_all_files':
          onSaveAllBuffers();
          break;
        case 'split_editor_vertical':
          onSplitRequest('vertical');
          break;
        case 'split_editor_horizontal':
          onSplitRequest('horizontal');
          break;
        case 'split_editor_grid':
          onSplitRequest('grid');
          break;
        case 'split_terminal_vertical':
          window.dispatchEvent(new CustomEvent('ledit:terminal-action', { detail: { action: 'split_vertical' } }));
          break;
        case 'split_terminal_horizontal':
          window.dispatchEvent(new CustomEvent('ledit:terminal-action', { detail: { action: 'split_horizontal' } }));
          break;
        case 'editor_toggle_word_wrap':
          document.dispatchEvent(new CustomEvent('editor-toggle-word-wrap'));
          break;
        case 'toggle_linked_scroll':
          document.dispatchEvent(new CustomEvent('editor-toggle-linked-scroll'));
          break;
        case 'toggle_minimap':
          document.dispatchEvent(new CustomEvent('editor-toggle-minimap'));
          break;
      }
    };

    window.addEventListener('ledit:hotkey', handleHotkey);
    return () => window.removeEventListener('ledit:hotkey', handleHotkey);
  }, [
    onToggleCommandPalette,
    onOpenCommandPalette,
    onNewFile,
    onToggleSidebar,
    onToggleTerminal,
    onPrimaryViewChange,
    onFocusTabIndex,
    onSplitRequest,
    onCloseBuffer,
    onCloseAllBuffers,
    onCloseOtherBuffers,
    onSaveAllBuffers,
    onSwitchToBuffer,
    onSwitchPane,
    activeBufferId,
    activePaneId,
    buffers,
  ]);
}
