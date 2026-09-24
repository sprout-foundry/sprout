/**
 * Screen workbench data (SP-140-8 §8b, item 8.2) — the client-side derivation
 * of the §5g screen brief over the design/ tree's raw files.
 *
 * `design_brief` (SP-140-5 §5g) is the screen-brief contract: purpose,
 * wireframe path, flows in/out with triggers, token refs (known/unknown),
 * open feedback, and README status. The Go tool computes it server-side; this
 * module mirrors the *same contract* client-side over the files the webui
 * already reads (`designApi`'s `/api/file` path — SP-140-3 §3f's zero-new-
 * endpoint rule), so the workbench stays a presentational view over one data
 * contract rather than a second aggregation (the spec's "one truth"):
 *
 * - flows: the same mermaid subset the flow canvas renders
 *   (`parseFlowGraph`), plus the brief's own line-scan read of edge triggers
 *   (the Go brief's `briefFlowEdgesInFile`), so `-- "label" -->` triggers are
 *   never blank for a labelled edge
 * - tokens: the same `{group.token}` reference scan `design_sync` runs
 *   (pkg/design's `tokenRefRe`)
 * - manifest: the same listing rule the inventory's status parser runs
 *   (SP-140-1 §1e: `- \`login\` — ready — sign-in entry point`)
 * - feedback: the §4d document the feedback reader parses
 *
 * Everything here is pure: text in, model out. The read side (fetching the
 * flow/token/README/feedback text) belongs to `ScreenWorkbenchContainer`;
 * this module takes what it was handed.
 */

import { parseFlowGraph, parseFlowNodeId, splitFlowOperators } from '../../design/flowText';
import type {
  DesignFeedbackAnnotation,
  DesignFeedbackEntry,
  DesignFeedbackFile,
  DesignInventory,
} from '../../services/api/types';
import { tokenFileModel } from './designTokens';
import { parseManifestListings } from './manifestListings';

// Re-exported for the brief's consumers: the manifest listing rule is part of
// the §5g contract this module documents, even though the parse lives beside
// the brief's other section parsers.
export { parseManifestListings };

/** The §1e README status markers a listing may carry. */
export const SCREEN_STATUSES = ['draft', 'review', 'ready'] as const;

/**
 * One flow edge touching the briefed screen, in the §5g in/out split.
 * `flow`/`flowName` are attached by `deriveScreenBrief` (the edges of one
 * graph are the base form).
 */
export interface ScreenFlowEdge {
  /** The flow file's workspace-relative path (e.g. design/flows/sign-up.mmd). */
  flow: string;
  /** The flow's stem (e.g. "sign-up"). */
  flowName: string;
  /** The edge endpoints (node ids == wireframe stems). */
  source: string;
  target: string;
  /** 'in' (the screen is the target), 'out' (the source), 'both' (self-edge). */
  direction: 'in' | 'out' | 'both';
  /** The mermaid edge label — what causes the transition. '' when unlabelled. */
  trigger: string;
  /** The far endpoint's stem ('' for a self-edge). */
  otherStem: string;
  /** The far endpoint's node label, when the flow declared one. */
  otherLabel: string;
}

/** An edge before its flow identity is attached (the `flowEdgesForBrief` form). */
export type ScreenFlowEdgeBase = Omit<ScreenFlowEdge, 'flow' | 'flowName'>;

/** One `{group.token}` the wireframe refers to, resolved against the tree. */
export interface ScreenTokenRef {
  /** The dotted token path (e.g. "color.brand.primary"). */
  path: string;
  /** True when the reference resolves to a DTCG leaf in design/tokens/. */
  known: boolean;
}

/** The §4d pending-feedback view for the screen (the brief's `feedback` facet). */
export interface ScreenFeedbackModel {
  /** The feedback file's path ('' when no file exists for the screen). */
  path: string;
  /** The §4d top-level status ('changes-requested', 'resolved', …). */
  status: string;
  /** Annotations with `resolved: false`. */
  open: number;
  /** Total annotation count in the file. */
  total: number;
  /** True while the file needs the agent's attention (pending status or open notes). */
  pending: boolean;
  /** The agent's closing note, when present. */
  resolution: string;
  /** The open annotations as "[area] note", in file order. */
  notes: string[];
}

