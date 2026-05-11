import { useCallback } from 'react';
import { showThemedConfirm } from '../ThemedDialog';
import { clearLayoutSnapshot } from '../../services/layoutPersistence';
import { debugLog } from '../../utils/log';
import type { PaletteMode, PaletteResult } from './types';

interface UseCommandExecutorOptions {
  onClose: () => void;
  onToggleSidebar: () => void;
  onToggleTerminal: () => void;
  onOpenHotkeysConfig: () => void;
  setMode: (mode: PaletteMode) => void;
  setQuery: (query: string) => void;
  inputRef: React.MutableRefObject<HTMLInputElement | null>;
}

interface UseCommandExecutorReturn {
  executeCommand: (commandId: string) => Promise<void>;
  executeSelected: (
    item: PaletteResult,
    onOpenFile: (filePath: string) => void,
    onNavigateToLine?: (line: number) => void,
  ) => void;
}

const RESET_PREFS_KEYS = [
  'sprout.editor.paneLayout',
  'sprout.editor.paneSizes',
  'sprout-terminal-height',
  'sprout-terminal-expanded',
  'sprout-sidebar-collapsed',
  'sprout-sidebar-width',
  'sprout.contextPanel.width',
  'sprout.contextPanel.collapsed',
  'editor:minimap-enabled',
  'filetree-show-ignored',
];

function useCommandExecutor(options: UseCommandExecutorOptions): UseCommandExecutorReturn {
  const { onClose, onToggleSidebar, onToggleTerminal, onOpenHotkeysConfig, setMode, setQuery, inputRef } = options;

  const executeCommand = useCallback(
    async (commandId: string) => {
      switch (commandId) {
        case 'command_palette':
          break;
        case 'quick_open':
          setMode('files');
          setQuery('');
          inputRef.current?.focus();
          return;
        case 'toggle_explorer':
        case 'toggle_sidebar':
          onToggleSidebar();
          break;
        case 'toggle_terminal':
          onToggleTerminal();
          break;
        case 'new_file':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'new_file' } }));
          break;
        case 'switch_to_chat':
        case 'switch_to_editor':
        case 'switch_to_git':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId } }));
          break;
        case 'open_hotkeys_config':
          onOpenHotkeysConfig();
          break;
        case 'split_editor_vertical':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_editor_vertical' } }));
          break;
        case 'split_editor_horizontal':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_editor_horizontal' } }));
          break;
        case 'split_editor_grid':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_editor_grid' } }));
          break;
        case 'focus_split_1':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_1' } }));
          break;
        case 'focus_split_2':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_2' } }));
          break;
        case 'focus_split_3':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_3' } }));
          break;
        case 'focus_split_4':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_4' } }));
          break;
        case 'focus_split_5':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_5' } }));
          break;
        case 'focus_split_6':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_6' } }));
          break;
        case 'split_terminal_vertical':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_terminal_vertical' } }));
          break;
        case 'split_terminal_horizontal':
          window.dispatchEvent(
            new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_terminal_horizontal' } }),
          );
          break;
        case 'editor_toggle_word_wrap':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'editor_toggle_word_wrap' } }));
          break;
        case 'toggle_linked_scroll':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_linked_scroll' } }));
          break;
        case 'toggle_minimap':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_minimap' } }));
          break;
        case 'editor_cycle_whitespace_rendering':
          window.dispatchEvent(new CustomEvent('editor-cycle-whitespace-rendering'));
          break;
        case 'close_editor':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'close_editor' } }));
          break;
        case 'save_file':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'save_file' } }));
          break;
        case 'save_all_files':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'save_all_files' } }));
          break;
        case 'close_all_editors':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'close_all_editors' } }));
          break;
        case 'close_other_editors':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'close_other_editors' } }));
          break;
        case 'toggle_pin_tab':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_pin_tab' } }));
          break;
        case 'focus_next_tab':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_next_tab' } }));
          break;
        case 'focus_prev_tab':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_prev_tab' } }));
          break;
        case 'reset_saved_layout': {
          if (
            !(await showThemedConfirm('Reset all saved layout settings? This cannot be undone.', { type: 'danger' }))
          ) {
            break;
          }
          clearLayoutSnapshot();
          for (const key of RESET_PREFS_KEYS) {
            try {
              window.localStorage.removeItem(key);
            } catch (err) {
              debugLog('[resetPreferences] localStorage.removeItem failed:', err);
            }
          }
          window.location.reload();
          return;
        }
        case 'format_document':
          document.dispatchEvent(new CustomEvent('editor-format-document'));
          break;
        case 'editor_find_all_references':
          document.dispatchEvent(new CustomEvent('editor-find-all-references'));
          break;
        case 'editor_workspace_symbol':
          document.dispatchEvent(new CustomEvent('editor-go-to-workspace-symbol'));
          break;
        case 'editor_goto_symbol':
          setMode('symbols');
          setQuery('');
          inputRef.current?.focus();
          return;
        default:
          break;
      }
      onClose();
    },
    [onClose, onToggleSidebar, onToggleTerminal, onOpenHotkeysConfig, setMode, setQuery, inputRef],
  );

  const executeSelected = useCallback(
    (item: PaletteResult, onOpenFile: (filePath: string) => void, onNavigateToLine?: (line: number) => void): void => {
      if (item.kind === 'command' && item.commandId) {
        executeCommand(item.commandId);
      } else if (item.kind === 'file' && item.filePath) {
        onOpenFile(item.filePath);
        onClose();
      } else if (item.kind === 'symbol' && item.symbolLine !== undefined && onNavigateToLine) {
        onNavigateToLine(item.symbolLine);
        onClose();
      }
    },
    [executeCommand, onClose],
  );

  return { executeCommand, executeSelected };
}

export default useCommandExecutor;
