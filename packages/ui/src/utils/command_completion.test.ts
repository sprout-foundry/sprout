import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  ARGUMENT_COMPLETION_DEBOUNCE_MS,
  argumentCandidatesFromResponse,
  createDebouncer,
  createHttpCommandCompletionApi,
  replaceLastWord,
  type CommandCompletionResponse,
} from './command_completion';

function mockFetchResponse(body: unknown = {}, ok = true) {
  return vi.fn().mockResolvedValue({
    ok,
    status: ok ? 200 : 500,
    json: async () => body,
  } as Response);
}

describe('createHttpCommandCompletionApi', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('POSTs the command to /api/command/complete and parses the response', async () => {
    const fetchMock = mockFetchResponse({
      command: 'risk-profile',
      completions: [{ text: 'permissive', description: '' }],
    });
    const api = createHttpCommandCompletionApi(fetchMock as unknown as typeof fetch);

    const result = await api.completeCommand('/risk-profile per');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith('/api/command/complete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ command: '/risk-profile per' }),
    });
    expect(result).toEqual({
      command: 'risk-profile',
      completions: [{ text: 'permissive', description: '' }],
    });
  });

  it('returns an empty result on a non-OK status without throwing', async () => {
    const fetchMock = mockFetchResponse({ error: 'boom' }, false);
    const api = createHttpCommandCompletionApi(fetchMock as unknown as typeof fetch);

    const result = await api.completeCommand('/risk-profile per');

    expect(result).toEqual({ command: '', completions: [] });
    // The response body is never parsed on error.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('returns an empty result when fetch rejects without throwing', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError('network down'));
    const api = createHttpCommandCompletionApi(fetchMock as unknown as typeof fetch);

    const result = await api.completeCommand('/risk-profile per');

    expect(result).toEqual({ command: '', completions: [] });
  });

  it('returns an empty result on malformed JSON without throwing', async () => {
    const fetchMock = mockFetchResponse({ completions: 'not-an-array' });
    const api = createHttpCommandCompletionApi(fetchMock as unknown as typeof fetch);

    const result = await api.completeCommand('/model');

    expect(result).toEqual({ command: '', completions: [] });
  });

  it('prefixes the endpoint with baseUrl', async () => {
    const fetchMock = mockFetchResponse({ command: '', completions: [] });
    const api = createHttpCommandCompletionApi(fetchMock as unknown as typeof fetch, '/proxy');

    await api.completeCommand('/model');

    expect(fetchMock).toHaveBeenCalledWith('/proxy/api/command/complete', expect.anything());
  });

  it('forwards an AbortSignal to fetch when provided', async () => {
    const fetchMock = mockFetchResponse({ command: '', completions: [] });
    const api = createHttpCommandCompletionApi(fetchMock as unknown as typeof fetch);
    const controller = new AbortController();

    await api.completeCommand('/model', controller.signal);

    expect(fetchMock).toHaveBeenCalledWith('/api/command/complete', expect.objectContaining({ signal: controller.signal }));
  });
});

describe('argumentCandidatesFromResponse', () => {
  it('maps argument completions to isArgument SlashCommands', () => {
    const result = argumentCandidatesFromResponse({
      command: 'risk-profile',
      completions: [
        { text: 'permissive', description: '' },
        { text: 'readonly', description: 'ignored for args' },
      ],
    });
    expect(result).toEqual([
      { name: 'permissive', description: '', isArgument: true },
      { name: 'readonly', description: 'ignored for args', isArgument: true },
    ]);
  });

  it('filters empty/malformed entries', () => {
    const result = argumentCandidatesFromResponse({
      command: 'risk-profile',
      completions: [
        { text: '  ', description: '' },
        { text: '', description: '' },
        { text: 'cautious', description: '' },
      ],
    });
    expect(result).toEqual([{ name: 'cautious', description: '', isArgument: true }]);
  });

  it('returns an empty array for null/undefined/missing completions', () => {
    expect(argumentCandidatesFromResponse(null)).toEqual([]);
    expect(argumentCandidatesFromResponse(undefined)).toEqual([]);
    expect(argumentCandidatesFromResponse({ command: '', completions: [] })).toEqual([]);
    expect(
      argumentCandidatesFromResponse({ command: '', completions: undefined } as unknown as CommandCompletionResponse),
    ).toEqual([]);
  });
});

describe('replaceLastWord', () => {
  it('replaces the last whitespace-delimited word and appends a trailing space', () => {
    expect(replaceLastWord('/risk-profile per', 'permissive')).toEqual({
      value: '/risk-profile permissive ',
      cursor: 25,
    });
  });

  it('appends the candidate when the text already ends with a space', () => {
    expect(replaceLastWord('/risk-profile ', 'permissive')).toEqual({
      value: '/risk-profile permissive ',
      cursor: 25,
    });
  });

  it('replaces the only word when there is no space', () => {
    expect(replaceLastWord('per', 'permissive')).toEqual({ value: 'permissive ', cursor: 11 });
  });

  it('handles tabs as word delimiters', () => {
    expect(replaceLastWord('/risk-profile\tper', 'permissive')).toEqual({
      value: '/risk-profile\tpermissive ',
      cursor: 25,
    });
  });

  it('handles an empty candidate', () => {
    expect(replaceLastWord('/risk-profile per', '')).toEqual({ value: '/risk-profile  ', cursor: 15 });
  });
});

describe('createDebouncer', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('coalesces repeated calls into one trailing invocation', () => {
    vi.useFakeTimers();
    const debouncer = createDebouncer(ARGUMENT_COMPLETION_DEBOUNCE_MS);
    const fn = vi.fn();

    debouncer.debounce(fn);
    vi.advanceTimersByTime(50);
    debouncer.debounce(fn);
    vi.advanceTimersByTime(50);
    debouncer.debounce(fn);

    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(ARGUMENT_COMPLETION_DEBOUNCE_MS);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('cancel() prevents a pending invocation', () => {
    vi.useFakeTimers();
    const debouncer = createDebouncer(ARGUMENT_COMPLETION_DEBOUNCE_MS);
    const fn = vi.fn();

    debouncer.debounce(fn);
    debouncer.cancel();
    vi.advanceTimersByTime(ARGUMENT_COMPLETION_DEBOUNCE_MS + 100);
    expect(fn).not.toHaveBeenCalled();
  });
});
