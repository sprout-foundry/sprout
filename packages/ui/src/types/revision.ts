/**
 * Revision types for @sprout/ui.
 *
 * Defines the core data structures used for revision/change tracking
 * across the UI components. These types are the single canonical source —
 * consumers in both `packages/ui` and `webui` should import from here
 * (via `@sprout/ui`).
 */

// ── Core types ─────────────────────────────────────────────────────

/** Summary of a file in a revision (no diff content). */
export interface RevisionFile {
  file_revision_hash?: string;
  path: string;
  operation: string;
  lines_added: number;
  lines_deleted: number;
}

/** Summary of a revision/changelog entry. */
export interface Revision {
  revision_id: string;
  timestamp: string;
  files: RevisionFile[];
  description: string;
}

/** Detailed file in a revision including diff content. */
export interface RevisionDetailFile extends RevisionFile {
  original_code: string;
  new_code: string;
  diff: string;
}

// ── Helper utilities ───────────────────────────────────────────────

/**
 * Normalize raw API response data into a Revision object with safe defaults.
 * Handles missing or malformed fields from the backend.
 */
export function normalizeRevision(raw: unknown): Revision {
  const r = raw as Record<string, unknown> | null | undefined;
  if (!r) {
    return {
      revision_id: 'unknown',
      timestamp: new Date().toISOString(),
      files: [],
      description: '',
    };
  }
  const files = Array.isArray(r.files)
    ? (r.files as Array<Record<string, unknown>>).map((file: Record<string, unknown>) => ({
        file_revision_hash: typeof file?.file_revision_hash === 'string' ? file.file_revision_hash : undefined,
        path: typeof file?.path === 'string' ? file.path : 'Unknown',
        operation: typeof file?.operation === 'string' ? file.operation : 'edited',
        lines_added: Number(file?.lines_added || 0),
        lines_deleted: Number(file?.lines_deleted || 0),
      }))
    : [];

  return {
    revision_id: typeof r?.revision_id === 'string' ? r.revision_id : 'unknown',
    timestamp: typeof r?.timestamp === 'string' ? r.timestamp : new Date().toISOString(),
    files,
    description: typeof r?.description === 'string' ? r.description : '',
  };
}

/**
 * Build a unique key for a revision file entry, used in React lists and
 * diff-expansion state tracking.
 */
export function buildRevisionFileKey(
  file: RevisionFile | (RevisionFile & { diff?: string }),
  index: number,
): string {
  return `${file.file_revision_hash || file.path}::${index}`;
}
