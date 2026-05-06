import { describe, it, expect } from 'vitest';
import { getNestedValue, setNestedValue } from './settingsHelpers';

describe('getNestedValue', () => {
  it('returns value for flat key', () => {
    const obj = { name: 'Alice', age: 30 };
    expect(getNestedValue(obj, 'name')).toBe('Alice');
    expect(getNestedValue(obj, 'age')).toBe(30);
  });

  it('returns value for nested key', () => {
    const obj = { a: { b: { c: 'deep' } } };
    expect(getNestedValue(obj, 'a.b.c')).toBe('deep');
  });

  it('returns empty string when path does not exist', () => {
    const obj = { a: { b: 1 } };
    expect(getNestedValue(obj, 'a.x')).toBe('');
    expect(getNestedValue(obj, 'a.b.y')).toBe('');
    expect(getNestedValue(obj, 'missing.key')).toBe('');
  });

  it('returns empty string for empty key', () => {
    const obj = { a: 1 };
    expect(getNestedValue(obj, '')).toBe('');
  });

  it('returns empty string when intermediate key is not an object', () => {
    const obj = { a: 'not an object' };
    expect(getNestedValue(obj, 'a.b')).toBe('');
  });

  it('handles deep nesting', () => {
    const obj = { a: { b: { c: { d: { e: 'bottom' } } } } };
    expect(getNestedValue(obj, 'a.b.c.d.e')).toBe('bottom');
  });

  it('returns intermediate value when path stops mid-way', () => {
    const obj = { a: { b: { c: 'val' } } };
    expect(getNestedValue(obj, 'a.b')).toEqual({ c: 'val' });
  });

  it('handles null and undefined values in the path', () => {
    const obj = { a: { b: null } };
    expect(getNestedValue(obj, 'a.b')).toBeNull();
    expect(getNestedValue(obj, 'a.b.c')).toBe('');

    const obj2 = { a: { b: undefined } };
    expect(getNestedValue(obj2, 'a.b')).toBeUndefined();
    expect(getNestedValue(obj2, 'a.b.c')).toBe('');
  });

  it('handles falsy values that are not null/undefined', () => {
    const obj = { a: { b: false, c: 0, d: '' } };
    expect(getNestedValue(obj, 'a.b')).toBe(false);
    expect(getNestedValue(obj, 'a.c')).toBe(0);
    expect(getNestedValue(obj, 'a.d')).toBe('');
  });

  it('handles array values', () => {
    const obj = { a: { b: [1, 2, 3] } };
    expect(getNestedValue(obj, 'a.b')).toEqual([1, 2, 3]);
  });
});

describe('setNestedValue', () => {
  it('sets value for flat key', () => {
    const obj = { a: 1, b: 2 };
    const result = setNestedValue(obj, 'a', 10);
    expect(result.a).toBe(10);
    expect(result.b).toBe(2);
    // original unchanged (immutable)
    expect(obj.a).toBe(1);
  });

  it('sets value at existing nested path', () => {
    const obj = { a: { b: { c: 'old' } } };
    const result = setNestedValue(obj, 'a.b.c', 'new');
    expect(result.a.b.c).toBe('new');
    // original unchanged
    expect((obj as any).a.b.c).toBe('old');
  });

  it('creates intermediate objects when missing', () => {
    const obj = { a: 1 };
    const result = setNestedValue(obj, 'b.c.d', 'deep');
    expect(result.b.c.d).toBe('deep');
    expect(result.a).toBe(1);
  });

  it('handles empty key (sets top-level empty-string key)', () => {
    const obj = { a: 1 };
    const result = setNestedValue(obj, '', 'val');
    expect(result['']).toBe('val');
    expect(result.a).toBe(1);
  });

  it('handles deep nesting creation', () => {
    const obj: Record<string, unknown> = {};
    const result = setNestedValue(obj, 'a.b.c.d.e', 'bottom');
    expect(result.a.b.c.d.e).toBe('bottom');
  });

  it('preserves sibling properties when setting nested value', () => {
    const obj = { a: { b: 1, c: 2 } };
    const result = setNestedValue(obj, 'a.b', 99);
    expect(result.a.b).toBe(99);
    expect(result.a.c).toBe(2);
  });

  it('replaces intermediate non-object with new object', () => {
    const obj = { a: 'not an object' };
    const result = setNestedValue(obj, 'a.b', 'val');
    expect(result.a.b).toBe('val');
    expect(typeof result.a).toBe('object');
  });

  it('does not mutate original object (full immutability)', () => {
    const obj = { a: { b: { c: 1, d: 2 } } };
    const result = setNestedValue(obj, 'a.b.c', 999);

    // Original deeply unchanged
    expect(obj.a.b.c).toBe(1);
    expect(obj.a.b.d).toBe(2);
    // Result changed only at target
    expect(result.a.b.c).toBe(999);
    expect(result.a.b.d).toBe(2);
  });

  it('shallow-copies intermediate objects', () => {
    const inner = { x: 1 };
    const obj = { a: { b: inner } };
    const result = setNestedValue(obj, 'a.b.x', 2);

    // Intermediate b is a new object (shallow copy), not the original
    expect(result.a.b).not.toBe(inner);
    expect(result.a.b.x).toBe(2);
  });

  it('handles setting various value types', () => {
    const obj: Record<string, unknown> = {};
    expect(setNestedValue(obj, 'a', null).a).toBeNull();
    expect(setNestedValue(obj, 'b', undefined).b).toBeUndefined();
    expect(setNestedValue(obj, 'c', false).c).toBe(false);
    expect(setNestedValue(obj, 'd', 0).d).toBe(0);
    expect(setNestedValue(obj, 'e', []).e).toEqual([]);
    expect(setNestedValue(obj, 'f', {}).f).toEqual({});
  });

  it('returns new top-level object even for same key', () => {
    const obj = { a: 1 };
    const result = setNestedValue(obj, 'a', 2);
    expect(result).not.toBe(obj);
    expect(result.a).toBe(2);
  });
});
