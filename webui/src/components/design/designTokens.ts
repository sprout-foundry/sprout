/**
 * Design token model — the pure half of the Tokens tab (SP-140-3 §3d).
 *
 * Parses W3C DTCG `*.tokens.json` documents into the grouped tree the tab
 * renders, mirroring the item-3.2 `designApi.parseTokensFile` conventions and
 * `pkg/design/tokens.go`'s charter (SP-140-1 §1a): the top level is groups,
 * leaves carry `$value`/`$type`, `$`-prefixed keys are metadata and never
 * structural, and a leaf is never descended into. Alias references
 * (`{group.token}`) and structured `$value`s (`border`'s object) are walked.
 *
 * Kept import-free and DOM-free so the grouping rules unit-test as plain
 * functions and `TokensTree.tsx` stays a presentational component under the
 * AGENTS.md 500-line rule.
 *
 * Everything here is read-only: the model is a view of file text, never a
 * mutation path (§3d — "editing tokens is file editing").
 */

/** The nine `$type` values SP-140-1 §1a accepts, in charter order. */
export const TOKEN_TYPES = [
  'color',
  'dimension',
  'fontFamily',
  'fontWeight',
  'number',
  'duration',
  'cubicBezier',
  'strokeStyle',
  'border',
] as const;

export type TokenType = (typeof TOKEN_TYPES)[number];

/** The type a leaf with no explicit and no inherited `$type` carries. */
export const UNKNOWN_TOKEN_TYPE = 'unknown';

/** Section ids for the grouped tree, in render order. */
export const TOKEN_SECTIONS = ['color', 'typography', 'spacing', 'other'] as const;

export type TokenSection = (typeof TOKEN_SECTIONS)[number];

/** Human labels for the grouped-tree sections. */
export const TOKEN_SECTION_LABELS: Record<TokenSection, string> = {
  color: 'Color',
  typography: 'Typography',
  spacing: 'Spacing',
  other: 'Other',
};

/**
 * Section for a leaf type. Typography owns the font face/weight/scale types,
 * spacing owns `dimension` (the "spacing (dimension)" specimens of §3d), and
 * the remaining charter types — motion, borders, plain numbers — land in
 * "Other" with their own specimen line.
 */
export function sectionForType(type: string): TokenSection {
  if (type === 'color') return 'color';
  if (type === 'fontFamily' || type === 'fontWeight' || type === 'number') return 'typography';
  if (type === 'dimension') return 'spacing';
  return 'other';
}

/**
 * A DTCG value rendered as display text. Structured values (a `border`'s
 * `{color,width,style}`, a `cubicBezier` array, an alias *inside* a border)
 * render as compact JSON so the table never shows `[object Object]`; scalars
 * render as their string form. No value is ever invented — an absent value is
 * the empty string.
 */
export function formatTokenValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return '';
  }
}

/**
 * The `{group.token}` alias a *whole* value points at, else null. Mirrors
 * `pkg/design/tokens_alias.go`'s `aliasPath`: only a string that is entirely
 * `{` + inner + `}` aliases, the inner text is used untrimmed and must be free
 * of braces. A multi-part string (`"{a} / {b}"`) is a plain value.
 */
export function tokenAliasPath(value: unknown): string | null {
  if (typeof value !== 'string') return null;
  if (value.length < 3 || !value.startsWith('{') || !value.endsWith('}')) return null;
  const inner = value.slice(1, -1);
  if (!inner || inner.includes('{') || inner.includes('}')) return null;
  return inner;
}

