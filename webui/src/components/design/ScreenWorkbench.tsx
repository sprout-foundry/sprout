/**
 * ScreenWorkbench — the §8b facet pane (SP-140-8 item 8.2).
 *
 * One screen at a time: the selected screen's facets in the spec's fixed
 * order — render first (the screen is what you came to see), then status,
 * open feedback, flows in/out with triggers, token refs, and the agent-state
 * slice. The data is the §5g screen brief (SP-140-5 §5g) derived client-side
 * in `screenBrief.ts`; this component is its presentational view.
 *
 * Facet links are navigation only (spec §8b): the Flows and Tokens facets
 * link out to the library surfaces, the open-feedback notes action through
 * the existing §3f resolution flow (the Details pane) or the §6f agent panel
 * (via `onAskAgent`'s prefill), and the agent-state slice prefills a
 * screen-scoped query. Editing stays in the existing surfaces — the
 * workbench never writes a file.
 *
 * A presentational component: the model (`brief` + the render content + the
 * annotation list) arrives as props; the read side lives in
 * `ScreenWorkbenchContainer`.
 */

import React, { useState } from 'react';
import type { DesignFeedbackAnnotation } from '../../services/api/types';
import { designRootPath } from '../../services/api/designApiPaths';
import LivePreview from '../LivePreview';
import AnnotationPins from './AnnotationPins';
import type { ScreenBriefModel } from './screenBrief';
import ScreenStatusMenu from './ScreenStatusMenu';
import './ScreenWorkbench.css';

export interface ScreenWorkbenchProps {
  /** The §5g brief for the selected screen (the pane's data contract). */
  brief: ScreenBriefModel;
  /** The render facet's content (the selected screen file's text). */
  renderContent: string;
  /** 'svg' for a wireframe, 'html' for a delivered screen. */
  renderLanguage: 'svg' | 'html';
  /** The rendered file's name (LivePreview's editor label). */
  renderFileName: string;
  /** The screen's annotations (the pins over the render facet). */
  annotations: readonly DesignFeedbackAnnotation[];
  /** The Flows facet's link: open the flow in the Flows canvas. */
  onOpenFlow?: (flowPath: string) => void;
  /** The Tokens facet's link: open the token library. */
  onOpenTokens?: () => void;
  /** The agent-panel prefill (the §6f panel flips to the Agent tab). */
  onAskAgent?: (prompt: string) => void;
  /** The open-feedback facet's "resolve" link: the §3f resolution flow. */
  onOpenFeedbackPane?: () => void;
  /** Fired after a status save (the host refetches the inventory). */
  onStatusSaved?: () => void;
  /** Consent-aware read override for the status editor (tests/hosts). */
  readFn?: typeof fetch;
}

/** The screen files the agent-state slice names (the screen's working set). */
function agentFiles(brief: ScreenBriefModel): string[] {
  const flows = new Set<string>();
  for (const edge of [...brief.flowsIn, ...brief.flowsOut]) flows.add(edge.flow);
  const files: string[] = [];
  if (brief.wireframeExists) files.push(brief.wireframe);
  if (brief.screenFileExists) files.push(brief.screenFile);
  if (brief.feedback.path) files.push(brief.feedback.path);
  for (const flow of flows) files.push(flow);
  return files;
}

/** The screen-scoped query the agent-state slice prefills (the §5g brief). */
export function screenAgentPrompt(brief: ScreenBriefModel): string {
  const open = brief.feedback.open;
  const parts = [
    `Brief screen \`${brief.screenName}\` before working on it: use design_brief(depth=full)`,
    `and act on its facets — render, README status${brief.status ? ` (${brief.status})` : ''},`,
    open > 0 ? `${open} open annotation(s) in ${brief.feedback.path || 'its feedback file'},` : 'its open feedback,',
    `the flows in/out with triggers (${brief.flowsIn.length} in, ${brief.flowsOut.length} out), and its {group.token} refs.`,
  ];
  return parts.join(' ');
}

/** The screen-scoped query one open annotation's "ask the agent" prefills. */
export function annotationAgentPrompt(brief: ScreenBriefModel, annotation: DesignFeedbackAnnotation): string {
  const note = annotation.note?.trim() || '(no note)';
  const area = annotation.area ? ` (area: ${annotation.area})` : '';
  const feedback = brief.feedback.path ? ` Mark it resolved in ${brief.feedback.path}.` : '';
  return `Screen \`${brief.screenName}\` — open annotation${area}: ${note} Update the screen and${feedback}`;
}

