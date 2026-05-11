/**
 * signatureHelp.ts — CodeMirror 6 extension for function signature help.
 *
 * Shows a tooltip with the current function signature and highlights the
 * active parameter when typing `(` or `,` inside a function call.
 *
 * Implementation approach:
 * - ViewPlugin manages a tooltip DOM element positioned near the cursor.
 * - Triggers on `(` and `,` keystrokes when inside a function-like context.
 * - Fetches signature info from the semantic API (POST /api/semantic).
 * - Ctrl+Shift+Space manually triggers signature help.
 * - Escape, `)`, or moving cursor away dismisses the tooltip.
 * - Debounced at 100ms to avoid rapid API calls.
 * - Only activates for TypeScript/Go files (semantic-supported languages).
 * - No-op when @codemirror/lsp-client is connected (it has its own signatureHelp).
 *
 * Theming:
 * - Uses CSS variables via EditorView.baseTheme().
 * - Active parameter is bolded in the signature display.
 */

import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, keymap } from '@codemirror/view';
import { Annotation, type Extension } from '@codemirror/state';
import { ApiService } from '../services/api';
import { isLSPClientConnected } from './lspExtensions';
import { debugLog } from '../utils/log';

// ── Constants ────────────────────────────────────────────────────────

const DEBOUNCE_MS = 100;

/** Internal annotation to avoid re-triggering on own dispatches. */
const signatureHelpAnnotation = Annotation.define<boolean>();

/** Languages that support signature help via the semantic API. */
const SIGNATURE_HELP_LANGUAGES = new Set(['typescript', 'typescript-jsx', 'javascript', 'javascript-jsx', 'go']);

// ── Types ──────────────────────────────────────────────────────────────

interface SignatureParameter {
  label: string;
  documentation?: string;
}

interface SignatureInfo {
  label: string;
  documentation?: string;
  parameters: SignatureParameter[];
}

interface SignatureHelpData {
  signatures: SignatureInfo[];
  activeSignature: number;
  activeParameter: number;
}

// ── Tooltip DOM Helpers ─────────────────────────────────────────────

function createTooltipDOM(): HTMLDivElement {
  const el = document.createElement('div');
  el.className = 'cm-signature-help-tooltip';
  el.setAttribute('role', 'tooltip');
  el.style.display = 'none';
  return el;
}

function renderSignature(el: HTMLDivElement, data: SignatureHelpData): void {
  const sig = data.signatures[data.activeSignature] ?? data.signatures[0];
  if (!sig) {
    el.style.display = 'none';
    return;
  }

  el.innerHTML = '';

  // Build the signature display with active parameter highlighted
  const sigContainer = document.createElement('div');
  sigContainer.className = 'cm-signature-help-signature';

  // Parse the label to highlight the active parameter
  const parts = splitSignatureAtParams(sig.label, sig.parameters, data.activeParameter);
  for (const part of parts) {
    const span = document.createElement('span');
    if (part.highlight) {
      span.className = 'cm-signature-help-active-param';
      span.style.fontWeight = 'bold';
    }
    span.textContent = part.text;
    sigContainer.appendChild(span);
  }

  el.appendChild(sigContainer);

  // Show documentation if available
  if (sig.documentation) {
    const docEl = document.createElement('div');
    docEl.className = 'cm-signature-help-doc';
    docEl.textContent = sig.documentation;
    el.appendChild(docEl);
  }

  // Show overload count if multiple signatures
  if (data.signatures.length > 1) {
    const overload = document.createElement('div');
    overload.className = 'cm-signature-help-overload';
    overload.textContent = `${data.activeSignature + 1}/${data.signatures.length}`;
    el.appendChild(overload);
  }

  el.style.display = 'block';
}

interface SignaturePart {
  text: string;
  highlight: boolean;
}

/**
 * Split a signature label into parts, highlighting the active parameter.
 *
 * Strategy: find the active parameter's label within the signature text.
 * Parameters are separated by commas inside parentheses.
 */
