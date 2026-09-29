import { NotificationProvider, notificationBus } from '@sprout/ui';
import { act, createRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { useNotifications } from '../contexts/NotificationContext';
import NotificationHistoryPanel, { formatNotificationAge } from './NotificationHistoryPanel';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function UnreadProbe(): JSX.Element {
  const { notifications } = useNotifications();
  return <span data-testid="unread">{notifications.filter((n) => !n.read).length}</span>;
}

function renderPanel(onClose = vi.fn()) {
  const anchor = createRef<HTMLButtonElement>();
  act(() => {
    root.render(
      <NotificationProvider>
        <button ref={anchor}>bell</button>
        <UnreadProbe />
        <NotificationHistoryPanel anchorRef={anchor} onClose={onClose} />
      </NotificationProvider>,
    );
  });
  return onClose;
}

describe('NotificationHistoryPanel', () => {
  it('shows an empty state', () => {
    renderPanel();
    expect(document.body.textContent).toContain('No notifications yet.');
  });

  it('lists notifications newest first and marks them read', () => {
    renderPanel();
    act(() => {
      notificationBus.notify('info', 'First', 'one');
      notificationBus.notify('error', 'Second', 'two');
    });
    const titles = Array.from(document.querySelectorAll('.notification-history-item-title')).map(
      (el) => el.textContent,
    );
    expect(titles).toEqual(['Second', 'First']);
  });

  it('marks existing notifications read when opened', () => {
    const anchor = createRef<HTMLButtonElement>();
    function Harness({ open }: { open: boolean }): JSX.Element {
      return (
        <>
          <button ref={anchor}>bell</button>
          <UnreadProbe />
          {open && <NotificationHistoryPanel anchorRef={anchor} onClose={() => {}} />}
        </>
      );
    }
    act(() => {
      root.render(
        <NotificationProvider>
          <Harness open={false} />
        </NotificationProvider>,
      );
    });
    act(() => {
      notificationBus.notify('warning', 'Heads up', 'msg');
    });
    expect(container.querySelector('[data-testid="unread"]')!.textContent).toBe('1');
    act(() => {
      root.render(
        <NotificationProvider>
          <Harness open />
        </NotificationProvider>,
      );
    });
    expect(container.querySelector('[data-testid="unread"]')!.textContent).toBe('0');
  });

  it('closes on Escape and on outside pointerdown', () => {
    const onClose = renderPanel();
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    act(() => {
      document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('drops under the top bar on a phone instead of beside its anchor', () => {
    const width = window.innerWidth;
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 });
    try {
      renderPanel();
      const panel = document.querySelector('.notification-history') as HTMLElement;
      expect(panel.style.left).toBe('8px');
      expect(panel.style.top).toBe('56px');
    } finally {
      Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
    }
  });

  it('clears all notifications', () => {
    renderPanel();
    act(() => {
      notificationBus.notify('info', 'Only', 'x');
    });
    act(() => {
      (document.querySelector('.notification-history-clear') as HTMLButtonElement).click();
    });
    expect(document.querySelectorAll('.notification-history-item')).toHaveLength(0);
  });
});

describe('formatNotificationAge', () => {
  it('formats recent ages', () => {
    const now = 1_000_000_000;
    expect(formatNotificationAge(now - 5_000, now)).toBe('just now');
    expect(formatNotificationAge(now - 5 * 60_000, now)).toBe('5m ago');
    expect(formatNotificationAge(now - 3 * 3_600_000, now)).toBe('3h ago');
  });
});
