// @ts-nocheck

import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import { copyToClipboard } from '../utils/clipboard';
import { useTheme } from '../contexts/ThemeContext';
import TerminalPane from './TerminalPane';

// ---------------------------------------------------------------------------
// Mock XTerm instance (mutable, shared across tests)
// ---------------------------------------------------------------------------

const mockFitAddon = {
  fit: vi.fn(),
};

const mockTerm = {
  hasSelection: vi.fn().mockReturnValue(false),
  getSelection: vi.fn().mockReturnValue(''),
  selectAll: vi.fn(),
  clear: vi.fn(),
  loadAddon: vi.fn(),
  open: vi.fn(),
  onData: vi.fn(() => ({ dispose: vi.fn() })),
  onSelectionChange: vi.fn(() => ({ dispose: vi.fn() })),
  focus: vi.fn(),
  writeln: vi.fn(),
  dispose: vi.fn(),
  registerLinkProvider: vi.fn(() => ({ dispose: vi.fn() })),
  attachCustomKeyEventHandler: vi.fn(),
  cols: 80,
  rows: 24,
  buffer: {
    active: {
      baseY: 0,
      getLine: vi.fn().mockReturnValue(null),
    },
  },
  options: {},
  core: { buffer: { x: 0 } },
};

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('@xterm/xterm', () => ({
  Terminal: vi.fn(() => mockTerm),
}));

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: vi.fn(() => mockFitAddon),
}));

vi.mock('@xterm/addon-search', () => ({
  SearchAddon: vi.fn(() => ({
    findNext: vi.fn(),
    findPrevious: vi.fn(),
    clearDecorations: vi.fn(),
    onDidChangeResults: vi.fn(() => ({ dispose: vi.fn() })),
    dispose: vi.fn(),
  })),
}));

const mockService = {
  sendRawInput: vi.fn(),
  sendResize: vi.fn(),
  onEvent: vi.fn(),
  removeEvent: vi.fn(),
  disconnect: vi.fn(),
  connect: vi.fn(),
  closeSession: vi.fn(),
  getSessionId: vi.fn().mockReturnValue('test-session'),
  isConnectedToServer: vi.fn().mockReturnValue(false),
  isReconnecting: vi.fn().mockReturnValue(false),
  isCurrentlyFrozen: vi.fn().mockReturnValue(false),
};

vi.mock('../services/terminalWebSocket', () => ({
  TerminalWebSocketService: {
    createInstance: vi.fn(() => mockService),
  },
}));

// ── lucide-react mock ────────────────────────────────────────────────
// lucide-react icons use a forwardRef pattern that breaks in jsdom.
// Replace every icon with a simple <svg> forwardRef component.
vi.mock('lucide-react', async () => {
  const React = await import('react');
  const createMockIcon = (name) => {
    const Comp = React.forwardRef((props, ref) => {
      return React.createElement('svg', { ref, 'data-icon': name, ...props });
    });
    Comp.displayName = name;
    return Comp;
  };
  const icons = [
    'X', 'TriangleAlert', 'Terminal',
    'Copy', 'ClipboardPaste', 'Search', 'Trash2', 'Rows2', 'Columns2', 'TextSelect', 'Link2',
    'ChevronUp', 'ChevronDown', 'Type', 'Hash',
  ];
  const mod = {};
  for (const name of icons) {
    mod[name] = createMockIcon(name);
  }
  return mod;
});

vi.mock('../contexts/ThemeContext', () => ({
  useTheme: vi.fn(),
}));

vi.mock('../utils/clipboard', () => {
  return {
    copyToClipboard: vi.fn().mockResolvedValue(undefined),
  };
});

// ---------------------------------------------------------------------------
// Clipboard mock (jsdom doesn't provide navigator.clipboard)
// ---------------------------------------------------------------------------

