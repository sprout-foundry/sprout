import { useState, useEffect, useCallback, useMemo, useRef, type ComponentType } from 'react';
import { createPortal } from 'react-dom';
import { fuzzyMatch as fuzzyMatchUtil, highlightMatch as highlightMatchUtil } from '../utils/commandPalette';
import './CommandPalette.css';

export interface PaletteItem {
  id: string;
  label: string;
  description?: string;
  category?: string;
  icon?: ComponentType<{ size?: number; className?: string }>;
  shortcut?: string;
}

export interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  items: PaletteItem[];
  onSelect?: (item: PaletteItem) => void;
  placeholder?: string;
  emptyMessage?: string;
  className?: string;
}

/**
 * A searchable command palette (like VS Code's Cmd+P).
 *
 * Features fuzzy search/filter, keyboard navigation (up/down/enter/escape),
 * grouped by category, highlighted matches, shortcut display, and
 * portal-based overlay.
 */
function CommandPalette({
  isOpen,
  onClose,
  items,
  onSelect,
  placeholder = 'Type a command or search...',
  emptyMessage = 'No results found',
  className,
}: CommandPaletteProps): JSX.Element | null {
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  // Focus input when opening
  useEffect(() => {
    if (isOpen) {
      setQuery('');
      setSelectedIndex(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [isOpen]);

  // Filter and sort items based on query
  const { filteredItems, groupedItems, matchIndices } = useMemo(() => {
    if (!query) {
      // Show all items grouped by category
      const grouped: Record<string, PaletteItem[]> = {};
      items.forEach((item) => {
        const cat = item.category || 'General';
        if (!grouped[cat]) grouped[cat] = [];
        grouped[cat].push(item);
      });
      return {
        filteredItems: items,
        groupedItems: grouped,
        matchIndices: new Map<string, number[]>(),
      };
    }

    // Filter and score items
    const scored = items
      .map((item) => {
        const labelMatch = fuzzyMatchUtil(query, item.label);
        const descMatch = item.description ? fuzzyMatchUtil(query, item.description) : { score: 0, indices: [] };
        const score = Math.max(labelMatch.score, descMatch.score * 0.5);
        return { item, score, indices: labelMatch.indices };
      })
      .filter((s) => s.score > 0)
      .sort((a, b) => b.score - a.score);

    const filtered = scored.map((s) => s.item);
    const indicesMap = new Map(scored.map((s) => [s.item.id, s.indices]));

    // Group by category
    const grouped: Record<string, PaletteItem[]> = {};
    filtered.forEach((item) => {
      const cat = item.category || 'General';
      if (!grouped[cat]) grouped[cat] = [];
      grouped[cat].push(item);
    });

    return {
      filteredItems: filtered,
      groupedItems: grouped,
      matchIndices: indicesMap,
    };
  }, [items, query]);

  // Flatten grouped items for keyboard navigation
  const flatItems = useMemo(() => {
    const flat: PaletteItem[] = [];
    Object.entries(groupedItems).forEach(([_, items]) => {
      flat.push(...items);
    });
    return flat;
  }, [groupedItems]);

  // Update selected index when filtered items change
  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  // Handle keyboard navigation
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      switch (e.key) {
        case 'Escape':
          e.preventDefault();
          onClose();
          break;
        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((prev) => Math.min(prev + 1, flatItems.length - 1));
          break;
        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((prev) => Math.max(prev - 1, 0));
          break;
        case 'Enter':
          e.preventDefault();
          if (flatItems[selectedIndex]) {
            onSelect?.(flatItems[selectedIndex]);
            onClose();
          }
          break;
      }
    },
    [flatItems, selectedIndex, onSelect, onClose],
  );

  // Handle item selection
  const handleItemClick = useCallback(
    (item: PaletteItem) => {
      onSelect?.(item);
      onClose();
    },
    [onSelect, onClose],
  );

  // Handle input change
  const handleInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setQuery(e.target.value);
  }, []);

  // Scroll selected item into view
  const selectedItemRef = useCallback((el: HTMLButtonElement | null) => {
    if (el) {
      el.scrollIntoView({ block: 'nearest' });
    }
  }, []);

  if (!isOpen) return null;

  return createPortal(
    <div className="commandpalette-backdrop" onClick={onClose}>
      <div
        className={`commandpalette ${className || ''}`}
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
      >
        {/* Search Input */}
        <div className="commandpalette-input-wrapper">
          {query ? null : (
            <svg className="commandpalette-search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.3-4.3" />
            </svg>
          )}
          <input
            ref={inputRef}
            type="text"
            className="commandpalette-input"
            value={query}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            autoComplete="off"
            autoFocus
          />
        </div>

        {/* Results List */}
        <div className="commandpalette-results">
          {flatItems.length === 0 ? (
            <div className="commandpalette-empty">{emptyMessage}</div>
          ) : (
            Object.entries(groupedItems).map(([category, items]) => (
              <div key={category} className="commandpalette-category">
                {category !== 'General' && (
                  <div className="commandpalette-category-title">{category}</div>
                )}
                {items.map((item, idx) => {
                  const globalIndex = flatItems.findIndex((i) => i.id === item.id);
                  const isSelected = globalIndex === selectedIndex;
                  const Icon = item.icon;
                  const indices = matchIndices?.get(item.id) || [];

                  return (
                    <button
                      key={item.id}
                      type="button"
                      ref={isSelected ? selectedItemRef : null}
                      className={`commandpalette-item ${isSelected ? 'commandpalette-item-selected' : ''}`}
                      onClick={() => handleItemClick(item)}
                      onMouseEnter={() => setSelectedIndex(globalIndex)}
                    >
                      {Icon && (
                        <span className="commandpalette-item-icon" aria-hidden="true">
                          <Icon size={16} />
                        </span>
                      )}
                      <div className="commandpalette-item-content">
                        <span className="commandpalette-item-label">
                          {highlightMatchUtil(item.label, indices)}
                        </span>
                        {item.description && (
                          <span className="commandpalette-item-description">{item.description}</span>
                        )}
                      </div>
                      {item.shortcut && (
                        <span className="commandpalette-item-shortcut">{item.shortcut}</span>
                      )}
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>

        {/* Footer */}
        <div className="commandpalette-footer">
          <span className="commandpalette-footer-hint">
            <kbd>↑</kbd> <kbd>↓</kbd> to navigate · <kbd>Enter</kbd> to select · <kbd>Esc</kbd> to close
          </span>
        </div>
      </div>
    </div>,
    document.body,
  );
}

export default CommandPalette;
