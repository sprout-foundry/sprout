/**
 * useReverseSearch - manages reverse-i-search overlay state and PTY input handling.
 *
 * Extracted from TerminalPane.tsx. Handles the reverse-search active/query state
 * (refs and React state), the batched reverse-search query update timer, the
 * handlePtyInput function that interprets Ctrl+R, Escape, Enter, Backspace, and
 * printable chars, and provides a resetReverseSearch function.
 */

import { useRef, useState, useCallback, useEffect } from 'react';
import type { TerminalWebSocketService } from '../services/terminalWebSocket';

export interface UseReverseSearchOptions {
  /** Ref to the TerminalWebSocketService — needed for handlePtyInput to send raw input. */
  terminalWSRef: React.MutableRefObject<TerminalWebSocketService | null>;
}

export interface UseReverseSearchReturn {
  /** Whether the reverse-search overlay is visible. */
  reverseSearchVisible: boolean;
  /** Current reverse-search query string. */
  reverseSearchQuery: string;
  /** Input handler for PTY mode (non-WASM). Sends data to WebSocket and tracks reverse-search. */
  handlePtyInput: (data: string) => void;
  /** Reset reverse search state to inactive. */
  resetReverseSearch: () => void;
}

export function useReverseSearch(options: UseReverseSearchOptions): UseReverseSearchReturn {
  const { terminalWSRef } = options;

  // Refs for immediate values (avoid closure staleness in input handling)
  const reverseSearchActiveRef = useRef(false);
  const reverseSearchQueryRef = useRef('');
  const reverseSearchTimerRef = useRef<number | null>(null);

  // React state for rendering
  const [reverseSearchVisible, setReverseSearchVisible] = useState(false);
  const [reverseSearchQuery, setReverseSearchQuery] = useState('');

  /** Batch reverse-search query updates to avoid excessive re-renders */
  const scheduleReverseSearchUpdate = useCallback(() => {
    if (reverseSearchTimerRef.current !== null) {
      clearTimeout(reverseSearchTimerRef.current);
    }
    reverseSearchTimerRef.current = window.setTimeout(() => {
      if (reverseSearchTimerRef.current !== null) {
        setReverseSearchQuery(reverseSearchQueryRef.current);
        reverseSearchTimerRef.current = null;
      }
    }, 20);
  }, []);

  /**
   * Handle input for remote PTY sessions with reverse-i-search overlay support.
   *
   * When not in WASM mode, this monitors input for Ctrl+R (reverse-i-search) and
   * maintains overlay state showing the search query. The overlay is purely visual
   * and doesn't interfere with the actual PTY data flow.
   */
  const handlePtyInput = useCallback(
    (data: string) => {
      terminalWSRef.current?.sendRawInput(data);

      if (data.length > 1) {
        // Multi-character sequence (escape sequences, etc.)
        if (data.startsWith('\x1b')) {
          return;
        }
        if (reverseSearchActiveRef.current) {
          reverseSearchQueryRef.current += data;
          scheduleReverseSearchUpdate();
        }
        return;
      }

      const ch = data;

      // Ctrl+R: activate reverse-i-search
      if (ch === '\x12') {
        if (!reverseSearchActiveRef.current) {
          reverseSearchActiveRef.current = true;
          reverseSearchQueryRef.current = '';
          setReverseSearchVisible(true);
          setReverseSearchQuery('');
        }
        return;
      }

      // Enter, newline, Ctrl+C, or Escape: deactivate reverse-i-search
      if (ch === '\r' || ch === '\n' || ch === '\x03' || ch === '\x1b') {
        reverseSearchActiveRef.current = false;
        reverseSearchQueryRef.current = '';
        setReverseSearchVisible(false);
        setReverseSearchQuery('');
        return;
      }

      // Backspace: trim query
      if (ch === '\x7f' || ch === '\b') {
        if (reverseSearchActiveRef.current && reverseSearchQueryRef.current.length > 0) {
          reverseSearchQueryRef.current = reverseSearchQueryRef.current.slice(0, -1);
          scheduleReverseSearchUpdate();
        }
        return;
      }

      // Printable chars while reverse search is active: append to query
      if (reverseSearchActiveRef.current && (ch >= ' ' || ch === '\t')) {
        reverseSearchQueryRef.current += ch;
        scheduleReverseSearchUpdate();
        return;
      }
    },
    [terminalWSRef, scheduleReverseSearchUpdate],
  );

  const resetReverseSearch = useCallback(() => {
    reverseSearchActiveRef.current = false;
    reverseSearchQueryRef.current = '';
    setReverseSearchVisible(false);
    setReverseSearchQuery('');
  }, []);

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (reverseSearchTimerRef.current !== null) {
        clearTimeout(reverseSearchTimerRef.current);
        reverseSearchTimerRef.current = null;
      }
    };
  }, []);

  return {
    reverseSearchVisible,
    reverseSearchQuery,
    handlePtyInput,
    resetReverseSearch,
  };
}

export default useReverseSearch;
