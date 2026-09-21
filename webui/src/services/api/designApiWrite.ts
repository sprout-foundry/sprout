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

/** The 409 payload the §7a safe-write seam returns on a revision conflict. */
export interface WriteConflict {
  path: string;
  currentMtime?: number;
  currentHash?: string;
}

/**
 * Extract the base revision (unix-seconds mtime) from a read response, for
 * callers that load-then-edit: pass it as SafeWriteOptions.baseMtime so the
 * write is revision-checked against what was just read (§7a). Returns
 * undefined when the response carries no Last-Modified header.
 */
export function baseMtimeFromResponse(response: Response): number | undefined {
  const header = response.headers.get('Last-Modified');
  if (!header) return undefined;
  const ms = Date.parse(header);
  return Number.isFinite(ms) ? Math.floor(ms / 1000) : undefined;
}

/** Thrown when a §7a conditional write is refused (409) — nothing was written. */
export class DesignWriteConflictError extends Error {
  readonly conflict: WriteConflict;
  constructor(conflict: WriteConflict) {
    super(`The file changed after it was read (${conflict.path}); nothing was written.`);
    this.name = 'DesignWriteConflictError';
    this.conflict = conflict;
  }
}

export interface SafeWriteOptions {
  /** The write transport override (tests/hosts). */
  writeFn?: typeof fetch;
  /**
   * The revision the caller last read: the inventory's `modified` (unix
   * seconds) and/or the sha256 of the loaded text. Omitted guards are simply
   * not sent.
   */
  baseMtime?: number;
  baseHash?: string;
  /**
   * Force the write through despite a conflict (the §7b "Keep mine" path —
   * a deliberate, user-visible overwrite, not a silent one).
   */
  force?: boolean;
}

/**
 * §7a conditional variant of `writeAsset`: sends `baseMtime`/`baseHash` and
 * throws `DesignWriteConflictError` when the server answers 409 (nothing was
 * written). Everything else behaves exactly like `writeAsset`.
 */
export async function writeAssetIfUnchanged(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  options: SafeWriteOptions = {},
): Promise<DesignWriteResult> {
  const target = designRootPath(path);
  const body: Record<string, unknown> = { content };
  if (typeof options.baseMtime === 'number') body.baseMtime = options.baseMtime;
  if (options.baseHash) body.baseHash = options.baseHash;
  const response = await (options.writeFn ?? fetchFn)(fileUrl(path), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (response.status === 409 && !options.force) {
    let conflict: WriteConflict = { path: target };
    try {
      const data = (await response.json()) as Partial<WriteConflict>;
      conflict = {
        path: typeof data.path === 'string' ? data.path : target,
        currentMtime: typeof data.currentMtime === 'number' ? data.currentMtime : undefined,
        currentHash: typeof data.currentHash === 'string' ? data.currentHash : undefined,
      };
    } catch {
      // A body we cannot parse still means one thing: conflict.
    }
    throw new DesignWriteConflictError(conflict);
  }
  // force: a 409 is the expected "the file moved, overwriting anyway" answer
  // — the user decided (§7b Keep mine). Any other non-ok status is a failure.
  if (!response.ok && response.status !== 409) {
    throw new Error(`Failed to write design asset: ${target}`);
  }
  return { path: target, content, response };
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

/** The canonical §4d payload every feedback write sends (defaults filled). */
function feedbackPayload(target: string, json: DesignFeedbackFile): DesignFeedbackFile {
  return {
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
  return writeDesignFile(
    fetchFn,
    path,
    JSON.stringify(feedbackPayload(target, json), null, 2),
    writeFn,
    `Failed to write feedback: ${designRootPath(path)}`,
  );
}

/**
 * §7a conditional variant of `writeFeedback` for the pin-drag gesture
 * (SP-140-7 §7e): sends the revision guards read from the feedback file just
 * before the edit and throws `DesignWriteConflictError` on 409 — an agent
 * write landing mid-gesture surfaces instead of being silently overwritten.
 * Serialization across quick successive drags stays the caller's job (the
 * grid's persist chain). Takes the same writeFn override as `writeFeedback`
 * via `guards.writeFn`.
 */
export async function writeFeedbackIfUnchanged(
  fetchFn: typeof fetch,
  target: string,
  json: DesignFeedbackFile,
  guards: { baseMtime?: number; baseHash?: string; writeFn?: typeof fetch; force?: boolean },
): Promise<DesignWriteResult> {
  const stem = target.replace(/\.json$/, '');
  const { writeFn, ...safe } = guards;
  return writeAssetIfUnchanged(
    fetchFn,
    `feedback/${stem}.json`,
    JSON.stringify(feedbackPayload(target, json), null, 2),
    { ...safe, writeFn },
  );
}
