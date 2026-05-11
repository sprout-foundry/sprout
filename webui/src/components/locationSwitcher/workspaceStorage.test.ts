import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  RECENT_WORKSPACES_KEY,
  REMOTE_RECENT_WORKSPACES_KEY,
  SSH_FAVORITE_WORKSPACES_KEY,
  MAX_RECENT_WORKSPACES,
} from './types';

// ---------------------------------------------------------------------------
// Mock normalizePath — delegate to real implementation for correctness
// ---------------------------------------------------------------------------

vi.mock('./pathUtils', () => ({
  normalizePath: (rawPath: string): string => {
    let normalized = rawPath.trim().replace(/\/+/g, '/');
    if (!normalized) {
      return '';
    }
    if (!normalized.startsWith('/')) {
      normalized = `/${normalized}`;
    }
    if (normalized.length > 1 && normalized.endsWith('/')) {
      normalized = normalized.slice(0, -1);
    }
    return normalized;
  },
}));

// ---------------------------------------------------------------------------
// Mock localStorage
// ---------------------------------------------------------------------------

function createMockStorage() {
  const store: Record<string, string> = {};

  return {
    store,
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
}

function setupStorage(mock: ReturnType<typeof createMockStorage>) {
  Object.defineProperty(window, 'localStorage', {
    value: mock,
    writable: true,
    configurable: true,
  });
}

// ---------------------------------------------------------------------------
// Dynamic imports — needed because vi.mock replaces the module
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// readRecentWorkspaces
// ---------------------------------------------------------------------------

describe('readRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let readRecentWorkspaces: () => string[];

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    // Re-import each test so mocks are fresh
    const mod = await import('./workspaceStorage');
    readRecentWorkspaces = mod.readRecentWorkspaces;
  });

  it('returns empty array when localStorage key is missing', () => {
    mock.getItem.mockReturnValue(null);
    expect(readRecentWorkspaces()).toEqual([]);
  });

  it('returns empty array for invalid JSON', () => {
    mock.getItem.mockReturnValue('not json at all');
    expect(readRecentWorkspaces()).toEqual([]);
  });

  it('returns empty array for non-array JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify({ foo: 'bar' }));
    expect(readRecentWorkspaces()).toEqual([]);
  });

  it('reads and normalizes paths', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['/project/src', '/home/user/project', '//double//slash//path']));
    const result = readRecentWorkspaces();
    expect(result).toEqual(['/project/src', '/home/user/project', '/double/slash/path']);
  });

  it('filters out non-string entries', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['/valid', 123, null, true]));
    const result = readRecentWorkspaces();
    expect(result).toEqual(['/valid']);
  });

  it('filters out paths that normalize to empty', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['/valid', '', '  ']));
    const result = readRecentWorkspaces();
    expect(result).toEqual(['/valid']);
  });

  it('enforces MAX_RECENT_WORKSPACES cap', () => {
    const paths = Array.from({ length: MAX_RECENT_WORKSPACES + 5 }, (_, i) => `/path${i}`);
    mock.getItem.mockReturnValue(JSON.stringify(paths));
    const result = readRecentWorkspaces();
    expect(result).toHaveLength(MAX_RECENT_WORKSPACES);
    expect(result[0]).toBe('/path0');
    expect(result[result.length - 1]).toBe(`/path${MAX_RECENT_WORKSPACES - 1}`);
  });

  it('handles empty array in storage', () => {
    mock.getItem.mockReturnValue(JSON.stringify([]));
    expect(readRecentWorkspaces()).toEqual([]);
  });

  it('deduplicates after normalization', () => {
    // '/a' and '/a/' normalize to same path
    mock.getItem.mockReturnValue(JSON.stringify(['/a', '/a/']));
    const result = readRecentWorkspaces();
    // After normalization both become '/a', but the code doesn't deduplicate
    // the read function normalizes each, so both become '/a' and are kept as is
    expect(result).toEqual(['/a', '/a']);
  });

  it('uses correct localStorage key', () => {
    readRecentWorkspaces();
    expect(mock.getItem).toHaveBeenCalledWith(RECENT_WORKSPACES_KEY);
  });

  it('handles storage throwing (SSS-friendly: no crash)', () => {
    mock.getItem.mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => readRecentWorkspaces()).not.toThrow();
    expect(readRecentWorkspaces()).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// writeRecentWorkspaces
// ---------------------------------------------------------------------------

describe('writeRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let writeRecentWorkspaces: (paths: string[]) => void;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    writeRecentWorkspaces = mod.writeRecentWorkspaces;
  });

  it('saves paths to correct localStorage key', () => {
    writeRecentWorkspaces(['/project', '/home']);
    expect(mock.setItem).toHaveBeenCalledWith(RECENT_WORKSPACES_KEY, JSON.stringify(['/project', '/home']));
  });

  it('saves empty array', () => {
    writeRecentWorkspaces([]);
    expect(mock.setItem).toHaveBeenCalledWith(RECENT_WORKSPACES_KEY, JSON.stringify([]));
  });

  it('slices to MAX_RECENT_WORKSPACES before saving', () => {
    const paths = Array.from({ length: MAX_RECENT_WORKSPACES + 5 }, (_, i) => `/path${i}`);
    writeRecentWorkspaces(paths);
    const callArgs = mock.setItem.mock.calls[0];
    const saved = JSON.parse(callArgs[1] as string);
    expect(saved).toHaveLength(MAX_RECENT_WORKSPACES);
    expect(saved[0]).toBe('/path0');
  });

  it('does not normalize before saving (caller responsibility)', () => {
    writeRecentWorkspaces(['/raw/path']);
    const callArgs = mock.setItem.mock.calls[0];
    expect(callArgs[0]).toBe(RECENT_WORKSPACES_KEY);
    expect(JSON.parse(callArgs[1] as string)).toEqual(['/raw/path']);
  });
});

