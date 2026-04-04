import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import GitHistoryContextMenu from './GitHistoryContextMenu';
import { copyToClipboard } from '../utils/clipboard';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock requestAnimationFrame so close-listener effect fires synchronously.
// jest does not auto-flush rAF; without this, close listeners never attach.
let rafId = 0;
beforeAll(() => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  global.requestAnimationFrame = ((cb: FrameRequestCallback) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = jest.fn();
});

jest.mock('../utils/clipboard', () => ({
  copyToClipboard: jest.fn().mockResolvedValue(undefined),
}));

Object.defineProperty(navigator, 'clipboard', {
  value: {
    writeText: jest.fn().mockResolvedValue(undefined),
    readText: jest.fn().mockResolvedValue(''),
  },
  writable: true,
  configurable: true,
});

const mockApiService = {
  checkoutGitCommit: jest.fn().mockResolvedValue({ message: 'success', commit: 'abc123' }),
  revertGitCommit: jest.fn().mockResolvedValue({ message: 'success', commit: 'abc123' }),
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let mountPoint: HTMLDivElement | null = null;
let commitRow: HTMLButtonElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeAll(() => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  jest.clearAllMocks();
  mockApiService.checkoutGitCommit.mockClear();
  mockApiService.revertGitCommit.mockClear();

  // The commit row lives directly on document.body (as a sibling to the React
  // mount point). This mirrors how commit rows exist outside the React tree.
  commitRow = document.createElement('button');
  commitRow.className = 'git-history-commit-row';
  commitRow.setAttribute('data-commit-hash', 'abcdef1234567890');
  commitRow.setAttribute('data-commit-short-hash', 'abcdef1');
  commitRow.setAttribute('data-commit-message', 'Fix: resolve the issue\n\nDetailed description');
  document.body.appendChild(commitRow);

  // React mount point — separate from the commit row container so React
  // rendering doesn't wipe out the DOM nodes the component listens to.
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);

  act(() => {
    root = createRoot(mountPoint!);
    root.render(<GitHistoryContextMenu apiService={mockApiService as any} />);
  });
});

afterEach(() => {
  act(() => {
    if (root) {
      root.unmount();
      root = null;
    }
  });
  if (mountPoint) {
    document.body.removeChild(mountPoint);
    mountPoint = null;
  }
  if (commitRow) {
    document.body.removeChild(commitRow);
    commitRow = null;
  }
  document.querySelectorAll('.context-menu').forEach((el) => el.remove());
});

function getMenu(): Element | null {
  return document.querySelector('.context-menu');
}

function getMenuItems(): Element[] {
  const menu = getMenu();
  return menu ? Array.from(menu.querySelectorAll('.context-menu-item')) : [];
}

function getMenuTexts(): string[] {
  return getMenuItems()
    .map((el) => el.textContent?.trim())
    .filter((t): t is string => Boolean(t));
}

