/**
 * FlowsCanvasContainer — composed adapter for the Flows tab (SP-140-3 §3b).
 *
 * Reads the active flow's `.mmd` text, its wireframes, and its `layout.json`
 * sidecar through `designApi`, then renders the presentational `FlowsCanvas`
 * as a pure function of workspace state. Kept separate from the canvas so the
 * heavy presentational module stays under the AGENTS.md 500-line rule.
 *
 * `flows` accepts either the inventory entries (the canvas reads their text
 * itself) or fully resolved `FlowCanvasFlow` values. Flow assets arrive as
 * `.mmd` files with no text on the inventory, so the union keeps the caller
 * from having to re-declare `text: ''` just to satisfy the prop type while
 * still letting a caller that has already read the source hand it straight in.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { readAsset } from '../../services/api/designApi';
import type { DesignAssetEntry, DesignLayoutSidecar } from '../../services/api/types';
import type { DesignTabProps } from './DesignTabProps';
import FlowsCanvas, { EMPTY_ASSETS, EMPTY_WIREFRAMES, selectCanvasFlow, type FlowCanvasFlow } from './FlowsCanvas';

export interface FlowsCanvasContainerProps extends DesignTabProps {
  /** Flow assets from the inventory, in rail order. */
  flows?: (DesignAssetEntry | FlowCanvasFlow)[];
  /** Wireframe assets from the inventory (node imagery). */
  wireframes?: DesignAssetEntry[];
  /** Sidecar asset paths from the inventory (`flows/<name>.layout.json`). */
  layouts?: DesignAssetEntry[];
  /** Flow selected by the caller; defaults to the first flow. */
  activeFlowPath?: string | null;
  /** `design_render` orientation hint shown/laid out for the active flow. */
  layoutHint?: string | null;
  /** Persist a dragged layout (item 3.6 seam; no write without it). */
  onPersistLayout?: (name: string, sidecar: DesignLayoutSidecar) => void;
  /** Fired when a node/edge pick should open its source in the editor. */
  onOpenFile?: (path: string) => void;
  /** Render the built-in status line; off when DesignView supplies its own. */
  showChrome?: boolean;
  /** Test seam: transport for the asset reads. */
  fetchFn?: typeof fetch;
  /**
   * Presentational-canvas override: when supplied, the flow text is taken from
   * these entries instead of being read through `designApi`. Used by the
   * container tests and by hosts that already hold the resolved flow text.
   */
  resolvedFlows?: FlowCanvasFlow[];
}

export function FlowsCanvasContainer({
  flows: flowEntries = EMPTY_ASSETS,
  wireframes = EMPTY_WIREFRAMES,
  layouts = EMPTY_WIREFRAMES,
  activeFlowPath = null,
  layoutHint = null,
  onSelectAsset,
  onOpenFile,
  onPersistLayout,
  showChrome = true,
  fetchFn,
  resolvedFlows,
}: FlowsCanvasContainerProps) {
  const contextFetch = useSproutFetch();
  const transport = fetchFn ?? contextFetch;
  const [texts, setTexts] = useState<Record<string, string>>({});

  // Flows, wireframes, and layout sidecars all arrive as text; the active
  // flow's sidecar is read in the same pass as the assets it positions.
  const requested = useMemo(
    () =>
      [
        ...flowEntries.map((asset) => asset.path),
        ...wireframes.map((asset) => asset.path),
        ...layouts.map((asset) => asset.path),
      ].join('|'),
    [flowEntries, wireframes, layouts],
  );
  const fetched = useRef('');
  useEffect(() => {
    // One read pass per unique path set: the path list is rebuilt from the
    // caller's arrays every render, so the effect is keyed on the joined string.
    if (fetched.current === requested) return;
    fetched.current = requested;
    let cancelled = false;
    (async () => {
      const read: Record<string, string> = {};
      await Promise.all(
        requested.split('|').map(async (path) => {
          if (!path) return;
          try {
            read[path] = await readAsset(transport, path);
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
  }, [requested, transport]);

  const flows = useMemo(
    () =>
      flowEntries.map((asset) => ({
        path: asset.path,
        name: (asset.name ?? asset.path).replace(/\.mmd$/, ''),
        text: resolvedFlows?.find((flow) => flow.path === asset.path)?.text ?? texts[asset.path] ?? '',
      })),
    [flowEntries, resolvedFlows, texts],
  );
  const wireframeText = useMemo(() => {
    const map: Record<string, string> = {};
    for (const asset of wireframes) map[asset.path] = texts[asset.path] ?? '';
    return map;
  }, [wireframes, texts]);

  const active = selectCanvasFlow(flows, activeFlowPath);
  const sidecarPath = active
    ? layouts.find((asset) => asset.path.includes(`${active.name}.layout.json`))?.path
    : undefined;
  const sidecar = useMemo(() => {
    const text = sidecarPath ? (texts[sidecarPath] ?? '') : '';
    return text ? parseSidecarText(text) : null;
  }, [sidecarPath, texts]);

  const handleLayoutChange = useCallback(
    (next: DesignLayoutSidecar) => {
      if (!active?.name || !onPersistLayout) return;
      onPersistLayout(active.name, next);
    },
    [active, onPersistLayout],
  );
  const handleLayoutPersist = useCallback(
    (next: DesignLayoutSidecar) => {
      if (!active?.name || !onPersistLayout) return;
      onPersistLayout(active.name, next);
    },
    [active, onPersistLayout],
  );

  // Click-through (§3b): the flow source is the only file that carries node and
  // edge identity, so both picks open it. The repo's editor tabs key off the
  // `.mmd` extension (`languageForFile` has no mermaid entry), so the opened
  // path keeps its extension and the mermaid text is what the user sees. A pick
  // that matches a statement opens at that line (`#L<n>`).
  const handleOpenSource = useCallback(
    (identity: string) => {
      if (!active?.path || !onOpenFile) return;
      const text = texts[active.path] ?? '';
      const lines = text.split('\n');
      const index = lines.findIndex((line: string) => line.includes(identity));
      onOpenFile(index < 0 ? active.path : `${active.path}#L${index + 1}`);
    },
    [active, onOpenFile, texts],
  );

  return (
    <FlowsCanvas
      flows={flows}
      activeFlowPath={activeFlowPath}
      wireframes={wireframes}
      wireframeText={wireframeText}
      sidecar={sidecar}
      layoutHint={layoutHint}
      onSelectAsset={onSelectAsset}
      onLayoutChange={handleLayoutChange}
      onLayoutPersist={handleLayoutPersist}
      showChrome={showChrome}
      onOpenSource={onOpenFile ? handleOpenSource : undefined}
    />
  );
}

/** Parse sidecar text defensively; unreadable JSON degrades to "no sidecar". */
function parseSidecarText(text: string): DesignLayoutSidecar | null {
  try {
    const value = JSON.parse(text) as DesignLayoutSidecar;
    if (!value || typeof value !== 'object' || typeof value.nodes !== 'object') return null;
    return value;
  } catch {
    return null;
  }
}
