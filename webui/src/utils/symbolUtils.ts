// ── Types ────────────────────────────────────────────────────────────────

export interface SymbolInfo {
  name: string;
  line: number; // 1-based line number
  kind: SymbolKind;
}

export type SymbolKind = 'function' | 'method' | 'class' | 'variable' | 'type' | 'constant' | 'interface';

// ── Constants ────────────────────────────────────────────────────────────

export const MAX_SYMBOLS = 500;

export const KIND_ICONS: Record<SymbolKind, string> = {
  function: 'ƒ',
  method: 'ƒ',
  class: 'C',
  variable: 'V',
  type: 'T',
  constant: 'K',
  interface: 'I',
};

/** Kinds that can act as scope containers for the breadcrumb. */
export const CONTAINER_KINDS: ReadonlySet<SymbolKind> = new Set<SymbolKind>([
  'function',
  'method',
  'class',
  'interface',
]);

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
  // Supports optional generic type params: const foo = <T>(x: T) =>
  [/\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s*)?(?:<[^>]*>)?\s*\(/, 'function'],
  // Arrow function: const foo = () =>
  // Supports optional generic type params: const foo = <T>(x: T) =>
  [
    /\bconst\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s*)?(?:<[^>]*>)?\s*(?:\([^)]*\)|[a-zA-Z_$][a-zA-Z0-9_$]*)\s*=>/,
    'function',
  ],
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

// ── WASM-backed extraction (tree-sitter) ─────────────────────────────────

/** Symbol kinds returned by pkg/ast/symbols.go's guessKind. Anything outside
 *  this set falls back to a regex-defined SymbolKind. */
type WasmSymbol = {
  name?: string;
  kind?: string;
  startLine?: number;
  endLine?: number;
};

/** Map ast.Symbol.Kind values to local SymbolKind. */
const WASM_KIND_MAP: Record<string, SymbolKind> = {
  function: 'function',
  method: 'method',
  class: 'class',
  interface: 'interface',
  type: 'type',
  enum: 'type',
  variable: 'variable',
  constant: 'constant',
  property: 'variable',
  // import/decorator/module/symbol are not surfaced — they aren't useful in
  // the outline/breadcrumb consumers of this function.
};

/** Map a language extension (".ts", ".py", ...) to the language identifier
 *  the Go side reports via SproutWasm.supportedLanguages(). */
function wasmLanguageForExt(ext: string | undefined): string | null {
  switch (ext) {
    case '.go':
      return 'go';
    case '.py':
      return 'python';
    case '.ts':
      return 'typescript';
    case '.tsx':
      return 'tsx';
    case '.js':
    case '.jsx':
    case '.mjs':
      return 'javascript';
    default:
      return null;
  }
}

/** Cache the set of WASM-supported languages between calls (it doesn't
 *  change at runtime). null = not yet probed; Set may be empty if WASM
 *  hasn't loaded yet. */
let wasmLangCache: Set<string> | null = null;

function getWasmSupportedLangs(): Set<string> | null {
  if (wasmLangCache) return wasmLangCache;
  const api = typeof window === 'undefined' ? undefined : window.SproutWasm;
  if (!api || typeof api.supportedLanguages !== 'function') return null;
  try {
    const raw = api.supportedLanguages();
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) {
      wasmLangCache = new Set(parsed.map((s) => String(s).toLowerCase()));
      return wasmLangCache;
    }
  } catch {
    // fall through to null
  }
  return null;
}

/** Memoization key → result. Same content string → same symbols. extractSymbols
 *  is called per-keystroke from sticky-scroll and outline panels, so this avoids
 *  repeatedly going through WASM for unchanged content. */
const symbolCache = new Map<string, SymbolInfo[]>();
const SYMBOL_CACHE_MAX = 32;