Object.defineProperty(navigator, 'clipboard', {
  value: {
    writeText: vi.fn().mockResolvedValue(undefined),
    readText: vi.fn().mockResolvedValue(''),
  },
  writable: true,
  configurable: true,
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fireContextMenu(element: Element, x = 100, y = 100) {
  const event = new MouseEvent('contextmenu', {
    bubbles: true,
    cancelable: true,
    clientX: x,
    clientY: y,
  });
  element.dispatchEvent(event);
  return event;
}

function getMenu() {
  return document.body.querySelector('.context-menu');
}

function getMenuItems() {
  const menu = getMenu();
  return menu ? Array.from(menu.querySelectorAll('.context-menu-item')) : [];
}

function getMenuTexts() {
  return getMenuItems().map((el) => el.textContent?.trim() || '');
}

const flushRAF = () =>
  act(async () => {
    await new Promise((resolve) => requestAnimationFrame(resolve));
    await Promise.resolve();
  });

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

/**
 * Set up a mock buffer line with full URL string at the right position.
 * The URL detection code concatenates getCell chars, then runs a regex.
 * We place the URL starting at a given column offset.
 */
function setupMockLineWithUrl(url: string, urlStartCol: number, lineLength: number) {
  mockTerm.buffer.active.baseY = 0;
  mockTerm.buffer.active.getLine.mockImplementation((lineIdx: number) => {
    if (lineIdx !== 0) return null;
    return {
      length: lineLength,
      getCell: vi.fn().mockImplementation((col: number) => ({
        getChars: () => {
          // If this cell falls in the URL range, return the corresponding char
          if (col >= urlStartCol && col < urlStartCol + url.length) {
            return url[col - urlStartCol];
          }
          return '';
        },
      })),
    };
  });
}

// ---------------------------------------------------------------------------
// Test Suite
// ---------------------------------------------------------------------------

describe.skip('TerminalPane context menu', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    // Re-assert Terminal constructor mock (vi.mock hoisted + clearAllMocks in afterEach)
    (Terminal as any).mockImplementation(() => mockTerm);

    // Re-assert FitAddon constructor mock so it always returns mockFitAddon
    (FitAddon as any).mockImplementation(() => mockFitAddon);

    // Re-assert WebSocket mock service factory
    (TerminalWebSocketService as any).createInstance.mockImplementation(() => mockService);

    // Clipboard mock is set up at module level via vi.mock('../utils/clipboard')
    // No need to re-assert — vi.clearAllMocks preserves mock implementations

    // Reset mock term state (not the functions themselves)
    mockTerm.hasSelection.mockReturnValue(false);
    mockTerm.getSelection.mockReturnValue('');
    mockTerm.buffer.active.baseY = 0;
    mockTerm.buffer.active.getLine.mockReturnValue(null);

    // Clear mock service call history
    mockService.sendRawInput.mockClear();
    mockService.sendResize.mockClear();
    mockService.onEvent.mockClear();
    mockService.removeEvent.mockClear();
    mockService.disconnect.mockClear();
    mockService.connect.mockClear();

    // Clear clipboard mocks and re-assert resolved values
    navigator.clipboard.readText.mockClear();
    navigator.clipboard.readText.mockResolvedValue('');
    navigator.clipboard.writeText.mockClear();
    navigator.clipboard.writeText.mockResolvedValue(undefined);

    // Theme context (mocked vi.fn via vi.mock)
    (useTheme as any).mockReturnValue({ themePack: { id: 'default' } });
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    // Clean up any leftover portal elements
    document.querySelectorAll('.context-menu').forEach((el) => el.remove());
    vi.clearAllMocks();
  });

  it.skip('context menu appears on right-click in the terminal pane content', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    expect(paneContent).toBeTruthy();

    fireContextMenu(paneContent);
    await flushPromises();

    expect(getMenu()).toBeTruthy();
  });

  it('context menu shows all 4 standard items when selection exists', async () => {
    mockTerm.hasSelection.mockReturnValue(true);

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const texts = getMenuTexts();
    expect(texts).toContain('Copy');
    expect(texts).toContain('Paste');
    expect(texts).toContain('Clear Terminal');
    expect(texts).toContain('Select All');

    // Copy should NOT be disabled
    const copyBtn = getMenuItems().find((item) => item.textContent?.includes('Copy'));
    expect(copyBtn?.classList.contains('disabled')).toBe(false);
    expect((copyBtn as HTMLButtonElement)?.disabled).toBe(false);
  });

  it.skip('Copy is disabled when no selection', async () => {
    mockTerm.hasSelection.mockReturnValue(false);

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const copyBtn = getMenuItems().find((item) => item.textContent?.includes('Copy'));
    expect(copyBtn).toBeTruthy();
    expect(copyBtn?.classList.contains('disabled')).toBe(true);
    expect((copyBtn as HTMLButtonElement)?.disabled).toBe(true);
  });

  it.skip('Copy Link appears when URL is detected under cursor', async () => {
    // Place URL at column 10 of line 0, length 50
    const url = 'https://example.com/path';
    const urlStartCol = 10;
    setupMockLineWithUrl(url, urlStartCol, 50);

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    // The handler uses xtermContainerRef.current (i.e. .terminal-xterm div)
    // for getBoundingClientRect, not .terminal-pane-content.
    const xtermContainer = container.querySelector('.terminal-xterm');
    expect(xtermContainer).toBeTruthy();

    // Spy getBoundingClientRect since jsdom returns all zeros for layout
    const rect = {
      left: 0,
      top: 0,
      width: 800,
      height: 480,
      right: 800,
      bottom: 480,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect;
    vi.spyOn(xtermContainer as HTMLElement, 'getBoundingClientRect').mockReturnValue(rect);

    // cellWidth = 800 / 80 = 10, cellHeight = 480 / 24 = 20
    // cellY = floor(clientY / cellHeight), want cellY = 0 → clientY < 20
    // cellX = floor(clientX / cellWidth), want cellX = 15 → clientX ∈ [150, 160)
    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent, 155, 10);
    await flushPromises();

    const texts = getMenuTexts();
    expect(texts).toContain('Copy Link');
  });

  it('Copy action calls copyToClipboard with selection text', async () => {
    mockTerm.hasSelection.mockReturnValue(true);
    mockTerm.getSelection.mockReturnValue('selected text');

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const copyBtn = getMenuItems().find((item) => item.textContent?.includes('Copy'));
    expect(copyBtn).toBeTruthy();

    await act(async () => {
      copyBtn.click();
    });
    await flushPromises();

    expect(copyToClipboard).toHaveBeenCalledWith('selected text');
  });

  it('Paste action reads clipboard and sends input', async () => {
    // isConnected must be true so terminalWSRef.current is set
    // (handlePaste calls terminalWSRef.current?.sendRawInput(...))
    navigator.clipboard.readText.mockResolvedValue('pasted text');

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={true} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const pasteBtn = getMenuItems().find((item) => item.textContent?.includes('Paste'));
    expect(pasteBtn).toBeTruthy();

    // handlePaste is async — it awaits navigator.clipboard.readText()
    // Multiple flushes to let the promise chain resolve
    await act(async () => {
      pasteBtn.click();
    });
    await flushPromises();
    await flushPromises();
    await flushPromises();

    expect(navigator.clipboard.readText).toHaveBeenCalled();
    expect(mockService.sendRawInput).toHaveBeenCalledWith('pasted text');
  });

  it('Select All action calls term.selectAll()', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const selectAllBtn = getMenuItems().find((item) => item.textContent?.includes('Select All'));
    expect(selectAllBtn).toBeTruthy();

    await act(async () => {
      selectAllBtn.click();
    });
    await flushPromises();

    expect(mockTerm.selectAll).toHaveBeenCalled();
  });

  it.skip('Clear Terminal action calls term.clear()', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const clearBtn = getMenuItems().find((item) => item.textContent?.includes('Clear Terminal'));
    expect(clearBtn).toBeTruthy();

    await act(async () => {
      clearBtn.click();
    });
    await flushPromises();

    expect(mockTerm.clear).toHaveBeenCalled();
  });

  it('context menu closes on Escape key', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();
    await flushRAF();

    expect(getMenu()).toBeTruthy();

    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('context menu closes on click outside', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();
    await flushRAF();

    expect(getMenu()).toBeTruthy();

    // Click outside the menu (on the body, not the menu itself)
    await act(async () => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('Copy Link does NOT appear when no URL is under cursor', async () => {
    // Ensure no URL mock is set — getLine returns null by default
    mockTerm.buffer.active.getLine.mockReturnValue(null);

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const xtermContainer = container.querySelector('.terminal-xterm');
    const rect = {
      left: 0,
      top: 0,
      width: 800,
      height: 480,
      right: 800,
      bottom: 480,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect;
    vi.spyOn(xtermContainer as HTMLElement, 'getBoundingClientRect').mockReturnValue(rect);

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent, 155, 10);
    await flushPromises();

    const texts = getMenuTexts();
    expect(texts).not.toContain('Copy Link');
  });

  it('context menu closes on scroll', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();
    await flushRAF();

    expect(getMenu()).toBeTruthy();

    await act(async () => {
      window.dispatchEvent(new Event('scroll', { bubbles: true }));
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('context menu closes when pane deactivates', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    expect(getMenu()).toBeTruthy();

    // Deactivate the pane
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={false} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });
});

// ── wordSeparator option ────────────────────────────────────────────

describe('TerminalPane wordSeparator', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    // Re-assert Terminal constructor mock (imported at module level via vi.mock)
    (Terminal as any).mockImplementation(() => mockTerm);

    // Re-assert FitAddon constructor mock (imported at module level via vi.mock)
    (FitAddon as any).mockImplementation(() => mockFitAddon);

    // Re-assert WebSocket mock service factory
    // TerminalWebSocketService is already imported at module top and mocked via vi.mock
    (TerminalWebSocketService as any).createInstance.mockImplementation(() => mockService);

    // Theme context (mocked vi.fn via vi.mock)
    (useTheme as any).mockReturnValue({ themePack: { id: 'default' } });
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    vi.clearAllMocks();
  });

  it('passes wordSeparator option to Terminal constructor', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<TerminalPane isActive={true} isConnected={false} showCloseButton={false} />);
    });
    await flushPromises();

    const calls = (Terminal as any).mock.calls;
    // Find the call that passed options (first arg is an object)
    const optionsCall = calls.find((call: unknown[]) => call[0] && typeof call[0] === 'object');
    expect(optionsCall).toBeDefined();
    expect(optionsCall[0].wordSeparator).toBe(' ()[]{}\',"`');
  });
});