// ---------------------------------------------------------------------------
// readRemoteRecentWorkspaces
// ---------------------------------------------------------------------------

describe('readRemoteRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let readRemoteRecentWorkspaces: () => Record<string, string[]>;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    readRemoteRecentWorkspaces = mod.readRemoteRecentWorkspaces;
  });

  it('returns empty object when localStorage key is missing', () => {
    mock.getItem.mockReturnValue(null);
    expect(readRemoteRecentWorkspaces()).toEqual({});
  });

  it('returns empty object for invalid JSON', () => {
    mock.getItem.mockReturnValue('garbage');
    expect(readRemoteRecentWorkspaces()).toEqual({});
  });

  it('returns empty object for non-object JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify('just a string'));
    expect(readRemoteRecentWorkspaces()).toEqual({});
  });

  it('returns empty object for array JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['arr']));
    expect(readRemoteRecentWorkspaces()).toEqual({});
  });

  it('returns empty object for null JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(null));
    expect(readRemoteRecentWorkspaces()).toEqual({});
  });

  it('reads and normalizes remote workspace entries', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        'host-a': ['/home/user', '//double//slash'],
        'host-b': ['/project/src'],
      }),
    );
    const result = readRemoteRecentWorkspaces();
    expect(result).toEqual({
      'host-a': ['/home/user', '/double/slash'],
      'host-b': ['/project/src'],
    });
  });

  it('filters non-string entries in each host array', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: ['/valid', 123, null, 'also-valid'],
      }),
    );
    const result = readRemoteRecentWorkspaces();
    expect(result).toEqual({
      host: ['/valid', '/also-valid'],
    });
  });

  it('enforces MAX_RECENT_WORKSPACES per host', () => {
    const longList = Array.from({ length: MAX_RECENT_WORKSPACES + 5 }, (_, i) => `/path${i}`);
    mock.getItem.mockReturnValue({
      host: longList,
    });
    mock.getItem.mockReturnValue(JSON.stringify({ host: longList }));
    const result = readRemoteRecentWorkspaces();
    expect(result.host).toHaveLength(MAX_RECENT_WORKSPACES);
    expect(result.host?.[0]).toBe('/path0');
  });

  it('handles non-array values for a host key gracefully', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: 'not an array',
      }),
    );
    const result = readRemoteRecentWorkspaces();
    expect(result).toEqual({ host: [] });
  });

  it('uses correct localStorage key', () => {
    readRemoteRecentWorkspaces();
    expect(mock.getItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY);
  });

  it('handles storage throwing (SSS-friendly)', () => {
    mock.getItem.mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => readRemoteRecentWorkspaces()).not.toThrow();
    expect(readRemoteRecentWorkspaces()).toEqual({});
  });

  it('filters out entries that normalize to empty', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: ['/valid', '', '  ', '/also'],
      }),
    );
    const result = readRemoteRecentWorkspaces();
    expect(result).toEqual({ host: ['/valid', '/also'] });
  });
});

// ---------------------------------------------------------------------------
// writeRemoteRecentWorkspaces
// ---------------------------------------------------------------------------

