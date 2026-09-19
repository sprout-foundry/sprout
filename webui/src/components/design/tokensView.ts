/**
 * Design token view helpers — grouping, search, and the detail pane's schema
 * hints (SP-140-3 §3d).
 *
 * The rendering half of the token model: `designTokens.ts` parses a document
 * into tokens, this module turns that list into the tree the tab draws (file →
 * section → group), filters it by token path, and supplies the JSON schema
 * hint / editor anchor the detail pane uses. Split out so both modules stay
 * under the AGENTS.md 500-line rule; both are pure and import-free apart from
 * the parsed-token types.
 */

import type { DesignToken, DesignTokenFile, TokenSection } from './designTokens';
import { TOKEN_SECTION_LABELS, TOKEN_SECTIONS, TOKEN_TYPES } from './designTokens';

/** Every token of a workspace's token files, in file then document order. */
export function allTokens(files: readonly DesignTokenFile[]): DesignToken[] {
  return files.flatMap((file) => file.tokens);
}

/**
 * Filter tokens by a path substring (case-insensitive), the §3d
 * "search/filter across token paths". An empty or whitespace query keeps every
 * token. The match is on the dotted path — including the group segments — so
 * "color.brand" narrows to one group and "primary" to every primary token.
 */
export function filterTokens(tokens: readonly DesignToken[], query: string): DesignToken[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return [...tokens];
  return tokens.filter((token) => token.path.toLowerCase().includes(needle));
}

/**
 * Group filtered tokens for rendering: file → section → tokens. Sections and
 * files keep their canonical order so the tree does not reshuffle as the query
 * narrows.
 */
export interface TokenGrouping {
  file: DesignTokenFile;
  sections: { section: TokenSection; label: string; tokens: DesignToken[] }[];
}

export function groupTokens(files: readonly DesignTokenFile[], tokens: readonly DesignToken[]): TokenGrouping[] {
  const groups: TokenGrouping[] = [];
  for (const file of files) {
    const ofFile = tokens.filter((token) => token.filePath === file.path);
    if (ofFile.length === 0) continue;
    const sections = TOKEN_SECTIONS.map((section) => ({
      section,
      label: TOKEN_SECTION_LABELS[section],
      tokens: ofFile.filter((token) => token.section === section),
    })).filter((entry) => entry.tokens.length > 0);
    groups.push({ file, sections });
  }
  return groups;
}

/**
 * The schema hint backing the detail pane's JSON validation (SP-140-1 §1a /
 * §3d). A compact, non-throwing description of the DTCG shape this document
 * must satisfy — the same rule set `pkg/design/tokens.go` enforces, and the
 * allowed `$type` list verbatim.
 */
export function tokenSchemaText(fileLabel = 'tokens/<tier>.tokens.json'): string {
  return [
    `DTCG token schema — ${fileLabel} (SP-140-1 §1a)`,
    '',
    '• Top level: an object of token groups.',
    '• A group: nested objects, optionally `$type`/`$description`.',
    '• A token: { "$value": <any JSON>, "$type": <allowed type>, "$description"?, "$extensions"? }.',
    `• Allowed $type: ${TOKEN_TYPES.join(', ')}.`,
    'Any other $type, a $type without $value, or $value alongside child groups is invalid.',
    '• Aliases: a whole value of the form `{group.token}` must resolve in the same file; cycles and dangling paths are invalid.',
    '• $extensions passes through untouched; consumers glob the directory (no index.json).',
  ].join('\n');
}

/** One schema-hint line for a token path, naming its `$type` and JSON shape. */
export function schemaHintForToken(token: DesignToken): string {
  return `${token.path} — $type ${token.type} ⇒ JSON ${singularShape(token.type)}`;
}

/** The JSON shape a `$type` names, for the hint (`fontFamily` → `fontFamily`). */
function singularShape(type: string): string {
  return type.endsWith('s') ? type.slice(0, -1) : type;
}

/** The `#L<n>` editor anchor for a token path, when the file has been read. */
export function anchorForToken(filePath: string, text: string, tokenPath: string): string {
  if (!text) return filePath;
  const lines = text.split('\n');
  const leaf = tokenPath.split('.').pop() ?? tokenPath;
  const index = lines.findIndex(
    (line) => tokenPath.split('.').every((segment) => line.includes(segment)) || line.includes(`"${leaf}"`),
  );
  return index < 0 ? filePath : `${filePath}#L${index + 1}`;
}
