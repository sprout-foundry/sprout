import { Search, Replace, ChevronDown, ChevronUp, X, AlertCircle, Loader2 } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import './SearchView.css';
import { highlightMatch } from './search/highlightMatch';
import SearchContextMenu, {
  createRowContextMenuHandler,
  createFileHeaderContextMenuHandler,
} from './search/SearchContextMenu';
import SearchResults from './search/SearchResults';
import type { SearchViewProps, SearchContextMenuState } from './search/types';
import { useSearchState } from './search/useSearchState';

/**
 * Search panel — text search with find/replace.
 *
 * Composition root that wires the search state hook, result renderers, and
 * context menu together.
 */
function SearchView({ onFileClick }: SearchViewProps): JSX.Element {
  const searchInputRef = useRef<HTMLInputElement>(null);

  const state = useSearchState(onFileClick, searchInputRef);

  // ── Context menu state ───────────────────────────────────────
  const [contextMenu, setContextMenu] = useState<SearchContextMenuState | null>(null);
  const closeContextMenu = useCallback(() => setContextMenu(null), []);

  // Replace-all rewrites every matched file; the first click arms it and
  // the second applies, so a stray click can't mass-edit the workspace.
  const [replaceArmed, setReplaceArmed] = useState(false);

  const onRowContextMenu = useCallback((e: MouseEvent, filePath: string, lineNumber: number, lineText: string) => {
    createRowContextMenuHandler(setContextMenu)(e, filePath, lineNumber, lineText);
  }, []);

  const onFileHeaderContextMenu = useCallback((e: MouseEvent, filePath: string) => {
    createFileHeaderContextMenuHandler(setContextMenu)(e, filePath);
  }, []);

  // ── Focus search input on mount ──────────────────────────────
  useEffect(() => {
    searchInputRef.current?.focus();
  }, []);

  // ── Destructure state for readability ────────────────────────
  const {
    searchQuery,
    replaceQuery,
    setReplaceQuery,
    caseSensitive,
    wholeWord,
    useRegex,
    toggleCaseSensitive,
    toggleWholeWord,
    toggleRegex,
    filteredResults,
    truncated,
    displayMatches,
    displayFiles,
    isSearching,
    error,
    replaceStatus,
    showReplace,
    setShowReplace,
    handleReplace,
    excludePatterns,
    setExcludePatterns,
    expandedFiles,
    toggleFile,
    handleSearchChange,
    handleSearchKeyDown,
    handleClear,
    handleFileClick,
  } = state;

  useEffect(() => {
    setReplaceArmed(false);
  }, [searchQuery, replaceQuery]);

  // ── Render ───────────────────────────────────────────────────
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
      </div>

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

      {/* Replace row */}
      {showReplace && (
        <div className="search-replace-row">
          <div className="search-input-wrapper">
            <Replace className="search-input-icon" size={16} />
            <input
              type="text"
              className="search-text-input"
              placeholder="Replace..."
              aria-label="Replace with"
              value={replaceQuery}
              onChange={(e) => setReplaceQuery(e.target.value)}
            />
          </div>
          <button
            type="button"
            className={`search-replace-btn${replaceArmed ? ' armed' : ''}`}
            onClick={() => {
              if (!replaceArmed) {
                setReplaceArmed(true);
                return;
              }
              setReplaceArmed(false);
              void handleReplace();
            }}
            onBlur={() => setReplaceArmed(false)}
            disabled={
              isSearching ||
              !searchQuery.trim() ||
              !replaceQuery.trim() ||
              !filteredResults ||
              filteredResults.length === 0
            }
            title={
              replaceArmed
                ? `Click again to replace in ${filteredResults?.length ?? 0} file(s)`
                : 'Replace all in matched files'
            }
          >
            {isSearching ? <Loader2 size={14} className="spinning" /> : <Replace size={14} aria-hidden="true" />}
            <span>{replaceArmed ? 'Confirm' : 'Replace all'}</span>
          </button>
        </div>
      )}

      {/* Replace status */}
      {replaceStatus && <div className="search-replace-status">{replaceStatus}</div>}

      {/* Expand/collapse replace toggle */}
      <button
        className="search-expand-toggle"
        onClick={() => setShowReplace(!showReplace)}
        title={showReplace ? 'Hide replace' : 'Show replace'}
      >
        {showReplace ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
      </button>

      {/* Search stats */}
      {filteredResults && (
        <div className="search-stats">
          {displayMatches} {displayMatches === 1 ? 'match' : 'matches'} in {displayFiles}{' '}
          {displayFiles === 1 ? 'file' : 'files'}
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

        {filteredResults && filteredResults.length === 0 && !isSearching && !error && (
          <div className="search-no-results">
            <Search size={24} />
            <span>No results found</span>
          </div>
        )}

        {filteredResults && (
          <SearchResults
            results={filteredResults}
            onFileClick={handleFileClick}
            onRowContextMenu={onRowContextMenu}
            onFileHeaderContextMenu={onFileHeaderContextMenu}
            toggleFile={toggleFile}
            highlightMatch={highlightMatch}
            expandedFiles={expandedFiles}
          />
        )}
      </div>

      {/* Context menu */}
      <SearchContextMenu
        contextMenu={contextMenu}
        excludePatterns={excludePatterns}
        onClose={closeContextMenu}
        onFileClick={handleFileClick}
        onExcludePatternsChange={setExcludePatterns}
      />
    </div>
  );
}

export default SearchView;
