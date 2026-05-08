import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ChangeEvent, KeyboardEvent, MouseEvent, ReactNode } from 'react';
import ContextMenu from './ContextMenu';
import { ApiService } from '../services/api';
import { copyToClipboard } from '../utils/clipboard';
import { useLog } from '../utils/log';
import {
  Search,
  Replace,
  ChevronDown,
  ChevronUp,
  X,
  AlertCircle,
  Loader2,
  ChevronRight,
  Copy,
  FileText,
  FolderOpen,
  Ban,
  Brain,
} from 'lucide-react';
import './SearchView.css';

interface SearchMatch {
  line_number: number;
  line: string;
  column_start: number;
  column_end: number;
  context_before: string[];
  context_after: string[];
}

interface SearchResult {
  file: string;
  matches: SearchMatch[];
  match_count: number;
}

interface SemanticSearchResult {
  file: string;
  name: string;
  signature: string;
  start_line: number;
  end_line: number;
  language: string;
  similarity: number;
  type: string;  // "code_unit" or "file"
  cluster_id?: number;  // 0 or undefined = not clustered, 1+ = cluster group
}

interface SemanticSearchResponse {
  results: SemanticSearchResult[];
  duplicate_clusters: DuplicateCluster[];
  query: string;
  total: number;
  duration: string;
}

interface DuplicateCluster {
  files: string[];
  similarity: number;
  count?: number;  // number of results in this cluster (may be undefined from backend)
}

interface SearchViewProps {
  onFileClick?: (filePath: string, lineNumber?: number) => void;
}

interface SearchContextMenuState {
  x: number;
  y: number;
  filePath: string;
  lineNumber?: number;
  matchText?: string;
  isFileHeader: boolean;
}

const DEBOUNCE_DELAY = 300;

