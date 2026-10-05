/**
 * progressSummary tests — SP-151 §151c (item 151.7). Exact-string table
 * tests for the deterministic progress-event templates, mirroring the Go
 * 151.6 cases (pkg/cliui/progress_summary_test.go) so the CLI and the
 * web UI progress strip agree.
 */

import { describe, expect, it } from 'vitest';
import type {
  ProgressCompleteData,
  ProgressMilestoneData,
  ProgressQuestionData,
  ProgressVerificationCheck,
  ProgressVerificationData,
} from '@sprout/events';
import {
  progressCompleteSummary,
  progressEventSummary,
  progressMilestoneSummary,
  progressQuestionSummary,
  progressVerificationSummary,
} from './progressSummary';

// ── Payload builders (fill the required @sprout/events fields) ──────

function milestone(over: Partial<ProgressMilestoneData> = {}): ProgressMilestoneData {
  return { run_id: 'run-1', plan_revision: 3, elapsed_ms: 1000, ...over };
}

function verification(over: Partial<ProgressVerificationData> = {}): ProgressVerificationData {
  return { run_id: 'run-1', plan_revision: 3, checks: [], ...over };
}

function complete(over: Partial<ProgressCompleteData> = {}): ProgressCompleteData {
  return { run_id: 'run-1', plan_revision: 3, ...over };
}

function question(over: Partial<ProgressQuestionData> = {}): ProgressQuestionData {
  return { run_id: 'run-1', plan_revision: 3, ...over };
}

function check(over: Partial<ProgressVerificationCheck> = {}): ProgressVerificationCheck {
  return { kind: 'build', ...over };
}

describe('progressMilestoneSummary', () => {
  it('finished with title and files', () => {
    expect(
      progressMilestoneSummary(milestone({ phase: 'finished', scope_title: 'sign-up form', files_touched: 4 })),
    ).toBe('Finished: sign-up form (4 files)');
  });

  it('finished with title, no files key', () => {
    expect(progressMilestoneSummary(milestone({ phase: 'finished', scope_title: 'sign-up form' }))).toBe(
      'Finished: sign-up form',
    );
  });

  it('finished with zero files omits the count', () => {
    expect(
      progressMilestoneSummary(milestone({ phase: 'finished', scope_title: 'sign-up form', files_touched: 0 })),
    ).toBe('Finished: sign-up form');
  });

  it('started', () => {
    expect(progressMilestoneSummary(milestone({ phase: 'started', scope_title: 'billing' }))).toBe('Started: billing');
  });

  it('scope id falls back when the title is absent', () => {
    expect(progressMilestoneSummary(milestone({ phase: 'started', scope_id: 'item-3' }))).toBe('Started: item-3');
  });

  it('finished with no scope falls back to the run', () => {
    expect(progressMilestoneSummary(milestone({ phase: 'finished' }))).toBe('Finished: run');
  });

  it('finished with no scope but files', () => {
    expect(progressMilestoneSummary(milestone({ phase: 'finished', files_touched: 2 }))).toBe(
      'Finished: run (2 files)',
    );
  });

  it('coalesced batch renders the count only', () => {
    expect(
      progressMilestoneSummary(
        milestone({
          milestones: [
            { run_id: 'run-1', plan_revision: 3, elapsed_ms: 1, scope_id: 'a', phase: 'finished' },
            { run_id: 'run-1', plan_revision: 3, elapsed_ms: 1, scope_id: 'b', phase: 'finished' },
            { run_id: 'run-1', plan_revision: 3, elapsed_ms: 1, scope_id: 'c', phase: 'started' },
          ],
        }),
      ),
    ).toBe('Milestones: 3');
  });

  it('unknown phase with no title says nothing', () => {
    expect(progressMilestoneSummary(milestone({ phase: 'paused' }))).toBe('');
  });

  it('NaN files_touched omits the count', () => {
    expect(
      progressMilestoneSummary(
        milestone({ phase: 'finished', scope_title: 'sign-up form', files_touched: Number.NaN }),
      ),
    ).toBe('Finished: sign-up form');
  });
});

