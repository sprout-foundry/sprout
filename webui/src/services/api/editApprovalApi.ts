/**
 * Edit Approval API — client for the per-hunk diff approval endpoints (SP-072).
 *
 * All functions accept a fetch function (from useSproutFetch or clientFetch)
 * so they work in both local and cloud modes. The injected transport adds the
 * X-Sprout-Client-ID header, SSH-proxy URL prefixing, and credentials — so
 * this module must NOT call raw `fetch` directly.
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
export async function getPendingEdit(fetchFn: typeof fetch, editId: string): Promise<PendingEdit> {
  const response = await fetchFn(`/api/edits/${encodeURIComponent(editId)}`);
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to fetch edit' }));
    throw new Error(data.message || data.error || `Failed to fetch edit ${editId}`);
  }
  return response.json();
}

/** Submit the user's per-hunk accept/reject decision. */
export async function submitEditDecision(
  fetchFn: typeof fetch,
  editId: string,
  decision: EditDecision,
): Promise<{ edit_id: string; decided: boolean }> {
  const id = encodeURIComponent(editId);
  const response = await fetchFn(`/api/edits/${id}/decision`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(decision),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to submit decision' }));
    throw new Error(data.message || data.error || `Failed to submit decision for ${editId}`);
  }
  return response.json();
}
