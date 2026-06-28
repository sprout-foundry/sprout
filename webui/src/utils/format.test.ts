import { describe, it, expect, vi, beforeEach } from 'vitest';
import { formatRelativeDate, firstLine, classifyDiffLine, formatDollar } from './format';

describe('formatRelativeDate', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it("returns 'just now' for less than 60 seconds ago", () => {
    const now = new Date('2024-01-01T00:00:00Z');
    vi.setSystemTime(now);

    const thirtySeconds = new Date(now.getTime() - 30_000);
    expect(formatRelativeDate(thirtySeconds.toISOString())).toBe('just now');
  });

  it("returns 'just now' at exactly 59 seconds", () => {
    const now = new Date('2024-01-01T00:00:00Z');
    vi.setSystemTime(now);

    const fiftyNineSeconds = new Date(now.getTime() - 59_000);
    expect(formatRelativeDate(fiftyNineSeconds.toISOString())).toBe('just now');
  });

  it("returns 'Xm ago' for 1 to 59 minutes", () => {
    const now = new Date('2024-01-01T00:00:00Z');
    vi.setSystemTime(now);

    const oneMinute = new Date(now.getTime() - 60_000);
    expect(formatRelativeDate(oneMinute.toISOString())).toBe('1m ago');

    const thirtyMinutes = new Date(now.getTime() - 30 * 60_000);
    expect(formatRelativeDate(thirtyMinutes.toISOString())).toBe('30m ago');

    const fiftyNineMinutes = new Date(now.getTime() - 59 * 60_000);
    expect(formatRelativeDate(fiftyNineMinutes.toISOString())).toBe('59m ago');
  });

  it("returns 'Xh ago' for 1 to 23 hours", () => {
    const now = new Date('2024-01-01T00:00:00Z');
    vi.setSystemTime(now);

    const oneHour = new Date(now.getTime() - 60 * 60_000);
    expect(formatRelativeDate(oneHour.toISOString())).toBe('1h ago');

    const twelveHours = new Date(now.getTime() - 12 * 60 * 60_000);
    expect(formatRelativeDate(twelveHours.toISOString())).toBe('12h ago');

    const twentyThreeHours = new Date(now.getTime() - 23 * 60 * 60_000);
    expect(formatRelativeDate(twentyThreeHours.toISOString())).toBe('23h ago');
  });

  it("returns 'Xd ago' for 1 to 6 days", () => {
    const now = new Date('2024-01-01T00:00:00Z');
    vi.setSystemTime(now);

    const oneDay = new Date(now.getTime() - 24 * 60 * 60_000);
    expect(formatRelativeDate(oneDay.toISOString())).toBe('1d ago');

    const threeDays = new Date(now.getTime() - 3 * 24 * 60 * 60_000);
    expect(formatRelativeDate(threeDays.toISOString())).toBe('3d ago');

    const sixDays = new Date(now.getTime() - 6 * 24 * 60 * 60_000);
    expect(formatRelativeDate(sixDays.toISOString())).toBe('6d ago');
  });

  it("returns 'Xw ago' for 1 to 4 weeks (7-34 days)", () => {
    const now = new Date('2024-01-01T00:00:00Z');
    vi.setSystemTime(now);

    const oneWeek = new Date(now.getTime() - 7 * 24 * 60 * 60_000);
    expect(formatRelativeDate(oneWeek.toISOString())).toBe('1w ago');

    const threeWeeks = new Date(now.getTime() - 21 * 24 * 60 * 60_000);
    expect(formatRelativeDate(threeWeeks.toISOString())).toBe('3w ago');
  });

  it("returns 'Xmo ago' for 1 to 11 months (35-359 days)", () => {
    const now = new Date('2024-06-01T00:00:00Z');
    vi.setSystemTime(now);

    // ~35 days (just over 5 weeks threshold)
    const thirtyFiveDays = new Date(now.getTime() - 35 * 24 * 60 * 60_000);
    expect(formatRelativeDate(thirtyFiveDays.toISOString())).toBe('1mo ago');

    // ~60 days = 2mo
    const sixtyDays = new Date(now.getTime() - 60 * 24 * 60 * 60_000);
    expect(formatRelativeDate(sixtyDays.toISOString())).toBe('2mo ago');

    // ~330 days = 11mo
    const threeHundredThirtyDays = new Date(now.getTime() - 330 * 24 * 60 * 60_000);
    expect(formatRelativeDate(threeHundredThirtyDays.toISOString())).toBe('11mo ago');
  });

  it("returns 'Xy ago' for 12+ months (365+ days)", () => {
    const now = new Date('2024-06-01T00:00:00Z');
    vi.setSystemTime(now);

    const oneYear = new Date(now.getTime() - 365 * 24 * 60 * 60_000);
    expect(formatRelativeDate(oneYear.toISOString())).toBe('1y ago');

    const twoYears = new Date(now.getTime() - 730 * 24 * 60 * 60_000);
    expect(formatRelativeDate(twoYears.toISOString())).toBe('2y ago');
  });

  it('returns original string for invalid dates', () => {
    expect(formatRelativeDate('not-a-date')).toBe('not-a-date');
    expect(formatRelativeDate('')).toBe('');
  });

  it('handles numeric strings (behavior varies by JS engine)', () => {
    // new Date('123') may parse as year 123 AD (producing "Xy ago") or
    // return Invalid Date (returning the original string), depending on engine.
    const result = formatRelativeDate('123');
    const isValid = !isNaN(new Date('123').getTime());
    if (isValid) {
      expect(result).toMatch(/\d+y ago/);
    } else {
      expect(result).toBe('123');
    }
  });

  it('returns original string for catch-all errors', () => {
    expect(formatRelativeDate('abc-def-ghi')).toBe('abc-def-ghi');
  });
});

