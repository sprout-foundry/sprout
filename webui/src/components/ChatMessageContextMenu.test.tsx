import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import ChatMessageContextMenu from './ChatMessageContextMenu';
import { copyToClipboard } from '../utils/clipboard';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock requestAnimationFrame so close-listener effect fires synchronously.
// jest does not auto-flush rAF; without this, close listeners never attach.
let rafId = 0;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let mountPoint: HTMLDivElement | null = null;
let chatContainer: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;
const onInsertAtCursor = jest.fn();

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  jest.clearAllMocks();
  onInsertAtCursor.mockClear();

  // Create a chat container (holds message bubbles) as a sibling to the React mount.
  // ChatMessageContextMenu gets a ref to this container and listens for contextmenu
  // events from it. The menu itself renders inside React's mount point.
  chatContainer = document.createElement('div');
  chatContainer.className = 'chat-container';
  document.body.appendChild(chatContainer);

  // React mount point — separate from the chat container so React rendering
  // doesn't wipe out the message bubble DOM nodes.
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);
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
  if (chatContainer) {
    document.body.removeChild(chatContainer);
    chatContainer = null;
  }
  document.querySelectorAll('.context-menu').forEach((el) => el.remove());
});

/**
 * Adds a message bubble to the chat container and renders the component.
 * Returns the bubble element and the containerRef pointing to the chat container.
 */
function renderWithBubble(
  innerHtml = '<div data-message-content="Hello world">Hello world</div>',
) {
  if (!chatContainer || !mountPoint) throw new Error('setup not run');

  const bubbleWrapper = document.createElement('div');
  bubbleWrapper.innerHTML = innerHtml;
  const bubble = bubbleWrapper.firstElementChild as HTMLElement;
  chatContainer.appendChild(bubble);

  const containerRef = React.createRef<HTMLDivElement>();
  (containerRef as React.MutableRefObject<HTMLDivElement | null>).current = chatContainer;

  act(() => {
    root = createRoot(mountPoint!);
    root.render(
      <ChatMessageContextMenu
        containerRef={containerRef}
        onInsertAtCursor={onInsertAtCursor}
      />,
    );
  });

  return { bubble, containerRef };
}

function getMenu(): Element | null {
  return document.querySelector('.context-menu');
}

function getMenuItems(): Element[] {
  const menu = getMenu();
  return menu ? Array.from(menu.querySelectorAll('.context-menu-item')) : [];
}

