import { describe, it, expect, beforeEach, vi } from 'vitest';
import { resolveEditorFilePath } from './lspClientService';

vi.mock('./workspaceStore', () => ({
  getWorkspaceRoot: () => '/w',
}));

const ROOT = '/Users/dev/proj';

describe('resolveEditorFilePath', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('passes virtual __workspace/ paths through unchanged', () => {
    expect(resolveEditorFilePath('__workspace/untitled-123')).toBe('__workspace/untitled-123');
    expect(resolveEditorFilePath('__workspace/chat/abc')).toBe('__workspace/chat/abc');
    expect(resolveEditorFilePath('__workspace/compare/x-1')).toBe('__workspace/compare/x-1');
  });

  it('is idempotent for a virtual path (no root doubling on re-resolve)', () => {
    const once = resolveEditorFilePath('__workspace/untitled-123');
    const twice = resolveEditorFilePath(once);
    expect(twice).toBe(once);
    expect(twice).not.toContain(ROOT);
  });

  it('resolves a workspace-relative path against the workspace root', () => {
    expect(resolveEditorFilePath('src/new.ts')).toBe('/w/src/new.ts');
  });

  it('is idempotent for a resolved workspace-relative path', () => {
    const once = resolveEditorFilePath('src/new.ts');
    expect(resolveEditorFilePath(once)).toBe(once);
  });

  it('passes absolute paths through unchanged', () => {
    expect(resolveEditorFilePath('/w/src/a.ts')).toBe('/w/src/a.ts');
  });

  it('never doubles an already-rooted path', () => {
    const resolved = resolveEditorFilePath('src/new.ts');
    expect(resolveEditorFilePath(resolved)).not.toBe(`${'/w'}${resolved}`);
  });
});
