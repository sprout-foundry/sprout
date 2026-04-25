import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { KeyboardEvent, MouseEvent } from 'react';
import { fuzzyFilter, highlightMatches } from '../utils/fuzzyMatch';
import type { FuzzyResult } from '../utils/fuzzyMatch';
import { KIND_ICONS, type SymbolKind } from './GoToSymbolOverlay';
import { ApiService } from '../services/api';
import { Loader2 } from 'lucide-react';
import './GoToWorkspaceSymbolOverlay.css';

// ── Types ────────────────────────────────────────────────────────────────

interface WorkspaceSymbolInfo {
  name: string;
  kind: SymbolKind;
  line?: number;
}

interface FileGroup {
  file: string;
  symbols: WorkspaceSymbolInfo[];
}

/** Flattened item for keyboard navigation (either a file header or a symbol) */
type NavigationItem =
  | { type: 'header'; file: string }
  | { type: 'symbol'; file: string; symbol: WorkspaceSymbolInfo };

interface GoToWorkspaceSymbolOverlayProps {
  visible: boolean;
  onSelectSymbol: (filePath: string, line?: number) => void;
  onClose: () => void;
}

// ── Constants ────────────────────────────────────────────────────────────

const DEBOUNCE_MS = 300;
const MAX_SYMBOLS_PER_FILE = 100;
const MAX_FILES = 50;

// ── Component ────────────────────────────────────────────────────────────

