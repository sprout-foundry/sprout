/** Static code analysis utilities for code actions — pure TypeScript, no CM6 dependencies. */
import type { CodeAction, CodeActionEdit } from './codeActions';

// ─── Document Types ────────────────────────────────────────────────

export interface DocLine {
  number: number;
  text: string;
  from: number;
  to: number;
  length: number;
}

export interface Doc {
  toString(): string;
  line(n: number): DocLine;
  lineAt(pos: number): DocLine;
  lines: number;
  length: number;
}

export interface Selection {
  empty: boolean;
  from: number;
  to: number;
}

// ─── Main Entry Point ─────────────────────────────────────────────

/** Compute all static code actions for the given cursor position (no LSP needed). */
export function computeStaticActions(doc: Doc, lineNum: number, selection: Selection, filePath: string): CodeAction[] {
  const actions: CodeAction[] = [];
  const ext = filePath.split('.').pop()?.toLowerCase() || '';

  // Validate line number is within document bounds
  if (lineNum < 1 || lineNum > doc.lines) return actions;

  // Get the line content
  const lineInfo = doc.line(lineNum);
  const lineContent = lineInfo.text;

  // ─── Action: Remove all trailing whitespace in file (always available) ───
  const fullContent = doc.toString();
  const trimmedFullContent = fullContent.replace(/[ \t]+\n/g, '\n').replace(/[ \t]+$/gm, '');
  if (trimmedFullContent !== fullContent) {
    actions.push({
      title: 'Remove all trailing whitespace',
      kind: 'source.removeTrailingWhitespace',
      edits: [
        {
          filePath,
          from: 0,
          to: doc.length,
          newText: trimmedFullContent,
        },
      ],
    });
  }

  // ─── Action: Convert tabs to spaces ───
  if (lineContent.includes('\t')) {
    const indentWidth = ['go', 'python', 'rs'].includes(ext) ? 4 : 2;
    const spacesContent = doc.toString().replace(/\t/g, ' '.repeat(indentWidth));
    if (spacesContent !== doc.toString()) {
      actions.push({
        title: 'Convert tabs to spaces',
        kind: 'refactor.convertTabs',
        edits: [
          {
            filePath,
            from: 0,
            to: doc.length,
            newText: spacesContent,
          },
        ],
      });
    }
  }

  // ─── Action: Convert leading spaces to tabs ───
  const leadingSpacesMatch = lineContent.match(/^( +)/);
  if (leadingSpacesMatch) {
    const spaceCount = leadingSpacesMatch[1].length;
    if (spaceCount >= 2 && (spaceCount % 2 === 0 || spaceCount % 4 === 0)) {
      const tabifyContent = doc.toString().replace(new RegExp(`^ {${spaceCount}}`, 'gm'), '\t');
      if (tabifyContent !== doc.toString()) {
        actions.push({
          title: 'Convert spaces to tabs',
          kind: 'refactor.convertSpaces',
          edits: [
            {
              filePath,
              from: 0,
              to: doc.length,
              newText: tabifyContent,
            },
          ],
        });
      }
    }
  }

  // ─── Action: Remove trailing whitespace on current line ───
  const trailingMatch = lineContent.match(/(\s+)$/);
  if (trailingMatch && trailingMatch[1].length > 0) {
    const trailingStart = lineInfo.from + (lineContent.length - trailingMatch[1].length);
    const trailingEnd = lineInfo.to;
    actions.push({
      title: 'Remove trailing whitespace',
      kind: 'refactor.remove',
      edits: [
        {
          filePath,
          from: trailingStart,
          to: trailingEnd,
          newText: '',
        },
      ],
    });
  }

  // ─── Check if cursor is near imports ───
  const isNearImport = lineNum <= 4 || isNearImportLine(lineNum, doc);

  // ─── JS/TS: Remove unused imports ───
  if (['js', 'ts', 'jsx', 'tsx', 'mjs', 'mts'].includes(ext) && isNearImport) {
    const jsUnusedActions = findUnusedJsImports(doc, filePath);
    actions.push(...jsUnusedActions);
  }

  // ─── Go: Remove unused imports ───
  if (ext === 'go' && isNearImport) {
    const goUnusedActions = findUnusedGoImports(doc, filePath);
    actions.push(...goUnusedActions);
  }

  // ─── Action: Remove empty lines around cursor ───
  const hasEmptyBefore = lineNum > 1 && doc.line(lineNum - 1).length === 0;
  const hasEmptyAfter = lineNum < doc.lines && doc.line(lineNum + 1).length === 0;

  if (hasEmptyBefore || hasEmptyAfter) {
    const edits: CodeActionEdit[] = [];
    if (hasEmptyBefore) {
      const emptyLineInfo = doc.line(lineNum - 1);
      const currentLine = doc.line(lineNum);
      // Remove from start of empty line to start of current line (includes the newline)
      edits.push({
        filePath,
        from: emptyLineInfo.from,
        to: currentLine.from,
        newText: '',
      });
    }
    if (hasEmptyAfter && lineNum < doc.lines) {
      const emptyLineInfo = doc.line(lineNum + 1);
      const nextLine = emptyLineInfo.number < doc.lines ? doc.line(emptyLineInfo.number + 1) : null;
      // Remove from start of empty line to start of next line (or end of doc if last line)
      edits.push({
        filePath,
        from: emptyLineInfo.from,
        to: nextLine ? nextLine.from : emptyLineInfo.to,
        newText: '',
      });
    }
    if (edits.length > 0) {
      actions.push({
        title: 'Remove empty lines',
        kind: 'refactor.remove',
        edits,
      });
    }
  }

  // ─── Action: Sort selected lines alphabetically ───
  if (!selection.empty) {
    const fromLine = doc.lineAt(selection.from).number;
    const toLine = doc.lineAt(selection.to).number;

    if (fromLine !== toLine) {
      const lines: string[] = [];
      for (let i = fromLine; i <= toLine; i++) {
        lines.push(doc.line(i).text);
      }

      const sortedLines = [...lines].sort((a, b) =>
        a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' }),
      );
      const isAlreadySorted = lines.every((line, idx) => line === sortedLines[idx]);

      if (!isAlreadySorted) {
        const fromPos = doc.line(fromLine).from;
        const toPos = doc.line(toLine).to;
        actions.push({
          title: 'Sort lines alphabetically',
          kind: 'refactor.sort',
          edits: [
            {
              filePath,
              from: fromPos,
              to: toPos,
              newText: sortedLines.join('\n'),
            },
          ],
        });
      }
    }
  }

  return actions;
}

