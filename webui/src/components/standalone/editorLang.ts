/**
 * E-M3 standalone editor language support — the CodeMirror 6 language
 * packages already in the webui's dependency tree, wired by file
 * extension. Standalone counterpart of the app editor's language
 * resolution (useEditorExtensions/buildExtensions languageId path).
 */
import type { Extension } from '@codemirror/state';
import { EditorView, keymap } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { bracketMatching, indentOnInput, syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language';
import { highlightSelectionMatches, search, searchKeymap } from '@codemirror/search';
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from '@codemirror/autocomplete';
import { lintGutter } from '@codemirror/lint';
import {
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
  drawSelection,
  rectangularSelection,
  crosshairCursor,
  dropCursor,
  highlightSpecialChars,
} from '@codemirror/view';

import { python } from '@codemirror/lang-python';
import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { html } from '@codemirror/lang-html';
import { css } from '@codemirror/lang-css';
import { markdown } from '@codemirror/lang-markdown';
import { go } from '@codemirror/lang-go';
import { cpp } from '@codemirror/lang-cpp';
import { rust } from '@codemirror/lang-rust';
import { sql } from '@codemirror/lang-sql';
import { yaml } from '@codemirror/lang-yaml';

/** Map a file path to its language extension. */
export function editorExtensionsFor(path: string): Extension[] {
  const lang = languageExtensionFor(path);
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
    ...(lang ? [lang] : []),
  ];
}

function languageExtensionFor(path: string): Extension | null {
  const ext = path.split('.').pop()?.toLowerCase() ?? '';
  switch (ext) {
    case 'py':
      return python();
    case 'js':
    case 'mjs':
    case 'cjs':
      return javascript();
    case 'ts':
    case 'tsx':
      return javascript({ typescript: true });
    case 'jsx':
      return javascript({ jsx: true });
    case 'json':
    case 'jsonc':
      return json();
    case 'html':
    case 'htm':
      return html();
    case 'css':
      return css();
    case 'md':
    case 'markdown':
      return markdown();
    case 'go':
      return go();
    case 'c':
    case 'h':
    case 'cpp':
    case 'hpp':
    case 'cc':
      return cpp();
    case 'rs':
      return rust();
    case 'sql':
      return sql();
    case 'yml':
    case 'yaml':
      return yaml();
    default:
      return null;
  }
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

// (lintGutter imported for parity with app editor; not in the base set to
// keep the standalone bundle lean — remove the import if linting is added.)
void lintGutter;
