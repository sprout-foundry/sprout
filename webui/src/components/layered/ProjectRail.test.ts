import { describe, expect, it } from 'vitest';
import { monogram } from './ProjectRail';

describe('monogram', () => {
  it.each([
    ['cli/oauth', 'O', 'a'],
    ['sprout-foundry/sprout-foundry', 'S', 'f'],
    ['acme/my_app', 'M', 'a'],
    ['acme/webUI', 'W', 'u'],
    ['acme/x', 'X', ''],
  ])('%s → %s%s', (label, major, minor) => {
    expect(monogram(label)).toEqual({ major, minor });
  });
});
