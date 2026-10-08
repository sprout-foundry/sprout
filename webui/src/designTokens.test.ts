/**
 * Design tokens live in the package (SP-160 §160d).
 *
 * The web UI no longer defines its own token values: they are the
 * `@sprout-foundry/design` package's `tokens.css`, imported once by
 * `webui/src/index.css`, and theme packs layer their variables on top at
 * runtime. These tests pin the two halves of that contract:
 *
 * (a) The web UI actually consumes the package — the entry stylesheet
 *     `@import`s `@sprout-foundry/design/tokens.css`, and `App.css` no longer
 *     carries the moved token block.
 * (b) Every token the web UI uses (`var(--token)` in its CSS, and the token
 *     names in the TS/TSX that apply them) is defined by the package, or is a
 *     documented runtime/theme-pack/component-local token. The scan is
 *     deliberately non-vacuous: it fails when a token the UI uses is removed
 *     from the package.
 *
 * The allowlist is split by why a token is allowed, so the test's silence is
 * auditable and each category is small enough to review:
 *
 *  - RUNTIME_TOKENS     — set on an element by JavaScript, not authored in CSS.
 *  - COMPONENT_TOKENS   — a component's own local vocabulary (a scoped rule or
 *                         a JS-applied style var), never a shared design token.
 *  - CODEMIRROR_TOKENS  — the CodeMirror extension surface's variables, whose
 *                         values the editor's own theme supplies.
 *  - THEME_PACK_TOKENS  — defined by the theme packs (`themes/themePacks.ts`),
 *                         the layer that sits on top of the package (SP-160
 *                         §160d).
 *  - PACKAGE_OVERRIDES  — a local default for a token the package does not
 *                         define yet (each has a `var(…, fallback)` at its use
 *                         site, so the UI renders without the package).
 */

import fs from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { THEME_PACKS } from './themes/themePacks';

const WEBUI_SRC = path.resolve(__dirname);
const DESIGN_TOKENS = path.resolve(__dirname, '../../packages/design/tokens.css');
const INDEX_CSS = path.resolve(WEBUI_SRC, 'index.css');
const APP_CSS = path.resolve(WEBUI_SRC, 'App.css');

/** Tokens applied to an element by JavaScript rather than authored in CSS. */
const RUNTIME_TOKENS = new Set([
  // components/Terminal.tsx — the measured terminal height.
  '--sprout-terminal-reserved-height',
  // hooks/useUIScale.ts — the paint factor (0.9/1/1.12/1.3).
  '--ui-scale',
  // components/standalone/editorMain.ts — the standalone editor's palette.
  '--editor-bg',
  '--editor-fg',
  // components/Sidebar.tsx (config/layout.ts) — the measured left rail width.
  '--sprout-left-inset',
]);

/**
 * A component's own local vocabulary: a scoped rule (`--cm-panel-*` on
 * `.cm-search`, `--select-caret-color` on a caret rule) or a style var a
 * component applies itself (SubagentTree/SubagentActivityFeed per-row colors).
 * These are not shared design tokens and never were defined on `:root`.
 */
const COMPONENT_TOKENS = new Set([
  '--st-color',
  '--st-persona-color',
  '--feed-persona-color',
  '--card-border-color',
  '--chat-input-height',
  '--select-caret-color',
  '--trigger-width',
  '--terminal-height',
  '--t-base',
  '--t-fast',
  '--cm-panel-bg',
  '--cm-panel-input-bg',
  '--cm-panel-input-border',
  '--cm-panel-input-placeholder',
  '--cm-panel-button-bg',
  '--cm-panel-button-border',
  '--cm-panel-button-fg',
  '--cm-panel-button-hover-bg',
  '--cm-panel-button-hover-border',
  '--cm-panel-button-active-bg',
  '--cm-search-invalid-bg',
  '--cm-search-invalid-border',
]);

/**
 * The CodeMirror extension surface's variables. The editor's own theme (or the
 * resolved theme pack) supplies their values; the web UI only references them.
 */
const CODEMIRROR_TOKENS = new Set([
  '--cm-bg',
  '--cm-fg',
  '--cm-gutter-fg',
  '--cm-border-color',
  '--cm-tooltip-bg',
  '--cm-tooltip-border',
  '--cm-whitespace-char',
  '--cm-whitespace-space',
]);