function splitSignatureAtParams(label: string, params: SignatureParameter[], activeParam: number): SignaturePart[] {
  // Find the opening paren for the parameter list
  const openParen = label.indexOf('(');
  if (openParen < 0) {
    return [{ text: label, highlight: false }];
  }

  const closeParen = findMatchingCloseParen(label, openParen);
  if (closeParen < 0) {
    return [{ text: label, highlight: false }];
  }

  const before = label.substring(0, openParen + 1);
  const paramList = label.substring(openParen + 1, closeParen);
  const after = label.substring(closeParen);

  // Split paramList by commas respecting nesting
  const paramSegments = splitByCommas(paramList);

  // Adjust activeParam if it's out of range (e.g. after trailing comma)
  const paramIdx = Math.min(activeParam, paramSegments.length - 1);

  const parts: SignaturePart[] = [{ text: before, highlight: false }];

  for (let i = 0; i < paramSegments.length; i++) {
    if (i > 0) {
      parts.push({ text: ', ', highlight: false });
    }
    parts.push({
      text: paramSegments[i],
      highlight: i === paramIdx,
    });
  }

  parts.push({ text: after, highlight: false });

  return parts;
}

function findMatchingCloseParen(s: string, openIdx: number): number {
  let depth = 1;
  for (let i = openIdx + 1; i < s.length; i++) {
    if (s[i] === '(') depth++;
    else if (s[i] === ')') {
      depth--;
      if (depth === 0) return i;
    }
  }
  return -1;
}

function splitByCommas(s: string): string[] {
  const result: string[] = [];
  let depth = 0;
  let current = '';

  for (const ch of s) {
    if (ch === '(' || ch === '[' || ch === '{' || ch === '<') {
      depth++;
      current += ch;
    } else if (ch === ')' || ch === ']' || ch === '}' || ch === '>') {
      depth--;
      current += ch;
    } else if (ch === ',' && depth === 0) {
      result.push(current.trim());
      current = '';
    } else {
      current += ch;
    }
  }

  const trimmed = current.trim();
  if (trimmed) {
    result.push(trimmed);
  }
  return result;
}

// ── ViewPlugin ─────────────────────────────────────────────────────

class SignatureHelpPlugin {
  private view: EditorView;
  private getFilePath: () => string | undefined;
  private getContent: () => string;
  private languageId: string | undefined;
  private tooltipEl: HTMLDivElement;
  private timeoutId: ReturnType<typeof setTimeout> | null = null;
  private destroyed = false;
  private fetchGeneration = 0;
  private lastSignatureData: SignatureHelpData | null = null;

  constructor(
    view: EditorView,
    getFilePath: () => string | undefined,
    getContent: () => string,
    languageId: string | undefined,
  ) {
    this.view = view;
    this.getFilePath = getFilePath;
    this.getContent = getContent;
    this.languageId = languageId;
    this.tooltipEl = createTooltipDOM();
    view.dom.appendChild(this.tooltipEl);
  }

  update(update: ViewUpdate): void {
    if (update.transactions.some((t) => t.annotation(signatureHelpAnnotation))) {
      return;
    }
    this.view = update.view;

    if (update.docChanged) {
      // Detect trigger chars by checking the character just before the cursor
      const pos = this.view.state.selection.main.head;
      const charBefore = pos > 0 ? this.view.state.doc.sliceString(pos - 1, pos) : '';
      if (charBefore === '(' || charBefore === ',') {
        this.scheduleUpdate();
      } else if (charBefore === ')') {
        // Dismiss on closing paren
        this.dismiss();
      } else {
        // Keep showing signature help while still inside a function call.
        // Re-fetch to update active parameter position.
        const curPos = this.view.state.selection.main.head;
        if (isInsideFunctionCall(this.view, curPos) && this.lastSignatureData) {
          this.scheduleUpdate(200);
        }
      }
    }

    // Dismiss on selection changes that aren't document changes
    if (update.selectionSet && !update.docChanged) {
      // Keep tooltip on selection-only changes if we have data
      // (e.g. cursor moved due to our own trigger). Reposition it.
      if (this.lastSignatureData) {
        this.positionTooltip();
      }
    }
  }

  /** Manually trigger signature help (e.g. via Ctrl+Shift+Space). */
  trigger(): void {
    this.scheduleUpdate(0);
  }

  dismiss(): void {
    this.tooltipEl.style.display = 'none';
    this.lastSignatureData = null;
  }

  /** Check if tooltip is currently visible (used by keybindings). */
  isTooltipVisible(): boolean {
    return this.lastSignatureData !== null;
  }

  /** Get the editor view (used by subclasses for cleanup registration). */
  getView(): EditorView {
    return this.view;
  }