// ─── Import Line Detection ────────────────────────────────────────

/** Check if the given line number is near an import declaration (within 3 lines). */
export function isNearImportLine(lineNum: number, doc: Doc): boolean {
  const start = Math.max(1, lineNum - 3);
  const end = Math.min(doc.lines, lineNum + 3);

  for (let i = start; i <= end; i++) {
    const lineText = doc.line(i).text.trim();
    // Check for JS/TS imports (including type imports)
    if (/^import\s+(type\s+)?[{'"]./.test(lineText)) return true;
    // Check for Go imports
    if (/^import\s+["(]/.test(lineText)) return true;
  }
  return false;
}

// ─── JS/TS Import Analysis ────────────────────────────────────────

interface ParsedImport {
  lineNum: number;
  path: string;
  symbols: string[];
  isDefault: boolean;
  isStar: boolean;
  lineText: string;
  from: number;
  to: number;
}

/** Find unused JS/TS imports and generate code actions. Scans first 100 lines, checks usage after. */
export function findUnusedJsImports(doc: Doc, filePath: string): CodeAction[] {
  const actions: CodeAction[] = [];
  const fullContent = doc.toString();

  const scanEnd = Math.min(doc.lines, 100);
  const importPattern = /^import\s+(?:{([^}]+)}|(\*)|([A-Z][a-zA-Z0-9]*))\s+from\s+['"]([^'"]+)['"]/;
  const defaultImportPattern = /^import\s+([A-Z][a-zA-Z0-9]*)\s+from\s+['"]([^'"]+)['"]/;

  const imports: ParsedImport[] = [];

  // Parse imports
  for (let i = 1; i <= scanEnd; i++) {
    const line = doc.line(i);
    const text = line.text.trim();

    // Handle type-only imports: strip "type " keyword for pattern matching
    const importText = text.replace(/^import\s+type\s+/, 'import ');

    // Named imports: import { a, b, c } from 'module'
    const namedMatch = importText.match(importPattern);
    if (namedMatch) {
      const symbols = namedMatch[1]
        ? namedMatch[1]
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean)
        : [];
      const isStar = namedMatch[2] === '*';
      const isDefault = namedMatch[3] !== undefined;
      const defaultName = namedMatch[3];

      if (isStar) {
        imports.push({
          lineNum: i,
          path: namedMatch[4],
          symbols: ['*'],
          isDefault: false,
          isStar: true,
          lineText: line.text,
          from: line.from,
          to: line.to,
        });
      } else if (isDefault && defaultName) {
        imports.push({
          lineNum: i,
          path: namedMatch[4],
          symbols: [defaultName],
          isDefault: true,
          isStar: false,
          lineText: line.text,
          from: line.from,
          to: line.to,
        });
      } else if (symbols.length > 0) {
        imports.push({
          lineNum: i,
          path: namedMatch[4],
          symbols,
          isDefault: false,
          isStar: false,
          lineText: line.text,
          from: line.from,
          to: line.to,
        });
      }
      continue;
    }

    // Default imports: import Component from 'module'
    const defaultMatch = importText.match(defaultImportPattern);
    if (defaultMatch) {
      imports.push({
        lineNum: i,
        path: defaultMatch[2],
        symbols: [defaultMatch[1]],
        isDefault: true,
        isStar: false,
        lineText: line.text,
        from: line.from,
        to: line.to,
      });
    }
  }

  if (imports.length === 0) return [];

  // Get code after imports (what we search for usage)
  const importEndPos = doc.line(scanEnd).to;
  const codeAfterImports = fullContent.slice(importEndPos);

  // Check each import for unused symbols
  for (const imp of imports) {
    const unusedSymbols: string[] = [];
    let allUnused = false;

    for (const sym of imp.symbols) {
      // Skip 'default' keyword itself
      if (sym === 'default' || sym === '*') continue;

      // Check if symbol is used in code
      const symbolPattern = new RegExp(`\\b${sym.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\b`);
      if (!symbolPattern.test(codeAfterImports)) {
        unusedSymbols.push(sym);
      }
    }

    // If all symbols are unused, remove the entire line
    if (unusedSymbols.length === imp.symbols.length) {
      allUnused = true;
    }

    if (allUnused || unusedSymbols.length > 0) {
      if (allUnused) {
        // Remove entire import line
        actions.push({
          title: `Remove unused import from '${imp.path}'`,
          kind: 'quickfix.unusedImport',
          edits: [
            {
              filePath,
              from: imp.from,
              to: imp.to,
              newText: '',
            },
          ],
        });
      } else if (unusedSymbols.length > 0) {
        const remainingSymbols = imp.symbols.filter((s) => !unusedSymbols.includes(s));

        if (remainingSymbols.length === 0) {
          // All remaining are unused, remove the line
          actions.push({
            title: `Remove unused import from '${imp.path}'`,
            kind: 'quickfix.unusedImport',
            edits: [
              {
                filePath,
                from: imp.from,
                to: imp.to,
                newText: '',
              },
            ],
          });
        } else {
          // Update import to only keep used symbols
          const newImportText = `import { ${remainingSymbols.join(', ')} } from '${imp.path}'`;
          actions.push({
            title: `Remove unused import: ${unusedSymbols.join(', ')} from '${imp.path}'`,
            kind: 'quickfix.unusedImport',
            edits: [
              {
                filePath,
                from: imp.from,
                to: imp.to,
                newText: newImportText,
              },
            ],
          });
        }
      }
    }
  }

  return actions;
}

// ─── Go Import Analysis ────────────────────────────────────────────

interface ParsedGoImport {
  lineNum: number;
  path: string;
  identifier: string;
  from: number;
  to: number;
  lineText: string;
}

/** Find unused Go imports. Scans import blocks and checks if package identifier is used. */
export function findUnusedGoImports(doc: Doc, filePath: string): CodeAction[] {
  const actions: CodeAction[] = [];
  const fullContent = doc.toString();

  // Matches single-line imports: import "fmt" or import f "fmt" (with optional alias)
  const singleImportPattern = /^import\s+(?:[a-zA-Z_][a-zA-Z0-9_]*\s+)?"([^"]+)"/;

  const imports: ParsedGoImport[] = [];

  // Scan first 100 lines
  const scanEnd = Math.min(doc.lines, 100);

  // Check for import block
  let inImportBlock = false;
  let importBlockStart = -1;
  let importBlockEnd = -1;

  for (let i = 1; i <= scanEnd; i++) {
    const line = doc.line(i);
    const text = line.text.trim();

    // Check for import block start - matches "import" alone or "import ("
    if (/^import\s*(\(|$)/.test(text)) {
      inImportBlock = true;
      importBlockStart = i;
      continue;
    }

    // If in import block, check for end
    if (inImportBlock) {
      if (text === ')') {
        importBlockEnd = i;
        // Collect all imports in the block
        for (let j = importBlockStart + 1; j < importBlockEnd; j++) {
          const blockLine = doc.line(j);
          const blockText = blockLine.text.trim();
          const match = blockText.match(/"([^"]+)"/);
          if (match) {
            const path = match[1];
            // Match aliased imports like: fmt "fmt" or "fmt"
            const idMatch = blockText.match(/^([a-zA-Z_][a-zA-Z0-9_]*)\s+"[^"]+"/);
            const id = idMatch ? idMatch[1] : path.split('/').pop() || path;
            // Skip underscore imports (used for side effects only)
            if (id === '_') continue;
            imports.push({
              lineNum: j,
              path,
              identifier: id,
              from: blockLine.from,
              to: blockLine.to,
              lineText: blockLine.text,
            });
          }
        }
        inImportBlock = false;
        importBlockStart = -1;
        importBlockEnd = -1;
      }
      continue;
    }

    // Single line imports
    const match = text.match(singleImportPattern);
    if (match) {
      const path = match[1];
      const aliasMatch = text.match(/^import\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+/);
      const id = aliasMatch ? aliasMatch[1] : path.split('/').pop() || path;
      // Skip underscore imports (used for side effects only)
      if (id === '_') continue;
      imports.push({ lineNum: i, path, identifier: id, from: line.from, to: line.to, lineText: line.text });
    }
  }

  if (imports.length === 0) return [];

  // Check each import for usage
  const codeAfterImports = fullContent.slice(doc.line(Math.min(doc.lines, 100)).to);

  for (const imp of imports) {
    const idPattern = new RegExp(`\\b${imp.identifier.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\b`);
    if (!idPattern.test(codeAfterImports)) {
      actions.push({
        title: `Remove unused import "${imp.path}"`,
        kind: 'quickfix.unusedImport',
        edits: [
          {
            filePath,
            from: imp.from,
            to: imp.to,
            newText: '',
          },
        ],
      });
    }
  }

  return actions;
}

// ─── Icon Mapping ──────────────────────────────────────────────

/** Get an icon label for a code action based on its kind. */
export function kindEmoji(kind: string): string {
  if (kind.includes('organizeImports') || kind.includes('import')) return '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16 3h5v5M8 3H3v5"/><path d="M21 8v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8"/><path d="M8 12h8"/><path d="M10 16h4"/></svg>';
  if (kind.includes('quickfix') || kind.includes('fix')) return '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>';
  if (kind.includes('remove') || kind.includes('delete')) return '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><line x1="10" x2="10" y1="11" y2="17"/><line x1="14" x2="14" y1="11" y2="17"/></svg>';
  if (kind.includes('refactor') || kind.includes('sort')) return '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 3 21 3 21 8"/><line x1="4" x2="21" y1="20" y2="20"/><polyline points="21 16 21 21 16 21"/><line x1="15" x2="21" y1="4" y2="10"/><line x1="4" x2="9" y1="20" y2="14"/></svg>';
  if (kind.includes('source') || kind.includes('removeTrailingWhitespace')) return '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4v7"/><path d="M4 20h8"/><path d="M20 11h-7"/><path d="M7 7l5 5-5 5"/><circle cx="18" cy="17" r="3"/><path d="M21 14v3h-3"/></svg>';
  return '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>';
}
