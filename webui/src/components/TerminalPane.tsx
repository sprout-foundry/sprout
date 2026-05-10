import { useState, useEffect, useRef, useCallback, useImperativeHandle, forwardRef } from 'react';
import { X, TriangleAlert, Terminal } from 'lucide-react';
import type { Terminal as XTerm } from '@xterm/xterm';
import { useTheme } from '../contexts/ThemeContext';
import { debugLog } from '../utils/log';
import { copyToClipboard } from '../utils/clipboard';
import TerminalSearchBar from './TerminalSearchBar';
import TerminalContextMenu from './TerminalContextMenu';
import ReverseSearchOverlay from './ReverseSearchOverlay';

// Hooks extracted from this component
import { useWasmTerminalInput } from '../hooks/useWasmTerminalInput';
import { useTerminalScrollback } from '../hooks/useTerminalScrollback';
import { useTerminalSearch } from '../hooks/useTerminalSearch';
import { useTerminalXTerm } from '../hooks/useTerminalXTerm';
import { useTerminalSession } from '../hooks/useTerminalSession';

export interface TerminalPaneHandle {
  clear: () => void;
  focus: () => void;
  /** Cleanup the pane's WebSocket connection when the session is being closed */
  cleanup: () => void;
}

interface TerminalPaneProps {
  /** Whether the parent terminal is expanded (mounted). */
  isActive: boolean;
  /** Whether the app-level WebSocket connection is available. */
  isConnected?: boolean;
  /** When true, renders a close button in the pane header. */
  showCloseButton: boolean;
  /** Called when the user clicks the pane close button. */
  onClose?: () => void;
  /** Notifies the parent of connection state changes. */
  onConnectionChange?: (connected: boolean) => void;
  /** Preferred shell name (e.g. "bash", "zsh", "fish") for the initial PTY session. */
  preferredShell?: string | null;
  /** Font size in pixels (overrides default). */
  fontSize?: number;
  /** Session ID to reattach to (for promoting background agent sessions to visible tabs). */
  reattachSessionId?: string | null;
  /** Called when the PTY process exits (pty_exit event from backend). */
  onProcessExit?: () => void;
  /** When true, automatically copies selected text to clipboard. */
  copyOnSelect?: boolean;
}

const EXPAND_RESIZE_DELAY_MS = 100;

