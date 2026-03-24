import React, { useState, useCallback, useRef, useEffect } from 'react';
import { ApiService } from '../services/api';
import {
  Search,
  Replace,
  ChevronDown,
  ChevronUp,
  X,
  AlertCircle,
  Loader2,
  ChevronRight,
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

interface SearchViewProps {
  onFileClick?: (filePath: string) => void;
}

const DEBOUNCE_DELAY = 300;

const SearchView: React.FC<SearchViewProps> = ({ onFileClick }) => {
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

  const searchInputRef = useRef<HTMLInputElement>(null);
  const debounceTimerRef = useRef<NodeJS.Timeout | null>(null);
  const apiService = ApiService.getInstance();

  // Focus search input on mount
  useEffect(() => {
    searchInputRef.current?.focus();
  }, []);

  // Debounced search function
  const performSearch = useCallback(async (query: string) => {
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
      console.error('Search failed:', err);
      setError(err instanceof Error ? err.message : 'Search failed');
      setResults(null);
    } finally {
      setIsSearching(false);
    }
  }, [caseSensitive, wholeWord, useRegex, apiService]);

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

  // Handle search input change
  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setSearchQuery(e.target.value);
  };

  // Handle search input key press
  const handleSearchKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      // Immediate search on Enter
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
      searchInputRef.current?.focus();
    }
  };

  // Handle replace
  const handleReplace = async () => {
    if (!searchQuery.trim() || !replaceQuery.trim()) {
      return;
    }

    if (!results || results.length === 0) {
      return;
    }

    setIsSearching(true);
    setReplaceStatus(null);
    setError(null);

    try {
      const allFilePaths = results.map(r => r.file);

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
      console.error('Replace failed:', err);
      setError(err instanceof Error ? err.message : 'Replace failed');
    } finally {
      setIsSearching(false);
    }
  };

  // Toggle file expansion
  const toggleFile = useCallback((filePath: string) => {
    setExpandedFiles(prev => {
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
  const handleFileClick = useCallback((filePath: string) => {
    onFileClick?.(filePath);
  }, [onFileClick]);

  // Clear search
  const handleClear = useCallback(() => {
    setSearchQuery('');
    setResults(null);
    setTotalMatches(0);
    setTotalFiles(0);
    setTruncated(false);
    setError(null);
    setReplaceStatus(null);
    searchInputRef.current?.focus();
  }, []);

  // Toggle case sensitivity
  const toggleCaseSensitive = useCallback(() => {
    setCaseSensitive(prev => !prev);
  }, []);

  // Toggle whole word
  const toggleWholeWord = useCallback(() => {
    setWholeWord(prev => !prev);
  }, []);

  // Toggle regex
  const toggleRegex = useCallback(() => {
    setUseRegex(prev => !prev);
  }, []);

  // Highlight match in line
  const highlightMatch = (line: string, colStart: number, colEnd: number): React.ReactNode => {
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

  // Get relative path (strip leading ./)
  const getRelativePath = (path: string): string => {
    return path.startsWith('./') ? path.slice(2) : path;
  };

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
            placeholder="Search..."
            value={searchQuery}
            onChange={handleSearchChange}
            onKeyDown={handleSearchKeyDown}
          />
          {searchQuery && (
            <button
              className="search-clear-btn"
              onClick={handleClear}
              title="Clear search"
              aria-label="Clear search"
            >
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
      </div>

      {/* Replace row */}
      {showReplace && (
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
            disabled={isSearching || !searchQuery.trim() || !replaceQuery.trim() || !results || results.length === 0}
            title="Replace all in matched files"
          >
            {isSearching ? <Loader2 size={16} className="spinning" /> : <Replace size={16} />}
          </button>
        </div>
      )}

      {/* Replace status */}
      {replaceStatus && (
        <div className="search-replace-status">{replaceStatus}</div>
      )}

      {/* Expand/collapse replace toggle */}
      <button
        className="search-expand-toggle"
        onClick={() => setShowReplace(!showReplace)}
        title={showReplace ? 'Hide replace' : 'Show replace'}
      >
        {showReplace ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
      </button>

      {/* Search stats */}
      {results && (
        <div className="search-stats">
          {totalMatches} {totalMatches === 1 ? 'match' : 'matches'} in{' '}
          {totalFiles} {totalFiles === 1 ? 'file' : 'files'}
          {truncated && ' (truncated)'}
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

        {results && results.length === 0 && !isSearching && !error && (
          <div className="search-no-results">
            <Search size={24} />
            <span>No results found</span>
          </div>
        )}

        {results && results.map((result) => {
          const isExpanded = expandedFiles.has(result.file);
          const relativePath = getRelativePath(result.file);

          return (
            <div key={result.file} className="search-file-group">
              <div
                className="search-file-header"
                onClick={() => toggleFile(result.file)}
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
                <span
                  className="search-file-path"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleFileClick(result.file);
                  }}
                >
                  {relativePath}
                </span>
                <span className="search-file-badge">{result.match_count}</span>
              </div>

              {isExpanded && (
                <div className="search-file-matches">
                  {result.matches.map((match, idx) => (
                    <div key={idx} className="search-match">
                      <span className="search-match-line-number">
                        {match.line_number}
                      </span>
                      <div className="search-match-line">
                        {highlightMatch(match.line, match.column_start, match.column_end)}
                      </div>
                      {match.context_before.length > 0 && (
                        <div className="search-match-context">
                          {match.context_before.map((ctx, i) => (
                            <div key={`before-${i}`} className="context-line">
                              <span className="search-match-line-number">
                                {match.line_number - (match.context_before.length - i)}
                              </span>
                              <span className="context-text">{ctx}</span>
                            </div>
                          ))}
                        </div>
                      )}
                      {match.context_after.length > 0 && (
                        <div className="search-match-context">
                          {match.context_after.map((ctx, i) => (
                            <div key={`after-${i}`} className="context-line">
                              <span className="search-match-line-number">
                                {match.line_number + match.context_before.length + i + 1}
                              </span>
                              <span className="context-text">{ctx}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default SearchView;
