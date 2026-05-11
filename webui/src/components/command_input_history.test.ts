import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { dedupeCommands, persistCommandHistory, loadCommandHistory, saveCommandHistory } from './command_input_history';

// ---------------------------------------------------------------------------
// Mock localStorage
// ---------------------------------------------------------------------------

function setupLocalStorage() {
  const store: Record<string, string> = {};

  const mockStorage = {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => {
      store[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      Object.keys(store).forEach((k) => delete store[k]);
    }),
    get length() {
      return Object.keys(store).length;
    },
    key: vi.fn((i: number) => Object.keys(store)[i] ?? null),
  };

  Object.defineProperty(window, 'localStorage', {
    value: mockStorage,
    writable: true,
    configurable: true,
  });

  return mockStorage;
}

// ---------------------------------------------------------------------------
// dedupeCommands
// ---------------------------------------------------------------------------

describe('dedupeCommands', () => {
  it('removes duplicates keeping last occurrence', () => {
    expect(dedupeCommands(['a', 'b', 'a', 'c'])).toEqual(['b', 'a', 'c']);
  });

  it('trims each command before dedup', () => {
    expect(dedupeCommands(['  hello  ', 'hello'])).toEqual(['hello']);
  });

  it('filters out empty strings after trim', () => {
    expect(dedupeCommands(['', '  ', 'hello', '\t', 'world'])).toEqual(['hello', 'world']);
  });

  it('returns empty array for empty input', () => {
    expect(dedupeCommands([])).toEqual([]);
  });

  it('returns empty array when all commands are empty/whitespace', () => {
    expect(dedupeCommands(['', '  ', '\n', '\t'])).toEqual([]);
  });

  it('enforces MAX_COMMAND_HISTORY cap of 100', () => {
    const commands = Array.from({ length: 120 }, (_, i) => `cmd${i}`);
    const result = dedupeCommands(commands);
    expect(result).toHaveLength(100);
    // Should keep the last 100
    expect(result[0]).toBe('cmd20');
    expect(result[99]).toBe('cmd119');
  });

  it('preserves ordering with last occurrence moving to end', () => {
    // a appears at indices 0, 2, 4 → last occurrence is at index 4
    const input = ['a', 'b', 'a', 'c', 'a', 'd'];
    const result = dedupeCommands(input);
    expect(result).toEqual(['b', 'c', 'a', 'd']);
  });

  it('handles all duplicates in input', () => {
    expect(dedupeCommands(['same', 'same', 'same'])).toEqual(['same']);
  });

  it('handles single command', () => {
    expect(dedupeCommands(['hello'])).toEqual(['hello']);
  });

  it('trims then deduplicates', () => {
    // 'hello' and '  hello  ' should be considered the same
    expect(dedupeCommands(['  hello  ', 'world', 'hello'])).toEqual(['world', 'hello']);
  });

  it('handles mixed whitespace in duplicates', () => {
    expect(dedupeCommands([' hello ', ' hello', 'hello ', 'hello'])).toEqual(['hello']);
  });

  it('caps after trimming and dedup', () => {
    // Create 110 unique commands (after trim they're still unique)
    const commands = Array.from({ length: 110 }, (_, i) => `  cmd${i}  `);
    const result = dedupeCommands(commands);
    expect(result).toHaveLength(100);
    expect(result[0]).toBe('cmd10');
    expect(result[99]).toBe('cmd109');
  });
});

// ---------------------------------------------------------------------------
// persistCommandHistory
// ---------------------------------------------------------------------------

