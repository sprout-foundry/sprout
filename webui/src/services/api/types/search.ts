/**
 * Search (text + semantic) API types.
 */

export interface SearchMatch {
  line_number: number;
  line: string;
  column_start: number;
  column_end: number;
  context_before: string[];
  context_after: string[];
}

export interface SearchResult {
  file: string;
  matches: SearchMatch[];
  match_count: number;
}

export interface SearchOptions {
  case_sensitive?: boolean;
  whole_word?: boolean;
  regex?: boolean;
  include?: string;
  exclude?: string;
  max_results?: number;
  context_lines?: number;
}

export interface SearchResponse {
  results: SearchResult[];
  total_matches: number;
  total_files: number;
  truncated: boolean;
  query: string;
}

export interface SearchReplaceMatch {
  line_number: number;
  old_line: string;
  new_line: string;
  column_start: number;
  column_end: number;
}

export interface SearchReplaceChange {
  file: string;
  matches: SearchReplaceMatch[];
  changed_lines: number;
}

export interface SearchReplaceRequest {
  search: string;
  replace: string;
  files: string[];
  case_sensitive?: boolean;
  whole_word?: boolean;
  regex?: boolean;
  preview: boolean;
}

export interface SearchReplaceResponse {
  changes: SearchReplaceChange[];
  total_changes: number;
  preview: boolean;
}

export interface SemanticSearchOptions {
  top_k?: number;
  threshold?: number;
}

export interface SemanticSearchResult {
  file: string;
  name: string;
  signature: string;
  start_line: number;
  end_line: number;
  language: string;
  similarity: number;
  type: string; // "code_unit" or "file"
}

export interface SemanticSearchDuplicateCluster {
  files: string[];
  similarity: number;
}

export interface SemanticSearchResponse {
  results: SemanticSearchResult[];
  duplicate_clusters: SemanticSearchDuplicateCluster[];
  query: string;
  total: number;
  duration: string;
  /** Optional informational note (e.g. when unavailable in browser mode). */
  note?: string;
}

export interface SemanticSearchStatusResponse {
  available: boolean;
  initialized: boolean;
  building: boolean;
  record_count: number;
  workspace: string;
  init_error?: string;
}

export interface SemanticSearchPreviewResponse {
  file: string;
  start_line: number;
  snippet: Array<{
    line_number: number;
    content: string;
    is_context: boolean;
  }>;
  total_lines: number;
}
