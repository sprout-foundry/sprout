/**
 * Theme Import System - Technical Scoping Document
 *
 * This module provides infrastructure for importing standard theme formats
 * (TextMate .tmTheme and VSCode color-theme.json) into the ledit webui's
 * custom theme system.
 *
 * ## Overview
 *
 * The ledit webui currently uses a custom `ThemePack` interface that defines
 * CSS custom properties for both the editor chrome (panes, toolbar, sidebar)
 * and CodeMirror-specific styling (`--cm-*`, `--editor-*`, etc.). This module
 * enables importing themes from external, widely-used formats.
 *
 * ## Supported Formats
 *
 * ### 1. TextMate Theme Format (.tmTheme)
 *
 * TextMate themes are XML-based plist files that define:
 * - **Syntax highlighting colors**: Via `<dict>` entries with `scope` selectors
 *   (e.g., `comment`, `string`, `keyword`, `constant.numeric`, etc.)
 * - **UI colors**: Via top-level settings including:
 *   - `background`: Editor background color
 *   - `foreground`: Default text color
 *   - `caret`: Cursor color
 *   - `selection`: Selection background color
 *   - `lineHighlight`: Active line background
 *   - `invisibles`: Whitespace rendering color
 *   - `findHighlight`: Find match highlight
 *
 * Scope selectors follow TextMate's scope hierarchy (e.g., `source.js string`
 * for JavaScript strings). The format is language-agnostic but relies on
 * grammars to assign scopes.
 *
 * **Limitation**: TextMate themes ONLY provide basic editor UI colors.
 * They do NOT define colors for toolbar, sidebar, tabs, status bar, etc.
 *
 * ### 2. VSCode Color Theme Format (color-theme.json)
 *
 * VSCode themes are JSON files with two main sections:
 *
 * - **`tokenColors`**: Array of color rules with:
 *   - `scope`: TextMate-like scope selector (comma-separated for multiple)
 *   - `settings`: Object with `foreground`, `background`, `fontStyle`
 *   - `name`: Optional human-readable name
 *
 * - **`colors`**: Object mapping 200+ UI element names to hex/rgba colors:
 *   - Editor: `editor.background`, `editor.foreground`, `editorCursor.foreground`
 *   - Gutter: `editorLineNumber.foreground`, `editorLineNumber.activeForeground`
 *   - Selection: `editor.selectionBackground`
 *   - Line highlight: `editor.lineHighlightBackground`
 *   - Widget: `editorWidget.background`, `editorWidget.border`
 *   - Sidebar: `sideBar.background`, `sideBar.foreground`
 *   - Activity bar: `activityBar.background`, `activityBar.foreground`
 *   - Tabs: `tab.activeBackground`, `tab.inactiveForeground`
 *   - Status bar: `statusBar.background`, `statusBar.foreground`
 *   - And many more (see VSCode Theme Color reference)
 *
 * VSCode themes are the **recommended format** for complete UI theming.
 * The vast majority of popular editor themes are distributed in this format.
 *
 * ## Mapping to CodeMirror 6
 *
 * ### Syntax Highlighting (HighlightStyle)
 *
 * CodeMirror 6 uses `HighlightStyle.define()` with tags from `@lezer/highlight`:
 *
 * ```typescript
 * import { tags } from "@lezer/highlight"
 * import { HighlightStyle } from "@codemirror/language"
 *
 * const myHighlightStyle = HighlightStyle.define([
 *   { tag: tags.keyword, color: "#ff79c6" },
 *   { tag: tags.comment, color: "#6272a4", fontStyle: "italic" },
 *   { tag: tags.string, color: "#f1fa8c" },
 *   { tag: tags.number, color: "#bd93f9" },
 *   { tag: tags.operator, color: "#ff79c6" },
 *   { tag: tags.definition(tags.function), color: "#50fa7b" },
 * ])
 * ```
 *
 * Available notable tags from `@lezer/highlight`:
 * - `tags.comment`, `tags.docComment` — Comments
 * - `tags.string`, `tags.escape` — Strings
 * - `tags.keyword`, `tags.operatorKeyword` — Keywords
 * - `tags.number`, `tags.bool`, `tags.null` — Literals
 * - `tags.regexp` — Regular expressions
 * - `tags.definition(tags.variable)` — Variable definitions
 * - `tags.variableName`, `tags.variableName.special brands` — Variables
 * - `tags.definition(tags.function)` — Function definitions
 * - `tags.function(tags.variableName)` — Function calls
 * - `tags.className`, `tags.definition(tags.className)` — Types/classes
 * - `tags.propertyName`, `tags.definition(tags.propertyName)` — Properties
 * - `tags.operator` — Operators
 * - `tags.punctuation`, `tags.separator` — Punctuation
 * - `tags.tagName`, `tags.attributeName` — Markup
 * - `tags.link`, `tags.url`, `tags.heading` — Special content
 * - `tags.strong`, `tags.emphasis` — Inline formatting
 * - `tags.processingInstruction`, `tags.content` — Markup structure
 * - `tags.invalid` — Error/invalid syntax
 *
 * ### Scope-to-Tag Mapping Strategy
 *
 * TextMate/VSCode scopes map to CodeMirror tags via pattern matching:
 *
 * | Scope Pattern              | CodeMirror Tag                                  |
 * |---------------------------|------------------------------------------------|
 * | `comment`                 | `tags.comment`                                  |
 * | `comment.block`           | `tags.comment`                                  |
 * | `comment.line`            | `tags.comment`                                  |
 * | `string`                  | `tags.string`                                   |
 * | `string.escape`           | `tags.escape`                                   |
 * | `string.regexp`           | `tags.regexp`                                   |
 * | `keyword`                 | `tags.keyword`                                  |
 * | `keyword.operator`        | `tags.operatorKeyword`                          |
 * | `storage.type`            | `tags.typeName`                                 |
 * | `entity.name.class`       | `tags.definition(tags.className)`               |
 * | `entity.name.function`    | `tags.definition(tags.function)`                |
 * | `support.function`        | `tags.function(tags.variableName)`              |
 * | `variable`                | `tags.variableName`                             |
 * | `constant.numeric`        | `tags.number`                                   |
 * | `constant.language`       | `tags.bool`                                     |
 * | `punctuation`             | `tags.punctuation`                              |
 * | `operator`                | `tags.operator`                                 |
 * | `entity.name.tag`         | `tags.tagName`                                  |
 * | `markup.heading`          | `tags.heading`                                  |
 * | `markup.bold`             | `tags.strong`                                   |
 * | `markup.italic`           | `tags.emphasis`                                 |
 * | `markup.underline.link`   | `tags.url`                                      |
 * | `invalid`                 | `tags.invalid`                                  |
 *
 * Scopes are matched by longest-suffix. E.g., `source.js string.quoted.double`
 * matches `string.quoted.double` → `tags.string`, falling back to `string` → `tags.string`.
 *
 * ### UI Colors to CSS Variables
 *
 * VSCode's `colors` object maps to ledit's `--cm-*` and UI CSS variables:
 *
 * | VSCode Color Key                      | ledit CSS Variable             |
 * |--------------------------------------|-------------------------------|
 * | `editor.background`                  | `--cm-bg`                     |
 * | `editor.foreground`                  | `--cm-fg`                     |
 * | `editorLineNumber.foreground`        | `--cm-gutter-fg`              |
 * | `editorLineNumber.activeForeground`  | `--cm-gutter-fg-active`       |
 * | `editorCursor.foreground`            | `--cm-cursor`                 |
 * | `editor.selectionBackground`         | `--cm-selection`              |
 * | `editor.lineHighlightBackground`     | `--cm-active-line`            |
 * | `editorGutter.background`            | `--cm-gutter-bg`              |
 * | `sideBar.background`                 | `--sidebar-bg`                |
 * | `sideBar.foreground`                 | `--text-primary`              |
 * | `sideBarSectionHeader.background`    | `--sidebar-header-bg`         |
 * | `tab.activeBackground`               | `--toolbar-bg`                |
 * | `tab.activeForeground`               | `--text-primary`              |
 * | `input.background`                   | `--input-bg`                  |
 * | `input.foreground`                   | `--input-fg`                  |
 * | `focusBorder`                        | `--border-focus`              |
 * | `activityBar.background`             | `--bg-secondary`              |
 * | `activityBar.foreground`             | `--text-primary`              |
 * | `statusBar.background`               | `--bg-secondary`              |
 * | `statusBar.foreground`               | `--text-secondary`            |
 * | `list.hoverBackground`               | `--sidebar-nav-btn-hover-bg`  |
 * | `editorWidget.background`            | `--bg-elevated`              |
 * | `badge.background`                   | `--accent-primary`            |
 * | `titleBar.activeBackground`           | `--bg-primary`              |
 *
 * Any VSCode color key without a mapping is silently ignored.
 * Missing ledit variables fall back to the `:root` CSS defaults in App.css.
 *
 * ## Tradeoffs and Recommendations
 *
 * ### Recommended Format: VSCode color-theme.json
 * - Most widely supported (hundreds of themes on VSCode Marketplace, Open VSX)
 * - Provides both syntax tokens AND full UI colors
 * - JSON format — easy to parse, no XML/plist dependency
 *
 * ### TextMate Format Considerations
 * - Better for syntax-only themes (e.g., language-specific color adjustments)
 * - Requires plist-XML parsing (may need `plist` npm package)
 * - Only provides basic editor colors — UI chrome will use defaults
 * - Still very common; many VSCode themes include `tokenColors` from TM themes
 *
 * ### Scope Matching Limitations
 * - Scope selectors can be complex (unions, intersections, negations)
 * - Full TextMate scope selector support is non-trivial
 * - **Recommendation**: Start with simple suffix matching; add complex selector
 *   support incrementally if needed
 *
 * ### Existing Packages
 * - `@uiw/codemirror-theme-vscode` — Pre-built CM6 themes, NOT a converter
 * - `@replit/codemirror-themes` — Replit's theme collection, reference only
 * - **No mature CM6 converter for VSCode/TM themes exists** — custom implementation needed
 *
 * ## Implementation Plan
 *
 * ### Phase 1: Core Importer (this file)
 * - Type definitions for TM and VSCode formats
 * - ThemeImporter class with import methods
 * - Scope-to-tag mapping table
 * - VSCode-colors-to-ledit-CSS mapping table
 * - Brightness-based dark/light mode detection
 *
 * ### Phase 2: HighlightStyle Integration
 * - Modify EditorPane to accept a HighlightStyle instance per ThemePack
 * - ThemeImporter.buildHighlightStyle() converts token rules → HighlightStyle
 * - Cache HighlightStyle instances to avoid re-creating on every render
 *
 * ### Phase 3: Persistence
 * - Store imported theme JSON in localStorage (keyed by theme ID)
 * - ThemeContext loads imported themes alongside built-in THEME_PACKS
 * - Provide theme management UI (import, delete, reset)
 *
 * ### Phase 4: Advanced Features
 * - File upload via drag-and-drop or file picker
 * - Theme validation (verify required colors exist)
 * - Theme merging (combine syntax from one theme with UI colors from another)
 *
 * ## Example Usage
 *
 * ```typescript
 * import { ThemeImporter } from './themes/themeImport'
 *
 * const importer = new ThemeImporter()
 *
 * // Import a VSCode theme
 * const vscodeTheme = myVSCodeThemeJSON // loaded from file or fetch
 * const themePack = importer.importVSCodeTheme(vscodeTheme)
 *
 * // Also build a CodeMirror HighlightStyle for syntax coloring
 * const highlightStyle = importer.buildHighlightStyle(vscodeTheme.tokenColors)
 *
 * // Register with ThemeContext
 * // themeContext.setThemePack(themePack.id)
 * ```
 *
 * ## References
 *
 * - TextMate Themes Manual: https://macromates.com/manual/en/themes
 * - VSCode Theme Color Reference: https://code.visualstudio.com/api/references/theme-color
 * - VSCode Color Theme Format: https://code.visualstudio.com/api/extension-guides/color-theme
 * - CodeMirror 6 Styling: https://codemirror.net/examples/styling/
 * - Lezer Highlight Tags: https://lezer.codemirror.net/docs/ref/#highlight.Tag
 * - CodeMirror HighlightStyle: https://codemirror.net/docs/ref/#language.HighlightStyle
 */