  private scheduleUpdate(delay: number = DEBOUNCE_MS): void {
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
    }

    this.timeoutId = setTimeout(() => {
      if (this.destroyed || !this.view) return;
      this.fetchSignatureHelp();
    }, delay);
  }

  private async fetchSignatureHelp(): Promise<void> {
    const filePath = this.getFilePath();
    const content = this.view.state.doc.toString();

    if (!filePath || !this.languageId) {
      return;
    }

    // Don't run if LSP client is connected (lsp-client handles it)
    if (isLSPClientConnected(this.languageId)) {
      return;
    }

    // Check if cursor is likely inside a function call
    const pos = this.view.state.selection.main.head;
    if (!isInsideFunctionCall(this.view, pos)) {
      this.dismiss();
      return;
    }

    // Convert position to line:col
    const line = this.view.state.doc.lineAt(pos);
    const lineNum = line.number;
    const col = pos - line.from + 1;

    const generation = ++this.fetchGeneration;

    try {
      const api = ApiService.getInstance();
      const result = await api.getSemanticSignatureHelp(filePath, content, this.languageId, lineNum, col);

      if (this.destroyed || this.fetchGeneration !== generation) return;

      if (result.error || !result.signature_help || !result.signature_help.signatures?.length) {
        this.dismiss();
        return;
      }

      this.lastSignatureData = result.signature_help;
      renderSignature(this.tooltipEl, result.signature_help);
      this.positionTooltip();

      // Re-render with annotation to avoid re-trigger
      this.view.dispatch({
        annotations: [signatureHelpAnnotation.of(true)],
      });
    } catch (err) {
      debugLog('[signatureHelp] Failed to fetch:', err);
      this.dismiss();
    }
  }

  private positionTooltip(): void {
    const pos = this.view.state.selection.main.head;
    const coords = this.view.coordsAtPos(pos);
    if (!coords) return;

    const editorRect = this.view.dom.getBoundingClientRect();
    const tooltipHeight = this.tooltipEl.offsetHeight || 60;

    // Position above cursor by default
    let top = coords.top - editorRect.top - tooltipHeight - 4;
    let left = coords.left - editorRect.left;

    // If tooltip would go above the editor, show below
    if (top < 0) {
      top = coords.bottom - editorRect.top + 4;
    }

    // Clamp horizontal position
    const editorWidth = this.view.dom.offsetWidth;
    const tooltipWidth = this.tooltipEl.offsetWidth || 300;
    if (left + tooltipWidth > editorWidth) {
      left = Math.max(0, editorWidth - tooltipWidth - 8);
    }
    if (left < 0) left = 4;

    this.tooltipEl.style.position = 'absolute';
    this.tooltipEl.style.top = `${top}px`;
    this.tooltipEl.style.left = `${left}px`;
    this.tooltipEl.style.zIndex = '100';
  }

  destroy(): void {
    this.destroyed = true;
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
      this.timeoutId = null;
    }
    if (this.tooltipEl.parentNode) {
      this.tooltipEl.parentNode.removeChild(this.tooltipEl);
    }
    this.lastSignatureData = null;
  }
}

/**
 * Check if cursor position is likely inside a function call.
 * Walks backward from cursor looking for unmatched `(` before encountering `;`, `{`, `}`, or start.
 */
function isInsideFunctionCall(view: EditorView, pos: number): boolean {
  const doc = view.state.doc;
  // Only materialize text up to the cursor — we scan backward, never forward.
  const text = pos > 0 ? doc.sliceString(0, pos) : '';

  let depth = 0;
  let i = pos - 1;

  while (i >= 0) {
    const ch = text[i];
    if (ch === ')') {
      depth++;
    } else if (ch === '(') {
      if (depth === 0) {
        return true; // Found unmatched opening paren => inside call
      }
      depth--;
    } else if (ch === ';' || ch === '{' || ch === '}') {
      // Statement/closure boundary — stop searching
      if (depth === 0) return false;
    }
    i--;
  }
  return false;
}

// ── Base Theme ─────────────────────────────────────────────────────

