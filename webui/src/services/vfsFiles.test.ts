import { describe, expect, it } from 'vitest';
import {
  getVfsManifestSnapshot,
  listVfsFileEntries,
  resolveWorkspacePath,
  trackFileWrite,
  untrackFileWrite,
} from './vfsFiles';
import type { WasmDirEntry, WasmShell } from './wasmShell';

/** A minimal WasmShell: listDir + a workspace root. */
function fakeShell(tree: Record<string, WasmDirEntry[] | { error: string }> = {}): WasmShell {
  const listDir = (path: string): { entries: WasmDirEntry[]; error?: string } => {
    const entry = tree[path];
    if (entry === undefined) return { entries: [] };
    if ('error' in entry) return { entries: [], error: entry.error };
    return { entries: entry };
  };
  return {
    listDir,
    getWorkspaceRoot: () => '/workspace',
    getCwd: () => '/workspace',
  } as unknown as WasmShell;
}

/** Seed the write manifest from a clean slate (it is a module singleton). */
function seedManifest(paths: string[]): void {
  for (const p of Array.from(getVfsManifestSnapshot())) untrackFileWrite(p);
  for (const p of paths) trackFileWrite(p);
}

describe('listVfsFileEntries', () => {
  it('lists the workspace files with size and mode, workspace-relative', () => {
    const shell = fakeShell({
      '/workspace': [
        { name: 'src', type: 'dir', size: 0, mode: 0 },
        { name: 'README.md', type: 'file', size: 5, mode: 0o644 },
      ],
      '/workspace/src': [{ name: 'a.ts', type: 'file', size: 10, mode: 0o644 }],
    });
    expect(listVfsFileEntries(shell)).toEqual([
      { path: 'README.md', size: 5, mode: 0o644 },
      { path: 'src/a.ts', size: 10, mode: 0o644 },
    ]);
  });

  it('excludes .git from the listing', () => {
    const shell = fakeShell({
      '/workspace': [
        { name: '.git', type: 'dir', size: 0, mode: 0 },
        { name: 'app.ts', type: 'file', size: 1, mode: 0o644 },
      ],
      '/workspace/.git': [{ name: 'config', type: 'file', size: 100, mode: 0o644 }],
    });
    // The walk starts at the workspace root; .git is skipped inside it. The
    // runtime dirs (siblings of the root) are never walked, so only the
    // project's own files remain.
    expect(listVfsFileEntries(shell)).toEqual([{ path: 'app.ts', size: 1, mode: 0o644 }]);
  });

  it('reports an empty (but healthy) workspace as no files', () => {
    seedManifest(['/workspace/src/a.ts']); // a manifest must not leak in
    expect(listVfsFileEntries(fakeShell({}))).toEqual([]);
  });

  it('falls back to the write manifest when the binary listing is broken', () => {
    seedManifest(['/workspace/src/a.ts', '/workspace/b.txt', '/home/user/cfg', '/tmp/x']);
    const shell = fakeShell({ '/workspace': { error: 'O_DIRECTORY' } });
    // Manifest paths only: sizes/modes are 0, and runtime dirs are excluded.
    expect(listVfsFileEntries(shell)).toEqual([
      { path: 'b.txt', size: 0, mode: 0 },
      { path: 'src/a.ts', size: 0, mode: 0 },
    ]);
  });

  it('folds in the manifest when a sub-directory listing fails mid-walk', () => {
    // A healthy root but one subtree unreadably errors: the walked files stay
    // (with their size/mode) and the manifest fills the dropped subtree, so a
    // file known to have been written is never dropped from the listing.
    seedManifest(['/workspace/src/a.ts', '/workspace/lost/deep.ts']);
    const shell = fakeShell({
      '/workspace': [
        { name: 'src', type: 'dir', size: 0, mode: 0 },
        { name: 'top.ts', type: 'file', size: 7, mode: 0o644 },
      ],
      '/workspace/src': { error: 'EACCES' }, // the subtree listing fails
    });
    expect(listVfsFileEntries(shell)).toEqual([
      { path: 'lost/deep.ts', size: 0, mode: 0 }, // manifest: dropped subtree
      { path: 'src/a.ts', size: 0, mode: 0 }, // manifest: also in the dropped subtree
      { path: 'top.ts', size: 7, mode: 0o644 }, // walked: carries size/mode
    ]);
  });
});

describe('resolveWorkspacePath', () => {
  it.each(['', '.', './', '/workspace', '/workspace/', 'workspace', 'workspace/', ' . '])(
    'reads %j as the workspace root',
    (raw) => {
      expect(resolveWorkspacePath(raw, '/workspace')).toBe('/workspace');
    },
  );

  it('resolves relative paths against the workspace root, not /', () => {
    expect(resolveWorkspacePath('src/a.ts', '/workspace')).toBe('/workspace/src/a.ts');
    expect(resolveWorkspacePath('./src//a.ts', '/workspace')).toBe('/workspace/src/a.ts');
    expect(resolveWorkspacePath('workspace/src/a.ts', '/workspace')).toBe('/workspace/src/a.ts');
  });

  it('keeps absolute paths and clamps .. at /', () => {
    expect(resolveWorkspacePath('/home/user/.config', '/workspace')).toBe('/home/user/.config');
    expect(resolveWorkspacePath('../../etc', '/workspace')).toBe('/etc');
    expect(resolveWorkspacePath('src\\a.ts\0', '/workspace')).toBe('/workspace/src/a.ts');
  });

  it('prefers an entry the workspace holds under the root name', () => {
    const hasEntry = (p: string) => p === '/workspace/workspace';
    expect(resolveWorkspacePath('workspace', '/workspace', hasEntry)).toBe('/workspace/workspace');
    expect(resolveWorkspacePath('workspace/a.json', '/workspace', hasEntry)).toBe('/workspace/workspace/a.json');
  });

  it('resolves against / for binaries without a workspace directory', () => {
    expect(resolveWorkspacePath('.', '/')).toBe('/');
    expect(resolveWorkspacePath('src/a.ts', '/')).toBe('/src/a.ts');
  });
});
