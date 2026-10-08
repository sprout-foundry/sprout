import { beforeEach, describe, expect, it } from 'vitest';
import {
  clearRecentFiles,
  loadRecentFiles,
  pruneRecentFiles,
  recordRecentFile,
  RECENT_FILES_LIMIT,
} from './paletteRecents';

function memStorage(): Storage {
  const map = new Map<string, string>();
  return {
    getLength: () => map.size,
    key: (i: number) => Array.from(map.keys())[i] ?? null,
    clear: () => map.clear(),
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
  };
}

const file = (name: string, path: string) => ({ name, path, type: 'file' });

describe('paletteRecents', () => {
  let storage: Storage;
  beforeEach(() => {
    storage = memStorage();
    clearRecentFiles(storage);
  });

  it('isolates recents per workspace root', () => {
    recordRecentFile('/repo-a', file('a.ts', 'src/a.ts'), storage);
    recordRecentFile('/repo-a', file('b.ts', 'src/b.ts'), storage);
    recordRecentFile('/repo-b', file('c.ts', 'lib/c.ts'), storage);

    expect(loadRecentFiles('/repo-a', storage).map((f) => f.path)).toEqual(['src/b.ts', 'src/a.ts']);
    expect(loadRecentFiles('/repo-b', storage).map((f) => f.path)).toEqual(['lib/c.ts']);
    expect(loadRecentFiles('', storage)).toEqual([]);
  });

  it('a workspace switch cannot surface another workspace\u2019s files', () => {
    recordRecentFile('/repo-a', file('old.ts', 'src/old.ts'), storage);
    // The new workspace has its own (empty) bucket.
    expect(loadRecentFiles('/repo-b', storage)).toEqual([]);
  });

  it('dedupes by path and moves the re-opened file to the front', () => {
    recordRecentFile('/r', file('a.ts', 'a.ts'), storage);
    recordRecentFile('/r', file('b.ts', 'b.ts'), storage);
    const next = recordRecentFile('/r', file('a2.ts', 'a.ts'), storage);
    expect(next.map((f) => f.path)).toEqual(['a.ts', 'b.ts']);
    expect(next[0].name).toBe('a2.ts');
  });

  it('caps the per-workspace list', () => {
    for (let i = 0; i < RECENT_FILES_LIMIT + 5; i++) {
      recordRecentFile('/r', file(`f${i}.ts`, `f${i}.ts`), storage);
    }
    const list = loadRecentFiles('/r', storage);
    expect(list).toHaveLength(RECENT_FILES_LIMIT);
    expect(list[0].path).toBe(`f${RECENT_FILES_LIMIT + 4}.ts`);
  });

  it('caps the number of stored roots, evicting the oldest', () => {
    for (let i = 0; i < 10; i++) {
      recordRecentFile(`/root-${i}`, file('x.ts', 'x.ts'), storage);
    }
    const raw = JSON.parse(storage.getItem('sprout.commandPalette.recentFiles.v2') ?? '{}') as {
      roots: Record<string, unknown>;
    };
    const rootKeys = Object.keys(raw.roots);
    expect(rootKeys).toHaveLength(8);
    expect(rootKeys).not.toContain('/root-0');
    expect(rootKeys).toContain('/root-9');
  });

  it('prunes entries that no longer exist in the index', () => {
    recordRecentFile('/r', file('gone.ts', 'gone.ts'), storage);
    recordRecentFile('/r', file('here.ts', 'here.ts'), storage);
    const next = pruneRecentFiles('/r', (p) => p !== 'gone.ts', storage);
    expect(next.map((f) => f.path)).toEqual(['here.ts']);
  });

  it('survives corrupt storage', () => {
    storage.setItem('sprout.commandPalette.recentFiles.v2', '{not json');
    expect(loadRecentFiles('/r', storage)).toEqual([]);
    expect(() => recordRecentFile('/r', file('x.ts', 'x.ts'), storage)).not.toThrow();
    expect(loadRecentFiles('/r', storage)).toHaveLength(1);
  });

  it('ignores v1-shaped payloads', () => {
    storage.setItem(
      'sprout.commandPalette.recentFiles.v2',
      JSON.stringify([{ name: 'x', path: '/abs/x', type: 'file' }]),
    );
    expect(loadRecentFiles('/r', storage)).toEqual([]);
  });
});