export interface DesignToken {
  /** Dotted document path — the search/filter key and the detail-pane hint. */
  path: string;
  /** Final path segment (the token's key in its group). */
  name: string;
  /** `$type` as declared, inherited, or `unknown`. */
  type: string;
  /** Section of the grouped tree this token renders under. */
  section: TokenSection;
  /** `$value` verbatim, for type-specific rendering (a swatch's color, …). */
  value: unknown;
  /** `$value` as display text. */
  valueText: string;
  /** `{group.token}` alias this token's whole value points at, else null. */
  alias: string | null;
  /** `$description`, when present. */
  description: string;
  /** `$extensions`, carried through verbatim (SP-140-1 §1a: never required). */
  extensions: unknown;
  /** Issuing file's design-root-relative path. */
  filePath: string;
  /** Issuing file's stem (`color.tokens.json` → `color`), the file group. */
  fileName: string;
}

export interface DesignTokenFile {
  /** Design-root-relative path (`tokens/color.tokens.json`). */
  path: string;
  /** File stem — the name every token in this file is grouped under. */
  name: string;
  tokens: DesignToken[];
  /** Distinct `$type`s in first-seen order. */
  types: string[];
  /** Parse/read failure the tree surfaces instead of an empty file. */
  error: string;
}

/** A leaf or group of the parsed document, as the tab renders it. */
export interface TokenNode {
  /** Dotted path from the document root ('' for the root group). */
  path: string;
  /** Segment under its parent ('' for the root group). */
  name: string;
  /** Group nodes nest; leaves are the DTCG tokens. */
  kind: 'group' | 'token';
  /** `$type` declared on a group — inherited by its otherwise-untyped leaves. */
  type: string;
  /** Tokens at or below this node (1 for a leaf), matching `parseTokensFile`. */
  tokenCount: number;
  children: TokenNode[];
}

export interface TokenTree {
  path: string;
  name: string;
  total: number;
  types: string[];
  nodes: TokenNode[];
}

interface RawNode {
  name: string;
  children: RawNode[];
  child: Map<string, RawNode>;
  leaf: boolean;
  type: string;
  /** Whether a `$type` key was present at all (an explicit '' is not a type). */
  hasType: boolean;
  value: unknown;
  description: string;
  extensions: unknown;
}

function rawNode(name: string): RawNode {
  return {
    name,
    children: [],
    child: new Map(),
    leaf: false,
    type: '',
    hasType: false,
    value: undefined,
    description: '',
    extensions: undefined,
  };
}

