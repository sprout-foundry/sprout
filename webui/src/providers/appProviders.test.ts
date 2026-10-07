/**
 * The app root composes its provider stack through `SproutProviders`.
 *
 * The wiring is the point: extracting the stack into the package is only real
 * if the local app renders through it. This pins the two places — `App.tsx`
 * renders its shell inside `SproutProviders`, and the wrapper is what the app
 * imports from the providers entry (not a hand-assembled stack), so a later
 * regression that re-inlines the stack fails here.
 *
 * The assertions are structural (source shape), not a render of the whole app:
 * `App` boots the WebSocket, the WASM shell and the initialization hooks,
 * which are not what this item changes.
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { SproutProviders } from './SproutProviders';

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, '../App.tsx'), 'utf-8');
const viewsEntry = readFileSync(resolve(here, '../views/index.ts'), 'utf-8');

describe('app root renders through SproutProviders', () => {
  it('imports the wrapper from the providers entry', () => {
    expect(appSource).toMatch(/import \{ SproutProviders \} from '\.\/providers';/);
  });

  it('renders the app shell inside the wrapper', () => {
    expect(appSource).toContain('<SproutProviders');
    // The shell closes inside the wrapper: AppInner is the wrapper's direct
    // child, so its closing tag is immediately followed by the wrapper's.
    expect(appSource).toMatch(/<AppInner \/>\s*<\/SproutProviders>/);
  });

  it('no longer assembles the stack by hand', () => {
    for (const provider of [
      '<SproutAdapterProvider',
      '<PlatformNavProvider',
      '<PluginContextProvider',
      '<ThemeProvider',
      '<HotkeyProvider',
      '<EditorManagerProvider',
    ]) {
      expect(appSource).not.toContain(provider);
    }
  });

  it('is the same wrapper the providers entry exports', () => {
    expect(typeof SproutProviders).toBe('function');
    expect(SproutProviders.displayName).toBe('SproutProviders');
  });
});

describe('the views entry re-exports the wrapper', () => {
  it('re-exports SproutProviders so a host importing the views entry gets it', () => {
    // The views entry is source in this repo (the package re-exports it), so
    // the assertion reads the source file directly rather than resolving the
    // package subpath, which the `exports` map does not surface.
    expect(viewsEntry).toMatch(/export \{ SproutProviders \} from '\.\.\/providers\/index';/);
  });
});
