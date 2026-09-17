/**
 * Read-side parsers for the design/ workspace tree (SP-140-1 §1a/§1e, SP-140-3
 * §3b) and the SP-140-4 §4d feedback reader (item 4.8). Split out of
 * `designApi.ts` so both modules stay under the AGENTS.md 500-line rule.
 *
 * Everything here is pure: text in, model out, no React, no I/O, no transport.
 * `designApi.ts` re-exports it, so `designApi` remains the one import site for
 * callers while the parsing rules live in one place — the DTCG token walk, the
 * mermaid subset, the README manifest listings/frames, and the §4d feedback
 * document (whose shape is shared with `designApiWrite.writeFeedback` and the
 * `feedbackWrite` model).
 */

import type { DesignFeedbackAnnotation, DesignFeedbackEntry, DesignFeedbackFile, DesignFrame } from './types';

/* -------------------------------------------------------------------------- */
/* Feedback (§4d)                                                             */
/* -------------------------------------------------------------------------- */

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
 * Normalise a parsed SP-140-4d feedback document: every field present, the
 * annotated values coerced to their schema types, and annotations filtered to
 * the resolvable ones (an id and a note). A `null`/non-object document yields
 * an empty file rather than throwing — the reader's input is a workspace file a
 * human or the agent may have hand-edited.
 *
 * This is deliberately the same §4d shape `parseFeedback` counts and
 * `designApiWrite.writeFeedback` writes, so a read → toggle → write round trip
 * never drops or invents a field.
 */
export function parseFeedbackFile(data: unknown, target: string): DesignFeedbackFile {
  const empty: DesignFeedbackFile = { target: target ?? '', status: '', resolution: '', annotations: [] };
  if (!data || typeof data !== 'object' || Array.isArray(data)) return empty;
  const record = data as Record<string, unknown>;
  const rawAnnotations = Array.isArray(record.annotations) ? record.annotations : [];
  const annotations: DesignFeedbackAnnotation[] = [];
  for (const raw of rawAnnotations) {
    if (!raw || typeof raw !== 'object') continue;
    const entry = raw as Record<string, unknown>;
    const id = typeof entry.id === 'string' ? entry.id : '';
    const note = typeof entry.note === 'string' ? entry.note : '';
    if (!id || !note) continue;
    const at = (entry.at ?? {}) as Record<string, unknown>;
    annotations.push({
      id,
      at: { x: typeof at.x === 'number' ? at.x : 0, y: typeof at.y === 'number' ? at.y : 0 },
      area: typeof entry.area === 'string' ? entry.area : '',
      note,
      resolved: entry.resolved === true,
      created: typeof entry.created === 'string' ? entry.created : '',
    });
  }
  return {
    target: typeof record.target === 'string' && record.target ? record.target : (target ?? ''),
    status: typeof record.status === 'string' ? record.status : '',
    resolution: typeof record.resolution === 'string' ? record.resolution : '',
    annotations,
  };
}

/**
 * Parse a raw JSON string into a §4d document. Never throws: malformed JSON
 * yields the empty file for `target` (the same tolerance `parseFeedback` shows
 * the inventory grid).
 */
export function parseFeedbackJson(text: string, target: string): DesignFeedbackFile {
  if (!text) return parseFeedbackFile(null, target);
  try {
    return parseFeedbackFile(JSON.parse(text), target);
  } catch {
    return parseFeedbackFile(null, target);
  }
}

/* -------------------------------------------------------------------------- */
/* Tokens (§1a)                                                               */
/* -------------------------------------------------------------------------- */

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

/* -------------------------------------------------------------------------- */
/* Flows (§1c)                                                                */
/* -------------------------------------------------------------------------- */

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

/* -------------------------------------------------------------------------- */
/* Manifest (§1e)                                                             */
/* -------------------------------------------------------------------------- */

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

/** Last path segment, POSIX or Windows separators (the parser's small local copy). */
function basename(p: string): string {
  const parts = p.split('/');
  return parts[parts.length - 1] || p;
}