/** Attach a child, replacing a duplicate key in place (last wins, `setChild`). */
function setChild(parent: RawNode, child: RawNode): void {
  const existing = parent.child.get(child.name);
  if (existing) {
    Object.assign(existing, child);
    return;
  }
  parent.child.set(child.name, child);
  parent.children.push(child);
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/**
 * The structural keys of a `$value` object, as bare child nodes (no leaves):
 * `pkg/design/tokens_alias.go` resolves an alias that sits *inside* a
 * structured value (`border`'s `{"color": "{color.primary}"}`), so the tab
 * can show that reference resolving too. Returns null for a scalar/array
 * value or an object with no structural keys.
 */
function objectKeysAsChildren(value: unknown): RawNode | null {
  if (!isPlainObject(value)) return null;
  const synthetic = rawNode('');
  for (const key of Object.keys(value)) {
    if (key.startsWith('$')) continue;
    setChild(synthetic, rawNode(key));
  }
  return synthetic.children.length ? synthetic : null;
}

/**
 * Build one node from an object's members. Mirrors
 * `pkg/design/tokens_parse.go`'s `parseTokenObject`: `$`-prefixed keys are
 * metadata (`$value` marks a leaf, `$type` records the type, `$description`
 * and `$extensions` are carried, the rest ignored), any other key is a
 * structural child, and a leaf's `$value` is never descended into as tokens.
 */
function buildNode(name: string, object: Record<string, unknown>): RawNode {
  const node = rawNode(name);
  let sawValue = false;
  for (const [key, value] of Object.entries(object)) {
    if (key.startsWith('$')) continue;
    if (!isPlainObject(value)) continue;
    setChild(node, buildNode(key, value));
  }
  for (const [key, value] of Object.entries(object)) {
    if (key === '$value') {
      node.leaf = true;
      node.value = value;
      sawValue = true;
    } else if (key === '$type') {
      node.hasType = true;
      if (typeof value === 'string') node.type = value;
    } else if (key === '$description') {
      if (typeof value === 'string') node.description = value;
    } else if (key === '$extensions') {
      node.extensions = value;
    }
  }
  if (sawValue) {
    // A leaf is never walked as groups, but the keys of a structured $value
    // still resolve aliases (SP-140-1 §1a; tokens_alias.go). They are carried
    // as children so `collectLeafValues`' walk can see them, and the leaf
    // branch of `toTokenNode` ignores them for the tree shape.
    const metadata = objectKeysAsChildren(node.value);
    if (metadata) for (const child of metadata.children) setChild(node, child);
  }
  return node;
}

/**
 * Convert one parsed node into the tab's node shape. `inherited` carries the
 * nearest enclosing group's `$type`, which a leaf without its own `$type`
 * adopts (SP-140-1 §1a: only leaves must declare a type, and only *declared*
 * group types propagate — a group with no `$type` does not clear the outer
 * one).
 */
function toTokenNode(raw: RawNode, parentPath: string, inherited: string): TokenNode {
  const path = parentPath ? `${parentPath}.${raw.name}` : raw.name;
  const declared = raw.hasType ? raw.type : '';
  const type = declared || inherited;
  if (raw.leaf) {
    // A leaf is never descended into as groups (SP-140-1 §1a: $value makes a
    // node a token), so its `children` are the tokens themselves.
    return { path, name: raw.name, kind: 'token', type, tokenCount: 1, children: [] };
  }
  const children = raw.children.map((child) => toTokenNode(child, path, declared));
  return {
    path,
    name: raw.name,
    kind: 'group',
    type: declared,
    tokenCount: children.reduce((sum, child) => sum + child.tokenCount, 0),
    children,
  };
}

/**
 * Parse one `.tokens.json` document into an ordered tree. Never throws: a
 * document that is not a JSON object of groups yields `null` with a reason, so
 * the tab reports the file instead of rendering an empty tree.
 */
export function parseTokenTree(text: string, path: string, name: string): TokenTree | null {
  if (!text.trim()) return null;
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    return null;
  }
  if (!isPlainObject(data)) return null;

  const root: RawNode = rawNode('');
  for (const [key, value] of Object.entries(data)) {
    if (key.startsWith('$')) continue;
    if (!isPlainObject(value)) continue;
    setChild(root, buildNode(key, value));
  }
  if (root.children.length === 0) return null;

  const nodes = root.children.map((child) => toTokenNode(child, '', ''));
  const types = new Set<string>();
  // Walk again for the type list so it is first-seen ordered, like the parser.
  const collectTypes = (node: RawNode, inherited: string): void => {
    const declared = node.hasType ? node.type : '';
    const type = declared || inherited;
    if (node.leaf) {
      types.add(type || UNKNOWN_TOKEN_TYPE);
      return;
    }
    for (const child of node.children) collectTypes(child, declared);
  };
  for (const child of root.children) collectTypes(child, '');

  return {
    path,
    name,
    total: nodes.reduce((sum, node) => sum + node.tokenCount, 0),
    types: [...types],
    nodes,
  };
}

/** Flatten a token tree's leaves in document order. */
export function flattenTokens(tree: TokenTree | null | undefined): RawTokenRef[] {
  const out: RawTokenRef[] = [];
  if (!tree) return out;
  const walk = (node: TokenNode): void => {
    if (node.kind === 'token') {
      out.push({ node, file: tree });
      return;
    }
    for (const child of node.children) walk(child);
  };
  for (const node of tree.nodes) walk(node);
  return out;
}

/** One leaf with the file group it belongs to (the walk's carrier pair). */
export interface RawTokenRef {
  node: TokenNode;
  file: TokenTree;
}

interface RawLeafValue {
  value: unknown;
  description: string;
  extensions: unknown;
}

