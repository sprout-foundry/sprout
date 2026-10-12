import type { GitHttpRequest, HttpClient } from 'isomorphic-git';
import { describe, expect, it } from 'vitest';
import { CloneTooLargeError, byteLimitedHttp } from './gitHttpLimit';

function serving(...chunkSizes: number[]): HttpClient {
  return {
    async request(req: GitHttpRequest) {
      async function* body() {
        for (const n of chunkSizes) yield new Uint8Array(n);
      }
      return { url: req.url, method: req.method, headers: {}, statusCode: 200, statusMessage: 'OK', body: body() };
    },
  };
}

async function drain(client: HttpClient): Promise<number> {
  const res = await client.request({ url: 'https://proxy.test/x' });
  let n = 0;
  for await (const chunk of res.body!) n += chunk.byteLength;
  return n;
}

describe('byteLimitedHttp', () => {
  it('passes downloads under the limit through unchanged', async () => {
    const client = byteLimitedHttp(serving(10, 20), 100);
    expect(await drain(client)).toBe(30);
    expect(client.exceeded).toBe(false);
  });

  it('cuts a download off once the total across requests passes the limit', async () => {
    const client = byteLimitedHttp(serving(40, 40), 100);
    expect(await drain(client)).toBe(80);
    await expect(drain(client)).rejects.toBeInstanceOf(CloneTooLargeError);
    expect(client.exceeded).toBe(true);
  });
});