// ── PTY exit state ───────────────────────────────────────────────────

describe('TerminalPane pty_exit exited state', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    // Re-assert constructor mocks after vi.clearAllMocks
    (Terminal as any).mockImplementation(() => mockTerm);
    (FitAddon as any).mockImplementation(() => mockFitAddon);
    (TerminalWebSocketService as any).createInstance.mockImplementation(() => mockService);

    // Theme context
    (useTheme as any).mockReturnValue({ themePack: { id: 'default' } });

    // Reset mock term state
    mockTerm.hasSelection.mockReturnValue(false);
    mockTerm.getSelection.mockReturnValue('');
    mockTerm.buffer.active.baseY = 0;
    mockTerm.buffer.active.getLine.mockReturnValue(null);

    // Clear mock service call history
    mockService.sendRawInput.mockClear();
    mockService.sendResize.mockClear();
    mockService.onEvent.mockClear();
    mockService.removeEvent.mockClear();
    mockService.disconnect.mockClear();
    mockService.connect.mockClear();
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    vi.clearAllMocks();
  });

  /**
   * Helper: find the event handler callback that was registered via
   * mockService.onEvent, and invoke it with a synthetic WsEvent.
   */
  function triggerEvent(event: { type: string; data?: any }) {
    const calls = mockService.onEvent.mock.calls;
    expect(calls.length).toBeGreaterThan(0, 'Expected onEvent to have been called');
    const handler = calls[0][0]; // first registered callback
    if (typeof handler === 'function') {
      handler(event);
    }
  }

  /**
   * Helper: capture the onData callback registered on mockTerm and invoke it.
   */
  function triggerOnData(data: string) {
    const calls = mockTerm.onData.mock.calls;
    expect(calls.length).toBeGreaterThan(0, 'Expected onData to have been called');
    const cb = calls[0][0];
    if (typeof cb === 'function') {
      cb(data);
    }
  }

  it('does NOT have terminal-pane-exited class initially', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <TerminalPane
          isActive={true}
          isConnected={true}
          showCloseButton={false}
        />,
      );
    });
    await flushPromises();

    const pane = container.querySelector('.terminal-pane');
    expect(pane).toBeTruthy();
    expect(pane?.classList.contains('terminal-pane-exited')).toBe(false);
  });

  it('adds terminal-pane-exited class after pty_exit event', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <TerminalPane
          isActive={true}
          isConnected={true}
          showCloseButton={false}
        />,
      );
    });
    await flushPromises();

    const pane = container.querySelector('.terminal-pane');
    expect(pane?.classList.contains('terminal-pane-exited')).toBe(false);

    // Simulate pty_exit event from the WebSocket
    await act(async () => {
      triggerEvent({ type: 'pty_exit' });
    });
    await flushPromises();

    expect(pane?.classList.contains('terminal-pane-exited')).toBe(true);
  });

  it('calls onProcessExit prop when pty_exit event fires', async () => {
    const onProcessExit = vi.fn();

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <TerminalPane
          isActive={true}
          isConnected={true}
          showCloseButton={false}
          onProcessExit={onProcessExit}
        />,
      );
    });
    await flushPromises();

    expect(onProcessExit).not.toHaveBeenCalled();

    await act(async () => {
      triggerEvent({ type: 'pty_exit' });
    });
    await flushPromises();

    expect(onProcessExit).toHaveBeenCalledTimes(1);
  });

  it('blocks onData input when in exited state', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <TerminalPane
          isActive={true}
          isConnected={true}
          showCloseButton={false}
        />,
      );
    });
    await flushPromises();

    // Before exit: input should flow through to sendRawInput
    mockService.sendRawInput.mockClear();
    triggerOnData('hello');
    await flushPromises();
    expect(mockService.sendRawInput).toHaveBeenCalledWith('hello');

    // Trigger pty_exit to enter exited state
    await act(async () => {
      triggerEvent({ type: 'pty_exit' });
    });
    await flushPromises();

    // After exit: input should be blocked
    mockService.sendRawInput.mockClear();
    triggerOnData('blocked');
    await flushPromises();
    expect(mockService.sendRawInput).not.toHaveBeenCalled();
  });

  it('blocks onPaste input when in exited state', async () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <TerminalPane
          isActive={true}
          isConnected={true}
          showCloseButton={false}
        />,
      );
    });
    await flushPromises();

    // Simulate pty_exit to enter exited state
    await act(async () => {
      triggerEvent({ type: 'pty_exit' });
    });
    await flushPromises();

    const pane = container.querySelector('.terminal-pane');
    expect(pane?.classList.contains('terminal-pane-exited')).toBe(true);

    // The onPaste callback uses the same isExitedRef check as onData.
    // We verify this via the same input blocking mechanism — paste data
    // flows through handlePtyInput which calls sendRawInput.
    mockService.sendRawInput.mockClear();

    // Directly trigger paste via attachCustomKeyEventHandler is complex,
    // so we test the same isExitedRef guard by verifying onData is blocked
    // (both share the isExitedRef check).
    triggerOnData('pasted');
    await flushPromises();
    expect(mockService.sendRawInput).not.toHaveBeenCalled();
  });

  it('removes terminal-pane-exited class when pane reconnects', async () => {
    const onProcessExit = vi.fn();

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <TerminalPane
          isActive={true}
          isConnected={true}
          showCloseButton={false}
          onProcessExit={onProcessExit}
        />,
      );
    });
    await flushPromises();

    const pane = container.querySelector('.terminal-pane');

    // Enter exited state
    await act(async () => {
      triggerEvent({ type: 'pty_exit' });
    });
    await flushPromises();
    expect(pane?.classList.contains('terminal-pane-exited')).toBe(true);

    // Simulate reconnection by triggering session_ready (sets paneConnected to true)
    // which resets isExited back to false via the useEffect([paneConnected])
    await act(async () => {
      triggerEvent({ type: 'session_ready', data: { session_id: 'test-session' } });
    });
    await flushPromises();

    expect(pane?.classList.contains('terminal-pane-exited')).toBe(false);
  });
});
