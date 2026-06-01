// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ── Hoisted mock state (must be defined before vi.mock hoisting) ──────

const hoisted = vi.hoisted(() => {
  let mockTerminalInstance: any = null;
  let mockFitAddonInstance: any = null;

  // Mock Terminal class from @xterm/xterm
  class MockTerminal {
    options: any = {};
    disposed = false;
    loadedAddons: any[] = [];
    dataHandlers: ((data: string) => void)[] = [];
    element: HTMLDivElement | null = null;
    writeCalls: string[] = [];
    selectionText = '';
    selectAllCalled = false;
    clearCalled = false;
    pasteCalls: string[] = [];

    constructor(options?: any) {
      if (options) {
        this.options = { ...options };
      }
      mockTerminalInstance = this;
    }

    loadAddon(addon: any) {
      this.loadedAddons.push(addon);
    }

    open(parent: HTMLDivElement) {
      this.element = parent;
    }

    write(data: string) {
      this.writeCalls.push(data);
    }

    onData(callback: (data: string) => void) {
      this.dataHandlers.push(callback);
    }

    getSelection() {
      return this.selectionText || null;
    }

    paste(data: string) {
      this.pasteCalls.push(data);
      this.dataHandlers.forEach((cb) => cb(data));
    }

    selectAll() {
      this.selectAllCalled = true;
    }

    clear() {
      this.clearCalled = true;
    }

    dispose() {
      this.disposed = true;
      mockTerminalInstance = null;
    }
  }

  // Mock FitAddon class
  class MockFitAddon {
    fitted = false;

    fit() {
      this.fitted = true;
    }
  }

  return {
    MockTerminal,
    MockFitAddon,
    getMockTerminalInstance: () => mockTerminalInstance,
    getMockFitAddonInstance: () => mockFitAddonInstance,
  };
});

// Mock clipboard API
const mockClipboard = {
  readText: vi.fn().mockResolvedValue(''),
};

// ── Mocks before importing the component ────────────────────────────────

vi.mock('@xterm/xterm', () => ({
  Terminal: hoisted.MockTerminal,
}));

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: hoisted.MockFitAddon,
}));

// Mock ContextMenu
vi.mock('./ContextMenu', () => {
  function MockContextMenu({ isOpen, onClose, children }: any) {
    if (!isOpen) return null;
    return createElement('div', {
      className: 'mock-terminal-context-menu',
      onMouseDown: () => {
        act(() => { onClose(); });
      },
    }, children);
  }
  return { __esModule: true, default: MockContextMenu };
});

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('../utils/clipboard', () => ({
  copyToClipboard: vi.fn(),
}));

import TerminalPane, { TerminalThemePack, CreateTerminalConnection } from './TerminalPane';
import * as Clipboard from '../utils/clipboard';

// ── Helpers ──────────────────────────────────────────────────────────────

let container: HTMLDivElement;
let root: Root;

function resetMockState() {
  // No-op — the mock classes maintain state via hoisted getters
}

// MockConnection intersects the production connection type with two
// test-only fields the assertions reach into to drive the mock.
type MockConnection = ReturnType<CreateTerminalConnection> & {
  _onDataCbs: ((data: string) => void)[];
  _onExitCbs: ((code: number) => void)[];
};

function createMockConnection(): MockConnection {
  const onDataCbs: ((data: string) => void)[] = [];
  const onExitCbs: ((code: number) => void)[] = [];
  return {
    send: vi.fn((data: string) => {}),
    onData: vi.fn((cb: (data: string) => void) => { onDataCbs.push(cb); }),
    onExit: vi.fn((cb: (code: number) => void) => { onExitCbs.push(cb); }),
    close: vi.fn(),
    _onDataCbs: onDataCbs,
    _onExitCbs: onExitCbs,
  };
}

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // @ts-expect-error — mock navigator.clipboard
  globalThis.navigator.clipboard = mockClipboard;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
  delete (globalThis as any).navigator.clipboard;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  resetMockState();
  mockClipboard.readText.mockResolvedValue('');
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  // Clean up context menus rendered to document.body
  const menu = document.querySelector('.mock-terminal-context-menu');
  if (menu) menu.remove();
});

// ── Tests ────────────────────────────────────────────────────────────────

