/**
 * ScreenWorkbenchContainer — the read-side adapter for the §8b facet pane
 * (SP-140-8 item 8.2).
 *
 * Reads the raw files the screen brief derives over (SP-140-3 §3f's zero-new-
 * endpoint rule: everything goes through `designApi`'s `/api/file` path):
 * the selected asset's text (the render facet), the flow `.mmd` files (the
 * flows in/out), the README manifest (status + purpose), the token files
 * (the known-token set), and the screen's §4d feedback file (the open
 * annotations). The derivation itself is pure (`screenBrief.ts`); this
 * component owns only the fetch lifecycle — and the stale-read guard, so a
 * slow flow read for `login` never paints `dashboard`'s brief.
 *
 * Unreadable inputs degrade to the brief's empty facets (mirroring the Go
 * brief: an unreadable flow is skipped, a missing file is data) — only a
 * transport failure (5xx, off-workspace refusal) surfaces as an error line.
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { readAsset, readFeedback } from '../../services/api/designApi';
import type { DesignFeedbackAnnotation, DesignInventory } from '../../services/api/types';
import { deriveScreenBrief, stemOf, type ScreenBriefModel } from './screenBrief';
import { languageForScreen } from './ScreensGrid';
import ScreenWorkbench from './ScreenWorkbench';

export interface ScreenWorkbenchContainerProps {
  /** The design inventory the workbench renders from. */
  inventory: DesignInventory | null;
  /** The selected asset's path (a screen file or its wireframe). */
  selectedPath: string;
  /** Plain transport (defaults to the shell's fetch). */
  fetchFn?: typeof fetch;
  /** Consent-aware read override (§3f), e.g. `readFileWithConsent`. */
  readFn?: typeof fetch;
  /** The Flows facet's link: open the flow in the Flows canvas. */
  onOpenFlow?: (flowPath: string) => void;
  /** The Tokens facet's link: open the token library. */
  onOpenTokens?: () => void;
  /** The agent-panel prefill (the §6f panel flips to the Agent tab). */
  onAskAgent?: (prompt: string) => void;
  /** The open-feedback facet's "resolve" link (the §3f resolution flow). */
  onOpenFeedbackPane?: () => void;
  /** Fired after a status save (hosts refetch the inventory). */
  onStatusSaved?: () => void;
}

/** A file read that degrades to '' instead of failing the pane. */
async function readOrEmpty(read: () => Promise<string>): Promise<string> {
  try {
    return await read();
  } catch {
    return '';
  }
}

