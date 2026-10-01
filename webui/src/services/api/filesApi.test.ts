import { describe, expect, it } from 'vitest';
import { getFiles } from './filesApi';

const respond = (status: number, body: unknown) =>
  (async () => new Response(JSON.stringify(body), { status })) as unknown as typeof fetch;

describe('getFiles', () => {
  it('carries the server error code on a refusal', async () => {
    await expect(getFiles(respond(403, { code: 'workspace_not_selected' }))).rejects.toMatchObject({
      message: 'Failed to fetch files',
      code: 'workspace_not_selected',
    });
  });

  it('returns the listing on success', async () => {
    await expect(getFiles(respond(200, { files: [] }))).resolves.toEqual({ files: [] });
  });
});
