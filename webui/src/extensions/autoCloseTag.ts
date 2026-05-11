/**
 * autoCloseTag.ts — Auto-close HTML/XML/JSX tags in CodeMirror 6.
 *
 * When the user types `>`, automatically inserts the closing tag
 * for HTML, XML, and JSX elements (e.g., `<div>` becomes `<div>\n  \n</div>`).
 *
 * Only active for HTML, XML, JSX, TSX, and PHP language modes.
 */

import { type Extension, Compartment } from '@codemirror/state';
import { EditorView, type EditorView as EditorViewType } from '@codemirror/view';
import { indentUnit as indentUnitFacet } from '@codemirror/language';
import { getLineIndent } from '../utils/editorHotkeys';

/**
 * Language IDs for which auto-close tag should be active.
 */
const AUTO_CLOSE_TAG_LANGUAGE_IDS = new Set([
  'html',
  'xml',
  'javascript-jsx',
  'typescript-jsx',
  'php', // PHP files often contain HTML
]);

/**
 * Void elements that should NOT be auto-closed.
 * These are self-closing in HTML5.
 */
const VOID_ELEMENTS = new Set([
  'area',
  'base',
  'br',
  'col',
  'embed',
  'hr',
  'img',
  'input',
  'link',
  'meta',
  'param',
  'source',
  'track',
  'wbr',
]);

/**
 * Maximum number of characters to scan backward when looking for an opening `<`.
 */
const MAX_TAG_SCAN_DISTANCE = 500;

/**
 * Check whether a language ID supports auto-close tag.
 */
export function isAutoCloseTagLanguage(languageId: string | null | undefined): boolean {
  if (!languageId) {
    return false;
  }
  return AUTO_CLOSE_TAG_LANGUAGE_IDS.has(languageId);
}

/**
 * Extract the tag name from text before the cursor.
 * Scans backwards from cursor position to find `<tagname`.
 *
 * @param text - The document text before cursor position
 * @param cursorPos - Cursor position in the document
 * @returns The tag name (without `<`) or null if not a valid opening tag
 * @internal Exported for testing only
 */
export function extractTagName(docText: string, cursorPos: number): string | null {
  if (cursorPos < 1) {
    return null;
  }

  // The '>' was just typed, so it's at cursorPos - 1
  const charBefore = docText[cursorPos - 1];
  if (charBefore !== '>') {
    return null;
  }

  // Scan backwards to find '<', tracking quote context to avoid
  // matching inside string literals (e.g., `const x = "<div>"`).
  let startPos = cursorPos - 2;
  let inSingleQuote = false;
  let inDoubleQuote = false;
  let braceDepth = 0;
  while (startPos >= 0) {
    const ch = docText[startPos];
    // Track quote context (single and double quotes skip each other)
    if (ch === '"' && !inSingleQuote) {
      inDoubleQuote = !inDoubleQuote;
    } else if (ch === "'" && !inDoubleQuote) {
      inSingleQuote = !inSingleQuote;
    }
    // Track brace depth to detect unclosed JSX expressions (e.g., `<div data-x={5 >`)
    // When scanning backwards: } increments, { decrements
    if (ch === '}' && !inDoubleQuote && !inSingleQuote) {
      braceDepth++;
    }
    if (ch === '{' && !inDoubleQuote && !inSingleQuote) {
      braceDepth--;
    }
    if (ch === '<' && !inDoubleQuote && !inSingleQuote) {
      break;
    }
    // If we hit a newline before finding '<', this isn't a tag
    if (ch === '\n' || ch === '\r') {
      return null;
    }
    startPos--;
  }

  // If inside a quote or didn't find '<', skip
  if (inDoubleQuote || inSingleQuote || startPos < 0) {
    return null;
  }

  // If there are unclosed braces, the '>' belongs to a JSX expression, not a tag
  if (braceDepth !== 0) {
    return null;
  }

  // The character after '<' is the start of tag name
  const tagStart = startPos + 1;
  if (tagStart >= cursorPos - 1) {
    return null;
  }

  // Closing tags (</tag>) should not trigger auto-close
  if (docText[tagStart] === '/') {
    return null;
  }

  // Extract the tag name (characters between '<' and '>')
  const tagText = docText.slice(tagStart, cursorPos - 1).trim();

  // Skip empty tags
  if (tagText.length === 0) {
    return null;
  }

  // Skip comments: <!--
  if (tagText.startsWith('!--')) {
    return null;
  }

  // Skip doctype: <!DOCTYPE or <!doctype
  if (tagText.startsWith('!')) {
    return null;
  }

  // Skip processing instructions: <?xml
  if (tagText.startsWith('?')) {
    return null;
  }

  // Skip self-closing tags: tag/>
  // Check if there's a '/' before the '>'
  if (tagText.endsWith('/')) {
    return null;
  }

  // Check for attributes - find the first whitespace to get just the tag name
  const firstSpace = tagText.search(/\s/);
  let tagName = firstSpace >= 0 ? tagText.slice(0, firstSpace) : tagText;

  // Remove any leading '/' (in case of malformed self-closing)
  if (tagName.startsWith('/')) {
    tagName = tagName.slice(1);
  }

  // Skip empty after processing
  if (tagName.length === 0) {
    return null;
  }

  // Guard against spurious '>' inside the tag name (e.g., double-typed '>>')
  if (tagName.includes('>')) {
    return null;
  }

  // Check for void elements (case-insensitive)
  if (VOID_ELEMENTS.has(tagName.toLowerCase())) {
    return null;
  }

  // Tag names must start with a letter or underscore (allows <my-component>, <ns:tag>, <_custom>)
  if (!/^[a-zA-Z_]/.test(tagName)) {
    return null;
  }

  return tagName;
}

