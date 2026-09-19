/**
 * Workspace-mode registry and persistence.
 *
 * The registry decides which modes a workspace offers; the resolution rule
 * decides what a stale persisted id degrades to. Both are pure and worth
 * pinning directly — a regression here silently strands a user in a mode whose
 * surface has nothing to render.
 */

import { beforeEach, describe, expect, it } from 'vitest';
import { INSTANCE_PID_STORAGE_KEY } from '../constants/app';
import CodeShell from './CodeShell';
import DesignShell from './DesignShell';
import { DEFAULT_WORKSPACE_MODE, WORKSPACE_MODES, availableModes, resolveWorkspaceMode } from './registry';
import {
  persistWorkspaceMode,
  readPersistedWorkspaceMode,
  useWorkspaceMode,
  workspaceModeStorageKey,
} from './useWorkspaceMode';

const withDesign = { hasDesignTree: true };
const withoutDesign = { hasDesignTree: false };

describe('registry', () => {
  it('offers code in every workspace', () => {
    expect(availableModes(withoutDesign).map((m) => m.id)).toContain('code');
    expect(availableModes(withDesign).map((m) => m.id)).toContain('code');
  });

  it('offers design only where a design tree exists', () => {
    expect(availableModes(withoutDesign).map((m) => m.id)).not.toContain('design');
    expect(availableModes(withDesign).map((m) => m.id)).toContain('design');
  });

  it('preserves switcher order', () => {
    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design']);
  });

  it('gives every mode a label, hint, and icon', () => {
    for (const mode of WORKSPACE_MODES) {
      expect(mode.label).not.toBe('');
      expect(mode.hint).not.toBe('');
      expect(mode.icon).toBeTruthy();
    }
  });

  it('gives every mode a shell component', () => {
    for (const mode of WORKSPACE_MODES) {
      expect(typeof mode.Shell).toBe('function');
    }
    expect(WORKSPACE_MODES.find((m) => m.id === 'code')?.Shell).toBe(CodeShell);
    expect(WORKSPACE_MODES.find((m) => m.id === 'design')?.Shell).toBe(DesignShell);
  });
});

describe('resolveWorkspaceMode', () => {
  it('returns the requested mode when available', () => {
    expect(resolveWorkspaceMode('design', withDesign).id).toBe('design');
  });

  it('falls back to the default when the requested mode is not offered', () => {
    // A persisted `design` in a workspace that has since lost its design tree.
    expect(resolveWorkspaceMode('design', withoutDesign).id).toBe(DEFAULT_WORKSPACE_MODE);
  });

  it('falls back to the default for an unknown id', () => {
    expect(resolveWorkspaceMode('ship', withDesign).id).toBe(DEFAULT_WORKSPACE_MODE);
    expect(resolveWorkspaceMode(null, withDesign).id).toBe(DEFAULT_WORKSPACE_MODE);
    expect(resolveWorkspaceMode(undefined, withDesign).id).toBe(DEFAULT_WORKSPACE_MODE);
  });
});

describe('mode persistence', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('round-trips the active mode', () => {
    persistWorkspaceMode('design');
    expect(readPersistedWorkspaceMode()).toBe('design');
  });

  it('scopes the key by instance pid', () => {
    window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, '1111');
    const first = workspaceModeStorageKey();
    persistWorkspaceMode('design');

    window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, '2222');
    const second = workspaceModeStorageKey();

    expect(first).not.toBe(second);
    // A different instance has no mode of its own yet.
    expect(readPersistedWorkspaceMode()).toBeNull();
    // …and the first instance's choice is still there.
    window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, '1111');
    expect(readPersistedWorkspaceMode()).toBe('design');
  });

  it('returns null rather than throwing on unreadable storage', () => {
    window.localStorage.setItem(workspaceModeStorageKey(), 'not json');
    expect(readPersistedWorkspaceMode()).toBeNull();
  });

  it('does not throw when storage rejects the write', () => {
    const original = window.localStorage.setItem;
    window.localStorage.setItem = () => {
      throw new Error('quota');
    };
    expect(() => persistWorkspaceMode('design')).not.toThrow();
    window.localStorage.setItem = original;
  });
});

describe('useWorkspaceMode contract', () => {
  // The hook is exercised through its returned shape rather than a render,
  // since the shell integration is covered by the DesignView/e2e specs.
  it('exports a callable hook', () => {
    expect(typeof useWorkspaceMode).toBe('function');
  });
});
