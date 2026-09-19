/**
 * SP-140-3 item 3.8 — Tokens tab model.
 *
 * Unit coverage for the pure half of the tab: `designTokens.ts` (DTCG parse →
 * grouped tree, leaf-value collection, alias recognition/resolution) and
 * `tokensView.ts` (grouping, path filter, schema hints), plus the
 * `TokenSpecimens.tsx` inline-style helpers that paint token *values* (no
 * color literal is written anywhere).
 *
 * The component-facing tests — grouped rendering, swatches, specimens, search,
 * and the open-file hand-off — live in `TokensTree.test.tsx`.
 */

import { describe, expect, it } from 'vitest';
import {
  TOKEN_SECTION_LABELS,
  collectLeafValues,
  filterTokens,
  formatTokenValue,
  groupTokens,
  parseTokenTree,
  resolveTokenValue,
  schemaHintForToken,
  sectionForType,
  tokenAliasPath,
  tokenFileModel,
  tokenSchemaText,
} from './designTokens';
import { fontFamilies, spaceBarStyle, specimenWeight, swatchStyle } from './TokenSpecimens';
import { tokenFileName, tokenRelativePath } from './TokensTree';

/** A representative `*.tokens.json` tier with every specimen-bearing type. */
const COLOR_TOKENS = JSON.stringify(
  {
    color: {
      $description: 'brand palette',
      brand: {
        primary: { $type: 'color', $value: '#0055ff' },
        accent: { $type: 'color', $value: '#ffaa00' },
      },
      text: { $type: 'color', $value: '{color.brand.primary}' },
    },
    spacing: {
      sm: { $type: 'dimension', $value: '4px' },
      lg: { $type: 'dimension', $value: '24px' },
    },
    font: {
      body: { $type: 'fontFamily', $value: 'Inter, system-ui' },
      weightBold: { $type: 'fontWeight', $value: 700 },
    },
  },
  null,
  2,
);

/** A second tier (motion) exercising the "other" section and plain values. */
const MOTION_TOKENS = JSON.stringify(
  {
    duration: { fast: { $type: 'duration', $value: '120ms' } },
    border: {
      card: { $type: 'border', $value: { color: '{color.brand.primary}', width: '1px', style: 'solid' } },
    },
  },
  null,
  2,
);

/** Fixture text keyed by the inventory's workspace-relative path. */
const CONTENT: Record<string, string> = {
  'design/tokens/color.tokens.json': COLOR_TOKENS,
  'design/tokens/motion.tokens.json': MOTION_TOKENS,
};

const tokensOf = (path: string) =>
  tokenFileModel(
    path,
    CONTENT[path],
    path
      .split('/')
      .pop()!
      .replace(/\.tokens\.json$/, ''),
  );