import { HighlightStyle } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import type { ThemePack, ThemeMode } from './themePacks';

// ============================================================================
// Type Definitions — TextMate Theme Format (.tmTheme)
// ============================================================================

/**
 * TextMate theme setting for a specific scope or global editor settings.
 */
export interface TMThemeSetting {
  fontStyle?: string;
  foreground?: string;
  background?: string;
}

/**
 * Individual token color rule in a TextMate theme.
 */
export interface TMThemeToken {
  name?: string;
  scope: string;
  settings: TMThemeSetting;
}

/**
 * Global editor settings in a TextMate theme (the first entry in `settings[]`
 * that has no `scope` field).
 */
export interface TMThemeGlobalSettings extends TMThemeSetting {
  background?: string;
  foreground?: string;
  caret?: string;
  selection?: string;
  lineHighlight?: string;
  invisibles?: string;
  findHighlight?: string;
  findHighlightForeground?: string;
}

/**
 * Complete parsed TextMate theme (XML plist → JSON).
 */
export interface TMTheme {
  name: string;
  settings: Array<TMThemeSetting | TMThemeToken>;
  uuid?: string;
  semanticClass?: string;
  author?: string;
}

// ============================================================================
// Type Definitions — VSCode Color Theme Format (color-theme.json)
// ============================================================================

