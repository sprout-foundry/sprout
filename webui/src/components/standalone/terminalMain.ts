/**
 * E-M3 standalone terminal — xterm.js + the WASM POSIX shell, no React.
 *
 * Mirrors the webui TerminalPane's WASM path: keystrokes accumulate into
 * a line, Enter executes via wasmShell.executeCommand, stdout/stderr are
 * written back, and the prompt tracks the shell's cwd. Tab completion
 * uses shell.autoComplete. Command history with Up/Down mirrors the
 * webui's terminalConstants defaults.
 *
 * Host protocol (window.postMessage):
 *   host → page: { source: 'sprout-host', type: 'run', command }
 *   host → page: { source: 'sprout-host', type: 'put', path, content }  // sync file into WASM FS
 *   host → page: { source: 'sprout-host', type: 'theme', ... }
 *   page → host: { source: 'sprout-terminal', type: 'ready' }
 *   page → host: { source: 'sprout-terminal', type: 'command', command, exitCode }
 */
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import type { WasmShell } from '../../services/wasmShell';

const host = document.getElementById('terminal')!;

const term = new Terminal({
  cursorBlink: true,
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
  fontSize: 13,
  theme: {
    background: '#0d1117',
    foreground: '#c9d1d9',
    green: '#3fb950',
    cyan: '#39c5cf',
    blue: '#58a6ff',
  },
});
const fit = new FitAddon();
term.loadAddon(fit);
term.open(host);
fit.fit();

let wasm: WasmShell | null = null;
let inputBuffer = '';
let historyIndex = -1;
const commandHistory: string[] = [];

function prompt() {
  const cwd = wasm ? wasm.getCwd().replace(/^\/home\/user/, '~') : '~';
  term.write(`\r\n\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m${cwd}\x1b[0m$ `);
}

function post(type: string, payload: Record<string, unknown> = {}) {
  window.parent?.postMessage({ source: 'sprout-terminal', type, ...payload }, '*');
}

function execute(line: string) {
  const command = line.trim();
  if (!command) {
    prompt();
    return;
  }
  commandHistory.push(command);
  historyIndex = commandHistory.length;
  if (!wasm) {
    term.write('\r\n\x1b[31mwasm shell not ready\x1b[0m');
    prompt();
    return;
  }
  const result = wasm.executeCommand(command);
  if (result.stdout) term.write(`\r\n${result.stdout.replace(/\n/g, '\r\n')}`);
  if (result.stderr) term.write(`\r\n\x1b[31m${result.stderr.replace(/\n/g, '\r\n')}\x1b[0m`);
  post('command', { command, exitCode: result.exitCode });
  prompt();
}

term.onData((data) => {
  if (!wasm) return;
  switch (data) {
    case '\r': // Enter
      term.write('\r\n');
      execute(inputBuffer);
      inputBuffer = '';
      break;
    case '\u007f': // Backspace
      if (inputBuffer.length > 0) {
        inputBuffer = inputBuffer.slice(0, -1);
        term.write('\b \b');
      }
      break;
    case '\t': {
      // Tab completion
      const completions = wasm.autoComplete(inputBuffer);
      if (completions.completions && completions.completions.length === 1) {
        const completion = completions.completions[0];
        term.write('\x1b[K'); // clear to end of line
        term.write(
          `\r\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m${wasm.getCwd().replace(/^\/home\/user/, '~')}\x1b[0m$ ${completion}`,
        );
        inputBuffer = completion;
      } else if (completions.completions && completions.completions.length > 1) {
        term.write(`\r\n${completions.completions.join('  ')}\r\n`);
        term.write(
          `\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m${wasm.getCwd().replace(/^\/home\/user/, '~')}\x1b[0m$ ${inputBuffer}`,
        );
      }
      break;
    }
    case '\u0003': // Ctrl+C
      inputBuffer = '';
      term.write('^C');
      prompt();
      break;
    case '\u001b[A': {
      // Up
      if (historyIndex > 0) {
        historyIndex--;
        const cmd = commandHistory[historyIndex];
        term.write(
          `\x1b[2K\r\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m${wasm.getCwd().replace(/^\/home\/user/, '~')}\x1b[0m$ ${cmd}`,
        );
        inputBuffer = cmd;
      }
      break;
    }
    case '\u001b[B': {
      // Down
      if (historyIndex < commandHistory.length - 1) {
        historyIndex++;
        const cmd = commandHistory[historyIndex];
        term.write(
          `\x1b[2K\r\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m${wasm.getCwd().replace(/^\/home\/user/, '~')}\x1b[0m$ ${cmd}`,
        );
        inputBuffer = cmd;
      } else {
        historyIndex = commandHistory.length;
        term.write(
          `\x1b[2K\r\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m${wasm.getCwd().replace(/^\/home\/user/, '~')}\x1b[0m$ `,
        );
        inputBuffer = '';
      }
      break;
    }
    default:
      if (data >= ' ' && data !== '\u007f') {
        inputBuffer += data;
        term.write(data);
      }
  }
});

window.addEventListener('message', (ev: MessageEvent) => {
  const data = ev.data;
  if (!data || data.source !== 'sprout-host') return;
  if (data.type === 'run') {
    term.write(data.command);
    execute(String(data.command));
    inputBuffer = '';
  }
  if (data.type === 'put' && wasm) {
    // Host-synced file push (D5: the native host owns the file store and
    // mirrors editor saves into this instance's WASM FS).
    const err = wasm.writeFile(String(data.path), String(data.content ?? ''));
    if (err) term.writeln(`\x1b[31mput ${data.path} failed: ${err}\x1b[0m`);
  }
});

window.addEventListener('resize', () => fit.fit());

async function boot() {
  term.writeln('\x1b[1mSprout Studio terminal (WASM POSIX shell)\x1b[0m');
  term.writeln('Type \x1b[32mhelp\x1b[0m for available commands.');
  try {
    const mod = await import('../../services/wasmShell');
    wasm = await mod.initWasmShell({});
  } catch (err) {
    term.writeln(`\x1b[31mwasm init failed: ${String(err)}\x1b[0m`);
  }
  prompt();
  term.focus();
  post('ready');
}

boot();
