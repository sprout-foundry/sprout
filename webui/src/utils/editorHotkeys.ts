import { EditorView, KeyBinding } from '@codemirror/view';
import type { HotkeyEntry } from '../services/api';

interface EditorHotkeyActions {
  onSave?: () => void;
  onGoToLine?: () => void;
}

// ── Editor line-manipulation helpers ────────────────────────────────

function hasSingleSelection(view: EditorView): boolean {
  return view.state.selection.ranges.length === 1;
}

function duplicateCurrentLine(view: EditorView, direction: 'up' | 'down' = 'down'): boolean {
  if (!hasSingleSelection(view)) return false;

  const cursor = view.state.selection.main.head;
  const line = view.state.doc.lineAt(cursor);
  const lineText = line.text;
  const insertText = lineText + '\n';

  if (direction === 'up') {
    view.dispatch({
      changes: { from: line.from, insert: insertText },
      selection: { anchor: cursor + insertText.length },
      scrollIntoView: true,
    });
    return true;
  }

  const insertPos = line.to;
  const prefix = line.to === view.state.doc.length ? '\n' : '';
  view.dispatch({
    changes: { from: insertPos, insert: prefix + lineText },
    selection: { anchor: cursor + prefix.length + lineText.length + (line.to === view.state.doc.length ? 1 : 0) },
    scrollIntoView: true,
  });
  return true;
}

function deleteCurrentLine(view: EditorView): boolean {
  if (!hasSingleSelection(view)) return false;

  const cursor = view.state.selection.main.head;
  const line = view.state.doc.lineAt(cursor);
  const isLastLine = line.to === view.state.doc.length;

  let from = line.from;
  let to = line.to;

  if (!isLastLine) {
    to += 1;
  } else if (line.from > 0) {
    from -= 1;
  }

  view.dispatch({
    changes: { from, to, insert: '' },
    selection: { anchor: Math.max(0, from) },
    scrollIntoView: true,
  });
  return true;
}

function moveCurrentLine(view: EditorView, direction: 'up' | 'down'): boolean {
  if (!hasSingleSelection(view)) return false;

  const cursor = view.state.selection.main.head;
  const line = view.state.doc.lineAt(cursor);

  if (direction === 'up') {
    if (line.number <= 1) return true;
    const prevLine = view.state.doc.line(line.number - 1);
    const from = prevLine.from;
    const to = line.to;
    const swapped = `${line.text}\n${prevLine.text}`;
    const newCursor = Math.max(from, cursor - (prevLine.length + 1));
    view.dispatch({
      changes: { from, to, insert: swapped },
      selection: { anchor: newCursor },
      scrollIntoView: true,
    });
    return true;
  }

  if (line.number >= view.state.doc.lines) return true;
  const nextLine = view.state.doc.line(line.number + 1);
  const from = line.from;
  const to = nextLine.to;
  const swapped = `${nextLine.text}\n${line.text}`;
  const newCursor = Math.min(from + swapped.length, cursor + (nextLine.length + 1));
  view.dispatch({
    changes: { from, to, insert: swapped },
    selection: { anchor: newCursor },
    scrollIntoView: true,
  });
  return true;
}

// ── Hotkey entry → CodeMirror key notation translator ──────────────

// Convert "Ctrl+G" → "Mod-g", "Alt+ArrowUp" → "Alt-ArrowUp"
// CodeMirror uses: Mod (Cmd/Ctrl), Shift, Alt as prefixes, and capitalised key names.
function hotkeyToCodeMirror(key: string): string | null {
  const parts = key.split('+').map(p => p.trim()).filter(Boolean);
  if (parts.length === 0) return null;

  const modifiers: string[] = [];
  let mainKey = '';

  for (const part of parts) {
    const lower = part.toLowerCase();
    if (lower === 'ctrl' || lower === 'cmd') {
      modifiers.push('Mod');
    } else if (lower === 'shift') {
      modifiers.push('Shift');
    } else if (lower === 'alt' || lower === 'meta') {
      modifiers.push('Alt');
    } else {
      mainKey = part;
    }
  }

  if (!mainKey) return null;

  // Normalise the main key part for CodeMirror.
  // CodeMirror expects ArrowUp, ArrowDown, ArrowLeft, ArrowRight, etc.
  const keyMap: Record<string, string> = {
    'ArrowUp': 'ArrowUp',
    'ArrowDown': 'ArrowDown',
    'ArrowLeft': 'ArrowLeft',
    'ArrowRight': 'ArrowRight',
    'Backspace': 'Backspace',
    'Delete': 'Delete',
    'Enter': 'Enter',
    'Escape': 'Escape',
    'Home': 'Home',
    'End': 'End',
    'PageUp': 'PageUp',
    'PageDown': 'PageDown',
    'Tab': 'Tab',
    'Space': 'Space',
    'Backquote': '`',
  };

  const normalisedKey = keyMap[mainKey] ?? mainKey.toLowerCase();

  return [...modifiers, normalisedKey].join('-');
}