function GoToWorkspaceSymbolOverlay({
  visible,
  onSelectSymbol,
  onClose,
}: GoToWorkspaceSymbolOverlayProps): JSX.Element | null {
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fileGroups, setFileGroups] = useState<FileGroup[]>([]);
  const [totalCount, setTotalCount] = useState(0);

  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const prevVisibleRef = useRef(false);
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const requestIdRef = useRef(0);
  const apiServiceRef = useRef(ApiService.getInstance());

  // ── Reset state when visibility changes ───────────────────────────────

  useEffect(() => {
    if (visible && !prevVisibleRef.current) {
      setQuery('');
      setSelectedIndex(0);
      setLoading(false);
      setError(null);
      setFileGroups([]);
      setTotalCount(0);
    }
    prevVisibleRef.current = visible;
  }, [visible]);

  // ── Auto-focus input when overlay opens ───────────────────────────────

  useEffect(() => {
    if (visible) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [visible]);

  // ── Fetch workspace symbols from API ────────────────────────────────

  const fetchSymbols = useCallback(async (searchQuery: string) => {
    const requestId = ++requestIdRef.current;
    setLoading(true);
    setError(null);

    try {
      const result = await apiServiceRef.current.getWorkspaceSymbols(searchQuery);

      // Ignore stale response if a newer request was initiated
      if (requestId !== requestIdRef.current) return;

      // Map API response to our types (API returns kind as string)
      const groups: FileGroup[] = (result.files || []).map((file) => ({
        file: file.file,
        symbols: file.symbols.map((sym) => ({
          name: sym.name,
          // Normalize kind strings to our SymbolKind type (API returns 'func' not 'function')
          kind: (sym.kind === 'func' ? 'function' : sym.kind) as SymbolKind,
          line: sym.line,
        })),
      }));

      setFileGroups(groups);
      setTotalCount(result.total || 0);
    } catch (err) {
      // Ignore stale response
      if (requestId !== requestIdRef.current) return;
      console.error('[GoToWorkspaceSymbolOverlay] Failed to fetch symbols:', err);
      setError('Failed to fetch symbols');
      setFileGroups([]);
      setTotalCount(0);
    } finally {
      setLoading(false);
    }
  }, []);

  // ── Debounced search on query change ────────────────────────────────

  useEffect(() => {
    if (!visible) return;

    // Clear any pending timer
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }

    // Immediate fetch on first open with empty query
    if (query.trim() === '') {
      void fetchSymbols('');
      return;
    }

    // Debounce subsequent queries
    debounceTimerRef.current = setTimeout(() => {
      void fetchSymbols(query);
    }, DEBOUNCE_MS);

    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [query, visible, fetchSymbols]);

  // ── Build flattened navigation list ────────────────────────────────

  const navigationItems = useMemo((): NavigationItem[] => {
    const items: NavigationItem[] = [];

    for (const group of fileGroups) {
      // Add file header
      items.push({ type: 'header', file: group.file });

      // Add symbols under this file
      for (const symbol of group.symbols) {
        items.push({ type: 'symbol', file: group.file, symbol });
      }
    }

    return items;
  }, [fileGroups]);

  // ── Build filtered results with fuzzy matching ─────────────────────

  const filteredResults = useMemo((): FuzzyResult<NavigationItem>[] => {
    const trimmed = query.trim();
    if (!trimmed) {
      // No query: show all symbols (skip headers in results but keep them for structure)
      // Return only symbol items, not headers
      return navigationItems
        .filter((item): item is NavigationItem & { type: 'symbol' } => item.type === 'symbol')
        .map((item) => ({
          item,
          score: 0,
          matches: [],
        }));
    }

    // Fuzzy filter on symbol names and file paths
    return fuzzyFilter(
      trimmed,
      navigationItems,
      (item) => {
        if (item.type === 'header') return item.file;
        return `${item.file}:${item.symbol.name}`;
      },
      MAX_FILES * MAX_SYMBOLS_PER_FILE,
    );
  }, [query, navigationItems]);

  // ── Determine display items based on query ─────────────────────────

  const hasQuery = query.trim().length > 0;
  const displayItems = hasQuery
    ? filteredResults.map((r) => r.item)
    : navigationItems.filter((item): item is NavigationItem & { type: 'symbol' } => item.type === 'symbol');
  const itemCount = displayItems.length;

  // ── Reset selected index when results change ────────────────────────

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  // ── Scroll selected item into view ────────────���───────────────────────

  useEffect(() => {
    const container = listRef.current;
    if (!container) return;
    const selected = container.querySelector('[data-selected="true"]');
    if (selected) {
      selected.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selectedIndex]);

  // ── Handle keyboard navigation ────────────────────────────────────────

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      e.stopPropagation();

      switch (e.key) {
        case 'Escape':
          e.preventDefault();
          onClose();
          break;

        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((prev) => Math.min(prev + 1, Math.max(itemCount - 1, 0)));
          break;

        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((prev) => Math.max(prev - 1, 0));
          break;

        case 'Enter':
          e.preventDefault();
          if (itemCount > 0 && selectedIndex < displayItems.length) {
            const item = displayItems[selectedIndex];
            if (item.type === 'symbol') {
              onSelectSymbol(item.file, item.symbol.line);
              onClose();
            }
          }
          break;
      }
    },
    [itemCount, selectedIndex, displayItems, onClose, onSelectSymbol],
  );

  // ── Handle item click ─────────────────────────────────────────────────

  const handleItemClick = useCallback(
    (item: NavigationItem) => {
      if (item.type === 'symbol') {
        onSelectSymbol(item.file, item.symbol.line);
        onClose();
      }
    },
    [onSelectSymbol, onClose],
  );

  // ── Handle mouse enter on item (track selection hover) ────────────────

  const handleItemMouseEnter = useCallback((index: number) => {
    setSelectedIndex(index);
  }, []);

  // ── Stop mousedown from stealing focus ────────────────────────────────

  const handleMouseDown = useCallback((e: MouseEvent) => {
    // Don't let the dropdown steal focus from the input
    e.preventDefault();
  }, []);

  // ── Map display index to navigation index for highlighting ───────────

  const getMatchesForDisplayIndex = useCallback(
    (displayIdx: number): Array<[number, number]> => {
      if (!hasQuery) return [];

      const result = filteredResults[displayIdx];
      if (!result) return [];

      const item = result.item;
      if (item.type === 'header') return [];

      // Calculate offset: file path + ':' + symbol name
      const prefixLength = item.file.length + 1;
      return result.matches
        .filter((m) => m[0] >= prefixLength)
        .map((m): [number, number] => [m[0] - prefixLength, m[1] - prefixLength]);
    },
    [hasQuery, filteredResults],
  );

  if (!visible) return null;

  const isEmpty = displayItems.length === 0 && !loading;

  // Count total symbols
  const totalSymbols = fileGroups.reduce((sum, group) => sum + group.symbols.length, 0);

  return (
    <div className="goto-workspace-symbol-overlay">
      {/* Search input */}
      <div className="goto-workspace-symbol-input-wrapper">
        <span className="goto-workspace-symbol-prefix">#</span>
        <input
          ref={inputRef}
          type="text"
          className="goto-workspace-symbol-input"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Go to Symbol in Workspace"
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
        />
      </div>

      {/* Symbol list */}
      <div className="goto-workspace-symbol-list" ref={listRef} onMouseDown={handleMouseDown}>
        {/* Loading state */}
        {loading && !hasQuery && (
          <div className="goto-workspace-symbol-loading">
            <Loader2 size={14} className="spinner" />
            <span>Loading symbols...</span>
          </div>
        )}

        {/* Loading with query */}
        {loading && hasQuery && (
          <div className="goto-workspace-symbol-loading">
            <Loader2 size={14} className="spinner" />
            <span>Searching...</span>
          </div>
        )}

        {/* Error state */}
        {error && !loading && (
          <div className="goto-workspace-symbol-empty">{error}</div>
        )}

        {/* Empty state with query */}
        {isEmpty && hasQuery && !loading && (
          <div className="goto-workspace-symbol-empty">No matching symbols found</div>
        )}

        {/* Empty state without query */}
        {isEmpty && !hasQuery && !loading && (
          <div className="goto-workspace-symbol-empty">No symbols in workspace</div>
        )}

        {/* Symbol count */}
        {!isEmpty && !loading && !hasQuery && (
          <div className="goto-workspace-symbol-count">
            {totalSymbols} symbol{totalSymbols !== 1 ? 's' : ''} in {fileGroups.length} file{fileGroups.length !== 1 ? 's' : ''}
            {totalCount > 0 && totalCount !== totalSymbols ? ` (${totalCount} total indexed)` : ''}
          </div>
        )}

        {!isEmpty && !loading && (
          <div>
            {/* Render grouped view: file headers + symbols */}
            {fileGroups.map((group) => {
              // Find display indices for this file's symbols
              const symbolDisplayItems: Array<{ displayIdx: number; symbol: WorkspaceSymbolInfo }> = [];

              displayItems.forEach((item, idx) => {
                if (item.type === 'symbol' && item.file === group.file) {
                  symbolDisplayItems.push({ displayIdx: idx, symbol: item.symbol });
                }
              });

              // If we have symbols to show for this file (either all of them, or filtered ones)
              const showFile = symbolDisplayItems.length > 0;

              if (!showFile) return null;

              return (
                <div key={group.file} className="goto-workspace-symbol-file-group">
                  {/* File header */}
                  <div className="goto-workspace-symbol-file-header">
                    {group.file}
                  </div>

                  {/* Symbols under this file */}
                  {symbolDisplayItems.map(({ displayIdx, symbol }) => {
                    const isActive = displayIdx === selectedIndex;
                    const matches = hasQuery ? getMatchesForDisplayIndex(displayIdx) : [];
                    const icon = KIND_ICONS[symbol.kind] || '?';

                    return (
                      <div
                        key={`${group.file}:${symbol.name}:${symbol.line ?? 0}`}
                        data-selected={isActive}
                        className={`goto-workspace-symbol-item${isActive ? ' goto-workspace-symbol-item-active' : ''}`}
                        onClick={() => handleItemClick({ type: 'symbol', file: group.file, symbol })}
                        onMouseEnter={() => handleItemMouseEnter(displayIdx)}
                      >
                        <span className={`goto-workspace-symbol-kind goto-workspace-symbol-kind-${symbol.kind}`}>{icon}</span>
                        <span
                          className="goto-workspace-symbol-name"
                          dangerouslySetInnerHTML={{
                            __html: hasQuery ? highlightMatches(symbol.name, matches) : symbol.name,
                          }}
                        />
                        {symbol.line !== undefined && (
                          <span className="goto-workspace-symbol-line">:{symbol.line}</span>
                        )}
                      </div>
                    );
                  })}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

export default GoToWorkspaceSymbolOverlay;
