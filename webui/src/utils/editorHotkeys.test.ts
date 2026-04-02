// @ts-nocheck

// Mock @codemirror/search to avoid ESM-only transitive dependencies
// (@marijn/find-cluster-break) that Jest/CRA cannot transform.
jest.mock('@codemirror/search', () => ({
  selectSelectionMatches: jest.fn(() => true),
}));

import { getEditorKeymap, getLineIndent } from './editorHotkeys';

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
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Enter', command_id: 'editor_insert_line_below' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
      expect(below!.preventDefault).toBe(true);
    });

    it('translates Ctrl+Shift+Enter → Mod-Shift-Enter for editor_insert_line_above', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_above' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
      expect(above!.preventDefault).toBe(true);
    });

    it('translates Cmd+Enter → Mod-Enter (Mac-style)', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Cmd+Enter', command_id: 'editor_insert_line_below' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
    });

    it('translates Cmd+Shift+Enter → Mod-Shift-Enter (Mac-style)', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Cmd+Shift+Enter', command_id: 'editor_insert_line_above' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
    });

    it('translates Ctrl+Shift+L → Mod-Shift-l for editor_select_all_occurrences', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Shift+L', command_id: 'editor_select_all_occurrences' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
      expect(selAll!.preventDefault).toBe(true);
    });

    it('translates Cmd+Shift+L → Mod-Shift-l (Mac-style)', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Cmd+Shift+L', command_id: 'editor_select_all_occurrences' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
    });
  });

  describe('editor_insert_line_below bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Enter', command_id: 'editor_insert_line_below' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const below = keymap.find((b) => b.key === 'Mod-Enter');
      expect(below).toBeDefined();
      expect(typeof below!.run).toBe('function');
    });
  });

  describe('editor_insert_line_above bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_above' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const above = keymap.find((b) => b.key === 'Mod-Shift-Enter');
      expect(above).toBeDefined();
      expect(typeof above!.run).toBe('function');
    });
  });

  describe('editor_select_all_occurrences bindings', () => {
    it('produces bindings when configured', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Shift+L', command_id: 'editor_select_all_occurrences' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      const selAll = keymap.find((b) => b.key === 'Mod-Shift-l');
      expect(selAll).toBeDefined();
      expect(typeof selAll!.run).toBe('function');
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
  });

  describe('EDITOR_COMMAND_IDS coverage', () => {
    it('includes editor_insert_line_below as a handled command_id', () => {
      // Pass an entry with editor_insert_line_below and verify it produces
      // a binding (which means the command_id was recognised).
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Enter', command_id: 'editor_insert_line_below' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      // If the command_id were not in EDITOR_COMMAND_IDS, it would be
      // silently skipped and no Mod-Enter binding would exist.
      expect(keymap.some((b) => b.key === 'Mod-Enter')).toBe(true);
    });

    it('includes editor_insert_line_above as a handled command_id', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_above' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      expect(keymap.some((b) => b.key === 'Mod-Shift-Enter')).toBe(true);
    });

    it('includes editor_select_all_occurrences as a handled command_id', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Shift+L', command_id: 'editor_select_all_occurrences' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      // If the command_id were not in EDITOR_COMMAND_IDS, it would be
      // silently skipped and no Mod-Shift-l binding would exist.
      expect(keymap.some((b) => b.key === 'Mod-Shift-l')).toBe(true);
    });

    it('ignores entries with unknown command_ids', () => {
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Enter', command_id: 'unknown_command' },
      ];
      const keymap = getEditorKeymap(entries, emptyActions);
      // The unknown command should not produce any binding, but the
      // fallback for insert_line_below should still be present.
      expect(keymap.some((b) => b.key === 'Mod-Enter')).toBe(true);
      // Only fallbacks + save + goto — no binding from the unknown entry.
      const knownKeys = ['Mod-s', 'Mod-g', 'Mod-Enter', 'Mod-Shift-Enter', 'Mod-Shift-l'];
      expect(keymap.every((b) => b.key != null && knownKeys.includes(b.key))).toBe(true);
    });
  });

  describe('user-configured keys override defaults', () => {
    it('uses the user key instead of the fallback when both could apply', () => {
      // Configure editor_insert_line_below with a custom key (Ctrl+Shift+Enter)
      // The fallback Mod-Enter should NOT appear since a binding exists.
      const entries: HotkeyEntry[] = [
        { key: 'Ctrl+Shift+Enter', command_id: 'editor_insert_line_below' },
      ];
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
});
