/**
 * Design-tooling vocabulary shared by `designApi.ts` and `designApiWrite.ts`
 * (SP-140-3 §3f). Split out so each of those modules stays under the AGENTS.md
 * 500-line rule while both keep one spelling of the design tree's constants and
 * path rules.
 *
 * The path rule is the important one: an inventory entry path is
 * *workspace-relative* (`design/screens/login.html`) while the `/api/file`
 * endpoint takes the same path back through `designRootPath`, which accepts a
 * path already under `design/` and also a design-root-relative one
 * (`screens/login.html`). Both normalise to one URL, so the read and the
 * write-back always agree on the target file.
 */

import type { DesignAssetKind } from './types';

export const DESIGN_DIR = 'design';
export const SUMMARY_MAX_CHARS = 4000;

/** Asset extensions the inventory lists (screens/wireframes are extension-driven). */
export const ASSET_EXTENSIONS = ['.svg', '.mmd', '.mmdc', '.json', '.md', '.html', '.css', '.txt'];

/** Classify a workspace-relative path into a design asset kind, else null. */
export function classify(rel: string): DesignAssetKind | null {
  if (!rel.startsWith(DESIGN_DIR + '/')) return null;
  const inner = rel.slice(DESIGN_DIR.length + 1);
  const name = basename(inner);
  if (inner === 'README.md' || inner === 'README') return 'manifest';
  if (inner.startsWith('wireframes/')) return name.endsWith('.svg') ? 'wireframe' : null;
  if (inner.startsWith('screens/')) return 'screen';
  if (inner.startsWith('flows/')) {
    if (name.endsWith('.layout.json')) return 'layout';
    return name.endsWith('.mmd') || name.endsWith('.mmdc') ? 'flow' : null;
  }
  if (inner.startsWith('tokens/')) return name.endsWith('.tokens.json') ? 'tokens' : null;
  if (inner.startsWith('feedback/')) return name.endsWith('.json') ? 'feedback' : null;
  if (inner.startsWith('brand/')) return 'brand';
  if (inner.startsWith('icons/')) return 'icon';
  return null;
}

/** Last path segment. */
export function basename(p: string): string {
  const parts = p.split('/');
  return parts[parts.length - 1] || p;
}

/** Workspace-relative path (POSIX separators), design/ prefix intact. */
export function relativePath(path: string, workspaceRoot: string): string {
  const raw = (path ?? '').replace(/\\/g, '/');
  if (raw && !raw.startsWith('/')) return raw.replace(/^\.\//, '');
  const root = workspaceRoot.replace(/\\/g, '/').replace(/\/+$/, '');
  const rel = root && raw.startsWith(root + '/') ? raw.slice(root.length + 1) : raw;
  return rel.replace(/^\.\//, '');
}

/** Design-root-relative path for an inventory entry at `rel`. */
export function designRootPath(rel: string): string {
  const normalized = rel.replace(/\\/g, '/').replace(/^\.\//, '').replace(/^\/+/, '');
  return normalized.startsWith(DESIGN_DIR + '/') ? normalized : `${DESIGN_DIR}/${normalized}`;
}

/** The `/api/file` URL for a design asset path (design-root-relative or not). */
export function fileUrl(path: string): string {
  return `/api/file?path=${encodeURIComponent(designRootPath(path))}`;
}
