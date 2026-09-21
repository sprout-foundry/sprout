/**
 * SP-140-3 item 3.8 — Tokens tab (component).
 *
 * Covers the §3d rendering contract against a fixture DTCG document: the
 * `*.tokens.json` files render grouped (file → section → token group → token),
 * `color` tokens carry a swatch painted with their own value, typography and
 * spacing tokens render specimens, the search box filters by token path, and
 * clicking a token opens its `.tokens.json` through the select/open-file
 * callbacks (read-only — the pane has no editor).
 *
 * The pure model (parse/group/filter/schema) is covered by
 * `designTokens.test.ts`; this file owns the DOM. `designApi` is mocked for the
 * file-read path (`readAsset`) while the real pure parsers stay in play, per
 * the ScreensGrid.test / FlowsCanvas.test convention. Assertions use the
 * suite's plain-expect style (`expect(...)`) rather than
 * `@testing-library/jest-dom` matchers, matching FlowsCanvas.test.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import { buildInventory, readAsset } from '../../services/api/designApi';
import type { DesignInventory, FilesResponse } from '../../services/api/types';
import TokensTree, { TokensTabContainer } from './TokensTree';

vi.mock('../../services/api/designApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('../../services/api/designApi');
  return { ...actual, readAsset: vi.fn() };
});

const mockedReadAsset = vi.mocked(readAsset);

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

/**
 * The workspace file list as `/api/files` returns it. Paths are
 * workspace-relative; `buildInventory` keeps the `design/` prefix on the entry
 * path and classifies `tokens/*.tokens.json` as the `tokens` kind.
 */
function filesResponse(): FilesResponse {
  const paths = ['design/README.md', 'design/tokens/color.tokens.json', 'design/tokens/motion.tokens.json'];
  return { files: paths.map((path) => ({ path, size: 0, modified: 0 })) } as FilesResponse;
}

/** The inventory the shell hands the tab (token files classified). */
function fixtureInventory(): DesignInventory {
  return buildInventory(filesResponse());
}

/** Fixture text keyed by the inventory's workspace-relative path. */
const CONTENT: Record<string, string> = {
  'design/tokens/color.tokens.json': COLOR_TOKENS,
  'design/tokens/motion.tokens.json': MOTION_TOKENS,
};

function renderTree(props: Partial<React.ComponentProps<typeof TokensTree>> = {}) {
  return render(
    <SproutAdapterProvider>
      <TokensTree inventory={fixtureInventory()} contentByPath={CONTENT} {...props} />
    </SproutAdapterProvider>,
  );
}

describe('TokensTree grouped tree', () => {
  it('renders one file section per token file', () => {
    renderTree();
    expect(screen.getByTestId('design-tokens-tree').getAttribute('data-token-count')).toBe('9');
    expect(screen.getByTestId('design-token-file-color')).toBeTruthy();
    expect(screen.getByTestId('design-token-file-motion')).toBeTruthy();
    expect(screen.getByTestId('design-token-file-count-color').textContent).toContain('7 tokens');
    expect(screen.getByTestId('design-token-file-count-motion').textContent).toContain('2 tokens');
  });

  it('groups tokens under their section and parent group', () => {
    renderTree();
    expect(screen.getByText('Color')).toBeTruthy();
    expect(screen.getByText('Typography')).toBeTruthy();
    expect(screen.getByText('Spacing')).toBeTruthy();
    expect(screen.getByText('Other')).toBeTruthy();
    expect(screen.getByTestId('design-token-group-color.brand')).toBeTruthy();
    expect(screen.getByTestId('design-token-group-spacing')).toBeTruthy();
  });

  it('renders a row per token with its path and type chip', () => {
    renderTree();
    expect(screen.getByTestId('design-token-row-color-color.brand.primary')).toBeTruthy();
    expect(screen.getByTestId('design-token-type-color-color.brand.primary').textContent).toBe('color');
    expect(screen.getByTestId('design-token-type-motion-border.card').textContent).toBe('border');
  });

  it('renders the empty state for a workspace without tokens', () => {
    render(
      <SproutAdapterProvider>
        <TokensTree inventory={{ ...fixtureInventory(), tokenFiles: [] }} contentByPath={CONTENT} />
      </SproutAdapterProvider>,
    );
    expect(screen.getByTestId('design-tokens-tree').getAttribute('data-inventory')).toBe('empty');
    expect(screen.getByText('No token files in this workspace.')).toBeTruthy();
  });

  it('renders the missing-tree state for a workspace without design/', () => {
    render(
      <SproutAdapterProvider>
        <TokensTree inventory={{ ...fixtureInventory(), exists: false }} contentByPath={CONTENT} />
      </SproutAdapterProvider>,
    );
    expect(screen.getByTestId('design-tokens-tree').getAttribute('data-inventory')).toBe('missing');
  });
});

