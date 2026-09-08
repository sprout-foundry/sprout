/**
 * safeJsonParse tests — WebUI boundary hardening.
 *
 * The helpers must never throw: bad input, empty input, and non-string input
 * all degrade to the caller's fallback.
 */

import { describe, expect, it, vi, afterEach } from 'vitest';
import { safeJsonObject, safeJsonParse, safeJsonParseOrNull } from './json';
import { debugLog } from './log';

vi.mock('./log', () => ({ debugLog: vi.fn() }));

describe('safeJsonParse', () => {
  afterEach(() => vi.clearAllMocks());

  it('parses valid JSON', () => {
    expect(safeJsonParse('{"a":1}', {})).toEqual({ a: 1 });
    expect(safeJsonParse('[1,2]', [])).toEqual([1, 2]);
    expect(safeJsonParse('"text"', '')).toBe('text');
    expect(safeJsonParse('42', 0)).toBe(42);
  });

  it('returns the fallback for invalid JSON', () => {
    expect(safeJsonParse('not json', { default: true })).toEqual({ default: true });
    expect(safeJsonParse('{"a":', null)).toBeNull();
    expect(safeJsonParse('undefined', [])).toEqual([]);
  });

  it('returns the fallback for empty / whitespace-only strings', () => {
    expect(safeJsonParse('', null)).toBeNull();
    expect(safeJsonParse('   ', { a: 1 })).toEqual({ a: 1 });
  });

  it('returns the fallback for non-string input (missing storage entry, unset bridge field)', () => {
    expect(safeJsonParse(undefined, { a: 1 })).toEqual({ a: 1 });
    expect(safeJsonParse(null, [])).toEqual([]);
    expect(safeJsonParse(123, 'x')).toBe('x');
    expect(safeJsonParse({ already: 'an object' }, {})).toEqual({});
  });

  it('logs the failure via debugLog instead of throwing', () => {
    safeJsonParse('{oops', null);
    expect(debugLog).toHaveBeenCalled();
  });

  it('truncates the logged input so a huge malformed payload does not flood the console', () => {
    safeJsonParse('{oops' + 'x'.repeat(10_000), null);
    const call = vi.mocked(debugLog).mock.calls[0];
    const loggedInput = String(call[2] ?? '');
    // `input=` prefix (6) + 200-char slice + ellipsis (1) — well under the
    // 10,000-char payload.
    expect(loggedInput.length).toBeLessThanOrEqual(207);
    expect(loggedInput.endsWith('…')).toBe(true);
  });
});

describe('safeJsonParseOrNull', () => {
  it('returns parsed value or null', () => {
    expect(safeJsonParseOrNull('{"a":1}')).toEqual({ a: 1 });
    expect(safeJsonParseOrNull('nope')).toBeNull();
    expect(safeJsonParseOrNull(undefined)).toBeNull();
  });
});

describe('safeJsonObject', () => {
  it('returns the object for a valid JSON object', () => {
    expect(safeJsonObject('{"a":1}')).toEqual({ a: 1 });
  });

  it('returns the fallback for valid JSON that is not an object', () => {
    expect(safeJsonObject('[1,2]')).toEqual({});
    expect(safeJsonObject('[1,2]', { keep: true })).toEqual({ keep: true });
    expect(safeJsonObject('"str"', {})).toEqual({});
    expect(safeJsonObject('null', {})).toEqual({});
  });

  it('returns the fallback for invalid JSON', () => {
    expect(safeJsonObject('{{', { fallback: 1 })).toEqual({ fallback: 1 });
  });
});
