import { describe, expect, it } from 'vitest';
import { resolveWorkspacePath } from './vfsFiles';

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