/**
 * A single token color rule in a VSCode theme.
 */
export interface VSCodeTokenColor {
  name?: string;
  scope?: string;
  settings: {
    foreground?: string;
    background?: string;
    fontStyle?: string;
  };
}

/**
 * Complete VSCode color-theme.json file.
 */
export interface VSCodeTheme {
  name: string;
  type?: 'dark' | 'light' | 'hc' | 'hcLight';
  tokenColors: VSCodeTokenColor[];
  colors?: Record<string, string>;
  semanticTokenColors?: Record<string, { foreground?: string; fontStyle?: string } | string>;
}

// ============================================================================
// Import Result Type
// ============================================================================

export interface ImportResult {
  success: boolean;
  themePack?: ThemePack;
  highlightStyle?: HighlightStyle;
  warnings?: string[];
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const SCOPE_TO_TAG_ENTRIES: Array<[string, any[]]> = [
  // --- Comments ---
  ['comment', [tags.comment]],
  ['comment.block', [tags.comment]],
  ['comment.line', [tags.comment]],
  ['comment.doc', [tags.docComment, tags.comment]],

  // --- Strings ---
  ['string', [tags.string]],
  ['string.quoted', [tags.string]],
  ['string.quoted.double', [tags.string]],
  ['string.quoted.single', [tags.string]],
  ['string.template', [tags.string]],
  ['string.regexp', [tags.regexp]],
  ['string.escape', [tags.escape]],

  // --- Keywords ---
  ['keyword', [tags.keyword]],
  ['keyword.control', [tags.keyword]],
  ['keyword.operator', [tags.operatorKeyword]],

  // --- Types ---
  ['storage.type', [tags.typeName]],
  ['entity.name.type', [tags.definition(tags.className)]],
  ['entity.name.class', [tags.definition(tags.className)]],

  // --- Functions ---
  ['entity.name.function', [tags.function(tags.variableName)]],
  ['support.function', [tags.function(tags.variableName)]],
  ['meta.function-call', [tags.function(tags.variableName)]],

  // --- Variables ---
  ['variable', [tags.variableName]],
  ['variable.parameter', [tags.variableName]],

  // --- Constants ---
  ['constant.numeric', [tags.number]],
  ['constant.language', [tags.bool]],
  ['constant.character', [tags.string]],

  // --- Operators & punctuation ---
  ['operator', [tags.operator]],
  ['punctuation', [tags.punctuation]],
  ['punctuation.terminator', [tags.punctuation]],
  ['punctuation.separator', [tags.separator]],

  // --- Properties ---
  ['variable.other.property', [tags.propertyName]],
  ['entity.other.attribute-name', [tags.attributeName]],

  // --- Markup ---
  ['entity.name.tag', [tags.tagName]],
  ['markup.heading', [tags.heading]],
  ['markup.bold', [tags.strong]],
  ['markup.italic', [tags.emphasis]],
  ['markup.underline.link', [tags.url]],
  ['markup.raw', [tags.processingInstruction]],

  // --- Special ---
  ['invalid', [tags.invalid]],
  ['invalid.deprecated', [tags.invalid]],
];

// ============================================================================
// Theme Importer
// ============================================================================

/**
 * Converts standard editor theme formats (VSCode, TextMate) into
 * ledit `ThemePack` objects and CodeMirror `HighlightStyle` instances.
 */
export class ThemeImporter {
  /**
   * Maps TextMate/VSCode scope selectors to CodeMirror highlight tags.
   * Scopes are matched by longest suffix (most specific match wins).
   */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private scopeToTag: Map<string, any> = new Map();

