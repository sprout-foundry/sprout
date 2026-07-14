import type { ReactNode } from 'react';

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

export interface SemanticSearchResult {
  file: string;
  name: string;
  signature: string;
  start_line: number;
  end_line: number;
  language: string;
  similarity: number;
  type: string; // "code_unit" or "file"
  cluster_id?: number; // 0 or undefined = not clustered, 1+ = cluster group
}

export interface SemanticSearchResponse {
  results: SemanticSearchResult[];
  duplicate_clusters: DuplicateCluster[];
  query: string;
  total: number;
  duration: string;
}

export interface DuplicateCluster {
  files: string[];
  similarity: number;
  count?: number; // number of results in this cluster (may be undefined from backend)
}

export interface SearchViewProps {
  onFileClick?: (filePath: string, lineNumber?: number) => void;
}

export interface SearchContextMenuState {
  x: number;
  y: number;
  filePath: string;
  lineNumber?: number;
  matchText?: string;
  isFileHeader: boolean;
}

/** State + actions exposed by useSearchState hook. */
export interface SearchState {
  // Query state
  searchQuery: string;
  replaceQuery: string;
  setSearchQuery: (q: string) => void;
  setReplaceQuery: (q: string) => void;

  // Option toggles
  caseSensitive: boolean;
  wholeWord: boolean;
  useRegex: boolean;
  semanticMode: boolean;
  toggleCaseSensitive: () => void;
  toggleWholeWord: () => void;
  toggleRegex: () => void;
  toggleSemanticMode: () => void;

  // Results
  results: SearchResult[] | null;
  filteredResults: SearchResult[] | null;
  semanticResults: SemanticSearchResult[] | null;
  semanticDuration: string | null;
  /** Optional informational note returned by semantic search (e.g. unavailable in browser mode). */
  semanticNote: string | null;
  duplicateClusters: DuplicateCluster[] | null;
  totalMatches: number;
  totalFiles: number;
  truncated: boolean;
  displayMatches: number;
  displayFiles: number;

  // Status
  isSearching: boolean;
  error: string | null;
  replaceStatus: string | null;

  // Replace
  showReplace: boolean;
  setShowReplace: (v: boolean) => void;
  handleReplace: () => Promise<void>;

  // Exclude patterns
  excludePatterns: string;
  setExcludePatterns: (p: string) => void;

  // Semantic
  semanticThreshold: number;
  setSemanticThreshold: (v: number) => void;
  indexStatus: { available: boolean; initialized: boolean; building: boolean; record_count: number } | null;
  isBuilding: boolean;

  // Expansion
  expandedFiles: Set<string>;
  toggleFile: (filePath: string) => void;

  // Actions
  handleSearchChange: (e: React.ChangeEvent<HTMLInputElement>) => void;
  handleSearchKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
  handleClear: () => void;
  handleFileClick: (filePath: string, lineNumber?: number) => void;
}

/** Callbacks for rendering result rows. */
export interface ResultRowCallbacks {
  onFileClick: (filePath: string, lineNumber?: number) => void;
  onRowContextMenu: (e: React.MouseEvent, filePath: string, lineNumber: number, lineText: string) => void;
  onFileHeaderContextMenu: (e: React.MouseEvent, filePath: string) => void;
  toggleFile: (filePath: string) => void;
  highlightMatch: (line: string, colStart: number, colEnd: number) => ReactNode;
  expandedFiles: Set<string>;
}

/** Callbacks for semantic result rendering. */
export interface SemanticResultCallbacks {
  onFileClick: (filePath: string, lineNumber?: number) => void;
  onMouseEnter: (e: React.MouseEvent<HTMLDivElement>, result: SemanticSearchResult) => void;
  onMouseLeave: () => void;
}

export const DEBOUNCE_DELAY = 300;
