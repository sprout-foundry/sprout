/**
 * Language registry for the editor.
 *
 * Provides a central list of supported languages, each with a unique id,
 * display name, and associated file extensions/filenames.  Also exposes
 * helpers for auto-detection and CodeMirror extension creation.
 */

import { Extension } from '@codemirror/state';

// Language support — official @codemirror/lang-* packages
import { javascript } from '@codemirror/lang-javascript';
import { python } from '@codemirror/lang-python';
import { go } from '@codemirror/lang-go';
import { json } from '@codemirror/lang-json';
import { html } from '@codemirror/lang-html';
import { css } from '@codemirror/lang-css';
import { markdown } from '@codemirror/lang-markdown';
import { php } from '@codemirror/lang-php';
import { wast } from '@codemirror/lang-wast';
import { rust } from '@codemirror/lang-rust';
import { cpp } from '@codemirror/lang-cpp';
import { java } from '@codemirror/lang-java';
import { yaml } from '@codemirror/lang-yaml';
import { xml } from '@codemirror/lang-xml';
import { sql } from '@codemirror/lang-sql';
import { ruby } from 'codemirror-lang-ruby';

// Legacy modes
import { StreamLanguage } from '@codemirror/language';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { toml } from '@codemirror/legacy-modes/mode/toml';
import { dockerFile } from '@codemirror/legacy-modes/mode/dockerfile';
import { clike } from '@codemirror/legacy-modes/mode/clike';
import { clojure } from '@codemirror/legacy-modes/mode/clojure';
import { coffeeScript } from '@codemirror/legacy-modes/mode/coffeescript';
import { diff } from '@codemirror/legacy-modes/mode/diff';
import { elm } from '@codemirror/legacy-modes/mode/elm';
import { erlang } from '@codemirror/legacy-modes/mode/erlang';
import { fortran } from '@codemirror/legacy-modes/mode/fortran';
import { groovy } from '@codemirror/legacy-modes/mode/groovy';
import { haskell } from '@codemirror/legacy-modes/mode/haskell';
import { julia } from '@codemirror/legacy-modes/mode/julia';
import { lua } from '@codemirror/legacy-modes/mode/lua';
import { oCaml, fSharp } from '@codemirror/legacy-modes/mode/mllike';
import { nginx } from '@codemirror/legacy-modes/mode/nginx';
import { perl } from '@codemirror/legacy-modes/mode/perl';
import { powerShell } from '@codemirror/legacy-modes/mode/powershell';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { protobuf } from '@codemirror/legacy-modes/mode/protobuf';
import { r } from '@codemirror/legacy-modes/mode/r';
import { sass } from '@codemirror/legacy-modes/mode/sass';
import { scheme } from '@codemirror/legacy-modes/mode/scheme';
import { swift } from '@codemirror/legacy-modes/mode/swift';
import { tcl } from '@codemirror/legacy-modes/mode/tcl';
import { vb } from '@codemirror/legacy-modes/mode/vb';
import { verilog } from '@codemirror/legacy-modes/mode/verilog';
import { vhdl } from '@codemirror/legacy-modes/mode/vhdl';
import { cmake } from '@codemirror/legacy-modes/mode/cmake';
import { crystal } from '@codemirror/legacy-modes/mode/crystal';
import { d } from '@codemirror/legacy-modes/mode/d';
import { gas } from '@codemirror/legacy-modes/mode/gas';
import { textile } from '@codemirror/legacy-modes/mode/textile';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface LanguageEntry {
  id: string;           // Unique identifier (e.g. 'javascript')
  name: string;         // Display name (e.g. 'JavaScript')
  extensions: string[]; // File extensions this language handles (without leading dot)
  filenames?: string[]; // Special filenames (lowercase, e.g. ['dockerfile', 'gemfile'])
}

// ---------------------------------------------------------------------------
// Language list
// ---------------------------------------------------------------------------

const RUBY_FILENAMES = [
  'gemfile', 'rakefile', '.pryrc', '.irbrc', 'guardfile',
  'capfile', 'berksfile', 'thorfile', 'vagrantfile', 'config.ru',
];