/**
 * Tokens the theme packs define. They are not in the package because they are
 * the override layer that sits on top of it (SP-160 §160d: "theme packs layer
 * on top").
 */
const THEME_PACK_TOKENS = new Set(THEME_PACKS.flatMap((pack) => Object.keys(pack.variables)));

/**
 * A local default for a token the package does not define yet; each use site
 * carries `var(--token, <fallback>)`, so the UI renders without the package.
 */
const PACKAGE_OVERRIDES = new Set([
  '--accent',
  '--accent-color-hover',
  '--accent-danger',
  '--accent-design',
  '--accent-primary-bg',
  '--accent-primary-dim',
  '--accent-warning-alpha',
  '--bg',
  '--bg-card',
  '--bg-raised',
  '--border',
  '--border-accent',
  '--editor-font-family',
  '--editor-font-size',
  '--editor-gutter-bg',
  '--font-size-md',
  '--font-size-xl',
  '--space-md',
  '--space-sm',
  '--space-xs',
  '--status-error',
  '--status-success',
  '--warning-fg',
  // ErrorBoundary's standalone fallback palette.
  '--background',
  '--code-bg',
  '--error',
  '--primary',
  '--primary-hover',
  '--secondary',
  '--secondary-hover',
  '--text',
]);

const ALLOWED = new Set([
  ...RUNTIME_TOKENS,
  ...COMPONENT_TOKENS,
  ...CODEMIRROR_TOKENS,
  ...THEME_PACK_TOKENS,
  ...PACKAGE_OVERRIDES,
]);

/** Every `--token` name declared in the package's tokens.css. */
function declaredTokens(css: string): Set<string> {
  return new Set([...css.matchAll(/(--[A-Za-z0-9_-]+)\s*:/g)].map((m) => m[1]));
}

