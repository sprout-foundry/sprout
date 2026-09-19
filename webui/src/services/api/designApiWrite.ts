/**
 * Design write-back paths — the mutation half of the design domain API
 * (SP-140-3 §3c/§3f, SP-140-4 §4d). Split from `designApi.ts` (which keeps the
 * inventory/read half) so both modules stay under the AGENTS.md 500-line rule.
 *
 * Everything here is built over the existing workspace-file endpoint
 * (`POST /api/file`) — zero new HTTP endpoints, no backend changes. Each
 * function takes its transport as a parameter so tests can inject a mock and a
 * host can pass the consent-aware write (`writeFileWithConsent`), matching the
 * filesApi/designApi convention.
 *
 * `writeAsset` is the Screens tab's `LivePreview` write-back path (§3c): the
 * detail pane's edits go through the same file-write pathway as any other
 * design asset.
 */

import { designRootPath, fileUrl } from './designApiPaths';
import type { DesignFeedbackFile, DesignLayoutSidecar, DesignWriteResult } from './types';

async function writeDesignFile(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  writeFn: typeof fetch | undefined,
  failure: string,
): Promise<DesignWriteResult> {
  const response = await (writeFn ?? fetchFn)(fileUrl(path), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content }),
  });
  if (!response.ok) throw new Error(failure);
  return { path: designRootPath(path), content, response };
}

/**
 * Write a design asset's text back to its file — the Screens tab's
 * `LivePreview` write-back path (SP-140-3 §3c). The path may be
 * workspace-relative (`design/screens/login.html`) or design-root-relative
 * (`screens/login.html`); both normalise through `designRootPath`. Passing the
 * consent-aware write function (`writeFileWithConsent`) keeps an off-workspace
 * target behind the usual consent prompt.
 */
export function writeAsset(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  writeFn?: typeof fetch,
): Promise<DesignWriteResult> {
  return writeDesignFile(fetchFn, path, content, writeFn, `Failed to write design asset: ${designRootPath(path)}`);
}

/**
 * Write `design/flows/<name>.layout.json` — SP-140 invariant 2 sidecar.
 * Pass the consent-aware write function (writeFileWithFetch /
 * writeFileWithConsent) as `writeFn`.
 */
export function writeLayout(
  fetchFn: typeof fetch,
  name: string,
  sidecar: DesignLayoutSidecar,
  writeFn?: typeof fetch,
): Promise<DesignWriteResult> {
  const stem = name.replace(/\.layout\.json$/, '').replace(/\.mmd$/, '');
  const path = `flows/${stem}.layout.json`;
  const payload: DesignLayoutSidecar = {
    nodes: sidecar.nodes ?? {},
    layoutHint: sidecar.layoutHint ?? '',
    derivedFrom: sidecar.derivedFrom ?? '',
  };
  return writeDesignFile(
    fetchFn,
    path,
    JSON.stringify(payload, null, 2),
    writeFn,
    `Failed to write layout sidecar: ${designRootPath(path)}`,
  );
}

/**
 * Write `design/feedback/<target>.json` in the SP-140-4d schema (including
 * the `resolution` field and per-annotation `resolved` flags).
 */
export function writeFeedback(
  fetchFn: typeof fetch,
  target: string,
  json: DesignFeedbackFile,
  writeFn?: typeof fetch,
): Promise<DesignWriteResult> {
  const stem = target.replace(/\.json$/, '');
  const path = `feedback/${stem}.json`;
  const payload: DesignFeedbackFile = {
    target: json.target ?? target,
    status: json.status ?? '',
    resolution: json.resolution ?? '',
    annotations: (json.annotations ?? []).map((a) => ({
      id: a.id ?? '',
      at: { x: a.at?.x ?? 0, y: a.at?.y ?? 0 },
      area: a.area ?? '',
      note: a.note ?? '',
      resolved: a.resolved ?? false,
      created: a.created ?? '',
    })),
  };
  return writeDesignFile(
    fetchFn,
    path,
    JSON.stringify(payload, null, 2),
    writeFn,
    `Failed to write feedback: ${designRootPath(path)}`,
  );
}