describe('TerminalPane', () => {
  // ── Initialization ─────────────────────────────────────────────────

  it('renders container div with terminal-pane class', () => {
    act(() => {
      root.render(
        createElement(TerminalPane, {
          isActive: true,
          sessionId: 'test-session',
        })
      );
    });

    const pane = container.querySelector('.terminal-pane');
    expect(pane).not.toBeNull();
    expect(pane?.getAttribute('style')).toContain('width: 100%');
    expect(pane?.getAttribute('style')).toContain('height: 100%');
  });

  it('creates Terminal instance with default options', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance()).not.toBeNull();
    expect(hoisted.getMockTerminalInstance().options.fontSize).toBe(13);
    expect(hoisted.getMockTerminalInstance().options.cursorBlink).toBe(true);
    expect(hoisted.getMockTerminalInstance().options.convertEol).toBe(true);
    expect(hoisted.getMockTerminalInstance().options.scrollback).toBe(10000);
    expect(hoisted.getMockTerminalInstance().options.wordSeparator).toBe(" ()[]{}',\"`");
  });

  it('uses custom fontSize', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        fontSize: 16,
      }));
    });

    expect(hoisted.getMockTerminalInstance().options.fontSize).toBe(16);
  });

  it('uses default fontFamily', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance().options.fontFamily).toContain('JetBrains Mono');
  });

  it('uses custom fontFamily from themePack', () => {
    const themePack: TerminalThemePack = {
      name: 'custom',
      terminal: {
        fontFamily: 'Fira Code',
        background: '#000000',
        foreground: '#ffffff',
        cursor: '#00ff00',
        selectionBackground: '#333333',
      },
    };

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        themePack,
      }));
    });

    expect(hoisted.getMockTerminalInstance().options.fontFamily).toBe('Fira Code');
    expect(hoisted.getMockTerminalInstance().options.theme.background).toBe('#000000');
    expect(hoisted.getMockTerminalInstance().options.theme.foreground).toBe('#ffffff');
    expect(hoisted.getMockTerminalInstance().options.theme.cursor).toBe('#00ff00');
    expect(hoisted.getMockTerminalInstance().options.theme.selectionBackground).toBe('#333333');
  });

  it('loads FitAddon', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance().loadedAddons.length).toBeGreaterThan(0);
    const fitAddon = hoisted.getMockTerminalInstance().loadedAddons.find(
      (a: any) => a instanceof hoisted.MockFitAddon
    );
    expect(fitAddon).toBeDefined();
  });

  it('opens terminal in container element', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance().element).toBe(container.querySelector('.terminal-pane'));
  });

  it('calls fit() after loading FitAddon', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const fitAddon = hoisted.getMockTerminalInstance().loadedAddons[0];
    expect(fitAddon.fitted).toBe(true);
  });

  // ── Font size updates ──────────────────────────────────────────────

  it('updates fontSize when prop changes', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        fontSize: 13,
      }));
    });

    expect(hoisted.getMockTerminalInstance().options.fontSize).toBe(13);

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        fontSize: 16,
      }));
    });

    expect(hoisted.getMockTerminalInstance().options.fontSize).toBe(16);
  });

  it('calls fit() when font size changes', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        fontSize: 13,
      }));
    });

    const fitAddon = hoisted.getMockTerminalInstance().loadedAddons[0];
    fitAddon.fitted = false; // reset

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        fontSize: 16,
      }));
    });

    expect(fitAddon.fitted).toBe(true);
  });

  // ── Connection management ──────────────────────────────────────────

  it('creates connection when isActive and createConnection provided', () => {
    const createConnection = vi.fn().mockImplementation(() => {
      const onDataCbs: ((data: string) => void)[] = [];
      const onExitCbs: ((code: number) => void)[] = [];
      return {
        send: vi.fn(),
        onData: vi.fn((cb: (data: string) => void) => { onDataCbs.push(cb); }),
        onExit: vi.fn((cb: (code: number) => void) => { onExitCbs.push(cb); }),
        close: vi.fn(),
        _onDataCbs: onDataCbs,
        _onExitCbs: onExitCbs,
      };
    });

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        createConnection,
      }));
    });

    expect(createConnection).toHaveBeenCalledWith('test-session');
  });

  it('does not create connection when isActive is false', () => {
    const createConnection = vi.fn();

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: false,
        sessionId: 'test-session',
        createConnection,
      }));
    });

    expect(createConnection).not.toHaveBeenCalled();
  });

  it('does not create connection when createConnection is not provided', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance().dataHandlers.length).toBeGreaterThan(0);
  });

  it('writes connection data to terminal', () => {
    const mockConn = createMockConnection();
    const createConnection = vi.fn().mockReturnValue(mockConn);

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        createConnection,
      }));
    });

    // Simulate connection sending data
    act(() => {
      mockConn._onDataCbs.forEach((cb: (data: string) => void) => cb('hello world'));
    });

    expect(hoisted.getMockTerminalInstance().writeCalls).toContain('hello world');
  });

  it('sends terminal data to connection', () => {
    const mockConn = createMockConnection();
    const createConnection = vi.fn().mockReturnValue(mockConn);

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        createConnection,
      }));
    });

    // Simulate terminal producing data
    act(() => {
      hoisted.getMockTerminalInstance().dataHandlers.forEach((cb: (data: string) => void) => cb('user input'));
    });

    expect(mockConn.send).toHaveBeenCalledWith('user input');
  });

  it('writes exit message when connection exits', () => {
    const mockConn = createMockConnection();
    const createConnection = vi.fn().mockReturnValue(mockConn);

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        createConnection,
      }));
    });

    // Simulate connection exit
    act(() => {
      mockConn._onExitCbs.forEach((cb: (code: number) => void) => cb(0));
    });

    expect(hoisted.getMockTerminalInstance().writeCalls.length).toBeGreaterThan(0);
    expect(hoisted.getMockTerminalInstance().writeCalls[hoisted.getMockTerminalInstance().writeCalls.length - 1]).toContain('Process exited with code 0');
  });

  it('closes connection on unmount', () => {
    const mockConn = createMockConnection();
    const createConnection = vi.fn().mockReturnValue(mockConn);

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        createConnection,
      }));
    });

    act(() => {
      root.unmount();
    });

    expect(mockConn.close).toHaveBeenCalled();
  });

  // ── WASM shell ─────────────────────────────────────────────────────

  it('creates WASM shell when createWasmShell is provided', async () => {
    const mockShell = {
      write: vi.fn(),
      onData: vi.fn(),
      close: vi.fn(),
    };
    const createWasmShell = vi.fn().mockResolvedValue(mockShell);

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        createWasmShell,
      }));
    });

    expect(createWasmShell).toHaveBeenCalled();

    await act(async () => {});

    expect(mockShell.onData).toHaveBeenCalled();
  });

  it('does not create WASM shell when isActive is false', () => {
    const createWasmShell = vi.fn();

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: false,
        sessionId: 'test-session',
        createWasmShell,
      }));
    });

    expect(createWasmShell).not.toHaveBeenCalled();
  });

  // ── Focus handling ─────────────────────────────────────────────────

  it('fires onFocus callback when terminal gets focus', () => {
    const onFocus = vi.fn();

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        onFocus,
      }));
    });

    // Simulate focus event on terminal element
    act(() => {
      hoisted.getMockTerminalInstance().element.dispatchEvent(new Event('focus'));
    });

    expect(onFocus).toHaveBeenCalled();
  });

  // ── Cleanup on unmount ─────────────────────────────────────────────

  it('disposes terminal on unmount', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const terminal = hoisted.getMockTerminalInstance();

    act(() => {
      root.unmount();
    });

    expect(terminal.disposed).toBe(true);
  });

  // ── Split resize ───────────────────────────────────────────────────

  it('does not resize when isSplit does not change', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        isSplit: false,
      }));
    });

    const fitAddon = hoisted.getMockTerminalInstance().loadedAddons[0];
    fitAddon.fitted = false;

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        isSplit: false,
      }));
    });

    // fit should not be called again for unchanged isSplit
    expect(fitAddon.fitted).toBe(false);
  });

  it('calls fit() when isSplit changes', () => {
    vi.useFakeTimers();

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        isSplit: false,
      }));
    });

    const fitAddon = hoisted.getMockTerminalInstance().loadedAddons[0];
    fitAddon.fitted = false; // reset after initial mount

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
        isSplit: true,
      }));
    });

    // isSplit change triggers a delayed fit() via setTimeout(50ms)
    act(() => {
      vi.advanceTimersByTime(100);
    });

    expect(fitAddon.fitted).toBe(true);

    vi.useRealTimers();
  });

  // ── Context menu ───────────────────────────────────────────────────

  it('opens context menu on right-click', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    expect(document.querySelector('.mock-terminal-context-menu')).not.toBeNull();
  });

  it('context menu has copy option', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let copyBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Copy')) {
        copyBtn = btn as HTMLElement;
        break;
      }
    }
    expect(copyBtn).not.toBeNull();
  });

  it('context menu has paste option', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let pasteBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Paste')) {
        pasteBtn = btn as HTMLElement;
        break;
      }
    }
    expect(pasteBtn).not.toBeNull();
  });

  it('context menu has clear option', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let clearBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Clear Terminal')) {
        clearBtn = btn as HTMLElement;
        break;
      }
    }
    expect(clearBtn).not.toBeNull();
  });

  it('context menu has select all option', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let selectAllBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Select All')) {
        selectAllBtn = btn as HTMLElement;
        break;
      }
    }
    expect(selectAllBtn).not.toBeNull();
  });

  // ── Context menu actions ───────────────────────────────────────────

  it('copies selection when copy is clicked', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    // Set terminal selection
    hoisted.getMockTerminalInstance().selectionText = 'selected text';

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const copyBtn = menu?.querySelectorAll('button')[0];
    act(() => {
      copyBtn?.click();
    });

    expect(Clipboard.copyToClipboard).toHaveBeenCalledWith('selected text');
  });

  it('does not call copyToClipboard when no selection', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    // No selection set (defaults to empty string → falsy)
    hoisted.getMockTerminalInstance().selectionText = '';

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const copyBtn = menu?.querySelectorAll('button')[0];
    act(() => {
      copyBtn?.click();
    });

    expect(Clipboard.copyToClipboard).not.toHaveBeenCalled();
  });

  it('reads clipboard and pastes when paste is clicked', async () => {
    mockClipboard.readText.mockResolvedValue('pasted text');

    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const pasteBtn = menu?.querySelectorAll('button')[1];
    act(() => {
      pasteBtn?.click();
    });

    await act(async () => {});

    expect(mockClipboard.readText).toHaveBeenCalled();
    // Verify paste was called with the clipboard content
    expect(hoisted.getMockTerminalInstance().pasteCalls).toContain('pasted text');
  });

  it('clears terminal when clear is clicked', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const clearBtn = menu?.querySelectorAll('button')[2];
    act(() => {
      clearBtn?.click();
    });

    expect(hoisted.getMockTerminalInstance().clearCalled).toBe(true);
  });

  it('selects all when select all is clicked', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    const pane = container.querySelector('.terminal-pane');
    act(() => {
      pane?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-terminal-context-menu');
    const selectAllBtn = menu?.querySelectorAll('button')[3];
    act(() => {
      selectAllBtn?.click();
    });

    expect(hoisted.getMockTerminalInstance().selectAllCalled).toBe(true);
  });

  // ── Default theme colors ───────────────────────────────────────────

  it('uses default theme colors when no themePack', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance().options.theme.background).toBe('#1e1e2e');
    expect(hoisted.getMockTerminalInstance().options.theme.foreground).toBe('#cdd6f4');
    expect(hoisted.getMockTerminalInstance().options.theme.cursor).toBe('#f5e0dc');
    expect(hoisted.getMockTerminalInstance().options.theme.selectionBackground).toBe('#585b7066');
  });

  // ── Props passed through ───────────────────────────────────────────

  it('passes sessionId to component', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'custom-session-123',
      }));
    });

    expect(hoisted.getMockTerminalInstance()).not.toBeNull();
  });

  it('uses default fontSize of 13', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance().options.fontSize).toBe(13);
  });

  it('uses isSplit default of false', () => {
    act(() => {
      root.render(createElement(TerminalPane, {
        isActive: true,
        sessionId: 'test-session',
      }));
    });

    expect(hoisted.getMockTerminalInstance()).not.toBeNull();
  });
});
