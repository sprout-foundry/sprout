// @vitest-environment jsdom
/**
 * MockupPanel — the mockup in the preview slot.
 *
 * What is pinned: the document is read from `design/mockup.html` and rendered
 * in a sandboxed iframe, an absent file renders the empty state (not an
 * error), a failed read leaves the last good document on screen, and window
 * focus re-reads it so a change drawn in the chat appears without a reload.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import MockupPanel, { MOCKUP_PATH } from './MockupPanel';

const fetchMock = vi.fn();

vi.mock('../contexts/SproutAdapterContext', () => ({
  useSproutFetch: () => fetchMock,
}));

let container: HTMLDivElement;
let root: Root;

function ok(body: string): Response {
  return { ok: true, text: async () => body } as Response;
}

function notFound(): Response {
  return { ok: false, status: 404, text: async () => '' } as Response;
}

async function render(): Promise<void> {
  await act(async () => {
    root.render(createElement(MockupPanel, { onClose: vi.fn() }));
  });
}

beforeEach(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  fetchMock.mockReset();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('MockupPanel', () => {
  it('reads the mockup from its fixed path and renders it in a sandboxed frame', async () => {
    fetchMock.mockResolvedValue(ok('<html><body>Home</body></html>'));
    await render();

    expect(fetchMock).toHaveBeenCalledWith(`/api/file?path=${encodeURIComponent(MOCKUP_PATH)}`);
    const frame = container.querySelector('[data-testid="mockup-panel-frame"]');
    expect(frame).not.toBeNull();
    expect(frame?.getAttribute('srcdoc')).toContain('Home');
    // The document's own scripts run against its own assets.
    expect(frame?.getAttribute('sandbox')).toBe('allow-scripts allow-same-origin');
  });

  it('renders the empty state when no mockup exists yet', async () => {
    fetchMock.mockResolvedValue(notFound());
    await render();

    expect(container.querySelector('[data-testid="mockup-panel-empty"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="mockup-panel-frame"]')).toBeNull();
  });

  it('keeps the last good document when a re-read fails', async () => {
    fetchMock.mockResolvedValueOnce(ok('<html><body>Home</body></html>'));
    await render();
    expect(container.querySelector('[data-testid="mockup-panel-frame"]')).not.toBeNull();

    fetchMock.mockRejectedValueOnce(new Error('offline'));
    await act(async () => {
      window.dispatchEvent(new Event('focus'));
    });

    const frame = container.querySelector('[data-testid="mockup-panel-frame"]');
    expect(frame?.getAttribute('srcdoc')).toContain('Home');
  });

  it('re-reads on window focus so a change drawn in the chat appears', async () => {
    fetchMock.mockResolvedValueOnce(ok('<html><body>Home</body></html>'));
    await render();

    fetchMock.mockResolvedValueOnce(ok('<html><body>Checkout</body></html>'));
    await act(async () => {
      window.dispatchEvent(new Event('focus'));
    });

    const frame = container.querySelector('[data-testid="mockup-panel-frame"]');
    expect(frame?.getAttribute('srcdoc')).toContain('Checkout');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('closes through the pane affordance', async () => {
    const onClose = vi.fn();
    fetchMock.mockResolvedValue(ok('<html></html>'));
    await act(async () => {
      root.render(createElement(MockupPanel, { onClose }));
    });

    act(() => {
      (container.querySelector('[data-testid="mockup-panel-close"]') as HTMLElement).click();
    });
    expect(onClose).toHaveBeenCalled();
  });
});
