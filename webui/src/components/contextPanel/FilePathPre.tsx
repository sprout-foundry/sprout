import { stripAnsiCodes } from '../../utils/ansi';

// FILE_PATH_RE matches paths like src/foo/bar.ts, ./pkg/server.go, /abs/path/file.js
const FILE_PATH_RE = /((?:\.\.?\/|\/(?!\/))?(?:[\w.-]+\/)+[\w.-]+\.\w{1,10})/g;

/** Renders preformatted tool text with file paths as clickable links that open in the editor. */
export function FilePathPre({ text }: { text: string }): JSX.Element {
  // Strip ANSI codes before processing to ensure clean rendering
  const cleanedText = stripAnsiCodes(text);

  const parts: Array<string | JSX.Element> = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  FILE_PATH_RE.lastIndex = 0;
  while ((match = FILE_PATH_RE.exec(cleanedText)) !== null) {
    if (match.index > lastIndex) {
      parts.push(cleanedText.slice(lastIndex, match.index));
    }
    const filePath = match[1];
    parts.push(
      <span
        key={match.index}
        className="tool-output-file-link"
        role="button"
        tabIndex={0}
        onClick={() => window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path: filePath } }))}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path: filePath } }));
          }
        }}
      >
        {filePath}
      </span>,
    );
    lastIndex = match.index + match[0].length;
  }
  if (lastIndex < cleanedText.length) {
    parts.push(cleanedText.slice(lastIndex));
  }
  return <pre>{parts}</pre>;
}
