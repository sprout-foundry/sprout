// @vitest-environment jsdom
/**
 * R-3: boot-path tests for the Track R native-terminal seam. Mirrors
 * nativeFsBoot.test.tsx (the R-2f native-FS boot short-circuit tests).
 *
 * The compile-time flag `NATIVE_TERMINAL_ENABLED` (baked into
 * useTerminalSession + usePageVisibility) is controlled exactly like the other
 * native tests: `vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL','1')` +
 * `vi.resetModules()` + a FRESH dynamic import, so the constant read at import
 * time reflects the env. `../services/terminalWebSocket` is vi.mock-ed so we
 * can spy on `TerminalWebSocketService.createInstance`.
 *
 * Coverage in this file:
 *   1. `useTerminalSession` — flag ON: the WS-lifecycle effect short-circuits,
 *      `createInstance` is NEVER called, `paneConnected` stays false, and
 *      `terminalProvidedByShell === true`. Flag OFF + connected:
 *      `createInstance` IS called (today's exact behavior).
 *   2. `TerminalPane` render conditions — flag ON renders the single shared
 *      "Terminal provided by the native shell" placeholder and does NOT render
 *      "Loading terminal..."; flag OFF renders "Loading terminal...". This is
 *      covered by mounting the REAL TerminalPane component (the authoritative
 *      E2E check for the render branch the task asks about).
 *   3. Stub surface — importing
 *      `../services/nativeTerminalStubs/terminalWebSocket` directly: the
 *      no-op `TerminalWebSocketService` (createInstance/getInstance shared
 *      instance, onEvent returns an unsubscribe fn, isReady/
 *      isConnectedToServer false, getSessionId null, reprInput identity,
 *      freezeAll/resumeAll no-ops) and the compile-time
 *      `NATIVE_TERMINAL_ENABLED` constant (boolean, false when env unset).
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, act } from '@testing-library/react';
import React, { useRef } from 'react';
import type { UseTerminalSessionReturn } from '../hooks/useTerminalSession';

type XTerm = import('@xterm/xterm').Terminal;

// ── Hoisted spies + the terminalWebSocket mock (the module under test) ───────

const createInstanceMock = vi.hoisted(() => vi.fn());
const getInstanceMock = vi.hoisted(() => vi.fn());
const freezeAllMock = vi.hoisted(() => vi.fn());
const resumeAllMock = vi.hoisted(() => vi.fn());

const mockService = vi.hoisted(() => ({
  onEvent: vi.fn(() => () => {}),
  removeEvent: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
  closeSession: vi.fn(),
  setPreferredShell: vi.fn(),
  restoreSessionId: vi.fn(),
  getSessionId: vi.fn(() => 'test-session'),
  getSessionIdForReattach: vi.fn(() => null),
  isConnectedToServer: vi.fn(() => false),
  isReconnecting: vi.fn(() => false),
  isCurrentlyFrozen: vi.fn(() => false),
  sendResize: vi.fn(() => false),
  sendRawInput: vi.fn(() => false),
  sendCommand: vi.fn(() => false),
}));

vi.mock('../services/terminalWebSocket', () => ({
  TerminalWebSocketService: {
    createInstance: (...args: unknown[]) => createInstanceMock(...args),
    getInstance: (...args: unknown[]) => getInstanceMock(...args),
    freezeAll: (...args: unknown[]) => freezeAllMock(...args),
    resumeAll: (...args: unknown[]) => resumeAllMock(...args),
    registerInstance: vi.fn(),
    unregisterInstance: vi.fn(),
  },
  reprInput: (input: string) => input,
}));

// Keep the WASM terminal-input hook's dependency light (TerminalPane pulls
// useWasmTerminalInput → ../services/wasmShell). The connected-path effect never
// calls initWasmShell, but the module import still happens, so stub it.
vi.mock('../services/wasmShell', () => ({
  initWasmShell: vi.fn(async () => ({
    writeFile: () => '',
    readFile: () => ({ content: '' }),
    getCwd: () => '/',
    executeCommand: () => ({ stdout: '', stderr: '', exitCode: 0 }),
    autoComplete: () => ({ completions: [] }),
    changeDir: () => ({ cwd: '/' }),
    listDir: () => ({ entries: [] }),
    deleteFile: () => '',
    clearConversation: () => {},
    stopAgent: () => {},
  })),
  resetWasmShell: vi.fn(),
}));

// Lucide-react icons break under jsdom (forwardRef pattern) — replace with a
// simple svg. (Mirrors TerminalPane.test.tsx.)
vi.mock('lucide-react', async () => {
  const ReactMod = await import('react');
  const createMockIcon = (name: string) => {
    const Comp = ReactMod.forwardRef((props: Record<string, unknown>, ref: unknown) => {
      return ReactMod.createElement('svg', { ref, 'data-icon': name, ...props });
    });
    Comp.displayName = name;
    return Comp;
  };
  const icons = [
    'X',
    'TriangleAlert',
    'Terminal',
    'Copy',
    'ClipboardPaste',
    'Search',
    'Trash2',
    'Rows2',
    'Columns2',
    'TextSelect',
    'Link2',
    'ChevronUp',
    'ChevronDown',
    'Type',
    'Hash',
  ];
  const mod: Record<string, unknown> = {};
  for (const name of icons) mod[name] = createMockIcon(name);
  return mod;
});

vi.mock('../contexts/ThemeContext', () => ({
  useTheme: vi.fn(() => ({ themePack: { id: 'default' } })),
}));

vi.mock('../utils/clipboard', () => ({
  copyToClipboard: vi.fn().mockResolvedValue(undefined),
}));

// xterm + addons: constructible fns (the xterm hook does `new XTerm(...)`).
vi.mock('@xterm/xterm', () => {
  const mockTerm = {
    hasSelection: vi.fn(() => false),
    getSelection: vi.fn(() => ''),
    selectAll: vi.fn(),
    clear: vi.fn(),
    loadAddon: vi.fn(),
    open: vi.fn(),
    onData: vi.fn(() => ({ dispose: vi.fn() })),
    onSelectionChange: vi.fn(() => ({ dispose: vi.fn() })),
    onTitleChange: vi.fn(() => ({ dispose: vi.fn() })),
    focus: vi.fn(),
    writeln: vi.fn(),
    dispose: vi.fn(),
    registerLinkProvider: vi.fn(() => ({ dispose: vi.fn() })),
    attachCustomKeyEventHandler: vi.fn(),
    cols: 80,
    rows: 24,
    buffer: { active: { baseY: 0, getLine: vi.fn(() => null) } },
    options: {},
    core: { buffer: { x: 0 } },
  };
  return {
    Terminal: vi.fn(function (this: object) {
      Object.assign(this, mockTerm);
    }),
  };
});
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: vi.fn(function (this: Record<string, unknown>) {
    this.fit = vi.fn();
    this.dispose = vi.fn();
  }),
}));
vi.mock('@xterm/addon-search', () => ({
  SearchAddon: vi.fn(function (this: Record<string, unknown>) {
    this.findNext = vi.fn();
    this.findPrevious = vi.fn();
    this.clearDecorations = vi.fn();
    this.onDidChangeResults = vi.fn(() => ({ dispose: vi.fn() }));
    this.dispose = vi.fn();
  }),
}));

beforeEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  createInstanceMock.mockReset();
  getInstanceMock.mockReset();
  getInstanceMock.mockReturnValue(mockService);
  createInstanceMock.mockReturnValue(mockService);
  // terminalScrollback (pulled in via useTerminalScrollback in TerminalPane)
  // touches indexedDB on mount/unmount; jsdom has no indexedDB, so install a
  // minimal inert stub to keep the unmount cleanup from throwing benign
  // "indexedDB is not defined" console noise (it does not affect assertions).
  if (typeof globalThis.indexedDB === 'undefined') {
    (globalThis as unknown as { indexedDB: unknown }).indexedDB = {
      open: () => ({
        onupgradeneeded: null,
        onerror: null,
        onsuccess: null,
        result: { transaction: () => ({ objectStore: () => ({ createObjectStore: () => {} }), oncomplete: () => {} }) },
      }),
    };
  }
});

afterEach(() => {
  vi.unstubAllEnvs();
});

// ── 1. useTerminalSession hook-level signal ───────────────────────────────────

type HookModule = typeof import('../hooks/useTerminalSession');

async function loadHook(enabled: boolean): Promise<HookModule> {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL', '');
  vi.resetModules();
  return import('../hooks/useTerminalSession');
}

const captured: React.MutableRefObject<UseTerminalSessionReturn | null> = { current: null };

function SessionHookHarness(props: {
  isActive: boolean;
  isConnected: boolean;
  captured: React.MutableRefObject<UseTerminalSessionReturn | null>;
}) {
  // Pre-populated xterm ref: the hook only reads xtermRef.current, and a plain
  // object guarantees the null-checks never misfire.
  const xtermRef = useRef<XTerm | null>({ write: vi.fn(), writeln: vi.fn(), focus: vi.fn() } as unknown as XTerm);
  const fitAddonRef = useRef<unknown>(null) as unknown as React.RefObject<import('@xterm/addon-fit').FitAddon | null>;
  const ret = useSessionHook({
    isActive: props.isActive,
    isConnected: props.isConnected,
    xtermRef,
    fitAddonRef,
    preferredShell: null,
    reattachSessionId: null,
    onResetSearch: () => {},
    onResetReverseSearch: () => {},
    onSaveScrollback: () => {},
    onLoadScrollback: () => {},
  });
  props.captured.current = ret;
  return null;
}

// Loaded dynamically per flag state.
let useSessionHook: HookModule['useTerminalSession'];

async function loadHookFn(enabled: boolean): Promise<void> {
  const mod = await loadHook(enabled);
  useSessionHook = mod.useTerminalSession;
}

describe('useTerminalSession — R-3 effect short-circuit', () => {
  beforeEach(() => {
    captured.current = null;
  });

  it('flag ON: effect short-circuits — createInstance NEVER called, paneConnected stays false, terminalProvidedByShell=true', async () => {
    await loadHookFn(true);

    await act(async () => {
      render(<SessionHookHarness isActive={true} isConnected={true} captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook, 'hook result must have been captured').toBeTruthy();
    expect(hook!.terminalProvidedByShell).toBe(true);
    expect(hook!.paneConnected).toBe(false);
    expect(createInstanceMock).not.toHaveBeenCalled();
  });

  it('flag OFF + connected: createInstance IS called (today behavior), terminalProvidedByShell=false', async () => {
    await loadHookFn(false);

    await act(async () => {
      render(<SessionHookHarness isActive={true} isConnected={true} captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook, 'hook result must have been captured').toBeTruthy();
    expect(hook!.terminalProvidedByShell).toBe(false);
    expect(createInstanceMock).toHaveBeenCalledTimes(1);
  });

  it('flag OFF + not active: createInstance NOT called (effect skips, matches today behavior)', async () => {
    await loadHookFn(false);

    await act(async () => {
      render(<SessionHookHarness isActive={false} isConnected={true} captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(captured.current).toBeTruthy();
    expect(createInstanceMock).not.toHaveBeenCalled();
  });
});

// ── 2. TerminalPane render conditions (real component mount) ─────────────────
//
// Mounting the real TerminalPane is the authoritative check for the render
// branch the task asks about:
//   { (wasmProvidedByShell || terminalProvidedByShell) && <…Terminal provided by the native shell> }
//   { !paneConnected && !wasmActive && !wasmLoading && !wasmProvidedByShell &&
//     !terminalProvidedByShell && <…Loading terminal…> }
// Flag ON: terminalProvidedByShell=true → the placeholder renders and the
// "Loading terminal..." line is suppressed. Flag OFF: neither provided-by-shell
// bit is set and paneConnected stays false (no session_ready event fired) →
// "Loading terminal..." renders.

type PaneModule = typeof import('../components/TerminalPane');

async function loadPane(enabled: boolean): Promise<PaneModule['default']> {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL', '');
  vi.resetModules();
  const mod = await import('../components/TerminalPane');
  return mod.default;
}

const PLACEHOLDER = 'Terminal provided by the native shell';
const LOADING = 'Loading terminal...';

describe('TerminalPane — R-3 render conditions', () => {
  it('flag ON: renders the shell-provided placeholder and NOT "Loading terminal..."', async () => {
    const TerminalPane = await loadPane(true);

    let rendered!: ReturnType<typeof render>;
    await act(async () => {
      rendered = render(<TerminalPane isActive={true} isConnected={true} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const text = rendered.container.textContent ?? '';
    expect(text).toContain(PLACEHOLDER);
    expect(text).not.toContain(LOADING);
    // The shell-owned terminal means no PTY transport is ever opened.
    expect(createInstanceMock).not.toHaveBeenCalled();
  });

  it('flag OFF: renders "Loading terminal..." and NOT the shell-provided placeholder', async () => {
    const TerminalPane = await loadPane(false);

    let rendered!: ReturnType<typeof render>;
    await act(async () => {
      rendered = render(<TerminalPane isActive={true} isConnected={true} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const text = rendered.container.textContent ?? '';
    expect(text).toContain(LOADING);
    expect(text).not.toContain(PLACEHOLDER);
    // Default build still opens the PTY transport.
    expect(createInstanceMock).toHaveBeenCalledTimes(1);
  });
});

// ── 3. Stub surface (the hard-exclusion stand-in) ─────────────────────────────
//
// Vitest does NOT apply vite's conditional alias (nativeTerminalStubAliases),
// so we import the stub by its DIRECT path. It is the inert no-op stand-in a
// --native-terminal dist bundles. `NATIVE_TERMINAL_ENABLED` is a module
// constant read from import.meta.env at import time, so we flip it per case.

describe('nativeTerminalStubs/terminalWebSocket (inert no-op surface)', () => {
  it('createInstance()/getInstance() return a shared inert instance; methods are no-ops', async () => {
    // Env unset in the jsdom default (vitest does not define the flag) →
    // createInstanceMock is NOT used here; we import the REAL stub module.
    const stub = await import('../services/nativeTerminalStubs/terminalWebSocket');
    const svc = stub.TerminalWebSocketService;

    const a = svc.createInstance();
    const b = svc.getInstance();
    // The shared no-op singleton: both return the same instance.
    expect(a).toBe(b);
    expect(a).toBe(svc.createInstance());

    // Read-only state methods return the inert values.
    expect(a.isReady()).toBe(false);
    expect(a.isConnectedToServer()).toBe(false);
    expect(a.isCurrentlyFrozen()).toBe(false);
    expect(a.isReconnecting()).toBe(false);
    expect(a.getSessionId()).toBeNull();
    expect(a.getSessionIdForReattach()).toBeNull();
    expect(a.restorePersistedSessionId()).toBeNull();

    // Command/transport methods are safe no-ops returning false.
    expect(a.sendCommand('ls')).toBe(false);
    expect(a.sendRawInput('x')).toBe(false);
    expect(a.sendResize(80, 24)).toBe(false);
    expect(a.closeSession()).toBe(false);

    // No-ops that must not throw.
    expect(() => {
      svc.freezeAll();
      svc.resumeAll();
      a.freeze();
      a.resume();
      a.connect();
      a.disconnect();
      a.clearPersistedSession();
      a.persistSessionId();
      a.setPreferredShell(null);
      a.resetAndReconnect();
      svc.registerInstance(a);
      svc.unregisterInstance(a);
    }).not.toThrow();
  });

  it('onEvent(cb) returns an unsubscribe function (a no-op)', async () => {
    const stub = await import('../services/nativeTerminalStubs/terminalWebSocket');
    const svc = stub.TerminalWebSocketService.getInstance();
    const unsub = svc.onEvent(() => {});
    expect(typeof unsub).toBe('function');
    expect(() => unsub()).not.toThrow();
  });

  it('reprInput is the identity function', async () => {
    const stub = await import('../services/nativeTerminalStubs/terminalWebSocket');
    expect(stub.reprInput('x')).toBe('x');
    expect(stub.reprInput('')).toBe('');
    expect(stub.reprInput('a b\n')).toBe('a b\n');
  });

  it('NATIVE_TERMINAL_ENABLED is a boolean and false when env is unset (flag-off state)', async () => {
    // VITE_SPROUT_NATIVE_TERMINAL is not defined by vitest here, so the
    // compile-time constant must evaluate to false.
    const { NATIVE_TERMINAL_ENABLED } = await import('../services/nativeTerminalStubs/nativeTerminalFlag');
    expect(typeof NATIVE_TERMINAL_ENABLED).toBe('boolean');
    expect(NATIVE_TERMINAL_ENABLED).toBe(false);
  });

  it('NATIVE_TERMINAL_ENABLED is true when VITE_SPROUT_NATIVE_TERMINAL=1 (fresh import)', async () => {
    vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL', '1');
    vi.resetModules();
    const { NATIVE_TERMINAL_ENABLED } = await import('../services/nativeTerminalStubs/nativeTerminalFlag');
    vi.unstubAllEnvs();
    expect(NATIVE_TERMINAL_ENABLED).toBe(true);
  });
});