function SearchView({ onFileClick }: SearchViewProps): JSX.Element {
  const log = useLog();
  const [searchQuery, setSearchQuery] = useState('');
  const [replaceQuery, setReplaceQuery] = useState('');
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [wholeWord, setWholeWord] = useState(false);
  const [useRegex, setUseRegex] = useState(false);
  const [semanticMode, setSemanticMode] = useState(false);
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [_totalMatches, setTotalMatches] = useState(0);
  const [_totalFiles, setTotalFiles] = useState(0);
  const [truncated, setTruncated] = useState(false);
  const [semanticResults, setSemanticResults] = useState<SemanticSearchResult[] | null>(null);
  const [semanticDuration, setSemanticDuration] = useState<string | null>(null);
  const [duplicateClusters, setDuplicateClusters] = useState<DuplicateCluster[] | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showReplace, setShowReplace] = useState(false);
  const [expandedFiles, setExpandedFiles] = useState<Set<string>>(new Set());
  const [replaceStatus, setReplaceStatus] = useState<string | null>(null);
  const [excludePatterns, setExcludePatterns] = useState('');

  const [contextMenu, setContextMenu] = useState<SearchContextMenuState | null>(null);

  // Semantic threshold control state
  const [semanticThreshold, setSemanticThreshold] = useState(0.75);

  // Embedding index status
  const [indexStatus, setIndexStatus] = useState<{available: boolean; initialized: boolean; building: boolean; record_count: number} | null>(null);

  // Build progress state
  const [isBuilding, setIsBuilding] = useState(false);

  // Cluster expansion state
  const [clustersExpanded, setClustersExpanded] = useState(true);

  // Hover preview state
  const [previewData, setPreviewData] = useState<{
    file: string;
    startLine: number;
    snippet: Array<{ line_number: number; content: string; is_context: boolean }>;
  } | null>(null);
  const [previewPosition, setPreviewPosition] = useState<{ x: number; y: number } | null>(null);
  const hoverTimerRef = useRef<NodeJS.Timeout | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);

  const searchInputRef = useRef<HTMLInputElement>(null);
  const debounceTimerRef = useRef<NodeJS.Timeout | null>(null);
  const apiService = ApiService.getInstance();

  // Focus search input on mount
  useEffect(() => {
    searchInputRef.current?.focus();
  }, []);

  // Clean up hover timer on unmount
  useEffect(() => {
    return () => {
      if (hoverTimerRef.current) clearTimeout(hoverTimerRef.current);
    };
  }, []);

  // Debounced search function
  const performSearch = useCallback(
    async (query: string) => {
      if (!query.trim()) {
        setResults(null);
        setTotalMatches(0);
        setTotalFiles(0);
        setTruncated(false);
        setSemanticResults(null);
        setSemanticDuration(null);
        setDuplicateClusters(null);
        setError(null);
        return;
      }

      setIsSearching(true);
      setError(null);
      setReplaceStatus(null);

      try {
        if (semanticMode) {
          // Semantic search mode
          const response = await apiService.searchSemantic(query, {
            top_k: 20,
            threshold: semanticThreshold,
          });

          setSemanticResults(response.results || []);
          setSemanticDuration(response.duration || null);
          setDuplicateClusters(response.duplicate_clusters || null);
          setResults(null);
        } else {
          // Text-based search mode
          const response = await apiService.search(query, {
            case_sensitive: caseSensitive,
            whole_word: wholeWord,
            regex: useRegex,
            exclude: excludePatterns || undefined,
          });

          setResults(response.results || []);
          setTotalMatches(response.total_matches || 0);
          setTotalFiles(response.total_files || 0);
          setTruncated(response.truncated || false);
          setSemanticResults(null);
          setSemanticDuration(null);

          // Auto-expand if only one result
          if (response.results && response.results.length === 1) {
            setExpandedFiles(new Set([response.results[0].file]));
          }
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Search failed';
        log.error(`Search failed: ${errorMessage}`, { title: 'Search Error' });
        setError(errorMessage);
        setResults(null);
        setSemanticResults(null);
      } finally {
        setIsSearching(false);
      }
    },
    [semanticMode, caseSensitive, wholeWord, useRegex, excludePatterns, semanticThreshold, apiService, log],
  );

  // Debounced search trigger
  useEffect(() => {
    let cancelled = false;

    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }

    if (searchQuery.trim()) {
      debounceTimerRef.current = setTimeout(() => {
        if (!cancelled) {
          performSearch(searchQuery);
        }
      }, DEBOUNCE_DELAY);
    } else {
      setResults(null);
      setSemanticResults(null);
      setSemanticDuration(null);
      setTotalMatches(0);
      setTotalFiles(0);
      setTruncated(false);
    }

    return () => {
      cancelled = true;
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [searchQuery, performSearch]);

  // Re-search when semanticMode toggles (clears results and re-searches if there's a query)
  useEffect(() => {
    setResults(null);
    setSemanticResults(null);
    setSemanticDuration(null);
    setDuplicateClusters(null);
    setTotalMatches(0);
    setTotalFiles(0);
    setTruncated(false);
    if (searchQuery.trim()) {
      // Clear debounce timer if pending, then trigger immediate re-search
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
      debounceTimerRef.current = setTimeout(() => {
        performSearch(searchQuery);
      }, DEBOUNCE_DELAY);
    }
  }, [semanticMode, searchQuery, performSearch]);

  // Fetch index status when semantic mode is toggled on; auto-trigger build if needed.
  useEffect(() => {
    if (!semanticMode) return;

    const checkAndBuild = async () => {
      try {
        const status = await apiService.searchSemanticStatus();
        setIndexStatus(status);

        // Auto-trigger build if index is available but not initialized and not already building.
        if (status.available && !status.initialized && !status.building) {
          setIsBuilding(true);
          try {
            await apiService.searchSemanticBuild();
            // Poll for completion.
            const poll = async () => {
              const s = await apiService.searchSemanticStatus();
              setIndexStatus(s);
              if (s.building) {
                setTimeout(poll, 2000);
              } else {
                setIsBuilding(false);
              }
            };
            setTimeout(poll, 1000);
          } catch {
            setIsBuilding(false);
          }
        } else if (status.building) {
          setIsBuilding(true);
          // Already building, just poll.
          const poll = async () => {
            const s = await apiService.searchSemanticStatus();
            setIndexStatus(s);
            if (s.building) {
              setTimeout(poll, 2000);
            } else {
              setIsBuilding(false);
            }
          };
          setTimeout(poll, 2000);
        }
      } catch {
        setIndexStatus(null);
      }
    };

    checkAndBuild();
  }, [semanticMode, apiService]);

  // Handle search input change
  const handleSearchChange = (e: ChangeEvent<HTMLInputElement>) => {
    setSearchQuery(e.target.value);
  };

  // Handle search input key press
  const handleSearchKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      // Immediate search on Enter
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
      performSearch(searchQuery);
    } else if (e.key === 'Escape') {
      setSearchQuery('');
      setResults(null);
      setSemanticResults(null);
      setSemanticDuration(null);
      setDuplicateClusters(null);
      setTotalMatches(0);
      setTotalFiles(0);
      setTruncated(false);
      setError(null);
      searchInputRef.current?.focus();
    }
  };

  // Get relative path (strip leading ./)
  const getRelativePath = (path: string): string => {
    return path.startsWith('./') ? path.slice(2) : path;
  };

  // Get parent directory path for excluding
  const getParentDirectory = (filePath: string): string => {
    const relative = getRelativePath(filePath);
    const lastSlash = relative.lastIndexOf('/');
    if (lastSlash === -1) {
      // File is at root, exclude it directly
      return relative;
    }
    return relative.substring(0, lastSlash + 1);
  };

  // ── Semantic hover preview handlers ──────────────────────────────

  const handleSemanticResultMouseEnter = useCallback((e: MouseEvent<HTMLDivElement>, result: SemanticSearchResult) => {
    if (hoverTimerRef.current) {
      clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    hoverTimerRef.current = setTimeout(async () => {
      try {
        const data = await apiService.searchSemanticPreview(result.file, result.start_line, 10);
        setPreviewData({
          file: data.file,
          startLine: data.start_line,
          snippet: data.snippet,
        });
        // Position the tooltip near the result, avoiding off-screen right edge
        const rect = e.currentTarget.getBoundingClientRect();
        const x = rect.right + 8 + 500 > window.innerWidth
          ? rect.left - 508
          : rect.right + 8;
        setPreviewPosition({
          x,
          y: rect.top,
        });
      } catch {
        // Preview not available — silently ignore
      }
    }, 300); // 300ms hover delay before showing preview
  }, [apiService]);

  const handleSemanticResultMouseLeave = useCallback(() => {
    if (hoverTimerRef.current) {
      clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    setPreviewData(null);
    setPreviewPosition(null);
  }, []);

  // Filter results based on exclude patterns
  const filteredResults = useMemo(() => {
    if (!results || !excludePatterns.trim()) return results;

    const patterns = excludePatterns
      .split(',')
      .map((p) => p.trim())
      .filter((p) => p.length > 0);

    if (patterns.length === 0) return results;

    return results.filter((result) => {
      const relativePath = getRelativePath(result.file);
      return !patterns.some((pattern) => {
        // Check if the file path starts with the exclude pattern
        if (pattern.endsWith('/')) {
          return relativePath.startsWith(pattern) || relativePath.startsWith(`./${pattern}`);
        }
        return relativePath === pattern || relativePath === `./${pattern}`;
      });
    });
  }, [results, excludePatterns]);

  // Handle replace
  const handleReplace = async () => {
    if (!searchQuery.trim() || !replaceQuery.trim()) {
      return;
    }

    if (!filteredResults || filteredResults.length === 0) {
      return;
    }

    setIsSearching(true);
    setReplaceStatus(null);
    setError(null);

    try {
      const allFilePaths = filteredResults.map((r) => r.file);

      const response = await apiService.searchReplace({
        search: searchQuery,
        replace: replaceQuery,
        files: allFilePaths,
        case_sensitive: caseSensitive,
        whole_word: wholeWord,
        regex: useRegex,
        preview: false,
      });

      setReplaceStatus(`Replaced ${response.total_changes} occurrences`);

      // Re-search to show updated results
      await performSearch(searchQuery);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Replace failed';
      log.error(`Replace failed: ${errorMessage}`, { title: 'Replace Error' });
      setError(errorMessage);
    } finally {
      setIsSearching(false);
    }
  };

  // Toggle file expansion
  const toggleFile = useCallback((filePath: string) => {
    setExpandedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(filePath)) {
        next.delete(filePath);
      } else {
        next.add(filePath);
      }
      return next;
    });
  }, []);

  // Handle file click
  const handleFileClick = useCallback(
    (filePath: string, lineNumber?: number) => {
      onFileClick?.(filePath, lineNumber);
    },
    [onFileClick],
  );

  // Clear search
  const handleClear = useCallback(() => {
    setSearchQuery('');
    setExcludePatterns('');
    setResults(null);
    setSemanticResults(null);
    setSemanticDuration(null);
    setDuplicateClusters(null);
    setTotalMatches(0);
    setTotalFiles(0);
    setTruncated(false);
    setError(null);
    setReplaceStatus(null);
    searchInputRef.current?.focus();
  }, []);

  // Toggle case sensitivity
  const toggleCaseSensitive = useCallback(() => {
    setCaseSensitive((prev) => !prev);
  }, []);

  // Toggle whole word
  const toggleWholeWord = useCallback(() => {
    setWholeWord((prev) => !prev);
  }, []);

  // Toggle regex
  const toggleRegex = useCallback(() => {
    setUseRegex((prev) => !prev);
  }, []);

  // Toggle semantic mode
  const toggleSemanticMode = useCallback(() => {
    setSemanticMode((prev) => !prev);
  }, []);

  // Highlight match in line
  const highlightMatch = (line: string, colStart: number, colEnd: number): ReactNode => {
    if (colStart <= 0 || colEnd <= colStart || colEnd > line.length) {
      return line;
    }

    const before = line.substring(0, colStart - 1);
    const match = line.substring(colStart - 1, colEnd);
    const after = line.substring(colEnd);

    return (
      <>
        {before}
        <span className="search-match-highlight">{match}</span>
        {after}
      </>
    );
  };

  // ── Context menu handlers ──────────────────────────────────

  const handleRowContextMenu = useCallback((e: MouseEvent, filePath: string, lineNumber: number, lineText: string) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      filePath,
      lineNumber,
      matchText: lineText,
      isFileHeader: false,
    });
  }, []);

  const handleFileHeaderContextMenu = useCallback((e: MouseEvent, filePath: string) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      filePath,
      isFileHeader: true,
    });
  }, []);

  const closeContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

  // Context menu action handlers
  const handleCopyMatchText = useCallback(() => {
    if (contextMenu?.matchText !== undefined) {
      copyToClipboard(contextMenu.matchText);
    }
    closeContextMenu();
  }, [contextMenu?.matchText, closeContextMenu]);

  const handleOpenInEditor = useCallback(() => {
    if (contextMenu) {
      handleFileClick(contextMenu.filePath, contextMenu.lineNumber);
    }
    closeContextMenu();
  }, [contextMenu, handleFileClick, closeContextMenu]);

  const handleCopyFilePath = useCallback(() => {
    if (contextMenu) {
      copyToClipboard(getRelativePath(contextMenu.filePath));
    }
    closeContextMenu();
  }, [contextMenu, closeContextMenu]);

  const handleExcludeFromSearch = useCallback(
    () => {
      if (!contextMenu) return;

      let patternToExclude: string;
      if (contextMenu.isFileHeader) {
        // Exclude the file itself
        patternToExclude = getRelativePath(contextMenu.filePath);
      } else {
        // Exclude the parent directory
        patternToExclude = getParentDirectory(contextMenu.filePath);
      }

      // Check if pattern already exists in the exclude list
      const existingPatterns = excludePatterns
        .split(',')
        .map((p) => p.trim())
        .filter((p) => p.length > 0);

      if (!existingPatterns.includes(patternToExclude)) {
        const newExclude = existingPatterns.length > 0 ? `${excludePatterns},${patternToExclude}` : patternToExclude;
        setExcludePatterns(newExclude);
      }

      closeContextMenu();
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [contextMenu, excludePatterns, closeContextMenu],
  );

  // Determine the label for the exclude action
  const getExcludeLabel = (): string => {
    if (!contextMenu) return '';
    if (contextMenu.isFileHeader) {
      return getRelativePath(contextMenu.filePath);
    }
    return getParentDirectory(contextMenu.filePath);
  };

  const isAlreadyExcluded = (): boolean => {
    if (!contextMenu) return false;
    const pattern = contextMenu.isFileHeader
      ? getRelativePath(contextMenu.filePath)
      : getParentDirectory(contextMenu.filePath);
    const existing = excludePatterns
      .split(',')
      .map((p) => p.trim())
      .filter((p) => p.length > 0);
    return existing.includes(pattern);
  };

  // Compute displayed counts from filtered results (accurate even when client-side filtering is active)
  const displayMatches = useMemo(
    () => filteredResults?.reduce((sum, r) => sum + r.match_count, 0) ?? 0,
    [filteredResults],
  );
  const displayFiles = filteredResults?.length ?? 0;

  return (
    <div className="search-view">
      {/* Search input group */}
      <div className="search-input-group">
        <div className="search-input-wrapper">
          <Search className="search-input-icon" size={16} />
          <input
            ref={searchInputRef}
            type="text"
            className="search-text-input"
            placeholder={semanticMode ? 'Search by meaning...' : 'Search...'}
            value={searchQuery}
            onChange={handleSearchChange}
            onKeyDown={handleSearchKeyDown}
          />
          {searchQuery && (
            <button className="search-clear-btn" onClick={handleClear} title="Clear search" aria-label="Clear search">
              <X size={14} />
            </button>
          )}
        </div>
      </div>

      {/* Search options row */}
      <div className="search-options">
        <button
          className={`search-option-btn ${caseSensitive ? 'active' : ''}`}
          onClick={toggleCaseSensitive}
          title="Case sensitive"
          aria-pressed={caseSensitive}
        >
          <span className="option-icon">Aa</span>
        </button>
        <button
          className={`search-option-btn ${wholeWord ? 'active' : ''}`}
          onClick={toggleWholeWord}
          title="Whole word"
          aria-pressed={wholeWord}
        >
          <span className="option-icon">W</span>
        </button>
        <button
          className={`search-option-btn ${useRegex ? 'active' : ''}`}
          onClick={toggleRegex}
          title="Use regex"
          aria-pressed={useRegex}
        >
          <span className="option-icon">.*</span>
        </button>
        <button
          className={`search-option-btn ${semanticMode ? 'active' : ''}`}
          onClick={toggleSemanticMode}
          title="Semantic search (finds code by meaning, not exact text)"
          aria-pressed={semanticMode}
        >
          <Brain size={14} />
        </button>
      </div>

      {/* Semantic index status indicator */}
      {semanticMode && indexStatus && (
        <div className="search-semantic-status">
          {isBuilding || indexStatus.building ? (
            <>
              <Loader2 size={12} className="spinning" />
              Building index...
            </>
          ) : indexStatus.initialized ? (
            <>
              <span className="search-semantic-status-dot search-semantic-status-dot--active" />
              {indexStatus.record_count.toLocaleString()} items indexed
            </>
          ) : indexStatus.available ? (
            <>
              <span className="search-semantic-status-dot search-semantic-status-dot--pending" />
              Index not built yet
            </>
          ) : (
            <>
              <span className="search-semantic-status-dot search-semantic-status-dot--inactive" />
              Embedding not available
            </>
          )}
        </div>
      )}

      {/* Semantic threshold control */}
      {semanticMode && (
        <div className="search-semantic-threshold">
          <label className="search-semantic-threshold-label">
            Min relevance: {(semanticThreshold * 100).toFixed(0)}%
          </label>
          <input
            type="range"
            min="0.5"
            max="0.95"
            step="0.05"
            value={semanticThreshold}
            onChange={(e) => setSemanticThreshold(parseFloat(e.target.value))}
            className="search-semantic-threshold-slider"
          />
        </div>
      )}

      {/* Exclude patterns indicator */}
      {excludePatterns && (
        <div className="search-exclude-indicator">
          <span className="search-exclude-label">Excluding:</span>
          <span className="search-exclude-patterns">{excludePatterns}</span>
          <button
            className="search-exclude-clear"
            onClick={() => setExcludePatterns('')}
            title="Clear excludes"
            aria-label="Clear excludes"
          >
            <X size={12} />
          </button>
        </div>
      )}

      {/* Replace row — hidden in semantic mode (no text to replace) */}
      {showReplace && !semanticMode && (
        <div className="search-replace-row">
          <div className="search-input-wrapper">
            <Replace className="search-input-icon" size={16} />
            <input
              type="text"
              className="search-replace-input"
              placeholder="Replace..."
              value={replaceQuery}
              onChange={(e) => setReplaceQuery(e.target.value)}
            />
          </div>
          <button
            className="search-replace-btn"
            onClick={handleReplace}
            disabled={
              isSearching ||
              !searchQuery.trim() ||
              !replaceQuery.trim() ||
              !filteredResults ||
              filteredResults.length === 0
            }
            title="Replace all in matched files"
          >
            {isSearching ? <Loader2 size={16} className="spinning" /> : <Replace size={16} />}
          </button>
        </div>
      )}

      {/* Replace status */}
      {replaceStatus && <div className="search-replace-status">{replaceStatus}</div>}

      {/* Expand/collapse replace toggle — hidden in semantic mode */}
      {!semanticMode && (
      <button
        className="search-expand-toggle"
        onClick={() => setShowReplace(!showReplace)}
        title={showReplace ? 'Hide replace' : 'Show replace'}
      >
        {showReplace ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
      </button>
      )}

      {/* Search stats */}
      {filteredResults && (
        <div className="search-stats">
          {displayMatches} {displayMatches === 1 ? 'match' : 'matches'} in {displayFiles}{' '}
          {displayFiles === 1 ? 'file' : 'files'}
          {truncated && ' (truncated)'}
        </div>
      )}
      {semanticResults && (
        <div className="search-stats">
          {semanticResults.length} {semanticResults.length === 1 ? 'match' : 'matches'}
          {semanticDuration && <span className="search-stats-duration"> ({semanticDuration})</span>}
        </div>
      )}

      {/* Duplicate cluster summary */}
      {duplicateClusters && duplicateClusters.length > 0 && (
        <div className="search-duplicate-summary">
          <button
            className="search-duplicate-summary-header"
            onClick={() => setClustersExpanded(!clustersExpanded)}
            title={clustersExpanded ? 'Collapse cluster info' : 'Expand cluster info'}
          >
            {clustersExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            <span className="search-duplicate-summary-icon">⑙</span>
            <span>
              {duplicateClusters.reduce((sum, c) => sum + (c.count ?? c.files.length), 0)} result{duplicateClusters.reduce((sum, c) => sum + (c.count ?? c.files.length), 0) === 1 ? '' : 's'} share similar patterns across{' '}
              {new Set(duplicateClusters.flatMap(c => c.files)).size} file{new Set(duplicateClusters.flatMap(c => c.files)).size === 1 ? '' : 's'}
            </span>
          </button>
          {clustersExpanded && (
            <div className="search-duplicate-summary-content">
              {duplicateClusters.map((cluster, idx) => (
                <div key={idx} className="search-duplicate-cluster-item">
                  <span className="search-duplicate-cluster-label">
                    Cluster {idx + 1}: ~{(cluster.similarity * 100).toFixed(0)}% similar — {cluster.count ?? cluster.files.length} result{(cluster.count ?? cluster.files.length) === 1 ? '' : 's'}
                  </span>
                  <span className="search-duplicate-cluster-files">
                    {cluster.files.map(f => getRelativePath(f)).join(', ')}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Search results */}
      <div className="search-results">
        {isSearching && (
          <div className="search-loading">
            <Loader2 size={16} className="spinning" />
            <span>Searching...</span>
          </div>
        )}

        {error && (
          <div className="search-error">
            <AlertCircle size={16} />
            <span>{error}</span>
          </div>
        )}

        {filteredResults && filteredResults.length === 0 && !isSearching && !error && !semanticResults && (
          <div className="search-no-results">
            <Search size={24} />
            <span>No results found</span>
          </div>
        )}

        {filteredResults &&
          filteredResults.map((result) => {
            const isExpanded = expandedFiles.has(result.file);
            const relativePath = getRelativePath(result.file);

            return (
              <div key={result.file} className="search-file-group">
                <div
                  className="search-file-header"
                  onClick={() => toggleFile(result.file)}
                  onContextMenu={(e) => handleFileHeaderContextMenu(e, result.file)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      toggleFile(result.file);
                    }
                  }}
                >
                  <span className="search-expand-icon">
                    {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                  </span>
                  <span className="search-file-path">{relativePath}</span>
                  <span className="search-file-badge">{result.match_count}</span>
                </div>

                {isExpanded && (
                  <div className="search-file-matches">
                    {result.matches.map((match, idx) => (
                      <div key={idx} className="search-match">
                        {match.context_before.map((ctx, i) => {
                          const contextLineNumber = match.line_number - (match.context_before.length - i);
                          return (
                            <div
                              key={`before-${i}`}
                              className="search-match-row search-match-row--context search-match-row--clickable"
                              role="button"
                              tabIndex={0}
                              onClick={() => handleFileClick(result.file, contextLineNumber)}
                              onContextMenu={(e) => handleRowContextMenu(e, result.file, contextLineNumber, ctx)}
                              onKeyDown={(e) => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                  e.preventDefault();
                                  handleFileClick(result.file, contextLineNumber);
                                }
                              }}
                            >
                              <span className="search-match-line-number">{contextLineNumber}</span>
                              <div className="search-match-line">{ctx}</div>
                            </div>
                          );
                        })}
                        <div
                          className="search-match-row search-match-row--hit search-match-row--clickable"
                          role="button"
                          tabIndex={0}
                          onClick={() => handleFileClick(result.file, match.line_number)}
                          onContextMenu={(e) => handleRowContextMenu(e, result.file, match.line_number, match.line)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter' || e.key === ' ') {
                              e.preventDefault();
                              handleFileClick(result.file, match.line_number);
                            }
                          }}
                        >
                          <span className="search-match-line-number">{match.line_number}</span>
                          <div className="search-match-line">
                            {highlightMatch(match.line, match.column_start, match.column_end)}
                          </div>
                        </div>
                        {match.context_after.map((ctx, i) => {
                          const afterLineNumber = match.line_number + i + 1;
                          return (
                            <div
                              key={`after-${i}`}
                              className="search-match-row search-match-row--context search-match-row--clickable"
                              role="button"
                              tabIndex={0}
                              onClick={() => handleFileClick(result.file, afterLineNumber)}
                              onContextMenu={(e) => handleRowContextMenu(e, result.file, afterLineNumber, ctx)}
                              onKeyDown={(e) => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                  e.preventDefault();
                                  handleFileClick(result.file, afterLineNumber);
                                }
                              }}
                            >
                              <span className="search-match-line-number">{afterLineNumber}</span>
                              <div className="search-match-line">{ctx}</div>
                            </div>
                          );
                        })}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          })}

        {/* Semantic search results */}
        {semanticResults && semanticResults.length === 0 && !isSearching && !error && (
          <div className="search-no-results">
            <Search size={24} />
            <span>No semantic results found</span>
          </div>
        )}

        {semanticResults &&
          semanticResults.map((result, idx) => {
            // File-level result: compact card without line numbers
            if (result.type === 'file') {
              const hasCluster = result.cluster_id && result.cluster_id > 0;
              return (
                <div
                  key={`file-${result.file}`}
                  className={`search-semantic-result search-semantic-result--file search-match-row search-match-row--clickable ${hasCluster ? 'search-semantic-result--clustered' : ''}`}
                  role="button"
                  tabIndex={0}
                  onClick={() => handleFileClick(result.file, 1)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      handleFileClick(result.file, 1);
                    }
                  }}
                >
                  <div className="search-semantic-result-main">
                    <span className="search-semantic-result-name">File</span>
                  </div>
                  <div className="search-semantic-result-meta">
                    <span className="search-semantic-result-file">{getRelativePath(result.file)}</span>
                    <div className="search-semantic-result-similarity-bar">
                      <div
                        className="search-semantic-result-similarity-fill"
                        style={{
                          width: `${result.similarity * 100}%`,
                          backgroundColor: result.similarity > 0.85 ? 'var(--accent-success)' : 'var(--accent-primary)',
                        }}
                      />
                    </div>
                    <span className="search-semantic-result-similarity">
                      {(result.similarity * 100).toFixed(0)}%
                    </span>
                    {hasCluster && (
                      <span className="search-semantic-result-cluster-badge" title={`Cluster ${result.cluster_id}`}>
                        {result.cluster_id}
                      </span>
                    )}
                  </div>
                </div>
              );
            }

                        // Code-unit result: detailed view with line numbers and preview
            const hasCluster = result.cluster_id && result.cluster_id > 0;
            return (
              <div
                key={`${result.file}-${result.start_line}`}
                className={`search-semantic-result search-match-row search-match-row--clickable ${hasCluster ? 'search-semantic-result--clustered' : ''}`}
                role="button"
                tabIndex={0}
                onClick={() => handleFileClick(result.file, result.start_line)}
                onMouseEnter={(e) => handleSemanticResultMouseEnter(e, result)}
                onMouseLeave={handleSemanticResultMouseLeave}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    handleFileClick(result.file, result.start_line);
                  }
                }}
              >
                <div className="search-semantic-result-main">
                  <span className="search-semantic-result-name">{result.name}</span>
                  {result.signature && (
                    <span className="search-semantic-result-signature">{result.signature}</span>
                  )}
                </div>
                <div className="search-semantic-result-meta">
                  <span className="search-semantic-result-file">{getRelativePath(result.file)}</span>
                  <span className="search-semantic-result-lines">
                    {result.start_line}–{result.end_line}
                  </span>
                  {result.language && (
                    <span className="search-semantic-result-lang">{result.language}</span>
                  )}
                  <div className="search-semantic-result-similarity-bar">
                    <div
                      className="search-semantic-result-similarity-fill"
                      style={{
                        width: `${result.similarity * 100}%`,
                        backgroundColor: result.similarity > 0.85 ? 'var(--accent-success)' : 'var(--accent-primary)',
                      }}
                    />
                  </div>
                  <span className="search-semantic-result-similarity">
                    {(result.similarity * 100).toFixed(0)}%
                  </span>
                  {hasCluster && (
                    <span className="search-semantic-result-cluster-badge" title={`Cluster ${result.cluster_id}`}>
                      {result.cluster_id}
                    </span>
                  )}
                </div>
              </div>
            );})}
      </div>

      {/* Semantic hover preview tooltip */}
      {previewData && previewPosition && (
        <div
          ref={previewRef}
          className="search-semantic-preview"
          style={{
            position: 'fixed',
            left: previewPosition.x,
            top: previewPosition.y,
            zIndex: 1000,
          }}
          onMouseEnter={() => { /* keep visible when hovering over preview */ }}
          onMouseLeave={handleSemanticResultMouseLeave}
        >
          <div className="search-semantic-preview-header">
            {getRelativePath(previewData.file)}
          </div>
          <pre className="search-semantic-preview-code">
            {previewData.snippet.map((line) => (
              <div
                key={line.line_number}
                className={`search-semantic-preview-line ${line.is_context ? 'search-semantic-preview-line--context' : ''}`}
              >
                <span className="search-semantic-preview-linenum">{line.line_number}</span>
                <span className="search-semantic-preview-content">{line.content}</span>
              </div>
            ))}
          </pre>
        </div>
      )}

      {/* Context menu */}
      <ContextMenu
        isOpen={contextMenu !== null}
        x={contextMenu?.x ?? 0}
        y={contextMenu?.y ?? 0}
        onClose={closeContextMenu}
      >
        {!contextMenu?.isFileHeader && (
          <>
            <button className="context-menu-item" onClick={handleCopyMatchText} type="button">
              <Copy size={13} />
              <span className="menu-item-label">Copy line text</span>
            </button>
            <button className="context-menu-item" onClick={handleOpenInEditor} type="button">
              <FileText size={13} />
              <span className="menu-item-label">Open in editor</span>
            </button>
            <div className="context-menu-divider" />
          </>
        )}
        <button className="context-menu-item" onClick={handleCopyFilePath} type="button">
          <FileText size={13} />
          <span className="menu-item-label">Copy file path</span>
        </button>
        <div className="context-menu-divider" />
        <button
          className={`context-menu-item ${isAlreadyExcluded() ? 'disabled' : ''}`}
          onClick={handleExcludeFromSearch}
          type="button"
          disabled={isAlreadyExcluded()}
        >
          {contextMenu?.isFileHeader ? <Ban size={13} /> : <FolderOpen size={13} />}
          <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0, flex: 1 }}>
            <span className="menu-item-label">
              {contextMenu?.isFileHeader ? 'Exclude file from search' : 'Exclude folder from search'}
            </span>
            <span
              style={{
                fontSize: 10,
                color: 'var(--text-tertiary)',
                fontFamily: 'var(--font-mono)',
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
              }}
            >
              {getExcludeLabel()}
            </span>
          </div>
        </button>
      </ContextMenu>
    </div>
  );
}

export default SearchView;
