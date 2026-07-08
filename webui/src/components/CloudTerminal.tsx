/**
 * CloudTerminal — lightweight xterm.js terminal backed by WASM shell.
 *
 * Renders directly in cloud mode, bypassing the Terminal/TerminalPane/PTY
 * infrastructure. Connects xterm.js straight to SproutWasm.executeCommand().
 */

import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useCallback, useEffect, useRef, useState } from 'react';
import { initWasmShell, type WasmShell } from '../services/wasmShell';
import '@xterm/xterm/css/xterm.css';
import './Terminal.css';

export default function CloudTerminal() {
  const containerRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const shellRef = useRef<WasmShell | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const currentLineRef = useRef('');
  const historyRef = useRef<string[]>([]);
  const historyIdxRef = useRef(-1);

  // Init xterm + WASM shell
  useEffect(() => {
    if (!containerRef.current) return;

    const terminal = new XTerm({
      cursorBlink: true,
      cursorStyle: 'bar',
      fontSize: 13,
      fontFamily: "'Cascadia Code', 'Fira Code', 'JetBrains Mono', 'Consolas', monospace",
      theme: {
        background: '#0f172a',
        foreground: '#e2e8f0',
        cursor: '#38bdf8',
        cursorAccent: '#0f172a',
        selectionBackground: '#334155',
      },
      allowProposedApi: true,
      scrollback: 5000,
    });

    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(containerRef.current);
    fitAddon.fit();
    fitAddonRef.current = fitAddon;

    xtermRef.current = terminal;

    // Load WASM shell
    initWasmShell()
      .then(shell => {
        shellRef.current = shell;
        setLoading(false);
        writePrompt(terminal, shell);
        setupInput(terminal, shell);
      })
      .catch(err => {
        setError(err instanceof Error ? err.message : String(err));
        setLoading(false);
      });

    // Resize observer
    const ro = new ResizeObserver(() => {
      try { fitAddon.fit(); } catch { /* ignore */ }
    });
    ro.observe(containerRef.current);

    return () => {
      ro.disconnect();
      terminal.dispose();
    };
  }, []);

  function setupInput(terminal: XTerm, shell: WasmShell) {
    terminal.onData(data => {
      for (const char of data) {
        switch (char) {
          case '\r': // Enter
            terminal.write('\r\n');
            executeLine(terminal, shell);
            break;
          case '\x7f': // Backspace
            if (currentLineRef.current.length > 0) {
              currentLineRef.current = currentLineRef.current.slice(0, -1);
              terminal.write('\b \b');
            }
            break;
          case '\x03': // Ctrl+C
            terminal.write('^C\r\n');
            currentLineRef.current = '';
            writePrompt(terminal, shell);
            break;
          case '\x0c': // Ctrl+L
            terminal.clear();
            writePrompt(terminal, shell);
            terminal.write(currentLineRef.current);
            break;
          case '\x1b': // Escape sequences (arrows come as multi-char)
            break;
          default:
            if (char >= ' ') {
              currentLineRef.current += char;
              terminal.write(char);
            }
            break;
        }
      }
    });
  }

  function executeLine(terminal: XTerm, shell: WasmShell) {
    const cmd = currentLineRef.current.trim();
    currentLineRef.current = '';

    if (!cmd) {
      writePrompt(terminal, shell);
      return;
    }

    // History
    const hist = historyRef.current;
    if (hist.length === 0 || hist[hist.length - 1] !== cmd) {
      hist.push(cmd);
    }
    historyIdxRef.current = -1;

    // Execute
    try {
      const result = shell.executeCommand(cmd);
      if (result.stdout) terminal.write(result.stdout);
      if (result.stderr) terminal.write('\x1b[31m' + result.stderr + '\x1b[0m');
    } catch (err) {
      terminal.write('\x1b[31mError: ' + (err instanceof Error ? err.message : String(err)) + '\x1b[0m');
    }

    writePrompt(terminal, shell);
  }

  function writePrompt(terminal: XTerm, shell: WasmShell) {
    try {
      let cwd = shell.getCwd();
      const home = '/home/user';
      if (cwd.startsWith(home)) cwd = '~' + cwd.slice(home.length);
      terminal.write('\x1b[1;32muser@wasm\x1b[0m:\x1b[1;34m' + cwd + '\x1b[0m$ ');
    } catch {
      terminal.write('\x1b[1;32muser@wasm\x1b[0m:~$ ');
    }
  }

  if (error) {
    return (
      <div className="terminal-container" data-testid="cloud-terminal">
        <div className="terminal-pane" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-secondary)', padding: '12px' }}>
          WASM shell failed: {error}
        </div>
      </div>
    );
  }

  return (
    <div className="terminal-container" data-testid="cloud-terminal">
      <div ref={containerRef} style={{ flex: 1, minHeight: 0 }} />
      {loading && (
        <div className="terminal-status-inline">
          Initializing browser shell (loading WebAssembly)...
        </div>
      )}
    </div>
  );
}