function getMenuTexts(): string[] {
  return getMenuItems().map((el) => el.textContent?.trim() ?? '');
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

describe('ChatMessageContextMenu', () => {
  // 1. Does NOT render menu initially (visible: false)
  test('does not render menu initially', () => {
    renderWithBubble();
    expect(getMenu()).toBeNull();
  });

  // 2. Shows menu on right-click inside a message bubble
  test('shows menu on right-click inside a message bubble', () => {
    const { bubble } = renderWithBubble();
    fireContextMenu(bubble);

    expect(getMenu()).not.toBeNull();
    expect(getMenuTexts()).toEqual(
      expect.arrayContaining(['Copy message', 'Insert at cursor']),
    );
  });

  // 3. Does NOT show menu on right-click outside a message bubble
  test('does not show menu on right-click outside a message bubble', () => {
    renderWithBubble();
    fireContextMenu(chatContainer!);

    expect(getMenu()).toBeNull();
  });

  // 4. "Copy message" button copies messageContent from data-message-content
  test('"Copy message" button copies messageContent', async () => {
    const msg = 'Hello world';
    const { bubble } = renderWithBubble(
      `<div data-message-content="${msg}">${msg}</div>`,
    );
    fireContextMenu(bubble);

    const copyBtn = getMenuItems().find(
      (el) => el.textContent?.trim() === 'Copy message',
    );
    expect(copyBtn).toBeDefined();

    await act(async () => {
      (copyBtn as HTMLElement)!.click();
      await flushPromises();
    });

    expect(copyToClipboard).toHaveBeenCalledWith(msg);
  });

  // 5. "Copy code block" button copies code text when right-clicking inside a <pre>
  test('"Copy code block" button copies code text from <pre>', async () => {
    const codeText = 'const x = 42;';
    const { bubble } = renderWithBubble(
      `<div data-message-content="Check this out">
        <pre><code>${codeText}</code></pre>
      </div>`,
    );
    // Right-click on the <code> element inside <pre> inside the bubble
    const codeEl = bubble!.querySelector('code')!;
    fireContextMenu(codeEl);

    const texts = getMenuTexts();
    expect(texts).toContain('Copy code block');

    const copyCodeBtn = getMenuItems().find(
      (el) => el.textContent?.trim() === 'Copy code block',
    );
    expect(copyCodeBtn).toBeDefined();

    await act(async () => {
      (copyCodeBtn as HTMLElement)!.click();
      await flushPromises();
    });

    expect(copyToClipboard).toHaveBeenCalledWith(codeText);
  });

  // 6. "Copy code block" button does NOT appear when NOT inside a <pre>
  test('"Copy code block" button does not appear when not inside a <pre>', () => {
    const { bubble } = renderWithBubble(
      `<div data-message-content="No code here">No code here</div>`,
    );
    fireContextMenu(bubble);

    const texts = getMenuTexts();
    expect(texts).toContain('Copy message');
    expect(texts).toContain('Insert at cursor');
    expect(texts).not.toContain('Copy code block');
  });

  // 7. "Insert at cursor" calls onInsertAtCursor with message content
  test('"Insert at cursor" calls onInsertAtCursor with message content', () => {
    const msg = 'Insert this text';
    const { bubble } = renderWithBubble(
      `<div data-message-content="${msg}">${msg}</div>`,
    );
    fireContextMenu(bubble);

    const insertBtn = getMenuItems().find(
      (el) => el.textContent?.trim() === 'Insert at cursor',
    );
    expect(insertBtn).toBeDefined();

    act(() => {
      (insertBtn as HTMLElement)!.click();
    });

    expect(onInsertAtCursor).toHaveBeenCalledWith(msg);
  });

  // 8. Menu closes on Escape key
  test('menu closes on Escape key', () => {
    const { bubble } = renderWithBubble();
    fireContextMenu(bubble);
    expect(getMenu()).not.toBeNull();

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(getMenu()).toBeNull();
  });

  // 9. Menu closes when clicking outside the menu
  test('menu closes when clicking outside the menu', () => {
    const { bubble } = renderWithBubble();
    fireContextMenu(bubble);
    expect(getMenu()).not.toBeNull();

    act(() => {
      document.dispatchEvent(
        new MouseEvent('mousedown', { bubbles: true }),
      );
    });

    expect(getMenu()).toBeNull();
  });

  // 10. Menu closes when scrolling
  test('menu closes when scrolling', () => {
    const { bubble } = renderWithBubble();
    fireContextMenu(bubble);
    expect(getMenu()).not.toBeNull();

    act(() => {
      window.dispatchEvent(new Event('scroll'));
    });

    expect(getMenu()).toBeNull();
  });

  // 11. Menu closes when window loses focus (blur)
  test('menu closes when window loses focus', () => {
    const { bubble } = renderWithBubble();
    fireContextMenu(bubble);
    expect(getMenu()).not.toBeNull();

    act(() => {
      window.dispatchEvent(new Event('blur'));
    });

    expect(getMenu()).toBeNull();
  });

  // 12. Viewport boundary clamping - menu stays on screen when near edges
  test('viewport boundary clamping keeps menu on screen', () => {
    const originalWidth = window.innerWidth;
    const originalHeight = window.innerHeight;

    Object.defineProperty(window, 'innerWidth', { value: 400, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: 300, configurable: true });

    const { bubble } = renderWithBubble();

    // Fire context menu at the far bottom-right corner
    fireContextMenu(bubble, 395, 295);

    const menu = getMenu();
    expect(menu).not.toBeNull();

    const rect = menu!.getBoundingClientRect();
    // The menu should be clamped so it doesn't overflow the viewport (pad = 8)
    expect(rect.right).toBeLessThanOrEqual(400 + 8);
    expect(rect.bottom).toBeLessThanOrEqual(300 + 8);

    Object.defineProperty(window, 'innerWidth', { value: originalWidth, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: originalHeight, configurable: true });
  });

  // 13. Timer cleanup - no "Copied!" toast state leaks after unmount
  test('timer cleanup on unmount prevents state updates', async () => {
    let consoleErrorCalls: string[] = [];
    const origError = console.error;
    console.error = (...args: any[]) => {
      consoleErrorCalls.push(args[0]);
      origError(...args);
    };

    const { bubble } = renderWithBubble();
    fireContextMenu(bubble);

    const copyBtn = getMenuItems().find(
      (el) => el.textContent?.trim() === 'Copy message',
    )!;
    expect(copyBtn).toBeDefined();

    // Click copy — schedules a "Copied!" label timer (1200ms) and a close timer (800ms)
    await act(async () => {
      (copyBtn as HTMLElement).click();
      await flushPromises();
    });

    // Unmount immediately while timers are still pending.
    // The clearTimers() in the unmount effect should clear all pending timeouts.
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
      (msg) => typeof msg === 'string' && msg.includes('Can\'t perform a React state update on an unmounted component'),
    );
    expect(hasUnmountWarning).toBe(false);

    console.error = origError;
  });
});
