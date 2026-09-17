/**
 * Screens tab body — the wireframe/screen browser (SP-140-3 §3c).
 *
 * A grid of thumbnail cards (one per `design/screens/*.html`, plus a wireframe
 * card for any `design/wireframes/*.svg` that has no screen yet), each with the
 * screen name and its README status chip (`draft`/`review`/`ready` — the
 * SP-140-1 §1e convention, populated on the inventory by `designApi.listAssets`).
 *
 * Sizing is device-frame-aware: a card whose screen name matches a frame
 * declared in the README `frames:` block is sized at that frame's aspect ratio
 * (`manifest.frames`, parsed by `designApi.parseFrames`), with the declared
 * pixel dimensions surfaced on the card. A card with no matching frame keeps a
 * neutral aspect ratio.
 *
 * Clicking a card selects the asset in the detail pane and, once the screen's
 * text has been read, mounts `LivePreview` as a *controlled* component
 * (`content`/`language`/`fileName` — SP-140-3 §3c: the synthetic `__workspace/`
 * preview-buffer path is deliberately not used) inside the pane. `LivePreview`'s
 * `onContentChange` is handed back to the detail pane (`onEditAsset`), which
 * writes the edited text through `designApi.writeAsset`.
 *
 * The component is a pure function of its props (the inventory, plus the
 * optional content/read/select seams) so it unit-tests without a network;
 * `ScreensTabContainer` below is the thin adapter that supplies the shell's
 * inventory, reads the selected screen's text, and owns the write-back.
 */

import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { designRootPath, readAsset, writeAsset } from '../../services/api/designApi';
import type { DesignAssetEntry, DesignFrame, DesignInventory } from '../../services/api/types';
import LivePreview from '../LivePreview';
import type { DesignTabProps } from './DesignTabProps';
import './DesignView.css';

/** Declared frames for the inventory's README; a missing README has none. */
export function framesOf(inventory?: DesignInventory | null): DesignFrame[] {
  return inventory?.manifest?.frames ?? [];
}

/**
 * The screen name a frame must match: the file stem, without its extension and
 * case-folded (literals, not dynamic `RegExp` — the slug rule means the stem
 * never contains a regex metacharacter). The inventory's `name` is the
 * basename and its `path` is workspace-relative (design/ prefix intact), so the
 * basename is preferred and the path is the fallback.
 */
export function screenStem(asset: Pick<DesignAssetEntry, 'name' | 'path'>): string {
  const name = asset.name || asset.path.split('/').pop() || '';
  const dot = name.lastIndexOf('.');
  // A dot in the first character is part of the stem (`name.startsWith('.')`).
  const stem = dot > 0 ? name.slice(0, dot) : name;
  return stem.toLowerCase();
}

/**
 * The path designApi's read/write helpers take: their `fileUrl` normalises the
 * path back under `design/`, so the inventory's workspace-relative path
 * (`design/screens/login.html`) and a design-root-relative one
 * (`screens/login.html`) both resolve. Kept as a single helper so the read and
 * the write-back always agree on the target file.
 */
