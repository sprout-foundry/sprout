/**
 * Shared change-tracking vocabulary for the two change UIs (the Agent
 * Changes panel and the per-turn strip) and the CSS op classes they
 * both style. The ChangeTracker API is loose about op spelling — the
 * session manifest says "create"/"delete", the file_changed event says
 * "created"/"deleted" or "write", and shell bulk churn arrives as
 * "shell_bulk" — so everything funnels through classifyChangeOp.
 */

/** Visual class shared by both UIs' CSS (`.op-create` etc.). */
export type ChangeOpClass = 'create' | 'edit' | 'delete' | 'bulk';

/** Map every op spelling the backend emits to one visual class. */
export function classifyChangeOp(op: string): ChangeOpClass {
  switch (op) {
    case 'create':
    case 'created':
    case 'write':
      return 'create';
    case 'delete':
    case 'deleted':
      return 'delete';
    case 'bulk':
    case 'shell_bulk':
      return 'bulk';
    default:
      return 'edit';
  }
}

/** Human label for an op, folded to the four canonical classes. */
export function changeOpLabel(op: string): string {
  switch (classifyChangeOp(op)) {
    case 'create':
      return 'Created';
    case 'delete':
      return 'Deleted';
    case 'bulk':
      return 'Build output';
    default:
      return 'Modified';
  }
}

/**
 * Plain-language op label for builder mode: someone who builds with the
 * agent, not a developer reading a diff. Same four canonical classes as
 * `changeOpLabel` (which stays the developer vocabulary — "Created" /
 * "Modified" / "Deleted" / "Build output" — untouched for every existing
 * consumer).
 */
export function builderChangeOpLabel(op: string): string {
  switch (classifyChangeOp(op)) {
    case 'create':
      return 'New file';
    case 'delete':
      return 'Removed';
    case 'bulk':
      return 'Build output';
    default:
      return 'Updated';
  }
}

/** True when the op is the shell-churn rollup (no per-file actions). */
export function isBulkOp(op: string): boolean {
  return classifyChangeOp(op) === 'bulk';
}

/**
 * Summarize a revert outcome for the notification log. The backend
 * reports restored/failed counts; when both are zero it attached a
 * summary explaining why nothing happened (tracking disabled, stale
 * snapshot, no record) — surface that instead of a misleading success.
 */
export function describeRevertOutcome(res: { restored?: number; failed?: number; summary?: string }): {
  level: 'info' | 'error';
  message: string;
} {
  if ((res.restored ?? 0) + (res.failed ?? 0) === 0 && res.summary) {
    return { level: 'error', message: `Revert did nothing: ${res.summary}` };
  }
  return { level: 'info', message: `Revert: ${res.summary ?? ''}`.trimEnd() };
}