/** The §5g brief for one screen, derived for the workbench (§8b's data contract). */
export interface ScreenBriefModel {
  /** The wireframe stem the brief was derived for. */
  screenName: string;
  /** A wireframe with the stem, or a feedback file targeting it. */
  found: boolean;
  /** The screen's purpose from the README listing ('' when unlisted). */
  purpose: string;
  /** The README status marker (draft|review|ready; '' when undeclared). */
  status: string;
  /** Whether the README listing names the screen (an unlisted one is an orphan). */
  listedInReadme: boolean;
  /** The wireframe path (the inventory entry, or the canonical design/ one). */
  wireframe: string;
  /** Whether that file is in the inventory. */
  wireframeExists: boolean;
  /** The delivered design/screens/<stem>.html path, when one exists. */
  screenFile: string;
  /** Whether the screen file is in the inventory. */
  screenFileExists: boolean;
  /** The flow edges into the screen, with triggers (sorted, deterministic). */
  flowsIn: ScreenFlowEdge[];
  /** The flow edges out of the screen, with triggers (sorted, deterministic). */
  flowsOut: ScreenFlowEdge[];
  /** The DTCG tokens the wireframe refers to, sorted by path. */
  tokenRefs: ScreenTokenRef[];
  /** The top-level token groups available to consume, sorted by the inventory. */
  tokenGroups: string[];
  /** The §4d feedback view for the screen. */
  feedback: ScreenFeedbackModel;
  /** A not-found helper ('' when the screen exists). */
  guidance: string;
}

/** Everything `deriveScreenBrief` needs; all read-side, all plain data. */
export interface ScreenBriefInput {
  /** The wireframe stem to brief (e.g. "login"). */
  stem: string;
  /** The design inventory the workbench renders from. */
  inventory: DesignInventory;
  /** Flow `.mmd` text keyed by flow path; an unreadable flow is simply absent (skipped, never a hard error — mirroring the Go brief). */
  flowTexts?: Record<string, string>;
  /** The wireframe's text for the token-reference scan; '' when missing. */
  wireframeText?: string;
  /** The README manifest's text; '' when missing. */
  readmeText?: string;
  /** Token file text keyed by file path; an unreadable file is absent. */
  tokenTexts?: Record<string, string>;
  /** The screen's §4d feedback file (the feedback reader's parse), when one exists. */
  feedback?: DesignFeedbackFile | null;
}

/* -------------------------------------------------------------------------- */
/* Stems                                                                      */
/* -------------------------------------------------------------------------- */

/** The screen stem an asset name carries (extension-stripped, lowercased). */
export function stemOf(name: string): string {
  const base =
    String(name ?? '')
      .split('/')
      .pop() ?? '';
  const dot = base.lastIndexOf('.');
  const stem = dot > 0 ? base.slice(0, dot) : base;
  return stem.toLowerCase();
}

/* -------------------------------------------------------------------------- */
/* Flows (the §5g "flows in/out with triggers")                                */
/* -------------------------------------------------------------------------- */

/** One edge statement of a flow file, in statement order. */
export interface FlowEdgeScan {
  source: string;
  target: string;
  /** The statement's label trigger (the `|label|` form or the quoted/bare
   * middle segment), '' when unlabelled. */
  trigger: string;
}

/**
 * The line-scan read of a flow file's edge triggers — the authoritative read
 * mirroring the Go brief's `briefFlowEdgesInFile` line scan
 * (pkg/design/brief.go). The trigger of an edge statement is its label
 * segment: the `|label|` form (`A -->|tap Submit| B`) or the quoted/bare
 * middle segment of the `A -- "tap Submit" --> B` form. Plain statements
 * yield a '' trigger, and an opaque or unknown operator form yields no edge
 * at all — never a spurious label.
 *
 * The scan splits on the same `splitFlowOperators` split the flow parser
 * uses, so the two can never disagree about where an edge boundary is — and
 * unlike the subset parser, it reads edge labels (which `parseFlowGraph`
 * drops), so a trigger is never blank for an edge that carries one.
 */
