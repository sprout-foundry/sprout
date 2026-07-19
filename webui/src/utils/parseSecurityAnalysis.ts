/**
 * Parse the LLM-derived security analysis payload from the
 * `security_analysis` event extra.
 *
 * SP-124 Phase 2: the Go backend (pkg/agent/security_analyzer.go → broker)
 * puts `extras["security_analysis"] = json.Marshal(SecurityAnalysis)` on
 * the WebSocket event. The wire field is a JSON-encoded STRING, not an
 * object — this parser decodes that string into the typed camelCase shape
 * the WebUI components consume.
 *
 * Returns undefined when:
 *  - the raw value is missing, empty, or not a string
 *  - JSON.parse fails (malformed payload from the LLM)
 *  - all four expected fields are absent after parsing
 *
 * Callers must treat undefined as "no analysis available" — the dialog
 * renders without the analysis block. The backend contract (SP-124) is
 * that analyzer failures are never blocking; this parser mirrors that
 * contract at the WebUI boundary: silent fall-through, never throw.
 */

export interface SecurityAnalysisShape {
  summary: string;
  modifies: string;
  riskAssessment: string;
  recommendation: string;
}

/**
 * Extract and parse the `security_analysis` field from a WS event payload.
 * Accepts the raw `eventData` (Record<string, unknown>) so handlers can
 * pass `event.data` directly without re-typing.
 */
export function parseSecurityAnalysis(eventData: unknown): SecurityAnalysisShape | undefined {
  if (eventData === null || typeof eventData !== 'object') return undefined;
  const raw = (eventData as Record<string, unknown>).security_analysis;
  return parseSecurityAnalysisString(raw);
}

/**
 * Lower-level parser accepting any candidate value. Each handler reaches
 * the field differently (`eventData.security_analysis` vs
 * `(data as unknown as Record<string, unknown>).security_analysis`) so
 * both entry points exist.
 */
export function parseSecurityAnalysisString(raw: unknown): SecurityAnalysisShape | undefined {
  if (typeof raw !== 'string' || !raw.trim()) return undefined;
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    if (parsed === null || typeof parsed !== 'object') return undefined;
    const sa: SecurityAnalysisShape = {
      summary: typeof parsed.summary === 'string' ? parsed.summary : '',
      modifies: typeof parsed.modifies === 'string' ? parsed.modifies : '',
      riskAssessment: typeof parsed.risk_assessment === 'string' ? parsed.risk_assessment : '',
      recommendation: typeof parsed.recommendation === 'string' ? parsed.recommendation : '',
    };
    if (!sa.summary && !sa.modifies && !sa.riskAssessment && !sa.recommendation) {
      return undefined;
    }
    return sa;
  } catch {
    return undefined;
  }
}
