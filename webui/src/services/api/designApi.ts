/**
 * Design domain API — data access for the design/ workspace tree.
 *
 * Built entirely over the existing workspace-file endpoints (`/api/files`,
 * `/api/file`) — zero new HTTP endpoints, no backend changes (SP-140-3 §3f).
 * Functions take their transport as a parameter (`fetchFn`) so tests can
 * inject a mock, matching the filesApi/editorApi convention.
 *
 * A `readFn` override is accepted on the read paths: `readFileWithConsent`
 * returns a raw `Response` (its body is the file text) whereas `clientFetch`/
 * `useSproutFetch` return JSON. Passing `readFn` keeps the module testable
 * with a plain fetch mock while letting DesignView reuse the consent-aware
 * workspace read path. See SP-140-3 §3b/§3e.
 *
 * This module keeps the inventory/read half and re-exports the write half
 * (`designApiWrite.ts`), the shared path vocabulary (`designApiPaths.ts`), the
 * read-side parsers (`designApiParse.ts`), and the domain types, so `designApi`
 * stays the one import site for callers and every file stays under the
 * AGENTS.md 500-line rule.
 */

import { feedbackFilePath } from '../../design/feedbackWrite';
import {
  parseFeedback,
  parseFeedbackJson,
  parseFlowText,
  parseFrames,
  parseManifestStatuses,
  parseTokensFile,
} from './designApiParse';
import {
  ASSET_EXTENSIONS,
  DESIGN_DIR,
  basename,
  classify,
  fileUrl,
  relativePath,
  SUMMARY_MAX_CHARS,
} from './designApiPaths';
import type {
  DesignAssetEntry,
  DesignAssetKind,
  DesignFeedbackFile,
  DesignFlowSummary,
  DesignInventory,
  DesignManifestSummary,
  DesignTokenGroup,
  FilesResponse,
} from './types';

export type {
  DesignAssetEntry,
  DesignAssetKind,
  DesignFeedbackAnnotation,
  DesignFeedbackEntry,
  DesignFeedbackFile,
  DesignFlowSummary,
  DesignFrame,
  DesignInventory,
  DesignLayoutSidecar,
  DesignManifestSummary,
  DesignTokenGroup,
  DesignWriteResult,
} from './types';

export { DESIGN_DIR, SUMMARY_MAX_CHARS, designRootPath } from './designApiPaths';
export {
  writeAsset,
  writeAssetIfUnchanged,
  writeFeedback,
  writeLayout,
  DesignWriteConflictError,
} from './designApiWrite';
export type { SafeWriteOptions, WriteConflict } from './designApiWrite';
export {
  parseFeedback,
  parseFeedbackFile,
  parseFeedbackJson,
  parseFlowText,
  parseFrames,
  parseManifestStatuses,
  parseTokensFile,
} from './designApiParse';

const EMPTY_MANIFEST: DesignManifestSummary = { path: 'README.md', exists: false, frames: [], chars: 0 };

/** Workspace roots containing a `design/` segment, from the file list. */
function designRootsOf(files: FilesResponse): string[] {
  const roots = new Set<string>();
  for (const f of files.files) {
    const p = (f.path ?? '').replace(/\\/g, '/');
    const idx = p.indexOf('/' + DESIGN_DIR + '/');
    if (idx > 0) roots.add(p.slice(0, idx));
    else if (p.endsWith('/' + DESIGN_DIR)) roots.add(p.slice(0, p.length - DESIGN_DIR.length - 1));
  }
  return [...roots];
}

function buildEntries(files: FilesResponse, workspaceRoot: string): DesignAssetEntry[] {
  const entries: DesignAssetEntry[] = [];
  for (const f of files.files) {
    const rel = relativePath(f.path ?? '', workspaceRoot);
    const kind = rel ? classify(rel) : null;
    if (!kind) continue;
    const listable = ASSET_EXTENSIONS.some((ext) => rel.endsWith(ext));
    if (kind !== 'manifest' && kind !== 'screen' && !listable) continue;
    if (kind === 'screen' && !listable && !rel.endsWith('/')) continue;
    entries.push({ path: rel, name: basename(rel), kind, size: 0, modified: 0 });
  }
  return entries.sort((a, b) => a.path.localeCompare(b.path));
}

/**
 * Walk a flattened file list and index the design/ subtree into an
 * inventory. Robust to a missing or empty design/ (returns exists: false).
 */
