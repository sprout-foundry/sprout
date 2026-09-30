import type { HotkeyEntry } from '../services/api/types/settings';

/**
 * Built-in shortcuts: the fallback when no hotkey config loads, and the
 * complete set in browser (cloud) mode, where there is no daemon to hold a
 * user config or apply presets.
 */
export const BROWSER_DEFAULT_HOTKEYS: HotkeyEntry[] = [
  { key: 'Ctrl+1', command_id: 'focus_tab_1' },
  { key: 'Cmd+1', command_id: 'focus_tab_1' },
  { key: 'Ctrl+2', command_id: 'focus_tab_2' },
  { key: 'Cmd+2', command_id: 'focus_tab_2' },
  { key: 'Ctrl+3', command_id: 'focus_tab_3' },
  { key: 'Cmd+3', command_id: 'focus_tab_3' },
  { key: 'Ctrl+4', command_id: 'focus_tab_4' },
  { key: 'Cmd+4', command_id: 'focus_tab_4' },
  { key: 'Ctrl+5', command_id: 'focus_tab_5' },
  { key: 'Cmd+5', command_id: 'focus_tab_5' },
  { key: 'Ctrl+6', command_id: 'focus_tab_6' },
  { key: 'Cmd+6', command_id: 'focus_tab_6' },
  { key: 'Ctrl+7', command_id: 'focus_tab_7' },
  { key: 'Cmd+7', command_id: 'focus_tab_7' },
  { key: 'Ctrl+8', command_id: 'focus_tab_8' },
  { key: 'Cmd+8', command_id: 'focus_tab_8' },
  { key: 'Ctrl+9', command_id: 'focus_tab_9' },
  { key: 'Cmd+9', command_id: 'focus_tab_9' },
  { key: 'Ctrl+S', command_id: 'save_file', global: true },
  { key: 'Cmd+S', command_id: 'save_file', global: true },
  { key: 'Ctrl+Shift+S', command_id: 'save_all_files', global: true },
  { key: 'Cmd+Shift+S', command_id: 'save_all_files', global: true },
  { key: 'Ctrl+W', command_id: 'close_editor', global: false },
  { key: 'Cmd+W', command_id: 'close_editor', global: false },
  { key: 'Ctrl+Shift+W', command_id: 'close_all_editors', global: true },
  { key: 'Cmd+Shift+W', command_id: 'close_all_editors', global: true },
  { key: 'Ctrl+Alt+W', command_id: 'close_other_editors', global: true },
  { key: 'Cmd+Alt+W', command_id: 'close_other_editors', global: true },
  { key: 'Ctrl+Tab', command_id: 'focus_next_tab', global: false },
  { key: 'Ctrl+Shift+Tab', command_id: 'focus_prev_tab', global: false },
  { key: 'Cmd+Shift+Tab', command_id: 'focus_prev_tab', global: false },
  { key: 'Alt+Z', command_id: 'editor_toggle_word_wrap', global: false },
  { key: 'Shift+Alt+F', command_id: 'format_document', global: false },
  { key: 'Shift+Cmd+F', command_id: 'format_document', global: false },
  { key: 'Ctrl+K', command_id: 'split_editor_horizontal', global: false },
  { key: 'Cmd+K', command_id: 'split_editor_horizontal', global: false },
  { key: 'Ctrl+`', command_id: 'toggle_terminal', global: true },
  { key: 'Alt+1', command_id: 'switch_to_editor', global: false },
  { key: 'Alt+2', command_id: 'switch_to_chat', global: false },
  { key: 'Alt+3', command_id: 'switch_to_git', global: false },
];
