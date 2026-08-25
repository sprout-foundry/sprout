import { useEffect, useRef, useCallback, useState } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import ContextMenu from './ContextMenu';
import { Copy, ClipboardPaste, Trash2, TextSelect } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { debugLog } from '../utils/log';
import { copyToClipboard } from '../utils/clipboard';
import { FONT_SIZE_DEFAULT } from './terminalConstants';

/** Minimal theme info needed for terminal colors */
export interface TerminalThemePack {
  name: string;
  terminal?: {
    background?: string;
    foreground?: string;
    cursor?: string;
    selectionBackground?: string;
    fontSize?: number;
    fontFamily?: string;
  };
}

/** Factory function to create terminal WebSocket connections */
export type CreateTerminalConnection = (sessionId: string) => {
  send: (data: string) => void;
  onData: (callback: (data: string) => void) => void;
  onExit: (callback: (code: number) => void) => void;
  close: () => void;
};

export interface TerminalPaneHandle {
  clear: () => void;
  focus: () => void;
  cleanup: () => void;
}

interface TerminalPaneProps {
  isActive: boolean;
  sessionId: string;
  fontSize?: number;
  isSplit?: boolean;
  themePack?: TerminalThemePack;
  createConnection?: CreateTerminalConnection;
  createWasmShell?: () => Promise<{
    write: (data: string) => void;
    onData: (callback: (data: string) => void) => void;
    close: () => void;
  } | null>;
  onFocus?: () => void;
}

const CONTEXT_MENU_ITEMS = [
  { id: 'copy', label: 'Copy', icon: Copy, shortcut: 'Cmd/Ctrl+Shift+C' },
  { id: 'paste', label: 'Paste', icon: ClipboardPaste, shortcut: 'Cmd/Ctrl+Shift+V' },
  { id: 'clear', label: 'Clear Terminal', icon: Trash2 },
  { id: 'selectAll', label: 'Select All', icon: TextSelect, shortcut: 'Cmd/Ctrl+A' },
];

function TerminalPane({
  isActive,
  sessionId,
  fontSize = FONT_SIZE_DEFAULT,
  isSplit = false,
  themePack,
  createConnection,
  createWasmShell,
  onFocus,
}: TerminalPaneProps): JSX.Element {
  const termRef = useRef<XTerm | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const connRef = useRef<ReturnType<CreateTerminalConnection> | null>(null);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null);

  // Initialize xterm
  useEffect(() => {
    if (!containerRef.current) return;

    const term = new XTerm({
      fontSize,
      fontFamily:
        themePack?.terminal?.fontFamily ??
        "'JetBrains Mono', 'Fira Code', Menlo, Monaco, monospace",
      cursorBlink: true,
      convertEol: true,
      scrollback: 10000,
      // Characters that delimit words for double-click selection.
      // Default matches xterm.js upstream: space, parens, brackets, braces,
      // quotes, comma, and backtick. Triple-click selects the entire line.
      wordSeparator: ' ()[]{}\',"`',
      theme: {
        background: themePack?.terminal?.background ?? '#1e1e2e',
        foreground: themePack?.terminal?.foreground ?? '#cdd6f4',
        cursor: themePack?.terminal?.cursor ?? '#f5e0dc',
        selectionBackground: themePack?.terminal?.selectionBackground ?? '#585b7066',
      },
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(containerRef.current);

    try { fitAddon.fit(); } catch { /* ignore */ }

    termRef.current = term;
    fitAddonRef.current = fitAddon;

    term.onData((data) => {
      connRef.current?.send(data);
    });

    const onFocusFn = () => onFocus?.();
    term.element?.addEventListener('focus', onFocusFn);

    return () => {
      term.element?.removeEventListener('focus', onFocusFn);
      term.dispose();
      termRef.current = null;
      fitAddonRef.current = null;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Font size updates
  useEffect(() => {
    if (termRef.current) {
      termRef.current.options.fontSize = fontSize;
      try { fitAddonRef.current?.fit(); } catch { /* ignore */ }
    }
  }, [fontSize]);

  // Connect to terminal session
  useEffect(() => {
    if (!isActive || !termRef.current || !createConnection) return;

    const conn = createConnection(sessionId);
    connRef.current = conn;

    conn.onData((data: string) => {
      termRef.current?.write(data);
    });

    conn.onExit((code: number) => {
      debugLog(`[TerminalPane] Session ${sessionId} exited with code ${code}`);
      termRef.current?.write(`\r\n\x1b[90m[Process exited with code ${code}]\x1b[0m\r\n`);
    });

    return () => {
      conn.close();
      connRef.current = null;
    };
  }, [isActive, sessionId, createConnection]);

  // WASM shell
  useEffect(() => {
    if (!isActive || !termRef.current || !createWasmShell) return;

    let disposed = false;
    let shell: { write: (data: string) => void; onData: (cb: (data: string) => void) => void; close: () => void } | null = null;
    let inputDisposable: { dispose: () => void } | null = null;

    createWasmShell().then((s) => {
      if (disposed || !s) return;
      shell = s;
      shell.onData((data: string) => {
        termRef.current?.write(data);
      });
      inputDisposable = termRef.current?.onData((data) => {
        shell?.write(data);
      }) ?? null;
    });

    return () => {
      disposed = true;
      inputDisposable?.dispose();
      shell?.close();
    };
  }, [isActive, createWasmShell]);

  // Resize on split change
  useEffect(() => {
    if (fitAddonRef.current) {
      const timer = setTimeout(() => {
        try { fitAddonRef.current?.fit(); } catch { /* ignore */ }
      }, 50);
      return () => clearTimeout(timer);
    }
  }, [isSplit]);

  // Context menu
  const handleContextMenu = useCallback((e: ReactMouseEvent) => {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY });
  }, []);

  const handleContextAction = useCallback((id: string) => {
    setContextMenu(null);
    const term = termRef.current;
    if (!term) return;

    switch (id) {
      case 'copy': {
        const sel = term.getSelection();
        if (sel) copyToClipboard(sel);
        break;
      }
      case 'paste': {
        navigator.clipboard.readText().then((text) => {
          if (text) term.paste(text);
        }).catch(() => { debugLog('[TerminalPane] Clipboard read failed'); });
        break;
      }
      case 'clear':
        term.clear();
        break;
      case 'selectAll':
        term.selectAll();
        break;
    }
  }, []);

  return (
    <>
      <div
        ref={containerRef}
        className="terminal-pane"
        onContextMenu={handleContextMenu}
        style={{ width: '100%', height: '100%' }}
      />
      <ContextMenu
        isOpen={contextMenu !== null}
        x={contextMenu?.x ?? 0}
        y={contextMenu?.y ?? 0}
        onClose={() => setContextMenu(null)}
      >
        {CONTEXT_MENU_ITEMS.map((item) => (
          <button
            key={item.id}
            className="context-menu-item"
            onClick={() => handleContextAction(item.id)}
          >
            <item.icon size={14} />
            <span>{item.label}</span>
            {item.shortcut && <span className="context-menu-shortcut">{item.shortcut}</span>}
          </button>
        ))}
      </ContextMenu>
    </>
  );
}

export default TerminalPane;
