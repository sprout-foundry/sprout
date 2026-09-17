/**
 * Design workspace (SP-140-3) API types — shared shapes for the design/ tree
 * data-access module (services/api/designApi.ts). Split out of designApi.ts
 * to stay under the 500-line file rule.
 */

export type DesignAssetKind =
  | 'manifest'
  | 'wireframe'
  | 'screen'
  | 'flow'
  | 'layout'
  | 'tokens'
  | 'feedback'
  | 'brand'
  | 'icon';

export interface DesignAssetEntry {
  /** Path relative to the design/ root (e.g. "wireframes/login.svg"). */
  path: string;
  /** Last path segment. */
  name: string;
  kind: DesignAssetKind;
  size: number;
  modified: number;
  /**
   * README status marker (`draft`/`review`/`ready`, SP-140-1 §1e) when the
   * manifest lists this asset; '' when unlisted. Populated by `listAssets`
   * for screens (the Screens tab's status chip).
   */
  status?: string;
}

export interface DesignTokenGroup {
  name: string;
  path: string;
  tokenCount: number;
  /** Distinct `$type` values seen in this file, in first-seen order. */
  types: string[];
}

export interface DesignFlowSummary {
  name: string;
  path: string;
  nodeCount: number;
  edgeCount: number;
  /** mermaid `flowchart <dir>` orientation hint, when declared. */
  direction: string;
}

export interface DesignFrame {
  name: string;
  width: number;
  height: number;
}

export interface DesignManifestSummary {
  path: string;
  exists: boolean;
  /** Device frames parsed from the manifest frames: block. */
  frames: DesignFrame[];
  chars: number;
}

export interface DesignFeedbackEntry {
  name: string;
  path: string;
  /** Top-level status (e.g. "changes-requested"); '' when absent/unparseable. */
  status: string;
  annotationCount: number;
  /** Count of annotations with `resolved: true`. */
  resolvedCount: number;
}

export interface DesignInventory {
  exists: boolean;
  manifest: DesignManifestSummary;
  assets: DesignAssetEntry[];
  wireframes: DesignAssetEntry[];
  screens: DesignAssetEntry[];
  flows: DesignAssetEntry[];
  layouts: DesignAssetEntry[];
  tokenFiles: DesignAssetEntry[];
  feedback: DesignFeedbackEntry[];
  tokenGroups: DesignTokenGroup[];
  tokenCount: number;
  flowSummaries: DesignFlowSummary[];
  summary: string;
}

/** Sidecar persisted by the canvas — SP-140 invariant 2 (derivedFrom hash). */
export interface DesignLayoutSidecar {
  nodes: Record<string, { x: number; y: number }>;
  layoutHint: string;
  derivedFrom: string;
}

export interface DesignFeedbackAnnotation {
  id: string;
  at: { x: number; y: number };
  area: string;
  note: string;
  resolved: boolean;
  created: string;
}

/** design/feedback/<target>.json per SP-140-4 §4d. */
export interface DesignFeedbackFile {
  target: string;
  status: string;
  resolution: string;
  annotations: DesignFeedbackAnnotation[];
}

export interface DesignWriteResult {
  path: string;
  content: string;
  response: Response;
}
