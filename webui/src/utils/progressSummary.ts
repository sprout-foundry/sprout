/**
 * Deterministic template summaries for the SP-151 progress events
 * (SP-151 §151c, item 151.7). Mirror the Go templates in
 * pkg/cliui/progress_summary.go (item 151.6) exactly so the CLI and the
 * web UI progress strip render the same text. Each template is a pure
 * function of the event payload fields: no model call, no invented
 * detail. The optional summarizer-role model summary (item 151.8) is a
 * separate item and is not implemented here.
 */

import type {
  ProgressCompleteData,
  ProgressMilestoneData,
  ProgressQuestionData,
  ProgressVerificationCheck,
  ProgressVerificationData,
} from '@sprout/events';

/**
 * Render a progress_milestone event (SP-151 §151a) as a single line. A
 * coalesced batch (SP-151 §151b — the payload carries a "milestones"
 * array of flat payloads) renders as a count only; a flat milestone
 * renders the scope item's phase. Returns "" when there is nothing to
 * say.
 */
export function progressMilestoneSummary(data?: ProgressMilestoneData): string {
  if (data?.milestones) {
    // A coalesced run of milestones stays one line — do not expand the
    // batch.
    return `Milestones: ${data.milestones.length}`;
  }
  const phase = data?.phase ?? '';
  const title = data?.scope_title || data?.scope_id || '';
  if (phase === 'finished') {
    const label = title || 'run';
    const files = Number(data?.files_touched);
    if (Number.isFinite(files) && files > 0) {
      return `Finished: ${label} (${files} files)`;
    }
    return `Finished: ${label}`;
  }
  // "started" or an unrecognized phase. An empty or unknown phase with
  // no title has nothing to say.
  if (title === '') return '';
  return `Started: ${title}`;
}

/**
 * Shared passed/total logic over a checks list (SP-151 §151c):
 * "Checks: <passed>/<total> passed", or "" when the list is empty. A
 * check counts as passed only when its `passed` field is true — a
 * skipped check is never passed.
 */
function verificationChecksSummary(checks?: ProgressVerificationCheck[]): string {
  if (!checks || checks.length === 0) return '';
  const passed = checks.filter((c) => c.passed === true).length;
  return `Checks: ${passed}/${checks.length} passed`;
}

/**
 * Render a progress_verification event (SP-151 §151a) as a single
 * "Checks: <passed>/<total> passed" line. A skipped check is listed,
 * never counted as passed. Returns "" when nothing ran.
 */
export function progressVerificationSummary(data?: ProgressVerificationData): string {
  return verificationChecksSummary(data?.checks);
}

/**
 * Render a progress_complete event (SP-151 §151a) as a single line: a
 * verified run carries the final check count when the nested
 * verification has checks, an unverified run carries its reason when
 * one is present.
 */
export function progressCompleteSummary(data?: ProgressCompleteData): string {
  if (data?.verified === true) {
    const nested = verificationChecksSummary(data.verification?.checks);
    if (nested !== '') return `Run complete — verified (${nested})`;
    return 'Run complete — verified';
  }
  const reason = data?.not_verified_reason ?? '';
  if (reason !== '') return `Run complete — not verified (${reason})`;
  return 'Run complete — not verified';
}

/**
 * Render a progress_question event (SP-151 §151a) as a single line.
 * Returns "" when the question is empty.
 */
export function progressQuestionSummary(data?: ProgressQuestionData): string {
  const question = data?.question ?? '';
  if (question === '') return '';
  return `Needs a decision: ${question}`;
}

/**
 * Dispatch a progress event to its deterministic template summary
 * (SP-151 §151c). Returns "" for non-progress event types or when the
 * template has nothing to say.
 */
export function progressEventSummary(type: string, data: unknown): string {
  switch (type) {
    case 'progress_milestone':
      return progressMilestoneSummary(data as ProgressMilestoneData | undefined);
    case 'progress_verification':
      return progressVerificationSummary(data as ProgressVerificationData | undefined);
    case 'progress_complete':
      return progressCompleteSummary(data as ProgressCompleteData | undefined);
    case 'progress_question':
      return progressQuestionSummary(data as ProgressQuestionData | undefined);
    default:
      return '';
  }
}