export const allLanguageEntries: LanguageEntry[] = [
  { id: 'javascript', name: 'JavaScript', extensions: ['js', 'mjs', 'cjs'] },
  { id: 'javascript-jsx', name: 'JavaScript (JSX)', extensions: ['jsx'] },
  { id: 'typescript', name: 'TypeScript', extensions: ['ts'] },
  { id: 'typescript-jsx', name: 'TypeScript (JSX)', extensions: ['tsx'] },
  { id: 'python', name: 'Python', extensions: ['py'] },
  { id: 'go', name: 'Go', extensions: ['go'] },
  { id: 'html', name: 'HTML', extensions: ['html', 'htm', 'svg'] },
  { id: 'css', name: 'CSS', extensions: ['css'] },
  { id: 'json', name: 'JSON', extensions: ['json'] },
  { id: 'markdown', name: 'Markdown', extensions: ['md', 'markdown'] },
  { id: 'php', name: 'PHP', extensions: ['php'] },
  { id: 'rust', name: 'Rust', extensions: ['rs'] },
  { id: 'c', name: 'C', extensions: ['c', 'h'] },
  { id: 'cpp', name: 'C++', extensions: ['cpp', 'cc', 'cxx', 'hpp', 'hxx', 'hh'] },
  { id: 'java', name: 'Java', extensions: ['java'] },
  { id: 'ruby', name: 'Ruby', extensions: ['rb', 'erb'], filenames: RUBY_FILENAMES },
  { id: 'shell', name: 'Shell', extensions: ['sh', 'bash', 'zsh', 'fish'] },
  { id: 'dockerfile', name: 'Dockerfile', extensions: ['dockerfile'], filenames: ['dockerfile'] },
  { id: 'yaml', name: 'YAML', extensions: ['yaml', 'yml'] },
  { id: 'toml', name: 'TOML', extensions: ['toml'] },
  { id: 'xml', name: 'XML', extensions: ['xml', 'xsl', 'xslt', 'xsd'] },
  { id: 'sql', name: 'SQL', extensions: ['sql'] },
  { id: 'wast', name: 'WebAssembly (WAT)', extensions: ['wat', 'wast'] },
  { id: 'csharp', name: 'C#', extensions: ['cs'] },
  { id: 'scala', name: 'Scala', extensions: ['scala'] },
  { id: 'kotlin', name: 'Kotlin', extensions: ['kt', 'kts'] },
  { id: 'dart', name: 'Dart', extensions: ['dart'] },
  { id: 'clojure', name: 'Clojure', extensions: ['clj', 'cljs', 'cljc', 'edn'] },
  { id: 'haskell', name: 'Haskell', extensions: ['hs'] },
  { id: 'elm', name: 'Elm', extensions: ['elm'] },
  { id: 'erlang', name: 'Erlang', extensions: ['erl', 'hrl'] },
  { id: 'ocaml', name: 'OCaml', extensions: ['ml', 'mli'] },
  { id: 'fsharp', name: 'F#', extensions: ['fs', 'fsi', 'fsx'] },
  { id: 'scheme', name: 'Scheme', extensions: ['scm', 'rkt'] },
  { id: 'lua', name: 'Lua', extensions: ['lua'] },
  { id: 'swift', name: 'Swift', extensions: ['swift'] },
  { id: 'coffeescript', name: 'CoffeeScript', extensions: ['coffee'] },
  { id: 'crystal', name: 'Crystal', extensions: ['cr'] },
  { id: 'sass', name: 'Sass/SCSS', extensions: ['sass', 'scss'] },
  { id: 'textile', name: 'Textile', extensions: ['textile'] },
  { id: 'cmake', name: 'CMake', extensions: ['cmake'] },
  // NOTE: 'conf' is intentionally NOT in extensions — not all .conf files are Nginx.
  // Nginx detection for .conf files is handled by a filename heuristic in detectLanguage().
  { id: 'nginx', name: 'Nginx', extensions: [] },
  { id: 'powershell', name: 'PowerShell', extensions: ['ps1', 'psm1', 'psd1'] },
  { id: 'protobuf', name: 'Protocol Buffers', extensions: ['proto'] },
  { id: 'r', name: 'R', extensions: ['r'] },
  { id: 'julia', name: 'Julia', extensions: ['jl'] },
  { id: 'fortran', name: 'Fortran', extensions: ['f', 'f90', 'f95', 'f03', 'f08', 'for'] },
  { id: 'd', name: 'D', extensions: ['d'] },
  { id: 'verilog', name: 'Verilog', extensions: ['v'] },
  { id: 'vhdl', name: 'VHDL', extensions: ['vh', 'vhd', 'vhdl'] },
  { id: 'groovy', name: 'Groovy', extensions: ['groovy', 'gradle'] },
  { id: 'perl', name: 'Perl', extensions: ['pl', 'pm'] },
  { id: 'tcl', name: 'Tcl', extensions: ['tcl'] },
  { id: 'vb', name: 'Visual Basic', extensions: ['vb', 'vbs'] },
  { id: 'properties', name: 'Java Properties', extensions: ['properties'] },
  { id: 'gas', name: 'Assembly (GAS)', extensions: ['s', 'asm'] },
  { id: 'diff', name: 'Diff', extensions: ['diff', 'patch'] },
  { id: 'plaintext', name: 'Plain Text', extensions: ['log', 'txt'] },
];

