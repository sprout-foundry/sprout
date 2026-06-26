// SP-009-migration: SelectionActionBar was migrated from local webui/src/components/
// to @sprout/ui. This test verifies the import from @sprout/ui works correctly
// and the component renders without crashing.

import { vi } from 'vitest';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { SelectionActionBar } from '@sprout/ui';

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
// Migration Verification: SelectionActionBar imported from @sprout/ui
// ---------------------------------------------------------------------------

describe('SelectionActionBar (@sprout/ui import)', () => {
  it('imports successfully from @sprout/ui', () => {
    expect(SelectionActionBar).toBeDefined();
    expect(typeof SelectionActionBar).toBe('function');
  });

  it('renders without crashing', () => {
    act(() => {
      root.render(
        createElement(SelectionActionBar, {
          count: 3,
          onClear: vi.fn(),
        }),
      );
    });

    expect(container.querySelector('.selection-action-bar')).not.toBeNull();
  });

  it('displays correct count text for singular', () => {
    act(() => {
      root.render(
        createElement(SelectionActionBar, {
          count: 1,
          onClear: vi.fn(),
        }),
      );
    });

    const countEl = container.querySelector('.selection-count');
    expect(countEl?.textContent).toContain('1 item selected');
  });

  it('displays correct count text for plural', () => {
    act(() => {
      root.render(
        createElement(SelectionActionBar, {
          count: 5,
          onClear: vi.fn(),
        }),
      );
    });

    const countEl = container.querySelector('.selection-count');
    expect(countEl?.textContent).toContain('5 items selected');
  });

  it('calls onClear when clear button is clicked', () => {
    const onClear = vi.fn();

    act(() => {
      root.render(
        createElement(SelectionActionBar, {
          count: 3,
          onClear,
        }),
      );
    });

    const btn = container.querySelector<HTMLButtonElement>('.clear-selection-btn');
    act(() => {
      btn?.click();
    });

    expect(onClear).toHaveBeenCalledTimes(1);
  });
});
