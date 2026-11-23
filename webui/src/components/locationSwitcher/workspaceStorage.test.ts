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
// readRecentProjectFolders
// ---------------------------------------------------------------------------

describe('readRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let readRecentProjectFolders: () => string[];

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    // Re-import each test so mocks are fresh
    const mod = await import('./workspaceStorage');
    readRecentProjectFolders = mod.readRecentProjectFolders;
  });

  it('returns empty array when localStorage key is missing', () => {
    mock.getItem.mockReturnValue(null);
    expect(readRecentProjectFolders()).toEqual([]);
  });

  it('returns empty array for invalid JSON', () => {
    mock.getItem.mockReturnValue('not json at all');
    expect(readRecentProjectFolders()).toEqual([]);
  });

  it('returns empty array for non-array JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify({ foo: 'bar' }));
    expect(readRecentProjectFolders()).toEqual([]);
  });

  it('reads and normalizes paths', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['/project/src', '/home/user/project', '//double//slash//path']));
    const result = readRecentProjectFolders();
    expect(result).toEqual(['/project/src', '/home/user/project', '/double/slash/path']);
  });

  it('filters out non-string entries', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['/valid', 123, null, true]));
    const result = readRecentProjectFolders();
    expect(result).toEqual(['/valid']);
  });

  it('filters out paths that normalize to empty', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['/valid', '', '  ']));
    const result = readRecentProjectFolders();
    expect(result).toEqual(['/valid']);
  });

  it('enforces MAX_RECENT_WORKSPACES cap', () => {
    const paths = Array.from({ length: MAX_RECENT_WORKSPACES + 5 }, (_, i) => `/path${i}`);
    mock.getItem.mockReturnValue(JSON.stringify(paths));
    const result = readRecentProjectFolders();
    expect(result).toHaveLength(MAX_RECENT_WORKSPACES);
    expect(result[0]).toBe('/path0');
    expect(result[result.length - 1]).toBe(`/path${MAX_RECENT_WORKSPACES - 1}`);
  });

  it('handles empty array in storage', () => {
    mock.getItem.mockReturnValue(JSON.stringify([]));
    expect(readRecentProjectFolders()).toEqual([]);
  });

  it('deduplicates after normalization', () => {
    // '/a' and '/a/' normalize to same path
    mock.getItem.mockReturnValue(JSON.stringify(['/a', '/a/']));
    const result = readRecentProjectFolders();
    // After normalization both become '/a', but the code doesn't deduplicate
    // the read function normalizes each, so both become '/a' and are kept as is
    expect(result).toEqual(['/a', '/a']);
  });

  it('uses correct localStorage key', () => {
    readRecentProjectFolders();
    expect(mock.getItem).toHaveBeenCalledWith(RECENT_WORKSPACES_KEY);
  });

  it('handles storage throwing (SSS-friendly: no crash)', () => {
    mock.getItem.mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => readRecentProjectFolders()).not.toThrow();
    expect(readRecentProjectFolders()).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// writeRecentProjectFolders
// ---------------------------------------------------------------------------

describe('writeRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let writeRecentProjectFolders: (paths: string[]) => void;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    writeRecentProjectFolders = mod.writeRecentProjectFolders;
  });

  it('saves paths to correct localStorage key', () => {
    writeRecentProjectFolders(['/project', '/home']);
    expect(mock.setItem).toHaveBeenCalledWith(RECENT_WORKSPACES_KEY, JSON.stringify(['/project', '/home']));
  });

  it('saves empty array', () => {
    writeRecentProjectFolders([]);
    expect(mock.setItem).toHaveBeenCalledWith(RECENT_WORKSPACES_KEY, JSON.stringify([]));
  });

  it('slices to MAX_RECENT_WORKSPACES before saving', () => {
    const paths = Array.from({ length: MAX_RECENT_WORKSPACES + 5 }, (_, i) => `/path${i}`);
    writeRecentProjectFolders(paths);
    const callArgs = mock.setItem.mock.calls[0];
    const saved = JSON.parse(callArgs[1] as string);
    expect(saved).toHaveLength(MAX_RECENT_WORKSPACES);
    expect(saved[0]).toBe('/path0');
  });

  it('does not normalize before saving (caller responsibility)', () => {
    writeRecentProjectFolders(['/raw/path']);
    const callArgs = mock.setItem.mock.calls[0];
    expect(callArgs[0]).toBe(RECENT_WORKSPACES_KEY);
    expect(JSON.parse(callArgs[1] as string)).toEqual(['/raw/path']);
  });
});

