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
 * SP-124b Phase 2: the payload may also carry `chain_length`,
 * `chain_subcommands`, and `chain_classifications` for chained commands.
 * These flow through unchanged into the parsed shape so the WebUI stepper
 * can render per-subcommand dots when ChainLength > 1.
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
  // SP-124b Phase 2: chain metadata for the stepper. 0 / undefined =
  // single-command analysis (no stepper).
  chainLength?: number;
  chainSubcommands?: string[];
  chainClassifications?: ('low' | 'moderate' | 'high')[];
}

const VALID_RISK_TONES = new Set(['low', 'moderate', 'high']);

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
    // SP-124b Phase 2: chain metadata. Only attach when chain_length is a
    // positive integer — single-command analyses keep the optional fields
    // undefined so the WebUI stepper is suppressed by the `chainLength > 1`
    // check in SecurityApprovalDialog.tsx.
    if (typeof parsed.chain_length === 'number' && Number.isFinite(parsed.chain_length) && parsed.chain_length > 1) {
      sa.chainLength = parsed.chain_length;
      if (Array.isArray(parsed.chain_subcommands)) {
        sa.chainSubcommands = parsed.chain_subcommands.filter((s): s is string => typeof s === 'string');
      }
      if (Array.isArray(parsed.chain_classifications)) {
        sa.chainClassifications = parsed.chain_classifications.filter(
          (s): s is 'low' | 'moderate' | 'high' => typeof s === 'string' && VALID_RISK_TONES.has(s),
        );
      }
    }
    return sa;
  } catch {
    return undefined;
  }
}
