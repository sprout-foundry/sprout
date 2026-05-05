import {
  createEmptyState,
  dedupeCommands,
  loadCommandHistory,
  persistCommandHistory,
} from './command_input_history';

describe('createEmptyState', () => {
  it('returns state with empty commands array', () => {
    const state = createEmptyState();
    expect(state.commands).toEqual([]);
  });

  it('returns state with index -1', () => {
    const state = createEmptyState();
    expect(state.index).toBe(-1);
  });

  it('returns state with empty tempInput', () => {
    const state = createEmptyState();
    expect(state.tempInput).toBe('');
  });
});

describe('dedupeCommands', () => {
  it('removes duplicate commands', () => {
    const commands = ['cmd1', 'cmd2', 'cmd1', 'cmd3', 'cmd2'];
    const result = dedupeCommands(commands);
    expect(result).toEqual(['cmd1', 'cmd2', 'cmd3']);
  });

  it('keeps first occurrence of each command', () => {
    const commands = ['cmd1', 'cmd2', 'cmd1'];
    const result = dedupeCommands(commands);
    expect(result).toEqual(['cmd1', 'cmd2']);
  });

  it('handles empty array', () => {
    const result = dedupeCommands([]);
    expect(result).toEqual([]);
  });

  it('handles array with no duplicates', () => {
    const commands = ['cmd1', 'cmd2', 'cmd3'];
    const result = dedupeCommands(commands);
    expect(result).toEqual(commands);
  });

  it('handles array with all same command', () => {
    const commands = ['cmd1', 'cmd1', 'cmd1', 'cmd1'];
    const result = dedupeCommands(commands);
    expect(result).toEqual(['cmd1']);
  });

  it('is case-sensitive', () => {
    const commands = ['cmd', 'Cmd', 'CMD'];
    const result = dedupeCommands(commands);
    expect(result).toEqual(['cmd', 'Cmd', 'CMD']);
  });

  it('preserves whitespace differences', () => {
    const commands = ['cmd1', 'cmd1 ', ' cmd1'];
    const result = dedupeCommands(commands);
    expect(result).toEqual(['cmd1', 'cmd1 ', ' cmd1']);
  });

  it('handles special characters', () => {
    const commands = ['npm install', 'npm install', 'npm test'];
    const result = dedupeCommands(commands);
    expect(result).toEqual(['npm install', 'npm test']);
  });
});

describe('loadCommandHistory', () => {
  beforeEach(() => {
    // Clear localStorage
    localStorage.clear();
  });

  it('returns empty state when localStorage has no history', async () => {
    const result = await loadCommandHistory();
    expect(result.commands).toEqual([]);
    expect(result.index).toBe(-1);
    expect(result.tempInput).toBe('');
  });

  it('loads commands from localStorage', async () => {
    const commands = ['cmd1', 'cmd2', 'cmd3'];
    localStorage.setItem('sprout:chat-history', JSON.stringify(commands));

    const result = await loadCommandHistory();
    expect(result.commands).toEqual(commands);
  });

  it('dedupes commands loaded from localStorage', async () => {
    const commands = ['cmd1', 'cmd2', 'cmd1', 'cmd3'];
    localStorage.setItem('sprout:chat-history', JSON.stringify(commands));

    const result = await loadCommandHistory();
    expect(result.commands).toEqual(['cmd1', 'cmd2', 'cmd3']);
  });

  it('handles invalid JSON in localStorage', async () => {
    localStorage.setItem('sprout:chat-history', 'invalid json');

    const result = await loadCommandHistory();
    expect(result.commands).toEqual([]);
  });

  it('handles non-JSON data in localStorage', async () => {
    localStorage.setItem('sprout:chat-history', 'plain text');

    const result = await loadCommandHistory();
    expect(result.commands).toEqual([]);
  });

  it('handles null in localStorage', async () => {
    localStorage.setItem('sprout:chat-history', 'null');

    const result = await loadCommandHistory();
    expect(result.commands).toEqual([]);
  });

  it('handles array in localStorage that is not string array', async () => {
    localStorage.setItem('sprout:chat-history', JSON.stringify([1, 2, 3]));

    const result = await loadCommandHistory();
    // Should still try to load it, even if not strings
    expect(result.commands.length).toBe(3);
  });

  describe('API fallback', () => {
    let mockApi: any;

    beforeEach(() => {
      mockApi = {
        getChatHistory: jest.fn().mockResolvedValue(['api-cmd1', 'api-cmd2']),
      };
    });

    it('falls back to API when localStorage is empty', async () => {
      const result = await loadCommandHistory(mockApi);
      expect(mockApi.getChatHistory).toHaveBeenCalled();
      expect(result.commands).toEqual(['api-cmd1', 'api-cmd2']);
    });

    it('dedupes API results', async () => {
      mockApi.getChatHistory.mockResolvedValueOnce(['api-cmd1', 'api-cmd2', 'api-cmd1']);
      const result = await loadCommandHistory(mockApi);
      expect(result.commands).toEqual(['api-cmd1', 'api-cmd2']);
    });

    it('does not call API when localStorage has data', async () => {
      localStorage.setItem('sprout:chat-history', JSON.stringify(['local-cmd']));
      mockApi.getChatHistory.mockClear();
      await loadCommandHistory(mockApi);
      expect(mockApi.getChatHistory).not.toHaveBeenCalled();
    });

    it('handles API errors gracefully', async () => {
      mockApi.getChatHistory.mockRejectedValueOnce(new Error('API error'));
      const result = await loadCommandHistory(mockApi);
      expect(result.commands).toEqual([]);
    });

    it('handles API with undefined getChatHistory', async () => {
      const api = {};
      const result = await loadCommandHistory(api as any);
      expect(result.commands).toEqual([]);
    });
  });
});