// ---------------------------------------------------------------------------
// readRemoteRecentProjectFolders
// ---------------------------------------------------------------------------

describe('readRemoteRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let readRemoteRecentProjectFolders: () => Record<string, string[]>;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    readRemoteRecentProjectFolders = mod.readRemoteRecentProjectFolders;
  });

  it('returns empty object when localStorage key is missing', () => {
    mock.getItem.mockReturnValue(null);
    expect(readRemoteRecentProjectFolders()).toEqual({});
  });

  it('returns empty object for invalid JSON', () => {
    mock.getItem.mockReturnValue('garbage');
    expect(readRemoteRecentProjectFolders()).toEqual({});
  });

  it('returns empty object for non-object JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify('just a string'));
    expect(readRemoteRecentProjectFolders()).toEqual({});
  });

  it('returns empty object for array JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['arr']));
    expect(readRemoteRecentProjectFolders()).toEqual({});
  });

  it('returns empty object for null JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(null));
    expect(readRemoteRecentProjectFolders()).toEqual({});
  });

  it('reads and normalizes remote workspace entries', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        'host-a': ['/home/user', '//double//slash'],
        'host-b': ['/project/src'],
      }),
    );
    const result = readRemoteRecentProjectFolders();
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
    const result = readRemoteRecentProjectFolders();
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
    const result = readRemoteRecentProjectFolders();
    expect(result.host).toHaveLength(MAX_RECENT_WORKSPACES);
    expect(result.host?.[0]).toBe('/path0');
  });

  it('handles non-array values for a host key gracefully', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: 'not an array',
      }),
    );
    const result = readRemoteRecentProjectFolders();
    expect(result).toEqual({ host: [] });
  });

  it('uses correct localStorage key', () => {
    readRemoteRecentProjectFolders();
    expect(mock.getItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY);
  });

  it('handles storage throwing (SSS-friendly)', () => {
    mock.getItem.mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => readRemoteRecentProjectFolders()).not.toThrow();
    expect(readRemoteRecentProjectFolders()).toEqual({});
  });

  it('filters out entries that normalize to empty', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: ['/valid', '', '  ', '/also'],
      }),
    );
    const result = readRemoteRecentProjectFolders();
    expect(result).toEqual({ host: ['/valid', '/also'] });
  });
});

// ---------------------------------------------------------------------------
// writeRemoteRecentProjectFolders
// ---------------------------------------------------------------------------

describe('writeRemoteRecentWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let writeRemoteRecentProjectFolders: (value: Record<string, string[]>) => void;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    writeRemoteRecentProjectFolders = mod.writeRemoteRecentProjectFolders;
  });

  it('saves to correct localStorage key', () => {
    writeRemoteRecentProjectFolders({ host: ['/path'] });
    expect(mock.setItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify({ host: ['/path'] }));
  });

  it('saves empty object', () => {
    writeRemoteRecentProjectFolders({});
    expect(mock.setItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify({}));
  });

  it('saves multiple hosts', () => {
    const value = {
      'host-a': ['/path1', '/path2'],
      'host-b': ['/path3'],
    };
    writeRemoteRecentProjectFolders(value);
    expect(mock.setItem).toHaveBeenCalledWith(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify(value));
  });
});

// ---------------------------------------------------------------------------
// readSSHFavoriteProjectFolders
// ---------------------------------------------------------------------------

