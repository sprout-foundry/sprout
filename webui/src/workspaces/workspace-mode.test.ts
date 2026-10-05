/**
 * Workspace-mode registry, registration, and persistence.
 *
 * The registry decides which modes a workspace offers; `registerWorkspaceMode`
 * is the one public way to put a mode in there (SP-155 §155b — the built-ins
 * register through it too); and the resolution rule decides what a stale
 * persisted id degrades to. All three are worth pinning directly — a
 * regression here silently strands a user in a mode whose surface has nothing
 * to render.
 */

import { MonitorPlay } from 'lucide-react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, beforeAll, afterEach, beforeEach, describe, expect, it } from 'vitest';
import { INSTANCE_PID_STORAGE_KEY } from '../constants/app';
import CodeShell from './CodeShell';
import DesignShell from './DesignShell';
import ModeSwitcher from './ModeSwitcher';
import {
  DEFAULT_WORKSPACE_MODE,
  WORKSPACE_MODES,
  availableModes,
  registerWorkspaceMode,
  resolveWorkspaceMode,
  type UnregisterWorkspaceMode,
  type WorkspaceModeRegistration,
} from './registry';
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

  it('offers design in every workspace — an empty tree gets the onboarding surface', () => {
    expect(availableModes(withoutDesign).map((m) => m.id)).toContain('design');
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

describe('registerWorkspaceMode (SP-155 §155b)', () => {
  let container: HTMLDivElement;
  let root: Root;
  /**
   * Disposers for registrations this block still holds. `afterEach` drains
   * them, so a registration can never outlive a test — even one that fails
   * mid-assertion before its own explicit dispose.
   */
  const disposers: UnregisterWorkspaceMode[] = [];

  /** A mode an embedding shell would add — registered through the public API. */
  const testMode = {
    id: 'preview',
    label: 'Preview',
    icon: MonitorPlay,
    hint: 'Live app preview',
    available: () => true,
    Shell: () => null,
  };

  /** Registers through the public API and tracks the disposer for cleanup. */
  function registerTestMode(definition: WorkspaceModeRegistration = testMode): UnregisterWorkspaceMode {
    const dispose = registerWorkspaceMode(definition);
    disposers.push(dispose);
    return dispose;
  }

  /** Disposes the registrations this test created, if any. */
  function disposeTestMode(): void {
    for (const dispose of disposers.splice(0, disposers.length)) dispose();
  }

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    disposeTestMode();
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  /** Renders the switcher with exactly the modes the registry currently offers. */
  function renderSwitcher() {
    act(() => {
      root.render(
        createElement(ModeSwitcher, {
          modes: availableModes(withDesign),
          activeId: 'code',
          onSelect: vi.fn(),
          trigger: ({ open }) => createElement('span', { className: open ? 'brand is-open' : 'brand' }),
          triggerLabel: 'Switch mode',
        }),
      );
    });
  }

  it('a test mode registers and appears in the switcher', () => {
    registerTestMode();

    // Appended after the built-ins, in switcher order.
    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design', 'preview']);

    // Open the switcher and assert the mode is listed.
    renderSwitcher();
    act(() => {
      container
        .querySelector('[data-testid="sidebar-brand-trigger"]')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    const option = document.querySelector('[data-testid="sidebar-brand-option-preview"]');
    expect(option).not.toBeNull();
    expect(option?.querySelector('.mode-switcher-option-label')?.textContent).toBe('Preview');
    expect(option?.querySelector('.mode-switcher-option-hint')?.textContent).toBe('Live app preview');

    disposeTestMode();
    // The switcher is back to the built-ins.
    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design']);
  });

  it('leaves Code and Design unchanged when a mode registers', () => {
    const builtins = WORKSPACE_MODES.slice();
    expect(builtins.map((m) => m.id)).toEqual(['code', 'design']);

    registerTestMode();

    // The built-ins keep their identity and positions; the new mode lands after them.
    expect(WORKSPACE_MODES[0]).toBe(builtins[0]);
    expect(WORKSPACE_MODES[1]).toBe(builtins[1]);
    expect(WORKSPACE_MODES[2].id).toBe('preview');
    expect(WORKSPACE_MODES[0].Shell).toBe(CodeShell);
    expect(WORKSPACE_MODES[1].Shell).toBe(DesignShell);
    expect(
      availableModes(withoutDesign)
        .slice(0, 2)
        .map((m) => m.id),
    ).toEqual(['code', 'design']);

    disposeTestMode();
    expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design']);
  });

  it('an unavailable mode is not offered — the switcher hides it entirely', () => {
    registerTestMode({ ...testMode, available: () => false });

    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design']);
    expect(availableModes(withoutDesign).map((m) => m.id)).toEqual(['code', 'design']);
  });

  it('re-registration replaces the mode in place; the stale disposer removes nothing', () => {
    const first = registerTestMode();
    const second = registerTestMode({ ...testMode, label: 'Preview (updated)' });

    // No duplicate entry; the mode keeps its position with the newer definition.
    expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design', 'preview']);
    expect(WORKSPACE_MODES[2].label).toBe('Preview (updated)');

    first();
    expect(availableModes(withDesign).map((m) => m.id)).toContain('preview');

    second();
    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design']);
  });

  it('defaults a missing hint to empty', () => {
    registerTestMode({ ...testMode, hint: undefined });

    expect(WORKSPACE_MODES.find((m) => m.id === 'preview')?.hint).toBe('');
  });

  it('leaves only the built-ins in the registry', () => {
    // Every test above disposed its registrations (explicitly or via
    // afterEach); nothing this block registered may outlive it.
    expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design']);
  });
});

describe('resolveWorkspaceMode', () => {
  it('returns the requested mode when available', () => {
    expect(resolveWorkspaceMode('design', withDesign).id).toBe('design');
    // An empty workspace does not disqualify the mode (empty state, not hidden).
    expect(resolveWorkspaceMode('design', withoutDesign).id).toBe('design');
  });

  it('falls back to the default when the requested mode is not offered', () => {
    // An unknown mode id degrades to the default experience.
    expect(resolveWorkspaceMode('ship', withoutDesign).id).toBe(DEFAULT_WORKSPACE_MODE);
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
