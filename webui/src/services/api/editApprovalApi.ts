/**
 * Edit Approval API — client for the per-hunk diff approval endpoints (SP-072).
 *
 * Endpoints:
 *   GET  /api/edits/{id}            — fetch pending edit details
 *   POST /api/edits/{id}/decision   — submit accept/reject decisions
 */

export interface EditHunk {
  id: string;
  summary: string;
  add_count: number;
  del_count: number;
  lines: string[];
}

export interface PendingEdit {
  id: string;
  path: string;
  hunks: EditHunk[];
  unified_diff: string;
  decided: boolean;
}

export interface EditDecision {
  accepted_hunks: string[];
  rejected: boolean;
}

/** Fetch a pending edit proposal by ID. */
export async function getPendingEdit(editId: string): Promise<PendingEdit> {
  const resp = await fetch(`/api/edits/${editId}`);
  if (!resp.ok) {
    throw new Error(`Failed to fetch edit ${editId}: ${resp.statusText}`);
  }
  return resp.json();
}

/** Submit the user's per-hunk accept/reject decision. */
export async function submitEditDecision(
  editId: string,
  decision: EditDecision
): Promise<{ edit_id: string; decided: boolean }> {
  const resp = await fetch(`/api/edits/${editId}/decision`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(decision),
  });
  if (!resp.ok) {
    throw new Error(`Failed to submit decision for ${editId}: ${resp.statusText}`);
  }
  return resp.json();
}
