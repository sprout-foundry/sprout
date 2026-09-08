/**
 * workspaceCwd — tests for the session working-directory store.
 * Covers: normalization/validation, persistence, subscribe/notify,
 * derived helpers (label, context line, path resolution), React binding.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';

import {
  WORKSPACE_CWD_STORAGE_KEY,
  normalizeWorkspaceCwd,
  getWorkspaceCwd,
  setWorkspaceCwd,
  subscribeWorkspaceCwd,
  workspaceCwdLabel,
  workspaceCwdContextLine,
  resolveWorkspacePath,
  resolveCdTarget,
  useWorkspaceCwd,
  __resetWorkspaceCwdForTests,
} from './workspaceCwd';

beforeEach(() => {
  __resetWorkspaceCwdForTests();
});

describe('normalizeWorkspaceCwd', () => {
  it('returns the empty string for root-ish inputs', () => {
    expect(normalizeWorkspaceCwd('')).toBe('');
    expect(normalizeWorkspaceCwd('/')).toBe('');
    expect(normalizeWorkspaceCwd('.')).toBe('');
    expect(normalizeWorkspaceCwd('./')).toBe('');
  });

  it('strips leading/trailing slashes and collapses duplicates', () => {
    expect(normalizeWorkspaceCwd('repos/a/b')).toBe('repos/a/b');
    expect(normalizeWorkspaceCwd('/repos/a/b/')).toBe('repos/a/b');
    expect(normalizeWorkspaceCwd('repos//a///b')).toBe('repos/a/b');
    expect(normalizeWorkspaceCwd('./repos/./a')).toBe('repos/a');
  });

  it('rejects .. traversal and non-string input', () => {
    expect(normalizeWorkspaceCwd('../etc')).toBeNull();
    expect(normalizeWorkspaceCwd('repos/../../etc')).toBeNull();
    expect(normalizeWorkspaceCwd('a/../b')).toBeNull();
    expect(normalizeWorkspaceCwd(undefined as unknown as string)).toBeNull();
  });
});

describe('store get/set', () => {
  it('defaults to workspace root', () => {
    expect(getWorkspaceCwd()).toBe('');
  });

  it('sets and returns a normalized cwd', () => {
    expect(setWorkspaceCwd('/repos/acme/api/')).toBe(true);
    expect(getWorkspaceCwd()).toBe('repos/acme/api');
  });

  it('rejects traversal without changing state', () => {
    expect(setWorkspaceCwd('../outside')).toBe(false);
    expect(getWorkspaceCwd()).toBe('');
  });

  it('treats setting the same value as a no-op (returns false)', () => {
    setWorkspaceCwd('repos/a/b');
    expect(setWorkspaceCwd('repos/a/b')).toBe(false);
    expect(getWorkspaceCwd()).toBe('repos/a/b');
  });

  it('writes through to localStorage on set', () => {
    setWorkspaceCwd('repos/acme/api');
    expect(window.localStorage.getItem(WORKSPACE_CWD_STORAGE_KEY)).toBe('repos/acme/api');
  });

  it('lazily re-reads the persisted value after a full reset', () => {
    __resetWorkspaceCwdForTests(); // wipes memory AND the persisted key
    window.localStorage.setItem(WORKSPACE_CWD_STORAGE_KEY, 'repos/x/y');
    expect(getWorkspaceCwd()).toBe('repos/x/y');
  });

  it('falls back to root when the persisted value is invalid', () => {
    __resetWorkspaceCwdForTests();
    window.localStorage.setItem(WORKSPACE_CWD_STORAGE_KEY, '../evil');
    expect(getWorkspaceCwd()).toBe('');
  });
});

describe('subscribe', () => {
  it('notifies listeners with the new cwd on change', () => {
    const seen: string[] = [];
    const unsub = subscribeWorkspaceCwd((cwd) => seen.push(cwd));
    setWorkspaceCwd('repos/a/b');
    unsub();
    setWorkspaceCwd('');
    expect(seen).toEqual(['repos/a/b']);
  });

  it('swallows listener errors without blocking other listeners', () => {
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const ok = vi.fn();
    const boom = vi.fn(() => {
      throw new Error('listener exploded');
    });
    const u1 = subscribeWorkspaceCwd(boom);
    const u2 = subscribeWorkspaceCwd(ok);
    setWorkspaceCwd('x');
    expect(boom).toHaveBeenCalledWith('x');
    expect(ok).toHaveBeenCalledWith('x');
    u1();
    u2();
    errSpy.mockRestore();
  });
});

describe('derived helpers', () => {
  it('workspaceCwdLabel: last segment, ~ for root', () => {
    expect(workspaceCwdLabel('')).toBe('~');
    expect(workspaceCwdLabel('repos/acme/api')).toBe('api');
    expect(workspaceCwdLabel('../../weird')).toBe('~'); // invalid → treated as root
  });

  it('workspaceCwdContextLine: empty at root, one line otherwise', () => {
    expect(workspaceCwdContextLine('')).toBe('');
    expect(workspaceCwdContextLine('repos/acme/api')).toBe(
      'Working directory: repos/acme/api (all project files live under this prefix)',
    );
    // Default-arg form reads the store.
    setWorkspaceCwd('repos/a/b');
    expect(workspaceCwdContextLine()).toContain('repos/a/b');
  });

  it('resolveWorkspacePath: roots relative paths, preserves absolute-for-cwd ones', () => {
    expect(resolveWorkspacePath('README.md', 'repos/a/b')).toBe('repos/a/b/README.md');
    expect(resolveWorkspacePath('repos/a/b/src/x.ts', 'repos/a/b')).toBe('repos/a/b/src/x.ts');
    expect(resolveWorkspacePath('src/x.ts', '')).toBe('src/x.ts');
    expect(resolveWorkspacePath('', 'repos/a/b')).toBe('repos/a/b');
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
      rendered = useWorkspaceCwd();
      return null;
    }

    await act(async () => {
      root.render(createElement(Probe));
    });
    expect(rendered).toBe('');

    await act(async () => {
      setWorkspaceCwd('repos/acme/api');
    });
    expect(rendered).toBe('repos/acme/api');

    await act(async () => {
      setWorkspaceCwd('');
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
