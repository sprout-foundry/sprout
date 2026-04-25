// @ts-nocheck

// Mock @codemirror/search to avoid ESM-only transitive dependencies
// (@marijn/find-cluster-break) that Jest/CRA cannot transform.
import { getEditorKeymap, getLineIndent } from './editorHotkeys';

jest.mock('@codemirror/search', () => ({
  selectSelectionMatches: jest.fn(() => true),
  selectNextOccurrence: jest.fn(() => true),
}));

jest.mock('@codemirror/state', () => ({
  EditorSelection: {
    create: jest.fn((ranges, mainIndex = 0) => ({
      ranges,
      mainIndex,
      main: ranges[mainIndex] || ranges[0],
    })),
    cursor: jest.fn((pos) => ({ anchor: pos, head: pos })),
    range: jest.fn((from, to) => ({ from, to })),
  },
  EditorState: {
    create: jest.fn(),
  },
  Compartment: class {
    of() { return this; }
    reconfigure() { return this; }
    get() { return null; }
  },
}));

jest.mock('@codemirror/view', () => ({
  EditorView: class {
    constructor() {}
    destroy() {}
    dispatch() {}
    focus() {}
    hasFocus() { return false; }
    contentDOM: {}
    dom: {}
    state: {}
    static scrollIntoView() { return {}; }
  },
  ViewPlugin: class {
    static fromClass() { return {}; }
  },
}));

jest.mock('@codemirror/language', () => ({
  bracketMatching: () => [],
  indentOnInput: () => [],
  foldGutter: () => [],
  foldKeymap: () => [],
}));

jest.mock('@codemirror/commands', () => ({
  toggleLineComment: jest.fn(() => true),
  toggleBlockComment: jest.fn(() => true),
  defaultKeymap: [],
  indentWithTab: {},
  history: () => [],
  undo: {},
  redo: {},
}));

jest.mock('../extensions/cursorHistory', () => ({
  navigateCursorBack: jest.fn(() => true),
  navigateCursorForward: jest.fn(() => true),
}));

describe('getLineIndent', () => {
  it('returns empty string for empty input', () => {
    expect(getLineIndent('')).toBe('');
  });

  it('returns empty string for line with no indentation', () => {
    expect(getLineIndent('const x = 1;')).toBe('');
  });

  it('returns leading spaces', () => {
    expect(getLineIndent('    const x = 1;')).toBe('    ');
  });

  it('returns leading tabs', () => {
    expect(getLineIndent('\t\tconst x = 1;')).toBe('\t\t');
  });

  it('returns mixed whitespace', () => {
    expect(getLineIndent('  \t  const x = 1;')).toBe('  \t  ');
  });

  it('returns full string for all-whitespace line', () => {
    expect(getLineIndent('    ')).toBe('    ');
  });
});

