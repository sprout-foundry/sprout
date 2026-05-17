import { toggleLineComment, toggleBlockComment } from '@codemirror/commands';
import { selectNextOccurrence, selectSelectionMatches } from '@codemirror/search';
import { EditorSelection } from '@codemirror/state';
import { type EditorView, type KeyBinding } from '@codemirror/view';
import { navigateCursorBack, navigateCursorForward } from '../extensions/cursorHistory';
import type { HotkeyEntry } from '../services/api';

export interface EditorHotkeyActions {
  onSave?: () => void;
  onGoToLine?: () => void;
  onGoToSymbol?: () => void;
  onGoToWorkspaceSymbol?: () => void;
  onToggleWordWrap?: () => void;
  onToggleRelativeLineNumbers?: () => void;
}

// ── Editor line-manipulation helpers ────────────────────────────────

function duplicateCurrentLine(view: EditorView, direction: 'up' | 'down' = 'down'): boolean {
  const state = view.state;
  const ranges = state.selection.ranges;

  // Single cursor: existing implementation
  if (ranges.length === 1) {
    const cursor = state.selection.main.head;
    const line = state.doc.lineAt(cursor);
    const lineText = line.text;
    const insertText = `${lineText}\n`;

    if (direction === 'up') {
      view.dispatch({
        changes: { from: line.from, insert: insertText },
        selection: { anchor: cursor + insertText.length },
        scrollIntoView: true,
      });
      return true;
    }

    const insertPos = line.to;
    const prefix = line.to === state.doc.length ? '\n' : '';
    view.dispatch({
      changes: { from: insertPos, insert: prefix + lineText },
      selection: {
        anchor: cursor + prefix.length + lineText.length + (line.to === state.doc.length ? 1 : 0),
      },
      scrollIntoView: true,
    });
    return true;
  }

  // Multi-cursor: collect unique lines, process bottom-to-top.
  // Use state.update() without explicit selection so CodeMirror maps
  // the current selection through the changes automatically (positions
  // in a TransactionSpec.selection refer to the post-change document,
  // so omitting it lets CM do the mapping for us).
  const lineSet = new Set<number>();
  for (const range of ranges) {
    lineSet.add(state.doc.lineAt(range.head).number);
  }
  const lineNumbers = Array.from(lineSet).sort((a, b) => b - a);

  const changes: { from: number; to: number; insert: string }[] = [];

  for (const lineNum of lineNumbers) {
    const line = state.doc.line(lineNum);
    const lineText = line.text;
    const insertText = `${lineText}\n`;

    if (direction === 'up') {
      changes.push({ from: line.from, to: line.from, insert: insertText });
    } else {
      const insertPos = line.to;
      const prefix = line.to === state.doc.length ? '\n' : '';
      changes.push({ from: insertPos, to: insertPos, insert: prefix + lineText });
    }
  }

  view.dispatch({ changes, scrollIntoView: true });
  return true;
}