export function ScreenWorkbenchContainer({
  inventory,
  selectedPath,
  fetchFn,
  readFn,
  onOpenFlow,
  onOpenTokens,
  onAskAgent,
  onOpenFeedbackPane,
  onStatusSaved,
}: ScreenWorkbenchContainerProps) {
  const contextFetch = useSproutFetch();
  const transport = fetchFn ?? contextFetch;
  const read = readFn ?? transport;

  const [brief, setBrief] = useState<ScreenBriefModel | null>(null);
  const [render, setRender] = useState<{ content: string; path: string } | null>(null);
  const [annotations, setAnnotations] = useState<DesignFeedbackAnnotation[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const fetchSeq = useRef(0);

  // The inventory refetches on the live-tree tick with a fresh object identity
  // even when nothing changed; re-reading every flow + token file on each tick
  // is avoidable churn. Derive a content signature (paths + modified stamps)
  // and skip the effect when it is unchanged for the same selection.
  const inventoryKey = useMemo(() => {
    if (!inventory) return '';
    const parts: string[] = [
      inventory.manifest?.path ?? '',
      ...(inventory.flows ?? []).map((f) => `${f.path}:${f.modified}`),
      ...(inventory.tokenFiles ?? []).map((f) => `${f.path}:${f.modified}`),
      ...(inventory.wireframes ?? []).map((f) => `${f.path}:${f.modified}`),
      ...(inventory.screens ?? []).map((f) => `${f.path}:${f.modified}`),
      ...(inventory.feedback ?? []).map((f) => `${f.path}:${f.annotationCount}:${f.resolvedCount}`),
    ];
    return parts.join('|');
  }, [inventory]);

  useEffect(() => {
    if (!inventory || !selectedPath) {
      // Invalidate any in-flight fetch too — without the bump, a pending read
      // for the old selection still owns the newest seq and would paint its
      // stale brief after this branch already rendered the empty pane.
      fetchSeq.current += 1;
      setBrief(null);
      setRender(null);
      setAnnotations([]);
      setError('');
      setLoading(false);
      return;
    }
    const inv = inventory;
    const seq = ++fetchSeq.current;
    setLoading(true);
    setError('');
    (async () => {
      const stem = stemOf(selectedPath);
      const readmePath = inv.manifest?.path ?? 'design/README.md';
      const wireframePath =
        inv.wireframes.find((entry) => stemOf(entry.name) === stem)?.path ?? `design/wireframes/${stem}.svg`;
      // §8b "render first" prefers the delivered HTML screen: a wireframe
      // selection (the rail's screen buttons select wireframes) renders the
      // screen file when one exists — the kit's runtime, chrome, and states
      // live on the HTML tier, and the wireframe is the planning sketch.
      const screenEntry = inv.screens.find((entry) => stemOf(entry.name) === stem);
      const renderPath = screenEntry?.path ?? selectedPath;
      // The selected asset's text is the pane's anchor: a transport failure
      // there (a 404 is data — a missing file — and readAsset yields '')
      // surfaces as an error line and skips the degrade reads entirely.
      // Everything else degrades: an unreadable flow is skipped, a missing
      // token file drops from the known set — the brief's "missing is data"
      // rule (mirroring the Go brief).
      const renderText = await readAsset(read, renderPath);
      const [readmeText, feedbackFile, flowTexts, tokenTexts, wireframeText] = await Promise.all([
        readOrEmpty(() => readAsset(read, readmePath)),
        (async () => {
          try {
            return await readFeedback(transport, stem, read);
          } catch {
            return null;
          }
        })(),
        Promise.all(
          (inv.flows ?? []).map(async (flow) => [flow.path, await readOrEmpty(() => readAsset(read, flow.path))]),
        ),
        Promise.all(
          (inv.tokenFiles ?? []).map(async (tokenFile) => [
            tokenFile.path,
            await readOrEmpty(() => readAsset(read, tokenFile.path)),
          ]),
        ),
        readOrEmpty(() => readAsset(read, wireframePath)),
      ]);
      if (seq !== fetchSeq.current) return; // a newer selection owns the pane now

      setBrief(
        deriveScreenBrief({
          stem,
          inventory: inv,
          flowTexts: Object.fromEntries(flowTexts),
          wireframeText,
          readmeText,
          tokenTexts: Object.fromEntries(tokenTexts),
          feedback: feedbackFile,
        }),
      );
      setRender({ content: renderText, path: renderPath });
      setAnnotations(feedbackFile?.annotations ?? []);
      setLoading(false);
    })().catch((err: unknown) => {
      if (seq !== fetchSeq.current) return;
      setError(err instanceof Error ? err.message : String(err));
      setBrief(null);
      setRender(null);
      setLoading(false);
    });
    return undefined;
    // inventory is read via the local `inv` captured above; the effect
    // re-runs when the derived signature changes, not on object identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inventoryKey, selectedPath, transport, read]);

  if (error) {
    return (
      <p className="design-workbench-error" data-testid="design-workbench-error">
        {error}
      </p>
    );
  }
  if (!brief || !render || loading) {
    return (
      <p className="design-workbench-loading" data-testid="design-workbench-loading">
        Loading the screen brief…
      </p>
    );
  }

  return (
    <ScreenWorkbench
      brief={brief}
      renderContent={render.content}
      renderLanguage={languageForScreen(render.path)}
      renderFileName={render.path}
      annotations={annotations}
      onOpenFlow={onOpenFlow}
      onOpenTokens={onOpenTokens}
      onAskAgent={onAskAgent}
      onOpenFeedbackPane={onOpenFeedbackPane}
      onStatusSaved={onStatusSaved}
      readFn={readFn}
    />
  );
}