const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(
  ({ isActive, isConnected = true, showCloseButton, onClose, onConnectionChange, preferredShell, fontSize, reattachSessionId, onProcessExit, copyOnSelect = false }, ref) => {
    const { themePack } = useTheme();

    // ── Reverse-i-search overlay state (PTY mode, visual only) ──────
    const reverseSearchActiveRef = useRef(false);
    const reverseSearchQueryRef = useRef('');
    const reverseSearchTimerRef = useRef<number | null>(null);
    const [reverseSearchVisible, setReverseSearchVisible] = useState(false);
    const [reverseSearchQuery, setReverseSearchQuery] = useState('');

    // ── Search state (needed before hooks so we can wire callbacks) ──
    const searchAddonRef = useRef<import('@xterm/addon-search').SearchAddon | null>(null);
    const searchInitialQueryRef = useRef<string | null>(null);

    // ═══════════════════════════════════════════════════════════════════
    // 1. WASM terminal input hook
    // ═══════════════════════════════════════════════════════════════════
    const wasmXtermRef = useRef<XTerm | null>(null);
    const { wasmActive, wasmActiveRef, wasmLoading, wasmError, handleWasmInput } = useWasmTerminalInput({
      xtermRef: wasmXtermRef,
      isActive,
      isConnected,
    });

    // ═══════════════════════════════════════════════════════════════════
    // 2. Search hook
    // ═══════════════════════════════════════════════════════════════════
    const searchXtermRef = useRef<XTerm | null>(null);
    const {
      searchBarRef,
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
    } = useTerminalSearch({
      xtermRef: searchXtermRef,
      searchAddonRef,
    });

    // ═══════════════════════════════════════════════════════════════════
    // 3. Scrollback hook
    // ═══════════════════════════════════════════════════════════════════
    const scrollbackXtermRef = useRef<XTerm | null>(null);
    const { saveScrollback, loadScrollbackToTerminal } = useTerminalScrollback({
      xtermRef: scrollbackXtermRef,
    });

    // ═══════════════════════════════════════════════════════════════════
    // 4. PTY input handler with reverse-i-search overlay tracking
    // ═══════════════════════════════════════════════════════════════════
    const terminalWSRefForPty = useRef<import('../services/terminalWebSocket').TerminalWebSocketService | null>(null);

    /** Batch reverse-search query updates to avoid excessive re-renders */
    const scheduleReverseSearchUpdate = useCallback(() => {
      if (reverseSearchTimerRef.current !== null) {
        clearTimeout(reverseSearchTimerRef.current);
      }
      reverseSearchTimerRef.current = window.setTimeout(() => {
        setReverseSearchQuery(reverseSearchQueryRef.current);
        reverseSearchTimerRef.current = null;
      }, 20);
    }, []);

    /**
     * Handle input for remote PTY sessions with reverse-i-search overlay support.
     *
     * When not in WASM mode, this monitors input for Ctrl+R (reverse-i-search) and
     * maintains overlay state showing the search query. The overlay is purely visual
     * and doesn't interfere with the actual PTY data flow.
     */
    const handlePtyInput = useCallback((data: string) => {
      terminalWSRefForPty.current?.sendRawInput(data);

      if (data.length > 1) {
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

      if (ch === '\x12') {
        if (!reverseSearchActiveRef.current) {
          reverseSearchActiveRef.current = true;
          reverseSearchQueryRef.current = '';
          setReverseSearchVisible(true);
          setReverseSearchQuery('');
        }
        return;
      }

      if (ch === '\r' || ch === '\n' || ch === '\x03' || ch === '\x1b') {
        reverseSearchActiveRef.current = false;
        reverseSearchQueryRef.current = '';
        setReverseSearchVisible(false);
        setReverseSearchQuery('');
        return;
      }

      if (ch === '\x7f' || ch === '\b') {
        if (reverseSearchActiveRef.current && reverseSearchQueryRef.current.length > 0) {
          reverseSearchQueryRef.current = reverseSearchQueryRef.current.slice(0, -1);
          scheduleReverseSearchUpdate();
        }
        return;
      }

      if (reverseSearchActiveRef.current && (ch >= ' ' || ch === '\t')) {
        reverseSearchQueryRef.current += ch;
        scheduleReverseSearchUpdate();
        return;
      }
    }, [scheduleReverseSearchUpdate]);

    // ═══════════════════════════════════════════════════════════════════
    // 5. Reverse search reset helper
    // ═══════════════════════════════════════════════════════════════════
    const resetReverseSearch = useCallback(() => {
      reverseSearchActiveRef.current = false;
      reverseSearchQueryRef.current = '';
      setReverseSearchVisible(false);
      setReverseSearchQuery('');
    }, []);

    // ═══════════════════════════════════════════════════════════════════
    // 6. xterm initialization hook
    // ═══════════════════════════════════════════════════════════════════
    const onData = useCallback((data: string) => {
      if (wasmActiveRef.current) {
        handleWasmInput(data);
      } else {
        handlePtyInput(data);
      }
    }, [handleWasmInput, handlePtyInput, wasmActiveRef]);

    const onPaste = useCallback((text: string) => {
      if (wasmActiveRef.current) {
        handleWasmInput(text);
      } else {
        handlePtyInput(text);
      }
    }, [handleWasmInput, handlePtyInput, wasmActiveRef]);

    const onSearchResults = useCallback((resultIndex: number | undefined, resultCount: number | undefined) => {
      setSearchResults(resultIndex, resultCount);
    }, [setSearchResults]);

    const onSearchToggle = useCallback((selection: string | null) => {
      if (searchVisible) {
        searchAddonRef.current?.clearDecorations();
        setSearchVisible(false);
      } else {
        searchInitialQueryRef.current = selection;
        setSearchVisible(true);
      }
    }, [searchVisible, setSearchVisible]);

    // Save scrollback for dispose — needs session's terminalWSRef
    const onSaveScrollbackForDispose = useCallback((sessionId: string) => {
      saveScrollback(sessionId);
    }, [saveScrollback]);

    // ═══════════════════════════════════════════════════════════════════
    // We need session hook's sendResize BEFORE creating xterm hook's
    // resize observer. But session hook needs xtermRef from xterm hook.
    // Solution: Use refs to break the circular dependency.
    // Session hook owns sendResize. TerminalPane wires resize observer
    // effects that call session's sendResize.
    // ═══════════════════════════════════════════════════════════════════

    // Temporarily null — will be wired after session hook creates it
    const sessionTerminalWSRef = useRef<import('../services/terminalWebSocket').TerminalWebSocketService | null>(null);

    const getSessionIdForXterm = useCallback((): string | undefined => {
      return sessionTerminalWSRef.current?.getSessionId() ?? undefined;
    }, []);

    const {
      paneWrapperRef,
      xtermContainerRef,
      xtermRef,
      fitAddonRef,
      searchAddonRef: xtermSearchAddonRef,
    } = useTerminalXTerm({
      isActive,
      fontSize,
      copyOnSelect,
      themePackId: themePack.id,
      onData,
      onPaste,
      onSearchResults,
      onSearchToggle,
      onSaveScrollback: onSaveScrollbackForDispose,
      getSessionId: getSessionIdForXterm,
    });

    // Wire xtermRef to the other hooks' xterm refs
    useEffect(() => {
      const t = xtermRef.current;
      wasmXtermRef.current = t;
      searchXtermRef.current = t;
      scrollbackXtermRef.current = t;
    });

    // Wire searchAddonRef from xterm hook to search hook
    useEffect(() => {
      searchAddonRef.current = xtermSearchAddonRef.current;
    });

    // ═══════════════════════════════════════════════════════════════════
    // 7. Session hook
    // ═══════════════════════════════════════════════════════════════════
    const {
      paneConnected,
      terminalWSRef,
      eventHandlerRef,
      sendResize,
      lastRestoreTimeRef,
    } = useTerminalSession({
      isActive,
      isConnected,
      xtermRef,
      fitAddonRef,
      preferredShell: preferredShell ?? null,
      reattachSessionId: reattachSessionId ?? null,
      onConnectionChange,
      onProcessExit,
      onResetSearch: resetSearch,
      onResetReverseSearch: resetReverseSearch,
      onSaveScrollback: saveScrollback,
      onLoadScrollback: loadScrollbackToTerminal,
    });

    // Wire session's terminalWSRef for PTY input handler and xterm dispose
    useEffect(() => {
      terminalWSRefForPty.current = terminalWSRef.current;
      sessionTerminalWSRef.current = terminalWSRef.current;
    });

    // ═══════════════════════════════════════════════════════════════════
    // 8. Resize observer (wires session sendResize with xterm layout)
    // ═══════════════════════════════════════════════════════════════════
    const resizeTimerRef = useRef<number | null>(null);
    const expandTimeoutRef = useRef<number | null>(null);
    const isMountedRef = useRef(true);

    useEffect(() => {
      if (!isActive || !paneConnected) return;

      const schedule = () => {
        if (resizeTimerRef.current !== null) window.clearTimeout(resizeTimerRef.current);
        resizeTimerRef.current = window.setTimeout(sendResize, 80);
      };

      // Skip the immediate resize if we just restored from a reattach — the
      // session_restored handler already sent a resize to avoid duplicate
      // SIGWINCH events that cause prompt line duplication.
      if (Date.now() - lastRestoreTimeRef.current > 5000) {
        schedule();
      }
      window.addEventListener('resize', schedule);

      let observer: ResizeObserver | null = null;
      if ('ResizeObserver' in window) {
        observer = new ResizeObserver(schedule);
        if (paneWrapperRef.current) observer.observe(paneWrapperRef.current);
        if (xtermContainerRef.current) observer.observe(xtermContainerRef.current);
      }

      return () => {
        window.removeEventListener('resize', schedule);
        observer?.disconnect();
        if (resizeTimerRef.current !== null) {
          window.clearTimeout(resizeTimerRef.current);
          resizeTimerRef.current = null;
        }
      };
    }, [isActive, paneConnected, sendResize, paneWrapperRef, xtermContainerRef]);

    // Listen for terminal expand event
    useEffect(() => {
      if (!isActive || !paneConnected) return;

      const handleExpand = () => {
        expandTimeoutRef.current = window.setTimeout(() => {
          if (isMountedRef.current) {
            sendResize();
          }
        }, EXPAND_RESIZE_DELAY_MS);
      };

      window.addEventListener('sprout-terminal-expand', handleExpand);
      return () => {
        window.removeEventListener('sprout-terminal-expand', handleExpand);
        if (expandTimeoutRef.current !== null) {
          window.clearTimeout(expandTimeoutRef.current);
          expandTimeoutRef.current = null;
        }
      };
    }, [isActive, paneConnected, sendResize]);

    // ═══════════════════════════════════════════════════════════════════
    // 9. Expose methods to parent via useImperativeHandle
    // ═══════════════════════════════════════════════════════════════════
    useImperativeHandle(ref, () => ({
      clear: () => xtermRef.current?.clear(),
      focus: () => xtermRef.current?.focus(),
      cleanup: () => {
        const service = terminalWSRef.current;
        if (!service) return;

        // Save scrollback before disconnecting
        const sessionId = service.getSessionId();
        const term = xtermRef.current;
        if (sessionId && term) {
          saveScrollback(sessionId);
        }

        // Remove the event handler
        if (eventHandlerRef.current) {
          service.removeEvent(eventHandlerRef.current);
          eventHandlerRef.current = null;
        }

        service.closeSession();
        service.disconnect();
        terminalWSRef.current = null;
      },
    }));

    // ═══════════════════════════════════════════════════════════════════
    // 10. Context menu handlers
    // ═══════════════════════════════════════════════════════════════════
    const getXTerminal = useCallback(() => xtermRef.current, []);
    const hasXTermSelection = useCallback(() => xtermRef.current?.hasSelection() ?? false, []);
    const handleContextCopy = useCallback((text: string) => {
      copyToClipboard(text).catch((err) => {
        debugLog('[TerminalPane] clipboard copy failed:', err);
      });
    }, []);
    const handleContextPaste = useCallback((text: string) => {
      if (wasmActiveRef.current) {
        handleWasmInput(text);
      } else {
        handlePtyInput(text);
      }
    }, [handleWasmInput, handlePtyInput, wasmActiveRef]);
    const handleContextClear = useCallback(() => {
      xtermRef.current?.clear();
    }, []);
    const handleContextSelectAll = useCallback(() => {
      xtermRef.current?.selectAll();
    }, []);
    const handleContextSplitPane = useCallback((direction: 'horizontal' | 'vertical') => {
      const action = direction === 'horizontal' ? 'split_horizontal' : 'split_vertical';
      window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: { action } }));
    }, []);

    // ═══════════════════════════════════════════════════════════════════
    // Render
    // ═══════════════════════════════════════════════════════════════════

    // Cleanup reverse search timer on unmount
    useEffect(() => {
      return () => {
        if (reverseSearchTimerRef.current !== null) {
          clearTimeout(reverseSearchTimerRef.current);
          reverseSearchTimerRef.current = null;
        }
        isMountedRef.current = false;
      };
    }, []);

    return (
      <div className="terminal-pane" ref={paneWrapperRef}>
        {showCloseButton && (
          <div className="terminal-pane-header">
            <span className={`terminal-pane-dot ${paneConnected || wasmActive ? 'connected' : 'disconnected'}`} />
            <button className="terminal-pane-close" onClick={onClose} title="Close pane">
              <X size={12} />
            </button>
          </div>
        )}
        <TerminalSearchBar
          ref={searchBarRef}
          visible={searchVisible}
          onSearch={handleSearch}
          onClose={handleCloseSearch}
          matchIndex={matchIndex}
          matchCount={matchCount}
          searchError={searchError}
          onSearchError={handleSearchError}
          initialQuery={searchInitialQueryRef.current}
        />
        <div
          className="terminal-pane-content"
          onClick={() => xtermRef.current?.focus()}
        >
          <div ref={xtermContainerRef} className="terminal-xterm" />
          {!wasmActive && (
            <ReverseSearchOverlay
              query={reverseSearchQuery}
              visible={reverseSearchVisible}
            />
          )}
        </div>
        {!paneConnected && !wasmActive && !wasmLoading && (
          <div className="terminal-status-inline">
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            Loading terminal...
          </div>
        )}
        {wasmLoading && (
          <div className="terminal-status-inline">
            <Terminal size={14} className="inline-block mr-1 align-text-bottom" style={{ animation: 'spin 1s linear infinite' }} />
            Initializing browser shell (loading WebAssembly)...
          </div>
        )}
        {wasmError && !wasmActive && (
          <div className="terminal-status-inline" style={{ color: '#ef6b73' }}>
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            WASM shell failed: {wasmError}
          </div>
        )}
        {wasmActive && (
          <div className="terminal-status-inline" style={{ color: '#7ddf97' }}>
            <Terminal size={14} className="inline-block mr-1 align-text-bottom" />
            Browser shell · Files persist in IndexedDB
          </div>
        )}
        <TerminalContextMenu
          containerRef={xtermContainerRef}
          getTerminal={getXTerminal}
          hasSelection={hasXTermSelection}
          onCopy={handleContextCopy}
          onPaste={handleContextPaste}
          onSearch={handleContextSearch}
          onClear={handleContextClear}
          onSelectAll={handleContextSelectAll}
          onSplitPane={handleContextSplitPane}
        />
      </div>
    );
  },
);

TerminalPane.displayName = 'TerminalPane';

export default TerminalPane;
