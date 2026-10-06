import { describe, expect, it } from 'vitest';
import { monogram, railSymbols } from './ProjectRail';

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

describe('railSymbols', () => {
  it('uses one letter while first letters are unique', () => {
    expect(railSymbols(['acme/platform', 'acme/sprout', 'acme/docs'])).toEqual(['P', 'S', 'D']);
  });

  it('uses two letters only for projects that share a first letter', () => {
    expect(railSymbols(['acme/sprout', 'acme/sprout-foundry', 'acme/platform'])).toEqual(['Sp', 'Sf', 'P']);
  });

  it('compares names, not owners', () => {
    expect(railSymbols(['alpha/web', 'beta/api'])).toEqual(['W', 'A']);
  });
});
