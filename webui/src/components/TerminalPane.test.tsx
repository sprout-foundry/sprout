// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import TerminalPane from './TerminalPane';

// ---------------------------------------------------------------------------
// Mock XTerm instance (mutable, shared across tests)
// ---------------------------------------------------------------------------

const mockFitAddon = {
  fit: jest.fn(),
};

const mockTerm = {
  hasSelection: jest.fn().mockReturnValue(false),
  getSelection: jest.fn().mockReturnValue(''),
  selectAll: jest.fn(),
  clear: jest.fn(),
  loadAddon: jest.fn(),
  open: jest.fn(),
  onData: jest.fn(),
  focus: jest.fn(),
  dispose: jest.fn(),
  cols: 80,
  rows: 24,
  buffer: {
    active: {
      baseY: 0,
      getLine: jest.fn().mockReturnValue(null),
    },
  },
  options: {},
  core: { buffer: { x: 0 } },
};

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('@xterm/xterm', () => ({
  Terminal: jest.fn(() => mockTerm),
}));

jest.mock('@xterm/addon-fit', () => ({
  FitAddon: jest.fn(() => mockFitAddon),
}));

const mockService = {
  sendRawInput: jest.fn(),
  sendResize: jest.fn(),
  onEvent: jest.fn(),
  removeEvent: jest.fn(),
  disconnect: jest.fn(),
  connect: jest.fn(),
};

jest.mock('../services/terminalWebSocket', () => ({
  TerminalWebSocketService: {
    createInstance: jest.fn(() => mockService),
  },
}));

jest.mock('../contexts/ThemeContext', () => ({
  useTheme: jest.fn(),
}));

jest.mock('../utils/clipboard', () => {
  return {
    copyToClipboard: jest.fn().mockResolvedValue(undefined),
  };
});

// ---------------------------------------------------------------------------
// Clipboard mock (jsdom doesn't provide navigator.clipboard)
// ---------------------------------------------------------------------------