const signatureHelpTheme = EditorView.baseTheme({
  '.cm-signature-help-tooltip': {
    maxWidth: '600px',
    padding: '6px 10px',
    borderRadius: '4px',
    border: '1px solid var(--cm-signature-help-border, rgba(100, 100, 100, 0.3))',
    backgroundColor: 'var(--cm-signature-help-bg, rgba(40, 44, 52, 0.96))',
    color: 'var(--cm-signature-help-fg, #abb2bf)',
    fontFamily: 'var(--editor-font-family, monospace)',
    fontSize: '0.9em',
    lineHeight: '1.5',
    boxShadow: '0 4px 12px rgba(0, 0, 0, 0.3)',
    pointerEvents: 'none',
    userSelect: 'none',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  '.cm-signature-help-signature': {
    fontFamily: 'var(--editor-font-family, monospace)',
    fontSize: '0.9em',
  },
  '.cm-signature-help-active-param': {
    fontWeight: 'bold',
    color: 'var(--cm-signature-help-active-color, #e5c07b)',
  },
  '.cm-signature-help-doc': {
    fontSize: '0.85em',
    color: 'var(--cm-signature-help-doc-color, rgba(171, 178, 191, 0.7))',
    marginTop: '4px',
    borderTop: '1px solid var(--cm-signature-help-border, rgba(100, 100, 100, 0.2))',
    paddingTop: '4px',
    whiteSpace: 'normal',
    maxWidth: '500px',
  },
  '.cm-signature-help-overload': {
    fontSize: '0.8em',
    color: 'var(--cm-signature-help-overload-color, rgba(171, 178, 191, 0.5))',
    marginTop: '2px',
    textAlign: 'right',
  },
  // Light mode overrides
  '&light .cm-signature-help-tooltip': {
    backgroundColor: 'var(--cm-signature-help-bg, rgba(255, 255, 255, 0.96))',
    color: 'var(--cm-signature-help-fg, #383a42)',
    border: '1px solid var(--cm-signature-help-border, rgba(180, 180, 180, 0.5))',
    boxShadow: '0 4px 12px rgba(0, 0, 0, 0.1)',
  },
  '&light .cm-signature-help-active-param': {
    color: 'var(--cm-signature-help-active-color, #c678dd)',
  },
  '&light .cm-signature-help-doc': {
    color: 'var(--cm-signature-help-doc-color, rgba(56, 58, 66, 0.6))',
  },
});

// We track running plugin instances via a global WeakMap so keybindings can access them.
const activePlugins = new WeakMap<EditorView, SignatureHelpPlugin>();

// Keybinding extension with plugin instance access
function signatureKeymapExtension(): Extension {
  return keymap.of([
    {
      key: 'Ctrl-Shift-Space',
      run(view: EditorView) {
        const plugin = activePlugins.get(view);
        if (plugin) {
          plugin.trigger();
          return true;
        }
        return false;
      },
    },
    {
      key: 'Escape',
      run(view: EditorView) {
        const plugin = activePlugins.get(view);
        if (plugin && plugin.isTooltipVisible()) {
          plugin.dismiss();
          return true;
        }
        return false;
      },
    },
  ]);
}

// ── Public API ──────────────────────────────────────────────────────

/**
 * Creates a CodeMirror 6 extension for signature help.
 *
 * @param getFilePath - Returns the current file path
 * @param getContent - Returns the current document content
 * @param languageId - The language identifier (e.g., "go", "typescript")
 * @returns Extension containing theme, keybindings, and ViewPlugin
 */
export function signatureHelpExtension(
  getFilePath: () => string | undefined,
  getContent: () => string,
  languageId: string | null | undefined,
): Extension {
  // No-op for unsupported languages
  if (!languageId || !SIGNATURE_HELP_LANGUAGES.has(languageId)) {
    return [];
  }

  return [
    signatureHelpTheme,
    ViewPlugin.fromClass(
      class extends SignatureHelpPlugin {
        constructor(view: EditorView) {
          super(view, getFilePath, getContent, languageId ?? undefined);
          activePlugins.set(view, this);
        }
        override destroy(): void {
          activePlugins.delete(this.getView());
          super.destroy();
        }
      },
      {
        decorations(_v): DecorationSet {
          return Decoration.none;
        },
      },
    ),
    signatureKeymapExtension(),
  ];
}

// Exports for testing
export {
  SignatureHelpPlugin,
  splitSignatureAtParams,
  splitByCommas,
  isInsideFunctionCall,
  renderSignature,
  SIGNATURE_HELP_LANGUAGES,
};