// ---------------------------------------------------------------------------
// Extension maps — language id → CodeMirror Extension[]
// ---------------------------------------------------------------------------

const extensionMap = new Map<string, () => Extension[]>([
  ['javascript',       () => [javascript()]],
  ['javascript-jsx',   () => [javascript({ jsx: true })]],
  ['typescript',       () => [javascript({ typescript: true })]],
  ['typescript-jsx',   () => [javascript({ typescript: true, jsx: true })]],
  ['python',           () => [python()]],
  ['go',               () => [go()]],
  ['html',             () => [html()]],
  ['css',              () => [css()]],
  ['json',             () => [json()]],
  ['markdown',         () => [markdown()]],
  ['php',              () => [php()]],
  ['rust',             () => [rust()]],
  ['c',                () => [cpp()]],
  ['cpp',              () => [cpp()]],
  ['java',             () => [java()]],
  ['ruby',             () => [ruby()]],
  ['shell',            () => [StreamLanguage.define(shell)]],
  ['dockerfile',       () => [StreamLanguage.define(dockerFile)]],
  ['yaml',             () => [yaml()]],
  ['toml',             () => [StreamLanguage.define(toml)]],
  ['xml',              () => [xml()]],
  ['sql',              () => [sql()]],
  ['wast',             () => [wast()]],
  ['csharp',           () => [StreamLanguage.define(clike({ name: 'csharp' } as any))]],
  ['scala',            () => [StreamLanguage.define(clike({ name: 'scala' } as any))]],
  ['kotlin',           () => [StreamLanguage.define(clike({ name: 'kotlin' } as any))]],
  ['dart',             () => [StreamLanguage.define(clike({ name: 'dart' } as any))]],
  ['clojure',          () => [StreamLanguage.define(clojure)]],
  ['haskell',          () => [StreamLanguage.define(haskell)]],
  ['elm',              () => [StreamLanguage.define(elm)]],
  ['erlang',           () => [StreamLanguage.define(erlang)]],
  ['ocaml',            () => [StreamLanguage.define(oCaml)]],
  ['fsharp',           () => [StreamLanguage.define(fSharp)]],
  ['scheme',           () => [StreamLanguage.define(scheme)]],
  ['lua',              () => [StreamLanguage.define(lua)]],
  ['swift',            () => [StreamLanguage.define(swift)]],
  ['coffeescript',     () => [StreamLanguage.define(coffeeScript)]],
  ['crystal',          () => [StreamLanguage.define(crystal)]],
  ['sass',             () => [StreamLanguage.define(sass)]],
  ['textile',          () => [StreamLanguage.define(textile)]],
  ['cmake',            () => [StreamLanguage.define(cmake)]],
  ['nginx',            () => [StreamLanguage.define(nginx)]],
  ['powershell',       () => [StreamLanguage.define(powerShell)]],
  ['protobuf',         () => [StreamLanguage.define(protobuf)]],
  ['r',                () => [StreamLanguage.define(r)]],
  ['julia',            () => [StreamLanguage.define(julia)]],
  ['fortran',          () => [StreamLanguage.define(fortran)]],
  ['d',                () => [StreamLanguage.define(d)]],
  ['verilog',          () => [StreamLanguage.define(verilog)]],
  ['vhdl',             () => [StreamLanguage.define(vhdl)]],
  ['groovy',           () => [StreamLanguage.define(groovy)]],
  ['perl',             () => [StreamLanguage.define(perl)]],
  ['tcl',              () => [StreamLanguage.define(tcl)]],
  ['vb',               () => [StreamLanguage.define(vb)]],
  ['properties',       () => [StreamLanguage.define(properties)]],
  ['gas',              () => [StreamLanguage.define(gas)]],
  ['diff',             () => [StreamLanguage.define(diff)]],
  ['plaintext',        () => []],
]);

