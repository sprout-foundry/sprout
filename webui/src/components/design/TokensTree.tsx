/**
 * Tokens tab body — the grouped DTCG browser (SP-140-3 §3d).
 *
 * Renders every `design/tokens/*.tokens.json` as a grouped tree (file → token
 * group → token), with a swatch per `color` token, font specimens for the
 * typography types, spacing bars for `dimension` tokens, and a type-appropriate
 * fallback for the remaining charter types. A search box filters the tree by
 * token path (§3d "search/filter across token paths").
 *
 * Read-only in v1: clicking a token selects it, and the detail pane's action
 * opens the `.tokens.json` in the editor (`onSelectAsset` puts the file in the
 * pane, `onOpenFile` hands it to the editor-tab mechanism) alongside a JSON
 * schema hint for validation — editing tokens is file editing (§3d).
 *
 * Split like the other tabs: the parsing/grouping rules live in the pure
 * `designTokens.ts` module and the type-specific specimen surfaces in
 * `TokenSpecimens.tsx`, so this file owns the tab's read/select wiring and
 * stays under the AGENTS.md 500-line rule.
 *
 * The component is a pure function of its props (the inventory, plus the
 * content/read/select seams) so it unit-tests without a network;
 * `TokensTabContainer` at the bottom is the thin adapter that reads each token
 * file through `designApi.readAsset` and supplies the shell's inventory.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { readAsset } from '../../services/api/designApi';
import type { DesignAssetEntry, DesignInventory } from '../../services/api/types';
import type { DesignTabProps } from './DesignTabProps';
import {
  anchorForToken,
  filterTokens,
  groupTokens,
  schemaHintForToken,
  tokenFileModel,
  tokenSchemaText,
  resolveTokenValue,
  type DesignToken,
  type DesignTokenFile,
  type TokenGrouping,
} from './designTokens';
import { SectionGroup, swatchStyle } from './TokenSpecimens';
import './DesignView.css';

/** Token files the tab knows about, from the inventory (workspace-relative). */
export function tokenFilesOf(inventory?: DesignInventory | null): DesignAssetEntry[] {
  return inventory?.tokenFiles ?? [];
}

