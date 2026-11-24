/**
 * projectFolderCwd — tests for the session working-directory store.
 * Covers: normalization/validation, persistence, subscribe/notify,
 * derived helpers (label, context line, path resolution), React binding.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';

import {
  WORKSPACE_CWD_STORAGE_KEY,
  normalizeProjectFolderCwd,
  getProjectFolderCwd,
  setProjectFolderCwd,
  subscribeProjectFolderCwd,
  projectFolderCwdLabel,
  projectFolderCwdContextLine,
  resolveProjectFolderPath,
  resolveCdTarget,
  useProjectFolderCwd,
  setProjectFolderCwdScope,
  projectFolderCwdStorageKey,
  __resetProjectFolderCwdForTests,
} from './projectFolderCwd';

beforeEach(() => {
  __resetProjectFolderCwdForTests();
});

describe('normalizeWorkspaceCwd', () => {
  it('returns the empty string for root-ish inputs', () => {
    expect(normalizeProjectFolderCwd('')).toBe('');
    expect(normalizeProjectFolderCwd('/')).toBe('');
    expect(normalizeProjectFolderCwd('.')).toBe('');
    expect(normalizeProjectFolderCwd('./')).toBe('');
  });

  it('strips leading/trailing slashes and collapses duplicates', () => {
    expect(normalizeProjectFolderCwd('repos/a/b')).toBe('repos/a/b');
    expect(normalizeProjectFolderCwd('/repos/a/b/')).toBe('repos/a/b');
    expect(normalizeProjectFolderCwd('repos//a///b')).toBe('repos/a/b');
    expect(normalizeProjectFolderCwd('./repos/./a')).toBe('repos/a');
  });

  it('rejects .. traversal and non-string input', () => {
    expect(normalizeProjectFolderCwd('../etc')).toBeNull();
    expect(normalizeProjectFolderCwd('repos/../../etc')).toBeNull();
    expect(normalizeProjectFolderCwd('a/../b')).toBeNull();
    expect(normalizeProjectFolderCwd(undefined as unknown as string)).toBeNull();
  });
});

describe('store get/set', () => {
  it('defaults to workspace root', () => {
    expect(getProjectFolderCwd()).toBe('');
  });

  it('sets and returns a normalized cwd', () => {
    expect(setProjectFolderCwd('/repos/acme/api/')).toBe(true);
    expect(getProjectFolderCwd()).toBe('repos/acme/api');
  });

  it('rejects traversal without changing state', () => {
    expect(setProjectFolderCwd('../outside')).toBe(false);
    expect(getProjectFolderCwd()).toBe('');
  });

  it('treats setting the same value as a no-op (returns false)', () => {
    setProjectFolderCwd('repos/a/b');
    expect(setProjectFolderCwd('repos/a/b')).toBe(false);
    expect(getProjectFolderCwd()).toBe('repos/a/b');
  });

  it('writes through to localStorage on set', () => {
    setProjectFolderCwd('repos/acme/api');
    expect(window.localStorage.getItem(WORKSPACE_CWD_STORAGE_KEY)).toBe('repos/acme/api');
  });

  it('lazily re-reads the persisted value after a full reset', () => {
    __resetProjectFolderCwdForTests(); // wipes memory AND the persisted key
    window.localStorage.setItem(WORKSPACE_CWD_STORAGE_KEY, 'repos/x/y');
    expect(getProjectFolderCwd()).toBe('repos/x/y');
  });

  it('falls back to root when the persisted value is invalid', () => {
    __resetProjectFolderCwdForTests();
    window.localStorage.setItem(WORKSPACE_CWD_STORAGE_KEY, '../evil');
    expect(getProjectFolderCwd()).toBe('');
  });
});

describe('subscribe', () => {
  it('notifies listeners with the new cwd on change', () => {
    const seen: string[] = [];
    const unsub = subscribeProjectFolderCwd((cwd) => seen.push(cwd));
    setProjectFolderCwd('repos/a/b');
    unsub();
    setProjectFolderCwd('');
    expect(seen).toEqual(['repos/a/b']);
  });

  it('swallows listener errors without blocking other listeners', () => {
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const ok = vi.fn();
    const boom = vi.fn(() => {
      throw new Error('listener exploded');
    });
    const u1 = subscribeProjectFolderCwd(boom);
    const u2 = subscribeProjectFolderCwd(ok);
    setProjectFolderCwd('x');
    expect(boom).toHaveBeenCalledWith('x');
    expect(ok).toHaveBeenCalledWith('x');
    u1();
    u2();
    errSpy.mockRestore();
  });
});

describe('derived helpers', () => {
  it('workspaceCwdLabel: last segment, ~ for root', () => {
    expect(projectFolderCwdLabel('')).toBe('~');
    expect(projectFolderCwdLabel('repos/acme/api')).toBe('api');
    expect(projectFolderCwdLabel('../../weird')).toBe('~'); // invalid → treated as root
  });

  it('workspaceCwdContextLine: empty at root, one line otherwise', () => {
    expect(projectFolderCwdContextLine('')).toBe('');
    expect(projectFolderCwdContextLine('repos/acme/api')).toBe(
      'Working directory: repos/acme/api (all project files live under this prefix)',
    );
    // Default-arg form reads the store.
    setProjectFolderCwd('repos/a/b');
    expect(projectFolderCwdContextLine()).toContain('repos/a/b');
  });

  it('resolveWorkspacePath: roots relative paths, preserves absolute-for-cwd ones', () => {
    expect(resolveProjectFolderPath('README.md', 'repos/a/b')).toBe('repos/a/b/README.md');
    expect(resolveProjectFolderPath('repos/a/b/src/x.ts', 'repos/a/b')).toBe('repos/a/b/src/x.ts');
    expect(resolveProjectFolderPath('src/x.ts', '')).toBe('src/x.ts');
    expect(resolveProjectFolderPath('', 'repos/a/b')).toBe('repos/a/b');
  });
});

describe('useWorkspaceCwd (React binding)', () => {
  it('re-renders the component when the cwd changes', async () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;

    let rendered = 'unset';
    function Probe(): null {
      rendered = useProjectFolderCwd();
      return null;
    }

    await act(async () => {
      root.render(createElement(Probe));
    });
    expect(rendered).toBe('');

    await act(async () => {
      setProjectFolderCwd('repos/acme/api');
    });
    expect(rendered).toBe('repos/acme/api');

    await act(async () => {
      setProjectFolderCwd('');
    });
    expect(rendered).toBe('');

    await act(async () => {
      root.unmount();
    });
    container.remove();
  });
});

describe('resolveCdTarget (terminal session cd)', () => {
  it('resolves plain names against the session dir', () => {
    expect(resolveCdTarget('repos', '')).toBe('repos');
    expect(resolveCdTarget('api', 'repos/acme')).toBe('repos/acme/api');
  });

  it('handles dot segments and trailing slashes', () => {
    expect(resolveCdTarget('.', 'repos/acme')).toBe('repos/acme');
    expect(resolveCdTarget('./api/', 'repos/acme')).toBe('repos/acme/api');
  });

  it('resolves .. with chroot semantics', () => {
    expect(resolveCdTarget('..', 'repos/acme')).toBe('repos');
    expect(resolveCdTarget('..', '')).toBe(''); // root is the ceiling
    expect(resolveCdTarget('../../..', 'repos/acme')).toBe(''); // clamps at root
    expect(resolveCdTarget('api/../..', 'repos/acme')).toBe('repos');
  });

  it('supports ~ and ~/… as root-relative home', () => {
    expect(resolveCdTarget('~', 'repos/acme')).toBe('');
    expect(resolveCdTarget('~/repos/x', 'repos/acme')).toBe('repos/x');
  });

  it('bare-ish inputs keep the session dir', () => {
    expect(resolveCdTarget('', 'repos/acme')).toBe('repos/acme');
    expect(resolveCdTarget('   ', 'repos/acme')).toBe('repos/acme');
  });

  it('rejects absolute paths and non-strings with null', () => {
    expect(resolveCdTarget('/etc', 'repos')).toBe(null);
    expect(resolveCdTarget('/', '')).toBe(null);
    expect(resolveCdTarget(undefined as unknown as string, '')).toBe(null);
  });
});

describe('workspace cwd scoping', () => {
  it('keeps a separate cwd per workspace root', () => {
    setProjectFolderCwdScope('/work/alpha');
    setProjectFolderCwd('repos/acme/api');
    expect(window.localStorage.getItem(projectFolderCwdStorageKey('/work/alpha'))).toBe('repos/acme/api');

    const seen: string[] = [];
    const unsubscribe = subscribeProjectFolderCwd((cwd) => seen.push(cwd));
    setProjectFolderCwdScope('/work/beta');
    expect(getProjectFolderCwd()).toBe('');
    expect(seen).toEqual(['']);

    setProjectFolderCwd('repos/other/web');
    setProjectFolderCwdScope('/work/alpha');
    expect(getProjectFolderCwd()).toBe('repos/acme/api');
    unsubscribe();
  });

  it('does not carry the unscoped value into a known workspace', () => {
    window.localStorage.setItem(WORKSPACE_CWD_STORAGE_KEY, 'repos/x/y');
    expect(getProjectFolderCwd()).toBe('repos/x/y');
    setProjectFolderCwdScope('/work/gamma');
    expect(getProjectFolderCwd()).toBe('');
    window.localStorage.removeItem(WORKSPACE_CWD_STORAGE_KEY);
  });
});