/**
 * Collect the `$value`/`$description`/`$extensions` of every leaf of a parsed
 * document into a path → metadata map. Needed because a `TokenNode` keeps only
 * the shape the tree renders; the tab reads the value from `designApi`'s
 * per-file read, which the model then pairs back up here.
 */
export function collectLeafValues(text: string, prefix = ''): Map<string, RawLeafValue> {
  const values = new Map<string, RawLeafValue>();
  if (!text.trim()) return values;
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    return values;
  }
  if (!isPlainObject(data)) return values;
  const walk = (object: Record<string, unknown>, base: string): void => {
    let leaf = false;
    let value: unknown;
    let description = '';
    let extensions: unknown;
    for (const [key, child] of Object.entries(object)) {
      if (key === '$value') {
        leaf = true;
        value = child;
      } else if (key === '$description' && typeof child === 'string') {
        description = child;
      } else if (key === '$extensions') {
        extensions = child;
      }
    }
    const path = base ? `${prefix ? `${prefix}.` : ''}${base}` : '';
    if (leaf) {
      values.set(path, { value, description, extensions });
      return;
    }
    for (const [key, child] of Object.entries(object)) {
      if (key.startsWith('$') || !isPlainObject(child)) continue;
      walk(child, base ? `${base}.${key}` : key);
    }
  };
  walk(data, '');
  return values;
}

/**
 * Resolve a token's value: its own `$value`, or the value of the token its
 * whole-value alias points at when the alias resolves *in the same file*
 * (SP-140-1 §1a: alias resolution is per-file). A dangling or cross-file alias
 * keeps the raw alias text — the detail pane shows it as such.
 */
export function resolveTokenValue(token: DesignToken, all: readonly DesignToken[]): unknown {
  if (typeof token.value !== 'string') return token.value;
  const alias = tokenAliasPath(token.value);
  if (!alias) return token.value;
  const target = all.find((candidate) => candidate.path === alias && candidate.filePath === token.filePath);
  return target ? target.value : token.value;
}

/**
 * Build the tab's token list: every leaf of the parsed file paired with the
 * values read from the same text. `path` is design-root-relative
 * (`tokens/color.tokens.json`), which is what the read and the rail agree on.
 */
export function tokenFileModel(path: string, text: string, name: string): DesignTokenFile {
  const stem = (name || path.split('/').pop() || path).replace(/\.tokens\.json$/, '');
  const tree = parseTokenTree(text, path, stem);
  if (!tree) {
    return { path, name: stem, tokens: [], types: [], error: 'Not a DTCG token document.' };
  }
  const values = collectLeafValues(text);
  const tokens: DesignToken[] = [];
  const walk = (node: TokenNode, inherited: string): void => {
    const type = node.kind === 'group' ? node.type || inherited : node.type;
    if (node.kind === 'token') {
      const meta = values.get(node.path);
      const value = meta ? meta.value : undefined;
      tokens.push({
        path: node.path,
        name: node.name,
        type: type || UNKNOWN_TOKEN_TYPE,
        section: sectionForType(type || UNKNOWN_TOKEN_TYPE),
        value,
        valueText: formatTokenValue(value),
        alias: tokenAliasPath(value),
        description: meta?.description ?? '',
        extensions: meta?.extensions,
        filePath: path,
        fileName: stem,
      });
      return;
    }
    for (const child of node.children) walk(child, type);
  };
  for (const node of tree.nodes) walk(node, '');

  return {
    path,
    name: stem,
    tokens,
    types: tree.types.length ? tree.types : [...new Set(tokens.map((token) => token.type))],
    error: '',
  };
}

// The view helpers (grouping, search, schema hints) live in `tokensView.ts`
// so both modules stay under the 500-line rule; re-exported here so
// `designTokens` remains one import site for the tab.
export {
  allTokens,
  anchorForToken,
  filterTokens,
  groupTokens,
  schemaHintForToken,
  tokenSchemaText,
} from './tokensView';
export type { TokenGrouping } from './tokensView';
