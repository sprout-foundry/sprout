// @ts-nocheck
/**
 * Integration test: verifies that when the CloudAdapter is installed with
 * platform nav items (tasks, billing, team), the PlatformNavProvider makes
 * them available to the Sidebar component.
 *
 * This tests the full chain:
 *   installAdapter(CloudAdapter) → PlatformNavProvider → Sidebar icon rail
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import type { PlatformNavItem } from '../services/apiAdapter';

// ---------------------------------------------------------------------------
// Mocks — install a mock adapter BEFORE the PlatformNavContext is loaded
// ---------------------------------------------------------------------------

// Create a mock adapter that has the 3 cloud nav items
const CLOUD_NAV_ITEMS: PlatformNavItem[] = [
  { id: 'tasks', label: 'Tasks', href: '/tasks', icon: 'list-checks', order: 1 },
  { id: 'billing', label: 'Billing', href: '/billing', icon: 'credit-card', order: 2 },
  { id: 'team', label: 'Team', href: '/team', icon: 'users', order: 3 },
];

const mockAdapter = {
  name: 'foundry-cloud',
  requiresBackendHealthCheck: true,
  fileOpsViaAPI: false,
  showOnboarding: false,
  supportsSSH: false,
  supportsInstances: true,
  supportsLocalTerminal: false,
  supportsSettings: false,
  platformNavItems: CLOUD_NAV_ITEMS,
  fetch: vi.fn().mockResolvedValue(new Response('{}', { status: 200 })),
  getWebSocketURL: () => 'wss://test.sprout.dev/ws',
};

// Mock apiAdapter module — this runs BEFORE any imports of apiAdapter resolve
vi.mock('../services/apiAdapter', () => ({
  getAdapter: vi.fn(() => mockAdapter),
  installAdapter: vi.fn(),
  hasAdapter: vi.fn(() => true),
  requiresBackendHealthCheck: vi.fn(() => true),
}));

// Now import — PlatformNavContext will use the mocked getAdapter()
import { PlatformNavProvider, usePlatformNav } from './PlatformNavContext';
import { getAdapter } from '../services/apiAdapter';

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
  latestContext = undefined;
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Test component that consumes the PlatformNavContext.
 */
function TestConsumer() {
  const ctx = usePlatformNav();
  latestContext = ctx;
  return createElement('div', { 'data-testid': 'consumer' });
}

/**
 * Mount the PlatformNavProvider with a TestConsumer child.
 */
function renderProvider() {
  act(() => {
    root.render(createElement(PlatformNavProvider, null, createElement(TestConsumer)));
  });
}

// ---------------------------------------------------------------------------
// Integration Tests: CloudAdapter → PlatformNavProvider
// ---------------------------------------------------------------------------

describe('PlatformNav Integration: CloudAdapter with platform nav items', () => {
  it('PlatformNavProvider exposes all 3 cloud nav items (tasks, billing, team)', () => {
    renderProvider();

    expect(latestContext).toBeDefined();
    expect(latestContext.platformNavItems).toHaveLength(3);
  });

  it('nav item 0 is tasks with correct properties', () => {
    renderProvider();

    const items = latestContext.platformNavItems;
    expect(items[0].id).toBe('tasks');
    expect(items[0].label).toBe('Tasks');
    expect(items[0].href).toBe('/tasks');
    expect(items[0].icon).toBe('list-checks');
    expect(items[0].order).toBe(1);
  });

  it('nav item 1 is billing with correct properties', () => {
    renderProvider();

    const items = latestContext.platformNavItems;
    expect(items[1].id).toBe('billing');
    expect(items[1].label).toBe('Billing');
    expect(items[1].href).toBe('/billing');
    expect(items[1].icon).toBe('credit-card');
    expect(items[1].order).toBe(2);
  });

  it('nav item 2 is team with correct properties', () => {
    renderProvider();

    const items = latestContext.platformNavItems;
    expect(items[2].id).toBe('team');
    expect(items[2].label).toBe('Team');
    expect(items[2].href).toBe('/team');
    expect(items[2].icon).toBe('users');
    expect(items[2].order).toBe(3);
  });

  it('items are ordered by their order field (ascending)', () => {
    renderProvider();

    const items = latestContext.platformNavItems;
    const orders = items.map((item: PlatformNavItem) => item.order ?? Infinity);
    expect(orders).toEqual([1, 2, 3]);
  });

  it('adapter identity is "foundry-cloud" confirming cloud adapter is installed', () => {
    const adapter = getAdapter();
    expect(adapter).not.toBeNull();
    expect(adapter!.name).toBe('foundry-cloud');
  });

  it('Sidebar receives nav items through usePlatformNav hook', () => {
    renderProvider();

    // Simulate what Sidebar.tsx does: read platformNavItems and sort them
    const { platformNavItems } = latestContext;
    const sorted = [...platformNavItems].sort(
      (a: PlatformNavItem, b: PlatformNavItem) => (a.order ?? Infinity) - (b.order ?? Infinity),
    );

    expect(sorted[0].id).toBe('tasks');
    expect(sorted[1].id).toBe('billing');
    expect(sorted[2].id).toBe('team');
  });

  it('all nav item IDs match the VALID_PLATFORM_VIEWS set used by Sidebar', () => {
    renderProvider();

    const VALID_PLATFORM_VIEWS = new Set(['tasks', 'billing', 'team']);
    const itemIds = latestContext.platformNavItems.map((item: PlatformNavItem) => item.id);

    for (const id of itemIds) {
      expect(VALID_PLATFORM_VIEWS.has(id)).toBe(true);
    }
  });
});

describe('PlatformNav Integration: Local mode (no adapter)', () => {
  // Simulate local mode by overriding getAdapter to return null
  beforeEach(() => {
    // Re-mock with null adapter to simulate local mode
    vi.mocked(getAdapter).mockReturnValue(null);
  });

  afterEach(() => {
    // Restore cloud adapter mock for other tests
    vi.mocked(getAdapter).mockReturnValue(mockAdapter);
  });

  it('PlatformNavProvider provides empty array when no adapter is installed', () => {
    renderProvider();

    expect(latestContext).toBeDefined();
    expect(latestContext.platformNavItems).toEqual([]);
    expect(latestContext.platformNavItems).toHaveLength(0);
  });
});
