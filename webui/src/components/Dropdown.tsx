import React, { useState, useEffect, useRef } from 'react';
import './Dropdown.css';

export interface DropdownItem {
  id: string;
  display: string;
  searchText: string;
  value: any;
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

const Dropdown: React.FC<DropdownProps> = ({
  items,
  options,
  onSelect,
  onCancel,
  isOpen
}) => {
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [filteredItems, setFilteredItems] = useState(items);
  const searchInputRef = useRef<HTMLInputElement>(null);

  // Filter items based on search query
  useEffect(() => {
    if (!searchQuery.trim()) {
      setFilteredItems(items);
    } else {
      const query = searchQuery.toLowerCase();
      const filtered = items.filter(item =>
        item.searchText.toLowerCase().includes(query) ||
        item.display.toLowerCase().includes(query)
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

  const handleKeyDown = (e: React.KeyboardEvent) => {
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
        setSelectedIndex(prev => Math.max(0, prev - 1));
        break;
      case 'ArrowDown':
        e.preventDefault();
        setSelectedIndex(prev => Math.min(filteredItems.length - 1, prev + 1));
        break;
      case 'PageUp':
        e.preventDefault();
        setSelectedIndex(prev => Math.max(0, prev - 10));
        break;
      case 'PageDown':
        e.preventDefault();
        setSelectedIndex(prev => Math.min(filteredItems.length - 1, prev + 10));
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

  const getItemCountDisplay = () => {
    if (!options.showCounts) return '';
    return `${filteredItems.length} items`;
  };

  const getMaxHeight = () => {
    if (options.maxHeight) return `${options.maxHeight}px`;
    return '400px'; // default max height
  };

  if (!isOpen) return null;

  return (
    <div className="dropdown-overlay" onClick={onCancel}>
      <div className="dropdown-container" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="dropdown-header">
          <div className="dropdown-prompt">{options.prompt}</div>
          {getItemCountDisplay() && (
            <div className="dropdown-count">{getItemCountDisplay()}</div>
          )}
        </div>

        {/* Search */}
        <div className="dropdown-search">
          <input
            ref={searchInputRef}
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={options.searchPrompt || 'Search...'}
            className="dropdown-search-input"
          />
        </div>

        {/* Items */}
        <div className="dropdown-items" style={{ maxHeight: getMaxHeight() }}>
          {filteredItems.length === 0 ? (
            <div className="dropdown-no-results">No matching items found</div>
          ) : (
            filteredItems.map((item, index) => (
              <div
                key={item.id}
                className={`dropdown-item ${index === selectedIndex ? 'selected' : ''}`}
                onClick={() => handleItemClick(item, index)}
              >
                <div className="dropdown-item-display">{item.display}</div>
              </div>
            ))
          )}
        </div>

        {/* Footer */}
        <div className="dropdown-footer">
          <div className="dropdown-help">
            <span>↑↓ Navigate</span>
            <span>Enter Select</span>
            <span>Esc Cancel</span>
            <span>Search Filter</span>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Dropdown;