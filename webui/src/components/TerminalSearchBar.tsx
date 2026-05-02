import React, { useState, useEffect, useRef, useCallback, forwardRef, useImperativeHandle } from 'react';
import type { ChangeEvent, KeyboardEvent } from 'react';
import { ChevronUp, ChevronDown, X, Type, Hash } from 'lucide-react';

export interface TerminalSearchOptions {
  query: string;
  caseSensitive: boolean;
  regex: boolean;
}

export interface TerminalSearchBarHandle {
  show: () => void;
  hide: () => void;
  toggle: () => void;
  focusInput: () => void;
}

interface TerminalSearchBarProps {
  visible: boolean;
  onSearch: (options: TerminalSearchOptions, direction: 'next' | 'previous') => void;
  onClose: () => void;
  matchIndex?: number;
  matchCount?: number;
}

const TerminalSearchBar = forwardRef<TerminalSearchBarHandle, TerminalSearchBarProps>(
  ({ visible, onSearch, onClose, matchIndex, matchCount }, ref) => {
    const [query, setQuery] = useState('');
    const [caseSensitive, setCaseSensitive] = useState(false);
    const [regex, setRegex] = useState(false);

    const inputRef = useRef<HTMLInputElement>(null);

    // Expose methods to parent
    useImperativeHandle(ref, () => ({
      show: () => {
        // Parent controls visibility via props
      },
      hide: () => {
        // Parent controls visibility via props
      },
      toggle: () => {
        // Parent controls visibility via props
      },
      focusInput: () => {
        inputRef.current?.focus();
      },
    }));

    // Focus input when search bar becomes visible
    useEffect(() => {
      if (visible) {
        // Small delay to ensure the DOM has updated
        const timer = setTimeout(() => {
          inputRef.current?.focus();
          inputRef.current?.select();
        }, 50);
        return () => clearTimeout(timer);
      }
    }, [visible]);

    // Handle search execution
    const executeSearch = useCallback((direction: 'next' | 'previous' = 'next') => {
      if (query.trim()) {
        onSearch(
          {
            query: query.trim(),
            caseSensitive,
            regex,
          },
          direction,
        );
      }
    }, [query, caseSensitive, regex, onSearch]);

    // Handle input change
    const handleInputChange = (e: ChangeEvent<HTMLInputElement>) => {
      setQuery(e.target.value);
    };

    // Handle keyboard shortcuts
    const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        executeSearch(e.shiftKey ? 'previous' : 'next');
      } else if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };

    // Handle navigation buttons
    const handlePrevious = () => {
      executeSearch('previous');
    };

    const handleNext = () => {
      executeSearch('next');
    };

    // Toggle case sensitivity
    const toggleCaseSensitive = () => {
      const newValue = !caseSensitive;
      setCaseSensitive(newValue);
      // Re-search with new options if we have a query
      if (query.trim()) {
        onSearch(
          {
            query: query.trim(),
            caseSensitive: newValue,
            regex,
          },
          'next',
        );
      }
    };

    // Toggle regex mode
    const toggleRegex = () => {
      const newValue = !regex;
      setRegex(newValue);
      // Re-search with new options if we have a query
      if (query.trim()) {
        onSearch(
          {
            query: query.trim(),
            caseSensitive,
            regex: newValue,
          },
          'next',
        );
      }
    };

    // Build match counter text
    const getMatchCounter = (): string => {
      if (matchCount === undefined || matchCount === 0) {
        return query.trim() ? 'No results' : '';
      }
      if (matchIndex === undefined) {
        return `${matchCount} results`;
      }
      return `${matchIndex + 1}/${matchCount}`;
    };

    if (!visible) {
      return null;
    }

    return (
      <div className="terminal-search-bar">
        <div className="terminal-search-input-wrapper">
          <input
            ref={inputRef}
            type="text"
            className="terminal-search-input"
            placeholder="Search in terminal..."
            value={query}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
          />
        </div>

        <div className="terminal-search-controls">
          <button
            className="terminal-search-nav-btn"
            onClick={handlePrevious}
            disabled={!query.trim()}
            title="Previous match (Shift+Enter)"
            type="button"
          >
            <ChevronUp size={14} />
          </button>

          <button
            className="terminal-search-nav-btn"
            onClick={handleNext}
            disabled={!query.trim()}
            title="Next match (Enter)"
            type="button"
          >
            <ChevronDown size={14} />
          </button>

          <span className="terminal-search-counter">
            {getMatchCounter()}
          </span>

          <div className="terminal-search-divider" />

          <button
            className={`terminal-search-toggle-btn ${caseSensitive ? 'active' : ''}`}
            onClick={toggleCaseSensitive}
            title="Match case (Aa)"
            type="button"
          >
            <Type size={14} />
          </button>

          <button
            className={`terminal-search-toggle-btn ${regex ? 'active' : ''}`}
            onClick={toggleRegex}
            title="Use regular expressions (.*)"
            type="button"
          >
            <Hash size={14} />
          </button>

          <div className="terminal-search-divider" />

          <button
            className="terminal-search-close-btn"
            onClick={onClose}
            title="Close (Escape)"
            type="button"
          >
            <X size={14} />
          </button>
        </div>
      </div>
    );
  },
);

TerminalSearchBar.displayName = 'TerminalSearchBar';

export default TerminalSearchBar;
