import { describe, it, expect, vi } from 'vitest';
import { getPendingEdit, submitEditDecision } from './editApprovalApi';

const makeFetch = (status: number, body: unknown) => {
  return vi.fn(async (url: string, init?: RequestInit) => {
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
    } as unknown as Response;
  });
};

describe('editApprovalApi', () => {
  it('getPendingEdit fetches the pending edit by id', async () => {
    const fetchFn = makeFetch(200, {
      id: 'edit-1',
      path: 'src/foo.ts',
      hunks: [{ id: 'h1', summary: 'adds helper', add_count: 3, del_count: 1, lines: [] }],
      unified_diff: '@@ ...',
      decided: false,
    });

    const result = await getPendingEdit(fetchFn as unknown as typeof fetch, 'edit-1');

    expect(fetchFn).toHaveBeenCalledTimes(1);
    expect(fetchFn).toHaveBeenCalledWith('/api/edits/edit-1');
    expect(result).toMatchObject({ id: 'edit-1', path: 'src/foo.ts', decided: false });
  });

  it('submitEditDecision posts the decision and returns the decision result', async () => {
    const fetchFn = makeFetch(200, { edit_id: 'edit-1', decided: true });

    const result = await submitEditDecision(fetchFn as unknown as typeof fetch, 'edit-1', {
      accepted_hunks: ['h1'],
      rejected: false,
    });

    expect(fetchFn).toHaveBeenCalledTimes(1);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/edits/edit-1/decision');
    expect(init.method).toBe('POST');
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(String(init.body))).toEqual({ accepted_hunks: ['h1'], rejected: false });
    expect(result).toEqual({ edit_id: 'edit-1', decided: true });
  });

  it('getPendingEdit surfaces the error message from the JSON body on HTTP error', async () => {
    const fetchFn = makeFetch(404, { message: 'edit not found' });

    await expect(getPendingEdit(fetchFn as unknown as typeof fetch, 'missing')).rejects.toThrow(
      'edit not found',
    );
  });

  it('submitEditDecision surfaces the error message from the JSON body on HTTP error', async () => {
    const fetchFn = makeFetch(500, { message: 'server exploded' });

    await expect(
      submitEditDecision(fetchFn as unknown as typeof fetch, 'edit-1', {
        accepted_hunks: [],
        rejected: true,
      }),
    ).rejects.toThrow('server exploded');
  });

  it('falls back to a message when the error body is not JSON', async () => {
    const fetchFn = vi.fn(async () => ({
      ok: false,
      status: 500,
      json: async () => {
        throw new Error('not json');
      },
    }) as unknown as typeof fetch);

    await expect(getPendingEdit(fetchFn, 'edit-1')).rejects.toThrow('Failed to fetch edit');
  });
});