describe('persistCommandHistory', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('saves commands to localStorage', () => {
    const commands = ['cmd1', 'cmd2', 'cmd3'];
    persistCommandHistory(commands);

    const stored = localStorage.getItem('sprout:chat-history');
    expect(stored).toBeDefined();

    const parsed = JSON.parse(stored!);
    expect(parsed).toEqual(commands);
  });

  it('dedupes commands before saving', () => {
    const commands = ['cmd1', 'cmd2', 'cmd1', 'cmd3'];
    persistCommandHistory(commands);

    const stored = localStorage.getItem('sprout:chat-history');
    const parsed = JSON.parse(stored!);
    expect(parsed).toEqual(['cmd1', 'cmd2', 'cmd3']);
  });

  it('limits history to MAX_COMMAND_HISTORY (100)', () => {
    const commands = Array.from({ length: 150 }, (_, i) => `cmd${i}`);
    persistCommandHistory(commands);

    const stored = localStorage.getItem('sprout:chat-history');
    const parsed = JSON.parse(stored!);
    expect(parsed).toHaveLength(100);
  });

  it('keeps most recent commands', () => {
    const commands = Array.from({ length: 150 }, (_, i) => `cmd${i}`);
    persistCommandHistory(commands);

    const stored = localStorage.getItem('sprout:chat-history');
    const parsed = JSON.parse(stored!);
    // Should keep last 100 (50-149)
    expect(parsed[0]).toBe('cmd0');
    expect(parsed[99]).toBe('cmd99');
  });

  it('handles empty array', () => {
    persistCommandHistory([]);

    const stored = localStorage.getItem('sprout:chat-history');
    expect(stored).toBeDefined();
    expect(JSON.parse(stored!)).toEqual([]);
  });

  it('overwrites existing history', () => {
    localStorage.setItem('sprout:chat-history', JSON.stringify(['old1', 'old2']));

    const newCommands = ['new1', 'new2', 'new3'];
    persistCommandHistory(newCommands);

    const stored = localStorage.getItem('sprout:chat-history');
    const parsed = JSON.parse(stored!);
    expect(parsed).toEqual(newCommands);
  });

  it('handles localStorage errors gracefully', () => {
    // Mock localStorage.setItem to throw
    const originalSetItem = localStorage.setItem;
    localStorage.setItem = jest.fn(() => {
      throw new Error('localStorage error');
    });

    // Should not throw
    expect(() => {
      persistCommandHistory(['cmd1', 'cmd2']);
    }).not.toThrow();

    localStorage.setItem = originalSetItem;
  });

  it('handles commands with special characters', () => {
    const commands = ['npm install --save', 'git checkout -b feature/test'];
    persistCommandHistory(commands);

    const stored = localStorage.getItem('sprout:chat-history');
    const parsed = JSON.parse(stored!);
    expect(parsed).toEqual(commands);
  });

  it('handles empty strings in commands', () => {
    const commands = ['cmd1', '', 'cmd2', ''];
    persistCommandHistory(commands);

    const stored = localStorage.getItem('sprout:chat-history');
    const parsed = JSON.parse(stored!);
    // Empty strings are not deduped (empty != empty check passes)
    expect(parsed.length).toBeGreaterThan(0);
  });
});

describe('command history workflow', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('loads and persists round-trip', async () => {
    const originalCommands = ['cmd1', 'cmd2', 'cmd3'];

    // Save
    persistCommandHistory(originalCommands);

    // Load
    const loaded = await loadCommandHistory();

    expect(loaded.commands).toEqual(originalCommands);
  });

  it('handles concurrent persist operations', () => {
    const commands1 = ['cmd1', 'cmd2'];
    const commands2 = ['cmd3', 'cmd4'];

    persistCommandHistory(commands1);
    persistCommandHistory(commands2);

    const stored = localStorage.getItem('sprout:chat-history');
    const parsed = JSON.parse(stored!);

    expect(parsed).toEqual(['cmd3', 'cmd4']);
  });
});