export function buildInventory(files: FilesResponse | null | undefined): DesignInventory {
  const empty: DesignInventory = {
    exists: false,
    manifest: { ...EMPTY_MANIFEST, frames: [] },
    assets: [],
    wireframes: [],
    screens: [],
    flows: [],
    layouts: [],
    tokenFiles: [],
    feedback: [],
    tokenGroups: [],
    tokenCount: 0,
    flowSummaries: [],
    summary: '',
  };
  if (!files || !Array.isArray(files.files)) return empty;

  const assets = buildEntries(files, designRootsOf(files)[0] ?? '');
  if (assets.length === 0) return empty;

  const byKind = (kind: DesignAssetKind) => assets.filter((a) => a.kind === kind);
  const manifestEntries = byKind('manifest');
  const flows = byKind('flow');

  return {
    ...empty,
    exists: true,
    manifest: manifestEntries.length
      ? { ...EMPTY_MANIFEST, path: manifestEntries[0].path, exists: true }
      : { ...EMPTY_MANIFEST, frames: [] },
    assets,
    wireframes: byKind('wireframe'),
    screens: byKind('screen'),
    flows,
    layouts: byKind('layout'),
    tokenFiles: byKind('tokens'),
    feedback: byKind('feedback').map((f) => ({
      name: f.name,
      path: f.path,
      status: '',
      annotationCount: 0,
      resolvedCount: 0,
    })),
    flowSummaries: flows.map((f) => ({
      name: f.name.replace(/\.(mmd|mmdc)$/, ''),
      path: f.path,
      nodeCount: 0,
      edgeCount: 0,
      direction: '',
    })),
    summary: assets
      .map((a) => a.path)
      .join('\n')
      .slice(0, SUMMARY_MAX_CHARS),
  };
}

/** Design-root-relative path for an inventory entry at `rel`. */

async function responseText(response: Response): Promise<string> {
  return typeof response.text === 'function' ? response.text() : '';
}

/** Deepest design/ subtree the inventory walk will descend, as a safety bound. */
export const DESIGN_LIST_MAX_DEPTH = 8;

/** A raw `/api/files` entry: the daemon returns absolute paths plus `is_dir`. */
interface RawListEntry {
  path?: string;
  name?: string;
  relative?: string;
  is_dir?: boolean;
  isDir?: boolean;
}

function isDirEntry(entry: RawListEntry): boolean {
  return Boolean(entry.is_dir ?? entry.isDir);
}

async function listDirectory(fetchFn: typeof fetch, dir: string): Promise<RawListEntry[]> {
  const response = await fetchFn(`/api/files?path=${encodeURIComponent(dir)}`);
  if (!response.ok) return [];
  const data = (await response.json()) as { files?: RawListEntry[] };
  return Array.isArray(data?.files) ? data.files : [];
}

/**
 * Flatten a design/ tree into the shape `buildInventory` indexes.
 *
 * `GET /api/files` lists one directory at a time (the same shallow listing the
 * sidebar's file tree walks lazily), so a single root listing never carries the
 * nested `design/**` entries the inventory needs. This walks the `design`
 * directories breadth-first from the root listing and returns one flat entry
 * list — directory entries kept (their trailing-slash handling is
 * `buildEntries`'s business), files as the daemon reported them.
 *
 * Bounded by `DESIGN_LIST_MAX_DEPTH` and de-duplicated by path so a cyclic or
 * pathological tree cannot spin the walk. A failed directory read ends that
 * branch rather than failing the whole inventory.
 */
export async function flattenDesignTree(fetchFn: typeof fetch, rootListing: RawListEntry[]): Promise<RawListEntry[]> {
  const designDirs = rootListing.filter((entry) => {
    const path = (entry.path ?? '').replace(/\\/g, '/').replace(/\/+$/, '');
    return isDirEntry(entry) && path.endsWith('/' + DESIGN_DIR);
  });
  const flat: RawListEntry[] = [...rootListing];
  const seen = new Set(flat.map((entry) => entry.path ?? ''));
  let frontier = designDirs.map((entry) => (entry.path ?? '').replace(/\\/g, '/'));

  for (let depth = 0; depth < DESIGN_LIST_MAX_DEPTH && frontier.length > 0; depth += 1) {
    const children: RawListEntry[] = [];
    for (const dir of frontier) {
      for (const entry of await listDirectory(fetchFn, dir)) {
        const path = entry.path ?? '';
        if (!path || seen.has(path)) continue;
        seen.add(path);
        flat.push(entry);
        if (isDirEntry(entry)) children.push(entry);
      }
    }
    frontier = children.map((entry) => (entry.path ?? '').replace(/\\/g, '/'));
  }
  return flat;
}

/**
 * Read a design asset's text. Default read path is GET /api/file (text body);
 * pass `readFileWithConsent` to reuse the consent-aware workspace read.
 * Throws on a non-OK response apart from 404 (which yields '').
 */
