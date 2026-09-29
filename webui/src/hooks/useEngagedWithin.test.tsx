import { act, createElement, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useEngagedWithin } from './useEngagedWithin';

let container: HTMLDivElement;
let root: Root;
let engaged: boolean | null = null;

function Probe() {
  const ref = useRef<HTMLDivElement>(null);
  engaged = useEngagedWithin(ref);
  return createElement(
    'div',
    null,
    createElement('div', { ref, id: 'inside' }, 'workspace'),
    createElement('button', { id: 'outside' }, 'sidebar'),
    createElement('div', { className: 'context-menu' }, createElement('button', { id: 'menu-item' }, 'Delete')),
  );
}

beforeEach(() => {
  (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

function mount() {
  act(() => root.render(createElement(Probe)));
}

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('useEngagedWithin', () => {
  it('follows clicks and focus in and out of the element', () => {
    mount();
    expect(engaged).toBe(true);
    act(() => {
      document.getElementById('outside')!.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    });
    expect(engaged).toBe(false);
    act(() => {
      document.getElementById('inside')!.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    });
    expect(engaged).toBe(true);
    act(() => {
      document.getElementById('outside')!.focus();
    });
    expect(engaged).toBe(false);
  });

  it('ignores menus and dialogs', () => {
    mount();
    act(() => {
      document.getElementById('menu-item')!.dispatchEvent(new Event('pointerdown', { bubbles: true }));
      document.getElementById('menu-item')!.focus();
    });
    expect(engaged).toBe(true);
  });
});
