/**
 * Token specimen surfaces for the Tokens tab (SP-140-3 §3d).
 *
 * The rendering half of the tab's type-specific specimens — color tiles,
 * typography samples, proportional spacing bars, and the plain fallback for
 * the remaining charter types — plus the grouped tree's rows and section
 * groups. Split from `TokensTree.tsx` so both files stay under the AGENTS.md
 * 500-line rule, and kept free of transport/state so the tree body owns the
 * read/select wiring.
 *
 * A token *value* is data: a swatch paints it with an inline `background`, a
 * font specimen with inline `fontFamily`/`fontWeight`, a spacing bar with an
 * inline percentage of the section's shared scale. No color literal is ever
 * written in code or CSS (the no-raw-hex house rule) — only the values that
 * come from the `.tokens.json`.
 */

import { useMemo, type CSSProperties } from 'react';
import type { DesignToken, TokenSection } from './designTokens';

/** Inline style for a color swatch — the value is data, never a CSS literal. */
export function swatchStyle(value: unknown): CSSProperties {
  return { background: typeof value === 'string' && value ? value : 'transparent' };
}

/**
 * Inline style for the spacing specimen bar. When `scaleMax` (the section's
 * largest dimension) is given the bar fills a percentage of the shared scale
 * so the relative rhythm reads at a glance; otherwise it falls back to the
 * value's own pixel width.
 */
export function spaceBarStyle(value: unknown, scaleMax?: number | null): CSSProperties {
  const text = typeof value === 'string' || typeof value === 'number' ? String(value) : '';
  const match = /^([0-9]*\.?[0-9]+)/.exec(text.trim());
  if (!match) return {};
  const px = Number(match[1]);
  if (scaleMax && scaleMax > 0) return { width: `${Math.min(100, (px / scaleMax) * 100)}%` };
  return { width: `${px}px` };
}

/** The largest leading-numeric dimension value among the tokens (null when none parses). */
export function maxDimensionValue(tokens: readonly DesignToken[]): number | null {
  let max: number | null = null;
  for (const token of tokens) {
    const match = /^([0-9]*\.?[0-9]+)/.exec(token.valueText.trim());
    if (!match) continue;
    const px = Number(match[1]);
    if (max === null || px > max) max = px;
  }
  return max;
}

