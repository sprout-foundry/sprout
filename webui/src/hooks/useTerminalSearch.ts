/**
 * useTerminalSearch - manages search functionality for the TerminalPane.
 *
 * Extracted from TerminalPane.tsx to keep the main component thin.
 * Manages the search bar state, refs, and handlers.
 * Does NOT create the SearchAddon - that is created by useTerminalXTerm
 * during xterm initialization.
 */

import { useRef, useState, useCallback } from 'react';
import type { Terminal as XTerm } from '@xterm/xterm';
import type { SearchAddon } from '@xterm/addon-search';
import type { TerminalSearchOptions, TerminalSearchBarHandle } from '../components/TerminalSearchBar';

export interface UseTerminalSearchOptions {
  xtermRef: React.RefObject<XTerm | null>;
  searchAddonRef: React.MutableRefObject<SearchAddon | null>;
}

export interface UseTerminalSearchReturn {
  searchBarRef: React.MutableRefObject<TerminalSearchBarHandle | null>;
  searchInitialQueryRef: React.MutableRefObject<string | null>;
  searchVisible: boolean;
  setSearchVisible: React.Dispatch<React.SetStateAction<boolean>>;
  matchIndex: number | undefined;
  matchCount: number | undefined;
  searchError: string | null;
  handleSearch: (options: TerminalSearchOptions, direction: 'next' | 'previous') => void;
  handleCloseSearch: () => void;
  handleSearchError: (message: string | null) => void;
  handleContextSearch: () => void;
  resetSearch: () => void;
  /** Update match index/count from external source (e.g. xterm search addon). */
  setSearchResults: (resultIndex: number | undefined, resultCount: number | undefined) => void;
}

export function useTerminalSearch(options: UseTerminalSearchOptions): UseTerminalSearchReturn {
  const { xtermRef, searchAddonRef } = options;

  const searchBarRef = useRef<TerminalSearchBarHandle | null>(null);
  const searchInitialQueryRef = useRef<string | null>(null);
  const [searchVisible, setSearchVisible] = useState(false);
  const [matchIndex, setMatchIndex] = useState<number | undefined>(undefined);
  const [matchCount, setMatchCount] = useState<number | undefined>(undefined);
  const [searchError, setSearchError] = useState<string | null>(null);

  const handleSearchError = useCallback((message: string | null) => {
    setSearchError(message);
  }, []);

  const handleSearch = useCallback(
    (searchOptions: TerminalSearchOptions, direction: 'next' | 'previous') => {
      const term = xtermRef.current;
      const searchAddon = searchAddonRef.current;
      if (!term || !searchAddon) return;

      const { query, caseSensitive, regex } = searchOptions;

      try {
        setSearchError(null);
        if (direction === 'next') {
          searchAddon.findNext(query, { caseSensitive, regex, wholeWord: false });
        } else {
          searchAddon.findPrevious(query, { caseSensitive, regex, wholeWord: false });
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        setSearchError(message);
      }
    },
    [xtermRef, searchAddonRef],
  );

  const handleCloseSearch = useCallback(() => {
    setSearchVisible(false);
    searchAddonRef.current?.clearDecorations();
    setMatchIndex(undefined);
    setMatchCount(undefined);
    setSearchError(null);
    xtermRef.current?.focus();
  }, [xtermRef, searchAddonRef]);

  const handleContextSearch = useCallback(() => {
    const sel = xtermRef.current?.getSelection();
    searchInitialQueryRef.current = sel && sel.trim() ? sel.trim() : null;
    setSearchVisible(true);
  }, [xtermRef]);

  const resetSearch = useCallback(() => {
    searchAddonRef.current?.clearDecorations();
    setSearchVisible(false);
    setMatchIndex(undefined);
    setMatchCount(undefined);
    setSearchError(null);
  }, [searchAddonRef]);

  const setSearchResults = useCallback((resultIndex: number | undefined, resultCount: number | undefined) => {
    setMatchIndex(resultIndex);
    setMatchCount(resultCount);
  }, []);

  return {
    searchBarRef,
    searchInitialQueryRef,
    searchVisible,
    setSearchVisible,
    matchIndex,
    matchCount,
    searchError,
    handleSearch,
    handleCloseSearch,
    handleSearchError,
    handleContextSearch,
    resetSearch,
    setSearchResults,
  };
}

export default useTerminalSearch;