function cacheKey(ext: string | undefined, content: string): string {
  // Lightweight FNV-1a 32-bit hash — collisions across different content are
  // extremely unlikely at this size, and we key on (ext, hash) anyway.
  let h = 0x811c9dc5;
  for (let i = 0; i < content.length; i++) {
    h ^= content.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return `${ext ?? ''}:${(h >>> 0).toString(16)}:${content.length}`;
}

function extractSymbolsViaWasm(content: string, language: string): SymbolInfo[] | null {
  const api = window.SproutWasm;
  if (!api || typeof api.extractSymbols !== 'function') return null;
  try {
    // WASM takes Uint8Array of bytes; use TextEncoder to handle UTF-8.
    const bytes = new TextEncoder().encode(content);
    // The Go side reads filePath only for language detection; pass a stub
    // that matches the language we're forcing.
    const path = `_inline.${language === 'tsx' ? 'tsx' : language === 'typescript' ? 'ts' : language === 'javascript' ? 'js' : language === 'python' ? 'py' : language}`;
    const raw = api.extractSymbols(path, bytes);
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return null;
    const out: SymbolInfo[] = [];
    for (const s of parsed as WasmSymbol[]) {
      if (!s || typeof s.name !== 'string' || !s.name) continue;
      const kind = WASM_KIND_MAP[String(s.kind ?? '').toLowerCase()];
      if (!kind) continue;
      const line = typeof s.startLine === 'number' ? s.startLine : 0;
      if (line <= 0) continue;
      out.push({ name: s.name, line, kind });
      if (out.length >= MAX_SYMBOLS) break;
    }
    return out;
  } catch {
    return null;
  }
}

// ── Symbol extraction ────────────────────────────────────────────────────

/**
 * Extract symbols from `content` using the WASM tree-sitter parser when
 * available for the language (go, javascript, python, tsx, typescript),
 * falling back to regex patterns otherwise. Results are memoized by
 * (extension, content hash) since callers re-invoke this per keystroke.
 */
export function extractSymbols(content: string, languageId?: string): SymbolInfo[] {
  const ext = languageId?.toLowerCase();

  // Fast cache check.
  const key = cacheKey(ext, content);
  const cached = symbolCache.get(key);
  if (cached) return cached;

  // Try WASM-backed extraction first when the language is supported.
  const wasmLang = wasmLanguageForExt(ext);
  if (wasmLang) {
    const supported = getWasmSupportedLangs();
    if (supported && supported.has(wasmLang)) {
      const viaWasm = extractSymbolsViaWasm(content, wasmLang);
      if (viaWasm) {
        rememberSymbols(key, viaWasm);
        return viaWasm;
      }
    }
  }

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

    // Strip single-line comments based on language.
    // Handles // style for Go/JS/TS and # style for Python.
    if (ext === '.go' || ext === '.ts' || ext === '.tsx' || ext === '.js' || ext === '.jsx' || ext === '.mjs') {
      const commentIdx = line.indexOf('//');
      if (commentIdx !== -1) line = line.slice(0, commentIdx);
      if (!line.trim()) continue;
    } else if (ext === '.py') {
      const commentIdx = line.indexOf('#');
      if (commentIdx !== -1) {
        // Simple heuristic: only strip # if it's not inside a string
        // Count quotes before # to determine if we're in a string context
        const before = line.slice(0, commentIdx);
        let inSingle = false,
          inDouble = false;
        for (let j = 0; j < before.length; j++) {
          if (before[j] === '"' && !inSingle) inDouble = !inDouble;
          if (before[j] === "'" && !inDouble) inSingle = !inSingle;
        }
        if (!inSingle && !inDouble) {
          line = line.slice(0, commentIdx);
        }
      }
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
        const dedupKey = `${name}:${i + 1}`;
        if (seen.has(dedupKey)) continue;
        seen.set(dedupKey, i + 1);
        symbols.push({ name, line: i + 1, kind });
        break; // only one symbol per line
      }
    }
  }

  rememberSymbols(key, symbols);
  return symbols;
}

function rememberSymbols(key: string, symbols: SymbolInfo[]): void {
  // Bound the cache so it can't grow unboundedly as users open many files.
  if (symbolCache.size >= SYMBOL_CACHE_MAX) {
    const oldestKey = symbolCache.keys().next().value;
    if (oldestKey !== undefined) symbolCache.delete(oldestKey);
  }
  symbolCache.set(key, symbols);
}

// ── Enclosing-symbol detection ───────────────────────────────────────────

