import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { KeyboardEvent, MouseEvent } from 'react';
import { fuzzyFilter, highlightMatches } from '../utils/fuzzyMatch';
import type { FuzzyResult } from '../utils/fuzzyMatch';
import './GoToSymbolOverlay.css';

// ── Types ────────────────────────────────────────────────────────────────

export interface SymbolInfo {
  name: string;
  line: number; // 1-based line number
  kind: SymbolKind;
}

export type SymbolKind = 'function' | 'method' | 'class' | 'variable' | 'type' | 'constant' | 'interface';

interface GoToSymbolOverlayProps {
  visible: boolean;
  content: string;
  fileExtension?: string;
  onSelectSymbol: (line: number) => void;
  onClose: () => void;
}

// ── Constants ────────────────────────────────────────────────────────────

const MAX_SYMBOLS = 500;

export const KIND_ICONS: Record<SymbolKind, string> = {
  function: 'ƒ',
  method: 'ƒ',
  class: 'C',
  variable: 'V',
  type: 'T',
  constant: 'K',
  interface: 'I',
};

// ── Language-specific patterns ───────────────────────────────────────────

/**
 * Each entry is a tuple of [RegExp, SymbolKind].
 * The RegExp must have exactly one capture group that extracts the symbol name.
 * Patterns are NOT global – a fresh regexp.exec() is called per line so
 * lastIndex is always 0.
 */
type PatternEntry = [RegExp, SymbolKind];