/** The distinct font families a `fontFamily` value names. */
export function fontFamilies(value: unknown): string[] {
  if (typeof value !== 'string' || !value.trim()) return [];
  return value
    .split(',')
    .map((part) => part.trim().replace(/^['"]|['"]$/g, ''))
    .filter(Boolean);
}

/** Numeric `fontWeight`/`number` values, for the specimen's weight class. */
function numericValue(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && /^[0-9]+$/.test(value.trim())) return Number(value.trim());
  return null;
}

/** A specimen's font weight: a numeric value where one exists, else 400. */
export function specimenWeight(value: unknown): number {
  return numericValue(value) ?? 400;
}

/** The `color` tile: a large swatch over the value — the palette reads at a glance. */
function ColorSpecimen({ value, valueText }: { value: unknown; valueText: string }) {
  return (
    <span className="design-tokens-tile-specimen" data-testid="token-specimen-color">
      <span
        className="design-token-swatch design-token-swatch-lg"
        style={swatchStyle(value)}
        data-testid="token-swatch"
        aria-hidden="true"
      />
      <span className="design-tokens-tile-value">{valueText}</span>
    </span>
  );
}

/** Typography specimen: the family's faces plus size/weight/scale metadata. */
function TypographySpecimen({
  families,
  weight,
  valueText,
}: {
  families: string[];
  weight: number;
  valueText: string;
}) {
  return (
    <span className="design-token-font-specimen" data-testid="token-specimen-typography">
      {families.map((family) => (
        <span
          key={family}
          className="design-token-font-line"
          style={{ fontFamily: family, fontWeight: weight }}
          data-testid="token-font-line"
        >
          Aa
        </span>
      ))}
      <span className="design-token-font-meta" data-testid="token-font-meta">
        {families.length ? families.join(', ') : valueText} · w{weight}
      </span>
    </span>
  );
}

/** Spacing specimen: a proportional bar on a full-width track (the section's shared scale). */
function SpacingSpecimen({
  value,
  valueText,
  scaleMax,
}: {
  value: unknown;
  valueText: string;
  scaleMax: number | null;
}) {
  return (
    <span className="design-tokens-value design-token-space-specimen" data-testid="token-specimen-spacing">
      <span className="design-token-space-track" aria-hidden="true">
        <span className="design-token-space-bar" style={spaceBarStyle(value, scaleMax)} data-testid="token-space-bar" />
      </span>
      <span className="design-tokens-tile-value">{valueText}</span>
    </span>
  );
}

/**
 * The specimen for a token, chosen by section: color tile, typography,
 * proportional spacing, and a plain value line for the remaining charter
 * types (motion, borders, numbers) rather than inventing a specimen for them.
 */
export function TokenSpecimen({ token, scaleMax = null }: { token: DesignToken; scaleMax?: number | null }) {
  if (token.section === 'color') return <ColorSpecimen value={token.value} valueText={token.valueText} />;
  if (token.section === 'typography') {
    return (
      <TypographySpecimen
        families={fontFamilies(token.value)}
        weight={specimenWeight(token.value)}
        valueText={token.valueText}
      />
    );
  }
  if (token.section === 'spacing') {
    return <SpacingSpecimen value={token.value} valueText={token.valueText} scaleMax={scaleMax} />;
  }
  return (
    <span className="design-tokens-value" data-testid="token-specimen-value">
      {token.valueText}
    </span>
  );
}

interface TokenRowProps {
  token: DesignToken;
  selected: boolean;
  onSelect: (token: DesignToken) => void;
  /** The section's shared spacing scale (largest dimension), for proportional bars. */
  scaleMax?: number | null;
}

/**
 * One token: color tokens render as a tile (name, swatch, value); every other
 * section renders a row (short name, specimen, type chip). The full dotted
 * path lives in the title and the detail pane.
 */
export function TokenRow({ token, selected, onSelect, scaleMax = null }: TokenRowProps) {
  const tile = token.section === 'color';
  return (
    <li>
      <button
        type="button"
        className={`design-tokens-row${tile ? ' design-tokens-tile' : ''}${selected ? ' selected' : ''}`}
        data-testid={`design-token-row-${token.fileName}-${token.path}`}
        data-token-path={token.path}
        data-token-type={token.type}
        data-token-section={token.section}
        data-token-file={token.filePath}
        onClick={() => onSelect(token)}
        title={token.path}
      >
        <span className={tile ? 'design-tokens-tile-name' : 'design-tokens-name'}>{token.name}</span>
        <TokenSpecimen token={token} scaleMax={scaleMax} />
        {!tile && (
          <span className="design-tokens-type" data-testid={`design-token-type-${token.fileName}-${token.path}`}>
            {token.type}
          </span>
        )}
      </button>
    </li>
  );
}

/** Tokens of one section, grouped one level deeper by their parent group. */
export function SectionGroup({
  section,
  label,
  tokens,
  selected,
  onSelect,
}: {
  section: TokenSection;
  label: string;
  tokens: DesignToken[];
  selected: string | null;
  onSelect: (token: DesignToken) => void;
}) {
  const byGroup = useMemo(() => {
    const buckets = new Map<string, DesignToken[]>();
    for (const token of tokens) {
      const group = token.path.split('.').slice(0, -1).join('.') || token.path;
      const list = buckets.get(group);
      if (list) list.push(token);
      else buckets.set(group, [token]);
    }
    return [...buckets.entries()];
  }, [tokens]);

  const scaleMax = useMemo(() => (section === 'spacing' ? maxDimensionValue(tokens) : null), [section, tokens]);

  return (
    <div
      className="design-tokens-section"
      data-testid={`design-token-section-${section}`}
      data-section-count={tokens.length}
    >
      <h4 className="design-tokens-section-title">{label}</h4>
      {byGroup.map(([group, members]) => (
        <div className="design-tokens-group" key={group} data-testid={`design-token-group-${group}`}>
          <h5 className="design-tokens-group-title">{group}</h5>
          <ul className={section === 'color' ? 'design-tokens-list design-tokens-grid' : 'design-tokens-list'}>
            {members.map((token) => (
              <TokenRow
                key={`${token.filePath}:${token.path}`}
                token={token}
                selected={selected === `${token.filePath}:${token.path}`}
                onSelect={onSelect}
                scaleMax={scaleMax}
              />
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}
