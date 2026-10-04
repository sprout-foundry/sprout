/**
 * Starters domain API types (SP-153 §153b, TODO 153.7).
 *
 * These mirror the JSON shapes served by pkg/webui/api_starters.go:
 *   - GET  /api/starters             → { starters: Starter[] }
 *   - POST /api/starters/instantiate → InstantiateStarterResponse
 *
 * `StarterManifest` mirrors the on-disk .sprout/starter.json schema owned by
 * pkg/startermanifest (JSON field names are the contract). The manifest is
 * deliberately lenient: only the starter identity is required, every command
 * field is optional, so the optional fields here are optional too.
 */

/** One embedded starter, as listed by GET /api/starters. */
export interface Starter {
  /** Starter id (the embedded tree's directory name). */
  id: string;
  /** Version of the embedded tree the starter carries. */
  version: string;
  /** Number of project-content files the starter's tree carries. */
  files: number;
  /** Whether the starter's descriptor (manifest) is present. */
  has_manifest: boolean;
}

/**
 * The starter manifest — the JSON document written to .sprout/starter.json on
 * instantiation. Mirrors pkg/startermanifest.StarterManifest.
 */
export interface StarterManifest {
  starter: { id: string; version: string };
  build?: string;
  test?: string;
  dev?: string;
  preview?: string;
  dev_port?: number;
  routes?: string[];
  build_output?: string;
}

/** Request body for POST /api/starters/instantiate. */
export interface InstantiateStarterParams {
  /** Starter id to instantiate (from a Starter list entry). */
  starter: string;
  /** Absolute target directory the starter is written into. */
  path: string;
  /** Optional project name (displayed/owned client-side; validated by the
   * server as a single safe name, not a path). */
  name?: string;
}

/** Response body of POST /api/starters/instantiate (200). */
export interface InstantiateStarterResponse {
  /** The (canonicalized) target directory the starter was written into. */
  root: string;
  /** The starter id that was instantiated. */
  starter: string;
  /** How many project-content files the tree carries. */
  files: number;
  /** The manifest that was written to .sprout/starter.json. */
  manifest: StarterManifest;
}
