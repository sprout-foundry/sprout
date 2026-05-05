// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { SproutProvider, useSproutAdapter } from './SproutAdapterContext';
import type { APIAdapter } from '../types/adapter';

// ---------------------------------------------------------------------------
// Mock Adapter Helper
// ---------------------------------------------------------------------------

function createMockAdapter(overrides: Partial<APIAdapter> = {}): APIAdapter {
  return {
    name: 'TestAdapter',
    fetch: vi.fn().mockResolvedValue({} as Response),
    getWebSocketURL: vi.fn().mockReturnValue(null),
    requiresBackendHealthCheck: false,
    fileOpsViaAPI: true,
    showOnboarding: true,
    supportsSSH: false,
    supportsInstances: false,
    supportsLocalTerminal: true,
    supportsSettings: true,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let latestContext: APIAdapter | null | undefined;

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
  latestContext = undefined;
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Test component that consumes the SproutAdapterContext and stores
 * a reference to the latest hook return value for inspection in tests.
 */
function TestConsumer() {
  latestContext = useSproutAdapter();
  return createElement('div', { 'data-testid': 'consumer' });
}

/**
 * Mounts the SproutProvider with a TestConsumer child.
 */
function renderProvider(adapter: APIAdapter | null = null) {
  act(() => {
    // @ts-expect-error — createElement accepts children as rest args, but TS overloads don't reflect this
    root.render(createElement(SproutProvider, { adapter }, createElement(TestConsumer)));
  });
}

/** Shorthand to get the current context value from the latest render. */
const ctx = () => latestContext;

/**
 * Asserts that the adapter is non-null and returns it.
 * Throws a clear error if the adapter is null/undefined.
 */
function requireCtx(): APIAdapter {
  const v = latestContext;
  if (v === null || v === undefined) {
    throw new Error('Expected adapter to be non-null in test');
  }
  return v;
}

// ---------------------------------------------------------------------------
// Tests: useSproutAdapter hook
// ---------------------------------------------------------------------------

describe('useSproutAdapter', () => {
  it('throws an error when used outside of SproutProvider', () => {
    // Suppress the expected console.error from React when the component throws
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    // Render TestConsumer WITHOUT the provider — the hook should throw
    expect(() => {
      act(() => {
        root.render(createElement(TestConsumer));
      });
    }).toThrow('useSproutAdapter must be used within SproutProvider');

    consoleSpy.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Tests: SproutProvider
// ---------------------------------------------------------------------------

describe('SproutProvider', () => {
  it('provides null adapter when adapter prop is null', () => {
    renderProvider(null);

    expect(ctx()).toBeDefined();
    expect(ctx()).toBeNull();
  });

  it('provides the adapter when given a full APIAdapter', () => {
    const adapter = createMockAdapter({
      name: 'FullAdapter',
      requiresBackendHealthCheck: true,
      fileOpsViaAPI: false,
      showOnboarding: false,
      supportsSSH: true,
      supportsInstances: true,
      supportsLocalTerminal: false,
      supportsSettings: false,
    });

    renderProvider(adapter);

    expect(ctx()).toBeDefined();
    expect(ctx()).toBe(adapter);
    expect(requireCtx().name).toBe('FullAdapter');
    expect(requireCtx().requiresBackendHealthCheck).toBe(true);
    expect(requireCtx().fileOpsViaAPI).toBe(false);
    expect(requireCtx().showOnboarding).toBe(false);
    expect(requireCtx().supportsSSH).toBe(true);
    expect(requireCtx().supportsInstances).toBe(true);
    expect(requireCtx().supportsLocalTerminal).toBe(false);
    expect(requireCtx().supportsSettings).toBe(false);
    expect(requireCtx().fetch).toBe(adapter.fetch);
    expect(requireCtx().getWebSocketURL).toBe(adapter.getWebSocketURL);
  });

  it('provides adapter with only required fields (no optional fields like platformNavItems)', () => {
    const adapter = createMockAdapter();

    // The default mock has no platformNavItems, simulating a minimal adapter
    expect(adapter.platformNavItems).toBeUndefined();

    renderProvider(adapter);

    expect(ctx()).toBe(adapter);
    expect(requireCtx().name).toBe('TestAdapter');
    expect(requireCtx().platformNavItems).toBeUndefined();
  });

  it('provides adapter with platformNavItems', () => {
    const navItems = [
      { id: 'billing', label: 'Billing', href: '/billing', icon: 'credit-card', order: 1 },
      { id: 'tasks', label: 'Tasks', href: '/tasks', order: 2 },
    ];

    const adapter = createMockAdapter({
      name: 'CloudAdapter',
      platformNavItems: navItems,
    });

    renderProvider(adapter);

    expect(ctx()).toBe(adapter);
    expect(requireCtx().platformNavItems).toBe(navItems);
    expect(requireCtx().platformNavItems).toHaveLength(2);
    expect(requireCtx().platformNavItems![0]).toEqual({
      id: 'billing',
      label: 'Billing',
      href: '/billing',
      icon: 'credit-card',
      order: 1,
    });
    expect(requireCtx().platformNavItems![1]).toEqual({
      id: 'tasks',
      label: 'Tasks',
      href: '/tasks',
      order: 2,
    });
  });

  it('provides adapter with platformNavItems having only required fields (no icon or order)', () => {
    const navItems = [
      { id: 'settings', label: 'Settings', href: '/settings' },
    ];

    const adapter = createMockAdapter({
      name: 'MinimalNavAdapter',
      platformNavItems: navItems,
    });

    renderProvider(adapter);

    expect(requireCtx().platformNavItems!).toEqual(navItems);
    expect(requireCtx().platformNavItems![0].id).toBe('settings');
    expect(requireCtx().platformNavItems![0].label).toBe('Settings');
    expect(requireCtx().platformNavItems![0].href).toBe('/settings');
    // Optional fields should be undefined
    expect(requireCtx().platformNavItems![0].icon).toBeUndefined();
    expect(requireCtx().platformNavItems![0].order).toBeUndefined();
  });

  it('renders children correctly', () => {
    renderProvider(null);

    // The TestConsumer should have rendered its div
    expect(container.querySelector('[data-testid="consumer"]')).not.toBeNull();
  });

  it('returns the exact adapter instance (reference equality)', () => {
    const adapter = createMockAdapter({ name: 'RefTestAdapter' });

    renderProvider(adapter);

    // useSproutAdapter should return the exact same object reference
    expect(ctx()).toBe(adapter);
  });

  it('context value is stable across rerenders with the same adapter', () => {
    const adapter = createMockAdapter({ name: 'StableAdapter' });

    renderProvider(adapter);

    const firstResult = ctx();

    // Rerender with the same adapter
    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(createElement(SproutProvider, { adapter }, createElement(TestConsumer)));
    });

    const secondResult = ctx();

    // The context value should be the same reference (useMemo with [adapter])
    expect(secondResult).toBe(firstResult);
  });

  it('context value updates when adapter prop changes', () => {
    const firstAdapter = createMockAdapter({ name: 'FirstAdapter' });
    const secondAdapter = createMockAdapter({ name: 'SecondAdapter' });

    renderProvider(firstAdapter);
    expect(ctx()).toBe(firstAdapter);
    expect(requireCtx().name).toBe('FirstAdapter');

    // Rerender with a different adapter
    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(createElement(SproutProvider, { adapter: secondAdapter }, createElement(TestConsumer)));
    });

    expect(ctx()).toBe(secondAdapter);
    expect(requireCtx().name).toBe('SecondAdapter');
  });

  it('useSproutAdapter returns null adapter correctly (not undefined)', () => {
    renderProvider(null);

    expect(ctx()).toBeNull();
    // Explicitly check that it's null, not undefined
    expect(ctx()).not.toBeUndefined();
  });

  it('updates to null when adapter prop changes from adapter to null', () => {
    const adapter = createMockAdapter({ name: 'FirstAdapter' });
    renderProvider(adapter);
    expect(ctx()).toBe(adapter);

    // Rerender with null
    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(createElement(SproutProvider, { adapter: null }, createElement(TestConsumer)));
    });

    expect(ctx()).toBeNull();
  });

  it('inner SproutProvider overrides outer SproutProvider', () => {
    const outerAdapter = createMockAdapter({ name: 'Outer' });
    const innerAdapter = createMockAdapter({ name: 'Inner' });

    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(SproutProvider, { adapter: outerAdapter },
          // @ts-expect-error — createElement accepts children as rest args
          createElement(SproutProvider, { adapter: innerAdapter },
            createElement(TestConsumer)
          )
        )
      );
    });

    expect(ctx()).toBe(innerAdapter);
    expect(requireCtx().name).toBe('Inner');
  });
});