export async function readAsset(fetchFn: typeof fetch, path: string, readFn?: typeof fetch): Promise<string> {
  const response = await (readFn ?? fetchFn)(fileUrl(path));
  if (!response.ok) {
    if (response.status === 404) return '';
    throw new Error(`Failed to read design asset: ${path}`);
  }
  return responseText(response);
}

/**
 * Read and parse `target`'s SP-140-4d feedback file — the read half of the
 * detail pane's resolution flow (SP-140-4 item 4.8). Built over the same
 * `/api/file` read path as `readAsset` (zero new HTTP endpoints), so a
 * consent-aware `readFn` works here too.
 *
 * A missing file is not an error: an asset with no annotations yet (or one
 * whose file was never written) yields the empty document, letting the pane
 * still offer the resolution note for a target it has no annotations for.
 */
export async function readFeedback(
  fetchFn: typeof fetch,
  target: string,
  readFn?: typeof fetch,
): Promise<DesignFeedbackFile> {
  const text = await readAsset(fetchFn, feedbackFilePath(target), readFn);
  return parseFeedbackJson(text, target);
}

/**
 * Inventory the design/ tree. Reuses filesApi.getFiles (the workspace file
 * list) and, for tokens/flows/feedback, the per-file read path to derive
 * group counts and flow node/edge counts. Errors surface as `exists: false`
 * rather than throwing so a workspace without design/ is not an error.
 */
export async function listAssets(fetchFn: typeof fetch, readFn?: typeof fetch): Promise<DesignInventory> {
  let files: FilesResponse;
  try {
    const response = await fetchFn('/api/files');
    if (!response.ok) return buildInventory(null);
    files = (await response.json()) as FilesResponse;
  } catch {
    return buildInventory(null);
  }

  // A shallow root listing only carries the `design` directory entry itself, so
  // the nested design/** assets the inventory indexes have to be walked for.
  // A listing that already carries them (a recursive backend, or a caller that
  // hands `buildInventory` a flattened list directly) short-circuits: the walk
  // only runs when the root listing alone yields no inventory.
  let inventory = buildInventory(files);
  if (!inventory.exists && Array.isArray(files?.files)) {
    const flat = await flattenDesignTree(fetchFn, files.files as RawListEntry[]);
    if (flat.length > files.files.length) {
      files = { ...files, files: flat as FilesResponse['files'] };
      inventory = buildInventory(files);
    }
  }
  if (!inventory.exists) return inventory;

  const reader = readFn ?? fetchFn;
  const readText = async (rel: string): Promise<string> => {
    try {
      const response = await reader(fileUrl(rel));
      return response.ok ? await responseText(response) : '';
    } catch {
      return '';
    }
  };

  const childPath = (f: DesignAssetEntry, prefix: string): string =>
    f.path.slice(f.path.indexOf(prefix) + prefix.length);

  const tokenGroups = Promise.all(
    inventory.tokenFiles.map(async (f): Promise<DesignTokenGroup> => {
      const parsed = parseTokensFile(await readText(f.path));
      return { name: childPath(f, 'tokens/'), path: f.path, tokenCount: parsed.tokenCount, types: parsed.types };
    }),
  );
  const flowSummaries = Promise.all(
    inventory.flowSummaries.map(async (f): Promise<DesignFlowSummary> => {
      const parsed = parseFlowText(await readText(f.path));
      return { ...f, nodeCount: parsed.nodeCount, edgeCount: parsed.edgeCount, direction: parsed.direction };
    }),
  );
  const feedback = Promise.all(inventory.feedback.map(async (f) => parseFeedback(await readText(f.path), f.path)));
  const manifestText = readText(inventory.manifest.exists ? inventory.manifest.path : 'README.md');

  const [groups, flows, fb, md] = await Promise.all([tokenGroups, flowSummaries, feedback, manifestText]);
  const screenStatuses = parseManifestStatuses(md);
  return {
    ...inventory,
    manifest: inventory.manifest.exists
      ? { ...inventory.manifest, chars: md.length, frames: parseFrames(md) }
      : inventory.manifest,
    // Screens carry the README status chip (§3c); the wireframe of the same
    // stem is the fallback, mirroring `pkg/design/inventory.go`'s name.
    screens: inventory.screens.map((screen) => {
      const stem = screen.name.replace(/\.[^.]*$/, '').toLowerCase();
      const status = screenStatuses[stem] ?? screenStatuses[`${stem}.html`] ?? '';
      return status ? { ...screen, status } : screen;
    }),
    tokenGroups: groups,
    tokenCount: groups.reduce((sum, g) => sum + g.tokenCount, 0),
    flowSummaries: flows,
    feedback: fb,
  };
}
