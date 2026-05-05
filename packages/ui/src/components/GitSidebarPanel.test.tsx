// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import GitSidebarPanel from './GitSidebarPanel';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('GitSidebarPanel', () => {
  it('renders as a div with placeholder text', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel));
    });

    const el = container.querySelector('div');
    expect(el).not.toBeNull();
    expect(el?.textContent).toBe('GitSidebarPanel placeholder');
  });

  it('is a default export', () => {
    expect(typeof GitSidebarPanel).toBe('function');
  });
});