/** Every `var(--token)` (or `var(--token, fallback)`) reference in a text. */
function referencedTokens(text: string): Set<string> {
  return new Set([...text.matchAll(/var\(\s*(--[A-Za-z0-9_-]+)/g)].map((m) => m[1]));
}

/** The token names a source file introduces (declarations, CSS-var assignment keys, style-object keys). */
function tokenNamesInSource(text: string): Set<string> {
  // Comments are stripped so prose like `--native-git` (a build seam name, not
  // a token) is never mistaken for a token a file introduces.
  const code = stripComments(text);
  const names = new Set<string>();
  for (const match of code.matchAll(/(--[A-Za-z0-9_-]+)\s*:/g)) names.add(match[1]);
  for (const match of code.matchAll(/setProperty\(\s*['"](--[A-Za-z0-9_-]+)['"]/g)) names.add(match[1]);
  // A style object's custom-property key: `{ '--st-color': … }`.
  for (const match of code.matchAll(/['"](--[A-Za-z0-9_-]+)['"]\s*:/g)) names.add(match[1]);
  return names;
}

/** Remove `//` and `/* *\/` comments so only code is scanned for token names. */
function stripComments(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

/** Walk `dir`, collecting files whose name matches `filter`. */
function walk(dir: string, filter: (name: string) => boolean): string[] {
  const out: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(full, filter));
    else if (filter(entry.name)) out.push(full);
  }
  return out;
}

const rel = (file: string) => path.relative(WEBUI_SRC, file).split(path.sep).join('/');

const cssFiles = walk(WEBUI_SRC, (name) => name.endsWith('.css')).filter((file) => !file.includes('.test.'));
const sourceFiles = walk(WEBUI_SRC, (name) => /\.tsx?$/.test(name)).filter(
  (file) => !/\.(test|spec)\.tsx?$/.test(file) && !file.endsWith('.d.ts'),
);

const packageTokens = declaredTokens(fs.readFileSync(DESIGN_TOKENS, 'utf-8'));

/** Tokens referenced across the web UI's CSS that the predicate rejects. */
function uncoveredCssTokens(declared: Set<string>): string[] {
  const missing: string[] = [];
  for (const file of cssFiles) {
    for (const token of referencedTokens(fs.readFileSync(file, 'utf-8'))) {
      if (!declared.has(token) && !ALLOWED.has(token)) missing.push(`${rel(file)} uses undefined token ${token}`);
    }
  }
  return missing;
}

describe('the web UI consumes the design tokens from the package', () => {
  it('imports the package token stylesheet from the app entry stylesheet', () => {
    const indexCss = fs.readFileSync(INDEX_CSS, 'utf-8');
    expect(indexCss).toMatch(/@import\s+['"]@sprout-foundry\/design\/tokens\.css['"]/);
    expect(indexCss).toMatch(/@import\s+['"]@sprout-foundry\/design\/reset\.css['"]/);
  });

  it('declares the package dependency in webui/package.json', () => {
    const pkg = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../package.json'), 'utf-8'));
    // The same `file:` form the sibling internal packages use, so the workspace
    // symlink resolves it in dev and the build bundles it.
    expect(pkg.dependencies['@sprout-foundry/design']).toBe('file:../packages/design');
  });

  it('no longer defines the moved token block in App.css', () => {
    const appCss = fs.readFileSync(APP_CSS, 'utf-8');
    // The canonical shared tokens moved to the package, so App.css's `:root`
    // block must no longer declare them. `--text-*`/`--space-*` still appear
    // inside the `[data-ui-scale]` override blocks (a scale cascade, not the
    // base block), so the scan is windowed to the `:root { … }` block itself.
    const rootBlock = appCss.match(/:root\s*\{[^}]*\}/)?.[0] ?? '';
    expect(rootBlock).not.toBe('');
    for (const token of ['--bg-primary', '--accent-primary', '--text-primary', '--space-4', '--radius-md']) {
      expect(rootBlock).not.toMatch(new RegExp(`${token}\\s*:`));
    }
    // The app keeps its runtime tokens in that block.
    expect(rootBlock).toMatch(/--sprout-bottom-nav-height\s*:/);
  });

  it('the package declares tokens (the coverage check is not vacuous)', () => {
    expect(packageTokens.size).toBeGreaterThan(100);
  });
});

describe('every token the web UI uses is defined by the package', () => {
  it('covers every var(--token) reference in the web UI CSS', () => {
    expect(uncoveredCssTokens(packageTokens)).toEqual([]);
  });

  it('covers every token name a component introduces', () => {
    const missing: string[] = [];
    for (const file of sourceFiles) {
      for (const token of tokenNamesInSource(fs.readFileSync(file, 'utf-8'))) {
        if (!packageTokens.has(token) && !ALLOWED.has(token)) {
          missing.push(`${rel(file)} introduces undefined token ${token}`);
        }
      }
    }
    expect(missing).toEqual([]);
  });

  it('would fail if a used token were removed from the package', () => {
    // Non-vacuous proof: simulate the package losing a token the UI uses and
    // assert the coverage predicate reports it. `--radius-md` is declared only
    // by the package (theme packs do not redefine it) and used widely; with it
    // removed from the declared set the scan must flag it.
    const shrunk = new Set(packageTokens);
    shrunk.delete('--radius-md');
    const offenders = uncoveredCssTokens(shrunk);
    expect(offenders.length).toBeGreaterThan(0);
    expect(offenders.some((entry) => entry.endsWith('uses undefined token --radius-md'))).toBe(true);
  });

  it('would fail if the UI introduced a token the package does not define', () => {
    // The source half of the same proof: a rename/typo'd token name in TSX is
    // flagged. `--st-color` is allowed as a component token; drop it from the
    // allowlist and the component that applies it is reported.
    const withoutComponentTokens = new Set(ALLOWED);
    withoutComponentTokens.delete('--st-color');
    const missing: string[] = [];
    for (const file of sourceFiles) {
      for (const token of tokenNamesInSource(fs.readFileSync(file, 'utf-8'))) {
        if (!packageTokens.has(token) && !withoutComponentTokens.has(token)) {
          missing.push(`${rel(file)} introduces undefined token ${token}`);
        }
      }
    }
    expect(missing.some((entry) => entry.endsWith('introduces undefined token --st-color'))).toBe(true);
  });
});
