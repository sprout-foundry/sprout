/**
 * Workspace-mode registry, registration, and persistence.
 *
 * The registry decides which modes a workspace offers; `registerWorkspaceMode`
 * is the one public way to put a mode in there (the built-ins
 * go through the same write path at load, minus the public API's built-in
 * guard); and the resolution rule decides what a stale persisted id degrades
 * to. All three are worth pinning directly — a regression here silently
 * strands a user in a mode whose surface has nothing to render.
 */

import { Code2, MonitorPlay, Palette } from 'lucide-react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, beforeAll, afterEach, beforeEach, describe, expect, it } from 'vitest';
import { configuredDefaultWorkspaceMode, overrideDefaultWorkspaceMode } from '../config/workspaceMode';
import { INSTANCE_PID_STORAGE_KEY } from '../constants/app';
import CodeShell from './CodeShell';
import DesignShell from './DesignShell';
import ModeSwitcher from './ModeSwitcher';
import {
  BUILTIN_DEFAULT_WORKSPACE_MODE,
  BUILTIN_WORKSPACE_MODE_IDS,
  WORKSPACE_MODES,
  availableModes,
  defaultWorkspaceMode,
  isBuiltinWorkspaceModeId,
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

/** The test-mode payload the resolution tests register inline. */
const previewMode: WorkspaceModeRegistration = {
  id: 'preview',
  label: 'Preview',
  icon: MonitorPlay,
  hint: 'Live app preview',
  available: () => true,
  Shell: () => null,
};

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

describe('registerWorkspaceMode', () => {
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

  it('rejects re-registering a built-in id — the built-in keeps its original definition', () => {
    // The ids the public API refuses, via the predicate an embedder checks first.
    expect(BUILTIN_WORKSPACE_MODE_IDS).toEqual(['code', 'design']);
    for (const id of BUILTIN_WORKSPACE_MODE_IDS) expect(isBuiltinWorkspaceModeId(id)).toBe(true);
    expect(isBuiltinWorkspaceModeId('preview')).toBe(false);
    expect(isBuiltinWorkspaceModeId(null as unknown as string)).toBe(false);

    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      const builtins = WORKSPACE_MODES.slice();

      // An embedding shell trying to take over the Code entry is refused; the
      // payload replaces every field a hijack would want to change.
      const disposeCode = registerTestMode({
        ...testMode,
        id: 'code',
        label: 'Code (hijacked)',
        hint: 'Not the real Code mode',
        available: () => false,
      });

      // No duplicate, no replacement: same entry object, same label/icon/shell.
      expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design']);
      const code = WORKSPACE_MODES.find((m) => m.id === 'code')!;
      expect(code).toBe(builtins[0]);
      expect(code.label).toBe('Code');
      expect(code.icon).toBe(Code2);
      expect(code.hint).toBe('Chat, editor, git, terminal');
      expect(code.Shell).toBe(CodeShell);
      expect(code.available(withDesign)).toBe(true);

      // The disposer returned by the rejected call removes nothing.
      disposeCode();
      expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design']);
      expect(WORKSPACE_MODES.find((m) => m.id === 'code')).toBe(builtins[0]);

      // Same for the Design built-in.
      const disposeDesign = registerTestMode({ ...testMode, id: 'design', label: 'Design (hijacked)' });
      expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design']);
      const design = WORKSPACE_MODES.find((m) => m.id === 'design')!;
      expect(design).toBe(builtins[1]);
      expect(design.label).toBe('Design');
      expect(design.icon).toBe(Palette);
      expect(design.Shell).toBe(DesignShell);

      disposeDesign();
      expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design']);

      expect(warn).toHaveBeenCalledTimes(2);
    } finally {
      warn.mockRestore();
    }
  });

  it('never leaves the registry with zero modes across register/dispose orders', () => {
    // The built-ins are always registered and always offered, in any order.
    const ids = () => availableModes(withDesign).map((m) => m.id);
    expect(ids()).toEqual(['code', 'design']);

    const first = registerTestMode({ ...testMode, id: 'preview' });
    const second = registerTestMode({ ...testMode, id: 'extra' });
    expect(ids()).toEqual(['code', 'design', 'preview', 'extra']);

    first();
    second();
    expect(ids()).toEqual(['code', 'design']);
    expect(availableModes(withoutDesign).map((m) => m.id)).toEqual(['code', 'design']);

    // Disposing both generations of a re-registered id still leaves the built-ins.
    const stale = registerTestMode({ ...testMode, id: 'preview' });
    const fresh = registerTestMode({ ...testMode, id: 'preview' });
    stale();
    expect(ids()).toEqual(['code', 'design', 'preview']);
    fresh();
    expect(ids()).toEqual(['code', 'design']);

    // Double-dispose is harmless too.
    fresh();
    expect(ids()).toEqual(['code', 'design']);
  });

  it('leaves only the built-ins in the registry', () => {
    // Every test above disposed its registrations (explicitly or via
    // afterEach); nothing this block registered may outlive it — including
    // the rejected built-in registrations, whose no-op disposers cannot
    // remove the built-ins they pointed at.
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
    // An unknown mode id degrades to the default experience. With no
    // configuration the default is the built-in baseline.
    expect(resolveWorkspaceMode('ship', withoutDesign).id).toBe(BUILTIN_DEFAULT_WORKSPACE_MODE);
  });

  it('falls back to the default for an unknown id', () => {
    expect(resolveWorkspaceMode('ship', withDesign).id).toBe(BUILTIN_DEFAULT_WORKSPACE_MODE);
    expect(resolveWorkspaceMode(null, withDesign).id).toBe(BUILTIN_DEFAULT_WORKSPACE_MODE);
    expect(resolveWorkspaceMode(undefined, withDesign).id).toBe(BUILTIN_DEFAULT_WORKSPACE_MODE);
  });

  it('returns a real mode for every id a register/dispose sequence can produce', () => {
    // Through the public API alone the built-ins are irremovable, so whatever
    // is registered and disposed in between, resolution always has the
    // built-ins to fall back on. Registered inline (not via `registerTestMode`,
    // which lives in the other block) and disposed exhaustively.
    const register = (id: string) => registerWorkspaceMode({ ...previewMode, id });
    // `?.` fails the assert rather than throwing, if resolution ever broke.
    const assertResolves = (ids: (string | null | undefined)[]) => {
      for (const id of ids) expect(resolveWorkspaceMode(id, withDesign)?.Shell).toBeTypeOf('function');
    };
    const first = register('preview');
    const second = register('extra');
    assertResolves([null, undefined, 'preview', 'extra', 'design', 'missing-id']);
    first();
    assertResolves([null, undefined, 'preview', 'design']);
    second();
    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design']);
    expect(resolveWorkspaceMode(null, withDesign).id).toBe(BUILTIN_DEFAULT_WORKSPACE_MODE);
  });

  it('yields the registered built-in default when nothing is available, and never undefined', () => {
    const snapshot = WORKSPACE_MODES.slice();
    const originals = WORKSPACE_MODES.map((mode) => mode.available);
    try {
      // Both states below are unreachable through the public API (the
      // built-ins cannot be removed, and ship as `() => true`), but resolution
      // must never return undefined: hidden modes yield the registered
      // built-in default; an emptied registry throws.
      WORKSPACE_MODES.forEach((mode) => (mode.available = () => false));
      expect(availableModes(withDesign)).toEqual([]);
      expect(resolveWorkspaceMode(null, withDesign)).toBe(snapshot[0]);
      expect(resolveWorkspaceMode(null, withDesign).id).toBe(BUILTIN_DEFAULT_WORKSPACE_MODE);

      WORKSPACE_MODES.splice(0, WORKSPACE_MODES.length);
      expect(() => resolveWorkspaceMode(null, withDesign)).toThrow(/no workspace modes registered/);
    } finally {
      WORKSPACE_MODES.splice(0, WORKSPACE_MODES.length, ...snapshot);
      WORKSPACE_MODES.forEach((mode, index) => (mode.available = originals[index]));
    }
    // Fully restored for the surrounding tests.
    expect(WORKSPACE_MODES.map((m) => m.id)).toEqual(['code', 'design']);
    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design']);
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

describe('config-driven default mode', () => {
  const disposers: UnregisterWorkspaceMode[] = [];

  function register(definition: WorkspaceModeRegistration): UnregisterWorkspaceMode {
    const dispose = registerWorkspaceMode(definition);
    disposers.push(dispose);
    return dispose;
  }

  function disposeAll(): void {
    for (const dispose of disposers.splice(0, disposers.length)) dispose();
  }

  // The config module is a module singleton; reset it to the unconfigured
  // state (the build-time base, null in the test env) on both edges so no
  // override leaks into another test.
  beforeEach(() => {
    overrideDefaultWorkspaceMode(null);
  });

  afterEach(() => {
    overrideDefaultWorkspaceMode(null);
    disposeAll();
  });

  it('starts in the built-in code mode when nothing is configured', () => {
    expect(configuredDefaultWorkspaceMode()).toBeNull();
    expect(defaultWorkspaceMode(withDesign)).toBe('code');
    expect(defaultWorkspaceMode(withoutDesign)).toBe('code');
    // A null or unknown request resolves to the built-in default.
    expect(resolveWorkspaceMode(null, withDesign).id).toBe('code');
    expect(resolveWorkspaceMode('unknown-id', withoutDesign).id).toBe('code');
  });

  it('starts in a configured mode when that mode is available', () => {
    overrideDefaultWorkspaceMode('design');
    expect(configuredDefaultWorkspaceMode()).toBe('design');
    // Design is offered in every workspace, so it is the default either way.
    expect(defaultWorkspaceMode(withDesign)).toBe('design');
    expect(defaultWorkspaceMode(withoutDesign)).toBe('design');
    // A null or unknown request resolves to the configured default.
    expect(resolveWorkspaceMode(null, withDesign).id).toBe('design');
    expect(resolveWorkspaceMode('unknown-id', withDesign).id).toBe('design');
    // …but an explicitly requested available mode still wins.
    expect(resolveWorkspaceMode('code', withDesign).id).toBe('code');
  });

  it('falls back to code when the configured default names an unregistered mode', () => {
    overrideDefaultWorkspaceMode('preview');
    // 'preview' is not registered, so it is never in availableModes.
    expect(defaultWorkspaceMode(withDesign)).toBe('code');
    expect(resolveWorkspaceMode(null, withDesign).id).toBe('code');
  });

  it('falls back to code when the configured default names an unavailable mode', () => {
    register({
      id: 'preview',
      label: 'Preview',
      icon: MonitorPlay,
      hint: 'Live app preview',
      available: () => false,
      Shell: () => null,
    });
    overrideDefaultWorkspaceMode('preview');
    // 'preview' is registered but never offered, so it is not the default.
    expect(defaultWorkspaceMode(withDesign)).toBe('code');
    expect(defaultWorkspaceMode(withoutDesign)).toBe('code');
    expect(resolveWorkspaceMode(null, withDesign).id).toBe('code');
    // The built-ins are still offered unchanged.
    expect(availableModes(withDesign).map((m) => m.id)).toEqual(['code', 'design']);
  });
});