function EdgeRow({
  edge,
  testid,
  onOpenFlow,
}: {
  edge: ScreenBriefModel['flowsIn'][number];
  testid: string;
  onOpenFlow?: (flowPath: string) => void;
}) {
  const other = edge.direction === 'both' ? edge.target : edge.otherStem;
  const label =
    edge.direction === 'in'
      ? `${other} → ${edge.target}`
      : edge.direction === 'both'
        ? `${edge.source} → ${edge.target} (self)`
        : `${edge.source} → ${other}`;
  const open = onOpenFlow ? () => onOpenFlow(edge.flow) : undefined;
  return (
    <div
      className="design-workbench-flow-row"
      data-testid={testid}
      // The row is the affordance (click anywhere on it opens the flow in the
      // canvas); the button inside is the keyboard target — its click
      // bubbles to the row's single handler, so both paths call once.
      onClick={open}
    >
      <span className="design-workbench-flow-label">{label}</span>
      {edge.trigger ? <span className="design-workbench-flow-trigger"> on “{edge.trigger}”</span> : null}
      {onOpenFlow ? (
        <button type="button" className="design-workbench-flow-open" data-testid={`${testid}-open`}>
          Open in Flows
        </button>
      ) : null}
    </div>
  );
}

export default function ScreenWorkbench({
  brief,
  renderContent,
  renderLanguage,
  renderFileName,
  annotations,
  onOpenFlow,
  onOpenTokens,
  onAskAgent,
  onOpenFeedbackPane,
  onStatusSaved,
  readFn,
}: ScreenWorkbenchProps) {
  const open = (annotations ?? []).filter((annotation) => !annotation.resolved);
  const [focusedAnnotation, setFocusedAnnotation] = useState<string | null>(null);
  const files = agentFiles(brief);

  if (!brief.found) {
    return (
      <div
        className="design-workbench"
        data-testid="design-workbench"
        data-screen={brief.screenName}
        data-found="false"
      >
        <p className="design-workbench-notfound" data-testid="design-workbench-not-found">
          {brief.guidance}
        </p>
      </div>
    );
  }

  return (
    <div className="design-workbench" data-testid="design-workbench" data-screen={brief.screenName} data-found="true">
      <header className="design-workbench-header" data-testid="design-workbench-header">
        <span className="design-workbench-screen-name">{brief.screenName}</span>
        {brief.status ? (
          <span
            className={`design-workbench-status design-workbench-status--${brief.status}`}
            data-testid="design-workbench-status-chip"
          >
            {brief.status}
          </span>
        ) : null}
        {open.length > 0 ? (
          <span
            className="design-workbench-open-count"
            data-testid="design-workbench-open-count"
            data-open-count={open.length}
          >
            {open.length} open
          </span>
        ) : null}
        {brief.purpose ? <span className="design-workbench-purpose">{brief.purpose}</span> : null}
      </header>

      {/* §8b 1 — Render (the screen is what you came to see). */}
      <section className="design-workbench-render" data-testid="design-workbench-render">
        <div className="design-workbench-render-stage">
          <LivePreview
            content={renderContent}
            language={renderLanguage}
            fileName={renderFileName}
            // Design screens preview through the §143.4 ref rewriter (the
            // iframe copy only); a wireframe SVG has no external refs.
            previewPath={renderLanguage === 'html' ? designRootPath(renderFileName) : undefined}
          />
          <AnnotationPins
            target={renderFileName}
            annotations={[...annotations]}
            selectedId={focusedAnnotation}
            onSelect={setFocusedAnnotation}
          />
        </div>
      </section>

      {/* §8b 2 — Status (the README marker + the §7.3 structured editor). */}
      <section className="design-workbench-facet" data-testid="design-workbench-status">
        <h3 className="design-workbench-facet-heading">Status</h3>
        <ScreenStatusMenu stem={brief.screenName} onSaved={onStatusSaved} readFn={readFn} />
      </section>

      {/* §8b 3 — Open feedback (each note actionable: resolve, or ask the agent). */}
      <section className="design-workbench-facet" data-testid="design-workbench-feedback">
        <h3 className="design-workbench-facet-heading">Open feedback</h3>
        {open.length === 0 ? (
          <p className="design-workbench-empty" data-testid="design-workbench-feedback-empty">
            {brief.feedback.path === ''
              ? 'No feedback file for this screen yet.'
              : brief.feedback.open === 0
                ? 'No open annotations.'
                : `The feedback file lists ${brief.feedback.open} open annotation(s) but its contents could not be read.`}
          </p>
        ) : (
          <>
            <ul className="design-workbench-note-list">
              {open.map((annotation) => (
                <li
                  key={annotation.id}
                  className={`design-workbench-note-row${focusedAnnotation === annotation.id ? ' focused' : ''}`}
                  data-testid={`design-workbench-annotation-${annotation.id}`}
                  data-area={annotation.area || ''}
                >
                  <span className="design-workbench-note-text">
                    {annotation.area ? `[${annotation.area}] ` : ''}
                    {annotation.note}
                  </span>
                  {onAskAgent ? (
                    <button
                      type="button"
                      className="design-workbench-note-ask"
                      data-testid={`design-workbench-annotation-ask-${annotation.id}`}
                      onClick={() => onAskAgent(annotationAgentPrompt(brief, annotation))}
                    >
                      Ask agent
                    </button>
                  ) : null}
                </li>
              ))}
            </ul>
            {onOpenFeedbackPane ? (
              <button
                type="button"
                className="design-workbench-note-resolve"
                data-testid="design-workbench-feedback-resolve"
                onClick={onOpenFeedbackPane}
              >
                Resolve in feedback pane
              </button>
            ) : null}
          </>
        )}
        {brief.feedback.resolution ? (
          <p className="design-workbench-resolution" data-testid="design-workbench-resolution">
            {brief.feedback.resolution}
          </p>
        ) : null}
      </section>

      {/* §8b 4 — Flows (the edges touching this screen, as links into the canvas). */}
      <section className="design-workbench-facet" data-testid="design-workbench-flows">
        <h3 className="design-workbench-facet-heading">Flows</h3>
        {brief.flowsIn.length === 0 && brief.flowsOut.length === 0 ? (
          <p className="design-workbench-empty" data-testid="design-workbench-flows-empty">
            No flows touch this screen yet.
          </p>
        ) : (
          <div className="design-workbench-flow-columns">
            <div>
              {brief.flowsIn.length > 0 ? <h4 className="design-workbench-flow-sub">In</h4> : null}
              <ul>
                {brief.flowsIn.map((edge, index) => (
                  <li key={`${edge.flow}-${edge.source}-${index}`} className="design-workbench-flow-item">
                    <EdgeRow edge={edge} testid={`design-workbench-flow-in-${edge.flowName}`} onOpenFlow={onOpenFlow} />
                  </li>
                ))}
              </ul>
            </div>
            <div>
              {brief.flowsOut.length > 0 ? <h4 className="design-workbench-flow-sub">Out</h4> : null}
              <ul>
                {brief.flowsOut.map((edge, index) => (
                  <li key={`${edge.flow}-${edge.target}-${index}`} className="design-workbench-flow-item">
                    <EdgeRow
                      edge={edge}
                      testid={`design-workbench-flow-out-${edge.flowName}`}
                      onOpenFlow={onOpenFlow}
                    />
                  </li>
                ))}
              </ul>
            </div>
          </div>
        )}
      </section>

      {/* §8b 5 — Tokens (the {group.token} refs, known vs. unknown). */}
      <section className="design-workbench-facet" data-testid="design-workbench-tokens">
        <h3 className="design-workbench-facet-heading">Tokens</h3>
        {brief.tokenRefs.length === 0 ? (
          <p className="design-workbench-empty" data-testid="design-workbench-tokens-empty">
            {brief.tokenGroups.length > 0
              ? `No {token} refs yet — available groups: ${brief.tokenGroups.join(', ')}.`
              : 'No token refs and no token groups in the tree.'}
          </p>
        ) : (
          <div className="design-workbench-token-chips">
            {brief.tokenRefs.map((ref) => (
              <span
                key={ref.path}
                className={`design-workbench-token-chip${ref.known ? '' : ' design-workbench-token-chip--unknown'}`}
                data-testid={`design-workbench-token-ref-${ref.path}`}
                data-known={ref.known ? 'true' : 'false'}
              >
                {`{${ref.path}}`}
                {ref.known ? null : ' (unknown)'}
              </span>
            ))}
          </div>
        )}
        {onOpenTokens ? (
          <button
            type="button"
            className="design-workbench-token-open"
            data-testid="design-workbench-token-link"
            onClick={onOpenTokens}
          >
            Open in Tokens library
          </button>
        ) : null}
      </section>

      {/* §8b 6 — Agent-state slice (the screen's files + the scoped query). */}
      <section className="design-workbench-facet" data-testid="design-workbench-agent">
        <h3 className="design-workbench-facet-heading">Agent</h3>
        <ul className="design-workbench-agent-files">
          {files.map((file) => (
            <li key={file} className="design-workbench-agent-file" data-testid="design-workbench-agent-file">
              {file}
            </li>
          ))}
        </ul>
        {onAskAgent ? (
          <button
            type="button"
            className="design-workbench-agent-ask"
            data-testid="design-workbench-agent-ask"
            onClick={() => onAskAgent(screenAgentPrompt(brief))}
          >
            Ask the agent about this screen
          </button>
        ) : null}
      </section>
    </div>
  );
}
