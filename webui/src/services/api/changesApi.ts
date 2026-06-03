/**
 * Agent Changes domain API — mirrors the LLM-facing tools
 * (list_changes / show_my_change / summarize_my_session /
 * my_recent_changes / revert_my_changes). The JSON shapes here
 * MUST stay in sync with pkg/agent/tool_handlers_changes.go and
 * pkg/agent/tool_handlers_recover.go.
 */

// ── Response types (match Go envelopes) ─────────────────────────────

export interface SessionChangeEntry {
  path: string;
  // "bulk" is a rollup placeholder emitted by the change tracker when a
  // single shell command churns more files than the per-command volume
  // threshold. `path` then names the offending top-level directory
  // (trailing "/"), `bulk_count` is set, and per-file recovery is not
  // available — the UI renders it as a single build-output row.
  op: 'create' | 'edit' | 'delete' | 'bulk' | string;
  tool: string;
  timestamp: string; // RFC3339
  recoverable: boolean;
  bulk_count?: number;
}

export interface SessionChangesResponse {
  revision_id: string;
  enabled: boolean;
  count: number;
  files: SessionChangeEntry[];
}

export interface ChangeDiffResponse {
  found: boolean;
  path: string;
  op?: string;
  tool?: string;
  stats?: string;
  diff?: string;
}

export interface SessionSummaryBlock {
  started_at: string;
  ended_at: string;
  tools: Record<string, number>;
  files: Array<{ path: string; op: string }>;
}

export interface SessionSummaryResponse {
  enabled: boolean;
  blocks: SessionSummaryBlock[];
  totals: { changes: number; files: number };
}

export interface TimelineItem {
  path: string;
  op: string;
  tool: string;
  source: 'session' | 'persisted';
  revision_id?: string;
  timestamp: string;
  tier?: string;
}

export interface TimelineResponse {
  since?: string;
  count: number;
  items: TimelineItem[];
}

export interface RevertEntry {
  path: string;
  action: string;
  ok: boolean;
  message?: string;
}

export interface RevertResponse {
  restored: number;
  failed: number;
  summary: string;
  entries?: RevertEntry[];
}

// ── Endpoints ───────────────────────────────────────────────────────

export interface SessionChangesFilter {
  since?: string;        // RFC3339
  tool?: string;
  path_pattern?: string; // glob
}

export async function getSessionChanges(
  fetchFn: typeof fetch,
  filter: SessionChangesFilter = {},
): Promise<SessionChangesResponse> {
  const params = new URLSearchParams();
  if (filter.since) params.set('since', filter.since);
  if (filter.tool) params.set('tool', filter.tool);
  if (filter.path_pattern) params.set('path_pattern', filter.path_pattern);
  const qs = params.toString();
  const url = qs ? `/api/changes/session?${qs}` : '/api/changes/session';
  const response = await fetchFn(url);
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to fetch session changes: HTTP ${response.status}`);
  }
  return response.json();
}

export async function getChangeDiff(
  fetchFn: typeof fetch,
  path: string,
): Promise<ChangeDiffResponse> {
  const params = new URLSearchParams({ path });
  const response = await fetchFn(`/api/changes/diff?${params.toString()}`);
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to fetch change diff: HTTP ${response.status}`);
  }
  return response.json();
}

export async function getSessionSummary(
  fetchFn: typeof fetch,
): Promise<SessionSummaryResponse> {
  const response = await fetchFn('/api/changes/summary');
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to fetch session summary: HTTP ${response.status}`);
  }
  return response.json();
}

export async function getChangesTimeline(
  fetchFn: typeof fetch,
  since?: string,
): Promise<TimelineResponse> {
  const url = since
    ? `/api/changes/timeline?since=${encodeURIComponent(since)}`
    : '/api/changes/timeline';
  const response = await fetchFn(url);
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to fetch changes timeline: HTTP ${response.status}`);
  }
  return response.json();
}

export interface RevertRequest {
  scope?: 'all' | string;
  file?: string;
  since?: string;
}

export async function revertChanges(
  fetchFn: typeof fetch,
  req: RevertRequest,
): Promise<RevertResponse> {
  const response = await fetchFn('/api/changes/revert', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to revert changes: HTTP ${response.status}`);
  }
  return response.json();
}
