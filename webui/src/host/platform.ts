/**
 * The platform surface: the one module (besides the platform helpers in
 * platformUrl/platformGitHub and the account card) that names the platform's
 * pages. It is the cloud host's own resolution of Sprout's generic contract:
 *
 *  - the account-area exit items and the Home "Work" places, as data
 *    (label + intent), so no Sprout component hard-codes "/account/billing",
 *    "/team" or "#/runners";
 *  - `intentPath(intent)`, the mapping from an intent to the platform SPA
 *    path (with the SPA route in the hash and `?from=editor` in the query,
 *    SP-016 P0.7);
 *  - `platformEntitlements()`, which fetches the platform's billing status
 *    (the ledger check that used to live in CreditsChip) and returns the
 *    generic usage summary the contract exposes.
 *
 * Everything platform-specific about navigation and entitlements lives here,
 * so the components stay host-driven and the strings stay on the host side.
 */

import { platformHref } from './platformUrl';
import type { HostEntitlements, HostNavItem, HostNavigationIntent } from './types';

/**
 * The account-area exits, in the platform header's menu order. Paths carry
 * `?from=editor` (SP-016 P0.7) and put the SPA route in the hash: Team/Runners
 * are also flat API routes on the platform (GET /team, GET /runners), so a
 * plain /team would return the API's JSON, not the page. Team/Runners have no
 * dedicated navigation intent, so they reuse the account intent while carrying
 * their own destination path.
 */
export const PLATFORM_ACCOUNT_ITEMS: HostNavItem[] = [
  { label: 'Dashboard', intent: { type: 'nav', id: 'dashboard' } },
  { label: 'Tasks', intent: { type: 'nav', id: 'tasks' } },
  { label: 'Usage & billing', intent: { type: 'usage' } },
  { label: 'Team', intent: { type: 'nav', id: 'team' } },
  { label: 'Runners', intent: { type: 'nav', id: 'runners' } },
  { label: 'Settings', intent: { type: 'nav', id: 'settings' } },
];

/** The admin-area exit; shown only to platform administrators. */
export const PLATFORM_ADMIN_ITEM: HostNavItem = { label: 'Admin', intent: { type: 'nav', id: 'admin' } };

/** The Home "Work" places. */
export const PLATFORM_WORK_ITEMS: HostNavItem[] = [
  { label: 'Dashboard', intent: { type: 'nav', id: 'dashboard' } },
  { label: 'Tasks', intent: { type: 'nav', id: 'tasks' } },
  { label: 'Workspaces', intent: { type: 'nav', id: 'workspaces' } },
];

/** The platform page each nav id names — the one place the page strings live. */
const PLATFORM_PAGES: Record<string, string> = {
  dashboard: '/?from=editor',
  tasks: '/?from=editor#/tasks',
  workspaces: '/?from=editor#/workspaces',
  team: '/?from=editor#/team',
  runners: '/?from=editor#/runners',
  settings: '/?from=editor#/settings',
  admin: '/?from=editor#/admin',
};

/**
 * The platform SPA path an intent resolves to, or null when the host has no
 * page for the intent. Each account/work item carries a distinct `nav` intent
 * (its platform id), so the mapping is unambiguous.
 */
export function intentPath(intent: HostNavigationIntent): string | null {
  if (intent.type === 'usage') return '/?from=editor#/account/billing';
  if (intent.type === 'account') return '/?from=editor';
  if (intent.type === 'nav') return PLATFORM_PAGES[intent.id] ?? null;
  return null;
}

/** Convenience: the path for a `HostNavItem`, via its own intent. */
export function itemHref(item: HostNavItem): string | null {
  return intentPath(item.intent);
}

/**
 * The platform usage summary for the header's credits chip. Reads the
 * platform's billing status (the same `/billing/status` call CreditsChip
 * used to make) and returns a generic entitlement, or undefined when the
 * ledger is not v2 or the call fails/does not respond.
 */
export async function platformEntitlements(): Promise<HostEntitlements | undefined> {
  try {
    const res = await fetch(platformHref('/billing/status'), { credentials: 'include' });
    if (!res.ok) return undefined;
    const status = (await res.json()) as { ledger?: string; total_remaining?: number };
    if (status.ledger !== 'v2' || typeof status.total_remaining !== 'number') return undefined;
    return {
      usageSummary: {
        remaining: status.total_remaining,
        label: 'credits',
        linkTarget: platformHref('/?from=editor#/account/billing'),
      },
    };
  } catch {
    return undefined;
  }
}

/** The platform's path for a URL already in its SPA ("/#/tasks/42"). */
export function platformPageHref(path: string): string {
  return platformHref(path);
}