describe('getEditorKeymap', () => {
  const emptyActions = { onSave: jest.fn(), onGoToLine: jest.fn() };

  describe('hotkeyToCodeMirror (indirect via getEditorKeymap)', () => {
    it('translates Ctrl+Enter → Mod-Enter for editor_insert_line_below', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Enter', command_id: 'editor_insert_line_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
      expect(below!.preventDefault).toBe(true);
    });

    it('translates Ctrl+Shift+Enter → Mod-Shift-Enter for editor_insert_line_above', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_above' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
      expect(above!.preventDefault).toBe(true);
    });

    it('translates Cmd+Enter → Mod-Enter (Mac-style)', () => {
      const entries: HotkeyEntry[] = [{ key: 'Cmd+Enter', command_id: 'editor_insert_line_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
    });

    it('translates Cmd+Shift+Enter → Mod-Shift-Enter (Mac-style)', () => {
      const entries: HotkeyEntry[] = [{ key: 'Cmd+Shift+Enter', command_id: 'editor_insert_line_above' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
    });

    it('translates Ctrl+Shift+L → Mod-Shift-l for editor_select_all_occurrences', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+L', command_id: 'editor_select_all_occurrences' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
      expect(selAll!.preventDefault).toBe(true);
    });

    it('translates Cmd+Shift+L → Mod-Shift-l (Mac-style)', () => {
      const entries: HotkeyEntry[] = [{ key: 'Cmd+Shift+L', command_id: 'editor_select_all_occurrences' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
    });

    it('translates Ctrl+Shift+O → Mod-Shift-o for editor_goto_symbol', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+O', command_id: 'editor_goto_symbol' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const gotoSym = keymap.find((b) => b.key === 'Mod-Shift-o');
      expect(gotoSym).toBeDefined();
      expect(gotoSym!.preventDefault).toBe(true);
    });

    it('translates Cmd+Shift+O → Mod-Shift-o (Mac-style)', () => {
      const entries: HotkeyEntry[] = [{ key: 'Cmd+Shift+O', command_id: 'editor_goto_symbol' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const gotoSym = keymap.find((b) => b.key === 'Mod-Shift-o');
      expect(gotoSym).toBeDefined();
    });

    it('translates Ctrl+D → Mod-d for editor_add_selection_to_next_match', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+D', command_id: 'editor_add_selection_to_next_match' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const addNext = keymap.find((b) => b.key === 'Mod-d');
      expect(addNext).toBeDefined();
      expect(addNext!.preventDefault).toBe(true);
    });

    it('translates Cmd+D → Mod-d for editor_add_selection_to_next_match', () => {
      const entries: HotkeyEntry[] = [{ key: 'Cmd+D', command_id: 'editor_add_selection_to_next_match' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const addNext = keymap.find((b) => b.key === 'Mod-d');
      expect(addNext).toBeDefined();
    });
  });

  describe('editor_insert_line_below bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Enter', command_id: 'editor_insert_line_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
      expect(typeof below!.run).toBe('function');
    });
  });

  describe('editor_insert_line_above bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_above' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
      expect(typeof above!.run).toBe('function');
    });
  });

  describe('editor_select_all_occurrences bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+L', command_id: 'editor_select_all_occurrences' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
      expect(typeof selAll!.run).toBe('function');
    });
  });

  describe('editor_goto_symbol bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+O', command_id: 'editor_goto_symbol' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const gotoSym = keymap.find((b) => b.key === 'Mod-Shift-o');
      expect(gotoSym).toBeDefined();
      expect(typeof gotoSym!.run).toBe('function');
    });
  });

  describe('editor_add_selection_to_next_match bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+D', command_id: 'editor_add_selection_to_next_match' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const addNext = keymap.find((b) => b.key === 'Mod-d');
      expect(addNext).toBeDefined();
      expect(typeof addNext!.run).toBe('function');
    });
  });

  describe('editor_insert_cursor_above bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Alt+ArrowUp', command_id: 'editor_insert_cursor_above' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const cursorAbove = keymap.find((b) => b.key === 'Mod-Alt-ArrowUp');
      expect(cursorAbove).toBeDefined();
      expect(typeof cursorAbove!.run).toBe('function');
    });

    it('translates Cmd+Alt+ArrowUp → Mod-Alt-ArrowUp (Mac-style)', () => {
      const entries: HotkeyEntry[] = [{ key: 'Cmd+Alt+ArrowUp', command_id: 'editor_insert_cursor_above' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const cursorAbove = keymap.find((b) => b.key === 'Mod-Alt-ArrowUp');
      expect(cursorAbove).toBeDefined();
    });

    it('has no fallback binding when not configured', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      expect(keymap.some((b) => b.key === 'Mod-Alt-ArrowUp')).toBe(false);
    });
  });

  describe('editor_insert_cursor_below bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Alt+ArrowDown', command_id: 'editor_insert_cursor_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const cursorBelow = keymap.find((b) => b.key === 'Mod-Alt-ArrowDown');
      expect(cursorBelow).toBeDefined();
      expect(typeof cursorBelow!.run).toBe('function');
    });

    it('translates Cmd+Alt+ArrowDown → Mod-Alt-ArrowDown (Mac-style)', () => {
      const entries: HotkeyEntry[] = [{ key: 'Cmd+Alt+ArrowDown', command_id: 'editor_insert_cursor_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const cursorBelow = keymap.find((b) => b.key === 'Mod-Alt-ArrowDown');
      expect(cursorBelow).toBeDefined();
    });

    it('has no fallback binding when not configured', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      expect(keymap.some((b) => b.key === 'Mod-Alt-ArrowDown')).toBe(false);
    });
  });

  describe('editor_toggle_word_wrap bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [{ key: 'Alt+Z', command_id: 'editor_toggle_word_wrap' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      const toggleWrap = keymap.find((b) => b.key === 'Alt-z');
      expect(toggleWrap).toBeDefined();
      expect(typeof toggleWrap!.run).toBe('function');
    });

    it('invokes onToggleWordWrap and returns true when run is called', () => {
      const onToggleWordWrap = jest.fn();
      const actions = { onSave: jest.fn(), onGoToLine: jest.fn(), onToggleWordWrap };
      const entries: HotkeyEntry[] = [{ key: 'Alt+Z', command_id: 'editor_toggle_word_wrap' }];
      const keymap = getEditorKeymap(entries, actions);
      const toggleWrap = keymap.find((b) => b.key === 'Alt-z');
      expect(toggleWrap).toBeDefined();

      const result = toggleWrap!.run(null as any);
      expect(result).toBe(true);
      expect(onToggleWordWrap).toHaveBeenCalledTimes(1);
    });
  });

  describe('fallback defaults (no hotkey entries)', () => {
    it('includes Mod-Enter fallback for editor_insert_line_below when no entries provided', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
      expect(below!.preventDefault).toBe(true);
      expect(typeof below!.run).toBe('function');
    });

    it('includes Mod-Enter fallback when entries array is empty', () => {
      const keymap = getEditorKeymap([], emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
    });

    it('includes Mod-Shift-Enter fallback for editor_insert_line_above when no entries provided', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
      expect(above!.preventDefault).toBe(true);
      expect(typeof above!.run).toBe('function');
    });

    it('includes Mod-Shift-Enter fallback when entries array is empty', () => {
      const keymap = getEditorKeymap([], emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
    });

    it('includes Mod-Shift-l fallback for editor_select_all_occurrences when no entries provided', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
      expect(selAll!.preventDefault).toBe(true);
      expect(typeof selAll!.run).toBe('function');
    });

    it('includes Mod-Shift-l fallback when entries array is empty', () => {
      const keymap = getEditorKeymap([], emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
    });

    it('includes Mod-Shift-o fallback for editor_goto_symbol when no entries provided', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      const gotoSym = keymap.find((b) => b.key === 'Mod-Shift-o');
      expect(gotoSym).toBeDefined();
      expect(gotoSym!.preventDefault).toBe(true);
      expect(typeof gotoSym!.run).toBe('function');
    });

    it('includes Mod-Shift-o fallback for editor_goto_symbol when entries array is empty', () => {
      const keymap = getEditorKeymap([], emptyActions);
      const gotoSym = keymap.find((b) => b.key === 'Mod-Shift-o');
      expect(gotoSym).toBeDefined();
    });

    it('includes Mod-d fallback for editor_add_selection_to_next_match when no entries provided', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      const addNext = keymap.find((b) => b.key === 'Mod-d');
      expect(addNext).toBeDefined();
      expect(addNext!.preventDefault).toBe(true);
      expect(typeof addNext!.run).toBe('function');
    });

    it('includes Alt-z fallback for editor_toggle_word_wrap when no entries provided', () => {
      const keymap = getEditorKeymap(null, emptyActions);
      const toggleWrap = keymap.find((b) => b.key === 'Alt-z');
      expect(toggleWrap).toBeDefined();
      expect(toggleWrap!.preventDefault).toBe(true);
      expect(typeof toggleWrap!.run).toBe('function');
    });
  });

  describe('EDITOR_COMMAND_IDS coverage', () => {
    it('includes editor_insert_line_below as a handled command_id', () => {
      // Pass an entry with editor_insert_line_below and verify it produces
      // a binding (which means the command_id was recognised).
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Enter', command_id: 'editor_insert_line_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      // If the command_id were not in EDITOR_COMMAND_IDS, it would be
      // silently skipped and no Mod-Enter binding would exist.
      expect(keymap.some((b) => b.key === 'Mod-Enter')).toBe(true);
    });

    it('includes editor_insert_line_above as a handled command_id', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_above' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      expect(keymap.some((b) => b.key === 'Mod-Shift-Enter')).toBe(true);
    });

    it('includes editor_select_all_occurrences as a handled command_id', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+L', command_id: 'editor_select_all_occurrences' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      // If the command_id were not in EDITOR_COMMAND_IDS, it would be
      // silently skipped and no Mod-Shift-l binding would exist.
      expect(keymap.some((b) => b.key === 'Mod-Shift-l')).toBe(true);
    });

    it('includes editor_goto_symbol as a handled command_id', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+O', command_id: 'editor_goto_symbol' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      // If the command_id were not in EDITOR_COMMAND_IDS, it would be
      // silently skipped and no Mod-Shift-o binding would exist.
      expect(keymap.some((b) => b.key === 'Mod-Shift-o')).toBe(true);
    });

    it('includes editor_add_selection_to_next_match as a handled command_id', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+D', command_id: 'editor_add_selection_to_next_match' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      expect(keymap.some((b) => b.key === 'Mod-d')).toBe(true);
    });

    it('includes editor_toggle_word_wrap as a handled command_id', () => {
      const entries: HotkeyEntry[] = [{ key: 'Alt+Z', command_id: 'editor_toggle_word_wrap' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      expect(keymap.some((b) => b.key === 'Alt-z')).toBe(true);
    });

    it('includes editor_insert_cursor_above as a handled command_id', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Alt+ArrowUp', command_id: 'editor_insert_cursor_above' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      expect(keymap.some((b) => b.key === 'Mod-Alt-ArrowUp')).toBe(true);
    });

    it('includes editor_insert_cursor_below as a handled command_id', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Alt+ArrowDown', command_id: 'editor_insert_cursor_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      expect(keymap.some((b) => b.key === 'Mod-Alt-ArrowDown')).toBe(true);
    });

    it('ignores entries with unknown command_ids', () => {
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Enter', command_id: 'unknown_command' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      // The unknown command should not produce any binding, but the
      // fallback for insert_line_below should still be present.
      expect(keymap.some((b) => b.key === 'Mod-Enter')).toBe(true);
      // Only fallbacks + save + goto — no binding from the unknown entry.
      const knownKeys = [
        'Mod-s',
        'Mod-g',
        'Mod-Enter',
        'Mod-Shift-Enter',
        'Mod-Shift-l',
        'Mod-Shift-o',
        'Mod-d',
        'Alt-z',
        'Mod-Alt-r',
        'Alt-ArrowLeft',
        'Alt-ArrowRight',
        'Mod-k',
        'Mod-/',
        'Mod-Shift-/',
        'Mod-t',
      ];
      expect(keymap.every((b) => b.key != null && knownKeys.includes(b.key))).toBe(true);
    });
  });

  describe('user-configured keys override defaults', () => {
    it('uses the user key instead of the fallback when both could apply', () => {
      // Configure editor_insert_line_below with a custom key (Ctrl+Shift+Enter)
      // The fallback Mod-Enter should NOT appear since a binding exists.
      const entries: HotkeyEntry[] = [{ key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_below' }];
      const keymap = getEditorKeymap(entries, emptyActions);
      // The user key should be translated to Mod-Shift-Enter
      const custom = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(custom).toBeDefined();
      // There should NOT be a Mod-Enter binding for insert_line_below
      // (fallback should not be added when a user binding exists).
      // Note: Mod-Shift-Enter could also come from insert_line_above fallback,
      // so we count how many Mod-Shift-Enter bindings exist.
      const shiftEnterBindings = keymap.filter((b) => b.key === 'Mod-Shift-Enter');
      // One from user config (insert_line_below) + one from fallback (insert_line_above)
      expect(shiftEnterBindings.length).toBe(2);
      // No Mod-Enter fallback for insert_line_below should exist
      const modEnterBindings = keymap.filter((b) => b.key === 'Mod-Enter');
      expect(modEnterBindings.length).toBe(0);
    });
  });

  // ── Multi-cursor line operation tests ───────────────────────────────

  describe('multi-cursor line operations', () => {
    // Helper to create a mock EditorView with multiple cursors
    function createMockMultiCursorView(lines, cursorPositions) {
      // Build line data from the lines array
      const lineData = [];
      let pos = 0;
      for (let i = 0; i < lines.length; i++) {
        lineData.push({
          number: i + 1,
          text: lines[i],
          from: pos,
          to: pos + lines[i].length,
          length: lines[i].length,
        });
        pos += lines[i].length + 1; // +1 for newline
      }
      const fullText = lines.join('\n');

      // Build ranges from cursor positions
      const ranges = cursorPositions.map((p) => ({ from: p, to: p, head: p }));

      const doc = {
        toString: () => fullText,
        length: fullText.length,
        lines: lines.length,
        lineAt: (p) => {
          for (let i = lineData.length - 1; i >= 0; i--) {
            if (p >= lineData[i].from && (p <= lineData[i].to || (i === lineData.length - 1 && p === lineData[i].to + 1))) {
              return lineData[i];
            }
          }
          return lineData[0];
        },
        line: (n) => lineData[n - 1],
      };

      const dispatched = [];
      const mockView = {
        state: {
          doc,
          selection: { ranges, main: ranges[0], mainIndex: 0 },
        },
        dispatch: (tr) => dispatched.push(tr),
        focus: () => {},
      };

      return { view: mockView, dispatched, doc, lineData };
    }

    describe('multiple cursors - delete current line', () => {
      it('deletes lines at each cursor position', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['line1', 'line2', 'line3'],
          [0, 12]
        );

        // Ctrl+Shift+K → Mod-Shift-k
        const entries = [{ key: 'Ctrl+Shift+K', command_id: 'editor_delete_line' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const deleteBinding = keymap.find((b) => b.key === 'Mod-Shift-k');
        expect(deleteBinding).toBeDefined();

        const result = deleteBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);

        const tr = dispatched[0];
        // Should have changes for both lines (line1 and line3)
        expect(tr.changes).toBeDefined();
        // Should delete 2 unique lines (bottom-to-top order)
        expect(tr.changes.length).toBe(2);
        // Both should be deletions (empty insert)
        expect(tr.changes[0].insert).toBe('');
        expect(tr.changes[1].insert).toBe('');
        // Selection should have cursors for unique lines
        expect(tr.selection).toBeDefined();
        expect(tr.selection.ranges.length).toBe(2);
      });
    });

    describe('multiple cursors - duplicate current line (down)', () => {
      it('duplicates lines at each cursor position', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['line1', 'line2', 'line3'],
          [0, 12]
        );

        // Ctrl+Shift+D → Mod-Shift-d
        const entries = [{ key: 'Ctrl+Shift+D', command_id: 'editor_duplicate_line_down' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const dupBinding = keymap.find((b) => b.key === 'Mod-Shift-d');
        expect(dupBinding).toBeDefined();

        const result = dupBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        const tr = dispatched[0];
        expect(Array.isArray(tr.changes)).toBe(true);
        // 2 unique lines → 2 changes
        expect(tr.changes.length).toBe(2);
        expect(tr.changes[0].insert).toContain('line3');
        expect(tr.changes[1].insert).toContain('line1');
      });
    });

    describe('multiple cursors - duplicate current line (up)', () => {
      it('duplicates lines above each cursor position', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['line1', 'line2', 'line3'],
          [6, 18]
        );

        // Ctrl+Shift+Alt+D → Mod-Shift-Alt-d
        const entries = [{ key: 'Ctrl+Shift+Alt+D', command_id: 'editor_duplicate_line_up' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const dupBinding = keymap.find((b) => b.key === 'Mod-Shift-Alt-d');
        expect(dupBinding).toBeDefined();

        const result = dupBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        const tr = dispatched[0];
        expect(Array.isArray(tr.changes)).toBe(true);
        expect(tr.changes.length).toBe(2);
        expect(tr.changes[0].insert).toContain('line3');
        expect(tr.changes[1].insert).toContain('line2');
      });
    });

    describe('multiple cursors - insert line below', () => {
      it('inserts blank lines below each unique cursor line', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['  const x = 1;', '    let y = 2;'],
          [2, 18]
        );

        // Insert line below uses Mod-Enter fallback
        const keymap = getEditorKeymap(null, emptyActions);
        const belowBinding = keymap.find((b) => b.key === 'Mod-Enter');
        expect(belowBinding).toBeDefined();

        const result = belowBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        const tr = dispatched[0];
        expect(Array.isArray(tr.changes)).toBe(true);
        // 2 unique lines → 2 changes
        expect(tr.changes.length).toBe(2);
        expect(tr.changes[0].insert).toContain('\n');
        expect(tr.changes[1].insert).toContain('\n');
      });
    });

    describe('multiple cursors - insert line above', () => {
      it('inserts blank lines above each unique cursor line', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['  const x = 1;', '    let y = 2;'],
          [2, 18]
        );

        const keymap = getEditorKeymap(null, emptyActions);
        const aboveBinding = keymap.find((b) => b.key === 'Mod-Shift-Enter');
        expect(aboveBinding).toBeDefined();

        const result = aboveBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        const tr = dispatched[0];
        expect(Array.isArray(tr.changes)).toBe(true);
        expect(tr.changes.length).toBe(2);
        expect(tr.changes[0].insert).toContain('\n');
      });
    });

    describe('multiple cursors - move line up', () => {
      it('moves lines up from each cursor position', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['line1', 'line2', 'line3', 'line4'],
          [6, 18]
        );

        const entries = [{ key: 'Alt+ArrowUp', command_id: 'editor_move_line_up' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const moveUpBinding = keymap.find((b) => b.key === 'Alt-ArrowUp');
        expect(moveUpBinding).toBeDefined();

        const result = moveUpBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        const tr = dispatched[0];
        expect(Array.isArray(tr.changes)).toBe(true);
        // Should have 2 swap changes
        expect(tr.changes.length).toBe(2);
      });
    });

    describe('multiple cursors - same line deduplication', () => {
      it('only duplicates once when multiple cursors are on the same line', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['some line', 'another'],
          [2, 4]
        );

        const entries = [{ key: 'Ctrl+Shift+D', command_id: 'editor_duplicate_line_down' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const dupBinding = keymap.find((b) => b.key === 'Mod-Shift-d');
        expect(dupBinding).toBeDefined();

        const result = dupBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        const tr = dispatched[0];
        // Only 1 change because both cursors are on line 1
        expect(Array.isArray(tr.changes)).toBe(true);
        expect(tr.changes.length).toBe(1);
        expect(tr.changes[0].insert).toContain('some line');
      });
    });

    describe('single cursor - regression tests', () => {
      it('delete line works with single cursor', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['line1', 'line2', 'line3'],
          [0]
        );

        const entries = [{ key: 'Ctrl+Shift+K', command_id: 'editor_delete_line' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const deleteBinding = keymap.find((b) => b.key === 'Mod-Shift-k');

        const result = deleteBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        expect(dispatched[0].changes).toBeDefined();
      });

      it('duplicate line down works with single cursor', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['const x = 1;'],
          [5]
        );

        const entries = [{ key: 'Ctrl+Shift+D', command_id: 'editor_duplicate_line_down' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const dupBinding = keymap.find((b) => b.key === 'Mod-Shift-d');

        const result = dupBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        expect(dispatched[0].changes).toBeDefined();
      });

      it('insert line below works with single cursor', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['  indented'],
          [2]
        );

        const keymap = getEditorKeymap(null, emptyActions);
        const belowBinding = keymap.find((b) => b.key === 'Mod-Enter');

        const result = belowBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        expect(dispatched[0].changes.insert).toContain('  ');
      });

      it('insert line above works with single cursor', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['  indented'],
          [2]
        );

        const keymap = getEditorKeymap(null, emptyActions);
        const aboveBinding = keymap.find((b) => b.key === 'Mod-Shift-Enter');

        const result = aboveBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        expect(dispatched[0].changes.insert).toContain('  ');
      });

      it('move line up works with single cursor', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['line1', 'line2'],
          [6]
        );

        const entries = [{ key: 'Alt+ArrowUp', command_id: 'editor_move_line_up' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const moveUpBinding = keymap.find((b) => b.key === 'Alt-ArrowUp');

        const result = moveUpBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        expect(dispatched[0].changes.from).toBe(0);
        expect(dispatched[0].changes.to).toBe(11);
      });

      it('move line down works with single cursor', () => {
        const { view, dispatched } = createMockMultiCursorView(
          ['line1', 'line2'],
          [0]
        );

        const entries = [{ key: 'Alt+ArrowDown', command_id: 'editor_move_line_down' }];
        const keymap = getEditorKeymap(entries, emptyActions);
        const moveDownBinding = keymap.find((b) => b.key === 'Alt-ArrowDown');

        const result = moveDownBinding.run(view);
        expect(result).toBe(true);
        expect(dispatched.length).toBe(1);
        expect(dispatched[0].changes.from).toBe(0);
        expect(dispatched[0].changes.to).toBe(11);
      });
    });
  });
});