Object.defineProperty(navigator, 'clipboard', {
  value: {
    writeText: jest.fn().mockResolvedValue(undefined),
    readText: jest.fn().mockResolvedValue(''),
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
      getCell: jest.fn().mockImplementation((col: number) => ({
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

describe('TerminalPane context menu', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    // Re-assert Terminal constructor mock (jest.mock hoisted + clearAllMocks in afterEach)
    const { Terminal } = require('@xterm/xterm');
    Terminal.mockImplementation(() => mockTerm);

    // Re-assert FitAddon constructor mock so it always returns mockFitAddon
    const { FitAddon } = require('@xterm/addon-fit');
    FitAddon.mockImplementation(() => mockFitAddon);

    // Re-assert WebSocket mock service factory
    const { TerminalWebSocketService } = require('../services/terminalWebSocket');
    TerminalWebSocketService.createInstance.mockImplementation(() => mockService);

    // Re-assert clipboard mock
    const { copyToClipboard } = require('../utils/clipboard');
    copyToClipboard.mockResolvedValue(undefined);

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

    // Theme context
    const { useTheme } = require('../contexts/ThemeContext');
    useTheme.mockReturnValue({ themePack: { id: 'default' } });
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    // Clean up any leftover portal elements
    document.querySelectorAll('.context-menu').forEach((el) => el.remove());
    jest.clearAllMocks();
  });

  it('context menu appears on right-click in the terminal pane content', async () => {
    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
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

    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
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
    const copyBtn = getMenuItems().find((item) =>
      item.textContent?.includes('Copy')
    );
    expect(copyBtn?.classList.contains('disabled')).toBe(false);
    expect((copyBtn as HTMLButtonElement)?.disabled).toBe(false);
  });

  it('Copy is disabled when no selection', async () => {
    mockTerm.hasSelection.mockReturnValue(false);

    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const copyBtn = getMenuItems().find((item) =>
      item.textContent?.includes('Copy')
    );
    expect(copyBtn).toBeTruthy();
    expect(copyBtn?.classList.contains('disabled')).toBe(true);
    expect((copyBtn as HTMLButtonElement)?.disabled).toBe(true);
  });

  it('Copy Link appears when URL is detected under cursor', async () => {
    // Place URL at column 10 of line 0, length 50
    const url = 'https://example.com/path';
    const urlStartCol = 10;
    setupMockLineWithUrl(url, urlStartCol, 50);

    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    // The handler uses xtermContainerRef.current (i.e. .terminal-xterm div)
    // for getBoundingClientRect, not .terminal-pane-content.
    const xtermContainer = container.querySelector('.terminal-xterm');
    expect(xtermContainer).toBeTruthy();

    // Spy getBoundingClientRect since jsdom returns all zeros for layout
    const rect = {
      left: 0, top: 0, width: 800, height: 480,
      right: 800, bottom: 480, x: 0, y: 0,
      toJSON: () => ({}),
    } as DOMRect;
    jest.spyOn(xtermContainer as HTMLElement, 'getBoundingClientRect').mockReturnValue(rect);

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
    const { copyToClipboard } = require('../utils/clipboard');
    mockTerm.hasSelection.mockReturnValue(true);
    mockTerm.getSelection.mockReturnValue('selected text');

    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const copyBtn = getMenuItems().find((item) =>
      item.textContent?.includes('Copy')
    );
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

    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={true} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const pasteBtn = getMenuItems().find((item) =>
      item.textContent?.includes('Paste')
    );
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
    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const selectAllBtn = getMenuItems().find((item) =>
      item.textContent?.includes('Select All')
    );
    expect(selectAllBtn).toBeTruthy();

    await act(async () => {
      selectAllBtn.click();
    });
    await flushPromises();

    expect(mockTerm.selectAll).toHaveBeenCalled();
  });

  it('Clear Terminal action calls term.clear()', async () => {
    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    const clearBtn = getMenuItems().find((item) =>
      item.textContent?.includes('Clear Terminal')
    );
    expect(clearBtn).toBeTruthy();

    await act(async () => {
      clearBtn.click();
    });
    await flushPromises();

    expect(mockTerm.clear).toHaveBeenCalled();
  });

  it('context menu closes on Escape key', async () => {
    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();
    await flushRAF();

    expect(getMenu()).toBeTruthy();

    await act(async () => {
      document.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
      );
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('context menu closes on click outside', async () => {
    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();
    await flushRAF();

    expect(getMenu()).toBeTruthy();

    // Click outside the menu (on the body, not the menu itself)
    await act(async () => {
      document.body.dispatchEvent(
        new MouseEvent('mousedown', { bubbles: true })
      );
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('Copy Link does NOT appear when no URL is under cursor', async () => {
    // Ensure no URL mock is set — getLine returns null by default
    mockTerm.buffer.active.getLine.mockReturnValue(null);

    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const xtermContainer = container.querySelector('.terminal-xterm');
    const rect = {
      left: 0, top: 0, width: 800, height: 480,
      right: 800, bottom: 480, x: 0, y: 0,
      toJSON: () => ({}),
    } as DOMRect;
    jest.spyOn(xtermContainer as HTMLElement, 'getBoundingClientRect').mockReturnValue(rect);

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent, 155, 10);
    await flushPromises();

    const texts = getMenuTexts();
    expect(texts).not.toContain('Copy Link');
  });

  it('context menu closes on scroll', async () => {
    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
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
    await act(async () => {
      root.render(
        <TerminalPane isActive={true} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    const paneContent = container.querySelector('.terminal-pane-content');
    fireContextMenu(paneContent);
    await flushPromises();

    expect(getMenu()).toBeTruthy();

    // Deactivate the pane
    await act(async () => {
      root.render(
        <TerminalPane isActive={false} isConnected={false} showCloseButton={false} />
      );
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });
});
