import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

describe('resolveWebUIClientId boot race', () => {
  beforeEach(() => {
    vi.resetModules();
    window.sessionStorage.clear();
    window.localStorage.clear();
    document.cookie = 'sprout_client_id=; expires=Thu, 01 Jan 1970 00:00:00 GMT';
  });
  afterEach(() => vi.unstubAllGlobals());

  it('keeps the id the event socket stored while recovery awaited the server', async () => {
    let finishRecovery!: (r: Response) => void;
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>((resolve) => (finishRecovery = resolve))),
    );
    const { resolveWebUIClientId, getWebUIClientId } = await import('./clientSession');

    const resolving = resolveWebUIClientId();
    // Meanwhile the event WebSocket connects and mints the tab's id.
    const socketId = getWebUIClientId();
    finishRecovery(new Response(null, { status: 200 }));

    expect(await resolving).toBe(socketId);
    expect(getWebUIClientId()).toBe(socketId);
  });
});