  constructor() {
    this.scopeToTag = new Map(SCOPE_TO_TAG_ENTRIES);
  }

  /**
   * Maps VSCode `colors` keys to ledit CSS custom property names.
   */
  private readonly vscodeColorMap: Record<string, string> = {
    // CodeMirror editor core
    'editor.background': '--cm-bg',
    'editor.foreground': '--cm-fg',
    'editorLineNumber.foreground': '--cm-gutter-fg',
    'editorLineNumber.activeForeground': '--cm-gutter-fg-active',
    'editorCursor.foreground': '--cm-cursor',
    'editor.selectionBackground': '--cm-selection',
    'editor.lineHighlightBackground': '--cm-active-line',
    'editorGutter.background': '--cm-gutter-bg',
    'editor.lineHighlightBorder': '--cm-active-line-gutter',
    'editorCursor.background': '--cm-cursor',

    // App chrome
    'sideBar.background': '--sidebar-bg',
    'sideBar.foreground': '--text-primary',
    'sideBar.border': '--border-default',
    'sideBarSectionHeader.background': '--sidebar-header-bg',
    'activityBar.background': '--bg-secondary',
    'activityBar.foreground': '--text-primary',
    'activityBar.activeBorder': '--accent-primary',
    'titleBar.activeBackground': '--bg-primary',
    'statusBar.background': '--bg-secondary',
    'statusBar.foreground': '--text-secondary',

    // Tabs / toolbar
    'tab.activeBackground': '--toolbar-bg',
    'tab.activeForeground': '--text-primary',
    'tab.inactiveBackground': '--bg-secondary',
    'tab.inactiveForeground': '--text-secondary',
    'tab.activeBorder': '--accent-primary',

    // Input
    'input.background': '--input-bg',
    'input.foreground': '--input-fg',
    'input.border': '--border-default',

    // Focus / borders
    focusBorder: '--border-focus',

    // Lists
    'list.hoverBackground': '--sidebar-nav-btn-hover-bg',
    'list.activeSelectionBackground': '--sidebar-select-bg',
    'list.focusBackground': '--sidebar-select-bg',

    // Editor widgets
    'editorWidget.background': '--bg-elevated',
    'editorWidget.border': '--border-default',

    // Accents
    'badge.background': '--accent-primary',
    'badge.foreground': '--bg-primary',
  };

