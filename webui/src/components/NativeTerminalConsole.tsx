import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * Native-mode interactive console (Track R, terminal portion).
 *
 * Rendered by TerminalPane INSTEAD of the inert "provided by the native
 * shell" placeholder when the native terminal gate is active (ratified
 * build + shell declares the `terminal` capability). Each submitted line
 * goes over the §15 bridge channel (`window.SproutStudio.terminalSpawn`)
 * to the shell's concrete transport (workspace-scoped emulated shell on
 * iOS), streaming `chunk` pushes into a scrollback buffer — so the user
 * gets a working prompt surface instead of a dead handoff notice.
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

const WELCOME = [
  'Native shell console (workspace-scoped emulated shell).',
  'Commands: ls cat head tail wc grep find touch rm mkdir echo pwd · pipes · && ;',
  'type `help` for details',
].join('\n');

export function NativeTerminalConsole(): React.ReactElement {
  const [lines, setLines] = useState<string[]>(WELCOME.split('\n'));
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const append = useCallback((...newLines: string[]) => {
    setLines((prev) => [...prev, ...newLines]);
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

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines]);

  const runLine = useCallback(
    async (raw: string) => {
      const line = raw.trim();
      append(`$ ${line}`);
      if (!line) return;

      const bridge = window.SproutStudio;
      if (!hasSpawnBridge(bridge)) {
        append('sprout-studio: bridge unavailable');
        return;
      }

      if (line === 'help') {
        append(
          'Built-ins: ' +
            'ls cat head tail wc grep find touch rm mkdir echo pwd. ' +
            'Pipes (|), sequences (;) and chains (&&) supported. ' +
            'All paths resolve from the workspace root.',
        );
        return;
      }

      // The emulated shell is rooted at the workspace root and has no
      // `cd` builtin — say so instead of silently misbehaving.
      if (/^cd(\s|$)/.test(line)) {
        append('cd: not supported (commands run from the workspace root)');
        return;
      }

      setBusy(true);
      let exitCode = 0;
      try {
        await new Promise<void>((resolve) => {
          void bridge
            .terminalSpawn(line, {
              onChunk: (text) => {
                // Emulator output arrives atomically; split into lines
                // and drop a single trailing empty (final newline).
                const parts = String(text).split('\n');
                if (parts.length > 1 && parts[parts.length - 1] === '') parts.pop();
                if (parts.length) append(...parts);
              },
              onExit: (code) => {
                exitCode = code;
                resolve();
              },
              onError: (err) => {
                append(`error: ${err}`);
                exitCode = 127;
                resolve();
              },
            })
            .catch(() => {
              append('terminalSpawn: bridge call failed');
              resolve();
            });
        });
      } finally {
        setBusy(false);
        if (exitCode !== 0) append(`(exit ${exitCode})`);
      }
    },
    [append],
  );

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0d1117] font-mono text-[13px] text-[#c9d1d9]">
      <div
        ref={scrollRef}
        className="min-h-0 flex-1 overflow-y-auto px-3 py-2"
        onClick={() => inputRef.current?.focus()}
      >
        {lines.map((l, i) => (
          <div key={i} className="whitespace-pre-wrap break-words leading-[1.45]">
            {l}
          </div>
        ))}
        {busy && <div className="text-[#8b949e]">…</div>}
      </div>
      <form
        className="flex items-center gap-2 border-t border-[#21262d] px-3 py-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (busy) return;
          const value = input;
          setInput('');
          void runLine(value);
        }}
      >
        <span className="shrink-0 text-[#3fb950]">$</span>
        <input
          ref={inputRef}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          className="min-w-0 flex-1 bg-transparent text-[#c9d1d9] outline-none"
          placeholder="run a command…"
          autoFocus
          spellCheck={false}
          autoCapitalize="off"
          autoComplete="off"
        />
      </form>
    </div>
  );
}
