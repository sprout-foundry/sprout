import type { APIAdapter, PlatformNavItem } from './adapter';

// SP-016 P0.1: the extended PlatformNavItem contract. These tests are
// compile-time assertions that `badge` and `external` exist with the right
// types, plus value-level checks that pre-existing item shapes (without the
// new optional fields) remain valid — the extension must not break any
// existing consumer of the type.

describe('PlatformNavItem contract', () => {
  it('accepts items without the new optional fields (backward compatibility)', () => {
    const item: PlatformNavItem = {
      id: 'tasks',
      label: 'Tasks',
      href: '/tasks',
      icon: 'list-checks',
      order: 1,
    };
    expect(item.badge).toBeUndefined();
    expect(item.external).toBeUndefined();
  });

  it('accepts a number badge and the external flag', () => {
    const item: PlatformNavItem = {
      id: 'tasks',
      label: 'Tasks',
      href: '/tasks',
      badge: 3,
      external: true,
    };
    expect(item.badge).toBe(3);
    expect(item.external).toBe(true);
  });

  it('accepts a string badge (e.g. an overage state)', () => {
    const item: PlatformNavItem = {
      id: 'billing',
      label: 'Billing',
      href: '/account/billing',
      badge: 'overage',
    };
    expect(item.badge).toBe('overage');
    expect(item.external).toBeUndefined();
  });

  it('exposes platformNavItems on APIAdapter with the extended shape', () => {
    const items: readonly PlatformNavItem[] = [
      { id: 'dashboard', label: 'Dashboard', href: '/', external: true, badge: 'ok' },
      { id: 'tasks', label: 'Tasks', href: '/tasks' },
    ];
    const adapter: Pick<APIAdapter, 'platformNavItems'> = { platformNavItems: items };
    expect(adapter.platformNavItems?.[0]?.external).toBe(true);
    expect(adapter.platformNavItems?.[1]?.badge).toBeUndefined();
  });
});