// ── Editor-specific command IDs that this module handles ────────────

const EDITOR_COMMAND_IDS = new Set([
  'save_file',
  'editor_goto_line',
  'editor_move_line_up',
  'editor_move_line_down',
  'editor_duplicate_line_up',
  'editor_duplicate_line_down',
  'editor_delete_line',
]);

// ── Public API ──────────────────────────────────────────────────────

/**
 * Build a CodeMirror keymap from user-configured hotkey entries.
 *
 * Only entries whose `command_id` is one of the editor-specific IDs
 * listed in `EDITOR_COMMAND_IDS` are wired up. Each entry's `key`
 * string is translated from the storage format (`Ctrl+G`) into
 * CodeMirror's format (`Mod-g`).
 *
 * Falls back to sensible defaults for any command_id that is missing
 * from the hotkey list, so the editor always has Save and Go-to-line
 * bound.
 */
export function getEditorKeymap(
  hotkeyEntries: HotkeyEntry[] | null,
  actions: EditorHotkeyActions,
): KeyBinding[] {
  const entries = hotkeyEntries ?? [];

  // Index entries by command_id for quick lookup.
  const byCommand = new Map<string, HotkeyEntry[]>();
  for (const entry of entries) {
    if (!EDITOR_COMMAND_IDS.has(entry.command_id)) continue;
    const list = byCommand.get(entry.command_id) ?? [];
    list.push(entry);
    byCommand.set(entry.command_id, list);
  }

  function bindingsFor(
    commandId: string,
    run: (view: EditorView) => boolean,
  ): KeyBinding[] {
    const group = byCommand.get(commandId) ?? [];
    const result: KeyBinding[] = [];
    for (const entry of group) {
      const cmKey = hotkeyToCodeMirror(entry.key);
      if (!cmKey) continue;
      result.push({ key: cmKey, preventDefault: true, run });
    }
    return result;
  }

  const bindings: KeyBinding[] = [];

  // Save
  const saveBindings = bindingsFor('save_file', () => {
    actions.onSave?.();
    return true;
  });
  if (saveBindings.length === 0) {
    // Fallback: Mod-s
    bindings.push({ key: 'Mod-s', preventDefault: true, run: () => { actions.onSave?.(); return true; } });
  } else {
    bindings.push(...saveBindings);
  }

  // Go to line
  const gotoBindings = bindingsFor('editor_goto_line', () => {
    actions.onGoToLine?.();
    return true;
  });
  if (gotoBindings.length === 0) {
    // Fallback: Mod-g
    bindings.push({ key: 'Mod-g', preventDefault: true, run: () => { actions.onGoToLine?.(); return true; } });
  } else {
    bindings.push(...gotoBindings);
  }

  // Line move / dup / delete — only add bindings if user configured them
  bindings.push(...bindingsFor('editor_move_line_up', (v) => moveCurrentLine(v, 'up')));
  bindings.push(...bindingsFor('editor_move_line_down', (v) => moveCurrentLine(v, 'down')));
  bindings.push(...bindingsFor('editor_duplicate_line_up', (v) => duplicateCurrentLine(v, 'up')));
  bindings.push(...bindingsFor('editor_duplicate_line_down', (v) => duplicateCurrentLine(v, 'down')));
  bindings.push(...bindingsFor('editor_delete_line', (v) => deleteCurrentLine(v)));

  return bindings;
}
