// @ts-nocheck

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
  TERMINAL_HEIGHT_DEFAULT,
  TERMINAL_HEIGHT_MIN,
  TERMINAL_HEIGHT_STORAGE_KEY,
  parseTerminalHeight,
  clampTerminalHeight,
  FONT_SIZE_MIN,
  FONT_SIZE_MAX,
  FONT_SIZE_STORAGE_KEY,
  parseFontSize,
  clampFontSize,
  COPY_ON_SELECT_STORAGE_KEY,
  parseCopyOnSelect,
} from './terminalPref';
import { FONT_SIZE_DEFAULT } from './terminalConstants';

describe('terminalPref — constants', () => {
  it('exports expected terminal-height values', () => {
    expect(TERMINAL_HEIGHT_DEFAULT).toBe(400);
    expect(TERMINAL_HEIGHT_MIN).toBe(120);
    expect(TERMINAL_HEIGHT_STORAGE_KEY).toBe('sprout-terminal-height');
  });

  it('exports expected font-size values', () => {
    expect(FONT_SIZE_MIN).toBe(8);
    expect(FONT_SIZE_MAX).toBe(32);
    expect(FONT_SIZE_STORAGE_KEY).toBe('sprout-terminal-font-size');
  });

  it('exports the copy-on-select storage key', () => {
    expect(COPY_ON_SELECT_STORAGE_KEY).toBe('sprout-terminal-copy-on-select');
  });
});

describe('parseTerminalHeight', () => {
  it('returns the default for null', () => {
    expect(parseTerminalHeight(null)).toBe(TERMINAL_HEIGHT_DEFAULT);
  });
  it('returns the default for empty string', () => {
    expect(parseTerminalHeight('')).toBe(TERMINAL_HEIGHT_DEFAULT);
  });
  it('returns the default for non-numeric input', () => {
    expect(parseTerminalHeight('not-a-number')).toBe(TERMINAL_HEIGHT_DEFAULT);
  });
  it('parses a valid numeric string', () => {
    expect(parseTerminalHeight('250')).toBe(250);
  });
  it('parses a float-valued numeric string', () => {
    expect(parseTerminalHeight('333.7')).toBe(333.7);
  });
});

describe('clampTerminalHeight', () => {
  it('returns the default for NaN', () => {
    expect(clampTerminalHeight(NaN)).toBe(TERMINAL_HEIGHT_DEFAULT);
  });
  it('returns the default for non-finite values', () => {
    expect(clampTerminalHeight(Infinity)).toBe(TERMINAL_HEIGHT_DEFAULT);
    expect(clampTerminalHeight(-Infinity)).toBe(TERMINAL_HEIGHT_DEFAULT);
  });
  it('clamps to TERMINAL_HEIGHT_MIN when too small', () => {
    expect(clampTerminalHeight(50)).toBe(TERMINAL_HEIGHT_MIN);
  });
  it('clamps to viewport-innerHeight minus MAX_FACTOR (100) when too large', () => {
    // jsdom default window.innerHeight is 768 → 768 - 100 = 668
    expect(clampTerminalHeight(99999)).toBe(window.innerHeight - 100);
  });
  it('passes through values within bounds', () => {
    expect(clampTerminalHeight(400)).toBe(400);
  });
});

describe('parseFontSize', () => {
  it('returns the default for null', () => {
    expect(parseFontSize(null)).toBe(FONT_SIZE_DEFAULT);
  });
  it('returns the default for empty string', () => {
    expect(parseFontSize('')).toBe(FONT_SIZE_DEFAULT);
  });
  it('returns the default for non-numeric input', () => {
    expect(parseFontSize('garbage')).toBe(FONT_SIZE_DEFAULT);
  });
  it('parses a valid numeric string', () => {
    expect(parseFontSize('14')).toBe(14);
  });
  it('parses a float-valued font size', () => {
    expect(parseFontSize('13.5')).toBe(13.5);
  });
});

describe('clampFontSize', () => {
  it('returns the default for NaN', () => {
    expect(clampFontSize(NaN)).toBe(FONT_SIZE_DEFAULT);
  });
  it('clamps to FONT_SIZE_MIN when too small', () => {
    expect(clampFontSize(7)).toBe(FONT_SIZE_MIN);
  });
  it('clamps to FONT_SIZE_MAX when too large', () => {
    expect(clampFontSize(100)).toBe(FONT_SIZE_MAX);
  });
  it('passes through values within bounds', () => {
    expect(clampFontSize(14)).toBe(14);
  });
});

describe('parseCopyOnSelect', () => {
  it('returns the fallback when input is null', () => {
    expect(parseCopyOnSelect(null, true)).toBe(true);
    expect(parseCopyOnSelect(null, false)).toBe(false);
  });
  it('parses "true" literally', () => {
    expect(parseCopyOnSelect('true', false)).toBe(true);
  });
  it('parses "false" literally', () => {
    expect(parseCopyOnSelect('false', true)).toBe(false);
  });
  it('treats "TRUE" (uppercase) as not-true (case-sensitive on purpose)', () => {
    expect(parseCopyOnSelect('TRUE', false)).toBe(false);
  });
  it('treats empty string as not-true (only "true" matches)', () => {
    expect(parseCopyOnSelect('', true)).toBe(false);
  });
});

describe('window-environment edge cases', () => {
  let originalWindow: any;

  beforeEach(() => {
    originalWindow = (globalThis as any).window;
  });
  afterEach(() => {
    (globalThis as any).window = originalWindow;
  });

  it('clampTerminalHeight returns the default when window is undefined (SSR)', () => {
    (globalThis as any).window = undefined;
    expect(clampTerminalHeight(999)).toBe(TERMINAL_HEIGHT_DEFAULT);
  });
});