// ---------------------------------------------------------------------------
// Extension → language id lookup  (used for auto-detection)
// ---------------------------------------------------------------------------

const extensionToLanguageId = new Map<string, string>();
for (const entry of allLanguageEntries) {
  for (const ext of entry.extensions) {
    extensionToLanguageId.set(ext, entry.id);
  }
}

// ---------------------------------------------------------------------------
// Filename → language id lookup
// ---------------------------------------------------------------------------

const filenameToLanguageId = new Map<string, string>();
for (const entry of allLanguageEntries) {
  if (entry.filenames) {
    for (const fname of entry.filenames) {
      filenameToLanguageId.set(fname, entry.id);
    }
  }
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Get CodeMirror extensions for a given language id.
 * Returns the language support extensions (syntax highlighting, etc.)
 * These should be placed in a Compartment for hot-swapping.
 */
export function getLanguageExtensions(languageId: string | null | undefined): Extension[] {
  if (!languageId) return [];
  const factory = extensionMap.get(languageId);
  if (!factory) return [];
  return factory();
}

/**
 * Look up which language a LanguageEntry serves for a given extension (without leading dot).
 * Returns null if no language matches.
 */
function lookupByExtension(ext: string): string | null {
  return extensionToLanguageId.get(ext.toLowerCase()) ?? null;
}

/**
 * Look up which language a LanguageEntry serves for a given filename.
 * Only matches on the exact filename patterns registered in `filenames`.
 */
function lookupByFilename(fileName: string): string | null {
  const lower = fileName.toLowerCase();
  // Exact match
  const exact = filenameToLanguageId.get(lower);
  if (exact) return exact;

  // Dockerfile.dev, Dockerfile.prod etc.
  const base = lower.replace(/\.[^.]+$/, '');
  if (base === 'dockerfile') return 'dockerfile';

  return null;
}

/**
 * Auto-detect language from file extension and/or filename.
 * This mirrors the previous logic that lived in `getLanguageSupport()`.
 *
 * @param ext   File extension *without* leading dot (e.g. "ts"), or undefined
 * @param fileName Full file name (e.g. "Dockerfile.prod")
 */
export function detectLanguage(ext: string | undefined, fileName?: string): string | null {
  if (!ext && fileName) {
    return lookupByFilename(fileName);
  }
  if (!ext) return null;

  // First try extension-based lookup
  const byExt = lookupByExtension(ext);
  if (byExt) return byExt;

  // Heuristics for ambiguous extensions:
  // .conf files are only treated as Nginx if the filename suggests it
  if (ext.toLowerCase() === 'conf' && fileName && /nginx/i.test(fileName)) {
    return 'nginx';
  }

  // Dockerfile variants that have non-standard extensions like .dev, .prod
  if (fileName && /^dockerfile$/i.test((fileName).replace(/\.[^.]+$/, ''))) {
    return 'dockerfile';
  }

  return null;
}

/**
 * Convenience: resolve the effective language id for a buffer.
 * If an override is set, use it; otherwise auto-detect.
 */
export function resolveLanguageId(
  languageOverride: string | null | undefined,
  ext?: string,
  fileName?: string,
): { languageId: string | null; isAutoDetected: boolean } {
  if (languageOverride != null && languageOverride !== '') {
    return { languageId: languageOverride, isAutoDetected: false };
  }
  const detected = detectLanguage(ext, fileName);
  return { languageId: detected, isAutoDetected: detected !== null };
}
