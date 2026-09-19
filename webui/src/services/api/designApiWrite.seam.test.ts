/**
 * Safe-write seam client tests (SP-140-7 §7a, TODO item 7.1).
 */

import { describe, expect, it, vi } from 'vitest';
import { DesignWriteConflictError, writeAssetIfUnchanged } from './designApiWrite';

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

describe('writeAssetIfUnchanged', () => {
  it('sends baseMtime/baseHash and succeeds on 200', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(200, { success: true }));
    const result = await writeAssetIfUnchanged(globalThis.fetch, 'screens/login.html', '<p>x</p>', {
      writeFn: transport,
      baseMtime: 1788877200,
      baseHash: 'abc',
    });
    expect(result.path).toBe('design/screens/login.html');
    const [url, init] = transport.mock.calls[0];
    expect(String(url)).toContain('path=design%2Fscreens%2Flogin.html');
    const body = JSON.parse(String(init?.body));
    expect(body).toEqual({ content: '<p>x</p>', baseMtime: 1788877200, baseHash: 'abc' });
  });

  it('omitted guards are not sent (opt-in seam)', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(200, {}));
    await writeAssetIfUnchanged(globalThis.fetch, 'screens/login.html', '<p>x</p>', { writeFn: transport });
    const body = JSON.parse(String(transport.mock.calls[0][1]?.body));
    expect(body).toEqual({ content: '<p>x</p>' });
  });

  it('a 409 throws DesignWriteConflictError with the current revision', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse(409, {
        error: 'revision_conflict',
        path: 'design/screens/login.html',
        currentMtime: 42,
        currentHash: 'ff0',
      }),
    );
    await expect(
      writeAssetIfUnchanged(globalThis.fetch, 'screens/login.html', '<p>x</p>', { writeFn: transport, baseMtime: 41 }),
    ).rejects.toThrow(DesignWriteConflictError);
  });

  it('force bypasses the 409 (the §7b Keep-mine path)', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(409, { error: 'revision_conflict' }));
    await expect(
      writeAssetIfUnchanged(globalThis.fetch, 'screens/login.html', '<p>mine</p>', {
        writeFn: transport,
        baseMtime: 41,
        force: true,
      }),
    ).resolves.toMatchObject({ path: 'design/screens/login.html', content: '<p>mine</p>' });
  });

  it('an unparseable 409 body still conflicts', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response('<html>502</html>', { status: 409 }));
    await expect(
      writeAssetIfUnchanged(globalThis.fetch, 'screens/login.html', '<p>x</p>', { writeFn: transport }),
    ).rejects.toThrow(DesignWriteConflictError);
  });

  it('a non-409 failure still raises a plain error', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response('denied', { status: 403 }));
    await expect(
      writeAssetIfUnchanged(globalThis.fetch, 'screens/login.html', '<p>x</p>', { writeFn: transport }),
    ).rejects.toThrow(/Failed to write design asset/);
  });
});
