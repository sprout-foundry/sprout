/**
 * LoopResults — the detail pane's loop-results section (SP-140-6 §6g).
 *
 * With an asset selected, shows:
 *  - Last critique: the §6d sidecar's findings (severity chip, area, note,
 *    suggestion), with a stale marker when the critique ran before the
 *    asset's last modification. A missing sidecar renders "no critique
 *    recorded" and offers the prefill prompt to run one.
 *  - Drift context: when the tree is code-ahead, the asset shows the
 *    "Adopt via agent" prefill built from the status endpoint's remedy. The
 *    UI never writes design/ — adoption is agent-mediated (§ premise).
 *
 * Reads go through the consent-aware read seam; failures render the empty
 * state (the section is advisory).
 */

import { FlaskConical, GitPullRequestArrow } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import {
  adoptPrompt,
  critiqueFindingsPathFor,
  critiqueIsStale,
  severityClass,
  type CritiqueFindingsDoc,
} from '../../design/loopResults';
import type { DesignStatusDriftRow } from '../../services/api/designStatusApi';

export interface LoopResultsProps {
  /** The selected asset's path, design-root-relative or workspace-relative. */
  path?: string | null;
  /** The asset's last modification (inventory `modified`, unix seconds). */
  assetModified?: number;
  /** The code-ahead drift row from the status endpoint, when ahead. */
  codeAhead?: DesignStatusDriftRow | null;
  /** Prefill the agent panel (the §6f prefill flow). */
  onAskAgent?: (prompt: string) => void;
  /** Read seam override (tests/hosts). */
  readFn?: typeof fetch;
}

export default function LoopResults({ path, assetModified, codeAhead, onAskAgent, readFn }: LoopResultsProps) {
  const contextFetch = useSproutFetch();
  const transport = readFn ?? contextFetch;
  const [doc, setDoc] = useState<CritiqueFindingsDoc | null>(null);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!path) {
      setDoc(null);
      setLoaded(false);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const response = await transport(`/api/file?path=${encodeURIComponent(critiqueFindingsPathFor(path))}`);
        if (!response.ok) throw new Error(String(response.status));
        const parsed = (await response.json()) as CritiqueFindingsDoc;
        if (!cancelled) {
          setDoc(parsed);
          setLoaded(true);
        }
      } catch {
        if (!cancelled) {
          setDoc(null);
          setLoaded(true);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [path, transport]);

  if (!path) return null;

  const stale = critiqueIsStale(doc?.generated, assetModified);

  return (
    <section className="design-loop-results" data-testid="design-loop-results" data-path={path}>
      <div className="design-loop-critique" data-testid="design-loop-critique">
        <h3 className="design-loop-heading">
          <FlaskConical size={12} /> Last critique
        </h3>
        {!loaded ? (
          <p className="design-loop-empty">Checking for a recorded critique…</p>
        ) : !doc ? (
          <p className="design-loop-empty" data-testid="design-loop-no-critique">
            No critique recorded.{' '}
            {onAskAgent && (
              <button
                type="button"
                className="design-loop-ask"
                data-testid="design-loop-run-critique"
                onClick={() =>
                  onAskAgent(`Run design_critique on ${path} (rubric: all) and fix any blocker or major findings.`)
                }
              >
                Ask the agent to run one
              </button>
            )}
          </p>
        ) : (
          <>
            <p className="design-loop-meta" data-testid="design-loop-critique-meta">
              {doc.rubric} · {doc.visual ? 'visual' : 'static'} · {doc.generated}
              {stale && (
                <span className="design-loop-stale" data-testid="design-loop-stale">
                  stale — the asset changed after this critique
                </span>
              )}
            </p>
            {doc.findings.length === 0 ? (
              <p className="design-loop-empty" data-testid="design-loop-clean">
                No findings recorded.
              </p>
            ) : (
              <ul className="design-loop-findings">
                {doc.findings.map((finding, index) => (
                  <li
                    key={`${finding.rule ?? 'vision'}-${finding.line ?? 0}-${index}`}
                    className={`design-loop-finding severity-${severityClass(finding.severity)}`}
                    data-testid={`design-loop-finding-${index}`}
                  >
                    <span className="design-loop-severity">{finding.severity}</span>
                    <span className="design-loop-area">{finding.area}</span>
                    <span className="design-loop-note">{finding.note}</span>
                    {finding.suggestion && <span className="design-loop-suggestion">{finding.suggestion}</span>}
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>

      {codeAhead?.ahead && onAskAgent && (
        <div className="design-loop-drift" data-testid="design-loop-drift">
          <h3 className="design-loop-heading">
            <GitPullRequestArrow size={12} /> Implementation ahead
          </h3>
          <p className="design-loop-meta">
            {codeAhead.summary ?? 'The implementation moved ahead of the semantic layer.'}
          </p>
          <button
            type="button"
            className="design-loop-ask"
            data-testid="design-loop-adopt"
            onClick={() => onAskAgent(adoptPrompt(codeAhead.remedy, path))}
          >
            Adopt via agent
          </button>
        </div>
      )}
    </section>
  );
}
