// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { PlatformNavProvider, usePlatformNav } from './PlatformNavContext';
import { getAdapter } from '../services/apiAdapter';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../services/apiAdapter', () => ({
  getAdapter: jest.fn(),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let latestContext: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
  latestContext = undefined;
  // Default: no adapter installed
  getAdapter.mockReturnValue(null);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Test component that consumes the PlatformNavContext and stores
 * a reference to the latest context value for inspection in tests.
 */
function TestConsumer() {
  const ctx = usePlatformNav();
  latestContext = ctx;
  return createElement('div', { 'data-testid': 'consumer' });
}

/**
 * Mounts the PlatformNavProvider with a TestConsumer child.
 */
function renderProvider() {
  act(() => {
    root.render(createElement(PlatformNavProvider, null, createElement(TestConsumer)));
  });
}

/** Shorthand to get the current context value from the latest render. */
const ctx = () => latestContext;

// ---------------------------------------------------------------------------
// Tests: usePlatformNav hook
// ---------------------------------------------------------------------------

describe('usePlatformNav', () => {
  it('throws an error when used outside of PlatformNavProvider', () => {
    // Suppress the expected console.error from React when the component throws
    const consoleSpy = jest.spyOn(console, 'error').mockImplementation(() => {});

    // Render TestConsumer WITHOUT the provider — the hook should throw
    expect(() => {
      act(() => {
        root.render(createElement(TestConsumer));
      });
    }).toThrow('usePlatformNav must be used within PlatformNavProvider');

    consoleSpy.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Tests: PlatformNavProvider
// ---------------------------------------------------------------------------

describe('PlatformNavProvider', () => {
  it('provides empty array when no adapter is installed (getAdapter returns null)', () => {
    getAdapter.mockReturnValue(null);
    renderProvider();

    expect(ctx()).toBeDefined();
    expect(ctx().platformNavItems).toEqual([]);
  });

  it('provides empty array when adapter has no platformNavItems (undefined)', () => {
    getAdapter.mockReturnValue({
      name: 'TestAdapter',
      platformNavItems: undefined,
    });
    renderProvider();

    expect(ctx()).toBeDefined();
    expect(ctx().platformNavItems).toEqual([]);
  });

  it('provides the adapter nav items when adapter has platformNavItems', () => {
    const navItems = [
      { id: 'billing', label: 'Billing', href: '/billing', icon: 'credit-card', order: 1 },
      { id: 'tasks', label: 'Tasks', href: '/tasks', order: 2 },
    ];

    getAdapter.mockReturnValue({
      name: 'CloudAdapter',
      platformNavItems: navItems,
    });
    renderProvider();

    expect(ctx()).toBeDefined();
    expect(ctx().platformNavItems).toBe(navItems);
    expect(ctx().platformNavItems).toHaveLength(2);
    expect(ctx().platformNavItems[0]).toEqual({
      id: 'billing',
      label: 'Billing',
      href: '/billing',
      icon: 'credit-card',
      order: 1,
    });
    expect(ctx().platformNavItems[1]).toEqual({
      id: 'tasks',
      label: 'Tasks',
      href: '/tasks',
      order: 2,
    });
  });

  it('provides nav items with only required fields (no icon or order)', () => {
    const navItems = [
      { id: 'settings', label: 'Settings', href: '/settings' },
    ];

    getAdapter.mockReturnValue({
      name: 'MinimalAdapter',
      platformNavItems: navItems,
    });
    renderProvider();

    expect(ctx().platformNavItems).toEqual(navItems);
    expect(ctx().platformNavItems[0].id).toBe('settings');
    expect(ctx().platformNavItems[0].label).toBe('Settings');
    expect(ctx().platformNavItems[0].href).toBe('/settings');
    // Optional fields should be undefined
    expect(ctx().platformNavItems[0].icon).toBeUndefined();
    expect(ctx().platformNavItems[0].order).toBeUndefined();
  });

  it('provides a single nav item correctly', () => {
    const navItems = [
      { id: 'dashboard', label: 'Dashboard', href: '/dashboard', order: 0 },
    ];

    getAdapter.mockReturnValue({
      name: 'SingleItemAdapter',
      platformNavItems: navItems,
    });
    renderProvider();

    expect(ctx().platformNavItems).toHaveLength(1);
    expect(ctx().platformNavItems[0].id).toBe('dashboard');
  });

  it('provides the same reference to the adapter nav items array', () => {
    const navItems = [
      { id: 'test', label: 'Test', href: '/test' },
    ];

    getAdapter.mockReturnValue({
      name: 'RefTestAdapter',
      platformNavItems: navItems,
    });
    renderProvider();

    // The provider should pass through the exact array reference
    // (it does not copy or transform the array)
    expect(ctx().platformNavItems).toBe(navItems);
  });

  it('renders children correctly', () => {
    getAdapter.mockReturnValue(null);
    renderProvider();

    // The TestConsumer should have rendered its div
    expect(container.querySelector('[data-testid="consumer"]')).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: usePlatformNav returns correct items inside the provider
// ---------------------------------------------------------------------------

describe('usePlatformNav inside provider', () => {
  it('returns the platformNavItems from the context', () => {
    const navItems = [
      { id: 'billing', label: 'Billing', href: '/billing' },
      { id: 'tasks', label: 'Tasks', href: '/tasks' },
      { id: 'audit-log', label: 'Audit Log', href: '/audit', icon: 'list', order: 3 },
    ];

    getAdapter.mockReturnValue({
      name: 'FullAdapter',
      platformNavItems: navItems,
    });
    renderProvider();

    const result = ctx();
    expect(result).toBeDefined();
    expect(result.platformNavItems).toBe(navItems);
    expect(result.platformNavItems).toHaveLength(3);
  });

  it('returns a stable context object across rerenders with the same adapter', () => {
    const navItems = [{ id: 'a', label: 'A', href: '/a' }];

    getAdapter.mockReturnValue({
      name: 'StableAdapter',
      platformNavItems: navItems,
    });
    renderProvider();

    const firstCtx = ctx();

    // Rerender with the same adapter
    act(() => {
      root.render(createElement(PlatformNavProvider, null, createElement(TestConsumer)));
    });

    const secondCtx = ctx();

    // The context value object itself should be stable (useMemo with [])
    expect(secondCtx).toBe(firstCtx);

    // The platformNavItems array reference should also be stable
    expect(secondCtx.platformNavItems).toBe(firstCtx.platformNavItems);
  });
});
