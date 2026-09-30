import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./NotificationHistoryPanel', () => ({
  default: ({ onClose }: { onClose: () => void }) => (
    <div data-testid="history">
      <button type="button" onClick={onClose}>
        close
      </button>
    </div>
  ),
}));

import { OPEN_NOTIFICATIONS_EVENT } from '../config/layout';
import NotificationCenterHost from './NotificationCenterHost';

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

describe('NotificationCenterHost', () => {
  it('opens the history when anything asks for it, and closes it', () => {
    act(() => root.render(<NotificationCenterHost />));
    expect(container.querySelector('[data-testid="history"]')).toBeNull();

    act(() => {
      window.dispatchEvent(new CustomEvent(OPEN_NOTIFICATIONS_EVENT, { detail: { anchor: document.body } }));
    });
    expect(container.querySelector('[data-testid="history"]')).not.toBeNull();

    act(() => (container.querySelector('button') as HTMLButtonElement).click());
    expect(container.querySelector('[data-testid="history"]')).toBeNull();
  });
});
