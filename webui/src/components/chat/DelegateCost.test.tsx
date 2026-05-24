// @ts-nocheck
/**
 * DelegateCost.test.tsx — Tests for the delegate cost display component.
 *
 * Verifies:
 *   - Empty input → returns null
 *   - Zero tokens/cost → returns null
 *   - Valid data → renders formatted tokens and cost
 *   - Multiple activities → sums tokens and costs correctly
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { DelegateCost } from './DelegateCost';
import type { DelegateActivity } from '@sprout/ui';

let container: HTMLDivElement;
let root: Root;

function makeActivity(overrides: Partial<DelegateActivity> = {}): DelegateActivity {
  return {
    delegateId: overrides.delegateId ?? 'delegate-1',
    action: overrides.action ?? 'started',
    summary: overrides.summary ?? 'Test activity',
    depth: overrides.depth ?? 0,
    tokensUsed: overrides.tokensUsed ?? 0,
    cost: overrides.cost ?? 0,
    toolsCalled: overrides.toolsCalled ?? [],
    status: overrides.status ?? 'running',
  };
}

beforeAll(() => {
  // @ts-expect-error
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

describe('DelegateCost', () => {
  it('returns null when activities array is empty', () => {
    act(() => {
      root.render(createElement(DelegateCost, { activities: [] }));
    });
    expect(container.innerHTML).toBe('');
  });

  it('returns null when all activities have zero tokens and zero cost', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [makeActivity({ tokensUsed: 0, cost: 0 })],
        }),
      );
    });
    expect(container.innerHTML).toBe('');
  });

  it('returns null when multiple activities all have zero tokens and zero cost', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [
            makeActivity({ delegateId: 'd1', tokensUsed: 0, cost: 0 }),
            makeActivity({ delegateId: 'd2', tokensUsed: 0, cost: 0 }),
          ],
        }),
      );
    });
    expect(container.innerHTML).toBe('');
  });

  it('displays formatted tokens and cost when activities have values', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [
            makeActivity({ tokensUsed: 1500, cost: 0.0123 }),
          ],
        }),
      );
    });

    const badge = container.querySelector('.delegate-cost-badge');
    expect(badge).not.toBeNull();
    expect(container.querySelector('.delegate-cost-label')?.textContent).toContain('Delegates');
    expect(container.querySelector('.delegate-cost-value')?.textContent).toContain('1.5k');
    expect(container.querySelector('.delegate-cost-separator')?.textContent).toBe('·');
  });

  it('correctly sums tokens and costs across multiple activities', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [
            makeActivity({ delegateId: 'd1', tokensUsed: 500, cost: 0.01 }),
            makeActivity({ delegateId: 'd2', tokensUsed: 800, cost: 0.02 }),
            makeActivity({ delegateId: 'd3', tokensUsed: 200, cost: 0.005 }),
          ],
        }),
      );
    });

    // Total: 1500 tokens (1.5k), $0.0350
    expect(container.querySelector('.delegate-cost-badge')).not.toBeNull();
    expect(container.querySelectorAll('.delegate-cost-value')).toHaveLength(2);
    expect(container.querySelector('.delegate-cost-value')?.textContent).toContain('1.5k');
    expect(container.querySelectorAll('.delegate-cost-value')[1]?.textContent).toContain('0.0350');
  });

  it('formats tokens with k suffix for values >= 1000', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [makeActivity({ tokensUsed: 2500, cost: 0 })],
        }),
      );
    });

    expect(container.querySelector('.delegate-cost-value')?.textContent).toContain('2.5k');
  });

  it('formats tokens with M suffix for values >= 1000000', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [makeActivity({ tokensUsed: 1500000, cost: 0 })],
        }),
      );
    });

    expect(container.querySelector('.delegate-cost-value')?.textContent).toContain('1.5M');
  });

  it('displays raw number for tokens < 1000', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [makeActivity({ tokensUsed: 42, cost: 0 })],
        }),
      );
    });

    expect(container.querySelector('.delegate-cost-value')?.textContent).toContain('42');
  });

  it('formats cost with 4 decimal places', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [makeActivity({ tokensUsed: 0, cost: 0.05 })],
        }),
      );
    });

    expect(container.querySelectorAll('.delegate-cost-value')[1]?.textContent).toContain('0.0500');
  });

  it('renders both token and cost values together with separator', () => {
    act(() => {
      root.render(
        createElement(DelegateCost, {
          activities: [makeActivity({ tokensUsed: 100, cost: 0.01 })],
        }),
      );
    });

    const badge = container.querySelector('.delegate-cost-badge');
    expect(badge).not.toBeNull();
    expect(badge?.textContent).toContain('tok');
    expect(badge?.textContent).toContain('·');
    expect(badge?.textContent).toContain('$');
  });
});
