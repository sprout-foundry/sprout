/**
 * Hover tooltip extension for CodeMirror.
 *
 * Provides type-signature and documentation tooltips when hovering over
 * tokens in TypeScript, JavaScript, and Go files. Uses the semantic API
 * (POST /api/semantic with method "hover") backed by gopls or the
 * TypeScript language service.
 *
 * For non-LSP languages the extension is a no-op (returns null, no tooltip).
 */

import { hoverTooltip, type HoverTooltipSource, type EditorView } from '@codemirror/view';
import { ApiService } from '../services/api';
import { resolveLanguageId } from './languageRegistry';
import { debugLog } from '../utils/log';

/** Semantic language IDs that support hover. */
const HOVER_LANGUAGES = new Set([
  'typescript', 'typescript-jsx', 'javascript', 'javascript-jsx', 'go',
]);

/**
 * Build a CodeMirror extension that shows hover tooltips via the semantic API.
 *
 * @param getFilePath - returns the current file path (for API calls)
 * @param getContent - returns the current document content (for API calls)
 */
export function createHoverTooltipExtension(
  getFilePath: () => string | undefined,
  getContent: () => string,
) {
  const api = ApiService.getInstance();

  const source: HoverTooltipSource = async (view: EditorView, pos: number) => {
    const filePath = getFilePath();
    if (!filePath || filePath.startsWith('__workspace/')) return null;

    const ext = filePath.split('.').pop() || '';
    const name = filePath.split('/').pop() || '';
    const { languageId } = resolveLanguageId(undefined, ext.replace(/^\./, ''), name);
    if (!languageId || !HOVER_LANGUAGES.has(languageId)) return null;

    // Convert the hovered position (0-based offset) to 1-based line:column
    // for the semantic API.
    const line = view.state.doc.lineAt(pos);
    const lineNum = line.number;
    const col = pos - line.from + 1; // 1-based column

    try {
      const result = await api.getSemanticHover(filePath, getContent(), languageId, lineNum, col);
      if (result.error || !result.hover?.contents) return null;

      const contents = result.hover.contents.trim();
      if (!contents) return null;

      // Anchor tooltip at the hovered position
      return {
        pos,
        create() {
          const dom = document.createElement('div');
          dom.className = 'cm-hover-tooltip';
          dom.innerHTML = formatMarkdown(contents);
          return { dom };
        },
      };
    } catch (err) {
      debugLog('[hoverTooltip] failed:', err);
      return null;
    }
  };

  return hoverTooltip(source, {
    hoverTime: 350,
    hideOnChange: true,
  });
}

/**
 * Minimal markdown-to-HTML for hover tooltips.
 * Escapes raw HTML first, then applies markdown transformations on
 * the safe text. Handles code spans, code blocks, bold, italic, and
 * line breaks.
 */
function formatMarkdown(md: string): string {
  // Escape all raw HTML first to prevent XSS
  let safe = escapeHtml(md);

  // Code blocks (``` ... ```) — restore escaped backticks first
  safe = safe.replace(/```[\s\S]*?```/g, (match) => {
    // Strip the ``` delimiters, keep inner text (already escaped)
    return '<pre><code>' + match.replace(/^```\w*\n?/, '').replace(/\n```$/, '') + '</code></pre>';
  });
  // Inline code
  safe = safe.replace(/`([^`]+)`/g, '<code>$1</code>');
  // Bold
  safe = safe.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  // Italic
  safe = safe.replace(/\*(.+?)\*/g, '<em>$1</em>');
  // Line breaks → <br>
  safe = safe.replace(/\n/g, '<br>');
  return safe;
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}
