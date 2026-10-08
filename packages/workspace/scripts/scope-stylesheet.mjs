// Scope the package's bundled stylesheet to the workspace root and emit it
// as the package's one stable stylesheet artifact.
//
// vite's library build concatenates every CSS module the views graph reaches
// into one asset (`chunks/workspace.css`) that is written in the *app's*
// global vocabulary: token declarations on `:root`, `body`/`html` rules and
// element selectors such as `select.styled-select`. Loaded on a host page
// that stylesheet would leak onto the host's own DOM outside the mounted
// workspace. This pass rewrites it so every rule applies only inside the
// workspace root (`.sprout-workspace`, the class `SproutWorkspace` renders)
// and writes the result to the package's declared `dist/workspace.css`.
//
// The rewrite is textual and deliberately conservative: it rewrites only the
// selectors that would otherwise escape the workspace root and leaves every
// workspace-rooted (already class-scoped) rule byte-for-byte alone, so the
// stylesheet cannot be broken by a selector grammar it does not model.

import {
  existsSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const dist = resolve(packageDir, "dist");
const chunksDir = resolve(dist, "chunks");

/** The workspace root class `SproutWorkspace` renders (webui `sprout-workspace`). */
export const WORKSPACE_ROOT_CLASS = ".sprout-workspace";
/** The stable artifact path the package ships and its `exports` map declares. */
export const STYLESHEET_PATH = "dist/workspace.css";

const hasClass = (selector) =>
  selector === WORKSPACE_ROOT_CLASS ||
  selector.startsWith(`${WORKSPACE_ROOT_CLASS} `) ||
  selector.startsWith(`${WORKSPACE_ROOT_CLASS}.`) ||
  selector.startsWith(`${WORKSPACE_ROOT_CLASS}[`) ||
  selector.startsWith(`${WORKSPACE_ROOT_CLASS}:`);
/**
 * A keyframe step prelude (`from`, `to`, `50%`, `0%`) — a selector-like token
 * that is not a selector at all and must never be scoped.
 */
const isKeyframeStep = (selector) =>
  /^(from|to|\d+(?:\.\d+)?%)$/i.test(selector);

/**
 * The first compound of a selector — everything up to the first combinator or
 * whitespace (`body` in `body .x`, `.foo.bar` in `.foo.bar > .x`).
 */
function firstCompound(selector) {
  const m = selector.match(/^[^\s>+~]+/);
  return m ? m[0] : selector;
}

/**
 * `*` and bare element selectors (`body`, `html`, `select`, `button`, …) —
 * anything that matches elements a host page also owns. A selector whose first
 * compound is (or begins with) the workspace-root class is already scoped; a
 * selector whose first compound carries any other *class* (`.foo`,
 * `select.styled-select`) is component-scoped and kept.
 *
 * Exported so the artifact test can assert the shipped stylesheet against the
 * pass's own predicate — one definition, so the test cannot drift from the pass.
 */
export function isLeakingSelector(selector) {
  const s = selector.trim();
  if (!s) return false;
  if (hasClass(s)) return false;
  if (isKeyframeStep(s)) return false;
  if (s === "*") return true;
  const first = firstCompound(s);
  // A class in the first compound anchors the selector to a component (or the
  // workspace root), so it cannot match a host element at the top level.
  if (first.includes(".")) return false;
  // Otherwise the selector reaches elements by name (`body`, `select`), by the
  // universal `*`, or by a bare attribute (`[data-theme=dark] .x`) — all of
  // which also match host-owned elements — and leaks onto the host page.
  return /^[a-z*[]/i.test(first);
}

/** Split a selector list on top-level commas (no nesting inside strings). */
function splitSelectorList(list) {
  return list
    .split(",")
    .map((part) => part.trim())
    .filter((part) => part.length > 0);
}

/**
 * Rewrite ONE selector so it only matches inside the workspace root.
 *
 * - A leading `:root` (the app's global token block, its light-theme block,
 *   and `:root[data-theme] .child` guards) becomes the workspace root class,
 *   plus `[data-theme]` when the original carried it — the tokens land on the
 *   workspace root and the theme guard still applies within it.
 * - A leading `:root.some-class` (e.g. `:root.phone-tab-bar-shown`) keeps the
 *   class on the workspace root.
 * - A leak (bare element/universal) gets the workspace-root descendant prefix.
 * - Everything else is untouched.
 */
function scopeSelector(selector) {
  const s = selector.trim();
  if (s.startsWith(":root")) {
    const rest = s.slice(":root".length);
    return `${WORKSPACE_ROOT_CLASS}${rest}`;
  }
  if (isLeakingSelector(s)) {
    return `${WORKSPACE_ROOT_CLASS} ${s}`;
  }
  return s;
}

/**
 * Rewrite a whole rule prelude — the text before a `{`. An at-rule prelude
 * (`@media …`, `@keyframes …`) is returned unchanged; a selector list is
 * scoped selector by selector and rejoined.
 *
 * `keepLeadingSpace` preserves the whitespace the prelude carried before its
 * first selector when the rule follows other output (a source-formatted space
 * between two rules). At the very start of the stylesheet it is dropped, so a
 * leading comment's residual whitespace does not leak into the first selector.
 */
function scopePrelude(prelude, keepLeadingSpace) {
  const trimmed = prelude.trim();
  if (trimmed.startsWith("@")) return prelude;
  const leading = keepLeadingSpace
    ? prelude.slice(0, prelude.length - prelude.trimStart().length)
    : "";
  return leading + splitSelectorList(trimmed).map(scopeSelector).join(",");
}

// ── A minimal, comment/string-aware scanner over the emitted CSS ─────────
//
// The stylesheet is minified but still carries the xterm license banner in a
// `/* ... */` comment, and it contains `url("data:...")` payloads with
// braces and commas inside strings. The scanner tracks string state and
// comments so a `{` inside a comment or a `,` inside a data URI never
// splits a selector.

/** Remove CSS comments, respecting strings. */
function stripComments(css) {
  let out = "";
  let i = 0;
  while (i < css.length) {
    const ch = css[i];
    if (ch === '"' || ch === "'") {
      const quote = ch;
      out += ch;
      i += 1;
      while (i < css.length && css[i] !== quote) {
        out += css[i];
        if (css[i] === "\\") {
          i += 1;
          if (i < css.length) out += css[i];
        }
        i += 1;
      }
      if (i < css.length) {
        out += css[i];
        i += 1;
      }
      continue;
    }
    if (ch === "/" && css[i + 1] === "*") {
      i += 2;
      while (i < css.length && !(css[i] === "*" && css[i + 1] === "/")) i += 1;
      i += 2;
      continue;
    }
    out += ch;
    i += 1;
  }
  return out;
}

/**
 * Rewrite every rule prelude in a comment-free stylesheet. The scanner walks
 * the string once, buffering text until a `{` (where the buffered prelude is
 * scoped) or a `}` (where the buffered declaration body is flushed verbatim).
 *
 * A stack tracks the open blocks so the scan knows when it is inside an
 * `@keyframes` body: every prelude there is a keyframe step (`0%`, `from`,
 * `0%, to`, `0%, 100%` — and vite minifies `100%` to `to`), not a selector,
 * and is passed through verbatim. Detecting keyframes structurally rather
 * than by a regex on the step text makes the pass immune to whitespace and to
 * multi-step lists, either of which a regex would miss and then mangle into
 * an invalid rule.
 */
function scopeRules(css) {
  let out = "";
  let pending = "";
  const stack = [];
  let i = 0;
  while (i < css.length) {
    const ch = css[i];
    if (ch === '"' || ch === "'") {
      const quote = ch;
      pending += ch;
      i += 1;
      while (i < css.length && css[i] !== quote) {
        pending += css[i];
        if (css[i] === "\\") {
          i += 1;
          if (i < css.length) pending += css[i];
        }
        i += 1;
      }
      if (i < css.length) {
        pending += css[i];
        i += 1;
      }
      continue;
    }
    if (ch === "{") {
      const prelude = pending;
      const inKeyframes = stack.includes("keyframes");
      out += inKeyframes ? prelude : scopePrelude(prelude, out.length > 0);
      out += "{";
      stack.push(/^\s*@keyframes/.test(prelude) ? "keyframes" : "rule");
      pending = "";
    } else if (ch === "}") {
      out += pending + "}";
      stack.pop();
      pending = "";
    } else {
      pending += ch;
    }
    i += 1;
  }
  out += pending;
  return out;
}

/** Scope a bundled stylesheet: comments stripped, leaking selectors scoped. */
export function scopeStylesheet(css) {
  return scopeRules(stripComments(css));
}

/**
 * The bundled CSS assets vite emitted under `dist/chunks/`, sorted so the
 * output is deterministic regardless of directory order.
 */
function bundledStylesheets() {
  if (!existsSync(chunksDir)) return [];
  return readdirSync(chunksDir)
    .filter((name) => name.endsWith(".css"))
    .sort()
    .map((name) => join(chunksDir, name));
}

/**
 * Scope the emitted stylesheet(s), write the single artifact to
 * `dist/workspace.css`, and remove the incidental `chunks/*.css`. Idempotent:
 * running it twice produces the same bytes.
 */
export function emitWorkspaceStylesheet() {
  const sources = bundledStylesheets();
  if (sources.length === 0) {
    throw new Error(
      "scope-stylesheet: no bundled stylesheet under dist/chunks — did the build emit CSS?",
    );
  }
  const scoped = sources
    .map((file) => scopeStylesheet(readFileSync(file, "utf-8")))
    .join("\n");
  writeFileSync(resolve(dist, "workspace.css"), scoped);
  for (const file of sources) rmSync(file);
  return {
    path: STYLESHEET_PATH,
    bytes: Buffer.byteLength(scoped),
    sources: sources.length,
  };
}

// `node scripts/scope-stylesheet.mjs` runs the pass on its own (used by the
// build script after `vite build`).
if (import.meta.url === `file://${process.argv[1]}`) {
  const result = emitWorkspaceStylesheet();
  process.stdout.write(
    `scope-stylesheet: wrote ${relative(packageDir, resolve(dist, "workspace.css"))} ` +
      `(${result.sources} bundled source(s), ${result.bytes} bytes)\n`,
  );
}
