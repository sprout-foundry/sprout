import { act, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { useIsNarrow } from './useIsNarrow';

let callbacks: Array<() => void> = [];
class FakeResizeObserver {
  constructor(cb: () => void) {
    callbacks.push(cb);
  }
  observe() {}
  disconnect() {}
}

let container: HTMLDivElement;
let root: Root;
let width = 400;
let seen: boolean[] = [];

function Probe() {
  const ref = useRef<HTMLDivElement>(null);
  seen.push(useIsNarrow(ref, 560));
  return <div ref={ref} />;
}

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});
beforeEach(() => {
  callbacks = [];
  seen = [];
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({ width }) as DOMRect);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('useIsNarrow', () => {
  it('reports narrow from the first measure and follows resizes', () => {
    width = 400;
    act(() => root.render(<Probe />));
    expect(seen.at(-1)).toBe(true);

    width = 900;
    act(() => callbacks.forEach((cb) => cb()));
    expect(seen.at(-1)).toBe(false);
  });

  it('keeps its answer while the element is hidden', () => {
    width = 400;
    act(() => root.render(<Probe />));
    width = 0;
    act(() => callbacks.forEach((cb) => cb()));
    expect(seen.at(-1)).toBe(true);
  });
});
