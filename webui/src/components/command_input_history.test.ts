// @ts-nocheck

import { loadCommandHistory, saveCommandHistory, dedupeCommands, persistCommandHistory } from './command_input_history';

describe('command_input_history', () => {
  const origGetItem = Object.getOwnPropertyDescriptor(Storage.prototype, 'getItem');
  const origSetItem = Object.getOwnPropertyDescriptor(Storage.prototype, 'setItem');

  beforeEach(() => {
    jest.restoreAllMocks();
    jest.clearAllMocks();
    localStorage.clear();
  });

  afterAll(() => {
    if (origGetItem) Object.defineProperty(Storage.prototype, 'getItem', origGetItem);
    if (origSetItem) Object.defineProperty(Storage.prototype, 'setItem', origSetItem);
  });

  it('loads history from localStorage', async () => {
    localStorage.setItem('ledit:chat-history', JSON.stringify(['cmd1', 'cmd2', 'cmd1', 'cmd3']));

    const result = await loadCommandHistory({} as any);
    expect(result).toEqual(['cmd2', 'cmd1', 'cmd3']);
  });

  it('returns empty history when localStorage is empty', async () => {
    const result = await loadCommandHistory({} as any);
    expect(result).toEqual([]);
  });

  it('returns empty history when localStorage is corrupted', async () => {
    localStorage.setItem('ledit:chat-history', 'not-json');

    const result = await loadCommandHistory({} as any);
    expect(result).toEqual([]);
  });

  it('trims and deduplicates commands', () => {
    const result = dedupeCommands([' first ', '', 'second', 'first', 'third ']);
    expect(result).toEqual(['second', 'first', 'third']);
  });

  it('deduplicates but preserves last occurrence', () => {
    const result = dedupeCommands(['alpha', 'beta', 'alpha']);
    expect(result).toEqual(['beta', 'alpha']);
  });

  it('saveCommandHistory persists deduplicated commands to localStorage', async () => {
    const result = await saveCommandHistory({} as any, ['old'], 'new');
    expect(result).toEqual({
      commands: ['old', 'new'],
      index: -1,
      tempInput: '',
    });
    expect(JSON.parse(localStorage.getItem('ledit:chat-history'))).toEqual(['old', 'new']);
  });
});