  /**
   * Parses a TextMate/VSCode scope selector to a CodeMirror tag array.
   * Handles comma-separated scopes and suffix matching.
   */
  private resolveScope(scope: string): readonly any[] | null {
    // Handle comma-separated scopes (take first match)
    const parts = scope.split(',').map((s) => s.trim());
    for (const part of parts) {
      const tag = this.matchScope(part);
      if (tag) return tag;
    }
    return null;
  }

  /**
   * Matches a single scope string to a CodeMirror tag by longest suffix.
   */
  private matchScope(scope: string): readonly any[] | null {
    // Try exact match
    const exact = this.scopeToTag.get(scope);
    if (exact) return exact;

    // Try progressively shorter suffixes (e.g., "source.js string.quoted" → "string.quoted" → "string")
    const segments = scope.split('.');
    for (let i = 1; i < segments.length; i++) {
      const suffix = segments.slice(i).join('.');
      const match = this.scopeToTag.get(suffix);
      if (match) return match;
    }

    return null;
  }

  /**
   * Determines if a hex color is light or dark by relative luminance.
   */
  private isLightColor(hex: string): boolean {
    const cleaned = hex.replace('#', '');
    const r = parseInt(cleaned.substring(0, 2), 16);
    const g = parseInt(cleaned.substring(2, 4), 16);
    const b = parseInt(cleaned.substring(4, 6), 16);
    return (0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.5;
  }

  /**
   * Detects whether a theme is dark or light based on its background color.
   */
  private detectMode(bgColor: string | undefined): ThemeMode {
    if (!bgColor) return 'dark';
    return this.isLightColor(bgColor) ? 'light' : 'dark';
  }

  /**
   * Parses a fontStyle string ("italic", "bold", "bold italic", "underline")
   * into a CodeMirror style object subset.
   */
  private parseFontStyle(fontStyle?: string): { fontStyle?: string; fontWeight?: string; textDecoration?: string } {
    if (!fontStyle) return {};
    const result: Record<string, string> = {};
    const lower = fontStyle.toLowerCase();
    if (lower.includes('italic')) result.fontStyle = 'italic';
    if (lower.includes('bold')) result.fontWeight = 'bold';
    if (lower.includes('underline')) result.textDecoration = 'underline';

    // If both bold and italic, CodeMirror EditorView expects fontStyle: "bold italic"
    if (result.fontStyle && result.fontWeight) {
      result.fontStyle = 'bold italic';
      delete result.fontWeight;
    }

    return result;
  }

  /**
   * Maps VSCode `colors` object to ledit CSS custom properties.
   * Unmapped keys are silently ignored.
   */
  mapUIColors(colors: Record<string, string>): Record<string, string> {
    const vars: Record<string, string> = {};
    for (const [vscodeKey, value] of Object.entries(colors)) {
      const leditKey = this.vscodeColorMap[vscodeKey];
      if (leditKey && value) {
        vars[leditKey] = value;
      }
    }
    return vars;
  }

  /**
   * Builds a CodeMirror `HighlightStyle` from VSCode token color rules.
   *
   * @param tokenRules - Array of `VSCodeTokenColor` objects
   * @returns `HighlightStyle` instance ready for use as a CodeMirror extension
   */
  buildHighlightStyle(tokenRules: VSCodeTokenColor[]): HighlightStyle {
    const specs: Array<{
      tag: readonly any[];
      color?: string;
      fontStyle?: string;
      fontWeight?: string;
      textDecoration?: string;
    }> = [];

    for (const rule of tokenRules) {
      if (!rule.scope || !rule.settings.foreground) continue;

      const tag = this.resolveScope(rule.scope);
      if (!tag) continue;

      const { fontStyle, fontWeight, textDecoration } = this.parseFontStyle(rule.settings.fontStyle);

      specs.push({
        tag,
        color: rule.settings.foreground,
        ...(fontStyle ? { fontStyle } : {}),
        ...(fontWeight ? { fontWeight } : {}),
        ...(textDecoration ? { textDecoration } : {}),
      });
    }

    return HighlightStyle.define(specs);
  }

  /**
   * Imports a VSCode color theme into a ledit `ThemePack`.
   *
   * @param theme - Parsed VSCode color-theme.json object
   * @returns `ImportResult` with the theme pack and optional highlight style
   */
  importVSCodeTheme(theme: VSCodeTheme): ImportResult {
    const warnings: string[] = [];
    const id = `imported-${theme.name.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
    const bgColor = theme.colors?.['editor.background'] || theme.colors?.['sideBar.background'];
    const mode = this.detectMode(bgColor);

    // Map UI colors
    const cssVars = this.mapUIColors(theme.colors || {});

    // Warn if critical colors are missing
    if (!cssVars['--cm-cursor']) {
      warnings.push('No cursor color defined — using default');
      cssVars['--cm-cursor'] = mode === 'dark' ? '#f8f8f2' : '#526999';
    }
    if (!cssVars['--cm-bg']) warnings.push('No editor background defined');

    const themePack: ThemePack = {
      id,
      name: theme.name,
      mode,
      description: `Imported VSCode theme: ${theme.name}`,
      editorSyntaxStyle: mode === 'dark' ? 'one-dark' : 'default',
      variables: cssVars,
    };

    const highlightStyle = this.buildHighlightStyle(theme.tokenColors);

    return { success: true, themePack, highlightStyle, warnings };
  }

  /**
   * Imports a TextMate theme into a ledit `ThemePack`.
   *
   * TextMate themes typically only provide editor UI colors (background,
   * foreground, cursor, selection) and syntax token colors. UI chrome colors
   * (sidebar, toolbar, tabs) are NOT available and fall back to defaults.
   *
   * @param theme - Parsed TextMate theme (XML plist → JSON)
   * @returns `ImportResult` with the theme pack and optional highlight style
   */
  importTMTheme(theme: TMTheme): ImportResult {
    const warnings: string[] = [];
    const id = `imported-tm-${theme.name.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;

    // Separate global settings (first entry, no `scope`) from token rules
    let globalSettings: Partial<TMThemeGlobalSettings> = {};
    const tokenRules: TMThemeToken[] = [];

    for (const entry of theme.settings) {
      if ('scope' in entry && entry.scope) {
        tokenRules.push(entry as TMThemeToken);
      } else {
        globalSettings = { ...globalSettings, ...entry };
      }
    }

    const mode = this.detectMode(globalSettings.background);

    // Map TextMate global settings to ledit CSS vars
    const tmColorMap: Record<string, string> = {};
    if (globalSettings.background) tmColorMap['--cm-bg'] = globalSettings.background;
    if (globalSettings.foreground) tmColorMap['--cm-fg'] = globalSettings.foreground;
    if (globalSettings.caret) tmColorMap['--cm-cursor'] = globalSettings.caret;
    if (globalSettings.selection) tmColorMap['--cm-selection'] = globalSettings.selection;
    if (globalSettings.lineHighlight) tmColorMap['--cm-active-line'] = globalSettings.lineHighlight;

    if (!tmColorMap['--cm-cursor']) {
      warnings.push('No caret/cursor color defined — using default');
      tmColorMap['--cm-cursor'] = mode === 'dark' ? '#f8f8f2' : '#526999';
    }

    const themePack: ThemePack = {
      id,
      name: theme.name,
      mode,
      description: `Imported TextMate theme: ${theme.name}`,
      editorSyntaxStyle: mode === 'dark' ? 'one-dark' : 'default',
      variables: tmColorMap,
    };

    // Convert TMTokenRules to VSCodeTokenColor format for buildHighlightStyle
    const vsFormat: VSCodeTokenColor[] = tokenRules.map((t) => ({
      name: t.name,
      scope: t.scope,
      settings: { foreground: t.settings.foreground, fontStyle: t.settings.fontStyle },
    }));
    const highlightStyle = this.buildHighlightStyle(vsFormat);

    return { success: true, themePack, highlightStyle, warnings };
  }
}
