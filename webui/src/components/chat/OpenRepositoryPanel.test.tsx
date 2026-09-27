import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../bootstrapAdapter', () => ({ getPlatformURL: () => undefined }));

import { OpenRepositoryPanel } from './OpenRepositoryPanel';

let container: HTMLDivElement;
let root: Root;
const originalLocation = window.location;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  Object.defineProperty(window, 'location', { configurable: true, value: { ...originalLocation, search: '' } });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  Object.defineProperty(window, 'location', { configurable: true, value: originalLocation });
});

function type(value: string) {
  const input = container.querySelector<HTMLInputElement>('[data-testid="open-repo-input"]')!;
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

describe('OpenRepositoryPanel', () => {
  it('opens the named repository through ?repo=', () => {
    act(() => root.render(<OpenRepositoryPanel />));
    type('acme/widgets');
    act(() => {
      container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(window.location.search).toBe('repo=https%3A%2F%2Fgithub.com%2Facme%2Fwidgets');
  });

  it('explains an unusable repository reference', () => {
    act(() => root.render(<OpenRepositoryPanel />));
    type('not a repo');
    act(() => {
      container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(container.querySelector('[role="alert"]')?.textContent).toBeTruthy();
    expect(window.location.search).toBe('');
  });
});
