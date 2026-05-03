/**
 * bootstrapAdapter.ts — Install cloud adapter before component tree loads.
 *
 * Must be the first import in index.tsx so that config/mode.ts feature flags
 * read the correct adapter state when they are first evaluated.
 */

import { installAdapter } from './services/apiAdapter';
import { CloudAdapter } from './services/cloudAdapter';
import type { PlatformNavItem } from './services/apiAdapter';

const CLOUD_NAV_ITEMS: PlatformNavItem[] = [
  { id: 'tasks', label: 'Tasks', href: '/tasks', icon: 'list-checks', order: 1 },
  { id: 'billing', label: 'Billing', href: '/billing', icon: 'credit-card', order: 2 },
  { id: 'team', label: 'Team', href: '/team', icon: 'users', order: 3 },
];

// Use Vite's import.meta.env instead of process.env for browser compatibility
const mode = import.meta.env.REACT_APP_SPROUT_MODE;
const apiBase = import.meta.env.REACT_APP_FOUNDRY_API_URL;
const wsUrl = import.meta.env.REACT_APP_FOUNDRY_WS_URL;

if (mode === 'cloud') {
  const resolvedApiBase = apiBase || window.location.origin;
  const resolvedWsUrl = wsUrl ||
    `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`;

  installAdapter(new CloudAdapter({ apiBase: resolvedApiBase, wsUrl: resolvedWsUrl, navItems: CLOUD_NAV_ITEMS }));
}
