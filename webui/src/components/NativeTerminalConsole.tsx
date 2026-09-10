import { useCallback, useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import {
  TERMINAL_THEME_EXPORT,
  getTerminalFontFamilyExport,
  FONT_SIZE_DEFAULT_EXPORT,
} from '../hooks/useTerminalXTerm';
import { getWorkspaceCwd, resolveCdTarget, subscribeWorkspaceCwd, workspaceCwdLabel } from '../services/workspaceCwd';

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
 * no interactive programs, no ctrl-C). `cd` is implemented console-side:
 * the console tracks a session directory (validated against the native fs
 * via a suppressed `pwd` spawn before committing, chrooted to the workspace
 * root) and passes it with every spawn. Without a session `cd`, spawns use
 * the shared workspace cwd (services/workspaceCwd.ts; workspace root when
 * unset — exactly the pre-cwd behavior).
 */

type SpawnBridge = {
  terminalSpawn: (
    command: string,
    opts?: {
      /** Working directory for this spawn (workspace-relative; omitted =
       *  workspace root — exactly today's behavior). The native emulated
       *  shell resolves the command against it; `pwd` prints it. */
      cwd?: string;
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

/** Base prompt (workspace root): a subtle green `$ `. */
const PROMPT_ROOT = '\x1b[32m$\x1b[0m ';

/**
 * Prompt for a working directory: the last path segment before the `$ `
 * (e.g. `repo $ `), or `~` when the label itself is empty. Kept subtle —
 * same green, one short segment, no full path noise.
 */
function makePrompt(label: string): string {
  if (!label || label === '~') return PROMPT_ROOT;
  return `\x1b[32m${label}\x1b[0m \x1b[32m$\x1b[0m `;
}

export function NativeTerminalConsole(): React.ReactElement {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const busyRef = useRef(false);
  const runLineRef = useRef<(line: string) => void>(() => {});

  // Working directory shared with Files / Git / Agent (services/workspaceCwd).
  // Kept in a ref so the line-editor closure (created once on mount) always
  // renders the CURRENT prompt; re-renders on cwd change are unnecessary.
  const cwdRef = useRef(getWorkspaceCwd());
  const promptRef = useRef(makePrompt(workspaceCwdLabel(cwdRef.current)));
  // Session directory set by `cd` (or null = follow the shared cwd). Lives
  // in a ref: the line-editor closure reads it at submit time, and unlike
  // the shared cwd it must NOT be reset by Files-panel cwd changes.
  const sessionCwdRef = useRef<string | null>(null);

  useEffect(() => {
    const unsubscribe = subscribeWorkspaceCwd((cwd) => {
      cwdRef.current = cwd;
      promptRef.current = makePrompt(workspaceCwdLabel(cwd));
    });
    return unsubscribe;
  }, []);

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
      // best-effort: private mode blocks storage; losing the toast-suppression
      // flag only means one avoidable warning toast this session.
    }
    return () => {
      try {
        sessionStorage.removeItem('sprout-native-terminal');
      } catch {
        // best-effort: cleanup only — the flag is rewritten on next mount.
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
    term.write(promptRef.current);

    // Line-editor state (xterm has no line discipline here — the
    // transport is spawn-per-line, so we implement one).
    let lineBuf = '';
    const history: string[] = [];
    let histIdx = -1; // index into `history`; length == "blank line"
    let escState = 0; // 0 = normal, 1 = saw ESC, 2 = saw CSI/SS3 introducer

    const writePrompt = () => term.write(promptRef.current);
    const redraw = () => term.write(`\r\x1b[K${promptRef.current}${lineBuf}`);

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

    // On-screen keyboard: on iPadOS/iOS the keyboard OVERLAYS the webview
    // (WKWebView is not resized), so the layout viewport still claims the
    // full screen and a bottom-anchored terminal's input line lands behind
    // the keyboard. window.visualViewport reports the UNOBSCURED region —
    // fit xterm against it and shift the console up while the keyboard is
    // up, so the live prompt row is always on screen.
    let vvCleanup: (() => void) | null = null;
    const vv = window.visualViewport;
    if (vv) {
      const applyVv = () => {
        // Hidden tab: visualViewport reports stale/zero geometry — skip.
        if (vv.height < 1) return;
        // Keyboard compensation only while the keyboard is actually up
        // (visible region meaningfully shorter than the layout window);
        // otherwise clear the override so the normal flex layout and its
        // ResizeObserver path rule — desktop never gets an inline height.
        const keyboardUp = vv.height < window.innerHeight - 40;
        if (keyboardUp) {
          // Fit to the UNOBSCURED height, capped by the pane's layout box.
          const paneHeight = host.parentElement?.clientHeight ?? vv.height;
          const box = Math.min(vv.height, paneHeight);
          if (box < 1) return;
          host.style.height = `${box}px`;
        } else {
          host.style.height = '';
        }
        fitAddon.fit();
        // Keep the live prompt row visible after the keyboard shrinks the
        // grid (xterm keeps the old scroll position otherwise).
        term.scrollToBottom();
      };
      vv.addEventListener('resize', applyVv);
      vv.addEventListener('scroll', applyVv);
      applyVv();
      vvCleanup = () => {
        vv.removeEventListener('resize', applyVv);
        vv.removeEventListener('scroll', applyVv);
        host.style.height = '';
      };
    }

    return () => {
      vvCleanup?.();
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

      const prompt = () => term.write(promptRef.current);

      // One-shot cwd semantics: the session dir if the user `cd`'d, else
      // the shared cwd read at submit time (Files/Git/Agent stay synced).
      const cwd = sessionCwdRef.current ?? cwdRef.current;

      if (line === 'help') {
        term.writeln('Built-ins: ls cat head tail wc grep find touch rm mkdir echo pwd cd.');
        term.writeln('Pipes (|), sequences (;) and chains (&&) supported. All paths');
        term.writeln(
          cwd === ''
            ? 'resolve from the workspace root. Ctrl-C cancels input.'
            : `resolve from ${cwd}. Ctrl-C cancels input.`,
        );
        prompt();
        return;
      }

      // ── pwd — where this session is (may differ from the shared cwd) ──
      if (line === 'pwd') {
        term.writeln(cwd === '' ? '/' : `/${cwd}`);
        prompt();
        return;
      }

      // ── cd — session-scoped, chrooted to the workspace root ────────────
      // The native emulator is one-shot per line (no process to hold a
      // directory), so the console tracks the session dir here and passes
      // it with every spawn. Bare `cd` returns to following the shared
      // cwd. The native side still validates each spawn's cwd, so a
      // directory deleted mid-session exits honestly instead of lying.
      if (/^cd(\s|$)/.test(line)) {
        const arg = line.replace(/^cd\s*/, '').trim();
        if (arg === '') {
          sessionCwdRef.current = null; // follow the shared cwd again
          cwdRef.current = getWorkspaceCwd();
          promptRef.current = makePrompt(workspaceCwdLabel(cwdRef.current));
          prompt();
          return;
        }
        const target = resolveCdTarget(arg, cwdRef.current);
        if (target === null) {
          term.writeln(`cd: unsupported path: ${arg} (relative to ${cwd === '' ? '/' : `/${cwd}`})`);
          prompt();
          return;
        }
        // Validate by spawning pwd with the target cwd — the native side
        // rejects missing dirs with exit 1, so this can't lie. Output is
        // suppressed (own onChunk): only the exit code matters here.
        busyRef.current = true;
        const bridge = window.SproutStudio;
        if (!hasSpawnBridge(bridge)) {
          busyRef.current = false;
          term.writeln('sprout-studio: bridge unavailable');
          prompt();
          return;
        }
        void new Promise<void>((resolve) => {
          void bridge
            .terminalSpawn('pwd', {
              ...(target === '' ? {} : { cwd: target }),
              onChunk: () => {},
              onExit: (code: number) => {
                if (code === 0) {
                  sessionCwdRef.current = target;
                  cwdRef.current = target;
                  promptRef.current = makePrompt(workspaceCwdLabel(target));
                  term.writeln(`(session dir: ${target === '' ? '/' : `/${target}`})`);
                } else {
                  term.writeln(`cd: no such directory: ${target === '' ? '/' : `/${target}`}`);
                }
                resolve();
              },
              onError: (err: string) => {
                term.writeln(`\x1b[31mcd: ${err}\x1b[0m`);
                resolve();
              },
            })
            .catch((err: unknown) => {
              const reason = err instanceof Error ? err.message : err ? String(err) : 'unknown error';
              term.writeln(`\x1b[31mcd: ${reason}\x1b[0m`);
              resolve();
            });
        }).finally(() => {
          busyRef.current = false;
          prompt();
        });
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
            // Bridge contract: `cwd` is an OPTIONAL workspace-relative
            // directory. Omitted (workspace root) → exactly today's
            // behavior; the native side treats a missing cwd as the root.
            ...(cwd === '' ? {} : { cwd }),
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
          .catch((err: unknown) => {
            // The bridge promise itself rejected (channel missing, native
            // side threw before spawning). Show the reason when there is one.
            const reason = err instanceof Error ? err.message : err ? String(err) : 'unknown error';
            term.writeln(`\x1b[31mterminal error:\x1b[0m ${reason}`);
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
      className="native-terminal-console-host"
      // FitAddon measures THIS element's client box (excludes padding),
      // so the inset keeps the text grid clear of the edges while the
      // black background runs flush. The bottom padding includes the
      // home-indicator inset (viewport-fit=cover): the clearance lives
      // INSIDE the console's black surface — a portal-level inset band
      // painted --bg-secondary under this near-black and read as "a
      // slight margin around the terminal" on device.
      style={{
        padding: '6px 8px',
        paddingBottom: 'calc(6px + env(safe-area-inset-bottom, 0px))',
      }}
      role="region"
      aria-label="Terminal console"
    />
  );
}
