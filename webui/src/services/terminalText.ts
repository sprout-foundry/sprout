import type { WasmShellResult } from './wasmShell';

/** A result as a terminal shows it: output in execution order, errors in red. */
export function terminalText(res: WasmShellResult): string {
  const segments = res.output ?? [{ text: res.stdout }, { err: true, text: res.stderr }];
  return segments
    .filter((s) => s.text)
    .map((s) => {
      const text = s.text.replace(/\r?\n/g, '\r\n');
      return s.err ? `\x1b[31m${text}\x1b[0m` : text;
    })
    .join('');
}
