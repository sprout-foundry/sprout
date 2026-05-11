/**
 * Search domain API — adapter-aware search operations.
 */

import type {
  SearchOptions,
  SearchResponse,
  SearchReplaceRequest,
  SearchReplaceResponse,
  SemanticSearchOptions,
  SemanticSearchResponse,
  SemanticSearchStatusResponse,
  SemanticSearchPreviewResponse,
} from './types';

export async function search(fetchFn: typeof fetch, query: string, options?: SearchOptions): Promise<SearchResponse> {
  const params = new URLSearchParams({ query });
  if (options?.case_sensitive) params.set('case_sensitive', 'true');
  if (options?.whole_word) params.set('whole_word', 'true');
  if (options?.regex) params.set('regex', 'true');
  if (options?.include) params.set('include', options.include);
  if (options?.exclude) params.set('exclude', options.exclude);
  if (options?.max_results) params.set('max_results', String(options.max_results));
  if (options?.context_lines != null) params.set('context_lines', String(options.context_lines));

  const response = await fetchFn(`/api/search?${params}`);
  if (!response.ok) throw new Error(`Search failed: ${response.statusText}`);
  return response.json();
}

export async function searchReplace(
  fetchFn: typeof fetch,
  request: SearchReplaceRequest,
): Promise<SearchReplaceResponse> {
  const response = await fetchFn('/api/search/replace', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) throw new Error(`Replace failed: ${response.statusText}`);
  return response.json();
}

export async function searchSemantic(
  fetchFn: typeof fetch,
  query: string,
  options?: SemanticSearchOptions,
): Promise<SemanticSearchResponse> {
  const params = new URLSearchParams({ query });
  if (options?.top_k) params.set('top_k', String(options.top_k));
  if (options?.threshold != null) params.set('threshold', String(options.threshold));

  const response = await fetchFn(`/api/search/semantic?${params}`);
  if (!response.ok) throw new Error(`Semantic search failed: ${response.statusText}`);
  return response.json();
}

export async function searchSemanticStatus(fetchFn: typeof fetch): Promise<SemanticSearchStatusResponse> {
  const response = await fetchFn('/api/search/semantic/status');
  if (!response.ok) throw new Error('Failed to get semantic status');
  return response.json();
}

export async function searchSemanticBuild(fetchFn: typeof fetch): Promise<{ status: string }> {
  const response = await fetchFn('/api/search/semantic/build', { method: 'POST' });
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(((data as Record<string, unknown>).error as string) || 'Failed to start build');
  }
  return response.json();
}

export async function searchSemanticPreview(
  fetchFn: typeof fetch,
  file: string,
  startLine: number,
  context?: number,
): Promise<SemanticSearchPreviewResponse> {
  const params = new URLSearchParams({ file, start_line: String(startLine) });
  if (context) params.set('context', String(context));
  const response = await fetchFn(`/api/search/semantic/preview?${params}`);
  if (!response.ok) throw new Error('Failed to get preview');
  return response.json();
}
