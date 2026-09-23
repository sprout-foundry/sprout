/**
 * Drag gesture tests (SP-140-7 §7e): pin drag persisting `at`
 * coordinates, and grid reorder persisting manifest order.
 */

import { describe, expect, it } from 'vitest';
import { moveItem, reorderManifestScreens } from './gridReorder';

describe('moveItem', () => {
  it('moves one item, keeping the others stable', () => {
    expect(moveItem(['a', 'b', 'c'], 0, 2)).toEqual(['b', 'c', 'a']);
    expect(moveItem(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b']);
  });

  it('no-op moves return the same order', () => {
    expect(moveItem(['a', 'b'], 1, 1)).toEqual(['a', 'b']);
    expect(moveItem(['a', 'b'], 0, 5)).toEqual(['a', 'b']);
    expect(moveItem(['a', 'b'], -1, 0)).toEqual(['a', 'b']);
  });
});

const manifest = `# Design Workspace

frames:
  mobile: 390x844

## Screens

- \`login\` — draft — sign-in entry point
- \`home\` — ready — post-sign-in landing
- \`inbox\` — draft — message list

## Flows

- \`sign-up\` — draft — account creation
`;

describe('reorderManifestScreens', () => {
  it('reorders only the Screens section, preserving statuses and summaries', () => {
    const { text, changed } = reorderManifestScreens(manifest, ['home', 'login', 'inbox']);
    expect(changed).toBe(true);
    const screens = text
      .split('\n')
      .filter((line) => /^- `/.test(line.trim()))
      .slice(0, 3);
    expect(screens[0]).toContain('`home`');
    expect(screens[0]).toContain('ready');
    expect(screens[1]).toContain('`login`');
    expect(screens[1]).toContain('draft');
    expect(screens[2]).toContain('`inbox`');
    // Flows untouched.
    expect(text).toContain('- `sign-up` — draft — account creation');
    expect(text.indexOf('## Screens')).toBeLessThan(text.indexOf('## Flows'));
  });

  it('the frames block and headings never move', () => {
    const { text } = reorderManifestScreens(manifest, ['inbox', 'home', 'login']);
    expect(text.indexOf('frames:')).toBeGreaterThan(-1);
    expect(text.indexOf('frames:')).toBeLessThan(text.indexOf('## Screens'));
  });

  it('same order is a no-op', () => {
    const { changed } = reorderManifestScreens(manifest, ['login', 'home', 'inbox']);
    expect(changed).toBe(false);
  });

  it('a manifest without a Screens section is refused', () => {
    const { text, changed } = reorderManifestScreens('# nothing here\n', ['a']);
    expect(changed).toBe(false);
    expect(text).toBe('# nothing here\n');
  });

  it('unmentioned stems keep their relative order after the mentioned ones', () => {
    const { text } = reorderManifestScreens(manifest, ['inbox']);
    const screens = text.split('## Screens')[1].split('## Flows')[0];
    const order = screens
      .split('\n')
      .filter((line) => line.trim().startsWith('- `'))
      .map((line) => line.slice(line.indexOf('`') + 1, line.indexOf('`', line.indexOf('`') + 1)));
    expect(order).toEqual(['inbox', 'login', 'home']);
  });
});
