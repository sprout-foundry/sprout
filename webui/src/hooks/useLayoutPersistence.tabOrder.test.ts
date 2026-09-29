import { describe, expect, it } from 'vitest';
import { mergeTabOrder } from './useLayoutPersistence';

describe('mergeTabOrder', () => {
  it('keeps the slots of tabs that have not reopened yet', () => {
    expect(mergeTabOrder(['chat/a', 'x.ts', 'chat/b'], ['x.ts'])).toEqual(['chat/a', 'x.ts', 'chat/b']);
  });

  it('records a drag among open tabs', () => {
    expect(mergeTabOrder(['a', 'b', 'c'], ['c', 'a', 'b'])).toEqual(['c', 'a', 'b']);
  });

  it('appends tabs the saved order has never seen', () => {
    expect(mergeTabOrder(['a', 'b'], ['a', 'b', 'new'])).toEqual(['a', 'b', 'new']);
  });
});
