import { useCallback, useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import {
  TERMINAL_THEME_EXPORT,
  getTerminalFontFamilyExport,
  FONT_SIZE_DEFAULT_EXPORT,
} from '../hooks/useTerminalXTerm';

/**
 * Native-mode interactive console (Track R, terminal portion).
 *
 * Rendered by TerminalPane INSTEAD of the inert "provided by the native
 * shell" placeholder when the native terminal gate is active (ratified
 * build + shell declares the `terminal` capability). Each submitted line
 * goes over the §15 bridge channel (`window.SproutStudio.terminalSpawn`)
 * to the shell's concrete transport (workspace-scoped emulated shell on
 * iOS), streaming `chunk` pushes into a real xterm.js grid — the same
 * engine, theme, and font the PTY tier renders with, so every terminal
 * surface in the app looks identical.
 *
 * One-shot command semantics: every line is an independent spawn (no PTY,
 * no interactive programs, no ctrl-C). The emulated shell is rooted at
 * the workspace root and has no `cd` — surfaced honestly.
 */

type SpawnBridge = {
  terminalSpawn: (
    command: string,
    opts?: {
      onChunk?: (text: string) => void;
      onExit?: (exitCode: number) => void;
      onError?: (error: string) => void;
    },
  ) => Promise<unknown>;
};

function hasSpawnBridge(bridge: unknown): bridge is SpawnBridge {
  return (
    typeof bridge === 'object' &&
    bridge !== null &&
    typeof (bridge as { terminalSpawn?: unknown }).terminalSpawn === 'function'
  );
}

declare global {
  interface Window {
    SproutStudio?: unknown;
  }
}

const WELCOME = ['\x1b[2mtype help for commands\x1b[0m'];

const PROMPT = '\x1b[32m$\x1b[0m ';

export function NativeTerminalConsole(): React.ReactElement {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const busyRef = useRef(false);
  const runLineRef = useRef<(line: string) => void>(() => {});

  const println = useCallback((text: string) => {
    const term = termRef.current;
    if (!term) return;
    for (const line of text.split('\n')) term.writeln(line);
  }, []);

  useEffect(() => {
    // Mark native mode for useAvailableShells: the daemon shells
    // endpoint does not exist in the app, and in native mode it never
    // will — suppress the fetch and the warning toast entirely.
    try {
      sessionStorage.setItem('sprout-native-terminal', '1');
    } catch {
      /* private mode: toast suppression is cosmetic, ignore */
    }
    return () => {
      try {
        sessionStorage.removeItem('sprout-native-terminal');
      } catch {
        /* ignore */
      }
    };
  }, []);

  // ── xterm lifecycle ────────────────────────────────────────────────
  useEffect(() => {
    const host = hostRef.current;
    if (!host || termRef.current) return;

    const term = new Terminal({
      fontFamily: getTerminalFontFamilyExport(),
      fontSize: FONT_SIZE_DEFAULT_EXPORT,
      lineHeight: 1.2,
      letterSpacing: 0,
      scrollback: 5000,
      wordSeparator: ' ()[]{}\',"`',
      theme: TERMINAL_THEME_EXPORT,
      cursorBlink: true,
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(host);
    termRef.current = term;

    // Defer first fit one frame so the container has layout.
    const raf = requestAnimationFrame(() => fitAddon.fit());
    for (const line of WELCOME) term.writeln(line);
    term.write(PROMPT);

    // Line-editor state (xterm has no line discipline here — the
    // transport is spawn-per-line, so we implement one).
    let lineBuf = '';
    let history: string[] = [];
    let histIdx = -1; // index into `history`; length == "blank line"
    let escState = 0; // 0 = normal, 1 = saw ESC, 2 = saw CSI/SS3 introducer

    const writePrompt = () => term.write(PROMPT);
    const redraw = () => term.write(`\r\x1b[K${PROMPT}${lineBuf}`);

    const recall = (delta: -1 | 1) => {
      if (history.length === 0) return;
      const next = histIdx + delta;
      if (next < 0 || next > history.length) return;
      histIdx = next;
      lineBuf = histIdx === history.length ? '' : history[histIdx];
      redraw();
    };

    const dataSub = term.onData((data) => {
      if (busyRef.current) return; // input locked while a command runs
      for (const ch of data) {
        const code = ch.codePointAt(0) ?? 0;

        // Escape-sequence state machine first, so typed letters like
        // 'A', 'B', 'O' never collide with arrow-key bytes.
        if (escState === 0 && ch === '\x1b') {
          escState = 1;
          continue;
        }
        if (escState === 1) {
          escState = ch === '[' || ch === 'O' ? 2 : 0; // other: Alt-combo, drop
          continue;
        }
        if (escState === 2) {
          escState = 0;
          if (ch === 'A') recall(-1);
          else if (ch === 'B') recall(1);
          // other CSI termini (C/D/~/…): unused here
          continue;
        }

        if (ch === '\r') {
          const line = lineBuf;
          lineBuf = '';
          histIdx = history.length;
          term.write('\r\n');
          if (line.trim()) {
            history.push(line);
            histIdx = history.length;
            void runLineRef.current(line);
          } else {
            writePrompt();
          }
        } else if (ch === '\x7f') {
          if (lineBuf.length > 0) {
            lineBuf = lineBuf.slice(0, -1);
            term.write('\b \b');
          }
        } else if (code === 3) {
          // Ctrl-C: cancel the current input line
          lineBuf = '';
          histIdx = history.length;
          term.write('^C\r\n');
          writePrompt();
        } else if (code === 4 && lineBuf.length === 0) {
          // Ctrl-D on an empty line: print a hint (no persistent
          // session to exit — say so instead of doing nothing).
          term.write('^D\r\n');
          term.writeln('(one-shot console: no session to exit)');
          writePrompt();
        } else if (code >= 32) {
          lineBuf += ch;
          term.write(ch);
        }
        // other control chars: ignored
      }
    });

    const onResize = () => fitAddon.fit();
    window.addEventListener('resize', onResize);
    const ro = new ResizeObserver(() => fitAddon.fit());
    ro.observe(host);

    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener('resize', onResize);
      ro.disconnect();
      dataSub.dispose();
      term.dispose();
      termRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const runLine = useCallback(
    (line: string) => {
      const term = termRef.current;
      if (!term) return;

      const prompt = () => term.write(PROMPT);

      if (line === 'help') {
        term.writeln(
          'Built-ins: ls cat head tail wc grep find touch rm mkdir echo pwd.',
        );
        term.writeln(
          'Pipes (|), sequences (;) and chains (&&) supported. All paths',
        );
        term.writeln('resolve from the workspace root. Ctrl-C cancels input.');
        prompt();
        return;
      }

      // The emulated shell is rooted at the workspace root and has no
      // `cd` builtin — say so instead of silently misbehaving.
      if (/^cd(\s|$)/.test(line)) {
        term.writeln('cd: not supported (commands run from the workspace root)');
        prompt();
        return;
      }

      busyRef.current = true;
      let exitCode = 0;
      void new Promise<void>((resolve) => {
        const bridge = window.SproutStudio;
        if (!hasSpawnBridge(bridge)) {
          term.writeln('sprout-studio: bridge unavailable');
          resolve();
          return;
        }
        void bridge
          .terminalSpawn(line, {
            onChunk: (text) => {
              // Emulator output arrives atomically; strip ONE final
              // newline (writeln re-adds line endings).
              const t = String(text);
              println(t.endsWith('\n') ? t.slice(0, -1) : t);
            },
            onExit: (code) => {
              exitCode = code;
              resolve();
            },
            onError: (err) => {
              term.writeln(`\x1b[31merror:\x1b[0m ${err}`);
              exitCode = 127;
              resolve();
            },
          })
          .catch(() => {
            term.writeln('terminalSpawn: bridge call failed');
            resolve();
          });
      }).finally(() => {
        busyRef.current = false;
        if (exitCode !== 0) term.writeln(`\x1b[2m(exit ${exitCode})\x1b[0m`);
        prompt();
      });
    },
    [println],
  );

  useEffect(() => {
    runLineRef.current = runLine;
  }, [runLine]);

  return (
    <div
      ref={hostRef}
      className="h-full min-h-0 w-full overflow-hidden bg-[#05070d]"
      style={{ padding: '6px 8px' }}
    />
  );
}
