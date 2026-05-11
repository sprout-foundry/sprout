/**
 * emmet.ts — Emmet abbreviation expansion for HTML, CSS, and JSX in CodeMirror 6.
 *
 * Wraps @emmetio/codemirror6-plugin to provide:
 * - Abbreviation tracking with live preview (Tab to expand, Esc to cancel)
 * - Wrap-with-abbreviation (Ctrl+Shift+W by default)
 * - Manual expand-abbreviation command (Ctrl+E fallback)
 *
 * Only active for HTML, XML, CSS, SCSS, SASS, and JSX language modes.
 */

import { type Extension, Compartment } from '@codemirror/state';
import { keymap, type EditorView } from '@codemirror/view';
import {
  abbreviationTracker,
  wrapWithAbbreviation,
  expandAbbreviation,
  EmmetKnownSyntax,
} from '@emmetio/codemirror6-plugin';

/**
 * Language IDs for which Emmet should be active.
 */
const EMMET_LANGUAGE_IDS = new Set(['html', 'xml', 'css', 'sass', 'scss', 'javascript-jsx', 'typescript-jsx']);

/**
 * Map from our language IDs to Emmet syntax strings.
 */
const LANGUAGE_TO_EMMET_SYNTAX: Record<string, EmmetKnownSyntax> = {
  html: EmmetKnownSyntax.html,
  xml: EmmetKnownSyntax.xml,
  css: EmmetKnownSyntax.css,
  scss: EmmetKnownSyntax.scss,
  sass: EmmetKnownSyntax.sass,
  'javascript-jsx': EmmetKnownSyntax.jsx,
  'typescript-jsx': EmmetKnownSyntax.tsx,
};

/**
 * Returns the appropriate Emmet syntax for a given language ID, or null
 * if Emmet should not be active for that language.
 */
function getEmmetSyntax(languageId: string | null | undefined): EmmetKnownSyntax | null {
  if (!languageId || !EMMET_LANGUAGE_IDS.has(languageId)) {
    return null;
  }
  return LANGUAGE_TO_EMMET_SYNTAX[languageId] ?? null;
}

/**
 * Build the Emmet Extension[] for a given language ID.
 * Returns an empty array when Emmet should not be active.
 */
export function buildEmmetExtensions(languageId: string | null | undefined): Extension[] {
  const syntax = getEmmetSyntax(languageId);
  if (!syntax) {
    return [];
  }

  try {
    return [
      abbreviationTracker({ syntax }),
      wrapWithAbbreviation('Ctrl-Shift-w'),
      keymap.of([
        {
          key: 'Ctrl-e',
          mac: 'Cmd-e',
          run: expandAbbreviation,
        },
      ]),
    ];
  } catch (err) {
    console.error('[emmet] Failed to build extensions:', err);
    return [];
  }
}

/**
 * Create a Compartment for Emmet extensions.
 * Use this to reconfigure Emmet when the language changes.
 */
export function createEmmetCompartment(): Compartment {
  return new Compartment();
}

/**
 * Get initial Emmet extensions for a Compartment (empty array = disabled).
 */
export function getInitialEmmetExtensions(languageId: string | null | undefined): Extension[] {
  return buildEmmetExtensions(languageId);
}

/**
 * Reconfigure the Emmet compartment on the given view for a new language.
 */
export function reconfigureEmmet(
  compartment: Compartment,
  view: EditorView,
  languageId: string | null | undefined,
): void {
  try {
    view.dispatch({
      effects: compartment.reconfigure(buildEmmetExtensions(languageId)),
    });
  } catch (err) {
    // Graceful degradation — emmet reconfiguration must not crash the editor.
    console.error('[emmet] Failed to reconfigure compartment:', err);
  }
}

/**
 * Check whether Emmet is active for a given language ID.
 */
export function isEmmetLanguage(languageId: string | null | undefined): boolean {
  return getEmmetSyntax(languageId) !== null;
}