describe('persistCommandHistory', () => {
  let mockStorage: ReturnType<typeof setupLocalStorage>;

  beforeEach(() => {
    mockStorage = setupLocalStorage();
  });

  it('saves commands to localStorage under correct key', () => {
    persistCommandHistory(['hello', 'world']);
    expect(mockStorage.setItem).toHaveBeenCalledWith('sprout:chat-history', JSON.stringify(['hello', 'world']));
  });

  it('saves empty array', () => {
    persistCommandHistory([]);
    expect(mockStorage.setItem).toHaveBeenCalledWith('sprout:chat-history', JSON.stringify([]));
  });

  it('does not throw on failure', () => {
    vi.spyOn(mockStorage, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    expect(() => persistCommandHistory(['cmd'])).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// loadCommandHistory
// ---------------------------------------------------------------------------

describe('loadCommandHistory', () => {
  let mockStorage: ReturnType<typeof setupLocalStorage>;

  beforeEach(() => {
    mockStorage = setupLocalStorage();
  });

  it('returns empty array when localStorage is empty', async () => {
    mockStorage.getItem.mockReturnValue(null);
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual([]);
  });

  it('returns empty array for missing key', async () => {
    mockStorage.getItem.mockReturnValue(null);
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual([]);
  });

  it('loads and deduplicates from localStorage', async () => {
    mockStorage.getItem.mockReturnValue(JSON.stringify(['a', 'b', 'a', '  c  ', 'c']));
    const result = await loadCommandHistory({} as any);
    // 'a' deduped (last wins → moves to end), '  c  ' trimmed to 'c', then 'c' deduped (moves to end)
    expect(result).toEqual(['b', 'a', 'c']);
  });

  it('handles invalid JSON gracefully', async () => {
    mockStorage.getItem.mockReturnValue('not json');
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual([]);
  });

  it('handles non-array JSON gracefully', async () => {
    mockStorage.getItem.mockReturnValue(JSON.stringify({ foo: 'bar' }));
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual([]);
  });

  it('handles localStorage throwing (unavailable)', async () => {
    mockStorage.getItem.mockImplementation(() => {
      throw new Error('SecurityError');
    });
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual([]);
  });

  it('returns parsed array for valid JSON', async () => {
    mockStorage.getItem.mockReturnValue(JSON.stringify(['cmd1', 'cmd2']));
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual(['cmd1', 'cmd2']);
  });

  it('deduplicates loaded data', async () => {
    mockStorage.getItem.mockReturnValue(JSON.stringify(['x', 'y', 'x']));
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual(['y', 'x']);
  });
});

// ---------------------------------------------------------------------------
// saveCommandHistory
// ---------------------------------------------------------------------------

describe('saveCommandHistory', () => {
  let mockStorage: ReturnType<typeof setupLocalStorage>;

  beforeEach(() => {
    mockStorage = setupLocalStorage();
  });

  it('appends command, deduplicates, and persists', async () => {
    mockStorage.getItem.mockReturnValue(JSON.stringify(['old']));
    const result = await saveCommandHistory({} as any, ['old'], 'new');
    expect(result.commands).toEqual(['old', 'new']);
    expect(result.index).toBe(-1);
    expect(result.tempInput).toBe('');
    expect(mockStorage.setItem).toHaveBeenCalledWith('sprout:chat-history', JSON.stringify(['old', 'new']));
  });

  it('deduplicates new command against existing', async () => {
    mockStorage.getItem.mockReturnValue(JSON.stringify(['cmd']));
    const result = await saveCommandHistory({} as any, ['cmd'], 'cmd');
    expect(result.commands).toEqual(['cmd']);
    expect(result.index).toBe(-1);
  });

  it('trims the new command before adding', async () => {
    const result = await saveCommandHistory({} as any, [], '  trimmed  ');
    expect(result.commands).toEqual(['trimmed']);
  });

  it('skips empty command after trim', async () => {
    const result = await saveCommandHistory({} as any, ['existing'], '  ');
    expect(result.commands).toEqual(['existing']);
  });

  it('returns index of -1', async () => {
    const result = await saveCommandHistory({} as any, [], 'test');
    expect(result.index).toBe(-1);
  });

  it('returns tempInput of empty string', async () => {
    const result = await saveCommandHistory({} as any, [], 'test');
    expect(result.tempInput).toBe('');
  });

  it('enforces MAX_COMMAND_HISTORY cap when appending', async () => {
    const existing = Array.from({ length: 100 }, (_, i) => `cmd${i}`);
    const result = await saveCommandHistory({} as any, existing, 'cmd101');
    expect(result.commands).toHaveLength(100);
    expect(result.commands[0]).toBe('cmd1');
    expect(result.commands[99]).toBe('cmd101');
  });

  it('moves duplicate to end of list', async () => {
    const result = await saveCommandHistory({} as any, ['a', 'b', 'c'], 'a');
    expect(result.commands).toEqual(['b', 'c', 'a']);
  });
});
