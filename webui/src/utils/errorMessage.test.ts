/**
 * toUserErrorMessage tests — error presentation hardening.
 *
 * Panels and toasts must show one short human line, never a stack trace or
 * a raw `Error: ` prefix.
 */

import { describe, expect, it } from 'vitest';
import { toUserErrorMessage } from './errorMessage';

describe('toUserErrorMessage', () => {
  it('uses the Error message', () => {
    expect(toUserErrorMessage(new Error('permission denied'), 'fallback')).toBe('permission denied');
  });

  it('strips a leading "Error: " prefix', () => {
    expect(toUserErrorMessage(new Error('Error: permission denied'), 'fallback')).toBe('permission denied');
    expect(toUserErrorMessage(new Error('TypeError: bad thing'), 'fallback')).toBe('bad thing');
    expect(toUserErrorMessage(new Error('Uncaught RangeError: bad thing'), 'fallback')).toBe('bad thing');
  });

  it('keeps only the first line — never a stack trace', () => {
    const err = new Error('network unreachable');
    err.message = 'network unreachable\n    at fetch (app.js:1:1)\n    at send (app.js:9:9)';
    expect(toUserErrorMessage(err, 'fallback')).toBe('network unreachable');
  });

  it('returns the fallback when the message is empty or whitespace', () => {
    expect(toUserErrorMessage(new Error(''), 'fallback')).toBe('fallback');
    expect(toUserErrorMessage(new Error('   \n  '), 'fallback')).toBe('fallback');
    expect(toUserErrorMessage(undefined, 'fallback')).toBe('fallback');
    expect(toUserErrorMessage(null, 'fallback')).toBe('fallback');
    expect(toUserErrorMessage(42, 'fallback')).toBe('fallback');
    expect(toUserErrorMessage({ weird: 'object' }, 'fallback')).toBe('fallback');
  });

  it('uses a thrown string as-is (first line only)', () => {
    expect(toUserErrorMessage('plain failure', 'fallback')).toBe('plain failure');
    expect(toUserErrorMessage('line one\nline two', 'fallback')).toBe('line one');
    expect(toUserErrorMessage('', 'fallback')).toBe('fallback');
  });

  it('truncates very long messages', () => {
    const long = 'x'.repeat(500);
    const out = toUserErrorMessage(new Error(long), 'fallback');
    expect(out.length).toBeLessThanOrEqual(301);
    expect(out.endsWith('…')).toBe(true);
  });

  it('only strips a leading prefix — a mid-message "Error:" is preserved', () => {
    expect(toUserErrorMessage(new Error('Server Error: upstream failed'), 'fallback')).toBe(
      'Server Error: upstream failed',
    );
  });
});