describe('designTokens model', () => {
  it('parses a DTCG document into an ordered group tree', () => {
    const tree = parseTokenTree(COLOR_TOKENS, 'tokens/color.tokens.json', 'color');
    expect(tree).not.toBeNull();
    expect(tree!.total).toBe(7);
    expect(tree!.nodes.map((node) => node.name)).toEqual(['color', 'spacing', 'font']);
    expect(tree!.nodes[0].kind).toBe('group');
    expect(tree!.nodes[0].tokenCount).toBe(3);
    expect(tree!.nodes[0].children[0].path).toBe('color.brand');
    expect(tree!.nodes[0].children[0].children[0].path).toBe('color.brand.primary');
  });

  it('marks leaves as tokens and keeps $metadata out of the tree', () => {
    const tree = parseTokenTree(COLOR_TOKENS, 'tokens/color.tokens.json', 'color')!;
    const brand = tree.nodes[0].children[0];
    expect(brand.children.map((node) => node.kind)).toEqual(['token', 'token']);
    // `$description` on the color group is not a child.
    expect(tree.nodes[0].children.map((node) => node.name)).toEqual(['brand', 'text']);
  });

  it('rejects a non-object or unparsable document without throwing', () => {
    expect(parseTokenTree('not json', 'tokens/x.tokens.json', 'x')).toBeNull();
    expect(parseTokenTree('[1,2]', 'tokens/x.tokens.json', 'x')).toBeNull();
    expect(parseTokenTree('', 'tokens/x.tokens.json', 'x')).toBeNull();
  });

  it('derives a section per type: color, typography, spacing, other', () => {
    expect(sectionForType('color')).toBe('color');
    expect(sectionForType('fontFamily')).toBe('typography');
    expect(sectionForType('fontWeight')).toBe('typography');
    expect(sectionForType('number')).toBe('typography');
    expect(sectionForType('dimension')).toBe('spacing');
    expect(sectionForType('duration')).toBe('other');
    expect(sectionForType('border')).toBe('other');
    expect(TOKEN_SECTION_LABELS.spacing).toBe('Spacing');
  });

  it('builds the tab token list with values, types, and files', () => {
    const file = tokensOf('design/tokens/color.tokens.json');
    expect(file.error).toBe('');
    expect(file.name).toBe('color');
    expect(file.tokens.map((token) => token.path)).toEqual([
      'color.brand.primary',
      'color.brand.accent',
      'color.text',
      'spacing.sm',
      'spacing.lg',
      'font.body',
      'font.weightBold',
    ]);
    const primary = file.tokens[0];
    expect(primary.type).toBe('color');
    expect(primary.value).toBe('#0055ff');
    expect(primary.valueText).toBe('#0055ff');
    expect(primary.section).toBe('color');
    expect(primary.filePath).toBe('design/tokens/color.tokens.json');
    expect(primary.fileName).toBe('color');
  });

  it('collects leaf values and metadata for every leaf', () => {
    const values = collectLeafValues(COLOR_TOKENS);
    expect(values.size).toBe(7);
    expect(values.get('color.brand.primary')).toMatchObject({ value: '#0055ff' });
    expect(values.get('font.body')).toMatchObject({ value: 'Inter, system-ui' });
    expect(values.get('font.weightBold')?.value).toBe(700);
  });

  it('recognises whole-value aliases only, per tokens_alias.go', () => {
    expect(tokenAliasPath('{color.brand.primary}')).toBe('color.brand.primary');
    expect(tokenAliasPath('{ a.b }')).toBe(' a.b ');
    expect(tokenAliasPath('{a} / {b}')).toBeNull();
    expect(tokenAliasPath('{a{b}}')).toBeNull();
    expect(tokenAliasPath('#0055ff')).toBeNull();
    expect(tokenAliasPath(42)).toBeNull();
  });

  it('resolves an alias within the same file and leaves a dangling one raw', () => {
    const file = tokensOf('design/tokens/color.tokens.json');
    const text = file.tokens.find((token) => token.path === 'color.text')!;
    expect(text.alias).toBe('color.brand.primary');
    expect(resolveTokenValue(text, file.tokens)).toBe('#0055ff');

    const dangling = { ...text, value: '{color.nope}' };
    expect(resolveTokenValue(dangling, file.tokens)).toBe('{color.nope}');
  });

  it('filters by token path substring, case-insensitively', () => {
    const all = [
      ...tokensOf('design/tokens/color.tokens.json').tokens,
      ...tokensOf('design/tokens/motion.tokens.json').tokens,
    ];
    expect(filterTokens(all, '')).toHaveLength(all.length);
    expect(filterTokens(all, '   ')).toHaveLength(all.length);
    expect(filterTokens(all, 'color.brand').map((t) => t.path)).toEqual(['color.brand.primary', 'color.brand.accent']);
    expect(filterTokens(all, 'SPACING').map((t) => t.path)).toEqual(['spacing.sm', 'spacing.lg']);
    expect(filterTokens(all, 'font.body').map((t) => t.path)).toEqual(['font.body']);
    expect(filterTokens(all, 'nothing.matches')).toHaveLength(0);
  });

  it('groups filtered tokens by file then section, keeping canonical order', () => {
    const files = [tokensOf('design/tokens/color.tokens.json'), tokensOf('design/tokens/motion.tokens.json')];
    const all = files.flatMap((file) => file.tokens);

    const groups = groupTokens(files, all);
    expect(groups.map((group) => group.file.name)).toEqual(['color', 'motion']);
    expect(groups[0].sections.map((section) => section.section)).toEqual(['color', 'typography', 'spacing']);
    expect(groups[1].sections.map((section) => section.section)).toEqual(['other']);

    // A filter that empties a file drops it from the grouping entirely.
    const narrowed = groupTokens(files, filterTokens(all, 'spacing.lg'));
    expect(narrowed).toHaveLength(1);
    expect(narrowed[0].sections.map((section) => section.tokens.map((t) => t.path))).toEqual([['spacing.lg']]);
  });

  it('formats structured values as JSON rather than [object Object]', () => {
    expect(formatTokenValue('4px')).toBe('4px');
    expect(formatTokenValue(700)).toBe('700');
    expect(formatTokenValue({ color: '#000', width: '1px' })).toBe('{"color":"#000","width":"1px"}');
    expect(formatTokenValue([0.3, 0, 0, 1])).toBe('[0.3,0,0,1]');
    expect(formatTokenValue(undefined)).toBe('');
  });

  it('carries the §1a schema hint and a per-token hint naming the type', () => {
    const schema = tokenSchemaText();
    expect(schema).toContain('DTCG token schema');
    expect(schema).toContain('color, dimension, fontFamily, fontWeight, number, duration, cubicBezier');
    expect(schema).toContain('{group.token}');
    const token = tokensOf('design/tokens/color.tokens.json').tokens[0];
    expect(schemaHintForToken(token)).toBe('color.brand.primary — $type color ⇒ JSON color');
  });

  it('reports an unreadable file instead of inventing tokens', () => {
    const file = tokenFileModel('design/tokens/broken.tokens.json', '{"oops": ', 'broken');
    expect(file.tokens).toEqual([]);
    expect(file.error).toBe('Not a DTCG token document.');
  });

  it('maps inventory paths onto the design-root-relative read path', () => {
    expect(tokenRelativePath('design/tokens/color.tokens.json')).toBe('tokens/color.tokens.json');
    expect(tokenRelativePath('tokens/color.tokens.json')).toBe('tokens/color.tokens.json');
    expect(tokenFileName({ path: 'design/tokens/color.tokens.json', name: 'color.tokens.json' })).toBe('color');
  });
});

describe('TokenSpecimens', () => {
  it('paints a swatch with the token value itself', () => {
    expect(swatchStyle('#0055ff')).toEqual({ background: '#0055ff' });
    expect(swatchStyle('')).toEqual({ background: 'transparent' });
    expect(swatchStyle(undefined)).toEqual({ background: 'transparent' });
  });

  it('sizes a spacing bar from the dimension value', () => {
    expect(spaceBarStyle('24px')).toEqual({ width: '24px' });
    expect(spaceBarStyle('4px')).toEqual({ width: '4px' });
    expect(spaceBarStyle('auto')).toEqual({});
  });

  it('splits font families and picks a numeric weight', () => {
    expect(fontFamilies('Inter, system-ui')).toEqual(['Inter', 'system-ui']);
    expect(fontFamilies('"IBM Plex Sans", sans-serif')).toEqual(['IBM Plex Sans', 'sans-serif']);
    expect(fontFamilies('')).toEqual([]);
    expect(specimenWeight(700)).toBe(700);
    expect(specimenWeight('700')).toBe(700);
    expect(specimenWeight('bold')).toBe(400);
  });
});
