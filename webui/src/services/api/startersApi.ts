/**
 * Starters domain API — SP-153 §153b (TODO 153.7).
 *
 * Adapter-aware access to the embedded, versioned starter catalogue
 * (pkg/webui/api_starters.go):
 *   - listStarters:       GET  /api/starters
 *   - instantiateStarter: POST /api/starters/instantiate
 *
 * Each function takes a fetch function as its first parameter (the
 * adapter-aware transport convention) and returns plain typed values.
 * The backend is trusted to return JSON, but both functions parse
 * defensively: a non-JSON or empty body degrades to a clear error rather
 * than throwing a parser exception.
 */

import type { InstantiateStarterParams, InstantiateStarterResponse, Starter, StarterManifest } from './types';

/** Defensively parse a starter-list body into a well-formed Starter[]. */
function parseStarterList(data: Record<string, unknown>): Starter[] {
  if (!Array.isArray(data.starters)) return [];
  return (data.starters as unknown[])
    .filter((s): s is Record<string, unknown> => typeof s === 'object' && s != null)
    .map((s) => ({
      id: String(s.id ?? ''),
      version: String(s.version ?? ''),
      files: Number(s.files ?? 0),
      has_manifest: s.has_manifest === true || s.has_manifest === 'true',
    }))
    .filter((s) => s.id !== '');
}

/**
 * List the embedded starters (id, version, file count, manifest presence).
 * GET /api/starters.
 */
export async function listStarters(fetchFn: typeof fetch): Promise<Starter[]> {
  const response = await fetchFn('/api/starters');
  const data = (await response.json().catch(() => ({}))) as Record<string, unknown>;
  if (!response.ok) {
    throw new Error(String(data.error ?? data.message ?? 'Failed to list starters'));
  }
  return parseStarterList(data);
}

/**
 * Instantiate a starter into a fresh project directory.
 * POST /api/starters/instantiate with { starter, path, name }.
 *
 * Resolves to the 200 body (root, starter, files, manifest). On failure the
 * server's error code+message is thrown (e.g. 404 unknown_starter, 400
 * path_required / destination_not_empty) so callers can surface it.
 */
export async function instantiateStarter(
  fetchFn: typeof fetch,
  params: InstantiateStarterParams,
): Promise<InstantiateStarterResponse> {
  const response = await fetchFn('/api/starters/instantiate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      starter: params.starter,
      path: params.path,
      name: params.name ?? '',
    }),
  });
  const data = (await response.json().catch(() => ({}))) as Record<string, unknown>;
  if (!response.ok) {
    throw new Error(String(data.error ?? data.message ?? 'Failed to instantiate starter'));
  }

  // Normalize the manifest so the returned value always satisfies
  // StarterManifest (required identity present; optional fields omitted when
  // absent, mirroring the backend's omitempty).
  const raw = (data.manifest ?? {}) as Record<string, unknown>;
  const rawStarter = (raw.starter ?? {}) as Record<string, unknown>;
  const manifest: StarterManifest = {
    starter: {
      id: String(rawStarter.id ?? ''),
      version: String(rawStarter.version ?? ''),
    },
  };
  if (typeof raw.build === 'string') manifest.build = raw.build;
  if (typeof raw.test === 'string') manifest.test = raw.test;
  if (typeof raw.dev === 'string') manifest.dev = raw.dev;
  if (typeof raw.preview === 'string') manifest.preview = raw.preview;
  if (typeof raw.dev_port === 'number') manifest.dev_port = raw.dev_port;
  if (Array.isArray(raw.routes)) {
    manifest.routes = (raw.routes as unknown[]).filter((r) => typeof r === 'string') as string[];
  }
  if (typeof raw.build_output === 'string') manifest.build_output = raw.build_output;

  return {
    root: String(data.root ?? ''),
    starter: String(data.starter ?? ''),
    files: Number(data.files ?? 0),
    manifest,
  };
}