/** The design-root-relative path a token file's read takes (`designApi` path rule). */
export function tokenRelativePath(path: string): string {
  const normalized = (path ?? '').replace(/\\/g, '/').replace(/^\.\//, '');
  return normalized.startsWith('design/') ? normalized.slice('design/'.length) : normalized;
}

/** File name shown for a token file: the file stem, without `.tokens.json`. */
export function tokenFileName(entry: Pick<DesignAssetEntry, 'name' | 'path'>): string {
  const name = entry.name || entry.path.split('/').pop() || entry.path;
  return name.replace(/\.tokens\.json$/, '') || name;
}

export interface TokensTreeProps extends DesignTabProps {
  /** Design inventory from the shell; absent while it is still loading. */
  inventory?: DesignInventory | null;
  /**
   * Override the per-file read: text keyed by the inventory's
   * workspace-relative path. Supplying it lets a host (or a test) hand
   * already-read text straight in, per the ScreensGrid convention.
   */
  contentByPath?: Record<string, string>;
  /** Transport seam for the file reads. */
  fetchFn?: typeof fetch;
  /** Fired when a token's source file should open in the editor. */
  onOpenFile?: (path: string) => void;
}

// ---------------------------------------------------------------------------
// Tab body
// ---------------------------------------------------------------------------

export default function TokensTree({
  inventory = null,
  contentByPath,
  fetchFn,
  onSelectAsset,
  onOpenFile,
}: TokensTreeProps = {}) {
  const contextFetch = useSproutFetch();
  const transport = fetchFn ?? contextFetch;
  const [query, setQuery] = useState('');
  const [texts, setTexts] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState<{ filePath: string; tokenPath: string } | null>(null);

  const entries = useMemo(() => tokenFilesOf(inventory), [inventory]);
  const requestedPaths = useMemo(() => entries.map((entry) => entry.path).join('|'), [entries]);

  // Read every token file once through designApi's read path (the same
  // consent-aware workspace read the other tabs use). A failed read leaves that
  // file empty; the model reports "not a DTCG document" rather than inventing
  // tokens.
  useEffect(() => {
    if (contentByPath || !requestedPaths) return;
    let cancelled = false;
    (async () => {
      const read: Record<string, string> = {};
      await Promise.all(
        requestedPaths.split('|').map(async (path) => {
          try {
            read[path] = await readAsset(transport, tokenRelativePath(path));
          } catch {
            read[path] = '';
          }
        }),
      );
      if (!cancelled) setTexts(read);
    })();
    return () => {
      cancelled = true;
    };
  }, [requestedPaths, contentByPath, transport]);

  const textFor = useCallback((path: string) => contentByPath?.[path] ?? texts[path] ?? '', [contentByPath, texts]);

  const files = useMemo<DesignTokenFile[]>(
    () => entries.map((entry) => tokenFileModel(entry.path, textFor(entry.path), tokenFileName(entry))),
    [entries, textFor],
  );

  const filtered = useMemo(
    () =>
      filterTokens(
        files.flatMap((file) => file.tokens),
        query,
      ),
    [files, query],
  );
  const groupings = useMemo(() => groupTokens(files, filtered), [files, filtered]);
  const total = files.reduce((sum, file) => sum + file.tokens.length, 0);

  const handleSelect = useCallback(
    (token: DesignToken) => {
      setSelected({ filePath: token.filePath, tokenPath: token.path });
      onSelectAsset?.(token.filePath);
    },
    [onSelectAsset],
  );

  const selectedToken = useMemo(() => {
    if (!selected) return null;
    return (
      files
        .find((file) => file.path === selected.filePath)
        ?.tokens.find((token) => token.path === selected.tokenPath) ?? null
    );
  }, [files, selected]);

  if (!inventory || !inventory.exists) {
    return (
      <div className="design-tab-body" data-testid="design-tokens-tree" data-inventory="missing">
        <p className="design-tab-placeholder">Couldn&apos;t load the design inventory.</p>
      </div>
    );
  }

  if (entries.length === 0) {
    return (
      <div className="design-tab-body" data-testid="design-tokens-tree" data-inventory="empty">
        <p className="design-tab-placeholder">No token files in this workspace.</p>
      </div>
    );
  }

  const selectedEntry = selected ? entries.find((entry) => entry.path === selected.filePath) : undefined;
  const selectedSourcePath = selectedEntry ? tokenRelativePath(selectedEntry.path) : '';
  // One resolution pass for the pane: an alias that did not resolve keeps its
  // own text, so "resolved !== value" is exactly the dangling case.
  const resolvedValue = selectedToken
    ? resolveTokenValue(
        selectedToken,
        files.flatMap((file) => file.tokens),
      )
    : '';
  const resolvedText = resolvedValue === '' || resolvedValue === undefined ? '—' : String(resolvedValue);
  const aliasResolved = selectedToken ? String(resolvedValue) !== selectedToken.valueText : false;

  return (
    <div
      className="design-tokens"
      data-testid="design-tokens-tree"
      data-token-count={total}
      data-visible-count={filtered.length}
      data-selected={selected ? (selectedToken?.path ?? '') : ''}
    >
      <div className="design-tokens-header">
        <label className="design-tokens-search-label" htmlFor="design-tokens-search">
          <span className="design-tokens-summary">Filter by token path</span>
          <input
            id="design-tokens-search"
            type="search"
            className="design-tokens-search"
            placeholder="e.g. color.brand"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            data-testid="design-tokens-search"
          />
        </label>
        <span className="design-tokens-summary" data-testid="design-tokens-count">
          {filtered.length} of {total} tokens
        </span>
      </div>

      <div className="design-tokens-files">
        {groupings.length === 0 ? (
          <p className="design-tab-placeholder" data-testid="design-tokens-empty">
            No tokens match “{query}”.
          </p>
        ) : (
          groupings.map((grouping: TokenGrouping) => (
            <section
              className="design-tokens-file"
              key={grouping.file.path}
              data-testid={`design-token-file-${grouping.file.name}`}
            >
              <h3 className="design-tokens-file-heading">
                {grouping.file.name}
                <span className="design-tokens-file-meta" data-testid={`design-token-file-count-${grouping.file.name}`}>
                  {grouping.file.path} · {grouping.file.tokens.length} tokens
                </span>
              </h3>
              {grouping.sections.map((entry) => (
                <SectionGroup
                  key={entry.section}
                  section={entry.section}
                  label={entry.label}
                  tokens={entry.tokens}
                  selected={selected ? `${selected.filePath}:${selected.tokenPath}` : null}
                  onSelect={handleSelect}
                />
              ))}
            </section>
          ))
        )}
      </div>

      {selectedToken ? (
        <div className="design-tokens-detail" data-testid="design-tokens-token-detail">
          <h3 className="design-detail-heading">{selectedToken.path}</h3>
          <span
            className="design-token-swatch design-token-swatch-lg"
            style={swatchStyle(selectedToken.value)}
            data-testid="design-token-detail-swatch"
            aria-hidden="true"
          />
          <div className="design-tokens-detail-row">
            <span className="design-tokens-detail-label">Value</span>
            <span className="design-tokens-detail-value" data-testid="design-token-detail-value">
              {selectedToken.valueText || '—'}
            </span>
          </div>
          <div className="design-tokens-detail-row">
            <span className="design-tokens-detail-label">Resolved</span>
            <span className="design-tokens-detail-value" data-testid="design-token-detail-resolved">
              {resolvedText}
            </span>
          </div>
          {selectedToken.alias ? (
            <div className="design-tokens-detail-row">
              <span className="design-tokens-detail-label">Alias</span>
              <span
                className={`design-tokens-detail-value design-tokens-alias${aliasResolved ? '' : ' is-dangling'}`}
                data-testid="design-token-detail-alias"
                data-alias-dangling={aliasResolved ? 'false' : 'true'}
              >
                {selectedToken.alias}
              </span>
            </div>
          ) : null}
          {selectedToken.description ? (
            <div className="design-tokens-detail-row">
              <span className="design-tokens-detail-label">Note</span>
              <span className="design-tokens-detail-value">{selectedToken.description}</span>
            </div>
          ) : null}
          <div className="design-tokens-detail-row">
            <span className="design-tokens-detail-label">Schema</span>
            <span className="design-tokens-detail-value" data-testid="design-token-schema-hint">
              {schemaHintForToken(selectedToken)}
            </span>
          </div>
          {selectedEntry && onOpenFile ? (
            <button
              type="button"
              className="design-detail-open"
              data-testid="design-token-open"
              onClick={() =>
                onOpenFile(anchorForToken(selectedSourcePath, textFor(selectedEntry.path), selectedToken.path))
              }
            >
              Open {selectedSourcePath} in editor
            </button>
          ) : null}
        </div>
      ) : (
        <p className="design-tab-placeholder" data-testid="design-tokens-hint">
          Select a token to inspect it; the schema for its file is shown here.
        </p>
      )}

      {!selectedToken ? (
        <details className="design-tokens-schema-wrap">
          <summary className="design-tokens-schema-summary">DTCG token schema reference</summary>
          <pre className="design-tokens-schema" data-testid="design-tokens-schema">
            {tokenSchemaText()}
          </pre>
        </details>
      ) : null}
    </div>
  );
}

/**
 * TokensTabContainer — composed adapter for the Tokens tab (§3d).
 *
 * The shell already fetches the inventory (which carries the token-file list
 * and per-file `tokenGroups` counts), so this adapter only wires the tab's
 * read transport and the editor hand-off; without a `fetchFn` override it uses
 * the context's consent-aware fetch, matching the other tabs.
 */
export function TokensTabContainer(props: TokensTreeProps) {
  const contextFetch = useSproutFetch();
  return <TokensTree {...props} fetchFn={props.fetchFn ?? contextFetch} />;
}
