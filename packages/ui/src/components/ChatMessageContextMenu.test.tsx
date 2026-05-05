// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import ChatMessageContextMenu from './ChatMessageContextMenu';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

// Capture originals so we can restore them in afterAll
const originalRAF = globalThis.requestAnimationFrame;
const originalCAF = globalThis.cancelAnimationFrame;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // Mock requestAnimationFrame to run synchronously so ContextMenu's event
  // listeners are attached immediately after render (no async delay).
  (globalThis as any).requestAnimationFrame = (cb: FrameRequestCallback) => cb(0);
  (globalThis as any).cancelAnimationFrame = () => {};
});

afterAll(() => {
  globalThis.requestAnimationFrame = originalRAF;
  globalThis.cancelAnimationFrame = originalCAF;
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: ChatMessageContextMenu
// ---------------------------------------------------------------------------

describe('ChatMessageContextMenu', () => {
  let containerRef: { current: HTMLDivElement | null };

  beforeEach(() => {
    containerRef = { current: null };
    containerRef.current = document.createElement('div');
    document.body.appendChild(containerRef.current);
  });

  afterEach(() => {
    if (containerRef.current) {
      containerRef.current.remove();
    }
  });

  it('renders without crashing when no contextmenu is active', () => {
    const onInsertAtCursor = jest.fn();

    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    // No context menu should be visible initially
    const menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();
  });

  it('shows context menu when right-clicking on a message bubble', () => {
    const onInsertAtCursor = jest.fn();

    // Set up a message bubble inside the container
    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Hello world');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    // Simulate context menu event on the bubble
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
  });

  it('does not show context menu when clicking outside container', () => {
    const onInsertAtCursor = jest.fn();

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    // Fire context menu on an element outside the container
    const outside = document.createElement('div');
    document.body.appendChild(outside);

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      outside.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();
  });

  it('shows Copy message button in the menu', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Hello world');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    // Should have a Copy message button
    const buttons = menu?.querySelectorAll('.context-menu-item');
    expect(buttons).not.toBeNull();
    expect(buttons?.length).toBeGreaterThanOrEqual(1);
  });

  it('shows Copy code block button when right-clicking inside a <pre>', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Here is code');
    const pre = document.createElement('pre');
    const code = document.createElement('code');
    code.textContent = 'console.log("hello")';
    pre.appendChild(code);
    bubble.appendChild(pre);
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      code.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    // Should have Copy code block button
    const labels = menu?.querySelectorAll('.menu-item-label');
    const labelTexts = Array.from(labels!).map((l) => l.textContent);
    expect(labelTexts).toContain('Copy message');
    expect(labelTexts).toContain('Copy code block');
  });

  it('does not show Copy code block button when right-clicking outside <pre>', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Just text');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    const labels = menu?.querySelectorAll('.menu-item-label');
    const labelTexts = Array.from(labels!).map((l) => l.textContent);
    expect(labelTexts).toContain('Copy message');
    expect(labelTexts).not.toContain('Copy code block');
  });

  it('shows Insert at cursor button in the menu', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Insert me');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    const labels = menu?.querySelectorAll('.menu-item-label');
    const labelTexts = Array.from(labels!).map((l) => l.textContent);
    expect(labelTexts).toContain('Insert at cursor');
  });

  it('calls onInsertAtCursor when Insert at cursor is clicked', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Insert text');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    // Find and click the "Insert at cursor" button
    const menu = document.querySelector('.context-menu');
    const labels = menu?.querySelectorAll('.menu-item-label');
    let insertBtn: HTMLButtonElement | null = null;
    for (const label of labels!) {
      if (label.textContent === 'Insert at cursor') {
        insertBtn = label.closest('button') as HTMLButtonElement;
        break;
      }
    }

    act(() => {
      insertBtn?.click();
    });

    expect(onInsertAtCursor).toHaveBeenCalledWith('Insert text');
  });

  it('does not show menu when text is selected (let native menu handle)', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Hello');
    containerRef.current!.appendChild(bubble);

    // Mock getSelection to return selected text
    const originalGetSelection = window.getSelection;
    // @ts-expect-error — mock getSelection
    window.getSelection = () => ({ toString: () => 'selected text', trim: () => 'selected text' });

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    // Menu should not appear because text is selected
    const menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();

    // @ts-expect-error — restore
    window.getSelection = originalGetSelection;
  });

  it('closes menu on outside click', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Hello');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    // Open menu
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    let menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();

    // Click outside
    act(() => {
      const outside = document.createElement('div');
      document.body.appendChild(outside);
      const event = new MouseEvent('mousedown', { bubbles: true });
      outside.dispatchEvent(event);
    });

    menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();
  });

  it('closes menu on Escape key', () => {
    const onInsertAtCursor = jest.fn();

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Hello');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    // Open menu
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    let menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();

    // Press Escape
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
      document.dispatchEvent(event);
    });

    menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();
  });

  it('copies message content when Copy message is clicked', async () => {
    const onInsertAtCursor = jest.fn();

    // Mock clipboard API
    Object.assign(navigator, {
      clipboard: {
        writeText: jest.fn().mockResolvedValue(undefined),
      },
    });

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Copy this text');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    // Find and click the "Copy message" button
    const menu = document.querySelector('.context-menu');
    const labels = menu?.querySelectorAll('.menu-item-label');
    let copyBtn: HTMLButtonElement | null = null;
    for (const label of labels!) {
      if (label.textContent === 'Copy message') {
        copyBtn = label.closest('button') as HTMLButtonElement;
        break;
      }
    }

    // The click handler is async (await copyToClipboard), so use async act
    // to wait for the state update before asserting.
    await act(async () => {
      copyBtn?.click();
    });

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('Copy this text');
  });

  it('copies code block when Copy code block is clicked', async () => {
    const onInsertAtCursor = jest.fn();

    Object.assign(navigator, {
      clipboard: {
        writeText: jest.fn().mockResolvedValue(undefined),
      },
    });

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Message with code');
    const pre = document.createElement('pre');
    const code = document.createElement('code');
    code.textContent = 'function hello() {}';
    pre.appendChild(code);
    bubble.appendChild(pre);
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      code.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    const labels = menu?.querySelectorAll('.menu-item-label');
    let copyCodeBtn: HTMLButtonElement | null = null;
    for (const label of labels!) {
      if (label.textContent === 'Copy code block') {
        copyCodeBtn = label.closest('button') as HTMLButtonElement;
        break;
      }
    }

        // The click handler is async (await copyToClipboard), so use async act
    // to wait for the state update before asserting.
    await act(async () => {
      copyCodeBtn?.click();
    });

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('function hello() {}');
  });

  it('shows "Copied!" text after copying message', async () => {
    const onInsertAtCursor = jest.fn();

    Object.assign(navigator, {
      clipboard: {
        writeText: jest.fn().mockResolvedValue(undefined),
      },
    });

    const bubble = document.createElement('div');
    bubble.setAttribute('data-message-content', 'Hello');
    containerRef.current!.appendChild(bubble);

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      bubble.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    const labels = menu?.querySelectorAll('.menu-item-label');
    let copyBtn: HTMLButtonElement | null = null;
    for (const label of labels!) {
      if (label.textContent === 'Copy message') {
        copyBtn = label.closest('button') as HTMLButtonElement;
        break;
      }
    }

    // The click handler is async (await copyToClipboard), so we must
    // wrap in an async act to capture the state update before asserting.
    await act(async () => {
      copyBtn?.click();
    });

    // After copy, the label should briefly change to "Copied!"
    const menuAfter = document.querySelector('.context-menu');
    const labelsAfter = menuAfter?.querySelectorAll('.menu-item-label');
    const labelTexts = Array.from(labelsAfter!).map((l) => l.textContent);
    expect(labelTexts).toContain('Copied!');
  });

  it('does not show menu when containerRef is null', () => {
    const onInsertAtCursor = jest.fn();
    const nullRef = { current: null };

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef: nullRef,
          onInsertAtCursor,
        })
      );
    });

    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      clientX: 100,
      clientY: 200,
    });
    document.dispatchEvent(event);

    const menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();
  });

  it('does not show menu when target has no data-message-content ancestor', () => {
    const onInsertAtCursor = jest.fn();

    act(() => {
      root.render(
        createElement(ChatMessageContextMenu, {
          containerRef,
          onInsertAtCursor,
        })
      );
    });

    // Right-click on container itself (no bubble)
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      });
      containerRef.current!.dispatchEvent(event);
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();
  });
});
