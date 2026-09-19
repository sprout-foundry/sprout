/**
 * Token specimen surfaces for the Tokens tab (SP-140-3 §3d).
 *
 * The rendering half of the tab's type-specific specimens — color swatches,
 * typography samples, spacing bars, and the plain fallback for the remaining
 * charter types — plus the grouped tree's rows and section groups. Split from
 * `TokensTree.tsx` so both files stay under the AGENTS.md 500-line rule, and
 * kept free of transport/state so the tree body owns the read/select wiring.
 *
 * A token *value* is data: a swatch paints it with an inline `background`, a
 * font specimen with inline `fontFamily`/`fontWeight`, a spacing bar with an
 * inline `width`. No color literal is ever written in code or CSS (the
 * no-raw-hex house rule) — only the values that come from the `.tokens.json`.
 */

import { useMemo, type CSSProperties } from 'react';
import type { DesignToken, TokenSection } from './designTokens';

/** Inline style for a color swatch — the value is data, never a CSS literal. */
export function swatchStyle(value: unknown): CSSProperties {
  return { background: typeof value === 'string' && value ? value : 'transparent' };
}

/** Inline style for a spacing specimen bar: its width is the token's value. */
export function spaceBarStyle(value: unknown): CSSProperties {
  const text = typeof value === 'string' || typeof value === 'number' ? String(value) : '';
  const match = /^([0-9]*\.?[0-9]+)/.exec(text.trim());
  return match ? { width: `${match[1]}px` } : {};
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

/** The `color` swatch with the token's own value beside it. */
function ColorSpecimen({ value }: { value: unknown }) {
  return (
    <span className="design-tokens-value" data-testid="token-specimen-color">
      <span className="design-token-swatch" style={swatchStyle(value)} data-testid="token-swatch" aria-hidden="true" />
      {String(value ?? '')}
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
          Ag
        </span>
      ))}
      <span className="design-token-font-meta" data-testid="token-font-meta">
        {families.length ? families.join(', ') : valueText} · w{weight}
      </span>
    </span>
  );
}

/** Spacing specimen: a bar whose width is the token's own dimension value. */
function SpacingSpecimen({ value, valueText }: { value: unknown; valueText: string }) {
  return (
    <span className="design-tokens-value" data-testid="token-specimen-spacing">
      <span
        className="design-token-space-bar"
        style={spaceBarStyle(value)}
        data-testid="token-space-bar"
        aria-hidden="true"
      />
      {valueText}
    </span>
  );
}

/**
 * The specimen for a token, chosen by section: color swatch, typography,
 * spacing, and a plain value line for the remaining charter types (motion,
 * borders, numbers) rather than inventing a specimen for them.
 */
export function TokenSpecimen({ token }: { token: DesignToken }) {
  if (token.section === 'color') return <ColorSpecimen value={token.value} />;
  if (token.section === 'typography') {
    return (
      <TypographySpecimen
        families={fontFamilies(token.value)}
        weight={specimenWeight(token.value)}
        valueText={token.valueText}
      />
    );
  }
  if (token.section === 'spacing') return <SpacingSpecimen value={token.value} valueText={token.valueText} />;
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
}

/** One token row: path, specimen, type chip — a button reporting the pick. */
export function TokenRow({ token, selected, onSelect }: TokenRowProps) {
  return (
    <li>
      <button
        type="button"
        className={`design-tokens-row${selected ? ' selected' : ''}`}
        data-testid={`design-token-row-${token.fileName}-${token.path}`}
        data-token-path={token.path}
        data-token-type={token.type}
        data-token-section={token.section}
        data-token-file={token.filePath}
        onClick={() => onSelect(token)}
        title={token.path}
      >
        <span className="design-tokens-path">{token.path}</span>
        <TokenSpecimen token={token} />
        <span className="design-tokens-type" data-testid={`design-token-type-${token.fileName}-${token.path}`}>
          {token.type}
        </span>
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
          <ul className="design-tokens-list">
            {members.map((token) => (
              <TokenRow
                key={`${token.filePath}:${token.path}`}
                token={token}
                selected={selected === `${token.filePath}:${token.path}`}
                onSelect={onSelect}
              />
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}
