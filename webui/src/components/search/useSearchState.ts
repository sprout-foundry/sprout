import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ChangeEvent, KeyboardEvent } from 'react';
import { ApiService } from '../../services/api';
import { useLog } from '../../utils/log';
import type { SearchResult, SearchState } from './types';
import { DEBOUNCE_DELAY } from './types';

/** Strip leading ./ from a path. */
export function getRelativePath(path: string): string {
  return path.startsWith('./') ? path.slice(2) : path;
}

/** Get parent directory path for excluding (trailing slash). */
export function getParentDirectory(filePath: string): string {
  const relative = getRelativePath(filePath);
  const lastSlash = relative.lastIndexOf('/');
  if (lastSlash === -1) {
    return relative;
  }
  return relative.substring(0, lastSlash + 1);
}

/**
 * Encapsulates all search state, effects, and callbacks.
 * Returns a flat object consumed by SearchView and its sub-components.
 */
export function useSearchState(
  onFileClick?: (filePath: string, lineNumber?: number) => void,
  searchInputRef?: React.RefObject<HTMLInputElement | null>,
): SearchState {
  const log = useLog();
  const apiService = ApiService.getInstance();

  // ── State ────────────────────────────────────────────────────
  const [searchQuery, setSearchQuery] = useState('');
  const [replaceQuery, setReplaceQuery] = useState('');
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [wholeWord, setWholeWord] = useState(false);
  const [useRegex, setUseRegex] = useState(false);
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [totalMatches, setTotalMatches] = useState(0);
  const [totalFiles, setTotalFiles] = useState(0);
  const [truncated, setTruncated] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showReplace, setShowReplace] = useState(false);
  const [expandedFiles, setExpandedFiles] = useState<Set<string>>(new Set());
  const [replaceStatus, setReplaceStatus] = useState<string | null>(null);
  const [excludePatterns, setExcludePatterns] = useState('');

  const debounceTimerRef = useRef<NodeJS.Timeout | null>(null);

  // ── Core search function ─────────────────────────────────────

  const performSearch = useCallback(
    async (query: string) => {
      if (!query.trim()) {
        setResults(null);
        setTotalMatches(0);
        setTotalFiles(0);
        setTruncated(false);
        setError(null);
        return;
      }

      setIsSearching(true);
      setError(null);
      setReplaceStatus(null);

      try {
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

        // Auto-expand if only one result
        if (response.results && response.results.length === 1) {
          setExpandedFiles(new Set([response.results[0].file]));
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Search failed';
        log.error(`Search failed: ${errorMessage}`, { title: 'Search Error' });
        setError(errorMessage);
        setResults(null);
      } finally {
        setIsSearching(false);
      }
    },
    [caseSensitive, wholeWord, useRegex, excludePatterns, apiService, log],
  );

  // ── Debounced search trigger ─────────────────────────────────

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

  // ── Filter results by exclude patterns ───────────────────────

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
        if (pattern.endsWith('/')) {
          return relativePath.startsWith(pattern) || relativePath.startsWith(`./${pattern}`);
        }
        return relativePath === pattern || relativePath === `./${pattern}`;
      });
    });
  }, [results, excludePatterns]);

  // ── Computed display counts ──────────────────────────────────

  const displayMatches = useMemo(
    () => filteredResults?.reduce((sum, r) => sum + r.match_count, 0) ?? 0,
    [filteredResults],
  );
  const displayFiles = filteredResults?.length ?? 0;

  // ── Handlers ─────────────────────────────────────────────────

  const handleSearchChange = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    setSearchQuery(e.target.value);
  }, []);

  const handleSearchKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter') {
        if (debounceTimerRef.current) {
          clearTimeout(debounceTimerRef.current);
        }
        performSearch(searchQuery);
      } else if (e.key === 'Escape') {
        setSearchQuery('');
        setResults(null);
        setTotalMatches(0);
        setTotalFiles(0);
        setTruncated(false);
        setError(null);
        searchInputRef?.current?.focus();
      }
    },
    [performSearch, searchQuery, searchInputRef],
  );

  const handleReplace = useCallback(async () => {
    if (!searchQuery.trim() || !replaceQuery.trim()) return;
    if (!filteredResults || filteredResults.length === 0) return;

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
      await performSearch(searchQuery);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Replace failed';
      log.error(`Replace failed: ${errorMessage}`, { title: 'Replace Error' });
      setError(errorMessage);
    } finally {
      setIsSearching(false);
    }
  }, [searchQuery, replaceQuery, filteredResults, caseSensitive, wholeWord, useRegex, apiService, log, performSearch]);

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

  const handleFileClick = useCallback(
    (filePath: string, lineNumber?: number) => {
      onFileClick?.(filePath, lineNumber);
    },
    [onFileClick],
  );

  const handleClear = useCallback(() => {
    setSearchQuery('');
    setExcludePatterns('');
    setResults(null);
    setTotalMatches(0);
    setTotalFiles(0);
    setTruncated(false);
    setError(null);
    setReplaceStatus(null);
    searchInputRef?.current?.focus();
  }, [searchInputRef]);

  const toggleCaseSensitive = useCallback(() => setCaseSensitive((prev) => !prev), []);
  const toggleWholeWord = useCallback(() => setWholeWord((prev) => !prev), []);
  const toggleRegex = useCallback(() => setUseRegex((prev) => !prev), []);

  // ── Return ───────────────────────────────────────────────────

  return {
    // Query
    searchQuery,
    replaceQuery,
    setSearchQuery,
    setReplaceQuery,
    // Options
    caseSensitive,
    wholeWord,
    useRegex,
    toggleCaseSensitive,
    toggleWholeWord,
    toggleRegex,
    // Results
    results,
    filteredResults,
    totalMatches,
    totalFiles,
    truncated,
    displayMatches,
    displayFiles,
    // Status
    isSearching,
    error,
    replaceStatus,
    // Replace
    showReplace,
    setShowReplace,
    handleReplace,
    // Exclude
    excludePatterns,
    setExcludePatterns,
    // Expansion
    expandedFiles,
    toggleFile,
    // Actions
    handleSearchChange,
    handleSearchKeyDown,
    handleClear,
    handleFileClick,
  };
}
