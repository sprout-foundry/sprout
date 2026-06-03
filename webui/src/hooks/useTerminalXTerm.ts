/**
 * useTerminalXTerm - manages xterm.js initialization and lifecycle
 * for the TerminalPane component.
 *
 * Extracted from TerminalPane.tsx. Handles xterm creation, addon loading,
 * theme/font sync, copyOnSelect, and wheel event prevention.
 * Resize observer and expand listener remain in TerminalPane since they
 * need sendResize from session hook (avoiding circular dependencies).
 */

import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import { Terminal as XTerm } from '@xterm/xterm';
import type { IDisposable } from '@xterm/xterm';
import { useRef, useEffect, useCallback } from 'react';
import '@xterm/xterm/css/xterm.css';
import { FONT_SIZE_DEFAULT } from '../components/terminalConstants';
import { registerTerminalFilePathLinks } from '../extensions/terminalFilePaths';
import { copyToClipboard } from '../utils/clipboard';
import { debugLog } from '../utils/log';

export interface UseTerminalXTermOptions {
  isActive: boolean;
  /** When true, the terminal should steal focus on init. Only the visible
      session in the focused pane should have this set. */
  shouldFocus?: boolean;
  fontSize?: number;
  copyOnSelect: boolean;
  themePackId: string;
  /** Called when xterm receives data (user typing). */
  onData: (data: string) => void;
  /** Called for clipboard paste (Ctrl+Shift+V). */
  onPaste: (text: string) => void;
  /** Called when search results change. */
  onSearchResults: (resultIndex: number | undefined, resultCount: number | undefined) => void;
  /** Called when Ctrl+Shift+F is pressed. Receives the current selection text. */
  onSearchToggle: (selection: string | null) => void;
  /** Save scrollback for a session (called during xterm dispose). */
  onSaveScrollback: (sessionId: string) => void;
  /** Get the terminal WebSocket service's session ID (for scrollback). */
  getSessionId: () => string | undefined;
  /** Called when xterm receives an OSC 0/2 title change. Empty/whitespace
      titles are filtered out before this fires. */
  onTitleChange?: (title: string) => void;
}

export interface UseTerminalXTermReturn {
  paneWrapperRef: React.RefObject<HTMLDivElement>;
  xtermContainerRef: React.RefObject<HTMLDivElement>;
  xtermRef: React.MutableRefObject<XTerm | null>;
  fitAddonRef: React.MutableRefObject<FitAddon | null>;
  searchAddonRef: React.MutableRefObject<SearchAddon | null>;
}

const TERMINAL_THEME = {
  background: '#05070d',
  foreground: '#d7dee9',
  cursor: '#5ea1ff',
  cursorAccent: '#05070d',
  selectionBackground: 'rgba(94, 161, 255, 0.25)',
  black: '#111827',
  red: '#ef6b73',
  green: '#7ddf97',
  yellow: '#f4d56f',
  blue: '#5ea1ff',
  magenta: '#c792ea',
  cyan: '#4fd3d9',
  white: '#d7dee9',
  brightBlack: '#5f6b7a',
  brightRed: '#ff8a92',
  brightGreen: '#96f0ad',
  brightYellow: '#ffe08a',
  brightBlue: '#86b8ff',
  brightMagenta: '#f0abfc',
  brightCyan: '#75e7eb',
  brightWhite: '#ffffff',
};

function getTerminalFontFamily(): string {
  const css = getComputedStyle(document.documentElement);
  const raw = (css.getPropertyValue('--font-mono') || '').trim();
  if (!raw || raw.includes('var(')) {
    return "'JetBrains Mono', 'SF Mono', 'Fira Code', 'Consolas', monospace";
  }
  return raw;
}

