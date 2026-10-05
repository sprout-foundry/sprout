import { describe, expect, it } from 'vitest';
import type {
  ProgressCompleteData,
  ProgressMilestoneData,
  ProgressQuestionData,
  ProgressVerificationData,
  WsEvent,
} from '@sprout/events';
import type { ServerEventType } from './generated';

// ── Compile-time assertions (SP-151 §151d) ──────────────────────────
//
// The four SP-151 progress event types must be members of the
// @sprout/events WsEvent union, each carrying its ...Data payload, and of
// the webui ServerEventType mirror (webui/src/types/generated.ts). The
// aliases below fail type-checking if a member is removed or its data
// shape drifts from the exported ...Data interface.

type Expect<T extends true> = T;
type Equal<X, Y> = (<T>() => T extends X ? 1 : 2) extends <T>() => T extends Y ? 1 : 2 ? true : false;

type WsEventDataOf<T extends string> = Extract<WsEvent, { type: T }>['data'];

// A missing union member reduces Extract to `never`, so these only pass
// when the member exists AND its data type is exactly the ...Data shape.
type _MilestoneMember = Expect<Equal<WsEventDataOf<'progress_milestone'>, ProgressMilestoneData | undefined>>;
type _QuestionMember = Expect<Equal<WsEventDataOf<'progress_question'>, ProgressQuestionData | undefined>>;
type _VerificationMember = Expect<Equal<WsEventDataOf<'progress_verification'>, ProgressVerificationData | undefined>>;
type _CompleteMember = Expect<Equal<WsEventDataOf<'progress_complete'>, ProgressCompleteData | undefined>>;

// The webui mirror union must carry the same four literals.
type _ServerEventTypeMirror = Expect<
  Equal<
    Extract<
      ServerEventType,
      'progress_milestone' | 'progress_question' | 'progress_verification' | 'progress_complete'
    >,
    'progress_milestone' | 'progress_question' | 'progress_verification' | 'progress_complete'
  >
>;

describe('SP-151 progress event types (§151d)', () => {
  it('accepts a value of each new ...Data shape as a WsEvent', () => {
    const samples: WsEvent[] = [
      {
        type: 'progress_milestone',
        id: 'e1',
        timestamp: '2026-10-05T00:00:00Z',
        data: {
          run_id: 'run-1',
          plan_revision: 3,
          scope_id: 'scope-signup',
          scope_title: 'Sign-up form',
          phase: 'finished',
          files_touched: 4,
          elapsed_ms: 42000,
        },
      },
      {
        type: 'progress_question',
        data: {
          run_id: 'run-1',
          plan_revision: 3,
          scope_id: 'scope-signup',
          question: 'Which auth method?',
          header: 'Auth',
          options: [
            { label: 'OAuth', description: 'external IdP' },
            { label: 'Magic link', value: 'magic' },
          ],
          why_it_matters: 'locks in the session design',
        },
      },
      {
        type: 'progress_verification',
        data: {
          run_id: 'run-1',
          plan_revision: 3,
          passed: true,
          checks: [
            { kind: 'build', items: ['acc-1'], command: 'make build', passed: true },
            { kind: 'test', skipped: true, reason: 'no trusted test command' },
          ],
        },
      },
      {
        type: 'progress_complete',
        data: {
          run_id: 'run-1',
          plan_revision: 3,
          verified: true,
          verification: { run_id: 'run-1', plan_revision: 3, passed: true, checks: [] },
        },
      },
    ];

    for (const event of samples) {
      expect(event.type).toMatch(/^progress_(milestone|question|verification|complete)$/);
    }
  });

  it('carries each type in the ServerEventType mirror (generated.ts)', () => {
    const mirrorTypes: ServerEventType[] = [
      'progress_milestone',
      'progress_question',
      'progress_verification',
      'progress_complete',
    ];
    expect(mirrorTypes).toHaveLength(4);
    expect(mirrorTypes).toEqual([
      'progress_milestone',
      'progress_question',
      'progress_verification',
      'progress_complete',
    ]);
  });
});