function deleteCurrentLine(view: EditorView): boolean {
  const state = view.state;
  const ranges = state.selection.ranges;

  // Single cursor: existing implementation
  if (ranges.length === 1) {
    const cursor = state.selection.main.head;
    const line = state.doc.lineAt(cursor);
    const isLastLine = line.to === state.doc.length;

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

  // Multi-cursor: collect unique lines, process bottom-to-top.
  // Compute explicit post-change selection: each deleted line's cursor
  // collapses to the line's start position (or previous line if last).
  const lineSet = new Set<number>();
  for (const range of ranges) {
    const line = state.doc.lineAt(range.head);
    lineSet.add(line.number);
  }
  const lineNumbers = Array.from(lineSet).sort((a, b) => b - a);

  const changes: { from: number; to: number; insert: string }[] = [];
  const newRanges: { anchor: number }[] = [];

  for (const lineNum of lineNumbers) {
    const line = state.doc.line(lineNum);
    const isLastLine = line.to === state.doc.length;

    let from = line.from;
    let to = line.to;

    if (!isLastLine) {
      to += 1;
    } else if (line.from > 0) {
      from -= 1;
    }

    changes.push({ from, to, insert: '' });
    newRanges.push({ anchor: Math.max(0, from) });
  }

  view.dispatch({
    changes,
    selection: EditorSelection.create(
      newRanges.map((r) => EditorSelection.cursor(r.anchor)),
      0,
    ),
    scrollIntoView: true,
  });
  return true;
}

/** Extract leading whitespace (indentation) from a line of text. */
export function getLineIndent(text: string): string {
  const match = text.match(/^(\s*)/);
  return match ? match[1] : '';
}

// NOTE: Cursor placement is intentionally at column 0 of the new line
// (before the indentation). This matches CodeMirror's built-in
// `insertBlankLine` behavior for "below" and keeps the implementation
// simple. VS Code also places the cursor at the indentation level for
// "insert below" but at `Math.min(indent.length, cursorColumn)` for
// "insert above." We use the simpler column-0 approach for both, which
// is consistent with CodeMirror's built-in editor behavior.

function insertLineBelow(view: EditorView): boolean {
  const state = view.state;
  const ranges = state.selection.ranges;

  // Single cursor: existing implementation
  if (ranges.length === 1) {
    const line = state.doc.lineAt(state.selection.main.head);
    const indent = getLineIndent(line.text);
    const endOfLine = line.to;
    const insertText = `\n${indent}`;
    view.dispatch({
      changes: { from: endOfLine, insert: insertText },
      selection: { anchor: endOfLine + 1 },
      scrollIntoView: true,
    });
    return true;
  }

  // Multi-cursor: collect unique lines, process bottom-to-top.
  // Omit explicit selection so CodeMirror maps cursors through changes.
  const lineSet = new Set<number>();
  for (const range of ranges) {
    lineSet.add(state.doc.lineAt(range.head).number);
  }
  const lineNumbers = Array.from(lineSet).sort((a, b) => b - a);

  const changes: { from: number; to: number; insert: string }[] = [];
  for (const lineNum of lineNumbers) {
    const line = state.doc.line(lineNum);
    const indent = getLineIndent(line.text);
    const endOfLine = line.to;
    changes.push({ from: endOfLine, to: endOfLine, insert: `\n${indent}` });
  }

  view.dispatch({ changes, scrollIntoView: true });
  return true;
}

function insertLineAbove(view: EditorView): boolean {
  const state = view.state;
  const ranges = state.selection.ranges;

  // Single cursor: existing implementation
  if (ranges.length === 1) {
    const line = state.doc.lineAt(state.selection.main.head);
    const indent = getLineIndent(line.text);
    const insertText = `${indent}\n`;
    view.dispatch({
      changes: { from: line.from, insert: insertText },
      selection: { anchor: line.from },
      scrollIntoView: true,
    });
    return true;
  }

  // Multi-cursor: collect unique lines, process bottom-to-top.
  // Omit explicit selection so CodeMirror maps cursors through changes.
  const lineSet = new Set<number>();
  for (const range of ranges) {
    lineSet.add(state.doc.lineAt(range.head).number);
  }
  const lineNumbers = Array.from(lineSet).sort((a, b) => b - a);

  const changes: { from: number; to: number; insert: string }[] = [];
  for (const lineNum of lineNumbers) {
    const line = state.doc.line(lineNum);
    const indent = getLineIndent(line.text);
    changes.push({ from: line.from, to: line.from, insert: `${indent}\n` });
  }

  view.dispatch({ changes, scrollIntoView: true });
  return true;
}

function moveCurrentLine(view: EditorView, direction: 'up' | 'down'): boolean {
  const state = view.state;
  const ranges = state.selection.ranges;

  // Single cursor: existing implementation
  if (ranges.length === 1) {
    const cursor = state.selection.main.head;
    const line = state.doc.lineAt(cursor);

    if (direction === 'up') {
      if (line.number <= 1) return true;
      const prevLine = state.doc.line(line.number - 1);
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

    if (line.number >= state.doc.lines) return true;
    const nextLine = state.doc.line(line.number + 1);
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

  // Multi-cursor: collect unique lines, determine processing order
  // Use state.update() without explicit selection so CodeMirror maps
  // cursors through the changes automatically. This correctly handles
  // position shifts after each line swap.
  const lineSet = new Set<number>();
  for (const range of ranges) {
    lineSet.add(state.doc.lineAt(range.head).number);
  }

  // 'up': process top-to-bottom (lower line numbers first)
  // 'down': process bottom-to-top (higher line numbers first)
  const lineNumbers = Array.from(lineSet).sort((a, b) => (direction === 'up' ? a - b : b - a));

  const changes: { from: number; to: number; insert: string }[] = [];

  for (const lineNum of lineNumbers) {
    const line = state.doc.line(lineNum);

    if (direction === 'up') {
      if (line.number <= 1) continue;
      const prevLine = state.doc.line(line.number - 1);
      const from = prevLine.from;
      const to = line.to;
      const swapped = `${line.text}\n${prevLine.text}`;
      changes.push({ from, to, insert: swapped });
    } else {
      if (line.number >= state.doc.lines) continue;
      const nextLine = state.doc.line(line.number + 1);
      const from = line.from;
      const to = nextLine.to;
      const swapped = `${nextLine.text}\n${line.text}`;
      changes.push({ from, to, insert: swapped });
    }
  }

  if (changes.length === 0) return true;

  view.dispatch({ changes, scrollIntoView: true });
  return true;
}

function insertCursorAbove(view: EditorView): boolean {
  const state = view.state;
  const originalRanges = state.selection.ranges;
  const addedRanges = originalRanges.flatMap((r) => {
    const line = state.doc.lineAt(r.head);
    if (line.number <= 1) return [];
    const prevLine = state.doc.line(line.number - 1);
    const col = Math.min(r.head - line.from, prevLine.length);
    return [EditorSelection.range(prevLine.from + col, prevLine.from + col)];
  });
  if (addedRanges.length === 0) return true;
  const tr = state.update({
    selection: EditorSelection.create([...originalRanges, ...addedRanges], state.selection.mainIndex),
  });
  if (tr.selection) {
    view.dispatch(tr);
  }
  return true;
}

function insertCursorBelow(view: EditorView): boolean {
  const state = view.state;
  const originalRanges = state.selection.ranges;
  const addedRanges = originalRanges.flatMap((r) => {
    const line = state.doc.lineAt(r.head);
    if (line.number >= state.doc.lines) return [];
    const nextLine = state.doc.line(line.number + 1);
    const col = Math.min(r.head - line.from, nextLine.length);
    return [EditorSelection.range(nextLine.from + col, nextLine.from + col)];
  });
  if (addedRanges.length === 0) return true;
  const tr = state.update({
    selection: EditorSelection.create([...originalRanges, ...addedRanges], state.selection.mainIndex),
  });
  if (tr.selection) {
    view.dispatch(tr);
  }
  return true;
}

// ── Hotkey entry → CodeMirror key notation translator ──────────────

// Convert "Ctrl+G" → "Mod-g", "Alt+ArrowUp" → "Alt-ArrowUp"
// CodeMirror uses: Mod (Cmd/Ctrl), Shift, Alt as prefixes, and capitalised key names.
function hotkeyToCodeMirror(key: string): string | null {
  const parts = key
    .split('+')
    .map((p) => p.trim())
    .filter(Boolean);
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
    ArrowUp: 'ArrowUp',
    ArrowDown: 'ArrowDown',
    ArrowLeft: 'ArrowLeft',
    ArrowRight: 'ArrowRight',
    Backspace: 'Backspace',
    Delete: 'Delete',
    Enter: 'Enter',
    Escape: 'Escape',
    Home: 'Home',
    End: 'End',
    PageUp: 'PageUp',
    PageDown: 'PageDown',
    Tab: 'Tab',
    Space: 'Space',
    Backquote: '`',
  };

  const normalisedKey = keyMap[mainKey] ?? mainKey.toLowerCase();

  return [...modifiers, normalisedKey].join('-');
}

// ── Editor-specific command IDs that this module handles ────────────

const EDITOR_COMMAND_IDS = new Set([
  'save_file',
  'editor_goto_line',
  'editor_goto_symbol',
  'editor_workspace_symbol',
  'editor_move_line_up',
  'editor_move_line_down',
  'editor_duplicate_line_up',
  'editor_duplicate_line_down',
  'editor_delete_line',
  'editor_insert_line_below',
  'editor_insert_line_above',
  'editor_select_all_occurrences',
  'editor_add_selection_to_next_match',
  'editor_toggle_word_wrap',
  'editor_toggle_relative_line_numbers',
  'editor_navigate_back',
  'editor_navigate_forward',
  'split_editor_horizontal',
  'editor_insert_cursor_above',
  'editor_insert_cursor_below',
  'editor_toggle_line_comment',
  'editor_toggle_block_comment',
  'toggle_linked_scroll',
  'toggle_minimap',
  'editor_cycle_whitespace_rendering',
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
export function getEditorKeymap(hotkeyEntries: HotkeyEntry[] | null, actions: EditorHotkeyActions): KeyBinding[] {
  const entries = hotkeyEntries ?? [];

  // Index entries by command_id for quick lookup.
  const byCommand = new Map<string, HotkeyEntry[]>();
  for (const entry of entries) {
    if (!EDITOR_COMMAND_IDS.has(entry.command_id)) continue;
    const list = byCommand.get(entry.command_id) ?? [];
    list.push(entry);
    byCommand.set(entry.command_id, list);
  }

  function bindingsFor(commandId: string, run: (view: EditorView) => boolean): KeyBinding[] {
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
    bindings.push({
      key: 'Mod-s',
      preventDefault: true,
      run: () => {
        actions.onSave?.();
        return true;
      },
    });
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
    bindings.push({
      key: 'Mod-g',
      preventDefault: true,
      run: () => {
        actions.onGoToLine?.();
        return true;
      },
    });
  } else {
    bindings.push(...gotoBindings);
  }

  // Go to symbol
  const gotoSymbolBindings = bindingsFor('editor_goto_symbol', () => {
    actions.onGoToSymbol?.();
    return true;
  });
  if (gotoSymbolBindings.length === 0) {
    // Fallback: Mod-Shift-o (Ctrl+Shift+O / Cmd+Shift+O)
    bindings.push({
      key: 'Mod-Shift-o',
      preventDefault: true,
      run: () => {
        actions.onGoToSymbol?.();
        return true;
      },
    });
  } else {
    bindings.push(...gotoSymbolBindings);
  }

  // Line move / dup / delete — only add bindings if user configured them
  bindings.push(...bindingsFor('editor_move_line_up', (v) => moveCurrentLine(v, 'up')));
  bindings.push(...bindingsFor('editor_move_line_down', (v) => moveCurrentLine(v, 'down')));
  bindings.push(...bindingsFor('editor_duplicate_line_up', (v) => duplicateCurrentLine(v, 'up')));
  bindings.push(...bindingsFor('editor_duplicate_line_down', (v) => duplicateCurrentLine(v, 'down')));

  // Intentionally no fallback for editor_delete_line — we do not want
  // to auto-bind any key to line deletion. Users must explicitly map a
  // key (e.g. Ctrl+Shift+K in VS Code preset) to enable this command.
  bindings.push(...bindingsFor('editor_delete_line', (v) => deleteCurrentLine(v)));

  // Insert line below — fallback to Mod-Enter (same as CodeMirror's
  // built-in insertBlankLine binding we're overriding).
  const insertBelowBindings = bindingsFor('editor_insert_line_below', (v) => insertLineBelow(v));
  if (insertBelowBindings.length === 0) {
    bindings.push({ key: 'Mod-Enter', preventDefault: true, run: (v) => insertLineBelow(v) });
  } else {
    bindings.push(...insertBelowBindings);
  }

  // Insert line above — fallback to Mod-Shift-Enter (VS Code default).
  const insertAboveBindings = bindingsFor('editor_insert_line_above', (v) => insertLineAbove(v));
  if (insertAboveBindings.length === 0) {
    bindings.push({ key: 'Mod-Shift-Enter', preventDefault: true, run: (v) => insertLineAbove(v) });
  } else {
    bindings.push(...insertAboveBindings);
  }

  // Select all occurrences — fallback to Mod-Shift-l (Ctrl+Shift+L).
  // NOTE: CodeMirror's searchKeymap also binds Mod-Shift-l → selectAllOccurrences.
  // This custom keymap MUST be registered after searchKeymap in the extension
  // array (see EditorPane.tsx) so our binding takes priority and remains
  // consistent with the user-configurable hotkey system.
  const selectAllOccBindings = bindingsFor('editor_select_all_occurrences', (v) => {
    return selectSelectionMatches(v);
  });
  if (selectAllOccBindings.length === 0) {
    bindings.push({
      key: 'Mod-Shift-l',
      preventDefault: true,
      run: (v) => selectSelectionMatches(v),
    });
  } else {
    bindings.push(...selectAllOccBindings);
  }

  // Add selection to next find match — fallback to Mod-d (Ctrl+D / Cmd+D).
  // CodeMirror's searchKeymap also binds Mod-d → selectNextOccurrence, but
  // this explicit entry makes the binding visible in the configurable hotkey
  // system so users can discover and remap it in settings.
  const addNextMatchBindings = bindingsFor('editor_add_selection_to_next_match', (v) => {
    return selectNextOccurrence(v);
  });
  if (addNextMatchBindings.length === 0) {
    bindings.push({ key: 'Mod-d', preventDefault: true, run: (v) => selectNextOccurrence(v) });
  } else {
    bindings.push(...addNextMatchBindings);
  }

  // Toggle word wrap — fallback to Alt-z (VS Code default).
  const toggleWordWrapBindings = bindingsFor('editor_toggle_word_wrap', () => {
    actions.onToggleWordWrap?.();
    return true;
  });
  if (toggleWordWrapBindings.length === 0) {
    bindings.push({
      key: 'Alt-z',
      preventDefault: true,
      run: () => {
        actions.onToggleWordWrap?.();
        return true;
      },
    });
  } else {
    bindings.push(...toggleWordWrapBindings);
  }

  // Toggle relative line numbers — fallback to Ctrl-Alt-r (vim-friendly).
  const toggleRelativeLineNumbersBindings = bindingsFor('editor_toggle_relative_line_numbers', () => {
    actions.onToggleRelativeLineNumbers?.();
    return true;
  });
  if (toggleRelativeLineNumbersBindings.length === 0) {
    bindings.push({
      key: 'Mod-Alt-r',
      preventDefault: true,
      run: () => {
        actions.onToggleRelativeLineNumbers?.();
        return true;
      },
    });
  } else {
    bindings.push(...toggleRelativeLineNumbersBindings);
  }

  // Navigate back — fallback to Alt-ArrowLeft (VS Code default).
  // NOTE: CodeMirror's defaultKeymap binds Alt-ArrowLeft to cursorSyntaxLeft.
  // This custom keymap is registered AFTER defaultKeymap in EditorPane.tsx,
  // so our binding takes priority.
  const navBackBindings = bindingsFor('editor_navigate_back', (v) => {
    return navigateCursorBack(v);
  });
  if (navBackBindings.length === 0) {
    bindings.push({
      key: 'Alt-ArrowLeft',
      preventDefault: true,
      run: (v) => navigateCursorBack(v),
    });
  } else {
    bindings.push(...navBackBindings);
  }

  // Navigate forward — fallback to Alt-ArrowRight (VS Code default).
  const navForwardBindings = bindingsFor('editor_navigate_forward', (v) => {
    return navigateCursorForward(v);
  });
  if (navForwardBindings.length === 0) {
    bindings.push({
      key: 'Alt-ArrowRight',
      preventDefault: true,
      run: (v) => navigateCursorForward(v),
    });
  } else {
    bindings.push(...navForwardBindings);
  }

  // Split editor horizontal — fallback to Mod-k (Ctrl+K / Cmd+K).
  // NOTE: CodeMirror's defaultKeymap (emacsStyleKeymap) binds Mod-k →
  // deleteToLineEnd. This custom binding is registered AFTER defaultKeymap
  // in EditorPane.tsx, so it takes priority and replaces the Emacs-style
  // delete-to-end-of-line behavior when the hotkey is configured.
  // We dispatch a global sprout:hotkey event so AppContent handles the
  // actual split, and return true to prevent CodeMirror's deleteToLineEnd
  // from also firing.
  const splitHorizBindings = bindingsFor('split_editor_horizontal', () => {
    window.dispatchEvent(
      new CustomEvent('sprout:hotkey', {
        detail: { commandId: 'split_editor_horizontal' },
      }),
    );
    return true;
  });
  if (splitHorizBindings.length === 0) {
    bindings.push({
      key: 'Mod-k',
      preventDefault: true,
      run: () => {
        window.dispatchEvent(
          new CustomEvent('sprout:hotkey', {
            detail: { commandId: 'split_editor_horizontal' },
          }),
        );
        return true;
      },
    });
  } else {
    bindings.push(...splitHorizBindings);
  }

  // Insert cursor above (Ctrl+Alt+ArrowUp) — multi-cursor editing.
  // No fallback: only bound when explicitly configured to avoid
  // conflicts with Alt+ArrowUp (move line up).
  bindings.push(...bindingsFor('editor_insert_cursor_above', (v) => insertCursorAbove(v)));

  // Insert cursor below (Ctrl+Alt+ArrowDown) — multi-cursor editing.
  // No fallback: only bound when explicitly configured to avoid
  // conflicts with Alt+ArrowDown (move line down).
  bindings.push(...bindingsFor('editor_insert_cursor_below', (v) => insertCursorBelow(v)));

  // Toggle line comment — fallback to Mod-/ (Ctrl+/ in VS Code).
  // NOTE: CodeMirror's defaultKeymap also binds Mod-/ → toggleComment.
  // This explicit binding overrides it to use toggleLineComment instead,
  // which always uses line comments (matching VS Code behavior) rather
  // than auto-detecting between line and block comments.
  const toggleLineCommentBindings = bindingsFor('editor_toggle_line_comment', (v) => {
    return toggleLineComment(v);
  });
  if (toggleLineCommentBindings.length === 0) {
    bindings.push({
      key: 'Mod-/',
      preventDefault: true,
      run: (v) => toggleLineComment(v),
    });
  } else {
    bindings.push(...toggleLineCommentBindings);
  }

  // Toggle block comment — fallback to Mod-Shift-/ (Ctrl+Shift+/ in VS Code).
  // CodeMirror's defaultKeymap binds Shift-Alt-a → toggleBlockComment instead.
  // Our binding uses the more standard Ctrl+Shift+/ (VS Code convention).
  const toggleBlockCommentBindings = bindingsFor('editor_toggle_block_comment', (v) => {
    return toggleBlockComment(v);
  });
  if (toggleBlockCommentBindings.length === 0) {
    bindings.push({
      key: 'Mod-Shift-/',
      preventDefault: true,
      run: (v) => toggleBlockComment(v),
    });
  } else {
    bindings.push(...toggleBlockCommentBindings);
  }

  // Toggle linked scrolling for split panes showing the same file.
  // No default key binding — users must explicitly configure one.
  bindings.push(
    ...bindingsFor('toggle_linked_scroll', () => {
      window.dispatchEvent(
        new CustomEvent('sprout:hotkey', {
          detail: { commandId: 'toggle_linked_scroll' },
        }),
      );
      return true;
    }),
  );

  // Toggle minimap visibility in the editor gutter.
  // No default key binding — users must explicitly configure one.
  bindings.push(
    ...bindingsFor('toggle_minimap', () => {
      window.dispatchEvent(
        new CustomEvent('sprout:hotkey', {
          detail: { commandId: 'toggle_minimap' },
        }),
      );
      return true;
    }),
  );

  // Cycle whitespace rendering mode (none → boundary → all).
  // No default key binding — users must explicitly configure one.
  bindings.push(
    ...bindingsFor('editor_cycle_whitespace_rendering', () => {
      document.dispatchEvent(new CustomEvent('editor-cycle-whitespace-rendering'));
      return true;
    }),
  );

  // Go to workspace symbol — fallback Mod-t (Ctrl+T / Cmd+T) matching the preset definition.
  const gotoWorkspaceSymbolBindings = bindingsFor('editor_workspace_symbol', () => {
    actions.onGoToWorkspaceSymbol?.();
    return true;
  });
  if (gotoWorkspaceSymbolBindings.length === 0) {
    bindings.push({
      key: 'Mod-t',
      preventDefault: true,
      run: () => {
        actions.onGoToWorkspaceSymbol?.();
        return true;
      },
    });
  } else {
    bindings.push(...gotoWorkspaceSymbolBindings);
  }

  return bindings;
}
