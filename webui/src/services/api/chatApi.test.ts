import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { retractSteer } from './chatApi';

describe('chatApi steer retraction', () => {
  let fetchCalls: Array<{ url: string; init: RequestInit }>;

  beforeEach(() => {
    fetchCalls = [];
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  const makeFetch = (status: number, body: unknown) => {
    return vi.fn(async (url: string, init?: RequestInit) => {
      fetchCalls.push({ url, init: init ?? {} });
      return {
        ok: status >= 200 && status < 300,
        status,
        json: async () => body,
      } as unknown as Response;
    });
  };

  it('retractSteer posts to the retract endpoint', async () => {
    const fetchFn = makeFetch(200, { success: true, message: 'fix typo plz' });
    const result = await retractSteer(fetchFn as unknown as typeof fetch, 'chat-1');

    expect(fetchCalls).toHaveLength(1);
    expect(fetchCalls[0].url).toBe('/api/query/steer/retract');
    expect(fetchCalls[0].init.method).toBe('POST');
    expect(JSON.parse(String(fetchCalls[0].init.body))).toEqual({ chat_id: 'chat-1' });
    expect(result).toEqual({ success: true, message: 'fix typo plz' });
  });

  it('retractSteer omits chat_id when no chat id given', async () => {
    const fetchFn = makeFetch(200, { success: false, message: '' });
    const result = await retractSteer(fetchFn as unknown as typeof fetch);

    expect(JSON.parse(String(fetchCalls[0].init.body))).toEqual({});
    expect(result).toEqual({ success: false, message: '' });
  });

  it('retractSteer throws on HTTP error', async () => {
    const fetchFn = makeFetch(500, { message: 'agent unavailable' });
    await expect(retractSteer(fetchFn as unknown as typeof fetch)).rejects.toThrow('agent unavailable');
  });
});
