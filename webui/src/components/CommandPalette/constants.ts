import { supportsLocalTerminal } from '../../config/mode';
import type { CommandDef } from './CommandPalette';

// ── Command definitions ────────────────────────────────────────────────────

export const COMMAND_DEFINITIONS: CommandDef[] = [
  { id: 'quick_open', label: 'Go to File...', category: 'File' },
  { id: 'new_file', label: 'New File', category: 'File' },
  { id: 'save_file', label: 'Save File', category: 'File' },
  { id: 'save_all_files', label: 'Save All Files', category: 'File' },
  { id: 'close_editor', label: 'Close Editor', category: 'File' },
  { id: 'close_all_editors', label: 'Close All Editors', category: 'File' },
  { id: 'close_other_editors', label: 'Close Other Editors', category: 'File' },
  { id: 'toggle_pin_tab', label: 'Toggle Pin Tab', category: 'File' },
  { id: 'command_palette', label: 'Show All Commands', category: 'General' },
  { id: 'toggle_explorer', label: 'Toggle File Explorer', category: 'View' },
  { id: 'toggle_sidebar', label: 'Toggle Sidebar', category: 'View' },
  { id: 'toggle_terminal', label: 'Toggle Terminal', category: 'View' },
  { id: 'split_editor_vertical', label: 'Split Editor Vertical', category: 'View' },
  { id: 'split_editor_horizontal', label: 'Split Editor Horizontal', category: 'View' },
  { id: 'split_editor_grid', label: 'Split Editor Grid', category: 'View' },
  { id: 'focus_split_1', label: 'Focus Editor Split 1', category: 'View' },
  { id: 'focus_split_2', label: 'Focus Editor Split 2', category: 'View' },
  { id: 'focus_split_3', label: 'Focus Editor Split 3', category: 'View' },
  { id: 'focus_split_4', label: 'Focus Editor Split 4', category: 'View' },
  { id: 'focus_split_5', label: 'Focus Editor Split 5', category: 'View' },
  { id: 'focus_split_6', label: 'Focus Editor Split 6', category: 'View' },
  { id: 'split_terminal_vertical', label: 'Split Terminal Vertical', category: 'View' },
  { id: 'split_terminal_horizontal', label: 'Split Terminal Horizontal', category: 'View' },
  { id: 'editor_toggle_word_wrap', label: 'Toggle Word Wrap', category: 'View' },
  { id: 'toggle_minimap', label: 'Toggle Minimap', category: 'View' },
  { id: 'editor_cycle_whitespace_rendering', label: 'Cycle Whitespace Rendering', category: 'View' },
  { id: 'editor_toggle_relative_line_numbers', label: 'Toggle Relative Line Numbers', category: 'View' },
  { id: 'editor_toggle_inlay_hints', label: 'Toggle Inlay Hints', category: 'View' },
  { id: 'editor_toggle_signature_help', label: 'Toggle Signature Help', category: 'View' },
  { id: 'editor_cycle_tab_size', label: 'Cycle Indent Size', category: 'View' },
  { id: 'editor_zoom_in', label: 'Editor: Zoom In', category: 'View' },
  { id: 'editor_zoom_out', label: 'Editor: Zoom Out', category: 'View' },
  { id: 'editor_reset_zoom', label: 'Editor: Reset Zoom', category: 'View' },
  { id: 'toggle_linked_scroll', label: 'Toggle Linked Scrolling', category: 'View' },
  { id: 'editor_toggle_format_on_save', label: 'Toggle Format on Save', category: 'Editor' },
  { id: 'editor_reveal_in_explorer', label: 'Reveal Active File in Explorer', category: 'Editor' },
  { id: 'editor_copy_relative_path', label: 'Copy Active File Path (Relative)', category: 'Editor' },
  { id: 'editor_copy_absolute_path', label: 'Copy Active File Path (Absolute)', category: 'Editor' },
  { id: 'editor_open_live_preview', label: 'Open Live Preview (SVG / HTML)', category: 'Editor' },
  { id: 'editor_toggle_markdown_preview', label: 'Toggle Markdown Preview', category: 'Editor' },
  { id: 'reset_saved_layout', label: 'Reset Saved Layout', category: 'View' },
  { id: 'focus_next_tab', label: 'Focus Next Tab', category: 'Navigation' },
  { id: 'focus_prev_tab', label: 'Focus Previous Tab', category: 'Navigation' },
  { id: 'switch_to_chat', label: 'Switch to Chat', category: 'Navigation' },
  { id: 'switch_to_editor', label: 'Switch to Editor', category: 'Navigation' },
  { id: 'switch_to_git', label: 'Switch to Git', category: 'Navigation' },
  { id: 'open_hotkeys_config', label: 'Edit Keyboard Shortcuts', category: 'Preferences' },
  { id: 'format_document', label: 'Format Document', category: 'Editor' },
  { id: 'editor_find_all_references', label: 'Find All References', category: 'Editor' },
  { id: 'editor_workspace_symbol', label: 'Go to Symbol in Workspace', category: 'Editor' },
  { id: 'editor_goto_symbol', label: 'Go to Symbol in File', category: 'Editor' },
];

export const VISIBLE_COMMANDS = COMMAND_DEFINITIONS.filter((cmd) => {
  if (
    !supportsLocalTerminal &&
    (cmd.id === 'toggle_terminal' || cmd.id === 'split_terminal_vertical' || cmd.id === 'split_terminal_horizontal')
  ) {
    return false;
  }
  return true;
});

// ── File browsing constants ────────────────────────────────────────────────

export const MAX_FILE_RESULTS = 100;
export const MAX_INDEXED_FILES = 12000;
export const MAX_INDEXED_DIRECTORIES = 3000;
export const SKIP_DIRECTORIES = new Set([
  '.git',
  'node_modules',
  '.next',
  'dist',
  'build',
  '__pycache__',
  '.venv',
  'vendor',
]);
export const MAX_DIRECTORY_DEPTH = 8;
export const MAX_SYMBOL_RESULTS = 100;

// ── Prefix-based auto-detection ──────────────────────────────────────────

export const COMMAND_PREFIX = '>';
export const SYMBOL_PREFIXES = ['@', '#'];
