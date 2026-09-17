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
 * (`designApiWrite.ts`), the shared path vocabulary (`designApiPaths.ts`), and
 * the domain types, so `designApi` stays the one import site for callers and
 * every file stays under the AGENTS.md 500-line rule.
 */

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
  DesignFeedbackEntry,
  DesignFlowSummary,
  DesignFrame,
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
export { writeAsset, writeFeedback, writeLayout } from './designApiWrite';

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

/** Parse SP-140-4d feedback JSON. Never throws — a bad file yields zeros. */
export function parseFeedback(text: string, innerPath: string): DesignFeedbackEntry {
  const base: DesignFeedbackEntry = {
    name: basename(innerPath),
    path: innerPath,
    status: '',
    annotationCount: 0,
    resolvedCount: 0,
  };
  if (!text) return base;
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    return base;
  }
  if (!data || typeof data !== 'object') return base;
  const record = data as Record<string, unknown>;
  const annotations = Array.isArray(record.annotations) ? record.annotations : [];
  return {
    ...base,
    status: typeof record.status === 'string' ? record.status : '',
    annotationCount: annotations.length,
    resolvedCount: annotations.filter((a) => (a as { resolved?: unknown })?.resolved === true).length,
  };
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

  const inventory = buildInventory(files);
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

interface TokenParseResult {
  tokenCount: number;
  types: string[];
}

/** Count DTCG leaves (objects with `$value`) and collect `$type` values. */
export function parseTokensFile(text: string): TokenParseResult {
  const result: TokenParseResult = { tokenCount: 0, types: [] };
  if (!text) return result;
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    return result;
  }
  const types = new Set<string>();
  const walk = (node: unknown): void => {
    if (!node || typeof node !== 'object' || Array.isArray(node)) return;
    const record = node as Record<string, unknown>;
    if ('$value' in record) {
      result.tokenCount += 1;
      if (typeof record.$type === 'string') types.add(record.$type);
      return;
    }
    for (const [key, value] of Object.entries(record)) {
      if (key.startsWith('$')) continue;
      walk(value);
    }
  };
  walk(data);
  result.types = [...types];
  return result;
}

interface FlowParseResult {
  nodeCount: number;
  edgeCount: number;
  direction: string;
}

const FLOW_OPERATORS = ['-.->', '--o', '--x', 'o--', 'x--', '-->', '==>', '===', '---', '--', '=='];
const LEADING_ID = /^[A-Za-z0-9_-]+/;
const FLOW_KEYWORDS = ['end', 'subgraph', 'classdef', 'class ', 'style', 'linkstyle', 'click'];

/** Split a statement into segments around connect operators (mermaid subset). */
function splitFlowOperators(line: string): { segs: string[]; ops: string[] } {
  const segs: string[] = [];
  const ops: string[] = [];
  let current = '';
  let i = 0;
  while (i < line.length) {
    const op = FLOW_OPERATORS.find((candidate) => line.startsWith(candidate, i));
    if (op) {
      segs.push(current);
      ops.push(op);
      current = '';
      i += op.length;
    } else {
      current += line[i];
      i += 1;
    }
  }
  segs.push(current);
  return { segs, ops };
}

/**
 * Subset mermaid flowchart parser mirroring pkg/design/flowchart.go: direction
 * hint, distinct node ids, and edge count. Comments, subgraph/end, and
 * styling keywords are skipped.
 */
export function parseFlowText(text: string): FlowParseResult {
  const result: FlowParseResult = { nodeCount: 0, edgeCount: 0, direction: '' };
  if (!text) return result;
  const nodes = new Set<string>();

  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line || line.startsWith('%%')) continue;
    const lower = line.toLowerCase();
    if (lower.startsWith('flowchart') || lower.startsWith('graph')) {
      const fields = line.split(/\s+/);
      if (fields.length >= 2) result.direction = fields[1];
      continue;
    }
    if (FLOW_KEYWORDS.some((kw) => lower.startsWith(kw))) continue;
    const { segs, ops } = splitFlowOperators(line);
    const ids = segs.map((seg) => LEADING_ID.exec(seg.trim())?.[0] ?? '');
    for (const id of ids) if (id) nodes.add(id);
    for (let i = 0; i < ops.length; i += 1) {
      if (ids[i] && ids[i + 1]) result.edgeCount += 1;
    }
  }
  result.nodeCount = nodes.size;
  return result;
}

/**
 * Status markers a manifest listing may carry, mirroring
 * `pkg/design/inventory.go`'s `parseManifestListings` (`draft`/`review`/`ready`
 * — the SP-140-1 §1e convention). A middle segment outside this set is part of
 * the summary, never a status.
 */
const MANIFEST_STATUSES = ['draft', 'review', 'ready'] as const;

/**
 * Parse the manifest's screen/flow status markers, mirroring
 * `pkg/design/inventory.go`'s `parseManifestListings`: a listing bullet of the
 * form ``- `login` — ready — sign-in entry point`` maps `login` → `ready`.
 * The dash separator may be an em dash (the template's) or a spaced ASCII
 * hyphen, matching the Go parser, and an unrecognised middle segment leaves the
 * entry without a status. Names are keyed lowercased; the value is lowercase.
 */
export function parseManifestStatuses(text: string): Record<string, string> {
  const statuses: Record<string, string> = {};
  if (!text) return statuses;
  for (const raw of text.split('\n')) {
    const trimmed = raw.trim();
    if (!trimmed.startsWith('- ')) continue;
    const body = trimmed.slice(2).trim();
    const start = body.indexOf('`');
    if (start < 0) continue;
    const rest = body.slice(start + 1);
    const end = rest.indexOf('`');
    if (end < 0) continue;
    const name = rest.slice(0, end).trim();
    if (!name) continue;
    const segments = splitDashSegments(rest.slice(end + 1));
    if (segments.length === 0) continue;
    const marker = segments[0].toLowerCase();
    if ((MANIFEST_STATUSES as readonly string[]).includes(marker)) statuses[name.toLowerCase()] = marker;
  }
  return statuses;
}

/** Split a manifest listing tail on its dash separators, dropping empties. */
function splitDashSegments(tail: string): string[] {
  let text = tail.trim();
  if (!text) return [];
  for (const dash of ['\u2014', '-']) {
    if (text.startsWith(dash + ' ')) text = text.slice(dash.length + 1);
    if (text.endsWith(' ' + dash)) text = text.slice(0, text.length - dash.length - 1);
  }
  const normalized = text.split(' \u2014 ').join('\u0000').split(' - ').join('\u0000');
  return normalized
    .split('\u0000')
    .map((segment) => segment.trim())
    .filter((segment) => segment !== '');
}

/** Parse the manifest frames: block (`name: WxH`), mirroring pkg/design/frames.go. */
export function parseFrames(text: string): DesignFrame[] {
  const frames: DesignFrame[] = [];
  if (!text) return frames;
  let inBlock = false;
  for (const raw of text.split('\n')) {
    if (!raw.trim()) continue;
    const indented = raw[0] === ' ' || raw[0] === '\t';
    const trimmed = raw.trim();
    if (inBlock && !indented) inBlock = false;
    if (!inBlock && !indented && trimmed === 'frames:') {
      inBlock = true;
      continue;
    }
    if (!inBlock || !indented) continue;
    const [rawName, ...rest] = trimmed.split(':');
    const parts = rest.join(':').trim().split('x');
    if (!rawName?.trim() || parts.length !== 2) continue;
    const width = Number(parts[0]);
    const height = Number(parts[1]);
    if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0) continue;
    frames.push({ name: rawName.trim(), width, height });
  }
  return frames;
}
