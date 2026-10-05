import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { retractSteer, sendQuery } from './chatApi';

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

describe('chatApi sendQuery mode', () => {
  const okFetch = () =>
    vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, json: async () => ({}) }) as Response);

  it('sends the workspace mode so the server can prepare the agent', async () => {
    const fetchFn = okFetch();
    await sendQuery(fetchFn as unknown as typeof fetch, 'make a login screen', 'chat-1', 'design');
    expect(JSON.parse(String(fetchFn.mock.calls[0][1]?.body))).toEqual({
      query: 'make a login screen',
      chat_id: 'chat-1',
      mode: 'design',
    });
  });

  it('omits mode when none is given', async () => {
    const fetchFn = okFetch();
    await sendQuery(fetchFn as unknown as typeof fetch, 'hi');
    expect(JSON.parse(String(fetchFn.mock.calls[0][1]?.body))).toEqual({ query: 'hi' });
  });
});
