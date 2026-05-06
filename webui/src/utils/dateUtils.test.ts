import { describe, it, expect } from 'vitest';
import { parseDate } from './dateUtils';

describe('parseDate', () => {
  it('returns Date instances as-is', () => {
    const d = new Date('2024-06-15T12:00:00Z');
    expect(parseDate(d)).toBe(d);
  });

  it('parses valid ISO date strings', () => {
    const result = parseDate('2024-01-15T08:30:00Z');
    expect(result).toBeInstanceOf(Date);
    expect(result.getTime()).toBe(new Date('2024-01-15T08:30:00Z').getTime());
  });

  it('parses valid numeric timestamps', () => {
    const timestamp = 1700000000000;
    const result = parseDate(timestamp);
    expect(result).toBeInstanceOf(Date);
    expect(result.getTime()).toBe(timestamp);
  });

  it('parses valid date string without time', () => {
    const result = parseDate('2024-06-01');
    expect(result).toBeInstanceOf(Date);
    expect(Number.isNaN(result.getTime())).toBe(false);
  });

  it('returns new Date() for null', () => {
    const result = parseDate(null);
    expect(result).toBeInstanceOf(Date);
  });

  it('returns new Date() for undefined', () => {
    const result = parseDate(undefined);
    expect(result).toBeInstanceOf(Date);
  });

  it('returns new Date() for empty string', () => {
    const result = parseDate('');
    expect(result).toBeInstanceOf(Date);
  });

  it('returns new Date() (current time) for invalid date strings', () => {
    const result = parseDate('not-a-date');
    expect(result).toBeInstanceOf(Date);
    // parseDate returns new Date() for invalid values, which is current time (not NaN)
    expect(Number.isNaN(result.getTime())).toBe(false);
  });

  it('returns new Date() for garbage strings', () => {
    const result = parseDate('abc-def-ghi');
    expect(result).toBeInstanceOf(Date);
  });

  it('returns new Date() for objects', () => {
    const result = parseDate({ foo: 'bar' });
    expect(result).toBeInstanceOf(Date);
  });

  it('returns new Date() for arrays', () => {
    const result = parseDate([1, 2, 3]);
    expect(result).toBeInstanceOf(Date);
  });

  it('returns new Date() for boolean values', () => {
    expect(parseDate(true)).toBeInstanceOf(Date);
    expect(parseDate(false)).toBeInstanceOf(Date);
  });

  it('returns new Date() for NaN', () => {
    const result = parseDate(NaN);
    expect(result).toBeInstanceOf(Date);
  });

  it('handles negative timestamps', () => {
    const result = parseDate(-1000);
    expect(result).toBeInstanceOf(Date);
    expect(result.getTime()).toBe(-1000);
  });

  it('handles zero timestamp', () => {
    const result = parseDate(0);
    expect(result).toBeInstanceOf(Date);
    expect(result.getTime()).toBe(0);
  });
});
