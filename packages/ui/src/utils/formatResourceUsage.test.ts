import { describe, it, expect } from 'vitest';
import { formatCost, formatTokens } from './formatResourceUsage';

describe('formatCost (SP-053-perTurnCost)', () => {
  it('formats zero cost as $0.0000', () => {
    expect(formatCost(0)).toBe('$0.0000');
  });

  it('formats small cost with 4 decimal places', () => {
    expect(formatCost(0.0034)).toBe('$0.0034');
  });

  it('formats cost greater than 1 dollar', () => {
    expect(formatCost(1.2345)).toBe('$1.2345');
  });

  it('formats exact round values', () => {
    expect(formatCost(0.12)).toBe('$0.1200');
  });

  it('pads to 4 decimal places for small values', () => {
    expect(formatCost(0.01)).toBe('$0.0100');
  });
});

describe('formatTokens (SP-053-perTurnCost)', () => {
  it('formats tokens under 1000 as plain number', () => {
    expect(formatTokens(500)).toBe('500');
  });

  it('formats zero tokens as "0"', () => {
    expect(formatTokens(0)).toBe('0');
  });

  it('formats exactly 1000 tokens as "1.0k"', () => {
    expect(formatTokens(1000)).toBe('1.0k');
  });

  it('formats tokens over 1000 with k suffix', () => {
    expect(formatTokens(5000)).toBe('5.0k');
  });

  it('formats 1200 tokens as "1.2k"', () => {
    expect(formatTokens(1200)).toBe('1.2k');
  });

  it('formats large token counts', () => {
    expect(formatTokens(15000)).toBe('15.0k');
  });

  it('formats token counts with fractional thousands', () => {
    expect(formatTokens(3450)).toBe('3.5k');
  });

  it('formats 999 tokens without k suffix', () => {
    expect(formatTokens(999)).toBe('999');
  });
});
