import type { SearchAddon } from '@xterm/addon-search';
import type { Terminal as XTerm } from '@xterm/xterm';
import { X, TriangleAlert, Terminal } from 'lucide-react';
import React, { useEffect, useRef, useCallback, useImperativeHandle, forwardRef } from 'react';
import { useTheme } from '../contexts/ThemeContext';

// Hooks extracted from this component
import { useReverseSearch } from '../hooks/useReverseSearch';
import { useTerminalContextMenu } from '../hooks/useTerminalContextMenu';
import { useTerminalResize } from '../hooks/useTerminalResize';
import { useTerminalScrollback } from '../hooks/useTerminalScrollback';
import { useTerminalSearch } from '../hooks/useTerminalSearch';
import { useTerminalSession } from '../hooks/useTerminalSession';
import { useTerminalXTerm } from '../hooks/useTerminalXTerm';
import { useWasmTerminalInput } from '../hooks/useWasmTerminalInput';
import type { TerminalWebSocketService } from '../services/terminalWebSocket';
import ReverseSearchOverlay from './ReverseSearchOverlay';
import TerminalContextMenu from './TerminalContextMenu';
import TerminalSearchBar from './TerminalSearchBar';

export interface TerminalPaneHandle {
  clear: () => void;
  focus: () => void;
  /** Cleanup the pane's WebSocket connection when the session is being closed */
  cleanup: () => void;
}

interface TerminalPaneProps {
  /** Whether the parent terminal is expanded (mounted). */
  isActive: boolean;
  /** When true, the terminal should steal focus on init. Only the visible
      session in the focused pane should have this set. */
  shouldFocus?: boolean;
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

const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(
  (
    {
      isActive,
      shouldFocus = true,
      isConnected = true,
      showCloseButton,
      onClose,
      onConnectionChange,
      preferredShell,
      fontSize,
      reattachSessionId,
      onProcessExit,
      copyOnSelect = false,
    },
    ref,
  ) => {
    const { themePack } = useTheme();

    // ── Search state (needed before hooks so we can wire callbacks) ──
    const searchAddonRef = useRef<SearchAddon | null>(null);
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
    const terminalWSRefForPty = useRef<TerminalWebSocketService | null>(null);

    const { reverseSearchVisible, reverseSearchQuery, handlePtyInput, resetReverseSearch } = useReverseSearch({
      terminalWSRef: terminalWSRefForPty,
    });

    // ═══════════════════════════════════════════════════════════════════
    // 5. xterm initialization hook
    // ═══════════════════════════════════════════════════════════════════
    const onData = useCallback(
      (data: string) => {
        if (wasmActiveRef.current) {
          handleWasmInput(data);
        } else {
          handlePtyInput(data);
        }
      },
      [handleWasmInput, handlePtyInput, wasmActiveRef],
    );

    const onPaste = useCallback(
      (text: string) => {
        if (wasmActiveRef.current) {
          handleWasmInput(text);
        } else {
          handlePtyInput(text);
        }
      },
      [handleWasmInput, handlePtyInput, wasmActiveRef],
    );

    const onSearchResults = useCallback(
      (resultIndex: number | undefined, resultCount: number | undefined) => {
        setSearchResults(resultIndex, resultCount);
      },
      [setSearchResults],
    );

    const onSearchToggle = useCallback(
      (selection: string | null) => {
        if (searchVisible) {
          searchAddonRef.current?.clearDecorations();
          setSearchVisible(false);
        } else {
          searchInitialQueryRef.current = selection;
          setSearchVisible(true);
        }
      },
      [searchVisible, setSearchVisible],
    );

    // Save scrollback for dispose — needs session's terminalWSRef
    const onSaveScrollbackForDispose = useCallback(
      (sessionId: string) => {
        saveScrollback(sessionId);
      },
      [saveScrollback],
    );

    // ═══════════════════════════════════════════════════════════════════
    // We need session hook's sendResize BEFORE creating xterm hook's
    // resize observer. But session hook needs xtermRef from xterm hook.
    // Solution: Use refs to break the circular dependency.
    // Session hook owns sendResize. TerminalPane wires resize observer
    // effects that call session's sendResize.
    // ═══════════════════════════════════════════════════════════════════

    // Temporarily null — will be wired after session hook creates it
    const sessionTerminalWSRef = useRef<TerminalWebSocketService | null>(null);

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
      shouldFocus,
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
    const { paneConnected, terminalWSRef, eventHandlerRef, sendResize, lastRestoreTimeRef } = useTerminalSession({
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
    useTerminalResize({
      isActive,
      paneConnected,
      sendResize,
      paneWrapperRef,
      xtermContainerRef,
      lastRestoreTimeRef,
    });

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
    const {
      getXTerminal,
      hasXTermSelection,
      handleContextCopy,
      handleContextPaste,
      handleContextClear,
      handleContextSelectAll,
      handleContextSplitPane,
    } = useTerminalContextMenu({
      xtermRef,
      wasmActiveRef,
      handleWasmInput,
      handlePtyInput,
    });

    // ═══════════════════════════════════════════════════════════════════
    // Render
    // ═══════════════════════════════════════════════════════════════════
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
        <div className="terminal-pane-content" onClick={() => xtermRef.current?.focus()}>
          <div ref={xtermContainerRef} className="terminal-xterm" />
          {!wasmActive && <ReverseSearchOverlay query={reverseSearchQuery} visible={reverseSearchVisible} />}
        </div>
        {!paneConnected && !wasmActive && !wasmLoading && (
          <div className="terminal-status-inline">
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            Loading terminal...
          </div>
        )}
        {wasmLoading && (
          <div className="terminal-status-inline">
            <Terminal
              size={14}
              className="inline-block mr-1 align-text-bottom"
              style={{ animation: 'spin 1s linear infinite' }}
            />
            Initializing browser shell (loading WebAssembly)...
          </div>
        )}
        {wasmError && !wasmActive && (
          <div className="terminal-status-inline terminal-status-inline--error">
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            WASM shell failed: {wasmError}
          </div>
        )}
        {wasmActive && (
          <div className="terminal-status-inline terminal-status-inline--success">
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

const TerminalPaneComponent = React.memo(TerminalPane);
TerminalPaneComponent.displayName = 'TerminalPane';

export default TerminalPaneComponent;
