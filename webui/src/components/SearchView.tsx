import { Search, Replace, ChevronDown, ChevronUp, X, AlertCircle, Loader2, ChevronRight, Brain } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import './SearchView.css';
import { ApiService } from '../services/api';
import { highlightMatch } from './search/highlightMatch';
import SearchContextMenu, {
  createRowContextMenuHandler,
  createFileHeaderContextMenuHandler,
} from './search/SearchContextMenu';
import SearchResults from './search/SearchResults';
import SemanticPreviewTooltip from './search/SemanticPreviewTooltip';
import type { PreviewData, PreviewPosition } from './search/SemanticPreviewTooltip';
import SemanticSearchResults from './search/SemanticSearchResults';
import type { SearchViewProps, SearchContextMenuState, SemanticSearchResult } from './search/types';
import { useSearchState, getRelativePath } from './search/useSearchState';

/**
 * Search panel — text search with find/replace and semantic code search.
 *
 * Composition root that wires the search state hook, result renderers,
 * context menu, and semantic preview tooltip together.
 */
function SearchView({ onFileClick }: SearchViewProps): JSX.Element {
  const searchInputRef = useRef<HTMLInputElement>(null);
  const hoverTimerRef = useRef<NodeJS.Timeout | null>(null);

  const state = useSearchState(onFileClick, searchInputRef);

  // ── Context menu state ───────────────────────────────────────
  const [contextMenu, setContextMenu] = useState<SearchContextMenuState | null>(null);
  const closeContextMenu = useCallback(() => setContextMenu(null), []);

  const onRowContextMenu = useCallback((e: MouseEvent, filePath: string, lineNumber: number, lineText: string) => {
    createRowContextMenuHandler(setContextMenu)(e, filePath, lineNumber, lineText);
  }, []);

  const onFileHeaderContextMenu = useCallback((e: MouseEvent, filePath: string) => {
    createFileHeaderContextMenuHandler(setContextMenu)(e, filePath);
  }, []);

  // ── Semantic hover preview state ─────────────────────────────
  const [previewData, setPreviewData] = useState<PreviewData | null>(null);
  const [previewPosition, setPreviewPosition] = useState<PreviewPosition | null>(null);

  const apiService = ApiService.getInstance();

  const handleSemanticResultMouseEnter = useCallback(
    (e: MouseEvent<HTMLDivElement>, result: SemanticSearchResult) => {
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
          const rect = e.currentTarget.getBoundingClientRect();
          const x = rect.right + 8 + 500 > window.innerWidth ? rect.left - 508 : rect.right + 8;
          setPreviewPosition({ x, y: rect.top });
        } catch {
          // Preview not available — silently ignore
        }
      }, 300);
    },
    [apiService],
  );

  const handleSemanticResultMouseLeave = useCallback(() => {
    if (hoverTimerRef.current) {
      clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    setPreviewData(null);
    setPreviewPosition(null);
  }, []);

  // ── Cluster expansion state ──────────────────────────────────
  const [clustersExpanded, setClustersExpanded] = useState(true);

  // ── Focus search input on mount ──────────────────────────────
  useEffect(() => {
    searchInputRef.current?.focus();
  }, []);

  // Clean up hover timer on unmount
  useEffect(() => {
    return () => {
      if (hoverTimerRef.current) clearTimeout(hoverTimerRef.current);
    };
  }, []);

  // ── Destructure state for readability ────────────────────────
  const {
    searchQuery,
    replaceQuery,
    setReplaceQuery,
    caseSensitive,
    wholeWord,
    useRegex,
    semanticMode,
    toggleCaseSensitive,
    toggleWholeWord,
    toggleRegex,
    toggleSemanticMode,
    filteredResults,
    semanticResults,
    semanticDuration,
    semanticNote,
    duplicateClusters,
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
    semanticThreshold,
    setSemanticThreshold,
    indexStatus,
    isBuilding,
    expandedFiles,
    toggleFile,
    handleSearchChange,
    handleSearchKeyDown,
    handleClear,
    handleFileClick,
  } = state;

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

      {/* Replace row — hidden in semantic mode */}
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
              {duplicateClusters.reduce((sum, c) => sum + (c.count ?? c.files.length), 0)} result
              {duplicateClusters.reduce((sum, c) => sum + (c.count ?? c.files.length), 0) === 1 ? '' : 's'} share
              similar patterns across {new Set(duplicateClusters.flatMap((c) => c.files)).size} file
              {new Set(duplicateClusters.flatMap((c) => c.files)).size === 1 ? '' : 's'}
            </span>
          </button>
          {clustersExpanded && (
            <div className="search-duplicate-summary-content">
              {duplicateClusters.map((cluster, idx) => (
                <div key={idx} className="search-duplicate-cluster-item">
                  <span className="search-duplicate-cluster-label">
                    Cluster {idx + 1}: ~{(cluster.similarity * 100).toFixed(0)}% similar —{' '}
                    {cluster.count ?? cluster.files.length} result
                    {(cluster.count ?? cluster.files.length) === 1 ? '' : 's'}
                  </span>
                  <span className="search-duplicate-cluster-files">
                    {cluster.files.map((f) => getRelativePath(f)).join(', ')}
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

        {/* Semantic search results */}
        {semanticResults && semanticResults.length === 0 && !isSearching && !error && (
          <div className="search-no-results">
            <Search size={24} />
            <span>{semanticNote || 'No semantic results found'}</span>
          </div>
        )}

        {semanticResults && semanticResults.length > 0 && (
          <SemanticSearchResults
            results={semanticResults}
            onFileClick={handleFileClick}
            onMouseEnter={handleSemanticResultMouseEnter}
            onMouseLeave={handleSemanticResultMouseLeave}
          />
        )}
      </div>

      {/* Semantic hover preview tooltip */}
      <SemanticPreviewTooltip
        previewData={previewData}
        previewPosition={previewPosition}
        onMouseLeave={handleSemanticResultMouseLeave}
      />

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
