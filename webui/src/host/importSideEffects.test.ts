/**
 * Importing the host must perform no fetch (SP-160 §160b §"No platform
 * calls on import").
 *
 * The host entry is part of the package's static import graph: a host that
 * imports `@sprout-foundry/workspace` (and the web UI's own entry that
 * imports the host) evaluates these modules eagerly. Any fetch at module
 * scope — the cloud host resolving its entitlements, or a platform-URL
 * helper reaching the bootstrap adapter — would then run for every importer,
 * in the local build included.
 *
 * These tests import the modules fresh (`vi.resetModules()`) with `global.fetch`
 * stubbed, and assert nothing is fetched on import. Then they prove the
 * entitlements fetch still happens — but only when the consumer asks
 * (`resolve()`), exactly once per call.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

/** A fetch spy shaped like the billing-status response the host expects. */
function billingFetchSpy() {
  return vi.fn(async () => new Response(JSON.stringify({ ledger: 'v2', total_remaining: 12_000 }), { status: 200 }));
}

describe('importing the host performs no fetch', () => {
  let fetchSpy: ReturnType<typeof billingFetchSpy>;

  beforeEach(() => {
    fetchSpy = billingFetchSpy();
    vi.stubGlobal('fetch', fetchSpy);
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('importing ./cloudHost makes no fetch', async () => {
    const mod = await import('./cloudHost');
    expect(mod.cloudHost).toBeDefined();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('importing the host barrel (./index) makes no fetch', async () => {
    const mod = await import('./index');
    // The barrel carries the contract; the platform implementation is not part
    // of it (it lives in the internal platform module).
    expect(mod.localHost).toBeDefined();
    expect(mod.useHost).toBeTypeOf('function');
    expect(mod.setActiveHost).toBeTypeOf('function');
    expect((mod as Record<string, unknown>).cloudHost).toBeUndefined();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('importing the internal platform module makes no fetch', async () => {
    const mod = await import('./platform');
    expect(mod.cloudHost).toBeDefined();
    expect(mod.platformHref).toBeTypeOf('function');
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('importing ./platformUrl makes no fetch', async () => {
    const mod = await import('./platformUrl');
    expect(mod.platformHref).toBeTypeOf('function');
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('entitlements resolve lazily — resolve() fetches exactly once', async () => {
    const { cloudHost } = await import('./cloudHost');
    expect(fetchSpy).not.toHaveBeenCalled();

    await cloudHost.entitlements?.resolve?.();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(String(fetchSpy.mock.calls[0][0])).toContain('/billing/status');
    expect(cloudHost.entitlements?.usageSummary?.remaining).toBe(12_000);
  });

  it('the mount path (resolve) is the only trigger — a second resolve is one more fetch', async () => {
    const { cloudHost } = await import('./cloudHost');
    expect(fetchSpy).not.toHaveBeenCalled();
    await cloudHost.entitlements?.resolve?.();
    await cloudHost.entitlements?.resolve?.();
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });
});
