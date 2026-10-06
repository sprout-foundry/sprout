/**
 * standalone editor language support — CodeMirror 6 language packages,
 * loaded on demand.
 *
 * Language packages are code-split: `languageExtensionFor` is async and
 * dynamic-imports only the package the opened file needs. The synchronous
 * base extensions (gutters, history, search, bracket matching) load with
 * the page; the ~270KB of language grammars load per language, first use.
 * Unknown/plain-text files never fetch any grammar.
 *
 * `editorExtensionsFor` stays synchronous (the editor builds immediately);
 * `loadLanguageExtension` resolves and the caller swaps the language
 * compartment when it lands.
 */
import type { Extension } from '@codemirror/state';
import { Compartment } from '@codemirror/state';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { bracketMatching, indentOnInput, syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language';
import { highlightSelectionMatches, search, searchKeymap } from '@codemirror/search';
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from '@codemirror/autocomplete';
import { keymap } from '@codemirror/view';
import {
  EditorView,
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
  drawSelection,
  rectangularSelection,
  crosshairCursor,
  dropCursor,
  highlightSpecialChars,
} from '@codemirror/view';

export { EditorView };

/** The language compartment: the one slice of the config that swaps async. */
export const languageCompartment = new Compartment();

/** The synchronous base: everything except the language grammar. */
export function editorExtensionsFor(path: string): Extension[] {
  return [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    drawSelection(),
    dropCursor(),
    rectangularSelection(),
    crosshairCursor(),
    highlightActiveLine(),
    closeBrackets(),
    autocompletion(),
    bracketMatching(),
    indentOnInput(),
    syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
    highlightSelectionMatches(),
    search({ top: true }),
    keymap.of([
      ...closeBracketsKeymap,
      ...defaultKeymap,
      ...searchKeymap,
      ...historyKeymap,
      ...completionKeymap,
      indentWithTab,
    ]),
    EditorView.theme({
      '&': { backgroundColor: 'transparent' },
      '.cm-content': { caretColor: 'var(--editor-fg, #d4d4d4)' },
    }),
    // Placeholder compartment — swapped for the real grammar when
    // loadLanguageExtension resolves. Empty until then (plain-textColors).
    languageCompartment.of([]),
  ];
}

type LanguageLoader = () => Promise<{ [k: string]: Extension }>;

/** ext → dynamic importer. The default export name varies per package. */
const LANGUAGE_LOADERS: Record<string, LanguageLoader> = {
  py: () => import('@codemirror/lang-python').then((m) => ({ python: m.python() })),
  js: () => import('@codemirror/lang-javascript').then((m) => ({ javascript: m.javascript() })),
  mjs: () => import('@codemirror/lang-javascript').then((m) => ({ javascript: m.javascript() })),
  cjs: () => import('@codemirror/lang-javascript').then((m) => ({ javascript: m.javascript() })),
  ts: () => import('@codemirror/lang-javascript').then((m) => ({ javascript: m.javascript({ typescript: true }) })),
  tsx: () => import('@codemirror/lang-javascript').then((m) => ({ javascript: m.javascript({ typescript: true }) })),
  jsx: () => import('@codemirror/lang-javascript').then((m) => ({ javascript: m.javascript({ jsx: true }) })),
  json: () => import('@codemirror/lang-json').then((m) => ({ json: m.json() })),
  jsonc: () => import('@codemirror/lang-json').then((m) => ({ json: m.json() })),
  html: () => import('@codemirror/lang-html').then((m) => ({ html: m.html() })),
  htm: () => import('@codemirror/lang-html').then((m) => ({ html: m.html() })),
  css: () => import('@codemirror/lang-css').then((m) => ({ css: m.css() })),
  md: () => import('@codemirror/lang-markdown').then((m) => ({ markdown: m.markdown() })),
  markdown: () => import('@codemirror/lang-markdown').then((m) => ({ markdown: m.markdown() })),
  go: () => import('@codemirror/lang-go').then((m) => ({ go: m.go() })),
  c: () => import('@codemirror/lang-cpp').then((m) => ({ cpp: m.cpp() })),
  h: () => import('@codemirror/lang-cpp').then((m) => ({ cpp: m.cpp() })),
  cpp: () => import('@codemirror/lang-cpp').then((m) => ({ cpp: m.cpp() })),
  hpp: () => import('@codemirror/lang-cpp').then((m) => ({ cpp: m.cpp() })),
  cc: () => import('@codemirror/lang-cpp').then((m) => ({ cpp: m.cpp() })),
  rs: () => import('@codemirror/lang-rust').then((m) => ({ rust: m.rust() })),
  sql: () => import('@codemirror/lang-sql').then((m) => ({ sql: m.sql() })),
  yml: () => import('@codemirror/lang-yaml').then((m) => ({ yaml: m.yaml() })),
  yaml: () => import('@codemirror/lang-yaml').then((m) => ({ yaml: m.yaml() })),
};

/**
 * Load the grammar for `path`. Resolves null for plain-text extensions —
 * the caller leaves the compartment empty.
 */
export async function loadLanguageExtension(path: string): Promise<Extension | null> {
  const ext = path.split('.').pop()?.toLowerCase() ?? '';
  const loader = LANGUAGE_LOADERS[ext];
  if (!loader) return null;
  const bag = await loader();
  const first = Object.values(bag)[0];
  return first ?? null;
}

/** Human label for the status bar. */
export function languageTitleFor(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? '';
  const names: Record<string, string> = {
    py: 'Python',
    js: 'JavaScript',
    mjs: 'JavaScript',
    cjs: 'JavaScript',
    ts: 'TypeScript',
    tsx: 'TypeScript React',
    jsx: 'JavaScript React',
    json: 'JSON',
    html: 'HTML',
    htm: 'HTML',
    css: 'CSS',
    md: 'Markdown',
    go: 'Go',
    c: 'C',
    h: 'C Header',
    cpp: 'C++',
    hpp: 'C++ Header',
    cc: 'C++',
    rs: 'Rust',
    sql: 'SQL',
    yml: 'YAML',
    yaml: 'YAML',
  };
  return names[ext] ?? (ext.toUpperCase() || 'Plain Text');
}