export function scanFlowEdges(text: string): FlowEdgeScan[] {
  const edges: FlowEdgeScan[] = [];
  const seen = new Set<string>();
  const record = (source: string, target: string, trigger: string): void => {
    if (!source || !target) return;
    const key = `${source}\u0000${target}`;
    if (seen.has(key)) return;
    seen.add(key);
    edges.push({ source, target, trigger: trigger.trim() });
  };
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (line === '' || line.startsWith('%%')) continue;
    const lower = line.toLowerCase();
    if (lower.startsWith('flowchart') || lower.startsWith('graph')) continue;
    if (
      lower === 'end' ||
      lower.startsWith('subgraph') ||
      lower.startsWith('classdef') ||
      lower.startsWith('class') ||
      lower.startsWith('style') ||
      lower.startsWith('linkstyle') ||
      lower.startsWith('click')
    ) {
      continue;
    }
    const { segments, operators } = splitFlowOperators(line);
    if (operators.length === 0 || segments.length < 2) continue;
    // Normalise each segment: strip a leading `|label|`, record the node id,
    // and keep the raw form for the label case.
    const norm = segments.map((seg) => {
      let pipe = '';
      let body = seg;
      const pipeMatch = /^\s*\|([^|]*)\|\s*(.*)$/.exec(seg);
      if (pipeMatch) {
        pipe = pipeMatch[1].trim();
        body = pipeMatch[2];
      }
      const node = parseFlowNodeId(body);
      return { raw: seg, node, pipe };
    });
    const isLabel = (seg: (typeof norm)[number]): boolean => seg.node === '' && seg.pipe === '';
    for (let i = 0; i < norm.length; i += 1) {
      const src = norm[i];
      if (isLabel(src) || src.node === '') continue;
      let next = i + 1;
      let trigger = src.pipe;
      if (trigger === '' && next < norm.length && isLabel(norm[next])) {
        trigger = segmentLabel(norm[next].raw);
        next += 1;
      }
      if (next >= norm.length) continue;
      const tgt = norm[next];
      if (trigger === '') trigger = tgt.pipe;
      if (tgt.node === '') continue;
      record(src.node, tgt.node, trigger);
    }
  }
  return edges;
}

/**
 * The label text of a standalone label segment (the middle segment of an
 * `src -- "label" --> tgt` statement): a quoted string, a bare label, or
 * ''. A fragment carrying an operator character is not a label. Mirrors the
 * Go brief's `briefSegmentLabel`.
 */
function segmentLabel(seg: string): string {
  const s = seg.trim();
  if (s === '') return '';
  if (/[<>=]/.test(s)) return '';
  const m = /^\s*(?:"([^"]*)"|([^"|]+?))\s*$/.exec(s);
  if (!m) return '';
  if (m[1] !== undefined && m[1] !== '') return m[1];
  return (m[2] ?? '').trim();
}

/**
 * The edges of one flow file touching the screen: the line-scan read
 * (authoritative — it reads the edge labels the subset parser drops) merged
 * with the parser's edge list (a defensive backfill for operator shapes the
 * scan cannot split, e.g. dotted `-.->` links), with the §5g direction
 * split: a self-edge is 'both' (it lands in both the in and the out list),
 * an edge out of the screen is 'out' (the far stem is the target), and the
 * rest are 'in' (the far stem is the source). Triggers are the flow's own
 * edge labels, so the workbench and the flow canvas never disagree about an
 * edge.
 */
export function flowEdgesForBrief(text: string, stem: string): ScreenFlowEdgeBase[] {
  const edges: ScreenFlowEdgeBase[] = [];
  if (!text || !stem) return edges;
  const graph = parseFlowGraph(text);
  const merged = new Map<string, { source: string; target: string; trigger: string }>();
  for (const edge of scanFlowEdges(text)) {
    merged.set(`${edge.source}\u0000${edge.target}`, edge);
  }
  for (const edge of graph.edges) {
    const key = `${edge.source}\u0000${edge.target}`;
    if (!merged.has(key)) merged.set(key, { source: edge.source, target: edge.target, trigger: '' });
  }
  const labelOf = (id: string): string => graph.nodes.find((node) => node.id === id)?.label ?? id;
  for (const edge of merged.values()) {
    if (edge.source !== stem && edge.target !== stem) continue;
    const base = { source: edge.source, target: edge.target, trigger: edge.trigger };
    if (edge.source === stem && edge.target === stem) {
      edges.push({ ...base, direction: 'both', otherStem: '', otherLabel: '' });
      continue;
    }
    if (edge.source === stem) {
      edges.push({ ...base, direction: 'out', otherStem: edge.target, otherLabel: labelOf(edge.target) });
    } else {
      edges.push({ ...base, direction: 'in', otherStem: edge.source, otherLabel: labelOf(edge.source) });
    }
  }
  return edges;
}