/**
 * Get the indent unit string from the editor view's state.
 * Falls back to 2 spaces if the facet is not configured.
 */
function getIndentUnit(view: EditorViewType): string {
  try {
    return view.state.facet(indentUnitFacet);
  } catch {
    // Facet not configured; fall back to 2-space indent
    return '  ';
  }
}

/**
 * Handle auto-close tag logic after '>' was typed.
 * Uses an update listener so the '>' character is already inserted by default CM behavior.
 */
function maybeAutoCloseTag(view: EditorViewType): void {
  const pos = view.state.selection.main.head;
  const doc = view.state.doc;

  // Need at least 2 characters: '<' and '>'
  if (pos < 2) {
    return;
  }

  // The '>' was just inserted, so pos is right after it
  // Look at the character before the cursor
  const charBefore = doc.sliceString(pos - 1, pos);
  if (charBefore !== '>') {
    return;
  }

  // Get text before the cursor position to analyze
  // Look at last MAX_TAG_SCAN_DISTANCE characters to find relevant tag context
  const startScan = Math.max(0, pos - MAX_TAG_SCAN_DISTANCE);
  const textBefore = doc.sliceString(startScan, pos);

  // Extract the tag name from what we just typed (the cursor is right after the '>')
  const tagName = extractTagName(textBefore, pos - startScan);

  if (!tagName) {
    return;
  }

  // Get the current line to determine indentation
  const line = doc.lineAt(pos);
  const lineText = line.text;
  const currentIndent = getLineIndent(lineText);
  const indentUnit = getIndentUnit(view);
  const newIndent = currentIndent + indentUnit;

  // Build the insertion: newline, indented blank line, then closing tag
  const closingTag = `\n${newIndent}\n${currentIndent}</${tagName}>`;

  // Insert the closing tag after the '>'
  // The cursor should land on the indented blank line (between the tags)
  view.dispatch({
    changes: {
      from: pos,
      insert: closingTag,
    },
    selection: {
      anchor: pos + 1 + newIndent.length,
    },
  });
}

/**
 * Build the auto-close tag Extension[] for a given language ID.
 * Returns an empty array when auto-close tag should not be active.
 */
export function buildAutoCloseTagExtensions(languageId: string | null | undefined): Extension[] {
  if (!isAutoCloseTagLanguage(languageId)) {
    return [];
  }

  try {
    // Use updateListener to detect when '>' is typed via user input
    return [
      EditorView.updateListener.of((update) => {
        // Only trigger on document changes from user typing
        if (!update.docChanged) {
          return;
        }
        // Check if this is a user input type event
        const isUserInput = update.transactions.some((tr) => tr.isUserEvent('input.type'));
        if (!isUserInput) {
          return;
        }
        maybeAutoCloseTag(update.view);
      }),
    ];
  } catch (err) {
    console.error('[autoCloseTag] Failed to build extensions:', err);
    return [];
  }
}

/**
 * Create a Compartment for auto-close tag extensions.
 * Use this to reconfigure when the language changes.
 */
export function createAutoCloseTagCompartment(): Compartment {
  return new Compartment();
}

/**
 * Get initial auto-close tag extensions for a Compartment (empty array = disabled).
 */
export function getInitialAutoCloseTagExtensions(languageId: string | null | undefined): Extension[] {
  return buildAutoCloseTagExtensions(languageId);
}

/**
 * Reconfigure the auto-close tag compartment on the given view for a new language.
 */
export function reconfigureAutoCloseTag(
  compartment: Compartment,
  view: EditorView,
  languageId: string | null | undefined,
): void {
  try {
    view.dispatch({
      effects: compartment.reconfigure(buildAutoCloseTagExtensions(languageId)),
    });
  } catch (err) {
    // Graceful degradation — auto-close tag reconfiguration must not crash the editor.
    console.error('[autoCloseTag] Failed to reconfigure compartment:', err);
  }
}
