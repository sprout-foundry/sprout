import { Check } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import './Dropdown.css';

export interface DropdownItem {
  id: string;
  display: string;
  searchText: string;
  value: unknown;
}

export interface DropdownOptions {
  prompt: string;
  searchPrompt?: string;
  maxHeight?: number;
  showCounts?: boolean;
}

interface DropdownProps {
  items: DropdownItem[];
  options: DropdownOptions;
  onSelect: (item: DropdownItem) => void;
  onCancel: () => void;
  isOpen: boolean;
}

/**
 * Modal picker shown for agent dropdown prompts (ui:show_dropdown).
 * ARIA: the search input acts as an editable combobox whose popup is a
 * listbox (WAI-ARIA APG combobox pattern). DOM focus stays on the input
 * while aria-activedescendant points at the highlighted option, so screen
 * readers announce options as the user arrows through them.
 */
function Dropdown({ items, options, onSelect, onCancel, isOpen }: DropdownProps): JSX.Element | null {
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [filteredItems, setFilteredItems] = useState(items);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listboxId = useId();

  // Filter items based on search query
  useEffect(() => {
    if (!searchQuery.trim()) {
      setFilteredItems(items);
    } else {
      const query = searchQuery.toLowerCase();
      const filtered = items.filter(
        (item) => item.searchText.toLowerCase().includes(query) || item.display.toLowerCase().includes(query),
      );
      setFilteredItems(filtered);
    }
  }, [searchQuery, items]);

  // Reset selected index when filtered items change
  useEffect(() => {
    setSelectedIndex(0);
  }, [filteredItems]);

  // Focus search input when dropdown opens
  useEffect(() => {
    if (isOpen && searchInputRef.current) {
      searchInputRef.current.focus();
    }
  }, [isOpen]);

  // Keep the highlighted option visible while navigating with the keyboard.
  useEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const active = list.querySelector(`#${CSS.escape(`${listboxId}-opt-${selectedIndex}`)}`);
    active?.scrollIntoView({ block: 'nearest' });
  }, [selectedIndex, listboxId]);

  const handleKeyDown = (e: KeyboardEvent) => {
    switch (e.key) {
      case 'Escape':
        e.preventDefault();
        onCancel();
        break;
      case 'Enter':
        e.preventDefault();
        if (filteredItems.length > 0) {
          onSelect(filteredItems[selectedIndex]);
        }
        break;
      case 'ArrowUp':
        e.preventDefault();
        setSelectedIndex((prev) => Math.max(0, prev - 1));
        break;
      case 'ArrowDown':
        e.preventDefault();
        setSelectedIndex((prev) => Math.min(filteredItems.length - 1, prev + 1));
        break;
      case 'PageUp':
        e.preventDefault();
        setSelectedIndex((prev) => Math.max(0, prev - 10));
        break;
      case 'PageDown':
        e.preventDefault();
        setSelectedIndex((prev) => Math.min(filteredItems.length - 1, prev + 10));
        break;
      case 'Home':
        e.preventDefault();
        setSelectedIndex(0);
        break;
      case 'End':
        e.preventDefault();
        setSelectedIndex(filteredItems.length - 1);
        break;
    }
  };

  const handleItemClick = (item: DropdownItem, index: number) => {
    setSelectedIndex(index);
    onSelect(item);
  };

  const countDisplay = options.showCounts ? `${filteredItems.length} items` : '';
  const maxHeight = options.maxHeight ? `${options.maxHeight}px` : '400px';

  if (!isOpen) return null;

  return (
    <div className="dropdown-overlay" onClick={onCancel}>
      <div
        className="dropdown-container"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={options.prompt}
      >
        {/* Header */}
        <div className="dropdown-header">
          <div className="dropdown-prompt">{options.prompt}</div>
          {countDisplay && <div className="dropdown-count">{countDisplay}</div>}
        </div>

        {/* Search — editable combobox controlling the listbox below */}
        <div className="dropdown-search">
          <input
            ref={searchInputRef}
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={options.searchPrompt || 'Search...'}
            className="dropdown-search-input"
            role="combobox"
            aria-expanded="true"
            aria-controls={listboxId}
            aria-activedescendant={filteredItems.length > 0 ? `${listboxId}-opt-${selectedIndex}` : undefined}
            aria-autocomplete="list"
            aria-label={options.searchPrompt || 'Search options'}
          />
        </div>

        {/* Items */}
        <div
          ref={listRef}
          id={listboxId}
          className="dropdown-items"
          role="listbox"
          aria-label={options.prompt}
          style={{ maxHeight }}
        >
          {filteredItems.length === 0 ? (
            <div className="dropdown-no-results">No matching items found</div>
          ) : (
            filteredItems.map((item, index) => {
              const isSelected = index === selectedIndex;
              return (
                <div
                  key={item.id}
                  id={`${listboxId}-opt-${index}`}
                  role="option"
                  aria-selected={isSelected}
                  className={`dropdown-item ${isSelected ? 'selected' : ''}`}
                  onMouseEnter={() => setSelectedIndex(index)}
                  onClick={() => handleItemClick(item, index)}
                >
                  <span className="dropdown-item-display">{item.display}</span>
                  {isSelected && (
                    <span className="dropdown-item-check" aria-hidden="true">
                      <Check size={16} />
                    </span>
                  )}
                </div>
              );
            })
          )}
        </div>

        {/* Footer */}
        <div className="dropdown-footer" aria-hidden="true">
          <div className="dropdown-help">
            <span>
              <kbd>↑</kbd>
              <kbd>↓</kbd> Navigate
            </span>
            <span>
              <kbd>↵</kbd> Select
            </span>
            <span>
              <kbd>Esc</kbd> Cancel
            </span>
            <span>Type to filter</span>
          </div>
        </div>
      </div>
    </div>
  );
}

export default Dropdown;
