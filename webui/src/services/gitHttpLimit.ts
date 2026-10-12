import type { GitHttpRequest, GitHttpResponse, HttpClient } from 'isomorphic-git';

/** Largest packfile an in-browser clone downloads before giving up. */
export const MAX_CLONE_BYTES = 200 * 1024 * 1024;

/** A clone that would download more than the limit; the import falls back to files. */
export class CloneTooLargeError extends Error {
  constructor(readonly limitBytes: number) {
    super(`repository is larger than the in-browser clone limit (${Math.round(limitBytes / (1024 * 1024))} MB)`);
    this.name = 'CloneTooLargeError';
  }
}

/** An HTTP client whose downloads stop at a byte limit. */
export interface ByteLimitedHttp extends HttpClient {
  /** Whether a download passed the limit (isomorphic-git may report that as another error). */
  readonly exceeded: boolean;
}

/**
 * Wrap an isomorphic-git HTTP client so the response bodies of one operation
 * together stop at `maxBytes`: the download is cut off as soon as it passes
 * the limit instead of filling IndexedDB with a repository the page can't use.
 */
export function byteLimitedHttp(client: HttpClient, maxBytes: number): ByteLimitedHttp {
  let received = 0;
  let exceeded = false;
  async function* limited(body: AsyncIterableIterator<Uint8Array>): AsyncIterableIterator<Uint8Array> {
    for await (const chunk of body) {
      received += chunk.byteLength;
      if (received > maxBytes) {
        exceeded = true;
        throw new CloneTooLargeError(maxBytes);
      }
      yield chunk;
    }
  }
  return {
    get exceeded() {
      return exceeded;
    },
    async request(req: GitHttpRequest): Promise<GitHttpResponse> {
      const res = await client.request(req);
      return res.body ? { ...res, body: limited(res.body) } : res;
    },
  };
}