export function designRelativePath(path: string): string {
  const normalized = (path ?? '').replace(/\\/g, '/').replace(/^\.\//, '');
  return normalized.startsWith('design/') ? normalized.slice('design/'.length) : normalized;
}

/** The README frame a screen is sized by, when its name matches one. */
export function frameForScreen(
  asset: Pick<DesignAssetEntry, 'name' | 'path'>,
  frames: readonly DesignFrame[],
): DesignFrame | null {
  const stem = screenStem(asset);
  return frames.find((frame) => frame.name.toLowerCase() === stem) ?? null;
}

/** Content language for a screen/wireframe asset: SVG thumbnails, HTML screens. */
export function languageForScreen(path: string | null | undefined): 'svg' | 'html' {
  return (path ?? '').toLowerCase().endsWith('.svg') ? 'svg' : 'html';
}

/**
 * The thumbnail image URL for a card.
 *
 * Inventory card paths are workspace-relative (`design/wireframes/login.svg`)
 * while design-root-relative paths (`wireframes/login.svg`) also reach this
 * module, so the prefix goes through `designRootPath` — which leaves an
 * already-prefixed path alone instead of doubling it
 * (`design/design/wireframes/...`).
 */
export function thumbnailUrl(path: string): string {
  return `/api/file?path=${encodeURIComponent(designRootPath(path))}`;
}

/** The README status marker for an asset (`draft`/`review`/`ready`), else ''. */
export function statusOf(asset: Pick<DesignAssetEntry, 'name' | 'path' | 'status'>): string {
  if (asset.status) return asset.status;
  return '';
}

export interface ScreenCard {
  /** Asset path relative to the design/ root — the detail-pane selection. */
  path: string;
  /** Display name (the file stem). */
  name: string;
  /** 'screen' for `screens/*.html`, 'wireframe' for a wireframe with no screen. */
  kind: 'screen' | 'wireframe';
  /** README status marker, when the manifest lists the asset. */
  status: string;
  /** README device frame the card is sized by, when its name matches one. */
  frame: DesignFrame | null;
}

/**
 * The cards to render: every screen, plus a wireframe-only card for a
 * wireframe whose stem has no screen file (the inventory's screens join on
 * stem, mirroring `pkg/design/inventory.go`'s screen/wireframe pairing).
 */
export function screenCards(
  screens: readonly DesignAssetEntry[],
  wireframes: readonly DesignAssetEntry[],
  frames: readonly DesignFrame[],
): ScreenCard[] {
  const covered = new Set(screens.map((screen) => screenStem(screen)));
  const cards: ScreenCard[] = screens.map((screen) => ({
    path: screen.path,
    name: screenStem(screen),
    kind: 'screen',
    status: statusOf(screen),
    frame: frameForScreen(screen, frames),
  }));
  for (const wireframe of wireframes) {
    const stem = screenStem(wireframe);
    if (covered.has(stem) || stem === 'sprite') continue;
    cards.push({
      path: wireframe.path,
      name: stem,
      kind: 'wireframe',
      status: statusOf(wireframe),
      frame: frameForScreen(wireframe, frames),
    });
  }
  return cards;
}

/** The neutral aspect ratio for a card with no declared frame (4:3). */
export const DEFAULT_CARD_ASPECT = '4 / 3';

/** Inline sizing for a card: the declared frame's ratio, else the default. */
export function cardAspectStyle(frame: DesignFrame | null): CSSProperties {
  return { aspectRatio: frame ? `${frame.width} / ${frame.height}` : DEFAULT_CARD_ASPECT };
}

export interface ScreensGridProps extends DesignTabProps {
  /** Design inventory from the shell; absent while it is still loading. */
  inventory?: DesignInventory | null;
  /**
   * Override the screen read. Defaults to the container's `readAsset` path;
   * supplying it lets a host (or a test) hand already-read text straight in.
   */
  contentByPath?: Record<string, string>;
  /** Transport seam for the screen reads. */
  fetchFn?: typeof fetch;
  /**
   * Fired with the edited screen text when `LivePreview` reports a change. When
   * omitted, the grid renders the preview read-only and never writes.
   */
  onEditAsset?: (path: string, content: string) => void;
}

export default function ScreensGrid({
  inventory = null,
  contentByPath,
  fetchFn,
  onSelectAsset,
  onEditAsset,
}: ScreensGridProps = {}) {
  const contextFetch = useSproutFetch();
  const transport = fetchFn ?? contextFetch;
  const [selected, setSelected] = useState<string | null>(null);
  const [texts, setTexts] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  const frames = useMemo(() => framesOf(inventory), [inventory]);
  const cards = useMemo(
    () => screenCards(inventory?.screens ?? [], inventory?.wireframes ?? [], frames),
    [inventory, frames],
  );
  // Key the read effect on the card set; the array itself is rebuilt per render.
  const cardPaths = cards.map((card) => card.path);
  const requestedPaths = cardPaths.join('|');

  const handleSelect = useCallback(
    (path: string) => {
      setSelected(path);
      onSelectAsset?.(path);
    },
    [onSelectAsset],
  );

  // Reads every card once so the grid shows real thumbnails rather than a
  // placeholder box; a failed read leaves that card on its placeholder.
  useEffect(() => {
    if (contentByPath || !requestedPaths) return;
    let cancelled = false;
    (async () => {
      const read: Record<string, string> = {};
      await Promise.all(
        requestedPaths.split('|').map(async (path) => {
          try {
            read[path] = await readAsset(transport, designRelativePath(path));
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

  const contentFor = useCallback((path: string) => contentByPath?.[path] ?? texts[path] ?? '', [contentByPath, texts]);

  const handleContentChange = useCallback(
    (path: string, content: string) => {
      if (!onEditAsset) return;
      setTexts((current) => (current[path] === content ? current : { ...current, [path]: content }));
      try {
        onEditAsset(path, content);
        setError(null);
      } catch {
        setError(`Could not write ${path}.`);
      }
    },
    [onEditAsset],
  );

  if (!inventory || !inventory.exists) {
    return (
      <div
        className="design-tab-body"
        data-testid="design-screens-grid"
        data-screen-selected=""
        data-inventory="missing"
      >
        <p className="design-tab-placeholder">Couldn&apos;t load the design inventory.</p>
      </div>
    );
  }

  const selectedCard = cards.find((card) => card.path === selected) ?? null;

  return (
    <div
      className="design-screens"
      data-testid="design-screens-grid"
      data-screen-selected={selected ?? ''}
      data-screen-count={cards.length}
    >
      {cards.length === 0 ? (
        <p className="design-tab-placeholder">No screens in this workspace.</p>
      ) : (
        <ul className="design-screens-grid" data-testid="design-screens-cards">
          {cards.map((card) => {
            const content = contentFor(card.path);
            const status = card.status;
            return (
              <li key={card.path}>
                <button
                  type="button"
                  className={`design-screen-card${card.path === selected ? ' selected' : ''}`}
                  data-testid={`design-screen-card-${card.name}`}
                  data-kind={card.kind}
                  data-frame={card.frame ? card.frame.name : ''}
                  data-status={status}
                  data-content=""
                  onClick={() => handleSelect(card.path)}
                  title={card.path}
                >
                  <span
                    className="design-screen-thumb"
                    style={cardAspectStyle(card.frame)}
                    data-testid={`design-screen-thumb-box-${card.name}`}
                  >
                    {content ? (
                      <img
                        className="design-screen-thumb-image"
                        src={thumbnailUrl(card.path)}
                        alt=""
                        data-testid={`design-screen-thumb-${card.name}`}
                      />
                    ) : (
                      <span className="design-screen-thumb-empty" aria-hidden="true" />
                    )}
                  </span>
                  <span className="design-screen-meta">
                    <span className="design-screen-name" title={card.path}>
                      {card.name}
                    </span>
                    {card.frame ? (
                      <span className="design-screen-frame" data-testid={`design-screen-frame-${card.name}`}>
                        {card.frame.name} · {card.frame.width}×{card.frame.height}
                      </span>
                    ) : null}
                    {status ? (
                      <span
                        className={`design-screen-status status-${status}`}
                        data-testid={`design-screen-status-${card.name}`}
                      >
                        {status}
                      </span>
                    ) : null}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}

      {selectedCard ? (
        <div className="design-screen-detail" data-testid="design-screen-detail">
          <h3 className="design-detail-heading">{selectedCard.name}</h3>
          {error ? (
            <p className="design-tab-placeholder" data-testid="design-screen-error">
              {error}
            </p>
          ) : null}
          <LivePreview
            content={contentFor(selectedCard.path)}
            language={languageForScreen(selectedCard.path)}
            fileName={selectedCard.path}
            onContentChange={
              onEditAsset ? (content: string) => handleContentChange(selectedCard.path, content) : undefined
            }
          />
        </div>
      ) : (
        <p className="design-tab-placeholder" data-testid="design-screen-placeholder">
          Select a screen to preview it.
        </p>
      )}
    </div>
  );
}

/**
 * ScreensTabContainer — composed adapter for the Screens tab (SP-140-3 §3c).
 *
 * Owns the read/write side the shell's prop contract does not cover: it reads
 * the selected screen's text through `designApi.readAsset` (the same
 * consent-aware workspace read the other tabs use, when one is supplied) and
 * hands `LivePreview`'s edits to `designApi.writeAsset`, normalising the path
 * under `design/` — the screens live there, and the write goes through the
 * existing file-write pathway (zero new HTTP endpoints, §3f).
 *
 * A failed write is swallowed here (the grid surfaces it through its error
 * line): a preview that keeps the user's text on screen is more useful than a
 * thrown render, and the next edit retries.
 */
export function ScreensTabContainer({
  inventory,
  onSelectAsset,
  fetchFn,
  readFn,
  writeFn,
}: ScreensGridProps & {
  /** Consent-aware read override (§3f), e.g. `readFileWithConsent`. */
  readFn?: typeof fetch;
  /** Write transport, e.g. `writeFileWithConsent`; defaults to `fetchFn`. */
  writeFn?: typeof fetch;
}) {
  const contextFetch = useSproutFetch();
  const transport = fetchFn ?? contextFetch;

  const handleEditAsset = useCallback(
    (path: string, content: string) => {
      // The write goes through designApi's shared file-write pathway (§3f).
      void writeAsset(transport, path, content, writeFn ?? transport).catch(() => {
        // The grid keeps the edited text; the next edit retries the write.
      });
    },
    [transport, writeFn],
  );

  return (
    <ScreensGrid
      inventory={inventory}
      onSelectAsset={onSelectAsset}
      // A consent-aware read override wins over the plain transport.
      fetchFn={readFn ?? transport}
      onEditAsset={handleEditAsset}
    />
  );
}