describe('firstLine', () => {
  it('returns the full string when no newline exists', () => {
    expect(firstLine('single line')).toBe('single line');
  });

  it('returns text before the first newline', () => {
    expect(firstLine('first\nsecond\nthird')).toBe('first');
  });

  it('returns empty string for empty input', () => {
    expect(firstLine('')).toBe('');
  });

  it('returns empty string when input starts with newline', () => {
    expect(firstLine('\nsecond')).toBe('');
  });

  it('handles string with only a newline', () => {
    expect(firstLine('\n')).toBe('');
  });

  it('handles trailing newline with content before it', () => {
    expect(firstLine('hello\n')).toBe('hello');
  });
});

describe('classifyDiffLine', () => {
  it("returns 'file' for +++ prefixed lines", () => {
    expect(classifyDiffLine('+++ b/file.txt')).toBe('file');
    expect(classifyDiffLine('+++ text')).toBe('file');
  });

  it("returns 'file' for --- prefixed lines", () => {
    expect(classifyDiffLine('--- a/file.txt')).toBe('file');
    expect(classifyDiffLine('--- text')).toBe('file');
  });

  it("returns 'hunk' for @@ prefixed lines", () => {
    expect(classifyDiffLine('@@ -1,3 +1,5 @@')).toBe('hunk');
    expect(classifyDiffLine('@@ something @@')).toBe('hunk');
  });

  it("returns 'add' for + prefixed lines (not +++ or ---)", () => {
    expect(classifyDiffLine('+added line')).toBe('add');
    expect(classifyDiffLine('+')).toBe('add');
  });

  it("returns 'del' for - prefixed lines (not ---)", () => {
    expect(classifyDiffLine('-removed line')).toBe('del');
    expect(classifyDiffLine('-')).toBe('del');
  });

  it("returns 'context' for lines without special prefixes", () => {
    expect(classifyDiffLine(' context line')).toBe('context');
    expect(classifyDiffLine('regular text')).toBe('context');
    expect(classifyDiffLine('')).toBe('context');
  });

  it("handles '---' taking precedence over '-' classification", () => {
    expect(classifyDiffLine('--- header')).toBe('file');
  });

  it("handles '+++' taking precedence over '+' classification", () => {
    expect(classifyDiffLine('+++ header')).toBe('file');
  });
});

describe('formatDollar', () => {
  it('formats with 4 decimal places and dollar sign', () => {
    expect(formatDollar(1.2345)).toBe('$1.2345');
  });

  it('formats zero correctly', () => {
    expect(formatDollar(0)).toBe('$0.0000');
  });

  it('pads short decimals', () => {
    expect(formatDollar(5)).toBe('$5.0000');
  });

  it('truncates long decimals', () => {
    expect(formatDollar(1.23456789)).toBe('$1.2346');
  });
});
