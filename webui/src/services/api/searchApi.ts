/**
 * Search domain API — adapter-aware search operations.
 */

import { SearchOptions, SearchResponse, SearchReplaceRequest, SearchReplaceResponse } from './types';

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

export async function searchReplace(fetchFn: typeof fetch, request: SearchReplaceRequest): Promise<SearchReplaceResponse> {
  const response = await fetchFn('/api/search/replace', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) throw new Error(`Replace failed: ${response.statusText}`);
  return response.json();
}