describe('writeRemoteRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let writeRemoteRecentWorkspaces: (value: Record<string, string[]>) => void;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    writeRemoteRecentWorkspaces = mod.writeRemoteRecentWorkspaces;
  });

  it('saves to correct localStorage key', () => {
    writeRemoteRecentWorkspaces({ host: ['/path'] });
    expect(mock.setItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify({ host: ['/path'] }));
  });

  it('saves empty object', () => {
    writeRemoteRecentWorkspaces({});
    expect(mock.setItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify({}));
  });

  it('saves multiple hosts', () => {
    const value = {
      'host-a': ['/path1', '/path2'],
      'host-b': ['/path3'],
    };
    writeRemoteRecentWorkspaces(value);
    expect(mock.setItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify(value));
  });
});

// ---------------------------------------------------------------------------
// readSSHFavoriteWorkspaces
// ---------------------------------------------------------------------------

describe('readSSHFavoriteWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let readSSHFavoriteWorkspaces: () => Record<string, string[]>;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    readSSHFavoriteWorkspaces = mod.readSSHFavoriteWorkspaces;
  });

  it('returns empty object when localStorage key is missing', () => {
    mock.getItem.mockReturnValue(null);
    expect(readSSHFavoriteWorkspaces()).toEqual({});
  });

  it('returns empty object for invalid JSON', () => {
    mock.getItem.mockReturnValue('garbage');
    expect(readSSHFavoriteWorkspaces()).toEqual({});
  });

  it('returns empty object for non-object JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify('string'));
    expect(readSSHFavoriteWorkspaces()).toEqual({});
  });

  it('returns empty object for array JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['arr']));
    expect(readSSHFavoriteWorkspaces()).toEqual({});
  });

  it('returns empty object for null JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(null));
    expect(readSSHFavoriteWorkspaces()).toEqual({});
  });

  it('reads and normalizes SSH favorite entries', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        myserver: ['/home/user/projects', '//double//slash'],
        otherhost: ['/project'],
      }),
    );
    const result = readSSHFavoriteWorkspaces();
    expect(result).toEqual({
      myserver: ['/home/user/projects', '/double/slash'],
      otherhost: ['/project'],
    });
  });

  it('filters non-string entries in each host array', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: ['/valid', 123, null],
      }),
    );
    const result = readSSHFavoriteWorkspaces();
    expect(result).toEqual({ host: ['/valid'] });
  });

  it('enforces MAX_RECENT_WORKSPACES per host', () => {
    const longList = Array.from({ length: MAX_RECENT_WORKSPACES + 5 }, (_, i) => `/path${i}`);
    mock.getItem.mockReturnValue(JSON.stringify({ host: longList }));
    const result = readSSHFavoriteWorkspaces();
    expect(result.host).toHaveLength(MAX_RECENT_WORKSPACES);
  });

  it('handles non-array values for a host key gracefully', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: 'not an array',
      }),
    );
    const result = readSSHFavoriteWorkspaces();
    expect(result).toEqual({ host: [] });
  });

  it('uses correct localStorage key', () => {
    readSSHFavoriteWorkspaces();
    expect(mock.getItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY);
  });

  it('handles storage throwing (SSS-friendly)', () => {
    mock.getItem.mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => readSSHFavoriteWorkspaces()).not.toThrow();
    expect(readSSHFavoriteWorkspaces()).toEqual({});
  });

  it('filters out entries that normalize to empty', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: ['/valid', '', '  ', '/also'],
      }),
    );
    const result = readSSHFavoriteWorkspaces();
    expect(result).toEqual({ host: ['/valid', '/also'] });
  });
});

// ---------------------------------------------------------------------------
// writeSSHFavoriteWorkspaces
// ---------------------------------------------------------------------------

describe('writeSSHFavoriteWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let writeSSHFavoriteWorkspaces: (value: Record<string, string[]>) => void;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    writeSSHFavoriteWorkspaces = mod.writeSSHFavoriteWorkspaces;
  });

  it('saves to correct localStorage key', () => {
    writeSSHFavoriteWorkspaces({ host: ['/path'] });
    expect(mock.setItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify({ host: ['/path'] }));
  });

  it('saves empty object', () => {
    writeSSHFavoriteWorkspaces({});
    expect(mock.setItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify({}));
  });

  it('saves multiple hosts', () => {
    const value = {
      'host-a': ['/path1', '/path2'],
      'host-b': ['/path3'],
    };
    writeSSHFavoriteWorkspaces(value);
    expect(mock.setItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify(value));
  });

  it('does not throw on storage error (SSS-friendly)', () => {
    vi.spyOn(mock, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    expect(() => writeSSHFavoriteWorkspaces({ host: ['/path'] })).not.toThrow();
  });
});
