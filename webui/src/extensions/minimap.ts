/**
 * minimap.ts — CodeMirror 6 extension wrapping @replit/codemirror-minimap.
 *
 * Provides a `minimapExtension()` factory that returns a CodeMirror Extension
 * enabling a minimap in the editor's right gutter area.
 *
 * Configuration:
 * - `displayText: 'blocks'` — renders code as simplified blocks for
 *   performance (better for large files).
 * - `showOverlay: 'always'` — always shows the viewport indicator.
 * - The `create` callback builds a `<div class="cm-minimap-container">`
 *   that the minimap renders into.
 */

import { EditorView } from '@codemirror/view';
import { showMinimap } from '@replit/codemirror-minimap';

// Re-export the facet for advanced direct usage.
export { showMinimap };

// ── Base theme ──────────────────────────────────────────────────────

// NOTE: Width is managed dynamically by @replit/codemirror-minimap at
// runtime (inline style based on editor width). Setting width here would
// be overridden. Height is the only property we need to declare.
const minimapBaseTheme = EditorView.baseTheme({
  '.cm-minimap-container': {
    height: '100%',
  },
});

// ── Stable config (hoisted to avoid re-creation on every doc change) ──

const minimapConfig = {
  create: (view: EditorView) => {
    const dom = document.createElement('div');
    dom.className = 'cm-minimap-container';
    return { dom };
  },
  displayText: 'blocks' as const,
  showOverlay: 'always' as const,
};

// ── Public API ──────────────────────────────────────────────────────

/**
 * Returns a CodeMirror extension that enables the minimap gutter.
 *
 * Include in the editor's extensions array (typically via a Compartment
 * so it can be toggled at runtime):
 * ```ts
 * import { minimapExtension } from '../extensions/minimap';
 * // …
 * minimapCompartment.current.of(minimapExtension()),
 * ```
 */
export function minimapExtension() {
  return [
    minimapBaseTheme,
    showMinimap.compute(['doc'], () => minimapConfig),
  ];
}
