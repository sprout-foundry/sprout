// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import SelectionActionBar from './SelectionActionBar';

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
  jest.clearAllMocks();
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

describe('SelectionActionBar', () => {
  it('renders the bar with count 1 (singular)', () => {
    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 1,
        onClear: jest.fn(),
      }));
    });

    expect(container.querySelector('.selection-action-bar')).not.toBeNull();
    const countEl = container.querySelector('.selection-count');
    expect(countEl).not.toBeNull();
    expect(countEl?.textContent).toContain('1 item selected');
  });

  it('renders the bar with count > 1 (plural)', () => {
    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 5,
        onClear: jest.fn(),
      }));
    });

    const countEl = container.querySelector('.selection-count');
    expect(countEl).not.toBeNull();
    expect(countEl?.textContent).toContain('5 items selected');
  });

  it('renders the bar with count 0', () => {
    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 0,
        onClear: jest.fn(),
      }));
    });

    const countEl = container.querySelector('.selection-count');
    expect(countEl?.textContent).toContain('0 items selected');
  });

  it('renders clear button with correct aria-label', () => {
    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 3,
        onClear: jest.fn(),
      }));
    });

    const btn = container.querySelector('.clear-selection-btn');
    expect(btn).not.toBeNull();
    expect(btn?.getAttribute('aria-label')).toBe('Clear selection');
  });

  it('clear button has type="button"', () => {
    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 3,
        onClear: jest.fn(),
      }));
    });

    const btn = container.querySelector('.clear-selection-btn');
    expect(btn?.getAttribute('type')).toBe('button');
  });

  it('clear button text is "Clear"', () => {
    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 3,
        onClear: jest.fn(),
      }));
    });

    const btn = container.querySelector('.clear-selection-btn');
    expect(btn?.textContent).toContain('Clear');
  });

  it('calls onClear when clear button is clicked', () => {
    const onClear = jest.fn();

    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 3,
        onClear,
      }));
    });

    const btn = container.querySelector('.clear-selection-btn');
    act(() => {
      btn?.click();
    });

    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it('renders X icon inside clear button', () => {
    act(() => {
      root.render(createElement(SelectionActionBar, {
        count: 2,
        onClear: jest.fn(),
      }));
    });

    const btn = container.querySelector('.clear-selection-btn');
    // The X icon from lucide-react renders as an SVG
    expect(btn?.querySelector('svg')).not.toBeNull();
  });
});