describe('TokensTree color swatches', () => {
  it('renders a swatch painted with the color token value', () => {
    renderTree();
    const row = screen.getByTestId('design-token-row-color-color.brand.primary');
    const swatches = row.querySelectorAll('[data-testid="token-swatch"]');
    expect(swatches.length).toBe(1);
    expect((swatches[0] as HTMLElement).style.background).toBe('rgb(0, 85, 255)');
    // The value is displayed next to the swatch.
    expect(row.querySelector('[data-testid="token-specimen-color"]')!.textContent).toContain('#0055ff');
  });

  it('renders every color token with a swatch and no other type with one', () => {
    renderTree();
    expect(screen.getAllByTestId('token-swatch')).toHaveLength(3);
  });
});

describe('TokensTree typography and spacing specimens', () => {
  it('renders a font specimen per family plus weight metadata', () => {
    renderTree();
    const body = screen.getByTestId('design-token-row-color-font.body');
    const lines = body.querySelectorAll('[data-testid="token-font-line"]');
    expect(lines.length).toBe(2);
    expect((lines[0] as HTMLElement).style.fontFamily).toBe('Inter');
    expect((lines[1] as HTMLElement).style.fontFamily).toBe('system-ui');
    expect(body.querySelector('[data-testid="token-font-meta"]')!.textContent).toContain('Inter, system-ui');
  });

  it('carries a numeric font weight into the specimen', () => {
    renderTree();
    const bold = screen.getByTestId('design-token-row-color-font.weightBold');
    const meta = bold.querySelector('[data-testid="token-font-meta"]')!;
    expect(meta.textContent).toContain('w700');
  });

  it('renders a spacing bar sized from the dimension value', () => {
    renderTree();
    const row = screen.getByTestId('design-token-row-color-spacing.lg');
    const bar = row.querySelector('[data-testid="token-space-bar"]') as HTMLElement;
    expect(bar.style.width).toBe('24px');
    expect(row.querySelector('[data-testid="token-specimen-spacing"]')!.textContent).toContain('24px');
  });

  it('falls back to a plain value line for the remaining charter types', () => {
    renderTree();
    const border = screen.getByTestId('design-token-row-motion-border.card');
    expect(border.querySelector('[data-testid="token-specimen-value"]')!.textContent).toContain(
      '{color.brand.primary}',
    );
    const duration = screen.getByTestId('design-token-row-motion-duration.fast');
    expect(duration.textContent).toContain('120ms');
  });
});

describe('TokensTree search/filter', () => {
  it('filters the tree by token path substring', () => {
    renderTree();
    const search = screen.getByTestId('design-tokens-search');

    fireEvent.change(search, { target: { value: 'spacing' } });
    expect(screen.getByTestId('design-tokens-tree').getAttribute('data-visible-count')).toBe('2');
    expect(screen.getByTestId('design-token-row-color-spacing.sm')).toBeTruthy();
    expect(screen.queryByTestId('design-token-row-color-color.brand.primary')).toBeNull();
    // The motion file has no spacing tokens, so its section drops out.
    expect(screen.queryByTestId('design-token-file-motion')).toBeNull();
  });

  it('matches case-insensitively across group segments', () => {
    renderTree();
    fireEvent.change(screen.getByTestId('design-tokens-search'), { target: { value: 'BRAND' } });
    expect(screen.getByTestId('design-tokens-tree').getAttribute('data-visible-count')).toBe('2');
    expect(screen.getByTestId('design-token-row-color-color.brand.accent')).toBeTruthy();
  });

  it('shows an empty state when nothing matches and restores on clear', () => {
    renderTree();
    const search = screen.getByTestId('design-tokens-search');
    fireEvent.change(search, { target: { value: 'nope.nothing' } });
    expect(screen.getByTestId('design-tokens-empty').textContent).toContain('nope.nothing');
    expect(screen.getByTestId('design-tokens-count').textContent).toBe('0 of 9 tokens');

    fireEvent.change(search, { target: { value: '' } });
    expect(screen.queryByTestId('design-tokens-empty')).toBeNull();
    expect(screen.getByTestId('design-tokens-count').textContent).toBe('9 of 9 tokens');
  });
});

