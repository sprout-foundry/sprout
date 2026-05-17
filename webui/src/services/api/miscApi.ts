/**
 * Stats/Health/Providers/Misc domain API — adapter-aware operations.
 */

import type {
  StatsResponse,
  ProviderOption,
  ProviderModelsResponse,
  ChangelogResponse,
  ChangesResponse,
  RevisionDetailResponse,
  RollbackResponse,
  DeepReviewResponse,
  DeepReviewFixResponse,
  DeepReviewFixStartResponse,
  DeepReviewFixStatusResponse,
} from './types';

// ── Stats ──────────────────────────────────────────────────────────

export async function getStats(fetchFn: typeof fetch): Promise<StatsResponse> {
  const response = await fetchFn('/api/stats');
  if (!response.ok) throw new Error('Failed to fetch stats');
  return response.json();
}

// ── Health ─────────────────────────────────────────────────────────

export async function checkHealth(fetchFn: typeof fetch): Promise<boolean> {
  try {
    const response = await fetchFn('/health');
    return response.ok;
  } catch {
    return false;
  }
}

// ── Providers ──────────────────────────────────────────────────────

export async function getProviders(
  fetchFn: typeof fetch,
): Promise<{ providers: ProviderOption[]; current_provider?: string; current_model?: string }> {
  const response = await fetchFn('/api/providers');
  if (!response.ok) throw new Error('Failed to fetch providers');
  return response.json();
}

export async function getProviderModels(fetchFn: typeof fetch, provider: string): Promise<ProviderModelsResponse> {
  const params = new URLSearchParams({ provider });
  const response = await fetchFn(`/api/providers/models?${params.toString()}`);
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to fetch models: HTTP ${response.status}`);
  }
  return response.json();
}

// ── Changelog / Revisions ──────────────────────────────────────────

export async function getChangelog(fetchFn: typeof fetch): Promise<ChangelogResponse> {
  const cacheBuster = Date.now();
  const response = await fetchFn(`/api/history/changelog?_=${cacheBuster}`, { cache: 'no-store' });
  if (!response.ok) throw new Error('Failed to fetch changelog');
  return response.json();
}

export async function getChanges(fetchFn: typeof fetch): Promise<ChangesResponse> {
  const cacheBuster = Date.now();
  const response = await fetchFn(`/api/history/changes?_=${cacheBuster}`, { cache: 'no-store' });
  if (!response.ok) throw new Error('Failed to fetch changes');
  return response.json();
}

export async function getRevisionDetails(fetchFn: typeof fetch, revisionId: string): Promise<RevisionDetailResponse> {
  const cacheBuster = Date.now();
  const response = await fetchFn(
    `/api/history/revision?revision_id=${encodeURIComponent(revisionId)}&_=${cacheBuster}`,
    { cache: 'no-store' },
  );
  if (!response.ok) throw new Error('Failed to fetch revision details');
  return response.json();
}

export async function rollbackToRevision(fetchFn: typeof fetch, revisionId: string): Promise<RollbackResponse> {
  const response = await fetchFn('/api/history/rollback', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision_id: revisionId }),
  });
  if (!response.ok) throw new Error('Failed to rollback revision');
  return response.json();
}

// ── Review ─────────────────────────────────────────────────────────

export async function generateDeepReview(fetchFn: typeof fetch): Promise<DeepReviewResponse> {
  const response = await fetchFn('/api/git/deep-review', { method: 'POST' });
  if (!response.ok) throw new Error('Failed to generate deep review');
  return response.json();
}

export async function fixFromDeepReview(fetchFn: typeof fetch, reviewOutput: string): Promise<DeepReviewFixResponse> {
  const response = await fetchFn('/api/git/deep-review/fix', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ review_output: reviewOutput }),
  });
  if (!response.ok) throw new Error('Failed to fix from review');
  return response.json();
}

export async function startFixFromDeepReview(
  fetchFn: typeof fetch,
  reviewOutput: string,
  options?: { fixPrompt?: string; selectedItems?: string[] },
): Promise<DeepReviewFixStartResponse> {
  const response = await fetchFn('/api/git/deep-review/fix/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      review_output: reviewOutput,
      fix_prompt: options?.fixPrompt,
      selected_items: options?.selectedItems,
    }),
  });
  if (!response.ok) throw new Error('Failed to start async fix');
  return response.json();
}

export async function getFixFromDeepReviewStatus(
  fetchFn: typeof fetch,
  jobId: string,
  since = 0,
): Promise<DeepReviewFixStatusResponse> {
  const response = await fetchFn(`/api/git/deep-review/fix/status?job_id=${encodeURIComponent(jobId)}&since=${since}`);
  if (!response.ok) throw new Error('Failed to get fix status');
  return response.json();
}

// ── Support ────────────────────────────────────────────────────────

export async function exportSupportBundle(fetchFn: typeof fetch): Promise<void> {
  const response = await fetchFn('/api/support-bundle', { method: 'GET' });
  if (!response.ok) {
    // Read the error body for a descriptive message (e.g., in cloud mode the
    // server returns { error: 'Support bundles not available in cloud mode' }).
    const errData = await response.json().catch(() => ({}));
    throw new Error(String(errData.error || errData.message || `Support bundle failed: HTTP ${response.status}`));
  }

  const disposition = response.headers.get('Content-Disposition') ?? '';
  const match = disposition.match(/filename="([^"]+)"/);
  const filename = match ? match[1] : 'sprout-diagnostics.zip';

  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  URL.revokeObjectURL(url);
}
