import { useState, useEffect, useRef, useCallback, forwardRef, useImperativeHandle } from 'react';
import type { ChangeEvent, KeyboardEvent } from 'react';
import { ChevronUp, ChevronDown, X, Type, Hash } from 'lucide-react';

export interface TerminalSearchOptions {
  query: string;
  caseSensitive: boolean;
  regex: boolean;
}

export interface TerminalSearchBarHandle {
  focusInput: () => void;
  setQuery: (query: string) => void;
}

interface TerminalSearchBarProps {
  visible: boolean;
  onSearch: (options: TerminalSearchOptions, direction: 'next' | 'previous') => void;
  onClose: () => void;
  matchIndex?: number;
  matchCount?: number;
  searchError?: string | null;
  onSearchError?: (message: string | null) => void;
  initialQuery?: string | null;
}

const TerminalSearchBar = forwardRef<TerminalSearchBarHandle, TerminalSearchBarProps>(
  ({ visible, onSearch, onClose, matchIndex, matchCount, searchError, onSearchError, initialQuery }, ref) => {
    const [query, setQuery] = useState('');
    const [caseSensitive, setCaseSensitive] = useState(false);
    const [regex, setRegex] = useState(false);

    const inputRef = useRef<HTMLInputElement>(null);

    // Expose methods to parent
    useImperativeHandle(ref, () => ({
      focusInput: () => {
        inputRef.current?.focus();
      },
      setQuery: (q: string) => {
        setQuery(q);
      },
    }));

    // Focus input when search bar becomes visible
    useEffect(() => {
      if (visible) {
        // Double rAF ensures the DOM has been painted before focusing.
        // Track both handles so cleanup can cancel either one.
        let innerRaf = 0;
        const outerRaf = requestAnimationFrame(() => {
          innerRaf = requestAnimationFrame(() => {
            inputRef.current?.focus();
            // Apply initial query from terminal selection if provided
            if (initialQuery) {
              setQuery(initialQuery);
            }
            inputRef.current?.select();
          });
        });
        return () => {
          cancelAnimationFrame(outerRaf);
          if (innerRaf) cancelAnimationFrame(innerRaf);
        };
      }
    }, [visible, initialQuery]);

    // Handle search execution
    const executeSearch = useCallback(
      (direction: 'next' | 'previous' = 'next') => {
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
      },
      [query, caseSensitive, regex, onSearch],
    );

    // Handle input change
    const handleInputChange = (e: ChangeEvent<HTMLInputElement>) => {
      setQuery(e.target.value);
      if (searchError) {
        onSearchError?.(null);
      }
    };

    // Handle keyboard shortcuts
    const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.ctrlKey && e.shiftKey && e.key.toLowerCase() === 'f') {
        e.preventDefault();
        onClose();
        return;
      }
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
            type="search"
            className={`terminal-search-input ${searchError ? 'terminal-search-input-error' : ''}`}
            placeholder="Search in terminal..."
            value={query}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            aria-label="Search in terminal"
          />
          {searchError && <span className="terminal-search-error">{searchError}</span>}
        </div>

        <div className="terminal-search-controls">
          <button
            className="terminal-search-nav-btn"
            onClick={handlePrevious}
            disabled={!query.trim()}
            title="Previous match (Shift+Enter)"
            aria-label="Previous match"
            type="button"
          >
            <ChevronUp size={14} />
          </button>

          <button
            className="terminal-search-nav-btn"
            onClick={handleNext}
            disabled={!query.trim()}
            title="Next match (Enter)"
            aria-label="Next match"
            type="button"
          >
            <ChevronDown size={14} />
          </button>

          <span
            className={`terminal-search-counter ${matchCount === 0 && query.trim() ? 'no-results' : ''}`}
            aria-live="polite"
          >
            {getMatchCounter()}
          </span>

          <div className="terminal-search-divider" />

          <button
            className={`terminal-search-toggle-btn ${caseSensitive ? 'active' : ''}`}
            onClick={toggleCaseSensitive}
            title="Match case (Aa)"
            aria-label="Match case"
            aria-pressed={caseSensitive}
            type="button"
          >
            <Type size={14} />
          </button>

          <button
            className={`terminal-search-toggle-btn ${regex ? 'active' : ''}`}
            onClick={toggleRegex}
            title="Use regular expressions (.*)"
            aria-label="Use regular expression"
            aria-pressed={regex}
            type="button"
          >
            <Hash size={14} />
          </button>

          <div className="terminal-search-divider" />

          <button
            className="terminal-search-close-btn"
            onClick={onClose}
            title="Close (Escape)"
            aria-label="Close search"
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
