/**
 * useWorkspaceFileTree — the file tree's callbacks against the workspace
 * file API. Pins the listing shape (mapping and order), the create / delete /
 * rename calls, the selection pass-through, and that it needs no provider.
 */

import type { FileInfo } from '@sprout/ui';
import { renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { fetchWorkspaceFiles, useWorkspaceFileTree } from './useWorkspaceFileTree';

function jsonResponse(body: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    statusText: ok ? 'OK' : 'Server Error',
    json: async () => body,
  } as unknown as Response;
}

function fakeFetch(body: unknown = { message: 'success' }) {
  const fn = vi.fn(async (_url: string, _init?: RequestInit) => jsonResponse(body));
  return { fn, fetchFn: fn as unknown as typeof fetch };
}

describe('fetchWorkspaceFiles', () => {
  it('lists a directory: directories first, ignored entries last, then by name', async () => {
    const { fn, fetchFn } = fakeFetch({
      message: 'success',
      files: [
        { name: 'b.ts', path: 'src/b.ts', size: 3, mod_time: 7 },
        { name: 'dist', path: 'src/dist', is_dir: true, git_status: 'ignored' },
        { name: 'a.log', path: 'src/a.log', git_status: 'ignored' },
        { name: 'lib', path: 'src/lib', isDir: true },
        { name: 'Makefile', path: 'src/Makefile', modified: 9, git_status: 'modified' },
      ],
    });

    const files = await fetchWorkspaceFiles('src', fetchFn);

    expect(fn).toHaveBeenCalledWith('/api/files?path=src');
    expect(files.map((f) => f.path)).toEqual(['src/lib', 'src/dist', 'src/b.ts', 'src/Makefile', 'src/a.log']);
    expect(files.find((f) => f.name === 'b.ts')).toEqual({
      name: 'b.ts',
      path: 'src/b.ts',
      size: 3,
      modified: 7,
      isDir: false,
      ext: '.ts',
      gitStatus: undefined,
    });
    expect(files.find((f) => f.name === 'Makefile')).toMatchObject({ ext: '', modified: 9, gitStatus: 'modified' });
    expect(files.find((f) => f.name === 'lib')).toMatchObject({ isDir: true, ext: '' });
  });

  it('rejects on a failed response or a non-success message', async () => {
    const failing = vi.fn(async () => jsonResponse({}, false)) as unknown as typeof fetch;
    await expect(fetchWorkspaceFiles('.', failing)).rejects.toThrow(/Failed to fetch files/);

    const { fetchFn } = fakeFetch({ message: 'path outside workspace' });
    await expect(fetchWorkspaceFiles('.', fetchFn)).rejects.toThrow('path outside workspace');
  });

  // The daemon sends BOTH spellings: `path` is absolute, `relative` is the
  // workspace-relative one. The tree composes its node paths from rootPath +
  // child names, so a node carrying the absolute path matches nothing it
  // builds — ancestor expansion, selection and reveal all compare against
  // the relative form. Fixtures using a relative `path` hid this.
  it('maps the daemon absolute path to the workspace-relative one', async () => {
    const { fetchFn } = fakeFetch({
      message: 'success',
      files: [
        { name: 'a.ts', path: '/w/src/a.ts', relative: 'src/a.ts', size: 1 },
        { name: 'lib', path: '/w/src/lib', relative: 'src/lib', is_dir: true },
      ],
    });

    const files = await fetchWorkspaceFiles('src', fetchFn, '/w');

    expect(files.map((f) => f.path)).toEqual(['src/lib', 'src/a.ts']);
  });

  it('strips a known workspace root when only the absolute path is sent', async () => {
    const { fetchFn } = fakeFetch({
      message: 'success',
      files: [{ name: 'a.ts', path: '/w/src/a.ts', size: 1 }],
    });

    const files = await fetchWorkspaceFiles('src', fetchFn, '/w');

    expect(files[0].path).toBe('src/a.ts');
  });

  it('keeps a path that is already relative when no root is known', async () => {
    const { fetchFn } = fakeFetch({
      message: 'success',
      files: [{ name: 'a.ts', path: 'src/a.ts', size: 1 }],
    });

    const files = await fetchWorkspaceFiles('src', fetchFn);

    expect(files[0].path).toBe('src/a.ts');
  });
});

describe('useWorkspaceFileTree', () => {
  it('returns FileTreeProps rooted at the workspace root by default, without a provider', async () => {
    const { fn, fetchFn } = fakeFetch({ message: 'success', files: [] });
    const { result } = renderHook(() => useWorkspaceFileTree({ fetchFn }));

    expect(result.current.rootPath).toBe('.');
    expect(result.current.selectedFile).toBeUndefined();
    await expect(result.current.onFetchFiles?.('.')).resolves.toEqual([]);
    expect(fn).toHaveBeenCalledWith('/api/files?path=.');
  });

  it('creates, deletes and renames through the file API', async () => {
    const { fn, fetchFn } = fakeFetch({ message: 'ok' });
    const { result } = renderHook(() => useWorkspaceFileTree({ fetchFn }));

    await result.current.onCreateFile?.('.', 'new.txt');
    await result.current.onCreateFolder?.('src', 'pkg');
    await result.current.onDeletePath?.('old.txt', false);
    await result.current.onRenamePath?.('a.txt', 'b.txt');

    const calls = fn.mock.calls.map(([url, init]) => [url, JSON.parse(String(init?.body))]);
    expect(calls).toEqual([
      ['/api/create', { path: 'new.txt' }],
      ['/api/create', { directory: 'src/pkg', path: 'src/pkg' }],
      ['/api/delete', { path: 'old.txt' }],
      ['/api/rename', { old_path: 'a.txt', new_path: 'b.txt' }],
    ]);
  });

  it('passes selection, root and workspace root through, with stable callbacks', () => {
    const onFileSelect = vi.fn();
    const { fetchFn } = fakeFetch();
    const { result, rerender } = renderHook(() =>
      useWorkspaceFileTree({ fetchFn, onFileSelect, selectedFile: 'a.ts', rootPath: 'repos/x', workspaceRoot: '/w' }),
    );
    const file: FileInfo = { name: 'a.ts', path: 'a.ts', size: 0, modified: 0, isDir: false, ext: '.ts' };
    result.current.onFileSelect(file);

    expect(onFileSelect).toHaveBeenCalledWith(file);
    expect(result.current).toMatchObject({ selectedFile: 'a.ts', rootPath: 'repos/x', workspaceRoot: '/w' });

    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });
});