/** The brief's deterministic edge order: flow, then endpoints, then trigger. */
function byBriefEdgeOrder(a: ScreenFlowEdge, b: ScreenFlowEdge): number {
  if (a.flow !== b.flow) return a.flow < b.flow ? -1 : 1;
  if (a.source !== b.source) return a.source < b.source ? -1 : 1;
  if (a.target !== b.target) return a.target < b.target ? -1 : 1;
  return a.trigger < b.trigger ? -1 : a.trigger > b.trigger ? 1 : 0;
}

/* -------------------------------------------------------------------------- */
/* Tokens (the §5g "token refs, known vs. unknown")                            */
/* -------------------------------------------------------------------------- */

/**
 * The DTCG `{group.token}` reference, mirroring pkg/design/sync.go's
 * `tokenRefRe`: a dotted path (at least two segments) inside braces. Applied
 * with `matchAll`, so every occurrence of the wireframe's comments is seen.
 */
export const SCREEN_TOKEN_REF_RE = /\{\s*([A-Za-z][A-Za-z0-9]*(?:\.[A-Za-z0-9_-]+)+)\s*\}/g;

/**
 * The token refs a wireframe carries (its `{group.token}` references),
 * de-duplicated and sorted by path, each resolved against the tree's known
 * DTCG leaves. Mirrors the Go brief's `briefWireframeTokenRefs`.
 */
export function tokenRefsOf(text: string, knownPaths: ReadonlySet<string>): ScreenTokenRef[] {
  const refs: ScreenTokenRef[] = [];
  if (!text) return refs;
  const seen = new Set<string>();
  for (const match of text.matchAll(SCREEN_TOKEN_REF_RE)) {
    const path = match[1];
    if (!path || seen.has(path)) continue;
    seen.add(path);
    refs.push({ path, known: knownPaths.has(path) });
  }
  refs.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  return refs;
}

/**
 * The DTCG leaf paths the token files define — the set a reference resolves
 * against. The same walk the Tokens tab renders (`tokenFileModel`), so a
 * ref is "known" exactly when the token palette lists it. Deliberate
 * divergence from the Go brief, whose known set resolves through the
 * alias-aware export projection: plain trees agree, only alias chains differ.
 */
export function knownTokenPaths(tokenTexts: Record<string, string> | null | undefined): Set<string> {
  const known = new Set<string>();
  for (const [path, text] of Object.entries(tokenTexts ?? {})) {
    const name = path.split('/').pop() ?? path;
    for (const token of tokenFileModel(path, text, name).tokens) known.add(token.path);
  }
  return known;
}

/* -------------------------------------------------------------------------- */
/* Feedback (the §5g "open feedback")                                          */
/* -------------------------------------------------------------------------- */

/**
 * The open (unresolved) annotations as "[area] note" strings in file order,
 * mirroring the Go brief's `briefOpenAnnotationNotes` (a note without an area
 * renders bare; an area without a note renders bracketed).
 */
export function openAnnotationNotes(annotations: readonly DesignFeedbackAnnotation[] | null | undefined): string[] {
  const notes: string[] = [];
  for (const annotation of annotations ?? []) {
    if (annotation.resolved) continue;
    const note = (annotation.note ?? '').trim();
    const area = (annotation.area ?? '').trim();
    if (area && note) notes.push(`[${area}] ${note}`);
    else if (note) notes.push(note);
    else if (area) notes.push(`[${area}]`);
  }
  return notes;
}