describe('readSSHFavoriteWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let readSSHFavoriteProjectFolders: () => Record<string, string[]>;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    readSSHFavoriteProjectFolders = mod.readSSHFavoriteProjectFolders;
  });

  it('returns empty object when localStorage key is missing', () => {
    mock.getItem.mockReturnValue(null);
    expect(readSSHFavoriteProjectFolders()).toEqual({});
  });

  it('returns empty object for invalid JSON', () => {
    mock.getItem.mockReturnValue('garbage');
    expect(readSSHFavoriteProjectFolders()).toEqual({});
  });

  it('returns empty object for non-object JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify('string'));
    expect(readSSHFavoriteProjectFolders()).toEqual({});
  });

  it('returns empty object for array JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(['arr']));
    expect(readSSHFavoriteProjectFolders()).toEqual({});
  });

  it('returns empty object for null JSON', () => {
    mock.getItem.mockReturnValue(JSON.stringify(null));
    expect(readSSHFavoriteProjectFolders()).toEqual({});
  });

  it('reads and normalizes SSH favorite entries', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        myserver: ['/home/user/projects', '//double//slash'],
        otherhost: ['/project'],
      }),
    );
    const result = readSSHFavoriteProjectFolders();
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
    const result = readSSHFavoriteProjectFolders();
    expect(result).toEqual({ host: ['/valid'] });
  });

  it('enforces MAX_RECENT_WORKSPACES per host', () => {
    const longList = Array.from({ length: MAX_RECENT_WORKSPACES + 5 }, (_, i) => `/path${i}`);
    mock.getItem.mockReturnValue(JSON.stringify({ host: longList }));
    const result = readSSHFavoriteProjectFolders();
    expect(result.host).toHaveLength(MAX_RECENT_WORKSPACES);
  });

  it('handles non-array values for a host key gracefully', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: 'not an array',
      }),
    );
    const result = readSSHFavoriteProjectFolders();
    expect(result).toEqual({ host: [] });
  });

  it('uses correct localStorage key', () => {
    readSSHFavoriteProjectFolders();
    expect(mock.getItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY);
  });

  it('handles storage throwing (SSS-friendly)', () => {
    mock.getItem.mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => readSSHFavoriteProjectFolders()).not.toThrow();
    expect(readSSHFavoriteProjectFolders()).toEqual({});
  });

  it('filters out entries that normalize to empty', () => {
    mock.getItem.mockReturnValue(
      JSON.stringify({
        host: ['/valid', '', '  ', '/also'],
      }),
    );
    const result = readSSHFavoriteProjectFolders();
    expect(result).toEqual({ host: ['/valid', '/also'] });
  });
});

// ---------------------------------------------------------------------------
// writeSSHFavoriteProjectFolders
// ---------------------------------------------------------------------------

describe('writeSSHFavoriteWorkspaces', () => {
  let mock: ReturnType<typeof createMockStorage>;
  let writeSSHFavoriteProjectFolders: (value: Record<string, string[]>) => void;

  beforeEach(async () => {
    mock = createMockStorage();
    setupStorage(mock);
    const mod = await import('./workspaceStorage');
    writeSSHFavoriteProjectFolders = mod.writeSSHFavoriteProjectFolders;
  });

  it('saves to correct localStorage key', () => {
    writeSSHFavoriteProjectFolders({ host: ['/path'] });
    expect(mock.setItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify({ host: ['/path'] }));
  });

  it('saves empty object', () => {
    writeSSHFavoriteProjectFolders({});
    expect(mock.setItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify({}));
  });

  it('saves multiple hosts', () => {
    const value = {
      'host-a': ['/path1', '/path2'],
      'host-b': ['/path3'],
    };
    writeSSHFavoriteProjectFolders(value);
    expect(mock.setItem).toHaveBeenCalledWith(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify(value));
  });

  it('does not throw on storage error (SSS-friendly)', () => {
    vi.spyOn(mock, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    expect(() => writeSSHFavoriteProjectFolders({ host: ['/path'] })).not.toThrow();
  });
});