/**
 * Find the 1-based end line (inclusive) of a symbol's scope.
 *
 * For brace-based languages (JS/TS/Go), counts balanced braces from the
 * symbol's definition line onward.
 *
 * For Python, uses indentation-based scope detection.
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
export function findSymbolScopeEnd(lines: string[], startLineIndex: number, languageId?: string): number {
  if (languageId === '.py') {
    return findPythonScopeEnd(lines, startLineIndex);
  }
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
 * Find Python scope end using indentation levels.
 * The symbol's scope ends when a line at a lesser or equal indentation level
 * is encountered after the indented body.
 */
function findPythonScopeEnd(lines: string[], startLineIndex: number): number {
  const declLine = lines[startLineIndex] || '';
  const declIndent = declLine.search(/\S/);
  if (declIndent === -1) return lines.length; // blank line, no scope

  // Find what indentation the body uses
  let bodyStarted = false;
  let bodyIndent = -1;

  for (let i = startLineIndex + 1; i < lines.length; i++) {
    const line = lines[i];

    // Skip blank lines and comment-only lines
    if (!line.trim() || line.trimStart().startsWith('#')) continue;

    const lineIndent = line.search(/\S/);

    if (!bodyStarted) {
      if (lineIndent > declIndent) {
        bodyStarted = true;
        bodyIndent = lineIndent;
      } else {
        // First non-blank line isn't indented past declaration → empty body
        return i;
      }
      continue;
    }

    // Lines more indented than the body are nested (still in scope)
    if (lineIndent > bodyIndent) continue;

    // Lines at the body indentation level are still part of the scope
    if (lineIndent === bodyIndent) continue;

    // Line at or less than declaration indentation → scope ends
    if (lineIndent <= declIndent) {
      return i;
    }
  }

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

    const endLine = findSymbolScopeEnd(lines, sym.line - 1, languageId);
    if (cursorLine <= endLine) {
      result.push(sym);
      if (result.length >= 3) break; // cap at 3 levels deep
    }
  }

  return result;
}

/**
 * Get the scope path string for a symbol at the given line.
 * Returns the names of enclosing container symbols (outermost → innermost),
 * joined with the › separator. Returns empty string if no containers.
 */
export function getScopePath(
  content: string,
  languageId: string | undefined,
  symbolLine: number,
  symbolName: string,
): string {
  const enclosing = getEnclosingSymbols(content, languageId, symbolLine);
  // Filter out the symbol itself (getEnclosingSymbols may return it)
  const containers = enclosing.filter((s) => !(s.name === symbolName && s.line === symbolLine));
  return containers.map((s) => s.name).join(' › ');
}

/**
 * Build a Map of symbolLine → scopePath string for a set of symbols.
 *
 * Single-pass optimization: splits content once, computes container endLines
 * using `findSymbolScopeEnd` once per container, then builds the path for
 * each symbol from the precomputed containers that enclose it — without
 * calling `getScopePath` per symbol (which redundantly re-parses and
 * re-splits content each time).
 */
export function buildScopePaths(
  content: string,
  languageId: string | undefined,
  symbols: SymbolInfo[],
): Map<number, string> {
  const map = new Map<number, string>();
  if (!content || symbols.length === 0) return map;

  const lines = content.split('\n');

  // Compute enclosing containers for each symbol in a single pass over containers.
  // For each symbol, we need the container names that enclose it (excluding itself).

  // Gather all container-kind symbols, sorted by line (outermost first).
  const containers = symbols.filter((s) => CONTAINER_KINDS.has(s.kind)).sort((a, b) => a.line - b.line);

  // Precompute endLine for each container (one findSymbolScopeEnd call per container).
  const containerRanges: Array<{ sym: SymbolInfo; endLine: number }> = containers.map((sym) => ({
    sym,
    endLine: findSymbolScopeEnd(lines, sym.line - 1, languageId),
  }));

  for (const sym of symbols) {
    // Collect enclosing containers (those that start before/at sym.line and
    // whose scope extends past sym.line), excluding the symbol itself.
    const enclosing: SymbolInfo[] = [];
    for (const { sym: container, endLine } of containerRanges) {
      if (container.line > sym.line) break; // sorted — no more can enclose
      if (sym.name === container.name && sym.line === container.line) continue;
      if (sym.line <= endLine) {
        enclosing.push(container);
        if (enclosing.length >= 3) break; // cap at 3
      }
    }
    if (enclosing.length > 0) {
      map.set(sym.line, enclosing.map((s) => s.name).join(' › '));
    }
  }

  return map;
}