/**
 * The §4d feedback facet: the inventory entry (existence/counts) + the parsed file (notes/resolution).
 *
 * Matching is by file name (the read key `design/feedback/<stem>.json`). The
 * Go brief's `briefFeedbackMatches` additionally accepts a file whose `target`
 * field names the screen even when the file's own name differs — a case this
 * name-keyed client read cannot discover without reading every feedback file.
 * The webui's own feedback writer names files for their target, so the two
 * arms agree on webui-authored trees.
 */
export function feedbackModelOf(
  entry: DesignFeedbackEntry | null | undefined,
  file: DesignFeedbackFile | null | undefined,
): ScreenFeedbackModel {
  const open = entry
    ? entry.annotationCount - entry.resolvedCount
    : (file?.annotations ?? []).filter((annotation) => !annotation.resolved).length;
  const status = entry?.status ?? file?.status ?? '';
  const model: ScreenFeedbackModel = {
    path: entry?.path ?? '',
    status,
    open,
    total: entry?.annotationCount ?? file?.annotations?.length ?? 0,
    pending: status === 'changes-requested' || open > 0,
    resolution: file?.resolution ?? '',
    notes: openAnnotationNotes(file?.annotations),
  };
  return model;
}

/* -------------------------------------------------------------------------- */
/* The brief                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * Derive the §5g screen brief for a stem from the inventory + the raw file
 * texts the container read. Deterministic: the same inputs yield the same
 * brief, and every list is sorted by a canonical key (the Go brief's
 * determinism property, kept client-side).
 */
export function deriveScreenBrief(input: ScreenBriefInput): ScreenBriefModel {
  const stem = input.stem;
  const { inventory } = input;

  const wireframeEntry = inventory.wireframes.find((entry) => stemOf(entry.name) === stem) ?? null;
  const screenEntry = inventory.screens.find((entry) => stemOf(entry.name) === stem) ?? null;
  // The canonical design/ paths are the not-found pointers (the Go brief
  // names where the wireframe *would* live); inventory paths win when present.
  const wireframe = wireframeEntry?.path ?? `design/wireframes/${stem}.svg`;
  const screenFile = screenEntry?.path ?? `design/screens/${stem}.html`;

  const key = stem.toLowerCase();
  const listings = parseManifestListings(input.readmeText ?? '');
  const status = listings.statuses[key] ?? '';
  const purpose = listings.summaries[key] ?? '';
  const listedInReadme = listings.listed.includes(key);

  // Flow edges touching the screen: every readable flow, in inventory order.
  const flowsIn: ScreenFlowEdge[] = [];
  const flowsOut: ScreenFlowEdge[] = [];
  for (const flow of inventory.flows ?? []) {
    const text = input.flowTexts?.[flow.path];
    if (!text) continue; // an unreadable flow is skipped, never a hard error
    const flowName = (flow.name || flow.path.split('/').pop() || '').replace(/\.mmd$/, '');
    for (const edge of flowEdgesForBrief(text, stem)) {
      const full: ScreenFlowEdge = { ...edge, flow: flow.path, flowName };
      if (edge.direction === 'in' || edge.direction === 'both') flowsIn.push(full);
      if (edge.direction === 'out' || edge.direction === 'both') flowsOut.push(full);
    }
  }
  flowsIn.sort(byBriefEdgeOrder);
  flowsOut.sort(byBriefEdgeOrder);

  const known = knownTokenPaths(input.tokenTexts);
  const tokenRefs = tokenRefsOf(input.wireframeText ?? '', known);
  const tokenGroups = (inventory.tokenGroups ?? []).map((group) => group.name);

  const feedbackEntry = (inventory.feedback ?? []).find((entry) => stemOf(entry.name) === stem) ?? null;
  const feedback = feedbackModelOf(feedbackEntry, input.feedback ?? null);

  const found = wireframeEntry !== null || feedback.path !== '';
  const guidance = found
    ? ''
    : `No wireframe ${wireframe} and no feedback targeting "${stem}". Check the screen name, or list the design tree's screens and flows (design_assets).`;

  return {
    screenName: stem,
    found,
    purpose,
    status,
    listedInReadme,
    wireframe,
    wireframeExists: wireframeEntry !== null,
    screenFile,
    screenFileExists: screenEntry !== null,
    flowsIn,
    flowsOut,
    tokenRefs,
    tokenGroups,
    feedback,
    guidance,
  };
}