const GO_PATTERNS: PatternEntry[] = [
  // func Foo(...)
  [/\bfunc\s+(?:\([^)]+\)\s+)?([A-Z][a-zA-Z0-9_]*)\s*\(/, 'method'],
  // func foo(...)
  [/\bfunc\s+(?:\([^)]+\)\s+)?([a-z][a-zA-Z0-9_]*)\s*\(/, 'function'],
  // type Foo struct
  [/\btype\s+([A-Z][a-zA-Z0-9_]*)\s+struct\b/, 'class'],
  // type Foo interface
  [/\btype\s+([A-Z][a-zA-Z0-9_]*)\s+interface\b/, 'interface'],
  // type Foo = ...
  [/\btype\s+([A-Z][a-zA-Z0-9_]*)\s+=/, 'type'],
  // type Foo underlyingType (e.g. type Foo string)
  [/\btype\s+([A-Z][a-zA-Z0-9_]*)\s+[a-zA-Z]/, 'type'],
  // var Foo Type
  [/\bvar\s+([A-Z][a-zA-Z0-9_]*)\s+[a-zA-Z]/, 'variable'],
  // const Foo = iota / ...
  [/\bconst\s+([A-Z][a-zA-Z0-9_]*)\s+=/, 'constant'],
  // go-style interface method inside interface block:  Foo(args)
  // Accept tab or space indentation
  [/^\s+([A-Z][a-zA-Z0-9_]*)\s*\(/, 'method'],
  // go-style const() / var() block items: indented Exported = ...
  // Distinguish from interface methods by requiring '=' and not '('
  [/^\s+([A-Z][a-zA-Z0-9_]*)\s*=[^=]/, 'constant'],
];

const PYTHON_PATTERNS: PatternEntry[] = [
  // class Foo:
  [/\bclass\s+([A-Za-z_][a-zA-Z0-9_]*)\s*[:(]/, 'class'],
  // async def foo(
  [/\basync\s+def\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(/, 'function'],
  // def foo(
  [/\bdef\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(/, 'function'],
  // FOO = ... (module-level constant convention: upper snake case)
  [/^([A-Z_][A-Z0-9_]*)\s*=/, 'constant'],
  // foo = ... (module level variable)
  [/^([a-z_][a-zA-Z0-9_]*)\s*=[^=]/, 'variable'],
];

const TYPESCRIPT_PATTERNS: PatternEntry[] = [
  // export default function foo(
  [/\bexport\s+default\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[<(]/, 'function'],
  // export function foo(
  [/\bexport\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[<(]/, 'function'],
  // async function foo(
  [/\basync\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[<(]/, 'function'],
  // function foo(
  [/\bfunction\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[<(]/, 'function'],
  // export class Foo
  [/\bexport\s+(?:abstract\s+)?class\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'class'],
  // class Foo
  [/\bclass\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'class'],
  // export interface Foo
  [/\bexport\s+interface\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'interface'],
  // interface Foo
  [/\binterface\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'interface'],
  // export type Foo =
  [/\bexport\s+type\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=/, 'type'],
  // type Foo =
  [/\btype\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=/, 'type'],
  // const foo = ( must NOT be treated as function if value is not a function
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s*)?\(/, 'function'],
  // Arrow function: const foo = () =>
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[a-zA-Z_$][a-zA-Z0-9_$]*)\s*=>/, 'function'],
  // const foo: Type = ...
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*:/, 'variable'],
  // const foo = value (non-function)
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=/, 'variable'],
  // let foo = / let foo:
  [/\blet\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[=:(]/, 'variable'],
  // var foo = / var foo:
  [/\bvar\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[=:(]/, 'variable'],
  // class method: foo(args)
  // Uses a negative lookahead to exclude control-flow keywords
  // (if/for/while/switch/return/typeof/catch/throw/new delete case)
  // that could false-positive due to regex backtracking on ^\s+.
  [
    /^\s+(?:(?:public|private|protected|static|async|abstract|readonly)\s+)*\s*(?!if\b|for\b|while\b|switch\b|return\b|typeof\b|catch\b|throw\b|new\b|delete\b|case\b)([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/,
    'method',
  ],
  // uppercase const: const FOO = ...
  [/\bconst\s+([A-Z_][A-Z0-9_]*)\s*=/, 'constant'],
];

const JAVASCRIPT_PATTERNS: PatternEntry[] = [
  // export default function foo(
  [/\bexport\s+default\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/, 'function'],
  // export function foo(
  [/\bexport\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/, 'function'],
  // async function foo(
  [/\basync\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/, 'function'],
  // function foo(
  [/\bfunction\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/, 'function'],
  // export class Foo
  [/\bexport\s+class\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'class'],
  // class Foo
  [/\bclass\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'class'],
  // const foo = (...) =>
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[a-zA-Z_$][a-zA-Z0-9_$]*)\s*=>/, 'function'],
  // const foo = (immediately-invoked function expression)
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s*)?\(/, 'function'],
  // const foo = ...
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=/, 'variable'],
  // let foo =
  [/\blet\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=/, 'variable'],
  // var foo =
  [/\bvar\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=/, 'variable'],
  // class method (non-backtracking, same negative lookahead as TypeScript)
  [
    /^\s+(?:(?:public|private|protected|static|async|abstract|readonly)\s+)*\s*(?!if\b|for\b|while\b|switch\b|return\b|typeof\b|catch\b|throw\b|new\b|delete\b|case\b)([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/,
    'method',
  ],
  // uppercase const
  [/\bconst\s+([A-Z_][A-Z0-9_]*)\s*=/, 'constant'],
];

const GENERIC_PATTERNS: PatternEntry[] = [
  // function foo(
  [/\bfunction\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/, 'function'],
  // class Foo
  [/\bclass\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'class'],
  // interface Foo
  [/\binterface\s+([a-zA-Z_$][a-zA-Z0-9_$]*)/, 'interface'],
  // def foo(
  [/\bdef\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(/, 'function'],
  // func foo(
  [/\bfunc\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(/, 'function'],
  // const / let / var
  [/\b(?:const|let|var)\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=/, 'variable'],
  // type Foo struct
  [/\btype\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+struct/, 'class'],
  // type Foo interface
  [/\btype\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+interface/, 'interface'],
  // Method (non-backtracking, same negative lookahead)
  [
    /^\s+(?:(?:public|private|protected|static|async|abstract|readonly)\s+)*\s*(?!if\b|for\b|while\b|switch\b|return\b|typeof\b|catch\b|throw\b|new\b|delete\b|case\b)([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/,
    'method',
  ],
];

// ── Symbol extraction ────────────────────────────────────────────────────

/**
 * Extract symbols from `content` using regex patterns that are chosen based
 * on the file extension (`languageId`).  Falls back to generic patterns when
 * the language is unrecognised.
 */
export function extractSymbols(content: string, languageId?: string): SymbolInfo[] {
  const ext = languageId?.toLowerCase();
  let patterns: PatternEntry[];

  if (ext === '.go') {
    patterns = GO_PATTERNS;
  } else if (ext === '.py') {
    patterns = PYTHON_PATTERNS;
  } else if (ext === '.ts' || ext === '.tsx') {
    patterns = TYPESCRIPT_PATTERNS;
  } else if (ext === '.js' || ext === '.jsx' || ext === '.mjs') {
    patterns = JAVASCRIPT_PATTERNS;
  } else {
    patterns = GENERIC_PATTERNS;
  }

  const lines = content.split('\n');
  const seen = new Map<string, number>(); // `${name}:${line}` → first line (dedup)
  const symbols: SymbolInfo[] = [];

  for (let i = 0; i < lines.length && symbols.length < MAX_SYMBOLS; i++) {
    let line = lines[i];
    // Skip blank lines cheaply.
    if (!line || line.trim().length === 0) continue;

    // Strip single-line comments for Go (// style).
    // Backtick raw strings are not handled here, but they can't start
    // with // in a way that affects symbol extraction.
    if (ext === '.go') {
      const commentIdx = line.indexOf('//');
      if (commentIdx !== -1) line = line.slice(0, commentIdx);
      if (!line.trim()) continue;
    }

    for (const [pattern, kind] of patterns) {
      const m = pattern.exec(line);
      if (m) {
        const name = m[1];
        if (!name) continue;
        // Dedup: keep only one symbol per (name, line) pair.
        // Using name:line as key allows same-named symbols in different
        // classes (different lines) while still preventing duplicate
        // extractions from multiple patterns matching on the same line.
        const key = `${name}:${i + 1}`;
        if (seen.has(key)) continue;
        seen.set(key, i + 1);
        symbols.push({ name, line: i + 1, kind });
        break; // only one symbol per line
      }
    }
  }

  return symbols;
}

// ── Enclosing-symbol detection ───────────────────────────────────────────

/** Kinds that can act as scope containers for the breadcrumb. */
const CONTAINER_KINDS: ReadonlySet<SymbolKind> = new Set<SymbolKind>(['function', 'method', 'class', 'interface']);

/**
 * Find the 1-based end line (inclusive) of a symbol's scope by counting
 * balanced braces from the symbol's definition line onward.
 *
 * Handles:
 *  - Single-quoted, double-quoted, and backtick strings (skips braces inside).
 *  - Multi-line strings (the `inString` state persists across line boundaries).
 *  - `//` line comments (skips the rest of the line).
 *  - Block comments (skips the rest of the line inside a block comment).
 *  - Handles escape sequences inside strings (e.g. `\"`, `\'`, `\\`).
 *
 * If no matching close brace is found, returns `lines.length` (end of file).
 */
function findSymbolScopeEnd(lines: string[], startLineIndex: number): number {
  let braceCount = 0;
  let foundFirstBrace = false;
  let inBlockComment = false;
  // Tracks whether we are inside a string that spans across lines.
  // null  = not inside a string
  // "'" / '"' / '`' = the quote character that opened the string
  let inString: string | null = null;

  for (let i = startLineIndex; i < lines.length; i++) {
    const line = lines[i];
    for (let j = 0; j < line.length; j++) {
      const ch = line[j];

      // ── Inside a string (possibly multi-line) ────────────────────────
      if (inString) {
        // Escape sequence — skip the next character.
        // Note: j += 1 (not 2) because the for-loop's j++ increment
        // advances j by one more, effectively skipping exactly 2 chars
        // (the backslash and the escaped character).
        if (ch === '\\' && j + 1 < line.length) {
          j += 1; // skip backslash; loop j++ skips escaped char
          continue;
        }
        // Closing quote matching the one that opened the string
        if (ch === inString) {
          inString = null;
        }
        // Everything else inside the string is ignored (no brace counting)
        continue;
      }

      // ── Inside a block comment — look for */ ────────────────────────
      if (inBlockComment) {
        if (ch === '*' && j + 1 < line.length && line[j + 1] === '/') {
          inBlockComment = false;
          j++; // skip the /
        }
        continue;
      }

      // Start of block comment
      if (ch === '/' && j + 1 < line.length && line[j + 1] === '*') {
        inBlockComment = true;
        j++; // skip the *
        continue;
      }

      // Line comment — skip rest of line
      if (ch === '/' && j + 1 < line.length && line[j + 1] === '/') {
        break; // skip to end of line
      }

      // String start — enter string state (may be multi-line for backticks)
      if (ch === "'" || ch === '"' || ch === '`') {
        inString = ch;
        continue;
      }

      if (ch === '{') {
        braceCount++;
        foundFirstBrace = true;
      } else if (ch === '}') {
        braceCount--;
        if (foundFirstBrace && braceCount === 0) {
          return i + 1; // 1-based inclusive end line
        }
      }
    }
    // Note: inString persists to the next line iteration for multi-line strings
  }

  // No matching close brace — scope extends to end of file
  return lines.length;
}

/**
 * Return the symbols (up to 3) that enclose `cursorLine` (1-based).
 *
 * Only container kinds are considered (function, method, class, interface).
 * Symbols are returned sorted by line ascending (outermost → innermost).
 */
export function getEnclosingSymbols(
  content: string,
  languageId: string | undefined,
  cursorLine: number, // 1-based
): SymbolInfo[] {
  if (!content || cursorLine < 1) return [];

  const allSymbols = extractSymbols(content, languageId);
  const lines = content.split('\n');
  const result: SymbolInfo[] = [];

  // Filter to container kinds only and process in line order
  const containers = allSymbols.filter((s) => CONTAINER_KINDS.has(s.kind)).sort((a, b) => a.line - b.line);

  for (const sym of containers) {
    if (sym.line > cursorLine) continue; // symbol starts after cursor

    const endLine = findSymbolScopeEnd(lines, sym.line - 1);
    if (cursorLine <= endLine) {
      result.push(sym);
      if (result.length >= 3) break; // cap at 3 levels deep
    }
  }

  return result;
}

// ── Component ────────────────────────────────────────────────────────────

function GoToSymbolOverlay({
  visible,
  content,
  fileExtension,
  onSelectSymbol,
  onClose,
}: GoToSymbolOverlayProps): JSX.Element | null {
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);

  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const prevVisibleRef = useRef(false);

  // ── Reset state when visibility changes ───────────────────────────────

  useEffect(() => {
    if (visible && !prevVisibleRef.current) {
      setQuery('');
      setSelectedIndex(0);
    }
    prevVisibleRef.current = visible;
  }, [visible]);

  // ── Auto-focus input when overlay opens ───────────────────────────────

  useEffect(() => {
    if (visible) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [visible]);

  // ── Extract symbols (memoised) ────────────────────────────────────────

  const allSymbols = useMemo(() => extractSymbols(content, fileExtension), [content, fileExtension]);

  // ── Filter symbols with fuzzy matching ────────────────────────────────

  const filteredResults = useMemo((): FuzzyResult<SymbolInfo>[] => {
    const trimmed = query.trim();
    if (!trimmed) return [];
    return fuzzyFilter(trimmed, allSymbols, (s) => s.name, MAX_SYMBOLS);
  }, [query, allSymbols]);

  // ── Reset selected index when results change ──────────────────────────

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  // ── Scroll selected item into view ────────────────────────────────────

  useEffect(() => {
    const container = listRef.current;
    if (!container) return;
    const selected = container.querySelector('[data-selected="true"]');
    if (selected) {
      selected.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selectedIndex]);

  // ── Resolve the currently displayed item list and length ──────────────

  const hasQuery = query.trim().length > 0;
  const displayItems: SymbolInfo[] = hasQuery ? filteredResults.map((r) => r.item) : allSymbols;
  const itemCount = displayItems.length;

  // ── Handle keyboard navigation ────────────────────────────────────────

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      e.stopPropagation();

      switch (e.key) {
        case 'Escape':
          e.preventDefault();
          onClose();
          break;

        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((prev) => Math.min(prev + 1, Math.max(itemCount - 1, 0)));
          break;

        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((prev) => Math.max(prev - 1, 0));
          break;

        case 'Enter':
          e.preventDefault();
          if (hasQuery && filteredResults[selectedIndex]) {
            onSelectSymbol(filteredResults[selectedIndex].item.line);
            onClose();
          } else if (!hasQuery && allSymbols[selectedIndex]) {
            onSelectSymbol(allSymbols[selectedIndex].line);
            onClose();
          }
          break;
      }
    },
    [filteredResults, allSymbols, itemCount, selectedIndex, hasQuery, onClose, onSelectSymbol],
  );

  // ── Handle item click ─────────────────────────────────────────────────

  const handleItemClick = useCallback(
    (symbol: SymbolInfo) => {
      onSelectSymbol(symbol.line);
      onClose();
    },
    [onSelectSymbol, onClose],
  );

  // ── Handle mouse enter on item (track selection hover) ────────────────

  const handleItemMouseEnter = useCallback((index: number) => {
    setSelectedIndex(index);
  }, []);

  // ── Stop mousedown from stealing focus ────────────────────────────────

  const handleMouseDown = useCallback((e: MouseEvent) => {
    // Don't let the dropdown steal focus from the input
    e.preventDefault();
  }, []);

  if (!visible) return null;

  const isEmpty = displayItems.length === 0;

  return (
    <div className="goto-symbol-overlay">
      {/* Search input */}
      <div className="goto-symbol-input-wrapper">
        <span className="goto-symbol-prefix">@</span>
        <input
          ref={inputRef}
          type="text"
          className="goto-symbol-input"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Go to Symbol in File"
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
        />
      </div>

      {/* Symbol list */}
      <div className="goto-symbol-list" ref={listRef} onMouseDown={handleMouseDown}>
        {isEmpty && hasQuery && <div className="goto-symbol-empty">No matching symbols</div>}

        {isEmpty && !hasQuery && <div className="goto-symbol-empty">No symbols found</div>}

        {!isEmpty && !hasQuery && (
          <div className="goto-symbol-count">
            {displayItems.length} symbol{displayItems.length !== 1 ? 's' : ''} found
          </div>
        )}

        {displayItems.map((symbol, index) => {
          const matches = hasQuery && filteredResults[index] ? filteredResults[index].matches : [];
          const isActive = index === selectedIndex;
          const icon = KIND_ICONS[symbol.kind] || '?';

          return (
            <div
              key={`${symbol.kind}-${symbol.name}-${symbol.line}`}
              data-selected={isActive}
              className={`goto-symbol-item${isActive ? ' goto-symbol-item-active' : ''}`}
              onClick={() => handleItemClick(symbol)}
              onMouseEnter={() => handleItemMouseEnter(index)}
            >
              <span className={`goto-symbol-kind goto-symbol-kind-${symbol.kind}`}>{icon}</span>
              <span
                className="goto-symbol-name"
                dangerouslySetInnerHTML={{
                  __html: hasQuery ? highlightMatches(symbol.name, matches) : symbol.name,
                }}
              />
              <span className="goto-symbol-line">:{symbol.line}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default GoToSymbolOverlay;
