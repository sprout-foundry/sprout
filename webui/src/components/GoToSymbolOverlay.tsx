import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { KeyboardEvent, MouseEvent } from 'react';
import { fuzzyFilter, highlightMatches } from '../utils/fuzzyMatch';
import type { FuzzyResult } from '../utils/fuzzyMatch';
import { type SymbolInfo, type SymbolKind, KIND_ICONS, extractSymbols, buildScopePaths } from '../utils/symbolUtils';
import './GoToSymbolOverlay.css';

// ── Types ────────────────────────────────────────────────────────────────

// Re-export types so existing consumers continue to compile.
export type { SymbolInfo, SymbolKind };
export { KIND_ICONS };

interface GoToSymbolOverlayProps {
  visible: boolean;
  content: string;
  fileExtension?: string;
  onSelectSymbol: (line: number) => void;
  onClose: () => void;
}

// ── Component ────────────────────────────────────────────────────────────

function GoToSymbolOverlay({
  visible,
  content,
  fileExtension,
  onSelectSymbol,
  onClose,
}: GoToSymbolOverlayProps): JSX.Element | null {
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);

  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const prevVisibleRef = useRef(false);

  // ── Reset state when visibility changes ───────────────────────────────

  useEffect(() => {
    if (visible && !prevVisibleRef.current) {
      setQuery('');
      setSelectedIndex(0);
    }
    prevVisibleRef.current = visible;
  }, [visible]);

  // ── Auto-focus input when overlay opens ───────────────────────────────

  useEffect(() => {
    if (visible) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [visible]);

  // ── Extract symbols (memoised) ────────────────────────────────────────

  const allSymbols = useMemo(() => extractSymbols(content, fileExtension), [content, fileExtension]);

  // ── Compute scope paths for each symbol (memoised) ─────────────────────

  const scopePaths = useMemo(
    () => buildScopePaths(content, fileExtension, allSymbols),
    [content, fileExtension, allSymbols],
  );

  // ── Filter symbols with fuzzy matching ────────────────────────────────

  const filteredResults = useMemo((): FuzzyResult<SymbolInfo>[] => {
    const trimmed = query.trim();
    if (!trimmed) return [];
    return fuzzyFilter(trimmed, allSymbols, (s) => s.name, 500);
  }, [query, allSymbols]);

  // ── Reset selected index when results change ──────────────────────────

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  // ── Scroll selected item into view ────────────────────────────────────

  useEffect(() => {
    const container = listRef.current;
    if (!container) return;
    const selected = container.querySelector('[data-selected="true"]');
    if (selected) {
      selected.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selectedIndex]);

  // ── Resolve the currently displayed item list and length ──────────────

  const hasQuery = query.trim().length > 0;
  const displayItems: SymbolInfo[] = hasQuery ? filteredResults.map((r) => r.item) : allSymbols;
  const itemCount = displayItems.length;

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
          if (hasQuery && filteredResults[selectedIndex]) {
            onSelectSymbol(filteredResults[selectedIndex].item.line);
            onClose();
          } else if (!hasQuery && allSymbols[selectedIndex]) {
            onSelectSymbol(allSymbols[selectedIndex].line);
            onClose();
          }
          break;
      }
    },
    [filteredResults, allSymbols, itemCount, selectedIndex, hasQuery, onClose, onSelectSymbol],
  );

  // ── Handle item click ─────────────────────────────────────────────────

  const handleItemClick = useCallback(
    (symbol: SymbolInfo) => {
      onSelectSymbol(symbol.line);
      onClose();
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

  if (!visible) return null;

  const isEmpty = displayItems.length === 0;

  return (
    <div className="goto-symbol-overlay">
      {/* Search input */}
      <div className="goto-symbol-input-wrapper">
        <span className="goto-symbol-prefix">@</span>
        <input
          ref={inputRef}
          type="text"
          className="goto-symbol-input"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Go to Symbol in File"
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          role="combobox"
          aria-expanded={visible}
          aria-controls="goto-symbol-list"
          aria-activedescendant={displayItems.length > 0 ? `goto-symbol-item-${selectedIndex}` : undefined}
        />
      </div>

      {/* Symbol list */}
      <div
        className="goto-symbol-list"
        ref={listRef}
        onMouseDown={handleMouseDown}
        id="goto-symbol-list"
        role="listbox"
        aria-label="Symbols"
      >
        {isEmpty && hasQuery && <div className="goto-symbol-empty">No matching symbols</div>}

        {isEmpty && !hasQuery && <div className="goto-symbol-empty">No symbols found</div>}

        {!isEmpty && !hasQuery && (
          <div className="goto-symbol-count">
            {displayItems.length} symbol{displayItems.length !== 1 ? 's' : ''} found
          </div>
        )}

        {displayItems.map((symbol, index) => {
          const matches = hasQuery && filteredResults[index] ? filteredResults[index].matches : [];
          const isActive = index === selectedIndex;
          const icon = KIND_ICONS[symbol.kind] || '?';
          const scopePath = scopePaths.get(symbol.line);

          return (
            <div
              key={`${symbol.kind}-${symbol.name}-${symbol.line}`}
              id={`goto-symbol-item-${index}`}
              data-selected={isActive}
              className={`goto-symbol-item${isActive ? ' goto-symbol-item-active' : ''}${scopePath ? ' goto-symbol-item-scoped' : ''}`}
              onClick={() => handleItemClick(symbol)}
              onMouseEnter={() => handleItemMouseEnter(index)}
              role="option"
              aria-selected={isActive}
            >
              <div className="goto-symbol-item-main">
                <span className={`goto-symbol-kind goto-symbol-kind-${symbol.kind}`}>{icon}</span>
                <span
                  className="goto-symbol-name"
                  dangerouslySetInnerHTML={{
                    __html: hasQuery ? highlightMatches(symbol.name, matches) : symbol.name,
                  }}
                />
                <span className="goto-symbol-line">:{symbol.line}</span>
              </div>
              {scopePath && <span className="goto-symbol-scope">{scopePath}</span>}
            </div>
          );
        })}
      </div>
    </div>
  );
}

export default GoToSymbolOverlay;