export function useTerminalXTerm(options: UseTerminalXTermOptions): UseTerminalXTermReturn {
  const {
    isActive,
    fontSize,
    copyOnSelect,
    themePackId,
    onData,
    onPaste,
    onSearchResults,
    onSearchToggle,
    onSaveScrollback,
    getSessionId,
    onTitleChange,
    shouldFocus = true,
  } = options;

  const paneWrapperRef = useRef<HTMLDivElement>(null);
  const xtermContainerRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const searchAddonRef = useRef<SearchAddon | null>(null);
  const linkProviderRef = useRef<IDisposable | null>(null);
  const copyOnSelectTimerRef = useRef<number | null>(null);
  const copyOnSelectRef = useRef(copyOnSelect);
  copyOnSelectRef.current = copyOnSelect;
  const fontSizeRef = useRef(fontSize);
  fontSizeRef.current = fontSize;

  // ── Wheel event handler ──────────────────────────────────────────
  const handleWheel = useCallback((e: WheelEvent) => {
    const term = xtermRef.current;
    if (!term) return;
    const buffer = term.buffer.active;
    const atTop = buffer.viewportY === 0;
    const atBottom = buffer.viewportY === buffer.baseY;
    const scrollingUp = e.deltaY < 0;
    const scrollingDown = e.deltaY > 0;
    if ((scrollingUp && !atTop) || (scrollingDown && !atBottom)) {
      e.preventDefault();
    }
  }, []);

  // Stabilize callbacks in refs so the xterm init effect doesn't
  // tear down / recreate when callbacks change.
  const onDataRef = useRef(onData);
  onDataRef.current = onData;
  const onPasteRef = useRef(onPaste);
  onPasteRef.current = onPaste;
  const onSearchToggleRef = useRef(onSearchToggle);
  onSearchToggleRef.current = onSearchToggle;
  const onSearchResultsRef = useRef(onSearchResults);
  onSearchResultsRef.current = onSearchResults;
  const onSaveScrollbackRef = useRef(onSaveScrollback);
  onSaveScrollbackRef.current = onSaveScrollback;
  const getSessionIdRef = useRef(getSessionId);
  getSessionIdRef.current = getSessionId;
  const onTitleChangeRef = useRef(onTitleChange);
  onTitleChangeRef.current = onTitleChange;

  // ── Initialize xterm when pane becomes active ─────────────────────
  useEffect(() => {
    if (!isActive || !xtermContainerRef.current || xtermRef.current) return;

    const term = new XTerm({
      convertEol: false,
      cursorBlink: true,
      allowProposedApi: true,
      fontFamily: getTerminalFontFamily(),
      fontSize: fontSizeRef.current ?? FONT_SIZE_DEFAULT,
      lineHeight: 1.2,
      letterSpacing: 0,
      scrollback: 5000,
      wordSeparator: ' ()[]{}\',"`',
      theme: TERMINAL_THEME,
    });
    const fitAddon = new FitAddon();
    const searchAddon = new SearchAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(searchAddon);
    term.open(xtermContainerRef.current);

    linkProviderRef.current = registerTerminalFilePathLinks(term);

    const container = xtermContainerRef.current;
    if (container) {
      container.addEventListener('wheel', handleWheel, { passive: false });
    }

    xtermRef.current = term;

    // Ctrl+Shift+C/V/F handler
    term.attachCustomKeyEventHandler((event: KeyboardEvent) => {
      if (event.ctrlKey && event.shiftKey && !event.altKey && !event.metaKey) {
        if (event.key.toLowerCase() === 'c') {
          event.preventDefault();
          if (term.hasSelection()) {
            copyToClipboard(term.getSelection()).catch((err) => {
              debugLog('[TerminalPane] clipboard copy failed:', err);
            });
          }
          return false;
        }
        if (event.key.toLowerCase() === 'v') {
          event.preventDefault();
          navigator.clipboard
            .readText()
            .then((text) => {
              onPasteRef.current(text);
            })
            .catch((err) => {
              debugLog('[TerminalPane] clipboard paste failed:', err);
            });
          return false;
        }
        if (event.key.toLowerCase() === 'f') {
          event.preventDefault();
          const sel = xtermRef.current?.getSelection();
          onSearchToggleRef.current(sel && sel.trim() ? sel.trim() : null);
          return false;
        }
      }
      return true;
    });

    fitAddonRef.current = fitAddon;
    searchAddonRef.current = searchAddon;

    // Search results listener
    const resultsDisposable = searchAddon.onDidChangeResults(
      (results: { resultIndex?: number; resultCount?: number }) => {
        onSearchResultsRef.current(results.resultIndex, results.resultCount);
      },
    );

    // OSC 0/2 title sequences (e.g. `\e]0;new title\a`). The parent decides
    // whether to honor the title for the tab name (it's overridden by manual
    // renames). Empty/whitespace titles are dropped — many shells emit a
    // bare `\e]0;\a` on prompt redraw.
    const titleDisposable = term.onTitleChange((title) => {
      const trimmed = (title ?? '').trim();
      if (!trimmed) return;
      onTitleChangeRef.current?.(trimmed);
    });

    // Data handler
    term.onData((data) => {
      onDataRef.current(data);
    });

    // Copy-on-select handler
    const selectionChangeDisposable = term.onSelectionChange(() => {
      if (copyOnSelectRef.current && term.hasSelection()) {
        if (copyOnSelectTimerRef.current !== null) {
          clearTimeout(copyOnSelectTimerRef.current);
        }
        copyOnSelectTimerRef.current = window.setTimeout(() => {
          if (!copyOnSelectRef.current) {
            copyOnSelectTimerRef.current = null;
            return;
          }
          try {
            const selection = term.getSelection();
            if (selection) {
              copyToClipboard(selection).catch((err) => {
                debugLog('[TerminalPane] copy-on-select failed:', err);
              });
            }
          } catch (err) {
            debugLog('[TerminalPane] copy-on-select failed:', err);
          }
          copyOnSelectTimerRef.current = null;
        }, 150);
      }
    });

    requestAnimationFrame(() => {
      fitAddon.fit();
      if (shouldFocus) {
        term.focus();
      }
    });

    return () => {
      // Save scrollback before disposing xterm
      const sessionId = getSessionIdRef.current();
      if (sessionId) {
        onSaveScrollbackRef.current(sessionId);
      }

      linkProviderRef.current?.dispose();
      linkProviderRef.current = null;
      resultsDisposable.dispose();
      titleDisposable.dispose();
      selectionChangeDisposable.dispose();
      if (copyOnSelectTimerRef.current !== null) {
        clearTimeout(copyOnSelectTimerRef.current);
        copyOnSelectTimerRef.current = null;
      }
      if (container) {
        container.removeEventListener('wheel', handleWheel);
      }
      try {
        term.dispose();
      } catch (err) {
        debugLog('[TerminalPane] failed to dispose xterm instance:', err);
      }
      xtermRef.current = null;
      fitAddonRef.current = null;
      searchAddonRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fontSize intentionally excluded; updates handled by separate effect
  }, [isActive, handleWheel]);

  // ── Keep theme and font size in sync ──────────────────────────────
  useEffect(() => {
    if (!xtermRef.current) return;
    xtermRef.current.options.theme = TERMINAL_THEME;
    xtermRef.current.options.fontFamily = getTerminalFontFamily();
    xtermRef.current.options.fontSize = fontSize ?? FONT_SIZE_DEFAULT;
    requestAnimationFrame(() => fitAddonRef.current?.fit());
  }, [themePackId, fontSize]);

  // ── Keep copyOnSelect ref in sync ─────────────────────────────────
  useEffect(() => {
    copyOnSelectRef.current = copyOnSelect;
    if (!copyOnSelect && copyOnSelectTimerRef.current !== null) {
      clearTimeout(copyOnSelectTimerRef.current);
      copyOnSelectTimerRef.current = null;
    }
  }, [copyOnSelect]);

  return {
    paneWrapperRef,
    xtermContainerRef,
    xtermRef,
    fitAddonRef,
    searchAddonRef,
  };
}

export default useTerminalXTerm;