describe('TokensTree detail pane and open-file hand-off', () => {
  it('selects a token and reports the file to the detail pane', () => {
    const onSelectAsset = vi.fn();
    renderTree({ onSelectAsset });

    fireEvent.click(screen.getByTestId('design-token-row-color-color.brand.primary'));

    expect(onSelectAsset).toHaveBeenCalledWith('design/tokens/color.tokens.json');
    expect(screen.getByTestId('design-tokens-tree').getAttribute('data-selected')).toBe('color.brand.primary');
    expect(screen.getByTestId('design-token-detail-value').textContent).toBe('#0055ff');
    expect(screen.getByTestId('design-token-schema-hint').textContent).toContain('$type color');
  });

  it('shows the resolved value of an alias token', () => {
    renderTree({ onSelectAsset: vi.fn() });
    fireEvent.click(screen.getByTestId('design-token-row-color-color.text'));
    expect(screen.getByTestId('design-token-detail-value').textContent).toBe('{color.brand.primary}');
    expect(screen.getByTestId('design-token-detail-resolved').textContent).toBe('#0055ff');
    expect(screen.getByTestId('design-token-detail-alias').getAttribute('data-alias-dangling')).toBe('false');
  });

  it('flags a dangling alias instead of resolving it', () => {
    const dangling = JSON.stringify({
      color: { broken: { $type: 'color', $value: '{color.absent}' } },
    });
    render(
      <SproutAdapterProvider>
        <TokensTree
          inventory={fixtureInventory()}
          contentByPath={{ ...CONTENT, 'design/tokens/motion.tokens.json': dangling }}
        />
      </SproutAdapterProvider>,
    );
    fireEvent.click(screen.getByTestId('design-token-row-motion-color.broken'));
    expect(screen.getByTestId('design-token-detail-alias').getAttribute('data-alias-dangling')).toBe('true');
  });

  it('opens the .tokens.json in the editor from the detail pane', () => {
    const onOpenFile = vi.fn();
    const onSelectAsset = vi.fn();
    renderTree({ onSelectAsset, onOpenFile });

    fireEvent.click(screen.getByTestId('design-token-row-color-spacing.lg'));
    const open = screen.getByTestId('design-token-open');
    expect(open.textContent).toContain('tokens/color.tokens.json');

    fireEvent.click(open);
    // The path handed to the editor mechanism is the `.tokens.json` file,
    // anchored at the token's line when the text has been read.
    expect(onOpenFile).toHaveBeenCalledTimes(1);
    expect(onOpenFile.mock.calls[0][0]).toContain('tokens/color.tokens.json');
  });

  it('renders no open button without an onOpenFile wiring', () => {
    renderTree();
    fireEvent.click(screen.getByTestId('design-token-row-color-color.brand.primary'));
    expect(screen.queryByTestId('design-token-open')).toBeNull();
  });

  it('shows the schema hint before any token is selected', () => {
    renderTree();
    expect(screen.getByTestId('design-tokens-schema').textContent).toContain('DTCG token schema');
    expect(screen.queryByTestId('design-tokens-token-detail')).toBeNull();
  });

  it('edits only through the structured value editor (§7c); no free-form fields', () => {
    renderTree({ onOpenFile: vi.fn() });
    fireEvent.click(screen.getByTestId('design-token-row-color-color.brand.primary'));
    const detail = screen.getByTestId('design-tokens-token-detail');
    // SP-140-7 §7c replaces the old read-only contract: the pane has exactly
    // the structured editor's inputs (value text + color well) — no other
    // editable field may appear.
    const editable = detail.querySelectorAll('input, textarea, [contenteditable="true"]');
    expect(editable.length).toBe(2);
    const testids = Array.from(editable).map((el) => el.getAttribute('data-testid'));
    expect(testids).toContain('design-token-input');
    expect(testids).toContain('design-token-color-well');
  });
});

describe('TokensTabContainer', () => {
  it('reads each token file through designApi.readAsset', async () => {
    mockedReadAsset.mockImplementation(async (_fetch, path: string) =>
      path === 'tokens/color.tokens.json' ? COLOR_TOKENS : MOTION_TOKENS,
    );

    render(
      <SproutAdapterProvider>
        <TokensTabContainer inventory={fixtureInventory()} />
      </SproutAdapterProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('design-tokens-tree').getAttribute('data-token-count')).toBe('9');
    });
    const read = mockedReadAsset.mock.calls.map((call) => call[1]);
    expect(read).toContain('tokens/color.tokens.json');
    expect(read).toContain('tokens/motion.tokens.json');
    expect(screen.getByTestId('design-token-row-color-color.brand.primary')).toBeTruthy();

    mockedReadAsset.mockReset();
  });
});