describe('progressVerificationSummary', () => {
  it('all passed', () => {
    expect(
      progressVerificationSummary(verification({ checks: [check({ passed: true }), check({ passed: true })] })),
    ).toBe('Checks: 2/2 passed');
  });

  it('one failed', () => {
    expect(
      progressVerificationSummary(
        verification({
          checks: [check({ passed: true }), check({ kind: 'test', passed: false, excerpt: 'FAIL' })],
        }),
      ),
    ).toBe('Checks: 1/2 passed');
  });

  it('skipped checks are not counted as passed', () => {
    expect(
      progressVerificationSummary(
        verification({
          checks: [check({ passed: true }), check({ kind: 'lint', skipped: true, reason: 'no trusted command' })],
        }),
      ),
    ).toBe('Checks: 1/2 passed');
  });

  it('all skipped', () => {
    expect(
      progressVerificationSummary(
        verification({
          checks: [check({ kind: 'lint', skipped: true }), check({ kind: 'smoke', skipped: true })],
        }),
      ),
    ).toBe('Checks: 0/2 passed');
  });

  it('nothing ran says nothing', () => {
    expect(progressVerificationSummary(verification({ checks: [] }))).toBe('');
  });

  it('missing checks key says nothing', () => {
    expect(progressVerificationSummary(undefined)).toBe('');
  });
});

describe('progressCompleteSummary', () => {
  it('verified with the final checks', () => {
    expect(
      progressCompleteSummary(
        complete({
          verified: true,
          verification: verification({
            passed: true,
            checks: [
              check({ passed: true }),
              check({ kind: 'test', passed: true }),
              check({ kind: 'smoke', passed: true }),
            ],
          }),
        }),
      ),
    ).toBe('Run complete — verified (Checks: 3/3 passed)');
  });

  it('verified without a nested verification', () => {
    expect(progressCompleteSummary(complete({ verified: true }))).toBe('Run complete — verified');
  });

  it('verified with an empty nested checks list', () => {
    expect(progressCompleteSummary(complete({ verified: true, verification: verification({ checks: [] }) }))).toBe(
      'Run complete — verified',
    );
  });

  it('not verified with a reason', () => {
    expect(progressCompleteSummary(complete({ not_verified_reason: 'verification disabled' }))).toBe(
      'Run complete — not verified (verification disabled)',
    );
  });

  it('not verified without a reason says nothing', () => {
    expect(progressCompleteSummary(complete({}))).toBe('');
  });

  it('not verified with the enabled-case reason still renders the notice', () => {
    expect(progressCompleteSummary(complete({ not_verified_reason: 'no code changes this turn' }))).toBe(
      'Run complete — not verified (no code changes this turn)',
    );
  });
});

describe('progressQuestionSummary', () => {
  it('question text', () => {
    expect(progressQuestionSummary(question({ question: 'Which DB?', options: [{ label: 'postgres' }] }))).toBe(
      'Needs a decision: Which DB?',
    );
  });

  it('empty question says nothing', () => {
    expect(progressQuestionSummary(question({ question: '' }))).toBe('');
  });

  it('missing data says nothing', () => {
    expect(progressQuestionSummary(undefined)).toBe('');
  });
});

// Dispatch: each of the four progress event types reaches its template,
// anything else renders nothing, and a progress event whose template is
// empty renders nothing.
describe('progressEventSummary', () => {
  it('progress_milestone reaches the milestone template', () => {
    expect(
      progressEventSummary(
        'progress_milestone',
        milestone({ phase: 'finished', scope_title: 'sign-up form', files_touched: 4 }),
      ),
    ).toBe('Finished: sign-up form (4 files)');
  });

  it('progress_verification reaches the verification template', () => {
    expect(
      progressEventSummary(
        'progress_verification',
        verification({ checks: [check({ passed: true }), check({ passed: true })] }),
      ),
    ).toBe('Checks: 2/2 passed');
  });

  it('progress_complete reaches the complete template', () => {
    expect(progressEventSummary('progress_complete', complete({ verified: true }))).toBe('Run complete — verified');
  });

  it('progress_question reaches the question template', () => {
    expect(progressEventSummary('progress_question', question({ question: 'Which DB?' }))).toBe(
      'Needs a decision: Which DB?',
    );
  });

  it('unknown type renders nothing', () => {
    expect(progressEventSummary('agent_message', milestone({ phase: 'finished' }))).toBe('');
    expect(progressEventSummary('totally_unknown', question({ question: 'Which DB?' }))).toBe('');
  });

  it('empty template renders nothing', () => {
    expect(progressEventSummary('progress_verification', verification({ checks: [] }))).toBe('');
    expect(progressEventSummary('progress_milestone', milestone({ phase: 'paused' }))).toBe('');
    expect(progressEventSummary('progress_question', question({ question: '' }))).toBe('');
  });
});
