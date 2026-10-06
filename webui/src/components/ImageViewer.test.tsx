// @vitest-environment jsdom

/**
 * ImageViewer pan/zoom: the view must be navigable with the mouse at any zoom
 * and the image must never be draggable off the pane.
 */
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ImageViewer from './ImageViewer';

vi.mock('../services/fileAccess', () => ({
  readFileWithConsent: vi.fn(),
}));

vi.mock('../utils/log', () => ({
  useLog: () => ({ error: vi.fn(), warn: vi.fn(), info: vi.fn() }),
}));

vi.mock('./ViewerToolbar', () => ({
  default: () => null,
}));

import { readFileWithConsent } from '../services/fileAccess';

const CONTAINER_W = 800;
const CONTAINER_H = 600;

function stubLayout() {
  Object.defineProperty(HTMLElement.prototype, 'clientWidth', {
    configurable: true,
    get() {
      return CONTAINER_W;
    },
  });
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get() {
      return CONTAINER_H;
    },
  });
}

/** Stub the global Image so setting `src` fires onload with fixture dims. */
function stubImage(width: number, height: number) {
  class FakeImage {
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;
    private _src = '';
    width = width;
    height = height;
    set src(v: string) {
      this._src = v;
      queueMicrotask(() => this.onload?.());
    }
    get src() {
      return this._src;
    }
  }
  vi.stubGlobal('Image', FakeImage as unknown as typeof Image);
}

/** Read the current translate/scale applied to the image content. */
function readTransform(container: HTMLElement): { x: number; y: number; scale: number } {
  const content = container.querySelector('.image-viewer-content') as HTMLElement;
  const m = /translate\(([-\d.]+)px,\s*([-\d.]+)px\)\s*scale\(([-\d.]+)\)/.exec(content.style.transform || '');
  if (!m) return { x: 0, y: 0, scale: 1 };
  return { x: parseFloat(m[1]), y: parseFloat(m[2]), scale: parseFloat(m[3]) };
}

async function render(imageW: number, imageH: number): Promise<{ root: Root; container: HTMLElement }> {
  stubImage(imageW, imageH);
  (readFileWithConsent as unknown as { mockResolvedValue: (v: unknown) => void }).mockResolvedValue({
    ok: true,
    blob: async () => new Blob(['x'], { type: 'image/png' }),
  });
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  await act(async () => {
    root.render(<ImageViewer filePath="/a.png" fileName="a.png" fileSize={100} />);
  });
  // Let the image onload + fitToWindow settle.
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  const container = host.querySelector('.image-viewer-container') as HTMLElement;
  return { root, container };
}

const drag = (container: HTMLElement, dx: number, dy: number) => {
  act(() => {
    container.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, clientX: 400, clientY: 300, button: 0 }));
  });
  act(() => {
    window.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, clientX: 400 + dx, clientY: 300 + dy }));
  });
  act(() => {
    window.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
  });
};

describe('ImageViewer pan/zoom', () => {
  beforeEach(() => {
    stubLayout();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    document.body.innerHTML = '';
  });

  it('pans by dragging even when the image fits the pane (zoom <= 1)', async () => {
    // A small image fits at scale 1; dragging must still work (the old gate
    // returned early at zoom <= 1, so the image could not be moved at all).
    const { container } = await render(400, 300);
    const before = readTransform(container);
    drag(container, 50, 30);
    const after = readTransform(container);
    expect(after.x).not.toBe(before.x);
    expect(after.y).not.toBe(before.y);
  });

  it('keeps a fitting image fully inside the pane when dragged', async () => {
    // Image 400x300 at scale 1 in an 800x600 pane: it can never be dragged so
    // far that it leaves the pane.
    const { container } = await render(400, 300);
    drag(container, 5000, 5000);
    const t = readTransform(container);
    expect(t.x).toBeGreaterThanOrEqual(0);
    expect(t.x).toBeLessThanOrEqual(CONTAINER_W - 400 * t.scale);
    expect(t.y).toBeGreaterThanOrEqual(0);
    expect(t.y).toBeLessThanOrEqual(CONTAINER_H - 300 * t.scale);
  });

  it('bounds a large (zoomed) image to the pane edges — no gap behind it', async () => {
    // Start fit, then zoom to 100% so the image (1600x1200) is larger than the
    // pane (800x600) and panning must be bounded to its edges.
    const { container } = await render(1600, 1200);
    act(() => {
      container.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, clientX: 400, clientY: 300 }));
    });
    expect(readTransform(container).scale).toBeCloseTo(1, 2);
    // Drag hard left/up: the image should stop at the pane edge (translate >= cw - w).
    drag(container, -100000, -100000);
    const t = readTransform(container);
    expect(t.x).toBeCloseTo(CONTAINER_W - 1600 * t.scale, 0);
    expect(t.y).toBeCloseTo(CONTAINER_H - 1200 * t.scale, 0);
    // And hard right/down stops at 0.
    drag(container, 100000, 100000);
    const t2 = readTransform(container);
    expect(t2.x).toBeCloseTo(0, 0);
    expect(t2.y).toBeCloseTo(0, 0);
  });

  it('ends the drag when the mouse is released outside the pane', async () => {
    const { container } = await render(1600, 1200);
    act(() => {
      container.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, clientX: 400, clientY: 300, button: 0 }));
    });
    act(() => {
      window.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, clientX: 300, clientY: 200 }));
    });
    // Release on window, not the container.
    act(() => {
      window.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
    });
    expect(container.className).not.toContain('dragging');
    // A further move must not pan (drag already ended).
    const t = readTransform(container);
    act(() => {
      window.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, clientX: 100, clientY: 100 }));
    });
    expect(readTransform(container)).toEqual(t);
  });

  it('double-click toggles between fit and 100%', async () => {
    // A 1600x1200 image in an 800x600 pane fits below 100%.
    const { container } = await render(1600, 1200);
    const fit = readTransform(container).scale;
    expect(fit).toBeLessThan(1);
    act(() => {
      container.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, clientX: 400, clientY: 300 }));
    });
    expect(readTransform(container).scale).toBeCloseTo(1, 2);
    act(() => {
      container.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, clientX: 400, clientY: 300 }));
    });
    expect(readTransform(container).scale).toBeCloseTo(fit, 2);
  });

  it('supports bare keyboard zoom keys once the pane has focus', async () => {
    const { container } = await render(1600, 1200);
    // Clicking focuses the pane (keyboard shortcuts depend on it).
    drag(container, 0, 0);
    act(() => {
      container.dispatchEvent(new KeyboardEvent('keydown', { key: '1', bubbles: true }));
    });
    expect(readTransform(container).scale).toBeCloseTo(1, 2);
    act(() => {
      container.dispatchEvent(new KeyboardEvent('keydown', { key: '0', bubbles: true }));
    });
    expect(readTransform(container).scale).toBeLessThan(1);
  });
});
