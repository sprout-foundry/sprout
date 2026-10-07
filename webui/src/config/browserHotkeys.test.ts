import { describe, expect, it } from 'vitest';
import { BROWSER_DEFAULT_HOTKEYS } from './browserHotkeys';

describe('BROWSER_DEFAULT_HOTKEYS', () => {
  it('binds reveal-in-explorer and both copy-path commands', () => {
    const ids = new Set(BROWSER_DEFAULT_HOTKEYS.map((h) => h.command_id));
    expect(ids.has('editor_reveal_in_explorer')).toBe(true);
    expect(ids.has('editor_copy_relative_path')).toBe(true);
    expect(ids.has('editor_copy_absolute_path')).toBe(true);
  });

  it('binds each of them to exactly one key with Shift+Alt as the modifier', () => {
    for (const id of ['editor_reveal_in_explorer', 'editor_copy_relative_path', 'editor_copy_absolute_path']) {
      const bindings = BROWSER_DEFAULT_HOTKEYS.filter((h) => h.command_id === id);
      expect(bindings).toHaveLength(1);
      expect(bindings[0].key.startsWith('Shift+Alt+')).toBe(true);
      expect(bindings[0].global).toBe(false);
    }
  });

  it('has no duplicate key bindings across the whole set', () => {
    const seen = new Set<string>();
    for (const h of BROWSER_DEFAULT_HOTKEYS) {
      expect(seen.has(h.key), `duplicate binding for ${h.key}`).toBe(false);
      seen.add(h.key);
    }
  });
});
