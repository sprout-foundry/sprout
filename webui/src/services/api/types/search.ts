/**
 * Search API types.
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