/** Dispatch a contextmenu MouseEvent on `target` inside act(). */
function fireContextMenu(target: Element, x = 200, y = 200) {
  act(() => {
    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: x,
      clientY: y,
    });
    target.dispatchEvent(event);
  });
}

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('GitHistoryContextMenu', () => {
  // 1. Does NOT render menu initially (visible: false)
  test('does not render menu initially', () => {
    expect(getMenu()).toBeNull();
  });

  // 2. Shows menu on right-click on a .git-history-commit-row with 4 items
  test('shows menu on right-click with 4 menu items', () => {
    fireContextMenu(commitRow!);

    expect(getMenu()).not.toBeNull();
    expect(getMenuItems()).toHaveLength(4);
    expect(getMenuTexts()).toEqual(
      expect.arrayContaining([
        expect.stringContaining('Copy commit SHA'),
        expect.stringContaining('Copy commit message'),
        expect.stringContaining('Checkout'),
        expect.stringContaining('Revert'),
      ]),
    );
  });

  // 3. Does NOT show menu on right-click outside a commit row
  test('does not show menu on right-click outside a commit row', () => {
    fireContextMenu(mountPoint!);

    expect(getMenu()).toBeNull();
  });

  // 4. "Copy commit SHA" copies full hash and shows 'Copied!' feedback
  test('"Copy commit SHA" copies full hash and shows Copied! feedback', async () => {
    fireContextMenu(commitRow!);

    const copyShaBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Copy commit SHA'));
    expect(copyShaBtn).toBeDefined();

    await act(async () => {
      (copyShaBtn as HTMLElement).click();
      await flushPromises();
    });

    expect(copyToClipboard).toHaveBeenCalledWith('abcdef1234567890');

    // Button label should now show 'Copied!'
    const updatedTexts = getMenuTexts();
    expect(updatedTexts).toEqual(expect.arrayContaining([expect.stringContaining('Copied!')]));
  });

  // 5. "Copy commit message" copies full message and shows 'Copied!' feedback
  test('"Copy commit message" copies full message and shows Copied! feedback', async () => {
    fireContextMenu(commitRow!);

    const copyMsgBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Copy commit message'));
    expect(copyMsgBtn).toBeDefined();

    await act(async () => {
      (copyMsgBtn as HTMLElement).click();
      await flushPromises();
    });

    expect(copyToClipboard).toHaveBeenCalledWith('Fix: resolve the issue\n\nDetailed description');

    // Button label should now show 'Copied!'
    const updatedTexts = getMenuTexts();
    expect(updatedTexts).toEqual(expect.arrayContaining([expect.stringContaining('Copied!')]));
  });

  // 6. Menu closes on Escape key
  test('menu closes on Escape key', () => {
    fireContextMenu(commitRow!);
    expect(getMenu()).not.toBeNull();

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(getMenu()).toBeNull();
  });

  // 7. Menu closes when clicking outside the menu
  test('menu closes when clicking outside the menu', () => {
    fireContextMenu(commitRow!);
    expect(getMenu()).not.toBeNull();

    act(() => {
      document.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(getMenu()).toBeNull();
  });

  // 8. Menu closes when scrolling
  test('menu closes when scrolling', () => {
    fireContextMenu(commitRow!);
    expect(getMenu()).not.toBeNull();

    act(() => {
      window.dispatchEvent(new Event('scroll'));
    });

    expect(getMenu()).toBeNull();
  });

  // 9. Menu closes when window loses focus (blur)
  test('menu closes when window loses focus', () => {
    fireContextMenu(commitRow!);
    expect(getMenu()).not.toBeNull();

    act(() => {
      window.dispatchEvent(new Event('blur'));
    });

    expect(getMenu()).toBeNull();
  });

  // 10. Viewport boundary clamping - menu stays on screen when near edges
  test('viewport boundary clamping keeps menu on screen', () => {
    const originalWidth = window.innerWidth;
    const originalHeight = window.innerHeight;

    Object.defineProperty(window, 'innerWidth', { value: 400, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: 300, configurable: true });

    fireContextMenu(commitRow!, 395, 295);

    const menu = getMenu();
    expect(menu).not.toBeNull();

    const rect = menu!.getBoundingClientRect();
    // The menu should be clamped so it doesn't overflow the viewport (pad = 8)
    expect(rect.right).toBeLessThanOrEqual(400 + 8);
    expect(rect.bottom).toBeLessThanOrEqual(300 + 8);

    Object.defineProperty(window, 'innerWidth', { value: originalWidth, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: originalHeight, configurable: true });
  });

  // 11. Timer cleanup - no state updates leak after unmount
  test('timer cleanup on unmount prevents state updates', async () => {
    const consoleErrorCalls: string[] = [];
    const origError = console.error;
    console.error = (...args: any[]) => {
      consoleErrorCalls.push(args[0]);
      origError(...args);
    };

    fireContextMenu(commitRow!);

    const copyShaBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Copy commit SHA'))!;
    expect(copyShaBtn).toBeDefined();

    // Click copy — schedules a "Copied!" label timer (1200ms) and a close timer (800ms)
    await act(async () => {
      (copyShaBtn as HTMLElement).click();
      await flushPromises();
    });

    // Unmount immediately while timers are still pending.
    act(() => {
      if (root) {
        root.unmount();
        root = null;
      }
    });

    // Yield to let any pending microtasks/macrotasks run
    await flushPromises();

    // No React warnings about updating an unmounted component
    const hasUnmountWarning = consoleErrorCalls.some(
      (msg) => typeof msg === 'string' && msg.includes("Can't perform a React state update on an unmounted component"),
    );
    expect(hasUnmountWarning).toBe(false);

    console.error = origError;
  });

  // 12. Does not show menu when commit row has no data-commit-hash
  test('does not show menu when commit row has no hash', () => {
    const rowWithoutHash = document.createElement('button');
    rowWithoutHash.className = 'git-history-commit-row';
    // No data-commit-hash attribute
    document.body.appendChild(rowWithoutHash);

    fireContextMenu(rowWithoutHash);

    expect(getMenu()).toBeNull();
    document.body.removeChild(rowWithoutHash);
  });

  // 13. Checkout calls apiService.checkoutGitCommit with full hash
  test('checkout calls apiService.checkoutGitCommit and shows Checked out feedback', async () => {
    jest.spyOn(window, 'confirm').mockReturnValue(true);

    fireContextMenu(commitRow!);

    const checkoutBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Checkout'));
    expect(checkoutBtn).toBeDefined();

    await act(async () => {
      (checkoutBtn as HTMLElement).click();
      await flushPromises();
    });

    expect(mockApiService.checkoutGitCommit).toHaveBeenCalledWith('abcdef1234567890');

    const updatedTexts = getMenuTexts();
    expect(updatedTexts).toEqual(expect.arrayContaining([expect.stringContaining('Checked out')]));

    (window.confirm as jest.Mock).mockRestore();
  });

  // 14. Revert calls apiService.revertGitCommit with full hash
  test('revert calls apiService.revertGitCommit and shows Reverted feedback', async () => {
    jest.spyOn(window, 'confirm').mockReturnValue(true);

    fireContextMenu(commitRow!);

    const revertBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Revert'));
    expect(revertBtn).toBeDefined();

    await act(async () => {
      (revertBtn as HTMLElement).click();
      await flushPromises();
    });

    expect(mockApiService.revertGitCommit).toHaveBeenCalledWith('abcdef1234567890');

    const updatedTexts = getMenuTexts();
    expect(updatedTexts).toEqual(expect.arrayContaining([expect.stringContaining('Reverted')]));

    (window.confirm as jest.Mock).mockRestore();
  });

  // 15. Checkout cancelled (user clicks Cancel) closes menu without calling API
  test('checkout cancelled closes menu without calling API', async () => {
    jest.spyOn(window, 'confirm').mockReturnValue(false);

    fireContextMenu(commitRow!);

    const checkoutBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Checkout'));
    expect(checkoutBtn).toBeDefined();

    act(() => {
      (checkoutBtn as HTMLElement).click();
    });

    expect(mockApiService.checkoutGitCommit).not.toHaveBeenCalled();

    (window.confirm as jest.Mock).mockRestore();
  });

  // 16. Revert cancelled (user clicks Cancel) closes menu without calling API
  test('revert cancelled closes menu without calling API', async () => {
    jest.spyOn(window, 'confirm').mockReturnValue(false);

    fireContextMenu(commitRow!);

    const revertBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Revert'));
    expect(revertBtn).toBeDefined();

    act(() => {
      (revertBtn as HTMLElement).click();
    });

    expect(mockApiService.revertGitCommit).not.toHaveBeenCalled();

    (window.confirm as jest.Mock).mockRestore();
  });

  // 17. Checkout error shows error message in actionStatus
  test('checkout error shows error message in actionStatus', async () => {
    jest.spyOn(window, 'confirm').mockReturnValue(true);
    mockApiService.checkoutGitCommit.mockRejectedValueOnce(new Error('merge conflict'));

    fireContextMenu(commitRow!);

    const checkoutBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Checkout'));
    expect(checkoutBtn).toBeDefined();

    await act(async () => {
      (checkoutBtn as HTMLElement).click();
      await flushPromises();
    });

    const statusEl = document.querySelector('.git-history-context-menu-status');
    expect(statusEl).not.toBeNull();
    expect(statusEl!.textContent).toContain('merge conflict');

    (window.confirm as jest.Mock).mockRestore();
  });

  // 18. Revert error shows error message in actionStatus
  test('revert error shows error message in actionStatus', async () => {
    jest.spyOn(window, 'confirm').mockReturnValue(true);
    mockApiService.revertGitCommit.mockRejectedValueOnce(new Error('merge conflict'));

    fireContextMenu(commitRow!);

    const revertBtn = getMenuItems().find((el) => el.textContent?.trim().includes('Revert'));
    expect(revertBtn).toBeDefined();

    await act(async () => {
      (revertBtn as HTMLElement).click();
      await flushPromises();
    });

    const statusEl = document.querySelector('.git-history-context-menu-status');
    expect(statusEl).not.toBeNull();
    expect(statusEl!.textContent).toContain('merge conflict');

    (window.confirm as jest.Mock).mockRestore();
  });
});
