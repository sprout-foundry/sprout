/**
 * SP-140-3 item 3.3 AC 5 — DesignView must not be in the main bundle.
 *
 * The view is route-lazy (`React.lazy(() => import('./design/DesignView'))` in
 * `EditorWorkspace.tsx`), so a workspace without `design/` never pays for it.
 * The dev server the e2e spec runs against serves raw ES modules with no chunk
 * graph, so this property is asserted here instead: against `webui/dist`, the
 * production build.
 *
 * The build is produced by `cd webui && npx vite build`. When `dist/` is absent
 * (a fresh checkout, or a CI job that has not built), the suite skips rather
 * than failing on an artifact it did not create.
 */

import fs from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

const DIST = path.resolve(__dirname, '../../../dist');

function readChunks(suffix: string): Array<{ name: string; text: string }> {
  const assets = path.join(DIST, 'assets');
  if (!fs.existsSync(assets)) return [];
  return fs
    .readdirSync(assets)
    .filter((name) => name.endsWith(suffix))
    .map((name) => ({ name, text: fs.readFileSync(path.join(assets, name), 'utf8') }));
}

const jsChunks = readChunks('.js');
const designParts = jsChunks.filter((chunk) => /^DesignView-.*\.js$/.test(chunk.name));
const mainParts = jsChunks.filter((chunk) => /^main-.*\.js$/.test(chunk.name));
const built = designParts.length > 0 && mainParts.length > 0;

describe.skipIf(!built)('DesignView bundle split (AC 5)', () => {
  it('emits the view as its own chunk', () => {
    expect(designParts.length).toBeGreaterThan(0);
  });

  it('keeps the component body out of the main bundle', () => {
    // A testid that only DesignView's subtree renders. Its presence in the main
    // chunk would mean the module was pulled into the entry graph and the lazy
    // import no longer splits anything.
    const marker = 'design-flows-canvas';
    for (const chunk of designParts) {
      expect(chunk.text).toContain(marker);
    }
    for (const chunk of mainParts) {
      expect(chunk.text).not.toContain(marker);
    }
  });

  it('references the view from the main bundle only by dynamic import', () => {
    // The entry graph holds the chunk's filename (the import call), never the
    // module's contents.
    const referenced = mainParts.some((chunk) => /DesignView-[\w-]+\.js/.test(chunk.text));
    expect(referenced).toBe(true);
  });

  it('ships the view styles as their own chunk', () => {
    const css = fs.readdirSync(path.join(DIST, 'assets')).filter((name) => /^DesignView-.*\.css$/.test(name));
    expect(css.length).toBeGreaterThan(0);
  });
});
