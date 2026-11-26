import { beforeEach, describe, expect, it } from 'vitest';
import { readMovedItem } from './movedStorageKey';

describe('readMovedItem', () => {
  beforeEach(() => window.localStorage.clear());

  it('moves a value saved under the earlier key, once', () => {
    window.localStorage.setItem('old', 'v');
    expect(readMovedItem(window.localStorage, 'new', 'old')).toBe('v');
    expect(window.localStorage.getItem('new')).toBe('v');
    expect(window.localStorage.getItem('old')).toBeNull();
  });

  it('prefers the current key and leaves the earlier one alone', () => {
    window.localStorage.setItem('new', 'current');
    window.localStorage.setItem('old', 'earlier');
    expect(readMovedItem(window.localStorage, 'new', 'old')).toBe('current');
    expect(window.localStorage.getItem('old')).toBe('earlier');
  });

  it('answers null when neither is set', () => {
    expect(readMovedItem(window.localStorage, 'new', 'old')).toBeNull();
  });
});
