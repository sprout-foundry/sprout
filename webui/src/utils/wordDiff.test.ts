import { describe, expect, it } from 'vitest';
import { computeWordDiff } from './wordDiff';

describe('computeWordDiff', () => {
  it('marks only the changed token when one word differs', () => {
    const { del, add } = computeWordDiff('const foo = bar;', 'const foo = baz;');
    const delChanged = del
      .filter((p) => p.changed)
      .map((p) => p.text)
      .join('');
    const addChanged = add
      .filter((p) => p.changed)
      .map((p) => p.text)
      .join('');
    expect(delChanged).toBe('bar;');
    expect(addChanged).toBe('baz;');
  });

  it('leaves identical text fully unmarked', () => {
    const { del, add } = computeWordDiff('same text here', 'same text here');
    expect(del.every((p) => !p.changed)).toBe(true);
    expect(add.every((p) => !p.changed)).toBe(true);
  });

  it('handles pure insertion', () => {
    const { del, add } = computeWordDiff('one two', 'one two three');
    expect(del.every((p) => !p.changed)).toBe(true);
    const changed = add
      .filter((p) => p.changed)
      .map((p) => p.text)
      .join('');
    expect(changed.trim()).toBe('three');
  });

  it('handles pure deletion', () => {
    const { del, add } = computeWordDiff('one two three', 'one two');
    expect(add.every((p) => !p.changed)).toBe(true);
    const changed = del
      .filter((p) => p.changed)
      .map((p) => p.text)
      .join('');
    expect(changed.trim()).toBe('three');
  });

  it('marks everything changed when one side is empty', () => {
    const { del, add } = computeWordDiff('', 'new content');
    expect(del).toHaveLength(0);
    expect(add).toHaveLength(1);
    expect(add[0]).toMatchObject({ text: 'new content', changed: true });
  });

  it('round-trips the original text through the spans', () => {
    const oldText = 'function calculateTotal(items, taxRate) {';
    const newText = 'function calculateTotal(items, discountRate) {';
    const { del, add } = computeWordDiff(oldText, newText);
    expect(del.map((p) => p.text).join('')).toBe(oldText);
    expect(add.map((p) => p.text).join('')).toBe(newText);
  });

  it('falls back to whole-run marking for very large inputs', () => {
    const big = Array.from({ length: 2500 }, (_, i) => `word${i}`).join(' ');
    const { del, add } = computeWordDiff(big, big.replace(/word10;/, 'changed;'));
    // Should not throw; both sides still round-trip.
    expect(del.map((p) => p.text).join('')).toBe(big);
    expect(add.map((p) => p.text).join('')).toHaveLength(big.length);
  });
});
