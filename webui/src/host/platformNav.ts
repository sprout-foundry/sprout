/**
 * The fallback platform nav items, used when the platform did not serve its
 * own `navItems` (older platform versions). This lives in the host tree
 * because it names the platform's pages (/account/billing, /team, /runners,
 * /admin) — those strings must not appear outside webui/src/host/.
 *
 * SP-016 P0.4: items that have a registered plugin view (all but "admin")
 * stay unflagged so the host switches them in-editor — exactly today's
 * behavior for this fallback list. "admin" has no plugin view, so today it
 * exits via the href; the explicit external flag preserves that on the new
 * contract.
 */

import type { PlatformNavItem } from '@sprout/ui';

export const CLOUD_NAV_ITEMS: PlatformNavItem[] = [
  { id: 'dashboard', label: 'Dashboard', href: '/', icon: 'layout-dashboard', order: 0 },
  { id: 'tasks', label: 'Tasks', href: '/tasks', icon: 'list-checks', order: 1 },
  { id: 'billing', label: 'Billing', href: '/account/billing', icon: 'credit-card', order: 2 },
  { id: 'team', label: 'Team', href: '/team', icon: 'users', order: 3 },
  { id: 'runners', label: 'Runners', href: '/runners', icon: 'server', order: 4 },
  { id: 'workspaces', label: 'Workspaces', href: '/workspaces', icon: 'monitor', order: 5 },
  { id: 'admin', label: 'Admin', href: '/admin', icon: 'shield', order: 6, external: true },
];
