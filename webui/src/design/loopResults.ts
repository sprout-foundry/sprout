/**
 * Loop-results model (SP-140-6 §6g).
 *
 * Pure helpers behind the detail pane's "loop results" section: the critique
 * sidecar path for an asset (the webui twin of pkg/agent_tools'
 * CritiqueFindingsSidecarPath — one naming rule, two languages, keep in
 * step), the stale marker rule (generated < asset mtime), and the Go status
 * payload's RFC3339 parsing.
 */

/** One persisted critique finding (the Go critiqueFinding JSON shape). */
export interface CritiqueFindingDoc {
  target: string;
  area: string;
  severity: string;
  note: string;
  suggestion?: string;
  line?: number;
  rule?: string;
}

/** The §6d findings sidecar's JSON shape (critiqueFindingsSidecarDoc). */
export interface CritiqueFindingsDoc {
  target: string;
  rubric: string;
  sourceHash?: string;
  generated: string;
  visual: boolean;
  findings: CritiqueFindingDoc[];
}

/**
 * The findings sidecar path for a design asset (workspace-relative). Mirrors
 * CritiqueFindingsSidecarPath in pkg/agent_tools/design_critique_findings_sidecar.go:
 * design/.cache/renders/findings/<label with / replaced by ->.findings.json.
 * The label slug cannot traverse: every separator becomes a hyphen.
 */
export function critiqueFindingsPathFor(assetPath: string): string {
  const normalized = (assetPath ?? '').replace(/\\/g, '/');
  const label = normalized.startsWith('design/') ? normalized : `design/${normalized}`;
  const slug = label.replace(/\//g, '-');
  return `design/.cache/renders/findings/${slug}.findings.json`;
}

/**
 * The §6g stale marker rule: the recorded critique is stale when it ran
 * before the asset's last modification (the inventory's `modified`, unix
 * seconds). An unparseable/missing timestamp is NOT stale — absence of
 * evidence is not staleness; the missing-sidecar state handles that case.
 */
export function critiqueIsStale(generated: string | undefined, assetModifiedSeconds: number | undefined): boolean {
  if (!generated) return false;
  const generatedMs = Date.parse(generated);
  if (!Number.isFinite(generatedMs)) return false;
  if (typeof assetModifiedSeconds !== 'number' || !Number.isFinite(assetModifiedSeconds)) return false;
  return generatedMs < assetModifiedSeconds * 1000;
}

/** Severity class for a finding chip (the critique vocabulary). */
export function severityClass(severity: string): string {
  const known = ['blocker', 'major', 'minor', 'info'];
  return known.includes(severity) ? severity : 'info';
}

/**
 * The adopt prompt for a code-ahead asset (§6g): the UI never applies sync —
 * it hands the endpoint's remedy to the agent as a prefill.
 */
export function adoptPrompt(remedy: string | undefined, assetPath: string): string {
  const base = remedy || "Run design_sync to import the implementation's semantic deltas into design/.";
  return `${base} (This asset — ${assetPath} — may be affected.)`;
}
