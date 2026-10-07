/**
 * The cloud host's platform surface (SP-160 §160b): the intent→path mapping
 * and the entitlements resolver. The items name the platform's pages; these
 * tests pin the mapping and the billing-status fetch's happy / legacy-ledger /
 * network-error cases.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { platformEntitlements, intentPath, PLATFORM_ACCOUNT_ITEMS, PLATFORM_WORK_ITEMS } from './platform';

vi.mock('../bootstrapAdapter', () => ({ getPlatformURL: () => undefined }));

afterEach(() => vi.unstubAllGlobals());

describe('intentPath', () => {
  it('maps usage to the billing page', () => {
    expect(intentPath({ type: 'usage' })).toBe('/?from=editor#/account/billing');
  });

  it('maps the account intent to the dashboard', () => {
    expect(intentPath({ type: 'account' })).toBe('/?from=editor');
  });

  it('maps each named nav id to its platform page', () => {
    expect(intentPath({ type: 'nav', id: 'dashboard' })).toBe('/?from=editor');
    expect(intentPath({ type: 'nav', id: 'tasks' })).toBe('/?from=editor#/tasks');
    expect(intentPath({ type: 'nav', id: 'team' })).toBe('/?from=editor#/team');
    expect(intentPath({ type: 'nav', id: 'runners' })).toBe('/?from=editor#/runners');
    expect(intentPath({ type: 'nav', id: 'settings' })).toBe('/?from=editor#/settings');
    expect(intentPath({ type: 'nav', id: 'admin' })).toBe('/?from=editor#/admin');
    expect(intentPath({ type: 'nav', id: 'workspaces' })).toBe('/?from=editor#/workspaces');
  });

  it('returns null for an intent the host has no page for', () => {
    expect(intentPath({ type: 'nav', id: 'nope' })).toBeNull();
    expect(intentPath({ type: 'project', project: 'p' })).toBeNull();
    expect(intentPath({ type: 'signOut' })).toBeNull();
  });

  it('every item in the work and account lists resolves to a path', () => {
    for (const item of [...PLATFORM_WORK_ITEMS, ...PLATFORM_ACCOUNT_ITEMS]) {
      expect(intentPath(item.intent)).not.toBeNull();
    }
  });
});

describe('platformEntitlements', () => {
  function stubBilling(body: unknown, status = 200) {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status })));
  }

  it('returns the usage summary for the v2 ledger', async () => {
    stubBilling({ ledger: 'v2', total_remaining: 450_000 });
    await expect(platformEntitlements()).resolves.toEqual({
      usageSummary: {
        remaining: 450_000,
        label: 'credits',
        linkTarget: '/?from=editor#/account/billing',
      },
    });
  });

  it('returns undefined on the legacy ledger', async () => {
    stubBilling({ ledger: 'v1', total_remaining: 450_000 });
    await expect(platformEntitlements()).resolves.toBeUndefined();
  });

  it('returns undefined when total_remaining is not a number', async () => {
    stubBilling({ ledger: 'v2' });
    await expect(platformEntitlements()).resolves.toBeUndefined();
  });

  it('returns undefined on a non-ok response', async () => {
    stubBilling({}, 500);
    await expect(platformEntitlements()).resolves.toBeUndefined();
  });

  it('returns undefined when the request fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')));
    await expect(platformEntitlements()).resolves.toBeUndefined();
  });
});
