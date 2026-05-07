import type { Terminal, ILinkProvider, ILink, IDisposable } from '@xterm/xterm';

/**
 * Regex pattern for file paths with line numbers.
 * Matches patterns like:
 * - ./foo.go:12:34
 * - ./foo.go:12
 * - foo.go:12:34
 * - foo.go:12
 * - /absolute/path/to/foo.go:12:34
 * - /absolute/path/to/foo.go:12
 *
 * Pattern breakdown:
 * - (?<=^|[\s(]) - Lookbehind: must start at line start or after whitespace/paren
 * - (\.?\/?(?:[\w.\/_-]+\/)?[\w_-]+\.[a-zA-Z][\w]*) - Capture group 1: file path
 *   - \.?\/? - Optional ./ or /
 *   - (?:[\w.\/_-]+\/)? - Optional directory segments (must end with /)
 *   - [\w_-]+ - Filename (letters, digits, underscores, hyphens)
 *   - \.[a-zA-Z][\w]* - File extension (dot + at least one letter, avoids IPs)
 * - (?::(\d+)) - Capture group 2: line number after colon
 * - (?::(\d+))? - Capture group 3: optional column number after colon
 * - (?=$|[\s),;:]) - Lookahead: must end at line end or before whitespace/delimiter/colon
 */
export const filePathPattern = /(?<=^|[\s(])(\.?\/?(?:[\w.\/_-]+\/)?[\w_-]+\.[a-zA-Z][\w]*)(?::(\d+))(?::(\d+))?(?=$|[\s),;:])/g;

/**
 * Result of parsing a file path match from the terminal.
 */
export interface FilePathMatch {
  /** The file path, with leading ./ stripped if present */
  path: string;
  /** The line number (1-based) */
  lineNumber: number;
  /** The column number (1-based), if provided */
  columnNumber?: number;
}

/**
 * Parse a regex match into a structured FilePathMatch.
 * Strips leading ./ from the path if present.
 */
export function parseFilePathMatch(match: RegExpMatchArray): FilePathMatch {
  const [, rawPath, lineStr, colStr] = match;
  const path = rawPath.startsWith('./') ? rawPath.slice(2) : rawPath;
  const lineNumber = parseInt(lineStr, 10);
  const columnNumber = colStr ? parseInt(colStr, 10) : undefined;

  return { path, lineNumber, columnNumber };
}

/**
 * Register a link provider on the terminal to detect file path patterns
 * and make them clickable. When clicked, dispatches a custom event to open
 * the file in the editor.
 *
 * @param terminal - The xterm.js Terminal instance
 * @returns An IDisposable for cleanup (call dispose() to unregister)
 */
export function registerTerminalFilePathLinks(terminal: Terminal): IDisposable {
  const linkProvider: ILinkProvider = {
    provideLinks(bufferLineNumber: number, callback: (links: ILink[] | undefined) => void): void {
      // Get the line from the active buffer (0-indexed)
      const line = terminal.buffer.active.getLine(bufferLineNumber - 1);
      if (!line) {
        callback(undefined);
        return;
      }

      // Get line text with trailing whitespace trimmed
      const lineText = line.translateToString(true);
      if (!lineText) {
        callback(undefined);
        return;
      }

      const links: ILink[] = [];

      // Reset regex state before scanning
      filePathPattern.lastIndex = 0;

      let match: RegExpExecArray | null;
      while ((match = filePathPattern.exec(lineText)) !== null) {
        const { path, lineNumber } = parseFilePathMatch(match);
        const startIndex = match.index;
        const endIndex = startIndex + match[0].length;

        links.push({
          range: {
            // x coordinates are 1-based in xterm's IBufferCellPosition
            start: { x: startIndex + 1, y: bufferLineNumber },
            end: { x: endIndex + 1, y: bufferLineNumber },
          },
          text: match[0], // Full matched text including line numbers
          activate(_event: MouseEvent, _text: string): void {
            // Dispatch custom event to open the file in the editor
            const customEvent = new CustomEvent('sprout:open-in-editor', {
              detail: { path, lineNumber },
            });
            window.dispatchEvent(customEvent);
          },
          decorations: {
            underline: true,
            pointerCursor: true,
          },
        });
      }

      callback(links.length > 0 ? links : undefined);
    },
  };

  return terminal.registerLinkProvider(linkProvider);
}
