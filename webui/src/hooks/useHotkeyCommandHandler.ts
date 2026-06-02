import { useEffect, useRef } from 'react';
import { showThemedConfirm } from '../components/ThemedDialog';
import { supportsLocalTerminal } from '../config/mode';
import { clearLayoutSnapshot } from '../services/layoutPersistence';
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
  /** Focus a specific pane by index (0-based) in the panes array */
  onFocusPaneIndex: (index: number) => void;
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
  /** Toggle pin state of the active buffer */
  onTogglePinTab: () => void;
  /** Get active buffer ID */
  activeBufferId: string | null;
  /** Get active pane ID */
  activePaneId: string | null;
  /** All buffers map */
  buffers: Map<string, EditorBuffer>;
}

/**
 * Installs a global listener for `sprout:hotkey` custom events and dispatches
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
    onFocusPaneIndex,
    onSplitRequest,
    onCloseBuffer,
    onCloseAllBuffers,
    onCloseOtherBuffers,
    onSaveAllBuffers,
    onTogglePinTab,
    onSwitchToBuffer,
    onSwitchPane,
    activeBufferId,
    activePaneId,
    buffers,
  } = options;

  // Keep ref to buffers to avoid recreating the effect on every keystroke
  const buffersRef = useRef<Map<string, EditorBuffer>>(buffers);
  buffersRef.current = buffers;

  useEffect(() => {
    const handleHotkey = async (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (!detail?.commandId) return;

      // Gate terminal commands in cloud mode
      if (
        !supportsLocalTerminal &&
        (detail.commandId === 'toggle_terminal' ||
          detail.commandId === 'split_terminal_vertical' ||
          detail.commandId === 'split_terminal_horizontal' ||
          detail.commandId === 'clear_terminal' ||
          detail.commandId === 'kill_terminal')
      ) {
        return;
      }

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
          const activeBuffer = activeBufferId ? buffersRef.current.get(activeBufferId) : null;
          const filePath =
            activeBuffer?.file?.path && !activeBuffer.file.isDir && activeBuffer.kind === 'file'
              ? activeBuffer.file.path
              : null;

          if (filePath) {
            window.dispatchEvent(new CustomEvent('sprout:reveal-in-explorer', { detail: { path: filePath } }));
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
        case 'focus_split_1':
          onFocusPaneIndex(0);
          break;
        case 'focus_split_2':
          onFocusPaneIndex(1);
          break;
        case 'focus_split_3':
          onFocusPaneIndex(2);
          break;
        case 'focus_split_4':
          onFocusPaneIndex(3);
          break;
        case 'focus_split_5':
          onFocusPaneIndex(4);
          break;
        case 'focus_split_6':
          onFocusPaneIndex(5);
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
          const paneBuffers = Array.from(buffersRef.current.values()).filter(
            (buffer) => buffer.paneId === activePaneId,
          );
          if (paneBuffers.length <= 1) break;
          const currentIdx = activeBufferId ? paneBuffers.findIndex((b) => b.id === activeBufferId) : -1;
          const nextIdx = currentIdx + 1 < paneBuffers.length ? currentIdx + 1 : 0;
          if (paneBuffers[nextIdx]) {
            onSwitchPane(activePaneId);
            onSwitchToBuffer(paneBuffers[nextIdx].id);
          }
          break;
        }
        case 'focus_prev_tab': {
          if (!activePaneId) break;
          const paneBuffersPrev = Array.from(buffersRef.current.values()).filter(
            (buffer) => buffer.paneId === activePaneId,
          );
          if (paneBuffersPrev.length <= 1) break;
          const currentIdx = activeBufferId ? paneBuffersPrev.findIndex((b) => b.id === activeBufferId) : -1;
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
        case 'toggle_pin_tab':
          onTogglePinTab();
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
          window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: { action: 'split_vertical' } }));
          break;
        case 'split_terminal_horizontal':
          window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: { action: 'split_horizontal' } }));
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
        case 'editor_toggle_relative_line_numbers':
          document.dispatchEvent(new CustomEvent('editor-toggle-relative-line-numbers'));
          break;
        case 'editor_toggle_inlay_hints':
          document.dispatchEvent(new CustomEvent('editor-toggle-inlay-hints'));
          break;
        case 'editor_toggle_signature_help':
          document.dispatchEvent(new CustomEvent('editor-toggle-signature-help'));
          break;
        case 'editor_cycle_tab_size':
          document.dispatchEvent(new CustomEvent('editor-cycle-tab-size'));
          break;
        case 'editor_zoom_in':
          document.dispatchEvent(new CustomEvent('editor-zoom-in'));
          break;
        case 'editor_zoom_out':
          document.dispatchEvent(new CustomEvent('editor-zoom-out'));
          break;
        case 'editor_reset_zoom':
          document.dispatchEvent(new CustomEvent('editor-reset-zoom'));
          break;
        case 'editor_toggle_format_on_save':
          document.dispatchEvent(new CustomEvent('editor-toggle-format-on-save'));
          break;
        case 'editor_reveal_in_explorer': {
          const buf = activeBufferId ? buffersRef.current.get(activeBufferId) : null;
          const path = buf?.file?.path;
          if (path && !path.startsWith('__workspace/')) {
            window.dispatchEvent(new CustomEvent('sprout:reveal-in-explorer', { detail: { path } }));
          }
          break;
        }
        case 'editor_copy_relative_path': {
          const buf = activeBufferId ? buffersRef.current.get(activeBufferId) : null;
          const path = buf?.file?.path;
          if (path && !path.startsWith('__workspace/')) {
            void navigator.clipboard?.writeText(path);
          }
          break;
        }
        case 'editor_copy_absolute_path': {
          // The backend stores absolute paths in file.path when the workspace
          // root is known; for relative paths we need the workspace root.
          // Use the same path either way — consumers can canonicalize.
          const buf = activeBufferId ? buffersRef.current.get(activeBufferId) : null;
          const path = buf?.file?.path;
          if (path && !path.startsWith('__workspace/')) {
            void navigator.clipboard?.writeText(path);
          }
          break;
        }
        case 'editor_open_live_preview':
          document.dispatchEvent(new CustomEvent('editor-open-live-preview'));
          break;
        case 'editor_toggle_markdown_preview':
          document.dispatchEvent(new CustomEvent('editor-toggle-markdown-preview'));
          break;
        // Editor standard commands (document-level events for CodeMirror)
        case 'undo':
          document.dispatchEvent(new CustomEvent('editor-undo'));
          break;
        case 'redo':
          document.dispatchEvent(new CustomEvent('editor-redo'));
          break;
        case 'editor_cut':
          document.dispatchEvent(new CustomEvent('editor-cut'));
          break;
        case 'editor_copy':
          document.dispatchEvent(new CustomEvent('editor-copy'));
          break;
        case 'editor_paste':
          document.dispatchEvent(new CustomEvent('editor-paste'));
          break;
        case 'editor_find':
          document.dispatchEvent(new CustomEvent('editor-find'));
          break;
        case 'editor_replace':
          document.dispatchEvent(new CustomEvent('editor-find-replace'));
          break;
        case 'editor_select_all':
          document.dispatchEvent(new CustomEvent('editor-select-all'));
          break;
        case 'clear_terminal':
          window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: { action: 'clear' } }));
          break;
        case 'kill_terminal':
          window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: { action: 'kill' } }));
          break;
        case 'save_file':
          // Dispatch to editor save handler for single-file save
          document.dispatchEvent(new CustomEvent('editor-save-current'));
          break;
        case 'format_document':
          document.dispatchEvent(new CustomEvent('editor-format-document'));
          break;
        case 'reset_saved_layout': {
          if (
            !(await showThemedConfirm('Reset all saved layout settings? This cannot be undone.', { type: 'danger' }))
          ) {
            break;
          }
          // Match CommandPalette reset logic: clear persisted snapshot + all layout keys
          clearLayoutSnapshot();
          const keys = [
            'sprout.editor.paneLayout',
            'sprout.editor.paneSizes',
            'sprout-terminal-height',
            'sprout-terminal-expanded',
            'sprout-sidebar-collapsed',
            'sprout-sidebar-width',
            'sprout.contextPanel.width',
            'sprout.contextPanel.collapsed',
            'editor:minimap-enabled',
            'editor:word-wrap-enabled',
            'editor:linked-scroll-enabled',
            'filetree-show-ignored',
          ];
          for (const key of keys) {
            try {
              localStorage.removeItem(key);
            } catch {
              // Ignore storage errors (quota, security policy, etc.)
            }
          }
          window.location.reload();
          break;
        }
      }
    };

    window.addEventListener('sprout:hotkey', handleHotkey);
    return () => window.removeEventListener('sprout:hotkey', handleHotkey);
  }, [
    onToggleCommandPalette,
    onOpenCommandPalette,
    onNewFile,
    onToggleSidebar,
    onToggleTerminal,
    onPrimaryViewChange,
    onFocusTabIndex,
    onFocusPaneIndex,
    onSplitRequest,
    onCloseBuffer,
    onCloseAllBuffers,
    onCloseOtherBuffers,
    onSaveAllBuffers,
    onTogglePinTab,
    onSwitchToBuffer,
    onSwitchPane,
    activeBufferId,
    activePaneId,
  ]);
}
